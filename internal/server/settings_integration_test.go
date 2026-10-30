// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/descope/virtualwebauthn"

	"github.com/chmbrb/PetCommunityProject/internal/auth"
)

// addPasskey adds a new passkey to the signed-in account and returns it.
func (e *authTestEnv) addPasskey(c *http.Client, owner *passkey, name string) (*passkey, map[string]any) {
	e.t.Helper()
	status, begin := e.do(c, "POST", "/api/v1/me/passkeys/begin", nil, nil)
	if status != http.StatusOK {
		e.t.Fatalf("add passkey begin: status %d, body %v", status, begin)
	}
	opts, err := virtualwebauthn.ParseAttestationOptions(mustJSON(e.t, begin["options"]))
	if err != nil {
		e.t.Fatal(err)
	}
	if len(opts.ExcludeCredentials) == 0 {
		e.t.Error("add passkey options don't exclude the account's existing passkeys")
	}
	pk := &passkey{
		credential: virtualwebauthn.NewCredential(virtualwebauthn.KeyTypeEC2),
		accountID:  owner.accountID, email: owner.email,
	}
	pk.authenticator = virtualwebauthn.NewAuthenticatorWithOptions(virtualwebauthn.AuthenticatorOptions{
		UserHandle: []byte(opts.UserID),
	})
	response := virtualwebauthn.CreateAttestationResponse(e.rp, pk.authenticator, pk.credential, *opts)
	pk.authenticator.AddCredential(pk.credential)

	status, body := e.do(c, "POST", "/api/v1/me/passkeys/finish", map[string]any{
		"challenge_id": begin["challenge_id"], "credential": json.RawMessage(response), "name": name,
	}, nil)
	if status != http.StatusCreated {
		e.t.Fatalf("add passkey finish: status %d, body %v", status, body)
	}
	return pk, body
}

// listPasskeys returns the names of the signed-in account's passkeys, by ID.
func (e *authTestEnv) listPasskeys(c *http.Client) map[string]string {
	e.t.Helper()
	status, body := e.do(c, "GET", "/api/v1/me/passkeys", nil, nil)
	if status != http.StatusOK {
		e.t.Fatalf("list passkeys: status %d, body %v", status, body)
	}
	out := map[string]string{}
	for _, item := range body["passkeys"].([]any) {
		m := item.(map[string]any)
		out[m["id"].(string)] = m["name"].(string)
	}
	return out
}

// ageSessions makes the account's sessions look like they signed in an hour ago.
func (e *authTestEnv) ageSessions(accountID string) {
	e.t.Helper()
	if _, err := e.pool.Exec(context.Background(),
		`UPDATE sessions SET created_at = now() - interval '1 hour' WHERE account_id = $1`, accountID); err != nil {
		e.t.Fatal(err)
	}
}

