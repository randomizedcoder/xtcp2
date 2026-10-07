// Package testkit provides deterministic private adapters for monitor tests.
// Production monitoring packages must not import it.
package testkit

import (
	"context"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

// CollectorFunc adapts a synchronous scripted function to a collector.
type CollectorFunc func(context.Context, model.Job) model.Result

// Collect forwards the original context and attempt identity unchanged.
func (f CollectorFunc) Collect(ctx context.Context, job model.Job) model.Result {
	return f(ctx, job)
}

// InventoryFuncs makes dump/query behavior explicit at each test call site.
type InventoryFuncs struct {
	DumpFunc  func(context.Context) (model.Candidate, error)
	QueryFunc func(context.Context, model.DeviceKey) (model.Observation, error)
}

// Dump executes the scripted complete or failed inventory operation.
func (f InventoryFuncs) Dump(ctx context.Context) (model.Candidate, error) { return f.DumpFunc(ctx) }

// Query executes a scripted identity query.
func (f InventoryFuncs) Query(ctx context.Context, key model.DeviceKey) (model.Observation, error) {
	return f.QueryFunc(ctx, key)
}

// StoreFuncs exposes explicit load, save and release behavior, including errors.
type StoreFuncs struct {
	LoadFunc  func(context.Context) (model.Baseline, bool, error)
	SaveFunc  func(context.Context, model.Baseline) model.SaveResult
	CloseFunc func() error
}

// LoadAndLock returns the scripted record, presence and error separately.
func (f StoreFuncs) LoadAndLock(ctx context.Context) (model.Baseline, bool, error) {
	return f.LoadFunc(ctx)
}

// Save returns the scripted durability outcome.
func (f StoreFuncs) Save(ctx context.Context, record model.Baseline) model.SaveResult {
	return f.SaveFunc(ctx, record)
}

// Close executes the scripted lock/resource release.
func (f StoreFuncs) Close() error { return f.CloseFunc() }

// EventFuncs exposes ordered delivery and explicit close behavior.
type EventFuncs struct {
	RunFunc   func(context.Context, func(model.Event) bool) error
	CloseFunc func() error
}

// Run forwards context and the callback's backpressure/stop decision.
func (f EventFuncs) Run(ctx context.Context, visit func(model.Event) bool) error {
	return f.RunFunc(ctx, visit)
}

// Close executes the scripted reader wakeup.
func (f EventFuncs) Close() error { return f.CloseFunc() }

var (
	_ model.Collector       = CollectorFunc(nil)
	_ model.InventorySource = InventoryFuncs{}
	_ model.BaselineStore   = StoreFuncs{}
	_ model.EventSource     = EventFuncs{}
)
