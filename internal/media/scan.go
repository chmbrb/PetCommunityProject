// SPDX-License-Identifier: AGPL-3.0-or-later

package media

import (
	"bufio"
	"context"
	"crypto/md5"  //nolint:gosec // G501: matching hash lists that are published as MD5, not for security
	"crypto/sha1" //nolint:gosec // G505: matching hash lists that are published as SHA-1, not for security
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/gen2brain/avif"
	"github.com/gen2brain/heic"
	"github.com/gen2brain/webp"

	"github.com/chmbrb/PetCommunityProject/internal/config"
)

// Verdict is the result of checking an upload against known abuse imagery.
type Verdict string

const (
	// NotScanned means no scanning service is configured.
	NotScanned Verdict = "not_scanned"
	// Clean means the service found no match.
	Clean Verdict = "clean"
	// Match means the image matches known abuse imagery (CSAM). It must be kept
	// unpublished, unviewed, and reported as the law requires (THREAT_MODEL.md C1).
	Match Verdict = "match"
)

// ScanResult is a scanner's answer for one image.
type ScanResult struct {
	Verdict Verdict
	// Reference identifies what matched, for the report: for example the matching
	// hash, or the provider's tracking ID. Never image content.
	Reference string
}

// Scanner checks images against known abuse imagery (ADR 0022). A Scanner must fail
// closed: if it can't reach its service, it returns an error, never Clean.
type Scanner interface {
	// Name identifies the scanner in case records, e.g. "hashlist".
	Name() string
	Scan(ctx context.Context, original []byte) (ScanResult, error)
}

// NewScanner returns the configured scanner.
func NewScanner(cfg config.AbuseScan) (Scanner, error) {
	switch cfg.Scanner {
	case "", config.ScannerNone:
		return NoScanner{}, nil
	case config.ScannerHashList:
		f, err := os.Open(cfg.HashListFile)
		if err != nil {
			return nil, fmt.Errorf("PC_ABUSE_HASH_LIST_FILE: %w", err)
		}
		defer func() { _ = f.Close() }() // read-only; nothing to flush
		return LoadHashList(f)
	default:
		return nil, fmt.Errorf("unknown abuse scanner %q", cfg.Scanner)
	}
}

// NoScanner is the Scanner used until a provider is configured. Uploads are limited
// to accounts with a confirmed email meanwhile.
type NoScanner struct{}

// Name identifies the scanner.
func (NoScanner) Name() string { return config.ScannerNone }

// Scan reports that no scan was done.
func (NoScanner) Scan(context.Context, []byte) (ScanResult, error) {
	return ScanResult{Verdict: NotScanned}, nil
}

// HashListScanner matches uploads byte for byte against a list of MD5, SHA-1, or
// SHA-256 digests, the forms in which hash lists of known abuse imagery are shared
// (for example by NCMEC with registered providers). Exact hashes miss any image that
// was resized or re-encoded, so it's a minimum, not a substitute for a perceptual
// matching service such as PhotoDNA.
type HashListScanner struct {
	// Digests by algorithm, keyed by the raw digest bytes.
	md5, sha1, sha256 map[string]struct{}
}

// LoadHashList reads one hex digest per line. Blank lines and lines starting with #
// are skipped. The algorithm is taken from the digest's length.
func LoadHashList(r io.Reader) (*HashListScanner, error) {
	s := &HashListScanner{md5: map[string]struct{}{}, sha1: map[string]struct{}{}, sha256: map[string]struct{}{}}
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		digest, err := hex.DecodeString(line)
		if err != nil {
			return nil, fmt.Errorf("hash list line %d: not a hex digest", n)
		}
		switch len(digest) {
		case md5.Size:
			s.md5[string(digest)] = struct{}{}
		case sha1.Size:
			s.sha1[string(digest)] = struct{}{}
		case sha256.Size:
			s.sha256[string(digest)] = struct{}{}
		default:
			return nil, fmt.Errorf("hash list line %d: %d-byte digest isn't MD5, SHA-1, or SHA-256", n, len(digest))
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read hash list: %w", err)
	}
	if s.Len() == 0 {
		return nil, errors.New("hash list is empty")
	}
	return s, nil
}

// Len returns the number of digests in the list.
func (s *HashListScanner) Len() int { return len(s.md5) + len(s.sha1) + len(s.sha256) }

// Name identifies the scanner.
func (s *HashListScanner) Name() string { return config.ScannerHashList }

// Scan looks up the image's digests.
func (s *HashListScanner) Scan(_ context.Context, original []byte) (ScanResult, error) {
	checks := []struct {
		name string
		list map[string]struct{}
		sum  func([]byte) []byte
	}{
		{"sha256", s.sha256, func(b []byte) []byte { d := sha256.Sum256(b); return d[:] }},
		{"sha1", s.sha1, func(b []byte) []byte { d := sha1.Sum(b); return d[:] }}, //nolint:gosec // G401: see import
		{"md5", s.md5, func(b []byte) []byte { d := md5.Sum(b); return d[:] }},    //nolint:gosec // G401: see import
	}
	for _, c := range checks {
		if len(c.list) == 0 {
			continue
		}
		if d := c.sum(original); has(c.list, d) {
			return ScanResult{Verdict: Match, Reference: c.name + ":" + hex.EncodeToString(d)}, nil
		}
	}
	return ScanResult{Verdict: Clean}, nil
}

func has(list map[string]struct{}, digest []byte) bool {
	_, ok := list[string(digest)]
	return ok
}

// CheckPureCodecs fails if any image codec loaded a C shared library instead of its
// memory-safe Go build. The codecs prefer a system library when one is installed, so
// builds must use the nodynamic tag; this catches a build that doesn't (ADR 0021).
func CheckPureCodecs() error {
	var errs []error
	for name, dyn := range map[string]func() error{"webp": webp.Dynamic, "avif": avif.Dynamic, "heic": heic.Dynamic} {
		if dyn() == nil {
			errs = append(errs, errors.New(name+" codec loaded a C shared library; build with -tags nodynamic"))
		}
	}
	return errors.Join(errs...)
}
