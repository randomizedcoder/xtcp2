package xtcp

import (
	"bytes"
	"context"
	"io"
	"net"
	"testing"
	"time"
)

// TestTCPDest_send drives the tcp destination against a real in-process TCP
// listener and asserts the marshalled payload arrives verbatim (framing is the
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
