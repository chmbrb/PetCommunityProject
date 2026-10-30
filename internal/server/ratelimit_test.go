// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/google/uuid"

	"github.com/chmbrb/PetCommunityProject/internal/auth"
	"github.com/chmbrb/PetCommunityProject/internal/config"
	"github.com/chmbrb/PetCommunityProject/internal/ratelimit"
)

// closedAuth refuses sign-ups and returns empty sign-in options, so requests that get
// past the rate limiter have a recognizable outcome.
type closedAuth struct{ noAuth }

func (closedAuth) BeginSignup(context.Context, auth.SignupInput) (uuid.UUID, *protocol.PublicKeyCredentialCreationOptions, error) {
	return uuid.Nil, nil, auth.ErrRegistrationClosed
}

func (closedAuth) BeginLogin(context.Context) (uuid.UUID, *protocol.PublicKeyCredentialRequestOptions, error) {
	return uuid.New(), &protocol.PublicKeyCredentialRequestOptions{}, nil
}

func newRateLimitedServer(limits config.RateLimits, trustProxy bool) http.Handler {
	cfg := config.Config{
		InstanceName: "Test Instance",
		SourceURL:    config.DefaultSourceURL,
		TrustProxy:   trustProxy,
		RateLimits:   limits,
	}
	return New(cfg, &fakeStore{}, closedAuth{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

type reqOpts struct {
	method     string
	path       string
	remoteAddr string
	forwarded  string
	body       string
}

func send(h http.Handler, o reqOpts) *httptest.ResponseRecorder {
	if o.method == "" {
		o.method = http.MethodGet
	}
	if o.remoteAddr == "" {
		o.remoteAddr = "192.0.2.10:40000"
	}
	req := httptest.NewRequest(o.method, o.path, strings.NewReader(o.body))
	req.RemoteAddr = o.remoteAddr
	if o.forwarded != "" {
		req.Header.Set("X-Forwarded-For", o.forwarded)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

var tight = ratelimit.Policy{Burst: 2, Every: time.Hour}

func TestSignUpRateLimit(t *testing.T) {
	h := newRateLimitedServer(config.RateLimits{SignUp: tight}, false)
	signup := reqOpts{method: http.MethodPost, path: "/api/v1/auth/signup/begin", body: "{}"}

	for i := range 2 {
		if rec := send(h, signup); rec.Code != http.StatusForbidden {
			t.Fatalf("sign-up %d within limit: status %d, want 403 from the handler", i+1, rec.Code)
		}
	}
	rec := send(h, signup)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("sign-up over limit: status %d, want 429", rec.Code)
	}
	var body errorBody
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil || body.Error.Code != "rate_limited" {
		t.Fatalf("body = %+v, err = %v", body, err)
	}
	if secs, err := strconv.Atoi(rec.Header().Get("Retry-After")); err != nil || secs < 1 || secs > 3600 {
		t.Errorf("Retry-After = %q, want 1–3600 seconds", rec.Header().Get("Retry-After"))
	}

	// The sign-up limit doesn't affect sign-in or other API requests.
	if rec := send(h, reqOpts{method: http.MethodPost, path: "/api/v1/auth/login/begin"}); rec.Code != http.StatusOK {
		t.Errorf("sign-in after sign-up limit: status %d, want 200", rec.Code)
	}
	if rec := send(h, reqOpts{path: "/api/v1/instance"}); rec.Code != http.StatusOK {
		t.Errorf("instance after sign-up limit: status %d, want 200", rec.Code)
	}
}

func TestSignInRateLimit(t *testing.T) {
	h := newRateLimitedServer(config.RateLimits{SignIn: tight}, false)
	login := reqOpts{method: http.MethodPost, path: "/api/v1/auth/login/begin"}
	send(h, login)
	send(h, login)
	if rec := send(h, login); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("sign-in over limit: status %d, want 429", rec.Code)
	}
	// Finishing sign-in shares the same budget.
	if rec := send(h, reqOpts{method: http.MethodPost, path: "/api/v1/auth/login/finish"}); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("sign-in finish over limit: status %d, want 429", rec.Code)
	}
}

func TestAPIRateLimitIsPerClient(t *testing.T) {
	h := newRateLimitedServer(config.RateLimits{API: tight}, false)
	instance := reqOpts{path: "/api/v1/instance"}
	send(h, instance)
	send(h, instance)
	if rec := send(h, instance); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("over limit: status %d, want 429", rec.Code)
	}

	other := reqOpts{path: "/api/v1/instance", remoteAddr: "198.51.100.7:50000"}
	if rec := send(h, other); rec.Code != http.StatusOK {
		t.Errorf("another client: status %d, want 200", rec.Code)
	}

	// Health checks aren't under /api and are never limited.
	for range 5 {
		if rec := send(h, reqOpts{path: "/healthz"}); rec.Code != http.StatusOK {
			t.Fatalf("healthz: status %d, want 200", rec.Code)
		}
	}
}

func TestForwardedForIgnoredUnlessProxyTrusted(t *testing.T) {
	h := newRateLimitedServer(config.RateLimits{API: tight}, false)
	// A client rotating X-Forwarded-For values is still one client.
	for i := range 3 {
		rec := send(h, reqOpts{path: "/api/v1/instance", forwarded: "203.0.113." + strconv.Itoa(i)})
		if i == 2 && rec.Code != http.StatusTooManyRequests {
			t.Fatalf("spoofed X-Forwarded-For evaded the limit: status %d", rec.Code)
		}
	}
}

func TestForwardedForUsedBehindTrustedProxy(t *testing.T) {
	h := newRateLimitedServer(config.RateLimits{API: tight}, true)
	proxy := "10.89.0.5:45000" // every request arrives from Caddy

	for range 2 {
		send(h, reqOpts{path: "/api/v1/instance", remoteAddr: proxy, forwarded: "203.0.113.1"})
	}
	if rec := send(h, reqOpts{path: "/api/v1/instance", remoteAddr: proxy, forwarded: "203.0.113.1"}); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("client over limit: status %d, want 429", rec.Code)
	}
	if rec := send(h, reqOpts{path: "/api/v1/instance", remoteAddr: proxy, forwarded: "203.0.113.2"}); rec.Code != http.StatusOK {
		t.Fatalf("different client behind the same proxy: status %d, want 200", rec.Code)
	}
	// Only the rightmost entry counts: entries a client adds itself are ignored.
	if rec := send(h, reqOpts{path: "/api/v1/instance", remoteAddr: proxy, forwarded: "198.51.100.9, 203.0.113.1"}); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("client prepending a fake address evaded the limit: status %d", rec.Code)
	}
}

