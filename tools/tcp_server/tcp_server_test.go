package main

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

func TestRunServer_echoAndShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Bind on :0, find the actual port via a probe listener.
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	_ = probe.Close() //nolint:errcheck // test plumbing

	srvDone := make(chan error, 1)
	go func() {
		srvDone <- runServer(ctx, "127.0.0.1", port)
	}()

	// Retry-dial until the listener is ready (race vs goroutine start).
	addr := "127.0.0.1:" + itoa(port)
	var conn net.Conn
	for range 50 {
		conn, err = net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("dial %s: %v", addr, err)
	}
	defer func() { _ = conn.Close() }() //nolint:errcheck // test plumbing

	// Echo round-trip.
	if _, werr := conn.Write([]byte("hello")); werr != nil {
		t.Fatalf("write: %v", werr)
	}
	buf := make([]byte, 5)
	if rerr := conn.SetReadDeadline(time.Now().Add(time.Second)); rerr != nil {
		t.Fatal(rerr)
	}
	if _, rerr := conn.Read(buf); rerr != nil {
		t.Fatalf("read: %v", rerr)
	}
	if string(buf) != "hello" {
		t.Errorf("got %q, want hello", buf)
	}
	_ = conn.Close() //nolint:errcheck // test plumbing

	cancel()
	select {
	case err = <-srvDone:
		if err != nil {
			t.Errorf("runServer returned err: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runServer did not shut down after cancel")
	}
}

func TestRunServer_bindError(t *testing.T) {
	if err := runServer(t.Context(), "bad-host-:-:bind", 4000); err == nil {
		t.Error("malformed bind should error")
	}
}

func TestRunMain_zeroCount(t *testing.T) {
	// count=0 → no goroutines spawned, wg.Wait returns immediately.
	var stderr strings.Builder
	if rc := runMain(t.Context(), []string{"-count", "0", "-bind", "127.0.0.1"}, &stderr); rc != 0 {
		t.Errorf("rc = %d, want 0", rc)
	}
}

func TestRunMain_invalidFlag(t *testing.T) {
	var stderr strings.Builder
	if rc := runMain(t.Context(), []string{"-not-a-flag"}, &stderr); rc != 2 {
		t.Errorf("rc = %d, want 2", rc)
	}
}

func TestRunMain_cancellable(t *testing.T) {
	// count=1 but bind to a guaranteed-conflicting addr (port 0 isn't valid
	// for our startPort+i math, but the listener will succeed; cancel to break).
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan int, 1)
	go func() {
		done <- runMain(ctx, []string{"-count", "1", "-bind", "127.0.0.1"}, &strings.Builder{})
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case rc := <-done:
		if rc != 0 {
			t.Errorf("rc = %d, want 0", rc)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runMain did not exit on cancel")
	}
}

// runMain spawns runServer in a goroutine; when runServer fails the
// goroutine's log.Printf branch fires. Bind to a malformed address so
// every spawned goroutine returns an err immediately, then the wg.Wait
// exit lets runMain return 0.
func TestRunMain_runServerLogsErr(t *testing.T) {
	var stderr strings.Builder
	rc := runMain(t.Context(), []string{
		"-count", "1",
		"-bind", "bad-host-:-:bind",
	}, &stderr)
	if rc != 0 {
		t.Errorf("rc = %d, want 0 (runMain doesn't surface goroutine err)", rc)
	}
}

func TestHandleConn_eof(t *testing.T) {
	// In-process Pipe: handleConn echoes whatever it reads. Closing the
	// remote end causes io.Copy to return EOF; handleConn returns.
	a, b := net.Pipe()
	done := make(chan struct{})
	go func() {
		handleConn(a)
		close(done)
	}()
	if _, err := b.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4)
	if err := b.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Read(buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf) != "ping" {
		t.Errorf("echo: got %q, want ping", buf)
	}
	_ = b.Close() //nolint:errcheck // test plumbing
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handleConn did not return after remote close")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
