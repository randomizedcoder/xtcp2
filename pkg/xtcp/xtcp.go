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
	"github.com/randomizedcoder/xtcp2/pkg/xtcp_config"
	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
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

	packetBufferPool sync.Pool
	xtcpEnvelopePool sync.Pool
	xtcpRecordPool   sync.Pool
	nlhPool          sync.Pool
	rtaPool          sync.Pool
	destBytesPool    sync.Pool

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

func NewXTCP(ctx context.Context, cancel context.CancelFunc, config *xtcp_config.XtcpConfig) *XTCP {

	x := new(XTCP)

	x.ctx = ctx
	x.cancel = cancel

	x.config = config
	x.debugLevel = x.config.DebugLevel
	x.fatalf = log.Fatalf

	x.Init(ctx)

	return x
}

func NewNsTestingXTCP(ctx context.Context, cancel context.CancelFunc, debugLevel uint32) *XTCP {

	x := new(XTCP)

	x.ctx = ctx
	x.cancel = cancel
	x.fatalf = log.Fatalf

	x.config = &xtcp_config.XtcpConfig{
		NlTimeoutMilliseconds: 5000,
		Dest:                  schemeNull,
		MarshalTo:             "proto",
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
		wg.Add(1)
		go x.watchNsNamespace(ctx, wg, key.(string))
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
