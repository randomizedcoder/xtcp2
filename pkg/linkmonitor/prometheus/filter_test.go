package prometheus

import (
	"strings"
	"testing"

	client "github.com/prometheus/client_golang/prometheus"
)

func TestNameMatcherTables(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		include, exclude                             string
		wantErr                                      string
		selected, rejected                           []string
	}{
		{"exact core", "positive", "default core pattern", "audit names selected, bulk names rejected", CoreMetricsPattern, "", "",
			[]string{"go_link_monitor_interface_up", "go_link_monitor_interface_check", "go_link_monitor_interface_speed_bits_per_second", "go_link_monitor_interface_duplex_info", "go_link_monitor_up_links", "go_link_monitor_build_info", "go_link_monitor_collector_success",
				"go_link_monitor_interface_carrier_down_changes_total", "go_link_monitor_rdma_port_up", "go_link_monitor_rdma_port_check"},
			[]string{"go_link_monitor_ethtool_statistic", "go_link_monitor_netstat_Tcp_ActiveOpens", "go_link_monitor_collector_support", "go_link_monitor_collector_errors_total", "go_link_monitor_infiniband_symbol_error_total",
				"go_link_monitor_infiniband_link_downed_total", "go_link_monitor_rdma_port_active_width", "go_link_monitor_interface_receive_dropped_total", "go_link_monitor_rdma_hw_counter"}},
		{"everything", "positive", "include .*", "every name selected", ".*", "", "",
			[]string{"go_link_monitor_ethtool_statistic", "x", "go_link_monitor_up_links"}, nil},
		{"alternatives", "positive", "multi-alternative include", "each alternative selected", "a|b_c|d:e", "", "",
			[]string{"a", "b_c", "d:e"}, []string{"ab", "b", "c"}},
		{"bad include", "negative", "malformed include", "construction error names include", "[", "", "include", nil, nil},
		{"bad exclude", "negative", "malformed exclude", "construction error names exclude", ".*", "(", "exclude", nil, nil},
		{"outside include", "negative", "name absent from include", "rejected", "go_link_monitor_up_links", "", "",
			nil, []string{"go_link_monitor_interface_up"}},
		{"exclude wins", "negative", "name in include and exclude", "exclude takes precedence", "go_link_monitor_.*", "go_link_monitor_ethtool_.*", "",
			[]string{"go_link_monitor_up_links"}, []string{"go_link_monitor_ethtool_statistic"}},
		{"empty include", "boundary", "empty include", "nothing selected, not even the empty name", "", "", "",
			nil, []string{"", "go_link_monitor_up_links", "x"}},
		{"empty exclude", "boundary", "empty exclude", "nothing excluded", "x|", "", "",
			[]string{"x", ""}, nil},
		{"anchored", "boundary", "prefix and suffix of a selected name", "only the exact name is selected", "go_link_monitor_up_links", "", "",
			[]string{"go_link_monitor_up_links"}, []string{"go_link_monitor_up_links_delta", "x_go_link_monitor_up_links", "go_link_monitor_baseline_up_links"}},
		{"user anchors", "corner", "caller supplies ^ and $", "double anchoring is harmless", "^go_link_monitor_up_links$", "^$", "",
			[]string{"go_link_monitor_up_links"}, []string{"go_link_monitor_up_links_delta"}},
		{"top-level alternation", "corner", "alternation without grouping", "anchors bind the whole alternation", "a|b", "", "",
			[]string{"a", "b"}, []string{"ab", "xa", "bx"}},
		{"empty name", "corner", "empty-string name against .*", "selected because .* matches empty", ".*", "", "",
			[]string{""}, nil},
		{"unicode", "corner", "unicode literal in pattern", "matched as UTF-8 text", "métrique_.*", "", "",
			[]string{"métrique_x"}, []string{"metrique_x"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			match, err := NameMatcher(tc.include, tc.exclude)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error=%v, want containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range tc.selected {
				if !match(name) {
					t.Errorf("%q rejected, want selected", name)
				}
			}
			for _, name := range tc.rejected {
				if match(name) {
					t.Errorf("%q selected, want rejected", name)
				}
			}
		})
	}
}

