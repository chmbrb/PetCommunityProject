// SPDX-License-Identifier: AGPL-3.0-or-later

package media

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/gen2brain/avif"
	"github.com/gen2brain/webp"
)

// secretMarker is placed in test EXIF data; it must never appear in any output.
const secretMarker = "PC-SECRET-GPS-47.6062N-122.3321W"

// testImage draws a gradient with a dark block in the top-left corner, so
// orientation and similarity can be checked.
func testImage(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			c := color.NRGBA{R: uint8(x * 255 / w), G: uint8(y * 255 / h), B: 128, A: 255}
			if x < w/4 && y < h/4 {
				c = color.NRGBA{A: 255}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

// jpegWithExif encodes a JPEG and inserts an EXIF block with an orientation and an
// ImageDescription holding secretMarker, standing in for GPS and camera data.
func jpegWithExif(t *testing.T, img image.Image, orientation uint16) []byte {
	t.Helper()
	var plain bytes.Buffer
	if err := jpeg.Encode(&plain, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	desc := []byte(secretMarker + "\x00")
	le := binary.LittleEndian
	tiff := []byte("II*\x00")
	tiff = le.AppendUint32(tiff, 8) // IFD0 offset
	tiff = le.AppendUint16(tiff, 2) // two entries
	// Orientation: SHORT, count 1, value inline.
	tiff = le.AppendUint16(tiff, 0x0112)
	tiff = le.AppendUint16(tiff, 3)
	tiff = le.AppendUint32(tiff, 1)
	tiff = le.AppendUint16(tiff, orientation)
	tiff = le.AppendUint16(tiff, 0)
	// ImageDescription: ASCII, stored after the IFD.
	tiff = le.AppendUint16(tiff, 0x010E)
	tiff = le.AppendUint16(tiff, 2)
	tiff = le.AppendUint32(tiff, uint32(len(desc)))
	tiff = le.AppendUint32(tiff, uint32(8+2+2*12+4))
	tiff = le.AppendUint32(tiff, 0) // no next IFD
	tiff = append(tiff, desc...)

	app1 := append([]byte("Exif\x00\x00"), tiff...)
	seg := []byte{0xFF, 0xE1}
	seg = binary.BigEndian.AppendUint16(seg, uint16(len(app1)+2))
	seg = append(seg, app1...)

	out := append([]byte{}, plain.Bytes()[:2]...) // SOI
	out = append(out, seg...)
	return append(out, plain.Bytes()[2:]...)
}

func variant(t *testing.T, r Result, name string) Variant {
	t.Helper()
	for _, v := range r.Variants {
		if v.Name == name {
			return v
		}
	}
	t.Fatalf("no variant %q in %v", name, names(r))
	return Variant{}
}

func names(r Result) []string {
	var out []string
	for _, v := range r.Variants {
		out = append(out, v.Name)
	}
	return out
}

func TestProcessStripsMetadataAndAppliesOrientation(t *testing.T) {
	// A 1200×600 landscape photo stored sideways: orientation 6 means "rotate 90°
	// clockwise to display", so the upright image is 600×1200.
	data := jpegWithExif(t, testImage(1200, 600), 6)
	if !bytes.Contains(data, []byte(secretMarker)) {
		t.Fatal("test input lacks the marker")
	}

	res, err := Process(data)
	if err != nil {
		t.Fatal(err)
	}
	if res.Format != JPEG || res.Width != 600 || res.Height != 1200 {
		t.Errorf("result %s %dx%d, want upright JPEG 600x1200", res.Format, res.Width, res.Height)
	}
	for _, v := range res.Variants {
		if bytes.Contains(v.Data, []byte(secretMarker)) || bytes.Contains(v.Data, []byte("Exif")) {
			t.Errorf("variant %s still contains EXIF data", v.Name)
		}
		if v.Height < v.Width {
			t.Errorf("variant %s is %dx%d, want portrait after rotation", v.Name, v.Width, v.Height)
		}
	}
	// 1200 is the longest edge: 320 and 800 are made, and 1600 at the original 1200
	// (never enlarged).
	if got := names(res); len(got) != 4 {
		t.Errorf("variants = %v, want 320.webp, 800.webp, 1600.webp, 800.jpg", got)
	}
	if large := variant(t, res, "1600.webp"); large.Width != 600 || large.Height != 1200 {
		t.Errorf("1600.webp is %dx%d, want the original 600x1200", large.Width, large.Height)
	}
	medium := variant(t, res, "800.webp")
	if medium.Width != 400 || medium.Height != 800 {
		t.Errorf("800.webp is %dx%d, want 400x800", medium.Width, medium.Height)
	}
	img, err := webp.Decode(bytes.NewReader(medium.Data))
	if err != nil {
		t.Fatalf("output webp doesn't decode: %v", err)
	}
	// The dark block was top-left before rotating 90° clockwise, so it's now top-right.
	if c := color.NRGBAModel.Convert(img.At(380, 10)).(color.NRGBA); c.R > 60 || c.G > 60 {
		t.Errorf("top-right pixel after rotation = %v, want dark", c)
	}
	fallback := variant(t, res, "800.jpg")
	if _, err := jpeg.Decode(bytes.NewReader(fallback.Data)); err != nil {
		t.Errorf("JPEG fallback doesn't decode: %v", err)
	}
}

func TestProcessFormats(t *testing.T) {
	img := testImage(900, 700)
	var pngBuf, webpBuf, avifBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		t.Fatal(err)
	}
	if err := webp.Encode(&webpBuf, img); err != nil {
		t.Fatal(err)
	}
	if err := avif.Encode(&avifBuf, img); err != nil {
		t.Fatal(err)
	}
	for format, data := range map[Format][]byte{PNG: pngBuf.Bytes(), WebP: webpBuf.Bytes(), AVIF: avifBuf.Bytes()} {
		t.Run(string(format), func(t *testing.T) {
			res, err := Process(data)
			if err != nil {
				t.Fatal(err)
			}
			if res.Format != format || res.Width != 900 || res.Height != 700 {
				t.Errorf("result %s %dx%d", res.Format, res.Width, res.Height)
			}
			if v := variant(t, res, "320.webp"); v.Width != 320 {
				t.Errorf("320.webp width = %d", v.Width)
			}
		})
	}
}

func TestProcessSmallImageIsNotEnlarged(t *testing.T) {
	var buf bytes.Buffer
	_ = png.Encode(&buf, testImage(100, 50))
	res, err := Process(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range res.Variants {
		if v.Width != 100 || v.Height != 50 {
			t.Errorf("variant %s is %dx%d, want the original 100x50", v.Name, v.Width, v.Height)
		}
	}
	if len(res.Variants) != 2 {
		t.Errorf("variants = %v, want one WebP and the JPEG fallback", names(res))
	}
}

func TestProcessTransparentPNGFallbackIsFlattened(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 50, 50)) // fully transparent
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	res, err := Process(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	out, _ := jpeg.Decode(bytes.NewReader(variant(t, res, "800.jpg").Data))
	if r, g, b, _ := out.At(25, 25).RGBA(); r>>8 < 240 || g>>8 < 240 || b>>8 < 240 {
		t.Errorf("transparent area in JPEG = %d,%d,%d, want white", r>>8, g>>8, b>>8)
	}
}

// pngClaiming returns a tiny PNG whose header claims the given dimensions.
func pngClaiming(t *testing.T, w, h uint32) []byte {
	t.Helper()
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewGray(image.Rect(0, 0, 1, 1)))
	data := buf.Bytes()
	// IHDR data starts at byte 16: width, height, then 5 more bytes; CRC follows.
	binary.BigEndian.PutUint32(data[16:20], w)
	binary.BigEndian.PutUint32(data[20:24], h)
	binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
	return data
}

func TestProcessRejects(t *testing.T) {
	var good bytes.Buffer
	_ = jpeg.Encode(&good, testImage(200, 200), nil)

	tests := []struct {
		name string
		data []byte
		want error
	}{
		{"GIF", []byte("GIF89a\x01\x00\x01\x00\x80\x00\x00\xff\xff\xff\x00\x00\x00!\xf9\x04"), ErrUnsupported},
		{"SVG", []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`), ErrUnsupported},
		{"empty", nil, ErrUnsupported},
		{"decompression bomb", pngClaiming(t, 60000, 60000), ErrTooLarge},
		{"truncated JPEG", good.Bytes()[:len(good.Bytes())/3], ErrUndecodable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Process(tt.data); !errors.Is(err, tt.want) {
				t.Errorf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestPerceptualHash(t *testing.T) {
	big := testImage(1600, 1200)
	same := resize(big, 320)
	other := image.NewNRGBA(image.Rect(0, 0, 320, 240))
	for y := range 240 {
		for x := range 320 {
			other.SetNRGBA(x, y, color.NRGBA{R: uint8((x * y) % 256), G: uint8(y), B: uint8(x), A: 255})
		}
	}
	if d := Distance(dHash(big), dHash(same)); d > 6 {
		t.Errorf("same photo at another size: distance %d, want ≤ 6", d)
	}
	if d := Distance(dHash(big), dHash(other)); d < 16 {
		t.Errorf("different photos: distance %d, want ≥ 16", d)
	}
}

func TestSniff(t *testing.T) {
	ftyp := func(major string, compat ...string) []byte {
		box := []byte{0, 0, 0, 0}
		box = append(box, "ftyp"+major+"\x00\x00\x00\x00"...)
		for _, c := range compat {
			box = append(box, c...)
		}
		binary.BigEndian.PutUint32(box[0:4], uint32(len(box)))
		return append(box, make([]byte, 16)...)
	}
	tests := []struct {
		head []byte
		want Format
		ok   bool
	}{
		{[]byte{0xFF, 0xD8, 0xFF, 0xE0}, JPEG, true},
		{[]byte("\x89PNG\r\n\x1a\n...."), PNG, true},
		{[]byte("RIFF\x00\x00\x00\x00WEBPVP8 "), WebP, true},
		{ftyp("heic", "mif1", "heic"), HEIC, true},
		{ftyp("mif1", "mif1", "heic"), HEIC, true},
		{ftyp("avif", "mif1", "avif"), AVIF, true},
		{ftyp("mif1", "mif1", "avif"), AVIF, true},
		{ftyp("isom", "isom", "mp41"), "", false}, // MP4 video
		{[]byte("RIFF\x00\x00\x00\x00WAVEfmt "), "", false},
		{[]byte("<svg"), "", false},
	}
	for _, tt := range tests {
		got, ok := Sniff(tt.head)
		if got != tt.want || ok != tt.ok {
			t.Errorf("Sniff(%q) = %q, %v; want %q, %v", tt.head[:min(12, len(tt.head))], got, ok, tt.want, tt.ok)
		}
	}
}

func TestOrient(t *testing.T) {
	// A 2x1 image: red on the left, blue on the right.
	src := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	red, blue := color.NRGBA{R: 255, A: 255}, color.NRGBA{B: 255, A: 255}
	src.SetNRGBA(0, 0, red)
	src.SetNRGBA(1, 0, blue)

	cw := orient(src, 6).(*image.NRGBA) // rotate 90° clockwise: 1x2, red on top
	if cw.Bounds().Dx() != 1 || cw.NRGBAAt(0, 0) != red || cw.NRGBAAt(0, 1) != blue {
		t.Errorf("orientation 6: got %v", cw.Pix)
	}
	ccw := orient(src, 8).(*image.NRGBA) // rotate 90° counter-clockwise: blue on top
	if ccw.NRGBAAt(0, 0) != blue || ccw.NRGBAAt(0, 1) != red {
		t.Errorf("orientation 8: got %v", ccw.Pix)
	}
	if orient(src, 1) != image.Image(src) {
		t.Error("orientation 1 should return the image unchanged")
	}
}

func TestCodecsArePure(t *testing.T) {
	if err := CheckPureCodecs(); err != nil {
		t.Fatal(err)
	}
}

// FuzzJPEGOrientation feeds arbitrary bytes to the EXIF reader, which parses
// untrusted input: it must never panic.
func FuzzJPEGOrientation(f *testing.F) {
	f.Add([]byte{0xFF, 0xD8, 0xFF, 0xE1, 0x00, 0x08, 'E', 'x', 'i', 'f', 0, 0})
	f.Add([]byte{0xFF, 0xD8, 0xFF, 0xE1, 0xFF, 0xFF})
	f.Fuzz(func(t *testing.T, data []byte) {
		if o := jpegOrientation(data); o < 1 || o > 8 {
			t.Fatalf("orientation %d out of range", o)
		}
	})
}

// FuzzSniff checks the format detector never panics on arbitrary input.
func FuzzSniff(f *testing.F) {
	f.Add([]byte("\x00\x00\x00\x08ftyp"))
	f.Add([]byte("\xff\xff\xff\xffftypavif"))
	f.Fuzz(func(t *testing.T, data []byte) {
		Sniff(data)
	})
}
