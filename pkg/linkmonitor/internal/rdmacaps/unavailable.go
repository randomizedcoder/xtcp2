//go:build !linux || !rdma || !cgo

package rdmacaps

import "context"

func exchangeLocal(context.Context, string, uint32, [256]byte) ([256]byte, error) {
	return [256]byte{}, ErrUnavailable
}
