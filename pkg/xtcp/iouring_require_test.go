package xtcp

import (
	"os"
	"testing"
)

// requireIoUringEnvCst, when "1", turns "io_uring unavailable" from a skip
// into a failure. The nix test derivations set it, so a build host that
// can't create a ring shows red instead of silently passing the io_uring
// tests; a plain local `go test` still skips.
const requireIoUringEnvCst = "XTCP2_REQUIRE_IOURING"

// skipNoIoUring skips tb because io_uring is unavailable (err says why),
// or fails it when XTCP2_REQUIRE_IOURING=1.
func skipNoIoUring(tb testing.TB, err error) {
	tb.Helper()
	if os.Getenv(requireIoUringEnvCst) == "1" {
		tb.Fatalf("io_uring unavailable and %s=1: %v", requireIoUringEnvCst, err)
		return
	}
	tb.Skipf("io_uring unavailable: %v", err)
}

// recordTB records Fatalf and Skipf instead of stopping the test.
type recordTB struct {
	testing.TB
	fataled, skipped bool
}

func (r *recordTB) Helper()               {}
func (r *recordTB) Fatalf(string, ...any) { r.fataled = true }
func (r *recordTB) Skipf(string, ...any)  { r.skipped = true }

func TestSkipNoIoUring(t *testing.T) {
	tests := []struct {
		description     string
		env             string
		expectedFatal   bool
		expectedSkipped bool
	}{
		{description: "positive: unset skips", env: "", expectedSkipped: true},
		{description: "positive: 1 fails", env: "1", expectedFatal: true},
		{description: "negative: 0 skips", env: "0", expectedSkipped: true},
		{description: "corner: true is not 1, so it skips", env: "true", expectedSkipped: true},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Setenv(requireIoUringEnvCst, tc.env)
			r := &recordTB{TB: t}
			skipNoIoUring(r, os.ErrNotExist)
			if r.fataled != tc.expectedFatal || r.skipped != tc.expectedSkipped {
				t.Errorf("fataled=%v skipped=%v, expected fataled=%v skipped=%v",
					r.fataled, r.skipped, tc.expectedFatal, tc.expectedSkipped)
			}
		})
	}
}
