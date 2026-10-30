// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"context"
	"errors"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/google/uuid"
)

type middleware func(http.Handler) http.Handler

// chain applies middleware so that the first one listed is the outermost.
func chain(h http.Handler, mws ...middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

type requestIDKey struct{}

// requestID tags each request with a fresh ID, returned in X-Request-ID and logged.
// Incoming IDs are ignored so clients can't inject values into logs.
func (s *Server) requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := uuid.NewString()
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
	})
}

func requestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// accessLog logs one line per request. Query strings are left out because they may
// carry tokens.
func (s *Server) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.logger.InfoContext(r.Context(), "request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", requestIDFrom(r.Context()),
		)
	})
}

func (s *Server) recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if err, ok := v.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					panic(v)
				}
				s.logger.ErrorContext(r.Context(), "panic serving request",
					"panic", v,
					"stack", string(debug.Stack()),
					"request_id", requestIDFrom(r.Context()),
				)
				writeError(w, http.StatusInternalServerError, "internal", "internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// securityHeaders sets headers for API responses. Caddy sets the page-level
// headers (HSTS, CSP) in front of the SPA (ADR 0008).
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Cache-Control", "no-store")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// limitBody caps request bodies. Uploads have their own, larger limit (handleUpload).
func limitBody(n int64) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || r.URL.Path != uploadPath {
				r.Body = http.MaxBytesReader(w, r.Body, n)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// jsonRoutingErrors replaces the router's plain-text 404 and 405 responses with
// JSON errors on API paths, so clients always get the same error shape.
func jsonRoutingErrors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(&routingErrorWriter{ResponseWriter: w}, r)
	})
}

type routingErrorWriter struct {
	http.ResponseWriter
	replaced bool
}

func (w *routingErrorWriter) WriteHeader(code int) {
	plainText := strings.HasPrefix(w.Header().Get("Content-Type"), "text/plain")
	switch {
	case plainText && code == http.StatusNotFound:
		w.replaced = true
		writeError(w.ResponseWriter, code, "not_found", "not found")
	case plainText && code == http.StatusMethodNotAllowed:
		w.replaced = true
		writeError(w.ResponseWriter, code, "method_not_allowed", "method not allowed")
	default:
		w.ResponseWriter.WriteHeader(code)
	}
}

func (w *routingErrorWriter) Write(b []byte) (int, error) {
	if w.replaced {
		return len(b), nil
	}
	return w.ResponseWriter.Write(b)
}

// crossOriginProtection rejects cross-origin state-changing requests from browsers,
// which protects cookie sessions against CSRF (ADR 0014). It uses the browser's
// Sec-Fetch-Site and Origin headers rather than tokens. The native apps' origins are
// trusted; they authenticate with bearer tokens, which can't be sent cross-site.
func crossOriginProtection() middleware {
	cop := http.NewCrossOriginProtection()
	for origin := range nativeAppOrigins {
		if err := cop.AddTrustedOrigin(origin); err != nil {
			panic(err)
		}
	}
	cop.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusForbidden, "cross_origin_request", "cross-origin request rejected")
	}))
	return cop.Handler
}

// nativeAppOrigins are the origins the Capacitor apps load from (ADR 0018).
// The web app is same-origin and needs no CORS.
var nativeAppOrigins = map[string]bool{
	"capacitor://localhost": true, // iOS
	"https://localhost":     true, // Android
}

// nativeAppCORS lets the native apps call the API. Credentials (cookies) are never
// allowed cross-origin; the apps authenticate with bearer tokens (ADR 0014).
func nativeAppCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if !strings.HasPrefix(r.URL.Path, "/api/") || !nativeAppOrigins[origin] {
			next.ServeHTTP(w, r)
			return
		}
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", origin)
		h.Add("Vary", "Origin")
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE")
			h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			h.Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
