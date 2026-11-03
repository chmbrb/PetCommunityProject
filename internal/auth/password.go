// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/argon2"

	"github.com/chmbrb/PetCommunityProject/internal/store"
)

// Password settings (ADR 0014).
const (
	minPasswordLength = 12
	maxPasswordLength = 256
	// maxPasswordAttempts wrong passwords in a row lock password sign-in for
	// passwordLockout. Email codes and passkeys keep working, so a lockout can't keep
	// the owner out.
	maxPasswordAttempts = 10
	passwordLockout     = 15 * time.Minute
	// knownDeviceTTL is how long a device stays recognized after its last sign-in.
	knownDeviceTTL = 365 * 24 * time.Hour
	// hashSlots caps concurrent password hashing: each hash uses argon2Memory, so a
	// burst of sign-in attempts can't exhaust the server's memory.
	hashSlots = 4
)

// argon2id parameters: OWASP's recommended minimum (19 MiB, 2 passes, 1 lane).
// Hashes made with other parameters are upgraded at the next sign-in.
const (
	argon2Time    = 2
	argon2Memory  = 19 * 1024
	argon2Threads = 1
	argon2KeyLen  = 32
	argon2SaltLen = 16
)

var (
	ErrPasswordLocked = errors.New("too many wrong passwords; try again in 15 minutes, or sign in with a code we email you")
	// ErrNewDeviceCode means the password was right, but the device isn't recognized,
	// so an emailed code is needed to finish signing in.
	ErrNewDeviceCode = errors.New("we've emailed you a code to confirm it's you on this new device")
	// ErrLastSignInMethod refuses removing a password when the account couldn't sign
	// in reliably without it.
	ErrLastSignInMethod = errors.New("confirm your email address or add a passkey before removing your password, so you can still sign in")
)

// dummyHash is checked against when an account doesn't exist or has no password, so
// those attempts take as long as real ones and don't reveal which accounts exist.
var dummyHash = mustHash("not-a-real-password")

// hashPassword returns an argon2id hash in the PHC string format.
func hashPassword(password string) (string, error) {
	salt := make([]byte, argon2SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argon2Time, argon2Memory, argon2Threads, argon2KeyLen)
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argon2Memory, argon2Time, argon2Threads, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

func mustHash(password string) string {
	h, err := hashPassword(password)
	if err != nil {
		panic(err)
	}
	return h
}

// verifyPassword reports whether password matches encoded, and whether the hash
// should be replaced because it uses older parameters.
func verifyPassword(password, encoded string) (ok, rehash bool) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, false
	}
	var version int
	var memory uint32
	var passes uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, false
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &passes, &threads); err != nil {
		return false, false
	}
	b64 := base64.RawStdEncoding
	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return false, false
	}
	want, err := b64.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return false, false
	}
	got := argon2.IDKey([]byte(password), salt, passes, memory, threads, uint32(len(want))) //nolint:gosec // G115: len of a decoded hash, far below 2^32
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return false, false
	}
	return true, memory != argon2Memory || passes != argon2Time || threads != argon2Threads || len(want) != argon2KeyLen
}

// withHashSlot runs fn while holding one of the password hashing slots.
func (s *Service) withHashSlot(ctx context.Context, fn func()) error {
	select {
	case s.hashing <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-s.hashing }()
	fn()
	return nil
}

// checkNewPassword applies the password rules: length, not the person's email or
// handle, and not found in known breaches. Following NIST SP 800-63B, there are no
// composition rules (required digits, symbols, and so on): length and the breach
// check matter far more.
func (s *Service) checkNewPassword(ctx context.Context, password, email, handle string) error {
	n := utf8.RuneCountInString(password)
	switch {
	case !utf8.ValidString(password):
		return &ValidationError{Field: "password", Message: "the password contains characters that aren't allowed"}
	case n < minPasswordLength:
		return &ValidationError{Field: "password", Message: fmt.Sprintf("use at least %d characters", minPasswordLength)}
	case n > maxPasswordLength:
		return &ValidationError{Field: "password", Message: fmt.Sprintf("use at most %d characters", maxPasswordLength)}
	}
	if err := checkNotIdentity(password, email, handle); err != nil {
		return err
	}
	if s.breaches != nil {
		breached, err := s.breaches.Breached(ctx, password)
		switch {
		case err != nil:
			// Better to accept a password than to stop people setting one while the
			// service is unreachable; the other rules still apply.
			s.logger.WarnContext(ctx, "breached-password check unavailable; password accepted without it", "error", err)
		case breached:
			return &ValidationError{Field: "password", Message: "this password has appeared in a data breach, so attackers try it; please choose another"}
		}
	}
	return nil
}

