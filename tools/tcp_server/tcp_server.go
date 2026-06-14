package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
)

const (
	startPort = 4000

	countCst = 10

	bindCst = "0.0.0.0"
)

func main() {

	count := flag.Int("count", countCst, "count")
	bind := flag.String("bind", bindCst, "bind")

	flag.Parse()

	ctx := context.Background()
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
}

// runServer binds <bind:port> and echoes each accepted connection. Returns
// when ctx is cancelled (after closing the listener) or on a hard listener
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
	go func() {
		<-ctx.Done()
		_ = ln.Close() //nolint:errcheck // shutdown path
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
