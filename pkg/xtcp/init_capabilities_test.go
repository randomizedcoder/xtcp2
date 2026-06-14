package xtcp

import (
	"testing"
)

// checkCapabilities calls unix.Capget for the current process. The result
// depends on whether the test is being run as root/CAP_SYS_ADMIN. We can't
// guarantee a specific outcome but we can verify the function doesn't
// panic and the err path is exercised regardless.
func TestCheckCapabilities_doesntPanic(t *testing.T) {
	x := &XTCP{}
	_ = x.checkCapabilities() //nolint:errcheck // result is environment-dependent
}

func TestCheckCapabilities_debugLog(t *testing.T) {
	x := &XTCP{debugLevel: 11}
	_ = x.checkCapabilities() //nolint:errcheck // result is environment-dependent
}
