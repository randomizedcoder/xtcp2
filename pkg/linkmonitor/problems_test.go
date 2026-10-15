package linkmonitor

import (
	"errors"
	"math"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

// problemBase anchors harness wall time: wall = problemBase + monotonic.
var problemBase = time.Unix(1_000, 0)

// problemHarness drives the real reducer and publication with an explicit
// clock, so windows are tested without sleeping.
type problemHarness struct {
	t              testing.TB
	r              *reducer
	m              *Monitor
	now            time.Duration
	baselineErrors uint64
}

func newProblemHarness(t testing.TB, mutate func(*Config)) *problemHarness {
	t.Helper()
	cfg := pollConfig()
	if mutate != nil {
		mutate(&cfg)
	}
	policy, err := newProblemPolicy(cfg)
	if err != nil {
		t.Fatal(err)
	}
	r := newReducer(1)
	r.problems = policy
	return &problemHarness{t: t, r: r, m: new(Monitor)}
}

func (h *problemHarness) stamp() model.Stamp {
	return model.Stamp{Wall: problemBase.Add(h.now), Monotonic: h.now}
}

func (h *problemHarness) at(now time.Duration) *problemHarness { h.now = now; return h }

func ethernetKey(index uint32) model.DeviceKey {
	return model.DeviceKey{Namespace: 1, Kind: model.DeviceEthernet, Index: index}
}

func (h *problemHarness) device(index uint32, up bool) {
	h.t.Helper()
	observation := observed(h.r, index, up)
	observation.Observed = h.stamp()
	if _, err := h.r.observe(observation); err != nil {
		h.t.Fatal(err)
	}
}

func (h *problemHarness) excluded(index uint32) {
	h.t.Helper()
	observation := observed(h.r, index, true)
	observation.Device.Eligibility = model.Excluded
	observation.Observed = h.stamp()
	if _, err := h.r.observe(observation); err != nil {
		h.t.Fatal(err)
	}
}

func (h *problemHarness) result(key model.JobKey, result model.Result) model.Job {
	h.t.Helper()
	job, err := h.r.startCollection(key, h.stamp())
	if err != nil {
		h.t.Fatal(err)
	}
	result.Job, result.Finished = job, h.stamp()
	if result.Support == 0 {
		result.Support = model.Supported
	}
	if _, err := h.r.finishCollection(result); err != nil {
		h.t.Fatal(err)
	}
	return job
}

// settings installs a speed check result as the scheduler does.
func (h *problemHarness) settings(index uint32, speed model.Check) {
	h.t.Helper()
	active := presentValue(uint64(10_000_000_000))
	checks := &model.SettingsChecks{Speed: speed, Duplex: model.CheckPass, ActiveBits: active}
	job := h.result(model.JobKey{Namespace: 1, Device: ethernetKey(index), Collector: model.CollectorSettings}, model.Result{Settings: checks})
	if !h.r.setChecks(job, deviceChecks{maximumSpeed: speed, fullDuplex: model.CheckPass, activeBits: active}) {
		h.t.Fatal("settings checks rejected")
	}
}

func (h *problemHarness) collect(index uint32, kind model.CollectorKind, samples ...model.Sample) {
	h.t.Helper()
	h.result(model.JobKey{Namespace: 1, Device: ethernetKey(index), Collector: kind}, model.Result{Samples: samples})
}

func (h *problemHarness) fail(index uint32, kind model.CollectorKind) {
	h.t.Helper()
	key := model.JobKey{Namespace: 1, Device: ethernetKey(index), Collector: kind}
	if index == 0 {
		key.Device = model.DeviceKey{}
	}
	h.result(key, model.Result{Err: errors.New("injected"), Reason: model.ErrorReason(1)})
}

func (h *problemHarness) publish() {
	h.t.Helper()
	if !h.r.recordResync(model.Token{SourceEpoch: h.r.epoch, Revision: h.r.revision}, h.stamp()) {
		h.t.Fatal("resync rejected")
	}
	state := publicationState{expected: presentValue(uint64(len(h.r.slots))), now: h.stamp(), baselineWriteErrors: h.baselineErrors}
	if err := h.r.publish(h.m, state); err != nil {
		h.t.Fatal(err)
	}
}

// scrape returns the active problems at monotonic time at; the host is "".
func (h *problemHarness) scrape(at time.Duration) map[string]Problems {
	out := make(map[string]Problems)
	s := h.m.Snapshot()
	now := problemBase.Add(at)
	s.RangeProblems(now, func(name string, p Problems) bool {
		out[name] = p
		return true
	})
	if host := s.HostProblems(now); host != 0 {
		out[""] = host
	}
	return out
}

// healthy creates eth1 up with passing checks.
func (h *problemHarness) healthy() *problemHarness {
	h.device(1, true)
	h.settings(1, model.CheckPass)
	return h
}

func errCounter(name string, value uint64) model.Sample {
	return model.Sample{Descriptor: "go_link_monitor_interface_" + name, Kind: model.SampleCounter, Number: model.Unsigned(value), Counter: model.CounterIdentity{Source: "kernel", Width: 64}}
}

func statistic(name string, value uint64) model.Sample {
	return model.Sample{Descriptor: "ethtool_statistic", Kind: model.SampleUntyped, Number: model.Unsigned(value),
		Labels: []model.Label{{Name: "encoding", Value: "utf8"}, {Name: "statistic", Value: name}}}
}

func TestProblemNames(t *testing.T) {
	var all Problems = 1<<problemReasons - 1
	var got []string
	all.Range(func(reason string) bool { got = append(got, reason); return true })
	if !slices.Equal(got, ProblemReasonNames()) || all.Count() != problemReasons {
		t.Fatalf("reasons = %v", got)
	}
	stopped := 0
	all.Range(func(string) bool { stopped++; return false })
	if stopped != 1 {
		t.Fatalf("Range ignored stop after %d visits", stopped)
	}
}

func TestProblemPolicyConfig(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expected string
		mutate                                func(*Config)
		valid                                 bool
		enabled                               []evidenceClass
	}{
		{"defaults", "positive", "default windows and classes", "link, rdma and flap enabled", nil, true, []evidenceClass{evidenceLink, evidenceRDMA, evidenceFlap}},
		{"drop", "positive", "link,rdma,drop classes", "drop also enabled", func(c *Config) { c.ErrorClasses = []string{"link", "rdma", "drop"} }, true, []evidenceClass{evidenceLink, evidenceRDMA, evidenceDrop, evidenceFlap}},
		{"no classes", "boundary", "empty class list", "only flap enabled", func(c *Config) { c.ErrorClasses = nil }, true, []evidenceClass{evidenceFlap}},
		{"flap off", "boundary", "flap window zero", "flap class disabled", func(c *Config) { c.FlapWindow = 0 }, true, []evidenceClass{evidenceLink, evidenceRDMA}},
		{"errors off", "boundary", "error window zero", "accepted", func(c *Config) { c.ErrorWindow = 0 }, true, []evidenceClass{evidenceLink, evidenceRDMA, evidenceFlap}},
		{"equals stats", "boundary", "window equals the stats interval", "accepted", func(c *Config) { c.ErrorWindow = c.StatsInterval }, true, []evidenceClass{evidenceLink, evidenceRDMA, evidenceFlap}},
		{"below stats", "negative", "flap window one nanosecond below the stats interval", "rejected", func(c *Config) { c.FlapWindow = c.StatsInterval - 1 }, false, nil},
		{"negative", "negative", "negative error window", "rejected", func(c *Config) { c.ErrorWindow = -time.Second }, false, nil},
		{"unknown class", "negative", "class bogus", "rejected", func(c *Config) { c.ErrorClasses = []string{"link", "bogus"} }, false, nil},
		{"flap class", "negative", "flap is not a selectable error class", "rejected", func(c *Config) { c.ErrorClasses = []string{"flap"} }, false, nil},
		{"duplicate class", "negative", "link listed twice", "rejected", func(c *Config) { c.ErrorClasses = []string{"link", "link"} }, false, nil},
		{"bad exclude", "negative", "unbalanced exclude regexp", "rejected", func(c *Config) { c.ErrorExclude = "(" }, false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			cfg := pollConfig()
			if tc.mutate != nil {
				tc.mutate(&cfg)
			}
			p, err := newProblemPolicy(cfg)
			if (err == nil) != tc.valid {
				t.Fatalf("error = %v", err)
			}
			if _, err := validateConfig(cfg); (err == nil) != tc.valid {
				t.Fatalf("validateConfig error = %v", err)
			}
			for class := evidenceNone; class < evidenceClasses; class++ {
				if want := slices.Contains(tc.enabled, class); tc.valid && p.enabled(class) != want {
					t.Fatalf("class %d enabled = %v, want %v", class, p.enabled(class), want)
				}
			}
		})
	}
}

