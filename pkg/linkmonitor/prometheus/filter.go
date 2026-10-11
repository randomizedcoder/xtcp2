package prometheus

import (
	"fmt"
	"regexp"
)

// CoreMetricsPattern selects the audit set: link counts against the baseline,
// per-interface and RDMA port up/speed/duplex/width checks, flaps, link error
// counters and collector success. RDMA counters, bulk driver/PHY statistics,
// host protocol fields and other diagnostics remain available from an
// unfiltered collector.
const CoreMetricsPattern = "go_link_monitor_(" +
	"build_info|up_links|baseline_up_links|up_links_delta|baseline_ready|collection_healthy|" +
	"last_successful_resync_timestamp_seconds|" +
	"interface_up|interface_check|interface_speed_bits_per_second|interface_max_speed_bits_per_second|" +
	"interface_duplex_info|interface_max_speed_exception|max_speed_exception_match|" +
	"rdma_port_up|rdma_port_check|" +
	"observed_oper_transitions_total|interface_carrier_changes_total|" +
	"interface_carrier_up_changes_total|interface_carrier_down_changes_total|" +
	"interface_receive_crc_errors_total|interface_receive_errors_total|interface_transmit_errors_total|" +
	"collector_success)"

// Option configures a collector at construction.
type Option func(*collector)

// WithMetricFilter emits only metric names for which match returns true. The
// collector calls match at most once per distinct name for its lifetime: fixed
// names at construction and source names when they first appear in a schema
// revision. Scrapes only read the cached result. A nil match emits everything.
func WithMetricFilter(match func(name string) bool) Option {
	return func(c *collector) { c.match = match }
}

// NameMatcher compiles fully anchored include and exclude expressions once. An
// empty include matches nothing; an empty exclude excludes nothing. A name is
// selected when it matches include and does not match exclude.
func NameMatcher(include, exclude string) (func(string) bool, error) {
	if include == "" {
		return func(string) bool { return false }, nil
	}
	in, err := regexp.Compile("^(?:" + include + ")$")
	if err != nil {
		return nil, fmt.Errorf("metric include regexp: %w", err)
	}
	if exclude == "" {
		return in.MatchString, nil
	}
	ex, err := regexp.Compile("^(?:" + exclude + ")$")
	if err != nil {
		return nil, fmt.Errorf("metric exclude regexp: %w", err)
	}
	return func(name string) bool { return in.MatchString(name) && !ex.MatchString(name) }, nil
}

// selects reports whether name is emitted, consulting match only for names not
// seen before. Callers hold c.mu, or run before the collector is shared.
func (c *collector) selects(name string) bool {
	if c.match == nil {
		return true
	}
	if selected, ok := c.matched[name]; ok {
		return selected
	}
	selected := c.match(name)
	c.matched[name] = selected
	return selected
}
