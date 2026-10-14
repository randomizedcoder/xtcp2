package prometheus

import (
	"slices"
	"time"

	client "github.com/prometheus/client_golang/prometheus"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
)

const (
	interfaceLabel = "interface"
	collectorLabel = "collector"
	reasonLabel    = "reason"
	statusLabel    = "status"
	unknownStatus  = "unknown"
)

var fixedLabels = map[string][]string{
	"baseline_up_links": nil, "up_links": nil, "up_links_delta": nil,
	"baseline_ready": nil, "baseline_write_errors_total": nil, "collection_healthy": nil,
	"last_successful_resync_timestamp_seconds": nil,
	"resyncs_total": {reasonLabel, "result"},
	"interface_up":  {interfaceLabel}, "interface_check": {interfaceLabel, "check", statusLabel},
	"interface_classification": {interfaceLabel, statusLabel}, "interface_max_speed_exception": {interfaceLabel},
	"interface_expected_speed_bits_per_second": {interfaceLabel},
	"max_speed_exception_match":                {"selector", statusLabel},
	"observed_oper_transitions_total":          {interfaceLabel, "direction"},
	"collector_support":                        {interfaceLabel, collectorLabel, statusLabel},
	"collector_success":                        {interfaceLabel, collectorLabel},
	"collector_last_success_timestamp_seconds": {interfaceLabel, collectorLabel},
	"collector_duration_seconds":               {interfaceLabel, collectorLabel},
	"collector_errors_total":                   {interfaceLabel, collectorLabel, reasonLabel},
	"collector_omitted_statistics":             {interfaceLabel, collectorLabel, reasonLabel},
	"counter_discontinuities_total":            {interfaceLabel, "source"},
	"interface_last_flap_timestamp_seconds":    {interfaceLabel},
	"interface_last_error_timestamp_seconds":   {interfaceLabel, "class"},
}

func fixedDescriptors() map[string]*client.Desc {
	result := make(map[string]*client.Desc, len(fixedLabels))
	for name, names := range fixedLabels {
		result[name] = client.NewDesc(metricPrefix+name, "Link monitor "+name+".", names, nil)
	}
	return result
}

// deviceScoped reports whether any selected fixed family is per interface, so
// collectors that select none skip the device walk entirely.
func deviceScoped(fixed map[string]*client.Desc) bool {
	for name, desc := range fixed {
		if desc != nil && slices.Contains(fixedLabels[name], interfaceLabel) {
			return true
		}
	}
	return false
}

func (e emitter) gauge(name string, value float64, labels ...string) {
	e.metric(e.fixed[name], client.GaugeValue, value, labels...)
}
func (e emitter) counter(name string, value uint64, labels ...string) {
	e.metric(e.fixed[name], client.CounterValue, float64(value), labels...)
}
func boolean(value bool) float64 {
	if value {
		return 1
	}
	return 0
}

func (e emitter) policy(s linkmonitor.Snapshot, devices bool) {
	counts, health := s.Counts(), s.Health()
	if v, ok := counts.Current(); ok {
		e.gauge("up_links", float64(v))
	}
	if v, ok := counts.Expected(); ok {
		e.gauge("baseline_up_links", float64(v))
	}
	if negative, magnitude, ok := counts.Delta(); ok {
		value := float64(magnitude)
		if negative {
			value = -value
		}
		e.gauge("up_links_delta", value)
	}
	e.gauge("baseline_ready", boolean(health.BaselineReady))
	e.gauge("collection_healthy", boolean(health.CollectionHealthy))
	e.counter("baseline_write_errors_total", s.BaselineWriteErrors())
	if stamp, ok := s.LastSuccessfulResync(); ok {
		e.gauge("last_successful_resync_timestamp_seconds", seconds(stamp))
	}
	s.RangeResyncs(func(reason, result string, count uint64) bool {
		e.counter("resyncs_total", count, reason, result)
		return true
	})
	s.RangeExceptions(func(selector, status string) bool {
		for _, candidate := range [...]string{"matched", "unmatched", "ambiguous"} {
			e.gauge("max_speed_exception_match", boolean(status == candidate), selector, candidate)
		}
		return true
	})
	e.diagnostics("", s.HostCollector())
	if devices {
		s.RangeDevices(func(d linkmonitor.DeviceView) bool { e.device(s, d); return true })
	}
}

func seconds(t time.Time) float64 { return float64(t.Unix()) + float64(t.Nanosecond())/1e9 }

func (e emitter) device(s linkmonitor.Snapshot, d linkmonitor.DeviceView) {
	name := d.Name()
	for i, status := range [...]string{unknownStatus, "included", "excluded"} {
		e.gauge("interface_classification", boolean(int(d.Eligibility()) == i), name, status)
	}
	if d.Eligibility() != linkmonitor.EligibilityIncluded {
		return
	}
	if up, ok := d.Up(); ok {
		e.gauge("interface_up", boolean(up), name)
	}
	e.gauge("interface_max_speed_exception", boolean(s.MaximumSpeedException(d)), name)
	if bits, ok := s.ExpectedSpeed(d); ok {
		e.gauge("interface_expected_speed_bits_per_second", float64(bits), name)
	}
	checks := [...]linkmonitor.CheckStatus{d.MaximumSpeedCheck(), d.FullDuplexCheck(), d.MaximumWidthCheck()}
	for i, check := range [...]string{"max_speed", "full_duplex", "max_width"} {
		for j, status := range [...]string{unknownStatus, "pass", "fail", "not_applicable"} {
			e.gauge("interface_check", boolean(int(checks[i]) == j), name, check, status)
		}
	}
	if at, ok := d.LastFlap(); ok {
		e.gauge("interface_last_flap_timestamp_seconds", seconds(at), name)
	}
	d.RangeLastErrors(func(class string, at time.Time) bool {
		e.gauge("interface_last_error_timestamp_seconds", seconds(at), name, class)
		return true
	})
	up, down := d.ObservedTransitions()
	e.counter("observed_oper_transitions_total", up, name, "up")
	e.counter("observed_oper_transitions_total", down, name, "down")
	d.RangeCollectors(func(v linkmonitor.CollectorView) bool { e.diagnostics(name, v); return true })
}

func (e emitter) diagnostics(name string, v linkmonitor.CollectorView) {
	collector := v.Name()
	for _, status := range [...]string{"supported", "unsupported", unknownStatus, "not_applicable"} {
		e.gauge("collector_support", boolean(v.Support() == status), name, collector, status)
	}
	if success, ok := v.Success(); ok {
		e.gauge("collector_success", boolean(success), name, collector)
	}
	if stamp, ok := v.LastSuccess(); ok {
		e.gauge("collector_last_success_timestamp_seconds", seconds(stamp), name, collector)
	}
	if duration, ok := v.Duration(); ok {
		e.gauge("collector_duration_seconds", duration.Seconds(), name, collector)
	}
	v.RangeErrors(func(reason string, count uint64) bool {
		e.counter("collector_errors_total", count, name, collector, reason)
		return true
	})
	v.RangeOmissions(func(reason string, count uint64) bool {
		e.gauge("collector_omitted_statistics", float64(count), name, collector, reason)
		return true
	})
	switch collector {
	case "standard", "carrier", "driver", "phy":
		e.counter("counter_discontinuities_total", v.Discontinuities(), name, collector)
	case "rdma_counters":
		e.counter("counter_discontinuities_total", v.Discontinuities(), name, "rdma")
	}
}
