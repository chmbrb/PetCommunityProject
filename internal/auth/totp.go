// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // G505: TOTP (RFC 6238) as every authenticator app implements it uses HMAC-SHA-1, which remains sound for HMAC
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/chmbrb/PetCommunityProject/internal/store"
)

// TOTP settings (RFC 6238): the defaults every authenticator app and password manager
// supports.
const (
	totpPeriod     = 30 * time.Second
	totpDigits     = 6
	totpSecretSize = 20 // 160 bits, as RFC 4226 recommends
	// totpSkew accepts codes one step either side of now, for clocks that drift.
	totpSkew = 1

	recoveryCodeCount = 10
	// mfaChallengeTTL is how long a sign-in waits for its second step.
	mfaChallengeTTL = 5 * time.Minute
	// maxMFAAttempts wrong codes end the sign-in; the person starts again.
	maxMFAAttempts = 5
)

var (
	ErrTOTPExists        = errors.New("an authenticator app is already set up; remove it first to set up another")
	ErrTOTPNotSetUp      = errors.New("start setting up an authenticator app first")
	ErrTOTPCodeWrong     = errors.New("that code isn't right; check your authenticator app's clock, or wait for the next code")
	ErrRecoveryCodeWrong = errors.New("that recovery code isn't right, or was already used")
	ErrMFAExpired        = errors.New("this sign-in has expired or had too many wrong codes; please sign in again")
	ErrMFAKeyMismatch    = errors.New("the authenticator's secret was encrypted with a different MFA key")
)

// MFARequiredError means the first sign-in step succeeded and the account uses
// two-step sign-in. The client sends Token with a code to FinishMFA.
type MFARequiredError struct {
	Token string
}

func (e *MFARequiredError) Error() string {
	return "enter the code from your authenticator app"
}

// MFAStatus describes the account's two-step sign-in, for account settings.
type MFAStatus struct {
	TOTPEnabledAt     *time.Time
	RecoveryCodesLeft int64
}

// TOTPSetup is shown to the person once, to add the account to an authenticator.
type TOTPSetup struct {
	// Secret is the setup key, in base32, for typing or pasting into a password manager.
	Secret string
	// URI is the otpauth:// link: shown as a QR code, or opened on the same phone.
	URI string
}

// totpCode returns the code for one time step (RFC 4226 dynamic truncation).
func totpCode(secret []byte, step uint64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], step)
	mac := hmac.New(sha1.New, secret)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	n := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", totpDigits, n%1_000_000)
}

// checkTOTP returns the time step code is valid for, or false. Steps at or before
// lastStep are refused, so each code works once.
func checkTOTP(secret []byte, code string, now time.Time, lastStep int64) (int64, bool) {
	code = strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' {
			return -1
		}
		return r
	}, code)
	if len(code) != totpDigits || strings.Trim(code, "0123456789") != "" {
		return 0, false
	}
	current := now.Unix() / int64(totpPeriod/time.Second)
	for d := int64(-totpSkew); d <= totpSkew; d++ {
		step := current + d
		if step <= lastStep || step < 0 {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(totpCode(secret, uint64(step))), []byte(code)) == 1 {
			return step, true
		}
	}
	return 0, false
}

// mfaSealer encrypts TOTP secrets with a key derived from the MFA key, binding each to
// its account so a secret copied to another row doesn't decrypt.
type mfaSealer struct {
	aead  cipher.AEAD
	keyID string
}

func newMFASealer(key []byte) (*mfaSealer, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("MFA key must be 32 bytes, got %d", len(key))
	}
	encKey, err := hkdf.Key(sha256.New, key, nil, "PetCommunityProject TOTP secrets v1", 32)
	if err != nil {
		return nil, err
	}
	idBytes, err := hkdf.Key(sha256.New, key, nil, "PetCommunityProject TOTP secrets key ID v1", 8)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(encKey)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &mfaSealer{aead: aead, keyID: hex.EncodeToString(idBytes)}, nil
}

