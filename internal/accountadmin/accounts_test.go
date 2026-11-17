// SPDX-License-Identifier: AGPL-3.0-or-later

package accountadmin

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/chmbrb/PetCommunityProject/internal/store"
)

// fakeDB mirrors the SQL's rules in memory. Its transactions don't roll back, so tests
// only check them on success or on errors raised before any write.
type fakeDB struct {
	accounts map[uuid.UUID]store.GetAccountForOperatorRow
	passkeys map[uuid.UUID][]store.ListAccountPasskeysRow
	sessions map[uuid.UUID]int64 // active sessions per account
	signups  bool
	audits   []store.InsertAuditEventParams
}

func (f *fakeDB) InTx(_ context.Context, fn func(q Queries) error) error { return fn(f) }

func (f *fakeDB) FindAccounts(_ context.Context, query string) ([]store.FindAccountsRow, error) {
	var out []store.FindAccountsRow
	for _, a := range f.accounts {
		if a.Handle == query || a.Email == query {
			out = append(out, store.FindAccountsRow{ID: a.ID, Handle: a.Handle, Email: a.Email, Status: a.Status})
		}
	}
	return out, nil
}

func (f *fakeDB) GetAccountForOperator(_ context.Context, id uuid.UUID) (store.GetAccountForOperatorRow, error) {
	a, ok := f.accounts[id]
	if !ok {
		return a, pgx.ErrNoRows
	}
	return a, nil
}

func (f *fakeDB) ListAccountPasskeys(_ context.Context, id uuid.UUID) ([]store.ListAccountPasskeysRow, error) {
	return f.passkeys[id], nil
}

func (f *fakeDB) ListAccountSessions(_ context.Context, id uuid.UUID) ([]store.ListAccountSessionsRow, error) {
	return make([]store.ListAccountSessionsRow, f.sessions[id]), nil
}

func (f *fakeDB) setStatus(id uuid.UUID, from []string, to string) int64 {
	a, ok := f.accounts[id]
	if !ok {
		return 0
	}
	for _, s := range from {
		if a.Status == s {
			a.Status = to
			f.accounts[id] = a
			return 1
		}
	}
	return 0
}

func (f *fakeDB) SuspendAccount(_ context.Context, id uuid.UUID) (int64, error) {
	return f.setStatus(id, []string{"active", "restricted"}, "suspended"), nil
}

func (f *fakeDB) RevokeAllAccountSessions(_ context.Context, id uuid.UUID) (int64, error) {
	n := f.sessions[id]
	f.sessions[id] = 0
	return n, nil
}

func (f *fakeDB) ReinstateAccount(_ context.Context, id uuid.UUID) (int64, error) {
	if f.accounts[id].HeldByCases > 0 {
		return 0, nil
	}
	return f.setStatus(id, []string{"suspended"}, "active"), nil
}

func (f *fakeDB) CountAccountPasskeys(_ context.Context, id uuid.UUID) (int64, error) {
	return int64(len(f.passkeys[id])), nil
}

func (f *fakeDB) GetAccountPasskeyName(_ context.Context, arg store.GetAccountPasskeyNameParams) (string, error) {
	for _, p := range f.passkeys[arg.AccountID] {
		if p.ID == arg.ID {
			return p.Name, nil
		}
	}
	return "", pgx.ErrNoRows
}

func (f *fakeDB) DeletePasskey(_ context.Context, arg store.DeletePasskeyParams) (int64, error) {
	keys := f.passkeys[arg.AccountID]
	for i, p := range keys {
		if p.ID == arg.ID {
			f.passkeys[arg.AccountID] = append(keys[:i:i], keys[i+1:]...)
			return 1, nil
		}
	}
	return 0, nil
}

func (f *fakeDB) FeatureFlagEnabled(_ context.Context, flag string) (bool, error) {
	return flag == "open_registration" && f.signups, nil
}

