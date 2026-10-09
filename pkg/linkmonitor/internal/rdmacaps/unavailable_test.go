//go:build !linux || !rdma || !cgo

package rdmacaps

import (
	"errors"
	"testing"
)

func TestUMADUnavailable(t *testing.T) {
	t.Log("negative: core build without UMAD; expected explicit unavailable capability source")
	_, err := Query(t.Context(), "hca", 1)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
}
