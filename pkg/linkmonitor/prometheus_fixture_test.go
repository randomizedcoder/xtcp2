package linkmonitor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

// PrometheusFixture is a test-only bridge for testing the adapter with real publications.
type PrometheusFixture struct {
	Monitor *Monitor
	r       *reducer
	t       testing.TB
	now     time.Duration
}

// NewPrometheusFixture creates a source-free reducer; the monitor is never run.
func NewPrometheusFixture(t testing.TB) *PrometheusFixture {
	t.Helper()
	return &PrometheusFixture{Monitor: new(Monitor), r: newReducer(1), t: t}
}

// Device publishes classified inventory evidence through the real reducer.
func (f *PrometheusFixture) Device(index uint32, name string, up bool) {
	f.t.Helper()
	_, err := f.r.observe(model.Observation{Device: model.Device{
		Key:   model.DeviceKey{Namespace: 1, Kind: model.DeviceEthernet, Index: index},
		Token: model.Token{SourceEpoch: f.r.epoch}, Name: name, Eligibility: model.Eligible, Up: presentValue(up),
	}, Observed: model.Stamp{Wall: time.Unix(10, 500000000), Monotonic: f.now}})
	if err != nil {
		f.t.Fatal(err)
	}
}

// Samples applies a completed source attempt and returns schema-validation failures.
func (f *PrometheusFixture) Samples(index uint32, kind model.CollectorKind, samples ...model.Sample) error {
	f.t.Helper()
	key := model.JobKey{Namespace: 1, Collector: kind}
	if index != 0 {
		key.Device = model.DeviceKey{Namespace: 1, Kind: model.DeviceEthernet, Index: index}
	}
	return f.samples(key, samples)
}

func (f *PrometheusFixture) samples(key model.JobKey, samples []model.Sample) error {
	stamp := model.Stamp{Wall: time.Unix(10, 500000000), Monotonic: f.now}
	job, err := f.r.startCollection(key, stamp)
	if err != nil {
		return err
	}
	_, err = f.r.finishCollection(model.Result{Job: job, Finished: stamp, Support: model.Supported, Samples: samples})
	return err
}

// Speed completes an Ethernet settings collection that measured activeMbps
// on a port whose maximum is maximumMbps, installing its checks as the
// scheduler does.
func (f *PrometheusFixture) Speed(index uint32, activeMbps, maximumMbps uint64) {
	f.t.Helper()
	key := model.JobKey{Namespace: 1, Collector: model.CollectorSettings,
		Device: model.DeviceKey{Namespace: 1, Kind: model.DeviceEthernet, Index: index}}
	stamp := model.Stamp{Wall: time.Unix(10, 500000000), Monotonic: f.now}
	job, err := f.r.startCollection(key, stamp)
	if err != nil {
		f.t.Fatal(err)
	}
	active := presentValue(activeMbps * 1_000_000)
	settings := &model.SettingsChecks{Speed: equalCheck(activeMbps, maximumMbps), Duplex: model.CheckPass, ActiveBits: active}
	if _, err := f.r.finishCollection(model.Result{Job: job, Finished: stamp, Support: model.Supported, Settings: settings}); err != nil {
		f.t.Fatal(err)
	}
	if !f.r.setChecks(job, deviceChecks{maximumSpeed: settings.Speed, fullDuplex: settings.Duplex, activeBits: active}) {
		f.t.Fatal("settings checks rejected")
	}
}

// RDMA installs real state projections for two native ports or one shared RoCE link.
func (f *PrometheusFixture) RDMA(native bool) {
	f.t.Helper()
	ports := make([]model.RDMAPort, 0, 2)
	for port := uint32(1); port <= 2; port++ {
		key := model.DeviceKey{Namespace: 1, Kind: model.DeviceEthernet, Index: 1}
		layer := rdmaEthernetLayer
		if native {
			key = model.DeviceKey{Namespace: 1, Kind: model.DeviceNativeRDMA, RDMADevice: "mlx5_0", Port: port}
			layer = rdmaNativeLayer
		}
		d := model.Device{Key: key, Name: "eth0", Token: model.Token{SourceEpoch: f.r.epoch}, Eligibility: model.Eligible, Up: presentValue(true), RDMA: true}
		if _, err := f.r.observe(model.Observation{Device: d}); err != nil {
			f.t.Fatal(err)
		}
		p := model.RDMAPort{Device: "mlx5_0", Port: port, Layer: layer, State: presentValue(uint8(4)), Physical: presentValue(uint8(5)), Aliases: []string{"eth0"}, Versions: []string{"v2"}}
		ports = append(ports, p)
		if native {
			samples, _ := rdmaSamples([]model.RDMAPort{p}, presentValue(true))
			if err := f.samples(model.JobKey{Namespace: 1, Device: key, Collector: model.CollectorRDMAState}, samples); err != nil {
				f.t.Fatal(err)
			}
		}
	}
	if !native {
		samples, _ := rdmaSamples(ports, presentValue(true))
		if err := f.Samples(1, model.CollectorRDMAState, samples...); err != nil {
			f.t.Fatal(err)
		}
	}
}

