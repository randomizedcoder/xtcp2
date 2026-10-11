// Package prometheus exposes immutable link-monitor snapshots to caller-owned registries.
package prometheus

import (
	"fmt"
	"slices"
	"sync"
	"sync/atomic"

	client "github.com/prometheus/client_golang/prometheus"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
)

type collector struct {
	snapshot func() linkmonitor.Snapshot
	mu       sync.Mutex
	catalog  atomic.Pointer[catalog]
	fixed    map[string]*client.Desc
	sources  map[string]definition
}

type definition struct {
	desc       *client.Desc
	labels     []string
	labelIndex map[string]int
	kind       client.ValueType
}

type catalog struct {
	revision    uint64
	definitions map[string]definition
}

// NewCollector constructs an unchecked collector without I/O or registration.
// The caller owns registry registration, the monitor lifecycle and HTTP serving.
func NewCollector(m *linkmonitor.Monitor) client.Collector {
	c := &collector{fixed: fixedDescriptors(), sources: make(map[string]definition)}
	if m != nil {
		c.snapshot = m.Snapshot
	}
	return c
}

// Describe intentionally emits nothing: future protocol fields are not enumerable.
func (c *collector) Describe(chan<- *client.Desc) {}

// Collect uses exactly one immutable root and never waits for source collection.
func (c *collector) Collect(out chan<- client.Metric) {
	if c.snapshot == nil {
		out <- client.NewInvalidMetric(c.fixed["collection_healthy"], fmt.Errorf("nil link monitor"))
		return
	}
	snapshot := c.snapshot()
	catalog, err := c.descriptors(snapshot)
	if err != nil {
		out <- client.NewInvalidMetric(c.fixed["collection_healthy"], err)
		return
	}
	emit := emitter{out: out, fixed: c.fixed}
	emit.policy(snapshot)
	snapshot.RangeSamples(func(s linkmonitor.SampleView) bool {
		emit.sample(s, catalog)
		return true
	})
}

func (c *collector) descriptors(s linkmonitor.Snapshot) (*catalog, error) {
	if current := c.catalog.Load(); current != nil && current.revision == s.SchemaRevision() {
		return current, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	current := c.catalog.Load()
	if current != nil && current.revision == s.SchemaRevision() {
		return current, nil
	}
	next := &catalog{revision: s.SchemaRevision(), definitions: make(map[string]definition)}
	var failure error
	s.RangeDescriptors(func(d linkmonitor.DescriptorView) bool {
		var labels []string
		d.RangeLabels(func(label string) bool { labels = append(labels, label); return true })
		kind, err := metricType(d.Kind())
		if err != nil {
			failure = err
			return false
		}
		if d.Fixed() {
			if old, ok := c.sources[d.Name()]; ok {
				if old.kind != kind || !slices.Equal(old.labels, labels) {
					failure = fmt.Errorf("fixed metric schema changed: %s", d.Name())
					return false
				}
				next.definitions[d.Key()] = old
				return true
			}
		}
		if current != nil {
			if old, ok := current.definitions[d.Key()]; ok && old.kind == kind && slices.Equal(old.labels, labels) {
				next.definitions[d.Key()] = old
				return true
			}
		}
		next.definitions[d.Key()] = newDefinition(client.NewDesc(d.Name(), "Cached source value for "+d.Name()+".", labels, nil), labels, kind)
		if d.Fixed() {
			if c.sources == nil {
				c.sources = make(map[string]definition)
			}
			c.sources[d.Name()] = next.definitions[d.Key()]
		}
		return true
	})
	if failure != nil {
		return nil, failure
	}
	if current == nil || next.revision > current.revision {
		c.catalog.Store(next)
	}
	return next, nil
}

func newDefinition(desc *client.Desc, labels []string, kind client.ValueType) definition {
	indexes := make(map[string]int, len(labels))
	for i, label := range labels {
		indexes[label] = i
	}
	return definition{desc: desc, labels: labels, labelIndex: indexes, kind: kind}
}

func metricType(kind linkmonitor.SampleKind) (client.ValueType, error) {
	switch kind {
	case linkmonitor.SampleGauge:
		return client.GaugeValue, nil
	case linkmonitor.SampleCounter:
		return client.CounterValue, nil
	case linkmonitor.SampleUntyped:
		return client.UntypedValue, nil
	default:
		return 0, fmt.Errorf("invalid sample kind %d", kind)
	}
}

func number(n linkmonitor.Number) (float64, error) {
	switch n.Kind() {
	case linkmonitor.NumberUnsigned:
		v, _ := n.Uint64()
		return float64(v), nil
	case linkmonitor.NumberSigned:
		v, _ := n.Int64()
		return float64(v), nil
	case linkmonitor.NumberFloat:
		v, _ := n.Float64()
		return v, nil
	default:
		return 0, fmt.Errorf("absent or invalid sample number")
	}
}

type emitter struct {
	out    chan<- client.Metric
	fixed  map[string]*client.Desc
	values []string
	seen   []bool
}

func (e emitter) metric(desc *client.Desc, kind client.ValueType, value float64, labels ...string) {
	m, err := client.NewConstMetric(desc, kind, value, labels...)
	if err != nil {
		m = client.NewInvalidMetric(desc, err)
	}
	e.out <- m
}

func (e *emitter) sample(s linkmonitor.SampleView, catalog *catalog) {
	d, ok := catalog.definitions[s.DescriptorKey()]
	if !ok {
		e.out <- client.NewInvalidMetric(e.fixed["collection_healthy"], fmt.Errorf("missing descriptor %q", s.DescriptorKey()))
		return
	}
	valid := e.bindLabels(d, s.RangeLabels)
	v, err := number(s.Number())
	if err == nil && !valid {
		err = fmt.Errorf("sample label mismatch for %q", s.DescriptorKey())
	}
	if err != nil {
		e.out <- client.NewInvalidMetric(d.desc, err)
		return
	}
	e.metric(d.desc, d.kind, v, e.values...)
}

func (e *emitter) bindLabels(d definition, rangeLabels func(func(string, string) bool)) bool {
	e.values = resizeStrings(e.values, len(d.labels))
	e.seen = resizeBools(e.seen, len(d.labels))
	clear(e.values)
	clear(e.seen)
	seen := 0
	labelError := false
	rangeLabels(func(name, value string) bool {
		i, ok := d.labelIndex[name]
		if !ok || e.seen[i] {
			labelError = true
			return true
		}
		if ok && !e.seen[i] {
			e.values[i] = value
			e.seen[i] = true
			seen++
		}
		return true
	})
	return !labelError && seen == len(e.values)
}

func resizeStrings(values []string, length int) []string {
	if cap(values) < length {
		return make([]string, length)
	}
	return values[:length]
}

func resizeBools(values []bool, length int) []bool {
	if cap(values) < length {
		return make([]bool, length)
	}
	return values[:length]
}
