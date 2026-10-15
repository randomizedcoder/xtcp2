package linkmonitor_test

import (
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
	monitorprom "github.com/randomizedcoder/xtcp2/pkg/linkmonitor/prometheus"
)

var updatePollGolden = flag.Bool("update-poll-golden", false, "rewrite testdata/ondemand golden files")

// pollGolden compares lines with the golden file, which was generated at the
// commit before the on-demand mode with -update-poll-golden.
func pollGolden(t *testing.T, name string, lines []string) {
	t.Helper()
	path := filepath.Join("testdata", "ondemand", name)
	got := strings.Join(lines, "\n") + "\n"
	if *updatePollGolden {
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		gotLines, wantLines := lines, strings.Split(strings.TrimSuffix(string(want), "\n"), "\n")
		for _, line := range gotLines {
			if !slices.Contains(wantLines, line) {
				t.Errorf("new: %s", line)
			}
		}
		for _, line := range wantLines {
			if !slices.Contains(gotLines, line) {
				t.Errorf("gone: %s", line)
			}
		}
		t.Fatalf("%s differs from %s", name, path)
	}
}

func TestPollModeUnchanged(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		build                                        func(*linkmonitor.PrometheusFixture)
		golden                                       string
		summary                                      bool
	}{
		{"metrics-all", "positive", "deployed Ethernet host and native RDMA, full exposition", "every series as before the on-demand mode", func(f *linkmonitor.PrometheusFixture) {
			f.PerformanceDeployedHost()
			f.PerformanceNative()
			f.Publish(3)
		}, "all.golden", false},
		{"summary", "positive", "the same host's /metrics summary", "as before", func(f *linkmonitor.PrometheusFixture) {
			f.PerformanceDeployedHost()
			f.PerformanceNative()
			f.Publish(3)
		}, "summary.golden", true},
		{"empty-host", "boundary", "no devices", "as before", func(f *linkmonitor.PrometheusFixture) { f.Publish(0) }, "empty.golden", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			f := linkmonitor.NewPrometheusFixture(t)
			tc.build(f)
			r := monitorRegistry(t, f.Monitor)
			if tc.summary {
				r = summaryRegistry(t, f.Monitor, fixedClock(f.ScrapeTime(0)))
			}
			pollGolden(t, tc.golden, series(t, r))
		})
	}
	t.Run("no-new-series", func(t *testing.T) {
		t.Log("negative: poll mode; expected: no rdma_errors collector and no on-demand series")
		f := linkmonitor.NewPrometheusFixture(t)
		f.PerformanceDeployedHost()
		f.PerformanceNative()
		f.Publish(3)
		for _, line := range series(t, monitorRegistry(t, f.Monitor)) {
			if strings.Contains(line, "rdma_errors") || strings.Contains(line, "on_demand") || monitorprom.CategoryOf(line[:strings.IndexByte(line, '{')]) == "" {
				t.Errorf("poll mode publishes %s", line)
			}
		}
	})
}