func TestProblemExcludeAnchored(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ErrorExclude = `go_link_monitor_interface_receive_errors_total@eth1`
	p, err := newProblemPolicy(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, category, description, key string
		expected                         bool
	}{
		{"exact", "positive", "exact series key", "go_link_monitor_interface_receive_errors_total@eth1", true},
		{"suffix", "negative", "same key on eth10", "go_link_monitor_interface_receive_errors_total@eth10", false},
		{"prefix", "negative", "key with a leading prefix", "x_go_link_monitor_interface_receive_errors_total@eth1", false},
	} {
		t.Logf("%s: %s; expected match=%v", tc.category, tc.description, tc.expected)
		if got := p.exclude.MatchString(tc.key); got != tc.expected {
			t.Fatalf("%s: match = %v", tc.name, got)
		}
	}
}

func TestClassifyEvidence(t *testing.T) {
	stat := func(name string) []model.Label { return []model.Label{{Name: "statistic", Value: name}} }
	for _, tc := range []struct {
		name, category, description, metric string
		labels                              []model.Label
		expected                            evidenceClass
	}{
		{"crc", "positive", "netdev CRC errors", "go_link_monitor_interface_receive_crc_errors_total", nil, evidenceLink},
		{"tx errors", "positive", "netdev transmit errors", "go_link_monitor_interface_transmit_errors_total", nil, evidenceLink},
		{"rx packets", "negative", "netdev packets", "go_link_monitor_interface_receive_packets_total", nil, evidenceNone},
		{"rx dropped", "positive", "netdev drops", "go_link_monitor_interface_receive_dropped_total", nil, evidenceDrop},
		{"carrier down", "positive", "carrier down changes", "go_link_monitor_interface_carrier_down_changes_total", nil, evidenceFlap},
		{"carrier up", "negative", "carrier up changes", "go_link_monitor_interface_carrier_up_changes_total", nil, evidenceNone},
		{"ib downed", "positive", "IB link downed", "go_link_monitor_infiniband_link_downed_total", nil, evidenceFlap},
		{"ib symbol", "positive", "IB symbol errors", "go_link_monitor_infiniband_symbol_error_total", nil, evidenceRDMA},
		{"ib discards", "positive", "IB transmit discards", "go_link_monitor_infiniband_port_xmit_discards_total", nil, evidenceRDMA},
		{"ib vl15", "corner", "VL15 dropped is RDMA, not drop", "go_link_monitor_infiniband_vl15_dropped_total", nil, evidenceRDMA},
		{"ib data", "negative", "IB data counter", "go_link_monitor_infiniband_port_rcv_data_total", nil, evidenceNone},
		{"ib gauge", "negative", "IB name without _total", "go_link_monitor_infiniband_symbol_error", nil, evidenceNone},
		{"ethtool fcs", "positive", "ethtool rx_fcs_errors", "go_link_monitor_ethtool_statistic", stat("rx_fcs_errors"), evidenceLink},
		{"ethtool packets", "negative", "ethtool rx_packets", "go_link_monitor_ethtool_statistic", stat("rx_packets"), evidenceNone},
		{"ethtool drop", "positive", "ethtool rx_discards", "go_link_monitor_ethtool_statistic", stat("rx_discards"), evidenceDrop},
		{"ethtool error wins", "corner", "rx_dropped_errors matches both patterns", "go_link_monitor_ethtool_statistic", stat("rx_dropped_errors"), evidenceLink},
		{"ethtool case", "corner", "upper-case CRC", "go_link_monitor_ethtool_statistic", stat("RX_CRC"), evidenceLink},
		{"phy symbol", "positive", "phy symbol error", "go_link_monitor_phy_statistic", stat("phy_symbol_err"), evidenceLink},
		{"no statistic", "boundary", "ethtool sample without statistic label", "go_link_monitor_ethtool_statistic", nil, evidenceNone},
		{"netstat", "negative", "netstat error fields are the reserved host class", "go_link_monitor_netstat_Tcp_InErrs", nil, evidenceNone},
		{"empty", "boundary", "empty name", "", nil, evidenceNone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected class %d", tc.category, tc.description, tc.expected)
			if got := classifyEvidence(tc.metric, tc.labels); got != tc.expected {
				t.Fatalf("class = %d", got)
			}
		})
	}
}