// Exceptions resolves selectors against the current authoritative inventory.
func (f *PrometheusFixture) Exceptions(selectors ...string) {
	f.r.exceptions = resolveExceptions(selectors, f.r.rdmaExceptionDevices())
}

// Publish freezes a complete inventory and a known baseline.
func (f *PrometheusFixture) Publish(expected uint64) {
	f.t.Helper()
	stamp := model.Stamp{Wall: time.Unix(10, 500000000), Monotonic: f.now}
	if !f.r.recordResync(model.Token{SourceEpoch: f.r.epoch, Revision: f.r.revision}, stamp) {
		f.t.Fatal("resync rejected")
	}
	if err := f.r.publish(f.Monitor, publicationState{expected: presentValue(expected), now: stamp}); err != nil {
		f.t.Fatal(err)
	}
}

// Expire advances only the owner's monotonic time, never scrape-time policy.
func (f *PrometheusFixture) Expire(at time.Duration) { f.now = at; f.r.expire(at) }

// Remove applies an authoritative deletion.
func (f *PrometheusFixture) Remove(index uint32) {
	f.t.Helper()
	_, err := f.r.remove(model.DeviceKey{Namespace: 1, Kind: model.DeviceEthernet, Index: index}, model.Token{SourceEpoch: f.r.epoch})
	if err != nil {
		f.t.Fatal(err)
	}
}

// StartControls makes the fixture monitor accept Submit until ctx ends, so
// adapter tests can drive real control tickets without a session.
func (f *PrometheusFixture) StartControls(ctx context.Context) {
	f.Monitor.control.wake = make(chan struct{}, 1)
	f.Monitor.control.changed = make(chan struct{})
	f.Monitor.control.start(ctx)
}

// RunControls begins and completes all pending control work, as one session
// publication does, and reports whether any work ran.
func (f *PrometheusFixture) RunControls() bool {
	work := f.Monitor.control.begin()
	f.Monitor.control.complete(work)
	return work != 0
}

// StopControls stops accepting controls and releases waiters, as shutdown does.
func (f *PrometheusFixture) StopControls() { f.Monitor.control.stop() }

// ProblemConfig applies the problem fields of a configuration edited by
// mutate, as a validated monitor configuration does.
func (f *PrometheusFixture) ProblemConfig(mutate func(*Config)) {
	f.t.Helper()
	cfg := DefaultConfig()
	mutate(&cfg)
	policy, err := newProblemPolicy(cfg)
	if err != nil {
		f.t.Fatal(err)
	}
	f.r.problems = policy
}

// Fail completes a failed attempt of one collector; index 0 is the host.
func (f *PrometheusFixture) Fail(index uint32, kind model.CollectorKind) {
	f.t.Helper()
	key := model.JobKey{Namespace: 1, Collector: kind}
	if index != 0 {
		key.Device = model.DeviceKey{Namespace: 1, Kind: model.DeviceEthernet, Index: index}
	}
	stamp := model.Stamp{Wall: time.Unix(10, 500000000), Monotonic: f.now}
	job, err := f.r.startCollection(key, stamp)
	if err != nil {
		f.t.Fatal(err)
	}
	if _, err := f.r.finishCollection(model.Result{Job: job, Finished: stamp, Support: model.Supported, Err: errFixture, Reason: model.ErrorReason(1)}); err != nil {
		f.t.Fatal(err)
	}
}

var errFixture = errors.New("fixture failure")

// NativeSamples applies a completed attempt on native RDMA port mlx5_0:port.
func (f *PrometheusFixture) NativeSamples(port uint32, kind model.CollectorKind, samples ...model.Sample) error {
	f.t.Helper()
	return f.samples(model.JobKey{Namespace: 1, Collector: kind,
		Device: model.DeviceKey{Namespace: 1, Kind: model.DeviceNativeRDMA, RDMADevice: "mlx5_0", Port: port}}, samples)
}

// Advance moves the owner's monotonic time without running expiry.
func (f *PrometheusFixture) Advance(at time.Duration) { f.now = at }

// ScrapeTime is the wall clock reading at owner time at, given that every
// fixture stamp carries the same wall time and the latest monotonic time.
func (f *PrometheusFixture) ScrapeTime(at time.Duration) time.Time {
	return time.Unix(10, 500000000).Add(at - f.now)
}

// FixedSourceNames lists the application-defined source metric names.
func FixedSourceNames() []string {
	names := make([]string, 0, len(fixedSourceNames))
	for name := range fixedSourceNames {
		names = append(names, name)
	}
	return names
}
