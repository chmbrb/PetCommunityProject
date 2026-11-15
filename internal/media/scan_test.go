// SPDX-License-Identifier: AGPL-3.0-or-later

package media

import (
	"bytes"
	"context"
	"crypto/md5" //nolint:gosec // G501: the hash lists being tested use MD5
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/chmbrb/PetCommunityProject/internal/config"
)

func TestHashListScanner(t *testing.T) {
	known := []byte("stand-in for a known image")
	other := []byte("someone's cat")
	md5Sum := md5.Sum(known) //nolint:gosec // G401: see import
	shaSum := sha256.Sum256(known)

	for name, list := range map[string]string{
		"sha256": "# a comment\n\n" + hex.EncodeToString(shaSum[:]) + "\n",
		"md5":    strings.ToUpper(hex.EncodeToString(md5Sum[:])) + "\r\n",
	} {
		s, err := LoadHashList(strings.NewReader(list))
		if err != nil {
			t.Fatalf("%s: LoadHashList: %v", name, err)
		}
		res, err := s.Scan(context.Background(), known)
		if err != nil || res.Verdict != Match || !strings.HasPrefix(res.Reference, name+":") {
			t.Errorf("%s: known image: %+v, %v", name, res, err)
		}
		if res, _ := s.Scan(context.Background(), other); res.Verdict != Clean || res.Reference != "" {
			t.Errorf("%s: other image: %+v", name, res)
		}
	}

	for name, list := range map[string]string{
		"empty":       "# nothing here\n",
		"not hex":     "not-a-digest\n",
		"wrong size":  "abcdef\n",
		"one bad row": hex.EncodeToString(shaSum[:]) + "\nzz\n",
	} {
		if _, err := LoadHashList(strings.NewReader(list)); err == nil {
			t.Errorf("%s list: no error", name)
		}
	}
}

func TestNewScanner(t *testing.T) {
	s, err := NewScanner(config.AbuseScan{Scanner: config.ScannerNone})
	if err != nil || s.Name() != config.ScannerNone {
		t.Fatalf("none: %v, %v", s, err)
	}
	if res, _ := s.Scan(context.Background(), nil); res.Verdict != NotScanned {
		t.Errorf("NoScanner verdict = %q", res.Verdict)
	}
	if _, err := NewScanner(config.AbuseScan{Scanner: config.ScannerHashList, HashListFile: t.TempDir() + "/missing"}); err == nil {
		t.Error("hashlist with a missing file: no error")
	}
}

func TestEvidenceSealer(t *testing.T) {
	s, err := NewEvidenceSealer(config.DevSecretKey)
	if err != nil {
		t.Fatal(err)
	}
	id, otherID := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	original := []byte("\xFF\xD8\xFF original upload")

	sealed, err := s.Seal(id, original)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, original) {
		t.Error("sealed evidence contains the original")
	}
	if got, err := s.Open(id, sealed); err != nil || !bytes.Equal(got, original) {
		t.Fatalf("Open: %q, %v", got, err)
	}
	if _, err := s.Open(otherID, sealed); !errors.Is(err, ErrEvidenceCorrupt) {
		t.Errorf("opened under another upload's ID: %v", err)
	}
	otherKey, _ := NewEvidenceSealer(bytes.Repeat([]byte{7}, 32))
	if _, err := otherKey.Open(id, sealed); !errors.Is(err, ErrEvidenceCorrupt) {
		t.Errorf("opened with another secret: %v", err)
	}
	sealed[len(sealed)-1] ^= 1
	if _, err := s.Open(id, sealed); !errors.Is(err, ErrEvidenceCorrupt) {
		t.Errorf("opened after tampering: %v", err)
	}
}
