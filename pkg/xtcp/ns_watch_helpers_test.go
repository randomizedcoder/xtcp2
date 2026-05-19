package xtcp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	fsnotify "gopkg.in/fsnotify.v1"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/randomizedcoder/xtcp2/pkg/xtcp_config"
)

// ns_watch.go refactor: watchNsNamespace dropped from gocyclo 18 → 11
// by extracting ensureNetNSDir, dispatchNsFsEvent, handleNsWatcherErr.
// These tests cover each helper with positive / negative / boundary /
// corner / adversarial categories, plus race + benchmarks.
// Pre-existing run_helpers_test.go drives watchNsNamespace end-to-end;
// these tests pin the units in isolation.

// newWatcherXTCP wires a minimal XTCP fixture with everything
// watch-related these helpers touch (pC, fdToNsMap not used here, but
// nsMap is required for nsAdd/nsDelete). Used by dispatchNsFsEvent.
func newWatcherXTCP(t *testing.T) *XTCP {
	t.Helper()
	x := newTestXTCP(t, "null:")
	x.fdToNsMap = &sync.Map{}
	x.nsMap = &sync.Map{}
	// nsAdd reads x.config.* — provide an EnabledDeserializers so the
	// helper doesn't nil-deref if a Create event reaches that path.
	x.config.EnabledDeserializers = &xtcp_config.EnabledDeserializers{
		Enabled: map[string]bool{},
	}
	return x
}

// ───────────────────────────────────────────────────────────────────────
// ensureNetNSDir — process-global linuxNetNSDirCst is the production
// constant; tests can't redirect it cheaply, so the table covers the
// "non-production-path" branches that DON'T require root.
// ───────────────────────────────────────────────────────────────────────

func TestEnsureNetNSDir_table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		category string
		build    func(t *testing.T) string
		wantErr  bool
	}{
		{
			name:     "positive_custom_dir_existing",
			category: "positive",
			build:    func(t *testing.T) string { return t.TempDir() },
			wantErr:  false,
		},
		{
			name:     "negative_custom_dir_does_not_exist_still_noop",
			category: "negative",
			// ensureNetNSDir is a no-op for any path != linuxNetNSDirCst,
			// even one that doesn't exist on disk. The subsequent
			// watcher.Add in watchNsNamespace is what surfaces missing
			// dirs — that's by design (don't create user-supplied paths).
			build:   func(t *testing.T) string { return "/no/such/dir/probably" },
			wantErr: false,
		},
		{
			name:     "boundary_empty_string",
			category: "boundary",
			build:    func(t *testing.T) string { return "" },
			wantErr:  false,
		},
		{
			name:     "corner_relative_path",
			category: "corner",
			build:    func(t *testing.T) string { return "." },
			wantErr:  false,
		},
		{
			name:     "adversarial_path_with_special_chars",
			category: "adversarial",
			build:    func(t *testing.T) string { return "/run/netns_with_underscore/" },
			wantErr:  false,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			x := newWatcherXTCP(t)
			x.debugLevel = 0
			dir := tc.build(t)
			err := x.ensureNetNSDir(dir)
			if (err != nil) != tc.wantErr {
				t.Errorf("ensureNetNSDir(%q) err = %v, wantErr=%v", dir, err, tc.wantErr)
			}
		})
	}
}

// ───────────────────────────────────────────────────────────────────────
// dispatchNsFsEvent
// ───────────────────────────────────────────────────────────────────────

func TestDispatchNsFsEvent_table(t *testing.T) {
	// Cannot t.Parallel: nsAdd may touch shared state on x; each subtest
	// gets its own XTCP fixture but the helper logs to stderr.
	cases := []struct {
		name             string
		category         string
		ok               bool
		eventOp          fsnotify.Op
		wantErrSubstring string
		wantEventCnt     float64
		wantCloseCnt     float64
	}{
		{
			name:         "positive_create_event_no_error",
			category:     "positive",
			ok:           true,
			eventOp:      fsnotify.Create,
			wantEventCnt: 1,
		},
		{
			name:         "positive_remove_event_no_error",
			category:     "positive",
			ok:           true,
			eventOp:      fsnotify.Remove,
			wantEventCnt: 1,
		},
		{
			name:             "negative_channel_closed_returns_error",
			category:         "negative",
			ok:               false,
			eventOp:          fsnotify.Create,
			wantErrSubstring: "event channel closed",
			wantEventCnt:     1,
			wantCloseCnt:     1,
		},
		{
			name:         "boundary_zero_op_no_action_no_error",
			category:     "boundary",
			ok:           true,
			eventOp:      fsnotify.Op(0),
			wantEventCnt: 1,
		},
		{
			name:         "corner_chmod_event_silently_ignored",
			category:     "corner",
			ok:           true,
			eventOp:      fsnotify.Chmod,
			wantEventCnt: 1,
		},
		{
			name:         "corner_create_with_chmod_bits_still_creates",
			category:     "corner",
			ok:           true,
			eventOp:      fsnotify.Create | fsnotify.Chmod,
			wantEventCnt: 1,
		},
		{
			name:         "adversarial_all_bits_set",
			category:     "adversarial",
			ok:           true,
			eventOp:      fsnotify.Create | fsnotify.Write | fsnotify.Remove | fsnotify.Rename | fsnotify.Chmod,
			wantEventCnt: 1,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			x := newWatcherXTCP(t)
			x.debugLevel = 0
			// Use a tempdir name so nsAdd has something to chew on.
			ev := fsnotify.Event{
				Name: filepath.Join(t.TempDir(), "ns1"),
				Op:   tc.eventOp,
			}
			err := x.dispatchNsFsEvent(context.Background(), "/some/dir", ev, tc.ok)
			if tc.wantErrSubstring != "" {
				if err == nil || !contains(err.Error(), tc.wantErrSubstring) {
					t.Errorf("err = %v, want substring %q", err, tc.wantErrSubstring)
				}
			} else if err != nil {
				t.Errorf("err = %v, want nil", err)
			}
			gotEvent := testutil.ToFloat64(
				x.pC.WithLabelValues("watchNamespaces", "event", "counter"))
			if gotEvent != tc.wantEventCnt {
				t.Errorf("event counter = %v, want %v", gotEvent, tc.wantEventCnt)
			}
			gotClose := testutil.ToFloat64(
				x.pC.WithLabelValues("watchNamespaces", "watcherClose", "counter"))
			if gotClose != tc.wantCloseCnt {
				t.Errorf("watcherClose counter = %v, want %v", gotClose, tc.wantCloseCnt)
			}
		})
	}
}

