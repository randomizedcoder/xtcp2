//go:build enrich_locality

package xtcp

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/localnet"
	"github.com/randomizedcoder/xtcp2/pkg/rtnlmon"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// listenEnv is one TestLocalityListener row's world: the index with its
// listener seams faked, the timers armed and the backoff waits taken.
type listenEnv struct {
	x      *XTCP
	li     *localityIndex
	timers []func()
	waits  []time.Duration
	now    time.Time
	runs   []func(e *listenEnv, h rtnlmon.Handler) error
	opts   []rtnlmon.Options
	cancel context.CancelFunc
}

// stop ends the listener's context, as nsDelete does, and returns what
// rtnlmon.Run then returns.
func (e *listenEnv) stop() error {
	e.cancel()
	return context.Canceled
}

// fire runs every armed debounce timer, as their goroutines would.
func (e *listenEnv) fire() {
	ts := e.timers
	e.timers = nil
	for _, f := range ts {
		f()
	}
}

func (e *listenEnv) locality(inode uint64, addr string) localnet.Locality {
	r, ok := e.li.Resolve(inode, netip.MustParseAddr(addr), 0)
	if !ok {
		return localnet.LocalityUnspecified
	}
	return localnet.Locality(r.Locality)
}

func (e *listenEnv) count(variable, typ string) int {
	return int(testutil.ToFloat64(e.x.pC.WithLabelValues("localityListener", variable, typ)))
}

