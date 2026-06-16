// Package xtcp is the long-running daemon that streams TCP socket state
// out of the kernel via netlink inet_diag, deserialises the responses, and
// fans them out to configurable destinations (unixgram, unix, udp, kafka,
// nats, nsq, valkey, null). The package owns the netlinker, deserializer,
// poller, namespace-watcher, marshaller, and destination registries; cmd
// binaries wire them together.
package xtcp

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/randomizedcoder/xtcp2/pkg/xsync"
	"github.com/randomizedcoder/xtcp2/pkg/xtcp_config"
	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

const (
	linuxNetNSDirCst  = "/run/netns/"
	dockerNetNsDirCst = "/run/docker/netns/"

	quantileError    = 0.05
	summaryVecMaxAge = 5 * time.Minute
)

type XTCP struct {
	ctx    context.Context
	cancel context.CancelFunc

	config *xtcp_config.XtcpConfig

	// ns[netNSitem]
	netNsDirs   *sync.Map
	nsMap       *sync.Map
	fdToNsMap   *sync.Map
	storeCount  atomic.Uint64
	deleteCount atomic.Uint64
	generation  atomic.Uint64

	packetBufferPool xsync.Pool[*[]byte]
	xtcpEnvelopePool xsync.Pool[*xtcp_flat_record.Envelope]
	xtcpRecordPool   xsync.Pool[*xtcp_flat_record.XtcpFlatRecord]
	nlhPool          xsync.Pool[*xtcpnl.NlMsgHdr]
	rtaPool          xsync.Pool[*xtcpnl.RTAttr]
	destBytesPool    xsync.Pool[*[]byte]

	currentEnvelope       *xtcp_flat_record.Envelope
	pollStartTime         time.Time
	envelopeMu            sync.Mutex
	changePollFrequencyCh chan time.Duration
	pollRequestCh         chan struct{}

	nlRequest *[]byte

	netlinkerDoneCh chan netlinkerDone
	pollTime        sync.Map

	pollTimeoutTimer *time.Timer

	hostname string

	RTATypeDeserializer    map[int]func(buf []byte, xtcpRecord *xtcp_flat_record.XtcpFlatRecord) (err error)
	RTATypeDeserializerStr map[int]string

	xtcpRecordZeroizer map[xtcp_flat_record.XtcpFlatRecord_CongestionAlgorithm]func(xtcpRecord *xtcp_flat_record.XtcpFlatRecord)

	Marshallers sync.Map
	Marshaller  func(r *xtcp_flat_record.XtcpFlatRecord) (buf *[]byte)

	EnvelopeMarshallers sync.Map
	EnvelopeMarshaller  func(e *xtcp_flat_record.Envelope) (buf *[]byte)

	// dest is the chosen Destination for this process. Built once at startup
	// in InitDests by looking up x.config.Dest's scheme in the package-level
	// destRegistry. Per-destination state (clients, conns, fds, pools) lives
	// inside the implementation behind this interface — no destination-typed
	// fields leak onto XTCP, which is what lets `-tags dest_kafka` etc. omit
	// entire library packages from the binary.
	dest Destination

	// Signals poller can start
	DestinationReady chan struct{}

	// Netlinker function dispatch — same pattern as Marshaller. Variants
	// registered in InitNetlinkers; one chosen at init based on config.IoUring.
	Netlinkers     sync.Map
	Netlinker      NetlinkerFunc
	NetlinkerReady chan struct{}

	// rings holds the per-Netlinker io_uring rings when config.IoUring is
	// true. Key is the netlinker id (uint32). Empty / unused on the syscall path.
	rings sync.Map

	// fatalf is the function used by initialisation paths to abort on startup
	// errors. Defaults to log.Fatalf; tests override it with t.Fatalf so they
	// can drive the init paths without taking down the process.
	fatalf func(format string, args ...any)

	// registry is the Prometheus registry InitPromethus and the gRPC
	// service constructors register metrics into. Defaults to
	// prometheus.DefaultRegisterer in NewXTCP / NewNsTestingXTCP so
	// production behavior is unchanged; tests pre-fill this field with a
	// fresh prometheus.NewRegistry() so repeated InitPromethus /
	// NewXtcp*Service calls within the same process don't panic from
	// duplicate metrics collector registration.
	registry prometheus.Registerer

	flatRecordService *xtcpFlatRecordService
	configService     *xtcpConfigService

	pC *prometheus.CounterVec
	pH *prometheus.SummaryVec
	pG prometheus.Gauge

	debugLevel uint32
}

// network namespace item
// these are the items we track about each network name space
type netNSitem struct {
	name     *string
	ctx      context.Context
	cancel   context.CancelFunc
	wg       *sync.WaitGroup
	socketFD int
}

type netlinkerDone struct {
	fd int
	t  time.Time
}

// constructorRegistry is the prometheus.Registerer the NewXTCP /
// NewNsTestingXTCP constructors install on the returned XTCP before
// calling Init. Defaults to prometheus.DefaultRegisterer (production
// behavior). Tests swap this in for a fresh prometheus.NewRegistry()
// so the constructors are re-runnable in one process without panicking
// from duplicate metrics collector registration.
var constructorRegistry prometheus.Registerer = prometheus.DefaultRegisterer

