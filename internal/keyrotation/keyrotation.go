// SPDX-License-Identifier: AGPL-3.0-or-later

// Package keyrotation moves data encrypted under a key being retired onto the current
// key (ADR 0025): actors' private keys and abuse imagery evidence under PC_SECRET_KEY,
// and authenticator secrets under the MFA key. It only touches the kinds of data whose
// keys the process holds, so it runs in the worker for one and the API's container for
// the other.
package keyrotation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/google/uuid"

	"github.com/chmbrb/PetCommunityProject/internal/actorkeys"
	"github.com/chmbrb/PetCommunityProject/internal/auth"
	"github.com/chmbrb/PetCommunityProject/internal/media"
	"github.com/chmbrb/PetCommunityProject/internal/storage"
	"github.com/chmbrb/PetCommunityProject/internal/store"
)

// batch is how many rows each round re-encrypts.
const batch = 100

// maxEvidence bounds what's read from the evidence bucket (uploads are at most 15 MB).
const maxEvidence = 32 << 20

// Queries are the database calls rotation makes. *store.Queries implements them.
type Queries interface {
	CountActorKeysByKeyID(ctx context.Context) ([]store.CountActorKeysByKeyIDRow, error)
	ListActorKeysUnderKey(ctx context.Context, arg store.ListActorKeysUnderKeyParams) ([]store.ListActorKeysUnderKeyRow, error)
	ReplaceActorPrivateKey(ctx context.Context, arg store.ReplaceActorPrivateKeyParams) (int64, error)
	CountTOTPSecretsByKeyID(ctx context.Context) ([]store.CountTOTPSecretsByKeyIDRow, error)
	ListTOTPSecretsUnderKey(ctx context.Context, arg store.ListTOTPSecretsUnderKeyParams) ([]store.ListTOTPSecretsUnderKeyRow, error)
	ReplaceTOTPSecret(ctx context.Context, arg store.ReplaceTOTPSecretParams) (int64, error)
	ListAbuseImageryEvidence(ctx context.Context) ([]store.ListAbuseImageryEvidenceRow, error)
	AbuseImageryCaseExists(ctx context.Context, id uuid.UUID) (bool, error)
}

// Keys are the keyrings the process holds; nil ones are skipped.
type Keys struct {
	ActorKeys *actorkeys.Sealer
	Evidence  *media.EvidenceSealer
	// EvidenceBucket is where evidence files are; needed with Evidence.
	EvidenceBucket storage.Bucket
	MFA            *auth.MFAKeyring
}

// Rotator reports on and re-encrypts data.
type Rotator struct {
	db   Queries
	keys Keys
}

// New returns a rotator for the keys the process holds.
func New(db Queries, keys Keys) *Rotator { return &Rotator{db: db, keys: keys} }

// Report says, for one kind of data, how much is under each key.
type Report struct {
	Kind string
	// Current is up to date. Previous is under the key being retired, or, for
	// evidence, in the old format: `reencrypt` moves it. Unreadable is under neither
	// key, which means a key is missing from the configuration.
	Current, Previous, Unreadable int64
	// Skipped says why this kind wasn't looked at, e.g. the process lacks its key.
	Skipped string
}

// Done reports whether nothing needs re-encrypting or is unreadable.
func (r Report) Done() bool { return r.Skipped == "" && r.Previous == 0 && r.Unreadable == 0 }

func (r Report) String() string {
	if r.Skipped != "" {
		return fmt.Sprintf("%s: skipped (%s)", r.Kind, r.Skipped)
	}
	return fmt.Sprintf("%s: %d current, %d to re-encrypt, %d unreadable", r.Kind, r.Current, r.Previous, r.Unreadable)
}

const (
	kindActorKeys = "actor signing keys"
	kindTOTP      = "authenticator secrets"
	kindEvidence  = "abuse imagery evidence"
)