func TestCollectorFixedSelection(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		opts                                         []Option
		emitted, skipped                             []string
		matchCalls                                   int
	}{
		{"unfiltered", "positive", "no option", "every fixed descriptor kept and no match cache", nil,
			[]string{"up_links", "collector_support", "collection_healthy"}, nil, 0},
		{"nil filter", "boundary", "explicit nil match", "same as unfiltered", []Option{WithMetricFilter(nil)},
			[]string{"up_links", "collector_support"}, nil, 0},
		{"core", "positive", "default core pattern", "bulk diagnostics skipped", []Option{WithMetricFilter(mustMatcher(t, CoreMetricsPattern, ""))},
			[]string{"up_links", "interface_check", "collector_success", "collection_healthy"}, []string{"collector_support", "collector_errors_total", "resyncs_total"}, len(fixedDescriptors())},
		{"reject all", "negative", "filter rejects everything", "every fixed descriptor skipped", []Option{WithMetricFilter(func(string) bool { return false })},
			nil, []string{"up_links", "collection_healthy"}, len(fixedDescriptors())},
		{"last option wins", "corner", "two filters", "later option replaces earlier", []Option{WithMetricFilter(func(string) bool { return false }), WithMetricFilter(nil)},
			[]string{"up_links"}, nil, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			c := NewCollector(nil, tc.opts...).(*collector)
			if len(c.fixed) != len(fixedDescriptors()) {
				t.Fatalf("fixed keys=%d, want %d", len(c.fixed), len(fixedDescriptors()))
			}
			for _, name := range tc.emitted {
				if c.fixed[name] == nil {
					t.Errorf("%s skipped, want emitted", name)
				}
			}
			for _, name := range tc.skipped {
				if c.fixed[name] != nil {
					t.Errorf("%s emitted, want skipped", name)
				}
			}
			if len(c.matched) != tc.matchCalls {
				t.Errorf("match cache=%d, want %d", len(c.matched), tc.matchCalls)
			}
		})
	}
}

func TestCollectorSelectsOncePerName(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		lookups                                      []string
		wantCalls                                    int
	}{
		{"single", "positive", "one name", "one match call", []string{"a"}, 1},
		{"repeat", "boundary", "same name many times", "still one match call", []string{"a", "a", "a", "a"}, 1},
		{"distinct", "positive", "distinct names", "one call per name", []string{"a", "b", "c"}, 3},
		{"interleaved", "corner", "names return after others", "cached result reused", []string{"a", "b", "a", "b", "c", "a"}, 3},
		{"none", "boundary", "no lookups", "no calls", nil, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			calls := 0
			c := &collector{match: func(name string) bool { calls++; return name == "a" }, matched: make(map[string]bool)}
			for _, name := range tc.lookups {
				if got := c.selects(name); got != (name == "a") {
					t.Fatalf("selects(%q)=%t", name, got)
				}
			}
			if calls != tc.wantCalls {
				t.Fatalf("match calls=%d, want %d", calls, tc.wantCalls)
			}
		})
	}
}

func TestCollectorFilteredFailuresRemainVisible(t *testing.T) {
	t.Log("negative: nil monitor with every name filtered out; expected: Gather still reports the failure")
	r := client.NewRegistry()
	if err := r.Register(NewCollector(nil, WithMetricFilter(func(string) bool { return false }))); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Gather(); err == nil || !strings.Contains(err.Error(), "nil link monitor") {
		t.Fatalf("Gather error=%v, want nil link monitor", err)
	}
}

func mustMatcher(t *testing.T, include, exclude string) func(string) bool {
	t.Helper()
	match, err := NameMatcher(include, exclude)
	if err != nil {
		t.Fatal(err)
	}
	return match
}
