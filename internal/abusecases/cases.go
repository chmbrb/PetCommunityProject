// SPDX-License-Identifier: AGPL-3.0-or-later

// Package abusecases is the operator tooling for abuse imagery cases (ADR 0022): listing
// cases, exporting a case's evidence for the legally required report, and recording the
// report. Every action that touches evidence or changes a case is written to the audit
// log, with the operator's name, before it takes effect.
package abusecases

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/chmbrb/PetCommunityProject/internal/database"
	"github.com/chmbrb/PetCommunityProject/internal/media"
	"github.com/chmbrb/PetCommunityProject/internal/storage"
	"github.com/chmbrb/PetCommunityProject/internal/store"
)

var (
	ErrNotFound = errors.New("no such case")
	// ErrWrongStatus means the case isn't in a state the action applies to, such as
	// reporting a case that's already reported.
	ErrWrongStatus = errors.New("the case's status doesn't allow this")
	// ErrEvidenceMismatch means the decrypted evidence isn't the file the case recorded.
	ErrEvidenceMismatch = errors.New("evidence doesn't match the case's SHA-256")
)

// Statuses a case can have; "all" lists every case.
var statuses = map[string]bool{"open": true, "reported": true, "closed": true}

// maxEvidence bounds what's read from the evidence bucket: uploads are limited to 15 MB,
// and sealing adds a few bytes.
const maxEvidence = 32 << 20

// Queries are the database calls the service makes, inside or outside a transaction.
// *store.Queries implements them.
type Queries interface {
	ListAbuseImageryCases(ctx context.Context, status *string) ([]store.ListAbuseImageryCasesRow, error)
	GetAbuseImageryCase(ctx context.Context, id uuid.UUID) (store.GetAbuseImageryCaseRow, error)
	MarkAbuseImageryCaseReported(ctx context.Context, arg store.MarkAbuseImageryCaseReportedParams) (int64, error)
	CloseAbuseImageryCase(ctx context.Context, id uuid.UUID) (int64, error)
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

// Service runs the operator's actions on cases.
type Service struct {
	db       DB
	evidence storage.Bucket
	sealer   *media.EvidenceSealer
	now      func() time.Time
}

// New returns the service for a database, the evidence bucket, and the sealer made from
// PC_SECRET_KEY. Only exporting evidence needs the bucket and sealer.
func New(st *database.Store, evidence storage.Bucket, sealer *media.EvidenceSealer) *Service {
	return newService(storeDB{st}, evidence, sealer, time.Now)
}

func newService(db DB, evidence storage.Bucket, sealer *media.EvidenceSealer, now func() time.Time) *Service {
	return &Service{db: db, evidence: evidence, sealer: sealer, now: now}
}

// List returns cases with the given status ("open", "reported", "closed", or "all"),
// oldest first.
func (s *Service) List(ctx context.Context, status string) ([]store.ListAbuseImageryCasesRow, error) {
	var filter *string
	switch {
	case status == "all":
	case statuses[status]:
		filter = &status
	default:
		return nil, fmt.Errorf("unknown status %q: use open, reported, closed, or all", status)
	}
	return s.db.ListAbuseImageryCases(ctx, filter)
}

// Get returns a case with the details a report asks for.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (store.GetAbuseImageryCaseRow, error) {
	return get(ctx, s.db, id)
}

// Export writes a case's original upload to w, for attaching to the report. The audit
// event is recorded first, so no export goes unrecorded. The file is checked against
// the SHA-256 the case recorded, so what's reported is exactly what was uploaded.
func (s *Service) Export(ctx context.Context, id uuid.UUID, operator string, w io.Writer) error {
	if err := needOperator(operator); err != nil {
		return err
	}
	c, err := get(ctx, s.db, id)
	if err != nil {
		return err
	}
	rc, err := s.evidence.Get(ctx, c.EvidenceKey)
	if err != nil {
		return fmt.Errorf("read evidence: %w", err)
	}
	defer rc.Close()
	sealed, err := io.ReadAll(io.LimitReader(rc, maxEvidence))
	if err != nil {
		return fmt.Errorf("read evidence: %w", err)
	}
	original, err := s.sealer.Open(c.MediaID, sealed)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(original)
	if !bytes.Equal(sum[:], c.Sha256) {
		return ErrEvidenceMismatch
	}
	err = s.db.InTx(ctx, func(q Queries) error {
		return audit(ctx, q, "abuse_imagery.evidence_exported", id, map[string]any{
			"operator": operator, "sha256": hex.EncodeToString(sum[:]),
		})
	})
	if err != nil {
		return err
	}
	_, err = w.Write(original)
	return err
}

