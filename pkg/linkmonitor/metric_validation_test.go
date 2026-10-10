package linkmonitor

import (
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

func validationMetric(name string) model.Sample {
	return model.Sample{Descriptor: name, Kind: model.SampleGauge, Number: model.Unsigned(1)}
}

func requireValidationSample(t testing.TB, f *PrometheusFixture, index uint32, kind model.CollectorKind, sample model.Sample) {
	t.Helper()
	if err := f.Samples(index, kind, sample); err != nil {
		t.Fatal(err)
	}
}

func TestCollectionValidationAdmission(t *testing.T) {
	host := validationMetric("metric")
	host.Labels = []model.Label{{Name: interfaceLabel, Value: "eth1"}}
	renamedHost := host
	renamedHost.Labels = []model.Label{{Name: interfaceLabel, Value: "renamed"}}
	unscoped := validationMetric("metric")
	unscoped.NoInterfaceLabel = true
	wrongKind := validationMetric("metric")
	wrongKind.Kind = model.SampleUntyped
	cases := []struct {
		name, category, description, expected string
		change                                func(*PrometheusFixture)
		index                                 uint32
		kind                                  model.CollectorKind
		sample                                model.Sample
		wantError                             bool
	}{
		{"distinct device", "positive", "same family on two interfaces", "accept distinct final series", nil, 2, model.CollectorPHY, validationMetric("metric"), false},
		{"duplicate collector", "negative", "two collectors publish one interface series", "reject duplicate", nil, 1, model.CollectorPHY, validationMetric("metric"), true},
		{"kind conflict", "negative", "distinct interfaces disagree on family type", "reject descriptor conflict", nil, 2, model.CollectorPHY, wrongKind, true},
		{"host collision", "negative", "host labels match a scoped device series", "reject duplicate", nil, 0, model.CollectorNetstat, host, true},
		{"rename releases old", "corner", "cached device changes name before host admission", "accept old interface label", func(f *PrometheusFixture) { f.Device(1, "renamed", true) }, 0, model.CollectorNetstat, host, false},
		{"rename collision", "corner", "cached device changes name to incoming host label", "reject new final duplicate", func(f *PrometheusFixture) { f.Device(1, "renamed", true) }, 0, model.CollectorNetstat, renamedHost, true},
		{"removal", "boundary", "last holder of series removed", "accept formerly occupied series", func(f *PrometheusFixture) { f.Remove(1) }, 0, model.CollectorNetstat, host, false},
		{"replacement", "positive", "collector replaces its schema", "old series is available", func(f *PrometheusFixture) {
			requireValidationSample(f.t, f, 1, model.CollectorDriver, validationMetric("replacement"))
		}, 1, model.CollectorPHY, validationMetric("metric"), false},
		{"unscoped", "negative", "two devices suppress the interface label", "reject cross-device duplicate", func(f *PrometheusFixture) { requireValidationSample(f.t, f, 1, model.CollectorDriver, unscoped) }, 2, model.CollectorPHY, unscoped, true},
	}
	for i := range cases {
		tc := &cases[i]
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			f := NewPrometheusFixture(t)
			f.Device(1, "eth1", true)
			f.Device(2, "eth2", true)
			requireValidationSample(t, f, 1, model.CollectorDriver, validationMetric("metric"))
			if tc.change != nil {
				tc.change(f)
			}
			if err := f.Samples(tc.index, tc.kind, tc.sample); (err != nil) != tc.wantError {
				t.Fatalf("%s: error=%v", tc.expected, err)
			}
		})
	}
}

func TestCollectionValidationLifetime(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expected string
		count                                 int
	}{
		{"empty", "boundary", "empty supported schema", "cache empty projection and release on unsupported", 0},
		{"one", "positive", "one metric with numeric refresh", "reuse projection and release on unsupported", 1},
		{"maximum", "boundary", "65,536 metrics", "cache every series and release on unsupported", maximumSamples},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			f := NewPrometheusFixture(t)
			f.Device(1, "eth1", true)
			samples := PerformanceSamples(tc.count)
			if err := f.Samples(1, model.CollectorDriver, samples...); err != nil {
				t.Fatal(err)
			}
			state := &slotAt(f.r, 1).collectors[model.CollectorDriver]
			cached := state.validation
			if cached == nil || validationSize(cached) != tc.count {
				t.Fatal("missing validation metadata")
			}
			if len(samples) != 0 {
				samples[0].Number = model.Unsigned(2)
			}
			if err := f.Samples(1, model.CollectorDriver, samples...); err != nil || state.validation != cached {
				t.Fatal("numeric refresh rebuilt validation", err)
			}
			job, err := f.r.startCollection(state.job.Key, model.Stamp{})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.r.finishCollection(model.Result{Job: job, Support: model.Unsupported}); err != nil || state.validation != nil {
				t.Fatal("unsupported source retained validation", err)
			}
		})
	}
}

func validationSize(v *collectionValidation) int {
	count := 0
	for _, keys := range v.series {
		count += len(keys)
	}
	return count
}

func TestCollectionValidationSharedGroup(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expected, value string
		wantError                                    bool
	}{
		{"distinct", "positive", "one family and interface with distinct queue labels", "accept without modifying previous cache", "two", false},
		{"duplicate", "negative", "same queue label in another collector", "reject without modifying previous cache", "one", true},
		{"empty", "boundary", "empty queue label is distinct from one", "accept empty label and retain it", "", false},
		{"nul", "corner", "label contains a NUL byte", "retain exact length-prefixed identity", "one\x00two", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			f := NewPrometheusFixture(t)
			f.Device(1, "eth1", true)
			first := validationMetric("metric")
			first.Labels = []model.Label{{Name: "queue", Value: "one"}}
			requireValidationSample(t, f, 1, model.CollectorDriver, first)
			cached := slotAt(f.r, 1).collectors[model.CollectorDriver].validation
			next := validationMetric("metric")
			next.Labels = []model.Label{{Name: "queue", Value: tc.value}}
			if err := f.Samples(1, model.CollectorPHY, next); (err != nil) != tc.wantError {
				t.Fatal(tc.expected, err)
			}
			if validationSize(cached) != 1 {
				t.Fatal("merging mutated the retained collector cache")
			}
			if err := f.Samples(1, model.CollectorChannels, next); err == nil {
				t.Fatal("third collector duplicated an already admitted series")
			}
			if slotAt(f.r, 1).collectors[model.CollectorChannels].validation != nil {
				t.Fatal("rejected source retained proposed validation metadata")
			}
		})
	}
}
