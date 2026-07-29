//go:build dest_s3parquet

package xtcp

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// TestClassifyUploadErr pins the bounded set of upload-error classes so the
// uploadErrorClass counter can never grow beyond these label values, and so a
// production error (e.g. the AWS-S3 TLS verify failure) lands in the expected
// bucket. Every case must return one of the s3ErrClass* constants.
func TestClassifyUploadErr(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, s3ErrClassOther},
		{"canceled", context.Canceled, s3ErrClassCanceled},
		{"deadline", context.DeadlineExceeded, s3ErrClassTimeout},
		{"deadline wrapped", fmt.Errorf("PutObject: %w", context.DeadlineExceeded), s3ErrClassTimeout},
		{"tls x509", errors.New(`tls: failed to verify certificate: x509: certificate signed by unknown authority`), s3ErrClassTLS},
		{"dns", errors.New(`dial tcp: lookup runpod-xtcp-dev.s3.amazonaws.com: no such host`), s3ErrClassDNS},
		{"refused", errors.New(`dial tcp 10.0.0.1:443: connect: connection refused`), s3ErrClassRefused},
		{"io timeout", errors.New(`Put "https://…": read tcp: i/o timeout`), s3ErrClassTimeout},
		{"other", errors.New(`AccessDenied: you do not have permission to access this resource`), s3ErrClassOther},
	}
	// Guard: the classifier must only ever emit values from this closed set.
	allowed := map[string]bool{
		s3ErrClassTLS: true, s3ErrClassTimeout: true, s3ErrClassCanceled: true,
		s3ErrClassDNS: true, s3ErrClassRefused: true, s3ErrClassOther: true,
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyUploadErr(tt.err)
			if got != tt.want {
				t.Errorf("classifyUploadErr(%v) = %q, want %q", tt.err, got, tt.want)
			}
			if !allowed[got] {
				t.Errorf("classifyUploadErr(%v) returned unbounded label %q (cardinality risk)", tt.err, got)
			}
		})
	}
}
