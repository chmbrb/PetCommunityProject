// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"context"
	"net/http"

	"github.com/chmbrb/PetCommunityProject/internal/auth"
)

type signInLinkRequest struct {
	Email string `json:"email"`
}

type tokenRequest struct {
	Token string `json:"token"`
}

// emailProofRequest finishes a sign-in or sign-up with what came in the email: the
// link's token, or the address and the code.
type emailProofRequest struct {
	Token string `json:"token"`
	Email string `json:"email"`
	Code  string `json:"code"`
	// Password optionally sets a password when finishing a sign-up.
	Password string `json:"password"`
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

// handleSignInLinkFinish signs in with the link or code from a sign-in email.
func (s *Server) handleSignInLinkFinish(w http.ResponseWriter, r *http.Request) {
	s.finishWithEmail(w, r, http.StatusOK, false, func(ctx context.Context, proof auth.EmailProof, _ string, client auth.Client) (auth.Session, error) {
		return s.auth.FinishEmailSignIn(ctx, proof, client)
	})
}

// handleEmailSignupRequest checks the sign-up details and emails a link and code that
// create the account.
func (s *Server) handleEmailSignupRequest(w http.ResponseWriter, r *http.Request) {
	var in auth.SignupInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if err := s.auth.RequestEmailSignup(r.Context(), in); err != nil {
		s.writeAuthError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, acceptedResponse{Status: "sent"})
}

// handleEmailSignupFinish creates the account with the link or code from a sign-up email.
func (s *Server) handleEmailSignupFinish(w http.ResponseWriter, r *http.Request) {
	s.finishWithEmail(w, r, http.StatusCreated, true, s.auth.FinishEmailSignup)
}

// finishWithEmail decodes an emailProofRequest and runs fn. allowPassword says whether
// the request may set a password.
func (s *Server) finishWithEmail(w http.ResponseWriter, r *http.Request, status int, allowPassword bool,
	fn func(context.Context, auth.EmailProof, string, auth.Client) (auth.Session, error),
) {
	var req emailProofRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	client, ok := parseClient(w, req.Client)
	if !ok {
		return
	}
	if req.Password != "" && !allowPassword {
		writeError(w, http.StatusBadRequest, "invalid_request", "a password can't be set here")
		return
	}
	sess, err := fn(r.Context(), auth.EmailProof{Token: req.Token, Email: req.Email, Code: req.Code}, req.Password, client)
	if err != nil {
		s.writeAuthError(w, r, err)
		return
	}
	s.startSession(w, r, status, sess, client)
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
