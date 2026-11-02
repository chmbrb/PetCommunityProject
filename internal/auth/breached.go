// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"bufio"
	"context"
	"crypto/sha1" //nolint:gosec // G505: SHA-1 is what the Pwned Passwords API is keyed on, not used for security here
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// BreachChecker reports whether a password is known from data breaches.
type BreachChecker interface {
	Breached(ctx context.Context, password string) (bool, error)
}

// PwnedPasswords checks passwords with the Have I Been Pwned "range" API using
// k-anonymity: only the first 5 hex characters of the password's SHA-1 hash are sent,
// and the service returns every breached hash starting with them, so it never learns
// the password or whether it matched. Responses are padded with fake entries, so their
// size doesn't give the answer away either.
type PwnedPasswords struct {
	baseURL string
	client  *http.Client
}

// NewPwnedPasswords returns a checker for api.pwnedpasswords.com.
func NewPwnedPasswords() *PwnedPasswords {
	return &PwnedPasswords{baseURL: "https://api.pwnedpasswords.com", client: &http.Client{Timeout: 3 * time.Second}}
}

// Breached reports whether the password appears in the breach corpus.
func (p *PwnedPasswords) Breached(ctx context.Context, password string) (bool, error) {
	sum := sha1.Sum([]byte(password)) //nolint:gosec // G401: see the import
	digest := strings.ToUpper(hex.EncodeToString(sum[:]))
	prefix, suffix := digest[:5], digest[5:]

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/range/"+prefix, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Add-Padding", "true")
	req.Header.Set("User-Agent", "PetCommunityProject")
	resp, err := p.client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("pwned passwords: %s", resp.Status)
	}
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		hashSuffix, count, ok := strings.Cut(strings.TrimSpace(sc.Text()), ":")
		// Padding entries have a count of 0.
		if ok && hashSuffix == suffix && count != "0" {
			return true, nil
		}
	}
	return false, sc.Err()
}