// checkNotIdentity refuses a password that is the person's email address, the name
// part of it, or their handle: the first things an attacker would try.
func checkNotIdentity(password, email, handle string) error {
	lower := strings.ToLower(strings.TrimSpace(password))
	local, _, _ := strings.Cut(email, "@")
	for _, v := range []string{email, local, handle} {
		if v != "" && lower == strings.ToLower(v) {
			return &ValidationError{Field: "password", Message: "don't use your email address or handle as your password"}
		}
	}
	return nil
}

// hashNewPassword checks password against the rules and hashes it.
func (s *Service) hashNewPassword(ctx context.Context, password, email, handle string) (string, error) {
	if err := s.checkNewPassword(ctx, password, email, handle); err != nil {
		return "", err
	}
	var hash string
	var herr error
	if err := s.withHashSlot(ctx, func() { hash, herr = hashPassword(password) }); err != nil {
		return "", err
	}
	return hash, herr
}

// PasswordSignIn signs in with an email address and password. A correct password isn't
// always enough, so a leaked password alone can't take over the account: accounts with
// two-step sign-in get an *MFARequiredError, and from a device the account hasn't
// signed in from before, others get ErrNewDeviceCode and an emailed code, and repeat
// the request with it.
func (s *Service) PasswordSignIn(ctx context.Context, email, password, code string, client Client) (Session, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || password == "" || len(password) > 4*maxPasswordLength {
		return Session{}, ErrSignInFailed
	}

	var issued Session
	var outcome error
	err := s.db.InTx(ctx, func(tx pgx.Tx, q *store.Queries) error {
		row, err := q.GetPasswordLogin(ctx, email)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && (row.PasswordHash == nil || !canSignIn(row.Status))) {
			// Spend the same time as a real check, so the answer doesn't reveal
			// whether the account exists or has a password.
			if err := s.withHashSlot(ctx, func() { verifyPassword(password, dummyHash) }); err != nil {
				return err
			}
			outcome = ErrSignInFailed
			return nil
		}
		if err != nil {
			return err
		}
		if row.PasswordLockedUntil != nil && time.Now().Before(*row.PasswordLockedUntil) {
			outcome = ErrPasswordLocked
			return nil
		}

		var ok, rehash bool
		if err := s.withHashSlot(ctx, func() { ok, rehash = verifyPassword(password, *row.PasswordHash) }); err != nil {
			return err
		}
		if !ok {
			lockedUntil, err := q.RecordPasswordFailure(ctx, store.RecordPasswordFailureParams{
				ID: row.ID, MaxAttempts: maxPasswordAttempts, LockoutSeconds: int32(passwordLockout / time.Second),
			})
			if err != nil {
				return err
			}
			outcome = ErrSignInFailed
			if lockedUntil != nil && time.Now().Before(*lockedUntil) {
				s.logger.InfoContext(ctx, "password sign-in locked after repeated failures", "account_id", row.ID)
				outcome = ErrPasswordLocked
			}
			return nil
		}
		if err := q.ResetPasswordFailures(ctx, row.ID); err != nil {
			return err
		}
		if rehash {
			var newHash string
			var herr error
			if err := s.withHashSlot(ctx, func() { newHash, herr = hashPassword(password) }); err != nil {
				return err
			}
			if herr != nil {
				return herr
			}
			if err := q.RehashPassword(ctx, store.RehashPasswordParams{ID: row.ID, PasswordHash: &newHash}); err != nil {
				return err
			}
		}

		// With two-step sign-in, the authenticator code is the second step, on every
		// device. Without it, devices the account hasn't used need an emailed code.
		if err := requireSecondStep(ctx, q, row.ID, client); err != nil {
			var secondStep *MFARequiredError
			if errors.As(err, &secondStep) {
				outcome = secondStep
				return nil
			}
			return err
		}
		known, err := isKnownDevice(ctx, q, row.ID)
		if err != nil {
			return err
		}
		if !known {
			if code == "" {
				if ok, _ := s.emailLimiter.Allow(row.Email); !ok {
					outcome = ErrTooManyEmails
					return nil
				}
				if _, err := s.queue.InsertTx(ctx, tx, EmailJob{
					AccountID: row.ID, Purpose: PurposeNewDevice, Detail: userAgentFrom(ctx),
				}, nil); err != nil {
					return err
				}
				outcome = ErrNewDeviceCode
				return nil
			}
			_, codeHash, wellFormed := codeProof(EmailProof{Email: row.Email, Code: code})
			if !wellFormed {
				outcome = ErrCodeInvalid
				return nil
			}
			if _, err := q.ConsumeEmailCode(ctx, store.ConsumeEmailCodeParams{
				Email: row.Email, Purpose: PurposeNewDevice, CodeHash: codeHash, MaxAttempts: maxCodeAttempts,
			}); errors.Is(err, pgx.ErrNoRows) {
				if err := q.RecordEmailCodeFailure(ctx, store.RecordEmailCodeFailureParams{
					Email: row.Email, Purpose: PurposeNewDevice, MaxAttempts: maxCodeAttempts,
				}); err != nil {
					return err
				}
				outcome = ErrCodeInvalid
				return nil
			} else if err != nil {
				return err
			}
		}

		if err := q.TouchAccount(ctx, row.ID); err != nil {
			return err
		}
		issued, err = createSession(ctx, q, row.ID, client)
		return err
	})
	if err != nil {
		return Session{}, err
	}
	if outcome != nil {
		return Session{}, outcome
	}
	return issued, nil
}