func (m *mfaSealer) seal(accountID uuid.UUID, secret []byte) ([]byte, error) {
	nonce := make([]byte, m.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return m.aead.Seal(nonce, nonce, secret, accountID[:]), nil
}

func (m *mfaSealer) open(accountID uuid.UUID, keyID string, sealed []byte) ([]byte, error) {
	if keyID != m.keyID {
		return nil, ErrMFAKeyMismatch
	}
	n := m.aead.NonceSize()
	if len(sealed) < n {
		return nil, errors.New("corrupt TOTP secret")
	}
	return m.aead.Open(nil, sealed[:n], sealed[n:], accountID[:])
}

// recoveryAlphabet leaves out letters and digits that are easy to confuse (0/o, 1/l/i).
const recoveryAlphabet = "abcdefghjkmnpqrstuvwxyz23456789"

// newRecoveryCodes returns fresh codes, formatted xxxx-xxxx-xxxx (about 59 bits each).
func newRecoveryCodes() ([]string, error) {
	codes := make([]string, recoveryCodeCount)
	buf := make([]byte, 12)
	for i := range codes {
		var b strings.Builder
		for j := range buf {
			if j > 0 && j%4 == 0 {
				b.WriteByte('-')
			}
			// rand.Text-style rejection sampling keeps every character equally likely.
			for {
				if _, err := rand.Read(buf[j : j+1]); err != nil {
					return nil, err
				}
				if int(buf[j]) < 256-256%len(recoveryAlphabet) {
					b.WriteByte(recoveryAlphabet[int(buf[j])%len(recoveryAlphabet)])
					break
				}
			}
		}
		codes[i] = b.String()
	}
	return codes, nil
}

// hashRecoveryCode normalizes a code as typed (any case, with or without dashes) and
// hashes it. The codes are long and random, so a plain hash is enough.
func hashRecoveryCode(code string) []byte {
	code = strings.Map(func(r rune) rune {
		if r == '-' || r == ' ' {
			return -1
		}
		return r
	}, strings.ToLower(strings.TrimSpace(code)))
	sum := sha256.Sum256([]byte("recovery-code:" + code))
	return sum[:]
}

// replaceRecoveryCodes deletes the account's codes and creates a new set.
func replaceRecoveryCodes(ctx context.Context, q *store.Queries, accountID uuid.UUID) ([]string, error) {
	codes, err := newRecoveryCodes()
	if err != nil {
		return nil, err
	}
	if err := q.DeleteRecoveryCodes(ctx, accountID); err != nil {
		return nil, err
	}
	for _, c := range codes {
		if err := q.CreateRecoveryCode(ctx, store.CreateRecoveryCodeParams{
			ID: mustUUIDv7(), AccountID: accountID, CodeHash: hashRecoveryCode(c),
		}); err != nil {
			return nil, err
		}
	}
	return codes, nil
}

// BeginTOTPSetup creates a new authenticator secret for the account, unconfirmed until
// ConfirmTOTP gets a valid code from it.
func (s *Service) BeginTOTPSetup(ctx context.Context, p Principal, issuer string) (TOTPSetup, error) {
	if err := requireRecentSignIn(p); err != nil {
		return TOTPSetup{}, err
	}
	if s.mfa == nil {
		return TOTPSetup{}, errors.New("no MFA key configured")
	}
	me, err := s.db.GetMe(ctx, p.AccountID)
	if err != nil {
		return TOTPSetup{}, err
	}
	secret := make([]byte, totpSecretSize)
	if _, err := rand.Read(secret); err != nil {
		return TOTPSetup{}, err
	}
	sealed, err := s.mfa.seal(p.AccountID, secret)
	if err != nil {
		return TOTPSetup{}, err
	}
	n, err := s.db.StartTOTPSetup(ctx, store.StartTOTPSetupParams{
		AccountID: p.AccountID, SecretEncrypted: sealed, KeyID: s.mfa.keyID,
	})
	if err != nil {
		return TOTPSetup{}, err
	}
	if n == 0 {
		return TOTPSetup{}, ErrTOTPExists
	}

	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret)
	// The label is the instance and handle, not the email address, which the
	// authenticator app would otherwise show and may sync.
	label := url.PathEscape(issuer + ":@" + me.Handle)
	params := url.Values{
		"secret": {encoded}, "issuer": {issuer}, "algorithm": {"SHA1"},
		"digits": {fmt.Sprint(totpDigits)}, "period": {fmt.Sprint(int(totpPeriod / time.Second))},
	}
	return TOTPSetup{Secret: encoded, URI: "otpauth://totp/" + label + "?" + params.Encode()}, nil
}