func TestStaticProblems(t *testing.T) {
	const grace = 30*time.Second + 15*time.Second
	for _, tc := range []struct {
		name, category, description, expected string
		setup                                 func(*problemHarness)
		scrape                                time.Duration
		want                                  map[string]Problems
	}{
		{"speed-fail", "positive", "up interface below maximum speed", "max_speed", func(h *problemHarness) {
			h.device(1, true)
			h.settings(1, model.CheckFail)
		}, 0, map[string]Problems{"eth1": ProblemMaxSpeed}},
		{"speed-pass", "negative", "up interface, checks pass", "no problems", func(h *problemHarness) { h.healthy() }, grace, map[string]Problems{}},
		{"speed-excepted", "negative", "max_speed fail covered by a bare-name exception", "no problems", func(h *problemHarness) {
			h.device(1, true)
			h.settings(1, model.CheckFail)
			h.r.exceptions = resolveExceptions([]string{"eth1"}, h.r.rdmaExceptionDevices())
		}, 0, map[string]Problems{}},
		{"down-interface", "negative", "interface down", "no interface problems", func(h *problemHarness) { h.device(1, false) }, grace, map[string]Problems{}},
		{"excluded-interface", "negative", "excluded interface with no checks", "no problems", func(h *problemHarness) { h.excluded(1) }, grace, map[string]Problems{}},
		{"excluded-with-evidence", "corner", "excluded interface goes down", "no problems despite flap evidence", func(h *problemHarness) {
			h.excluded(1)
			observation := observed(h.r, 1, false)
			observation.Device.Eligibility = model.Excluded
			if _, err := h.r.observe(observation); err != nil {
				h.t.Fatal(err)
			}
		}, 0, map[string]Problems{}},
		{"unknown-inside-grace", "boundary", "up, no settings, one nanosecond before grace", "no problems", func(h *problemHarness) { h.device(1, true) }, grace - 1, map[string]Problems{}},
		{"unknown-at-grace", "boundary", "up, no settings, exactly at grace", "check_unknown", func(h *problemHarness) { h.device(1, true) }, grace, map[string]Problems{"eth1": ProblemCheckUnknown}},
		{"unknown-after-up", "corner", "came up at 100s; grace restarts", "no problems at 100s+grace-1", func(h *problemHarness) {
			h.device(1, false)
			h.at(100*time.Second).device(1, true)
		}, 100*time.Second + grace - 1, map[string]Problems{}},
		{"unknown-excepted", "corner", "excepted interface with unknown checks", "check_unknown still reported", func(h *problemHarness) {
			h.device(1, true)
			h.r.exceptions = resolveExceptions([]string{"eth1"}, h.r.rdmaExceptionDevices())
		}, grace, map[string]Problems{"eth1": ProblemCheckUnknown}},
		{"failed-attempt", "positive", "driver source failed", "collector", func(h *problemHarness) {
			h.healthy().fail(1, model.CollectorDriver)
		}, 0, map[string]Problems{"eth1": ProblemCollector}},
		{"collector-down", "negative", "driver failed on a down interface", "no interface problems", func(h *problemHarness) {
			h.device(1, false)
			h.fail(1, model.CollectorDriver)
		}, 0, map[string]Problems{}},
		{"stale-before-limit", "boundary", "driver success 3 intervals - 1ns ago", "no problems", func(h *problemHarness) {
			h.healthy().collect(1, model.CollectorDriver, statistic("rx_packets", 1))
			h.at(45*time.Second-1).settings(1, model.CheckPass)
		}, 45*time.Second - 1, map[string]Problems{}},
		{"stale-at-limit", "boundary", "driver success exactly 3 intervals ago", "collector (freshness expires at the deadline)", func(h *problemHarness) {
			h.healthy().collect(1, model.CollectorDriver, statistic("rx_packets", 1))
			h.at(45*time.Second).settings(1, model.CheckPass)
		}, 45 * time.Second, map[string]Problems{"eth1": ProblemCollector}},
		{"unsupported", "negative", "phy unsupported", "no problems", func(h *problemHarness) {
			h.healthy().result(model.JobKey{Namespace: 1, Device: ethernetKey(1), Collector: model.CollectorPHY}, model.Result{Support: model.Unsupported})
		}, 0, map[string]Problems{}},
		{"two-collectors-failed", "corner", "driver and rings failing", "one collector reason", func(h *problemHarness) {
			h.healthy().fail(1, model.CollectorDriver)
			h.fail(1, model.CollectorRings)
		}, 0, map[string]Problems{"eth1": ProblemCollector}},
		{"multiple-reasons", "corner", "speed fail, flap and failed collector", "three reasons", func(h *problemHarness) {
			h.device(1, true)
			h.settings(1, model.CheckFail)
			h.fail(1, model.CollectorDriver)
			h.device(1, false)
			h.device(1, true)
			h.settings(1, model.CheckFail)
		}, 0, map[string]Problems{"eth1": ProblemMaxSpeed | ProblemFlap | ProblemCollector}},
		{"netstat-failed", "positive", "host netstat read fails", "host collector", func(h *problemHarness) {
			h.healthy().fail(0, model.CollectorNetstat)
		}, 0, map[string]Problems{"": ProblemCollector}},
		{"exception-unmatched", "positive", "selector names a missing interface", "host exception_unmatched", func(h *problemHarness) {
			h.healthy()
			h.r.exceptions = resolveExceptions([]string{"eth9"}, h.r.rdmaExceptionDevices())
		}, 0, map[string]Problems{"": ProblemExceptionUnmatched}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			h := newProblemHarness(t, nil)
			tc.setup(h)
			h.publish()
			if got := h.scrape(tc.scrape); !mapsEqual(got, tc.want) {
				t.Fatalf("problems = %v, want %v", got, tc.want)
			}
		})
	}
}

