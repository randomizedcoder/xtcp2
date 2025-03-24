package main

import (
	"context"
	"flag"
	"io"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	_ "unsafe"

	"github.com/randomizedcoder/xtcp2/pkg/misc"
	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// unsafe for FastRandN

// "google.golang.org/grpc/encoding/gzip"
// https://github.com/grpc/grpc-go/blob/master/examples/features/compression/client/main.go

//go:linkname FastRand runtime.fastrand
func FastRand() uint32

// https://cs.opensource.google/go/go/+/master:src/runtime/stubs.go;l=151?q=FastRandN&ss=go%2Fgo
// https://lemire.me/blog/2016/06/27/a-fast-alternative-to-the-modulo-reduction/

//go:linkname FastRandN runtime.fastrandn
func FastRandN(n uint32) uint32

const (
	signalChannelSizeCst = 10
	cancelSleepTimeCst   = 20 * time.Second

	tagertHostnameCst = "localhost"
	grpcPortCst       = "8888"

	pollFrequencyCst = 10 * time.Second

	// min recommended is 5 minutes
	// xtcp2 grpc server policy has mintime = 60
	keepaliveTime = 119 * time.Second
	// default 20s
	keepaliveTimeout = 20 * time.Second

	ResourceExhaustedSleepTime = 30 * time.Second
	JitterSleepMaxMs           = 10000

	reconnectTime = 10 * time.Second

	servicePolicyString = `
{
  "loadBalancingConfig": [{"round_robin":{}}],
  "timeout": "10.000000001s",
  "methodConfig": [{
    "name": [{}],
    "waitForReady": true,
    "retryPolicy": {
      "MaxAttempts": 3,
      "InitialBackoff": ".01s",
      "MaxBackoff": "10s",
      "BackoffMultiplier": 2.0,
			"Jitter": 0.3,
      "RetryableStatusCodes": [
        "DEADLINE_EXCEEDED",
        "INTERNAL",
        "UNAVAILABLE",
        "DATA_LOSS"
      ]
    }
  }]
}
`

// https://grpc.io/docs/guides/status-codes/
)

var (
	// Passed by "go build -ldflags" for the show version
	commit  string
	date    string
	version string

	debugLevel uint
)

func main() {

	misc.DieIfNotLinux()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	complete := make(chan struct{}, signalChannelSizeCst)
	go initSignalHandler(cancel, complete)

	target := flag.String("target", tagertHostnameCst, "Target hostanme")
	poll := flag.Bool("poll", false, "poll mode means the client will trigger polling via the PollFlatRecords service")
	pollFrequency := flag.Duration("pollFrequency", pollFrequencyCst, "poll mode frequency")
	workers := flag.Int("workers", 10, "workers")
	json := flag.Bool("json", false, "json output")
	d := flag.Uint("d", 11, "debugLevel")
	v := flag.Bool("v", false, "show version")
	flag.Parse()

	// Print version information passed in via ldflags in the Makefile
	if *v {
		log.Printf("xtcp commit:%s\tdate(UTC):%s\tversion:%s", commit, date, version)
		os.Exit(0)
	}
	debugLevel = *d

	if *poll {
		pollMode(ctx, *target, &complete, *pollFrequency, *json, debugLevel)
	} else {
		listenMode(ctx, *target, *workers, &complete, *json)
	}

}

// func (c *xTCPFlatRecordServiceClient) PollFlatRecords(
// ctx context.Context, opts ...grpc.CallOption) (grpc.BidiStreamingClient[PollFlatRecordsRequest, FlatRecordsResponse], error) {
func pollMode(ctx context.Context, target string, complete *chan struct{}, pollFrequency time.Duration, json bool, debugLevel uint) {

	if debugLevel > 10 {
		log.Printf("pollMode starting")
	}

	conn := newGRPCClient(target + ":" + grpcPortCst)

	client := xtcp_flat_record.NewXTCPFlatRecordServiceClient(conn)

	ticker := time.NewTicker(pollFrequency)

	// shortCtx, cancel := context.WithTimeout(ctx, pollFrequency-time.Duration(10*time.Millisecond))
	// defer cancel()

	stream, err := client.PollFlatRecords(ctx)
	if err != nil {
		log.Fatalf("client.PollFlatRecords(shortCtx) err:%v", err)
	}

	//recvCh := make(chan *xtcp_flat_record.FlatRecordsResponse)
	wg := new(sync.WaitGroup)
	wg.Add(1)
	go pollStreamRecv(ctx, wg, json, &stream, debugLevel)

breakPoint:
	for i := 0; ; i++ {

		if debugLevel > 10 {
			log.Printf("pollMode i:%d", i)
		}

		select {
		case <-ctx.Done():
			break breakPoint

		case <-*complete:
			break breakPoint

		case <-ticker.C:
			stream.Send(&xtcp_flat_record.PollFlatRecordsRequest{})
			if debugLevel > 10 {
				log.Printf("pollMode i:%d <-ticker.C, send", i)
			}
			//default:
			//non-blocking
		}

	}

	wg.Wait()
}

func pollStreamRecv(
	ctx context.Context,
	wg *sync.WaitGroup,
	json bool,
	//recvCh chan *xtcp_flat_record.FlatRecordsResponse,
	stream *grpc.BidiStreamingClient[xtcp_flat_record.PollFlatRecordsRequest, xtcp_flat_record.PollFlatRecordsResponse],
	debugLevel uint) {

	defer wg.Done()

	if debugLevel > 10 {
		log.Printf("pollStreamRecv started")
	}

breakPoint:
	for i := 0; ; i++ {
		pollFlatRecordsResponse, err := (*stream).Recv()
		if debugLevel > 10 {
			log.Printf("pollStreamRecv i:%d .Recv()", i)
		}
		if err == io.EOF {
			if debugLevel > 10 {
				log.Println("pollStreamRecv io.EOF")
			}
			break breakPoint
		}

		if err != nil {
			if debugLevel > 10 {
				log.Printf("pollStreamRecv err:%v", err)
			}

			select {
			case <-ctx.Done():
				break breakPoint
			default:
				// non-blocking
			}
			continue
		}
		//log.Printf("rec:%v", rec)
		printPollFlatRecordsResponse(pollFlatRecordsResponse, 1, json, debugLevel)

		//recvCh <- rec

		select {
		case <-ctx.Done():
			break breakPoint
		default:
			//non-blocking
		}
	}
}

func listenMode(ctx context.Context, target string, workers int, complete *chan struct{}, json bool) {

	var wg sync.WaitGroup
	wg.Add(workers)
	for j := 0; j < workers; j++ {

		conn := newGRPCClient(target + ":" + grpcPortCst)

		go singleStreamingClient(ctx, &wg, conn, json, j)
	}

	wg.Wait()

	*complete <- struct{}{}
}

func singleStreamingClient(ctx context.Context, wg *sync.WaitGroup, conn *grpc.ClientConn, json bool, id int) {

	defer wg.Done()

breakPoint:
	for i := 0; ; i++ {

		select {
		case <-ctx.Done():
			break breakPoint
		default:
			// non-blocking
		}

		wg.Add(1)
		stream(ctx, wg, conn, json, id)

		select {
		case <-ctx.Done():
			break breakPoint
		default:
			// non-blocking
		}

		sleepTime := reconnectTime + (time.Duration(FastRandN(JitterSleepMaxMs)) * time.Millisecond)
		if debugLevel > 10 {
			log.Printf("restarting client i:%d, after sleeping:%0.3f", i, sleepTime.Seconds())
		}

		time.Sleep(sleepTime)

	}
}

func newGRPCClient(target string) *grpc.ClientConn {
	keepalive := &keepalive.ClientParameters{
		Time:                keepaliveTime,
		Timeout:             keepaliveTimeout,
		PermitWithoutStream: true,
	}

	conn, err := grpc.NewClient(
		target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.UseCompressor(gzip.Name)),
		grpc.WithKeepaliveParams(*keepalive),
		grpc.WithDefaultServiceConfig(servicePolicyString),
	)
	if err != nil {
		log.Fatal("Error connecting to gRPC server: ", err.Error())
	}
	//defer conn.Close()
	return conn
}

