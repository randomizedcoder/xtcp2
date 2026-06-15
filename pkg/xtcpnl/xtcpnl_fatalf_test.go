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

// Bug 66 regression: SO_RCVTIMEO timeout in milliseconds must decompose
// into BOTH whole seconds AND sub-second microseconds. The previous
// >=1000 branch dropped the modulo, so 1500ms set tv to 1s + 0us.
func TestMillisToTimeval_table(t *testing.T) {
	cases := []struct {
		name    string
		millis  int64
		wantSec int64
		wantUs  int64
	}{
		{"zero", 0, 0, 0},
		{"sub_second_500ms", 500, 0, 500_000},
		{"exactly_one_sec", 1000, 1, 0},
		{"one_and_half_sec_bug66", 1500, 1, 500_000},
		{"two_and_half_sec_bug66", 2500, 2, 500_000},
		{"five_seconds_exact", 5000, 5, 0},
		{"odd_remainder_9999ms", 9999, 9, 999_000},
		{"one_microsecond_floor_1ms", 1, 0, 1_000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tv := millisToTimeval(tc.millis)
			if tv.Sec != tc.wantSec || int64(tv.Usec) != tc.wantUs {
				t.Errorf("millisToTimeval(%d) = {Sec:%d, Usec:%d}; want {Sec:%d, Usec:%d}",
					tc.millis, tv.Sec, tv.Usec, tc.wantSec, tc.wantUs)
			}
		})
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
