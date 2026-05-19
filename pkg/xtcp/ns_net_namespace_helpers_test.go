package xtcp

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// ns_net_namespace.go refactor: openAndSetNSWithRetries dropped from
// gocyclo 17 → 8 by extracting attemptOpenAndSetns + backoffSleep, and
// inserting a swappable openAndSetnsSyscalls seam so the retry logic
// is unit-testable without CAP_SYS_ADMIN / a real netns mount.
//
// These tests cover both extracted helpers with positive / negative /
// boundary / corner / adversarial categories, plus race + benchmarks.

// withFakeSyscalls swaps the package-level openAndSetnsSyscalls seam
// for the duration of fn and restores on return. Process-wide state,
// so callers MUST NOT t.Parallel() within the scope.
func withFakeSyscalls(t *testing.T, fake openAndSetnsSyscallsT, fn func()) {
	t.Helper()
	orig := openAndSetnsSyscalls
	openAndSetnsSyscalls = fake
	defer func() { openAndSetnsSyscalls = orig }()
	fn()
}

// withShortBackoff swaps backoffFactorCst to a microsecond so the
// retry-exhaust path doesn't wall-clock ~10s. Restores on cleanup.
func withShortBackoff(t *testing.T) {
	t.Helper()
	orig := backoffFactorCst
	backoffFactorCst = 1 * time.Microsecond
	t.Cleanup(func() { backoffFactorCst = orig })
}

// withSeededMountInfo points mountInfoDir at a temp file containing a
// line that matches the given namespace name, so checkMountInfo's
// strings.Contains gate passes.
func withSeededMountInfo(t *testing.T, nsName string) {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), "mountinfo")
	contents := fmt.Sprintf("10 1 0:1 / /run/netns/%s rw shared:0 - tmpfs tmpfs\n", nsName)
	if err := os.WriteFile(tmp, []byte(contents), 0600); err != nil {
		t.Fatalf("seed mountinfo: %v", err)
	}
	orig := mountInfoDir
	mountInfoDir = tmp
	t.Cleanup(func() { mountInfoDir = orig })
}

// fakeOutcomes lets a test prescribe per-attempt Open + Setns results.
type fakeOpenResult struct {
	fd  int
	err error
}

type fakeOutcomes struct {
	opens       []fakeOpenResult
	setnses     []error
	closeErrs   []error
	openIdx     atomic.Int32
	setnsIdx    atomic.Int32
	closeIdx    atomic.Int32
	totalCloses atomic.Int32
}

func (o *fakeOutcomes) syscalls() openAndSetnsSyscallsT {
	return openAndSetnsSyscallsT{
		open: func(_ string, _ int, _ uint32) (int, error) {
			i := o.openIdx.Add(1) - 1
			if int(i) >= len(o.opens) {
				return -1, fmt.Errorf("fake open exhausted")
			}
			return o.opens[i].fd, o.opens[i].err
		},
		setns: func(_ int, _ int) error {
			i := o.setnsIdx.Add(1) - 1
			if int(i) >= len(o.setnses) {
				return fmt.Errorf("fake setns exhausted")
			}
			return o.setnses[i]
		},
		close: func(_ int) error {
			o.totalCloses.Add(1)
			i := o.closeIdx.Add(1) - 1
			if int(i) >= len(o.closeErrs) {
				return nil
			}
			return o.closeErrs[i]
		},
	}
}

// ───────────────────────────────────────────────────────────────────────
// attemptOpenAndSetns
// ───────────────────────────────────────────────────────────────────────

