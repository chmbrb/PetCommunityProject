// SPDX-License-Identifier: AGPL-3.0-or-later

package keyrotation

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/google/uuid"

	"github.com/chmbrb/PetCommunityProject/internal/actorkeys"
	"github.com/chmbrb/PetCommunityProject/internal/auth"
	"github.com/chmbrb/PetCommunityProject/internal/media"
	"github.com/chmbrb/PetCommunityProject/internal/storage"
	"github.com/chmbrb/PetCommunityProject/internal/store"
)

type sealedRow struct {
	sealed []byte
	keyID  string
}

// fakeDB keeps the rotated tables in memory, with the same conditional updates.
type fakeDB struct {
	actors map[uuid.UUID]sealedRow
	totp   map[uuid.UUID]sealedRow
	cases  []store.ListAbuseImageryEvidenceRow
	// purgeOnCheck cases are purged by the worker while re-encryption runs: just
	// before the existence check.
	purgeOnCheck map[uuid.UUID]bool
	purged       map[uuid.UUID]bool
}

func (f *fakeDB) CountActorKeysByKeyID(context.Context) ([]store.CountActorKeysByKeyIDRow, error) {
	counts := map[string]int64{}
	for _, r := range f.actors {
		counts[r.keyID]++
	}
	var out []store.CountActorKeysByKeyIDRow
	for id, n := range counts {
		out = append(out, store.CountActorKeysByKeyIDRow{KeyID: &id, N: n})
	}
	return out, nil
}

func (f *fakeDB) ListActorKeysUnderKey(_ context.Context, arg store.ListActorKeysUnderKeyParams) ([]store.ListActorKeysUnderKeyRow, error) {
	var out []store.ListActorKeysUnderKeyRow
	for id, r := range f.actors {
		if r.keyID == arg.KeyID && len(out) < int(arg.Batch) {
			out = append(out, store.ListActorKeysUnderKeyRow{ID: id, PrivateKeyEnc: r.sealed})
		}
	}
	return out, nil
}

func (f *fakeDB) ReplaceActorPrivateKey(_ context.Context, arg store.ReplaceActorPrivateKeyParams) (int64, error) {
	if r, ok := f.actors[arg.ID]; !ok || r.keyID != arg.OldKeyID {
		return 0, nil
	}
	f.actors[arg.ID] = sealedRow{arg.Sealed, arg.NewKeyID}
	return 1, nil
}

func (f *fakeDB) CountTOTPSecretsByKeyID(context.Context) ([]store.CountTOTPSecretsByKeyIDRow, error) {
	counts := map[string]int64{}
	for _, r := range f.totp {
		counts[r.keyID]++
	}
	var out []store.CountTOTPSecretsByKeyIDRow
	for id, n := range counts {
		out = append(out, store.CountTOTPSecretsByKeyIDRow{KeyID: id, N: n})
	}
	return out, nil
}

func (f *fakeDB) ListTOTPSecretsUnderKey(_ context.Context, arg store.ListTOTPSecretsUnderKeyParams) ([]store.ListTOTPSecretsUnderKeyRow, error) {
	var out []store.ListTOTPSecretsUnderKeyRow
	for id, r := range f.totp {
		if r.keyID == arg.KeyID && len(out) < int(arg.Batch) {
			out = append(out, store.ListTOTPSecretsUnderKeyRow{AccountID: id, SecretEncrypted: r.sealed})
		}
	}
	return out, nil
}

func (f *fakeDB) ReplaceTOTPSecret(_ context.Context, arg store.ReplaceTOTPSecretParams) (int64, error) {
	if r, ok := f.totp[arg.AccountID]; !ok || r.keyID != arg.OldKeyID {
		return 0, nil
	}
	f.totp[arg.AccountID] = sealedRow{arg.Sealed, arg.NewKeyID}
	return 1, nil
}