func mapsEqual(a, b map[string]Problems) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

func TestErrorEvidenceWindow(t *testing.T) {
	const w = defaultProblemWindow
	const t0 = 60 * time.Second
	identity := func(s model.Sample) model.Sample { s.Counter.Lifetime = 2; return s }
	for _, tc := range []struct {
		name, category, description, expected string
		mutate                                func(*Config)
		before, after                         []model.Sample
		extra                                 func(*problemHarness)
		scrape                                time.Duration
		want                                  Problems
	}{
		{"crc-increase", "positive", "CRC 10 to 11 at t", "errors", nil, []model.Sample{errCounter("receive_crc_errors_total", 10)}, []model.Sample{errCounter("receive_crc_errors_total", 11)}, nil, t0, ProblemErrors},
		{"no-change", "negative", "CRC 10 to 10", "none", nil, []model.Sample{errCounter("receive_crc_errors_total", 10)}, []model.Sample{errCounter("receive_crc_errors_total", 10)}, nil, t0, 0},
		{"first-sample", "negative", "first sample 1e6 only anchors", "none", nil, nil, []model.Sample{errCounter("receive_crc_errors_total", 1e6)}, nil, t0, 0},
		{"reset", "negative", "100 to 3", "none", nil, []model.Sample{errCounter("receive_errors_total", 100)}, []model.Sample{errCounter("receive_errors_total", 3)}, nil, t0, 0},
		{"wrap-max", "corner", "2^64-1 to 0", "none", nil, []model.Sample{errCounter("receive_errors_total", math.MaxUint64)}, []model.Sample{errCounter("receive_errors_total", 0)}, nil, t0, 0},
		{"identity-change", "negative", "new counter lifetime with higher value", "none", nil, []model.Sample{errCounter("receive_errors_total", 1)}, []model.Sample{identity(errCounter("receive_errors_total", 2))}, nil, t0, 0},
		{"schema-change", "corner", "new series added while CRC increases", "errors", nil, []model.Sample{errCounter("receive_crc_errors_total", 1)}, []model.Sample{errCounter("receive_crc_errors_total", 2), errCounter("receive_packets_total", 5)}, nil, t0, ProblemErrors},
		{"ethtool-pattern", "positive", "rx_fcs_errors increases", "errors", nil, []model.Sample{statistic("rx_fcs_errors", 1)}, []model.Sample{statistic("rx_fcs_errors", 2)}, nil, t0, ProblemErrors},
		{"ethtool-non-error", "negative", "rx_packets increases", "none", nil, []model.Sample{statistic("rx_packets", 1)}, []model.Sample{statistic("rx_packets", 2)}, nil, t0, 0},
		{"excluded-series", "negative", "excluded receive_errors on eth1", "none", func(c *Config) { c.ErrorExclude = `go_link_monitor_interface_receive_errors_total@eth1` }, []model.Sample{errCounter("receive_errors_total", 1)}, []model.Sample{errCounter("receive_errors_total", 2)}, nil, t0, 0},
		{"excluded-other", "negative", "exclusion of a different series", "errors", func(c *Config) { c.ErrorExclude = `.*@eth2` }, []model.Sample{errCounter("receive_errors_total", 1)}, []model.Sample{errCounter("receive_errors_total", 2)}, nil, t0, ProblemErrors},
		{"excluded-statistic", "negative", "excluded ethtool statistic key", "none", func(c *Config) { c.ErrorExclude = `go_link_monitor_ethtool_statistic:rx_fcs_errors@eth1` }, []model.Sample{statistic("rx_fcs_errors", 1)}, []model.Sample{statistic("rx_fcs_errors", 2)}, nil, t0, 0},
		{"drop-class-off", "negative", "rx_dropped increases, drop disabled", "none", nil, []model.Sample{errCounter("receive_dropped_total", 1)}, []model.Sample{errCounter("receive_dropped_total", 2)}, nil, t0, 0},
		{"drop-class-on", "positive", "rx_dropped increases, drop enabled", "errors", func(c *Config) { c.ErrorClasses = []string{"link", "rdma", "drop"} }, []model.Sample{errCounter("receive_dropped_total", 1)}, []model.Sample{errCounter("receive_dropped_total", 2)}, nil, t0, ProblemErrors},
		{"link-class-off", "negative", "CRC increases with only rdma enabled", "none", func(c *Config) { c.ErrorClasses = []string{"rdma"} }, []model.Sample{errCounter("receive_crc_errors_total", 1)}, []model.Sample{errCounter("receive_crc_errors_total", 2)}, nil, t0, 0},
		{"window-inside", "positive", "scrape at t+W-1ns", "errors", nil, []model.Sample{errCounter("receive_errors_total", 1)}, []model.Sample{errCounter("receive_errors_total", 2)}, nil, t0 + w - 1, ProblemErrors},
		{"window-edge", "boundary", "scrape at t+W", "errors (inclusive)", nil, []model.Sample{errCounter("receive_errors_total", 1)}, []model.Sample{errCounter("receive_errors_total", 2)}, nil, t0 + w, ProblemErrors},
		{"window-after", "boundary", "scrape at t+W+1ns", "none without republishing", nil, []model.Sample{errCounter("receive_errors_total", 1)}, []model.Sample{errCounter("receive_errors_total", 2)}, nil, t0 + w + 1, 0},
		{"window-refresh", "corner", "second increase at t+W/2", "errors at t+W+1ns", nil, []model.Sample{errCounter("receive_errors_total", 1)}, []model.Sample{errCounter("receive_errors_total", 2)}, func(h *problemHarness) {
			h.at(t0+w/2).collect(1, model.CollectorNetdev, errCounter("receive_errors_total", 3))
		}, t0 + w + 1, ProblemErrors},
		{"window-zero", "boundary", "error window zero", "none", func(c *Config) { c.ErrorWindow = 0 }, []model.Sample{errCounter("receive_errors_total", 1)}, []model.Sample{errCounter("receive_errors_total", 2)}, nil, t0, 0},
		{"flap-not-error", "negative", "carrier down counter increases", "flap only", nil, []model.Sample{errCounter("carrier_down_changes_total", 4)}, []model.Sample{errCounter("carrier_down_changes_total", 5)}, nil, t0, ProblemFlap},
		{"carrier-up-only", "negative", "carrier up counter increases", "none", nil, []model.Sample{errCounter("carrier_up_changes_total", 4)}, []model.Sample{errCounter("carrier_up_changes_total", 5)}, nil, t0, 0},
		{"flap-window-zero", "boundary", "carrier down with flap window zero", "none", func(c *Config) { c.FlapWindow = 0 }, []model.Sample{errCounter("carrier_down_changes_total", 4)}, []model.Sample{errCounter("carrier_down_changes_total", 5)}, nil, t0, 0},
		{"flap-default-after", "boundary", "carrier down, scrape at t+180s+1ns", "none", nil, []model.Sample{errCounter("carrier_down_changes_total", 4)}, []model.Sample{errCounter("carrier_down_changes_total", 5)}, nil, t0 + w + 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			h := newProblemHarness(t, tc.mutate).healthy()
			if tc.before != nil {
				h.at(t0-15*time.Second).collect(1, model.CollectorNetdev, tc.before...)
			}
			h.at(t0).collect(1, model.CollectorNetdev, tc.after...)
			if tc.extra != nil {
				tc.extra(h)
			}
			h.settings(1, model.CheckPass)
			h.publish()
			want := map[string]Problems{}
			if tc.want != 0 {
				want["eth1"] = tc.want
			}
			if got := h.scrape(tc.scrape); !mapsEqual(got, want) {
				t.Fatalf("problems = %v, want %v", got, want)
			}
		})
	}
}

