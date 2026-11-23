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

// runMain wires flag parsing + server fan-out. Extracted so tests can drive
// it with a cancellable ctx + synthetic args without subprocessing. The ctx
// argument lets tests bind once + cancel; production passes context.Background.
func runMain(ctx context.Context, args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("tcp_server", flag.ContinueOnError)
	fs.SetOutput(stderr)
	count := fs.Int("count", countCst, "count")
	bind := fs.String("bind", bindCst, "bind")
	network := fs.String("network", networkCst, "tcp (family from -bind; 0.0.0.0 is dual-stack), tcp4 or tcp6")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if _, _, err := listenTarget(*network, *bind, startPort+*count-1); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}

	var wg sync.WaitGroup
	for i := 0; i < *count; i++ {
		wg.Add(1)
		go func(port int) {
			defer wg.Done()
			if err := runServer(ctx, *network, *bind, port); err != nil {
				log.Printf("runServer port=%d err=%v", port, err)
			}
		}(startPort + i)
	}
	wg.Wait()
	return 0
}

// runServer binds <bind:port> and echoes each accepted connection. Returns
// when ctx is canceled (after closing the listener) or on a hard listener
// error. Extracted from main() / server() so tests can drive it with a
// 0-port bind and ctx.Cancel() instead of a panic loop.
func runServer(ctx context.Context, network, bind string, port int) error {
	network, addr, err := listenTarget(network, bind, port)
	if err != nil {
		return err
	}
	lc := net.ListenConfig{}
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
		go handleConn(conn)
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

// handleConn echoes bytes back to the connection until EOF or error.
func handleConn(conn net.Conn) {
	defer func() {
		if cerr := conn.Close(); cerr != nil {
			log.Printf("handleConn: conn close: %v", cerr)
		}
	}()
	if _, cerr := io.Copy(conn, conn); cerr != nil {
		log.Printf("echo copy: %v", cerr)
	}
}
