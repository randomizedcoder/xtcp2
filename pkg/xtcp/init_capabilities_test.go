package xtcp

import (
	"errors"
	"testing"

	"golang.org/x/sys/unix"
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

// capgetFunc swap: inject success caps (both CAP_SYS_CHROOT and
// CAP_NET_ADMIN set in Effective) so the success-return branch is
// exercised.
func TestCheckCapabilities_hasAllCaps(t *testing.T) {
	prev := capgetFunc
	t.Cleanup(func() { capgetFunc = prev })
	capgetFunc = func(_ *unix.CapUserHeader, c *unix.CapUserData) error {
		c.Effective = (1 << unix.CAP_SYS_CHROOT) | (1 << unix.CAP_NET_ADMIN)
		return nil
	}
	x := &XTCP{debugLevel: 11}
	if err := x.checkCapabilities(); err != nil {
		t.Errorf("err = %v, want nil with both caps set", err)
	}
}

// capgetFunc swap: only one cap set → returns the "needs CAP_NET_ADMIN
// and CAP_SYS_CHROOT" error.
func TestCheckCapabilities_missingOneCap(t *testing.T) {
	prev := capgetFunc
	t.Cleanup(func() { capgetFunc = prev })
	capgetFunc = func(_ *unix.CapUserHeader, c *unix.CapUserData) error {
		c.Effective = 1 << unix.CAP_NET_ADMIN
		return nil
	}
	x := &XTCP{}
	if err := x.checkCapabilities(); err == nil {
		t.Error("missing CAP_SYS_CHROOT should error")
	}
}

// capgetFunc swap: returns an error → checkCapabilities wraps and
// returns "failed to get capabilities".
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
