//go:build !linux

package rdmacaps

import "context"

// exchangeNative needs Linux's umad devices; elsewhere every query reports
// ErrUnavailable.
func exchangeNative(context.Context, string, uint32, [256]byte) ([256]byte, error) {
	return [256]byte{}, ErrUnavailable
}