// Status counts what's under each key, for every kind of data.
func (r *Rotator) Status(ctx context.Context) ([]Report, error) {
	actor, err := r.actorKeyStatus(ctx)
	if err != nil {
		return nil, err
	}
	totp, err := r.totpStatus(ctx)
	if err != nil {
		return nil, err
	}
	evidence, err := r.evidenceStatus(ctx)
	if err != nil {
		return nil, err
	}
	return []Report{actor, evidence, totp}, nil
}

// Reencrypt moves everything under the previous key onto the current one, then
// returns the status. It's safe to stop and run again.
func (r *Rotator) Reencrypt(ctx context.Context) ([]Report, error) {
	if err := r.reencryptActorKeys(ctx); err != nil {
		return nil, fmt.Errorf("%s: %w", kindActorKeys, err)
	}
	if err := r.reencryptEvidence(ctx); err != nil {
		return nil, fmt.Errorf("%s: %w", kindEvidence, err)
	}
	if err := r.reencryptTOTP(ctx); err != nil {
		return nil, fmt.Errorf("%s: %w", kindTOTP, err)
	}
	return r.Status(ctx)
}

// tally sorts counts by key ID into a report.
func tally(kind, current, previous string, counts map[string]int64) Report {
	rep := Report{Kind: kind}
	for id, n := range counts {
		switch {
		case id == current:
			rep.Current += n
		case previous != "" && id == previous:
			rep.Previous += n
		default:
			rep.Unreadable += n
		}
	}
	return rep
}

func (r *Rotator) actorKeyStatus(ctx context.Context) (Report, error) {
	if r.keys.ActorKeys == nil {
		return Report{Kind: kindActorKeys, Skipped: "this process doesn't have PC_SECRET_KEY; run it in the worker"}, nil
	}
	rows, err := r.db.CountActorKeysByKeyID(ctx)
	if err != nil {
		return Report{}, err
	}
	counts := map[string]int64{}
	for _, row := range rows {
		if row.KeyID != nil {
			counts[*row.KeyID] += row.N
		}
	}
	return tally(kindActorKeys, r.keys.ActorKeys.KeyID(), r.keys.ActorKeys.PreviousKeyID(), counts), nil
}

func (r *Rotator) totpStatus(ctx context.Context) (Report, error) {
	if r.keys.MFA == nil {
		return Report{Kind: kindTOTP, Skipped: "this process doesn't have the MFA key; run it in the app container"}, nil
	}
	rows, err := r.db.CountTOTPSecretsByKeyID(ctx)
	if err != nil {
		return Report{}, err
	}
	counts := map[string]int64{}
	for _, row := range rows {
		counts[row.KeyID] += row.N
	}
	return tally(kindTOTP, r.keys.MFA.KeyID(), r.keys.MFA.PreviousKeyID(), counts), nil
}

func (r *Rotator) reencryptActorKeys(ctx context.Context) error {
	s := r.keys.ActorKeys
	if s == nil || s.PreviousKeyID() == "" {
		return nil
	}
	for {
		rows, err := r.db.ListActorKeysUnderKey(ctx, store.ListActorKeysUnderKeyParams{KeyID: s.PreviousKeyID(), Batch: batch})
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		for _, row := range rows {
			key, err := s.Open(row.ID, s.PreviousKeyID(), row.PrivateKeyEnc)
			if err != nil {
				return fmt.Errorf("actor %s: %w", row.ID, err)
			}
			sealed, err := s.Seal(row.ID, key)
			if err != nil {
				return err
			}
			// Zero rows means the key changed meanwhile; it's under the current key now.
			if _, err := r.db.ReplaceActorPrivateKey(ctx, store.ReplaceActorPrivateKeyParams{
				ID: row.ID, Sealed: sealed, NewKeyID: s.KeyID(), OldKeyID: s.PreviousKeyID(),
			}); err != nil {
				return err
			}
		}
	}
}

