// SPDX-License-Identifier: AGPL-3.0-or-later

// Package accountadmin is the operator tooling for accounts until moderation tools
// exist (Phase 2): finding an account, suspending and reinstating it, removing a
// passkey after a takeover, and stopping sign-ups (INCIDENT_RESPONSE.md). Every change
// is written to the audit log with the operator's name.
package accountadmin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/chmbrb/PetCommunityProject/internal/database"
	"github.com/chmbrb/PetCommunityProject/internal/store"
)

var (
	ErrNotFound = errors.New("no such account")
	// ErrWrongStatus means the account isn't in a state the action applies to.
	ErrWrongStatus = errors.New("the account's status doesn't allow this")
	// ErrHeldByCase means an abuse imagery case keeps the account suspended until
	// the case is closed (ADR 0022).
	ErrHeldByCase = errors.New("an open or reported abuse imagery case keeps this account suspended; close the case first")
	// ErrLastPasskey means removing the passkey would leave an account with an
	// unconfirmed email unable to sign in.
	ErrLastPasskey = errors.New("this is the account's only passkey and its email isn't confirmed, so it couldn't sign in without it")
)

// Queries are the database calls the service makes. *store.Queries implements them.
type Queries interface {
	FindAccounts(ctx context.Context, query string) ([]store.FindAccountsRow, error)
	GetAccountForOperator(ctx context.Context, id uuid.UUID) (store.GetAccountForOperatorRow, error)
	ListAccountPasskeys(ctx context.Context, accountID uuid.UUID) ([]store.ListAccountPasskeysRow, error)
	ListAccountSessions(ctx context.Context, accountID uuid.UUID) ([]store.ListAccountSessionsRow, error)
	SuspendAccount(ctx context.Context, id uuid.UUID) (int64, error)
	RevokeAllAccountSessions(ctx context.Context, accountID uuid.UUID) (int64, error)
	ReinstateAccount(ctx context.Context, id uuid.UUID) (int64, error)
	CountAccountPasskeys(ctx context.Context, accountID uuid.UUID) (int64, error)
	GetAccountPasskeyName(ctx context.Context, arg store.GetAccountPasskeyNameParams) (string, error)
	DeletePasskey(ctx context.Context, arg store.DeletePasskeyParams) (int64, error)
	FeatureFlagEnabled(ctx context.Context, flag string) (bool, error)
	SetFeatureFlag(ctx context.Context, arg store.SetFeatureFlagParams) error
	InsertAuditEvent(ctx context.Context, arg store.InsertAuditEventParams) error
}

// DB is Queries plus transactions.
type DB interface {
	Queries
	InTx(ctx context.Context, fn func(q Queries) error) error
}

type storeDB struct{ *database.Store }

func (s storeDB) InTx(ctx context.Context, fn func(q Queries) error) error {
	return s.Store.InTx(ctx, func(_ pgx.Tx, q *store.Queries) error { return fn(q) })
}

// Service runs the operator's actions on accounts.
type Service struct {
	db DB
}

// New returns the service for a database.
func New(st *database.Store) *Service { return &Service{db: storeDB{st}} }

// Account is one account as an operator sees it.
type Account struct {
	store.GetAccountForOperatorRow
	Passkeys []store.ListAccountPasskeysRow
	// Sessions are the signed-in devices: not revoked and not expired.
	Sessions []store.ListAccountSessionsRow
}

// Find returns accounts with exactly this handle (with or without "@") or email
// address.
func (s *Service) Find(ctx context.Context, query string) ([]store.FindAccountsRow, error) {
	q := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(query), "@"))
	if q == "" {
		return nil, errors.New("give a handle or an email address")
	}
	return s.db.FindAccounts(ctx, q)
}

// Get returns an account with its passkeys and signed-in devices.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (Account, error) {
	row, err := get(ctx, s.db, id)
	if err != nil {
		return Account{}, err
	}
	passkeys, err := s.db.ListAccountPasskeys(ctx, id)
	if err != nil {
		return Account{}, err
	}
	sessions, err := s.db.ListAccountSessions(ctx, id)
	if err != nil {
		return Account{}, err
	}
	return Account{GetAccountForOperatorRow: row, Passkeys: passkeys, Sessions: sessions}, nil
}

// Suspend stops an account from signing in and signs it out everywhere. It returns how
// many sessions were revoked.
func (s *Service) Suspend(ctx context.Context, id uuid.UUID, reason, operator string) (int64, error) {
	reason, err := need(reason, operator)
	if err != nil {
		return 0, err
	}
	var revoked int64
	err = s.db.InTx(ctx, func(q Queries) error {
		n, err := q.SuspendAccount(ctx, id)
		if err != nil {
			return err
		}
		if n == 0 {
			return statusError(ctx, q, id, "only an active or restricted account can be suspended")
		}
		if revoked, err = q.RevokeAllAccountSessions(ctx, id); err != nil {
			return err
		}
		return audit(ctx, q, "account.suspended", "account", id, map[string]any{
			"operator": operator, "reason": reason, "sessions_revoked": revoked,
		})
	})
	return revoked, err
}

