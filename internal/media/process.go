// SPDX-License-Identifier: AGPL-3.0-or-later

package media

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math/bits"

	"github.com/gen2brain/avif"
	"github.com/gen2brain/heic"
	"github.com/gen2brain/webp"
	xdraw "golang.org/x/image/draw"
)

const (
	// MaxUploadBytes is the largest accepted upload.
	MaxUploadBytes = 15 << 20
	// MaxPixels is checked from the image header before decoding, so a small file
	// that claims enormous dimensions (a decompression bomb) is never decoded.
	MaxPixels = 40_000_000
)

var (
	ErrUnsupported = errors.New("unsupported image type; use JPEG, PNG, WebP, HEIC, or AVIF")
	ErrTooLarge    = errors.New("image dimensions are too large")
	ErrUndecodable = errors.New("the image couldn't be read")
)

// Variant is one re-encoded image.
type Variant struct {
	// Name is the file name within the asset, e.g. "800.webp".
	Name        string
	Width       int
	Height      int
	ContentType string
	Data        []byte
}

// Result is a processed upload.
type Result struct {
	Format Format
	// Width and Height are of the upright original.
	Width, Height int
	SHA256        [32]byte
	// PHash is a perceptual hash: near-identical images have nearby hashes, so it
	// finds re-uploads of the same photo after resizing or recompression.
	PHash    uint64
	Variants []Variant
}

// variantEdges are the longest-edge sizes produced, in pixels. Images are never
// enlarged: the first size at or beyond the original is made at the original's size,
// and larger ones are skipped.
var variantEdges = []int{320, 800, 1600}

// fallbackEdge is the size also encoded as JPEG, for clients without WebP.
const fallbackEdge = 800

// Process validates, decodes, and re-encodes an uploaded image. Nothing from the
// original file survives except pixels: metadata such as EXIF GPS location is dropped
// because variants are freshly encoded from decoded pixels.
func Process(data []byte) (Result, error) {
	format, ok := Sniff(data[:min(len(data), SniffLen)])
	if !ok {
		return Result{}, ErrUnsupported
	}
	if len(data) > MaxUploadBytes {
		return Result{}, ErrTooLarge
	}
	cfg, err := decodeConfig(format, data)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %w", ErrUndecodable, err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > MaxPixels {
		return Result{}, ErrTooLarge
	}

	img, err := decode(format, data)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %w", ErrUndecodable, err)
	}
	if format == JPEG {
		img = orient(img, jpegOrientation(data))
	}
	b := img.Bounds()
	res := Result{Format: format, Width: b.Dx(), Height: b.Dy(), SHA256: sha256.Sum256(data)}

	longest := max(res.Width, res.Height)
	for i, edge := range variantEdges {
		if i > 0 && variantEdges[i-1] >= longest {
			break
		}
		scaled := resize(img, min(edge, longest))
		if i == 0 {
			res.PHash = dHash(scaled)
		}
		var buf bytes.Buffer
		if err := webp.Encode(&buf, scaled, webp.Options{Quality: 80, Method: 4}); err != nil {
			return Result{}, fmt.Errorf("encode webp: %w", err)
		}
		sb := scaled.Bounds()
		res.Variants = append(res.Variants, Variant{
			Name: fmt.Sprintf("%d.webp", edge), Width: sb.Dx(), Height: sb.Dy(),
			ContentType: "image/webp", Data: buf.Bytes(),
		})
	}
	fallback, err := encodeJPEG(resize(img, min(fallbackEdge, longest)), fallbackEdge)
	if err != nil {
		return Result{}, err
	}
	res.Variants = append(res.Variants, fallback)
	return res, nil
}

// encodeJPEG flattens transparency onto white, since JPEG has no alpha channel.
func encodeJPEG(img image.Image, edge int) (Variant, error) {
	b := img.Bounds()
	flat := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	xdraw.Draw(flat, flat.Bounds(), &image.Uniform{C: color.White}, image.Point{}, xdraw.Src)
	xdraw.Draw(flat, flat.Bounds(), img, b.Min, xdraw.Over)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, flat, &jpeg.Options{Quality: 85}); err != nil {
		return Variant{}, fmt.Errorf("encode jpeg: %w", err)
	}
	return Variant{
		Name: fmt.Sprintf("%d.jpg", edge), Width: b.Dx(), Height: b.Dy(),
		ContentType: "image/jpeg", Data: buf.Bytes(),
	}, nil
}

func decodeConfig(f Format, data []byte) (image.Config, error) {
	r := bytes.NewReader(data)
	switch f {
	case JPEG:
		return jpeg.DecodeConfig(r)
	case PNG:
		return png.DecodeConfig(r)
	case WebP:
		return webp.DecodeConfig(r)
	case HEIC:
		return heic.DecodeConfig(r)
	case AVIF:
		return avif.DecodeConfig(r)
	}
	return image.Config{}, ErrUnsupported
}

func decode(f Format, data []byte) (image.Image, error) {
	r := bytes.NewReader(data)
	switch f {
	case JPEG:
		return jpeg.Decode(r)
	case PNG:
		return png.Decode(r)
	case WebP:
		return webp.Decode(r, webp.Options{AutoRotate: true})
	case HEIC:
		return heic.Decode(r) // applies the file's rotation itself
	case AVIF:
		return avif.Decode(r, avif.Options{AutoRotate: true})
	}
	return nil, ErrUnsupported
}

// resize scales the image so its longest edge is edge pixels. Large reductions take a
// quick bilinear pass to twice the target first, so the high-quality Catmull-Rom pass
// stays fast.
func resize(src image.Image, edge int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if max(w, h) <= edge {
		out := image.NewNRGBA(image.Rect(0, 0, w, h))
		xdraw.Draw(out, out.Bounds(), src, b.Min, xdraw.Src)
		return out
	}
	scale := func(img image.Image, longest int, q xdraw.Interpolator) image.Image {
		ib := img.Bounds()
		iw, ih := ib.Dx(), ib.Dy()
		var dw, dh int
		if iw >= ih {
			dw, dh = longest, max(1, ih*longest/iw)
		} else {
			dw, dh = max(1, iw*longest/ih), longest
		}
		out := image.NewNRGBA(image.Rect(0, 0, dw, dh))
		q.Scale(out, out.Bounds(), img, ib, xdraw.Src, nil)
		return out
	}
	if max(w, h) > 2*edge {
		src = scale(src, 2*edge, xdraw.ApproxBiLinear)
	}
	return scale(src, edge, xdraw.CatmullRom)
}

// dHash computes a 64-bit difference hash: the image is shrunk to 9×8 grayscale and
// each bit records whether a pixel is brighter than its right-hand neighbor.
func dHash(img image.Image) uint64 {
	small := image.NewGray(image.Rect(0, 0, 9, 8))
	xdraw.ApproxBiLinear.Scale(small, small.Bounds(), img, img.Bounds(), xdraw.Src, nil)
	var h uint64
	for y := range 8 {
		for x := range 8 {
			h <<= 1
			if small.GrayAt(x, y).Y > small.GrayAt(x+1, y).Y {
				h |= 1
			}
		}
	}
	return h
}

// Distance is the number of differing bits between two perceptual hashes. Distances
// up to about 6 of 64 usually mean the same photo.
func Distance(a, b uint64) int {
	return bits.OnesCount64(a ^ b)
}
