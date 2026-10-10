package linkmonitor

import (
	"errors"
	"math"
	"strconv"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

func counter(name string, value uint64, width uint8) model.Sample {
	return model.Sample{Descriptor: name, Kind: model.SampleCounter, Number: model.Unsigned(value), Counter: model.CounterIdentity{Source: "kernel", Width: width}}
}

func begin(t *testing.T, r *reducer, kind model.CollectorKind) model.Job {
	t.Helper()
	job, err := r.startCollection(model.JobKey{Namespace: r.namespace, Device: slotAt(r, 1).device.Key, Collector: kind}, model.Stamp{})
	if err != nil {
		t.Fatal(err)
	}
	return job
}

func collect(t *testing.T, r *reducer, kind model.CollectorKind, samples ...model.Sample) *collectorState {
	t.Helper()
	job := begin(t, r, kind)
	if ok, err := r.finishCollection(model.Result{Job: job, Support: model.Supported, Samples: samples}); !ok || err != nil {
		t.Fatalf("result=%v,%v", ok, err)
	}
	return &slotAt(r, 1).collectors[kind]
}

func TestReducerCounterContinuity(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expected string
		before, after                         uint64
		oldWidth, newWidth                    uint8
		sourceChange, lifetimeChange          bool
		want                                  uint64
	}{
		{"short flaps", "positive", "counter 8 to 10 while both polls see up", "raw increase, no invented observed transitions", 8, 10, 64, 64, false, false, 0},
		{"32-bit wrap ambiguous", "boundary", "32-bit maximum then zero", "one discontinuity, no guessed wrap extension", math.MaxUint32, 0, 32, 32, false, false, 1},
		{"64-bit precision", "boundary", "values beyond float64 exact range", "exact comparison without rounding", 1 << 53, 1<<53 + 1, 64, 64, false, false, 0},
		{"maximum", "boundary", "uint64 maximum then lower", "one discontinuity", math.MaxUint64, math.MaxUint64 - 1, 64, 64, false, false, 1},
		{"source", "corner", "counter source replaced without decrease", "one discontinuity", 8, 9, 64, 64, true, false, 1},
		{"width", "corner", "same value from different width", "one discontinuity", 8, 8, 32, 64, false, false, 1},
		{"lifetime", "corner", "source reset lifetime changes", "one discontinuity", 8, 9, 64, 64, false, true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			r := newReducer(1)
			mustObserve(t, r, observed(r, 1, true))
			state := collect(t, r, model.CollectorCarrier, counter("down", tc.before, tc.oldWidth))
			if state.discontinuities != 0 {
				t.Fatal("first observation invented history")
			}
			next := counter("down", tc.after, tc.newWidth)
			if tc.sourceChange {
				next.Counter.Source = "other"
			}
			if tc.lifetimeChange {
				next.Counter.Lifetime = 1
			}
			state = collect(t, r, model.CollectorCarrier, next)
			value, present := state.block.values[0].Uint64()
			if !present || value != tc.after || state.discontinuities != tc.want {
				t.Fatalf("value=%d discontinuities=%d", value, state.discontinuities)
			}
			if slotAt(r, 1).upTransitions != 0 || slotAt(r, 1).downTransitions != 0 {
				t.Fatal("source counter was added to observed transitions")
			}
		})
	}
}

func TestReducerCounterSets(t *testing.T) {
	r := newReducer(1)
	mustObserve(t, r, observed(r, 1, true))
	state := collect(t, r, model.CollectorDriver, counter("a", 8, 64), counter("b", 9, 64))
	collect(t, r, model.CollectorDriver, counter("b", 2, 64), counter("a", 1, 64))
	if state.discontinuities != 1 {
		t.Fatal("multiple decreases in one set must count once")
	}
	missing := counter("a", 0, 64)
	missing.Number = model.Number{}
	collect(t, r, model.CollectorDriver, missing)
	if len(state.history) != 1 {
		t.Fatal("removed schema key was retained")
	}
	collect(t, r, model.CollectorDriver, counter("a", 0, 64))
	if state.discontinuities != 2 {
		t.Fatal("temporary absence erased comparison history")
	}
	carrier := collect(t, r, model.CollectorCarrier, counter("carrier_a", 100, 64))
	if carrier.discontinuities != 0 || state.discontinuities != 2 {
		t.Fatal("overlapping collectors shared histories")
	}
}

