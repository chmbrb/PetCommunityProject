// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// RFC 6238 appendix B test values for SHA-1, cut to 6 digits.
func TestTOTPCodeRFCVectors(t *testing.T) {
	secret := []byte("12345678901234567890")
	for unix, want := range map[int64]string{
		59: "287082", 1111111109: "081804", 1111111111: "050471",
		1234567890: "005924", 2000000000: "279037", 20000000000: "353130",
	} {
		if got := totpCode(secret, uint64(unix/30)); got != want {
			t.Errorf("code at %d = %s, want %s", unix, got, want)
		}
	}
}

func TestCheckTOTP(t *testing.T) {
	secret := []byte("12345678901234567890")
	now := time.Unix(1111111111, 0)
	step := now.Unix() / 30
	code := totpCode(secret, uint64(step))

	if got, ok := checkTOTP(secret, code[:3]+" "+code[3:], now, 0); !ok || got != step {
		t.Errorf("current code (spaced): step %d, ok %v", got, ok)
	}
	// One step of clock drift either way is accepted; two isn't.
	for d, want := range map[int64]bool{-2: false, -1: true, 1: true, 2: false} {
		if _, ok := checkTOTP(secret, totpCode(secret, uint64(step+d)), now, 0); ok != want {
			t.Errorf("code %+d steps away accepted = %v, want %v", d, ok, want)
		}
	}
	// A code can't be used again, nor can an earlier one once a later one has been.
	if _, ok := checkTOTP(secret, code, now, step); ok {
		t.Error("replayed code accepted")
	}
	if _, ok := checkTOTP(secret, totpCode(secret, uint64(step-1)), now, step); ok {
		t.Error("earlier code accepted after a later one was used")
	}
	for _, bad := range []string{"", "12345", "1234567", "abcdef"} {
		if _, ok := checkTOTP(secret, bad, now, 0); ok {
			t.Errorf("malformed code %q accepted", bad)
		}
	}
}

func TestRecoveryCodes(t *testing.T) {
	codes, err := newRecoveryCodes()
	if err != nil {
		t.Fatal(err)
	}
	format := regexp.MustCompile(`^[a-hjkmnp-z2-9]{4}-[a-hjkmnp-z2-9]{4}-[a-hjkmnp-z2-9]{4}$`)
	seen := map[string]bool{}
	for _, c := range codes {
		if !format.MatchString(c) {
			t.Errorf("code %q doesn't match the format", c)
		}
		if seen[c] {
			t.Errorf("duplicate code %q", c)
		}
		seen[c] = true
	}
	if len(codes) != recoveryCodeCount {
		t.Errorf("%d codes, want %d", len(codes), recoveryCodeCount)
	}
	// Typed in capitals, without dashes, or with spaces, a code still matches.
	want := string(hashRecoveryCode(codes[0]))
	for _, typed := range []string{" " + codes[0] + " ", strings.ToUpper(codes[0]), strings.ReplaceAll(codes[0], "-", "")} {
		if string(hashRecoveryCode(typed)) != want {
			t.Errorf("typed %q doesn't match %q", typed, codes[0])
		}
	}
}

func TestMFASealer(t *testing.T) {
	key := make([]byte, 32)
	m, err := newMFASealer(key)
	if err != nil {
		t.Fatal(err)
	}
	a, b := uuid.New(), uuid.New()
	sealed, err := m.seal(a, []byte("totp secret bytes!!!"))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := m.open(a, m.keyID, sealed); err != nil || string(got) != "totp secret bytes!!!" {
		t.Fatalf("open = %q, %v", got, err)
	}
	// Bound to the account: a secret copied to another account doesn't decrypt.
	if _, err := m.open(b, m.keyID, sealed); err == nil {
		t.Error("secret opened for a different account")
	}
	other, _ := newMFASealer(append(make([]byte, 31), 1))
	if _, err := other.open(a, m.keyID, sealed); !errors.Is(err, ErrMFAKeyMismatch) {
		t.Errorf("open with a different key: err = %v, want ErrMFAKeyMismatch", err)
	}
}
