package main

import (
	"context"
	"io"
	"net"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/tools/internal/procnet"
	"github.com/randomizedcoder/xtcp2/tools/internal/sockopt"
)

// parsed is the part of serverConfig the new flags set.
type parsed struct {
	opts      sockopt.Options
	holdEOF   bool
	keepalive time.Duration
	err       string
}

// go test ./tools/tcp_server/ -run TestParseServerFlags
func TestParseServerFlags(t *testing.T) {
	tests := []struct {
		name        string
		category    string
		description string
		input       []string
		expected    parsed
	}{
		{"defaults", "positive", "no new flags is today's behavior", nil, parsed{}},
		{"all_opts", "positive", "every flag parses",
			[]string{"-cong", "dctcp", "-tos", "0xb8", "-priority", "4", "-rcvbuf", "1MiB", "-freebind", "-transparent", "-holdEOF"},
			parsed{opts: sockopt.Options{Cong: "dctcp", TOS: 0xb8, Priority: 4, RcvBuf: 1 << 20, Freebind: true, Transparent: true}, holdEOF: true}},
		{"tos_range", "negative", "TOS is one byte", []string{"-tos", "-1"}, parsed{err: "tos -1 out of range 0-255"}},
		{"cong_too_long", "negative", "longer than TCP_CA_NAME_MAX", []string{"-cong", strings.Repeat("s", 16)}, parsed{err: "cong name longer than 15"}},
		{"client_flag", "negative", "a client-only flag is rejected", []string{"-bulk", "1MiB"}, parsed{err: "flag provided but not defined: -bulk"}},
		{"tos_max", "boundary", "the largest TOS", []string{"-tos", "255"}, parsed{opts: sockopt.Options{TOS: 255}}},
		{"priority_zero", "boundary", "0 means not set", []string{"-priority", "0"}, parsed{}},
		{"keepalive_off", "positive", "a negative -keepalive turns keepalive off on accepted connections", []string{"-keepalive", "-1s"}, parsed{keepalive: -time.Second}},
		{"keepalive_bad", "negative", "a -keepalive that is not a duration", []string{"-keepalive", "1x"}, parsed{err: `invalid value "1x" for flag -keepalive`}},
		{"keepalive_zero", "boundary", "0 keeps Go's default keepalive", []string{"-keepalive", "0s"}, parsed{}},
		{"hold_alone", "corner", "-holdEOF without options", []string{"-holdEOF"}, parsed{holdEOF: true}},
		{"bad_rcvbuf_suffix", "corner", "a suffix outside K/M/G", []string{"-rcvbuf", "1TiB"}, parsed{err: `size "1TiB"`}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, err := parseFlags(tc.input, io.Discard)
			got := parsed{opts: c.opts, holdEOF: c.holdEOF, keepalive: c.keepalive}
			ok := errMatches(err, tc.expected.err)
			if err == nil {
				want := tc.expected
				want.err = ""
				ok = ok && got == want
			}
			if !ok {
				t.Errorf("%s [%s] %s: args %q: got %+v, %v; expected %+v",
					tc.name, tc.category, tc.description, tc.input, got, err, tc.expected)
			}
		})
	}
}

// holdInput is how the client ends the connection and whether the server
// holds after EOF; cancel cancels the server's context once the client has
// ended.
type holdInput struct {
	holdEOF   bool
	send      string
	halfClose bool // shutdown(SHUT_WR); false closes fully
	cancel    bool
}

// holdExpect is whether the server's socket is in CLOSE_WAIT once settled,
// whether handleConn returned, and what the client read back.
type holdExpect struct {
	closeWait bool
	returned  bool
	echoed    string
}

