// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
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

// Email purposes. The first three carry a one-time link (matching
// email_tokens.purpose); the others are security notices.
const (
	PurposeVerifyEmail    = "verify_email"
	PurposeSignIn         = "sign_in"
	PurposeSignUp         = "sign_up"
	PurposeNewDevice      = "new_device"
	PurposePasskeyAdded   = "passkey_added"
	PurposePasskeyRemoved = "passkey_removed"
	// PurposePasswordChanged's Detail says what happened: added, changed, or removed.
	PurposePasswordChanged = "password_changed"
)

// QueueEmail is the job queue that sends email.
const QueueEmail = "email"

const (
	verifyEmailTTL = 24 * time.Hour
	// Short, because a sign-in link is as good as a password while it lasts
	// (THREAT_MODEL.md S6).
	signInLinkTTL = 15 * time.Minute
	// Longer, because it's the first email a new person gets from the instance, and
	// it only creates an account for the address it was sent to.
	signUpTTL = time.Hour
	// maxCodeAttempts is how many wrong codes an email allows before its code (and
	// link) stop working. With 6 digits, that leaves a 1 in 200,000 chance of guessing.
	maxCodeAttempts = 5
)

var (
	ErrLinkInvalid   = errors.New("this link is invalid, has expired, or was already used")
	ErrCodeInvalid   = errors.New("that code is wrong or has expired; check you entered the code from the latest email, or request a new one")
	ErrTooManyEmails = errors.New("too many emails were sent to this address; please try again later")

	// errWrongCode is a well-formed code that didn't match, which counts as an attempt.
	errWrongCode = fmt.Errorf("wrong code: %w", ErrCodeInvalid)
)

// EmailProof is what a person brings back from a sign-in or sign-up email: the
// link's token, or the address the email went to and the 6-digit code in it.
type EmailProof struct {
	Token string
	Email string
	Code  string
}

// JobQueue inserts background jobs. *river.Client satisfies it.
type JobQueue interface {
	InsertTx(ctx context.Context, tx pgx.Tx, args river.JobArgs, opts *river.InsertOpts) (*rivertype.JobInsertResult, error)
}

// EmailJob asks the worker to send an account email. It deliberately holds no token:
// the worker creates the token when it sends, so tokens never sit in the job table.
type EmailJob struct {
	// AccountID is the recipient; unset for sign-up, which has no account yet.
	AccountID uuid.UUID `json:"account_id"`
	Purpose   string    `json:"purpose"`
	// Detail is shown in notices, e.g. the passkey's name.
	Detail string `json:"detail,omitempty"`
	// Signup holds the details entered at sign-up, and the address to send to.
	Signup *SignupInput `json:"signup,omitempty"`
}

// Kind identifies the job type to River.
func (EmailJob) Kind() string { return "email" }

