package prometheus

import (
	"fmt"
	"slices"
	"strings"
)

// Metric categories. Each go_link_monitor_ family belongs to exactly one;
// CategoryShared families are served with every category.
const (
	CategorySummary    = "summary"
	CategoryLink       = "link"
	CategoryTraffic    = "traffic"
	CategoryEthtool    = "ethtool"
	CategoryDriver     = "driver"
	CategoryRDMA       = "rdma"
	CategoryNetstat    = "netstat"
	CategoryCollection = "collection"
	CategoryShared     = "shared"
)

// Categories lists the detail categories in path order.
func Categories() []string {
	return []string{CategoryLink, CategoryTraffic, CategoryEthtool, CategoryDriver, CategoryRDMA, CategoryNetstat, CategoryCollection}
}

const metricPrefix = "go_link_monitor_"

var categoryNames = map[string]string{
	"build_info": CategoryShared, "collection_healthy": CategoryShared,

	"problems": CategorySummary, "interface_problem": CategorySummary, "host_problem": CategorySummary,
	"up_links_delta": CategorySummary, "baseline_ready": CategorySummary,

	"up_links": CategoryLink, "baseline_up_links": CategoryLink, "interface_up": CategoryLink,
	"interface_check": CategoryLink, "interface_classification": CategoryLink,
	"interface_max_speed_exception": CategoryLink, "max_speed_exception_match": CategoryLink,
	"interface_expected_speed_bits_per_second": CategoryLink, "interface_speed_bits_per_second": CategoryLink,
	"interface_max_speed_bits_per_second": CategoryLink, "interface_duplex_info": CategoryLink,
	"interface_carrier": CategoryLink, "interface_carrier_changes_total": CategoryLink,
	"interface_carrier_up_changes_total": CategoryLink, "interface_carrier_down_changes_total": CategoryLink,
	"observed_oper_transitions_total": CategoryLink, "interface_last_flap_timestamp_seconds": CategoryLink,
	"interface_last_error_timestamp_seconds": CategoryLink,

	"interface_multicast_total": CategoryTraffic, "interface_collisions_total": CategoryTraffic,

	"interface_driver_info": CategoryEthtool, "interface_driver_known": CategoryEthtool,
	"interface_autonegotiate": CategoryEthtool, "driver_link_down_events_total": CategoryEthtool,

	"ethtool_statistic": CategoryDriver, "phy_statistic": CategoryDriver,

	"collector_support": CategoryCollection, "collector_success": CategoryCollection,
	"collector_last_success_timestamp_seconds": CategoryCollection, "collector_duration_seconds": CategoryCollection,
	"collector_errors_total": CategoryCollection, "collector_omitted_statistics": CategoryCollection,
	"resyncs_total": CategoryCollection, "counter_discontinuities_total": CategoryCollection,
	"baseline_write_errors_total": CategoryCollection, "last_successful_resync_timestamp_seconds": CategoryCollection,
}

// categoryPrefixes assign families that are not fixed names, in match order.
var categoryPrefixes = [...]struct{ prefix, category string }{
	{"netstat_", CategoryNetstat},
	{"infiniband_", CategoryRDMA},
	{"rdma_", CategoryRDMA},
	{"interface_supported_", CategoryEthtool},
	{"interface_advertised_", CategoryEthtool},
	{"interface_autonegotiate_", CategoryEthtool},
	{"interface_pause_", CategoryEthtool},
	{"interface_asymmetric_pause_", CategoryEthtool},
	{"interface_channels", CategoryEthtool},
	{"interface_ring_", CategoryEthtool},
	{"grpc_", CategoryCollection},
}

// CategoryOf returns the category of a metric name, or "" for names outside
// the table, which are served only on the unfiltered path.
func CategoryOf(name string) string {
	family, ok := strings.CutPrefix(name, metricPrefix)
	if !ok {
		return ""
	}
	if category, ok := categoryNames[family]; ok {
		return category
	}
	for _, p := range categoryPrefixes {
		if strings.HasPrefix(family, p.prefix) {
			return p.category
		}
	}
	if (strings.HasPrefix(family, "interface_receive_") || strings.HasPrefix(family, "interface_transmit_")) && strings.HasSuffix(family, "_total") {
		return CategoryTraffic
	}
	return ""
}

// CategoryMatcher returns a WithMetricFilter matcher selecting the named
// categories plus the shared families. Names are validated once here.
func CategoryMatcher(categories ...string) (func(string) bool, error) {
	if err := ValidateCategories(categories); err != nil {
		return nil, err
	}
	selected := slices.Clone(categories)
	return func(name string) bool {
		category := CategoryOf(name)
		return category == CategoryShared || (category != "" && slices.Contains(selected, category))
	}, nil
}

// ValidateCategories rejects unknown and duplicate detail category names.
func ValidateCategories(categories []string) error {
	valid := Categories()
	for i, category := range categories {
		if !slices.Contains(valid, category) {
			return fmt.Errorf("unknown metric category %q", category)
		}
		if slices.Contains(categories[:i], category) {
			return fmt.Errorf("duplicate metric category %q", category)
		}
	}
	return nil
}
