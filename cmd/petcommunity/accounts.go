// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/google/uuid"

	"github.com/chmbrb/PetCommunityProject/internal/accountadmin"
	"github.com/chmbrb/PetCommunityProject/internal/config"
	"github.com/chmbrb/PetCommunityProject/internal/database"
)

const accountsUsage = `usage: petcommunity accounts <command>

Accounts, until moderation tools exist (INCIDENT_RESPONSE.md). Run on the server, e.g.
  docker compose run --rm -T worker accounts find @handle

commands:
  find HANDLE|EMAIL
      accounts with exactly this handle (with or without @) or email address
  show ID
      the account, how it signs in, its passkeys, and its signed-in devices
  suspend --operator NAME --reason TEXT ID
      stop the account signing in, and sign it out everywhere
  reinstate --operator NAME --reason TEXT ID
      lift a suspension; refused while an abuse imagery case is open or reported
  remove-passkey --operator NAME --reason TEXT ID PASSKEY_ID
      remove one passkey, e.g. one added during a takeover (PASSKEY_ID from show)

Every change goes in the audit log with the operator's name and the reason.
`

const signupsUsage = `usage: petcommunity signups [on|off --operator NAME --reason TEXT]

Without arguments, says whether new accounts can be created. "off" stops sign-ups
at once (existing accounts keep working); "on" opens them again. Recorded in the
audit log.
`

//nolint:gosec // G101: the command's help text, not a credential
const passwordSignInUsage = `usage: petcommunity password-sign-in [on|off --operator NAME --reason TEXT]

Without arguments, says whether people can sign in with a password. "off" pauses
password sign-ins at once, e.g. during a wave of account takeovers; email codes and
passkeys keep working, and people can still set passwords. "on" resumes them.
Recorded in the audit log.
`

var (
	errAccountsUsage       = errors.New("accounts usage")
	errSignupsUsage        = errors.New("signups usage")
	errPasswordSignInUsage = errors.New("password-sign-in usage")
)

func accounts(ctx context.Context, cfg config.Config, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errAccountsUsage
	}
	sub, args := args[0], args[1:]
	fs := flag.NewFlagSet("accounts "+sub, flag.ContinueOnError)
	fs.SetOutput(stderr)
	var operator, reason string
	wantArgs := 1
	switch sub {
	case "find", "show":
	case "suspend", "reinstate", "remove-passkey":
		fs.StringVar(&operator, "operator", "", "your name, for the audit log")
		fs.StringVar(&reason, "reason", "", "why, for the audit log")
		if sub == "remove-passkey" {
			wantArgs = 2
		}
	default:
		return errAccountsUsage
	}
	if err := fs.Parse(args); err != nil || fs.NArg() != wantArgs {
		return errAccountsUsage
	}
	var ids []uuid.UUID
	if sub != "find" {
		for _, a := range fs.Args() {
			id, err := uuid.Parse(a)
			if err != nil {
				return fmt.Errorf("%q isn't an ID; find the account's ID with `accounts find`", a)
			}
			ids = append(ids, id)
		}
	}

	svc, closeDB, err := openAccountAdmin(ctx, cfg)
	if err != nil {
		return err
	}
	defer closeDB()

	switch sub {
	case "find":
		rows, err := svc.Find(ctx, fs.Arg(0))
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			fmt.Fprintln(stderr, "no account with that handle or email")
			return nil
		}
		tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tHANDLE\tEMAIL\tSTATUS\tCREATED\tLAST ACTIVE")
		for _, r := range rows {
			fmt.Fprintf(tw, "%s\t@%s\t%s\t%s\t%s\t%s\n", r.ID, r.Handle, r.Email, r.Status,
				r.CreatedAt.UTC().Format(time.RFC3339), formatTime(r.LastActiveAt))
		}
		return tw.Flush()
	case "show":
		a, err := svc.Get(ctx, ids[0])
		if err != nil {
			return err
		}
		tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
		held := "no"
		if a.HeldByCases > 0 {
			held = fmt.Sprintf("yes: %d open or reported (petcommunity cases list)", a.HeldByCases)
		}
		for _, line := range [][2]string{
			{"Account", a.ID.String()},
			{"Status", a.Status},
			{"Handle", "@" + a.Handle},
			{"Display name", a.DisplayName},
			{"Email", a.Email},
			{"Email confirmed", formatTime(a.EmailVerifiedAt)},
			{"Created", a.CreatedAt.UTC().Format(time.RFC3339)},
			{"Last active", formatTime(a.LastActiveAt)},
			{"Password", yesNo(a.HasPassword)},
			{"Two-step sign-in", yesNo(a.HasTotp)},
			{"Abuse imagery case", held},
		} {
			fmt.Fprintf(tw, "%s:\t%s\n", line[0], line[1])
		}
		if err := tw.Flush(); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "\nPasskeys (%d):\n", len(a.Passkeys))
		tw = tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
		for _, p := range a.Passkeys {
			fmt.Fprintf(tw, "  %s\t%s\tadded %s\tlast used %s\n", p.ID, nameOr(p.Name),
				p.CreatedAt.UTC().Format(time.RFC3339), formatTime(p.LastUsedAt))
		}
		if err := tw.Flush(); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "\nSigned-in devices (%d):\n", len(a.Sessions))
		tw = tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
		for _, s := range a.Sessions {
			fmt.Fprintf(tw, "  %s\t%s\tsigned in %s\tlast seen %s\n", s.Client, nameOr(s.UserAgent),
				s.CreatedAt.UTC().Format(time.RFC3339), s.LastSeenAt.UTC().Format(time.RFC3339))
		}
		return tw.Flush()
	case "suspend":
		revoked, err := svc.Suspend(ctx, ids[0], reason, operator)
		if err != nil {
			return err
		}
		fmt.Fprintf(stderr, "account %s is suspended; %d signed-in devices were signed out\n", ids[0], revoked)
		return nil
	case "reinstate":
		if err := svc.Reinstate(ctx, ids[0], reason, operator); err != nil {
			return err
		}
		fmt.Fprintf(stderr, "account %s is active again\n", ids[0])
		return nil
	default: // remove-passkey
		name, err := svc.RemovePasskey(ctx, ids[0], ids[1], reason, operator)
		if err != nil {
			return err
		}
		fmt.Fprintf(stderr, "removed the passkey %q; no email is sent, so tell the account's owner\n", nameOr(name))
		return nil
	}
}