func (f *fakeDB) SetFeatureFlag(_ context.Context, arg store.SetFeatureFlagParams) error {
	if arg.Flag == "open_registration" {
		f.signups = arg.Enabled
	}
	return nil
}

func (f *fakeDB) InsertAuditEvent(_ context.Context, arg store.InsertAuditEventParams) error {
	f.audits = append(f.audits, arg)
	return nil
}

func setup() (*Service, *fakeDB, uuid.UUID) {
	id := uuid.New()
	verified := time.Now()
	db := &fakeDB{
		accounts: map[uuid.UUID]store.GetAccountForOperatorRow{id: {
			ID: id, Handle: "shelter_one", Email: "staff@shelter.example", Status: "active", EmailVerifiedAt: &verified,
		}},
		passkeys: map[uuid.UUID][]store.ListAccountPasskeysRow{id: {{ID: uuid.New(), Name: "Phone"}}},
		sessions: map[uuid.UUID]int64{id: 3},
		signups:  true,
	}
	return &Service{db: db}, db, id
}

func lastAudit(t *testing.T, db *fakeDB) (store.InsertAuditEventParams, map[string]any) {
	t.Helper()
	if len(db.audits) == 0 {
		t.Fatal("nothing recorded in the audit log")
	}
	e := db.audits[len(db.audits)-1]
	var d map[string]any
	if err := json.Unmarshal(e.Details, &d); err != nil {
		t.Fatal(err)
	}
	return e, d
}

func TestFindNormalizesTheQuery(t *testing.T) {
	svc, _, id := setup()
	for _, q := range []string{"@Shelter_One", " shelter_one ", "STAFF@shelter.example"} {
		rows, err := svc.Find(context.Background(), q)
		if err != nil || len(rows) != 1 || rows[0].ID != id {
			t.Errorf("Find(%q) = %v, %v", q, rows, err)
		}
	}
	if _, err := svc.Find(context.Background(), " @ "); err == nil {
		t.Error("an empty query was accepted")
	}
}

func TestSuspendSignsOutEverywhere(t *testing.T) {
	svc, db, id := setup()
	revoked, err := svc.Suspend(context.Background(), id, " suspected takeover ", "Sam")
	if err != nil || revoked != 3 {
		t.Fatalf("revoked %d, %v", revoked, err)
	}
	if db.accounts[id].Status != "suspended" || db.sessions[id] != 0 {
		t.Errorf("status %q, sessions %d", db.accounts[id].Status, db.sessions[id])
	}
	e, d := lastAudit(t, db)
	if e.Action != "account.suspended" || e.TargetID != id || d["operator"] != "Sam" || d["reason"] != "suspected takeover" {
		t.Errorf("audit = %s %v", e.Action, d)
	}
	if _, err := svc.Suspend(context.Background(), id, "again", "Sam"); !errors.Is(err, ErrWrongStatus) {
		t.Errorf("suspending twice: %v", err)
	}
}

