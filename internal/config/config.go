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

func valueOr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
