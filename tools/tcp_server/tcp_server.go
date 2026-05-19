package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sync"
)

const (
	startPort = 4000

	countCst = 10

	bindCst = "0.0.0.0"
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
	if err := fs.Parse(args); err != nil {
		return 2
	}

	var wg sync.WaitGroup
	for i := 0; i < *count; i++ {
		wg.Add(1)
		go func(port int) {
			defer wg.Done()
			if err := runServer(ctx, *bind, port); err != nil {
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
func runServer(ctx context.Context, bind string, port int) error {
	lc := net.ListenConfig{}
	ln, err := lc.Listen(ctx, "tcp", fmt.Sprintf("%s:%d", bind, port))
	if err != nil {
		return fmt.Errorf("listen %s:%d: %w", bind, port, err)
	}
	defer func() { _ = ln.Close() }() //nolint:errcheck // demo server teardown

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
			_ = ln.Close() //nolint:errcheck // shutdown path
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

// handleConn echoes bytes back to the connection until EOF or error.
func handleConn(conn net.Conn) {
	defer func() { _ = conn.Close() }() //nolint:errcheck // demo server teardown
	_, _ = io.Copy(conn, conn)          //nolint:errcheck // demo server teardown
}
