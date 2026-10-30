// SPDX-License-Identifier: AGPL-3.0-or-later

package media

import (
	"context"
	"errors"

	"github.com/gen2brain/avif"
	"github.com/gen2brain/heic"
	"github.com/gen2brain/webp"
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

// Scanner checks images against a known-abuse-imagery hash service (ADR 0011). The
// provider is still to be chosen; until then NoScanner is used, and uploads are
// limited to accounts with a confirmed email.
type Scanner interface {
	Scan(ctx context.Context, original []byte) (Verdict, error)
}

// NoScanner is the Scanner used until a provider is configured.
type NoScanner struct{}

// Scan reports that no scan was done.
func (NoScanner) Scan(context.Context, []byte) (Verdict, error) { return NotScanned, nil }

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