func stream(ctx context.Context, wg *sync.WaitGroup, conn *grpc.ClientConn, json bool, id int) {

	defer wg.Done()
	defer conn.Close()

	req := &xtcp_flat_record.FlatRecordsRequest{}

	client := xtcp_flat_record.NewXTCPFlatRecordServiceClient(conn)

	stream, err := client.FlatRecords(ctx, req)
	//stream, err := client.FlatRecords(ctx, req, grpc.CallContentSubtype(gzip.Name))
	//stream, err := client.FlatRecords(ctx, req, grpc.UseCompressor(gzip.Name))
	if err != nil {
		log.Fatal("Error making gRPC request: ", err.Error())
	}

breakPoint:
	for i := 0; ; i++ {

		select {
		case <-ctx.Done():
			break breakPoint
		default:
			// non-blocking
		}

		if debugLevel > 10 {
			log.Printf("id: %d waiting for message i:%d", id, i)
		}

		flatRecordsResponse, err := stream.Recv()
		if err == io.EOF {
			continue
		}

		if err != nil {
			if debugLevel > 10 {
				log.Printf("%v.ListFeatures(_) = _, %v", client, err)
			}

			select {
			case <-ctx.Done():
				break breakPoint
			default:
				// non-blocking
			}

			// https://github.com/grpc/grpc-go/blob/master/examples/features/error_handling/client/main.go

			if status.Code(err) != codes.ResourceExhausted {

				sleepTime := ResourceExhaustedSleepTime + (time.Duration(FastRandN(JitterSleepMaxMs)) * time.Millisecond)
				if debugLevel > 10 {
					log.Printf("Received ResourceExhausted error: %v, so sleeping:%0.3f before retry", err, sleepTime.Seconds())
				}
				time.Sleep(sleepTime)
				continue
			}

			printFlatRecordsResponse(flatRecordsResponse, id, json, debugLevel)

			continue
		}

	}

	if debugLevel > 10 {
		log.Printf("stream closing id:%d", id)
	}

}

