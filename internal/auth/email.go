// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/chmbrb/PetCommunityProject/internal/config"
	"github.com/chmbrb/PetCommunityProject/internal/database"
	pcmail "github.com/chmbrb/PetCommunityProject/internal/mail"
	"github.com/chmbrb/PetCommunityProject/internal/store"
)

// Email purposes, matching email_tokens.purpose.
const (
	PurposeVerifyEmail = "verify_email"
	PurposeSignIn      = "sign_in"
)

// QueueEmail is the job queue that sends email.
const QueueEmail = "email"

const (
	verifyEmailTTL = 24 * time.Hour
	// Short, because a sign-in link is as good as a password while it lasts
	// (THREAT_MODEL.md S6).
	signInLinkTTL = 15 * time.Minute
)

var (
	ErrLinkInvalid   = errors.New("this link is invalid, has expired, or was already used")
	ErrTooManyEmails = errors.New("too many emails were sent to this address; please try again later")
)

// JobQueue inserts background jobs. *river.Client satisfies it.
type JobQueue interface {
	InsertTx(ctx context.Context, tx pgx.Tx, args river.JobArgs, opts *river.InsertOpts) (*rivertype.JobInsertResult, error)
}

// EmailJob asks the worker to send an account email. It deliberately holds no token:
// the worker creates the token when it sends, so tokens never sit in the job table.
type EmailJob struct {
	AccountID uuid.UUID `json:"account_id"`
	Purpose   string    `json:"purpose"`
}

// Kind identifies the job type to River.
func (EmailJob) Kind() string { return "email" }

// InsertOpts puts email jobs on their own queue. A sign-in link is only useful for a
// few minutes, so it's retried briefly; a verification email can wait longer.
func (j EmailJob) InsertOpts() river.InsertOpts {
	attempts := 10
	if j.Purpose == PurposeSignIn {
		attempts = 3
	}
	return river.InsertOpts{Queue: QueueEmail, MaxAttempts: attempts}
}

// RequestSignInLink emails a sign-in link if an active account uses the address. It
// reports success either way, so it doesn't reveal which addresses have accounts.
func (s *Service) RequestSignInLink(ctx context.Context, email string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if addr, err := mail.ParseAddress(email); err != nil || addr.Address != email {
		return &ValidationError{Field: "email", Message: "enter a valid email address"}
	}
	if ok, _ := s.emailLimiter.Allow(email); !ok {
		s.logger.InfoContext(ctx, "sign-in link not sent: recipient limit reached")
		return nil
	}
	acct, err := s.db.GetAccountByEmail(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if !canSignIn(acct.Status) {
		return nil
	}
	return s.enqueueEmail(ctx, EmailJob{AccountID: acct.ID, Purpose: PurposeSignIn})
}

// FinishSignInLink signs in with a token from a sign-in email. Using the link also
// proves the person controls the address, so it verifies the email.
func (s *Service) FinishSignInLink(ctx context.Context, token string, client Client) (Session, error) {
	hash, ok := hashToken(token)
	if !ok {
		return Session{}, ErrLinkInvalid
	}
	var issued Session
	err := s.db.InTx(ctx, func(_ pgx.Tx, q *store.Queries) error {
		row, err := q.ConsumeEmailToken(ctx, store.ConsumeEmailTokenParams{TokenHash: hash, Purpose: PurposeSignIn})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrLinkInvalid
		}
		if err != nil {
			return err
		}
		acct, err := q.GetAccountForEmail(ctx, row.AccountID)
		if err != nil {
			return err
		}
		if !canSignIn(acct.Status) {
			return ErrSignInFailed
		}
		if acct.Email != row.Email {
			return ErrLinkInvalid
		}
		if _, err := q.MarkEmailVerified(ctx, store.MarkEmailVerifiedParams{ID: acct.ID, Email: row.Email}); err != nil {
			return err
		}
		if err := q.TouchAccount(ctx, acct.ID); err != nil {
			return err
		}
		issued, err = createSession(ctx, q, acct.ID, client)
		return err
	})
	return issued, err
}

// VerifyEmail confirms an address with a token from a verification email. It works
// without being signed in, since the link may open in a different browser.
func (s *Service) VerifyEmail(ctx context.Context, token string) error {
	hash, ok := hashToken(token)
	if !ok {
		return ErrLinkInvalid
	}
	return s.db.InTx(ctx, func(_ pgx.Tx, q *store.Queries) error {
		row, err := q.ConsumeEmailToken(ctx, store.ConsumeEmailTokenParams{TokenHash: hash, Purpose: PurposeVerifyEmail})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrLinkInvalid
		}
		if err != nil {
			return err
		}
		acct, err := q.GetAccountForEmail(ctx, row.AccountID)
		if err != nil {
			return err
		}
		if acct.Email != row.Email {
			return ErrLinkInvalid
		}
		_, err = q.MarkEmailVerified(ctx, store.MarkEmailVerifiedParams{ID: acct.ID, Email: row.Email})
		return err
	})
}

