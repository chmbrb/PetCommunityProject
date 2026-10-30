// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"net/http"
)

type signInLinkRequest struct {
	Email string `json:"email"`
}

type tokenRequest struct {
	Token string `json:"token"`
	// Client is "web" (session cookie, the default) or "mobile" (token in the response).
	Client string `json:"client"`
}

type acceptedResponse struct {
	Status string `json:"status"`
}

// handleSignInLinkRequest emails a sign-in link. The answer is the same whether or not
// the address has an account, so it can't be used to discover accounts.
func (s *Server) handleSignInLinkRequest(w http.ResponseWriter, r *http.Request) {
	var req signInLinkRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.auth.RequestSignInLink(r.Context(), req.Email); err != nil {
		s.writeAuthError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, acceptedResponse{Status: "sent_if_registered"})
}

// handleSignInLinkFinish signs in with the token from a sign-in email.
func (s *Server) handleSignInLinkFinish(w http.ResponseWriter, r *http.Request) {
	var req tokenRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	client, ok := parseClient(w, req.Client)
	if !ok {
		return
	}
	sess, err := s.auth.FinishSignInLink(r.Context(), req.Token, client)
	if err != nil {
		s.writeAuthError(w, r, err)
		return
	}
	s.startSession(w, r, http.StatusOK, sess, client)
}

// handleVerifyEmail confirms an email address with the token from a verification email.
func (s *Server) handleVerifyEmail(w http.ResponseWriter, r *http.Request) {
	var req tokenRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.auth.VerifyEmail(r.Context(), req.Token); err != nil {
		s.writeAuthError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleResendVerification sends another verification email to the signed-in account.
func (s *Server) handleResendVerification(w http.ResponseWriter, r *http.Request) {
	p, ok := principalFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "not signed in")
		return
	}
	if err := s.auth.ResendVerification(r.Context(), p.AccountID); err != nil {
		s.writeAuthError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, acceptedResponse{Status: "sent"})
}