// SetConstructorRegistry swaps the registry used by NewXTCP /
// NewNsTestingXTCP and returns the previous value. Intended for cross-
// package tests (cmd/ns, cmd/xtcp2) that want a fresh registry per
// test invocation. Restoring the previous value via the returned hook
// keeps successive tests' registrations isolated.
func SetConstructorRegistry(reg prometheus.Registerer) prometheus.Registerer {
	prev := constructorRegistry
	constructorRegistry = reg
	return prev
}

// SetNetNsCandidateDirs swaps the netns-directory list initSyncMaps
// probes for. Returns the previous list so tests can restore it.
// Cross-package tests (cmd/ns) prepend a tempdir so initSyncMaps
// doesn't fatalf on sandboxes lacking /run/netns + /run/docker/netns.
func SetNetNsCandidateDirs(dirs []string) []string {
	prev := netNsCandidateDirs
	netNsCandidateDirs = dirs
	return prev
}

// capabilityCheck is the startup capability gate, indirected through a
// package var (like constructorRegistry / netNsCandidateDirs) so tests
// can run NewXTCP / NewNsTestingXTCP → Init to completion on unprivileged
// sandboxes. The capability logic itself is exercised directly in
// init_capabilities_test.go; production keeps the hard fail-fast.
var capabilityCheck = (*XTCP).checkCapabilities

// SetCapabilityCheck swaps the capability-check seam and returns the
// previous value. Cross-package tests (cmd/ns) install a no-op and
// restore on cleanup so Init doesn't fatalf without CAP_SYS_ADMIN /
// CAP_NET_ADMIN.
func SetCapabilityCheck(f func(*XTCP) error) func(*XTCP) error {
	prev := capabilityCheck
	capabilityCheck = f
	return prev
}

func NewXTCP(ctx context.Context, cancel context.CancelFunc, config *xtcp_config.XtcpConfig) *XTCP {

	x := new(XTCP)

	x.ctx = ctx
	x.cancel = cancel

	x.config = config
	x.debugLevel = x.config.DebugLevel
	x.fatalf = log.Fatalf
	x.registry = constructorRegistry

	x.Init(ctx)

	return x
}

func NewNsTestingXTCP(ctx context.Context, cancel context.CancelFunc, debugLevel uint32) *XTCP {

	x := new(XTCP)

	x.ctx = ctx
	x.cancel = cancel
	x.fatalf = log.Fatalf
	x.registry = constructorRegistry

	x.config = &xtcp_config.XtcpConfig{
		NlTimeoutMilliseconds: 5000,
		Dest:                  schemeNull,
		MarshalTo:             MarshallerProtobufList,
		Topic:                 "not-a-topic",
		EnabledDeserializers: &xtcp_config.EnabledDeserializers{
			Enabled: make(map[string]bool),
		},
	}
	x.debugLevel = debugLevel

	x.Init(ctx)

	return x
}

// RunWithPoller is the main run function for xTCP
// it starts everything required, including the netlink socket poller
func (x *XTCP) RunWithPoller(ctx context.Context, wg *sync.WaitGroup) {
	x.Run(ctx, wg, true)
}

// RunNoPoller is only for testing, do not run this for real
// This will only monitor the name spaces, and was used for
// testing the kernel/user-land correctly stay in sync, there's
// no leaks, etc.  See also /cmd/nsTest/nsTest.go
// Basically, it just doesn't start the poller
func (x *XTCP) RunNoPoller(ctx context.Context, wg *sync.WaitGroup) {
	x.Run(ctx, wg, false)
}

func (x *XTCP) Run(ctx context.Context, wg *sync.WaitGroup, runPoller bool) {

	defer wg.Done()

	x.pC.WithLabelValues("Run", "start", "counter").Inc()

	go x.startGRPCflatRecordService(ctx)

	x.openDefaultNetLinkSocket(ctx)

	wg.Add(1)
	go x.nsMapCountReporter(ctx, wg)

	x.netNsDirs.Range(func(key, value interface{}) bool {
		dir, ok := key.(string)
		if !ok {
			return true
		}
		wg.Add(1)
		go func() {
			if err := x.watchNsNamespace(ctx, wg, dir); err != nil {
				log.Printf("watchNsNamespace(%s) err:%v", dir, err)
			}
		}()
		return true
	})

	wg.Add(1)
	go x.mapReconciler(ctx, wg)

	if runPoller {
		wg.Add(1)
		go x.Poller(ctx, wg)

		if x.debugLevel > 10 {
			log.Println("XTCP.Run() wg.Wait()")
		}
	}

	if x.debugLevel > 10 {
		log.Println("XTCP.Run() wg.Wait()")
	}
	wg.Wait()

	x.cancel()

	if x.debugLevel > 10 {
		log.Println("XTCP.Run() x.cancel()")
	}

	x.closeDestination()

	if x.debugLevel > 10 {
		log.Println("XTCP.Run() complete")
	}
}

func (x *XTCP) checkDoneNonBlocking(ctx context.Context) (netlinkerDone bool) {
	select {
	case <-ctx.Done():
		netlinkerDone = true
	default:
		// non blocking...
	}
	return
}

func (x *XTCP) closeDestination() {
	if x.dest == nil {
		return
	}
	if err := x.dest.Close(); err != nil && x.debugLevel > 10 {
		log.Printf("closeDestination: %v", err)
	}
}
