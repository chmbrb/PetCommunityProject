// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
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
	prod["PC_S3_ENDPOINT"] = "https://s3.example.com"
	prod["PC_S3_ACCESS_KEY"] = "key"
	prod["PC_S3_SECRET_KEY"] = "secret"
	prod["PC_MEDIA_URL"] = "https://media.pets.example.org"
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

func TestStorage(t *testing.T) {
	dev, err := Load(env(map[string]string{"PC_DATABASE_URL": "postgres://x"}))
	if err != nil {
		t.Fatal(err)
	}
	if dev.Storage.Endpoint != "http://localhost:9000" || dev.Storage.MediaURL != "http://localhost:9000/media" {
		t.Errorf("development storage = %+v, want MinIO on localhost:9000", dev.Storage)
	}

	prod := map[string]string{
		"PC_ENV": "production", "PC_INSTANCE_DOMAIN": "pets.example.org", "PC_DATABASE_URL": "postgres://app:s3cret@db/app",
		"PC_SMTP_HOST": "smtp.example.com", "PC_MAIL_FROM": "noreply@pets.example.org",
	}
	_, err = Load(env(prod))
	for _, want := range []string{"PC_S3_ENDPOINT", "PC_MEDIA_URL", "PC_S3_ACCESS_KEY"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("production without storage settings: err = %v, want mention of %s", err, want)
		}
	}

	prod["PC_S3_ENDPOINT"] = "https://s3.example.com"
	prod["PC_S3_ACCESS_KEY"] = "key"
	prod["PC_S3_SECRET_KEY"] = "secret"
	prod["PC_MEDIA_URL"] = "https://media.pets.example.org/"
	cfg, err := Load(env(prod))
	if err != nil {
		t.Fatalf("valid production storage: %v", err)
	}
	if cfg.Storage.MediaURL != "https://media.pets.example.org" {
		t.Errorf("MediaURL = %q, want trailing slash removed", cfg.Storage.MediaURL)
	}

	prod["PC_MEDIA_URL"] = "http://media.pets.example.org"
	if _, err := Load(env(prod)); err == nil || !strings.Contains(err.Error(), "https") {
		t.Errorf("plain-HTTP media URL in production: err = %v", err)
	}
	prod["PC_MEDIA_URL"] = "https://media.pets.example.org"
	prod["PC_S3_MEDIA_BUCKET"] = "quarantine"
	if _, err := Load(env(prod)); err == nil || !strings.Contains(err.Error(), "different") {
		t.Errorf("same bucket for quarantine and media: err = %v", err)
	}
}

