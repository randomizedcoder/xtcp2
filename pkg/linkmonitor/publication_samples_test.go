package linkmonitor

import (
	"strconv"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

func TestPublicationSamplePresenceAndScope(t *testing.T) {
	t.Log("positive/negative: host sentinel, absent and zero device samples; expected: exact present values, device labels only on device samples")
	r, m := newReducer(1), new(Monitor)
	mustObserve(t, r, observed(r, 1, true))
	missing := counter("absent", 0, 64)
	missing.Number = model.Number{}
	zero := counter("present_zero", 0, 64)
	zero.Labels = []model.Label{{Name: "source", Value: "driver"}, {Name: "interface", Value: "obsolete"}}
	collect(t, r, model.CollectorDriver, missing, zero)
	job, err := r.startCollection(model.JobKey{Namespace: r.namespace, Collector: model.CollectorNetstat}, model.Stamp{})
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := r.finishCollection(model.Result{Job: job, Support: model.Supported, Samples: []model.Sample{{
		Descriptor: "Tcp_MaxConn", Kind: model.SampleGauge, Number: model.Signed(-1),
	}}}); !ok || err != nil {
		t.Fatalf("host completion=%v,%v", ok, err)
	}
	s := publish(t, r, m)
	seen := 0
	s.RangeSamples(func(v SampleView) bool {
		seen++
		labels := make(map[string]string)
		v.RangeLabels(func(name, value string) bool {
			if _, duplicate := labels[name]; duplicate {
				t.Fatal("interface label duplicated")
			}
			labels[name] = value
			return true
		})
		switch v.DescriptorKey() {
		case "Tcp_MaxConn":
			if value, ok := v.Number().Int64(); !ok || value != -1 || len(labels) != 0 {
				t.Fatal("host sample was relabeled or changed signedness")
			}
		case "present_zero":
			if value, ok := v.Number().Uint64(); !ok || value != 0 || labels["interface"] != "eth1" || labels["source"] != "driver" || len(labels) != 2 {
				t.Fatal("present zero or device label changed")
			}
		default:
			t.Fatal("absent sample emitted as a value")
		}
		return true
	})
	if seen != 2 {
		t.Fatalf("saw %d samples, want host and present device value", seen)
	}
	host := s.root.host
	collect(t, r, model.CollectorDriver, counter("new", 1, 64))
	next := publish(t, r, m)
	if next.root.host != host || next.root.pages[0] == s.root.pages[0] {
		t.Fatal("device collection copied host block or failed to update its page")
	}
	visits := 0
	next.RangeSamples(func(SampleView) bool { visits++; return false })
	if visits != 1 {
		t.Fatal("false did not stop across host/device boundary")
	}
}

func TestPublicationNativeRDMAAndUnknownCounts(t *testing.T) {
	t.Log("corner: native port with an IPoIB alias and unknown eligibility; expected: canonical port identity, unknown aggregate and immutable earlier count")
	r, m := newReducer(77), new(Monitor)
	o := model.Observation{Device: model.Device{
		Key:   model.DeviceKey{Namespace: 77, Kind: model.DeviceNativeRDMA, RDMADevice: "mlx5_0", Port: 2},
		Token: model.Token{SourceEpoch: r.epoch}, Name: "ib0", Eligibility: model.Eligible, Up: presentValue(true),
	}}
	mustObserve(t, r, o)
	r.slots[0].checks.fullDuplex = model.CheckNotApplicable
	before := publish(t, r, m)
	before.RangeDevices(func(v DeviceView) bool {
		if v.Identity() != "rdma:mlx5_0:2" || v.Name() != v.Identity() || v.FullDuplexCheck() != CheckNotApplicable {
			t.Fatal("native RDMA port identity/duplex became its IPoIB alias")
		}
		return true
	})
	o.Device.Eligibility = model.EligibilityUnknown
	mustObserve(t, r, o)
	after := publish(t, r, m)
	if value, known := after.Counts().Current(); value != 1 || known {
		t.Fatal("unknown classification removed previous contribution or reported a healthy count")
	}
	if _, _, known := after.Counts().Delta(); known {
		t.Fatal("uncertain inventory produced an authoritative delta")
	}
	if value, known := before.Counts().Current(); value != 1 || !known {
		t.Fatal("later classification changed retained count")
	}
}

func TestPublicationAllocationIndependence(t *testing.T) {
	t.Log("boundary/performance contract: 1 versus 65,536 statistics; expected: same event allocation count, no allocations during read-only sample iteration")
	var small float64
	for _, count := range []int{1, maximumSamples} {
		r, m := newReducer(1), new(Monitor)
		mustObserve(t, r, observed(r, 1, true))
		samples := make([]model.Sample, count)
		for i := range samples {
			samples[i] = counter("stat_"+strconv.Itoa(i), uint64(i), 64)
		}
		collect(t, r, model.CollectorDriver, samples...)
		snapshot := publish(t, r, m)
		allocations := testing.AllocsPerRun(20, func() {
			o := observed(r, 1, !slotAt(r, 1).device.Up.Value)
			if _, err := r.observe(o); err != nil {
				t.Fatal(err)
			}
			if err := r.publish(m, publicationState{}); err != nil {
				t.Fatal(err)
			}
		})
		t.Logf("statistics=%d event allocations=%g", count, allocations)
		if count == 1 {
			small = allocations
		} else if allocations != small {
			t.Fatalf("event allocations grew with statistics: small=%g large=%g", small, allocations)
		}
		visits := 0
		if allocations := testing.AllocsPerRun(5, func() {
			snapshot.RangeSamples(func(SampleView) bool { visits++; return true })
		}); allocations != 0 || visits == 0 {
			t.Fatalf("sample iteration allocations=%g visits=%d", allocations, visits)
		}
	}
}