// go test ./tools/tcp_server/ -run TestHoldEOF
func TestHoldEOF(t *testing.T) {
	tests := []struct {
		name        string
		category    string
		description string
		input       holdInput
		expected    holdExpect
	}{
		{"hold", "positive", "the server keeps the socket after EOF",
			holdInput{holdEOF: true, halfClose: true}, holdExpect{closeWait: true}},
		{"echo_then_hold", "positive", "data is echoed before the hold",
			holdInput{holdEOF: true, send: "hello", halfClose: true}, holdExpect{closeWait: true, echoed: "hello"}},
		{"no_hold", "negative", "without -holdEOF the server closes on EOF",
			holdInput{halfClose: true}, holdExpect{returned: true}},
		{"cancel", "boundary", "a held socket closes when the server stops",
			holdInput{holdEOF: true, halfClose: true, cancel: true}, holdExpect{returned: true}},
		{"full_close", "corner", "a full close from the client is held too",
			holdInput{holdEOF: true}, holdExpect{closeWait: true}},
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
			done := make(chan struct{})
			go func() {
				defer close(done)
				c, aerr := ln.Accept()
				if aerr != nil {
					return
				}
				handleConn(ctx, c, tc.input.holdEOF)
			}()
			conn, err := net.Dial("tcp4", ln.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = conn.Close() }()
			if _, err := conn.Write([]byte(tc.input.send)); err != nil {
				t.Fatal(err)
			}
			var echoed []byte
			if tc.input.halfClose {
				if err := conn.(*net.TCPConn).CloseWrite(); err != nil {
					t.Fatal(err)
				}
				if err := conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond)); err != nil {
					t.Fatal(err)
				}
				echoed, _ = io.ReadAll(conn)
			} else {
				_ = conn.Close()
			}
			if tc.input.cancel {
				cancel()
			}
			serverPort := ln.Addr().(*net.TCPAddr).AddrPort().Port()
			closeWait := false
			var states []procnet.State
			for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
				states, err = procnet.Lookup(serverPort, 0)
				if err != nil {
					t.Fatal(err)
				}
				closeWait = slices.Contains(states, procnet.CloseWait)
				if closeWait == tc.expected.closeWait {
					break
				}
			}
			returned := false
			select {
			case <-done:
				returned = true
			case <-time.After(300 * time.Millisecond):
			}
			if closeWait != tc.expected.closeWait || returned != tc.expected.returned || string(echoed) != tc.expected.echoed {
				t.Errorf("%s [%s] %s: %+v: server port states %v, handleConn returned %v, echoed %q; expected CLOSE_WAIT %v, returned %v, echoed %q",
					tc.name, tc.category, tc.description, tc.input, states, returned, echoed,
					tc.expected.closeWait, tc.expected.returned, tc.expected.echoed)
			}
		})
	}
}

// errMatches reports whether err is nil when want is "", or contains want.
func errMatches(err error, want string) bool {
	if want == "" {
		return err == nil
	}
	return err != nil && strings.Contains(err.Error(), want)
}

// go test ./tools/tcp_server/ -run TestListenKeepAlive
func TestListenKeepAlive(t *testing.T) {
	tests := []struct {
		name, category, description string
		input                       time.Duration
		expectedOn                  int
	}{
		{"default", "positive", "0 keeps Go's default: accepted connections have keepalive on", 0, 1},
		{"off", "positive", "a negative value turns keepalive off on accepted connections", -time.Second, 0},
		{"set", "boundary", "a positive value keeps it on", 30 * time.Second, 1},
		{"off_large", "corner", "any negative value is off", -time.Hour, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lc := listenConfig(serverConfig{keepalive: tc.input})
			ln, err := lc.Listen(context.Background(), "tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = ln.Close() }()
			c, err := net.Dial("tcp4", ln.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = c.Close() }()
			a, err := ln.Accept()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = a.Close() }()
			raw, err := a.(*net.TCPConn).SyscallConn()
			if err != nil {
				t.Fatal(err)
			}
			var on int
			var gerr error
			if err := raw.Control(func(fd uintptr) {
				on, gerr = unix.GetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_KEEPALIVE)
			}); err != nil || gerr != nil {
				t.Fatal(err, gerr)
			}
			if on != tc.expectedOn {
				t.Errorf("%s [%s] %s: SO_KEEPALIVE %d, expected %d", tc.name, tc.category, tc.description, on, tc.expectedOn)
			}
		})
	}
}
