//go:build !linux

package rdmacaps

import "context"

// exchangeNative needs Linux's umad devices; elsewhere no backend has a
// transport.
func exchangeNative(context.Context, string, uint32, [256]byte) ([256]byte, error) {
	return [256]byte{}, ErrUnavailable
}
