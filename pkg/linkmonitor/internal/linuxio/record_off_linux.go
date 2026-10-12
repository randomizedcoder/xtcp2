//go:build !nlrecord

package linuxio

// recordTransport is the identity outside nlrecord builds; see record_linux.go.
func recordTransport(_ int, t transport) transport { return t }

// recordError has nothing to report outside nlrecord builds.
func recordError() error { return nil }
