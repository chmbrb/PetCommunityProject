// SPDX-License-Identifier: AGPL-3.0-or-later

package abusecases

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/chmbrb/PetCommunityProject/internal/media"
	"github.com/chmbrb/PetCommunityProject/internal/storage"
	"github.com/chmbrb/PetCommunityProject/internal/store"
)

// fakeDB keeps cases and audit events in memory. Its transactions don't roll back,
// so tests only check them on success or on errors raised before any write.
type fakeDB struct {
	cases  map[uuid.UUID]store.GetAbuseImageryCaseRow
	audits []store.InsertAuditEventParams
}

func (f *fakeDB) InTx(_ context.Context, fn func(q Queries) error) error { return fn(f) }

func (f *fakeDB) ListAbuseImageryCases(_ context.Context, status *string) ([]store.ListAbuseImageryCasesRow, error) {
	var out []store.ListAbuseImageryCasesRow
	for _, c := range f.cases {
		if status == nil || c.Status == *status {
			out = append(out, store.ListAbuseImageryCasesRow{ID: c.ID, Status: c.Status})
		}
	}
	return out, nil
}

func (f *fakeDB) GetAbuseImageryCase(_ context.Context, id uuid.UUID) (store.GetAbuseImageryCaseRow, error) {
	c, ok := f.cases[id]
	if !ok {
		return c, pgx.ErrNoRows
	}
	return c, nil
}

func (f *fakeDB) MarkAbuseImageryCaseReported(_ context.Context, arg store.MarkAbuseImageryCaseReportedParams) (int64, error) {
	c, ok := f.cases[arg.ID]
	if !ok || c.Status != "open" {
		return 0, nil
	}
	c.Status, c.ReportReference, c.ReportedAt, c.PreserveUntil = "reported", arg.ReportReference, arg.ReportedAt, arg.PreserveUntil
	f.cases[arg.ID] = c
	return 1, nil
}

func (f *fakeDB) CloseAbuseImageryCase(_ context.Context, id uuid.UUID) (int64, error) {
	c, ok := f.cases[id]
	if !ok || c.Status == "closed" {
		return 0, nil
	}
	c.Status = "closed"
	closed := now
	c.ClosedAt = &closed
	f.cases[id] = c
	return 1, nil
}

// purgeable mirrors the SQL: the database's now() is closedBefore + ClosedGrace.
func (f *fakeDB) purgeable(c store.GetAbuseImageryCaseRow, closedBefore time.Time) bool {
	current := closedBefore.Add(ClosedGrace)
	return c.Status == "closed" && c.ClosedAt.Before(closedBefore) && (c.PreserveUntil == nil || c.PreserveUntil.Before(current))
}

func (f *fakeDB) ListPurgeableAbuseImageryCases(_ context.Context, closedBefore *time.Time) ([]store.ListPurgeableAbuseImageryCasesRow, error) {
	var out []store.ListPurgeableAbuseImageryCasesRow
	for _, c := range f.cases {
		if f.purgeable(c, *closedBefore) {
			out = append(out, store.ListPurgeableAbuseImageryCasesRow{ID: c.ID, MediaID: c.MediaID, EvidenceKey: c.EvidenceKey, ReportReference: c.ReportReference})
		}
	}
	return out, nil
}

func (f *fakeDB) DeletePurgeableAbuseImageryCase(_ context.Context, arg store.DeletePurgeableAbuseImageryCaseParams) (int64, error) {
	c, ok := f.cases[arg.ID]
	if !ok || !f.purgeable(c, *arg.ClosedBefore) {
		return 0, nil
	}
	delete(f.cases, arg.ID)
	return 1, nil
}

func (f *fakeDB) InsertAuditEvent(_ context.Context, arg store.InsertAuditEventParams) error {
	f.audits = append(f.audits, arg)
	return nil
}

var now = time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)

