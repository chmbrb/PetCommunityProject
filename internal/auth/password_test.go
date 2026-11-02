// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"crypto/sha1" //nolint:gosec // G505: the Pwned Passwords API is keyed on SHA-1
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/crypto/argon2"
)

func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := hashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Errorf("hash = %q, want argon2id with the current parameters", hash)
	}
	if ok, rehash := verifyPassword("correct horse battery staple", hash); !ok || rehash {
		t.Errorf("verify right password = %v, rehash %v; want true, false", ok, rehash)
	}
	if ok, _ := verifyPassword("correct horse battery stapl", hash); ok {
		t.Error("wrong password verified")
	}
	other, _ := hashPassword("correct horse battery staple")
	if other == hash {
		t.Error("two hashes of the same password are identical; salts aren't random")
	}
	for _, bad := range []string{"", "plain", "$argon2i$v=19$m=1,t=1,p=1$c2FsdA$aGFzaA", "$argon2id$v=19$m=x$a$b"} {
		if ok, _ := verifyPassword("anything", bad); ok {
			t.Errorf("malformed hash %q verified", bad)
		}
	}
}

func TestOldHashesAreUpgraded(t *testing.T) {
	salt := []byte("0123456789abcdef")
	key := argon2.IDKey([]byte("an older password"), salt, 1, 8*1024, 1, 32)
	b64 := base64.RawStdEncoding
	old := fmt.Sprintf("$argon2id$v=19$m=8192,t=1,p=1$%s$%s", b64.EncodeToString(salt), b64.EncodeToString(key))
	if ok, rehash := verifyPassword("an older password", old); !ok || !rehash {
		t.Errorf("old-parameter hash: ok %v, rehash %v; want true, true", ok, rehash)
	}
}

func TestCheckNotIdentity(t *testing.T) {
	for _, pw := range []string{"sam.jones@example.org", "SAM.JONES", "  sam_jones  "} {
		if err := checkNotIdentity(pw, "sam.jones@example.org", "sam_jones"); err == nil {
			t.Errorf("password %q accepted, want refused", pw)
		}
	}
	if err := checkNotIdentity("a long, unrelated passphrase", "sam.jones@example.org", "sam_jones"); err != nil {
		t.Errorf("unrelated password refused: %v", err)
	}
}

func TestPwnedPasswords(t *testing.T) {
	sum := sha1.Sum([]byte("password123456")) //nolint:gosec // G401: see the import
	digest := strings.ToUpper(hex.EncodeToString(sum[:]))
	var gotPath, gotPadding string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotPadding = r.URL.Path, r.Header.Get("Add-Padding")
		// A padding entry for another password (count 0), then the real match.
		fmt.Fprintf(w, "0000000000000000000000000000000000A:0\r\n%s:42\r\n", digest[5:])
	}))
	defer srv.Close()
	p := &PwnedPasswords{baseURL: srv.URL, client: srv.Client()}

	breached, err := p.Breached(t.Context(), "password123456")
	if err != nil || !breached {
		t.Fatalf("Breached = %v, %v; want true", breached, err)
	}
	if gotPath != "/range/"+digest[:5] || gotPadding != "true" {
		t.Errorf("request path %q, Add-Padding %q; want only the 5-character prefix, with padding", gotPath, gotPadding)
	}
	if breached, _ := p.Breached(t.Context(), "an unlisted passphrase"); breached {
		t.Error("unlisted password reported as breached")
	}

	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer down.Close()
	if _, err := (&PwnedPasswords{baseURL: down.URL, client: down.Client()}).Breached(t.Context(), "x"); err == nil {
		t.Error("service error not reported")
	}
}
