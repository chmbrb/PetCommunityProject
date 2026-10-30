// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/google/uuid"

	"github.com/chmbrb/PetCommunityProject/internal/auth"
	"github.com/chmbrb/PetCommunityProject/internal/config"
	"github.com/chmbrb/PetCommunityProject/internal/media"
	"github.com/chmbrb/PetCommunityProject/internal/store"
)

// AuthService is the sign-in functionality the server needs (implemented by auth.Service).
type AuthService interface {
	BeginSignup(ctx context.Context, in auth.SignupInput) (uuid.UUID, *protocol.PublicKeyCredentialCreationOptions, error)
	FinishSignup(ctx context.Context, challengeID uuid.UUID, credential []byte, client auth.Client) (auth.Session, error)
	BeginLogin(ctx context.Context) (uuid.UUID, *protocol.PublicKeyCredentialRequestOptions, error)
	FinishLogin(ctx context.Context, challengeID uuid.UUID, credential []byte, client auth.Client) (auth.Session, error)
	Authenticate(ctx context.Context, token string) (auth.Principal, error)
	SignOut(ctx context.Context, sessionID uuid.UUID) error
	Me(ctx context.Context, accountID uuid.UUID) (store.GetMeRow, error)

	RequestSignInLink(ctx context.Context, email string) error
	FinishSignInLink(ctx context.Context, token string, client auth.Client) (auth.Session, error)
	VerifyEmail(ctx context.Context, token string) error
	ResendVerification(ctx context.Context, accountID uuid.UUID) error

	ListPasskeys(ctx context.Context, accountID uuid.UUID) ([]auth.PasskeyInfo, error)
	BeginAddPasskey(ctx context.Context, p auth.Principal) (uuid.UUID, *protocol.PublicKeyCredentialCreationOptions, error)
	FinishAddPasskey(ctx context.Context, p auth.Principal, challengeID uuid.UUID, credential []byte, name string) (auth.PasskeyInfo, error)
	RenamePasskey(ctx context.Context, p auth.Principal, id uuid.UUID, name string) error
	RemovePasskey(ctx context.Context, p auth.Principal, id uuid.UUID) error
	ListSessions(ctx context.Context, p auth.Principal) ([]auth.SessionInfo, error)
	RevokeSession(ctx context.Context, p auth.Principal, id uuid.UUID) error
	RevokeOtherSessions(ctx context.Context, p auth.Principal) (int64, error)
}

type principalKey struct{}

func principalFrom(ctx context.Context) (auth.Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(auth.Principal)
	return p, ok
}

// sessionCookieName uses the __Host- prefix in production, which browsers only accept
// on secure, host-only cookies with Path=/.
func (s *Server) sessionCookieName() string {
	if s.cfg.Env == config.Production {
		return "__Host-pc_session"
	}
	return "pc_session"
}

// authenticate attaches the signed-in principal to the request, from a bearer token
// (mobile apps) or the session cookie (web). Requests without valid credentials
// continue anonymously; handlers decide whether that's allowed.
func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Sessions started by this request record which browser or app they belong to.
		r = r.WithContext(auth.WithUserAgent(r.Context(), r.UserAgent()))

		token, fromCookie := "", false
		if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
			token = strings.TrimPrefix(h, "Bearer ")
		} else if c, err := r.Cookie(s.sessionCookieName()); err == nil {
			token, fromCookie = c.Value, true
		}
		if token == "" {
			next.ServeHTTP(w, r)
			return
		}

		p, err := s.auth.Authenticate(r.Context(), token)
		switch {
		case errors.Is(err, auth.ErrUnauthenticated):
			if fromCookie {
				s.clearSessionCookie(w)
			}
			next.ServeHTTP(w, r)
		case err != nil:
			s.logger.ErrorContext(r.Context(), "authenticate", "error", err)
			writeError(w, http.StatusInternalServerError, "internal", "internal error")
		default:
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, p)))
		}
	})
}

type beginResponse struct {
	ChallengeID uuid.UUID `json:"challenge_id"`
	Options     any       `json:"options"`
}