// Reinstate lifts a suspension. It refuses while an abuse imagery case holds the
// account; the database checks that too.
func (s *Service) Reinstate(ctx context.Context, id uuid.UUID, reason, operator string) error {
	reason, err := need(reason, operator)
	if err != nil {
		return err
	}
	return s.db.InTx(ctx, func(q Queries) error {
		n, err := q.ReinstateAccount(ctx, id)
		if err != nil {
			return err
		}
		if n == 0 {
			a, err := get(ctx, q, id)
			if err != nil {
				return err
			}
			if a.Status == "suspended" && a.HeldByCases > 0 {
				return ErrHeldByCase
			}
			return fmt.Errorf("%w: only a suspended account can be reinstated, and this one is %s", ErrWrongStatus, a.Status)
		}
		return audit(ctx, q, "account.reinstated", "account", id, map[string]any{
			"operator": operator, "reason": reason,
		})
	})
}

// RemovePasskey removes one of the account's passkeys, such as one an attacker added.
// No notice is emailed: the account is normally suspended while this happens, and
// suspended accounts get no email. The operator tells the owner instead.
func (s *Service) RemovePasskey(ctx context.Context, accountID, passkeyID uuid.UUID, reason, operator string) (string, error) {
	reason, err := need(reason, operator)
	if err != nil {
		return "", err
	}
	var name string
	err = s.db.InTx(ctx, func(q Queries) error {
		a, err := get(ctx, q, accountID)
		if err != nil {
			return err
		}
		// Locks the account's passkeys, so the count stays true until the delete.
		count, err := q.CountAccountPasskeys(ctx, accountID)
		if err != nil {
			return err
		}
		name, err = q.GetAccountPasskeyName(ctx, store.GetAccountPasskeyNameParams{ID: passkeyID, AccountID: accountID})
		if errors.Is(err, pgx.ErrNoRows) {
			return errors.New("the account has no passkey with that ID")
		}
		if err != nil {
			return err
		}
		if count <= 1 && a.EmailVerifiedAt == nil {
			return ErrLastPasskey
		}
		if _, err := q.DeletePasskey(ctx, store.DeletePasskeyParams{ID: passkeyID, AccountID: accountID}); err != nil {
			return err
		}
		return audit(ctx, q, "passkey.removed", "account", accountID, map[string]any{
			"operator": operator, "reason": reason, "passkey_id": passkeyID, "passkey_name": name,
		})
	})
	return name, err
}

// SignupsOpen reports whether new accounts can be created.
func (s *Service) SignupsOpen(ctx context.Context) (bool, error) {
	return s.db.FeatureFlagEnabled(ctx, "open_registration")
}

// SetSignups opens or closes sign-ups. It takes effect at once; existing accounts
// keep working either way.
func (s *Service) SetSignups(ctx context.Context, open bool, reason, operator string) error {
	reason, err := need(reason, operator)
	if err != nil {
		return err
	}
	return s.db.InTx(ctx, func(q Queries) error {
		if err := q.SetFeatureFlag(ctx, store.SetFeatureFlagParams{Flag: "open_registration", Enabled: open}); err != nil {
			return err
		}
		action := "instance.signups_closed"
		if open {
			action = "instance.signups_opened"
		}
		// The instance itself has no ID; the nil UUID stands for it.
		return audit(ctx, q, action, "instance", uuid.Nil, map[string]any{"operator": operator, "reason": reason})
	})
}

func get(ctx context.Context, q Queries, id uuid.UUID) (store.GetAccountForOperatorRow, error) {
	a, err := q.GetAccountForOperator(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

func statusError(ctx context.Context, q Queries, id uuid.UUID, why string) error {
	a, err := get(ctx, q, id)
	if err != nil {
		return err
	}
	return fmt.Errorf("%w: %s, and this one is %s", ErrWrongStatus, why, a.Status)
}

// need checks the reason and the operator's name, which every change records.
func need(reason, operator string) (string, error) {
	if strings.TrimSpace(operator) == "" {
		return "", errors.New("the operator's name is required, for the audit log")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "", errors.New("a reason is required, for the audit log")
	}
	return reason, nil
}

// audit records an operator's action. Operators act from the command line, not as a
// signed-in account, so the event names them in its details.
func audit(ctx context.Context, q Queries, action, targetType string, targetID uuid.UUID, details map[string]any) error {
	raw, err := json.Marshal(details)
	if err != nil {
		return err
	}
	return q.InsertAuditEvent(ctx, store.InsertAuditEventParams{
		ID: uuid.Must(uuid.NewV7()), Action: action, TargetType: targetType, TargetID: targetID, Details: raw,
	})
}
