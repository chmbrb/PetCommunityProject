// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"

	"github.com/google/uuid"

	"github.com/chmbrb/PetCommunityProject/internal/abusecases"
	"github.com/chmbrb/PetCommunityProject/internal/config"
	"github.com/chmbrb/PetCommunityProject/internal/database"
	"github.com/chmbrb/PetCommunityProject/internal/media"
	"github.com/chmbrb/PetCommunityProject/internal/storage"
)

const casesUsage = `usage: petcommunity cases <command>

Abuse imagery cases (ADR 0022). Run where PC_SECRET_KEY is available, e.g.
  docker compose run --rm -T worker cases list

commands:
  list [--status open|reported|closed|all]
      list cases, oldest first (default: open)
  show ID
      the case, the uploader, and the upload, for filling in the report
  export --operator NAME ID > FILE
      decrypt the original upload to standard output, for attaching to the
      report; refuses to write to a terminal, and is recorded in the audit log
  report --operator NAME --reference REF [--reported-at TIME] [--preserve-until TIME] ID
      record that the case was reported (REF: e.g. the CyberTipline report ID);
      evidence is kept for at least a year after the report
  close --operator NAME --reason TEXT ID
      close a case, e.g. a false match; a reported case keeps its evidence
      until its preservation date

TIME is RFC 3339, e.g. 2026-11-16T09:30:00Z. The operator's name goes in the
audit log with every export, report, and close.
`

// errCasesUsage asks run to print the cases usage and exit with status 2.
var errCasesUsage = errors.New("usage")

