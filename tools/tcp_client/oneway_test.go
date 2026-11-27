package main

import (
	"errors"
	"io"
	"net"
	"os"
	"testing"
	"time"
)

// onewayPeer is how the peer of a one-way transfer behaves.
type onewayPeer int

const (
	discardIdle onewayPeer = iota // read until 200 ms pass with nothing, then close
	discardN                      // read exactly n bytes, then close
	resetEarly                    // read 1 byte, then close with RST
)

// go test ./tools/tcp_client/ -run TestBulkOneWay
func TestBulkOneWay(t *testing.T) {
	type input struct {
		peer onewayPeer
		n    int64
	}
	type expect struct {
		peerRead int64 // -1: not checked
		err      string
	}
	tests := []struct {
		name        string
		category    string
		description string
		input       input
		expected    expect
	}{
		{"oneway_writes_n", "positive", "writes exactly n bytes, then nothing", input{discardIdle, 1 << 20}, expect{peerRead: 1 << 20}},
		{"oneway_peer_close", "positive", "returns nil once the peer closes", input{discardN, 4 << 10}, expect{peerRead: 4 << 10}},
		{"oneway_one_byte", "boundary", "the smallest transfer", input{discardIdle, 1}, expect{peerRead: 1}},
		{"oneway_zero", "boundary", "0 bytes is no transfer, so no wait", input{discardIdle, 0}, expect{peerRead: -1}},
		{"oneway_peer_reset", "corner", "a reset mid-transfer is an error, not a clean end", input{resetEarly, 64 << 20}, expect{peerRead: -1, err: "bulk one-way"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ln, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = ln.Close() }()
			readc := make(chan int64, 1)
			go func() {
				c, aerr := ln.Accept()
				if aerr != nil {
					readc <- -1
					return
				}
				readc <- onewayServe(c, tc.input.peer, tc.input.n)
			}()
			conn, err := net.Dial("tcp4", ln.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = conn.Close() }()
			errc := make(chan error, 1)
			go func() { errc <- bulkOneWay(conn, tc.input.n) }()
			var got error
			select {
			case got = <-errc:
			case <-time.After(5 * time.Second):
				t.Fatalf("%s: bulkOneWay did not return", tc.name)
			}
			peerRead := int64(-1)
			if tc.expected.peerRead >= 0 {
				peerRead = <-readc
			}
			if !errMatches(got, tc.expected.err) || peerRead != tc.expected.peerRead {
				t.Errorf("%s [%s] %s: %+v: peer read %d, %v; expected %d, error containing %q",
					tc.name, tc.category, tc.description, tc.input, peerRead, got, tc.expected.peerRead, tc.expected.err)
			}
		})
	}
}

// onewayServe plays peer p on c and returns the bytes it read.
func onewayServe(c net.Conn, p onewayPeer, n int64) int64 {
	defer func() { _ = c.Close() }()
	switch p {
	case discardN:
		got, _ := io.CopyN(io.Discard, c, n)
		return got
	case resetEarly:
		got, _ := io.CopyN(io.Discard, c, 1)
		_ = c.(*net.TCPConn).SetLinger(0)
		return got
	case discardIdle:
	}
	var got int64
	buf := make([]byte, 64<<10)
	for {
		_ = c.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		k, err := c.Read(buf)
		got += int64(k)
		if err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) || errors.Is(err, io.EOF) {
				return got
			}
			return -1
		}
	}
}
