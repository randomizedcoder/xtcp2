package xtcp

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"syscall"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_config"
)

// initSyncMaps + initHostname: previously log.Fatal'd unrecoverably; the
// fatalf-injection refactor routes both through x.fatalf so they can run
// to completion under test (with x.fatalf as a capture).

func TestInitSyncMaps_fatalfCapture(t *testing.T) {
	x := &XTCP{}
	var captured string
	x.fatalf = func(format string, args ...any) {
		captured = fmt.Sprintf(format, args...)
	}
	x.initSyncMaps()
	if x.nsMap == nil || x.fdToNsMap == nil || x.netNsDirs == nil {
		t.Error("initSyncMaps should allocate all three sync.Maps before fataling")
	}
	// On hosts WITHOUT /run/netns + /run/docker/netns, the fatal path
	// fires and we get a captured message. On hosts WITH those dirs,
	// captured stays empty. Both outcomes are valid.
	if captured != "" && !stringContains(captured, "network namespace") {
		t.Errorf("fatalf message mismatch: %q", captured)
	}
}

func TestInitSyncMaps_debugLog(t *testing.T) {
	x := &XTCP{debugLevel: 11}
	x.fatalf = func(string, ...any) {} // swallow if it fires
	x.initSyncMaps()
}

func TestInitSyncMaps_realDir(t *testing.T) {
	// Prepend a real tempdir to netNsCandidateDirs so the function
	// stores at least one entry and the fatal path is skipped.
	prev := netNsCandidateDirs
	t.Cleanup(func() { netNsCandidateDirs = prev })
	netNsCandidateDirs = append([]string{t.TempDir()}, prev...)

	x := &XTCP{}
	called := false
	x.fatalf = func(string, ...any) { called = true }
	x.initSyncMaps()
	if called {
		t.Error("fatalf should not fire when a candidate dir exists")
	}
	count := 0
	x.netNsDirs.Range(func(_, _ any) bool {
		count++
		return true
	})
	if count < 1 {
		t.Error("netNsDirs should have at least one entry")
	}
}

func TestInitSyncMaps_realDir_debugLog(t *testing.T) {
	prev := netNsCandidateDirs
	t.Cleanup(func() { netNsCandidateDirs = prev })
	netNsCandidateDirs = append([]string{t.TempDir()}, prev...)

	x := &XTCP{debugLevel: 11}
	x.fatalf = func(string, ...any) {}
	x.initSyncMaps()
}

func TestInitHostname_happy(t *testing.T) {
	x := &XTCP{}
	x.initHostname()
	if x.hostname == "" {
		t.Error("hostname should be non-empty")
	}
}

func TestInitHostname_error(t *testing.T) {
	prev := hostnameLookup
	hostnameLookup = func() (string, error) { return "", fmt.Errorf("synthetic") }
	t.Cleanup(func() { hostnameLookup = prev })

	x := &XTCP{}
	var captured string
	x.fatalf = func(format string, args ...any) {
		captured = fmt.Sprintf(format, args...)
	}
	x.initHostname()
	if x.hostname != "" {
		t.Errorf("hostname should remain empty on error; got %q", x.hostname)
	}
	if !stringContains(captured, "os.Hostname() error") {
		t.Errorf("fatalf not invoked with expected message; got %q", captured)
	}
}

// callFatalf: with x.fatalf swapped, the swap takes effect.
func TestCallFatalf_routes(t *testing.T) {
	x := &XTCP{}
	called := false
	x.fatalf = func(string, ...any) { called = true }
	x.callFatalf("oops")
	if !called {
		t.Error("x.fatalf swap was not invoked")
	}
}

// SetConstructorRegistry + SetNetNsCandidateDirs: round-trip the swap +
// restore-via-returned-value contract.
func TestSetConstructorRegistry_swapAndRestore(t *testing.T) {
	newReg := prometheus.NewRegistry()
	prev := SetConstructorRegistry(newReg)
	if constructorRegistry != newReg {
		t.Error("constructorRegistry not updated")
	}
	restored := SetConstructorRegistry(prev)
	if restored != newReg {
		t.Errorf("restore returned %v, want the swapped-in value", restored)
	}
	if constructorRegistry != prev {
		t.Error("constructorRegistry not restored")
	}
}

