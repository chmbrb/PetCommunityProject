// SPDX-License-Identifier: AGPL-3.0-or-later

package keyrotation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/chmbrb/PetCommunityProject/internal/actorkeys"
	"github.com/chmbrb/PetCommunityProject/internal/auth"
	"github.com/chmbrb/PetCommunityProject/internal/database"
)

// TestRotationInPostgres runs the queries against a real database. Other packages'
// tests share it, so this checks its own rows rather than totals, and leaves evidence
// out of the run: other tests' cases point at evidence in their own buckets.
func TestRotationInPostgres(t *testing.T) {
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

	// Keys unique to this run, so its rows are the only ones under them.
	oldSecret, newSecret := randomKey(t), randomKey(t)
	oldMFA, newMFA := randomKey(t), randomKey(t)
	oldActor := must(actorkeys.NewSealer(oldSecret, nil))
	oldTOTP := must(auth.NewMFAKeyring(oldMFA, nil))
	kp := must(actorkeys.Generate())
	secret := []byte("authenticator secret")

	var accounts []uuid.UUID
	for range 3 {
		accountID, actorID := uuid.New(), uuid.New()
		handle := "r_" + accountID.String()[:8]
		userHandle := sha512.Sum512(accountID[:])
		exec(`INSERT INTO accounts (id, email, webauthn_user_handle) VALUES ($1, $2, $3)`, accountID, handle+"@example.org", userHandle[:])
		exec(`INSERT INTO actors (id, type, handle, display_name, account_id, public_key_pem, private_key_enc, private_key_kid, keys_created_at)
		      VALUES ($1, 'person', $2, 'Rotation Test', $3, 'pem', $4, $5, now())`,
			actorID, handle, accountID, must(oldActor.Seal(actorID, kp.PrivateKey)), oldActor.KeyID())
		exec(`INSERT INTO totp_factors (account_id, secret_encrypted, key_id, confirmed_at) VALUES ($1, $2, $3, now())`,
			accountID, must(oldTOTP.Seal(accountID, secret)), oldTOTP.KeyID())
		accounts = append(accounts, accountID)
	}

	keys := Keys{
		ActorKeys: must(actorkeys.NewSealer(newSecret, oldSecret)),
		MFA:       must(auth.NewMFAKeyring(newMFA, oldMFA)),
	}
	r := New(database.NewStore(pool), keys)

	before := byKind(must(r.Status(ctx)))
	if before[kindActorKeys].Previous < 3 || before[kindTOTP].Previous < 3 {
		t.Fatalf("before: %s; %s", before[kindActorKeys], before[kindTOTP])
	}
	after := byKind(must(r.Reencrypt(ctx)))
	if after[kindActorKeys].Previous != 0 || after[kindTOTP].Previous != 0 {
		t.Errorf("after: %s; %s", after[kindActorKeys], after[kindTOTP])
	}
	if after[kindEvidence].Skipped == "" {
		t.Error("evidence wasn't skipped without its key")
	}

	// Each row opens with the new keys alone.
	newActor := must(actorkeys.NewSealer(newSecret, nil))
	newTOTP := must(auth.NewMFAKeyring(newMFA, nil))
	for _, accountID := range accounts {
		var actorID uuid.UUID
		var enc, totp []byte
		var actorKid, totpKid string
		if err := pool.QueryRow(ctx, `SELECT a.id, a.private_key_enc, a.private_key_kid, t.secret_encrypted, t.key_id
			FROM actors a JOIN totp_factors t ON t.account_id = a.account_id WHERE a.account_id = $1`, accountID).
			Scan(&actorID, &enc, &actorKid, &totp, &totpKid); err != nil {
			t.Fatal(err)
		}
		if got, err := newActor.Open(actorID, actorKid, enc); err != nil || !got.Equal(kp.PrivateKey) {
			t.Errorf("actor key after rotation: %v", err)
		}
		if got, err := newTOTP.Open(accountID, totpKid, totp); err != nil || !bytes.Equal(got, secret) {
			t.Errorf("authenticator secret after rotation: %v", err)
		}
	}

	// The evidence query lists a case with its file.
	actorID := uuid.New()
	mediaID, caseID := uuid.New(), uuid.New()
	sum := sha256.Sum256(caseID[:])
	// An organization, which needs no account (person actors must have one).
	exec(`INSERT INTO actors (id, type, handle, display_name) VALUES ($1, 'organization', $2, 'Evidence Test')`, actorID, "e_"+actorID.String()[:8])
	exec(`INSERT INTO media_assets (id, owner_actor_id, purpose, status, upload_bytes) VALUES ($1, $2, 'avatar', 'quarantined', 1)`, mediaID, actorID)
	exec(`INSERT INTO abuse_imagery_cases (id, media_id, owner_actor_id, scanner, sha256, evidence_key) VALUES ($1, $2, $3, 'hashlist', $4, $5)`,
		caseID, mediaID, actorID, sum[:], "media/"+mediaID.String())
	st := database.NewStore(pool)
	found := false
	for _, c := range must(st.ListAbuseImageryEvidence(ctx)) {
		if c.ID == caseID && c.MediaID == mediaID && c.EvidenceKey == "media/"+mediaID.String() {
			found = true
		}
	}
	if !found {
		t.Error("ListAbuseImageryEvidence doesn't list the case")
	}
	if exists := must(st.AbuseImageryCaseExists(ctx, caseID)); !exists {
		t.Error("AbuseImageryCaseExists = false for an existing case")
	}
	if exists := must(st.AbuseImageryCaseExists(ctx, uuid.New())); exists {
		t.Error("AbuseImageryCaseExists = true for a missing case")
	}
}

func randomKey(t *testing.T) []byte {
	t.Helper()
	id := uuid.New()
	sum := sha256.Sum256(append(id[:], []byte(t.Name())...))
	return sum[:]
}
