package linkmonitor

import (
	"math"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

func TestExactNumbers(t *testing.T) {
	tests := []struct {
		name, category, description, expected string
		number                                Number
		kind                                  NumberKind
	}{
		{"absent", "boundary", "zero value is missing", "no typed accessor succeeds", Number{}, NumberAbsent},
		{"unsigned zero", "boundary", "observed counter zero", "unsigned present zero", Number{value: model.Unsigned(0)}, NumberUnsigned},
		{"large counter", "boundary", "counter exceeds exact float64 range", "all uint64 bits retained", Number{value: model.Unsigned(math.MaxUint64)}, NumberUnsigned},
		{"signed sentinel", "negative", "Tcp_MaxConn=-1", "signed minus one", Number{value: model.Signed(-1)}, NumberSigned},
		{"fraction", "positive", "derived fractional value", "floating point representation", Number{value: model.Float(0.125)}, NumberFloat},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			if tc.number.Kind() != tc.kind {
				t.Fatal("wrong representation")
			}
			u, unsigned := tc.number.Uint64()
			i, signed := tc.number.Int64()
			f, floating := tc.number.Float64()
			if unsigned != (tc.kind == NumberUnsigned) || signed != (tc.kind == NumberSigned) || floating != (tc.kind == NumberFloat) {
				t.Fatal("accessor changed signedness or presence")
			}
			if (unsigned && tc.name == "large counter" && u != math.MaxUint64) || (signed && i != -1) || (floating && f != 0.125) {
				t.Fatal("value changed")
			}
		})
	}
}

func TestSnapshotViews(t *testing.T) {
	device := DeviceView{identity: "rdma:mlx5_0:1", name: "rdma:mlx5_0:1", generation: 42, up: true, upKnown: true,
		eligibility: model.Eligible, maximumSpeed: model.CheckFail, maximumWidth: model.CheckPass,
		fullDuplex: model.CheckNotApplicable, rdmaReadiness: model.CheckUnknown}
	block, err := freezeSamples(nil, []model.Sample{{Descriptor: "carrier_down_changes_total", Kind: model.SampleCounter,
		Number: model.Unsigned(math.MaxUint64), Labels: []model.Label{{Name: "interface", Value: "eno1"}, {Name: "source", Value: "kernel"}}}})
	if err != nil {
		t.Fatal(err)
	}
	s := Snapshot{root: &snapshotRoot{version: 9, health: Health{Running: true},
		pages: []*devicePage{{&deviceBlock{view: device}, &deviceBlock{view: device}}}, host: &collectorSnapshot{block: block}}}
	if s.Version() != 9 || !s.Health().Running {
		t.Fatal("snapshot metadata mismatch")
	}
	devices, samples, labels := 0, 0, 0
	s.RangeDevices(func(d DeviceView) bool {
		devices++
		up, known := d.Up()
		if d.Identity() != "rdma:mlx5_0:1" || d.Name() != d.Identity() || d.Generation() != 42 || !up || !known {
			t.Fatal("device view mismatch")
		}
		if d.Eligibility() != EligibilityIncluded || d.MaximumSpeedCheck() != CheckFail || d.MaximumWidthCheck() != CheckPass || d.FullDuplexCheck() != CheckNotApplicable || d.RDMAReadinessCheck() != CheckUnknown {
			t.Fatal("device view lost policy or source-validity distinctions")
		}
		return false
	})
	s.RangeSamples(func(v SampleView) bool {
		samples++
		if n, ok := v.Number().Uint64(); !ok || n != math.MaxUint64 || v.Kind() != SampleCounter || v.DescriptorKey() != "carrier_down_changes_total" {
			t.Fatal("sample view mismatch")
		}
		v.RangeLabels(func(key, value string) bool {
			labels++
			if key != "interface" || value != "eno1" {
				t.Fatal("label mismatch")
			}
			return false
		})
		return false
	})
	if devices != 1 || samples != 1 || labels != 1 {
		t.Fatal("iteration did not stop at false")
	}
	var empty Snapshot
	empty.RangeDevices(func(DeviceView) bool { t.Fatal("empty snapshot has device"); return true })
	empty.RangeSamples(func(SampleView) bool { t.Fatal("empty snapshot has sample"); return true })
	if empty.Version() != 0 || empty.Health() != (Health{}) {
		t.Fatal("invalid zero snapshot")
	}
}