func TestReinstateLiftsASuspension(t *testing.T) {
	svc, db, id := setup()
	ctx := context.Background()
	if err := svc.Reinstate(ctx, id, "owner confirmed", "Sam"); !errors.Is(err, ErrWrongStatus) {
		t.Errorf("reinstating an active account: %v", err)
	}
	if _, err := svc.Suspend(ctx, id, "suspected takeover", "Sam"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Reinstate(ctx, id, "owner confirmed", "Sam"); err != nil {
		t.Fatal(err)
	}
	if db.accounts[id].Status != "active" {
		t.Errorf("status = %q", db.accounts[id].Status)
	}
	if e, _ := lastAudit(t, db); e.Action != "account.reinstated" {
		t.Errorf("audit = %s", e.Action)
	}
}

func TestReinstateRefusesWhileAnAbuseCaseHoldsTheAccount(t *testing.T) {
	svc, db, id := setup()
	a := db.accounts[id]
	a.Status, a.HeldByCases = "suspended", 1
	db.accounts[id] = a
	if err := svc.Reinstate(context.Background(), id, "looks fine", "Sam"); !errors.Is(err, ErrHeldByCase) {
		t.Fatalf("err = %v, want ErrHeldByCase", err)
	}
	if db.accounts[id].Status != "suspended" || len(db.audits) != 0 {
		t.Error("the account was reinstated")
	}
}

func TestRemovePasskey(t *testing.T) {
	svc, db, id := setup()
	ctx := context.Background()
	stranger := store.ListAccountPasskeysRow{ID: uuid.New(), Name: "Unknown laptop"}
	db.passkeys[id] = append(db.passkeys[id], stranger)

	name, err := svc.RemovePasskey(ctx, id, stranger.ID, "added during takeover", "Sam")
	if err != nil || name != "Unknown laptop" || len(db.passkeys[id]) != 1 {
		t.Fatalf("removed %q, %v; %d left", name, err, len(db.passkeys[id]))
	}
	e, d := lastAudit(t, db)
	if e.Action != "passkey.removed" || d["passkey_name"] != "Unknown laptop" || d["passkey_id"] != stranger.ID.String() {
		t.Errorf("audit = %s %v", e.Action, d)
	}
	if _, err := svc.RemovePasskey(ctx, id, uuid.New(), "x", "Sam"); err == nil {
		t.Error("removed a passkey the account doesn't have")
	}
}

func TestRemovePasskeyKeepsAWayToSignIn(t *testing.T) {
	svc, db, id := setup()
	a := db.accounts[id]
	a.EmailVerifiedAt = nil
	db.accounts[id] = a
	only := db.passkeys[id][0].ID
	if _, err := svc.RemovePasskey(context.Background(), id, only, "x", "Sam"); !errors.Is(err, ErrLastPasskey) {
		t.Fatalf("err = %v, want ErrLastPasskey", err)
	}
	if len(db.passkeys[id]) != 1 {
		t.Error("the only passkey was removed")
	}
}

func TestSetSignups(t *testing.T) {
	svc, db, _ := setup()
	ctx := context.Background()
	if err := svc.SetSignups(ctx, false, "scam wave", "Sam"); err != nil {
		t.Fatal(err)
	}
	if open, _ := svc.SignupsOpen(ctx); open {
		t.Error("sign-ups still open")
	}
	e, d := lastAudit(t, db)
	if e.Action != "instance.signups_closed" || e.TargetType != "instance" || d["reason"] != "scam wave" {
		t.Errorf("audit = %s %s %v", e.Action, e.TargetType, d)
	}
	if err := svc.SetSignups(ctx, true, "wave over", "Sam"); err != nil {
		t.Fatal(err)
	}
	if e, _ := lastAudit(t, db); e.Action != "instance.signups_opened" {
		t.Errorf("audit = %s", e.Action)
	}
}

func TestChangesNeedAReasonAndAnOperator(t *testing.T) {
	svc, db, id := setup()
	ctx := context.Background()
	if _, err := svc.Suspend(ctx, id, "takeover", ""); err == nil {
		t.Error("suspend without an operator")
	}
	if _, err := svc.Suspend(ctx, id, " ", "Sam"); err == nil {
		t.Error("suspend without a reason")
	}
	if err := svc.SetSignups(ctx, false, "", "Sam"); err == nil {
		t.Error("closing sign-ups without a reason")
	}
	if db.accounts[id].Status != "active" || len(db.audits) != 0 {
		t.Error("something changed")
	}
}

func TestUnknownAccount(t *testing.T) {
	svc, _, _ := setup()
	ctx := context.Background()
	if _, err := svc.Get(ctx, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get: %v", err)
	}
	if _, err := svc.Suspend(ctx, uuid.New(), "x", "Sam"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Suspend: %v", err)
	}
	if err := svc.Reinstate(ctx, uuid.New(), "x", "Sam"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Reinstate: %v", err)
	}
}