func signups(ctx context.Context, cfg config.Config, args []string, stdout, stderr io.Writer) error {
	return instanceSwitch(ctx, cfg, args, stdout, stderr, errSignupsUsage, (*accountadmin.Service).SignupsOpen,
		(*accountadmin.Service).SetSignups, "sign-ups are open", "sign-ups are closed; existing accounts keep working")
}

func passwordSignIn(ctx context.Context, cfg config.Config, args []string, stdout, stderr io.Writer) error {
	return instanceSwitch(ctx, cfg, args, stdout, stderr, errPasswordSignInUsage, (*accountadmin.Service).PasswordSignInOn,
		(*accountadmin.Service).SetPasswordSignIn, "password sign-in is on",
		"password sign-in is paused; email codes and passkeys still work")
}

// instanceSwitch runs a command that shows an instance switch, or turns it "on" or
// "off" with --operator and --reason.
func instanceSwitch(ctx context.Context, cfg config.Config, args []string, stdout, stderr io.Writer, usage error,
	get func(*accountadmin.Service, context.Context) (bool, error),
	set func(*accountadmin.Service, context.Context, bool, string, string) error,
	onText, offText string,
) error {
	var on bool
	switch {
	case len(args) == 0:
	case args[0] == "on":
		on = true
	case args[0] == "off":
	default:
		return usage
	}
	var operator, reason string
	if len(args) > 0 {
		fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
		fs.SetOutput(stderr)
		fs.StringVar(&operator, "operator", "", "your name, for the audit log")
		fs.StringVar(&reason, "reason", "", "why, for the audit log")
		if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
			return usage
		}
	}

	svc, closeDB, err := openAccountAdmin(ctx, cfg)
	if err != nil {
		return err
	}
	defer closeDB()

	if len(args) > 0 {
		if err := set(svc, ctx, on, reason, operator); err != nil {
			return err
		}
	}
	now, err := get(svc, ctx)
	if err != nil {
		return err
	}
	if now {
		fmt.Fprintln(stdout, onText)
	} else {
		fmt.Fprintln(stdout, offText)
	}
	return nil
}

func openAccountAdmin(ctx context.Context, cfg config.Config) (*accountadmin.Service, func(), error) {
	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, nil, err
	}
	return accountadmin.New(database.NewStore(pool)), pool.Close, nil
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func nameOr(s string) string {
	if s == "" {
		return "(unnamed)"
	}
	return s
}
