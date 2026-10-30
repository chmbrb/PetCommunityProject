// SPDX-License-Identifier: AGPL-3.0-or-later

// Package config loads and validates runtime configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Environment selects development or production behavior.
type Environment string

const (
	Development Environment = "development"
	Production  Environment = "production"
)

// DevDatabasePassword is the password used by compose.dev.yaml. The app refuses
// to start in production while it is still in use (ADR 0009).
const DevDatabasePassword = "dev-only-password" //nolint:gosec // G101: public development password, rejected in production

// DefaultSourceURL is where the unmodified source code lives. Operators running a
// modified version must point PC_SOURCE_URL at their own source (AGPL section 13).
const DefaultSourceURL = "https://github.com/chmbrb/PetCommunityProject"

// Config holds all runtime settings.
type Config struct {
	Env            Environment
	HTTPAddr       string
	DatabaseURL    string
	InstanceName   string
	InstanceDomain string
	SourceURL      string

	// WebAuthnRPID is the passkey relying party ID: the domain passkeys are bound to.
	// It can't change without invalidating every registered passkey.
	WebAuthnRPID string
	// WebAuthnOrigins are the exact origins (scheme://host[:port]) allowed to use passkeys.
	WebAuthnOrigins []string
}

// Load reads configuration using getenv (normally os.Getenv) and validates it.
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		Env:            Environment(valueOr(getenv("PC_ENV"), string(Development))),
		HTTPAddr:       valueOr(getenv("PC_HTTP_ADDR"), ":8080"),
		DatabaseURL:    getenv("PC_DATABASE_URL"),
		InstanceName:   valueOr(getenv("PC_INSTANCE_NAME"), "PetCommunityProject"),
		InstanceDomain: getenv("PC_INSTANCE_DOMAIN"),
		SourceURL:      valueOr(getenv("PC_SOURCE_URL"), DefaultSourceURL),
	}
	cfg.WebAuthnRPID, cfg.WebAuthnOrigins = webAuthnDefaults(cfg.Env, cfg.InstanceDomain)
	if v := getenv("PC_WEBAUTHN_RP_ID"); v != "" {
		cfg.WebAuthnRPID = v
	}
	if v := getenv("PC_WEBAUTHN_ORIGINS"); v != "" {
		cfg.WebAuthnOrigins = splitList(v)
	}
	return cfg, cfg.validate()
}

func (c Config) validate() error {
	var errs []error

	switch c.Env {
	case Development, Production:
	default:
		errs = append(errs, fmt.Errorf("PC_ENV must be %q or %q, got %q", Development, Production, c.Env))
	}

	if c.DatabaseURL == "" {
		errs = append(errs, errors.New("PC_DATABASE_URL is required"))
	}

	if u, err := url.Parse(c.SourceURL); err != nil || u.Scheme != "https" || u.Host == "" {
		errs = append(errs, errors.New("PC_SOURCE_URL must be an https URL"))
	}

	if c.WebAuthnRPID == "" {
		errs = append(errs, errors.New("PC_WEBAUTHN_RP_ID is required (defaults to PC_INSTANCE_DOMAIN)"))
	}
	if len(c.WebAuthnOrigins) == 0 {
		errs = append(errs, errors.New("PC_WEBAUTHN_ORIGINS is required"))
	}
	for _, o := range c.WebAuthnOrigins {
		u, err := url.Parse(o)
		if err != nil || u.Host == "" || u.Path != "" || (c.Env == Production && u.Scheme != "https") {
			errs = append(errs, fmt.Errorf("PC_WEBAUTHN_ORIGINS: %q must be an origin like https://example.org", o))
		}
	}

	if c.Env == Production {
		if c.InstanceDomain == "" {
			errs = append(errs, errors.New("PC_INSTANCE_DOMAIN is required in production"))
		}
		if strings.Contains(c.DatabaseURL, DevDatabasePassword) {
			errs = append(errs, errors.New("PC_DATABASE_URL uses the development password; set a real password for production"))
		}
	}

	return errors.Join(errs...)
}

// webAuthnDefaults derives passkey settings: the instance domain in production, and
// the API and Vite dev server on localhost in development.
func webAuthnDefaults(env Environment, domain string) (rpID string, origins []string) {
	if env == Production {
		if domain == "" {
			return "", nil
		}
		return domain, []string{"https://" + domain}
	}
	return "localhost", []string{"http://localhost:5173", "http://localhost:8080"}
}

func splitList(v string) []string {
	var out []string
	for _, item := range strings.Split(v, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func valueOr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