type finishRequest struct {
	ChallengeID uuid.UUID       `json:"challenge_id"`
	Credential  json.RawMessage `json:"credential"`
	// Client is "web" (session cookie, the default) or "mobile" (token in the response).
	Client string `json:"client"`
}

type meResponse struct {
	Account accountJSON  `json:"account"`
	Actor   actorJSON    `json:"actor"`
	Session *sessionJSON `json:"session,omitempty"`
}

type accountJSON struct {
	ID            uuid.UUID `json:"id"`
	Email         string    `json:"email"`
	EmailVerified bool      `json:"email_verified"`
	Status        string    `json:"status"`
}

type actorJSON struct {
	ID          uuid.UUID `json:"id"`
	Handle      string    `json:"handle"`
	DisplayName string    `json:"display_name"`
	// Avatar lists the profile photo's sizes; empty when there's none.
	Avatar []media.PublicVariant `json:"avatar"`
}

// sessionJSON is only returned to mobile clients, which store the token themselves.
type sessionJSON struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (s *Server) handleSignupBegin(w http.ResponseWriter, r *http.Request) {
	var in auth.SignupInput
	if !decodeJSON(w, r, &in) {
		return
	}
	id, options, err := s.auth.BeginSignup(r.Context(), in)
	if err != nil {
		s.writeAuthError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, beginResponse{ChallengeID: id, Options: options})
}

func (s *Server) handleSignupFinish(w http.ResponseWriter, r *http.Request) {
	s.finish(w, r, http.StatusCreated, s.auth.FinishSignup)
}

func (s *Server) handleLoginBegin(w http.ResponseWriter, r *http.Request) {
	id, options, err := s.auth.BeginLogin(r.Context())
	if err != nil {
		s.writeAuthError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, beginResponse{ChallengeID: id, Options: options})
}

func (s *Server) handleLoginFinish(w http.ResponseWriter, r *http.Request) {
	s.finish(w, r, http.StatusOK, s.auth.FinishLogin)
}

type finishFunc func(ctx context.Context, challengeID uuid.UUID, credential []byte, client auth.Client) (auth.Session, error)

// finish completes a sign-up or sign-in ceremony and starts the session.
func (s *Server) finish(w http.ResponseWriter, r *http.Request, status int, fn finishFunc) {
	var req finishRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	client, ok := parseClient(w, req.Client)
	if !ok {
		return
	}
	sess, err := fn(r.Context(), req.ChallengeID, req.Credential, client)
	if err != nil {
		s.writeAuthError(w, r, err)
		return
	}
	s.startSession(w, r, status, sess, client)
}

// parseClient reads the "client" request field: "web" (the default) or "mobile".
func parseClient(w http.ResponseWriter, v string) (auth.Client, bool) {
	switch v {
	case "", "web":
		return auth.ClientWeb, true
	case "mobile":
		return auth.ClientMobile, true
	}
	writeError(w, http.StatusBadRequest, "invalid_request", `client must be "web" or "mobile"`)
	return "", false
}

// startSession responds to a successful sign-in: a cookie for web clients, the token
// in the body for mobile clients.
func (s *Server) startSession(w http.ResponseWriter, r *http.Request, status int, sess auth.Session, client auth.Client) {
	// Signing in again replaces this client's previous session rather than leaving it
	// active alongside the new one.
	if old, ok := principalFrom(r.Context()); ok {
		if err := s.auth.SignOut(r.Context(), old.SessionID); err != nil {
			s.logger.WarnContext(r.Context(), "revoke replaced session", "error", err)
		}
	}
	me, err := s.auth.Me(r.Context(), sess.AccountID)
	if err != nil {
		s.writeAuthError(w, r, err)
		return
	}

	resp := s.toMeResponse(me)
	if client == auth.ClientMobile {
		resp.Session = &sessionJSON{Token: sess.Token, ExpiresAt: sess.ExpiresAt}
	} else {
		s.setSessionCookie(w, sess)
	}
	writeJSON(w, status, resp)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	p, ok := principalFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "not signed in")
		return
	}
	if err := s.auth.SignOut(r.Context(), p.SessionID); err != nil {
		s.writeAuthError(w, r, err)
		return
	}
	s.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	p, ok := principalFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "not signed in")
		return
	}
	me, err := s.auth.Me(r.Context(), p.AccountID)
	if err != nil {
		s.writeAuthError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s.toMeResponse(me))
}

