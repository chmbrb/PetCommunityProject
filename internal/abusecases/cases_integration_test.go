// SPDX-License-Identifier: AGPL-3.0-or-later

package abusecases

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/chmbrb/PetCommunityProject/internal/database"
	"github.com/chmbrb/PetCommunityProject/internal/media"
	"github.com/chmbrb/PetCommunityProject/internal/storage"
	"github.com/chmbrb/PetCommunityProject/internal/store"
)

// TestCaseLifecycleInPostgres runs the queries against a real database, including the
// table's own rule that a reported case is preserved for at least a year.
func TestCaseLifecycleInPostgres(t *testing.T) {
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

	// An uploader, their upload, and a case with sealed evidence, as a match leaves them.
	accountID, actorID, mediaID := uuid.New(), uuid.New(), uuid.New()
	handle := "c_" + accountID.String()[:8]
	userHandle := sha512.Sum512(accountID[:])
	exec(`INSERT INTO accounts (id, email, webauthn_user_handle, status) VALUES ($1, $2, $3, 'suspended')`,
		accountID, handle+"@example.org", userHandle[:])
	exec(`INSERT INTO actors (id, type, handle, display_name, account_id) VALUES ($1, 'person', $2, 'Case Test', $3)`,
		actorID, handle, accountID)
	exec(`INSERT INTO media_assets (id, owner_actor_id, purpose, status, upload_bytes) VALUES ($1, $2, 'avatar', 'quarantined', 33)`,
		mediaID, actorID)

	sealer, err := media.NewEvidenceSealer(bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatal(err)
	}
	original := []byte("stand-in for the original upload")
	sum := sha256.Sum256(original)
	sealed, err := sealer.Seal(mediaID, original)
	if err != nil {
		t.Fatal(err)
	}
	bucket := &storage.MemoryBucket{}
	key := "evidence/" + mediaID.String()
	if err := bucket.Put(ctx, key, bytes.NewReader(sealed), int64(len(sealed)), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	st := database.NewStore(pool)
	caseID := uuid.New()
	if _, err := st.CreateAbuseImageryCase(ctx, store.CreateAbuseImageryCaseParams{
		ID: caseID, MediaID: mediaID, OwnerActorID: actorID, AccountID: uuid.NullUUID{UUID: accountID, Valid: true},
		Scanner: "hashlist", Sha256: sum[:], EvidenceKey: key,
	}); err != nil {
		t.Fatal(err)
	}
	svc := New(st, bucket, sealer)

	c, err := svc.Get(ctx, caseID)
	if err != nil {
		t.Fatal(err)
	}
	if c.Handle != handle || c.AccountEmail == nil || *c.AccountEmail != handle+"@example.org" || c.UploadBytes != 33 {
		t.Errorf("case details = %+v", c)
	}
	open, err := svc.List(ctx, "open")
	if err != nil || !containsCase(open, caseID) {
		t.Fatalf("open cases = %v, %v", open, err)
	}

	var out bytes.Buffer
	if err := svc.Export(ctx, caseID, "Sam", &out); err != nil || !bytes.Equal(out.Bytes(), original) {
		t.Fatalf("export: %v", err)
	}

	// The table refuses a reported case kept for less than a year.
	if _, err := st.MarkAbuseImageryCaseReported(ctx, store.MarkAbuseImageryCaseReportedParams{
		ID: caseID, ReportReference: ptr("x"), ReportedAt: ptr(time.Now()), PreserveUntil: ptr(time.Now().AddDate(0, 6, 0)),
	}); err == nil {
		t.Fatal("database accepted a preservation period under a year")
	}
	if _, err := svc.Report(ctx, caseID, "98765", time.Time{}, time.Time{}, "Sam"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Report(ctx, caseID, "98766", time.Time{}, time.Time{}, "Sam"); !errors.Is(err, ErrWrongStatus) {
		t.Fatalf("second report: %v", err)
	}
	if err := svc.Close(ctx, caseID, "report filed and acknowledged", "Sam"); err != nil {
		t.Fatal(err)
	}
	c, err = svc.Get(ctx, caseID)
	if err != nil || c.Status != "closed" || c.PreserveUntil == nil || c.PreserveUntil.Before(time.Now().AddDate(1, 0, -1)) {
		t.Fatalf("closed case = %+v, %v", c, err)
	}

	var actions []string
	rows, err := pool.Query(ctx, `SELECT action FROM audit_log WHERE target_id = $1 ORDER BY occurred_at, id`, caseID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			t.Fatal(err)
		}
		actions = append(actions, a)
	}
	want := []string{"abuse_imagery.evidence_exported", "abuse_imagery.reported", "abuse_imagery.closed"}
	if len(actions) != len(want) {
		t.Fatalf("audit actions = %v, want %v", actions, want)
	}
	for i := range want {
		if actions[i] != want[i] {
			t.Fatalf("audit actions = %v, want %v", actions, want)
		}
	}
}

func containsCase(rows []store.ListAbuseImageryCasesRow, id uuid.UUID) bool {
	for _, r := range rows {
		if r.ID == id {
			return true
		}
	}
	return false
}

func ptr[T any](v T) *T { return &v }