// cases runs the operator tooling for abuse imagery cases. stdout is where exports
// go, so it's checked for being a terminal before anything is written to it.
func cases(ctx context.Context, cfg config.Config, args []string, stdout io.Writer, stderr io.Writer) error {
	if len(args) == 0 {
		return errCasesUsage
	}
	sub, args := args[0], args[1:]
	fs := flag.NewFlagSet("cases "+sub, flag.ContinueOnError)
	fs.SetOutput(stderr)
	var status, operator, reference, reason, reportedAt, preserveUntil string
	switch sub {
	case "list":
		fs.StringVar(&status, "status", "open", "open, reported, closed, or all")
	case "show":
	case "export":
		fs.StringVar(&operator, "operator", "", "your name, for the audit log")
	case "report":
		fs.StringVar(&operator, "operator", "", "your name, for the audit log")
		fs.StringVar(&reference, "reference", "", "the report's reference, e.g. the CyberTipline report ID")
		fs.StringVar(&reportedAt, "reported-at", "", "when the report was filed (RFC 3339; default: now)")
		fs.StringVar(&preserveUntil, "preserve-until", "", "keep evidence until (RFC 3339; at least a year after the report)")
	case "close":
		fs.StringVar(&operator, "operator", "", "your name, for the audit log")
		fs.StringVar(&reason, "reason", "", "why the case is closed")
	default:
		return errCasesUsage
	}
	if err := fs.Parse(args); err != nil {
		return errCasesUsage
	}
	var id uuid.UUID
	if sub != "list" {
		if fs.NArg() != 1 {
			return errCasesUsage
		}
		var err error
		if id, err = uuid.Parse(fs.Arg(0)); err != nil {
			return fmt.Errorf("%q isn't a case ID", fs.Arg(0))
		}
	} else if fs.NArg() != 0 {
		return errCasesUsage
	}
	// Before connecting, so a mistake costs nothing.
	if sub == "export" && isTerminal(stdout) {
		return errors.New("export writes the original upload to standard output; redirect it to a file, e.g. > evidence.bin")
	}
	reportedTime, err := parseTime("reported-at", reportedAt)
	if err != nil {
		return err
	}
	preserveTime, err := parseTime("preserve-until", preserveUntil)
	if err != nil {
		return err
	}

	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	st := database.NewStore(pool)

	var svc *abusecases.Service
	if sub == "export" {
		// Only an export reads evidence, so only it needs the key and the bucket.
		if err := cfg.RequireSecretKey(); err != nil {
			return err
		}
		if cfg.Storage.EvidenceBucket == "" {
			return errors.New("PC_S3_EVIDENCE_BUCKET isn't set")
		}
		sealer, err := media.NewEvidenceSealer(cfg.SecretKey)
		if err != nil {
			return err
		}
		objects, err := storage.NewS3(cfg.Storage)
		if err != nil {
			return err
		}
		svc = abusecases.New(st, objects.Evidence, sealer)
	} else {
		svc = abusecases.New(st, nil, nil)
	}

	switch sub {
	case "list":
		rows, err := svc.List(ctx, status)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			fmt.Fprintln(stderr, "no cases")
			return nil
		}
		tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tSTATUS\tOPENED\tUPLOADER\tSCANNER\tPRESERVE UNTIL")
		for _, r := range rows {
			fmt.Fprintf(tw, "%s\t%s\t%s\t@%s\t%s\t%s\n", r.ID, r.Status, r.CreatedAt.UTC().Format(time.RFC3339),
				r.Handle, r.Scanner, formatTime(r.PreserveUntil))
		}
		return tw.Flush()
	case "show":
		c, err := svc.Get(ctx, id)
		if err != nil {
			return err
		}
		tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
		for _, line := range [][2]string{
			{"Case", c.ID.String()},
			{"Status", c.Status},
			{"Opened", c.CreatedAt.UTC().Format(time.RFC3339)},
			{"Scanner", c.Scanner},
			{"Match reference", deref(c.MatchReference)},
			{"Report reference", deref(c.ReportReference)},
			{"Reported", formatTime(c.ReportedAt)},
			{"Preserve until", formatTime(c.PreserveUntil)},
			{"Upload", c.MediaID.String()},
			{"Uploaded", c.UploadedAt.UTC().Format(time.RFC3339)},
			{"Upload purpose", c.UploadPurpose},
			{"Upload size", fmt.Sprintf("%d bytes", c.UploadBytes)},
			{"Upload SHA-256", hex.EncodeToString(c.Sha256)},
			{"Uploader", fmt.Sprintf("@%s (%s)", c.Handle, c.DisplayName)},
			{"Actor", c.OwnerActorID.String()},
			{"Account", uuidOrNone(c.AccountID)},
			{"Account email", deref(c.AccountEmail)},
			{"Account status", deref(c.AccountStatus)},
			{"Account created", formatTime(c.AccountCreatedAt)},
		} {
			fmt.Fprintf(tw, "%s:\t%s\n", line[0], line[1])
		}
		return tw.Flush()
	case "export":
		if err := svc.Export(ctx, id, operator, stdout); err != nil {
			return err
		}
		fmt.Fprintln(stderr, "exported; the export is recorded in the audit log")
		return nil
	case "report":
		until, err := svc.Report(ctx, id, reference, reportedTime, preserveTime, operator)
		if err != nil {
			return err
		}
		fmt.Fprintf(stderr, "case %s is reported; its evidence is kept until %s\n", id, until.UTC().Format(time.RFC3339))
		return nil
	default: // close
		if err := svc.Close(ctx, id, reason, operator); err != nil {
			return err
		}
		fmt.Fprintf(stderr, "case %s is closed\n", id)
		return nil
	}
}

// isTerminal reports whether w is a terminal, where binary evidence must never go.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func parseTime(name, v string) (time.Time, error) {
	if v == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}, fmt.Errorf("--%s: use RFC 3339, e.g. 2026-11-16T09:30:00Z", name)
	}
	return t, nil
}

func formatTime(t *time.Time) string {
	if t == nil {
		return "-"
	}
	return t.UTC().Format(time.RFC3339)
}

func deref(s *string) string {
	if s == nil || *s == "" {
		return "-"
	}
	return *s
}

func uuidOrNone(id uuid.NullUUID) string {
	if !id.Valid {
		return "-"
	}
	return id.UUID.String()
}