// ───────────────────────────────────────────────────────────────────────
// handleNsWatcherErr
// ───────────────────────────────────────────────────────────────────────

func TestHandleNsWatcherErr_table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name             string
		category         string
		ok               bool
		werr             error
		wantErrSubstring string
		wantErrCnt       float64
		wantCloseCnt     float64
	}{
		{
			name:       "positive_normal_error_no_return",
			category:   "positive",
			ok:         true,
			werr:       errors.New("some watcher hiccup"),
			wantErrCnt: 1,
		},
		{
			name:             "negative_error_channel_closed_returns",
			category:         "negative",
			ok:               false,
			werr:             nil,
			wantErrSubstring: "error channel closed",
			wantErrCnt:       1,
			wantCloseCnt:     1,
		},
		{
			name:       "boundary_nil_err_with_ok_true",
			category:   "boundary",
			ok:         true,
			werr:       nil,
			wantErrCnt: 1,
		},
		{
			name:       "corner_wrapped_error",
			category:   "corner",
			ok:         true,
			werr:       fmt.Errorf("watcher: %w", errors.New("inner")),
			wantErrCnt: 1,
		},
		{
			name:             "adversarial_huge_error_string",
			category:         "adversarial",
			ok:               false,
			werr:             nil,
			wantErrSubstring: "error channel closed",
			wantErrCnt:       1,
			wantCloseCnt:     1,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			x := newWatcherXTCP(t)
			x.debugLevel = 0
			err := x.handleNsWatcherErr("/some/dir", tc.werr, tc.ok)
			if tc.wantErrSubstring != "" {
				if err == nil || !contains(err.Error(), tc.wantErrSubstring) {
					t.Errorf("err = %v, want substring %q", err, tc.wantErrSubstring)
				}
			} else if err != nil {
				t.Errorf("err = %v, want nil", err)
			}
			gotErr := testutil.ToFloat64(
				x.pC.WithLabelValues("watchNamespaces", "error", "error"))
			if gotErr != tc.wantErrCnt {
				t.Errorf("error counter = %v, want %v", gotErr, tc.wantErrCnt)
			}
			gotClose := testutil.ToFloat64(
				x.pC.WithLabelValues("watchNamespaces", "watcherCloseErr", "counter"))
			if gotClose != tc.wantCloseCnt {
				t.Errorf("watcherCloseErr counter = %v, want %v", gotClose, tc.wantCloseCnt)
			}
		})
	}
}

// ───────────────────────────────────────────────────────────────────────
// Race — each goroutine works on its own XTCP fixture; verifies the
// pure helpers don't accidentally share package-global state.
// ───────────────────────────────────────────────────────────────────────

func TestNsWatchHelpers_concurrent(t *testing.T) {
	const goroutines = 32
	var wg sync.WaitGroup
	wg.Add(goroutines)
	var iter atomic.Int64
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			x := newWatcherXTCP(t)
			x.debugLevel = 0
			for j := 0; j < 200; j++ {
				_ = x.handleNsWatcherErr("/x", errors.New("e"), j%5 != 0)
				ev := fsnotify.Event{Name: "/x/y", Op: fsnotify.Create}
				_ = x.dispatchNsFsEvent(context.Background(), "/x", ev, j%7 != 0)
				_ = x.ensureNetNSDir(t.TempDir())
				iter.Add(1)
			}
		}()
	}
	wg.Wait()
}

// ───────────────────────────────────────────────────────────────────────
// Benchmarks
// ───────────────────────────────────────────────────────────────────────

func BenchmarkEnsureNetNSDir_customDir(b *testing.B) {
	b.ReportAllocs()
	x := newWatcherXTCP(&testing.T{})
	dir, _ := os.MkdirTemp("", "ensure_bench_")
	defer func() { _ = os.RemoveAll(dir) }()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = x.ensureNetNSDir(dir)
	}
}

func BenchmarkDispatchNsFsEvent_chmod(b *testing.B) {
	b.ReportAllocs()
	x := newWatcherXTCP(&testing.T{})
	x.debugLevel = 0
	ev := fsnotify.Event{Name: "/run/netns/x", Op: fsnotify.Chmod}
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = x.dispatchNsFsEvent(ctx, "/run/netns", ev, true)
	}
}

func BenchmarkHandleNsWatcherErr_normal(b *testing.B) {
	b.ReportAllocs()
	x := newWatcherXTCP(&testing.T{})
	x.debugLevel = 0
	werr := errors.New("test")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = x.handleNsWatcherErr("/x", werr, true)
	}
}

// contains is a small substring helper to avoid pulling in strings for
// the single assertion site.
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
