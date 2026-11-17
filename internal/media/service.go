// SPDX-License-Identifier: AGPL-3.0-or-later

package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/chmbrb/PetCommunityProject/internal/database"
	"github.com/chmbrb/PetCommunityProject/internal/storage"
	"github.com/chmbrb/PetCommunityProject/internal/store"
)

// Purposes an upload can be for, matching media_assets.purpose.
const PurposeAvatar = "avatar"

// QueueMedia is the job queue that processes uploads.
const QueueMedia = "media"

var (
	ErrNotFound = errors.New("not found")
	ErrNotReady = errors.New("the image is still being processed or was rejected")
)

// JobQueue inserts background jobs. *river.Client satisfies it.
type JobQueue interface {
	InsertTx(ctx context.Context, tx pgx.Tx, args river.JobArgs, opts *river.InsertOpts) (*rivertype.JobInsertResult, error)
}

// ProcessJob asks the worker to process one upload.
type ProcessJob struct {
	MediaID uuid.UUID `json:"media_id"`
}

// Kind identifies the job type to River.
func (ProcessJob) Kind() string { return "process_media" }

// InsertOpts puts uploads on their own queue, so image work can't delay email.
func (ProcessJob) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: QueueMedia, MaxAttempts: 3}
}

// AbuseAlertJob tells the operator that a match opened an abuse imagery case
// (ADR 0022), so the report isn't left waiting for someone to read the logs. It holds
// only the case: nothing about the upload or its owner goes into an email.
type AbuseAlertJob struct {
	CaseID   uuid.UUID `json:"case_id"`
	OpenedAt time.Time `json:"opened_at"`
}

// Kind identifies the job type to River.
func (AbuseAlertJob) Kind() string { return "abuse_imagery_alert" }

// InsertOpts retries for a long time: an alert that never arrives leaves a report
// unfiled.
func (AbuseAlertJob) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 25}
}

// Asset is an uploaded image as its owner sees it.
type Asset struct {
	ID              uuid.UUID
	Purpose         string
	Status          string
	Width, Height   int
	Variants        []PublicVariant
	RejectionReason string
	CreatedAt       time.Time
}

// PublicVariant is a published image size.
type PublicVariant struct {
	Name        string `json:"name"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	ContentType string `json:"content_type"`
	URL         string `json:"url"`
}

// storedVariant is a variant as recorded in media_assets.variants.
type storedVariant struct {
	Name        string `json:"name"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	ContentType string `json:"content_type"`
	Key         string `json:"key"`
}

// Service accepts uploads (API) and processes them (worker).
type Service struct {
	db       *database.Store
	storage  *storage.Storage
	queue    JobQueue
	scanning Scanning
	logger   *slog.Logger
}

// Scanning is what the worker needs to check uploads for known abuse imagery and to
// preserve the evidence when one matches (ADR 0022).
type Scanning struct {
	// Scanner checks each upload. Nil means NoScanner.
	Scanner Scanner
	// Evidence encrypts the originals of matching uploads. Required with any Scanner
	// other than NoScanner.
	Evidence *EvidenceSealer
}

// NewService returns a media service. The API needs queue; the worker needs scanning.
func NewService(db *database.Store, st *storage.Storage, queue JobQueue, scanning Scanning, logger *slog.Logger) *Service {
	if scanning.Scanner == nil {
		scanning.Scanner = NoScanner{}
	}
	return &Service{db: db, storage: st, queue: queue, scanning: scanning, logger: logger}
}

func quarantineKey(id uuid.UUID) string { return "uploads/" + id.String() }

func evidenceKey(id uuid.UUID) string { return "media/" + id.String() }

