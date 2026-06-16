package xtcp

import (
	"errors"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// withCapMask runs `body` with the capgetFunc seam temporarily replaced
// to return `eff` as the effective capability set. Cleanup restores the
// original seam.
func withCapMask(t *testing.T, eff uint32, body func()) {
	t.Helper()
	prev := capgetFunc
	t.Cleanup(func() { capgetFunc = prev })
	capgetFunc = func(_ *unix.CapUserHeader, c *unix.CapUserData) error {
		c.Effective = eff
		return nil
	}
	body()
}

// checkCapabilities calls unix.Capget for the current process. The
// result depends on whether the test is being run as root/CAP_SYS_ADMIN.
// We can't guarantee a specific outcome but we can verify the function
// doesn't panic.
func TestCheckCapabilities_doesntPanic(t *testing.T) {
	x := &XTCP{}
	_ = x.checkCapabilities()
}

func TestCheckCapabilities_debugLog(t *testing.T) {
	x := &XTCP{debugLevel: 11}
	_ = x.checkCapabilities()
}

// Both hard-required caps present → checkCapabilities returns nil.
// CAP_NET_RAW + CAP_SYS_RESOURCE missing → warnings printed but no
// returned error (start path proceeds).
func TestCheckCapabilities_hasAllRequired(t *testing.T) {
	withCapMask(t, (1<<unix.CAP_NET_ADMIN)|(1<<unix.CAP_SYS_ADMIN), func() {
		x := &XTCP{debugLevel: 11}
		if err := x.checkCapabilities(); err != nil {
			t.Errorf("err = %v, want nil with both hard-required caps set", err)
		}
	})
}

// All four caps present → no warnings, no error.
func TestCheckCapabilities_hasEverything(t *testing.T) {
	mask := uint32(0)
	for _, r := range requiredCaps {
		mask |= 1 << r.bit
	}
	withCapMask(t, mask, func() {
		x := &XTCP{}
		if err := x.checkCapabilities(); err != nil {
			t.Errorf("err = %v, want nil with every capability set", err)
		}
	})
}

// Missing CAP_NET_ADMIN → fatal-tier error that names the cap and
// includes the systemd remediation snippet.
func TestCheckCapabilities_missingNetAdmin(t *testing.T) {
	withCapMask(t, 1<<unix.CAP_SYS_ADMIN, func() {
		x := &XTCP{}
		err := x.checkCapabilities()
		if err == nil {
			t.Fatal("missing CAP_NET_ADMIN should error")
		}
		msg := err.Error()
		if !strings.Contains(msg, "CAP_NET_ADMIN") {
			t.Errorf("error should name CAP_NET_ADMIN; got %q", msg)
		}
		if !strings.Contains(msg, "AmbientCapabilities") {
			t.Errorf("error should include systemd snippet; got %q", msg)
		}
	})
}

// Missing CAP_SYS_ADMIN → fatal-tier error. This is the case that
// caused the 12 h soak crash; the regression test pins the message.
func TestCheckCapabilities_missingSysAdmin(t *testing.T) {
	withCapMask(t, 1<<unix.CAP_NET_ADMIN, func() {
		x := &XTCP{}
		err := x.checkCapabilities()
		if err == nil {
			t.Fatal("missing CAP_SYS_ADMIN should error")
		}
		msg := err.Error()
		if !strings.Contains(msg, "CAP_SYS_ADMIN") {
			t.Errorf("error should name CAP_SYS_ADMIN; got %q", msg)
		}
		// The message must mention the failure mode so an operator
		// reading it knows what's at stake.
		if !strings.Contains(msg, "setns") {
			t.Errorf("error should mention setns; got %q", msg)
		}
	})
}

// Missing only soft-required caps (CAP_NET_RAW + CAP_SYS_RESOURCE)
// → no error, daemon continues with warnings.
func TestCheckCapabilities_missingOnlySoftCaps(t *testing.T) {
	withCapMask(t, (1<<unix.CAP_NET_ADMIN)|(1<<unix.CAP_SYS_ADMIN), func() {
		x := &XTCP{}
		if err := x.checkCapabilities(); err != nil {
			t.Errorf("soft-only missing caps should not error; got %v", err)
		}
	})
}

// Multiple hard-required missing → all named in the error.
func TestCheckCapabilities_missingBothHardCaps(t *testing.T) {
	withCapMask(t, 0, func() {
		x := &XTCP{}
		err := x.checkCapabilities()
		if err == nil {
			t.Fatal("missing both hard caps should error")
		}
		msg := err.Error()
		if !strings.Contains(msg, "CAP_NET_ADMIN") {
			t.Errorf("error should name CAP_NET_ADMIN; got %q", msg)
		}
		if !strings.Contains(msg, "CAP_SYS_ADMIN") {
			t.Errorf("error should name CAP_SYS_ADMIN; got %q", msg)
		}
	})
}

// Capget itself failing → wrapped, surfaces the underlying error
// via errors.Is.
func TestCheckCapabilities_capgetErr(t *testing.T) {
	prev := capgetFunc
	t.Cleanup(func() { capgetFunc = prev })
	injected := errors.New("injected capget failure")
	capgetFunc = func(_ *unix.CapUserHeader, _ *unix.CapUserData) error {
		return injected
	}
	x := &XTCP{}
	err := x.checkCapabilities()
	if err == nil {
		t.Fatal("expected wrapped error")
	}
	if !errors.Is(err, injected) {
		t.Errorf("err should wrap injected; got %v", err)
	}
}
