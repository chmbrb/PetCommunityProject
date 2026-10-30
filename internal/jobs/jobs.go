// SPDX-License-Identifier: AGPL-3.0-or-later

// Package jobs sets up the River job queue (ADR 0006): the clients used by the app
// and the worker, and maintenance jobs.
package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/chmbrb/PetCommunityProject/internal/auth"
	"github.com/chmbrb/PetCommunityProject/internal/database"
)

// NewInserter returns a client that only inserts jobs, for the API server.
func NewInserter(pool *pgxpool.Pool, logger *slog.Logger) (*river.Client[pgx.Tx], error) {
	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{Logger: logger})
	if err != nil {
		return nil, fmt.Errorf("create job queue client: %w", err)
	}
	return client, nil
}

// NewWorker returns a client that runs every job type, for the worker process.
func NewWorker(pool *pgxpool.Pool, db *database.Store, emailer *auth.Emailer, keys *KeyMaker, logger *slog.Logger) (*river.Client[pgx.Tx], error) {
	workers := river.NewWorkers()
	river.AddWorker(workers, auth.NewEmailWorker(emailer))
	river.AddWorker(workers, &CleanupWorker{db: db, logger: logger})
	river.AddWorker(workers, &ActorKeysWorker{keys: keys})
	river.AddWorker(workers, &BackfillActorKeysWorker{keys: keys, logger: logger})

	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Logger:  logger,
		Workers: workers,
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: 2},
			auth.QueueEmail:    {MaxWorkers: 4},
		},
		PeriodicJobs: []*river.PeriodicJob{
			river.NewPeriodicJob(river.PeriodicInterval(time.Hour),
				func() (river.JobArgs, *river.InsertOpts) { return CleanupJob{}, nil },
				&river.PeriodicJobOpts{RunOnStart: true}),
			river.NewPeriodicJob(river.PeriodicInterval(time.Hour),
				func() (river.JobArgs, *river.InsertOpts) { return BackfillActorKeysJob{}, nil },
				&river.PeriodicJobOpts{RunOnStart: true}),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create job queue worker: %w", err)
	}
	return client, nil
}

// CleanupJob removes expired passkey challenges and email tokens.
type CleanupJob struct{}

// Kind identifies the job type to River.
func (CleanupJob) Kind() string { return "cleanup" }

// CleanupWorker runs CleanupJob.
type CleanupWorker struct {
	river.WorkerDefaults[CleanupJob]
	db     *database.Store
	logger *slog.Logger
}

// Work deletes expired rows.
func (w *CleanupWorker) Work(ctx context.Context, _ *river.Job[CleanupJob]) error {
	challenges, err := w.db.DeleteExpiredWebAuthnChallenges(ctx)
	if err != nil {
		return err
	}
	tokens, err := w.db.DeleteExpiredEmailTokens(ctx)
	if err != nil {
		return err
	}
	w.logger.InfoContext(ctx, "cleanup finished", "passkey_challenges", challenges, "email_tokens", tokens)
	return nil
}