// PasswordInfo describes the account's password, for account settings.
type PasswordInfo struct {
	Set       bool
	ChangedAt *time.Time
}

// SetPassword adds or changes the account's password. It needs a recent sign-in,
// signs out every other device, and emails a notice.
func (s *Service) SetPassword(ctx context.Context, p Principal, password string) error {
	if err := requireRecentSignIn(p); err != nil {
		return err
	}
	state, err := s.db.GetPasswordState(ctx, p.AccountID)
	if err != nil {
		return err
	}
	hash, err := s.hashNewPassword(ctx, password, state.Email, state.Handle)
	if err != nil {
		return err
	}
	detail := "added"
	if state.HasPassword {
		detail = "changed"
	}
	return s.db.InTx(ctx, func(tx pgx.Tx, q *store.Queries) error {
		if err := q.SetPasswordHash(ctx, store.SetPasswordHashParams{ID: p.AccountID, PasswordHash: &hash}); err != nil {
			return err
		}
		// Anyone who had the old password, or a stolen session, is signed out.
		if _, err := q.RevokeOtherAccountSessions(ctx, store.RevokeOtherAccountSessionsParams{
			AccountID: p.AccountID, ID: p.SessionID,
		}); err != nil {
			return err
		}
		_, err := s.queue.InsertTx(ctx, tx, EmailJob{AccountID: p.AccountID, Purpose: PurposePasswordChanged, Detail: detail}, nil)
		return err
	})
}

// RemovePassword removes the account's password, if it can still sign in without it:
// by email (once the address is verified) or with a passkey.
func (s *Service) RemovePassword(ctx context.Context, p Principal) error {
	if err := requireRecentSignIn(p); err != nil {
		return err
	}
	return s.db.InTx(ctx, func(tx pgx.Tx, q *store.Queries) error {
		acct, err := q.GetAccountForEmail(ctx, p.AccountID)
		if err != nil {
			return err
		}
		passkeys, err := q.CountAccountPasskeys(ctx, p.AccountID)
		if err != nil {
			return err
		}
		if acct.EmailVerifiedAt == nil && passkeys == 0 {
			return ErrLastSignInMethod
		}
		state, err := q.GetPasswordState(ctx, p.AccountID)
		if err != nil {
			return err
		}
		if !state.HasPassword {
			return nil
		}
		if err := q.SetPasswordHash(ctx, store.SetPasswordHashParams{ID: p.AccountID}); err != nil {
			return err
		}
		_, err = s.queue.InsertTx(ctx, tx, EmailJob{AccountID: p.AccountID, Purpose: PurposePasswordChanged, Detail: "removed"}, nil)
		return err
	})
}

type deviceTokenKey struct{}

// WithDeviceToken records the device token the client presented (a cookie in browsers,
// a header from the apps), so sign-ins can tell whether the device is known.
func WithDeviceToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, deviceTokenKey{}, token)
}

func deviceTokenFrom(ctx context.Context) string {
	token, _ := ctx.Value(deviceTokenKey{}).(string)
	return token
}

// isKnownDevice reports whether the request's device has signed in to the account before.
func isKnownDevice(ctx context.Context, q *store.Queries, accountID uuid.UUID) (bool, error) {
	hash, ok := hashToken(deviceTokenFrom(ctx))
	if !ok {
		return false, nil
	}
	return q.KnownDeviceExists(ctx, store.KnownDeviceExistsParams{AccountID: accountID, TokenHash: hash})
}

// rememberDevice records that the request's device signed in to the account. A device
// without a token gets a new one, returned for the client to keep.
func rememberDevice(ctx context.Context, q *store.Queries, accountID uuid.UUID) (issued string, err error) {
	hash, ok := hashToken(deviceTokenFrom(ctx))
	if !ok {
		if issued, hash, err = newToken(); err != nil {
			return "", err
		}
	}
	err = q.RememberDevice(ctx, store.RememberDeviceParams{
		ID: mustUUIDv7(), AccountID: accountID, TokenHash: hash, ExpiresAt: time.Now().Add(knownDeviceTTL),
	})
	return issued, err
}