func TestManagePasskeys(t *testing.T) {
	e := newAuthTestEnv(t)
	browser := e.newClient()
	first := e.signUp(browser, uniqueHandle(t))

	passkeys := e.listPasskeys(browser)
	if len(passkeys) != 1 {
		t.Fatalf("passkeys after sign-up = %v, want 1", passkeys)
	}
	var firstID string
	for id, name := range passkeys {
		firstID = id
		if name != "Passkey" {
			t.Errorf("sign-up passkey name = %q, want %q", name, "Passkey")
		}
	}

	second, added := e.addPasskey(browser, first, "Work laptop")
	secondID := added["id"].(string)
	if added["name"] != "Work laptop" || len(e.listPasskeys(browser)) != 2 {
		t.Fatalf("after adding: %v, list %v", added, e.listPasskeys(browser))
	}

	// The account is told a passkey was added, in case it wasn't them.
	if n := e.queuedEmails(first.accountID, auth.PurposePasskeyAdded); n != 1 {
		t.Fatalf("passkey-added notices queued = %d, want 1", n)
	}
	if err := e.emailer.Send(context.Background(), auth.EmailJob{
		AccountID: mustUUID(t, first.accountID), Purpose: auth.PurposePasskeyAdded, Detail: "Work laptop",
	}); err != nil {
		t.Fatal(err)
	}
	notice := e.mail.Messages()[len(e.mail.Messages())-1]
	if !strings.Contains(notice.Text, "Work laptop") || !strings.Contains(notice.Text, testOrigin+"/settings") {
		t.Errorf("notice text:\n%s", notice.Text)
	}

	// The new passkey signs in.
	if status, body := e.signIn(e.newClient(), second, ""); status != http.StatusOK {
		t.Fatalf("sign in with added passkey: status %d, body %v", status, body)
	}

	if status, body := e.do(browser, "PATCH", "/api/v1/me/passkeys/"+firstID, map[string]string{"name": "Phone"}, nil); status != http.StatusNoContent {
		t.Fatalf("rename: status %d, body %v", status, body)
	}
	if got := e.listPasskeys(browser)[firstID]; got != "Phone" {
		t.Errorf("renamed passkey = %q, want Phone", got)
	}
	if status, body := e.do(browser, "PATCH", "/api/v1/me/passkeys/"+firstID, map[string]string{"name": "  "}, nil); status != http.StatusUnprocessableEntity {
		t.Errorf("rename to blank: status %d, body %v", status, body)
	}

	if status, body := e.do(browser, "DELETE", "/api/v1/me/passkeys/"+secondID, nil, nil); status != http.StatusNoContent {
		t.Fatalf("remove: status %d, body %v", status, body)
	}
	if n := e.queuedEmails(first.accountID, auth.PurposePasskeyRemoved); n != 1 {
		t.Errorf("passkey-removed notices queued = %d, want 1", n)
	}
	// The removed passkey no longer signs in.
	if status, _ := e.signIn(e.newClient(), second, ""); status != http.StatusUnauthorized {
		t.Errorf("sign in with removed passkey: status %d, want 401", status)
	}

	status, body := e.do(browser, "DELETE", "/api/v1/me/passkeys/"+firstID, nil, nil)
	if status != http.StatusConflict || errorCode(body) != "last_passkey" {
		t.Fatalf("remove last passkey: status %d, body %v", status, body)
	}
}

func TestPasskeysAreScopedToTheirAccount(t *testing.T) {
	e := newAuthTestEnv(t)
	alice := e.newClient()
	alicePK := e.signUp(alice, uniqueHandle(t))
	e.addPasskey(alice, alicePK, "Second")
	var aliceID string
	for id := range e.listPasskeys(alice) {
		aliceID = id
	}

	mallory := e.newClient()
	e.signUp(mallory, uniqueHandle(t))
	if status, _ := e.do(mallory, "DELETE", "/api/v1/me/passkeys/"+aliceID, nil, nil); status != http.StatusNotFound {
		t.Errorf("removing another account's passkey: status %d, want 404", status)
	}
	if status, _ := e.do(mallory, "PATCH", "/api/v1/me/passkeys/"+aliceID, map[string]string{"name": "pwned"}, nil); status != http.StatusNotFound {
		t.Errorf("renaming another account's passkey: status %d, want 404", status)
	}
	if got := e.listPasskeys(alice); len(got) != 2 || got[aliceID] == "pwned" {
		t.Errorf("Alice's passkeys changed: %v", got)
	}

	// A challenge started by one account can't be finished by another.
	_, begin := e.do(alice, "POST", "/api/v1/me/passkeys/begin", nil, nil)
	opts, _ := virtualwebauthn.ParseAttestationOptions(mustJSON(t, begin["options"]))
	cred := virtualwebauthn.NewCredential(virtualwebauthn.KeyTypeEC2)
	authn := virtualwebauthn.NewAuthenticatorWithOptions(virtualwebauthn.AuthenticatorOptions{UserHandle: []byte(opts.UserID)})
	response := virtualwebauthn.CreateAttestationResponse(e.rp, authn, cred, *opts)
	status, body := e.do(mallory, "POST", "/api/v1/me/passkeys/finish", map[string]any{
		"challenge_id": begin["challenge_id"], "credential": json.RawMessage(response), "name": "sneaky",
	}, nil)
	if status != http.StatusBadRequest || errorCode(body) != "challenge_expired" {
		t.Errorf("finishing another account's challenge: status %d, body %v", status, body)
	}
}

