// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/chmbrb/PetCommunityProject/internal/auth"
	"github.com/chmbrb/PetCommunityProject/internal/storage"
)

const gpsMarker = "PC-TEST-GPS-47.6062N"

// photo returns a JPEG whose EXIF holds gpsMarker, standing in for location data.
// Its random block pattern makes it unlike any photo from earlier test runs, which
// stay in the test database and would otherwise match the near-duplicate check.
func photo(t *testing.T, seed uint8) []byte {
	t.Helper()
	blocks := make([]byte, 8*6)
	if _, err := rand.Read(blocks); err != nil {
		t.Fatal(err)
	}
	img := image.NewNRGBA(image.Rect(0, 0, 640, 480))
	for y := range 480 {
		for x := range 640 {
			v := blocks[(y/80)*8+x/80]
			img.SetNRGBA(x, y, color.NRGBA{R: v, G: v ^ seed, B: uint8(x / 3), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	app1 := append([]byte("Exif\x00\x00II*\x00\x08\x00\x00\x00\x00\x00"), gpsMarker...)
	seg := binary.BigEndian.AppendUint16([]byte{0xFF, 0xE1}, uint16(len(app1)+2))
	out := append([]byte{0xFF, 0xD8}, append(seg, app1...)...)
	return append(out, buf.Bytes()[2:]...)
}

// upload posts raw image bytes.
func (e *authTestEnv) upload(c *http.Client, data []byte) (int, map[string]any) {
	e.t.Helper()
	req, _ := http.NewRequest("POST", e.srv.URL+"/api/v1/media?purpose=avatar", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := c.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]any
	raw, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(raw, &body)
	return resp.StatusCode, body
}

// verifiedUser signs up and confirms the email address.
func (e *authTestEnv) verifiedUser(c *http.Client) *passkey {
	e.t.Helper()
	pk := e.signUp(c, uniqueHandle(e.t))
	token := e.deliverEmail(pk.accountID, auth.PurposeVerifyEmail, "/verify-email")
	if status, _ := e.do(c, "POST", "/api/v1/auth/email/verify", map[string]string{"token": token}, nil); status != http.StatusNoContent {
		e.t.Fatal("email verification failed")
	}
	return pk
}

func (e *authTestEnv) process(id string) {
	e.t.Helper()
	if err := e.media.Process(context.Background(), mustUUID(e.t, id)); err != nil {
		e.t.Fatalf("process %s: %v", id, err)
	}
}

func TestPhotoUploadPipeline(t *testing.T) {
	e := newAuthTestEnv(t)
	browser := e.newClient()
	pk := e.signUp(browser, uniqueHandle(t))

	// Uploads need a confirmed email until abuse-imagery scanning is set up.
	if status, body := e.upload(browser, photo(t, 1)); status != http.StatusForbidden || errorCode(body) != "email_unverified" {
		t.Fatalf("upload before confirming email: status %d, body %v", status, body)
	}
	token := e.deliverEmail(pk.accountID, auth.PurposeVerifyEmail, "/verify-email")
	e.do(browser, "POST", "/api/v1/auth/email/verify", map[string]string{"token": token}, nil)

	status, body := e.upload(browser, photo(t, 1))
	if status != http.StatusAccepted || body["status"] != "pending" {
		t.Fatalf("upload: status %d, body %v", status, body)
	}
	id := body["id"].(string)
	quarantine := e.objects.Quarantine.(*storage.MemoryBucket)
	if _, ok := quarantine.Objects()["uploads/"+id]; !ok {
		t.Fatal("raw upload isn't in quarantine")
	}
	if len(e.objects.Media.(*storage.MemoryBucket).Objects()) != 0 {
		t.Fatal("something was published before processing")
	}
	var queued int
	_ = e.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM river_job WHERE kind = 'process_media' AND args->>'media_id' = $1`, id).Scan(&queued)
	if queued != 1 {
		t.Fatalf("processing jobs queued = %d, want 1", queued)
	}

	// Not ready yet, so it can't be the profile photo.
	if status, body := e.do(browser, "PUT", "/api/v1/me/avatar", map[string]string{"media_id": id}, nil); status != http.StatusConflict {
		t.Fatalf("avatar before processing: status %d, body %v", status, body)
	}

	e.process(id)

	_, asset := e.do(browser, "GET", "/api/v1/media/"+id, nil, nil)
	if asset["status"] != "ready" || asset["width"] != float64(640) {
		t.Fatalf("after processing: %v", asset)
	}
	if _, ok := quarantine.Objects()["uploads/"+id]; ok {
		t.Error("raw upload still in quarantine after processing")
	}
	published := e.objects.Media.(*storage.MemoryBucket).Objects()
	// 640 wide: 320.webp, 800.webp at the original 640, and the 800.jpg fallback.
	if len(published) != 3 {
		t.Errorf("published objects = %d, want 3", len(published))
	}
	for key, obj := range published {
		if bytes.Contains(obj.Data, []byte(gpsMarker)) || bytes.Contains(obj.Data, []byte("Exif")) {
			t.Errorf("published %s still contains EXIF data", key)
		}
		if !strings.HasPrefix(key, id+"/") || !strings.Contains(obj.CacheControl, "immutable") {
			t.Errorf("published %s: cache-control %q", key, obj.CacheControl)
		}
	}
	for _, v := range asset["variants"].([]any) {
		if url := v.(map[string]any)["url"].(string); !strings.HasPrefix(url, "https://media.test/"+id+"/") {
			t.Errorf("variant URL %q", url)
		}
	}

	status, me := e.do(browser, "PUT", "/api/v1/me/avatar", map[string]string{"media_id": id}, nil)
	if status != http.StatusOK || len(nested(me, "actor", "avatar").([]any)) != 3 {
		t.Fatalf("set avatar: status %d, body %v", status, me)
	}
	if status, _ := e.do(browser, "DELETE", "/api/v1/me/avatar", nil, nil); status != http.StatusNoContent {
		t.Fatalf("remove avatar: status %d", status)
	}
	if _, me := e.do(browser, "GET", "/api/v1/me", nil, nil); len(nested(me, "actor", "avatar").([]any)) != 0 {
		t.Errorf("avatar after removing: %v", nested(me, "actor", "avatar"))
	}

	// Another account can't see or use the upload.
	mallory := e.newClient()
	e.verifiedUser(mallory)
	if status, _ := e.do(mallory, "GET", "/api/v1/media/"+id, nil, nil); status != http.StatusNotFound {
		t.Errorf("another account reading the upload: status %d, want 404", status)
	}
	if status, _ := e.do(mallory, "PUT", "/api/v1/me/avatar", map[string]string{"media_id": id}, nil); status != http.StatusNotFound {
		t.Errorf("another account using the upload: status %d, want 404", status)
	}
}

func TestUploadRejections(t *testing.T) {
	e := newAuthTestEnv(t)
	browser := e.newClient()
	e.verifiedUser(browser)

	gif := []byte("GIF89a\x01\x00\x01\x00\x80\x00\x00\xff\xff\xff\x00\x00\x00")
	if status, body := e.upload(browser, gif); status != http.StatusUnsupportedMediaType {
		t.Errorf("GIF upload: status %d, body %v", status, body)
	}
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	if status, _ := e.upload(browser, svg); status != http.StatusUnsupportedMediaType {
		t.Errorf("SVG upload: status %d", status)
	}
	if status, _ := e.upload(browser, bytes.Repeat([]byte{0xFF, 0xD8, 0xFF}, 6<<20)); status != http.StatusRequestEntityTooLarge {
		t.Errorf("18 MB upload: status %d, want 413", status)
	}

	// Passes the format check but isn't a real image: accepted, then rejected by the
	// worker, and the raw file is deleted.
	broken := append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte("junk"), 100)...)
	status, body := e.upload(browser, broken)
	if status != http.StatusAccepted {
		t.Fatalf("broken JPEG upload: status %d, body %v", status, body)
	}
	id := body["id"].(string)
	e.process(id)
	_, asset := e.do(browser, "GET", "/api/v1/media/"+id, nil, nil)
	if asset["status"] != "rejected" || asset["rejection_reason"] == "" {
		t.Errorf("broken JPEG after processing: %v", asset)
	}
	if _, ok := e.objects.Quarantine.(*storage.MemoryBucket).Objects()["uploads/"+id]; ok {
		t.Error("rejected upload left in quarantine")
	}
}

func TestSamePhotoFromAnotherAccountIsFlagged(t *testing.T) {
	e := newAuthTestEnv(t)
	original := photo(t, 77)

	alice := e.newClient()
	e.verifiedUser(alice)
	_, body := e.upload(alice, original)
	first := body["id"].(string)
	e.process(first)

	// Mallory re-uploads the same photo, re-encoded at a different size.
	img, _ := jpeg.Decode(bytes.NewReader(original))
	small := image.NewNRGBA(image.Rect(0, 0, 320, 240))
	for y := range 240 {
		for x := range 320 {
			small.Set(x, y, img.At(x*2, y*2))
		}
	}
	var copyBuf bytes.Buffer
	_ = jpeg.Encode(&copyBuf, small, &jpeg.Options{Quality: 60})

	mallory := e.newClient()
	e.verifiedUser(mallory)
	_, body = e.upload(mallory, copyBuf.Bytes())
	second := body["id"].(string)
	e.process(second)

	var similarTo *string
	_ = e.pool.QueryRow(context.Background(), `SELECT similar_to::text FROM media_assets WHERE id = $1`, second).Scan(&similarTo)
	if similarTo == nil || *similarTo != first {
		t.Errorf("re-uploaded photo similar_to = %v, want %s", similarTo, first)
	}
	// It's still published; flagging is for moderators, not automatic blocking.
	_, asset := e.do(mallory, "GET", "/api/v1/media/"+second, nil, nil)
	if asset["status"] != "ready" {
		t.Errorf("flagged upload status = %v, want ready", asset["status"])
	}
}
