package main

import (
	"bufio"
	"context"
	"net"
	"testing"
	"time"
)

func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()
	_, port, _ := net.SplitHostPort(l.Addr().String())
	return net.JoinHostPort("127.0.0.1", port)
}

// TestDribbleNeverCompletesHeaders: the connection stays mid-header forever —
// the server sees a partial request line and no terminator, so its read
// deadline (not a completed request) is the only thing that can end it.
func TestDribbleNeverCompletesHeaders(t *testing.T) {
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()

	served := make(chan net.Conn, 1)
	go func() {
		c, err := l.Accept()
		if err != nil {
			return
		}
		served <- c
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conns, err := dialAll(ctx, l.Addr().String(), 1, 0)
	if err != nil {
		t.Fatalf("dialAll: %v", err)
	}
	defer func() {
		for _, c := range conns {
			_ = c.SetLinger(0)
			_ = c.Close()
		}
	}()

	go dribble(ctx, conns[0], "/metrics", 1, 200*time.Millisecond)

	select {
	case c := <-served:
		br := bufio.NewReader(c)
		// Read with a deadline: the dribble writes a byte every 200 ms, so
		// a first byte must arrive, but a full request line (terminated by
		// \r\n) must never arrive no matter how long we wait.
		_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
		line, err := br.ReadString('\n')
		if err == nil && len(line) > 0 && line[len(line)-1] == '\n' {
			t.Fatalf("server received a terminated line %q — the dribble completed a header", line)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no connection reached the listener")
	}
}

// TestDialAllRamp: with a ramp, connections spread across the window rather
// than landing together.
func TestDialAllRamp(t *testing.T) {
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()

	accepted := make(chan time.Time, 8)
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			accepted <- time.Now()
			_ = c.Close()
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := time.Now()
	conns, err := dialAll(ctx, l.Addr().String(), 4, 800*time.Millisecond)
	if err != nil {
		t.Fatalf("dialAll: %v", err)
	}
	defer func() {
		for _, c := range conns {
			_ = c.SetLinger(0)
			_ = c.Close()
		}
	}()

	// dialAll returns after the LAST dial, which with a ramp is near the end
	// of the window.
	if took := time.Since(start); took < 600*time.Millisecond {
		t.Errorf("ramp dialAll returned after %v, expected it to spread over ~800ms", took)
	}
}
