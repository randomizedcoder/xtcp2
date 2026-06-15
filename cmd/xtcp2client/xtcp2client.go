package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
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
	os.Exit(runMain(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

// runMain wires flag parsing + pollMode/listenMode dispatch. Extracted so
// tests can drive it with synthetic args + a cancellable ctx (no gRPC
// server needed). Tests that exercise pollMode/listenMode use bufconn
// directly.
func runMain(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("xtcp2client", flag.ContinueOnError)
	fs.SetOutput(stderr)
	target := fs.String("target", tagertHostnameCst, "Target hostanme")
	poll := fs.Bool("poll", false, "poll mode means the client will trigger polling via the PollFlatRecords service")
	pollFrequency := fs.Duration("pollFrequency", pollFrequencyCst, "poll mode frequency")
	workers := fs.Int("workers", 10, "workers")
	json := fs.Bool("json", false, "json output")
	d := fs.Uint("d", 11, "debugLevel")
	v := fs.Bool("v", false, "show version")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *v {
		fmt.Fprintf(stdout, "xtcp commit:%s\tdate(UTC):%s\tversion:%s\n", commit, date, version)
		return 0
	}
	debugLevel = *d

	complete := make(chan struct{}, signalChannelSizeCst)
	addr := *target + ":" + grpcPortCst
	if *poll {
		pollMode(ctx, addr, &complete, *pollFrequency, *json, debugLevel)
	} else {
		listenMode(ctx, addr, *workers, &complete, *json)
	}
	return 0
}

// func (c *xTCPFlatRecordServiceClient) PollFlatRecords(
// ctx context.Context, opts ...grpc.CallOption) (grpc.BidiStreamingClient[PollFlatRecordsRequest, FlatRecordsResponse], error) {
func pollMode(ctx context.Context, addr string, complete *chan struct{}, pollFrequency time.Duration, json bool, debugLevel uint) {

	if debugLevel > 10 {
		log.Printf("pollMode starting")
	}

	conn := newGRPCClient(addr)
	// Close the conn + Stop the ticker on the way out. Previously
	// pollMode returned with both leaked — fine in a one-shot CLI run
	// but the daemon-embedded usage (and the test harness) leaked one
	// conn + one *time.Ticker per pollMode invocation.
	defer func() { _ = conn.Close() }() //nolint:errcheck // already on the way out; Close err is non-actionable

	client := xtcp_flat_record.NewXTCPFlatRecordServiceClient(conn)

	ticker := time.NewTicker(pollFrequency)
	defer ticker.Stop()

	// shortCtx, cancel := context.WithTimeout(ctx, pollFrequency-time.Duration(10*time.Millisecond))
	// defer cancel()

	stream, err := client.PollFlatRecords(ctx)
	if err != nil {
		log.Fatalf("client.PollFlatRecords(shortCtx) err:%v", err)
	}

	// recvCh := make(chan *xtcp_flat_record.FlatRecordsResponse)
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
			if serr := stream.Send(&xtcp_flat_record.PollFlatRecordsRequest{}); serr != nil {
				log.Printf("pollMode i:%d stream.Send err:%v — stopping poll loop", i, serr)
				break breakPoint
			}
			if debugLevel > 10 {
				log.Printf("pollMode i:%d <-ticker.C, send", i)
			}
			// default:
			// non-blocking
		}

	}

	wg.Wait()
}

func pollStreamRecv(
	ctx context.Context,
	wg *sync.WaitGroup,
	json bool,
	// recvCh chan *xtcp_flat_record.FlatRecordsResponse,
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

			// A broken stream typically returns the same error immediately
			// on every subsequent Recv (e.g. "rpc error: stream closed").
			// The previous code looped without backoff, pegging a CPU core
			// at 100% until ctx was canceled. Sleep briefly between
			// retries so the loop is ctx-cancellable AND non-spinny.
			select {
			case <-ctx.Done():
				break breakPoint
			case <-time.After(100 * time.Millisecond):
			}
			continue
		}
		// log.Printf("rec:%v", rec)
		printPollFlatRecordsResponse(pollFlatRecordsResponse, 1, json, debugLevel)

		// recvCh <- rec

		select {
		case <-ctx.Done():
			break breakPoint
		default:
			// non-blocking
		}
	}
}

func listenMode(ctx context.Context, addr string, workers int, complete *chan struct{}, json bool) {

	var wg sync.WaitGroup
	wg.Add(workers)
	for j := 0; j < workers; j++ {
		// singleStreamingClient now owns the conn lifetime — it
		// re-dials per reconnect iteration. Previously listenMode
		// dialed once and passed the conn down, but stream()
		// deferred-Close'd it on first return, so every "reconnect
		// after sleep" iteration after the first used a dead conn.
		go singleStreamingClient(ctx, &wg, addr, json, j)
	}

	wg.Wait()

	if complete != nil {
		select {
		case *complete <- struct{}{}:
		default:
		}
	}
}

// reconnectTimeVar wraps the production reconnectTime constant so tests
// can swap in a much shorter sleep to exercise the per-iteration
// restart branch without waiting 10 seconds.
var reconnectTimeVar = reconnectTime

