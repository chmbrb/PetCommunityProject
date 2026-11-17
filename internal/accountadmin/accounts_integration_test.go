// SPDX-License-Identifier: AGPL-3.0-or-later

package accountadmin

import (
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/chmbrb/PetCommunityProject/internal/database"
)

// TestAccountActionsInPostgres runs the queries against a real database, including the
// rule that an abuse imagery case keeps its account suspended. Sign-ups aren't switched
// here: other packages' tests share the database and sign people up.
func TestAccountActionsInPostgres(t *testing.T) {
	url := os.Getenv("PC_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("PC_TEST_DATABASE_URL not set; skipping database integration test")
	}
	ctx := context.Background()
	if _, err := database.Migrate(ctx, url); err != nil {
		t.Fatal(err)
	}
	pool, err := database.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}

	accountID, actorID := uuid.New(), uuid.New()
	handle := "a_" + accountID.String()[:8]
	email := handle + "@example.org"
	userHandle := sha512.Sum512(accountID[:])
	exec(`INSERT INTO accounts (id, email, webauthn_user_handle, email_verified_at) VALUES ($1, $2, $3, now())`,
		accountID, email, userHandle[:])
	exec(`INSERT INTO actors (id, type, handle, display_name, account_id) VALUES ($1, 'person', $2, 'Admin Test', $3)`,
		actorID, handle, accountID)
	// Two passkeys, and a signed-in device.
	keep, stranger := uuid.New(), uuid.New()
	for i, id := range []uuid.UUID{keep, stranger} {
		exec(`INSERT INTO auth_identities (id, account_id, kind) VALUES ($1, $2, 'passkey')`, id, accountID)
		exec(`INSERT INTO passkey_credentials (identity_id, credential_id, credential, name) VALUES ($1, $2, '{}', $3)`,
			id, id[:], []string{"Phone", "Unknown laptop"}[i])
	}
	token := sha256.Sum256(accountID[:])
	exec(`INSERT INTO sessions (id, token_hash, account_id, client, expires_at) VALUES ($1, $2, $3, 'web', $4)`,
		uuid.New(), token[:], accountID, time.Now().Add(time.Hour))

	svc := New(database.NewStore(pool))

	found, err := svc.Find(ctx, "@"+handle)
	if err != nil || len(found) != 1 || found[0].ID != accountID {
		t.Fatalf("Find = %v, %v", found, err)
	}
	if found, _ := svc.Find(ctx, email); len(found) != 1 {
		t.Errorf("Find by email = %v", found)
	}
	a, err := svc.Get(ctx, accountID)
	if err != nil || len(a.Passkeys) != 2 || len(a.Sessions) != 1 || a.HasPassword || a.HasTotp || a.HeldByCases != 0 {
		t.Fatalf("Get = %+v, %v", a, err)
	}

	if revoked, err := svc.Suspend(ctx, accountID, "suspected takeover", "Sam"); err != nil || revoked != 1 {
		t.Fatalf("Suspend: revoked %d, %v", revoked, err)
	}
	if a, _ := svc.Get(ctx, accountID); a.Status != "suspended" || len(a.Sessions) != 0 {
		t.Errorf("after suspending: status %q, %d sessions", a.Status, len(a.Sessions))
	}
	if name, err := svc.RemovePasskey(ctx, accountID, stranger, "added during takeover", "Sam"); err != nil || name != "Unknown laptop" {
		t.Fatalf("RemovePasskey: %q, %v", name, err)
	}

	// An open abuse imagery case holds the suspension, in the database itself.
	mediaID, caseID := uuid.New(), uuid.New()
	exec(`INSERT INTO media_assets (id, owner_actor_id, purpose, status, upload_bytes) VALUES ($1, $2, 'avatar', 'quarantined', 1)`,
		mediaID, actorID)
	exec(`INSERT INTO abuse_imagery_cases (id, media_id, owner_actor_id, account_id, scanner, sha256, evidence_key)
	      VALUES ($1, $2, $3, $4, 'hashlist', $5, 'media/x')`, caseID, mediaID, actorID, accountID, token[:])
	if err := svc.Reinstate(ctx, accountID, "owner confirmed", "Sam"); !errors.Is(err, ErrHeldByCase) {
		t.Fatalf("reinstate with an open case: %v", err)
	}
	exec(`UPDATE abuse_imagery_cases SET status = 'closed', closed_at = now() WHERE id = $1`, caseID)
	if err := svc.Reinstate(ctx, accountID, "false match, case closed", "Sam"); err != nil {
		t.Fatal(err)
	}
	if a, _ := svc.Get(ctx, accountID); a.Status != "active" || len(a.Passkeys) != 1 {
		t.Errorf("after reinstating: status %q, %d passkeys", a.Status, len(a.Passkeys))
	}

	var actions []string
	rows, err := pool.Query(ctx, `SELECT action FROM audit_log WHERE target_id = $1 ORDER BY occurred_at, id`, accountID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		actions = append(actions, s)
	}
	want := []string{"account.suspended", "passkey.removed", "account.reinstated"}
	if len(actions) != len(want) || actions[0] != want[0] || actions[1] != want[1] || actions[2] != want[2] {
		t.Errorf("audit actions = %v, want %v", actions, want)
	}
}
