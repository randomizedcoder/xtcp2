package prometheus

import (
	"fmt"
	"time"

	client "github.com/prometheus/client_golang/prometheus"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
)

// summaryCollector serves the sparse problem summary. It never walks samples,
// descriptors or devices: one snapshot, one clock read and the candidate list.
type summaryCollector struct {
	snapshot func() linkmonitor.Snapshot
	now      func() time.Time
	scalars  bool
	problems *client.Desc
	iface    *client.Desc
	host     *client.Desc
	delta    *client.Desc
	ready    *client.Desc
	healthy  *client.Desc
}

// SummaryOption configures a summary collector at construction.
type SummaryOption func(*summaryCollector)

// WithClock sets the scrape clock used to evaluate error and flap windows.
// The default is time.Now; tests inject a fixed clock.
func WithClock(now func() time.Time) SummaryOption {
	return func(c *summaryCollector) { c.now = now }
}

// WithoutScalars emits only problems and the sparse problem series, for a
// registry that already has an unfiltered collector serving the scalars.
func WithoutScalars() SummaryOption {
	return func(c *summaryCollector) { c.scalars = false }
}

// NewSummaryCollector constructs the alerting summary: problems,
// up_links_delta, baseline_ready, collection_healthy and one
// interface_problem or host_problem series per active reason. A healthy host
// emits only the scalars, however many interfaces it has.
func NewSummaryCollector(m *linkmonitor.Monitor, opts ...SummaryOption) client.Collector {
	desc := func(name, help string, labels ...string) *client.Desc {
		return client.NewDesc(metricPrefix+name, help, labels, nil)
	}
	c := &summaryCollector{
		now: time.Now, scalars: true,
		problems: desc("problems", "Number of interface_problem and host_problem series in this scrape."),
		iface:    desc("interface_problem", "Active problem reason for an included interface; absent when healthy.", interfaceLabel, reasonLabel),
		host:     desc("host_problem", "Active host-scope problem reason; absent when healthy.", reasonLabel),
		delta:    desc("up_links_delta", "Link monitor up_links_delta."),
		ready:    desc("baseline_ready", "Link monitor baseline_ready."),
		healthy:  desc("collection_healthy", "Link monitor collection_healthy."),
	}
	if m != nil {
		c.snapshot = m.Snapshot
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Describe intentionally emits nothing, like the general collector, so both
// can share a registry without descriptor conflicts.
func (c *summaryCollector) Describe(chan<- *client.Desc) {}

func (c *summaryCollector) Collect(out chan<- client.Metric) {
	if c.snapshot == nil {
		out <- client.NewInvalidMetric(invalidDesc, fmt.Errorf("nil link monitor"))
		return
	}
	s, now := c.snapshot(), c.now()
	count := 0
	s.RangeProblems(now, func(name string, p linkmonitor.Problems) bool {
		for reason, rest, ok := p.Next(); ok; reason, rest, ok = rest.Next() {
			out <- client.MustNewConstMetric(c.iface, client.GaugeValue, 1, name, reason)
			count++
		}
		return true
	})
	host := s.HostProblems(now)
	for reason, rest, ok := host.Next(); ok; reason, rest, ok = rest.Next() {
		out <- client.MustNewConstMetric(c.host, client.GaugeValue, 1, reason)
		count++
	}
	out <- client.MustNewConstMetric(c.problems, client.GaugeValue, float64(count))
	if !c.scalars {
		return
	}
	if negative, magnitude, ok := s.Counts().Delta(); ok {
		value := float64(magnitude)
		if negative {
			value = -value
		}
		out <- client.MustNewConstMetric(c.delta, client.GaugeValue, value)
	}
	health := s.Health()
	out <- client.MustNewConstMetric(c.ready, client.GaugeValue, boolean(health.BaselineReady))
	out <- client.MustNewConstMetric(c.healthy, client.GaugeValue, boolean(health.CollectionHealthy))
}