// ResendVerification sends another verification email to the signed-in account.
func (s *Service) ResendVerification(ctx context.Context, accountID uuid.UUID) error {
	acct, err := s.db.GetAccountForEmail(ctx, accountID)
	if err != nil {
		return err
	}
	if acct.EmailVerifiedAt != nil {
		return nil
	}
	if ok, _ := s.emailLimiter.Allow(acct.Email); !ok {
		return ErrTooManyEmails
	}
	return s.enqueueEmail(ctx, EmailJob{AccountID: accountID, Purpose: PurposeVerifyEmail})
}

func (s *Service) enqueueEmail(ctx context.Context, job EmailJob) error {
	return s.db.InTx(ctx, func(tx pgx.Tx, _ *store.Queries) error {
		_, err := s.queue.InsertTx(ctx, tx, job, nil)
		return err
	})
}

func canSignIn(status string) bool {
	return status == "active" || status == "restricted"
}

// Emailer creates email tokens and sends account emails. It runs in the worker.
type Emailer struct {
	db           *database.Store
	sender       pcmail.Sender
	publicURL    string
	instanceName string
	logger       *slog.Logger
}

// NewEmailer returns an emailer that sends through sender.
func NewEmailer(cfg config.Config, db *database.Store, sender pcmail.Sender, logger *slog.Logger) *Emailer {
	return &Emailer{
		db: db, sender: sender, logger: logger,
		publicURL:    strings.TrimSuffix(cfg.PublicURL, "/"),
		instanceName: cfg.InstanceName,
	}
}

// Send issues a fresh token and emails the link. Returning an error makes the job
// retry; each attempt issues a new token, and unused ones simply expire.
func (e *Emailer) Send(ctx context.Context, job EmailJob) error {
	acct, err := e.db.GetAccountForEmail(ctx, job.AccountID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // account deleted since the job was queued
	}
	if err != nil {
		return err
	}
	if !canSignIn(acct.Status) {
		return nil
	}

	var ttl time.Duration
	var path, expiresIn string
	var render func(string, pcmail.LinkEmail) (pcmail.Message, error)
	switch job.Purpose {
	case PurposeVerifyEmail:
		if acct.EmailVerifiedAt != nil {
			return nil
		}
		ttl, path, expiresIn, render = verifyEmailTTL, "/verify-email", "24 hours", pcmail.VerifyEmail
	case PurposeSignIn:
		ttl, path, expiresIn, render = signInLinkTTL, "/signin/email", "15 minutes", pcmail.SignInLink
	default:
		return river.JobCancel(fmt.Errorf("unknown email purpose %q", job.Purpose))
	}

	token, hash, err := newToken()
	if err != nil {
		return err
	}
	if err := e.db.CreateEmailToken(ctx, store.CreateEmailTokenParams{
		ID: mustUUIDv7(), TokenHash: hash, AccountID: acct.ID, Purpose: job.Purpose,
		Email: acct.Email, ExpiresAt: time.Now().Add(ttl),
	}); err != nil {
		return err
	}

	// The token goes in the URL fragment, which browsers never send to the server,
	// so it can't end up in proxy or server logs.
	msg, err := render(acct.Email, pcmail.LinkEmail{
		InstanceName: e.instanceName,
		DisplayName:  acct.DisplayName,
		Link:         e.publicURL + path + "#token=" + token,
		ExpiresIn:    expiresIn,
	})
	if err != nil {
		return river.JobCancel(err)
	}
	if err := e.sender.Send(ctx, msg); err != nil {
		return err
	}
	e.logger.InfoContext(ctx, "email sent", "purpose", job.Purpose, "account_id", acct.ID)
	return nil
}

// EmailWorker runs Emailer for queued EmailJobs.
type EmailWorker struct {
	river.WorkerDefaults[EmailJob]
	emailer *Emailer
}

// NewEmailWorker returns a River worker for email jobs.
func NewEmailWorker(e *Emailer) *EmailWorker {
	return &EmailWorker{emailer: e}
}

// Work sends one email.
func (w *EmailWorker) Work(ctx context.Context, job *river.Job[EmailJob]) error {
	return w.emailer.Send(ctx, job.Args)
}

// Timeout bounds a single send attempt.
func (w *EmailWorker) Timeout(*river.Job[EmailJob]) time.Duration {
	return time.Minute
}
