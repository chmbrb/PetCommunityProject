// SPDX-License-Identifier: AGPL-3.0-or-later

// Package media turns untrusted uploaded images into safe, re-encoded variants
// (ADR 0011, ADR 0021). Decoding uses Go's own decoders and WebAssembly-based codecs
// transpiled to Go, so parsing hostile files stays memory-safe.
package media

import (
	"bytes"
	"encoding/binary"
)

// Format is an accepted upload format, identified by content, never by file name.
type Format string

const (
	JPEG Format = "jpeg"
	PNG  Format = "png"
	WebP Format = "webp"
	HEIC Format = "heic"
	AVIF Format = "avif"
)

// SniffLen is how many leading bytes Sniff needs.
const SniffLen = 64

var heicBrands = map[string]bool{
	"heic": true, "heix": true, "hevc": true, "hevx": true,
	"heim": true, "heis": true, "mif1": true, "msf1": true,
}

// Sniff identifies an accepted image format from the file's first bytes.
// SVG, GIF, video, and everything else are rejected (ADR 0011).
func Sniff(head []byte) (Format, bool) {
	switch {
	case bytes.HasPrefix(head, []byte{0xFF, 0xD8, 0xFF}):
		return JPEG, true
	case bytes.HasPrefix(head, []byte("\x89PNG\r\n\x1a\n")):
		return PNG, true
	case len(head) >= 12 && string(head[0:4]) == "RIFF" && string(head[8:12]) == "WEBP":
		return WebP, true
	}

	// HEIC and AVIF are ISO base media files: a "ftyp" box listing brands.
	if len(head) < 16 || string(head[4:8]) != "ftyp" {
		return "", false
	}
	boxLen := int(binary.BigEndian.Uint32(head[0:4]))
	if boxLen < 16 || boxLen > len(head) {
		boxLen = len(head)
	}
	brands := []string{string(head[8:12])}
	for i := 16; i+4 <= boxLen; i += 4 {
		brands = append(brands, string(head[i:i+4]))
	}
	for _, b := range brands {
		if b == "avif" || b == "avis" {
			return AVIF, true
		}
	}
	for _, b := range brands {
		if heicBrands[b] {
			return HEIC, true
		}
	}
	return "", false
}

// ContentType is the MIME type of an accepted format.
func (f Format) ContentType() string {
	return "image/" + string(f)
}
