// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/chmbrb/PetCommunityProject/internal/store"
)

// ReauthWindow is how recently a session must have signed in to change sign-in
// methods (ADR 0014 step-up). It keeps a stolen, long-lived session from quietly
// adding a passkey of its own.
const ReauthWindow = 15 * time.Minute

var (
	ErrReauthRequired = errors.New("please confirm it's you by signing in again")
	ErrLastPasskey    = errors.New("you can't remove your only passkey; add another one first")
	ErrNotFound       = errors.New("not found")
)

// PasskeyInfo describes one of the account's passkeys.
type PasskeyInfo struct {
	ID         uuid.UUID
	Name       string
	CreatedAt  time.Time
	LastUsedAt *time.Time
	// Synced is true for passkeys a password manager syncs between devices.
	Synced bool
}

// SessionInfo describes one signed-in device.
type SessionInfo struct {
	ID         uuid.UUID
	Client     Client
	UserAgent  string
	CreatedAt  time.Time
	LastSeenAt time.Time
	// Current is the session making the request.
	Current bool
}

// requireRecentSignIn enforces ReauthWindow.
func requireRecentSignIn(p Principal) error {
	if time.Since(p.AuthenticatedAt) > ReauthWindow {
		return ErrReauthRequired
	}
	return nil
}

// normalizePasskeyName trims a passkey name and checks it. An empty name is allowed
// only when fallback is set, and then replaced by it.
func normalizePasskeyName(name, fallback string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		if fallback == "" {
			return "", &ValidationError{Field: "name", Message: "enter a name"}
		}
		return fallback, nil
	}
	if utf8.RuneCountInString(name) > 100 {
		return "", &ValidationError{Field: "name", Message: "use at most 100 characters"}
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", &ValidationError{Field: "name", Message: "the name contains characters that aren't allowed"}
		}
	}
	return name, nil
}

// ListPasskeys returns the account's passkeys, oldest first.
func (s *Service) ListPasskeys(ctx context.Context, accountID uuid.UUID) ([]PasskeyInfo, error) {
	rows, err := s.db.ListAccountPasskeys(ctx, accountID)
	if err != nil {
		return nil, err
	}
	out := make([]PasskeyInfo, 0, len(rows))
	for _, r := range rows {
		var c webauthn.Credential
		if err := json.Unmarshal(r.Credential, &c); err != nil {
			return nil, err
		}
		out = append(out, PasskeyInfo{
			ID: r.ID, Name: r.Name, CreatedAt: r.CreatedAt, LastUsedAt: r.LastUsedAt,
			Synced: c.Flags.BackupEligible,
		})
	}
	return out, nil
}

// BeginAddPasskey returns options for creating another passkey for the signed-in
// account. Passkeys the account already has are excluded, so the same device isn't
// registered twice.
func (s *Service) BeginAddPasskey(ctx context.Context, p Principal) (uuid.UUID, *protocol.PublicKeyCredentialCreationOptions, error) {
	if err := requireRecentSignIn(p); err != nil {
		return uuid.Nil, nil, err
	}
	user, err := s.passkeyUserFor(ctx, p.AccountID)
	if err != nil {
		return uuid.Nil, nil, err
	}
	exclude := make([]protocol.CredentialDescriptor, 0, len(user.credentials))
	for _, c := range user.credentials {
		exclude = append(exclude, c.Descriptor())
	}
	creation, session, err := s.wa.BeginRegistration(user, webauthn.WithExclusions(exclude))
	if err != nil {
		return uuid.Nil, nil, err
	}
	id, err := s.saveChallenge(ctx, "add_passkey", session, nil, p.AccountID)
	if err != nil {
		return uuid.Nil, nil, err
	}
	return id, &creation.Response, nil
}

