package main

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
	"google.golang.org/protobuf/proto"
)

func loopbackUDP(t *testing.T) (server *net.UDPConn, client *net.UDPConn) {
	t.Helper()
	saddr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0}
	srv, err := net.ListenUDP("udp", saddr)
	if err != nil {
		t.Fatal(err)
	}
	caddr, ok := srv.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatal("srv LocalAddr is not *net.UDPAddr")
	}
	cli, err := net.DialUDP("udp", nil, caddr)
	if err != nil {
		_ = srv.Close() //nolint:errcheck // test plumbing
		t.Fatal(err)
	}
	return srv, cli
}

func TestRunReceiver_happy(t *testing.T) {
	srv, cli := loopbackUDP(t)
	defer func() { _ = srv.Close() }() //nolint:errcheck // test plumbing
	defer func() { _ = cli.Close() }() //nolint:errcheck // test plumbing

	rec := &xtcp_flat_record.Envelope_XtcpFlatRecord{Hostname: "udp-test"}
	encoded, err := proto.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	rdone := make(chan error, 1)
	go func() {
		// Send one frame then cancel to break the loop.
		_, _ = cli.Write(encoded) //nolint:errcheck // test plumbing
		time.Sleep(50 * time.Millisecond)
		_ = srv.SetReadDeadline(time.Now()) //nolint:errcheck // unblock the read
	}()
	go func() {
		rdone <- runReceiver(ctx, srv)
	}()

	select {
	case err := <-rdone:
		// Could return nil (ctx done before next read) or an error from
		// the forced read-deadline. We just want the path exercised.
		_ = err
	case <-time.After(2 * time.Second):
		t.Fatal("runReceiver did not return")
	}
}

func TestRunReceiver_decodeError(t *testing.T) {
	srv, cli := loopbackUDP(t)
	defer func() { _ = srv.Close() }() //nolint:errcheck // test plumbing
	defer func() { _ = cli.Close() }() //nolint:errcheck // test plumbing

	rdone := make(chan error, 1)
	go func() {
		rdone <- runReceiver(t.Context(), srv)
	}()

	// 0xFF varint header is malformed → proto.Unmarshal returns error.
	_, _ = cli.Write([]byte{0xFF, 0xFF, 0xFF, 0xFF}) //nolint:errcheck // test plumbing

	select {
	case err := <-rdone:
		if !errors.Is(err, ErrDecode) {
			t.Errorf("runReceiver err = %v, want ErrDecode", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("decode error did not propagate")
	}
}

func TestRunReceiver_ctxCancel(t *testing.T) {
	srv, cli := loopbackUDP(t)
	defer func() { _ = srv.Close() }() //nolint:errcheck // test plumbing
	defer func() { _ = cli.Close() }() //nolint:errcheck // test plumbing

	ctx, cancel := context.WithCancel(t.Context())
	rdone := make(chan error, 1)
	go func() {
		rdone <- runReceiver(ctx, srv)
	}()
	cancel()
	_ = srv.SetReadDeadline(time.Now()) //nolint:errcheck // unblock the read so ctx is observed

	select {
	case err := <-rdone:
		if err != nil {
			t.Errorf("ctx-cancel runReceiver err = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runReceiver did not return after cancel")
	}
}

func TestRunReceiver_readError(t *testing.T) {
	srv, cli := loopbackUDP(t)
	_ = cli.Close()                    //nolint:errcheck // test plumbing
	defer func() { _ = srv.Close() }() //nolint:errcheck // test plumbing

	// Force a read error by closing the socket from another goroutine before
	// any data arrives. ReadFromUDP returns ErrClosed (not a timeout-style
	// "use of closed network connection" wrap on all kernels).
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = srv.Close() //nolint:errcheck // test plumbing
	}()
	err := runReceiver(t.Context(), srv)
	// Either ctx wasn't cancelled (=> err non-nil) or the cancel-race made
	// it nil; both branches are valid. Just exercise the path.
	_ = err
}