func (r *Rotator) reencryptTOTP(ctx context.Context) error {
	m := r.keys.MFA
	if m == nil || m.PreviousKeyID() == "" {
		return nil
	}
	for {
		rows, err := r.db.ListTOTPSecretsUnderKey(ctx, store.ListTOTPSecretsUnderKeyParams{KeyID: m.PreviousKeyID(), Batch: batch})
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		for _, row := range rows {
			secret, err := m.Open(row.AccountID, m.PreviousKeyID(), row.SecretEncrypted)
			if err != nil {
				return fmt.Errorf("account %s: %w", row.AccountID, err)
			}
			sealed, err := m.Seal(row.AccountID, secret)
			if err != nil {
				return err
			}
			// Zero rows means the person set up a new authenticator meanwhile.
			if _, err := r.db.ReplaceTOTPSecret(ctx, store.ReplaceTOTPSecretParams{
				AccountID: row.AccountID, Sealed: sealed, NewKeyID: m.KeyID(), OldKeyID: m.PreviousKeyID(),
			}); err != nil {
				return err
			}
		}
	}
}

// Evidence files carry their own key ID, so each one is read to see where it stands.

func (r *Rotator) evidenceStatus(ctx context.Context) (Report, error) {
	rep := Report{Kind: kindEvidence}
	if r.keys.Evidence == nil || r.keys.EvidenceBucket == nil {
		rep.Skipped = "this process doesn't have PC_SECRET_KEY and the evidence bucket; run it in the worker"
		return rep, nil
	}
	cases, err := r.db.ListAbuseImageryEvidence(ctx)
	if err != nil {
		return Report{}, err
	}
	for _, c := range cases {
		sealed, err := r.readEvidence(ctx, c.EvidenceKey)
		if err != nil {
			return Report{}, fmt.Errorf("case %s: %w", c.ID, err)
		}
		current, err := r.keys.Evidence.UnderCurrentKey(c.MediaID, sealed)
		switch {
		case errors.Is(err, media.ErrEvidenceCorrupt):
			rep.Unreadable++
		case err != nil:
			return Report{}, err
		case current:
			rep.Current++
		default:
			rep.Previous++
		}
	}
	return rep, nil
}

func (r *Rotator) reencryptEvidence(ctx context.Context) error {
	s := r.keys.Evidence
	if s == nil || r.keys.EvidenceBucket == nil {
		return nil
	}
	cases, err := r.db.ListAbuseImageryEvidence(ctx)
	if err != nil {
		return err
	}
	for _, c := range cases {
		sealed, err := r.readEvidence(ctx, c.EvidenceKey)
		if err != nil {
			return fmt.Errorf("case %s: %w", c.ID, err)
		}
		current, err := s.UnderCurrentKey(c.MediaID, sealed)
		switch {
		case errors.Is(err, media.ErrEvidenceCorrupt):
			// Under neither key: left alone, like other unreadable data, and counted
			// as unreadable by the status.
			continue
		case err != nil:
			return fmt.Errorf("case %s: %w", c.ID, err)
		case current:
			continue
		}
		original, err := s.Open(c.MediaID, sealed)
		if err != nil {
			return fmt.Errorf("case %s: %w", c.ID, err)
		}
		resealed, err := s.Seal(c.MediaID, original)
		if err != nil {
			return err
		}
		if err := r.keys.EvidenceBucket.Put(ctx, c.EvidenceKey, bytes.NewReader(resealed), int64(len(resealed)),
			storage.PutOptions{ContentType: "application/octet-stream"}); err != nil {
			return fmt.Errorf("case %s: %w", c.ID, err)
		}
		// If the case was purged meanwhile, don't leave its evidence behind.
		if exists, err := r.db.AbuseImageryCaseExists(ctx, c.ID); err != nil {
			return err
		} else if !exists {
			if err := r.keys.EvidenceBucket.Delete(ctx, c.EvidenceKey); err != nil {
				return fmt.Errorf("case %s was purged; delete its re-encrypted evidence: %w", c.ID, err)
			}
		}
	}
	return nil
}

func (r *Rotator) readEvidence(ctx context.Context, key string) ([]byte, error) {
	rc, err := r.keys.EvidenceBucket.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("read evidence: %w", err)
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, maxEvidence))
}
