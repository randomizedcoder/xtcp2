package rdmacaps

import (
	"errors"
	"testing"
)

// TestBackendSelection: Auto resolves by build, explicit backends stay as
// named, and a value that is no backend has no transport, so its queries fail
// with ErrUnavailable before any I/O.
func TestBackendSelection(t *testing.T) {
	auto := Native
	if LibibumadAvailable {
		auto = Libibumad
	}
	for _, tc := range []struct {
		name, category, description, expected string
		backend, resolved                     Backend
		text                                  string
	}{
		{"auto", "positive", "auto in this build", "libibumad with cgo, native without", Auto, auto, "auto"},
		{"libibumad", "positive", "explicit libibumad", "libibumad", Libibumad, Libibumad, "libibumad"},
		{"native", "positive", "explicit native", "native", Native, Native, "native"},
		{"unknown", "corner", "a value past the last backend", "itself, named unknown", Backend(9), Backend(9), "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			if got := tc.backend.Resolve(); got != tc.resolved {
				t.Fatalf("Resolve %v, want %v", got, tc.resolved)
			}
			if got := tc.backend.String(); got != tc.text {
				t.Fatalf("String %q, want %q", got, tc.text)
			}
		})
	}
	t.Run("unknown_query", func(t *testing.T) {
		t.Log("negative: query over a value that is no backend; expected ErrUnavailable without I/O")
		if _, err := Querier(Backend(9))(t.Context(), "hca", 1); !errors.Is(err, ErrUnavailable) {
			t.Fatal(err)
		}
	})
	t.Run("invalid_before_transport", func(t *testing.T) {
		t.Log("boundary: an invalid port on any backend; expected EINVAL before the transport runs")
		for _, b := range []Backend{Auto, Libibumad, Native} {
			if _, err := Querier(b)(t.Context(), "hca", 0); err == nil || errors.Is(err, ErrUnavailable) {
				t.Fatalf("%v: %v", b, err)
			}
		}
	})
}
