// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"net/http"
)

type finishMFARequest struct {
	MFAToken string `json:"mfa_token"`
	// Code is from the authenticator app; RecoveryCode is used instead when it isn't
	// available.
	Code         string `json:"code"`
	RecoveryCode string `json:"recovery_code"`
}

type totpSetupResponse struct {
	// Secret is the setup key for typing or pasting into a password manager; URI is the
	// otpauth:// link, for the QR code or for opening on the same phone.
	Secret string `json:"secret"`
	URI    string `json:"uri"`
}

type codeRequest struct {
	Code string `json:"code"`
}

type recoveryCodesResponse struct {
	RecoveryCodes []string `json:"recovery_codes"`
}

// handleFinishMFA completes a sign-in that answered 401 mfa_required. The session is
// for the client named in the first step.
func (s *Server) handleFinishMFA(w http.ResponseWriter, r *http.Request) {
	var req finishMFARequest
	if !decodeJSON(w, r, &req) {
		return
	}
	sess, err := s.auth.FinishMFA(r.Context(), req.MFAToken, req.Code, req.RecoveryCode)
	if err != nil {
		s.writeAuthError(w, r, err)
		return
	}
	s.startSession(w, r, http.StatusOK, sess, sess.Client)
}

// handleTOTPSetup starts adding an authenticator app: it returns the secret, to show
// once. Two-step sign-in turns on only when a code from it is confirmed.
func (s *Server) handleTOTPSetup(w http.ResponseWriter, r *http.Request) {
	p, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	setup, err := s.auth.BeginTOTPSetup(r.Context(), p, s.cfg.InstanceName)
	if err != nil {
		s.writeAuthError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, totpSetupResponse{Secret: setup.Secret, URI: setup.URI})
}

// handleTOTPConfirm turns on two-step sign-in with a code from the new authenticator,
// and returns recovery codes, shown once.
func (s *Server) handleTOTPConfirm(w http.ResponseWriter, r *http.Request) {
	p, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	var req codeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	codes, err := s.auth.ConfirmTOTP(r.Context(), p, req.Code)
	if err != nil {
		s.writeAuthError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, recoveryCodesResponse{RecoveryCodes: codes})
}

// handleRemoveTOTP turns off two-step sign-in.
func (s *Server) handleRemoveTOTP(w http.ResponseWriter, r *http.Request) {
	p, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	if err := s.auth.RemoveTOTP(r.Context(), p); err != nil {
		s.writeAuthError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleRegenerateRecoveryCodes replaces the recovery codes and returns the new ones.
func (s *Server) handleRegenerateRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	p, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	codes, err := s.auth.RegenerateRecoveryCodes(r.Context(), p)
	if err != nil {
		s.writeAuthError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, recoveryCodesResponse{RecoveryCodes: codes})
}
