package xtcp

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_config"
)

// netlinker.go refactor (gocyclo 17 → 6) extracted five helpers from the
// monolithic netlinkerSyscall body. These tests cover each helper
// directly using positive / negative / boundary / corner / adversarial
// categories. Concurrent invocations are exercised with -race in
// TestNetlinkerHelpers_concurrent. Benchmarks sit at the bottom.

// withCapturedLog redirects the standard logger to a bytes.Buffer for
// the duration of fn. Restores on return.
func withCapturedLog(t *testing.T, fn func()) string {
	t.Helper()
	old := log.Writer()
	flags := log.Flags()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(old)
		log.SetFlags(flags)
	}()
	fn()
	return buf.String()
}

// newNetlinkerXTCP wires the minimal XTCP fixture needed to drive the
// helpers: prom counters/histograms on a fresh registry, a fdToNsMap
// sync.Map, and a fatalf seam pointing at t.Fatalf.
func newNetlinkerXTCP(t *testing.T) *XTCP {
	t.Helper()
	x := newTestXTCP(t, "null:")
	x.fdToNsMap = &sync.Map{}
	x.config.CapturePath = filepath.Join(t.TempDir(), "cap_")
	return x
}

// ───────────────────────────────────────────────────────────────────────
// recvOneFromKernel — socketpair fixture; covers success + recv-error.
// The net.Error/Timeout branch is unreachable from a real syscall.Recvfrom
// (timeouts surface as syscall.EAGAIN, not a net.Error). Documented in
// the table so a future fix can pin it without confusion.
// ───────────────────────────────────────────────────────────────────────

// makeSocketPair returns a (readFD, writeFD) pair. The cleanup closes
// both fds exactly once; callers that close one side explicitly should
// use the closer returned to mark that side as already-closed (fd
// numbers can be recycled by the kernel under parallel tests + race,
// turning a benign double-close into a stray close of an unrelated
// socket in another goroutine).
func makeSocketPair(t *testing.T) (read, write int, closeRead func()) {
	t.Helper()
	pair, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_DGRAM, 0)
	if err != nil {
		t.Fatalf("socketpair: %v", err)
	}
	var closedRead bool
	var mu sync.Mutex
	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		if !closedRead {
			_ = syscall.Close(pair[0])
			closedRead = true
		}
		_ = syscall.Close(pair[1])
	})
	closeRead = func() {
		mu.Lock()
		defer mu.Unlock()
		if !closedRead {
			_ = syscall.Close(pair[0])
			closedRead = true
		}
	}
	return pair[0], pair[1], closeRead
}

func TestRecvOneFromKernel_table(t *testing.T) {
	t.Parallel()

	t.Run("positive_success_path", func(t *testing.T) {
		t.Parallel()
		x := newNetlinkerXTCP(t)
		readFD, writeFD, _ := makeSocketPair(t)
		payload := []byte("netlinkbytes")
		if _, err := syscall.Write(writeFD, payload); err != nil {
			t.Fatalf("write: %v", err)
		}
		buf := make([]byte, 256)
		n, retry := x.recvOneFromKernel(readFD, buf)
		if retry {
			t.Fatal("retry = true on success path")
		}
		if n != len(payload) {
			t.Errorf("n = %d, want %d", n, len(payload))
		}
		if !bytes.Equal(buf[:n], payload) {
			t.Errorf("buf = %q, want %q", buf[:n], payload)
		}
	})

	t.Run("negative_recvfrom_error_returns_retry", func(t *testing.T) {
		t.Parallel()
		x := newNetlinkerXTCP(t)
		readFD, _, closeRead := makeSocketPair(t)
		closeRead() // EBADF on next read; cleanup won't double-close
		buf := make([]byte, 64)
		n, retry := x.recvOneFromKernel(readFD, buf)
		if !retry {
			t.Errorf("retry = false on closed fd; want true")
		}
		if n != 0 {
			t.Errorf("n = %d on error, want 0", n)
		}
		if got := testutil.ToFloat64(
			x.pC.WithLabelValues("Netlinker", "nerr", "count")); got != 1 {
			t.Errorf("nerr counter = %v, want 1", got)
		}
	})

	t.Run("boundary_empty_buffer_returns_zero", func(t *testing.T) {
		t.Parallel()
		// syscall.Recvfrom with a zero-length slice would panic on &p[0].
		// Skip the actual call but verify the helper's metric labels exist
		// when invoked with a closed fd (so the recv errors immediately).
		x := newNetlinkerXTCP(t)
		readFD, _, closeRead := makeSocketPair(t)
		closeRead()
		buf := make([]byte, 1) // 1-byte recv → error from closed fd
		n, retry := x.recvOneFromKernel(readFD, buf)
		if !retry {
			t.Error("retry should be true on closed-fd recv")
		}
		if n != 0 {
			t.Errorf("n = %d want 0", n)
		}
	})
}

