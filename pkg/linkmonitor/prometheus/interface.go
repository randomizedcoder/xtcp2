package prometheus

import (
	"fmt"

	client "github.com/prometheus/client_golang/prometheus"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
)

// ErrUnknownInterface reports an interface name absent from the snapshot. It
// is linkmonitor.ErrUnknownInterface, which Monitor.Refresh also returns.
var ErrUnknownInterface = linkmonitor.ErrUnknownInterface

// deviceCollector is implemented by the collectors InterfaceCollector accepts.
type deviceCollector interface {
	collectDevice(out chan<- client.Metric, s linkmonitor.Snapshot, d linkmonitor.DeviceView)
}

type interfaceCollector struct {
	snapshot linkmonitor.Snapshot
	device   linkmonitor.DeviceView
	parts    []deviceCollector
}

// InterfaceCollector returns a request-scoped collector serving one interface
// of snapshot s through collectors built by NewCollector or
// NewSummaryCollector, each with its own filter and cached catalog. It emits
// the series of that device, which include RDMA counters attributed to it,
// plus collection_healthy when selected; host-scope families are omitted.
// Only that device's samples are read. A name absent from s returns
// ErrUnknownInterface.
func InterfaceCollector(s linkmonitor.Snapshot, name string, collectors ...client.Collector) (client.Collector, error) {
	parts := make([]deviceCollector, len(collectors))
	for i, c := range collectors {
		part, ok := c.(deviceCollector)
		if !ok {
			return nil, fmt.Errorf("collector %T has no interface view", c)
		}
		parts[i] = part
	}
	d, ok := s.Device(name)
	if !ok {
		return nil, ErrUnknownInterface
	}
	return &interfaceCollector{snapshot: s, device: d, parts: parts}, nil
}

// Describe intentionally emits nothing, like the collectors it wraps.
func (c *interfaceCollector) Describe(chan<- *client.Desc) {}

func (c *interfaceCollector) Collect(out chan<- client.Metric) {
	for _, part := range c.parts {
		part.collectDevice(out, c.snapshot, c.device)
	}
}

func (c *collector) collectDevice(out chan<- client.Metric, s linkmonitor.Snapshot, d linkmonitor.DeviceView) {
	catalog, err := c.descriptors(s)
	if err != nil {
		out <- client.NewInvalidMetric(invalidDesc, err)
		return
	}
	emit := emitter{out: out, fixed: c.fixed}
	emit.gauge("collection_healthy", boolean(s.Health().CollectionHealthy))
	if c.devices {
		emit.device(s, d)
	}
	var skip func(linkmonitor.SchemaID) bool
	if len(catalog.skip) != 0 {
		skip = func(id linkmonitor.SchemaID) bool { return catalog.skip[id] }
	}
	d.RangeSamplesSkipping(skip, func(sample linkmonitor.SampleView) bool {
		emit.sample(sample, catalog)
		return true
	})
}

// collectDevice emits only the device's active interface_problem series; the
// problems count and the scalars are host scope.
func (c *summaryCollector) collectDevice(out chan<- client.Metric, s linkmonitor.Snapshot, d linkmonitor.DeviceView) {
	p := s.DeviceProblems(d, c.now())
	for reason, rest, ok := p.Next(); ok; reason, rest, ok = rest.Next() {
		out <- client.MustNewConstMetric(c.iface, client.GaugeValue, 1, d.Name(), reason)
	}
}