func singleStreamingClient(ctx context.Context, wg *sync.WaitGroup, addr string, json bool, id int) {

	defer wg.Done()

breakPoint:
	for i := 0; ; i++ {

		select {
		case <-ctx.Done():
			break breakPoint
		default:
			// non-blocking
		}

		// Re-dial per iteration. stream() defer-Close()s the conn it
		// receives, so a single dial reused across iterations would
		// hand a closed conn to every reconnect — the original code
		// had this bug; the reconnect-with-sleep loop was effectively
		// dead code after iteration 0.
		conn := newGRPCClient(addr)
		wg.Add(1)
		stream(ctx, wg, conn, json, id)

		select {
		case <-ctx.Done():
			break breakPoint
		default:
			// non-blocking
		}

		sleepTime := reconnectTimeVar + (time.Duration(FastRandN(JitterSleepMaxMs)) * time.Millisecond)
		if debugLevel > 10 {
			log.Printf("restarting client i:%d, after sleeping:%0.3f", i, sleepTime.Seconds())
		}

		// time.Sleep ignores ctx — Ctrl-C should shut the client down
		// promptly even mid-reconnect-backoff, not after a full
		// reconnectTime + jitter wait.
		select {
		case <-ctx.Done():
			break breakPoint
		case <-time.After(sleepTime):
		}

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
	// defer conn.Close()
	return conn
}

// recvAction is the per-iteration decision the stream loop takes after
// stream.Recv returns. Either keep reading (continue), stop reading
// (break), or print the just-received response.
type recvAction int

const (
	recvBreak recvAction = iota
	recvContinue
	recvPrint
)

// classifyRecvErr maps a stream.Recv error into one of the recvAction
// outcomes. EOF + ctx-cancel are break. ResourceExhausted is a
// backoff-then-continue (the inner sleep is done by the caller because
// it needs the same ctx). Other errors fall through to continue —
// nothing useful to print since the response is nil on error.
func classifyRecvErr(err error) recvAction {
	if err == io.EOF {
		// End of stream — the server closed cleanly. Subsequent Recv
		// calls keep returning io.EOF, so caller breaks out of the loop
		// so the singleStreamingClient reconnect-with-sleep path
		// re-establishes a fresh stream.
		return recvBreak
	}
	if err != nil {
		return recvContinue
	}
	return recvPrint
}

// resourceExhaustedSleep waits jittered ResourceExhaustedSleepTime or
// until ctx is cancelled, whichever comes first. Returns true if the
// caller should break the loop (ctx cancelled during the wait).
func resourceExhaustedSleep(ctx context.Context, err error) (cancelled bool) {
	sleepTime := ResourceExhaustedSleepTime + (time.Duration(FastRandN(JitterSleepMaxMs)) * time.Millisecond)
	if debugLevel > 10 {
		log.Printf("Received ResourceExhausted error: %v, so sleeping:%0.3f before retry", err, sleepTime.Seconds())
	}
	select {
	case <-ctx.Done():
		return true
	case <-time.After(sleepTime):
		return false
	}
}

// ctxDone is a non-blocking ctx.Err() check — equivalent to the
// `select { case <-ctx.Done(): default: }` pattern but linear so it
// doesn't contribute to the surrounding function's cyclomatic.
func ctxDone(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

// handleRecvContinueErr applies the post-classifyRecvErr handling for
// a recvContinue outcome: optional debug log, ctx-cancel check,
// ResourceExhausted backoff. Returns true when the caller should
// break out of the stream loop, false to continue.
func handleRecvContinueErr(ctx context.Context, client any, err error) bool {
	if debugLevel > 10 {
		log.Printf("%v.ListFeatures(_) = _, %v", client, err)
	}
	if ctxDone(ctx) {
		return true
	}
	if status.Code(err) == codes.ResourceExhausted {
		if resourceExhaustedSleep(ctx, err) {
			return true
		}
	}
	return false
}

func stream(ctx context.Context, wg *sync.WaitGroup, conn *grpc.ClientConn, json bool, id int) {

	defer wg.Done()
	defer func() { _ = conn.Close() }() //nolint:errcheck // streaming client teardown; conn.Close err is non-actionable

	req := &xtcp_flat_record.FlatRecordsRequest{}
	client := xtcp_flat_record.NewXTCPFlatRecordServiceClient(conn)
	stream, err := client.FlatRecords(ctx, req)
	if err != nil {
		// Demoted from log.Fatal: the surrounding singleStreamingClient
		// loop is a "reconnect after sleep" loop (bug 65). A Fatal here
		// killed the whole client every time FlatRecords creation
		// failed (e.g., server briefly unreachable), defeating the
		// retry. Log + return so the caller's sleep+restart fires.
		log.Printf("Error making gRPC request: %v", err)
		return
	}

	for i := 0; ; i++ {
		if ctxDone(ctx) {
			break
		}
		if debugLevel > 10 {
			log.Printf("id: %d waiting for message i:%d", id, i)
		}
		resp, rerr := stream.Recv()
		switch classifyRecvErr(rerr) {
		case recvBreak:
			return
		case recvPrint:
			printFlatRecordsResponse(resp, id, json, debugLevel)
			continue
		}
		// recvContinue: classify the err further, optionally backoff.
		if handleRecvContinueErr(ctx, client, rerr) {
			break
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