func TestFlapEvidence(t *testing.T) {
	const t0 = 60 * time.Second
	for _, tc := range []struct {
		name, category, description, expected string
		mutate                                func(*Config)
		setup                                 func(*problemHarness)
		scrape                                time.Duration
		want                                  Problems
	}{
		{"oper-down-event", "positive", "up to down at t", "flap", nil, func(h *problemHarness) {
			h.healthy().at(t0).device(1, false)
		}, t0, ProblemFlap},
		{"flap-and-back-up", "corner", "down then up again", "flap while up", nil, func(h *problemHarness) {
			h.healthy().at(t0).device(1, false)
			h.device(1, true)
			h.settings(1, model.CheckPass)
		}, t0 + time.Second, ProblemFlap},
		{"initial-inventory-down", "negative", "already down at startup", "none", nil, func(h *problemHarness) { h.device(1, false) }, t0, 0},
		{"up-transition", "negative", "down to up only", "none", nil, func(h *problemHarness) {
			h.device(1, false)
			h.at(t0).device(1, true)
			h.settings(1, model.CheckPass)
		}, t0, 0},
		{"flap-default-window", "boundary", "scrape at t+180s", "flap", nil, func(h *problemHarness) {
			h.healthy().at(t0).device(1, false)
		}, t0 + defaultProblemWindow, ProblemFlap},
		{"flap-default-after", "boundary", "scrape at t+180s+1ns", "none", nil, func(h *problemHarness) {
			h.healthy().at(t0).device(1, false)
		}, t0 + defaultProblemWindow + 1, 0},
		{"flap-window-zero", "boundary", "flap window zero", "none", func(c *Config) { c.FlapWindow = 0 }, func(h *problemHarness) {
			h.healthy().at(t0).device(1, false)
		}, t0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			h := newProblemHarness(t, tc.mutate)
			tc.setup(h)
			h.publish()
			want := map[string]Problems{}
			if tc.want != 0 {
				want["eth1"] = tc.want
			}
			if got := h.scrape(tc.scrape); !mapsEqual(got, want) {
				t.Fatalf("problems = %v, want %v", got, want)
			}
			if tc.name == "flap-window-zero" {
				if _, ok := h.m.Snapshot().deviceAt(1).LastFlap(); !ok {
					t.Fatal("last flap time not kept with the window disabled")
				}
			}
		})
	}
}

