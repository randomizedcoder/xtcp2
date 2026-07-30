package xtcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"path/filepath"
	"testing"
	"time"
)

// TestUnixDest_send drives the unix (stream) destination against a real
// AF_UNIX/SOCK_STREAM listener. Unlike tcp, the unix stream destination
// varint-length-frames each record (net.Buffers{varint(len), payload}) so a
// stream receiver can split records, so the test decodes that framing.
func TestUnixDest_send(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "s.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("net.Listen(unix): %v", err)
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
		r := bufio.NewReader(conn)
		length, rerr := binary.ReadUvarint(r)
		if rerr != nil {
			return
		}
		payload := make([]byte, length)
		if _, rerr := io.ReadFull(r, payload); rerr != nil {
			return
		}
		got <- payload
	}()

	x := newTestXTCP(t, "unix:"+sock)
	d, err := newUnixDest(context.Background(), x)
	if err != nil {
		t.Fatalf("newUnixDest: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	payload := []byte("hello-unix-stream")
	b := append([]byte(nil), payload...)
	n, err := d.Send(context.Background(), &b)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if n != 1 {
		t.Errorf("Send n = %d, want 1 (one record)", n)
	}

	select {
	case rc := <-got:
		if !bytes.Equal(rc, payload) {
			t.Errorf("received %q, want %q", rc, payload)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("timed out waiting for the unix payload")
	}
}
