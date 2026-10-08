package linkmonitor

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestCarrierFileParsing(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expected, input string
		want                                         uint64
		bad                                          bool
	}{
		{"zero", "boundary", "present zero", "zero", "0\n", 0, false},
		{"maximum", "boundary", "uint32 maximum", "exact value", "4294967295\n", 4294967295, false},
		{"empty", "negative", "empty value", "reject", "", 0, true},
		{"overflow", "negative", "wider than kernel counter", "reject", "4294967296", 0, true},
		{"negative", "negative", "signed input", "reject", "-1", 0, true},
		{"suffix", "negative", "junk after digits", "reject", "1x", 0, true},
		{"oversize", "negative", "long file", "bounded rejection", strings.Repeat("0", 33), 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			path := filepath.Join(t.TempDir(), "counter")
			if err := os.WriteFile(path, []byte(tc.input), 0600); err != nil {
				t.Fatal(err)
			}
			got, err := readCarrierFile(t.Context(), path)
			if (err != nil) != tc.bad || (!tc.bad && got != tc.want) {
				t.Fatalf("value=%d error=%v", got, err)
			}
		})
	}
}

func TestCarrierSelectiveFallback(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expected string
		failure                               error
		replace                               bool
	}{
		{"missing", "negative", "optional file absent", "unsupported field", fs.ErrNotExist, false},
		{"permission", "negative", "file denies read", "permission diagnostic", syscall.EACCES, false},
		{"valid", "positive", "only down counter missing", "read only requested counter and two identity checks", nil, false},
		{"reused", "corner", "ifindex changes during read", "discard all values", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			indices, fields := 0, 0
			source := carrierFilesystem{root: "/fake", readFile: func(_ context.Context, path string) (uint64, error) {
				if filepath.Base(path) == "ifindex" {
					indices++
					if tc.replace && indices == 2 {
						return 2, nil
					}
					return 1, nil
				}
				fields++
				if filepath.Base(path) != "carrier_down_count" {
					t.Errorf("unexpected file: %s", path)
				}
				return 9, tc.failure
			}}
			values, failures := source.read(t.Context(), "eth1", 1, 8)
			if fields != 1 || indices != 2 {
				t.Fatal("unbounded/per-counter fanout")
			}
			if tc.replace {
				if values[3].Present || failures[3] == nil {
					t.Fatal("reused identity accepted")
				}
				return
			}
			if values[3].Present != (tc.failure == nil) {
				t.Fatal("missing/error fabricated zero")
			}
			if errors.Is(tc.failure, fs.ErrNotExist) {
				if failures[3] != nil {
					t.Fatal("unsupported became I/O error")
				}
			} else if !errors.Is(failures[3], tc.failure) {
				t.Fatalf("failure: %v", failures[3])
			}
		})
	}
}