// Upload stores a raw upload in quarantine and queues it for processing. Only the
// format sniff happens here; everything else is the worker's job, away from the API.
func (s *Service) Upload(ctx context.Context, accountID uuid.UUID, purpose string, body io.Reader, size int64) (Asset, error) {
	if purpose != PurposeAvatar {
		return Asset{}, fmt.Errorf("%w: unknown purpose", ErrUnsupported)
	}
	if size <= 0 || size > MaxUploadBytes {
		return Asset{}, ErrTooLarge
	}
	head := make([]byte, min(int(size), SniffLen))
	if _, err := io.ReadFull(body, head); err != nil {
		return Asset{}, fmt.Errorf("%w: %w", ErrUndecodable, err)
	}
	if _, ok := Sniff(head); !ok {
		return Asset{}, ErrUnsupported
	}
	actorID, err := s.db.GetPersonActorID(ctx, uuid.NullUUID{UUID: accountID, Valid: true})
	if err != nil {
		return Asset{}, err
	}

	id := uuid.Must(uuid.NewV7())
	// The stored type is deliberately generic: nothing about a raw upload is trusted.
	err = s.storage.Quarantine.Put(ctx, quarantineKey(id), io.MultiReader(bytes.NewReader(head), body), size,
		storage.PutOptions{ContentType: "application/octet-stream"})
	if err != nil {
		return Asset{}, err
	}
	err = s.db.InTx(ctx, func(tx pgx.Tx, q *store.Queries) error {
		if err := q.CreateMediaAsset(ctx, store.CreateMediaAssetParams{
			ID: id, OwnerActorID: actorID, Purpose: purpose, UploadBytes: size,
		}); err != nil {
			return err
		}
		_, err := s.queue.InsertTx(ctx, tx, ProcessJob{MediaID: id}, nil)
		return err
	})
	if err != nil {
		_ = s.storage.Quarantine.Delete(context.WithoutCancel(ctx), quarantineKey(id))
		return Asset{}, err
	}
	return Asset{ID: id, Purpose: purpose, Status: "pending", CreatedAt: time.Now()}, nil
}

// Get returns an upload, only to the account that owns it.
func (s *Service) Get(ctx context.Context, accountID, id uuid.UUID) (Asset, error) {
	actorID, err := s.db.GetPersonActorID(ctx, uuid.NullUUID{UUID: accountID, Valid: true})
	if err != nil {
		return Asset{}, err
	}
	row, err := s.db.GetMediaAsset(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && row.OwnerActorID != actorID) {
		return Asset{}, ErrNotFound
	}
	if err != nil {
		return Asset{}, err
	}
	a := Asset{ID: row.ID, Purpose: row.Purpose, Status: row.Status, CreatedAt: row.CreatedAt}
	if row.Width != nil && row.Height != nil {
		a.Width, a.Height = int(*row.Width), int(*row.Height)
	}
	if row.RejectionReason != nil {
		a.RejectionReason = *row.RejectionReason
	}
	if a.Variants, err = s.PublicVariants(row.Variants); err != nil {
		return Asset{}, err
	}
	return a, nil
}

// PublicVariants turns stored variants into public URLs.
func (s *Service) PublicVariants(raw []byte) ([]PublicVariant, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var stored []storedVariant
	if err := json.Unmarshal(raw, &stored); err != nil {
		return nil, err
	}
	out := make([]PublicVariant, 0, len(stored))
	for _, v := range stored {
		out = append(out, PublicVariant{
			Name: v.Name, Width: v.Width, Height: v.Height, ContentType: v.ContentType, URL: s.storage.MediaURL(v.Key),
		})
	}
	return out, nil
}

// SetAvatar makes a processed avatar upload the account's profile photo. uuid.Nil
// removes the profile photo.
func (s *Service) SetAvatar(ctx context.Context, accountID, mediaID uuid.UUID) error {
	actorID, err := s.db.GetPersonActorID(ctx, uuid.NullUUID{UUID: accountID, Valid: true})
	if err != nil {
		return err
	}
	if mediaID == uuid.Nil {
		return s.db.SetActorAvatar(ctx, store.SetActorAvatarParams{ID: actorID})
	}
	row, err := s.db.GetMediaAsset(ctx, mediaID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (row.OwnerActorID != actorID || row.Purpose != PurposeAvatar)) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if row.Status != "ready" {
		return ErrNotReady
	}
	return s.db.SetActorAvatar(ctx, store.SetActorAvatarParams{ID: actorID, AvatarMediaID: uuid.NullUUID{UUID: mediaID, Valid: true}})
}