func TestAttemptOpenAndSetns_table(t *testing.T) {
	cases := []struct {
		name          string
		category      string
		opens         []fakeOpenResult
		setnses       []error
		closeErrs     []error
		wantFD        int
		wantErrOpen   bool
		wantErrSetns  bool
		wantCloseCnt  int32
		wantPromLabel string
	}{
		{
			name:         "positive_open_and_setns_succeed",
			category:     "positive",
			opens:        []fakeOpenResult{{fd: 42, err: nil}},
			setnses:      []error{nil},
			wantFD:       42,
			wantCloseCnt: 0,
		},
		{
			name:          "negative_open_fails_no_setns",
			category:      "negative",
			opens:         []fakeOpenResult{{fd: -1, err: errors.New("open failed")}},
			setnses:       []error{nil},
			wantFD:        -1,
			wantErrOpen:   true,
			wantPromLabel: "open",
		},
		{
			name:          "negative_setns_fails_close_succeeds",
			category:      "negative",
			opens:         []fakeOpenResult{{fd: 7, err: nil}},
			setnses:       []error{errors.New("setns failed")},
			wantFD:        7,
			wantErrSetns:  true,
			wantCloseCnt:  1,
			wantPromLabel: "Setns",
		},
		{
			name:          "corner_setns_and_close_both_fail",
			category:      "corner",
			opens:         []fakeOpenResult{{fd: 9, err: nil}},
			setnses:       []error{errors.New("setns fail")},
			closeErrs:     []error{errors.New("close fail")},
			wantFD:        9,
			wantErrSetns:  true,
			wantCloseCnt:  1,
			wantPromLabel: "close",
		},
		{
			name:         "boundary_fd_zero_returned_as_is",
			category:     "boundary",
			opens:        []fakeOpenResult{{fd: 0, err: nil}},
			setnses:      []error{nil},
			wantFD:       0,
			wantCloseCnt: 0,
		},
		{
			name:        "adversarial_open_returns_fd_and_err",
			category:    "adversarial",
			opens:       []fakeOpenResult{{fd: 100, err: errors.New("strange")}},
			setnses:     []error{nil},
			wantFD:      100,
			wantErrOpen: true,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			fake := &fakeOutcomes{
				opens:     tc.opens,
				setnses:   tc.setnses,
				closeErrs: tc.closeErrs,
			}
			x := newTestXTCP(t, "null:")
			var fd int
			var errOpen, errSetns error
			withFakeSyscalls(t, fake.syscalls(), func() {
				ns := "xtest"
				fd, errOpen, errSetns = x.attemptOpenAndSetns(&ns)
			})
			if fd != tc.wantFD {
				t.Errorf("fd = %d, want %d", fd, tc.wantFD)
			}
			if (errOpen != nil) != tc.wantErrOpen {
				t.Errorf("errOpen = %v, want non-nil=%v", errOpen, tc.wantErrOpen)
			}
			if (errSetns != nil) != tc.wantErrSetns {
				t.Errorf("errSetns = %v, want non-nil=%v", errSetns, tc.wantErrSetns)
			}
			if got := fake.totalCloses.Load(); got != tc.wantCloseCnt {
				t.Errorf("close calls = %d, want %d", got, tc.wantCloseCnt)
			}
			if tc.wantPromLabel != "" {
				gotV := testutil.ToFloat64(
					x.pC.WithLabelValues("openAndSetNSWithRetries", tc.wantPromLabel, "error"))
				if gotV != 1 {
					t.Errorf("counter[%s] = %v, want 1", tc.wantPromLabel, gotV)
				}
			}
		})
	}
}

// ───────────────────────────────────────────────────────────────────────
// backoffSleep
// ───────────────────────────────────────────────────────────────────────

func TestBackoffSleep_table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		category    string
		attempt     int
		expectSleep bool
	}{
		{"positive_first_real_retry_sleeps", "positive", 1, true},
		{"negative_attempt_zero_skips", "negative", 0, false},
		{"negative_negative_attempt_skips", "negative", -1, false},
		{"boundary_one_below_max", "boundary", maxRetriesCst - 1, true},
		{"boundary_at_max_skips", "boundary", maxRetriesCst, false},
		{"corner_far_above_max_skips", "corner", maxRetriesCst * 100, false},
		{"adversarial_min_int_skips", "adversarial", -1 << 31, false},
	}
	// All rows must share the shrunken backoff factor; can't t.Parallel
	// inside individual rows because they all read/write the same var.
	orig := backoffFactorCst
	backoffFactorCst = 1 * time.Microsecond
	defer func() { backoffFactorCst = orig }()
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			x := newTestXTCP(t, "null:")
			start := time.Now()
			x.backoffSleep(tc.attempt)
			elapsed := time.Since(start)
			// With backoffFactorCst=1µs, even attempt=9 sleeps ~512µs.
			// Use 1µs as the threshold: any non-skipped sleep crosses it.
			if tc.expectSleep && elapsed < 1*time.Microsecond {
				t.Errorf("attempt=%d expected non-zero sleep, got %v", tc.attempt, elapsed)
			}
			// Skipped path should be sub-100µs in practice.
			if !tc.expectSleep && elapsed > 100*time.Millisecond {
				t.Errorf("attempt=%d expected to skip sleep, took %v", tc.attempt, elapsed)
			}
		})
	}
}

// ───────────────────────────────────────────────────────────────────────
// openAndSetNSWithRetries integration
// ───────────────────────────────────────────────────────────────────────

