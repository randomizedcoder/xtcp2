package xtcp

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestTCPDest_send drives the tcp destination against a real in-process TCP
// listener and asserts the marshaled payload arrives verbatim (framing is the
// marshaller's job, so tcpDest writes bytes unchanged).
func TestTCPDest_send(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	got := make(chan []byte, 1)
	go func() {
		conn, aerr := ln.Accept()
		if aerr != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		b, _ := io.ReadAll(conn) // returns when the client closes its side
		got <- b
	}()

	x := newTestXTCP(t, schemeTCP+":"+ln.Addr().String())
	d, err := newTCPDest(context.Background(), x)
	if err != nil {
		t.Fatalf("newTCPDest: %v", err)
	}

	payload := []byte("hello-tcp\n")
	b := append([]byte(nil), payload...)
	n, err := d.Send(context.Background(), &b)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if n != 1 {
		t.Errorf("Send n = %d, want 1 (one record)", n)
	}
	if err := d.Close(); err != nil { // close so the server's ReadAll returns
		t.Errorf("Close: %v", err)
	}

	select {
	case rc := <-got:
		if !bytes.Equal(rc, payload) {
			t.Errorf("received %q, want %q", rc, payload)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("timed out waiting for the tcp payload")
	}
}

// TestTCPDest_reconnect drives #339's contract: a write failure closes the
// conn and the next attempt re-dials, so a sink restart heals delivery
// without restarting the daemon. The server reads one payload, kills the
// conn, then accepts a second one.
func TestTCPDest_reconnect(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	first := make(chan []byte, 1)
	second := make(chan []byte, 1)
	go func() {
		conn, aerr := ln.Accept()
		if aerr != nil {
			return
		}
		// Read the first payload as a line, then kill the conn: ReadAll would
		// block until the client closes, which is exactly the race under test.
		line, _ := bufio.NewReader(conn).ReadString('\n')
		first <- []byte(line)
		_ = conn.Close() // kills the first conn; the next write fails

		conn2, aerr2 := ln.Accept()
		if aerr2 != nil {
			second <- nil
			return
		}
		// Line-read too: the client only closes after Close(), which the
		// test reaches after reading this channel.
		line2, _ := bufio.NewReader(conn2).ReadString('\n')
		second <- []byte(line2)
	}()

	x := newTestXTCP(t, schemeTCP+":"+ln.Addr().String())
	dest, err := newTCPDest(context.Background(), x)
	if err != nil {
		t.Fatalf("newTCPDest: %v", err)
	}
	td := dest.(*tcpDest)
	td.writeTimeout = 2 * time.Second
	td.jitterDur = func(time.Duration) time.Duration { return 0 } // backoff now
	td.drainTimeout = 2 * time.Second

	send := func(p string) {
		t.Helper()
		b := []byte(p)
		n, serr := dest.Send(context.Background(), &b)
		if serr != nil || n != 1 {
			t.Fatalf("Send(%q) = (%d, %v)", p, n, serr)
		}
	}
	send("first\n")
	select {
	case rc := <-first:
		if string(rc) != "first\n" {
			t.Fatalf("first payload = %q", rc)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("timed out waiting for the first payload")
	}

	// The server killed the first conn, but a write to a FIN-closed socket
	// can still land in the kernel buffer (the RST is in flight), so keep
	// sending: one write must fail — that is the reconnect trigger — and the
	// payload after it must arrive on the fresh conn.
	deadline := time.After(8 * time.Second)
	for {
		send("second\n")
		select {
		case rc := <-second:
			if string(rc) != "second\n" {
				t.Fatalf("second payload = %q", rc)
			}
			if got := testutil.ToFloat64(x.pC.WithLabelValues("destTCP", "Write", "error")); got < 1 {
				t.Errorf("destTCP write-error counter = %v, want >= 1 (the failed write drove the reconnect)", got)
			}
			if got := testutil.ToFloat64(x.pC.WithLabelValues("destTCP", "Reconnect", "count")); got < 1 {
				t.Errorf("destTCP reconnect counter = %v, want >= 1", got)
			}
			if err := dest.Close(); err != nil {
				t.Errorf("Close: %v", err)
			}
			return
		case <-time.After(150 * time.Millisecond):
			// keep sending until the RST lands
		case <-deadline:
			t.Fatal("timed out waiting for the reconnected payload")
		}
	}
}

// TestTCPDest_writeTimeout drives #345's contract: a write that never
// completes (the sink stops reading) is bounded by the deadline, not the
// TCP retransmit window. The dial is faked onto a pipe whose far end
// nobody reads, so the write blocks until the deadline fires.
func TestTCPDest_writeTimeout(t *testing.T) {
	x := newTestXTCP(t, schemeTCP+":127.0.0.1:1")
	dest, err := newTCPDest(context.Background(), x)
	if err != nil {
		t.Fatalf("newTCPDest: %v", err)
	}
	td := dest.(*tcpDest)

	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() { _ = serverConn.Close() })
	td.dial = func(context.Context) (net.Conn, error) { return clientConn, nil }
	td.writeTimeout = 100 * time.Millisecond
	td.attempts = 2
	td.jitterDur = func(time.Duration) time.Duration { return 0 }

	started := time.Now()
	b := []byte("blocked-write\n")
	if _, err := dest.Send(context.Background(), &b); err != nil {
		t.Fatalf("Send: %v", err)
	}

	// The write must fail within ~attempts*timeout, not park for the
	// retransmit window. Poll the write-error counter.
	deadline := time.After(5 * time.Second)
	for {
		if got := testutil.ToFloat64(x.pC.WithLabelValues("destTCP", "Write", "error")); got >= 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("the blocked write never errored (elapsed %v)", time.Since(started))
		default:
			time.Sleep(20 * time.Millisecond)
		}
	}
	if got := testutil.ToFloat64(x.pC.WithLabelValues("destTCP", "dropped", "error")); got < 1 {
		t.Errorf("destTCP dropped counter = %v, want >= 1 (attempts exhausted)", got)
	}
	if err := dest.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// TestTCPDest_queueEviction drives the queueFull path: past the depth bound
// the oldest envelope is dropped and counted, and the newest is kept. The
// dial parks on a channel so the worker holds the head while the test fills
// the queue; the cleanup unblocks it and the product attempt budget drains
// the rest.
func TestTCPDest_queueEviction(t *testing.T) {
	x := newTestXTCP(t, schemeTCP+":127.0.0.1:1")
	dest, err := newTCPDest(context.Background(), x)
	if err != nil {
		t.Fatalf("newTCPDest: %v", err)
	}
	td := dest.(*tcpDest)
	td.jitterDur = func(time.Duration) time.Duration { return 0 }
	td.drainTimeout = 200 * time.Millisecond

	parked := make(chan struct{}, 1)
	release := make(chan struct{})
	var parkedOnce sync.Once
	td.dial = func(ctx context.Context) (net.Conn, error) {
		parkedOnce.Do(func() { parked <- struct{}{} })
		select {
		case <-release:
			return nil, errors.New("released")
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	// depth 16 by default; the worker pops the head as soon as it is pushed
	// (then parks in dial), so send well past the bound: eviction is
	// guaranteed regardless of when the pop lands.
	for i := range 40 {
		b := []byte(fmt.Sprintf("env-%d\n", i))
		if _, serr := dest.Send(context.Background(), &b); serr != nil {
			t.Fatalf("Send %d: %v", i, serr)
		}
	}
	<-parked
	if got := testutil.ToFloat64(x.pC.WithLabelValues("destTCP", "queueFull", "error")); got < 1 {
		t.Errorf("destTCP queueFull counter = %v, want >= 1", got)
	}

	// The worker is parked in dial; Close reports the drain timeout and
	// forces the unwind. Release the dial so the worker exits.
	if cerr := dest.Close(); cerr == nil || !strings.Contains(cerr.Error(), "drain timeout") {
		t.Errorf("Close with a parked dial = %v, want drain timeout", cerr)
	}
	close(release)
}

// TestTCPDest_sendAfterClose pins the refund path: a Send racing Close gets
// errTCPClosed, and its buffer returns to destBytesPool (the pool stays
// warm across a restart).
func TestTCPDest_sendAfterClose(t *testing.T) {
	x := newTestXTCP(t, schemeTCP+":127.0.0.1:1")
	dest, err := newTCPDest(context.Background(), x)
	if err != nil {
		t.Fatalf("newTCPDest: %v", err)
	}
	if err := dest.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	b := []byte("after-close\n")
	if _, err := dest.Send(context.Background(), &b); !errors.Is(err, errTCPClosed) {
		t.Fatalf("Send after Close = %v, want errTCPClosed", err)
	}
}
