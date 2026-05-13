package xtcp

import "context"

// nullDest sends each marshalled record nowhere. Useful for benchmarking
// the deserialize+marshal path in isolation, and as the default for tests.
type nullDest struct {
	x *XTCP
}

func newNullDest(_ context.Context, x *XTCP) (Destination, error) {
	return &nullDest{x: x}, nil
}

func (d *nullDest) Send(_ context.Context, b *[]byte) (int, error) {
	d.x.pC.WithLabelValues("destNull", "start", "count").Inc()
	return len(*b), nil
}

func (d *nullDest) Close() error { return nil }

func init() {
	RegisterDestination("null", newNullDest)
}
