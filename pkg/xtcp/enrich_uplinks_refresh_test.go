package xtcp

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_flat_record"
	"github.com/randomizedcoder/xtcp2/pkg/lldp"
	"github.com/randomizedcoder/xtcp2/pkg/nicinfo"
)

// fakeUplinks scripts the uplink sources: each call to defaultUplinks returns
// the next list in routes (the last one repeats), and it counts the sysfs and
// lldpd reads a refresh makes.
type fakeUplinks struct {
	mu       sync.Mutex
	routes   [][]string
	physical []string
	lldpErr  error
	calls    int
	collects int
	fetches  int
}

func (f *fakeUplinks) sources() uplinkSources {
	return uplinkSources{
		defaultUplinks: func(maxN int) []string {
			f.mu.Lock()
			defer f.mu.Unlock()
			i := min(f.calls, len(f.routes)-1)
			f.calls++
			return slices.Clone(f.routes[i])
		},
		physical: func(maxN int) []string { return slices.Clone(f.physical) },
		collect: func(ifnames []string) []nicinfo.NIC {
			f.mu.Lock()
			f.collects++
			f.mu.Unlock()
			nics := make([]nicinfo.NIC, 0, len(ifnames))
			for _, n := range ifnames {
				nics = append(nics, nicinfo.NIC{Ifname: n, Driver: "drv-" + n})
			}
			return nics
		},
		fetchLLDP: func(context.Context) (map[string]lldp.Neighbor, error) {
			f.mu.Lock()
			f.fetches++
			err := f.lldpErr
			f.mu.Unlock()
			if err != nil {
				return nil, err
			}
			return map[string]lldp.Neighbor{"eth0": {Ifname: "eth0", ChassisName: "sw-a"}, "eth1": {Ifname: "eth1", ChassisName: "sw-b"}}, nil
		},
	}
}

func uplinkFixture(t *testing.T, f *fakeUplinks) *XTCP {
	t.Helper()
	x := newMetricsFixture(t, time.Minute)
	x.config.EnrichNicEnable = true
	x.config.EnrichLldpEnable = true
	x.uplinkSrc = f.sources()
	return x
}

func stampedUplink1(x *XTCP) (ifname, driver, chassis string) {
	r := &xtcp_flat_record.XtcpFlatRecord{}
	x.applyEnrichment(r)
	return r.Uplink1Ifname, r.Uplink1NicDriver, r.Uplink1LldpChassisName
}

func refreshCount(x *XTCP, outcome string) float64 {
	return testutil.ToFloat64(x.pC.WithLabelValues("refreshUplinks", "uplinks", outcome))
}

// go test ./pkg/xtcp/ -run TestRefreshUplinksOnce
func TestRefreshUplinksOnce(t *testing.T) {
	tests := []struct {
		name         string
		category     string
		description  string
		routes       [][]string // startup, then one refresh
		physical     []string
		lldpErr      error // on the refresh only
		wantUplink1  string
		wantDriver   string
		wantChassis  string
		wantCollects int
		wantOutcome  string
	}{
		{name: "swap_follows", category: "positive", description: "a metric swap moves the stamp",
			routes: [][]string{{"eth0", "eth1"}, {"eth1", "eth0"}}, wantUplink1: "eth1", wantDriver: "drv-eth1", wantChassis: "sw-b",
			wantCollects: 2, wantOutcome: "changed"},
		{name: "unchanged_no_rebuild", category: "negative", description: "the same list reads neither sysfs nor lldpd again",
			routes: [][]string{{"eth0", "eth1"}, {"eth0", "eth1"}}, wantUplink1: "eth0", wantDriver: "drv-eth0", wantChassis: "sw-a",
			wantCollects: 1, wantOutcome: "unchanged"},
		{name: "uplink_lost", category: "boundary", description: "the default route goes away: the physical fallback takes over",
			routes: [][]string{{"eth0"}, {}}, physical: []string{"eth2"}, wantUplink1: "eth2", wantDriver: "drv-eth2",
			wantCollects: 2, wantOutcome: "changed"},
		{name: "none_left", category: "boundary", description: "no route and no physical NIC: the columns go empty",
			routes: [][]string{{"eth0"}, {}}, wantCollects: 1, wantOutcome: "changed"},
		{name: "lldp_fails_on_refresh", category: "corner", description: "lldpd down on refresh keeps the new NIC columns",
			routes: [][]string{{"eth0", "eth1"}, {"eth1", "eth0"}}, lldpErr: errors.New("lldpd gone"),
			wantUplink1: "eth1", wantDriver: "drv-eth1", wantCollects: 2, wantOutcome: "lldp_error"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeUplinks{routes: tc.routes, physical: tc.physical}
			x := uplinkFixture(t, f)
			x.initUplinkEnrichers(context.Background())
			current := tc.routes[0]

			f.mu.Lock()
			f.lldpErr = tc.lldpErr
			f.mu.Unlock()
			x.refreshUplinksOnce(context.Background(), maxUplinks, current)

			gotIf, gotDrv, gotCh := stampedUplink1(x)
			if gotIf != tc.wantUplink1 || gotDrv != tc.wantDriver || gotCh != tc.wantChassis {
				t.Errorf("%s: uplink1 = %q/%q/%q, expected %q/%q/%q", tc.description, gotIf, gotDrv, gotCh, tc.wantUplink1, tc.wantDriver, tc.wantChassis)
			}
			if f.collects != tc.wantCollects {
				t.Errorf("%s: %d sysfs reads, expected %d", tc.description, f.collects, tc.wantCollects)
			}
			if got := refreshCount(x, tc.wantOutcome); got != 1 {
				t.Errorf("%s: refreshUplinks %s = %v, expected 1", tc.description, tc.wantOutcome, got)
			}
		})
	}
}

