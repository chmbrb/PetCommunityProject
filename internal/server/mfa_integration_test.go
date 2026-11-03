// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // G505: TOTP uses HMAC-SHA-1
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/chmbrb/PetCommunityProject/internal/auth"
)

// authenticator is a test authenticator app: an independent RFC 6238 implementation.
type authenticator struct {
	secret []byte
}

// code returns the code for the time step offset steps from now.
func (a authenticator) code(offset int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(time.Now().Unix()/30+offset))
	mac := hmac.New(sha1.New, a.secret)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	o := sum[19] & 0x0f
	return fmt.Sprintf("%06d", (binary.BigEndian.Uint32(sum[o:o+4])&0x7fffffff)%1_000_000)
}

// setUpTOTP turns on two-step sign-in for the signed-in browser and returns the
// authenticator and recovery codes.
func (e *authTestEnv) setUpTOTP(browser *http.Client) (authenticator, []string) {
	e.t.Helper()
	status, setup := e.do(browser, "POST", "/api/v1/me/totp/setup", nil, nil)
	if status != http.StatusOK {
		e.t.Fatalf("totp setup: status %d, body %v", status, setup)
	}
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(setup["secret"].(string))
	if err != nil {
		e.t.Fatal(err)
	}
	a := authenticator{secret: secret}
	status, body := e.do(browser, "POST", "/api/v1/me/totp/confirm", map[string]string{"code": a.code(0)}, nil)
	if status != http.StatusOK {
		e.t.Fatalf("totp confirm: status %d, body %v", status, body)
	}
	var codes []string
	for _, c := range body["recovery_codes"].([]any) {
		codes = append(codes, c.(string))
	}
	return a, codes
}

// emailSignIn signs in by email and returns the response of the finishing request.
func (e *authTestEnv) emailSignIn(c *http.Client, accountID, email string) (int, map[string]any) {
	e.t.Helper()
	e.do(e.newClient(), "POST", "/api/v1/auth/login/email/request", map[string]string{"email": email}, nil)
	token := e.deliverEmail(accountID, auth.PurposeSignIn, "/signin/email")
	return e.do(c, "POST", "/api/v1/auth/login/email/finish", map[string]string{"token": token}, nil)
}

// mfaToken returns the token from a 401 mfa_required response.
func (e *authTestEnv) mfaToken(status int, body map[string]any) string {
	e.t.Helper()
	token, _ := nested(body, "error", "mfa_token").(string)
	if status != http.StatusUnauthorized || errorCode(body) != "mfa_required" || token == "" {
		e.t.Fatalf("want 401 mfa_required with a token; got status %d, body %v", status, body)
	}
	return token
}

func TestTOTPSetup(t *testing.T) {
	e := newAuthTestEnv(t)
	browser := e.newClient()
	pk := e.signUp(browser, uniqueHandle(t))

	status, setup := e.do(browser, "POST", "/api/v1/me/totp/setup", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("setup: status %d, body %v", status, setup)
	}
	uri, err := url.Parse(setup["uri"].(string))
	if err != nil || uri.Scheme != "otpauth" || uri.Host != "totp" {
		t.Fatalf("uri = %v, %v", setup["uri"], err)
	}
	// The authenticator shows the instance and handle, never the email address.
	if label, _ := url.PathUnescape(uri.Path); label != "/Test Instance:@"+strings.TrimSuffix(pk.email, "@example.org") || strings.Contains(uri.String(), "example.org") {
		t.Errorf("uri label = %q (uri %s)", label, uri)
	}
	if q := uri.Query(); q.Get("secret") != setup["secret"] || q.Get("issuer") != "Test Instance" || q.Get("digits") != "6" || q.Get("period") != "30" {
		t.Errorf("uri query = %v", q)
	}
	// Nothing is on until a code is confirmed.
	if _, me := e.do(browser, "GET", "/api/v1/me", nil, nil); nested(me, "mfa", "totp_enabled_at") != nil {
		t.Errorf("two-step sign-in on before confirming: %v", me["mfa"])
	}
	if status, body := e.do(browser, "POST", "/api/v1/me/totp/confirm", map[string]string{"code": "000000"}, nil); status != http.StatusBadRequest || errorCode(body) != "code_invalid" {
		t.Errorf("confirm with a wrong code: status %d, body %v", status, body)
	}

	// Starting again replaces the unfinished setup.
	_, codes := e.setUpTOTP(browser)
	if len(codes) != 10 {
		t.Errorf("recovery codes = %d, want 10", len(codes))
	}
	_, me := e.do(browser, "GET", "/api/v1/me", nil, nil)
	if nested(me, "mfa", "totp_enabled_at") == nil || nested(me, "mfa", "recovery_codes_left") != float64(10) {
		t.Errorf("me after setup: %v", me["mfa"])
	}
	if n := e.queuedEmails(pk.accountID, auth.PurposeMFAChanged); n != 1 {
		t.Errorf("notices queued = %d, want 1", n)
	}
	if status, body := e.do(browser, "POST", "/api/v1/me/totp/setup", nil, nil); status != http.StatusConflict || errorCode(body) != "totp_exists" {
		t.Errorf("second setup: status %d, body %v", status, body)
	}
}

