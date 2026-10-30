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
	if cfg.WebAuthnRPID != "localhost" || len(cfg.WebAuthnOrigins) != 2 {
		t.Errorf("WebAuthn defaults = %q %v, want localhost with two dev origins", cfg.WebAuthnRPID, cfg.WebAuthnOrigins)
	}
}

func TestMailDefaults(t *testing.T) {
	cfg, err := Load(env(map[string]string{"PC_DATABASE_URL": "postgres://x"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mail.Host != "localhost" || cfg.Mail.Port != 1025 || cfg.Mail.Security != MailNone {
		t.Errorf("development mail = %+v, want Mailpit on localhost:1025", cfg.Mail)
	}
	if cfg.PublicURL != "http://localhost:5173" {
		t.Errorf("development PublicURL = %q", cfg.PublicURL)
	}
}

func TestProductionMail(t *testing.T) {
	prod := map[string]string{
		"PC_ENV":             "production",
		"PC_INSTANCE_DOMAIN": "pets.example.org",
		"PC_DATABASE_URL":    "postgres://app:s3cret@db/app",
	}
	_, err := Load(env(prod))
	for _, want := range []string{"PC_SMTP_HOST is required", "PC_MAIL_FROM is required"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("production without mail settings: err = %v, want %q", err, want)
		}
	}

	prod["PC_SMTP_HOST"] = "smtp.example.com"
	prod["PC_MAIL_FROM"] = "Pets <noreply@pets.example.org>"
	cfg, err := Load(env(prod))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Mail.Port != 587 || cfg.Mail.Security != MailSTARTTLS || cfg.PublicURL != "https://pets.example.org" {
		t.Errorf("production defaults: mail %+v, PublicURL %q", cfg.Mail, cfg.PublicURL)
	}

	prod["PC_SMTP_SECURITY"] = "none"
	if _, err := Load(env(prod)); err == nil || !strings.Contains(err.Error(), "only allowed in development") {
		t.Errorf("unencrypted SMTP in production: err = %v", err)
	}
	prod["PC_SMTP_SECURITY"] = "tls"
	prod["PC_SMTP_PORT"] = "smtps"
	if _, err := Load(env(prod)); err == nil || !strings.Contains(err.Error(), "PC_SMTP_PORT") {
		t.Errorf("invalid port: err = %v", err)
	}
}

func TestTrustProxy(t *testing.T) {
	base := map[string]string{"PC_DATABASE_URL": "postgres://x"}
	cfg, err := Load(env(base))
	if err != nil || cfg.TrustProxy {
		t.Fatalf("default TrustProxy = %v (err %v), want false", cfg.TrustProxy, err)
	}

	base["PC_TRUST_PROXY"] = "true"
	if cfg, err = Load(env(base)); err != nil || !cfg.TrustProxy {
		t.Fatalf("PC_TRUST_PROXY=true: TrustProxy = %v (err %v)", cfg.TrustProxy, err)
	}

	base["PC_TRUST_PROXY"] = "sometimes"
	if _, err = Load(env(base)); err == nil || !strings.Contains(err.Error(), "PC_TRUST_PROXY") {
		t.Fatalf("invalid PC_TRUST_PROXY: err = %v", err)
	}
}

func TestRateLimitsEnabledByDefault(t *testing.T) {
	cfg, err := Load(env(map[string]string{"PC_DATABASE_URL": "postgres://x"}))
	if err != nil {
		t.Fatal(err)
	}
	for name, p := range map[string]bool{
		"API": cfg.RateLimits.API.Disabled(), "SignIn": cfg.RateLimits.SignIn.Disabled(), "SignUp": cfg.RateLimits.SignUp.Disabled(),
	} {
		if p {
			t.Errorf("%s rate limit disabled by default", name)
		}
	}
}

func TestProductionRequiresRateLimits(t *testing.T) {
	cfg := Config{
		Env: Production, DatabaseURL: "postgres://app:s3cret@db/app", InstanceDomain: "pets.example.org",
		SourceURL: DefaultSourceURL, WebAuthnRPID: "pets.example.org", WebAuthnOrigins: []string{"https://pets.example.org"},
		PublicURL: "https://pets.example.org",
		Mail:      Mail{Host: "smtp.example.com", Port: 587, Security: MailSTARTTLS, From: "noreply@pets.example.org"},
	}
	if err := cfg.validate(); err == nil || !strings.Contains(err.Error(), "rate limits") {
		t.Fatalf("validate with no rate limits: err = %v", err)
	}
	cfg.RateLimits = DefaultRateLimits()
	if err := cfg.validate(); err != nil {
		t.Fatalf("validate with default rate limits: %v", err)
	}
}

func TestWebAuthnProductionDefaults(t *testing.T) {
	cfg, err := Load(env(map[string]string{
		"PC_ENV":             "production",
		"PC_INSTANCE_DOMAIN": "pets.example.org",
		"PC_DATABASE_URL":    "postgres://app:s3cret@db/app",
		"PC_SMTP_HOST":       "smtp.example.com",
		"PC_MAIL_FROM":       "noreply@pets.example.org",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.WebAuthnRPID != "pets.example.org" {
		t.Errorf("WebAuthnRPID = %q", cfg.WebAuthnRPID)
	}
	if len(cfg.WebAuthnOrigins) != 1 || cfg.WebAuthnOrigins[0] != "https://pets.example.org" {
		t.Errorf("WebAuthnOrigins = %v", cfg.WebAuthnOrigins)
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
			name: "insecure passkey origin in production",
			env: map[string]string{
				"PC_ENV":              "production",
				"PC_INSTANCE_DOMAIN":  "pets.example.org",
				"PC_DATABASE_URL":     "postgres://app:s3cret@db/app",
				"PC_WEBAUTHN_ORIGINS": "http://pets.example.org",
			},
			wantErr: "PC_WEBAUTHN_ORIGINS",
		},
		{
			name: "valid production",
			env: map[string]string{
				"PC_ENV":             "production",
				"PC_INSTANCE_DOMAIN": "pets.example.org",
				"PC_DATABASE_URL":    "postgres://app:s3cret@db/app",
				"PC_SMTP_HOST":       "smtp.example.com",
				"PC_MAIL_FROM":       "noreply@pets.example.org",
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
