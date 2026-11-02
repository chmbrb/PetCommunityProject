// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"net/http"
)

type passwordSignInRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	// Code is the emailed code, when signing in from a new device.
	Code string `json:"code"`
	// Client is "web" (session cookie, the default) or "mobile" (token in the response).
	Client string `json:"client"`
}

type setPasswordRequest struct {
	Password string `json:"password"`
}

// handlePasswordSignIn signs in with an email address and password. From an
// unrecognized device it answers 401 email_code_required and emails a code; the
// client repeats the request with the code.
func (s *Server) handlePasswordSignIn(w http.ResponseWriter, r *http.Request) {
	var req passwordSignInRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	client, ok := parseClient(w, req.Client)
	if !ok {
		return
	}
	sess, err := s.auth.PasswordSignIn(r.Context(), req.Email, req.Password, req.Code, client)
	if err != nil {
		s.writeAuthError(w, r, err)
		return
	}
	s.startSession(w, r, http.StatusOK, sess, client)
}

// handleSetPassword adds or changes the signed-in account's password.
func (s *Server) handleSetPassword(w http.ResponseWriter, r *http.Request) {
	p, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	var req setPasswordRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.auth.SetPassword(r.Context(), p, req.Password); err != nil {
		s.writeAuthError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleRemovePassword removes the signed-in account's password.
func (s *Server) handleRemovePassword(w http.ResponseWriter, r *http.Request) {
	p, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	if err := s.auth.RemovePassword(r.Context(), p); err != nil {
		s.writeAuthError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