// InsertOpts puts email jobs on their own queue. A sign-in link is only useful for a
// few minutes, so it's retried briefly; a verification email can wait longer.
func (j EmailJob) InsertOpts() river.InsertOpts {
	attempts := 10
	if j.Purpose == PurposeSignIn || j.Purpose == PurposeSignUp || j.Purpose == PurposeNewDevice {
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

// FinishEmailSignIn signs in with the link or code from a sign-in email. Using it
// also proves the person controls the address, so it verifies the email.
func (s *Service) FinishEmailSignIn(ctx context.Context, proof EmailProof, client Client) (Session, error) {
	var issued Session
	err := s.withEmailProof(ctx, proof, PurposeSignIn, func(_ pgx.Tx, q *store.Queries, row emailToken) error {
		acct, err := q.GetAccountForEmail(ctx, row.AccountID.UUID)
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

// RequestEmailSignup checks the details and emails a link and code that create the
// account. Nothing is created until one is used, so nobody can sign up an address
// they don't control, and abandoned sign-ups leave no account behind.
func (s *Service) RequestEmailSignup(ctx context.Context, in SignupInput) error {
	in, err := s.checkSignup(ctx, in)
	if err != nil {
		return err
	}
	if ok, _ := s.emailLimiter.Allow(in.Email); !ok {
		return ErrTooManyEmails
	}
	return s.enqueueEmail(ctx, EmailJob{Purpose: PurposeSignUp, Signup: &in})
}

// FinishEmailSignup creates the account from a sign-up email's link or code, with its
// address already verified, and starts a session. A password is optional; it's set
// only now, so it never waits anywhere before the account exists.
func (s *Service) FinishEmailSignup(ctx context.Context, proof EmailProof, password string, client Client) (Session, error) {
	var hash *string
	if password != "" {
		// The email and handle checks need the sign-up details, so they run below.
		h, err := s.hashNewPassword(ctx, password, "", "")
		if err != nil {
			return Session{}, err
		}
		hash = &h
	}
	var issued Session
	err := s.withEmailProof(ctx, proof, PurposeSignUp, func(tx pgx.Tx, q *store.Queries, row emailToken) error {
		var in SignupInput
		if err := json.Unmarshal(row.Signup, &in); err != nil {
			return err
		}
		if hash != nil {
			// Failing here rolls back, so the email can be used again.
			if err := checkNotIdentity(password, in.Email, in.Handle); err != nil {
				return err
			}
		}
		accountID, err := s.createAccount(ctx, tx, q, in)
		if err != nil {
			return err
		}
		if hash != nil {
			if err := q.SetPasswordHash(ctx, store.SetPasswordHashParams{ID: accountID, PasswordHash: hash}); err != nil {
				return err
			}
		}
		if _, err := q.MarkEmailVerified(ctx, store.MarkEmailVerifiedParams{ID: accountID, Email: in.Email}); err != nil {
			return err
		}
		issued, err = createSession(ctx, q, accountID, client)
		return err
	})
	if err != nil {
		return Session{}, mapUniqueViolation(err)
	}
	s.logger.InfoContext(ctx, "account created", "account_id", issued.AccountID, "method", "email")
	return issued, nil
}

// emailToken is a used-up email token.
type emailToken struct {
	AccountID uuid.NullUUID
	Email     string
	Signup    []byte
}

// withEmailProof uses up the token or code in proof and runs fn in the same
// transaction, so the token stays unused if fn fails. A wrong code is recorded
// afterwards, outside the transaction, so the failed attempt still counts.
func (s *Service) withEmailProof(ctx context.Context, proof EmailProof, purpose string, fn func(pgx.Tx, *store.Queries, emailToken) error) error {
	email, codeHash, useCode := codeProof(proof)
	err := s.db.InTx(ctx, func(tx pgx.Tx, q *store.Queries) error {
		var row emailToken
		switch {
		case proof.Token != "":
			hash, ok := hashToken(proof.Token)
			if !ok {
				return ErrLinkInvalid
			}
			r, err := q.ConsumeEmailToken(ctx, store.ConsumeEmailTokenParams{TokenHash: hash, Purpose: purpose})
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrLinkInvalid
			}
			if err != nil {
				return err
			}
			row = emailToken(r)
		case useCode:
			r, err := q.ConsumeEmailCode(ctx, store.ConsumeEmailCodeParams{
				Email: email, Purpose: purpose, CodeHash: codeHash, MaxAttempts: maxCodeAttempts,
			})
			if errors.Is(err, pgx.ErrNoRows) {
				return errWrongCode
			}
			if err != nil {
				return err
			}
			row = emailToken(r)
		default:
			return ErrCodeInvalid
		}
		return fn(tx, q, row)
	})
	if errors.Is(err, errWrongCode) {
		if ferr := s.db.RecordEmailCodeFailure(ctx, store.RecordEmailCodeFailureParams{
			Email: email, Purpose: purpose, MaxAttempts: maxCodeAttempts,
		}); ferr != nil {
			return ferr
		}
		return ErrCodeInvalid
	}
	return err
}

// codeProof returns the normalized address and code hash from proof, and whether
// they're well formed. Spaces and dashes in the code are ignored, since people copy
// it in pieces or as it's shown ("123 456").
func codeProof(proof EmailProof) (email string, codeHash []byte, ok bool) {
	email = strings.ToLower(strings.TrimSpace(proof.Email))
	code := strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' {
			return -1
		}
		return r
	}, proof.Code)
	if email == "" || len(code) != 6 || strings.Trim(code, "0123456789") != "" {
		return "", nil, false
	}
	return email, hashCode(code), true
}

// newCode returns a random 6-digit code for an email, and the hash stored in its place.
func newCode() (string, []byte, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", nil, err
	}
	code := fmt.Sprintf("%06d", n.Int64())
	return code, hashCode(code), nil
}