func TestSetNetNsCandidateDirs_swapAndRestore(t *testing.T) {
	newDirs := []string{"/tmp/test-only"}
	prev := SetNetNsCandidateDirs(newDirs)
	if &netNsCandidateDirs[0] != &newDirs[0] {
		t.Error("netNsCandidateDirs not updated")
	}
	restored := SetNetNsCandidateDirs(prev)
	if restored[0] != newDirs[0] {
		t.Errorf("restore returned %v, want the swapped-in value", restored)
	}
}

// NewXTCP via constructorRegistry swap + netNsCandidateDirs override.
// Pass a minimal valid config (null dest + valid marshaller + non-empty
// topic) so InputValidation passes.
func TestNewXTCP_runsToCompletion(t *testing.T) {
	prevReg := constructorRegistry
	prevDirs := netNsCandidateDirs
	t.Cleanup(func() {
		constructorRegistry = prevReg
		netNsCandidateDirs = prevDirs
	})
	constructorRegistry = prometheus.NewRegistry()
	netNsCandidateDirs = append([]string{t.TempDir()}, prevDirs...)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := &xtcp_config.XtcpConfig{
		Dest:      schemeNull,
		MarshalTo: MarshallerProtobufSingle,
		Topic:     "test",
		EnabledDeserializers: &xtcp_config.EnabledDeserializers{
			Enabled: make(map[string]bool),
		},
	}
	x := NewXTCP(ctx, cancel, cfg)
	if x == nil {
		t.Fatal("NewXTCP returned nil")
	}
	if x.Marshaller == nil {
		t.Error("Marshaller should be populated after Init")
	}
	if x.Netlinker == nil {
		t.Error("Netlinker should be populated after Init")
	}
}

// NewNsTestingXTCP via constructorRegistry swap + netNsCandidateDirs
// override. The full Init runs to completion: every helper now uses
// callFatalf and the fresh registry avoids duplicate-collector panics.
func TestNewNsTestingXTCP_runsToCompletion(t *testing.T) {
	// Override the package vars the constructor + Init read from.
	prevReg := constructorRegistry
	prevDirs := netNsCandidateDirs
	t.Cleanup(func() {
		constructorRegistry = prevReg
		netNsCandidateDirs = prevDirs
	})
	constructorRegistry = prometheus.NewRegistry()
	netNsCandidateDirs = append([]string{t.TempDir()}, prevDirs...)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	x := NewNsTestingXTCP(ctx, cancel, 0)
	if x == nil {
		t.Fatal("NewNsTestingXTCP returned nil")
	}
	if x.Marshaller == nil {
		t.Error("Marshaller should be populated after Init")
	}
	if x.Netlinker == nil {
		t.Error("Netlinker should be populated after Init")
	}
	if x.hostname == "" {
		t.Error("hostname should be populated after Init")
	}
}

