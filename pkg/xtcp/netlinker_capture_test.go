//go:build capture

package xtcp

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestAnnounceCapture: armed, the capture writer warns and raises its gauge;
// compiled in but idle it does neither.
func TestAnnounceCapture(t *testing.T) {
	cases := []struct {
		name        string
		category    string
		description string
		writeFiles  uint32
		expected    float64 // gauge; a warning is logged iff it is 1
	}{
		{"capture_compiled_warns", "positive", "armed: warning and gauge 1", 3, 1},
		{"capture_compiled_idle", "negative", "idle: no warning, gauge 0", 0, 0},
		{"capture_budget_one", "boundary", "the smallest budget still arms", 1, 1},
		{"capture_rearm_then_idle", "corner", "an idle announce clears a prior armed gauge", 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			x := newNetlinkerXTCP(t)
			x.config.WriteFiles = tc.writeFiles
			var buf bytes.Buffer
			prev := log.Writer()
			log.SetOutput(&buf)
			t.Cleanup(func() { log.SetOutput(prev) })
			debugCaptureEnabled.Set(1) // a stale armed value must not survive an idle announce
			x.announceCapture()
			if got := testutil.ToFloat64(debugCaptureEnabled); got != tc.expected {
				t.Errorf("%s: gauge = %v, want %v", tc.description, got, tc.expected)
			}
			warned := strings.Contains(buf.String(), "netlink capture is ARMED")
			if warned != (tc.expected == 1) {
				t.Errorf("%s: warned = %v, log %q", tc.description, warned, buf.String())
			}
			if warned && !strings.Contains(buf.String(), x.config.CapturePath) {
				t.Errorf("%s: warning does not name the capture path: %q", tc.description, buf.String())
			}
		})
	}
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
	root, err := os.OpenRoot(filepath.Dir(x.config.CapturePath))
	if err != nil {
		t.Fatalf("open capture dir: %v", err)
	}
	t.Cleanup(func() {
		if cerr := root.Close(); cerr != nil {
			t.Errorf("close capture dir: %v", cerr)
		}
	})
	data, err := root.ReadFile(first.Name())
	if err != nil {
		t.Fatalf("read capture: %v", err)
	}
	if string(data) != "abc" {
		t.Errorf("capture contents = %q, want %q", data, "abc")
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
