package linkmonitor

import (
	"container/list"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

func onDemand(c *Config) { c.CollectionMode = CollectionOnDemand }

// pollConfig is DefaultConfig in poll mode, for tests of poll behavior now
// that on demand is the default.
func pollConfig() Config {
	cfg := DefaultConfig()
	cfg.CollectionMode = CollectionPoll
	return cfg
}

func TestCollectionModeConfig(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expected string
		change                                func(*Config)
		wantError                             string
	}{
		{"poll-default", "positive", "defaults", "poll mode", func(*Config) {}, ""},
		{"on-demand-defaults", "positive", "on demand with default intervals", "valid", onDemand, ""},
		{"empty-mode", "boundary", "a configuration predating the mode", "valid, poll", func(c *Config) { c.CollectionMode = "" }, ""},
		{"bad-mode", "negative", "mode lazy", "rejected naming the modes", func(c *Config) { c.CollectionMode = "lazy" }, "invalid collection mode"},
		{"zero-watch", "negative", "on demand, watch interval 0", "rejected", func(c *Config) { onDemand(c); c.WatchInterval = 0 }, "must be positive"},
		{"negative-slow", "negative", "on demand, slow interval -1", "rejected", func(c *Config) { onDemand(c); c.SlowInterval = -1 }, "must be positive"},
		{"zero-wait", "negative", "on demand, wait 0", "rejected", func(c *Config) { onDemand(c); c.OnDemandWait = 0 }, "must be positive"},
		{"negative-ttl", "negative", "on demand, TTL -1", "rejected", func(c *Config) { onDemand(c); c.OnDemandTTL = -1 }, "nonnegative"},
		{"window-below-watch", "negative", "on demand, error window 30 s under the 60 s watch", "rejected naming the watch interval", func(c *Config) { onDemand(c); c.ErrorWindow = 30 * time.Second }, "at least the watch interval"},
		{"flap-below-watch", "negative", "on demand, flap window 59 s", "rejected", func(c *Config) { onDemand(c); c.FlapWindow = 59 * time.Second }, "at least the watch interval"},
		{"window-zero", "boundary", "on demand, error window 0", "valid: the reason is off", func(c *Config) { onDemand(c); c.ErrorWindow = 0 }, ""},
		{"window-equals-watch", "boundary", "error window equal to the watch interval", "valid", func(c *Config) { onDemand(c); c.ErrorWindow = c.WatchInterval }, ""},
		{"ttl-zero", "boundary", "TTL 0", "valid: every detail scrape collects", func(c *Config) { onDemand(c); c.OnDemandTTL = 0 }, ""},
		{"poll-ignores-on-demand", "corner", "poll mode with on-demand fields zero", "valid: unused in poll mode", func(c *Config) { c.WatchInterval, c.SlowInterval, c.OnDemandWait = 0, 0, 0 }, ""},
		{"poll-window-uses-stats", "corner", "poll mode, error window 30 s over the 15 s stats interval", "valid", func(c *Config) { c.ErrorWindow = 30 * time.Second }, ""},
		{"watch-overflow", "corner", "watch interval past MaxInt64/3", "rejected without overflow", func(c *Config) { onDemand(c); c.WatchInterval = math.MaxInt64/3 + 1 }, "exceeds representable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			cfg := pollConfig()
			tc.change(&cfg)
			_, err := validateConfig(cfg)
			if tc.wantError == "" && err != nil || tc.wantError != "" && (err == nil || !strings.Contains(err.Error(), tc.wantError)) {
				t.Fatalf("error %v, want %q", err, tc.wantError)
			}
		})
	}
}

func TestTierInterval(t *testing.T) {
	poll, demand := pollConfig(), pollConfig()
	onDemand(&demand)
	watch := []model.CollectorKind{model.CollectorNetdev, model.CollectorCarrier, model.CollectorDriver, model.CollectorPHY, model.CollectorRDMAState, model.CollectorRDMAErrors}
	for _, tc := range []struct {
		name, category, description, expected string
		cfg                                   Config
		kinds                                 []model.CollectorKind
		want                                  time.Duration
	}{
		{"poll-all", "positive", "poll mode, every kind", "StatsInterval", poll, []model.CollectorKind{model.CollectorNetdev, model.CollectorSettings, model.CollectorNetstat, model.CollectorRDMACounters}, poll.StatsInterval},
		{"on-demand-watch", "positive", "error and state collectors", "WatchInterval", demand, watch, demand.WatchInterval},
		{"on-demand-slow", "positive", "settings and RDMA capabilities", "SlowInterval", demand, []model.CollectorKind{model.CollectorSettings, model.CollectorRDMACapabilities}, demand.SlowInterval},
		{"on-demand-detail", "positive", "netstat and the rest of the RDMA counters", "zero: asked for only", demand, []model.CollectorKind{model.CollectorNetstat, model.CollectorRDMACounters}, 0},
		{"poll-ignores-tiers", "negative", "poll mode with a 1 s watch interval", "StatsInterval regardless", func() Config { c := poll; c.WatchInterval = time.Second; return c }(), watch, poll.StatsInterval},
		{"slow-equals-watch", "boundary", "slow interval equal to the watch interval", "no special case", func() Config { c := demand; c.SlowInterval = c.WatchInterval; return c }(), []model.CollectorKind{model.CollectorSettings}, demand.WatchInterval},
		{"unpolled", "boundary", "inventory, channels, rings and events on demand", "zero: never polled", demand, []model.CollectorKind{model.CollectorInventory, model.CollectorChannels, model.CollectorRings, model.CollectorRDMAEvents}, 0},
		{"unknown-kind", "corner", "a kind past the enum", "zero, no panic", demand, []model.CollectorKind{model.CollectorNetstat + 1}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			for _, kind := range tc.kinds {
				if got := tc.cfg.pollInterval(kind); got != tc.want {
					t.Errorf("kind %d: %v, want %v", kind, got, tc.want)
				}
			}
		})
	}
}

