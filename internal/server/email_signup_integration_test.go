// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"testing"

	"github.com/chmbrb/PetCommunityProject/internal/auth"
	"github.com/chmbrb/PetCommunityProject/internal/store"
)

var codeInEmail = regexp.MustCompile(`enter this code where you [a-z ]+: (\d{6})`)

// lastEmail returns the token and code from the most recently delivered email.
func (e *authTestEnv) lastEmail() (token, code string) {
	e.t.Helper()
	msgs := e.mail.Messages()
	if len(msgs) == 0 {
		e.t.Fatal("no email delivered")
	}
	text := msgs[len(msgs)-1].Text
	t, c := tokenInLink.FindStringSubmatch(text), codeInEmail.FindStringSubmatch(text)
	if t == nil || c == nil {
		e.t.Fatalf("email lacks a link or code:\n%s", text)
	}
	return t[1], c[1]
}

// requestSignup asks for a sign-up email and delivers it, as the worker would, and
// returns the token and code from it.
func (e *authTestEnv) requestSignup(handle string) (email, token, code string) {
	e.t.Helper()
	email = handle + "@example.org"
	status, body := e.do(e.newClient(), "POST", "/api/v1/auth/signup/email/request", map[string]string{
		"email": email, "handle": handle, "display_name": "Test " + handle,
	}, nil)
	if status != http.StatusAccepted {
		e.t.Fatalf("sign-up request: status %d, body %v", status, body)
	}
	var raw []byte
	err := e.pool.QueryRow(context.Background(),
		`SELECT args FROM river_job WHERE kind = 'email' AND args->>'purpose' = 'sign_up' AND args->'signup'->>'email' = $1 ORDER BY id DESC LIMIT 1`,
		email).Scan(&raw)
	if err != nil {
		e.t.Fatalf("find queued sign-up email: %v", err)
	}
	var job auth.EmailJob
	if err := json.Unmarshal(raw, &job); err != nil {
		e.t.Fatal(err)
	}
	if err := e.emailer.Send(context.Background(), job); err != nil {
		e.t.Fatalf("send sign-up email: %v", err)
	}
	if msg := e.mail.Messages()[len(e.mail.Messages())-1]; msg.To != email {
		e.t.Fatalf("sign-up email sent to %q, want %q", msg.To, email)
	}
	token, code = e.lastEmail()
	return email, token, code
}

func (e *authTestEnv) emailExists(email string) bool {
	e.t.Helper()
	ok, err := e.store.EmailExists(context.Background(), email)
	if err != nil {
		e.t.Fatal(err)
	}
	return ok
}