func TestChangingPasskeysRequiresRecentSignIn(t *testing.T) {
	e := newAuthTestEnv(t)
	browser := e.newClient()
	pk := e.signUp(browser, uniqueHandle(t))
	e.addPasskey(browser, pk, "Second")
	var someID string
	for id := range e.listPasskeys(browser) {
		someID = id
	}

	e.ageSessions(pk.accountID)
	for _, req := range []struct{ method, path string }{
		{"POST", "/api/v1/me/passkeys/begin"},
		{"DELETE", "/api/v1/me/passkeys/" + someID},
	} {
		status, body := e.do(browser, req.method, req.path, nil, nil)
		if status != http.StatusForbidden || errorCode(body) != "reauthentication_required" {
			t.Errorf("%s %s with an old sign-in: status %d, body %v", req.method, req.path, status, body)
		}
	}
	// Renaming is harmless and doesn't need it.
	if status, _ := e.do(browser, "PATCH", "/api/v1/me/passkeys/"+someID, map[string]string{"name": "Renamed"}, nil); status != http.StatusNoContent {
		t.Errorf("rename with an old sign-in: status %d, want 204", status)
	}

	// Confirming with a passkey replaces the old session, and changes are allowed again.
	if status, body := e.signIn(browser, pk, ""); status != http.StatusOK {
		t.Fatalf("re-sign-in: status %d, body %v", status, body)
	}
	if status, body := e.do(browser, "POST", "/api/v1/me/passkeys/begin", nil, nil); status != http.StatusOK {
		t.Fatalf("add passkey after re-sign-in: status %d, body %v", status, body)
	}
	_, sessions := e.do(browser, "GET", "/api/v1/me/sessions", nil, nil)
	if n := len(sessions["sessions"].([]any)); n != 1 {
		t.Errorf("sessions after re-sign-in = %d, want 1 (the old one replaced)", n)
	}
}

func TestManageSessions(t *testing.T) {
	e := newAuthTestEnv(t)
	laptop := e.newClient()
	pk := e.signUp(laptop, uniqueHandle(t))
	phone := e.newClient()
	if status, _ := e.signIn(phone, pk, ""); status != http.StatusOK {
		t.Fatal("phone sign-in failed")
	}

	_, body := e.do(laptop, "GET", "/api/v1/me/sessions", nil, nil)
	sessions := body["sessions"].([]any)
	if len(sessions) != 2 {
		t.Fatalf("sessions = %v, want 2", sessions)
	}
	var current int
	for _, s := range sessions {
		m := s.(map[string]any)
		if m["current"] == true {
			current++
		}
		if m["user_agent"] == "" {
			t.Errorf("session without user agent: %v", m)
		}
	}
	if current != 1 {
		t.Errorf("sessions marked current = %d, want 1", current)
	}

	status, body := e.do(laptop, "POST", "/api/v1/me/sessions/revoke-others", nil, nil)
	if status != http.StatusOK || body["revoked"] != float64(1) {
		t.Fatalf("revoke others: status %d, body %v", status, body)
	}
	if status, _ := e.do(phone, "GET", "/api/v1/me", nil, nil); status != http.StatusUnauthorized {
		t.Errorf("phone after revoke-others: status %d, want 401", status)
	}
	if status, _ := e.do(laptop, "GET", "/api/v1/me", nil, nil); status != http.StatusOK {
		t.Errorf("laptop after revoke-others: status %d, want 200", status)
	}

	// Another account can't sign this one out.
	_, body = e.do(laptop, "GET", "/api/v1/me/sessions", nil, nil)
	laptopSession := body["sessions"].([]any)[0].(map[string]any)["id"].(string)
	mallory := e.newClient()
	e.signUp(mallory, uniqueHandle(t))
	if status, _ := e.do(mallory, "DELETE", "/api/v1/me/sessions/"+laptopSession, nil, nil); status != http.StatusNotFound {
		t.Errorf("revoking another account's session: status %d, want 404", status)
	}
	if status, _ := e.do(laptop, "GET", "/api/v1/me", nil, nil); status != http.StatusOK {
		t.Fatalf("laptop signed out by another account: status %d", status)
	}

	// Revoking the current session signs this device out.
	if status, _ := e.do(laptop, "DELETE", "/api/v1/me/sessions/"+laptopSession, nil, nil); status != http.StatusNoContent {
		t.Fatalf("revoke current session: status %d", status)
	}
	if status, _ := e.do(laptop, "GET", "/api/v1/me", nil, nil); status != http.StatusUnauthorized {
		t.Errorf("after revoking current session: status %d, want 401", status)
	}
}
