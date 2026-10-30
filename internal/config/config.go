// SPDX-License-Identifier: AGPL-3.0-or-later

// Package config loads and validates runtime configuration from environment variables.
package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/chmbrb/PetCommunityProject/internal/ratelimit"
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

// DevSecretKey is the fixed secret key used in development when PC_SECRET_KEY isn't
// set. It's public, so production refuses it.
var DevSecretKey = func() []byte {
	sum := sha256.Sum256([]byte("PetCommunityProject development secret key; never use in production"))
	return sum[:]
}()

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

	// TrustProxy makes the app take the client IP from X-Forwarded-For, as set by
	// Caddy. Enable it only when the app is reachable solely through the proxy;
	// otherwise clients could choose their own IP and evade rate limits.
	TrustProxy bool
	// RateLimits apply per client IP (THREAT_MODEL.md A8, D1, S1).
	RateLimits RateLimits

	// PublicURL is the address people use to reach the web app, for links in email.
	PublicURL string
	// Mail is how the worker sends email.
	Mail Mail
	// Storage is the S3-compatible object storage for media (ADR 0010).
	Storage Storage
	// SecretKey (32 bytes, PC_SECRET_KEY) encrypts actors' private keys at rest
	// (ADR 0012). Only the worker needs it; it's nil in production processes that
	// weren't given it.
	SecretKey []byte
}

// Storage configures S3-compatible object storage: a hosted provider in production,
// MinIO in development.
type Storage struct {
	// Endpoint is the S3 API address, e.g. https://s3.us-west-004.backblazeb2.com.
	Endpoint  string
	Region    string
	AccessKey string
	SecretKey string
	// QuarantineBucket holds raw uploads until they're processed. Private.
	QuarantineBucket string
	// MediaBucket holds processed images. Publicly readable.
	MediaBucket string
	// MediaURL is the public address of MediaBucket's contents, e.g.
	// https://media.pets.example.org. Image URLs are MediaURL + "/" + key.
	MediaURL string
}

// RequireSecretKey reports whether a usable secret key is configured, for processes
// that encrypt or decrypt with it.
func (c Config) RequireSecretKey() error {
	if len(c.SecretKey) == 0 {
		return errors.New("PC_SECRET_KEY is required; generate one with `openssl rand -base64 32`")
	}
	if c.Env == Production && bytes.Equal(c.SecretKey, DevSecretKey) {
		return errors.New("PC_SECRET_KEY is the public development key; generate a real one for production")
	}
	return nil
}

// MailSecurity is how the connection to the SMTP server is protected.
type MailSecurity string

const (
	// MailSTARTTLS upgrades a plain connection with STARTTLS (usually port 587).
	MailSTARTTLS MailSecurity = "starttls"
	// MailTLS connects over TLS from the start (usually port 465).
	MailTLS MailSecurity = "tls"
	// MailNone sends unencrypted. Development only (Mailpit).
	MailNone MailSecurity = "none"
)

// Mail configures sending through a transactional email provider's SMTP server.
type Mail struct {
	Host     string
	Port     int
	Username string
	Password string
	Security MailSecurity
	// From is the sender, e.g. "Example Pet Community <noreply@pets.example.org>".
	From string
	// CAFile is an optional PEM file of extra certificate authorities to trust for the
	// SMTP connection, in addition to the system's. It's for testing the production
	// setup against a local mail catcher (compose.local.yaml); providers don't need it.
	// Certificates are always verified.
	CAFile string
}

// RateLimits groups the request rate limits. A policy with zero Burst is disabled.
type RateLimits struct {
	// API covers every /api request.
	API ratelimit.Policy
	// SignIn covers passkey sign-in attempts.
	SignIn ratelimit.Policy
	// SignUp covers account creation, which also reveals whether an email is
	// registered, so it's the strictest.
	SignUp ratelimit.Policy
	// EmailRecipient limits emails sent to any one address (sign-in links,
	// verification), so nobody can flood someone else's inbox.
	EmailRecipient ratelimit.Policy
	// Upload covers image uploads, which cost storage and processing time.
	Upload ratelimit.Policy
}

// DefaultRateLimits are generous for people, including several staff at a shelter
// sharing one IP address, and tight for automated abuse.
func DefaultRateLimits() RateLimits {
	return RateLimits{
		API:    ratelimit.Policy{Burst: 100, Every: 100 * time.Millisecond}, // 10/s sustained
		SignIn: ratelimit.Policy{Burst: 10, Every: 6 * time.Second},         // 10/min sustained
		SignUp: ratelimit.Policy{Burst: 5, Every: 2 * time.Minute},          // 30/h sustained
		// 3 at once, then 3/h sustained, per recipient address.
		EmailRecipient: ratelimit.Policy{Burst: 3, Every: 20 * time.Minute},
		Upload:         ratelimit.Policy{Burst: 10, Every: 3 * time.Minute}, // 20/h sustained
	}
}

