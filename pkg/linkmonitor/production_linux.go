package linkmonitor

import (
	"context"
	"fmt"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/baseline"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/rdmaevents"
	"golang.org/x/sys/unix"
)

func (m *Monitor) openProduction(ctx context.Context) (session, error) {
	if m.cfg.IOBackend != IOBackendPoller {
		return nil, ErrBackendUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var ns unix.Stat_t
	if err := unix.Stat("/proc/self/ns/net", &ns); err != nil {
		return nil, fmt.Errorf("monitor network namespace: %w", err)
	}
	store, err := baseline.New(m.cfg.BaselineFile)
	if err != nil {
		return nil, err
	}
	clock := productionClock{origin: time.Now()}
	m.logger.Info("link monitor build capabilities", "rdma_bindings", rdmaevents.Available,
		"rdma_requirement", "Linux rdma tag, cgo, libibverbs, libibumad and matching providers")
	s := &lifecycleSession{clock: clock, namespace: ns.Ino, store: store,
		statistics: true, settings: true, driverStatistics: true, hostStatistics: true, rdma: true,
		collectors: func(int) (workerCollector, error) { return emptyCollector{}, nil },
	}
	s.subscribe = func(ctx context.Context, _ uint64) (eventSubscription, error) {
		return openProductionEvents(ctx, ns.Ino, clock, m.logger, m.cfg.Resync)
	}
	return s, nil
}

// The final decorator rejects unhandled jobs rather than inventing success.
type emptyCollector struct{}

func (emptyCollector) Collect(_ context.Context, job model.Job) model.Result {
	return model.Result{Job: job, Support: model.Unsupported}
}

func (emptyCollector) Close() error { return nil }

type productionClock struct{ origin time.Time }

func (c productionClock) Now() model.Stamp {
	now := time.Now()
	return model.Stamp{Wall: now, Monotonic: now.Sub(c.origin)}
}

func (productionClock) NewTimer(d time.Duration) model.Timer {
	return productionTimer{time.NewTimer(d)}
}

type productionTimer struct{ timer *time.Timer }

func (t productionTimer) C() <-chan time.Time   { return t.timer.C }
func (t productionTimer) Reset(d time.Duration) { t.timer.Reset(d) }
func (t productionTimer) Stop()                 { t.timer.Stop() }
