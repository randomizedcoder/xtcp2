// Package s3 uploads the combined Parquet file to an S3-compatible bucket
// using minio-go v7, matching the credential and endpoint handling used by the
// xtcp2 s3parquet destination (static V4 creds, scheme-derived TLS, and the
// Docker `_FILE` secret convention handled by the caller).
package s3

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Config holds S3 connection and object placement settings.
type Config struct {
	Endpoint        string // may include http:// or https:// scheme
	Bucket          string
	Region          string
	Prefix          string // key prefix; joined with the filename
	AccessKey       string
	SecretKey       string
	SkipBucketProbe bool
}

// Uploader is the seam used by the collector; a fake implements it in tests.
type Uploader interface {
	Put(ctx context.Context, key string, body io.Reader, size int64) (string, error)
}

// minioUploader is the production Uploader backed by a *minio.Client.
type minioUploader struct {
	client *minio.Client
	cfg    Config
}

// New builds a minio-backed Uploader. The endpoint scheme selects TLS and is
// stripped to a bare host, mirroring the xtcp2 s3parquet client. When
// SkipBucketProbe is false it verifies the bucket exists up front.
func New(ctx context.Context, cfg Config) (Uploader, error) {
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("s3: bucket is required")
	}
	endpoint := cfg.Endpoint
	secure := true
	switch {
	case strings.HasPrefix(endpoint, "https://"):
		endpoint = strings.TrimPrefix(endpoint, "https://")
		secure = true
	case strings.HasPrefix(endpoint, "http://"):
		endpoint = strings.TrimPrefix(endpoint, "http://")
		secure = false
	}
	endpoint = strings.TrimSuffix(endpoint, "/")
	region := cfg.Region
	if region == "" {
		region = "us-east-1"
	}
	cl, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: secure,
		Region: region,
	})
	if err != nil {
		return nil, fmt.Errorf("s3: new client: %w", err)
	}
	if !cfg.SkipBucketProbe {
		exists, err := cl.BucketExists(ctx, cfg.Bucket)
		if err != nil {
			return nil, fmt.Errorf("s3: bucket probe: %w", err)
		}
		if !exists {
			return nil, fmt.Errorf("s3: bucket %q does not exist", cfg.Bucket)
		}
	}
	return &minioUploader{client: cl, cfg: cfg}, nil
}

// Key joins the configured prefix with filename, e.g. "prefix/2026-09-09.parquet".
func (cfg Config) Key(filename string) string {
	p := strings.Trim(cfg.Prefix, "/")
	if p == "" {
		return filename
	}
	return p + "/" + filename
}

// Put uploads body of the given size under key and returns the s3:// URL.
func (u *minioUploader) Put(ctx context.Context, key string, body io.Reader, size int64) (string, error) {
	_, err := u.client.PutObject(ctx, u.cfg.Bucket, key, body, size, minio.PutObjectOptions{
		ContentType: "application/octet-stream",
	})
	if err != nil {
		return "", fmt.Errorf("s3: put %s: %w", key, err)
	}
	return fmt.Sprintf("s3://%s/%s", u.cfg.Bucket, key), nil
}

// SecretFromFile reads a secret from path (the Docker `_FILE` convention),
// trimming surrounding whitespace. An empty path returns "" with no error so
// callers can fall back to a direct value.
func SecretFromFile(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("s3: read secret file: %w", err)
	}
	return strings.TrimSpace(string(b)), nil
}
