package main

import (
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestBuildMessage(t *testing.T) {
	got := buildMessage(4001, 8)
	want := "client4001"
	if !strings.HasPrefix(string(got), want) {
		t.Errorf("buildMessage prefix = %q, want prefix %q", got, want)
	}
	if len(got) != len(want)+8 {
		t.Errorf("buildMessage len = %d, want %d", len(got), len(want)+8)
	}
}

func TestBuildMessage_zeroPad(t *testing.T) {
	got := buildMessage(0, 0)
	if string(got) != "client0" {
		t.Errorf("buildMessage(0, 0) = %q, want client0", got)
	}
}

func TestDialWithRetry_happy(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }() //nolint:errcheck // test plumbing
	port := ln.Addr().(*net.TCPAddr).Port
	go func() {
		c, _ := ln.Accept() //nolint:errcheck // test plumbing
		if c != nil {
			_ = c.Close() //nolint:errcheck // test plumbing
		}
	}()
	conn, err := dialWithRetry("127.0.0.1", port, 3, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	_ = conn.Close() //nolint:errcheck // test plumbing
}

func TestDialWithRetry_connRefused(t *testing.T) {
	// Bind + close → guaranteed conn-refused on that port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close() //nolint:errcheck // test plumbing

	_, err = dialWithRetry("127.0.0.1", port, 2, 100*time.Millisecond)
	if err == nil {
		t.Error("conn-refused should produce error")
	}
}

func TestClientOnce_happy(t *testing.T) {
	a, b := net.Pipe()
	defer func() { _ = a.Close() }() //nolint:errcheck // test plumbing
	defer func() { _ = b.Close() }() //nolint:errcheck // test plumbing

	// Echo server side.
	go func() {
		buf := make([]byte, 64)
		n, _ := b.Read(buf) //nolint:errcheck // test plumbing
		_, _ = b.Write(buf[:n]) //nolint:errcheck // test plumbing
	}()

	reply := make([]byte, 32)
	err := clientOnce(a, []byte("hello"), reply, time.Second, time.Second)
	if err != nil {
		t.Errorf("clientOnce: %v", err)
	}
}

func TestClientOnce_readTimeout(t *testing.T) {
	a, b := net.Pipe()
	defer func() { _ = a.Close() }() //nolint:errcheck // test plumbing
	defer func() { _ = b.Close() }() //nolint:errcheck // test plumbing

	// Drain the write so it doesn't block, but never reply.
	go func() {
		buf := make([]byte, 64)
		_, _ = b.Read(buf) //nolint:errcheck // test plumbing
	}()

	reply := make([]byte, 16)
	err := clientOnce(a, []byte("x"), reply, 500*time.Millisecond, 50*time.Millisecond)
	if !errors.Is(err, ErrTimeout) {
		t.Errorf("clientOnce err = %v, want ErrTimeout", err)
	}
}

func TestClientOnce_writeError(t *testing.T) {
	a, b := net.Pipe()
	_ = b.Close() //nolint:errcheck // close the far end before Write

	err := clientOnce(a, []byte("x"), make([]byte, 16), time.Second, time.Second)
	if err == nil {
		t.Error("write to closed pipe should error")
	}
	if errors.Is(err, ErrTimeout) {
		t.Error("write error should NOT be ErrTimeout")
	}
	_ = a.Close() //nolint:errcheck // test plumbing
}

func TestRunMain_zeroCount(t *testing.T) {
	if rc := runMain([]string{"-count", "0"}, &strings.Builder{}); rc != 0 {
		t.Errorf("rc = %d, want 0", rc)
	}
}

// client() with a guaranteed conn-refused: dialWithRetry returns an error,
// client logs it and returns immediately (no infinite loop).
func TestClient_dialFailure(t *testing.T) {
	// Bind + close → guaranteed conn-refused.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close() //nolint:errcheck // test plumbing

	var wg sync.WaitGroup
	wg.Add(1)
	done := make(chan struct{})
	go func() {
		client(&wg, "127.0.0.1", port, time.Hour, time.Second, time.Second, 2, 4)
		close(done)
	}()
	wg.Wait()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("client did not return on dial failure")
	}
}

// client() reaching the read-error branch: dial succeeds, then the server
// closes the connection mid-loop. clientOnce returns a non-Timeout error
// and client returns.
func TestClient_serverCloses(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }() //nolint:errcheck // test plumbing
	port := ln.Addr().(*net.TCPAddr).Port

	// Accept one connection, close it immediately.
	go func() {
		c, _ := ln.Accept() //nolint:errcheck // test plumbing
		if c != nil {
			_ = c.Close() //nolint:errcheck // test plumbing
		}
	}()

	var wg sync.WaitGroup
	wg.Add(1)
	done := make(chan struct{})
	go func() {
		client(&wg, "127.0.0.1", port, time.Hour, time.Second, time.Second, 5, 4)
		close(done)
	}()
	wg.Wait()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("client did not return after server close")
	}
}

func TestRunMain_invalidFlag(t *testing.T) {
	if rc := runMain([]string{"-not-a-flag"}, &strings.Builder{}); rc != 2 {
		t.Errorf("rc = %d, want 2", rc)
	}
}

func TestClientOnce_readError(t *testing.T) {
	a, b := net.Pipe()
	defer func() { _ = a.Close() }() //nolint:errcheck // test plumbing

	// Drain the write, then close the remote side so the Read returns EOF.
	go func() {
		buf := make([]byte, 64)
		_, _ = b.Read(buf) //nolint:errcheck // test plumbing
		_ = b.Close()      //nolint:errcheck // test plumbing
	}()

	err := clientOnce(a, []byte("x"), make([]byte, 16), time.Second, time.Second)
	if err == nil {
		t.Error("read EOF should error")
	}
	if !errors.Is(err, io.EOF) && !strings.Contains(err.Error(), "read:") {
		t.Errorf("clientOnce: %v", err)
	}
}
