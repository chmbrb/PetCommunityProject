// SPDX-License-Identifier: AGPL-3.0-or-later

// Package server implements the HTTP API (`/api/v1`) and health endpoints.
package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/chmbrb/PetCommunityProject/internal/config"
	"github.com/chmbrb/PetCommunityProject/internal/store"
	"github.com/chmbrb/PetCommunityProject/internal/version"
)

// maxRequestBody limits API request bodies. Media uploads will get their own limit.
const maxRequestBody = 1 << 20

// Store is the data access the server needs.
type Store interface {
	Ping(ctx context.Context) error
	ListFeatureFlags(ctx context.Context) ([]store.ListFeatureFlagsRow, error)
}

// Server holds the HTTP handlers' dependencies.
type Server struct {
	cfg    config.Config
	store  Store
	auth   AuthService
	limits limiters
	logger *slog.Logger
}

// New returns the application's HTTP handler.
func New(cfg config.Config, st Store, authSvc AuthService, logger *slog.Logger) http.Handler {
	s := &Server{cfg: cfg, store: st, auth: authSvc, limits: newLimiters(cfg.RateLimits), logger: logger}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /readyz", s.handleReady)
	mux.HandleFunc("GET /api/v1/instance", s.handleInstance)

	mux.HandleFunc("POST /api/v1/auth/signup/begin", s.handleSignupBegin)
	mux.HandleFunc("POST /api/v1/auth/signup/finish", s.handleSignupFinish)
	mux.HandleFunc("POST /api/v1/auth/login/begin", s.handleLoginBegin)
	mux.HandleFunc("POST /api/v1/auth/login/finish", s.handleLoginFinish)
	mux.HandleFunc("POST /api/v1/auth/logout", s.handleLogout)
	mux.HandleFunc("GET /api/v1/me", s.handleMe)

	return chain(mux,
		s.requestID,
		s.accessLog,
		s.recoverPanics,
		securityHeaders,
		nativeAppCORS,
		s.rateLimit,
		crossOriginProtection(),
		limitBody(maxRequestBody),
		jsonRoutingErrors,
		s.authenticate,
	)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ping(r.Context()); err != nil {
		s.logger.ErrorContext(r.Context(), "readiness check failed", "error", err)
		writeError(w, http.StatusServiceUnavailable, "unavailable", "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// instanceResponse lets web and mobile clients adapt to an instance (ADR 0018).
type instanceResponse struct {
	Name       string          `json:"name"`
	Domain     string          `json:"domain"`
	Version    string          `json:"version"`
	APIVersion string          `json:"api_version"`
	SourceURL  string          `json:"source_url"`
	Features   map[string]bool `json:"features"`
}

func (s *Server) handleInstance(w http.ResponseWriter, r *http.Request) {
	flags, err := s.store.ListFeatureFlags(r.Context())
	if err != nil {
		s.logger.ErrorContext(r.Context(), "list feature flags", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "internal error")
		return
	}
	features := make(map[string]bool, len(flags))
	for _, f := range flags {
		features[f.Flag] = f.Enabled
	}
	writeJSON(w, http.StatusOK, instanceResponse{
		Name:       s.cfg.InstanceName,
		Domain:     s.cfg.InstanceDomain,
		Version:    version.Version,
		APIVersion: "v1",
		SourceURL:  s.cfg.SourceURL,
		Features:   features,
	})
}

type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	// Field names the invalid input field, when there is one.
	Field string `json:"field,omitempty"`
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorBody{Error: errorDetail{Code: code, Message: message}})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
