package xtcp

import (
	"context"
	"sync"
	"syscall"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_config"
)

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
