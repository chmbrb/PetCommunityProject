// SPDX-License-Identifier: AGPL-3.0-or-later

package jobs

import (
	"context"
	"log/slog"

	"github.com/riverqueue/river"

	"github.com/chmbrb/PetCommunityProject/internal/abusecases"
)

// AbuseCasePurgeJob deletes the evidence and the case for abuse imagery cases that
// were closed long enough ago and are past their preservation date (ADR 0022).
type AbuseCasePurgeJob struct{}

// Kind identifies the job type to River.
func (AbuseCasePurgeJob) Kind() string { return "abuse_case_purge" }

// AbuseCasePurgeWorker runs AbuseCasePurgeJob.
type AbuseCasePurgeWorker struct {
	river.WorkerDefaults[AbuseCasePurgeJob]
	cases  *abusecases.Service
	logger *slog.Logger
}

// Work purges what's due. A failure is retried; whatever it already purged stays purged.
func (w *AbuseCasePurgeWorker) Work(ctx context.Context, _ *river.Job[AbuseCasePurgeJob]) error {
	n, err := w.cases.Purge(ctx)
	if n > 0 {
		w.logger.InfoContext(ctx, "purged closed abuse imagery cases past their preservation date", "count", n)
	}
	return err
}