func TestEmailSignUpWithLink(t *testing.T) {
	e := newAuthTestEnv(t)
	handle := uniqueHandle(t)
	email, token, _ := e.requestSignup(handle)
	// Nothing exists until the address is confirmed.
	if e.emailExists(email) {
		t.Fatal("account created before the sign-up email was used")
	}

	// The link can be opened in any browser.
	browser := e.newClient()
	status, me := e.do(browser, "POST", "/api/v1/auth/signup/email/finish", map[string]string{"token": token}, nil)
	if status != http.StatusCreated {
		t.Fatalf("finish sign-up: status %d, body %v", status, me)
	}
	if nested(me, "actor", "handle") != handle || nested(me, "account", "email_verified") != true {
		t.Errorf("new account = %v, want handle %s with a verified email", me, handle)
	}
	accountID, _ := nested(me, "account", "id").(string)
	// A passkey isn't needed, and the address is already verified, so no
	// verification email is sent.
	if _, body := e.do(browser, "GET", "/api/v1/me/passkeys", nil, nil); len(body["passkeys"].([]any)) != 0 {
		t.Errorf("passkeys after email sign-up = %v, want none", body)
	}
	if n := e.queuedEmails(accountID, auth.PurposeVerifyEmail); n != 0 {
		t.Errorf("verification emails queued = %d, want 0", n)
	}
	var keyJobs int
	if err := e.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM river_job WHERE kind = 'actor_keys' AND args->>'actor_id' = $1`,
		nested(me, "actor", "id")).Scan(&keyJobs); err != nil {
		t.Fatal(err)
	}
	if keyJobs != 1 {
		t.Errorf("actor key jobs queued = %d, want 1", keyJobs)
	}

	status, body := e.do(e.newClient(), "POST", "/api/v1/auth/signup/email/finish", map[string]string{"token": token}, nil)
	if status != http.StatusBadRequest || errorCode(body) != "link_invalid" {
		t.Fatalf("reused sign-up link: status %d, body %v", status, body)
	}

	// The new account signs in by email.
	e.do(e.newClient(), "POST", "/api/v1/auth/login/email/request", map[string]string{"email": email}, nil)
	_ = e.deliverEmail(accountID, auth.PurposeSignIn, "/signin/email")
	_, code := e.lastEmail()
	status, body = e.do(e.newClient(), "POST", "/api/v1/auth/login/email/finish", map[string]string{"email": email, "code": code}, nil)
	if status != http.StatusOK || nested(body, "account", "id") != accountID {
		t.Fatalf("sign in with code: status %d, body %v", status, body)
	}
}

func TestEmailSignUpWithCodeOnMobile(t *testing.T) {
	e := newAuthTestEnv(t)
	email, _, code := e.requestSignup(uniqueHandle(t))

	// Codes are entered as shown or copied, with spaces, and the address in any case.
	spaced := code[:3] + " " + code[3:]
	status, body := e.do(&http.Client{}, "POST", "/api/v1/auth/signup/email/finish", map[string]string{
		"email": " " + email + " ", "code": spaced, "client": "mobile",
	}, nil)
	if status != http.StatusCreated || nested(body, "session", "token") == nil {
		t.Fatalf("mobile sign-up with code: status %d, body %v", status, body)
	}
}

func TestEmailSignUpChecksDetails(t *testing.T) {
	e := newAuthTestEnv(t)
	existing := e.signUp(e.newClient(), uniqueHandle(t))

	status, body := e.do(e.newClient(), "POST", "/api/v1/auth/signup/email/request", map[string]string{
		"email": existing.email, "handle": uniqueHandle(t), "display_name": "Second",
	}, nil)
	if status != http.StatusConflict || errorCode(body) != "email_taken" {
		t.Errorf("taken email: status %d, body %v", status, body)
	}
	status, body = e.do(e.newClient(), "POST", "/api/v1/auth/signup/email/request", map[string]string{
		"email": "new@example.org", "handle": "admin", "display_name": "Admin",
	}, nil)
	if status != http.StatusUnprocessableEntity || nested(body, "error", "field") != "handle" {
		t.Errorf("reserved handle: status %d, body %v", status, body)
	}
}

func TestEmailSignUpHandleTakenMeanwhile(t *testing.T) {
	e := newAuthTestEnv(t)
	handle := uniqueHandle(t)
	_, token, _ := e.requestSignup(handle)

	// Pending sign-ups don't reserve the handle; whoever finishes first gets it.
	c := e.newClient()
	status, begin := e.do(c, "POST", "/api/v1/auth/signup/begin", map[string]string{
		"email": "other-" + handle + "@example.org", "handle": handle, "display_name": "Other",
	}, nil)
	if status != http.StatusOK {
		t.Fatalf("second sign-up with the pending handle: status %d, body %v", status, begin)
	}
	e.finishPasskeySignup(c, begin)

	status, body := e.do(e.newClient(), "POST", "/api/v1/auth/signup/email/finish", map[string]string{"token": token}, nil)
	if status != http.StatusConflict || errorCode(body) != "handle_taken" {
		t.Fatalf("finish after the handle was taken: status %d, body %v", status, body)
	}
}

func TestEmailSignUpClosedRegistration(t *testing.T) {
	e := newAuthTestEnv(t)
	handle := uniqueHandle(t)
	_, token, _ := e.requestSignup(handle)

	ctx := context.Background()
	if err := e.store.SetFeatureFlag(ctx, store.SetFeatureFlagParams{Flag: "open_registration", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = e.store.SetFeatureFlag(ctx, store.SetFeatureFlagParams{Flag: "open_registration", Enabled: true})
	})

	status, body := e.do(e.newClient(), "POST", "/api/v1/auth/signup/email/request", map[string]string{
		"email": "closed-" + handle + "@example.org", "handle": "c" + handle, "display_name": "Closed",
	}, nil)
	if status != http.StatusForbidden || errorCode(body) != "registration_closed" {
		t.Errorf("request while closed: status %d, body %v", status, body)
	}
	// An email sent while registration was open doesn't work once it's closed.
	status, body = e.do(e.newClient(), "POST", "/api/v1/auth/signup/email/finish", map[string]string{"token": token}, nil)
	if status != http.StatusForbidden || errorCode(body) != "registration_closed" {
		t.Errorf("finish while closed: status %d, body %v", status, body)
	}
}

func TestEmailCodeAttemptLimit(t *testing.T) {
	e := newAuthTestEnv(t)
	pk := e.signUp(e.newClient(), uniqueHandle(t))
	e.do(e.newClient(), "POST", "/api/v1/auth/login/email/request", map[string]string{"email": pk.email}, nil)
	e.deliverEmail(pk.accountID, auth.PurposeSignIn, "/signin/email")
	_, code := e.lastEmail()
	wrong := otherCode(t, code)

	try := func(code string) (int, map[string]any) {
		return e.do(e.newClient(), "POST", "/api/v1/auth/login/email/finish", map[string]string{"email": pk.email, "code": code}, nil)
	}
	// Malformed codes are refused without counting as attempts.
	if status, body := try("12345"); status != http.StatusBadRequest || errorCode(body) != "code_invalid" {
		t.Fatalf("malformed code: status %d, body %v", status, body)
	}
	for i := range 4 {
		if status, body := try(wrong); status != http.StatusBadRequest || errorCode(body) != "code_invalid" {
			t.Fatalf("wrong code %d: status %d, body %v", i+1, status, body)
		}
	}
	// After four wrong codes, the right one still works...
	if status, body := try(code); status != http.StatusOK {
		t.Fatalf("right code after 4 wrong ones: status %d, body %v", status, body)
	}

	// ...but a fifth wrong code uses the email up, link included.
	e.do(e.newClient(), "POST", "/api/v1/auth/login/email/request", map[string]string{"email": pk.email}, nil)
	token := e.deliverEmail(pk.accountID, auth.PurposeSignIn, "/signin/email")
	_, code = e.lastEmail()
	wrong = otherCode(t, code)
	for range 5 {
		try(wrong)
	}
	if status, _ := try(code); status != http.StatusBadRequest {
		t.Errorf("right code after 5 wrong ones: status %d, want 400", status)
	}
	if status, body := e.do(e.newClient(), "POST", "/api/v1/auth/login/email/finish", map[string]string{"token": token}, nil); status != http.StatusBadRequest || errorCode(body) != "link_invalid" {
		t.Errorf("link after 5 wrong codes: status %d, body %v", status, body)
	}
}

func TestOnlyNewestEmailCodeWorks(t *testing.T) {
	e := newAuthTestEnv(t)
	pk := e.signUp(e.newClient(), uniqueHandle(t))
	e.do(e.newClient(), "POST", "/api/v1/auth/login/email/request", map[string]string{"email": pk.email}, nil)
	e.deliverEmail(pk.accountID, auth.PurposeSignIn, "/signin/email")
	firstToken, firstCode := e.lastEmail()
	e.do(e.newClient(), "POST", "/api/v1/auth/login/email/request", map[string]string{"email": pk.email}, nil)
	e.deliverEmail(pk.accountID, auth.PurposeSignIn, "/signin/email")
	_, secondCode := e.lastEmail()

	finish := func(body map[string]string) int {
		status, _ := e.do(e.newClient(), "POST", "/api/v1/auth/login/email/finish", body, nil)
		return status
	}
	if firstCode != secondCode {
		if status := finish(map[string]string{"email": pk.email, "code": firstCode}); status != http.StatusBadRequest {
			t.Errorf("older email's code: status %d, want 400", status)
		}
	}
	// A code is tied to the address it was sent to.
	other := e.signUp(e.newClient(), uniqueHandle(t))
	if status := finish(map[string]string{"email": other.email, "code": secondCode}); status != http.StatusBadRequest {
		t.Errorf("code with another address: status %d, want 400", status)
	}
	if status := finish(map[string]string{"email": pk.email, "code": secondCode}); status != http.StatusOK {
		t.Errorf("newest code: status %d, want 200", status)
	}
	// Older emails' links still work until they expire.
	if status := finish(map[string]string{"token": firstToken}); status != http.StatusOK {
		t.Errorf("older email's link: status %d, want 200", status)
	}
}

func TestRemoveLastPasskeyOnceEmailVerified(t *testing.T) {
	e := newAuthTestEnv(t)
	browser := e.newClient()
	pk := e.signUp(browser, uniqueHandle(t))
	_, list := e.do(browser, "GET", "/api/v1/me/passkeys", nil, nil)
	id, _ := list["passkeys"].([]any)[0].(map[string]any)["id"].(string)

	// Without a verified address, email sign-in might not reach the person.
	status, body := e.do(browser, "DELETE", "/api/v1/me/passkeys/"+id, nil, nil)
	if status != http.StatusConflict || errorCode(body) != "last_passkey" {
		t.Fatalf("remove only passkey, email unverified: status %d, body %v", status, body)
	}

	token := e.deliverEmail(pk.accountID, auth.PurposeVerifyEmail, "/verify-email")
	if status, _ := e.do(e.newClient(), "POST", "/api/v1/auth/email/verify", map[string]string{"token": token}, nil); status != http.StatusNoContent {
		t.Fatalf("verify email: status %d", status)
	}
	if status, body := e.do(browser, "DELETE", "/api/v1/me/passkeys/"+id, nil, nil); status != http.StatusNoContent {
		t.Fatalf("remove only passkey, email verified: status %d, body %v", status, body)
	}
	// Email sign-in still works.
	e.do(e.newClient(), "POST", "/api/v1/auth/login/email/request", map[string]string{"email": pk.email}, nil)
	token = e.deliverEmail(pk.accountID, auth.PurposeSignIn, "/signin/email")
	if status, _ := e.do(e.newClient(), "POST", "/api/v1/auth/login/email/finish", map[string]string{"token": token}, nil); status != http.StatusOK {
		t.Errorf("email sign-in without passkeys: status %d", status)
	}
}

// otherCode returns a well-formed code that differs from code.
func otherCode(t *testing.T, code string) string {
	t.Helper()
	n, err := strconv.Atoi(code)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%06d", (n+1)%1_000_000)
}