// setup returns a service with one open case whose evidence is sealed in the bucket.
func setup(t *testing.T) (*Service, *fakeDB, *storage.MemoryBucket, store.GetAbuseImageryCaseRow, []byte) {
	t.Helper()
	sealer, err := media.NewEvidenceSealer(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	original := []byte("stand-in for the original upload")
	sum := sha256.Sum256(original)
	c := store.GetAbuseImageryCaseRow{
		ID: uuid.New(), MediaID: uuid.New(), Status: "open", Sha256: sum[:], EvidenceKey: "evidence/case.bin",
	}
	sealed, err := sealer.Seal(c.MediaID, original)
	if err != nil {
		t.Fatal(err)
	}
	bucket := &storage.MemoryBucket{}
	if err := bucket.Put(context.Background(), c.EvidenceKey, bytes.NewReader(sealed), int64(len(sealed)), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	db := &fakeDB{cases: map[uuid.UUID]store.GetAbuseImageryCaseRow{c.ID: c}}
	return newService(db, bucket, sealer, func() time.Time { return now }), db, bucket, c, original
}

func details(t *testing.T, e store.InsertAuditEventParams) map[string]any {
	t.Helper()
	var d map[string]any
	if err := json.Unmarshal(e.Details, &d); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestExportWritesTheOriginalAndRecordsIt(t *testing.T) {
	svc, db, _, c, original := setup(t)
	var out bytes.Buffer
	if err := svc.Export(context.Background(), c.ID, "Sam", &out); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.Bytes(), original) {
		t.Fatal("exported file isn't the original")
	}
	if len(db.audits) != 1 || db.audits[0].Action != "abuse_imagery.evidence_exported" || db.audits[0].TargetID != c.ID {
		t.Fatalf("audit = %+v", db.audits)
	}
	if d := details(t, db.audits[0]); d["operator"] != "Sam" || d["sha256"] == "" {
		t.Errorf("details = %v", d)
	}
	if db.audits[0].AccountID.Valid {
		t.Error("operator actions aren't done by a signed-in account")
	}
}

func TestExportRefusesEvidenceThatDoesNotMatchTheCase(t *testing.T) {
	svc, db, _, c, _ := setup(t)
	c.Sha256 = make([]byte, 32)
	db.cases[c.ID] = c
	var out bytes.Buffer
	if err := svc.Export(context.Background(), c.ID, "Sam", &out); !errors.Is(err, ErrEvidenceMismatch) {
		t.Fatalf("err = %v, want ErrEvidenceMismatch", err)
	}
	if out.Len() != 0 || len(db.audits) != 0 {
		t.Error("nothing should be written or recorded")
	}
}

func TestExportRefusesTamperedEvidence(t *testing.T) {
	svc, _, bucket, c, _ := setup(t)
	if err := bucket.Put(context.Background(), c.EvidenceKey, bytes.NewReader([]byte{1, 2, 3}), 3, storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Export(context.Background(), c.ID, "Sam", &bytes.Buffer{}); !errors.Is(err, media.ErrEvidenceCorrupt) {
		t.Fatalf("err = %v, want ErrEvidenceCorrupt", err)
	}
}

func TestActionsNeedAnOperator(t *testing.T) {
	svc, db, _, c, _ := setup(t)
	ctx := context.Background()
	if err := svc.Export(ctx, c.ID, " ", &bytes.Buffer{}); err == nil {
		t.Error("export without an operator")
	}
	if _, err := svc.Report(ctx, c.ID, "123", time.Time{}, time.Time{}, ""); err == nil {
		t.Error("report without an operator")
	}
	if err := svc.Close(ctx, c.ID, "false match", ""); err == nil {
		t.Error("close without an operator")
	}
	if len(db.audits) != 0 {
		t.Error("nothing should be recorded")
	}
}

func TestUnknownCase(t *testing.T) {
	svc, _, _, _, _ := setup(t)
	ctx := context.Background()
	if _, err := svc.Get(ctx, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get: %v", err)
	}
	if err := svc.Export(ctx, uuid.New(), "Sam", &bytes.Buffer{}); !errors.Is(err, ErrNotFound) {
		t.Errorf("Export: %v", err)
	}
	if _, err := svc.Report(ctx, uuid.New(), "123", time.Time{}, time.Time{}, "Sam"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Report: %v", err)
	}
}

func TestReportPreservesForAtLeastAYear(t *testing.T) {
	svc, db, _, c, _ := setup(t)
	until, err := svc.Report(context.Background(), c.ID, " 98765 ", time.Time{}, time.Time{}, "Sam")
	if err != nil {
		t.Fatal(err)
	}
	if want := now.AddDate(1, 0, 0); !until.Equal(want) {
		t.Errorf("preserve until %v, want %v", until, want)
	}
	got := db.cases[c.ID]
	if got.Status != "reported" || *got.ReportReference != "98765" || !got.ReportedAt.Equal(now) {
		t.Errorf("case = %+v", got)
	}
	if len(db.audits) != 1 || db.audits[0].Action != "abuse_imagery.reported" {
		t.Fatalf("audit = %+v", db.audits)
	}
	if d := details(t, db.audits[0]); d["report_reference"] != "98765" || d["operator"] != "Sam" {
		t.Errorf("details = %v", d)
	}
}

func TestReportKeepsALongerPreservation(t *testing.T) {
	svc, _, _, c, _ := setup(t)
	later := now.AddDate(3, 0, 0)
	until, err := svc.Report(context.Background(), c.ID, "98765", now.Add(-time.Hour), later, "Sam")
	if err != nil {
		t.Fatal(err)
	}
	if !until.Equal(later) {
		t.Errorf("preserve until %v, want %v", until, later)
	}
}

func TestReportRejectsBadInput(t *testing.T) {
	svc, _, _, c, _ := setup(t)
	ctx := context.Background()
	if _, err := svc.Report(ctx, c.ID, "", time.Time{}, time.Time{}, "Sam"); err == nil {
		t.Error("report without a reference")
	}
	if _, err := svc.Report(ctx, c.ID, "1", now.Add(time.Hour), time.Time{}, "Sam"); err == nil {
		t.Error("report time in the future")
	}
}

func TestReportOnlyOnce(t *testing.T) {
	svc, db, _, c, _ := setup(t)
	ctx := context.Background()
	if _, err := svc.Report(ctx, c.ID, "1", time.Time{}, time.Time{}, "Sam"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Report(ctx, c.ID, "2", time.Time{}, time.Time{}, "Sam"); !errors.Is(err, ErrWrongStatus) {
		t.Fatalf("err = %v, want ErrWrongStatus", err)
	}
	if *db.cases[c.ID].ReportReference != "1" {
		t.Error("the first report's reference was replaced")
	}
}

func TestCloseRecordsTheReason(t *testing.T) {
	svc, db, _, c, _ := setup(t)
	ctx := context.Background()
	if err := svc.Close(ctx, c.ID, "hash list false positive", "Sam"); err != nil {
		t.Fatal(err)
	}
	if db.cases[c.ID].Status != "closed" {
		t.Fatal("case not closed")
	}
	d := details(t, db.audits[0])
	if db.audits[0].Action != "abuse_imagery.closed" || d["reason"] != "hash list false positive" || d["previous_status"] != "open" {
		t.Errorf("audit = %s %v", db.audits[0].Action, d)
	}
	if err := svc.Close(ctx, c.ID, "again", "Sam"); !errors.Is(err, ErrWrongStatus) {
		t.Errorf("closing twice: %v", err)
	}
	if err := svc.Close(ctx, c.ID, " ", "Sam"); err == nil {
		t.Error("close without a reason")
	}
}

func TestListFiltersByStatus(t *testing.T) {
	svc, _, _, _, _ := setup(t)
	ctx := context.Background()
	for status, want := range map[string]int{"open": 1, "reported": 0, "all": 1} {
		rows, err := svc.List(ctx, status)
		if err != nil || len(rows) != want {
			t.Errorf("List(%q) = %d rows, %v; want %d", status, len(rows), err, want)
		}
	}
	if _, err := svc.List(ctx, "pending"); err == nil {
		t.Error("unknown status accepted")
	}
}

func TestPurgeWaitsForTheGracePeriod(t *testing.T) {
	svc, db, bucket, c, _ := setup(t)
	ctx := context.Background()
	if err := svc.Close(ctx, c.ID, "hash list false positive", "Sam"); err != nil {
		t.Fatal(err)
	}
	svc.now = func() time.Time { return now.Add(ClosedGrace - time.Hour) }
	if n, err := svc.Purge(ctx); err != nil || n != 0 {
		t.Fatalf("purged %d, %v before the grace period ended", n, err)
	}
	if _, ok := bucket.Objects()[c.EvidenceKey]; !ok {
		t.Fatal("evidence deleted before the grace period ended")
	}

	svc.now = func() time.Time { return now.Add(ClosedGrace + time.Hour) }
	if n, err := svc.Purge(ctx); err != nil || n != 1 {
		t.Fatalf("purged %d, %v; want 1", n, err)
	}
	if _, ok := bucket.Objects()[c.EvidenceKey]; ok {
		t.Error("evidence not deleted")
	}
	if _, ok := db.cases[c.ID]; ok {
		t.Error("case not deleted")
	}
	last := db.audits[len(db.audits)-1]
	if last.Action != "abuse_imagery.purged" || last.TargetID != c.ID {
		t.Errorf("last audit = %+v", last)
	}
	if n, err := svc.Purge(ctx); err != nil || n != 0 {
		t.Errorf("second purge: %d, %v", n, err)
	}
}

func TestPurgeKeepsReportedEvidenceUntilItsPreservationDate(t *testing.T) {
	svc, db, bucket, c, _ := setup(t)
	ctx := context.Background()
	if _, err := svc.Report(ctx, c.ID, "98765", time.Time{}, time.Time{}, "Sam"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Close(ctx, c.ID, "report acknowledged", "Sam"); err != nil {
		t.Fatal(err)
	}
	// Well past the grace period, but before the year is up.
	svc.now = func() time.Time { return now.AddDate(0, 6, 0) }
	if n, err := svc.Purge(ctx); err != nil || n != 0 {
		t.Fatalf("purged %d, %v before the preservation date", n, err)
	}
	svc.now = func() time.Time { return now.AddDate(1, 0, 1) }
	if n, err := svc.Purge(ctx); err != nil || n != 1 {
		t.Fatalf("purged %d, %v after the preservation date; want 1", n, err)
	}
	if _, ok := bucket.Objects()[c.EvidenceKey]; ok || db.cases[c.ID].ID != uuid.Nil {
		t.Error("evidence or case left behind")
	}
}

func TestPurgeNeverTouchesOpenOrReportedCases(t *testing.T) {
	svc, db, bucket, c, _ := setup(t)
	ctx := context.Background()
	svc.now = func() time.Time { return now.AddDate(5, 0, 0) }
	if n, err := svc.Purge(ctx); err != nil || n != 0 {
		t.Fatalf("purged an open case: %d, %v", n, err)
	}
	svc.now = func() time.Time { return now }
	if _, err := svc.Report(ctx, c.ID, "98765", time.Time{}, time.Time{}, "Sam"); err != nil {
		t.Fatal(err)
	}
	svc.now = func() time.Time { return now.AddDate(5, 0, 0) }
	if n, err := svc.Purge(ctx); err != nil || n != 0 {
		t.Fatalf("purged a reported case that isn't closed: %d, %v", n, err)
	}
	if _, ok := bucket.Objects()[c.EvidenceKey]; !ok || db.cases[c.ID].Status != "reported" {
		t.Error("an unclosed case lost its evidence")
	}
}

func TestPurgeAfter(t *testing.T) {
	closed := now
	if !PurgeAfter(nil, nil).IsZero() {
		t.Error("a case that isn't closed has no purge date")
	}
	if got := PurgeAfter(&closed, nil); !got.Equal(now.Add(ClosedGrace)) {
		t.Errorf("unreported: %v", got)
	}
	keep := now.AddDate(1, 0, 0)
	if got := PurgeAfter(&closed, &keep); !got.Equal(keep) {
		t.Errorf("reported: %v", got)
	}
}
