// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"context"
	"net/http"
	"regexp"
	"testing"

	"github.com/google/uuid"

	"github.com/chmbrb/PetCommunityProject/internal/auth"
)

const testPassword = "a long and unusual passphrase"

var newDeviceCode = regexp.MustCompile(`enter this code:\s+(\d{6})`)

// breachList is a fake breached-password check.
type breachList map[string]bool

func (b breachList) Breached(_ context.Context, password string) (bool, error) {
	return b[password], nil
}

// signUpWithPassword creates an account by email, with a password, in browser.
func (e *authTestEnv) signUpWithPassword(browser *http.Client) (accountID, email string) {
	e.t.Helper()
	email, token, _ := e.requestSignup(uniqueHandle(e.t))
	status, me := e.do(browser, "POST", "/api/v1/auth/signup/email/finish", map[string]string{"token": token, "password": testPassword}, nil)
	if status != http.StatusCreated || nested(me, "account", "has_password") != true {
		e.t.Fatalf("sign up with password: status %d, body %v", status, me)
	}
	accountID, _ = nested(me, "account", "id").(string)
	return accountID, email
}

func (e *authTestEnv) passwordSignIn(c *http.Client, email, password, code string) (int, map[string]any) {
	e.t.Helper()
	return e.do(c, "POST", "/api/v1/auth/login/password", map[string]string{"email": email, "password": password, "code": code}, nil)
}

// deliverNewDeviceCode sends the queued new-device email and returns its code.
func (e *authTestEnv) deliverNewDeviceCode(accountID string) string {
	e.t.Helper()
	if e.queuedEmails(accountID, auth.PurposeNewDevice) == 0 {
		e.t.Fatal("no new-device email queued")
	}
	if err := e.emailer.Send(context.Background(), auth.EmailJob{AccountID: uuid.MustParse(accountID), Purpose: auth.PurposeNewDevice}); err != nil {
		e.t.Fatal(err)
	}
	msgs := e.mail.Messages()
	m := newDeviceCode.FindStringSubmatch(msgs[len(msgs)-1].Text)
	if m == nil {
		e.t.Fatalf("no code in new-device email:\n%s", msgs[len(msgs)-1].Text)
	}
	return m[1]
}

func TestPasswordSignInKnownAndNewDevices(t *testing.T) {
	e := newAuthTestEnv(t)
	browser := e.newClient()
	accountID, email := e.signUpWithPassword(browser)
	e.do(browser, "POST", "/api/v1/auth/logout", nil, nil)

	// The browser that signed up is known, so the password is enough there.
	if status, body := e.passwordSignIn(browser, email, testPassword, ""); status != http.StatusOK || nested(body, "account", "id") != accountID {
		t.Fatalf("password sign-in on a known device: status %d, body %v", status, body)
	}

	// Elsewhere, the right password asks for an emailed code; nothing is signed in yet.
	other := e.newClient()
	status, body := e.passwordSignIn(other, email, testPassword, "")
	if status != http.StatusUnauthorized || errorCode(body) != "email_code_required" {
		t.Fatalf("password sign-in on a new device: status %d, body %v", status, body)
	}
	if status, _ := e.do(other, "GET", "/api/v1/me", nil, nil); status != http.StatusUnauthorized {
		t.Fatalf("signed in before entering the code: status %d", status)
	}
	code := e.deliverNewDeviceCode(accountID)

	// The code doesn't work without the password.
	if status, _ := e.passwordSignIn(other, email, "the wrong password!", code); status != http.StatusUnauthorized {
		t.Errorf("code with a wrong password: status %d, want 401", status)
	}
	if status, body := e.passwordSignIn(other, email, testPassword, code); status != http.StatusOK {
		t.Fatalf("password and code: status %d, body %v", status, body)
	}
	// Now that device is known too.
	e.do(other, "POST", "/api/v1/auth/logout", nil, nil)
	if status, _ := e.passwordSignIn(other, email, testPassword, ""); status != http.StatusOK {
		t.Errorf("second sign-in on the now-known device: status %d", status)
	}
}

func TestPasswordSignInFailuresLookAlike(t *testing.T) {
	e := newAuthTestEnv(t)
	_, email := e.signUpWithPassword(e.newClient())
	passkeyOnly := e.signUp(e.newClient(), uniqueHandle(t))
	for name, attempt := range map[string][2]string{
		"wrong password":   {email, "not the password at all"},
		"unknown account":  {uniqueHandle(t) + "@example.org", testPassword},
		"no password set":  {passkeyOnly.email, testPassword},
		"missing password": {email, ""},
	} {
		status, body := e.passwordSignIn(e.newClient(), attempt[0], attempt[1], "")
		if status != http.StatusUnauthorized || errorCode(body) != "sign_in_failed" {
			t.Errorf("%s: status %d, body %v; want 401 sign_in_failed", name, status, body)
		}
	}
}

func TestPasswordLockout(t *testing.T) {
	e := newAuthTestEnv(t)
	browser := e.newClient()
	accountID, email := e.signUpWithPassword(browser)
	for i := range 9 {
		if status, body := e.passwordSignIn(browser, email, "wrong password number", ""); errorCode(body) != "sign_in_failed" {
			t.Fatalf("wrong password %d: status %d, body %v", i+1, status, body)
		}
	}
	if status, body := e.passwordSignIn(browser, email, "wrong password number", ""); status != http.StatusTooManyRequests || errorCode(body) != "password_locked" {
		t.Fatalf("10th wrong password: status %d, body %v", status, body)
	}
	if status, body := e.passwordSignIn(browser, email, testPassword, ""); errorCode(body) != "password_locked" {
		t.Fatalf("right password while locked: status %d, body %v", status, body)
	}
	// Email sign-in still works, so the owner isn't locked out.
	e.do(e.newClient(), "POST", "/api/v1/auth/login/email/request", map[string]string{"email": email}, nil)
	token := e.deliverEmail(accountID, auth.PurposeSignIn, "/signin/email")
	if status, _ := e.do(e.newClient(), "POST", "/api/v1/auth/login/email/finish", map[string]string{"token": token}, nil); status != http.StatusOK {
		t.Errorf("email sign-in while the password is locked: status %d", status)
	}
}

