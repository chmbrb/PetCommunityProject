// SPDX-License-Identifier: AGPL-3.0-or-later

package jobs

import (
	"context"
	"log/slog"
	"time"

	"github.com/riverqueue/river"

	"github.com/chmbrb/PetCommunityProject/internal/media"
)

// MediaWorker runs media.ProcessJob.
type MediaWorker struct {
	river.WorkerDefaults[media.ProcessJob]
	media *media.Service
}

// Work processes one upload.
func (w *MediaWorker) Work(ctx context.Context, job *river.Job[media.ProcessJob]) error {
	return w.media.Process(ctx, job.Args.MediaID)
}

// Timeout bounds one processing attempt; large photos take a few seconds.
func (w *MediaWorker) Timeout(*river.Job[media.ProcessJob]) time.Duration {
	return 2 * time.Minute
}

// MediaCleanupJob rejects uploads whose processing never finished.
type MediaCleanupJob struct{}

// Kind identifies the job type to River.
func (MediaCleanupJob) Kind() string { return "media_cleanup" }

// MediaCleanupWorker runs MediaCleanupJob.
type MediaCleanupWorker struct {
	river.WorkerDefaults[MediaCleanupJob]
	media  *media.Service
	logger *slog.Logger
}

// Work rejects stale uploads.
func (w *MediaCleanupWorker) Work(ctx context.Context, _ *river.Job[MediaCleanupJob]) error {
	n, err := w.media.CleanupStale(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		w.logger.InfoContext(ctx, "rejected stale uploads", "count", n)
	}
	return nil
}
