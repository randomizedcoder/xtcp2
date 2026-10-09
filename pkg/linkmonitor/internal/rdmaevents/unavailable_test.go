//go:build !linux || !rdma || !cgo

package rdmaevents

import (
	"context"
	"errors"
	"testing"
)

func TestUnavailableBuild(t *testing.T) {
	t.Log("negative: RDMA requested in core build; expected: explicit unavailable diagnostic")
	_, err := NewProvider("").Open(context.Background(), Identity{Name: "hca"})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
}