func TestClientIPAndKey(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		forwarded  string
		trust      bool
		wantKey    string
	}{
		{"IPv4 peer", "192.0.2.1:1234", "", false, "192.0.2.1"},
		{"IPv4-mapped IPv6 peer", "[::ffff:192.0.2.1]:1234", "", false, "192.0.2.1"},
		{"IPv6 peer grouped by /64", "[2001:db8:1:2:aaaa::1]:1234", "", false, "2001:db8:1:2::/64"},
		{"forwarded ignored without trust", "192.0.2.1:1234", "203.0.113.5", false, "192.0.2.1"},
		{"rightmost forwarded entry with trust", "10.0.0.2:1234", "198.51.100.1, 203.0.113.5", true, "203.0.113.5"},
		{"invalid forwarded falls back to peer", "10.0.0.2:1234", "not-an-ip", true, "10.0.0.2"},
		{"unparseable peer", "garbage", "", false, "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.forwarded != "" {
				req.Header.Set("X-Forwarded-For", tt.forwarded)
			}
			if got := rateLimitKey(clientIP(req, tt.trust)); got != tt.wantKey {
				t.Errorf("key = %q, want %q", got, tt.wantKey)
			}
		})
	}

	// Two addresses in the same /64 share a key; different /64s don't.
	a := rateLimitKey(netip.MustParseAddr("2001:db8:1:2::1"))
	b := rateLimitKey(netip.MustParseAddr("2001:db8:1:2:ffff::9"))
	c := rateLimitKey(netip.MustParseAddr("2001:db8:1:3::1"))
	if a != b || a == c {
		t.Errorf("IPv6 grouping: %q %q %q", a, b, c)
	}
}