// hashCode keeps codes out of the database in plain text. A 6-digit code's hash can be
// reversed by trying every code, so what protects a code is its short life and the
// attempt limit, not the hash.
func hashCode(code string) []byte {
	sum := sha256.Sum256([]byte("email-code:" + code))
	return sum[:]
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
		acct, err := q.GetAccountForEmail(ctx, row.AccountID.UUID)
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
	if job.Purpose == PurposeSignUp {
		return e.sendSignup(ctx, job)
	}
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

	switch job.Purpose {
	case PurposePasskeyAdded, PurposePasskeyRemoved, PurposePasswordChanged:
		return e.sendNotice(ctx, acct, job)
	}

	var ttl time.Duration
	var path, expiresIn string
	var withCode bool
	var render func(string, pcmail.LinkEmail) (pcmail.Message, error)
	switch job.Purpose {
	case PurposeVerifyEmail:
		if acct.EmailVerifiedAt != nil {
			return nil
		}
		ttl, path, expiresIn, render = verifyEmailTTL, "/verify-email", "24 hours", pcmail.VerifyEmail
	case PurposeSignIn:
		ttl, path, expiresIn, render = signInLinkTTL, "/signin/email", "15 minutes", pcmail.SignInLink
		withCode = true
	case PurposeNewDevice:
		// Code only: it must be entered on the device that gave the password. The
		// email still gets a token, unused, since every email token has one.
		ttl, path, expiresIn, render = signInLinkTTL, "/settings", "15 minutes", pcmail.NewDevice
		withCode = true
	default:
		return river.JobCancel(fmt.Errorf("unknown email purpose %q", job.Purpose))
	}

	data, err := e.issue(ctx, store.CreateEmailTokenParams{
		AccountID: uuid.NullUUID{UUID: acct.ID, Valid: true}, Purpose: job.Purpose,
		Email: acct.Email, ExpiresAt: time.Now().Add(ttl),
	}, path, withCode)
	if err != nil {
		return err
	}
	data.DisplayName, data.ExpiresIn, data.Detail = acct.DisplayName, expiresIn, job.Detail
	if job.Purpose == PurposeNewDevice {
		// The link points at the settings page, to change the password if it wasn't them.
		data.Link = e.publicURL + path
	}
	msg, err := render(acct.Email, data)
	if err != nil {
		return river.JobCancel(err)
	}
	if err := e.sender.Send(ctx, msg); err != nil {
		return err
	}
	e.logger.InfoContext(ctx, "email sent", "purpose", job.Purpose, "account_id", acct.ID)
	return nil
}

// sendSignup emails the link and code that create a new account.
func (e *Emailer) sendSignup(ctx context.Context, job EmailJob) error {
	if job.Signup == nil {
		return river.JobCancel(errors.New("sign-up email without sign-up details"))
	}
	in := *job.Signup
	details, err := json.Marshal(in)
	if err != nil {
		return river.JobCancel(err)
	}
	data, err := e.issue(ctx, store.CreateEmailTokenParams{
		Purpose: PurposeSignUp, Email: in.Email, Signup: details, ExpiresAt: time.Now().Add(signUpTTL),
	}, "/signup/email", true)
	if err != nil {
		return err
	}
	data.DisplayName, data.ExpiresIn = in.DisplayName, "1 hour"
	msg, err := pcmail.SignUp(in.Email, data)
	if err != nil {
		return river.JobCancel(err)
	}
	if err := e.sender.Send(ctx, msg); err != nil {
		return err
	}
	e.logger.InfoContext(ctx, "email sent", "purpose", job.Purpose)
	return nil
}

// issue stores a new token (and code, if withCode) described by params, and returns
// the email content for them: the link to path, and the code.
func (e *Emailer) issue(ctx context.Context, params store.CreateEmailTokenParams, path string, withCode bool) (pcmail.LinkEmail, error) {
	token, hash, err := newToken()
	if err != nil {
		return pcmail.LinkEmail{}, err
	}
	var code string
	if withCode {
		if code, params.CodeHash, err = newCode(); err != nil {
			return pcmail.LinkEmail{}, err
		}
	}
	params.ID, params.TokenHash = mustUUIDv7(), hash
	if err := e.db.CreateEmailToken(ctx, params); err != nil {
		return pcmail.LinkEmail{}, err
	}
	// The token goes in the URL fragment, which browsers never send to the server,
	// so it can't end up in proxy or server logs.
	return pcmail.LinkEmail{
		InstanceName: e.instanceName,
		Link:         e.publicURL + path + "#token=" + token,
		Code:         code,
	}, nil
}

// sendNotice emails a security notice that links to the account settings.
func (e *Emailer) sendNotice(ctx context.Context, acct store.GetAccountForEmailRow, job EmailJob) error {
	render := pcmail.PasskeyAdded
	switch job.Purpose {
	case PurposePasskeyRemoved:
		render = pcmail.PasskeyRemoved
	case PurposePasswordChanged:
		render = pcmail.PasswordChanged
	}
	msg, err := render(acct.Email, pcmail.LinkEmail{
		InstanceName: e.instanceName,
		DisplayName:  acct.DisplayName,
		Link:         e.publicURL + "/settings",
		Detail:       job.Detail,
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
