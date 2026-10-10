package linkmonitor

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

func TestPrometheusDiagnosticAttempts(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		reason                                       model.ErrorReason
		support                                      model.Support
		want                                         uint64
	}{
		{"permission", "negative", "source permission failure", "one permission error", model.ErrorPermission, model.SupportUnknown, 1},
		{"timeout", "negative", "logical timeout and late reply", "one timeout error", model.ErrorTimeout, model.SupportUnknown, 1},
		{"unspecified", "corner", "failure without reason", "one IO error", model.ErrorNone, model.SupportUnknown, 1},
		{"unsupported", "corner", "unsupported event provider", "support status without error count", model.ErrorIO, model.Unsupported, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			r := newReducer(1)
			job, err := r.startCollection(model.JobKey{Namespace: 1, Collector: model.CollectorNetstat}, model.Stamp{})
			if err != nil {
				t.Fatal(err)
			}
			result := model.Result{Job: job, Err: errors.New("test failure"), Reason: tc.reason, Support: tc.support}
			if accepted, err := r.finishCollection(result); !accepted || err != nil {
				t.Fatalf("finish %v %v", accepted, err)
			}
			if accepted, err := r.finishCollection(result); accepted || err != nil {
				t.Fatalf("duplicate %v %v", accepted, err)
			}
			view := CollectorView{kind: model.CollectorNetstat, state: freezeCollector(&r.host)}
			var count uint64
			view.RangeErrors(func(_ string, value uint64) bool { count += value; return true })
			if count != tc.want {
				t.Fatalf("count=%d want=%d", count, tc.want)
			}
		})
	}
}

