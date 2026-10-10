package linkmonitor

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"golang.org/x/sys/unix"
)

type productionRouteStub struct {
	ready <-chan struct{}
	reads atomic.Uint64
}

func (s *productionRouteStub) Receive(ctx context.Context, visit func([]byte) error) (int, error) {
	if s.reads.Add(1) != 1 {
		<-ctx.Done()
		return 0, ctx.Err()
	}
	select {
	case <-s.ready:
	case <-ctx.Done():
		return 0, ctx.Err()
	}
	return 1, visit(productionRouteFixture())
}
func (*productionRouteStub) Close() error { return nil }

func TestProductionOptionalIsolation(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		err                                          error
	}{
		{"denied", "negative", "optional multicast join denied", "route event delivered and cancellation joins both readers", unix.EACCES},
		{"missing", "boundary", "ethtool family absent", "route monitoring continues with recovery hint", unix.ENOENT},
		{"malformed", "corner", "optional malformed datagram", "reconciliation requested without discarding route stream", unix.EINVAL},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			ready := make(chan struct{})
			var attempts atomic.Uint64
			s := &productionEvents{route: &productionRouteStub{ready: ready}, namespace: 7,
				clock: productionClock{origin: time.Now()}, logger: slog.New(slog.NewTextHandler(io.Discard, nil)), resync: time.Hour,
				optional: func(context.Context, func(model.Event) bool) error {
					if attempts.Add(1) == 1 {
						close(ready)
					}
					return tc.err
				}}
			events := make(chan model.Event, 4)
			done := make(chan error, 1)
			go func() { done <- s.Run(ctx, func(e model.Event) bool { events <- e; return true }) }()
			var link, resync bool
			for !link || !resync {
				e := await(t, events)
				link = link || e.Kind == model.EventLink
				resync = resync || e.Kind == model.EventResync
			}
			cancel()
			if err := await(t, done); !errors.Is(err, context.Canceled) {
				t.Fatal(tc.expectedOutcome, err)
			}
		})
	}
}
