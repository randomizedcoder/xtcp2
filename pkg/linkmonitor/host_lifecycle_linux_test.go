package linkmonitor

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/testkit"
)

func TestHostLiveReadOnly(t *testing.T) {
	t.Log("positive: read the current namespace proc files; expected: valid exact untyped fields without labels")
	cfg, err := validateConfig(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	c := newHostCollector(nil, "", cfg)
	result := c.Collect(t.Context(), hostTestJob())
	if result.Err != nil || result.Support != model.Supported || len(result.Samples) == 0 {
		t.Fatal(result.Err)
	}
	for _, sample := range result.Samples {
		if !strings.HasPrefix(sample.Descriptor, "netstat_") || sample.Kind != model.SampleUntyped || len(sample.Labels) != 0 {
			t.Fatal(sample)
		}
	}
}

func TestHostEnabledSession(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		combined                                     bool
	}{
		{"host", "positive", "host collector with empty inventory", "successful snapshot and joined shutdown", false},
		{"combined", "corner", "host and Ethernet adapters", "shared pool and successful shutdown", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			s, m, _ := sessionFixture(t)
			s.hostStatistics, s.statistics, s.settings, s.driverStatistics = true, tc.combined, tc.combined, tc.combined
			s.procRoot = t.TempDir()
			for name, data := range map[string]string{"snmp": "Tcp: MaxConn\nTcp: -1", "netstat": ""} {
				if err := os.WriteFile(filepath.Join(s.procRoot, name), []byte(data), 0600); err != nil {
					t.Fatal(err)
				}
			}
			cancel, done := runSession(t, m)
			awaitSnapshot(t, m, func(s Snapshot) bool { return s.Health().Ready && s.HostCollector().Fresh() })
			cancel()
			if err := receiveTest(t, done); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestHostResources(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		fail                                         bool
	}{
		{"delegate", "positive", "nonhost job and close", "delegate result and close error retained", false},
		{"factory", "negative", "base construction fails", "factory error without wrapper", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			wanted := errors.New("base failure")
			closed, delegated := false, false
			s := &lifecycleSession{hostStatistics: true, collectors: func(int) (workerCollector, error) {
				if tc.fail {
					return nil, wanted
				}
				return testWorkerCollector{CollectorFunc: func(context.Context, model.Job) model.Result {
					delegated = true
					return model.Result{Support: model.Unsupported}
				}, closeFunc: func() error { closed = true; return wanted }}, nil
			}}
			c, err := s.collectorFactory()(0)
			if tc.fail {
				if !errors.Is(err, wanted) || c != nil {
					t.Fatal(tc.expectedOutcome)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			result := c.Collect(t.Context(), model.Job{})
			if !delegated || result.Support != model.Unsupported || !errors.Is(c.Close(), wanted) || !closed {
				t.Fatal(tc.expectedOutcome)
			}
		})
	}
}

type blockedHostReader struct {
	entered chan<- struct{}
	release <-chan struct{}
	closed  *atomic.Int32
}

func (r blockedHostReader) Read([]byte) (int, error) {
	r.entered <- struct{}{}
	<-r.release
	return 0, io.EOF
}
func (r blockedHostReader) Close() error { r.closed.Add(1); return nil }

func TestHostBlockedRead(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		saturated                                    bool
	}{
		{"host", "corner", "host read blocks through deadline", "worker and file retained until return", false},
		{"saturated", "negative", "host and three other workers block", "no fifth worker or premature close", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			clock := testkit.NewClock(time.Unix(100, 0))
			entered, release := make(chan struct{}, collectorWorkers), make(chan struct{})
			var fileCloses, workerCloses atomic.Int32
			pool, err := newCollectorPool(t.Context(), clock, func(int) (workerCollector, error) {
				c := hostTestCollector(t, nil)
				c.workerCollector = testWorkerCollector{CollectorFunc: func(context.Context, model.Job) model.Result { entered <- struct{}{}; <-release; return model.Result{} }, closeFunc: func() error { workerCloses.Add(1); return nil }}
				c.open = func(string) (io.ReadCloser, error) { return blockedHostReader{entered, release, &fileCloses}, nil }
				return c, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			s := newScheduler(newReducer(1), pool, clock)
			key := registerTestJob(t, s, 0, model.CollectorNetstat, schedulePolicy{})
			blocked := 1
			if tc.saturated {
				blocked = collectorWorkers
				for i := range collectorWorkers - 1 {
					registerTestJob(t, s, uint32(i+1), model.CollectorDriver, schedulePolicy{})
				}
			}
			dispatchTest(t, s)
			for range blocked {
				receiveTest(t, entered)
			}
			advanceSchedulerClock(t, clock, collectionBudget)
			s.expire(clock.Now())
			s.resyncHost()
			dispatchTest(t, s)
			if !s.running(key).timedOut || fileCloses.Load() != 0 {
				t.Fatal(tc.expectedOutcome)
			}
			// The owner can still apply link observations while physical reads block.
			mustObserve(t, s.reducer, observed(s.reducer, 20, true))
			pool.stop()
			select {
			case <-pool.done:
				t.Fatal("blocked resources closed")
			default:
			}
			close(release)
			for range blocked {
				s.complete(receiveTest(t, pool.results))
			}
			receiveTest(t, pool.done)
			if fileCloses.Load() != 1 || workerCloses.Load() != collectorWorkers || s.reducer.host.hasSuccess {
				t.Fatal(tc.expectedOutcome)
			}
		})
	}
}