func (f *fakeDB) ListAbuseImageryEvidence(context.Context) ([]store.ListAbuseImageryEvidenceRow, error) {
	var out []store.ListAbuseImageryEvidenceRow
	for _, c := range f.cases {
		if !f.purged[c.ID] {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeDB) AbuseImageryCaseExists(_ context.Context, id uuid.UUID) (bool, error) {
	if f.purgeOnCheck[id] {
		f.purged[id] = true
	}
	return !f.purged[id], nil
}

func key(b byte) []byte { return bytes.Repeat([]byte{b}, 32) }

// must unwraps a constructor's result; a setup failure panics, which fails the test.
func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// world is an instance's encrypted data, sealed under the "old" keys.
type world struct {
	db       *fakeDB
	bucket   *storage.MemoryBucket
	actorKey *actorkeys.KeyPair
	secret   []byte // a TOTP secret
	original []byte // an evidence file
}

func newWorld(t *testing.T, n int) *world {
	t.Helper()
	ctx := context.Background()
	oldActor := must(actorkeys.NewSealer(key(1), nil))
	oldMFA := must(auth.NewMFAKeyring(key(2), nil))
	oldEvidence := must(media.NewEvidenceSealer(key(1), nil))
	kp := must(actorkeys.Generate())
	w := &world{
		db: &fakeDB{
			actors: map[uuid.UUID]sealedRow{}, totp: map[uuid.UUID]sealedRow{},
			purgeOnCheck: map[uuid.UUID]bool{}, purged: map[uuid.UUID]bool{},
		},
		bucket:   &storage.MemoryBucket{},
		actorKey: &kp,
		secret:   []byte("authenticator secret"),
		original: []byte("evidence file"),
	}
	for range n {
		id := uuid.New()
		w.db.actors[id] = sealedRow{must(oldActor.Seal(id, kp.PrivateKey)), oldActor.KeyID()}
		w.db.totp[id] = sealedRow{must(oldMFA.Seal(id, w.secret)), oldMFA.KeyID()}
		c := store.ListAbuseImageryEvidenceRow{ID: uuid.New(), MediaID: uuid.New(), EvidenceKey: "media/" + id.String()}
		sealed := must(oldEvidence.Seal(c.MediaID, w.original))
		if err := w.bucket.Put(ctx, c.EvidenceKey, bytes.NewReader(sealed), int64(len(sealed)), storage.PutOptions{}); err != nil {
			t.Fatal(err)
		}
		w.db.cases = append(w.db.cases, c)
	}
	return w
}

// rotating returns keyrings after a rotation: new keys current, old ones previous.
func (w *world) rotating() Keys {
	return Keys{
		ActorKeys:      must(actorkeys.NewSealer(key(3), key(1))),
		Evidence:       must(media.NewEvidenceSealer(key(3), key(1))),
		EvidenceBucket: w.bucket,
		MFA:            must(auth.NewMFAKeyring(key(4), key(2))),
	}
}

func byKind(reports []Report) map[string]Report {
	m := map[string]Report{}
	for _, r := range reports {
		m[r.Kind] = r
	}
	return m
}

func TestRotationMovesEverythingToTheNewKeys(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t, 250) // more than two batches
	r := New(w.db, w.rotating())

	before := byKind(must(r.Status(ctx)))
	for _, kind := range []string{kindActorKeys, kindTOTP, kindEvidence} {
		if got := before[kind]; got.Previous != 250 || got.Current != 0 || got.Unreadable != 0 {
			t.Errorf("before: %s", got)
		}
	}

	after := must(r.Reencrypt(ctx))
	for _, rep := range after {
		if !rep.Done() || rep.Current != 250 {
			t.Errorf("after: %s", rep)
		}
	}

	// Everything opens with only the new keys: the old ones can be retired.
	newOnly := Keys{
		ActorKeys: must(actorkeys.NewSealer(key(3), nil)),
		Evidence:  must(media.NewEvidenceSealer(key(3), nil)),
		MFA:       must(auth.NewMFAKeyring(key(4), nil)),
	}
	for id, row := range w.db.actors {
		if got, err := newOnly.ActorKeys.Open(id, row.keyID, row.sealed); err != nil || !got.Equal(w.actorKey.PrivateKey) {
			t.Fatalf("actor key after rotation: %v", err)
		}
	}
	for id, row := range w.db.totp {
		if got, err := newOnly.MFA.Open(id, row.keyID, row.sealed); err != nil || !bytes.Equal(got, w.secret) {
			t.Fatalf("TOTP secret after rotation: %v", err)
		}
	}
	for _, c := range w.db.cases {
		rc := must(w.bucket.Get(ctx, c.EvidenceKey))
		sealed := must(io.ReadAll(rc))
		if got, err := newOnly.Evidence.Open(c.MediaID, sealed); err != nil || !bytes.Equal(got, w.original) {
			t.Fatalf("evidence after rotation: %v", err)
		}
	}

	// Running it again changes nothing.
	if again := must(r.Reencrypt(ctx)); !again[0].Done() || again[0].Current != 250 {
		t.Errorf("second run: %s", again[0])
	}
}

func TestStatusFindsDataNoConfiguredKeyCanRead(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t, 3)
	// A rotation where the old key wasn't kept as the previous key.
	r := New(w.db, Keys{
		ActorKeys:      must(actorkeys.NewSealer(key(3), nil)),
		Evidence:       must(media.NewEvidenceSealer(key(3), nil)),
		EvidenceBucket: w.bucket,
		MFA:            must(auth.NewMFAKeyring(key(4), nil)),
	})
	for _, rep := range must(r.Status(ctx)) {
		if rep.Unreadable != 3 || rep.Done() {
			t.Errorf("%s; want 3 unreadable", rep)
		}
	}
	// With no previous key, there's nothing to re-encrypt with, so nothing changes.
	for _, rep := range must(r.Reencrypt(ctx)) {
		if rep.Unreadable != 3 {
			t.Errorf("after reencrypt: %s", rep)
		}
	}
}

func TestKindsWithoutTheirKeyAreSkipped(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t, 2)
	// The API's container: only the MFA key.
	r := New(w.db, Keys{MFA: must(auth.NewMFAKeyring(key(4), key(2)))})
	reports := byKind(must(r.Reencrypt(ctx)))
	if reports[kindActorKeys].Skipped == "" || reports[kindEvidence].Skipped == "" {
		t.Errorf("kinds without their key weren't skipped: %v", reports)
	}
	if !reports[kindTOTP].Done() {
		t.Errorf("TOTP: %s", reports[kindTOTP])
	}
	for _, row := range w.db.actors {
		if row.keyID != must(actorkeys.NewSealer(key(1), nil)).KeyID() {
			t.Fatal("actor keys changed without PC_SECRET_KEY")
		}
	}
}

func TestEvidenceOfAPurgedCaseIsNotLeftBehind(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t, 1)
	c := w.db.cases[0]
	w.db.purgeOnCheck[c.ID] = true
	if _, err := New(w.db, w.rotating()).Reencrypt(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok := w.bucket.Objects()[c.EvidenceKey]; ok {
		t.Error("re-encrypted evidence of a purged case was left in the bucket")
	}
}
