package xtcp

import (
	"errors"
	"fmt"
	"strings"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
)

// ErrInsecureTransport marks a config that would send a credential over a
// plaintext connection without the operator opting in. The CLI maps it to a
// usage exit; ConfigService.Set refuses it.
var ErrInsecureTransport = errors.New("insecure transport")

// S3Endpoint resolves the s3parquet endpoint to the host[:port] form minio
// expects, and whether to use TLS. The endpoint is the address after
// `s3parquet:` in Dest, else S3Endpoint (-s3Endpoint / S3_ENDPOINT).
//
//   - https://host[:port] and a bare host[:port] use TLS.
//   - http://host[:port] is refused unless S3AllowInsecure is set: the access
//     key signs every request and each object crosses the network, so
//     plaintext is for local test fixtures only.
//   - any other scheme is refused.
func S3Endpoint(c *xtcp_config.XtcpConfig) (host string, secure bool, err error) {
	endpoint := c.GetS3Endpoint()
	if rest, ok := strings.CutPrefix(c.GetDest(), schemeS3Parquet+":"); ok && rest != "" {
		endpoint = rest
	}
	if endpoint == "" {
		return "", false, errors.New("s3parquet endpoint is empty (set -dest s3parquet:<endpoint> or S3_ENDPOINT)")
	}
	switch {
	case strings.HasPrefix(endpoint, "https://"):
		host, secure = strings.TrimPrefix(endpoint, "https://"), true
	case strings.HasPrefix(endpoint, "http://"):
		if !c.GetS3AllowInsecure() {
			return "", false, fmt.Errorf("%w: s3parquet endpoint %q is plaintext http; the S3 credential and every "+
				"object would cross the network unencrypted. Use https:// (or a bare host:port, which uses TLS), "+
				"or set -s3AllowInsecure (S3_ALLOW_INSECURE=true) for a local test fixture", ErrInsecureTransport, endpoint)
		}
		host, secure = strings.TrimPrefix(endpoint, "http://"), false
	case strings.Contains(endpoint, "://"):
		scheme, _, _ := strings.Cut(endpoint, "://")
		return "", false, fmt.Errorf("s3parquet endpoint %q: unsupported scheme %q (use https://, or http:// with -s3AllowInsecure)", endpoint, scheme)
	default:
		host, secure = endpoint, true
	}
	if host == "" {
		return "", false, fmt.Errorf("s3parquet endpoint %q has no host", endpoint)
	}
	return host, secure, nil
}

// CheckS3Transport applies S3Endpoint's transport rules when Dest selects
// s3parquet; any other destination passes. It runs at startup and in
// ConfigService.Set, so a plaintext endpoint fails before the daemon starts
// instead of when the destination is built.
func CheckS3Transport(c *xtcp_config.XtcpConfig) error {
	if scheme, _, _ := strings.Cut(c.GetDest(), ":"); scheme != schemeS3Parquet {
		return nil
	}
	_, _, err := S3Endpoint(c)
	return err
}
