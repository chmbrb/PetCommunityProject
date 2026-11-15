// SPDX-License-Identifier: AGPL-3.0-or-later

package media

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// evidenceVersion is the first byte of every sealed file, so the format can change later.
const evidenceVersion byte = 1

// ErrEvidenceCorrupt means sealed evidence failed to decrypt: altered, truncated,
// stored under another upload's ID, or sealed with a different PC_SECRET_KEY.
var ErrEvidenceCorrupt = errors.New("evidence can't be decrypted")

// EvidenceSealer encrypts the originals of uploads that matched known abuse imagery
// (ADR 0022), so the evidence bucket's contents are useless to anyone who obtains
// them without PC_SECRET_KEY. Files are bound to their upload's ID.
type EvidenceSealer struct {
	aead cipher.AEAD
}

// NewEvidenceSealer derives the encryption key from the 32-byte instance secret.
func NewEvidenceSealer(secret []byte) (*EvidenceSealer, error) {
	if len(secret) != 32 {
		return nil, fmt.Errorf("secret key must be 32 bytes, got %d", len(secret))
	}
	key, err := hkdf.Key(sha256.New, secret, nil, "PetCommunityProject abuse imagery evidence v1", 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &EvidenceSealer{aead: aead}, nil
}

// Seal encrypts an upload's original.
func (s *EvidenceSealer) Seal(mediaID uuid.UUID, original []byte) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := append([]byte{evidenceVersion}, nonce...)
	return s.aead.Seal(out, nonce, original, mediaID[:]), nil
}

// Open decrypts an upload's original, for the legally required report.
func (s *EvidenceSealer) Open(mediaID uuid.UUID, sealed []byte) ([]byte, error) {
	nonceSize := s.aead.NonceSize()
	if len(sealed) < 1+nonceSize || sealed[0] != evidenceVersion {
		return nil, ErrEvidenceCorrupt
	}
	original, err := s.aead.Open(nil, sealed[1:1+nonceSize], sealed[1+nonceSize:], mediaID[:])
	if err != nil {
		return nil, ErrEvidenceCorrupt
	}
	return original, nil
}
