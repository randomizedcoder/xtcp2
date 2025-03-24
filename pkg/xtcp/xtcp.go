package xtcp

import (
	"context"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	nats "github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/randomizedcoder/xtcp2/pkg/xtcp_config"
	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
	redis "github.com/redis/go-redis/v9"

	nsq "github.com/nsqio/go-nsq"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sr"
)

const (
	linuxNetNSDirCst  = "/run/netns/"
	dockerNetNsDirCst = "/run/docker/netns/"

	quantileError    = 0.05
	summaryVecMaxAge = 5 * time.Minute

	// For protobuf the size is at least 6, not 5
	// https://docs.confluent.io/platform/current/schema-registry/fundamentals/serdes-develop/index.html#wire-format
	KafkaHeaderSizeCst = 6
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
	kgoRecordPool    sync.Pool
	destBytesPool    sync.Pool

	currentEnvelope       *xtcp_flat_record.Envelope
	pollStartTime         time.Time
	envelopeMu            sync.Mutex
	changePollFrequencyCh chan time.Duration
	pollRequestCh         chan struct{}

	// Netlink socket variables
	// socketFD      int
	// socketAddress *unix.SockaddrNetlink
	// iour          *iouring.IOURing
	// resulter      chan iouring.Result

	nlRequest *[]byte

	//netlinkerDoneCh chan time.Time
	netlinkerDoneCh chan netlinkerDone
	pollTime        sync.Map

	pollTimeoutTimer *time.Timer

	hostname string

	RTATypeDeserializer    map[int]func(buf []byte, xtcpRecord *xtcp_flat_record.Envelope_XtcpFlatRecord) (err error)
	RTATypeDeserializerStr map[int]string

	xtcpRecordZeroizer map[xtcp_flat_record.Envelope_XtcpFlatRecord_CongestionAlgorithm]func(xtcpRecord *xtcp_flat_record.Envelope_XtcpFlatRecord)

	Marshallers      sync.Map
	Marshaller       func(e *xtcp_flat_record.Envelope) (buf *[]byte)
	Destinations     sync.Map
	Destination      func(ctx context.Context, xtcpRecordBinary *[]byte) (n int, err error)
	InitDestinations sync.Map
	// Signals poller can start
	DestinationReady chan struct{}

	kClient      *kgo.Client
	kRegClient   *sr.Client
	kSerde       sr.Serde
	schemaID     int
	nsqProducer  *nsq.Producer
	udpConn      net.Conn
	natsClient   *nats.Conn
	valKeyClient *redis.Client

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

	x.Init(ctx)

	return x
}

func NewNsTestingXTCP(ctx context.Context, cancel context.CancelFunc, debugLevel uint32) *XTCP {

	x := new(XTCP)

	x.ctx = ctx
	x.cancel = cancel

	x.config = &xtcp_config.XtcpConfig{
		NlTimeoutMilliseconds: 5000,
		Dest:                  "null",
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

	// /var/run/netns is a symlink to /run/netns
	// netnsDir = "/var/run/netns"
	// [das@hp1:~]$ ls -la /var/ | grep run
	// lrwxrwxrwx  1 root root   11 Sep 17 15:57 lock -> ../run/lock
	// lrwxrwxrwx  1 root root    6 Sep 17 15:57 run -> ../run
	// https://www.redhat.com/en/blog/net-namespaces
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
	switch x.config.Dest {
	case "kafka":
		x.kClient.Close()
	case "nsq":
		x.nsqProducer.Stop()
	case "udp":
		x.udpConn.Close()
	}
}
