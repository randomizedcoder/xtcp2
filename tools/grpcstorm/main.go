// grpcstorm drives gRPC load shapes against an xtcp2 listener — the stream
// storm and stream-grazing tools for rt-4-3/rt-11-3/rt-4-4
// (docs/redteam-plan.md).
//
// Modes:
//
//	storm (default): -conns C connections, each with -streams S
//	server-streaming FlatRecords RPCs, every stream reading at -read-rate
//	records/second (0 = never read: the never-draining shape that wedges the
//	server's per-stream send window). Holds for -duration, then closes and
//	prints GRPCSTORM_DONE. Per-stream counters print on close.
//
//	oversize (-oversize N): one PollFlatRecords RPC per connection carrying
//	an N-byte unknown field — over MaxRecvMsgSize (4 MB) the server answers
//	ResourceExhausted; prints the per-RPC status and exits. This is the
//	transport-cap probe (the recv-message-size backstop).
//
// Auth: -token or -token-file (raw listener token), or -hmac-key/-hmac-key-file
// (the signed-token listener shape, XTCP2_AUTH=hmac), is injected with
// listenerauth, the same as xtcp2client.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_flat_record"
	"github.com/randomizedcoder/xtcp2/pkg/listenerauth"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

func main() {
	target := flag.String("target", "", "host:port to dial")
	conns := flag.Int("conns", 1, "connections to open")
	streamsPer := flag.Int("streams", 1, "FlatRecords streams per connection")
	readRate := flag.Float64("read-rate", 0, "records per second per stream (0 = never read)")
	duration := flag.Duration("duration", 0, "hold duration, then close (0 = until SIGTERM)")
	token := flag.String("token", "", "raw listener token")
	tokenFile := flag.String("token-file", "", "file containing the raw listener token")
	hmacKey := flag.String("hmac-key", "", "HMAC shared key (the XTCP2_AUTH=hmac listener shape)")
	hmacKeyFile := flag.String("hmac-key-file", "", "file containing the HMAC shared key")
	oversize := flag.Int("oversize", 0, "bytes of unknown-field payload for the recv-cap probe (exits after)")
	flag.Parse()

	if *target == "" {
		fmt.Fprintln(os.Stderr, "grpcstorm: -target is required")
		os.Exit(2)
	}
	auth, err := listenerauth.ClientAuthFromValues(*token, *tokenFile, *hmacKey, *hmacKeyFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "grpcstorm: auth: %v\n", err)
		os.Exit(2)
	}

	// os.Exit lives in main, not run, so the deferred cleanup in run() is
	// not skipped (gocritic exitAfterDefer — same split as slowloris).
	os.Exit(run(*target, *conns, *streamsPer, *readRate, *duration, *oversize, auth))
}

func run(target string, conns, streamsPer int, readRate float64, duration time.Duration, oversize int, auth listenerauth.ClientAuth) int {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	if duration > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, duration)
		defer cancel()
	}

	if oversize > 0 {
		return oversizeProbe(ctx, target, conns, oversize, auth)
	}
	return storm(ctx, target, conns, streamsPer, readRate, auth)
}

// oversizeProbe sends one oversized PollFlatRecords RPC per connection and
// prints each outcome. Exit 0 when every RPC got a terminal error (the cap
// answered); 1 otherwise.
func oversizeProbe(ctx context.Context, target string, conns, size int, auth listenerauth.ClientAuth) int {
	exit := 0
	for i := 0; i < conns; i++ {
		conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			fmt.Printf("GRPCSTORM_OVERSIZE conn=%d dial=%v\n", i, err)
			exit = 1
			continue
		}
		rpcCtx, cancel := context.WithTimeout(listenerauth.ContextWithClientAuth(ctx, auth, time.Now()), 10*time.Second)
		stream, err := xtcp_flat_record.NewXTCPFlatRecordServiceClient(conn).PollFlatRecords(rpcCtx)
		if err == nil {
			req := &xtcp_flat_record.PollFlatRecordsRequest{}
			req.ProtoReflect().SetUnknown(make([]byte, size))
			err = stream.Send(req)
			if err == nil {
				// CloseAndRecv-shaped drain: the server's verdict arrives
				// on Recv (or the send fails).
				_, err = stream.Recv()
			}
		}
		cancel()
		if err == nil {
			fmt.Printf("GRPCSTORM_OVERSIZE conn=%d status=OK (the oversized request was ACCEPTED?)\n", i)
			exit = 1
		} else {
			fmt.Printf("GRPCSTORM_OVERSIZE conn=%d status=%s\n", i, status.Code(err))
		}
		_ = conn.Close() //nolint:errcheck // probe teardown: a close error changes nothing
	}
	return exit
}

// storm opens conns × streams FlatRecords RPCs, reading each at rate
// records/second. Returns the process exit code.
func storm(ctx context.Context, target string, conns, streamsPer int, readRate float64, auth listenerauth.ClientAuth) int {
	var mu sync.Mutex
	received := make([]int, conns*streamsPer)

	var wg sync.WaitGroup
	for c := 0; c < conns; c++ {
		conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			fmt.Fprintf(os.Stderr, "GRPCSTORM_ERR dial %d: %v\n", c, err)
			continue
		}
		for s := 0; s < streamsPer; s++ {
			idx := c*streamsPer + s
			wg.Add(1)
			go func(idx int, conn *grpc.ClientConn) {
				defer wg.Done()
				n := streamOnce(ctx, conn, readRate, auth)
				mu.Lock()
				received[idx] = n
				mu.Unlock()
			}(idx, conn)
		}
	}
	fmt.Printf("GRPCSTORM_READY conns=%d streams=%d read_rate=%v\n",
		conns, conns*streamsPer, readRate)
	wg.Wait()
	total := 0
	for _, n := range received {
		total += n
	}
	fmt.Printf("GRPCSTORM_DONE total_records=%d\n", total)
	return 0
}

// streamOnce opens one FlatRecords stream and reads from it until ctx ends,
// pacing reads at rate records/second. Returns the number of records read.
func streamOnce(ctx context.Context, conn *grpc.ClientConn, rate float64, auth listenerauth.ClientAuth) int {
	sctx, cancel := context.WithCancel(listenerauth.ContextWithClientAuth(ctx, auth, time.Now()))
	defer cancel()
	stream, err := xtcp_flat_record.NewXTCPFlatRecordServiceClient(conn).FlatRecords(sctx, &xtcp_flat_record.FlatRecordsRequest{})
	if err != nil {
		return 0
	}
	if rate == 0 {
		// Never-reading shape: hold the stream open and drain nothing.
		// The server still sends into it (flatRecordServiceSend pushes to
		// every stream), so the per-stream send window fills and the
		// server-side Send blocks — the wedge rt-11-3 measures.
		<-ctx.Done()
		return 0
	}
	n := 0
	interval := time.Duration(float64(time.Second) / rate)
	for {
		if ctx.Err() != nil {
			return n
		}
		if n > 0 {
			select {
			case <-time.After(interval):
			case <-ctx.Done():
				return n
			}
		}
		_, err = stream.Recv()
		if err != nil {
			if ctx.Err() == nil && status.Code(err) != codes.Canceled {
				fmt.Fprintf(os.Stderr, "GRPCSTORM_STREAM_ERR %v\n", err)
			}
			return n
		}
		n++
	}
}