func TestEmailSignInNeedsSecondStep(t *testing.T) {
	e := newAuthTestEnv(t)
	browser := e.newClient()
	pk := e.signUp(browser, uniqueHandle(t))
	a, _ := e.setUpTOTP(browser)

	other := e.newClient()
	token := e.mfaToken(e.emailSignIn(other, pk.accountID, pk.email))
	if status, _ := e.do(other, "GET", "/api/v1/me", nil, nil); status != http.StatusUnauthorized {
		t.Fatalf("signed in before the second step: status %d", status)
	}
	// The code used to confirm setup can't be replayed.
	if status, body := e.do(other, "POST", "/api/v1/auth/login/mfa", map[string]string{"mfa_token": token, "code": a.code(0)}, nil); status != http.StatusBadRequest || errorCode(body) != "code_invalid" {
		t.Errorf("replayed code: status %d, body %v", status, body)
	}
	status, me := e.do(other, "POST", "/api/v1/auth/login/mfa", map[string]string{"mfa_token": token, "code": a.code(1)}, nil)
	if status != http.StatusOK || nested(me, "account", "id") != pk.accountID {
		t.Fatalf("second step: status %d, body %v", status, me)
	}
	if status, _ := e.do(other, "GET", "/api/v1/me", nil, nil); status != http.StatusOK {
		t.Errorf("me after the second step: status %d", status)
	}
	// The pending sign-in is used up.
	if status, body := e.do(e.newClient(), "POST", "/api/v1/auth/login/mfa", map[string]string{"mfa_token": token, "code": a.code(1)}, nil); errorCode(body) != "mfa_expired" {
		t.Errorf("reused mfa token: status %d, body %v", status, body)
	}
	// Passkeys need no second step.
	if status, _ := e.signIn(e.newClient(), pk, ""); status != http.StatusOK {
		t.Errorf("passkey sign-in with two-step on: status %d", status)
	}
}

func TestPasswordSignInSecondStepAndRecoveryCodes(t *testing.T) {
	e := newAuthTestEnv(t)
	browser := e.newClient()
	pk := e.signUp(browser, uniqueHandle(t))
	if status, _ := e.do(browser, "PUT", "/api/v1/me/password", map[string]string{"password": testPassword}, nil); status != http.StatusNoContent {
		t.Fatalf("set password: status %d", status)
	}
	_, codes := e.setUpTOTP(browser)

	// With two-step sign-in, a new device gets the authenticator step, not an email.
	app := &http.Client{}
	status, body := e.do(app, "POST", "/api/v1/auth/login/password", map[string]string{"email": pk.email, "password": testPassword, "client": "mobile"}, nil)
	token := e.mfaToken(status, body)
	if n := e.queuedEmails(pk.accountID, auth.PurposeNewDevice); n != 0 {
		t.Errorf("new-device emails queued = %d, want 0", n)
	}
	// A recovery code works instead, typed in capitals; the session is for the app.
	status, body = e.do(app, "POST", "/api/v1/auth/login/mfa", map[string]string{"mfa_token": token, "recovery_code": strings.ToUpper(codes[0])}, nil)
	if status != http.StatusOK || nested(body, "session", "token") == nil {
		t.Fatalf("recovery code: status %d, body %v", status, body)
	}
	if n := e.queuedEmails(pk.accountID, auth.PurposeMFAChanged); n != 2 {
		t.Errorf("notices queued = %d, want 2 (turned on, recovery code used)", n)
	}

	// Each recovery code works once.
	status, body = e.do(app, "POST", "/api/v1/auth/login/password", map[string]string{"email": pk.email, "password": testPassword}, nil)
	token = e.mfaToken(status, body)
	if status, body := e.do(app, "POST", "/api/v1/auth/login/mfa", map[string]string{"mfa_token": token, "recovery_code": codes[0]}, nil); errorCode(body) != "code_invalid" {
		t.Errorf("reused recovery code: status %d, body %v", status, body)
	}
	if _, me := e.do(browser, "GET", "/api/v1/me", nil, nil); nested(me, "mfa", "recovery_codes_left") != float64(9) {
		t.Errorf("recovery codes left = %v, want 9", nested(me, "mfa", "recovery_codes_left"))
	}
}

