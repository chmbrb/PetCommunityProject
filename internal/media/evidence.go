// SPDX-License-Identifier: AGPL-3.0-or-later

package media

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// Sealed evidence starts with a format version. Version 1 is [1][nonce][ciphertext];
// version 2, written since key rotation (ADR 0025), adds the sealing key's ID:
// [2][8-byte key ID][nonce][ciphertext]. Both stay readable.
const (
	evidenceV1 byte = 1
	evidenceV2 byte = 2
	keyIDSize       = 8
)

// ErrEvidenceCorrupt means sealed evidence failed to decrypt: altered, truncated,
// stored under another upload's ID, or sealed with a different PC_SECRET_KEY.
var ErrEvidenceCorrupt = errors.New("evidence can't be decrypted")

// EvidenceSealer encrypts the originals of uploads that matched known abuse imagery
// (ADR 0022), so the evidence bucket's contents are useless to anyone who obtains
// them without PC_SECRET_KEY. Files are bound to their upload's ID. It seals under the
// current secret and opens under the current or, during a rotation, the previous one.
type EvidenceSealer struct {
	current  sealingKey
	previous *sealingKey
}

type sealingKey struct {
	aead cipher.AEAD
	id   []byte
}

// NewEvidenceSealer derives the encryption keys from the 32-byte instance secret and,
// while a rotation is under way, the previous one (nil otherwise).
func NewEvidenceSealer(secret, previous []byte) (*EvidenceSealer, error) {
	current, err := deriveEvidenceKey(secret)
	if err != nil {
		return nil, err
	}
	s := &EvidenceSealer{current: current}
	if previous != nil {
		p, err := deriveEvidenceKey(previous)
		if err != nil {
			return nil, fmt.Errorf("previous secret key: %w", err)
		}
		s.previous = &p
	}
	return s, nil
}

func deriveEvidenceKey(secret []byte) (sealingKey, error) {
	if len(secret) != 32 {
		return sealingKey{}, fmt.Errorf("secret key must be 32 bytes, got %d", len(secret))
	}
	key, err := hkdf.Key(sha256.New, secret, nil, "PetCommunityProject abuse imagery evidence v1", 32)
	if err != nil {
		return sealingKey{}, err
	}
	id, err := hkdf.Key(sha256.New, secret, nil, "PetCommunityProject abuse imagery evidence key ID v1", keyIDSize)
	if err != nil {
		return sealingKey{}, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return sealingKey{}, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return sealingKey{}, err
	}
	return sealingKey{aead: aead, id: id}, nil
}

// KeyID identifies the current secret, without revealing it.
func (s *EvidenceSealer) KeyID() string { return hex.EncodeToString(s.current.id) }

// PreviousKeyID identifies the secret being retired, or is empty when there's none.
func (s *EvidenceSealer) PreviousKeyID() string {
	if s.previous == nil {
		return ""
	}
	return hex.EncodeToString(s.previous.id)
}

// Seal encrypts an upload's original under the current secret, in format version 2.
func (s *EvidenceSealer) Seal(mediaID uuid.UUID, original []byte) ([]byte, error) {
	nonce := make([]byte, s.current.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := append([]byte{evidenceV2}, s.current.id...)
	out = append(out, nonce...)
	return s.current.aead.Seal(out, nonce, original, mediaID[:]), nil
}

// Open decrypts an upload's original, for the legally required report.
func (s *EvidenceSealer) Open(mediaID uuid.UUID, sealed []byte) ([]byte, error) {
	original, _, err := s.open(mediaID, sealed)
	return original, err
}

// UnderCurrentKey reports whether sealed evidence is already in the current format
// under the current secret, so re-encrypting it would change nothing. It opens the file
// to be sure, and reports an error if it can't be opened at all.
func (s *EvidenceSealer) UnderCurrentKey(mediaID uuid.UUID, sealed []byte) (bool, error) {
	_, current, err := s.open(mediaID, sealed)
	return current && len(sealed) > 0 && sealed[0] == evidenceV2, err
}

// open decrypts sealed evidence and says whether the current secret sealed it.
func (s *EvidenceSealer) open(mediaID uuid.UUID, sealed []byte) ([]byte, bool, error) {
	if len(sealed) == 0 {
		return nil, false, ErrEvidenceCorrupt
	}
	switch sealed[0] {
	case evidenceV2:
		if len(sealed) < 1+keyIDSize {
			return nil, false, ErrEvidenceCorrupt
		}
		id, body := sealed[1:1+keyIDSize], sealed[1+keyIDSize:]
		switch {
		case bytes.Equal(id, s.current.id):
			original, err := openWith(s.current, mediaID, body)
			return original, true, err
		case s.previous != nil && bytes.Equal(id, s.previous.id):
			original, err := openWith(*s.previous, mediaID, body)
			return original, false, err
		}
		return nil, false, ErrEvidenceCorrupt
	case evidenceV1:
		// No key ID: try the current secret, then the previous one. AES-GCM fails
		// cleanly with the wrong key, so only the right one opens it.
		body := sealed[1:]
		if original, err := openWith(s.current, mediaID, body); err == nil {
			return original, true, nil
		}
		if s.previous != nil {
			original, err := openWith(*s.previous, mediaID, body)
			return original, false, err
		}
		return nil, false, ErrEvidenceCorrupt
	}
	return nil, false, ErrEvidenceCorrupt
}

func openWith(k sealingKey, mediaID uuid.UUID, body []byte) ([]byte, error) {
	n := k.aead.NonceSize()
	if len(body) < n {
		return nil, ErrEvidenceCorrupt
	}
	original, err := k.aead.Open(nil, body[:n], body[n:], mediaID[:])
	if err != nil {
		return nil, ErrEvidenceCorrupt
	}
	return original, nil
}
