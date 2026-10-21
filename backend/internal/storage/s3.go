// Package storage keeps product photos in S3-compatible object storage.
package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3 stores objects in one bucket.
type S3 struct {
	client *minio.Client
	bucket string
}

// NewS3 connects to an S3 endpoint such as http://minio:9000 (path-style addressing).
func NewS3(endpoint, region, accessKey, secretKey, bucket string) (*S3, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("storage: invalid endpoint %q", endpoint)
	}
	if bucket == "" {
		return nil, errors.New("storage: bucket is required")
	}
	client, err := minio.New(u.Host, &minio.Options{
		Creds:        credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure:       u.Scheme == "https",
		Region:       region,
		BucketLookup: minio.BucketLookupPath,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: create client: %w", err)
	}
	return &S3{client: client, bucket: bucket}, nil
}

// Put uploads data under key.
func (s *S3) Put(ctx context.Context, key, contentType string, data []byte) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: contentType, CacheControl: "public, max-age=31536000, immutable"})
	if err != nil {
		return fmt.Errorf("storage: put %s: %w", key, err)
	}
	return nil
}

// Delete removes key; a missing object is not an error.
func (s *S3) Delete(ctx context.Context, key string) error {
	if err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("storage: delete %s: %w", key, err)
	}
	return nil
}
