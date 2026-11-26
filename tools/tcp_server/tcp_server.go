package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/netip"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/randomizedcoder/xtcp2/tools/internal/sockopt"
)

const (
	startPort = 4000

	countCst = 10

	bindCst = "0.0.0.0"

	// networkCst "tcp" lets Go pick the family from -bind: 0.0.0.0 or ::
	// opens a dual-stack AF_INET6 listener. tcp4 and tcp6 pin it.
	networkCst = "tcp"
)

func main() {
	os.Exit(runMain(context.Background(), os.Args[1:], os.Stderr))
}

// serverConfig is the parsed command line.
type serverConfig struct {
	count   int
	bind    string
	network string
	holdEOF bool
	opts    sockopt.Options
	// keepalive is net.ListenConfig.KeepAlive for accepted connections: 0
	// is Go's default (on, 15 s), a negative value is off.
	keepalive time.Duration
}

// parseFlags reads the command line into a serverConfig. Every error is
// also printed to stderr (flag errors with usage).
func parseFlags(args []string, stderr io.Writer) (serverConfig, error) {
	var c serverConfig
	fs := flag.NewFlagSet("tcp_server", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.IntVar(&c.count, "count", countCst, "count")
	fs.StringVar(&c.bind, "bind", bindCst, "bind")
	fs.StringVar(&c.network, "network", networkCst, "tcp (family from -bind; 0.0.0.0 is dual-stack), tcp4 or tcp6")
	fs.DurationVar(&c.keepalive, "keepalive", 0, "TCP keepalive idle time of accepted connections; 0 = Go's default (15s), negative = off")
	fs.BoolVar(&c.holdEOF, "holdEOF", false, "keep a connection open after the client's EOF (CLOSE_WAIT) until the server stops")
	c.opts.Register(fs)
	if err := fs.Parse(args); err != nil {
		return serverConfig{}, err
	}
	if err := c.opts.Validate(); err != nil {
		fmt.Fprintln(stderr, err)
		return serverConfig{}, err
	}
	return c, nil
}

// runMain wires flag parsing + server fan-out. Extracted so tests can drive
// it with a cancellable ctx + synthetic args without subprocessing. The ctx
// argument lets tests bind once + cancel; production passes context.Background.
func runMain(ctx context.Context, args []string, stderr io.Writer) int {
	c, err := parseFlags(args, stderr)
	if err != nil {
		return 2
	}
	if _, _, err := listenTarget(c.network, c.bind, startPort+c.count-1); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}

	var wg sync.WaitGroup
	for i := 0; i < c.count; i++ {
		wg.Add(1)
		go func(port int) {
			defer wg.Done()
			if err := runServer(ctx, c, port); err != nil {
				log.Printf("runServer port=%d err=%v", port, err)
			}
		}(startPort + i)
	}
	wg.Wait()
	return 0
}

// listenConfig is the ListenConfig for c: its socket options and the
// keepalive of accepted connections.
func listenConfig(c serverConfig) net.ListenConfig {
	lc := net.ListenConfig{KeepAlive: c.keepalive}
	if c.opts != (sockopt.Options{}) {
		lc.Control = c.opts.Control
	}
	return lc
}

// runServer binds <bind:port> and echoes each accepted connection. Returns
// when ctx is canceled (after closing the listener) or on a hard listener
// error. Extracted from main() / server() so tests can drive it with a
// 0-port bind and ctx.Cancel() instead of a panic loop.
func runServer(ctx context.Context, c serverConfig, port int) error {
	network, addr, err := listenTarget(c.network, c.bind, port)
	if err != nil {
		return err
	}
	lc := listenConfig(c)
	setOpts := c.opts != (sockopt.Options{})
	ln, err := lc.Listen(ctx, network, addr)
	if err != nil {
		return fmt.Errorf("listen %s %s: %w", network, addr, err)
	}
	defer func() {
		if cerr := ln.Close(); cerr != nil {
			log.Printf("runServer: listener close: %v", cerr)
		}
	}()

	// Close the listener on ctx cancel so the blocking Accept returns.
	// The stopCloseWatcher channel lets the watcher goroutine exit if
	// runServer returns early (Accept produced a non-ctx error) —
	// without it, the watcher stayed parked on <-ctx.Done() forever,
	// leaking one goroutine per runServer invocation. The test harness
	// invokes runServer in a fan-out loop, so the leak adds up.
	stopCloseWatcher := make(chan struct{})
	defer close(stopCloseWatcher)
	go func() {
		select {
		case <-ctx.Done():
			if cerr := ln.Close(); cerr != nil {
				log.Printf("runServer: listener close on ctx done: %v", cerr)
			}
		case <-stopCloseWatcher:
		}
	}()

	for {
		conn, aerr := ln.Accept()
		if aerr != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("accept: %w", aerr)
		}
		// Not every listener option reaches an accepted socket (see
		// sockopt.Options.ApplyConn), so set them again.
		if setOpts {
			if tc, ok := conn.(*net.TCPConn); ok {
				if oerr := c.opts.ApplyConn(tc); oerr != nil {
					log.Printf("runServer: accepted socket options: %v", oerr)
				}
			}
		}
		go handleConn(ctx, conn, c.holdEOF)
	}
}

// listenTarget returns the network and address runServer listens on for
// -network, -bind and one port.
func listenTarget(network, bind string, port int) (string, string, error) {
	if err := checkFamily(network, "-bind", bind); err != nil {
		return "", "", err
	}
	if port < 1 || port > 65535 {
		return "", "", fmt.Errorf("port %d is outside 1-65535", port)
	}
	return network, net.JoinHostPort(bind, strconv.Itoa(port)), nil
}

// checkFamily rejects a network other than tcp, tcp4 and tcp6, and an
// address literal of the other family for tcp4 or tcp6 (a host name is
// left to the resolver).
func checkFamily(network, flag, host string) error {
	switch network {
	case "tcp", "tcp4", "tcp6":
	default:
		return fmt.Errorf("-network: %q is not tcp, tcp4 or tcp6", network)
	}
	a, err := netip.ParseAddr(host)
	if err != nil || network == "tcp" {
		return nil
	}
	if network == "tcp4" && !a.Is4() {
		return fmt.Errorf("%s %s is not an IPv4 address, as -network tcp4 needs", flag, host)
	}
	if network == "tcp6" && a.Is4() {
		return fmt.Errorf("%s %s is not an IPv6 address, as -network tcp6 needs", flag, host)
	}
	return nil
}

// handleConn echoes bytes back to the connection until EOF or error. With
// holdEOF it then keeps the socket (CLOSE_WAIT) until ctx is done.
func handleConn(ctx context.Context, conn net.Conn, holdEOF bool) {
	defer func() {
		if cerr := conn.Close(); cerr != nil {
			log.Printf("handleConn: conn close: %v", cerr)
		}
	}()
	if _, cerr := io.Copy(conn, conn); cerr != nil {
		log.Printf("echo copy: %v", cerr)
	}
	if holdEOF {
		<-ctx.Done()
	}
}