func TestPrometheusSchemaAdmission(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		samples                                      []model.Sample
		wantError                                    bool
	}{
		{"valid", "positive", "valid exact counter", "accepted", []model.Sample{counter("go_link_monitor_test", 0, 64)}, false},
		{"name", "negative", "invalid metric identifier", "rejected", []model.Sample{counter("bad-name", 0, 64)}, true},
		{"label", "negative", "reserved label name", "rejected before publication", []model.Sample{{Descriptor: "metric", Kind: model.SampleGauge, Number: model.Unsigned(0), Labels: []model.Label{{Name: "__name__", Value: "override"}}}}, true},
		{"utf8", "negative", "invalid UTF-8 label value", "rejected without lossy sanitizing", []model.Sample{{Descriptor: "metric", Kind: model.SampleGauge, Number: model.Unsigned(0), Labels: []model.Label{{Name: "value", Value: string([]byte{0xff})}}}}, true},
		{"duplicate", "negative", "two equal final series", "rejected", []model.Sample{counter("same", 1, 64), counter("same", 2, 64)}, true},
		{"reserved", "negative", "source impersonates baseline", "rejected", []model.Sample{counter("go_link_monitor_baseline_up_links", 0, 64)}, true},
		{"alias", "negative", "two keys resolve to one family", "rejected duplicate", []model.Sample{counter("rdma_port_up", 1, 64), counter("go_link_monitor_rdma_port_up", 1, 64)}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			f := NewPrometheusFixture(t)
			err := f.Samples(0, model.CollectorNetstat, tc.samples...)
			if (err != nil) != tc.wantError {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestPrometheusSchemaRevision(t *testing.T) {
	t.Log("corner: values change, schema replaced, old snapshot retained; expected: stable revision for values and new revision for descriptors")
	f := NewPrometheusFixture(t)
	sample := counter("metric", 1, 64)
	if err := f.Samples(0, model.CollectorNetstat, sample); err != nil {
		t.Fatal(err)
	}
	f.Publish(0)
	old := f.Monitor.Snapshot()
	sample.Number = model.Unsigned(2)
	if err := f.Samples(0, model.CollectorNetstat, sample); err != nil {
		t.Fatal(err)
	}
	f.Publish(0)
	if f.Monitor.Snapshot().SchemaRevision() != old.SchemaRevision() {
		t.Fatal("value change rebuilt descriptors")
	}
	sample.Descriptor = "replacement"
	if err := f.Samples(0, model.CollectorNetstat, sample); err != nil {
		t.Fatal(err)
	}
	f.Publish(0)
	if f.Monitor.Snapshot().SchemaRevision() <= old.SchemaRevision() {
		t.Fatal("schema revision did not advance")
	}
	old.RangeDescriptors(func(d DescriptorView) bool {
		if d.Key() != "metric" {
			t.Fatal("retained catalog changed")
		}
		return true
	})
}

func TestPrometheusOmissionCounts(t *testing.T) {
	t.Log("boundary: known filtered count, failure then expiry; expected: filtered count retained, present samples counted stale")
	r := newReducer(1)
	key := model.JobKey{Namespace: 1, Collector: model.CollectorNetstat}
	job, err := r.startCollection(key, model.Stamp{})
	if err != nil {
		t.Fatal(err)
	}
	result := model.Result{Job: job, Support: model.Supported, Filtered: presentValue(uint64(2)), Samples: []model.Sample{counter("kept", 0, 64)}}
	if _, err := r.finishCollection(result); err != nil {
		t.Fatal(err)
	}
	job, err = r.startCollection(key, model.Stamp{Monotonic: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.finishCollection(model.Result{Job: job, Finished: model.Stamp{Monotonic: time.Second}, Err: errors.New("read failure"), Reason: model.ErrorIO}); err != nil {
		t.Fatal(err)
	}
	r.expire(45 * time.Second)
	view := CollectorView{kind: model.CollectorNetstat, state: freezeCollector(&r.host)}
	counts := make(map[string]uint64)
	view.RangeOmissions(func(reason string, count uint64) bool { counts[reason] = count; return true })
	if counts["filtered"] != 2 || counts["stale"] != 1 {
		t.Fatalf("omissions %v", counts)
	}
	if saturatingIncrement(math.MaxUint64) != math.MaxUint64 {
		t.Fatal("diagnostic counter wrapped")
	}
}

func TestPrometheusCarrierOmissions(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		at                                           time.Duration
		want                                         uint64
	}{
		{"fresh", "positive", "present zero carrier count", "known zero omitted", 44 * time.Second, 0},
		{"expired", "boundary", "carrier field reaches expiry", "one stale field", 45 * time.Second, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			r := newReducer(1)
			mustObserve(t, r, observed(r, 1, true))
			key := slotAt(r, 1).device.Key
			if err := r.carrierEvent(key, model.CarrierValues{1: presentValue(uint64(0))}, 1, model.Stamp{}); err != nil {
				t.Fatal(err)
			}
			r.expire(tc.at)
			state := &slotAt(r, 1).collectors[model.CollectorCarrier]
			if !state.stale.Present || state.stale.Value != tc.want {
				t.Fatalf("stale=%v", state.stale)
			}
		})
	}
}

func TestPrometheusCrossSourceSchemas(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		next                                         model.Sample
		wantError                                    bool
	}{
		{"duplicate", "negative", "two collectors supply same series", "reject later source", counter("family", 2, 64), true},
		{"type", "negative", "same family with conflicting types", "reject later source", model.Sample{Descriptor: "family", Kind: model.SampleGauge, Number: model.Unsigned(1), Labels: []model.Label{{Name: "source", Value: "other"}}}, true},
		{"labels", "negative", "same family with different label names", "reject later source", model.Sample{Descriptor: "family", Kind: model.SampleCounter, Number: model.Unsigned(1)}, true},
		{"unique", "positive", "matching family with different label value", "accept distinct series", model.Sample{Descriptor: "family", Kind: model.SampleCounter, Number: model.Unsigned(1), Labels: []model.Label{{Name: "source", Value: "other"}}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			f := NewPrometheusFixture(t)
			f.Device(1, "eth0", true)
			first := counter("family", 1, 64)
			first.Labels = []model.Label{{Name: "source", Value: "first"}}
			if tc.name == "duplicate" {
				tc.next.Labels = first.Labels
			}
			if err := f.Samples(1, model.CollectorDriver, first); err != nil {
				t.Fatal(err)
			}
			err := f.Samples(1, model.CollectorPHY, tc.next)
			if (err != nil) != tc.wantError {
				t.Fatalf("error=%v", err)
			}
		})
	}
}
