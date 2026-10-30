// SPDX-License-Identifier: AGPL-3.0-or-later

package jobs

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/chmbrb/PetCommunityProject/internal/actorkeys"
	"github.com/chmbrb/PetCommunityProject/internal/database"
	"github.com/chmbrb/PetCommunityProject/internal/store"
)

// backfillBatch caps how many missing key pairs one backfill run creates.
const backfillBatch = 200

// KeyMaker creates and stores actors' key pairs.
type KeyMaker struct {
	db     *database.Store
	sealer *actorkeys.Sealer
	logger *slog.Logger
}

// NewKeyMaker returns a KeyMaker that encrypts private keys with sealer.
func NewKeyMaker(db *database.Store, sealer *actorkeys.Sealer, logger *slog.Logger) *KeyMaker {
	return &KeyMaker{db: db, sealer: sealer, logger: logger}
}

// Ensure gives the actor a key pair if it doesn't have one. It's safe to repeat.
func (k *KeyMaker) Ensure(ctx context.Context, actorID uuid.UUID) error {
	kp, err := actorkeys.Generate()
	if err != nil {
		return err
	}
	sealed, err := k.sealer.Seal(actorID, kp.PrivateKey)
	if err != nil {
		return err
	}
	kid := k.sealer.KeyID()
	n, err := k.db.SetActorKeys(ctx, store.SetActorKeysParams{
		ID: actorID, PublicKeyPem: &kp.PublicKeyPEM, PrivateKeyEnc: sealed, PrivateKeyKid: &kid,
	})
	if err != nil {
		return fmt.Errorf("store actor keys: %w", err)
	}
	if n == 1 {
		k.logger.InfoContext(ctx, "actor keys created", "actor_id", actorID)
	}
	return nil
}

// Backfill creates key pairs for actors that don't have one, up to one batch, and
// reports how many it created.
func (k *KeyMaker) Backfill(ctx context.Context) (int, error) {
	ids, err := k.db.ListActorsWithoutKeys(ctx, backfillBatch)
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		if err := k.Ensure(ctx, id); err != nil {
			return 0, err
		}
	}
	return len(ids), nil
}

// ActorKeysWorker runs actorkeys.Job.
type ActorKeysWorker struct {
	river.WorkerDefaults[actorkeys.Job]
	keys *KeyMaker
}

// Work creates one actor's key pair.
func (w *ActorKeysWorker) Work(ctx context.Context, job *river.Job[actorkeys.Job]) error {
	return w.keys.Ensure(ctx, job.Args.ActorID)
}

// BackfillActorKeysJob creates key pairs for any actor missing one, e.g. actors
// created before keys existed, or whose key job failed.
type BackfillActorKeysJob struct{}

// Kind identifies the job type to River.
func (BackfillActorKeysJob) Kind() string { return "backfill_actor_keys" }

// BackfillActorKeysWorker runs BackfillActorKeysJob.
type BackfillActorKeysWorker struct {
	river.WorkerDefaults[BackfillActorKeysJob]
	keys   *KeyMaker
	logger *slog.Logger
}

// Work runs one backfill batch.
func (w *BackfillActorKeysWorker) Work(ctx context.Context, _ *river.Job[BackfillActorKeysJob]) error {
	n, err := w.keys.Backfill(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		w.logger.InfoContext(ctx, "backfilled actor keys", "count", n)
	}
	return nil
}
