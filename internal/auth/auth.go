// SPDX-License-Identifier: AGPL-3.0-or-later

// Package auth implements passkey sign-up and sign-in and server-side sessions
// (ADR 0014, ADR 0019).
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/chmbrb/PetCommunityProject/internal/config"
	"github.com/chmbrb/PetCommunityProject/internal/database"
	"github.com/chmbrb/PetCommunityProject/internal/ratelimit"
	"github.com/chmbrb/PetCommunityProject/internal/store"
)

const (
	challengeTTL     = 5 * time.Minute
	webSessionTTL    = 30 * 24 * time.Hour
	mobileSessionTTL = 180 * 24 * time.Hour
	// Sessions record activity at most this often, to avoid a write on every request.
	touchInterval = 5 * time.Minute
)

// Errors returned to the HTTP layer. Sign-in failures are deliberately not more
// specific, so they don't reveal which accounts or passkeys exist.
var (
	ErrRegistrationClosed = errors.New("registration is closed on this instance")
	ErrEmailTaken         = errors.New("an account with this email already exists")
	ErrHandleTaken        = errors.New("this handle is taken")
	ErrChallengeExpired   = errors.New("the passkey request expired; please try again")
	ErrPasskeyRejected    = errors.New("the passkey couldn't be verified")
	ErrSignInFailed       = errors.New("sign-in failed")
	ErrUnauthenticated    = errors.New("not signed in")
)

// Client is the kind of client a session belongs to.
type Client string

const (
	ClientWeb    Client = "web"
	ClientMobile Client = "mobile"
)

// Session is a newly issued session. Token is shown to the client once and never stored.
type Session struct {
	Token     string
	ExpiresAt time.Time
	AccountID uuid.UUID
}

// Principal is the signed-in account behind a request.
type Principal struct {
	AccountID uuid.UUID
	SessionID uuid.UUID
	Client    Client
	// AuthenticatedAt is when this session signed in, for step-up checks.
	AuthenticatedAt time.Time
}

type userAgentKey struct{}

// WithUserAgent records the client's User-Agent header, so sessions created while
// handling the request show which browser or app they belong to.
func WithUserAgent(ctx context.Context, userAgent string) context.Context {
	return context.WithValue(ctx, userAgentKey{}, userAgent)
}

// userAgentFrom returns the request's User-Agent, cleaned and cut to 256 bytes.
func userAgentFrom(ctx context.Context) string {
	ua, _ := ctx.Value(userAgentKey{}).(string)
	ua = strings.ToValidUTF8(strings.TrimSpace(ua), "")
	for len(ua) > 256 {
		_, size := utf8.DecodeLastRuneInString(ua)
		ua = ua[:len(ua)-size]
	}
	return ua
}

// Service implements sign-up, sign-in, and sessions.
type Service struct {
	db     *database.Store
	wa     *webauthn.WebAuthn
	queue  JobQueue
	logger *slog.Logger
	// emailLimiter caps emails per recipient address.
	emailLimiter *ratelimit.Limiter
}

// New configures passkeys for this instance. Emails are queued on queue and sent by
// the worker.
func New(cfg config.Config, db *database.Store, queue JobQueue, logger *slog.Logger) (*Service, error) {
	wa, err := webauthn.New(&webauthn.Config{
		RPID:          cfg.WebAuthnRPID,
		RPDisplayName: cfg.InstanceName,
		RPOrigins:     cfg.WebAuthnOrigins,
		// Passkeys must be discoverable (no username needed to sign in) and require
		// user verification (biometric or device PIN).
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			ResidentKey:        protocol.ResidentKeyRequirementRequired,
			RequireResidentKey: protocol.ResidentKeyRequired(),
			UserVerification:   protocol.VerificationRequired,
		},
		AttestationPreference: protocol.PreferNoAttestation,
	})
	if err != nil {
		return nil, fmt.Errorf("configure passkeys: %w", err)
	}
	return &Service{
		db: db, wa: wa, queue: queue, logger: logger,
		emailLimiter: ratelimit.New(cfg.RateLimits.EmailRecipient),
	}, nil
}

// passkeyUser adapts an account to go-webauthn's User interface.
type passkeyUser struct {
	handle      []byte
	name        string
	displayName string
	credentials []webauthn.Credential
}

