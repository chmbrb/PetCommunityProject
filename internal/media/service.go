// SPDX-License-Identifier: AGPL-3.0-or-later

package media

import (
	"bytes"
	"context"
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
	db      *database.Store
	storage *storage.Storage
	queue   JobQueue
	scanner Scanner
	logger  *slog.Logger
}

// NewService returns a media service. The API needs queue; the worker needs scanner.
func NewService(db *database.Store, st *storage.Storage, queue JobQueue, scanner Scanner, logger *slog.Logger) *Service {
	if scanner == nil {
		scanner = NoScanner{}
	}
	return &Service{db: db, storage: st, queue: queue, scanner: scanner, logger: logger}
}

func quarantineKey(id uuid.UUID) string { return "uploads/" + id.String() }

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

	verdict, err := s.scanner.Scan(ctx, data)
	if err != nil {
		return fmt.Errorf("scan upload: %w", err)
	}
	if verdict == Match {
		// Never published or viewed; the original stays in quarantine as evidence for
		// the legally required report (THREAT_MODEL.md C1). No content is logged.
		if _, err := s.db.MarkMediaQuarantined(ctx, id); err != nil {
			return err
		}
		s.logger.ErrorContext(ctx, "upload matched known abuse imagery; quarantined for reporting",
			"media_id", id, "owner_actor_id", row.OwnerActorID)
		return nil
	}

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
