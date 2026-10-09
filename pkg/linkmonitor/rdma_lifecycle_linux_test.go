package linkmonitor

import (
	"context"
	"errors"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

type rdmaStateFuncs struct {
	read  func(context.Context, model.RDMAPort) (model.RDMAPort, error)
	close func() error
}

func (f rdmaStateFuncs) Read(ctx context.Context, p model.RDMAPort) (model.RDMAPort, error) {
	return f.read(ctx, p)
}
func (f rdmaStateFuncs) Close() error { return f.close() }

func TestRDMAEnabledLifecycle(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		fail, closeFail                              bool
	}{
		{"native", "positive", "native discovery and required state in real owner loop", "fresh readiness, no claimed verbs event health, joined shutdown", false, false},
		{"startup", "negative", "required state factory fails", "startup error and acquired resources joined", true, false},
		{"close", "negative", "required state close fails", "close error returned after joining", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			s, m, _ := sessionFixture(t)
			inventory, _, _ := rdmaFixture(t, rdmaNativeLayer)
			s.rdma = true
			s.inventory = func(context.Context) (inventoryBackend, error) { return inventory, nil }
			closed := make(chan struct{})
			wanted := errors.New("RDMA resource failure")
			s.rdmaSource = func(context.Context) (rdmaStateSource, error) {
				if tc.fail {
					return nil, wanted
				}
				return rdmaStateFuncs{read: func(_ context.Context, p model.RDMAPort) (model.RDMAPort, error) { return p, nil }, close: func() error {
					close(closed)
					if tc.closeFail {
						return wanted
					}
					return nil
				}}, nil
			}
			cancel, done := runSession(t, m)
			if !tc.fail {
				awaitSnapshot(t, m, func(snapshot Snapshot) bool {
					found := false
					snapshot.RangeDevices(func(d DeviceView) bool {
						if d.Name() == "rdma:mlx5_0:1" && d.RDMAReadinessCheck() == CheckPass {
							found = true
						}
						return true
					})
					return found && !snapshot.Health().CollectionHealthy
				})
			}
			if !tc.fail {
				cancel()
			}
			err := receiveTest(t, done)
			if tc.fail || tc.closeFail {
				if !errors.Is(err, wanted) {
					t.Fatal(err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if !tc.fail {
				receiveTest(t, closed)
			}
			receiveTest(t, s.cleaned)
		})
	}
}
