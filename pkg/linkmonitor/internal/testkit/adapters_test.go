package testkit

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

func await[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("test synchronization timed out")
		var zero T
		return zero
	}
}

func TestScriptedEvents(t *testing.T) {
	failure := errors.New("scripted event failure")
	tests := []struct {
		name, category, description, expected string
		steps                                 []EventStep
		stopAfter                             int
		want                                  []model.EventKind
		wantError                             error
	}{
		{"order", "positive", "down/remove then new identity", "preserve both events", []EventStep{{Event: model.Event{Kind: model.EventRemove}}, {Event: model.Event{Kind: model.EventChange}}}, 0, []model.EventKind{model.EventRemove, model.EventChange}, nil},
		{"stop", "boundary", "consumer stops after first event", "no later event delivered", []EventStep{{Event: model.Event{Kind: model.EventRemove}}, {Event: model.Event{Kind: model.EventChange}}}, 1, []model.EventKind{model.EventRemove}, nil},
		{"error", "negative", "source errors between events", "earlier events retained and error returned", []EventStep{{Event: model.Event{Kind: model.EventChange}}, {Err: failure}, {Event: model.Event{Kind: model.EventRemove}}}, 0, []model.EventKind{model.EventChange}, failure},
		{"empty", "boundary", "successful empty script", "no invented events or error", nil, 0, nil, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			source := NewEvents(tc.steps...)
			got := make([]model.EventKind, 0, len(tc.steps))
			err := source.Run(t.Context(), func(event model.Event) bool {
				got = append(got, event.Kind)
				return tc.stopAfter == 0 || len(got) < tc.stopAfter
			})
			if !errors.Is(err, tc.wantError) {
				t.Fatalf("error = %v, want %v", err, tc.wantError)
			}
			if len(got) != len(tc.want) || (len(got) > 0 && !reflect.DeepEqual(got, tc.want)) {
				t.Fatalf("events = %v, want %v", got, tc.want)
			}
			if err := source.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestEventWakeup(t *testing.T) {
	for _, tc := range []struct {
		name, description, expected string
		closeSource                 bool
	}{
		{"cancel", "cancel while event source is blocked", "context.Canceled and no event", false},
		{"close", "close while event source is blocked", "context.Canceled and no event", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s; expected: %s", tc.description, tc.expected)
			gate := NewBarrier()
			defer gate.Release()
			source := NewEvents(EventStep{Gate: gate, Event: model.Event{Kind: model.EventChange}})
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			result := make(chan error, 1)
			go func() {
				result <- source.Run(ctx, func(model.Event) bool { t.Error("delivered canceled event"); return true })
			}()
			await(t, gate.Entered())
			if tc.closeSource {
				if err := source.Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				cancel()
			}
			if err := await(t, result); !errors.Is(err, context.Canceled) {
				t.Fatalf("Run = %v", err)
			}
		})
	}
}

func TestCollectorSupportAndErrors(t *testing.T) {
	job := model.Job{Key: model.JobKey{Namespace: 3, Collector: model.CollectorPHY}, Token: model.Token{Generation: 5, Revision: 6, SourceEpoch: 7, Attempt: 8}}
	failure := errors.New("permission denied")
	for _, tc := range []struct {
		name, description, expected string
		support                     model.Support
		err                         error
	}{
		{"unsupported", "driver lacks PHY stats", "unsupported without error", model.Unsupported, nil},
		{"denied", "driver query fails", "unknown support with error", model.SupportUnknown, failure},
		{"empty", "successful empty filtered set", "supported without samples", model.Supported, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s; expected: %s", tc.description, tc.expected)
			var collector model.Collector = CollectorFunc(func(ctx context.Context, received model.Job) model.Result {
				if received != job || ctx != t.Context() {
					t.Fatal("attempt identity or context lost")
				}
				return model.Result{Job: received, Support: tc.support, Err: tc.err}
			})
			got := collector.Collect(t.Context(), job)
			if got.Support != tc.support || !errors.Is(got.Err, tc.err) || len(got.Samples) != 0 {
				t.Fatal("support/failure/empty result conflated")
			}
		})
	}
}

func TestStoreScript(t *testing.T) {
	failure := errors.New("directory fsync failed")
	record := model.Baseline{Version: 1, Count: 0, RecordedAt: time.Unix(100, 0)}
	for _, tc := range []struct {
		name, description, expected string
		outcome                     model.SaveOutcome
		err                         error
	}{
		{"durable", "zero baseline saved successfully", "durable zero is present", model.SaveDurable, nil},
		{"failure", "write fails before rename", "failed outcome with error", model.SaveFailed, failure},
		{"indeterminate", "parent sync fails after rename", "indeterminate outcome with error", model.SaveIndeterminate, failure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s; expected: %s", tc.description, tc.expected)
			calls := make([]string, 0, 3)
			var store model.BaselineStore = StoreFuncs{
				LoadFunc: func(ctx context.Context) (model.Baseline, bool, error) {
					calls = append(calls, "load")
					return record, true, ctx.Err()
				},
				SaveFunc: func(ctx context.Context, got model.Baseline) model.SaveResult {
					calls = append(calls, "save")
					if got != record || ctx != t.Context() {
						t.Fatal("record or context lost")
					}
					return model.SaveResult{Outcome: tc.outcome, Err: tc.err}
				},
				CloseFunc: func() error { calls = append(calls, "close"); return nil },
			}
			if got, present, err := store.LoadAndLock(t.Context()); got != record || !present || err != nil {
				t.Fatal("present zero confused with missing record")
			}
			if got := store.Save(t.Context(), record); got.Outcome != tc.outcome || !errors.Is(got.Err, tc.err) {
				t.Fatal("durability outcome changed")
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(calls, []string{"load", "save", "close"}) {
				t.Fatalf("operation order %v", calls)
			}
		})
	}
}

func TestWorkerBarrier(t *testing.T) {
	gate := NewBarrier()
	defer gate.Release()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	finished := make(chan model.Result, 1)
	collector := CollectorFunc(func(ctx context.Context, job model.Job) model.Result {
		return model.Result{Job: job, Err: gate.Wait(ctx)}
	})
	go func() { finished <- collector.Collect(ctx, model.Job{}) }()
	await(t, gate.Entered())
	select {
	case <-finished:
		t.Fatal("blocked worker completed before release")
	default:
	}
	cancel()
	if result := await(t, finished); !errors.Is(result.Err, context.Canceled) {
		t.Fatal("cancellation not observable")
	}
	gate.Release()
	gate.Release()
	if err := gate.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
}
