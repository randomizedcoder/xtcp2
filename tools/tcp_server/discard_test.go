package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"testing"
	"time"
)

// discardInput is the server's mode and what the client sends before it
// half-closes.
type discardInput struct {
	discard bool
	holdEOF bool
	send    int
}

// discardExpect is what handleConn read, what the client got back, and
// whether handleConn returned on the client's EOF (a held one returns only
// once the server stops).
type discardExpect struct {
	read     int64
	echoed   int
	returned bool
}

// go test ./tools/tcp_server/ -run TestDiscard
func TestDiscard(t *testing.T) {
	tests := []struct {
		name        string
		category    string
		description string
		input       discardInput
		expected    discardExpect
	}{
		{"discard_reads_all", "positive", "-discard reads the whole stream and writes nothing",
			discardInput{discard: true, send: 1 << 20}, discardExpect{read: 1 << 20, returned: true}},
		{"echo_default", "negative", "without -discard the server still echoes",
			discardInput{send: 4 << 10}, discardExpect{read: 4 << 10, echoed: 4 << 10, returned: true}},
		{"discard_empty", "boundary", "a connection that sends nothing and closes",
			discardInput{discard: true}, discardExpect{returned: true}},
		{"discard_holdEOF", "corner", "-discard with -holdEOF still holds the socket after EOF",
			discardInput{discard: true, holdEOF: true, send: 1 << 10}, discardExpect{read: 1 << 10}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
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
				readc <- handleConn(ctx, c, tc.input.holdEOF, tc.input.discard)
			}()
			conn, err := net.Dial("tcp4", ln.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = conn.Close() }()
			// Read concurrently, so an echo can't fill both buffers.
			echoedc := make(chan int, 1)
			go func() {
				_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
				n, _ := io.Copy(io.Discard, conn)
				echoedc <- int(n)
			}()
			if _, err := conn.Write(bytes.Repeat([]byte{'x'}, tc.input.send)); err != nil {
				t.Fatal(err)
			}
			if err := conn.(*net.TCPConn).CloseWrite(); err != nil {
				t.Fatal(err)
			}
			var read int64 = -1
			returned := false
			select {
			case read = <-readc:
				returned = true
			case <-time.After(500 * time.Millisecond):
				cancel()
				read = <-readc
			}
			echoed := <-echoedc
			if read != tc.expected.read || echoed != tc.expected.echoed || returned != tc.expected.returned {
				t.Errorf("%s [%s] %s: %+v: read %d, echoed %d, returned on EOF %v; expected %+v",
					tc.name, tc.category, tc.description, tc.input, read, echoed, returned, tc.expected)
			}
		})
	}
}
