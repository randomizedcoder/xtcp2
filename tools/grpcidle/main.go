// grpcidle holds N gRPC client connections open against a target — the
// FD-pin shape for rt-11-2 (docs/redteam-plan.md).
//
// Unlike HTTP conns (whose ReadHeaderTimeout caps total header time at 5 s)
// a gRPC conn with no streams idles for MaxConnectionIdle (15 min on
// xtcp2's server), so N idle conns pin the server's FD budget with no
// accept/reap churn. Establishing the conn needs one RPC: the standard
// grpc.health.v1 check (registered on every xtcp2 gRPC listener). The RPC
// may fail (auth, transport) — the conn is established either way and then
// idles, which is exactly the held state.
//
// On SIGTERM/SIGINT every conn is closed and the process exits.
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

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

func main() {
	target := flag.String("target", "", "host:port to hold gRPC connections against")
	n := flag.Int("n", 0, "connections to open and idle")
	dialTimeout := flag.Duration("dial-timeout", 10*time.Second, "per-conn RPC deadline")
	workersFlag := flag.Int("workers", 64, "concurrent dialers during the ramp")
	flag.Parse()

	if *target == "" || *n <= 0 {
		fmt.Fprintln(os.Stderr, "grpcidle: -target and -n are required")
		os.Exit(2)
	}
	workers := *workersFlag

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	conns := dialAll(ctx, *target, *n, *dialTimeout, workers)
	fmt.Printf("GRPCIDLE_READY conns=%d target=%s\n", len(conns), *target)

	<-ctx.Done()

	var wg sync.WaitGroup
	for _, c := range conns {
		wg.Add(1)
		go func(c *grpc.ClientConn) {
			defer wg.Done()
			_ = c.Close()
		}(c)
	}
	wg.Wait()
	fmt.Println("GRPCIDLE_DONE")
}

// dialAll opens n conns (over `workers` goroutines) and establishes each
// with one health-check RPC (established = the conn's transport is up; the
// RPC itself may error — auth, health not serving — which is fine for the
// held state). Parallel because a target that answers slowly (e.g. an
// auth-gated listener with deliberate per-RPC jitter) would otherwise pace
// the ramp one RPC at a time.
func dialAll(ctx context.Context, target string, n int, dialTimeout time.Duration, workers int) []*grpc.ClientConn {
	var mu sync.Mutex
	conns := make([]*grpc.ClientConn, 0, n)
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range jobs {
				c, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
				if err != nil {
					fmt.Fprintf(os.Stderr, "GRPCIDLE_ERR dial: %v\n", err)
					continue
				}
				// One RPC to force the lazy transport up.
				rpcCtx, cancel := context.WithTimeout(ctx, dialTimeout)
				_, _ = healthpb.NewHealthClient(c).Check(rpcCtx, &healthpb.HealthCheckRequest{}) //nolint:errcheck // any outcome (auth, transport, serving) establishes the conn — the held state
				cancel()
				mu.Lock()
				conns = append(conns, c)
				mu.Unlock()
			}
		}()
	}
	for i := 0; i < n; i++ {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return conns
}
