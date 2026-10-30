// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/chmbrb/PetCommunityProject/internal/media"
)

// uploadPath receives raw image uploads. It's exempt from the general body limit and
// has its own (media.MaxUploadBytes).
const uploadPath = "/api/v1/media"

// MediaService is the upload functionality the server needs (media.Service).
type MediaService interface {
	Upload(ctx context.Context, accountID uuid.UUID, purpose string, body io.Reader, size int64) (media.Asset, error)
	Get(ctx context.Context, accountID, id uuid.UUID) (media.Asset, error)
	SetAvatar(ctx context.Context, accountID, mediaID uuid.UUID) error
	PublicVariants(raw []byte) ([]media.PublicVariant, error)
}

type assetJSON struct {
	ID              uuid.UUID             `json:"id"`
	Purpose         string                `json:"purpose"`
	Status          string                `json:"status"`
	Width           int                   `json:"width,omitempty"`
	Height          int                   `json:"height,omitempty"`
	Variants        []media.PublicVariant `json:"variants"`
	RejectionReason string                `json:"rejection_reason,omitempty"`
	CreatedAt       time.Time             `json:"created_at"`
}

func toAssetJSON(a media.Asset) assetJSON {
	variants := a.Variants
	if variants == nil {
		variants = []media.PublicVariant{}
	}
	return assetJSON{
		ID: a.ID, Purpose: a.Purpose, Status: a.Status, Width: a.Width, Height: a.Height,
		Variants: variants, RejectionReason: a.RejectionReason, CreatedAt: a.CreatedAt,
	}
}

// handleUpload accepts a raw image body (POST /api/v1/media?purpose=avatar). The image
// is processed in the background; poll GET /api/v1/media/{id} for the result.
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	p, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	// Until an abuse-imagery scanning service is set up (ADR 0011), only accounts
	// with a confirmed email can upload.
	me, err := s.auth.Me(r.Context(), p.AccountID)
	if err != nil {
		s.writeAuthError(w, r, err)
		return
	}
	if me.EmailVerifiedAt == nil {
		writeError(w, http.StatusForbidden, "email_unverified", "confirm your email address before uploading photos")
		return
	}
	if r.ContentLength <= 0 {
		writeError(w, http.StatusLengthRequired, "length_required", "send the image with a Content-Length")
		return
	}
	if r.ContentLength > media.MaxUploadBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", "the image is larger than 15 MB")
		return
	}
	body := http.MaxBytesReader(w, r.Body, media.MaxUploadBytes)
	asset, err := s.media.Upload(r.Context(), p.AccountID, r.URL.Query().Get("purpose"), body, r.ContentLength)
	if err != nil {
		s.writeMediaError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, toAssetJSON(asset))
}

func (s *Server) handleGetMedia(w http.ResponseWriter, r *http.Request) {
	p, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	asset, err := s.media.Get(r.Context(), p.AccountID, id)
	if err != nil {
		s.writeMediaError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAssetJSON(asset))
}

type setAvatarRequest struct {
	MediaID uuid.UUID `json:"media_id"`
}

func (s *Server) handleSetAvatar(w http.ResponseWriter, r *http.Request) {
	p, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	var req setAvatarRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.MediaID == uuid.Nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "media_id is required")
		return
	}
	if err := s.media.SetAvatar(r.Context(), p.AccountID, req.MediaID); err != nil {
		s.writeMediaError(w, r, err)
		return
	}
	s.handleMe(w, r)
}

func (s *Server) handleRemoveAvatar(w http.ResponseWriter, r *http.Request) {
	p, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	if err := s.media.SetAvatar(r.Context(), p.AccountID, uuid.Nil); err != nil {
		s.writeMediaError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) writeMediaError(w http.ResponseWriter, r *http.Request, err error) {
	var tooLarge *http.MaxBytesError
	switch {
	case errors.Is(err, media.ErrUnsupported):
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", media.ErrUnsupported.Error())
	case errors.Is(err, media.ErrTooLarge), errors.As(err, &tooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", "the image is larger than 15 MB")
	case errors.Is(err, media.ErrUndecodable):
		writeError(w, http.StatusBadRequest, "undecodable", media.ErrUndecodable.Error())
	case errors.Is(err, media.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "not found")
	case errors.Is(err, media.ErrNotReady):
		writeError(w, http.StatusConflict, "not_ready", media.ErrNotReady.Error())
	default:
		s.logger.ErrorContext(r.Context(), "media request failed", "error", err, "path", r.URL.Path)
		writeError(w, http.StatusInternalServerError, "internal", "internal error")
	}
}
