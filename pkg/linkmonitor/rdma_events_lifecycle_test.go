package linkmonitor

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/rdmaevents"
)

type rdmaEventFuncs struct {
	run   func(context.Context, func(rdmaevents.Event) bool) error
	close func() error
}

func (f rdmaEventFuncs) Run(ctx context.Context, emit func(rdmaevents.Event) bool) error {
	return f.run(ctx, emit)
}
func (f rdmaEventFuncs) Close() error { return f.close() }

func TestRDMAEventsEnabledLifecycle(t *testing.T) {
	t.Log("positive: real lifecycle with discovery, subscriptions and state; expected: native count becomes ready after subscription/reconciliation barrier, all generations join")
	s, m, _ := sessionFixture(t)
	inventory, _, _ := rdmaFixture(t, rdmaNativeLayer)
	s.rdma = true
	s.inventory = func(context.Context) (inventoryBackend, error) { return inventory, nil }
	s.rdmaSource = func(context.Context) (rdmaStateSource, error) {
		return rdmaStateFuncs{read: func(_ context.Context, p model.RDMAPort) (model.RDMAPort, error) { return p, nil }, close: func() error { return nil }}, nil
	}
	var opens, closes atomic.Int64
	var subscribedHCA atomic.Bool
	s.rdmaEventSource = func(_ context.Context, ids []rdmaevents.Identity) (rdmaevents.Source, error) {
		for _, id := range ids {
			if id.Name == "mlx5_0" {
				subscribedHCA.Store(true)
			}
		}
		opens.Add(1)
		return rdmaEventFuncs{run: func(ctx context.Context, _ func(rdmaevents.Event) bool) error { <-ctx.Done(); return ctx.Err() },
			close: func() error { closes.Add(1); return nil }}, nil
	}
	cancel, done := runSession(t, m)
	before := awaitSnapshot(t, m, func(snapshot Snapshot) bool { return snapshot.Health().Ready && snapshot.Health().CollectionHealthy })
	if !subscribedHCA.Load() {
		t.Fatal("eligible HCA set was not subscribed after discovery")
	}
	cancel()
	if err := receiveTest(t, done); err != nil {
		t.Fatal(err)
	}
	if opens.Load() != closes.Load() || !before.Health().Ready {
		t.Fatal("subscription leak or mutated retained snapshot")
	}
}