func (u *passkeyUser) WebAuthnID() []byte                         { return u.handle }
func (u *passkeyUser) WebAuthnName() string                       { return u.name }
func (u *passkeyUser) WebAuthnDisplayName() string                { return u.displayName }
func (u *passkeyUser) WebAuthnCredentials() []webauthn.Credential { return u.credentials }

// pendingSignup is stored with a signup challenge until the passkey is created.
type pendingSignup struct {
	AccountID  uuid.UUID   `json:"account_id"`
	UserHandle []byte      `json:"user_handle"`
	Input      SignupInput `json:"input"`
}

// BeginSignup validates the details and returns passkey creation options.
func (s *Service) BeginSignup(ctx context.Context, in SignupInput) (uuid.UUID, *protocol.PublicKeyCredentialCreationOptions, error) {
	in, err := in.Normalize()
	if err != nil {
		return uuid.Nil, nil, err
	}
	if open, err := s.db.FeatureFlagEnabled(ctx, "open_registration"); err != nil {
		return uuid.Nil, nil, err
	} else if !open {
		return uuid.Nil, nil, ErrRegistrationClosed
	}
	if taken, err := s.db.EmailExists(ctx, in.Email); err != nil {
		return uuid.Nil, nil, err
	} else if taken {
		return uuid.Nil, nil, ErrEmailTaken
	}
	if taken, err := s.db.HandleExists(ctx, in.Handle); err != nil {
		return uuid.Nil, nil, err
	} else if taken {
		return uuid.Nil, nil, ErrHandleTaken
	}

	pending := pendingSignup{AccountID: mustUUIDv7(), UserHandle: make([]byte, 64), Input: in}
	if _, err := rand.Read(pending.UserHandle); err != nil {
		return uuid.Nil, nil, err
	}
	user := &passkeyUser{handle: pending.UserHandle, name: in.Handle, displayName: in.DisplayName}
	creation, session, err := s.wa.BeginRegistration(user)
	if err != nil {
		return uuid.Nil, nil, fmt.Errorf("begin passkey registration: %w", err)
	}

	id, err := s.saveChallenge(ctx, "signup", session, &pending, uuid.Nil)
	if err != nil {
		return uuid.Nil, nil, err
	}
	return id, &creation.Response, nil
}

// FinishSignup verifies the new passkey, creates the account and its person actor,
// and starts a session.
func (s *Service) FinishSignup(ctx context.Context, challengeID uuid.UUID, credential []byte, client Client) (Session, error) {
	row, err := s.takeChallenge(ctx, challengeID, "signup")
	if err != nil {
		return Session{}, err
	}
	var session webauthn.SessionData
	var pending pendingSignup
	if err := json.Unmarshal(row.Session, &session); err != nil {
		return Session{}, err
	}
	if err := json.Unmarshal(row.Signup, &pending); err != nil {
		return Session{}, err
	}

	parsed, err := protocol.ParseCredentialCreationResponseBytes(credential)
	if err != nil {
		return Session{}, ErrPasskeyRejected
	}
	user := &passkeyUser{handle: pending.UserHandle, name: pending.Input.Handle, displayName: pending.Input.DisplayName}
	cred, err := s.wa.CreateCredential(user, session, parsed)
	if err != nil {
		s.logger.InfoContext(ctx, "passkey registration rejected", "error", err)
		return Session{}, ErrPasskeyRejected
	}
	credJSON, err := json.Marshal(cred)
	if err != nil {
		return Session{}, err
	}

	var issued Session
	err = s.db.InTx(ctx, func(tx pgx.Tx, q *store.Queries) error {
		if open, err := q.FeatureFlagEnabled(ctx, "open_registration"); err != nil {
			return err
		} else if !open {
			return ErrRegistrationClosed
		}
		in := pending.Input
		if err := q.CreateAccount(ctx, store.CreateAccountParams{
			ID: pending.AccountID, Email: in.Email, WebauthnUserHandle: pending.UserHandle,
		}); err != nil {
			return err
		}
		if err := q.CreatePersonActor(ctx, store.CreatePersonActorParams{
			ID: mustUUIDv7(), Handle: in.Handle, DisplayName: in.DisplayName, AccountID: uuid.NullUUID{UUID: pending.AccountID, Valid: true},
		}); err != nil {
			return err
		}
		identityID := mustUUIDv7()
		if err := q.CreateAuthIdentity(ctx, store.CreateAuthIdentityParams{
			ID: identityID, AccountID: pending.AccountID, Kind: "passkey",
		}); err != nil {
			return err
		}
		if err := q.CreatePasskeyCredential(ctx, store.CreatePasskeyCredentialParams{
			IdentityID: identityID, CredentialID: cred.ID, Credential: credJSON, Name: "Passkey",
		}); err != nil {
			return err
		}
		// Queued in the same transaction, so the email goes out only if the account exists.
		if _, err := s.queue.InsertTx(ctx, tx, EmailJob{AccountID: pending.AccountID, Purpose: PurposeVerifyEmail}, nil); err != nil {
			return fmt.Errorf("queue verification email: %w", err)
		}
		issued, err = createSession(ctx, q, pending.AccountID, client)
		return err
	})
	if err != nil {
		return Session{}, mapUniqueViolation(err)
	}
	s.logger.InfoContext(ctx, "account created", "account_id", issued.AccountID)
	return issued, nil
}