func TestReducerAttemptIsolation(t *testing.T) {
	for _, tc := range []struct {
		name, description, expected string
		change                      func(*testing.T, *reducer)
	}{
		{"rename", "metadata changes after request started", "ignore old revision", func(t *testing.T, r *reducer) {
			o := observed(r, 1, true)
			o.Device.Name = "renamed"
			mustObserve(t, r, o)
		}},
		{"loss", "source loss after request started", "ignore old epoch", func(t *testing.T, r *reducer) {
			if err := r.loseEvents(); err != nil {
				t.Fatal(err)
			}
		}},
		{"reuse", "remove and recreate after request started", "ignore old generation", func(t *testing.T, r *reducer) {
			o := observed(r, 1, true)
			if _, err := r.remove(o.Device.Key, model.Token{SourceEpoch: r.epoch}); err != nil {
				t.Fatal(err)
			}
			mustObserve(t, r, o)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("corner: %s; expected: %s", tc.description, tc.expected)
			r := newReducer(1)
			mustObserve(t, r, observed(r, 1, true))
			job := begin(t, r, model.CollectorCarrier)
			if _, err := r.startCollection(job.Key, model.Stamp{}); !errors.Is(err, errCollectionBusy) {
				t.Fatalf("overlap=%v", err)
			}
			tc.change(t, r)
			if accepted, err := r.finishCollection(model.Result{Job: job, Support: model.Supported, Samples: []model.Sample{counter("old", 99, 64)}}); err != nil || accepted {
				t.Fatal("stale result accepted")
			}
			state := collect(t, r, model.CollectorCarrier, counter("current", 1, 64))
			if state.block.schema.entries[0].key.descriptor != "current" {
				t.Fatal("late result replaced current data")
			}
		})
	}
}

func TestReducerResultAtomicity(t *testing.T) {
	for _, tc := range []struct {
		name, description, expected string
		invalid                     []model.Sample
	}{
		{"duplicate", "duplicate descriptor/labels", "reject whole set", []model.Sample{counter("a", 0, 64), counter("a", 0, 64)}},
		{"width", "source width over 64", "retain previous values", []model.Sample{counter("a", 0, 65)}},
		{"absent width", "absent counter with invalid schema width", "reject invalid metadata despite missing value", []model.Sample{{Descriptor: "a", Kind: model.SampleCounter, Counter: model.CounterIdentity{Width: 65}}}},
		{"overflow", "32-bit source carries 2^32", "reject rather than truncate", []model.Sample{counter("a", 1<<32, 32)}},
		{"signed counter", "negative value tagged counter", "reject instead of reinterpreting bits", []model.Sample{{Descriptor: "a", Kind: model.SampleCounter, Number: model.Signed(-1)}}},
		{"oversize", "65,537 samples", "reject before schema allocation", make([]model.Sample, maximumSamples+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("negative/boundary: %s; expected: %s", tc.description, tc.expected)
			r := newReducer(1)
			mustObserve(t, r, observed(r, 1, true))
			state := collect(t, r, model.CollectorCarrier, counter("a", 10, 64))
			previous := state.block
			job := begin(t, r, model.CollectorCarrier)
			if accepted, err := r.finishCollection(model.Result{Job: job, Support: model.Supported, Samples: tc.invalid}); !accepted || err == nil {
				t.Fatalf("invalid result=%v,%v", accepted, err)
			}
			if state.block != previous || state.discontinuities != 0 || state.reason != model.ErrorMalformed {
				t.Fatal("malformed set partially changed state")
			}
			collect(t, r, model.CollectorCarrier, counter("a", 11, 64))
			if state.discontinuities != 0 {
				t.Fatal("rejected set changed counter history")
			}
		})
	}
}

func TestReducerOldAttemptCannotReleaseWorker(t *testing.T) {
	t.Log("corner: duplicate completion during next attempt; expected: preserve the occupied worker and its result")
	r := newReducer(1)
	mustObserve(t, r, observed(r, 1, true))
	old := begin(t, r, model.CollectorCarrier)
	if ok, err := r.finishCollection(model.Result{Job: old, Support: model.Supported}); !ok || err != nil {
		t.Fatalf("first completion=%v,%v", ok, err)
	}
	current := begin(t, r, model.CollectorCarrier)
	if ok, err := r.finishCollection(model.Result{Job: old, Support: model.Supported}); ok || err != nil {
		t.Fatalf("duplicate completion=%v,%v", ok, err)
	}
	if _, err := r.startCollection(current.Key, model.Stamp{}); !errors.Is(err, errCollectionBusy) {
		t.Fatal("duplicate released another attempt")
	}
	if ok, err := r.finishCollection(model.Result{Job: current, Support: model.Supported}); !ok || err != nil {
		t.Fatalf("current completion=%v,%v", ok, err)
	}
}

