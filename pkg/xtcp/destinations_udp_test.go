package xtcp

import (
	"bytes"
	"context"
	"net"
	"testing"
	"time"
)

// TestUDPDest_send drives the udp destination (syscall path — config.IoUring is
// false in the test XTCP) against a real UDP socket and asserts the datagram
// arrives verbatim (one Write == one datagram == one record).
func TestUDPDest_send(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.ListenPacket: %v", err)
	}
	t.Cleanup(func() { _ = pc.Close() })

	x := newTestXTCP(t, "udp:"+pc.LocalAddr().String())
	d, err := newUDPDest(context.Background(), x)
	if err != nil {
		t.Fatalf("newUDPDest: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	payload := []byte("hello-udp")
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
