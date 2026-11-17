// SPDX-License-Identifier: AGPL-3.0-or-later

package jobs

import (
	"context"
	"time"

	"github.com/riverqueue/river"

	"github.com/chmbrb/PetCommunityProject/internal/mail"
	"github.com/chmbrb/PetCommunityProject/internal/media"
)

// AbuseAlertWorker emails the operator about each new abuse imagery case (ADR 0022).
type AbuseAlertWorker struct {
	river.WorkerDefaults[media.AbuseAlertJob]
	sender       mail.Sender
	to           string
	instanceName string
}

// NewAbuseAlertWorker sends alerts to the address in PC_ABUSE_ALERT_EMAIL.
func NewAbuseAlertWorker(sender mail.Sender, to, instanceName string) *AbuseAlertWorker {
	return &AbuseAlertWorker{sender: sender, to: to, instanceName: instanceName}
}

// Work sends one alert. A failure is retried: the case is already open, so the only
// thing at stake is the operator finding out.
func (w *AbuseAlertWorker) Work(ctx context.Context, job *river.Job[media.AbuseAlertJob]) error {
	msg, err := mail.AbuseAlert(w.to, mail.AbuseAlertEmail{
		InstanceName: w.instanceName,
		CaseID:       job.Args.CaseID.String(),
		OpenedAt:     job.Args.OpenedAt.UTC().Format(time.RFC3339),
	})
	if err != nil {
		return err
	}
	return w.sender.Send(ctx, msg)
}
