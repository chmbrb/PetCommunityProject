// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/chmbrb/PetCommunityProject/internal/auth"
	"github.com/chmbrb/PetCommunityProject/internal/config"
	"github.com/chmbrb/PetCommunityProject/internal/ratelimit"
)

var tokenInLink = regexp.MustCompile(`#token=([A-Za-z0-9_-]+)`)

// queuedEmails counts email jobs queued for an account with a purpose.
func (e *authTestEnv) queuedEmails(accountID, purpose string) int {
	e.t.Helper()
	var n int
	err := e.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM river_job WHERE kind = 'email' AND args->>'account_id' = $1 AND args->>'purpose' = $2`,
		accountID, purpose).Scan(&n)
	if err != nil {
		e.t.Fatal(err)
	}
	return n
}

// deliverEmail does what the worker would for a queued email job and returns the
// token from the link in the email.
func (e *authTestEnv) deliverEmail(accountID, purpose, wantPath string) string {
	e.t.Helper()
	if e.queuedEmails(accountID, purpose) == 0 {
		e.t.Fatalf("no %s email queued for account %s", purpose, accountID)
	}
	before := len(e.mail.Messages())
	if err := e.emailer.Send(context.Background(), auth.EmailJob{AccountID: uuid.MustParse(accountID), Purpose: purpose}); err != nil {
		e.t.Fatalf("send %s email: %v", purpose, err)
	}
	msgs := e.mail.Messages()
	if len(msgs) != before+1 {
		e.t.Fatalf("emailer sent %d messages, want 1", len(msgs)-before)
	}
	msg := msgs[len(msgs)-1]
	if !strings.Contains(msg.Text, testOrigin+wantPath+"#token=") {
		e.t.Fatalf("email link doesn't point to %s:\n%s", wantPath, msg.Text)
	}
	m := tokenInLink.FindStringSubmatch(msg.Text)
	if m == nil {
		e.t.Fatalf("no token in email:\n%s", msg.Text)
	}
	return m[1]
}

func TestEmailVerification(t *testing.T) {
	e := newAuthTestEnv(t)
	browser := e.newClient()
	pk := e.signUp(browser, uniqueHandle(t))

	if n := e.queuedEmails(pk.accountID, auth.PurposeVerifyEmail); n != 1 {
		t.Fatalf("verification emails queued at sign-up = %d, want 1", n)
	}
	token := e.deliverEmail(pk.accountID, auth.PurposeVerifyEmail, "/verify-email")
	if msg := e.mail.Messages()[0]; msg.To != pk.email {
		t.Errorf("email sent to %q, want %q", msg.To, pk.email)
	}

	// Verification works without being signed in: the link may open in another browser.
	if status, body := e.do(e.newClient(), "POST", "/api/v1/auth/email/verify", map[string]string{"token": token}, nil); status != http.StatusNoContent {
		t.Fatalf("verify: status %d, body %v", status, body)
	}
	if _, me := e.do(browser, "GET", "/api/v1/me", nil, nil); nested(me, "account", "email_verified") != true {
		t.Fatalf("after verifying, me = %v", me)
	}

	status, body := e.do(e.newClient(), "POST", "/api/v1/auth/email/verify", map[string]string{"token": token}, nil)
	if status != http.StatusBadRequest || errorCode(body) != "link_invalid" {
		t.Fatalf("reused verification token: status %d, body %v", status, body)
	}

	// Once verified, asking again doesn't queue another email.
	if status, _ := e.do(browser, "POST", "/api/v1/me/email/verification", nil, nil); status != http.StatusAccepted {
		t.Fatalf("resend when verified: status %d", status)
	}
	if n := e.queuedEmails(pk.accountID, auth.PurposeVerifyEmail); n != 1 {
		t.Errorf("verification emails after resend when verified = %d, want 1", n)
	}
}

func TestResendVerification(t *testing.T) {
	e := newAuthTestEnv(t)
	if status, _ := e.do(e.newClient(), "POST", "/api/v1/me/email/verification", nil, nil); status != http.StatusUnauthorized {
		t.Fatalf("resend while signed out: status %d, want 401", status)
	}

	browser := e.newClient()
	pk := e.signUp(browser, uniqueHandle(t))
	if status, _ := e.do(browser, "POST", "/api/v1/me/email/verification", nil, nil); status != http.StatusAccepted {
		t.Fatalf("resend: status %d", status)
	}
	if n := e.queuedEmails(pk.accountID, auth.PurposeVerifyEmail); n != 2 {
		t.Errorf("verification emails queued = %d, want 2", n)
	}
}

func TestSignInLink(t *testing.T) {
	e := newAuthTestEnv(t)
	pk := e.signUp(e.newClient(), uniqueHandle(t))

	status, body := e.do(e.newClient(), "POST", "/api/v1/auth/login/email/request", map[string]string{"email": strings.ToUpper(pk.email)}, nil)
	if status != http.StatusAccepted {
		t.Fatalf("request link: status %d, body %v", status, body)
	}
	token := e.deliverEmail(pk.accountID, auth.PurposeSignIn, "/signin/email")

	// The link can be opened in any browser.
	other := e.newClient()
	status, me := e.do(other, "POST", "/api/v1/auth/login/email/finish", map[string]string{"token": token}, nil)
	if status != http.StatusOK || nested(me, "account", "id") != pk.accountID {
		t.Fatalf("finish link sign-in: status %d, body %v", status, me)
	}
	// Using the link proves control of the address.
	if nested(me, "account", "email_verified") != true {
		t.Errorf("email not verified by sign-in link: %v", me)
	}
	if status, _ := e.do(other, "GET", "/api/v1/me", nil, nil); status != http.StatusOK {
		t.Fatalf("me after link sign-in: status %d", status)
	}

	status, body = e.do(e.newClient(), "POST", "/api/v1/auth/login/email/finish", map[string]string{"token": token}, nil)
	if status != http.StatusBadRequest || errorCode(body) != "link_invalid" {
		t.Fatalf("reused sign-in link: status %d, body %v", status, body)
	}
	status, body = e.do(e.newClient(), "POST", "/api/v1/auth/login/email/finish", map[string]string{"token": "not-a-token"}, nil)
	if status != http.StatusBadRequest || errorCode(body) != "link_invalid" {
		t.Fatalf("malformed token: status %d, body %v", status, body)
	}
}

func TestSignInLinkMobileGetsToken(t *testing.T) {
	e := newAuthTestEnv(t)
	pk := e.signUp(e.newClient(), uniqueHandle(t))
	e.do(e.newClient(), "POST", "/api/v1/auth/login/email/request", map[string]string{"email": pk.email}, nil)
	token := e.deliverEmail(pk.accountID, auth.PurposeSignIn, "/signin/email")

	status, body := e.do(&http.Client{}, "POST", "/api/v1/auth/login/email/finish", map[string]string{"token": token, "client": "mobile"}, nil)
	if status != http.StatusOK || nested(body, "session", "token") == nil {
		t.Fatalf("mobile link sign-in: status %d, body %v", status, body)
	}
}

func TestSignInLinkDoesNotRevealAccounts(t *testing.T) {
	e := newAuthTestEnv(t)
	status, body := e.do(e.newClient(), "POST", "/api/v1/auth/login/email/request", map[string]string{"email": uniqueHandle(t) + "@example.org"}, nil)
	if status != http.StatusAccepted {
		t.Fatalf("unknown address: status %d, body %v, want the same 202 as a known one", status, body)
	}
	status, body = e.do(e.newClient(), "POST", "/api/v1/auth/login/email/request", map[string]string{"email": "not an email"}, nil)
	if status != http.StatusUnprocessableEntity || nested(body, "error", "field") != "email" {
		t.Fatalf("invalid address: status %d, body %v", status, body)
	}
}

func TestSignInLinkRecipientLimit(t *testing.T) {
	e := newAuthTestEnv(t, func(c *config.Config) {
		c.RateLimits.EmailRecipient = ratelimit.Policy{Burst: 2, Every: time.Hour}
	})
	pk := e.signUp(e.newClient(), uniqueHandle(t))

	for i := range 4 {
		status, _ := e.do(e.newClient(), "POST", "/api/v1/auth/login/email/request", map[string]string{"email": pk.email}, nil)
		if status != http.StatusAccepted {
			t.Fatalf("request %d: status %d, want 202 even when limited", i+1, status)
		}
	}
	if n := e.queuedEmails(pk.accountID, auth.PurposeSignIn); n != 2 {
		t.Errorf("sign-in emails queued = %d, want 2 (recipient limit)", n)
	}
}

func mustUUID(t *testing.T, s string) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
