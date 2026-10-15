package io_uring

import (
	"os"
	"testing"
)

// skipNoIoUring skips tb because io_uring is unavailable (err says why),
// or fails it when XTCP2_REQUIRE_IOURING=1, which the nix test derivations
// set so a host without io_uring shows red instead of passing silently.
// Same contract as pkg/xtcp's helper of the same name.
func skipNoIoUring(tb testing.TB, err error) {
	tb.Helper()
	if os.Getenv("XTCP2_REQUIRE_IOURING") == "1" {
		tb.Fatalf("io_uring unavailable and XTCP2_REQUIRE_IOURING=1: %v", err)
		return
	}
	tb.Skipf("io_uring unavailable: %v", err)
}