func TestSecretKey(t *testing.T) {
	dev, err := Load(env(map[string]string{"PC_DATABASE_URL": "postgres://x"}))
	if err != nil || dev.RequireSecretKey() != nil {
		t.Fatalf("development falls back to the dev key: err %v, require %v", err, dev.RequireSecretKey())
	}

	prod := map[string]string{
		"PC_ENV": "production", "PC_INSTANCE_DOMAIN": "pets.example.org", "PC_DATABASE_URL": "postgres://app:s3cret@db/app",
		"PC_SMTP_HOST": "smtp.example.com", "PC_MAIL_FROM": "noreply@pets.example.org",
		"PC_S3_ENDPOINT": "https://s3.example.com", "PC_S3_ACCESS_KEY": "key", "PC_S3_SECRET_KEY": "secret",
		"PC_MEDIA_URL": "https://media.pets.example.org",
	}
	cfg, err := Load(env(prod))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RequireSecretKey() == nil {
		t.Error("production without PC_SECRET_KEY: RequireSecretKey = nil, want an error")
	}

	prod["PC_SECRET_KEY"] = base64.StdEncoding.EncodeToString(DevSecretKey)
	cfg, _ = Load(env(prod))
	if err := cfg.RequireSecretKey(); err == nil || !strings.Contains(err.Error(), "development key") {
		t.Errorf("production with the dev key: %v", err)
	}

	real := make([]byte, 32)
	real[0] = 1
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawURLEncoding} {
		prod["PC_SECRET_KEY"] = enc.EncodeToString(real)
		cfg, err = Load(env(prod))
		if err != nil || cfg.RequireSecretKey() != nil || !bytes.Equal(cfg.SecretKey, real) {
			t.Errorf("valid key (%v): err %v, require %v", enc, err, cfg.RequireSecretKey())
		}
	}

	prod["PC_SECRET_KEY"] = base64.StdEncoding.EncodeToString(real[:16])
	if _, err := Load(env(prod)); err == nil || !strings.Contains(err.Error(), "32 bytes") {
		t.Errorf("short key: err = %v", err)
	}
	prod["PC_SECRET_KEY"] = "not base64!"
	if _, err := Load(env(prod)); err == nil || !strings.Contains(err.Error(), "base64") {
		t.Errorf("non-base64 key: err = %v", err)
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
		Storage: Storage{
			Endpoint: "https://s3.example.com", AccessKey: "key", SecretKey: "secret",
			QuarantineBucket: "quarantine", MediaBucket: "media", MediaURL: "https://media.pets.example.org",
		},
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
		"PC_S3_ENDPOINT":     "https://s3.example.com",
		"PC_S3_ACCESS_KEY":   "key",
		"PC_S3_SECRET_KEY":   "secret",
		"PC_MEDIA_URL":       "https://media.pets.example.org",
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
				"PC_S3_ENDPOINT":     "https://s3.example.com",
				"PC_S3_ACCESS_KEY":   "key",
				"PC_S3_SECRET_KEY":   "secret",
				"PC_MEDIA_URL":       "https://media.pets.example.org",
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

func writeSecret(t *testing.T, value string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte(value), 0o400); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSecretsFromFiles(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	cfg, err := Load(env(map[string]string{
		"PC_DATABASE_URL":           "postgres://petcommunity@db:5432/petcommunity",
		"PC_DATABASE_PASSWORD_FILE": writeSecret(t, "p@ss/word+\n"),
		"PC_SECRET_KEY_FILE":        writeSecret(t, key+"\n"),
		"PC_SMTP_PASSWORD_FILE":     writeSecret(t, "smtp-pass\r\n"),
		"PC_S3_ACCESS_KEY_FILE":     writeSecret(t, "access"),
		"PC_S3_SECRET_KEY_FILE":     writeSecret(t, "storage-secret\n"),
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if want := "postgres://petcommunity:p%40ss%2Fword+@db:5432/petcommunity"; cfg.DatabaseURL != want {
		t.Errorf("DatabaseURL = %q, want %q", cfg.DatabaseURL, want)
	}
	if !bytes.Equal(cfg.SecretKey, bytes.Repeat([]byte{7}, 32)) {
		t.Error("SecretKey wasn't read from its file")
	}
	if cfg.Mail.Password != "smtp-pass" || cfg.Storage.AccessKey != "access" || cfg.Storage.SecretKey != "storage-secret" {
		t.Errorf("secrets = %q %q %q, want them read from files without trailing newlines",
			cfg.Mail.Password, cfg.Storage.AccessKey, cfg.Storage.SecretKey)
	}
}

func TestSecretFileErrors(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"both forms", map[string]string{
			"PC_SMTP_PASSWORD": "a", "PC_SMTP_PASSWORD_FILE": writeSecret(t, "b"),
		}, "set PC_SMTP_PASSWORD or PC_SMTP_PASSWORD_FILE, not both"},
		{"missing file", map[string]string{
			"PC_S3_SECRET_KEY_FILE": filepath.Join(t.TempDir(), "missing"),
		}, "PC_S3_SECRET_KEY_FILE:"},
		{"password already in URL", map[string]string{
			"PC_DATABASE_URL": "postgres://u:old@db/app", "PC_DATABASE_PASSWORD_FILE": writeSecret(t, "new"),
		}, "already contains a password"},
		{"URL without user", map[string]string{
			"PC_DATABASE_URL": "postgres://db/app", "PC_DATABASE_PASSWORD": "pw",
		}, "needs PC_DATABASE_URL to name the user"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, ok := tt.env["PC_DATABASE_URL"]; !ok {
				tt.env["PC_DATABASE_URL"] = "postgres://x"
			}
			_, err := Load(env(tt.env))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestProductionRejectsDevPasswordFromFile(t *testing.T) {
	_, err := Load(env(map[string]string{
		"PC_ENV":                    "production",
		"PC_DATABASE_URL":           "postgres://petcommunity@db/petcommunity",
		"PC_DATABASE_PASSWORD_FILE": writeSecret(t, DevDatabasePassword),
	}))
	if err == nil || !strings.Contains(err.Error(), "development password") {
		t.Errorf("err = %v, want the development password rejected", err)
	}
}
