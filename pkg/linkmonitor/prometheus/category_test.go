package prometheus

import (
	"slices"
	"strings"
	"testing"
)

func TestCategoryOf(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		metric                                       string
		want                                         string
	}{
		{"summary problems", "positive", "summary-only family", "summary", "go_link_monitor_problems", CategorySummary},
		{"summary interface", "positive", "sparse interface problem", "summary", "go_link_monitor_interface_problem", CategorySummary},
		{"shared build", "positive", "build_info is served everywhere", "shared", "go_link_monitor_build_info", CategoryShared},
		{"shared health", "positive", "collection_healthy is served everywhere", "shared", "go_link_monitor_collection_healthy", CategoryShared},
		{"link check", "positive", "interface_check", "link", "go_link_monitor_interface_check", CategoryLink},
		{"link flap time", "positive", "last flap timestamp", "link", "go_link_monitor_interface_last_flap_timestamp_seconds", CategoryLink},
		{"traffic netdev", "positive", "dynamic netdev counter", "traffic", "go_link_monitor_interface_receive_crc_errors_total", CategoryTraffic},
		{"traffic transmit", "positive", "dynamic transmit counter", "traffic", "go_link_monitor_interface_transmit_packets_total", CategoryTraffic},
		{"netstat dynamic", "positive", "netstat family with mixed case", "netstat", "go_link_monitor_netstat_TcpExt_TCPTimeouts", CategoryNetstat},
		{"ethtool settings", "positive", "supported speed family", "ethtool", "go_link_monitor_interface_supported_speed_info", CategoryEthtool},
		{"ethtool hex", "positive", "ring family", "ethtool", "go_link_monitor_interface_ring_rx_max", CategoryEthtool},
		{"ethtool channels", "positive", "channel family without trailing underscore", "ethtool", "go_link_monitor_interface_channels", CategoryEthtool},
		{"driver", "positive", "bulk driver statistic", "driver", "go_link_monitor_ethtool_statistic", CategoryDriver},
		{"rdma check", "positive", "rdma_port_check", "rdma", "go_link_monitor_rdma_port_check", CategoryRDMA},
		{"infiniband", "positive", "native counter", "rdma", "go_link_monitor_infiniband_symbol_error_total", CategoryRDMA},
		{"collection admin", "positive", "admin gRPC request metric", "collection", "go_link_monitor_grpc_requests_total", CategoryCollection},
		{"collection support", "positive", "collector_support", "collection", "go_link_monitor_collector_support", CategoryCollection},
		{"delta anchor", "boundary", "up_links_delta vs up_links", "different categories", "go_link_monitor_up_links_delta", CategorySummary},
		{"up links", "boundary", "up_links prefix of up_links_delta", "link", "go_link_monitor_up_links", CategoryLink},
		{"traffic no total", "boundary", "receive family without _total", "uncategorized", "go_link_monitor_interface_receive_rate", ""},
		{"prefix only", "boundary", "bare prefix", "uncategorized", "go_link_monitor_", ""},
		{"netstat bare", "boundary", "netstat_ with empty field", "netstat by prefix", "go_link_monitor_netstat_", CategoryNetstat},
		{"unknown", "negative", "unknown family", "uncategorized", "go_link_monitor_mystery", ""},
		{"non prefixed", "negative", "name without prefix", "uncategorized", "up_links", ""},
		{"foreign", "negative", "process metric", "uncategorized", "process_cpu_seconds_total", ""},
		{"empty", "corner", "empty name", "uncategorized", "", ""},
		{"double prefix", "corner", "prefix repeated", "uncategorized", "go_link_monitor_go_link_monitor_up_links", ""},
		{"case", "corner", "upper-case prefix", "uncategorized", "GO_LINK_MONITOR_up_links", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			if got := CategoryOf(tc.metric); got != tc.want {
				t.Fatalf("CategoryOf(%q)=%q, want %q", tc.metric, got, tc.want)
			}
		})
	}
}

// TestCategoryFixedFamilies requires every fixed descriptor to map to exactly
// one known category, so a new family cannot silently fall off every path.
func TestCategoryFixedFamilies(t *testing.T) {
	known := append(Categories(), CategorySummary, CategoryShared)
	for name := range fixedDescriptors() {
		if category := CategoryOf(metricPrefix + name); !slices.Contains(known, category) {
			t.Errorf("fixed family %q has category %q", name, category)
		}
	}
	for name, category := range categoryNames {
		if !slices.Contains(known, category) {
			t.Errorf("table family %q has unknown category %q", name, category)
		}
	}
}