func TestSetChangeAndRemovePassword(t *testing.T) {
	e := newAuthTestEnv(t)
	e.auth.SetBreachChecker(breachList{"password1234": true})
	t.Cleanup(func() { e.auth.SetBreachChecker(nil) })
	browser := e.newClient()
	pk := e.signUp(browser, uniqueHandle(t))
	elsewhere := e.newClient()
	e.signIn(elsewhere, pk, "")

	for name, pw := range map[string]string{
		"too short": "short",
		"breached":  "password1234",
		"email":     pk.email,
	} {
		status, body := e.do(browser, "PUT", "/api/v1/me/password", map[string]string{"password": pw}, nil)
		if status != http.StatusUnprocessableEntity || nested(body, "error", "field") != "password" {
			t.Errorf("%s password: status %d, body %v", name, status, body)
		}
	}

	if status, body := e.do(browser, "PUT", "/api/v1/me/password", map[string]string{"password": testPassword}, nil); status != http.StatusNoContent {
		t.Fatalf("set password: status %d, body %v", status, body)
	}
	if _, me := e.do(browser, "GET", "/api/v1/me", nil, nil); nested(me, "account", "has_password") != true {
		t.Errorf("me after setting a password: %v", me)
	}
	// Other devices are signed out; this one stays signed in.
	if status, _ := e.do(elsewhere, "GET", "/api/v1/me", nil, nil); status != http.StatusUnauthorized {
		t.Errorf("other device after password set: status %d, want 401", status)
	}
	if n := e.queuedEmails(pk.accountID, auth.PurposePasswordChanged); n != 1 {
		t.Errorf("password notices queued = %d, want 1", n)
	}
	if status, _ := e.passwordSignIn(browser, pk.email, testPassword, ""); status != http.StatusOK {
		t.Errorf("sign in with the new password: status %d", status)
	}

	if status, _ := e.do(browser, "DELETE", "/api/v1/me/password", nil, nil); status != http.StatusNoContent {
		t.Fatalf("remove password: status %d", status)
	}
	if status, _ := e.passwordSignIn(browser, pk.email, testPassword, ""); status != http.StatusUnauthorized {
		t.Errorf("sign in with the removed password: status %d, want 401", status)
	}
	if n := e.queuedEmails(pk.accountID, auth.PurposePasswordChanged); n != 2 {
		t.Errorf("password notices queued = %d, want 2", n)
	}
	if status, _ := e.do(e.newClient(), "PUT", "/api/v1/me/password", map[string]string{"password": testPassword}, nil); status != http.StatusUnauthorized {
		t.Errorf("set password signed out: status %d, want 401", status)
	}
}

func TestSignUpPasswordRules(t *testing.T) {
	e := newAuthTestEnv(t)
	handle := uniqueHandle(t)
	_, token, _ := e.requestSignup(handle)
	// A refused password leaves the email usable.
	status, body := e.do(e.newClient(), "POST", "/api/v1/auth/signup/email/finish", map[string]string{"token": token, "password": handle}, nil)
	if status != http.StatusUnprocessableEntity || nested(body, "error", "field") != "password" {
		t.Fatalf("handle as password: status %d, body %v", status, body)
	}
	if status, body := e.do(e.newClient(), "POST", "/api/v1/auth/signup/email/finish", map[string]string{"token": token}, nil); status != http.StatusCreated {
		t.Fatalf("same email without a password: status %d, body %v", status, body)
	}
	// Passwords can't be set through email sign-in.
	status, body = e.do(e.newClient(), "POST", "/api/v1/auth/login/email/finish", map[string]string{"token": "x", "password": testPassword}, nil)
	if status != http.StatusBadRequest || errorCode(body) != "invalid_request" {
		t.Errorf("password on email sign-in: status %d, body %v", status, body)
	}
}

func TestMobileDeviceToken(t *testing.T) {
	e := newAuthTestEnv(t)
	accountID, email := e.signUpWithPassword(e.newClient())
	app := &http.Client{}
	status, body := e.do(app, "POST", "/api/v1/auth/login/password", map[string]string{"email": email, "password": testPassword, "client": "mobile"}, nil)
	if errorCode(body) != "email_code_required" {
		t.Fatalf("app's first password sign-in: status %d, body %v", status, body)
	}
	code := e.deliverNewDeviceCode(accountID)
	status, body = e.do(app, "POST", "/api/v1/auth/login/password", map[string]string{"email": email, "password": testPassword, "code": code, "client": "mobile"}, nil)
	device, _ := nested(body, "session", "device_token").(string)
	if status != http.StatusOK || device == "" {
		t.Fatalf("app sign-in with code: status %d, body %v; want a device token", status, body)
	}
	// Sending the token back makes the app a known device.
	status, body = e.do(app, "POST", "/api/v1/auth/login/password", map[string]string{"email": email, "password": testPassword, "client": "mobile"},
		map[string]string{"X-Device-Token": device})
	if status != http.StatusOK || nested(body, "session", "device_token") != nil {
		t.Errorf("app sign-in with its device token: status %d, body %v; want no new token", status, body)
	}
}
