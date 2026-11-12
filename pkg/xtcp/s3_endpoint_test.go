package xtcp

import (
	"errors"
	"testing"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
)

func TestS3Endpoint(t *testing.T) {
	t.Parallel()
	type want struct {
		host     string
		secure   bool
		err      bool
		insecure bool // err is ErrInsecureTransport
	}
	cases := []struct {
		name        string
		category    string
		description string
		cfg         *xtcp_config.XtcpConfig
		expected    want
	}{
		{"https_dest", "positive", "https:// in -dest uses TLS",
			&xtcp_config.XtcpConfig{Dest: "s3parquet:https://s3.example:443"},
			want{host: "s3.example:443", secure: true}},
		{"s3_bare_endpoint", "boundary", "a bare host:port uses TLS (was plaintext)",
			&xtcp_config.XtcpConfig{Dest: "s3parquet:s3.example:9000"},
			want{host: "s3.example:9000", secure: true}},
		{"endpoint_from_flag", "positive", "bare -dest s3parquet: falls back to S3Endpoint",
			&xtcp_config.XtcpConfig{Dest: "s3parquet:", S3Endpoint: "https://s3.example"},
			want{host: "s3.example", secure: true}},
		{"s3_http_refused", "negative", "http:// without the opt-in is refused",
			&xtcp_config.XtcpConfig{Dest: "s3parquet:http://127.0.0.1:9000"},
			want{err: true, insecure: true}},
		{"s3_http_flag_refused", "negative", "http:// via S3Endpoint is refused too",
			&xtcp_config.XtcpConfig{Dest: "s3parquet", S3Endpoint: "http://127.0.0.1:9000"},
			want{err: true, insecure: true}},
		{"s3_http_allowed", "positive", "http:// with -s3AllowInsecure is plaintext",
			&xtcp_config.XtcpConfig{Dest: "s3parquet:http://127.0.0.1:9000", S3AllowInsecure: true},
			want{host: "127.0.0.1:9000", secure: false}},
		{"unknown_scheme", "negative", "another URL scheme is refused",
			&xtcp_config.XtcpConfig{Dest: "s3parquet:ftp://s3.example"},
			want{err: true}},
		{"empty", "corner", "no endpoint anywhere",
			&xtcp_config.XtcpConfig{Dest: "s3parquet:"},
			want{err: true}},
		{"scheme_only", "corner", "https:// with no host",
			&xtcp_config.XtcpConfig{Dest: "s3parquet:https://"},
			want{err: true}},
	}
	for _, tc := range cases {
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			host, secure, err := S3Endpoint(tc.cfg)
			if (err != nil) != tc.expected.err {
				t.Fatalf("%s: err = %v, want err %v", tc.description, err, tc.expected.err)
			}
			if got := errors.Is(err, ErrInsecureTransport); got != tc.expected.insecure {
				t.Errorf("%s: ErrInsecureTransport = %v, want %v (%v)", tc.description, got, tc.expected.insecure, err)
			}
			if err == nil && (host != tc.expected.host || secure != tc.expected.secure) {
				t.Errorf("%s: got (%q, %v), want (%q, %v)", tc.description, host, secure, tc.expected.host, tc.expected.secure)
			}
		})
	}
}

func TestCheckS3Transport(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		category    string
		description string
		cfg         *xtcp_config.XtcpConfig
		expected    bool // refused
	}{
		{"not_s3", "positive", "other destinations are not its concern", &xtcp_config.XtcpConfig{Dest: "kafka:b:9092"}, false},
		{"s3_https", "positive", "https passes", &xtcp_config.XtcpConfig{Dest: "s3parquet:https://s3"}, false},
		{"s3_http", "negative", "plaintext is refused", &xtcp_config.XtcpConfig{Dest: "s3parquet:http://s3"}, true},
		{"http_dest", "corner", "the http destination is not s3parquet", &xtcp_config.XtcpConfig{Dest: "http://collector"}, false},
		{"nil", "corner", "nil config", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			if got := CheckS3Transport(tc.cfg) != nil; got != tc.expected {
				t.Errorf("%s: refused = %v, want %v", tc.description, got, tc.expected)
			}
		})
	}
}