// Report records that the case was reported, with the report's reference (such as the
// CyberTipline report ID). The evidence is preserved for a year from reportedAt, the
// legal minimum (18 U.S.C. § 2258A), or until preserveUntil if that's later.
func (s *Service) Report(ctx context.Context, id uuid.UUID, reference string, reportedAt, preserveUntil time.Time, operator string) (time.Time, error) {
	if err := needOperator(operator); err != nil {
		return time.Time{}, err
	}
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return time.Time{}, errors.New("the report's reference is required")
	}
	if reportedAt.IsZero() {
		reportedAt = s.now()
	}
	if reportedAt.After(s.now().Add(5 * time.Minute)) {
		return time.Time{}, errors.New("the report time is in the future")
	}
	minimum := reportedAt.AddDate(1, 0, 0)
	if preserveUntil.Before(minimum) {
		preserveUntil = minimum
	}
	err := s.db.InTx(ctx, func(q Queries) error {
		n, err := q.MarkAbuseImageryCaseReported(ctx, store.MarkAbuseImageryCaseReportedParams{
			ID: id, ReportReference: &reference, ReportedAt: &reportedAt, PreserveUntil: &preserveUntil,
		})
		if err != nil {
			return err
		}
		if n == 0 {
			return statusError(ctx, q, id, "only an open case can be reported")
		}
		return audit(ctx, q, "abuse_imagery.reported", id, map[string]any{
			"operator": operator, "report_reference": reference, "preserve_until": preserveUntil,
		})
	})
	if err != nil {
		return time.Time{}, err
	}
	return preserveUntil, nil
}

// Close closes an open case (such as a false match) or a reported one. A reported case
// keeps its evidence until preserve_until either way.
func (s *Service) Close(ctx context.Context, id uuid.UUID, reason, operator string) error {
	if err := needOperator(operator); err != nil {
		return err
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return errors.New("a reason is required")
	}
	return s.db.InTx(ctx, func(q Queries) error {
		c, err := get(ctx, q, id)
		if err != nil {
			return err
		}
		n, err := q.CloseAbuseImageryCase(ctx, id)
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("%w: the case is already %s", ErrWrongStatus, c.Status)
		}
		return audit(ctx, q, "abuse_imagery.closed", id, map[string]any{
			"operator": operator, "reason": reason, "previous_status": c.Status,
		})
	})
}

func get(ctx context.Context, q Queries, id uuid.UUID) (store.GetAbuseImageryCaseRow, error) {
	c, err := q.GetAbuseImageryCase(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

// statusError explains why an update matched no rows: the case is missing, or has
// another status.
func statusError(ctx context.Context, q Queries, id uuid.UUID, why string) error {
	c, err := get(ctx, q, id)
	if err != nil {
		return err
	}
	return fmt.Errorf("%w: %s, and this case is %s", ErrWrongStatus, why, c.Status)
}

func needOperator(operator string) error {
	if strings.TrimSpace(operator) == "" {
		return errors.New("the operator's name is required, for the audit log")
	}
	return nil
}

// audit records an operator's action. Operators act from the command line, not as a
// signed-in account, so the audit event has no account and names them in its details.
func audit(ctx context.Context, q Queries, action string, caseID uuid.UUID, details map[string]any) error {
	raw, err := json.Marshal(details)
	if err != nil {
		return err
	}
	return q.InsertAuditEvent(ctx, store.InsertAuditEventParams{
		ID: uuid.Must(uuid.NewV7()), Action: action, TargetType: "abuse_imagery_case", TargetID: caseID, Details: raw,
	})
}