func (s *Server) toMeResponse(me store.GetMeRow) meResponse {
	resp := meResponse{
		Account: accountJSON{ID: me.AccountID, Email: me.Email, EmailVerified: me.EmailVerifiedAt != nil, Status: me.Status},
		Actor:   actorJSON{ID: me.ActorID, Handle: me.Handle, DisplayName: me.DisplayName, Avatar: []media.PublicVariant{}},
	}
	if s.media != nil && len(me.AvatarVariants) > 0 {
		if variants, err := s.media.PublicVariants(me.AvatarVariants); err == nil {
			resp.Actor.Avatar = variants
		} else {
			s.logger.Warn("decode avatar variants", "error", err)
		}
	}
	return resp
}

// Session cookies are Secure in production. Development serves plain HTTP on
// localhost, where a Secure cookie wouldn't be stored by every browser.
func (s *Server) setSessionCookie(w http.ResponseWriter, sess auth.Session) {
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: Secure is set in production; see above
		Name:     s.sessionCookieName(),
		Value:    sess.Token,
		Path:     "/",
		Expires:  sess.ExpiresAt,
		HttpOnly: true,
		Secure:   s.cfg.Env == config.Production,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: Secure is set in production; see setSessionCookie
		Name:     s.sessionCookieName(),
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.cfg.Env == config.Production,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) writeAuthError(w http.ResponseWriter, r *http.Request, err error) {
	var verr *auth.ValidationError
	switch {
	case errors.As(err, &verr):
		writeJSON(w, http.StatusUnprocessableEntity, errorBody{Error: errorDetail{
			Code: "validation_failed", Message: verr.Message, Field: verr.Field,
		}})
	case errors.Is(err, auth.ErrRegistrationClosed):
		writeError(w, http.StatusForbidden, "registration_closed", err.Error())
	case errors.Is(err, auth.ErrEmailTaken):
		writeJSON(w, http.StatusConflict, errorBody{Error: errorDetail{Code: "email_taken", Message: err.Error(), Field: "email"}})
	case errors.Is(err, auth.ErrHandleTaken):
		writeJSON(w, http.StatusConflict, errorBody{Error: errorDetail{Code: "handle_taken", Message: err.Error(), Field: "handle"}})
	case errors.Is(err, auth.ErrChallengeExpired):
		writeError(w, http.StatusBadRequest, "challenge_expired", err.Error())
	case errors.Is(err, auth.ErrPasskeyRejected):
		writeError(w, http.StatusBadRequest, "passkey_rejected", err.Error())
	case errors.Is(err, auth.ErrSignInFailed):
		writeError(w, http.StatusUnauthorized, "sign_in_failed", err.Error())
	case errors.Is(err, auth.ErrLinkInvalid):
		writeError(w, http.StatusBadRequest, "link_invalid", err.Error())
	case errors.Is(err, auth.ErrTooManyEmails):
		writeError(w, http.StatusTooManyRequests, "too_many_emails", err.Error())
	case errors.Is(err, auth.ErrReauthRequired):
		writeError(w, http.StatusForbidden, "reauthentication_required", err.Error())
	case errors.Is(err, auth.ErrLastPasskey):
		writeError(w, http.StatusConflict, "last_passkey", err.Error())
	case errors.Is(err, auth.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "not found")
	default:
		s.logger.ErrorContext(r.Context(), "auth request failed", "error", err, "path", r.URL.Path)
		writeError(w, http.StatusInternalServerError, "internal", "internal error")
	}
}

// decodeJSON reads a JSON request body, rejecting unknown fields. It writes the error
// response and returns false if the body is invalid.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "too_large", "request body too large")
		} else {
			writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		}
		return false
	}
	return true
}