// stringContains is a tiny substring helper kept local to this test file.
func stringContains(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// newInitFixture returns an XTCP shaped for the various Init* tests:
// fresh Prometheus registry, fatalf wired to t.Fatalf, ready channels
// allocated.
func newInitFixture(t *testing.T) *XTCP {
	t.Helper()
	x := &XTCP{
		config:           &xtcp_config.XtcpConfig{},
		DestinationReady: make(chan struct{}, 1),
		NetlinkerReady:   make(chan struct{}, 1),
	}
	x.fatalf = t.Fatalf
	reg := prometheus.NewRegistry()
	x.pC = promauto.With(reg).NewCounterVec(
		prometheus.CounterOpts{Subsystem: "xtcp_init_test",
			Name: promNameCounts, Help: "test counts"},
		promLabels,
	)
	return x
}

// ───────────────────────────────────────────────────────────────────────
// InitSyncPools — pool New funcs + packetBufferSize calculation
// ───────────────────────────────────────────────────────────────────────

func TestInitSyncPools_defaults(t *testing.T) {
	x := newInitFixture(t)
	var wg sync.WaitGroup
	wg.Add(1)
	x.InitSyncPools(&wg)
	wg.Wait()

	// PacketSizeMply was 0 → defaulted to 8.
	if x.config.PacketSizeMply != 8 {
		t.Errorf("PacketSizeMply = %d, want 8", x.config.PacketSizeMply)
	}
	// Pools should yield buffers via their New funcs.
	pb, _ := x.packetBufferPool.Get().(*[]byte)
	if pb == nil || cap(*pb) != syscall.Getpagesize()*8 {
		t.Errorf("packetBufferPool New() produced cap=%d, want %d",
			cap(*pb), syscall.Getpagesize()*8)
	}
	x.packetBufferPool.Put(pb)

	if x.xtcpRecordPool.Get() == nil {
		t.Error("xtcpRecordPool.Get returned nil")
	}
	if x.nlhPool.Get() == nil {
		t.Error("nlhPool.Get returned nil")
	}
	if x.rtaPool.Get() == nil {
		t.Error("rtaPool.Get returned nil")
	}
}

func TestInitSyncPools_explicitPacketSize(t *testing.T) {
	x := newInitFixture(t)
	x.config.PacketSize = 4096
	x.config.PacketSizeMply = 2
	var wg sync.WaitGroup
	wg.Add(1)
	x.InitSyncPools(&wg)
	wg.Wait()
	pb, _ := x.packetBufferPool.Get().(*[]byte)
	if cap(*pb) != 8192 {
		t.Errorf("packet buffer cap = %d, want 8192 (4096 * 2)", cap(*pb))
	}
}

// ───────────────────────────────────────────────────────────────────────
// InitNetlinkers — registers both variants, picks one based on IoUring.
// ───────────────────────────────────────────────────────────────────────

func TestInitNetlinkers_syscallDefault(t *testing.T) {
	x := newInitFixture(t)
	x.config.IoUring = false
	var wg sync.WaitGroup
	wg.Add(1)
	x.InitNetlinkers(context.Background(), &wg)
	wg.Wait()
	if x.Netlinker == nil {
		t.Fatal("Netlinker pointer nil after Init")
	}
	// Both variants should be registered.
	if _, ok := x.Netlinkers.Load("syscall"); !ok {
		t.Error("syscall variant missing from Netlinkers")
	}
	if _, ok := x.Netlinkers.Load("io_uring"); !ok {
		t.Error("io_uring variant missing from Netlinkers")
	}
	// Ready signal should fire.
	select {
	case <-x.NetlinkerReady:
	default:
		t.Error("NetlinkerReady should have been signalled")
	}
}

func TestInitNetlinkers_ioUringSelected(t *testing.T) {
	x := newInitFixture(t)
	x.config.IoUring = true
	var wg sync.WaitGroup
	wg.Add(1)
	x.InitNetlinkers(context.Background(), &wg)
	wg.Wait()
	if x.Netlinker == nil {
		t.Fatal("Netlinker pointer nil for io_uring path")
	}
}

// ───────────────────────────────────────────────────────────────────────
// InitDests — registry lookup + factory dispatch
// ───────────────────────────────────────────────────────────────────────

func TestInitDests_null(t *testing.T) {
	x := newInitFixture(t)
	x.config.Dest = "null"
	var wg sync.WaitGroup
	wg.Add(1)
	x.InitDests(context.Background(), &wg)
	wg.Wait()
	if x.dest == nil {
		t.Fatal("InitDests didn't set x.dest for null scheme")
	}
	// DestinationReady should be signalled.
	select {
	case <-x.DestinationReady:
	default:
		t.Error("DestinationReady should have been signalled")
	}
}

// ───────────────────────────────────────────────────────────────────────
// initChannels — allocates 5 channels of expected capacity.
// ───────────────────────────────────────────────────────────────────────

func TestInitChannels(t *testing.T) {
	x := newInitFixture(t)
	x.config.NetlinkersDoneChanSize = 32
	x.initChannels()
	if cap(x.DestinationReady) != destinationReadyChSize {
		t.Errorf("DestinationReady cap = %d, want %d",
			cap(x.DestinationReady), destinationReadyChSize)
	}
	if cap(x.NetlinkerReady) != netlinkerReadyChSize {
		t.Errorf("NetlinkerReady cap mismatch")
	}
	if cap(x.netlinkerDoneCh) != 32 {
		t.Errorf("netlinkerDoneCh cap = %d, want 32", cap(x.netlinkerDoneCh))
	}
	if cap(x.changePollFrequencyCh) != changePollFrequencyChSize {
		t.Errorf("changePollFrequencyCh cap mismatch")
	}
	if cap(x.pollRequestCh) != pollRequestChSize {
		t.Errorf("pollRequestCh cap mismatch")
	}
}

// ───────────────────────────────────────────────────────────────────────
// initHostname — populates x.hostname from os.Hostname.
// ───────────────────────────────────────────────────────────────────────

func TestInitHostname(t *testing.T) {
	x := newInitFixture(t)
	x.initHostname()
	if x.hostname == "" {
		t.Error("hostname should be populated by initHostname")
	}
}

// ───────────────────────────────────────────────────────────────────────
// CreateNetLinkRequest — builds the netlink request header + payload.
// ───────────────────────────────────────────────────────────────────────

func TestCreateNetLinkRequest(t *testing.T) {
	x := newInitFixture(t)
	x.config.NlmsgSeq = 12345
	var wg sync.WaitGroup
	wg.Add(1)
	got := x.CreateNetLinkRequest(&wg)
	wg.Wait()
	if got == nil || len(*got) == 0 {
		t.Fatal("CreateNetLinkRequest returned empty request")
	}
	// The first 16 bytes are the NlMsgHdr — verify the seq we set is in there.
	// NlMsgHdr.Seq is at offset 8 (Len:4, Type:2, Flags:2, then Seq:4).
	if (*got)[8] != 0x39 || (*got)[9] != 0x30 || (*got)[10] != 0 || (*got)[11] != 0 {
		t.Errorf("expected NlmsgSeq=12345 (0x3039 LE) at offset 8; got %v",
			(*got)[8:12])
	}
}

func TestInitDests_unknownScheme(t *testing.T) {
	fatalfHit := false
	x := newInitFixture(t)
	x.fatalf = func(format string, args ...any) { fatalfHit = true }
	x.config.Dest = "carrier:pigeon:9000"
	var wg sync.WaitGroup
	wg.Add(1)
	x.InitDests(context.Background(), &wg)
	wg.Wait()
	if !fatalfHit {
		t.Error("InitDests should have called fatalf for unknown scheme")
	}
}

// InitDests with debugLevel>10 also hits the CompiledInSchemes log branch
// at the bottom of the happy path.
func TestInitDests_debugLog(t *testing.T) {
	x := newInitFixture(t)
	x.config.Dest = "null"
	x.debugLevel = 20
	var wg sync.WaitGroup
	wg.Add(1)
	x.InitDests(context.Background(), &wg)
	wg.Wait()
	select {
	case <-x.DestinationReady:
	default:
		t.Error("DestinationReady should have been signalled")
	}
}

// InitDests with a registered scheme whose factory always errors: confirms
// the "factory(...) err" branch routes through x.fatalf. The registry is
// per-package so we register a unique scheme directly and remove it on
// cleanup.
func TestInitDests_factoryErr(t *testing.T) {
	const scheme = "errorscheme"
	errInjected := errors.New("injected factory error")
	destRegistryMu.Lock()
	destRegistry[scheme] = func(_ context.Context, _ *XTCP) (Destination, error) {
		return nil, errInjected
	}
	destRegistryMu.Unlock()
	t.Cleanup(func() {
		destRegistryMu.Lock()
		delete(destRegistry, scheme)
		destRegistryMu.Unlock()
	})

	fatalfHit := false
	x := newInitFixture(t)
	x.fatalf = func(format string, args ...any) { fatalfHit = true }
	x.config.Dest = scheme + ":"
	var wg sync.WaitGroup
	wg.Add(1)
	x.InitDests(context.Background(), &wg)
	wg.Wait()
	if !fatalfHit {
		t.Error("InitDests should have called fatalf for factory error")
	}
}