// Process runs the pipeline for one upload (ADR 0011): scan, decode and re-encode,
// hash, look for the same photo from another account, publish, and delete the raw
// upload. Returning an error retries the job; a rejected image isn't an error.
func (s *Service) Process(ctx context.Context, id uuid.UUID) error {
	row, err := s.db.GetMediaAsset(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if row.Status != "pending" {
		return nil // already handled
	}

	rc, err := s.storage.Quarantine.Get(ctx, quarantineKey(id))
	if errors.Is(err, storage.ErrNotFound) {
		return s.reject(ctx, id, "the upload was lost; please try again")
	}
	if err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(rc, MaxUploadBytes+1))
	_ = rc.Close()
	if err != nil {
		return err
	}

	// A scanner error retries the job; if it never succeeds, the upload is rejected by
	// CleanupStale. Nothing is published unscanned when a scanner is configured.
	scan, err := s.scanning.Scanner.Scan(ctx, data)
	if err != nil {
		return fmt.Errorf("scan upload: %w", err)
	}
	if scan.Verdict == Match {
		return s.preserveMatch(ctx, row.OwnerActorID, id, data, scan)
	}
	verdict := scan.Verdict

	res, err := Process(data)
	if errors.Is(err, ErrUnsupported) || errors.Is(err, ErrTooLarge) || errors.Is(err, ErrUndecodable) {
		s.logger.InfoContext(ctx, "upload rejected", "media_id", id, "error", err)
		return s.reject(ctx, id, rejectionMessage(err))
	}
	if err != nil {
		return err
	}

	phash := int64(res.PHash) //nolint:gosec // G115: stored as bit pattern; compared bitwise
	var similar uuid.NullUUID
	if other, err := s.db.FindSimilarMedia(ctx, store.FindSimilarMediaParams{OwnerActorID: row.OwnerActorID, Phash: phash}); err == nil {
		similar = uuid.NullUUID{UUID: other, Valid: true}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}

	stored := make([]storedVariant, 0, len(res.Variants))
	for _, v := range res.Variants {
		key := id.String() + "/" + v.Name
		if err := s.storage.Media.Put(ctx, key, bytes.NewReader(v.Data), int64(len(v.Data)), storage.PutOptions{
			ContentType:  v.ContentType,
			CacheControl: "public, max-age=31536000, immutable",
		}); err != nil {
			return err
		}
		stored = append(stored, storedVariant{Name: v.Name, Width: v.Width, Height: v.Height, ContentType: v.ContentType, Key: key})
	}
	variantsJSON, err := json.Marshal(stored)
	if err != nil {
		return err
	}
	format, scanned := string(res.Format), string(verdict)
	w, h := int32(res.Width), int32(res.Height) //nolint:gosec // G115: bounded by MaxPixels
	if _, err := s.db.MarkMediaReady(ctx, store.MarkMediaReadyParams{
		ID: id, Format: &format, Width: &w, Height: &h, Sha256: res.SHA256[:], Phash: &phash,
		Variants: variantsJSON, ScanVerdict: &scanned, SimilarTo: similar,
	}); err != nil {
		return err
	}
	if err := s.storage.Quarantine.Delete(ctx, quarantineKey(id)); err != nil {
		s.logger.WarnContext(ctx, "delete processed upload from quarantine", "media_id", id, "error", err)
	}
	if similar.Valid {
		s.logger.WarnContext(ctx, "upload looks like another account's photo", "media_id", id, "similar_to", similar.UUID)
	}
	s.logger.InfoContext(ctx, "upload processed", "media_id", id, "variants", len(stored))
	return nil
}

