// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/descope/virtualwebauthn"

	"github.com/chmbrb/PetCommunityProject/internal/auth"
	"github.com/chmbrb/PetCommunityProject/internal/config"
	"github.com/chmbrb/PetCommunityProject/internal/database"
	"github.com/chmbrb/PetCommunityProject/internal/store"
)

const testOrigin = "http://localhost:5173"

// authTestEnv runs the real server against the integration test database.
type authTestEnv struct {
	t     *testing.T
	srv   *httptest.Server
	store *database.Store
	rp    virtualwebauthn.RelyingParty
}

func newAuthTestEnv(t *testing.T) *authTestEnv {
	t.Helper()
	url := os.Getenv("PC_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("PC_TEST_DATABASE_URL not set; skipping database integration test")
	}
	ctx := context.Background()
	if _, err := database.Migrate(ctx, url); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	pool, err := database.Connect(ctx, url)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(pool.Close)

	cfg := config.Config{
		Env:             config.Development,
		InstanceName:    "Test Instance",
		SourceURL:       config.DefaultSourceURL,
		WebAuthnRPID:    "localhost",
		WebAuthnOrigins: []string{testOrigin},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	st := database.NewStore(pool)
	authSvc, err := auth.New(cfg, st, logger)
	if err != nil {
		t.Fatalf("auth.New: %v", err)
	}
	srv := httptest.NewServer(New(cfg, st, authSvc, logger))
	t.Cleanup(srv.Close)

	return &authTestEnv{
		t: t, srv: srv, store: st,
		rp: virtualwebauthn.RelyingParty{Name: "Test Instance", ID: "localhost", Origin: testOrigin},
	}
}

// newClient returns an HTTP client with its own cookie jar, like a separate browser.
func (e *authTestEnv) newClient() *http.Client {
	jar, err := cookiejar.New(nil)
	if err != nil {
		e.t.Fatal(err)
	}
	return &http.Client{Jar: jar}
}

func (e *authTestEnv) do(c *http.Client, method, path string, body any, headers map[string]string) (int, map[string]any) {
	e.t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			e.t.Fatal(err)
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, e.srv.URL+path, r)
	if err != nil {
		e.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &out); err != nil {
			e.t.Fatalf("%s %s: decode %q: %v", method, path, raw, err)
		}
	}
	return resp.StatusCode, out
}

// passkey is a virtual authenticator holding one credential.
type passkey struct {
	authenticator virtualwebauthn.Authenticator
	credential    virtualwebauthn.Credential
}

// signUp registers a new account with a new passkey and returns the passkey.
func (e *authTestEnv) signUp(c *http.Client, handle string) *passkey {
	e.t.Helper()
	status, begin := e.do(c, "POST", "/api/v1/auth/signup/begin", map[string]string{
		"email": handle + "@example.org", "handle": handle, "display_name": "Test " + handle,
	}, nil)
	if status != http.StatusOK {
		e.t.Fatalf("signup begin: status %d, body %v", status, begin)
	}
	opts, err := virtualwebauthn.ParseAttestationOptions(mustJSON(e.t, begin["options"]))
	if err != nil {
		e.t.Fatalf("ParseAttestationOptions: %v", err)
	}

	pk := &passkey{credential: virtualwebauthn.NewCredential(virtualwebauthn.KeyTypeEC2)}
	pk.authenticator = virtualwebauthn.NewAuthenticatorWithOptions(virtualwebauthn.AuthenticatorOptions{
		UserHandle: []byte(opts.UserID),
	})
	response := virtualwebauthn.CreateAttestationResponse(e.rp, pk.authenticator, pk.credential, *opts)
	pk.authenticator.AddCredential(pk.credential)

	status, me := e.do(c, "POST", "/api/v1/auth/signup/finish", map[string]any{
		"challenge_id": begin["challenge_id"], "credential": json.RawMessage(response),
	}, nil)
	if status != http.StatusCreated {
		e.t.Fatalf("signup finish: status %d, body %v", status, me)
	}

	// The same challenge can't be used twice.
	status, replay := e.do(c, "POST", "/api/v1/auth/signup/finish", map[string]any{
		"challenge_id": begin["challenge_id"], "credential": json.RawMessage(response),
	}, nil)
	if status != http.StatusBadRequest || errorCode(replay) != "challenge_expired" {
		e.t.Fatalf("replayed signup finish: status %d, body %v", status, replay)
	}
	return pk
}

// signIn signs in with a passkey and returns the response body.
func (e *authTestEnv) signIn(c *http.Client, pk *passkey, client string) (int, map[string]any) {
	e.t.Helper()
	status, begin := e.do(c, "POST", "/api/v1/auth/login/begin", nil, nil)
	if status != http.StatusOK {
		e.t.Fatalf("login begin: status %d, body %v", status, begin)
	}
	opts, err := virtualwebauthn.ParseAssertionOptions(mustJSON(e.t, begin["options"]))
	if err != nil {
		e.t.Fatalf("ParseAssertionOptions: %v", err)
	}
	response := virtualwebauthn.CreateAssertionResponse(e.rp, pk.authenticator, pk.credential, *opts)
	return e.do(c, "POST", "/api/v1/auth/login/finish", map[string]any{
		"challenge_id": begin["challenge_id"], "credential": json.RawMessage(response), "client": client,
	}, nil)
}

