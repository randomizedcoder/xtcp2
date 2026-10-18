//go:build !linux || !rdma || !cgo

package rdmacaps

import (
	"errors"
	"runtime"
	"testing"
)

// TestUMADUnavailable: a build without libibumad refuses it explicitly, and a
// non-Linux build has no transport at all. Linux builds without cgo query
// natively; TestBackendSelection covers that.
func TestUMADUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expected string
		backend                               Backend
		linux                                 bool // the row also holds on Linux
	}{
		{"libibumad", "negative", "libibumad in a build without the rdma tag and cgo", "ErrUnavailable", Libibumad, true},
		{"auto_other", "negative", "auto on a platform without umad", "ErrUnavailable", Auto, false},
		{"native_other", "negative", "native on a platform without umad", "ErrUnavailable", Native, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			if runtime.GOOS == "linux" && !tc.linux {
				t.Skip("Linux has the native transport")
			}
			if _, err := Querier(tc.backend)(t.Context(), "hca", 1); !errors.Is(err, ErrUnavailable) {
				t.Fatal(err)
			}
		})
	}
	if LibibumadAvailable {
		t.Fatal("LibibumadAvailable in a build without libibumad")
	}
	if runtime.GOOS != "linux" {
		if _, err := Query(t.Context(), "hca", 1); !errors.Is(err, ErrUnavailable) {
			t.Fatal(err)
		}
	}
}
