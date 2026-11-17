// SPDX-License-Identifier: AGPL-3.0-or-later

package actorkeys

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func secret(b byte) []byte {
	return bytes.Repeat([]byte{b}, 32)
}

func TestGenerateAndParse(t *testing.T) {
	kp, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	if kp.PrivateKey.N.BitLen() != KeyBits {
		t.Errorf("key size = %d bits, want %d", kp.PrivateKey.N.BitLen(), KeyBits)
	}
	pub, err := ParsePublicKeyPEM(kp.PublicKeyPEM)
	if err != nil {
		t.Fatal(err)
	}
	if !pub.Equal(&kp.PrivateKey.PublicKey) {
		t.Error("published public key doesn't match the private key")
	}
	if _, err := ParsePublicKeyPEM("not pem"); err == nil {
		t.Error("ParsePublicKeyPEM accepted garbage")
	}
}

func TestSealOpenRoundTrip(t *testing.T) {
	kp, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewSealer(secret(7), nil)
	if err != nil {
		t.Fatal(err)
	}
	actor := uuid.New()
	sealed, err := s.Seal(actor, kp.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, kp.PrivateKey.D.Bytes()[:16]) {
		t.Fatal("sealed key contains plaintext key material")
	}

	opened, err := s.Open(actor, s.KeyID(), sealed)
	if err != nil {
		t.Fatal(err)
	}
	// The decrypted key signs, and the published public key verifies.
	digest := sha256.Sum256([]byte("(request-target): post /inbox"))
	sig, err := rsa.SignPKCS1v15(rand.Reader, opened, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	pub, _ := ParsePublicKeyPEM(kp.PublicKeyPEM)
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, digest[:], sig); err != nil {
		t.Errorf("signature from decrypted key doesn't verify: %v", err)
	}
}

func TestOpenRejects(t *testing.T) {
	kp, _ := Generate()
	s, _ := NewSealer(secret(7), nil)
	actor := uuid.New()
	sealed, _ := s.Seal(actor, kp.PrivateKey)

	other, _ := NewSealer(secret(8), nil)
	if s.KeyID() == other.KeyID() {
		t.Fatal("different secrets have the same key ID")
	}
	if _, err := other.Open(actor, s.KeyID(), sealed); !errors.Is(err, ErrWrongSecret) {
		t.Errorf("other secret: err = %v, want ErrWrongSecret", err)
	}

	if _, err := s.Open(uuid.New(), s.KeyID(), sealed); !errors.Is(err, ErrCorrupt) {
		t.Errorf("key moved to another actor: err = %v, want ErrCorrupt", err)
	}

	tampered := append([]byte(nil), sealed...)
	tampered[len(tampered)-1] ^= 1
	if _, err := s.Open(actor, s.KeyID(), tampered); !errors.Is(err, ErrCorrupt) {
		t.Errorf("tampered key: err = %v, want ErrCorrupt", err)
	}
	if _, err := s.Open(actor, s.KeyID(), sealed[:5]); !errors.Is(err, ErrCorrupt) {
		t.Errorf("truncated key: err = %v, want ErrCorrupt", err)
	}
}

func TestSealerKeyIDIsStable(t *testing.T) {
	a, _ := NewSealer(secret(7), nil)
	b, _ := NewSealer(secret(7), nil)
	if a.KeyID() != b.KeyID() || len(a.KeyID()) != 16 {
		t.Errorf("key IDs %q and %q, want equal 16-character IDs", a.KeyID(), b.KeyID())
	}
	if _, err := NewSealer(secret(7)[:16], nil); err == nil {
		t.Error("NewSealer accepted a 16-byte secret")
	}
}

func TestSealerDuringRotation(t *testing.T) {
	kp, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	actor := uuid.New()
	old, _ := NewSealer(secret(7), nil)
	sealedOld, _ := old.Seal(actor, kp.PrivateKey)

	rotating, err := NewSealer(secret(8), secret(7))
	if err != nil {
		t.Fatal(err)
	}
	if rotating.PreviousKeyID() != old.KeyID() || rotating.KeyID() == old.KeyID() {
		t.Fatalf("key IDs: current %s, previous %s, old %s", rotating.KeyID(), rotating.PreviousKeyID(), old.KeyID())
	}
	// Keys sealed under the old secret still open.
	if got, err := rotating.Open(actor, old.KeyID(), sealedOld); err != nil || !got.Equal(kp.PrivateKey) {
		t.Fatalf("open under the previous secret: %v", err)
	}
	// New seals use the current secret, which the old sealer can't open.
	sealedNew, _ := rotating.Seal(actor, kp.PrivateKey)
	if _, err := rotating.Open(actor, rotating.KeyID(), sealedNew); err != nil {
		t.Fatalf("open under the current secret: %v", err)
	}
	if _, err := old.Open(actor, rotating.KeyID(), sealedNew); !errors.Is(err, ErrWrongSecret) {
		t.Errorf("old sealer opened a new seal: %v", err)
	}
	// A third secret is unknown to both.
	other, _ := NewSealer(secret(9), nil)
	if _, err := rotating.Open(actor, other.KeyID(), sealedNew); !errors.Is(err, ErrWrongSecret) {
		t.Errorf("unknown key ID: %v", err)
	}
	if _, err := NewSealer(secret(8), secret(7)[:16]); err == nil {
		t.Error("short previous secret accepted")
	}
	if none, _ := NewSealer(secret(8), nil); none.PreviousKeyID() != "" {
		t.Error("previous key ID without a previous secret")
	}
}
