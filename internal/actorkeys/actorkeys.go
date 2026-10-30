// SPDX-License-Identifier: AGPL-3.0-or-later

// Package actorkeys creates actors' signing key pairs and encrypts their private keys
// at rest (ADR 0012).
//
// Keys are RSA-2048, the type Mastodon and most fediverse software expect for HTTP
// Signatures. Private keys are encrypted with AES-256-GCM under a key derived from the
// instance's PC_SECRET_KEY, bound to the actor's ID so an encrypted key copied to
// another actor won't decrypt.
package actorkeys

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
)

// KeyBits is the RSA key size.
const KeyBits = 2048

// sealVersion is the first byte of every encrypted key, so the format can change later.
const sealVersion byte = 1

var (
	// ErrWrongSecret means a key was encrypted under a different PC_SECRET_KEY.
	ErrWrongSecret = errors.New("actor key was encrypted with a different PC_SECRET_KEY")
	// ErrCorrupt means an encrypted key failed to decrypt: altered, truncated, or
	// stored on the wrong actor.
	ErrCorrupt = errors.New("actor key can't be decrypted")
)

// KeyPair is a new actor key pair.
type KeyPair struct {
	PublicKeyPEM string
	PrivateKey   *rsa.PrivateKey
}

// Generate creates a key pair.
func Generate() (KeyPair, error) {
	priv, err := rsa.GenerateKey(rand.Reader, KeyBits)
	if err != nil {
		return KeyPair{}, fmt.Errorf("generate actor key: %w", err)
	}
	der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		return KeyPair{}, err
	}
	pub := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	return KeyPair{PublicKeyPEM: string(pub), PrivateKey: priv}, nil
}

// ParsePublicKeyPEM reads a public key in the PEM form ActivityPub publishes.
func ParsePublicKeyPEM(s string) (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(s))
	if block == nil || block.Type != "PUBLIC KEY" {
		return nil, errors.New("not a PEM public key")
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	rsaKey, ok := key.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("public key isn't RSA")
	}
	return rsaKey, nil
}

// Sealer encrypts and decrypts private keys.
type Sealer struct {
	aead  cipher.AEAD
	keyID string
}

// NewSealer derives the encryption key from the 32-byte instance secret.
func NewSealer(secret []byte) (*Sealer, error) {
	if len(secret) != 32 {
		return nil, fmt.Errorf("secret key must be 32 bytes, got %d", len(secret))
	}
	encKey, err := hkdf.Key(sha256.New, secret, nil, "PetCommunityProject actor private keys v1", 32)
	if err != nil {
		return nil, err
	}
	idBytes, err := hkdf.Key(sha256.New, secret, nil, "PetCommunityProject actor private keys key ID v1", 8)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(encKey)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Sealer{aead: aead, keyID: hex.EncodeToString(idBytes)}, nil
}

// KeyID identifies the secret, without revealing it. It's stored next to each
// encrypted key, so a wrong or rotated secret is detected rather than failing obscurely.
func (s *Sealer) KeyID() string { return s.keyID }

// Seal encrypts a private key for an actor.
func (s *Sealer) Seal(actorID uuid.UUID, key *rsa.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := append([]byte{sealVersion}, nonce...)
	return s.aead.Seal(out, nonce, der, actorID[:]), nil
}

// Open decrypts an actor's private key.
func (s *Sealer) Open(actorID uuid.UUID, keyID string, sealed []byte) (*rsa.PrivateKey, error) {
	if keyID != s.keyID {
		return nil, ErrWrongSecret
	}
	nonceSize := s.aead.NonceSize()
	if len(sealed) < 1+nonceSize || sealed[0] != sealVersion {
		return nil, ErrCorrupt
	}
	der, err := s.aead.Open(nil, sealed[1:1+nonceSize], sealed[1+nonceSize:], actorID[:])
	if err != nil {
		return nil, ErrCorrupt
	}
	key, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, ErrCorrupt
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, ErrCorrupt
	}
	return rsaKey, nil
}

// Job asks the worker to create an actor's key pair. Keys are generated in the
// worker, which holds PC_SECRET_KEY; the API never needs the secret.
type Job struct {
	ActorID uuid.UUID `json:"actor_id"`
}

// Kind identifies the job type to River.
func (Job) Kind() string { return "actor_keys" }

// InsertOpts makes the job unique per actor.
func (Job) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 5, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}
