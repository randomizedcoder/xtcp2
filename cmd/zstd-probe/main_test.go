package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/prometheus/client_golang/prometheus"
)

func writeZstd(t *testing.T, payload []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "payload.txt.zst")
	var b bytes.Buffer
	enc, err := zstd.NewWriter(&b)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if _, err := enc.Write(payload); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

// freeAddr binds and immediately releases a loopback port, so the returned
// address is free at the moment it is returned. Nothing stops another process
// taking it in between, which is why only the bound-port row depends on it.
func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("probe listen: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("probe close: %v", err)
	}
	return addr
}

// startMetricsOutcome is the whole observable result of one startMetrics call:
// the three return values, whether /readyz answers, and whether the requested
// address is left bound. A row asserting only the error could not tell a
// refused bind from a leaked listener.
type startMetricsOutcome struct {
	serverNonNil  bool
	addrNonEmpty  bool
	wantErr       bool
	readyzStatus  int  // probed only when non-zero
	addrFreeAfter bool // assert the requested address is left unbound
}

// TestStartMetrics covers the ctx parameter the noctx fix added, and records
// what it does and does not buy.
//
// Go's net package consults ctx during name resolution and not around the
// bind syscall, so a canceled context aborts a hostname listen but a literal
// IP:port listen still succeeds. Both are rows below: the first is the
// behavior gained, the second is the limit, measured rather than assumed.
func TestStartMetrics(t *testing.T) {
	tests := []struct {
		description string
		setup       func(t *testing.T) (context.Context, string)
		expected    startMetricsOutcome
	}{
		{
			description: "positive: a free loopback port binds and /readyz answers 200",
			setup: func(t *testing.T) (context.Context, string) {
				return context.Background(), "127.0.0.1:0"
			},
			expected: startMetricsOutcome{
				serverNonNil: true,
				addrNonEmpty: true,
				readyzStatus: http.StatusOK,
			},
		},
		{
			description: "negative: an empty addr returns nil, empty string and no error, binding nothing",
			setup: func(t *testing.T) (context.Context, string) {
				return context.Background(), ""
			},
			expected: startMetricsOutcome{},
		},
		{
			description: "boundary: an addr already bound by another listener returns an error rather than panicking",
			setup: func(t *testing.T) (context.Context, string) {
				ln, err := net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatalf("occupying listen: %v", err)
				}
				t.Cleanup(func() { _ = ln.Close() })
				return context.Background(), ln.Addr().String()
			},
			expected: startMetricsOutcome{wantErr: true},
		},
		{
			description: "corner: an already-canceled ctx with a hostname addr returns an error and binds nothing, because ctx reaches the resolver",
			setup: func(t *testing.T) (context.Context, string) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				_, port, err := net.SplitHostPort(freeAddr(t))
				if err != nil {
					t.Fatalf("SplitHostPort: %v", err)
				}
				return ctx, net.JoinHostPort("localhost", port)
			},
			expected: startMetricsOutcome{wantErr: true, addrFreeAfter: true},
		},
		{
			description: "corner: an already-canceled ctx with a literal IP:port still binds, which is the documented limit of the ctx parameter",
			setup: func(t *testing.T) (context.Context, string) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx, "127.0.0.1:0"
			},
			expected: startMetricsOutcome{
				serverNonNil: true,
				addrNonEmpty: true,
				readyzStatus: http.StatusOK,
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			ctx, addr := tc.setup(t)
			reg := prometheus.NewRegistry()
			srv, actualAddr, err := startMetrics(ctx, addr, "/metrics", reg)
			if srv != nil {
				t.Cleanup(func() {
					shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					_ = srv.Shutdown(shutdownCtx)
				})
			}
			if (err != nil) != tc.expected.wantErr {
				t.Fatalf("err = %v, want error = %v", err, tc.expected.wantErr)
			}
			if (srv != nil) != tc.expected.serverNonNil {
				t.Fatalf("server non-nil = %v, want %v", srv != nil, tc.expected.serverNonNil)
			}
			if (actualAddr != "") != tc.expected.addrNonEmpty {
				t.Fatalf("addr = %q, want non-empty = %v", actualAddr, tc.expected.addrNonEmpty)
			}
			if tc.expected.readyzStatus != 0 {
				resp, getErr := http.Get("http://" + actualAddr + "/readyz")
				if getErr != nil {
					t.Fatalf("GET /readyz: %v", getErr)
				}
				defer func() { _ = resp.Body.Close() }()
				if _, copyErr := io.Copy(io.Discard, resp.Body); copyErr != nil {
					t.Fatalf("drain /readyz: %v", copyErr)
				}
				if resp.StatusCode != tc.expected.readyzStatus {
					t.Fatalf("/readyz status = %d, want %d", resp.StatusCode, tc.expected.readyzStatus)
				}
			}
			if tc.expected.addrFreeAfter {
				ln, bindErr := net.Listen("tcp", addr)
				if bindErr != nil {
					t.Fatalf("addr %s is still bound after a refused startMetrics: %v", addr, bindErr)
				}
				if closeErr := ln.Close(); closeErr != nil {
					t.Fatalf("close re-bound listener: %v", closeErr)
				}
			}
		})
	}
}

func TestDecompressFileDiscard(t *testing.T) {
	payload := bytes.Repeat([]byte("xtcp2 zstd probe\n"), 1024)
	path := writeZstd(t, payload)

	got, err := decompressFile(path, "")
	if err != nil {
		t.Fatalf("decompressFile: %v", err)
	}
	if got.compressedBytes <= 0 {
		t.Fatalf("compressedBytes = %d, want > 0", got.compressedBytes)
	}
	if got.decompressedBytes != int64(len(payload)) {
		t.Fatalf("decompressedBytes = %d, want %d", got.decompressedBytes, len(payload))
	}
	if got.duration <= 0 {
		t.Fatalf("duration = %s, want > 0", got.duration)
	}
}

func TestDecompressFileOutput(t *testing.T) {
	payload := []byte("hello from zstd-probe\n")
	path := writeZstd(t, payload)
	outDir := t.TempDir()

	if _, err := decompressFile(path, outDir); err != nil {
		t.Fatalf("decompressFile: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(outDir, "payload.txt"))
	if err != nil {
		t.Fatalf("ReadFile output: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("output = %q, want %q", got, payload)
	}
}
