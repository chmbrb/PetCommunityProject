// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(env(map[string]string{"PC_DATABASE_URL": "postgres://localhost/db"}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Env != Development {
		t.Errorf("Env = %q, want %q", cfg.Env, Development)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr = %q, want :8080", cfg.HTTPAddr)
	}
	if cfg.SourceURL != DefaultSourceURL {
		t.Errorf("SourceURL = %q, want %q", cfg.SourceURL, DefaultSourceURL)
	}
}

func TestLoadValidation(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{
			name:    "missing database URL",
			env:     map[string]string{},
			wantErr: "PC_DATABASE_URL is required",
		},
		{
			name:    "unknown environment",
			env:     map[string]string{"PC_DATABASE_URL": "postgres://x", "PC_ENV": "staging"},
			wantErr: "PC_ENV must be",
		},
		{
			name:    "insecure source URL",
			env:     map[string]string{"PC_DATABASE_URL": "postgres://x", "PC_SOURCE_URL": "http://example.org/src"},
			wantErr: "PC_SOURCE_URL must be an https URL",
		},
		{
			name:    "production without domain",
			env:     map[string]string{"PC_DATABASE_URL": "postgres://x", "PC_ENV": "production"},
			wantErr: "PC_INSTANCE_DOMAIN is required",
		},
		{
			name: "production with development password",
			env: map[string]string{
				"PC_ENV":             "production",
				"PC_INSTANCE_DOMAIN": "pets.example.org",
				"PC_DATABASE_URL":    "postgres://app:" + DevDatabasePassword + "@db/app",
			},
			wantErr: "development password",
		},
		{
			name: "valid production",
			env: map[string]string{
				"PC_ENV":             "production",
				"PC_INSTANCE_DOMAIN": "pets.example.org",
				"PC_DATABASE_URL":    "postgres://app:s3cret@db/app",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(env(tt.env))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Load: unexpected error %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Load error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}
