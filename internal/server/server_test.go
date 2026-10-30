// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chmbrb/PetCommunityProject/internal/config"
	"github.com/chmbrb/PetCommunityProject/internal/store"
)

type fakeStore struct {
	pingErr  error
	flags    []store.ListFeatureFlagsRow
	flagsErr error
}

func (f *fakeStore) Ping(context.Context) error { return f.pingErr }

func (f *fakeStore) ListFeatureFlags(context.Context) ([]store.ListFeatureFlagsRow, error) {
	return f.flags, f.flagsErr
}

func newTestServer(st Store) http.Handler {
	cfg := config.Config{
		InstanceName:   "Test Instance",
		InstanceDomain: "pets.example.org",
		SourceURL:      config.DefaultSourceURL,
	}
	return New(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestHealth(t *testing.T) {
	rec := get(t, newTestServer(&fakeStore{}), "/healthz")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestReady(t *testing.T) {
	if rec := get(t, newTestServer(&fakeStore{}), "/readyz"); rec.Code != http.StatusOK {
		t.Errorf("healthy database: status = %d, want 200", rec.Code)
	}
	rec := get(t, newTestServer(&fakeStore{pingErr: errors.New("down")}), "/readyz")
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("database down: status = %d, want 503", rec.Code)
	}
}

func TestInstance(t *testing.T) {
	st := &fakeStore{flags: []store.ListFeatureFlagsRow{
		{Flag: "open_registration", Enabled: true},
		{Flag: "pet_sitting", Enabled: false},
	}}
	rec := get(t, newTestServer(st), "/api/v1/instance")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body instanceResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Name != "Test Instance" || body.APIVersion != "v1" || body.SourceURL != config.DefaultSourceURL {
		t.Errorf("unexpected body: %+v", body)
	}
	if !body.Features["open_registration"] || body.Features["pet_sitting"] {
		t.Errorf("features = %v", body.Features)
	}
}

func TestInstanceStoreError(t *testing.T) {
	rec := get(t, newTestServer(&fakeStore{flagsErr: errors.New("boom")}), "/api/v1/instance")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestUnknownAPIRoute(t *testing.T) {
	rec := get(t, newTestServer(&fakeStore{}), "/api/v1/nope")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	var body errorBody
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil || body.Error.Code != "not_found" {
		t.Fatalf("body = %+v, err = %v", body, err)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	rec := httptest.NewRecorder()
	newTestServer(&fakeStore{}).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/instance", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	if allow := rec.Header().Get("Allow"); !strings.Contains(allow, http.MethodGet) {
		t.Errorf("Allow = %q, want it to include GET", allow)
	}
	var body errorBody
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil || body.Error.Code != "method_not_allowed" {
		t.Fatalf("body = %+v, err = %v", body, err)
	}
}

func TestResponseHeaders(t *testing.T) {
	rec := get(t, newTestServer(&fakeStore{}), "/healthz")
	for header, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"Cache-Control":          "no-store",
		"Content-Type":           "application/json; charset=utf-8",
	} {
		if got := rec.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
	if rec.Header().Get("X-Request-ID") == "" {
		t.Error("X-Request-ID missing")
	}
}

func TestPanicRecovery(t *testing.T) {
	s := &Server{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	h := s.recoverPanics(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestNativeAppCORS(t *testing.T) {
	h := newTestServer(&fakeStore{})

	preflight := httptest.NewRequest(http.MethodOptions, "/api/v1/instance", nil)
	preflight.Header.Set("Origin", "capacitor://localhost")
	preflight.Header.Set("Access-Control-Request-Method", http.MethodGet)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, preflight)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want 204", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "capacitor://localhost" {
		t.Errorf("Access-Control-Allow-Origin = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Errorf("Access-Control-Allow-Credentials = %q, want unset", got)
	}

	other := httptest.NewRequest(http.MethodGet, "/api/v1/instance", nil)
	other.Header.Set("Origin", "https://evil.example")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, other)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("unknown origin got Access-Control-Allow-Origin = %q", got)
	}
}