func (s Snapshot) deviceAt(index uint32) (view DeviceView) {
	s.RangeDevices(func(d DeviceView) bool {
		if d.identity == "netdev:"+strconv.FormatUint(uint64(index), 10) {
			view = d
			return false
		}
		return true
	})
	return view
}

func TestBaselineWriteProblem(t *testing.T) {
	const t0 = 60 * time.Second
	for _, tc := range []struct {
		name, category, description, expected string
		mutate                                func(*Config)
		scrape                                time.Duration
		want                                  Problems
	}{
		{"inside", "positive", "save fails at t, scrape at t", "baseline_write", nil, t0, ProblemBaselineWrite},
		{"edge", "boundary", "scrape at t+W", "baseline_write", nil, t0 + defaultProblemWindow, ProblemBaselineWrite},
		{"after", "boundary", "scrape at t+W+1ns", "none", nil, t0 + defaultProblemWindow + 1, 0},
		{"window-zero", "boundary", "error window zero", "none", func(c *Config) { c.ErrorWindow = 0 }, t0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			h := newProblemHarness(t, tc.mutate).healthy()
			h.publish()
			h.baselineErrors = 1
			h.at(t0).settings(1, model.CheckPass)
			h.publish()
			// A later publication without new errors keeps the original time.
			h.at(t0+time.Second).settings(1, model.CheckPass)
			h.publish()
			want := map[string]Problems{}
			if tc.want != 0 {
				want[""] = tc.want
			}
			if got := h.scrape(tc.scrape); !mapsEqual(got, want) {
				t.Fatalf("problems = %v, want %v", got, want)
			}
		})
	}
}

