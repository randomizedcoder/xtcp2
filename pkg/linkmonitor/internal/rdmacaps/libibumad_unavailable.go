//go:build !linux || !rdma || !cgo

package rdmacaps

import "context"

// LibibumadAvailable reports whether this build has the cgo libibumad
// transport that Libibumad needs.
const LibibumadAvailable = false

func exchangeLocal(context.Context, string, uint32, [256]byte) ([256]byte, error) {
	return [256]byte{}, ErrUnavailable
}
