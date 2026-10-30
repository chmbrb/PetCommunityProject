// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/chmbrb/PetCommunityProject/internal/auth"
)

type passkeyJSON struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	Synced     bool       `json:"synced"`
}

func toPasskeyJSON(p auth.PasskeyInfo) passkeyJSON {
	return passkeyJSON{ID: p.ID, Name: p.Name, CreatedAt: p.CreatedAt, LastUsedAt: p.LastUsedAt, Synced: p.Synced}
}

type sessionInfoJSON struct {
	ID         uuid.UUID `json:"id"`
	Client     string    `json:"client"`
	UserAgent  string    `json:"user_agent"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	Current    bool      `json:"current"`
}

type addPasskeyFinishRequest struct {
	ChallengeID uuid.UUID       `json:"challenge_id"`
	Credential  json.RawMessage `json:"credential"`
	Name        string          `json:"name"`
}

type renamePasskeyRequest struct {
	Name string `json:"name"`
}

// requirePrincipal returns the signed-in principal, or writes 401 and returns false.
func requirePrincipal(w http.ResponseWriter, r *http.Request) (auth.Principal, bool) {
	p, ok := principalFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "not signed in")
	}
	return p, ok
}

// pathID parses the {id} path segment, writing 404 if it isn't a UUID.
func pathID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return uuid.Nil, false
	}
	return id, true
}

func (s *Server) handleListPasskeys(w http.ResponseWriter, r *http.Request) {
	p, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	passkeys, err := s.auth.ListPasskeys(r.Context(), p.AccountID)
	if err != nil {
		s.writeAuthError(w, r, err)
		return
	}
	out := make([]passkeyJSON, 0, len(passkeys))
	for _, pk := range passkeys {
		out = append(out, toPasskeyJSON(pk))
	}
	writeJSON(w, http.StatusOK, map[string]any{"passkeys": out})
}

func (s *Server) handleAddPasskeyBegin(w http.ResponseWriter, r *http.Request) {
	p, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	id, options, err := s.auth.BeginAddPasskey(r.Context(), p)
	if err != nil {
		s.writeAuthError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, beginResponse{ChallengeID: id, Options: options})
}

func (s *Server) handleAddPasskeyFinish(w http.ResponseWriter, r *http.Request) {
	p, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	var req addPasskeyFinishRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	pk, err := s.auth.FinishAddPasskey(r.Context(), p, req.ChallengeID, req.Credential, req.Name)
	if err != nil {
		s.writeAuthError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toPasskeyJSON(pk))
}

func (s *Server) handleRenamePasskey(w http.ResponseWriter, r *http.Request) {
	p, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req renamePasskeyRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.auth.RenamePasskey(r.Context(), p, id, req.Name); err != nil {
		s.writeAuthError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRemovePasskey(w http.ResponseWriter, r *http.Request) {
	p, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.auth.RemovePasskey(r.Context(), p, id); err != nil {
		s.writeAuthError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	p, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	sessions, err := s.auth.ListSessions(r.Context(), p)
	if err != nil {
		s.writeAuthError(w, r, err)
		return
	}
	out := make([]sessionInfoJSON, 0, len(sessions))
	for _, si := range sessions {
		out = append(out, sessionInfoJSON{
			ID: si.ID, Client: string(si.Client), UserAgent: si.UserAgent,
			CreatedAt: si.CreatedAt, LastSeenAt: si.LastSeenAt, Current: si.Current,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": out})
}

func (s *Server) handleRevokeSession(w http.ResponseWriter, r *http.Request) {
	p, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.auth.RevokeSession(r.Context(), p, id); err != nil {
		s.writeAuthError(w, r, err)
		return
	}
	if id == p.SessionID {
		s.clearSessionCookie(w)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRevokeOtherSessions(w http.ResponseWriter, r *http.Request) {
	p, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	n, err := s.auth.RevokeOtherSessions(r.Context(), p)
	if err != nil {
		s.writeAuthError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"revoked": n})
}