// tieredScheduler is a manual scheduler with on-demand tiers, as prepare
// builds it, with driver statistics and RDMA optional jobs enabled.
func tieredScheduler(t *testing.T, cfg Config) *scheduler {
	t.Helper()
	s, _ := testScheduler(t)
	s.statisticsInterval, s.rdmaInterval = cfg.StatsInterval, cfg.StatsInterval
	if cfg.onDemand() {
		s.tiers = new([model.CollectorNetstat + 1]time.Duration)
		for kind := range s.tiers {
			s.tiers[kind] = cfg.pollInterval(model.CollectorKind(kind))
		}
	}
	return s
}

func TestTierRegistration(t *testing.T) {
	poll, demand := pollConfig(), pollConfig()
	onDemand(&demand)
	for _, tc := range []struct {
		name, category, description, expected string
		cfg                                   Config
		kind                                  model.CollectorKind
		want                                  time.Duration
		optional                              []model.CollectorKind
	}{
		{"driver-watch", "positive", "driver statistics registered on demand", "WatchInterval", demand, model.CollectorDriver, demand.WatchInterval, nil},
		{"driver-poll", "positive", "driver statistics registered in poll mode", "StatsInterval", poll, model.CollectorDriver, poll.StatsInterval, nil},
		{"optional-poll", "negative", "RDMA optional kinds in poll mode", "no separate error-counter job", poll, 0, 0, []model.CollectorKind{model.CollectorRDMACapabilities, model.CollectorRDMACounters}},
		{"optional-on-demand", "positive", "RDMA optional kinds on demand", "the error counters are a job of their own", demand, 0, 0, []model.CollectorKind{model.CollectorRDMACapabilities, model.CollectorRDMACounters, model.CollectorRDMAErrors}},
		{"phy-watch", "boundary", "PHY statistics, the family's second kind", "WatchInterval", demand, model.CollectorPHY, demand.WatchInterval, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			s := tieredScheduler(t, tc.cfg)
			if tc.optional != nil {
				if got := s.optionalKinds(); len(got) != len(tc.optional) || got[len(got)-1] != tc.optional[len(tc.optional)-1] {
					t.Fatalf("optional kinds %v, want %v", got, tc.optional)
				}
				return
			}
			mustObserve(t, s.reducer, observed(s.reducer, 1, true))
			s.refreshStatistics(ethernetKey(1))
			job := s.jobs[model.JobKey{Namespace: 1, Device: ethernetKey(1), Collector: tc.kind}]
			if job == nil || job.policy.interval != tc.want {
				t.Fatalf("job %+v, want interval %v", job, tc.want)
			}
		})
	}
}

func TestTierPolling(t *testing.T) {
	cfg := DefaultConfig()
	onDemand(&cfg)
	for _, tc := range []struct {
		name, category, description, expected string
		kind                                  model.CollectorKind
		interval                              time.Duration
		elapsed                               time.Duration
		wantPending                           bool
	}{
		{"watch-due", "positive", "a watch job two watch intervals on, past its first poll and phase", "requested by its poll", model.CollectorDriver, cfg.WatchInterval, 2 * cfg.WatchInterval, true},
		{"detail-no-periodic", "negative", "netstat on demand after an hour", "never requested by a poll", model.CollectorNetstat, 0, time.Hour, false},
		{"watch-not-yet", "boundary", "a watch job before its phase can come round", "not requested", model.CollectorDriver, cfg.WatchInterval, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			s, clock := testScheduler(t)
			key := registerTestJob(t, s, 1, tc.kind, schedulePolicy{interval: tc.interval})
			dispatchTest(t, s)
			finishTest(t, s, 0) // the startup collection
			advanceSchedulerClock(t, clock, tc.elapsed)
			s.expire(clock.Now())
			if got := s.jobs[key].pending; got != tc.wantPending {
				t.Fatalf("pending %v, want %v", got, tc.wantPending)
			}
		})
	}
	t.Run("phase-spread", func(t *testing.T) {
		t.Log("corner: 100 devices on the 60 s watch interval; expected: polls spread, not bunched at one instant")
		phases := make(map[time.Duration]bool)
		for i := range uint32(100) {
			phases[pollPhase(ethernetKey(i+1), cfg.WatchInterval)/time.Second] = true
		}
		if len(phases) < 40 {
			t.Fatalf("100 devices fall on %d distinct seconds of 60", len(phases))
		}
	})
}