func TestOpenAndSetNSWithRetries_table(t *testing.T) {
	cases := []struct {
		name               string
		category           string
		opens              []fakeOpenResult
		setnses            []error
		closeErrs          []error
		wantFD             int
		wantCounterLabel   string
		wantCounterAtLeast float64
	}{
		{
			name:     "positive_first_attempt",
			category: "positive",
			opens:    []fakeOpenResult{{fd: 7, err: nil}},
			setnses:  []error{nil},
			wantFD:   7,
		},
		{
			name:     "positive_third_attempt_wins",
			category: "positive",
			opens: []fakeOpenResult{
				{fd: 10, err: nil},
				{fd: 11, err: nil},
				{fd: 12, err: nil},
			},
			setnses: []error{errors.New("transient"), errors.New("transient"), nil},
			wantFD:  12,
		},
		{
			name:     "negative_open_fails_first_attempt",
			category: "negative",
			opens:    []fakeOpenResult{{fd: -1, err: errors.New("permission denied")}},
			setnses:  []error{nil},
			wantFD:   -1,
		},
		{
			name:     "boundary_exhausted_retries",
			category: "boundary",
			opens: func() []fakeOpenResult {
				out := make([]fakeOpenResult, maxRetriesCst)
				for i := range out {
					out[i] = fakeOpenResult{fd: 100 + i, err: nil}
				}
				return out
			}(),
			setnses: func() []error {
				out := make([]error, maxRetriesCst)
				for i := range out {
					out[i] = errors.New("setns always fails")
				}
				return out
			}(),
			wantFD:             -1,
			wantCounterLabel:   "SetnsAfterRetries",
			wantCounterAtLeast: 1,
		},
		{
			name:     "corner_alternating_results",
			category: "corner",
			opens: []fakeOpenResult{
				{fd: 1, err: nil},
				{fd: 2, err: nil},
			},
			setnses: []error{errors.New("flap"), nil},
			wantFD:  2,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			ns := "ns_" + tc.name
			withSeededMountInfo(t, ns)
			withShortBackoff(t)
			fake := &fakeOutcomes{
				opens:     tc.opens,
				setnses:   tc.setnses,
				closeErrs: tc.closeErrs,
			}
			x := newTestXTCP(t, "null:")
			var fd int
			withFakeSyscalls(t, fake.syscalls(), func() {
				fullNS := "/run/netns/" + ns
				fd = x.openAndSetNSWithRetries(&fullNS)
			})
			if fd != tc.wantFD {
				t.Errorf("fd = %d, want %d", fd, tc.wantFD)
			}
			if tc.wantCounterLabel != "" {
				gotV := testutil.ToFloat64(
					x.pC.WithLabelValues("openAndSetNSWithRetries", tc.wantCounterLabel, "error"))
				if gotV < tc.wantCounterAtLeast {
					t.Errorf("counter[%s] = %v, want >= %v",
						tc.wantCounterLabel, gotV, tc.wantCounterAtLeast)
				}
			}
		})
	}
}

// TestOpenAndSetNSWithRetries_missingMountInfo verifies the
// short-circuit when checkMountInfoWithRetries returns (false, nil).
func TestOpenAndSetNSWithRetries_missingMountInfo(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "mountinfo")
	if err := os.WriteFile(tmp, []byte(""), 0600); err != nil {
		t.Fatalf("seed mountinfo: %v", err)
	}
	orig := mountInfoDir
	mountInfoDir = tmp
	defer func() { mountInfoDir = orig }()
	withShortBackoff(t)

	x := newTestXTCP(t, "null:")
	calls := atomic.Int32{}
	fake := openAndSetnsSyscallsT{
		open: func(_ string, _ int, _ uint32) (int, error) {
			calls.Add(1)
			return -1, syscall.ENOENT
		},
		setns: func(_ int, _ int) error { return nil },
		close: func(_ int) error { return nil },
	}
	var fd int
	withFakeSyscalls(t, fake, func() {
		ns := "/run/netns/never_mounted"
		fd = x.openAndSetNSWithRetries(&ns)
	})
	if fd != -1 {
		t.Errorf("fd = %d, want -1 on missing mount-info", fd)
	}
	if got := calls.Load(); got != 0 {
		t.Errorf("Open called %d times, want 0 (mount-info gate must short-circuit)", got)
	}
}

// ───────────────────────────────────────────────────────────────────────
// Race — backoffSleep is the only pure helper; the others swap the
// package-global seam so they can't run in parallel.
// ───────────────────────────────────────────────────────────────────────

func TestBackoffSleep_concurrent(t *testing.T) {
	orig := backoffFactorCst
	backoffFactorCst = 1 * time.Microsecond
	defer func() { backoffFactorCst = orig }()

	const goroutines = 16
	x := newTestXTCP(t, "null:")
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				attempt := j % 3
				if i%2 == 0 {
					attempt = -attempt
				}
				x.backoffSleep(attempt)
			}
		}()
	}
	wg.Wait()
}

// ───────────────────────────────────────────────────────────────────────
// Benchmarks
// ───────────────────────────────────────────────────────────────────────

func BenchmarkBackoffSleep_skipPath(b *testing.B) {
	b.ReportAllocs()
	x := newTestXTCP(&testing.T{}, "null:")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		x.backoffSleep(0)
	}
}

func BenchmarkAttemptOpenAndSetns_success(b *testing.B) {
	b.ReportAllocs()
	x := newTestXTCP(&testing.T{}, "null:")
	fake := openAndSetnsSyscallsT{
		open:  func(_ string, _ int, _ uint32) (int, error) { return 1, nil },
		setns: func(_ int, _ int) error { return nil },
		close: func(_ int) error { return nil },
	}
	orig := openAndSetnsSyscalls
	openAndSetnsSyscalls = fake
	defer func() { openAndSetnsSyscalls = orig }()
	ns := "/run/netns/bench"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = x.attemptOpenAndSetns(&ns)
	}
}
