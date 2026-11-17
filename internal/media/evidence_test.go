// SPDX-License-Identifier: AGPL-3.0-or-later

package media

import (
	"bytes"
	"crypto/rand"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func secretOf(b byte) []byte { return bytes.Repeat([]byte{b}, 32) }

// sealV1 writes evidence the way it was sealed before key rotation: no key ID.
func sealV1(t *testing.T, secret []byte, mediaID uuid.UUID, original []byte) []byte {
	t.Helper()
	k, err := deriveEvidenceKey(secret)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, k.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	return k.aead.Seal(append([]byte{evidenceV1}, nonce...), nonce, original, mediaID[:])
}

func TestEvidenceV2NamesItsKey(t *testing.T) {
	s, _ := NewEvidenceSealer(secretOf(1), nil)
	id := uuid.New()
	sealed, err := s.Seal(id, []byte("original"))
	if err != nil {
		t.Fatal(err)
	}
	if sealed[0] != evidenceV2 || !bytes.Equal(sealed[1:1+keyIDSize], s.current.id) {
		t.Fatalf("sealed header = %x", sealed[:1+keyIDSize])
	}
	if got, err := s.Open(id, sealed); err != nil || string(got) != "original" {
		t.Fatalf("open = %q, %v", got, err)
	}
	if current, err := s.UnderCurrentKey(id, sealed); err != nil || !current {
		t.Errorf("UnderCurrentKey = %v, %v", current, err)
	}
}

func TestEvidenceDuringRotation(t *testing.T) {
	id := uuid.New()
	old, _ := NewEvidenceSealer(secretOf(1), nil)
	sealedOld, _ := old.Seal(id, []byte("original"))
	v1 := sealV1(t, secretOf(1), id, []byte("original"))

	rotating, err := NewEvidenceSealer(secretOf(2), secretOf(1))
	if err != nil {
		t.Fatal(err)
	}
	if rotating.PreviousKeyID() != old.KeyID() {
		t.Fatalf("previous key ID = %s, want %s", rotating.PreviousKeyID(), old.KeyID())
	}
	for name, sealed := range map[string][]byte{"version 2 under the previous key": sealedOld, "version 1": v1} {
		if got, err := rotating.Open(id, sealed); err != nil || string(got) != "original" {
			t.Errorf("%s: open = %q, %v", name, got, err)
		}
		if current, err := rotating.UnderCurrentKey(id, sealed); err != nil || current {
			t.Errorf("%s: UnderCurrentKey = %v, %v; want false", name, current, err)
		}
	}
	// After rotation, the old sealer can't read what the new one sealed.
	sealedNew, _ := rotating.Seal(id, []byte("original"))
	if _, err := old.Open(id, sealedNew); !errors.Is(err, ErrEvidenceCorrupt) {
		t.Errorf("old sealer opened new evidence: %v", err)
	}
}

func TestEvidenceV1UnderTheCurrentKeyStillNeedsRewriting(t *testing.T) {
	id := uuid.New()
	s, _ := NewEvidenceSealer(secretOf(1), nil)
	v1 := sealV1(t, secretOf(1), id, []byte("original"))
	if got, err := s.Open(id, v1); err != nil || string(got) != "original" {
		t.Fatalf("open version 1 = %q, %v", got, err)
	}
	if current, err := s.UnderCurrentKey(id, v1); err != nil || current {
		t.Errorf("version 1 counts as current: %v, %v", current, err)
	}
}

func TestEvidenceRejects(t *testing.T) {
	id := uuid.New()
	s, _ := NewEvidenceSealer(secretOf(1), nil)
	sealed, _ := s.Seal(id, []byte("original"))
	other, _ := NewEvidenceSealer(secretOf(3), nil)
	tampered := bytes.Clone(sealed)
	tampered[len(tampered)-1] ^= 1
	for name, tc := range map[string]struct {
		sealer *EvidenceSealer
		id     uuid.UUID
		data   []byte
	}{
		"unknown key":       {other, id, sealed},
		"another upload":    {s, uuid.New(), sealed},
		"tampered":          {s, id, tampered},
		"truncated":         {s, id, sealed[:5]},
		"empty":             {s, id, nil},
		"unknown version":   {s, id, append([]byte{9}, sealed[1:]...)},
		"version 1, no key": {other, id, sealV1(t, secretOf(1), id, []byte("x"))},
	} {
		if _, err := tc.sealer.Open(tc.id, tc.data); !errors.Is(err, ErrEvidenceCorrupt) {
			t.Errorf("%s: err = %v, want ErrEvidenceCorrupt", name, err)
		}
	}
	if _, err := NewEvidenceSealer(secretOf(1), secretOf(1)[:16]); err == nil {
		t.Error("short previous secret accepted")
	}
}