func TestTierZeroGuards(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expected string
		rdma                                  bool
	}{
		{"traffic", "boundary", "traffic schedule with interval 0", "no periodic sweep or deadline; an explicit request still queues", false},
		{"rdma", "boundary", "RDMA state schedule with interval 0", "no divide by zero, no periodic request", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			s, clock := testScheduler(t)
			advanceSchedulerClock(t, clock, time.Hour)
			if tc.rdma {
				rs := newRDMASchedule(s.reducer, nil, 0)
				rs.ports[ethernetKey(1)] = []model.RDMAPort{{Device: "mlx5_0", Port: 1}}
				if err := rs.advance(clock.Now()); err != nil || len(rs.queue) != 0 {
					t.Fatalf("queue %v, %v", rs.queue, err)
				}
				return
			}
			tr := &trafficSchedule{scheduler: s, pending: make(map[model.DeviceKey]*list.Element)}
			if err := tr.advance(clock.Now()); err != nil || tr.bulk {
				t.Fatalf("bulk %v, %v", tr.bulk, err)
			}
			if _, set := s.reducer.deadlines.index[deadlineKey{kind: deadlineTrafficPoll}]; set {
				t.Fatal("zero interval armed a traffic poll")
			}
			mustObserve(t, s.reducer, observed(s.reducer, 1, true))
			tr.request(ethernetKey(1), 0)
			if tr.queue.Len() != 1 {
				t.Fatal("explicit request not queued")
			}
		})
	}
}

func TestOnDemandFreshness(t *testing.T) {
	cfg := DefaultConfig()
	onDemand(&cfg)
	y := cfg.WatchInterval
	for _, tc := range []struct {
		name, category, description, expected string
		kind                                  model.CollectorKind
		at                                    time.Duration
		wantFresh                             bool
	}{
		{"detail-never-stale", "positive", "netstat-style detail kind (RDMA counters) read an hour ago", "fresh: nothing expires it unread", model.CollectorRDMACounters, time.Hour, true},
		{"watch-fresh", "positive", "driver statistics 2Y after their read", "fresh", model.CollectorDriver, 2 * y, true},
		{"watch-at-3y", "boundary", "driver statistics exactly 3Y after", "expired: values expire at their exact deadline", model.CollectorDriver, 3 * y, false},
		{"watch-before-3y", "boundary", "driver statistics 1 ns before 3Y", "fresh", model.CollectorDriver, 3*y - 1, true},
		{"watch-stale", "negative", "driver statistics missing for 3Y + 1 s", "expired", model.CollectorDriver, 3*y + time.Second, false},
		{"slow-at-3z", "corner", "settings exactly 3Z after", "expired on the slow tier's lifetime", model.CollectorSettings, 3 * cfg.SlowInterval, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			h := newProblemHarness(t, onDemand)
			h.r.freshness = newFreshnessPolicy(cfg)
			h.device(1, true)
			h.collect(1, tc.kind, errCounter("receive_errors_total", 1))
			h.r.expire(tc.at)
			state := &slotAt(h.r, 1).collectors[tc.kind]
			if state.fresh != tc.wantFresh {
				t.Fatalf("fresh %v, want %v", state.fresh, tc.wantFresh)
			}
		})
	}
	for _, tc := range []struct {
		name, category, description, expected string
		gap                                   time.Duration
		increase                              bool
		want                                  Problems
	}{
		{"error-at-y", "positive", "CRC rises between two watch polls Y apart", "errors on the summary", y, true, ProblemErrors},
		{"no-error", "negative", "CRC flat between two watch polls", "no problem", y, false, 0},
		{"first-on-demand", "boundary", "first read only", "anchors only: no problem", 0, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			h := newProblemHarness(t, onDemand).healthy()
			value := uint64(10)
			if tc.gap != 0 {
				h.at(time.Minute).collect(1, model.CollectorNetdev, errCounter("receive_crc_errors_total", value))
			}
			if tc.increase {
				value++
			}
			h.at(time.Minute+tc.gap).collect(1, model.CollectorNetdev, errCounter("receive_crc_errors_total", value))
			h.settings(1, model.CheckPass)
			h.publish()
			want := map[string]Problems{}
			if tc.want != 0 {
				want["eth1"] = tc.want
			}
			if got := h.scrape(time.Minute + tc.gap); !mapsEqual(got, want) {
				t.Fatalf("problems %v, want %v", got, want)
			}
		})
	}
	t.Run("grace", func(t *testing.T) {
		t.Log("corner: on demand the unknown-check grace is Settle plus the slow interval; expected: that, not Settle plus the stats interval")
		p, err := newProblemPolicy(cfg)
		if err != nil || p.grace != cfg.Settle+cfg.SlowInterval {
			t.Fatalf("grace %v, %v; want %v", p.grace, err, cfg.Settle+cfg.SlowInterval)
		}
	})
}