// go test ./pkg/xtcp/ -run TestUplinkRefreshInterval
func TestUplinkRefreshInterval(t *testing.T) {
	tests := []struct {
		name        string
		category    string
		description string
		config      *xtcp_config.XtcpConfig
		expected    time.Duration
	}{
		{name: "auto_refresh", category: "positive", description: "auto-detected uplinks refresh on the locality interval",
			config: &xtcp_config.XtcpConfig{EnrichNicEnable: true, LocalityRefreshInterval: durationpb.New(time.Minute)}, expected: time.Minute},
		{name: "pinned_no_refresh", category: "negative", description: "pinned uplink interfaces never refresh",
			config: &xtcp_config.XtcpConfig{EnrichNicEnable: true, UplinkInterfaces: []string{"eth9"}, LocalityRefreshInterval: durationpb.New(time.Minute)}},
		{name: "interval_zero", category: "boundary", description: "locality_refresh_interval 0 loads once",
			config: &xtcp_config.XtcpConfig{EnrichNicEnable: true, LocalityRefreshInterval: durationpb.New(0)}},
		{name: "interval_unset", category: "corner", description: "no interval at all loads once",
			config: &xtcp_config.XtcpConfig{EnrichLldpEnable: true}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := uplinkRefreshInterval(tc.config); got != tc.expected {
				t.Errorf("%s: got %v, expected %v", tc.description, got, tc.expected)
			}
		})
	}
}

// go test ./pkg/xtcp/ -run TestRefreshUplinksLoop
func TestRefreshUplinksLoop(t *testing.T) {
	tests := []struct {
		name        string
		category    string
		description string
		ticks       int
		expected    string // uplink1 after the ticks
	}{
		{name: "two_ticks", category: "positive", description: "each tick re-reads the routes", ticks: 2, expected: "eth0"},
		{name: "one_tick", category: "boundary", description: "one tick applies the swap", ticks: 1, expected: "eth1"},
		{name: "no_tick", category: "corner", description: "cancel before any tick keeps the startup stamp", ticks: 0, expected: "eth0"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeUplinks{routes: [][]string{{"eth0", "eth1"}, {"eth1", "eth0"}, {"eth0", "eth1"}}}
			x := uplinkFixture(t, f)
			x.initUplinkEnrichers(context.Background())

			ctx, cancel := context.WithCancel(context.Background())
			tick := make(chan time.Time)
			done := make(chan struct{})
			go func() {
				x.refreshUplinks(ctx, maxUplinks, []string{"eth0", "eth1"}, tick)
				close(done)
			}()
			for range tc.ticks {
				tick <- time.Now()
			}
			cancel()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatalf("%s: the refresh loop did not return after cancel", tc.description)
			}
			if got, _, _ := stampedUplink1(x); got != tc.expected {
				t.Errorf("%s: uplink1 = %q, expected %q", tc.description, got, tc.expected)
			}
		})
	}
}

// TestUplinkStampConcurrent: stamping while a refresh swaps the stamp is
// race-free and never mixes two stamps in one record. Run with -race.
func TestUplinkStampConcurrent(t *testing.T) {
	f := &fakeUplinks{routes: [][]string{{"eth0", "eth1"}}}
	x := uplinkFixture(t, f)
	x.initUplinkEnrichers(context.Background())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	wg.Go(func() {
		lists := [][]string{{"eth1", "eth0"}, {"eth0", "eth1"}}
		current := []string{"eth0", "eth1"}
		for i := 0; ctx.Err() == nil && i < 2000; i++ {
			f.mu.Lock()
			f.routes = [][]string{lists[i%2]}
			f.calls = 0
			f.mu.Unlock()
			current = x.refreshUplinksOnce(ctx, maxUplinks, current)
		}
	})
	for range 20000 {
		r := &xtcp_flat_record.XtcpFlatRecord{}
		x.applyEnrichment(r)
		if r.Uplink1NicDriver != "drv-"+r.Uplink1Ifname || r.Uplink2NicDriver != "drv-"+r.Uplink2Ifname || r.Uplink1Ifname == r.Uplink2Ifname {
			t.Fatalf("mixed stamp: %q/%q, %q/%q", r.Uplink1Ifname, r.Uplink1NicDriver, r.Uplink2Ifname, r.Uplink2NicDriver)
		}
	}
	cancel()
	wg.Wait()
}

// go test ./pkg/xtcp/ -run '^$' -bench BenchmarkApplyEnrichmentUplink -benchmem
func BenchmarkApplyEnrichmentUplink(b *testing.B) {
	x := new(XTCP)
	s := buildUplinkStamp([]string{"eth0", "eth1"}, []nicinfo.NIC{{Ifname: "eth0", Driver: "mlx5_core"}, {Ifname: "eth1", Driver: "ice"}}, nil)
	x.uplinkStamp.Store(&s)
	r := &xtcp_flat_record.XtcpFlatRecord{}
	b.ReportAllocs()
	for b.Loop() {
		x.applyEnrichment(r)
	}
}
