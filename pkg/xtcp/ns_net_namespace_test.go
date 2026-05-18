package xtcp

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"golang.org/x/sys/unix"
)

// closeSocket + closeFD: both wrappers around unix.Close. Use a real
// socketpair (testable on any Linux) for the success path, then a stale
// FD for the error path.

func newCloseFixture(t *testing.T) *XTCP {
	t.Helper()
	x := &XTCP{}
	reg := prometheus.NewRegistry()
	x.pC = promauto.With(reg).NewCounterVec(
		prometheus.CounterOpts{Subsystem: "xtcp_close_test",
			Name: promNameCounts, Help: "test counts"},
		promLabels,
	)
	return x
}

func TestCloseSocket_success(t *testing.T) {
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_DGRAM, 0)
	if err != nil {
		t.Skipf("socketpair: %v", err)
	}
	defer func() { _ = unix.Close(fds[1]) }() //nolint:errcheck // test plumbing
	x := newCloseFixture(t)
	x.closeSocket(fds[0])
}

func TestCloseSocket_error(t *testing.T) {
	x := newCloseFixture(t)
	x.closeSocket(-1) // invalid fd → unix.Close returns EBADF
}

func TestCloseFD_success(t *testing.T) {
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_DGRAM, 0)
	if err != nil {
		t.Skipf("socketpair: %v", err)
	}
	defer func() { _ = unix.Close(fds[1]) }() //nolint:errcheck // test plumbing
	x := newCloseFixture(t)
	x.closeFD(fds[0])
}

func TestCloseFD_error(t *testing.T) {
	x := newCloseFixture(t)
	x.closeFD(-1) // invalid fd → unix.Close returns EBADF
}

// setSocketTimeoutViaSyscall: sets SO_RCVTIMEO on a real fd. The unit
// test uses a real socketpair fd so the underlying syscall succeeds.
func TestSetSocketTimeoutViaSyscall_success(t *testing.T) {
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_DGRAM, 0)
	if err != nil {
		t.Skipf("socketpair: %v", err)
	}
	defer func() {
		_ = unix.Close(fds[0]) //nolint:errcheck // test plumbing
		_ = unix.Close(fds[1]) //nolint:errcheck // test plumbing
	}()
	x := newCloseFixture(t)
	// Also need pH for the histogram observation.
	reg := prometheus.NewRegistry()
	x.pH = promauto.With(reg).NewSummaryVec(
		prometheus.SummaryOpts{
			Subsystem: "xtcp_settimeout_test", Name: promNameHistograms, Help: "test",
			Objectives: map[float64]float64{0.5: quantileError},
			MaxAge:     summaryVecMaxAge,
		},
		promLabels,
	)
	x.setSocketTimeoutViaSyscall(100, fds[0])
}

func TestSetSocketTimeoutViaSyscall_zero(t *testing.T) {
	// timeout=0 → short-circuit (no syscall).
	x := newCloseFixture(t)
	x.setSocketTimeoutViaSyscall(0, -1) // no-op
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
	x := newCloseFixture(t)
	x.setSocketTimeoutViaSyscall(2000, fds[0]) // >= 1000 → seconds path
}

// netNamespaceInstance: requires CAP_SYS_ADMIN + netlink. Microvm-only.

// checkMountInfo: opens /proc/self/mountinfo and grep's for nsName.
func TestCheckMountInfo_notFound(t *testing.T) {
	x := newCloseFixture(t)
	nsName := "ridiculously-unlikely-namespace-suffix-xq42"
	found, err := x.checkMountInfo(&nsName)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if found {
		t.Errorf("found = true for synthetic nsName; want false")
	}
}

func TestCheckMountInfo_found(t *testing.T) {
	x := newCloseFixture(t)
	nsName := "/" // every Linux mountinfo contains /
	found, err := x.checkMountInfo(&nsName)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !found {
		t.Error("expected mountinfo to contain /")
	}
}

func TestCheckMountInfo_debugLog(t *testing.T) {
	x := newCloseFixture(t)
	x.debugLevel = 11 // hit log.Printf branch
	nsName := "/"
	_, _ = x.checkMountInfo(&nsName) //nolint:errcheck // test plumbing
}