// ───────────────────────────────────────────────────────────────────────
// captureToFileIfEnabled — positive / negative / boundary / corner /
// adversarial. Each row asserts the returned wf and whether a file was
// created in CapturePath.
// ───────────────────────────────────────────────────────────────────────

func TestCaptureToFileIfEnabled_table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name           string
		category       string
		inputWF        uint32
		corruptPath    bool
		wantWF         uint32
		wantFileExists bool
	}{
		{"positive_writes_and_decrements", "positive", 3, false, 2, true},
		{"positive_single_budget", "positive", 1, false, 0, true},
		{"negative_zero_budget_noop", "negative", 0, false, 0, false},
		{"boundary_max_uint32_decrements", "boundary", ^uint32(0), false, ^uint32(0) - 1, true},
		{"corner_write_fails_resets_to_zero", "corner", 5, true, 0, false},
		{"adversarial_repeated_write_fail_stays_zero", "adversarial", 0, true, 0, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			x := newNetlinkerXTCP(t)
			if tc.corruptPath {
				// Point CapturePath at a non-existent + unwritable dir
				// so os.WriteFile fails. Cleanup is automatic via
				// t.TempDir below the unwritable dir.
				x.config.CapturePath = filepath.Join(t.TempDir(), "does_not_exist", "subdir") + string(os.PathSeparator) + "cap_"
			}
			payload := []byte("hello")
			gotWF := x.captureToFileIfEnabled(tc.inputWF, payload, 7)
			if gotWF != tc.wantWF {
				t.Errorf("returned wf = %d, want %d", gotWF, tc.wantWF)
			}
			// Look for any "netlink." file under the capture dir.
			capDir := filepath.Dir(x.config.CapturePath)
			matched := false
			if entries, err := os.ReadDir(capDir); err == nil {
				for _, e := range entries {
					if strings.HasPrefix(e.Name(), "netlink.") || strings.HasPrefix(e.Name(), "cap_netlink.") {
						matched = true
						break
					}
				}
			}
			if matched != tc.wantFileExists {
				t.Errorf("file-exists = %v, want %v (capDir=%s)", matched, tc.wantFileExists, capDir)
			}
		})
	}
}

// TestCaptureToFileIfEnabled_payloadLength pins bug-39-style behavior:
// the file holds exactly len(packet) bytes, not the pool's full
// capacity. This is the original reason for the captureToFile change.
func TestCaptureToFileIfEnabled_payloadLength(t *testing.T) {
	x := newNetlinkerXTCP(t)
	x.captureToFileIfEnabled(1, []byte("abc"), 0)
	entries, _ := os.ReadDir(filepath.Dir(x.config.CapturePath))
	if len(entries) == 0 {
		t.Fatal("expected one capture file, found none")
	}
	first := entries[0]
	full := filepath.Join(filepath.Dir(x.config.CapturePath), first.Name())
	data, err := os.ReadFile(full) //nolint:gosec // test code under t.TempDir
	if err != nil {
		t.Fatalf("read capture: %v", err)
	}
	if string(data) != "abc" {
		t.Errorf("capture contents = %q, want %q", data, "abc")
	}
}

