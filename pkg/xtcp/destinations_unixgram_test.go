package xtcp

import (
	"bytes"
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"
)

// TestUnixGramDest_send drives the unixgram destination against a real
// AF_UNIX/SOCK_DGRAM socket. One Write == one datagram == one record, written
// verbatim (no framing needed for datagrams).
func TestUnixGramDest_send(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "dg.sock")
	pc, err := net.ListenPacket("unixgram", sock)
	if err != nil {
		t.Fatalf("net.ListenPacket(unixgram): %v", err)
	}
	t.Cleanup(func() { _ = pc.Close() })

	x := newTestXTCP(t, "unixgram:"+sock)
	d, err := newUnixGramDest(context.Background(), x)
	if err != nil {
		t.Fatalf("newUnixGramDest: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	payload := []byte("hello-dgram")
	b := append([]byte(nil), payload...)
	n, err := d.Send(context.Background(), &b)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if n != 1 {
		t.Errorf("Send n = %d, want 1 (one record)", n)
	}

	_ = pc.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 1500)
	got, _, err := pc.ReadFrom(buf)
	if err != nil {
		t.Fatalf("ReadFrom: %v", err)
	}
	if !bytes.Equal(buf[:got], payload) {
		t.Errorf("received %q, want %q", buf[:got], payload)
	}
}