func printFlatRecordsResponse(flatRecordsResponse *xtcp_flat_record.FlatRecordsResponse, id int, json bool, debugLevel uint) {

	if debugLevel > 10 {
		b, err := proto.Marshal(flatRecordsResponse.GetXtcpFlatRecord())
		if err != nil {
			if debugLevel > 10 {
				log.Println("FlatRecords proto.Marshal(x) err: ", err)
			}
		}
		log.Printf("id:%d, FlatRecords len(b):%d", id, len(b))
	}

	if debugLevel > 10 {
		if json {
			jsonStr := protojson.Format(flatRecordsResponse.GetXtcpFlatRecord())
			log.Printf("id:%d, %s", id, jsonStr)
			return
		}

		log.Printf("id:%d, %s", id, flatRecordsResponse.GetXtcpFlatRecord())
	}
}

func printPollFlatRecordsResponse(pollFlatRecordsResponse *xtcp_flat_record.PollFlatRecordsResponse, id int, json bool, debugLevel uint) {

	if debugLevel > 10 {
		b, err := proto.Marshal(pollFlatRecordsResponse.GetXtcpFlatRecord())
		if err != nil {
			if debugLevel > 10 {
				log.Println("FlatRecords proto.Marshal(x) err: ", err)
			}
		}
		log.Printf("id:%d, FlatRecords len(b):%d", id, len(b))
	}

	if debugLevel > 10 {
		if json {
			jsonStr := protojson.Format(pollFlatRecordsResponse.GetXtcpFlatRecord())
			log.Printf("id:%d, %s", id, jsonStr)
			return
		}

		log.Printf("id:%d, %s", id, pollFlatRecordsResponse.GetXtcpFlatRecord())
	}
}

func initSignalHandler(cancel context.CancelFunc, complete <-chan struct{}) {

	c := make(chan os.Signal, signalChannelSizeCst)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	<-c
	log.Printf("Signal caught, closing application")
	cancel()

	log.Printf("Signal caught, cancel() called, and sleeping to allow goroutines to close")
	timer := time.NewTimer(cancelSleepTimeCst)

	select {
	case <-complete:
		log.Printf("<-complete exit(0)")
	case <-timer.C:
		// if we exit here, this means all the other go routines didn't shutdown
		// need to investigate why
		log.Printf("Sleep complete, goodbye! exit(0)")
	}

	os.Exit(0)
}
