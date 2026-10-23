package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
	"github.com/randomizedcoder/xtcp2/pkg/listener"
)

// ipRejectLogEvery bounds the log of connections the /ip listener closes, so
// a peer retrying in a loop cannot flood the log.
const ipRejectLogEvery = time.Minute

// ipServerStarter is the indirection point for the /ip listener, as
// promHandlerStarter is for the prom listener.
var ipServerStarter = startIPServer

// ipListenAddress returns the address of the /ip listener: ip_listen, checked
// to be a loopback host:port, or "" when /ip stays on the Prometheus listener
// or is not served. ip_listen without ip_paths is an error rather than a
// listener serving nothing; with the monitor disabled there is nothing to
// serve, so no listener.
func ipListenAddress(m *xtcp_config.LinkMonitorConfig) (string, error) {
	addr := m.GetIpListen()
	if addr == "" {
		return "", nil
	}
	if !m.GetIpPaths() {
		return "", fmt.Errorf("linkMonitorIPListen %q needs linkMonitorIPPaths", addr)
	}
	if _, err := listener.ParseLoopback(addr); err != nil {
		return "", fmt.Errorf("linkMonitorIPListen: %w", err)
	}
	if !m.GetEnable() {
		return "", nil
	}
	return addr, nil
}

// startIPServer binds the /ip listener now, so a bad address or a port in use
// fails startup as the prom listener's would, then serves the late-bound /ip
// routes on a worker that joins wg and drains when ctx ends. The listener has
// no listener auth: it accepts loopback peers only, who could run `ip` anyway.
func startIPServer(ctx context.Context, wg *sync.WaitGroup, m *xtcp_config.LinkMonitorConfig) error {
	addr, err := ipListenAddress(m)
	if err != nil || addr == "" {
		return err
	}
	// TTL and hop limit 1: replies never pass a router.
	ln, err := listener.Listen(ctx, listener.Endpoint{Network: listener.NetworkTCP, Address: addr}, 1, 1)
	if err != nil {
		return fmt.Errorf("ip listener: %w", err)
	}
	ln = listener.LoopbackOnly(ln, rateLimitedRejectLog(ipRejectLogEvery))
	wg.Add(1)
	go serveHTTP(ctx, wg, "ip listener", newPromServer(linkMonitorHTTP.ipRoutes()), ln, promShutdownGrace, func(err error) {
		// /ip is a convenience: losing it must not stop the collection.
		log.Printf("ip listener stopped: %v; /ip is unavailable until xtcp2 restarts", err)
	})
	log.Printf("ip listener: /ip and /i on %s, loopback peers only", addr)
	return nil
}

// rateLimitedRejectLog logs a closed non-loopback connection at most once
// per every, with the count closed since the last line, and the close error
// of the connection that line reports, if any.
func rateLimitedRejectLog(every time.Duration) func(net.Addr, error) {
	var last atomic.Int64
	var count atomic.Int64
	return func(a net.Addr, closeErr error) {
		n := count.Add(1)
		now := time.Now().UnixNano()
		prev := last.Load()
		if now-prev < int64(every) || !last.CompareAndSwap(prev, now) {
			return
		}
		count.Add(-n)
		if closeErr != nil {
			log.Printf("ip listener: closed %d connection(s) from non-loopback peers, last from %s: %v", n, a, closeErr)
			return
		}
		log.Printf("ip listener: closed %d connection(s) from non-loopback peers, last from %s", n, a)
	}
}
