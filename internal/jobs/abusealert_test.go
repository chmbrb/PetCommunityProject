// SPDX-License-Identifier: AGPL-3.0-or-later

package jobs

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/chmbrb/PetCommunityProject/internal/mail"
	"github.com/chmbrb/PetCommunityProject/internal/media"
)

func TestAbuseAlertWorkerEmailsTheOperator(t *testing.T) {
	recorder := &mail.Recorder{}
	w := NewAbuseAlertWorker(recorder, "abuse@localhost", "Example Pets")
	caseID := uuid.New()
	job := &river.Job[media.AbuseAlertJob]{
		JobRow: &rivertype.JobRow{},
		Args:   media.AbuseAlertJob{CaseID: caseID, OpenedAt: time.Date(2026, 11, 17, 8, 0, 0, 0, time.UTC)},
	}
	if err := w.Work(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	msgs := recorder.Messages()
	if len(msgs) != 1 || msgs[0].To != "abuse@localhost" {
		t.Fatalf("messages = %+v", msgs)
	}
	if !strings.Contains(msgs[0].Text, caseID.String()) || !strings.Contains(msgs[0].Text, "2026-11-17T08:00:00Z") {
		t.Errorf("text = %s", msgs[0].Text)
	}
}
