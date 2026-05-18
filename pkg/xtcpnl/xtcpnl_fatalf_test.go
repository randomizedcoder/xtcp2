package xtcpnl

import (
	"fmt"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// withFatalf swaps the package-level fatalf for the duration of the test,
// restoring it on cleanup. Returns a capture buffer with whatever
// formatted message any fatalf calls produced.
func withFatalf(t *testing.T) *strings.Builder {
	t.Helper()
	prev := fatalf
	var captured strings.Builder
	fatalf = func(format string, args ...any) {
		captured.WriteString(strings.TrimSuffix(fmt.Sprintf(format, args...), "\n"))
		captured.WriteString("\n")
	}
	t.Cleanup(func() { fatalf = prev })
	return &captured
}

// SetSocketTimeoutViaSyscall: timeout==0 returns nil without syscall;
// timeout>0 with an invalid fd hits the SetsockoptTimeval error branch.
func TestSetSocketTimeoutViaSyscall_zero(t *testing.T) {
	if err := SetSocketTimeoutViaSyscall(0, -1); err != nil {
		t.Errorf("timeout=0 should be a no-op; got err = %v", err)
	}
}

func TestSetSocketTimeoutViaSyscall_seconds(t *testing.T) {
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_DGRAM, 0)
	if err != nil {
		t.Skipf("socketpair: %v", err)
	}
	defer func() {
		_ = unix.Close(fds[0]) //nolint:errcheck // test plumbing
		_ = unix.Close(fds[1]) //nolint:errcheck // test plumbing
	}()
	if err := SetSocketTimeoutViaSyscall(2000, fds[0]); err != nil {
		t.Errorf("err = %v", err)
	}
}

func TestSetSocketTimeoutViaSyscall_milliseconds(t *testing.T) {
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_DGRAM, 0)
	if err != nil {
		t.Skipf("socketpair: %v", err)
	}
	defer func() {
		_ = unix.Close(fds[0]) //nolint:errcheck // test plumbing
		_ = unix.Close(fds[1]) //nolint:errcheck // test plumbing
	}()
	if err := SetSocketTimeoutViaSyscall(500, fds[0]); err != nil {
		t.Errorf("err = %v", err)
	}
}

func TestSetSocketTimeoutViaSyscall_invalidFD(t *testing.T) {
	captured := withFatalf(t)
	err := SetSocketTimeoutViaSyscall(2000, -1)
	if err == nil {
		t.Error("expected error from SetsockoptTimeval on invalid fd")
	}
	if !strings.Contains(captured.String(), "SetsockopttimeSpec") {
		t.Errorf("fatalf not invoked; got %q", captured.String())
	}
}

// SendNetlinkDumpRequest: invalid fd → Sendto error → fatalf fires.
func TestSendNetlinkDumpRequest_invalidFD(t *testing.T) {
	captured := withFatalf(t)
	SendNetlinkDumpRequest(-1, &unix.SockaddrNetlink{}, []byte("payload"))
	if !strings.Contains(captured.String(), "unix.Sendto") {
		t.Errorf("fatalf not invoked; got %q", captured.String())
	}
}

func TestSendNetlinkDumpRequestPtr_invalidFD(t *testing.T) {
	captured := withFatalf(t)
	payload := []byte("payload")
	SendNetlinkDumpRequestPtr(-1, &unix.SockaddrNetlink{}, &payload)
	if !strings.Contains(captured.String(), "unix.Sendto") {
		t.Errorf("fatalf not invoked; got %q", captured.String())
	}
}

// OpenNetlinkSocketWithTimeout: the netlink-socket open requires
// CAP_NET_ADMIN to bind on most distros + the kernel must support
// NETLINK_INET_DIAG. We can't reliably test the happy path; accept
// either a successful fd or a fatalf-captured failure.
func TestOpenNetlinkSocketWithTimeout_smokes(t *testing.T) {
	captured := withFatalf(t)
	fd := OpenNetlinkSocketWithTimeout(1000)
	if fd >= 0 {
		_ = unix.Close(fd) //nolint:errcheck // test plumbing
	}
	_ = captured // tolerate either outcome
}