func TestPasskeySignUpAndSignIn(t *testing.T) {
	e := newAuthTestEnv(t)
	browser := e.newClient()
	handle := uniqueHandle(t)

	pk := e.signUp(browser, handle)

	status, me := e.do(browser, "GET", "/api/v1/me", nil, nil)
	if status != http.StatusOK || nested(me, "actor", "handle") != handle {
		t.Fatalf("me after signup: status %d, body %v", status, me)
	}
	if nested(me, "account", "email_verified") != false {
		t.Errorf("new account email_verified = %v, want false", nested(me, "account", "email_verified"))
	}

	if status, body := e.do(browser, "POST", "/api/v1/auth/logout", nil, nil); status != http.StatusNoContent {
		t.Fatalf("logout: status %d, body %v", status, body)
	}
	if status, _ := e.do(browser, "GET", "/api/v1/me", nil, nil); status != http.StatusUnauthorized {
		t.Fatalf("me after logout: status %d, want 401", status)
	}

	if status, body := e.signIn(browser, pk, ""); status != http.StatusOK || nested(body, "session") != nil {
		t.Fatalf("web sign-in: status %d, body %v (web sessions must not return the token)", status, body)
	}
	if status, me := e.do(browser, "GET", "/api/v1/me", nil, nil); status != http.StatusOK || nested(me, "actor", "handle") != handle {
		t.Fatalf("me after sign-in: status %d, body %v", status, me)
	}
}

func TestMobileSignInUsesBearerToken(t *testing.T) {
	e := newAuthTestEnv(t)
	pk := e.signUp(e.newClient(), uniqueHandle(t))

	app := &http.Client{} // no cookie jar
	status, body := e.signIn(app, pk, "mobile")
	token, _ := nested(body, "session", "token").(string)
	if status != http.StatusOK || token == "" {
		t.Fatalf("mobile sign-in: status %d, body %v", status, body)
	}
	bearer := map[string]string{"Authorization": "Bearer " + token}
	if status, _ := e.do(app, "GET", "/api/v1/me", nil, bearer); status != http.StatusOK {
		t.Fatalf("me with bearer token: status %d", status)
	}
	if status, _ := e.do(app, "POST", "/api/v1/auth/logout", nil, bearer); status != http.StatusNoContent {
		t.Fatalf("logout with bearer token: status %d", status)
	}
	if status, _ := e.do(app, "GET", "/api/v1/me", nil, bearer); status != http.StatusUnauthorized {
		t.Fatalf("me with revoked token: status %d, want 401", status)
	}
}

func TestSignupRejectsTakenHandleAndInvalidInput(t *testing.T) {
	e := newAuthTestEnv(t)
	handle := uniqueHandle(t)
	e.signUp(e.newClient(), handle)

	status, body := e.do(e.newClient(), "POST", "/api/v1/auth/signup/begin", map[string]string{
		"email": "other-" + handle + "@example.org", "handle": handle, "display_name": "Other",
	}, nil)
	if status != http.StatusConflict || errorCode(body) != "handle_taken" {
		t.Fatalf("taken handle: status %d, body %v", status, body)
	}

	status, body = e.do(e.newClient(), "POST", "/api/v1/auth/signup/begin", map[string]string{
		"email": "not-an-email", "handle": uniqueHandle(t), "display_name": "Other",
	}, nil)
	if status != http.StatusUnprocessableEntity || nested(body, "error", "field") != "email" {
		t.Fatalf("invalid email: status %d, body %v", status, body)
	}
}

func TestCrossSiteRequestsRejected(t *testing.T) {
	e := newAuthTestEnv(t)
	status, body := e.do(e.newClient(), "POST", "/api/v1/auth/login/begin", nil, map[string]string{
		"Sec-Fetch-Site": "cross-site",
	})
	if status != http.StatusForbidden || errorCode(body) != "cross_origin_request" {
		t.Fatalf("cross-site POST: status %d, body %v", status, body)
	}
}

func TestSignupClosedWhenRegistrationDisabled(t *testing.T) {
	e := newAuthTestEnv(t)
	ctx := context.Background()
	if err := e.store.SetFeatureFlag(ctx, store.SetFeatureFlagParams{Flag: "open_registration", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = e.store.SetFeatureFlag(ctx, store.SetFeatureFlagParams{Flag: "open_registration", Enabled: true})
	})

	status, body := e.do(e.newClient(), "POST", "/api/v1/auth/signup/begin", map[string]string{
		"email": "closed@example.org", "handle": uniqueHandle(t), "display_name": "Closed",
	}, nil)
	if status != http.StatusForbidden || errorCode(body) != "registration_closed" {
		t.Fatalf("closed registration: status %d, body %v", status, body)
	}
}

func uniqueHandle(t *testing.T) string {
	t.Helper()
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return "t_" + hex.EncodeToString(b)
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func nested(m map[string]any, keys ...string) any {
	var v any = m
	for _, k := range keys {
		mm, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = mm[k]
	}
	return v
}

func errorCode(body map[string]any) any {
	return nested(body, "error", "code")
}