func newListenEnv(t *testing.T) *listenEnv {
	t.Helper()
	x, li := newLocalityFixture(t, time.Minute)
	e := &listenEnv{x: x, li: li, now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	li.clock = func() time.Time { return e.now }
	li.listener.afterFunc = func(d time.Duration, f func()) {
		if d != localityEventDebounceCst {
			t.Errorf("timer armed for %s, expected %s", d, localityEventDebounceCst)
		}
		e.timers = append(e.timers, f)
	}
	li.listener.wait = func(_ context.Context, d time.Duration) bool {
		e.waits = append(e.waits, d)
		e.now = e.now.Add(d)
		return true
	}
	li.listener.run = func(ctx context.Context, opts rtnlmon.Options, h rtnlmon.Handler) error {
		e.opts = append(e.opts, opts)
		if len(e.runs) == 0 {
			return e.stop() // every scripted run is done
		}
		r := e.runs[0]
		e.runs = e.runs[1:]
		return r(e, h)
	}
	return e
}

var (
	errListenerBroke = errors.New("injected")
	eth0Snapshot     = rtnlmon.Snapshot{
		Links: []xtcpnl.LinkInfo{{Index: 2, Name: "eth0", Flags: unix.IFF_UP}},
		Addrs: []xtcpnl.AddrInfo{{Family: unix.AF_INET, Prefixlen: 24, Index: 2, Local: []byte{10, 0, 0, 1}, Address: []byte{10, 0, 0, 1}}},
	}
	addSecond = xtcpnl.AddrEvent{Action: xtcpnl.EventActionAdd, Addr: xtcpnl.AddrInfo{Family: unix.AF_INET, Prefixlen: 24, Index: 2, Local: []byte{10, 0, 0, 2}, Address: []byte{10, 0, 0, 2}}}
	addThird  = xtcpnl.AddrEvent{Action: xtcpnl.EventActionAdd, Addr: xtcpnl.AddrInfo{Family: unix.AF_INET, Prefixlen: 24, Index: 2, Local: []byte{10, 0, 0, 3}, Address: []byte{10, 0, 0, 3}}}
)

// go test -ldflags=-checklinkname=0 -tags enrich_locality ./pkg/xtcp/ -run TestLocalityListener
func TestLocalityListener(t *testing.T) {
	type expect struct {
		self     []string        // addresses inode 1 resolves as self
		notSelf  []string        // and ones it does not
		rebuilds int             // snapshots published from the fold
		restarts int             // listener restarts
		waits    []time.Duration // backoff waits taken between runs
		dumped   []uint64        // namespaces Refresh dumped
	}
	start := func(e *listenEnv, h rtnlmon.Handler) error {
		return h.Snapshot(eth0Snapshot, rtnlmon.ReasonStart)
	}
	for _, row := range []struct {
		description string
		runs        []func(e *listenEnv, h rtnlmon.Handler) error
		// steps run after Listen returns; a run's own steps run inside it.
		steps    func(t *testing.T, e *listenEnv)
		expected expect
	}{
		{
			description: "positive: the start snapshot publishes at once",
			runs: []func(*listenEnv, rtnlmon.Handler) error{func(e *listenEnv, h rtnlmon.Handler) error {
				if err := start(e, h); err != nil {
					return err
				}
				if !e.li.slots()[1].isLive() {
					t.Error("not live after the start snapshot")
				}
				return e.stop()
			}},
			expected: expect{self: []string{"10.0.0.1"}, notSelf: []string{"10.0.0.2"}, rebuilds: 1},
		},
		{
			description: "positive: events publish once the debounce timer fires",
			runs: []func(*listenEnv, rtnlmon.Handler) error{func(e *listenEnv, h rtnlmon.Handler) error {
				if err := start(e, h); err != nil {
					return err
				}
				if err := h.Event(addSecond); err != nil {
					return err
				}
				if err := h.Event(addThird); err != nil {
					return err
				}
				if len(e.timers) != 1 {
					t.Errorf("%d timers for one burst, expected 1", len(e.timers))
				}
				if e.locality(1, "10.0.0.2") == localnet.LocalitySelf {
					t.Error("published before the debounce")
				}
				e.fire()
				return e.stop()
			}},
			expected: expect{self: []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"}, rebuilds: 2},
		},
		{
			description: "negative: an event that changes nothing arms no timer",
			runs: []func(*listenEnv, rtnlmon.Handler) error{func(e *listenEnv, h rtnlmon.Handler) error {
				if err := start(e, h); err != nil {
					return err
				}
				if err := h.Event(xtcpnl.LinkEvent{Action: xtcpnl.EventActionAdd, Link: eth0Snapshot.Links[0]}); err != nil {
					return err
				}
				if len(e.timers) != 0 {
					t.Errorf("%d timers, expected 0", len(e.timers))
				}
				return e.stop()
			}},
			expected: expect{self: []string{"10.0.0.1"}, rebuilds: 1},
		},
		{
			description: "negative: a failed run hands the namespace back, then restarts after the backoff",
			runs: []func(*listenEnv, rtnlmon.Handler) error{
				func(e *listenEnv, h rtnlmon.Handler) error {
					if err := start(e, h); err != nil {
						return err
					}
					return errListenerBroke
				},
				func(e *listenEnv, h rtnlmon.Handler) error {
					if e.li.slots()[1].isLive() {
						t.Error("still live between runs")
					}
					return errListenerBroke
				},
			},
			expected: expect{self: []string{"10.0.0.1"}, rebuilds: 1, restarts: 2, waits: []time.Duration{30 * time.Second, time.Minute}},
		},
		{
			description: "boundary: a run that was healthy past the backoff cap starts it over",
			runs: []func(*listenEnv, rtnlmon.Handler) error{
				func(e *listenEnv, h rtnlmon.Handler) error { return errListenerBroke },
				func(e *listenEnv, h rtnlmon.Handler) error {
					e.now = e.now.Add(localityRetryMaxCst)
					return errListenerBroke
				},
			},
			expected: expect{restarts: 2, waits: []time.Duration{30 * time.Second, 30 * time.Second}},
		},
		{
			description: "boundary: a timer that fires after the listener ended publishes nothing",
			runs: []func(*listenEnv, rtnlmon.Handler) error{func(e *listenEnv, h rtnlmon.Handler) error {
				if err := start(e, h); err != nil {
					return err
				}
				if err := h.Event(addSecond); err != nil {
					return err
				}
				return e.stop()
			}},
			steps:    func(_ *testing.T, e *listenEnv) { e.fire() },
			expected: expect{self: []string{"10.0.0.1"}, notSelf: []string{"10.0.0.2"}, rebuilds: 1},
		},
		{
			description: "corner: Refresh skips a live namespace and dumps the rest",
			runs: []func(*listenEnv, rtnlmon.Handler) error{func(e *listenEnv, h rtnlmon.Handler) error {
				if err := start(e, h); err != nil {
					return err
				}
				e.x.refreshLocality(map[uint64]nsIdentity{1: {inode: 1}, 2: {inode: 2}})
				return e.stop()
			}},
			expected: expect{self: []string{"10.0.0.1"}, rebuilds: 1, dumped: []uint64{2}},
		},
		{
			description: "corner: a dump Refresh began before the listener went live is discarded",
			runs: []func(*listenEnv, rtnlmon.Handler) error{func(e *listenEnv, h rtnlmon.Handler) error {
				slot := e.li.slots()[1]
				if err := start(e, h); err != nil {
					return err
				}
				slot.publishDump(realSnap) // 10.0.0.1 too, but not the listener's
				if slot.snap.Load() == realSnap {
					t.Error("a dump overwrote the live listener's snapshot")
				}
				return e.stop()
			}},
			expected: expect{self: []string{"10.0.0.1"}, rebuilds: 1},
		},
		{
			description: "corner: a vanished namespace's slot is dropped, and its listener's publishes go nowhere",
			runs: []func(*listenEnv, rtnlmon.Handler) error{func(e *listenEnv, h rtnlmon.Handler) error {
				if err := start(e, h); err != nil {
					return err
				}
				e.x.refreshLocality(map[uint64]nsIdentity{2: {inode: 2}})
				if err := h.Event(addSecond); err != nil {
					return err
				}
				e.fire()
				return e.stop()
			}},
			expected: expect{notSelf: []string{"10.0.0.1", "10.0.0.2"}, rebuilds: 2, dumped: []uint64{2}},
		},
	} {
		t.Run(row.description, func(t *testing.T) {
			e := newListenEnv(t)
			var dumped []uint64
			e.li.dumper = func(id nsIdentity) (*localnet.Snapshot, bool) {
				dumped = append(dumped, id.inode)
				return realSnap, true
			}
			e.runs = row.runs
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			e.cancel = cancel
			e.li.Listen(ctx, 1)
			if row.steps != nil {
				row.steps(t, e)
			}

			for _, a := range row.expected.self {
				if got := e.locality(1, a); got != localnet.LocalitySelf {
					t.Errorf("%s is %v, expected self", a, got)
				}
			}
			for _, a := range row.expected.notSelf {
				if got := e.locality(1, a); got == localnet.LocalitySelf {
					t.Errorf("%s is self, expected not", a)
				}
			}
			if slot := e.li.slots()[1]; slot != nil && slot.isLive() {
				t.Error("still live after Listen returned")
			}
			if got := e.count("rebuilds", "count"); got != row.expected.rebuilds {
				t.Errorf("%d rebuilds, expected %d", got, row.expected.rebuilds)
			}
			if got := e.count("restarts", "count"); got != row.expected.restarts {
				t.Errorf("%d restarts, expected %d", got, row.expected.restarts)
			}
			if !slices.Equal(e.waits, row.expected.waits) {
				t.Errorf("waits %v, expected %v", e.waits, row.expected.waits)
			}
			if !slices.Equal(dumped, row.expected.dumped) {
				t.Errorf("Refresh dumped %v, expected %v", dumped, row.expected.dumped)
			}
			if g := testutil.ToFloat64(e.x.pGV.WithLabelValues("localityListener", "live", "gauge")); g != 0 {
				t.Errorf("live gauge %v after Listen returned, expected 0", g)
			}
			for _, o := range e.opts {
				if o.ResyncAfter != time.Minute || o.RcvBuf != localityRcvBufCst || !slices.Equal(o.Groups, localityGroups) {
					t.Errorf("Options %+v", o)
				}
			}
		})
	}
}

// go test -ldflags=-checklinkname=0 -tags enrich_locality ./pkg/xtcp/ -run TestLocalityListenerKernel
func TestLocalityListenerKernel(t *testing.T) {
	t.Run("positive: the real listener goes live on this namespace and stops with its context", func(t *testing.T) {
		x, li := newLocalityFixture(t, time.Minute)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			x.locality.Listen(ctx, 1)
		}()
		deadline := time.Now().Add(5 * time.Second)
		for {
			if s := li.slots()[1]; s != nil && s.isLive() {
				break
			}
			if time.Now().After(deadline) {
				cancel()
				t.Fatalf("not live after 5s; run errors %v", testutil.ToFloat64(x.pC.WithLabelValues("localityListener", "run", "error")))
			}
			time.Sleep(10 * time.Millisecond)
		}
		if r, ok := li.Resolve(1, netip.MustParseAddr("127.0.0.1"), 0); !ok || localnet.Locality(r.Locality) != localnet.LocalitySelf {
			t.Errorf("127.0.0.1 resolved %+v %v, expected self", r, ok)
		}
		if n := testutil.ToFloat64(x.pC.WithLabelValues("localityListener", "snapshot", "start")); n != 1 {
			t.Errorf("%v start snapshots, expected 1", n)
		}
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("Listen did not return after its context ended")
		}
		if li.slots()[1].isLive() {
			t.Error("still live after Listen returned")
		}
	})
}