// Load reads configuration using getenv (normally os.Getenv) and validates it.
//
// Secrets (PC_DATABASE_PASSWORD, PC_SECRET_KEY, PC_SMTP_PASSWORD, PC_S3_ACCESS_KEY,
// PC_S3_SECRET_KEY) can instead be read from a file named by the same variable with a
// _FILE suffix, such as a Compose secret mounted under /run/secrets. Production uses
// files, which keeps secrets out of the environment, where they leak into `docker
// inspect`, crash reports and child processes.
func Load(getenv func(string) string) (Config, error) {
	var parseErrs []error
	readSecret := func(name string) string {
		v, err := secret(getenv, name)
		if err != nil {
			parseErrs = append(parseErrs, err)
		}
		return v
	}

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
	cfg.RateLimits = DefaultRateLimits()
	cfg.PublicURL = valueOr(getenv("PC_PUBLIC_URL"), publicURLDefault(cfg.Env, cfg.InstanceDomain))
	cfg.Mail = mailDefaults(cfg.Env)
	cfg.Mail.Host = valueOr(getenv("PC_SMTP_HOST"), cfg.Mail.Host)
	cfg.Mail.Username = getenv("PC_SMTP_USERNAME")
	cfg.Mail.Password = readSecret("PC_SMTP_PASSWORD")
	cfg.Mail.Security = MailSecurity(valueOr(getenv("PC_SMTP_SECURITY"), string(cfg.Mail.Security)))
	cfg.Mail.From = valueOr(getenv("PC_MAIL_FROM"), cfg.Mail.From)
	cfg.Mail.CAFile = getenv("PC_SMTP_CA_FILE")
	if cfg.Env != Production {
		cfg.SecretKey = DevSecretKey
	}
	cfg.Storage = storageDefaults(cfg.Env)
	cfg.Storage.Endpoint = valueOr(getenv("PC_S3_ENDPOINT"), cfg.Storage.Endpoint)
	cfg.Storage.Region = valueOr(getenv("PC_S3_REGION"), cfg.Storage.Region)
	cfg.Storage.AccessKey = valueOr(readSecret("PC_S3_ACCESS_KEY"), cfg.Storage.AccessKey)
	cfg.Storage.SecretKey = valueOr(readSecret("PC_S3_SECRET_KEY"), cfg.Storage.SecretKey)
	cfg.Storage.QuarantineBucket = valueOr(getenv("PC_S3_QUARANTINE_BUCKET"), cfg.Storage.QuarantineBucket)
	cfg.Storage.MediaBucket = valueOr(getenv("PC_S3_MEDIA_BUCKET"), cfg.Storage.MediaBucket)
	cfg.Storage.MediaURL = strings.TrimSuffix(valueOr(getenv("PC_MEDIA_URL"), cfg.Storage.MediaURL), "/")

	if pw := readSecret("PC_DATABASE_PASSWORD"); pw != "" && cfg.DatabaseURL != "" {
		u, err := withPassword(cfg.DatabaseURL, pw)
		if err != nil {
			parseErrs = append(parseErrs, err)
		}
		cfg.DatabaseURL = u
	}
	if v := getenv("PC_TRUST_PROXY"); v != "" {
		var err error
		if cfg.TrustProxy, err = strconv.ParseBool(v); err != nil {
			parseErrs = append(parseErrs, fmt.Errorf("PC_TRUST_PROXY must be true or false, got %q", v))
		}
	}
	if v := readSecret("PC_SECRET_KEY"); v != "" {
		key, err := decodeSecretKey(v)
		if err != nil {
			parseErrs = append(parseErrs, err)
		}
		cfg.SecretKey = key
	}
	if v := getenv("PC_SMTP_PORT"); v != "" {
		port, err := strconv.Atoi(v)
		if err != nil || port < 1 || port > 65535 {
			parseErrs = append(parseErrs, fmt.Errorf("PC_SMTP_PORT must be a port number, got %q", v))
		}
		cfg.Mail.Port = port
	}
	return cfg, errors.Join(errors.Join(parseErrs...), cfg.validate())
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

	if u, err := url.Parse(c.PublicURL); err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || strings.TrimSuffix(u.Path, "/") != "" {
		errs = append(errs, fmt.Errorf("PC_PUBLIC_URL must be a URL like https://pets.example.org, got %q", c.PublicURL))
	}
	switch c.Mail.Security {
	case MailSTARTTLS, MailTLS, MailNone:
	default:
		errs = append(errs, fmt.Errorf("PC_SMTP_SECURITY must be starttls, tls, or none, got %q", c.Mail.Security))
	}
	if c.Mail.From == "" {
		errs = append(errs, errors.New("PC_MAIL_FROM is required"))
	}
	if c.Mail.CAFile != "" && c.Mail.Security == MailNone {
		errs = append(errs, errors.New("PC_SMTP_CA_FILE needs PC_SMTP_SECURITY=starttls or tls"))
	}

	for name, v := range map[string]string{"PC_S3_ENDPOINT": c.Storage.Endpoint, "PC_MEDIA_URL": c.Storage.MediaURL} {
		if u, err := url.Parse(v); err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
			errs = append(errs, fmt.Errorf("%s must be a URL like https://example.org, got %q", name, v))
		}
	}
	// Browsers load images from PC_MEDIA_URL, so it must be HTTPS. PC_S3_ENDPOINT may
	// be plain HTTP for a storage server on the instance's private network.
	if c.Env == Production && !strings.HasPrefix(c.Storage.MediaURL, "https://") {
		errs = append(errs, errors.New("PC_MEDIA_URL must use https in production"))
	}
	if c.Storage.QuarantineBucket == "" || c.Storage.MediaBucket == "" || c.Storage.QuarantineBucket == c.Storage.MediaBucket {
		errs = append(errs, errors.New("PC_S3_QUARANTINE_BUCKET and PC_S3_MEDIA_BUCKET must be set and different (raw uploads must never be public)"))
	}

	if c.Env == Production {
		if c.Storage.AccessKey == "" || c.Storage.SecretKey == "" {
			errs = append(errs, errors.New("PC_S3_ACCESS_KEY and PC_S3_SECRET_KEY are required in production"))
		}
		if c.Storage.SecretKey == DevDatabasePassword {
			errs = append(errs, errors.New("PC_S3_SECRET_KEY uses the development password"))
		}
		if c.Mail.Host == "" {
			errs = append(errs, errors.New("PC_SMTP_HOST is required in production (your email provider's SMTP server)"))
		}
		if c.Mail.Security == MailNone {
			errs = append(errs, errors.New("PC_SMTP_SECURITY=none is only allowed in development"))
		}
		if !strings.HasPrefix(c.PublicURL, "https://") {
			errs = append(errs, errors.New("PC_PUBLIC_URL must use https in production"))
		}
		if c.RateLimits.API.Disabled() || c.RateLimits.SignIn.Disabled() || c.RateLimits.SignUp.Disabled() || c.RateLimits.EmailRecipient.Disabled() || c.RateLimits.Upload.Disabled() {
			errs = append(errs, errors.New("rate limits can't be disabled in production"))
		}
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

// secret returns the environment variable name, or the contents of the file named by
// name_FILE without the trailing newline. Setting both is an error, so it's always
// clear which one is in use.
func secret(getenv func(string) string, name string) (string, error) {
	v, file := getenv(name), getenv(name+"_FILE")
	if file == "" {
		return v, nil
	}
	if v != "" {
		return "", fmt.Errorf("set %s or %s_FILE, not both", name, name)
	}
	b, err := os.ReadFile(file) //nolint:gosec // G304: the path comes from the operator's configuration
	if err != nil {
		return "", fmt.Errorf("%s_FILE: %w", name, err)
	}
	return strings.TrimRight(string(b), "\r\n"), nil
}

// withPassword adds password to a database URL that names a user but no password.
func withPassword(databaseURL, password string) (string, error) {
	u, err := url.Parse(databaseURL)
	if err != nil || u.User == nil || u.User.Username() == "" {
		return databaseURL, errors.New("PC_DATABASE_PASSWORD needs PC_DATABASE_URL to name the user, e.g. postgres://petcommunity@db:5432/petcommunity")
	}
	if _, set := u.User.Password(); set {
		return databaseURL, errors.New("PC_DATABASE_URL already contains a password; remove it when using PC_DATABASE_PASSWORD")
	}
	u.User = url.UserPassword(u.User.Username(), password)
	return u.String(), nil
}

// decodeSecretKey accepts a 32-byte key in standard or URL-safe base64, padded or not.
func decodeSecretKey(v string) ([]byte, error) {
	v = strings.TrimSpace(v)
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if key, err := enc.DecodeString(v); err == nil {
			if len(key) != 32 {
				return nil, fmt.Errorf("PC_SECRET_KEY must be 32 bytes, got %d; generate one with `openssl rand -base64 32`", len(key))
			}
			return key, nil
		}
	}
	return nil, errors.New("PC_SECRET_KEY must be base64; generate one with `openssl rand -base64 32`")
}

// publicURLDefault is the instance's site in production, and the Vite dev server in
// development.
func publicURLDefault(env Environment, domain string) string {
	if env == Production && domain != "" {
		return "https://" + domain
	}
	if env == Production {
		return ""
	}
	return "http://localhost:5173"
}

// storageDefaults point development at MinIO (compose.dev.yaml). In production the
// operator sets their provider's endpoint, credentials, and public media address.
func storageDefaults(env Environment) Storage {
	s := Storage{Region: "us-east-1", QuarantineBucket: "quarantine", MediaBucket: "media"}
	if env == Production {
		return s
	}
	s.Endpoint = "http://localhost:9000"
	s.AccessKey = "petcommunity"
	s.SecretKey = DevDatabasePassword
	s.MediaURL = "http://localhost:9000/media"
	return s
}

// mailDefaults point development at Mailpit (compose.dev.yaml). In production the
// operator sets their provider's SMTP server.
func mailDefaults(env Environment) Mail {
	if env == Production {
		return Mail{Port: 587, Security: MailSTARTTLS}
	}
	return Mail{
		Host:     "localhost",
		Port:     1025,
		Security: MailNone,
		From:     "PetCommunityProject <noreply@localhost>",
	}
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