// BeginLogin returns options for signing in with any passkey for this instance.
func (s *Service) BeginLogin(ctx context.Context) (uuid.UUID, *protocol.PublicKeyCredentialRequestOptions, error) {
	assertion, session, err := s.wa.BeginDiscoverableLogin()
	if err != nil {
		return uuid.Nil, nil, fmt.Errorf("begin passkey login: %w", err)
	}
	id, err := s.saveChallenge(ctx, "login", session, nil, uuid.Nil)
	if err != nil {
		return uuid.Nil, nil, err
	}
	return id, &assertion.Response, nil
}

// FinishLogin verifies a passkey assertion and starts a session.
func (s *Service) FinishLogin(ctx context.Context, challengeID uuid.UUID, credential []byte, client Client) (Session, error) {
	row, err := s.takeChallenge(ctx, challengeID, "login")
	if err != nil {
		return Session{}, err
	}
	var session webauthn.SessionData
	if err := json.Unmarshal(row.Session, &session); err != nil {
		return Session{}, err
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(credential)
	if err != nil {
		return Session{}, ErrSignInFailed
	}

	var account store.GetAccountByWebAuthnHandleRow
	identityByCredential := map[string]uuid.UUID{}
	findUser := func(_, userHandle []byte) (webauthn.User, error) {
		var err error
		account, err = s.db.GetAccountByWebAuthnHandle(ctx, userHandle)
		if err != nil {
			return nil, err
		}
		rows, err := s.db.ListPasskeysForAccount(ctx, account.ID)
		if err != nil {
			return nil, err
		}
		user := &passkeyUser{handle: account.WebauthnUserHandle}
		for _, r := range rows {
			var c webauthn.Credential
			if err := json.Unmarshal(r.Credential, &c); err != nil {
				return nil, err
			}
			user.credentials = append(user.credentials, c)
			identityByCredential[string(r.CredentialID)] = r.IdentityID
		}
		return user, nil
	}

	_, cred, err := s.wa.ValidatePasskeyLogin(findUser, session, parsed)
	if err != nil {
		s.logger.InfoContext(ctx, "passkey sign-in rejected", "error", err)
		return Session{}, ErrSignInFailed
	}
	if cred.Authenticator.CloneWarning {
		// The signature counter went backwards: the passkey may have been copied.
		s.logger.WarnContext(ctx, "passkey clone warning; sign-in refused", "account_id", account.ID)
		return Session{}, ErrSignInFailed
	}
	if account.Status != "active" && account.Status != "restricted" {
		return Session{}, ErrSignInFailed
	}

	credJSON, err := json.Marshal(cred)
	if err != nil {
		return Session{}, err
	}
	identityID := identityByCredential[string(cred.ID)]

	var issued Session
	err = s.db.InTx(ctx, func(_ pgx.Tx, q *store.Queries) error {
		if err := q.UpdatePasskeyAfterLogin(ctx, store.UpdatePasskeyAfterLoginParams{
			IdentityID: identityID, Credential: credJSON,
		}); err != nil {
			return err
		}
		if err := q.TouchAuthIdentity(ctx, identityID); err != nil {
			return err
		}
		if err := q.TouchAccount(ctx, account.ID); err != nil {
			return err
		}
		issued, err = createSession(ctx, q, account.ID, client)
		return err
	})
	return issued, err
}

// Authenticate returns the principal for a session token.
func (s *Service) Authenticate(ctx context.Context, token string) (Principal, error) {
	hash, ok := hashToken(token)
	if !ok {
		return Principal{}, ErrUnauthenticated
	}
	row, err := s.db.GetActiveSession(ctx, hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return Principal{}, ErrUnauthenticated
	}
	if err != nil {
		return Principal{}, err
	}
	if time.Since(row.LastSeenAt) > touchInterval {
		if err := s.db.TouchSession(ctx, row.ID); err != nil {
			s.logger.WarnContext(ctx, "touch session", "error", err)
		}
		if err := s.db.TouchAccount(ctx, row.AccountID); err != nil {
			s.logger.WarnContext(ctx, "touch account", "error", err)
		}
	}
	return Principal{
		AccountID: row.AccountID, SessionID: row.ID, Client: Client(row.Client), AuthenticatedAt: row.CreatedAt,
	}, nil
}

// SignOut revokes a session.
func (s *Service) SignOut(ctx context.Context, sessionID uuid.UUID) error {
	return s.db.RevokeSession(ctx, sessionID)
}

// Me returns the signed-in account and its person actor.
func (s *Service) Me(ctx context.Context, accountID uuid.UUID) (store.GetMeRow, error) {
	return s.db.GetMe(ctx, accountID)
}

// saveChallenge stores a passkey ceremony. accountID binds an add-passkey challenge to
// the account that started it; it's uuid.Nil for sign-up and sign-in.
func (s *Service) saveChallenge(ctx context.Context, purpose string, session *webauthn.SessionData, pending *pendingSignup, accountID uuid.UUID) (uuid.UUID, error) {
	sessionJSON, err := json.Marshal(session)
	if err != nil {
		return uuid.Nil, err
	}
	var signupJSON []byte
	if pending != nil {
		if signupJSON, err = json.Marshal(pending); err != nil {
			return uuid.Nil, err
		}
	}
	id := mustUUIDv7()
	err = s.db.CreateWebAuthnChallenge(ctx, store.CreateWebAuthnChallengeParams{
		ID: id, Purpose: purpose, Session: sessionJSON, Signup: signupJSON,
		AccountID: uuid.NullUUID{UUID: accountID, Valid: accountID != uuid.Nil},
		ExpiresAt: time.Now().Add(challengeTTL),
	})
	return id, err
}

func (s *Service) takeChallenge(ctx context.Context, id uuid.UUID, purpose string) (store.TakeWebAuthnChallengeRow, error) {
	row, err := s.db.TakeWebAuthnChallenge(ctx, store.TakeWebAuthnChallengeParams{ID: id, Purpose: purpose})
	if errors.Is(err, pgx.ErrNoRows) {
		return row, ErrChallengeExpired
	}
	return row, err
}

func createSession(ctx context.Context, q *store.Queries, accountID uuid.UUID, client Client) (Session, error) {
	token, hash, err := newToken()
	if err != nil {
		return Session{}, err
	}

	ttl := webSessionTTL
	if client == ClientMobile {
		ttl = mobileSessionTTL
	}
	expires := time.Now().Add(ttl)
	err = q.CreateSession(ctx, store.CreateSessionParams{
		ID: mustUUIDv7(), TokenHash: hash, AccountID: accountID, Client: string(client),
		UserAgent: userAgentFrom(ctx), ExpiresAt: expires,
	})
	return Session{Token: token, ExpiresAt: expires, AccountID: accountID}, err
}

// newToken returns a random 256-bit token for a session or an email link, and the hash
// that's stored in its place.
func newToken() (token string, hash []byte, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256(raw)
	return token, sum[:], nil
}

// hashToken returns the stored form of a session or email token, or false if the
// token is malformed.
func hashToken(token string) ([]byte, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != 32 {
		return nil, false
	}
	sum := sha256.Sum256(raw)
	return sum[:], true
}

func mapUniqueViolation(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		return err
	}
	switch pgErr.ConstraintName {
	case "accounts_email_key":
		return ErrEmailTaken
	case "actors_handle_key":
		return ErrHandleTaken
	case "passkey_credentials_credential_id_key":
		return ErrPasskeyRejected
	}
	return err
}

func mustUUIDv7() uuid.UUID {
	return uuid.Must(uuid.NewV7())
}