func TestCategoryMatcher(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		categories                                   []string
		wantErr                                      string
		selected, rejected                           []string
	}{
		{"single", "positive", "link only", "link and shared selected", []string{CategoryLink}, "",
			[]string{"go_link_monitor_interface_check", "go_link_monitor_build_info", "go_link_monitor_collection_healthy"},
			[]string{"go_link_monitor_netstat_Tcp_ActiveOpens", "go_link_monitor_problems", "go_link_monitor_mystery"}},
		{"several", "positive", "netstat and rdma", "both selected", []string{CategoryNetstat, CategoryRDMA}, "",
			[]string{"go_link_monitor_netstat_Ip_Forwarding", "go_link_monitor_rdma_port_up"},
			[]string{"go_link_monitor_ethtool_statistic"}},
		{"all", "positive", "every detail category", "summary and unknown still rejected", Categories(), "",
			[]string{"go_link_monitor_interface_up", "go_link_monitor_grpc_requests_total"},
			[]string{"go_link_monitor_interface_problem", "go_link_monitor_mystery", "x"}},
		{"empty", "boundary", "no categories", "only shared selected", nil, "",
			[]string{"go_link_monitor_build_info"}, []string{"go_link_monitor_interface_up"}},
		{"unknown", "negative", "misspelled category", "construction error", []string{"links"}, "unknown", nil, nil},
		{"summary", "negative", "summary is not a detail category", "construction error", []string{CategorySummary}, "unknown", nil, nil},
		{"shared", "negative", "shared is implicit", "construction error", []string{CategoryShared}, "unknown", nil, nil},
		{"duplicate", "negative", "category listed twice", "construction error", []string{CategoryLink, CategoryLink}, "duplicate", nil, nil},
		{"empty name", "corner", "empty string category", "construction error", []string{""}, "unknown", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			if err := ValidateCategories(tc.categories); (err != nil) != (tc.wantErr != "") {
				t.Fatalf("ValidateCategories=%v, want error %q", err, tc.wantErr)
			}
			match, err := CategoryMatcher(tc.categories...)
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

// TestCategoryMatcherCopiesInput guards against a caller mutating the slice
// after construction changing what the matcher selects.
func TestCategoryMatcherCopiesInput(t *testing.T) {
	categories := []string{CategoryLink}
	match, err := CategoryMatcher(categories...)
	if err != nil {
		t.Fatal(err)
	}
	categories[0] = CategoryNetstat
	if !match("go_link_monitor_interface_up") || match("go_link_monitor_netstat_Ip_Forwarding") {
		t.Fatal("matcher followed caller mutation")
	}
}

// FuzzCategoryOf checks totality and determinism: every name maps to "" or a
// known category, the same each call, and detail matchers partition names.
func FuzzCategoryOf(f *testing.F) {
	for _, seed := range []string{"", "go_link_monitor_", "go_link_monitor_up_links", "go_link_monitor_netstat_Tcp_X",
		"go_link_monitor_interface_receive_x_total", "go_link_monitor_interface_receive_", "go_link_monitor_grpc_", "\xff"} {
		f.Add(seed)
	}
	known := append(Categories(), CategorySummary, CategoryShared, "")
	matchers := make(map[string]func(string) bool, len(Categories()))
	for _, category := range Categories() {
		match, err := CategoryMatcher(category)
		if err != nil {
			f.Fatal(err)
		}
		matchers[category] = match
	}
	f.Fuzz(func(t *testing.T, name string) {
		category := CategoryOf(name)
		if !slices.Contains(known, category) || CategoryOf(name) != category {
			t.Fatalf("CategoryOf(%q)=%q", name, category)
		}
		if category != "" && !strings.HasPrefix(name, metricPrefix) {
			t.Fatalf("unprefixed %q categorized as %q", name, category)
		}
		selected := 0
		for _, match := range matchers {
			if match(name) {
				selected++
			}
		}
		want := 0
		switch category {
		case CategoryShared:
			want = len(matchers)
		case "", CategorySummary:
		default:
			want = 1
		}
		if selected != want {
			t.Fatalf("%q (%q) selected by %d detail matchers, want %d", name, category, selected, want)
		}
	})
}
