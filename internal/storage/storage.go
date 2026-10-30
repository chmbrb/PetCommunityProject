// SPDX-License-Identifier: AGPL-3.0-or-later

// Package storage stores media in S3-compatible object storage (ADR 0010): a private
// quarantine bucket for raw uploads and a public bucket for processed images.
package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"sync"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/chmbrb/PetCommunityProject/internal/config"
)

// ErrNotFound means the object doesn't exist.
var ErrNotFound = errors.New("object not found")

// PutOptions are the stored object's HTTP metadata.
type PutOptions struct {
	ContentType  string
	CacheControl string
}

// Bucket is one storage bucket.
type Bucket interface {
	Put(ctx context.Context, key string, r io.Reader, size int64, opts PutOptions) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
}

// Storage is the instance's media storage.
type Storage struct {
	// Quarantine holds raw, unprocessed uploads. It's never publicly readable.
	Quarantine Bucket
	// Media holds processed images, publicly readable at MediaURL.
	Media    Bucket
	mediaURL string
}

// MediaURL returns the public address of an object in the media bucket.
func (s *Storage) MediaURL(key string) string {
	return s.mediaURL + "/" + key
}

// NewS3 connects to the configured S3-compatible storage.
func NewS3(cfg config.Storage) (*Storage, error) {
	u, err := url.Parse(cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse PC_S3_ENDPOINT: %w", err)
	}
	client, err := minio.New(u.Host, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: u.Scheme == "https",
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("create S3 client: %w", err)
	}
	return &Storage{
		Quarantine: &s3Bucket{client: client, name: cfg.QuarantineBucket},
		Media:      &s3Bucket{client: client, name: cfg.MediaBucket},
		mediaURL:   cfg.MediaURL,
	}, nil
}

type s3Bucket struct {
	client *minio.Client
	name   string
}

func (b *s3Bucket) Put(ctx context.Context, key string, r io.Reader, size int64, opts PutOptions) error {
	_, err := b.client.PutObject(ctx, b.name, key, r, size, minio.PutObjectOptions{
		ContentType: opts.ContentType, CacheControl: opts.CacheControl,
	})
	if err != nil {
		return fmt.Errorf("put %s/%s: %w", b.name, key, err)
	}
	return nil
}

func (b *s3Bucket) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := b.client.GetObject(ctx, b.name, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("get %s/%s: %w", b.name, key, err)
	}
	// GetObject is lazy; Stat surfaces a missing object now rather than on first read.
	if _, err := obj.Stat(); err != nil {
		_ = obj.Close()
		if minio.ToErrorResponse(err).Code == minio.NoSuchKey {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get %s/%s: %w", b.name, key, err)
	}
	return obj, nil
}

func (b *s3Bucket) Delete(ctx context.Context, key string) error {
	if err := b.client.RemoveObject(ctx, b.name, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("delete %s/%s: %w", b.name, key, err)
	}
	return nil
}

// NewMemory returns storage kept in memory, for tests.
func NewMemory(mediaURL string) *Storage {
	return &Storage{Quarantine: &MemoryBucket{}, Media: &MemoryBucket{}, mediaURL: mediaURL}
}

// MemoryBucket is an in-memory Bucket for tests.
type MemoryBucket struct {
	mu      sync.Mutex
	objects map[string]MemoryObject
}

// MemoryObject is a stored object.
type MemoryObject struct {
	Data []byte
	PutOptions
}

func (b *MemoryBucket) Put(_ context.Context, key string, r io.Reader, _ int64, opts PutOptions) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.objects == nil {
		b.objects = map[string]MemoryObject{}
	}
	b.objects[key] = MemoryObject{Data: data, PutOptions: opts}
	return nil
}

func (b *MemoryBucket) Get(_ context.Context, key string) (io.ReadCloser, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	obj, ok := b.objects[key]
	if !ok {
		return nil, ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(obj.Data)), nil
}

func (b *MemoryBucket) Delete(_ context.Context, key string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.objects, key)
	return nil
}

// Objects returns a copy of the stored objects, by key.
func (b *MemoryBucket) Objects() map[string]MemoryObject {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make(map[string]MemoryObject, len(b.objects))
	for k, v := range b.objects {
		out[k] = v
	}
	return out
}