// ConfirmTOTP turns on two-step sign-in once the person enters a valid code from their
// new authenticator, and returns recovery codes to show once.
func (s *Service) ConfirmTOTP(ctx context.Context, p Principal, code string) ([]string, error) {
	if err := requireRecentSignIn(p); err != nil {
		return nil, err
	}
	var codes []string
	err := s.db.InTx(ctx, func(tx pgx.Tx, q *store.Queries) error {
		f, err := q.GetTOTPFactor(ctx, p.AccountID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrTOTPNotSetUp
		}
		if err != nil {
			return err
		}
		if f.ConfirmedAt != nil {
			return ErrTOTPExists
		}
		secret, err := s.mfa.open(p.AccountID, f.KeyID, f.SecretEncrypted)
		if err != nil {
			return err
		}
		step, ok := checkTOTP(secret, code, time.Now(), f.LastUsedStep)
		if !ok {
			return ErrTOTPCodeWrong
		}
		if err := q.ConfirmTOTP(ctx, store.ConfirmTOTPParams{AccountID: p.AccountID, LastUsedStep: step}); err != nil {
			return err
		}
		if codes, err = replaceRecoveryCodes(ctx, q, p.AccountID); err != nil {
			return err
		}
		_, err = s.queue.InsertTx(ctx, tx, EmailJob{AccountID: p.AccountID, Purpose: PurposeMFAChanged, Detail: mfaTurnedOn}, nil)
		return err
	})
	return codes, err
}

// RemoveTOTP turns off two-step sign-in and deletes the recovery codes.
func (s *Service) RemoveTOTP(ctx context.Context, p Principal) error {
	if err := requireRecentSignIn(p); err != nil {
		return err
	}
	return s.db.InTx(ctx, func(tx pgx.Tx, q *store.Queries) error {
		enabled, err := q.HasConfirmedTOTP(ctx, p.AccountID)
		if err != nil {
			return err
		}
		if _, err := q.DeleteTOTPFactor(ctx, p.AccountID); err != nil {
			return err
		}
		if err := q.DeleteRecoveryCodes(ctx, p.AccountID); err != nil {
			return err
		}
		if !enabled {
			return nil // only an unfinished setup was cleared
		}
		_, err = s.queue.InsertTx(ctx, tx, EmailJob{AccountID: p.AccountID, Purpose: PurposeMFAChanged, Detail: mfaTurnedOff}, nil)
		return err
	})
}

// RegenerateRecoveryCodes replaces the recovery codes; the old ones stop working.
func (s *Service) RegenerateRecoveryCodes(ctx context.Context, p Principal) ([]string, error) {
	if err := requireRecentSignIn(p); err != nil {
		return nil, err
	}
	var codes []string
	err := s.db.InTx(ctx, func(tx pgx.Tx, q *store.Queries) error {
		enabled, err := q.HasConfirmedTOTP(ctx, p.AccountID)
		if err != nil {
			return err
		}
		if !enabled {
			return ErrTOTPNotSetUp
		}
		if codes, err = replaceRecoveryCodes(ctx, q, p.AccountID); err != nil {
			return err
		}
		_, err = s.queue.InsertTx(ctx, tx, EmailJob{AccountID: p.AccountID, Purpose: PurposeMFAChanged, Detail: mfaCodesReplaced}, nil)
		return err
	})
	return codes, err
}