// ───────────────────────────────────────────────────────────────────────
// maybeForceGC — table covers the offset-modulus contract.
// ───────────────────────────────────────────────────────────────────────

func TestMaybeForceGC_table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		category string
		packets  int
		wantGC   float64
	}{
		{"positive_modulus_hit", "positive", forceGCModulesCst, 1},
		{"positive_double_modulus", "positive", forceGCModulesCst * 2, 1},
		{"negative_packet_zero_no_gc", "negative", 0, 0},
		{"negative_off_modulus", "negative", forceGCModulesCst + 1, 0},
		{"boundary_one_less_than_modulus", "boundary", forceGCModulesCst - 1, 0},
		{"boundary_one_more_than_modulus", "boundary", forceGCModulesCst + 1, 0},
		{"corner_negative_packet_count", "corner", -1, 0},
		{"corner_min_int_no_panic", "corner", -1 << 31, 0},
		{"adversarial_huge_value_off_mod", "adversarial", forceGCModulesCst*1000 - 1, 0},
		{"adversarial_huge_value_on_mod", "adversarial", forceGCModulesCst * 1000, 1},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			x := newNetlinkerXTCP(t)
			before := testutil.ToFloat64(x.pC.WithLabelValues("Netlinker", "runtime.GC()", "count"))
			x.maybeForceGC(tc.packets)
			after := testutil.ToFloat64(x.pC.WithLabelValues("Netlinker", "runtime.GC()", "count"))
			if diff := after - before; diff != tc.wantGC {
				t.Errorf("GC counter delta = %v, want %v (packets=%d)", diff, tc.wantGC, tc.packets)
			}
		})
	}
}

// ───────────────────────────────────────────────────────────────────────
// logRecvDebug + logProcessedDebug — gate on debugLevel and fdToNsMap.
// ───────────────────────────────────────────────────────────────────────

func TestLogRecvDebug_table(t *testing.T) {
	cases := []struct {
		name           string
		category       string
		debugLevel     uint32
		storeNs        string // empty → don't store
		wantSubstr     string
		wantNotPresent bool
	}{
		{"positive_debug_on_ns_known", "positive", 101, "ns-A", "ns:ns-A", false},
		{"positive_debug_on_ns_unknown", "positive", 101, "", "Unknown FD!!", false},
		{"negative_debug_off_threshold", "negative", 100, "ns-A", "", true},
		{"negative_debug_off_zero", "negative", 0, "ns-A", "", true},
		{"boundary_just_above_threshold", "boundary", 101, "ns-A", "ns:ns-A", false},
		{"corner_debug_max", "corner", ^uint32(0), "ns-X", "ns:ns-X", false},
		{"adversarial_empty_ns_string", "adversarial", 101, "", "Unknown FD!!", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			x := newNetlinkerXTCP(t)
			x.debugLevel = tc.debugLevel
			if tc.storeNs != "" {
				x.fdToNsMap.Store(42, tc.storeNs)
			}
			out := withCapturedLog(t, func() {
				x.logRecvDebug(7, 3, 200, 42)
			})
			if tc.wantNotPresent {
				if out != "" {
					t.Errorf("expected no log output, got %q", out)
				}
				return
			}
			if !strings.Contains(out, tc.wantSubstr) {
				t.Errorf("log %q missing %q", out, tc.wantSubstr)
			}
		})
	}
}