// preserveMatch handles an upload that matched known abuse imagery (ADR 0022). The
// original is never decoded, published, or viewed: it's encrypted into the evidence
// bucket, and in one transaction the upload is quarantined, a case is opened for the
// legally required report, the owner's account is suspended and signed out, and
// each step is recorded in the audit log. Image content is never logged.
func (s *Service) preserveMatch(ctx context.Context, ownerActorID, id uuid.UUID, original []byte, scan ScanResult) error {
	if s.scanning.Evidence == nil || s.storage.Evidence == nil {
		return errors.New("an upload matched known abuse imagery, but no evidence storage is configured")
	}
	if s.queue == nil {
		return errors.New("an upload matched known abuse imagery, but there's no job queue to alert the operator")
	}
	sealed, err := s.scanning.Evidence.Seal(id, original)
	if err != nil {
		return err
	}
	// Stored under the upload's ID, so a retried job overwrites rather than duplicates.
	key := evidenceKey(id)
	if err := s.storage.Evidence.Put(ctx, key, bytes.NewReader(sealed), int64(len(sealed)),
		storage.PutOptions{ContentType: "application/octet-stream"}); err != nil {
		return fmt.Errorf("preserve evidence: %w", err)
	}

	sum := sha256.Sum256(original)
	caseID := uuid.Must(uuid.NewV7())
	var accountID uuid.NullUUID
	err = s.db.InTx(ctx, func(tx pgx.Tx, q *store.Queries) error {
		if accountID, err = q.GetActorAccountID(ctx, ownerActorID); err != nil {
			return err
		}
		var ref *string
		if scan.Reference != "" {
			ref = &scan.Reference
		}
		created, err := q.CreateAbuseImageryCase(ctx, store.CreateAbuseImageryCaseParams{
			ID: caseID, MediaID: id, OwnerActorID: ownerActorID, AccountID: accountID,
			Scanner: s.scanning.Scanner.Name(), MatchReference: ref, Sha256: sum[:], EvidenceKey: key,
		})
		if err != nil {
			return err
		}
		if created == 0 {
			return nil // an earlier attempt already did all of this
		}
		if _, err := q.MarkMediaQuarantined(ctx, id); err != nil {
			return err
		}
		// In the same transaction as the case, so every case gets exactly one alert.
		if _, err := s.queue.InsertTx(ctx, tx, AbuseAlertJob{CaseID: caseID, OpenedAt: time.Now().UTC()}, nil); err != nil {
			return fmt.Errorf("queue the operator alert: %w", err)
		}
		if err := audit(ctx, q, "abuse_imagery.matched", "media", id, map[string]any{
			"case_id": caseID, "scanner": s.scanning.Scanner.Name(), "owner_actor_id": ownerActorID,
		}); err != nil {
			return err
		}
		if !accountID.Valid {
			return nil
		}
		if _, err := q.SuspendAccount(ctx, accountID.UUID); err != nil {
			return err
		}
		revoked, err := q.RevokeAllAccountSessions(ctx, accountID.UUID)
		if err != nil {
			return err
		}
		return audit(ctx, q, "account.suspended", "account", accountID.UUID, map[string]any{
			"reason": "abuse_imagery_match", "case_id": caseID, "sessions_revoked": revoked,
		})
	})
	if err != nil {
		return err
	}
	// The only copy left is the encrypted one; the raw file would otherwise wait for
	// the quarantine bucket's lifecycle rule.
	if err := s.storage.Quarantine.Delete(ctx, quarantineKey(id)); err != nil {
		s.logger.WarnContext(ctx, "delete matched upload from quarantine", "media_id", id, "error", err)
	}
	s.logger.ErrorContext(ctx, "upload matched known abuse imagery; evidence preserved and account suspended for reporting",
		"media_id", id, "owner_actor_id", ownerActorID, "account_id", accountID.UUID)
	return nil
}

// audit appends an event done by the system (not by a signed-in account) to the audit log.
func audit(ctx context.Context, q *store.Queries, action, targetType string, targetID uuid.UUID, details map[string]any) error {
	raw, err := json.Marshal(details)
	if err != nil {
		return err
	}
	return q.InsertAuditEvent(ctx, store.InsertAuditEventParams{
		ID: uuid.Must(uuid.NewV7()), Action: action, TargetType: targetType, TargetID: targetID, Details: raw,
	})
}

// CleanupStale rejects uploads still pending after an hour (their job failed for
// good) and removes their raw files.
func (s *Service) CleanupStale(ctx context.Context) (int, error) {
	ids, err := s.db.ListStalePendingMedia(ctx)
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		if err := s.reject(ctx, id, "processing didn't finish; please try again"); err != nil {
			return 0, err
		}
	}
	return len(ids), nil
}

func (s *Service) reject(ctx context.Context, id uuid.UUID, reason string) error {
	if _, err := s.db.MarkMediaRejected(ctx, store.MarkMediaRejectedParams{ID: id, RejectionReason: &reason}); err != nil {
		return err
	}
	if err := s.storage.Quarantine.Delete(ctx, quarantineKey(id)); err != nil && !errors.Is(err, storage.ErrNotFound) {
		s.logger.WarnContext(ctx, "delete rejected upload from quarantine", "media_id", id, "error", err)
	}
	return nil
}

func rejectionMessage(err error) string {
	switch {
	case errors.Is(err, ErrUnsupported):
		return ErrUnsupported.Error()
	case errors.Is(err, ErrTooLarge):
		return "the image is too large; use one under 40 megapixels and 15 MB"
	default:
		return ErrUndecodable.Error()
	}
}