// requireSecondStep is called inside a sign-in's transaction once its first step
// (email or password) has succeeded. For accounts with two-step sign-in, it records a
// pending sign-in and returns an *MFARequiredError carrying its token; otherwise nil.
func requireSecondStep(ctx context.Context, q *store.Queries, accountID uuid.UUID, client Client) error {
	enabled, err := q.HasConfirmedTOTP(ctx, accountID)
	if err != nil || !enabled {
		return err
	}
	token, hash, err := newToken()
	if err != nil {
		return err
	}
	if err := q.CreateMFAChallenge(ctx, store.CreateMFAChallengeParams{
		ID: mustUUIDv7(), TokenHash: hash, AccountID: accountID, Client: string(client),
		ExpiresAt: time.Now().Add(mfaChallengeTTL),
	}); err != nil {
		return err
	}
	return &MFARequiredError{Token: token}
}

// FinishMFA completes a sign-in with its second step: a code from the authenticator
// app, or a recovery code.
func (s *Service) FinishMFA(ctx context.Context, mfaToken, code, recoveryCode string) (Session, error) {
	hash, ok := hashToken(mfaToken)
	if !ok {
		return Session{}, ErrMFAExpired
	}
	var issued Session
	var outcome error
	err := s.db.InTx(ctx, func(tx pgx.Tx, q *store.Queries) error {
		ch, err := q.GetMFAChallenge(ctx, hash)
		if errors.Is(err, pgx.ErrNoRows) {
			outcome = ErrMFAExpired
			return nil
		}
		if err != nil {
			return err
		}
		// wrong counts a wrong code, ending the sign-in at the limit.
		wrong := func() error {
			outcome = ErrTOTPCodeWrong
			attempts, err := q.RecordMFAFailure(ctx, ch.ID)
			if err != nil || attempts < maxMFAAttempts {
				return err
			}
			return q.DeleteMFAChallenge(ctx, ch.ID)
		}

		switch {
		case recoveryCode != "":
			n, err := q.UseRecoveryCode(ctx, store.UseRecoveryCodeParams{AccountID: ch.AccountID, CodeHash: hashRecoveryCode(recoveryCode)})
			if err != nil {
				return err
			}
			if n == 0 {
				if err := wrong(); err != nil {
					return err
				}
				outcome = ErrRecoveryCodeWrong
				return nil
			}
			left, err := q.CountRecoveryCodesLeft(ctx, ch.AccountID)
			if err != nil {
				return err
			}
			if _, err := s.queue.InsertTx(ctx, tx, EmailJob{
				AccountID: ch.AccountID, Purpose: PurposeMFAChanged, Detail: fmt.Sprintf(mfaCodeUsed, left),
			}, nil); err != nil {
				return err
			}
		default:
			f, err := q.GetTOTPFactor(ctx, ch.AccountID)
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && f.ConfirmedAt == nil) {
				// Two-step sign-in was turned off meanwhile; start again.
				outcome = ErrMFAExpired
				return q.DeleteMFAChallenge(ctx, ch.ID)
			}
			if err != nil {
				return err
			}
			secret, err := s.mfa.open(ch.AccountID, f.KeyID, f.SecretEncrypted)
			if err != nil {
				return err
			}
			step, ok := checkTOTP(secret, code, time.Now(), f.LastUsedStep)
			if !ok {
				return wrong()
			}
			if err := q.UseTOTPStep(ctx, store.UseTOTPStepParams{AccountID: ch.AccountID, LastUsedStep: step}); err != nil {
				return err
			}
		}

		if err := q.DeleteMFAChallenge(ctx, ch.ID); err != nil {
			return err
		}
		if err := q.TouchAccount(ctx, ch.AccountID); err != nil {
			return err
		}
		issued, err = createSession(ctx, q, ch.AccountID, Client(ch.Client))
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

// Details for PurposeMFAChanged notices, completing "… on your account".
const (
	mfaTurnedOn      = "Two-step sign-in was turned on"
	mfaTurnedOff     = "Two-step sign-in was turned off"
	mfaCodesReplaced = "New recovery codes were created (the old ones no longer work)"
	mfaCodeUsed      = "A recovery code was used to sign in (%d left)"
)