// FinishAddPasskey verifies and saves the new passkey, and emails the account a notice.
func (s *Service) FinishAddPasskey(ctx context.Context, p Principal, challengeID uuid.UUID, credential []byte, name string) (PasskeyInfo, error) {
	if err := requireRecentSignIn(p); err != nil {
		return PasskeyInfo{}, err
	}
	name, err := normalizePasskeyName(name, "Passkey")
	if err != nil {
		return PasskeyInfo{}, err
	}
	row, err := s.takeChallenge(ctx, challengeID, "add_passkey")
	if err != nil {
		return PasskeyInfo{}, err
	}
	// A challenge started by another account is treated as if it didn't exist.
	if !row.AccountID.Valid || row.AccountID.UUID != p.AccountID {
		return PasskeyInfo{}, ErrChallengeExpired
	}
	var session webauthn.SessionData
	if err := json.Unmarshal(row.Session, &session); err != nil {
		return PasskeyInfo{}, err
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(credential)
	if err != nil {
		return PasskeyInfo{}, ErrPasskeyRejected
	}
	user, err := s.passkeyUserFor(ctx, p.AccountID)
	if err != nil {
		return PasskeyInfo{}, err
	}
	cred, err := s.wa.CreateCredential(user, session, parsed)
	if err != nil {
		s.logger.InfoContext(ctx, "added passkey rejected", "error", err)
		return PasskeyInfo{}, ErrPasskeyRejected
	}
	credJSON, err := json.Marshal(cred)
	if err != nil {
		return PasskeyInfo{}, err
	}

	info := PasskeyInfo{ID: mustUUIDv7(), Name: name, CreatedAt: time.Now(), Synced: cred.Flags.BackupEligible}
	err = s.db.InTx(ctx, func(tx pgx.Tx, q *store.Queries) error {
		if err := q.CreateAuthIdentity(ctx, store.CreateAuthIdentityParams{
			ID: info.ID, AccountID: p.AccountID, Kind: "passkey",
		}); err != nil {
			return err
		}
		if err := q.CreatePasskeyCredential(ctx, store.CreatePasskeyCredentialParams{
			IdentityID: info.ID, CredentialID: cred.ID, Credential: credJSON, Name: name,
		}); err != nil {
			return err
		}
		_, err := s.queue.InsertTx(ctx, tx, EmailJob{AccountID: p.AccountID, Purpose: PurposePasskeyAdded, Detail: name}, nil)
		return err
	})
	if err != nil {
		return PasskeyInfo{}, mapUniqueViolation(err)
	}
	s.logger.InfoContext(ctx, "passkey added", "account_id", p.AccountID)
	return info, nil
}

// RenamePasskey changes the name of one of the account's passkeys.
func (s *Service) RenamePasskey(ctx context.Context, p Principal, id uuid.UUID, name string) error {
	name, err := normalizePasskeyName(name, "")
	if err != nil {
		return err
	}
	n, err := s.db.RenamePasskey(ctx, store.RenamePasskeyParams{ID: id, AccountID: p.AccountID, Name: name})
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// RemovePasskey deletes one of the account's passkeys and emails a notice. The last
// passkey can't be removed.
func (s *Service) RemovePasskey(ctx context.Context, p Principal, id uuid.UUID) error {
	if err := requireRecentSignIn(p); err != nil {
		return err
	}
	return s.db.InTx(ctx, func(tx pgx.Tx, q *store.Queries) error {
		count, err := q.CountAccountPasskeys(ctx, p.AccountID)
		if err != nil {
			return err
		}
		name, err := q.GetAccountPasskeyName(ctx, store.GetAccountPasskeyNameParams{ID: id, AccountID: p.AccountID})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if count <= 1 {
			return ErrLastPasskey
		}
		if _, err := q.DeletePasskey(ctx, store.DeletePasskeyParams{ID: id, AccountID: p.AccountID}); err != nil {
			return err
		}
		_, err = s.queue.InsertTx(ctx, tx, EmailJob{AccountID: p.AccountID, Purpose: PurposePasskeyRemoved, Detail: name}, nil)
		return err
	})
}

// ListSessions returns the account's signed-in devices, most recently active first.
func (s *Service) ListSessions(ctx context.Context, p Principal) ([]SessionInfo, error) {
	rows, err := s.db.ListAccountSessions(ctx, p.AccountID)
	if err != nil {
		return nil, err
	}
	out := make([]SessionInfo, 0, len(rows))
	for _, r := range rows {
		out = append(out, SessionInfo{
			ID: r.ID, Client: Client(r.Client), UserAgent: r.UserAgent,
			CreatedAt: r.CreatedAt, LastSeenAt: r.LastSeenAt, Current: r.ID == p.SessionID,
		})
	}
	return out, nil
}

// RevokeSession signs out one of the account's devices.
func (s *Service) RevokeSession(ctx context.Context, p Principal, id uuid.UUID) error {
	n, err := s.db.RevokeAccountSession(ctx, store.RevokeAccountSessionParams{ID: id, AccountID: p.AccountID})
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// RevokeOtherSessions signs out every device except the one making the request.
func (s *Service) RevokeOtherSessions(ctx context.Context, p Principal) (int64, error) {
	return s.db.RevokeOtherAccountSessions(ctx, store.RevokeOtherAccountSessionsParams{AccountID: p.AccountID, ID: p.SessionID})
}

// passkeyUserFor loads an account as a go-webauthn user, with its passkeys.
func (s *Service) passkeyUserFor(ctx context.Context, accountID uuid.UUID) (*passkeyUser, error) {
	acct, err := s.db.GetAccountWebAuthn(ctx, accountID)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.ListPasskeysForAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	user := &passkeyUser{handle: acct.WebauthnUserHandle, name: acct.Handle, displayName: acct.DisplayName}
	for _, r := range rows {
		var c webauthn.Credential
		if err := json.Unmarshal(r.Credential, &c); err != nil {
			return nil, err
		}
		user.credentials = append(user.credentials, c)
	}
	return user, nil
}
