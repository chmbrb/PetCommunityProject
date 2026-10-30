// SPDX-License-Identifier: AGPL-3.0-or-later

// Package mail renders and sends email through a transactional provider's SMTP server.
package mail

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"sync"
	"time"

	gomail "github.com/wneessen/go-mail"

	"github.com/chmbrb/PetCommunityProject/internal/config"
)

// Message is one email to one recipient.
type Message struct {
	To      string
	Subject string
	Text    string
	HTML    string
}

// Sender delivers messages.
type Sender interface {
	Send(ctx context.Context, msg Message) error
}

// SMTPSender sends through an SMTP server, normally an email provider's.
type SMTPSender struct {
	cfg config.Mail
	tls *tls.Config
}

// NewSMTPSender returns a sender for the configured server. It fails if the extra
// certificate authorities in cfg.CAFile can't be loaded.
func NewSMTPSender(cfg config.Mail) (*SMTPSender, error) {
	tlsCfg := &tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12}
	if cfg.CAFile != "" {
		pem, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read PC_SMTP_CA_FILE: %w", err)
		}
		pool, err := x509.SystemCertPool()
		if err != nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("PC_SMTP_CA_FILE %s contains no PEM certificates", cfg.CAFile)
		}
		tlsCfg.RootCAs = pool
	}
	return &SMTPSender{cfg: cfg, tls: tlsCfg}, nil
}

// Send connects, sends one message, and disconnects. Volume is low enough that
// keeping a connection open isn't worth the complexity.
func (s *SMTPSender) Send(ctx context.Context, msg Message) error {
	m := gomail.NewMsg()
	if err := m.From(s.cfg.From); err != nil {
		return fmt.Errorf("from address: %w", err)
	}
	if err := m.To(msg.To); err != nil {
		return fmt.Errorf("to address: %w", err)
	}
	m.Subject(msg.Subject)
	m.SetMessageID()
	m.SetDate()
	m.SetBodyString(gomail.TypeTextPlain, msg.Text)
	if msg.HTML != "" {
		m.AddAlternativeString(gomail.TypeTextHTML, msg.HTML)
	}

	opts := []gomail.Option{
		gomail.WithPort(s.cfg.Port),
		gomail.WithTimeout(30 * time.Second),
		gomail.WithTLSConfig(s.tls),
	}
	switch s.cfg.Security {
	case config.MailTLS:
		opts = append(opts, gomail.WithSSL())
	case config.MailSTARTTLS:
		opts = append(opts, gomail.WithTLSPolicy(gomail.TLSMandatory))
	case config.MailNone:
		opts = append(opts, gomail.WithTLSPolicy(gomail.NoTLS))
	}
	if s.cfg.Username != "" {
		opts = append(opts,
			gomail.WithSMTPAuth(gomail.SMTPAuthAutoDiscover),
			gomail.WithUsername(s.cfg.Username),
			gomail.WithPassword(s.cfg.Password),
		)
	}

	client, err := gomail.NewClient(s.cfg.Host, opts...)
	if err != nil {
		return fmt.Errorf("configure SMTP client: %w", err)
	}
	if err := client.DialAndSendWithContext(ctx, m); err != nil {
		return fmt.Errorf("send email: %w", err)
	}
	return nil
}

// Recorder is a Sender that keeps messages in memory, for tests.
type Recorder struct {
	mu       sync.Mutex
	messages []Message
}

// Send records the message.
func (r *Recorder) Send(_ context.Context, msg Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.messages = append(r.messages, msg)
	return nil
}

// Messages returns the messages sent so far.
func (r *Recorder) Messages() []Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Message(nil), r.messages...)
}
