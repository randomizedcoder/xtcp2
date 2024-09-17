package xtcp

import (
	"context"
	"log"
	"net"
	"sync"
	"time"

	nats "github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/randomizedcoder/xtcp2/pkg/config"
	"github.com/randomizedcoder/xtcp2/pkg/xtcppb"
	redis "github.com/redis/go-redis/v9"
	"golang.org/x/sys/unix"

	nsq "github.com/nsqio/go-nsq"
	"github.com/twmb/franz-go/pkg/kgo"
)

const (
	quantileError    = 0.05
	summaryVecMaxAge = 5 * time.Minute

	netlinkerDoneChSizeCst = 100

	startPollTimeKeyCst = "s"
	hostnameKeyCst      = "h"
)

type XTCP struct {
	config config.Config

	packetBufferPool sync.Pool
	xtcpRecordPool   sync.Pool
	nlhPool          sync.Pool
	rtaPool          sync.Pool
	kgoRecordPool    sync.Pool

	// Netlink socket variables
	socketFD      int
	socketAddress *unix.SockaddrNetlink
	// iour          *iouring.IOURing
	// resulter      chan iouring.Result

	nlRequest *[]byte

	netlinkerDoneCh chan time.Time
	allDoneCh       *chan struct{}
	pollTime        sync.Map
	hostname        sync.Map

	RTATypeDeserializer    map[int]func(buf []byte, xtcpRecord *xtcppb.FlatXtcpRecord) (err error)
	RTATypeDeserializerStr map[int]string

	xtcpRecordZeroizer map[xtcppb.FlatXtcpRecordCongestionAlgorithm]func(xtcpRecord *xtcppb.FlatXtcpRecord)

	Marshalers     sync.Map
	Marshaler      func(xtcpRecord *xtcppb.FlatXtcpRecord) (buf *[]byte)
	Destations     sync.Map
	Destation      func(ctx context.Context, xtcpRecordBinary *[]byte) (n int, err error)
	InitDestations sync.Map

	kClient      *kgo.Client
	nsqProducer  *nsq.Producer
	udpConn      net.Conn
	natsClient   *nats.Conn
	valKeyClient *redis.Client

	pC *prometheus.CounterVec
	pH *prometheus.SummaryVec

	debugLevel int
}

func NewXTCP(ctx context.Context, c config.Config, allDoneCh *chan struct{}) (*XTCP, error) {

	x := new(XTCP)

	x.config = c
	x.debugLevel = *x.config.DebugLevel
	x.Init(ctx, allDoneCh)

	return x, nil
}

func (x *XTCP) Run(ctx context.Context) {

	var wg sync.WaitGroup

	for i := 0; i < *x.config.Netlinkers; i++ {
		wg.Add(1)
		go x.Netlinker(ctx, &wg, i)
	}

	wg.Add(1)
	go x.Poller(ctx, &wg)

	if x.debugLevel > 10 {
		log.Println("XTCP.Run() go routines started")
	}

	wg.Wait()
	*x.allDoneCh <- struct{}{}

	if x.debugLevel > 10 {
		log.Println("XTCP.Run() allDoneCh")
	}

	switch *x.config.Dest {
	case "kafka":
		x.kClient.Close()
	case "nsq":
		x.nsqProducer.Stop()
	case "udp":
		x.udpConn.Close()
	}

	if x.debugLevel > 10 {
		log.Println("XTCP.Run() done")
	}

}

func (x *XTCP) CheckDoneNonBlocking(ctx context.Context) (netlinkerDone bool) {
	select {
	case <-ctx.Done():
		netlinkerDone = true
	default:
		// non blocking...
	}
	return
}
