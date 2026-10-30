// SPDX-License-Identifier: AGPL-3.0-or-later

package mail

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chmbrb/PetCommunityProject/internal/config"
)

// writeTestCA writes a self-signed CA certificate in PEM form and returns its path.
func writeTestCA(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "ca.crt")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestNewSMTPSenderCAFile(t *testing.T) {
	base := config.Mail{Host: "mailpit", Port: 1025, Security: config.MailSTARTTLS, From: "noreply@localhost"}

	s, err := NewSMTPSender(base)
	if err != nil || s.tls.RootCAs != nil || s.tls.ServerName != "mailpit" {
		t.Fatalf("without CA file: err %v, tls %+v (want system roots, ServerName mailpit)", err, s.tls)
	}

	withCA := base
	withCA.CAFile = writeTestCA(t)
	s, err = NewSMTPSender(withCA)
	if err != nil || s.tls.RootCAs == nil {
		t.Fatalf("with CA file: err %v", err)
	}
	if s.tls.InsecureSkipVerify {
		t.Fatal("certificate verification must stay on")
	}

	missing := base
	missing.CAFile = filepath.Join(t.TempDir(), "nope.crt")
	if _, err := NewSMTPSender(missing); err == nil {
		t.Error("missing CA file: want error")
	}

	notPEM := base
	notPEM.CAFile = filepath.Join(t.TempDir(), "junk.crt")
	_ = os.WriteFile(notPEM.CAFile, []byte("not a certificate"), 0o600)
	if _, err := NewSMTPSender(notPEM); err == nil || !strings.Contains(err.Error(), "no PEM certificates") {
		t.Errorf("non-PEM CA file: err = %v", err)
	}
}