func TestProblemCandidates(t *testing.T) {
	h := newProblemHarness(t, nil)
	for i := uint32(1); i <= 40; i++ {
		h.device(i, true)
		h.settings(i, model.CheckPass)
	}
	h.publish()
	if got := h.m.Snapshot().root.candidates; slices.ContainsFunc(got, func(m uint32) bool { return m != 0 }) {
		t.Fatalf("healthy candidates = %v", got)
	}
	h.at(10*time.Second).device(35, false)
	h.publish()
	first := h.m.Snapshot().root.candidates
	if len(first) != 2 || first[0] != 0 || first[1] == 0 {
		t.Fatalf("flap candidates = %v", first)
	}
	h.at(20 * time.Second).publish()
	if second := h.m.Snapshot().root.candidates; &second[0] != &first[0] {
		t.Fatal("unchanged candidates were copied")
	}
	h.at(10*time.Second + defaultProblemWindow + 1)
	for i := uint32(1); i <= 40; i++ {
		h.settings(i, model.CheckPass)
	}
	h.publish()
	if got := h.m.Snapshot().root.candidates; got[1] != 0 {
		t.Fatalf("expired evidence kept candidate: %v", got)
	}
}

func TestProblemRename(t *testing.T) {
	h := newProblemHarness(t, nil)
	h.device(1, true)
	h.settings(1, model.CheckFail)
	h.publish()
	observation := observed(h.r, 1, true)
	observation.Device.Name = "lan0"
	if _, err := h.r.observe(observation); err != nil {
		t.Fatal(err)
	}
	h.settings(1, model.CheckFail)
	h.publish()
	if got := h.scrape(0); !mapsEqual(got, map[string]Problems{"lan0": ProblemMaxSpeed}) {
		t.Fatalf("problems after rename = %v", got)
	}
}

