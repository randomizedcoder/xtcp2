//go:build !linux || !rdma || !cgo

package rdmaevents

import "context"

type unavailable struct{}

// NewProvider returns explicit unavailable diagnostics in a core-only build.
func NewProvider(_ string) Provider { return unavailable{} }

func (unavailable) Open(context.Context, Identity) (Handle, error) { return nil, ErrUnavailable }