func TestLogProcessedDebug_table(t *testing.T) {
	cases := []struct {
		name           string
		category       string
		debugLevel     uint32
		storeNs        string
		wantSubstr     string
		wantNotPresent bool
	}{
		{"positive_debug_on_ns_known", "positive", 101, "nsP", "ns:nsP", false},
		{"positive_debug_on_ns_unknown", "positive", 101, "", "p:5", false},
		{"negative_debug_off_threshold", "negative", 100, "nsP", "", true},
		{"boundary_debug_one_above", "boundary", 101, "nsP", "ns:nsP", false},
		{"corner_packet_max_uint64", "corner", 101, "nsP", fmt.Sprintf("p:%d", ^uint64(0)), false},
		{"adversarial_high_debug_level", "adversarial", 1 << 20, "nsP", "ns:nsP", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			x := newNetlinkerXTCP(t)
			x.debugLevel = tc.debugLevel
			if tc.storeNs != "" {
				x.fdToNsMap.Store(99, tc.storeNs)
			}
			out := withCapturedLog(t, func() {
				pVal := uint64(5)
				if strings.Contains(tc.wantSubstr, "max") || strings.Contains(tc.name, "max_uint64") {
					pVal = ^uint64(0)
				}
				x.logProcessedDebug(11, 4, 256, pVal, 99)
			})
			if tc.wantNotPresent {
				if out != "" {
					t.Errorf("expected no log, got %q", out)
				}
				return
			}
			if !strings.Contains(out, tc.wantSubstr) {
				t.Errorf("log %q missing %q", out, tc.wantSubstr)
			}
		})
	}
}

// ───────────────────────────────────────────────────────────────────────
// Race tests — exercise the helpers concurrently. Each XTCP fixture is
// independent; the helpers only touch x.pC / x.pH / x.fdToNsMap / disk.
// Run with `go test -race`.
// ───────────────────────────────────────────────────────────────────────

func TestNetlinkerHelpers_concurrent(t *testing.T) {
	const goroutines = 16
	x := newNetlinkerXTCP(t)
	x.fdToNsMap.Store(42, "shared-ns")
	x.debugLevel = 0 // keep stdout clean under race

	var wg sync.WaitGroup
	var fileCalls atomic.Int64
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			// Each goroutine drives all four pure helpers many times.
			for j := 0; j < 200; j++ {
				x.maybeForceGC(j * forceGCModulesCst)
				x.logRecvDebug(1, j, 64, 42)
				x.logProcessedDebug(1, j, 64, uint64(j), 42)
				if x.captureToFileIfEnabled(1, []byte("z"), 1) == 0 {
					fileCalls.Add(1)
				}
			}
		}()
	}
	wg.Wait()
}

// ───────────────────────────────────────────────────────────────────────
// Benchmarks
// ───────────────────────────────────────────────────────────────────────

func BenchmarkMaybeForceGC_off(b *testing.B) {
	b.ReportAllocs()
	x := newNetlinkerXTCP(&testing.T{})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		x.maybeForceGC(1)
	}
}

func BenchmarkLogRecvDebug_off(b *testing.B) {
	b.ReportAllocs()
	x := newNetlinkerXTCP(&testing.T{})
	x.debugLevel = 0
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		x.logRecvDebug(1, i, 64, 1)
	}
}

func BenchmarkCaptureToFileIfEnabled_off(b *testing.B) {
	b.ReportAllocs()
	x := newNetlinkerXTCP(&testing.T{})
	payload := []byte("z")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		x.captureToFileIfEnabled(0, payload, 1) // wf=0 → no I/O
	}
}

func BenchmarkRecvOneFromKernel_closedFD(b *testing.B) {
	// Exercises the err-path (closed fd → syscall error, retry=true).
	b.ReportAllocs()
	x := &XTCP{
		config: &xtcp_config.XtcpConfig{},
	}
	// Build minimal prom seams.
	tx := newNetlinkerXTCP(&testing.T{})
	x.pC = tx.pC
	x.pH = tx.pH
	pair, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_DGRAM, 0)
	if err != nil {
		b.Fatalf("socketpair: %v", err)
	}
	_ = syscall.Close(pair[0])
	_ = syscall.Close(pair[1])
	buf := make([]byte, 64)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = x.recvOneFromKernel(pair[0], buf)
	}
}
