package main

import (
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/tools/internal/procnet"
	"github.com/randomizedcoder/xtcp2/tools/internal/sockopt"
)

// parsed is the part of clientConfig the new flags set.
type parsed struct {
	opts        sockopt.Options
	bulk        sockopt.Size
	mode        closeMode
	dialTimeout time.Duration
	err         string
}

// go test ./tools/tcp_client/ -run TestParseClientFlags
func TestParseClientFlags(t *testing.T) {
	tests := []struct {
		name        string
		category    string
		description string
		input       []string
		expected    parsed
	}{
		{"defaults", "positive", "no new flags is today's behavior", nil, parsed{dialTimeout: dialTimeoutCst}},
		{"all_opts", "positive", "every flag parses",
			[]string{"-cong", "bbr", "-tos", "0x08", "-priority", "6", "-rcvbuf", "64KiB", "-freebind", "-bindNoPort", "-bulk", "4MiB", "-close", "-dialTimeout", "3m"},
			parsed{opts: sockopt.Options{Cong: "bbr", TOS: 8, Priority: 6, RcvBuf: 65536, Freebind: true, BindNoPort: true}, bulk: 4 << 20, mode: closeAfter, dialTimeout: 3 * time.Minute}},
		{"tos_range", "negative", "TOS is one byte", []string{"-tos", "256"}, parsed{err: "tos 256 out of range 0-255"}},
		{"cong_too_long", "negative", "longer than TCP_CA_NAME_MAX", []string{"-cong", strings.Repeat("c", 16)}, parsed{err: "cong name longer than 15"}},
		{"conflicting", "negative", "contradictory close modes", []string{"-close", "-halfclose"}, parsed{err: "-close and -halfclose are exclusive"}},
		{"bad_bulk", "negative", "a size that isn't one", []string{"-bulk", "lots"}, parsed{err: `size "lots"`}},
		{"dial_timeout_zero", "negative", "a dial needs time", []string{"-dialTimeout", "0s"}, parsed{err: "-dialTimeout 0s must be positive"}},
		{"tos_zero", "boundary", "0 means not set", []string{"-tos", "0"}, parsed{dialTimeout: dialTimeoutCst}},
		{"rcvbuf_min", "boundary", "the smallest buffer; the kernel raises it", []string{"-rcvbuf", "1"}, parsed{opts: sockopt.Options{RcvBuf: 1}, dialTimeout: dialTimeoutCst}},
		{"bulk_zero", "boundary", "0 is one message, as today", []string{"-bulk", "0"}, parsed{dialTimeout: dialTimeoutCst}},
		{"halfclose", "corner", "half-close alone", []string{"-halfclose"}, parsed{mode: halfClose, dialTimeout: dialTimeoutCst}},
		{"dial_timeout_long", "boundary", "longer than the kernel's SYN retries, for SYN_SENT sockets", []string{"-dialTimeout", "10m"}, parsed{dialTimeout: 10 * time.Minute}},
		{"last_wins", "corner", "a repeated flag takes the last value, so appended args override", []string{"-tos", "8", "-tos", "0xb8"}, parsed{opts: sockopt.Options{TOS: 0xb8}, dialTimeout: dialTimeoutCst}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, err := parseFlags(tc.input, io.Discard)
			got := parsed{opts: c.opts, bulk: c.bulk, mode: c.mode, dialTimeout: c.dialTimeout}
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

// bulkServer is how the peer of a bulk transfer behaves.
type bulkServer int

const (
	echoAll   bulkServer = iota // echo until EOF
	readClose                   // read 1000 bytes, then close
)

// go test ./tools/tcp_client/ -run TestBulk
func TestBulk(t *testing.T) {
	type input struct {
		server bulkServer
		n      int64
	}
	type expect struct {
		echoed int64
		err    string
	}
	tests := []struct {
		name        string
		category    string
		description string
		input       input
		expected    expect
	}{
		{"bulk_echo", "positive", "N bytes out and echoed back", input{echoAll, 1 << 20}, expect{1 << 20, ""}},
		{"bulk_short_read", "negative", "the server closes early", input{readClose, 1 << 20}, expect{0, "bulk: read"}},
		{"bulk_zero", "boundary", "0 sends nothing", input{echoAll, 0}, expect{0, ""}},
		{"bulk_one", "boundary", "a single byte", input{echoAll, 1}, expect{1, ""}},
		{"bulk_not_multiple", "corner", "a size that isn't a multiple of the buffer", input{echoAll, 100001}, expect{100001, ""}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			conn := dialPeer(t, func(c net.Conn) {
				if tc.input.server == readClose {
					_, _ = io.CopyN(io.Discard, c, 1000)
					return
				}
				_, _ = io.Copy(c, c)
			})
			got, err := bulk(conn, tc.input.n)
			if !errMatches(err, tc.expected.err) || (err == nil && got != tc.expected.echoed) {
				t.Errorf("%s [%s] %s: bulk(%d): echoed %d, %v; expected %d, error containing %q",
					tc.name, tc.category, tc.description, tc.input.n, got, err, tc.expected.echoed, tc.expected.err)
			}
		})
	}
}

// closeInput is one session: how the peer behaves and the client's -bulk
// and close mode.
type closeInput struct {
	server sessionServer
	bulk   sockopt.Size
	mode   closeMode
}

// sessionServer is how the peer of a session behaves.
type sessionServer int

const (
	echoClose  sessionServer = iota // echo until EOF, then close
	echoHold                        // echo until EOF, then keep the socket
	closeFirst                      // close without reading
)

// closeExpect is the client socket's state once the session settles, and
// whether session returned (with err).
type closeExpect struct {
	state    procnet.State
	returned bool
	err      string
}

// go test ./tools/tcp_client/ -run TestCloseModes
func TestCloseModes(t *testing.T) {
	tests := []struct {
		name        string
		category    string
		description string
		input       closeInput
		expected    closeExpect
	}{
		{"close_timewait", "positive", "the closer goes to TIME_WAIT",
			closeInput{echoClose, 0, closeAfter}, closeExpect{procnet.TimeWait, true, ""}},
		{"halfclose_finwait2", "positive", "half-close to a peer that holds stays in FIN_WAIT2",
			closeInput{echoHold, 0, halfClose}, closeExpect{procnet.FinWait2, false, ""}},
		{"keep_open", "negative", "no close mode stays ESTABLISHED",
			closeInput{echoClose, 0, keepOpen}, closeExpect{procnet.Established, false, ""}},
		{"peer_closes_first", "negative", "the exchange fails when the peer closes unread",
			closeInput{closeFirst, 0, closeAfter}, closeExpect{0, true, "read"}},
		{"bulk_then_close", "boundary", "bulk runs before the close",
			closeInput{echoClose, 64 << 10, closeAfter}, closeExpect{procnet.TimeWait, true, ""}},
		{"halfclose_peer_closes", "corner", "half-close to a peer that closes ends in TIME_WAIT",
			closeInput{echoClose, 0, halfClose}, closeExpect{procnet.TimeWait, true, ""}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			conn := dialPeer(t, func(c net.Conn) {
				switch tc.input.server {
				case closeFirst:
					return
				case echoHold:
					_, _ = io.Copy(c, c)
					<-t.Context().Done()
				default:
					_, _ = io.Copy(c, c)
				}
			})
			port := conn.LocalAddr().(*net.TCPAddr).AddrPort().Port()
			cfg := clientConfig{pads: 4, sleep: 20 * time.Millisecond, wto: time.Second, rto: time.Second,
				bulk: tc.input.bulk, mode: tc.input.mode}
			done := make(chan error, 1)
			go func() { done <- session(conn, cfg, int(port)) }()

			var err error
			returned := false
			if tc.expected.returned {
				select {
				case err = <-done:
					returned = true
				case <-time.After(5 * time.Second):
				}
			}
			var states []procnet.State
			if tc.expected.state != 0 {
				states = waitState(t, port, tc.expected.state)
			}
			if !tc.expected.returned {
				select {
				case err = <-done:
					returned = true
				default:
				}
			}
			ok := returned == tc.expected.returned && errMatches(err, tc.expected.err)
			if tc.expected.state != 0 {
				ok = ok && len(states) == 1 && states[0] == tc.expected.state
			}
			if !ok {
				t.Errorf("%s [%s] %s: %+v: client port %d states %v, returned %v (%v); expected %v, returned %v, error containing %q",
					tc.name, tc.category, tc.description, tc.input, port, states, returned, err,
					tc.expected.state, tc.expected.returned, tc.expected.err)
			}
		})
	}
}

// dialPeer listens on loopback, serves one connection with serve (closing
// it afterwards) and returns the client's end. Both close with the test.
func dialPeer(t *testing.T, serve func(net.Conn)) net.Conn {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		c, aerr := ln.Accept()
		if aerr != nil {
			return
		}
		defer func() { _ = c.Close() }()
		serve(c)
	}()
	conn, err := net.Dial("tcp4", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// waitState polls the sockets on a local port until one is in want, or 5s
// pass, and returns the last states seen.
func waitState(t *testing.T, port uint16, want procnet.State) []procnet.State {
	t.Helper()
	var states []procnet.State
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		var err error
		states, err = procnet.Lookup(port)
		if err != nil {
			t.Fatal(err)
		}
		if len(states) == 1 && states[0] == want {
			break
		}
	}
	return states
}

// errMatches reports whether err is nil when want is "", or contains want.
func errMatches(err error, want string) bool {
	if want == "" {
		return err == nil
	}
	return err != nil && strings.Contains(err.Error(), want)
}