func TestMFAAttemptLimit(t *testing.T) {
	e := newAuthTestEnv(t)
	browser := e.newClient()
	pk := e.signUp(browser, uniqueHandle(t))
	a, _ := e.setUpTOTP(browser)
	token := e.mfaToken(e.emailSignIn(e.newClient(), pk.accountID, pk.email))
	for i := range 5 {
		if status, body := e.do(e.newClient(), "POST", "/api/v1/auth/login/mfa", map[string]string{"mfa_token": token, "code": "000000"}, nil); errorCode(body) != "code_invalid" {
			t.Fatalf("wrong code %d: status %d, body %v", i+1, status, body)
		}
	}
	if status, body := e.do(e.newClient(), "POST", "/api/v1/auth/login/mfa", map[string]string{"mfa_token": token, "code": a.code(1)}, nil); errorCode(body) != "mfa_expired" {
		t.Errorf("right code after 5 wrong ones: status %d, body %v; want mfa_expired", status, body)
	}
}

func TestRegenerateCodesAndTurnOff(t *testing.T) {
	e := newAuthTestEnv(t)
	browser := e.newClient()
	pk := e.signUp(browser, uniqueHandle(t))
	if status, body := e.do(browser, "POST", "/api/v1/me/recovery-codes", nil, nil); status != http.StatusConflict || errorCode(body) != "totp_not_set_up" {
		t.Errorf("regenerate without two-step: status %d, body %v", status, body)
	}
	_, old := e.setUpTOTP(browser)
	status, body := e.do(browser, "POST", "/api/v1/me/recovery-codes", nil, nil)
	if status != http.StatusOK || len(body["recovery_codes"].([]any)) != 10 {
		t.Fatalf("regenerate: status %d, body %v", status, body)
	}
	token := e.mfaToken(e.emailSignIn(e.newClient(), pk.accountID, pk.email))
	if status, body := e.do(e.newClient(), "POST", "/api/v1/auth/login/mfa", map[string]string{"mfa_token": token, "recovery_code": old[0]}, nil); errorCode(body) != "code_invalid" {
		t.Errorf("old recovery code after regenerating: status %d, body %v", status, body)
	}

	if status, _ := e.do(browser, "DELETE", "/api/v1/me/totp", nil, nil); status != http.StatusNoContent {
		t.Fatalf("turn off: status %d", status)
	}
	// Email sign-in no longer asks for a second step, and pending ones are void.
	if status, body := e.emailSignIn(e.newClient(), pk.accountID, pk.email); status != http.StatusOK {
		t.Errorf("email sign-in after turning off: status %d, body %v", status, body)
	}
	if status, body := e.do(e.newClient(), "POST", "/api/v1/auth/login/mfa", map[string]string{"mfa_token": token, "code": "123456"}, nil); errorCode(body) != "mfa_expired" {
		t.Errorf("pending sign-in after turning off: status %d, body %v", status, body)
	}
	if n := e.queuedEmails(pk.accountID, auth.PurposeMFAChanged); n != 3 {
		t.Errorf("notices queued = %d, want 3 (on, new codes, off)", n)
	}
}