func TestReducerEvidenceNoAllocs(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expected string
		exclude                               string
	}{
		{"no-exclude", "positive", "steady results without exclusions", "zero allocations", ""},
		{"exclude", "positive", "steady results with a cached exclusion", "zero allocations after the first", `.*crc.*`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			h := newProblemHarness(t, func(c *Config) { c.ErrorExclude = tc.exclude }).healthy()
			key := model.JobKey{Namespace: 1, Device: ethernetKey(1), Collector: model.CollectorNetdev}
			samples := []model.Sample{errCounter("receive_crc_errors_total", 1), errCounter("receive_errors_total", 1), statistic("rx_fcs_errors", 1)}
			h.collect(1, model.CollectorNetdev, samples...)
			h.collect(1, model.CollectorNetdev, samples...)
			result := model.Result{Support: model.Supported, Samples: samples}
			allocs := testing.AllocsPerRun(100, func() {
				job, _ := h.r.startCollection(key, h.stamp())
				result.Job, result.Finished = job, h.stamp()
				_, _ = h.r.finishCollection(result)
			})
			baseline := newProblemHarness(t, func(c *Config) { c.ErrorWindow, c.FlapWindow, c.ErrorClasses = 0, 0, nil }).healthy()
			baseline.collect(1, model.CollectorNetdev, samples...)
			baseline.collect(1, model.CollectorNetdev, samples...)
			want := testing.AllocsPerRun(100, func() {
				job, _ := baseline.r.startCollection(key, baseline.stamp())
				result.Job, result.Finished = job, baseline.stamp()
				_, _ = baseline.r.finishCollection(result)
			})
			if allocs > want {
				t.Fatalf("evidence hook allocations = %v, disabled = %v", allocs, want)
			}
		})
	}
}
