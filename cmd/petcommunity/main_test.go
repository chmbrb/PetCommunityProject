// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunUsage(t *testing.T) {
	var stderr bytes.Buffer
	if code := run(context.Background(), nil, func(string) string { return "" }, &bytes.Buffer{}, &stderr); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "usage:") {
		t.Errorf("stderr = %q, want usage", stderr.String())
	}
}

func TestRunInvalidConfig(t *testing.T) {
	var stderr bytes.Buffer
	code := run(context.Background(), []string{"serve"}, func(string) string { return "" }, &bytes.Buffer{}, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "PC_DATABASE_URL is required") {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
}

func TestHealthcheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())

	if err := healthcheck(context.Background(), ":"+port); err != nil {
		t.Fatalf("healthy server: %v", err)
	}
	srv.Close()
	if err := healthcheck(context.Background(), ":"+port); err == nil {
		t.Fatal("stopped server: want error")
	}
}