func TestReducerSchemaLimit(t *testing.T) {
	t.Log("boundary: exactly 65,536 distinct samples; expected: accept complete schema, preserve final uint64 maximum")
	samples := make([]model.Sample, maximumSamples)
	for i := range samples {
		samples[i] = counter("stat_"+strconv.Itoa(i), math.MaxUint64, 64)
	}
	r := newReducer(1)
	mustObserve(t, r, observed(r, 1, true))
	state := collect(t, r, model.CollectorDriver, samples...)
	if len(state.history) != maximumSamples || len(state.block.values) != maximumSamples {
		t.Fatal("boundary sample set was truncated")
	}
	if value, ok := state.block.values[maximumSamples-1].Uint64(); !ok || value != math.MaxUint64 {
		t.Fatal("final sample lost precision")
	}
}

func TestReducerFailureAndSupport(t *testing.T) {
	for _, tc := range []struct {
		name, description, expected string
		support                     model.Support
		failure                     error
		retain                      bool
	}{
		{"read error", "temporary read error after successful sample", "retain data and history with IO error", model.SupportUnknown, errors.New("read failed"), true},
		{"unsupported", "source reports unsupported", "omit samples without converting absence to zero", model.Unsupported, nil, false},
		{"not applicable", "source does not apply to this device", "keep applicability distinct from error", model.NotApplicable, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("negative/corner: %s; expected: %s", tc.description, tc.expected)
			r := newReducer(1)
			mustObserve(t, r, observed(r, 1, true))
			state := collect(t, r, model.CollectorCarrier, counter("a", 10, 64))
			previous := state.block
			job := begin(t, r, model.CollectorCarrier)
			if ok, err := r.finishCollection(model.Result{Job: job, Support: tc.support, Err: tc.failure}); !ok || err != nil {
				t.Fatalf("completion=%v,%v", ok, err)
			}
			if tc.retain {
				if state.block != previous || state.support != model.Supported || state.reason != model.ErrorIO || !errors.Is(state.lastError, tc.failure) {
					t.Fatal("error lost previous data or failure status")
				}
			} else if state.block != nil || state.support != tc.support || state.reason != model.ErrorNone || state.lastError != nil {
				t.Fatal("support result was represented as data or failure")
			}
			collect(t, r, model.CollectorCarrier, counter("a", 9, 64))
			if state.discontinuities != 1 {
				t.Fatal("temporary unavailability erased counter history")
			}
		})
	}
}

func TestReducerSchemaOwnershipAndHost(t *testing.T) {
	r := newReducer(1)
	key := model.JobKey{Namespace: 1, Collector: model.CollectorNetstat}
	job, err := r.startCollection(key, model.Stamp{})
	if err != nil {
		t.Fatal(err)
	}
	labels := []model.Label{{Name: "z", Value: "last"}, {Name: "a", Value: "first"}}
	samples := []model.Sample{{Descriptor: "Tcp_MaxConn", Kind: model.SampleGauge, Number: model.Signed(-1), Labels: labels}}
	mustObserve(t, r, observed(r, 1, true)) // Unrelated interface revision must not stale host statistics.
	if ok, err := r.finishCollection(model.Result{Job: job, Support: model.Supported, Samples: samples}); !ok || err != nil {
		t.Fatalf("host result=%v,%v", ok, err)
	}
	before := r.host.block
	labels[0].Value = "mutated"
	samples[0].Number = model.Signed(42)
	if value, _ := before.values[0].Int64(); value != -1 || before.schema.entries[0].labels[1].Value != "last" {
		t.Fatal("published storage aliases source slices")
	}
	samples[0].Labels = []model.Label{{Name: "a", Value: "first"}, {Name: "z", Value: "last"}}
	job, err = r.startCollection(key, model.Stamp{})
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := r.finishCollection(model.Result{Job: job, Support: model.Supported, Samples: samples}); !ok || err != nil {
		t.Fatal(err)
	}
	if r.host.block.schema != before.schema {
		t.Fatal("equivalent label order rebuilt schema")
	}
	if value, _ := before.values[0].Int64(); value != -1 {
		t.Fatal("new collection overwrote retained old values")
	}
}
