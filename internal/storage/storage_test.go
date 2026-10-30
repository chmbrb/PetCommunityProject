// SPDX-License-Identifier: AGPL-3.0-or-later

package storage

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/chmbrb/PetCommunityProject/internal/config"
)

// exercise checks the Bucket contract.
func exercise(t *testing.T, b Bucket) {
	t.Helper()
	ctx := context.Background()
	key := "test/" + t.Name() + ".txt"

	if err := b.Put(ctx, key, strings.NewReader("hello"), 5, PutOptions{ContentType: "text/plain"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	r, err := b.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	data, _ := io.ReadAll(r)
	_ = r.Close()
	if string(data) != "hello" {
		t.Errorf("Get = %q, want hello", data)
	}
	if err := b.Delete(ctx, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := b.Get(ctx, key); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after Delete: err = %v, want ErrNotFound", err)
	}
}

func TestMemoryBucket(t *testing.T) {
	s := NewMemory("https://media.example.org")
	exercise(t, s.Quarantine)
	if got := s.MediaURL("a/b.webp"); got != "https://media.example.org/a/b.webp" {
		t.Errorf("MediaURL = %q", got)
	}
}

// TestS3Bucket runs against real S3-compatible storage, e.g. the development MinIO:
// PC_TEST_S3=1 go test ./internal/storage
func TestS3Bucket(t *testing.T) {
	if os.Getenv("PC_TEST_S3") == "" {
		t.Skip("PC_TEST_S3 not set; skipping S3 integration test")
	}
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewS3(cfg.Storage)
	if err != nil {
		t.Fatal(err)
	}
	exercise(t, s.Quarantine)
	exercise(t, s.Media)
}
