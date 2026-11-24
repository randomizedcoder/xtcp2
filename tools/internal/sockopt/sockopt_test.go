package sockopt

import (
	"errors"
	"math"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

// go test ./tools/internal/sockopt/ -run TestParseSize
func TestParseSize(t *testing.T) {
	type expect struct {
		size Size
		err  string
	}
	tests := []struct {
		name        string
		category    string
		description string
		input       string
		expected    expect
	}{
		{"bytes", "positive", "a plain byte count", "65536", expect{65536, ""}},
		{"mib", "positive", "a binary suffix", "1MiB", expect{1 << 20, ""}},
		{"gib", "positive", "the largest suffix", "1GiB", expect{1 << 30, ""}},
		{"not_a_number", "negative", "letters only", "abc", expect{0, `size "abc"`}},
		{"negative", "negative", "a size can't be negative", "-1", expect{0, `size "-1"`}},
		{"unknown_suffix", "negative", "a suffix outside K/M/G", "1TiB", expect{0, `size "1TiB"`}},
		{"zero", "boundary", "0 means not set", "0", expect{0, ""}},
		{"max_int64", "boundary", "the largest byte count", "9223372036854775807", expect{math.MaxInt64, ""}},
		{"overflow", "boundary", "a suffix that overflows int64", "8589934592GiB", expect{0, "overflows"}},
		{"short_suffix", "corner", "K is binary, like KiB", "4K", expect{4096, ""}},
		{"empty", "corner", "an empty value", "", expect{0, `size ""`}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseSize(tc.input)
			if !errMatches(err, tc.expected.err) || got != tc.expected.size {
				t.Errorf("%s [%s] %s: ParseSize(%q) = %d, %v; expected %d, error containing %q",
					tc.name, tc.category, tc.description, tc.input, got, err, tc.expected.size, tc.expected.err)
			}
		})
	}
}

// go test ./tools/internal/sockopt/ -run TestValidate
func TestValidate(t *testing.T) {
	tests := []struct {
		name        string
		category    string
		description string
		input       Options
		expected    string // error substring; "" is no error
	}{
		{"zero", "positive", "no options is valid", Options{}, ""},
		{"all", "positive", "every option in range",
			Options{Cong: "bbr", TOS: 0x08, Priority: 6, RcvBuf: 65536, Freebind: true, Transparent: true, BindNoPort: true}, ""},
		{"tos_range", "negative", "TOS is one byte", Options{TOS: 256}, "tos 256 out of range 0-255"},
		{"cong_too_long", "negative", "longer than TCP_CA_NAME_MAX", Options{Cong: strings.Repeat("a", 16)}, "cong name longer than 15"},
		{"priority_negative", "negative", "a negative priority", Options{Priority: -1}, "priority -1 is negative"},
		{"tos_max", "boundary", "the largest TOS", Options{TOS: 255}, ""},
		{"cong_max", "boundary", "a 15-character name", Options{Cong: strings.Repeat("a", 15)}, ""},
		{"rcvbuf_max", "boundary", "the largest int setsockopt takes", Options{RcvBuf: math.MaxInt32}, ""},
		{"rcvbuf_over", "corner", "one past an int", Options{RcvBuf: math.MaxInt32 + 1}, "rcvbuf 2147483648 exceeds 2147483647"},
		{"two_errors", "corner", "every problem is reported", Options{TOS: -1, Priority: -2}, "priority -2 is negative"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.input.Validate()
			if !errMatches(err, tc.expected) {
				t.Errorf("%s [%s] %s: Validate(%+v) = %v, expected error containing %q",
					tc.name, tc.category, tc.description, tc.input, err, tc.expected)
			}
		})
	}
}

// controlInput is a loopback connection: network and host to listen and
// dial on, the options for the dialer (client) or the listener (accepted).
type controlInput struct {
	network string
	host    string
	opts    Options
	side    string // "client" checks the dialed socket, "accepted" the server's
}

// controlExpect is the error the dial or listen returns, or what getsockopt
// reads back from the checked socket.
type controlExpect struct {
	err       string
	cong      string
	ints      map[string]int
	rcvbufMin int
	rcvbufMax int // 0: no upper bound; -1: 2 × net.core.rmem_max
	// optional means an EPERM naming the option passes too (a capability
	// the test may not have).
	optional bool
}

// go test ./tools/internal/sockopt/ -run TestControlApplied
func TestControlApplied(t *testing.T) {
	tests := []struct {
		name        string
		category    string
		description string
		input       controlInput
		expected    controlExpect
	}{
		{"cong_reno", "positive", "the algorithm reaches the socket",
			controlInput{"tcp4", "127.0.0.1", Options{Cong: "reno"}, "client"},
			controlExpect{cong: "reno"}},
		{"tos_v4", "positive", "TOS on IPv4",
			controlInput{"tcp4", "127.0.0.1", Options{TOS: 0x08}, "client"},
			controlExpect{ints: map[string]int{"IP_TOS": 0x08}}},
		{"tos_v6", "positive", "TOS sets the IPv6 traffic class and IP_TOS",
			controlInput{"tcp6", "::1", Options{TOS: 0xb8}, "client"},
			controlExpect{ints: map[string]int{"IPV6_TCLASS": 0xb8, "IP_TOS": 0xb8}}},
		{"priority", "positive", "SO_PRIORITY reaches the socket",
			controlInput{"tcp4", "127.0.0.1", Options{Priority: 6}, "client"},
			controlExpect{ints: map[string]int{"SO_PRIORITY": 6}}},
		{"sockopt_bits", "positive", "freebind and bind-no-port are set",
			controlInput{"tcp6", "::1", Options{Freebind: true, BindNoPort: true}, "client"},
			controlExpect{ints: map[string]int{"IP_FREEBIND": 1, "IP_BIND_ADDRESS_NO_PORT": 1}}},
		{"unknown_cong", "negative", "a bad name fails the dial loudly",
			controlInput{"tcp4", "127.0.0.1", Options{Cong: "nope"}, "client"},
			controlExpect{err: "TCP_CONGESTION nope"}},
		{"unknown_cong_listener", "negative", "a bad name fails the listen loudly",
			controlInput{"tcp4", "127.0.0.1", Options{Cong: "nope"}, "accepted"},
			controlExpect{err: "TCP_CONGESTION nope"}},
		{"rcvbuf_clamped", "boundary", "above rmem_max is clamped, not fatal",
			controlInput{"tcp4", "127.0.0.1", Options{RcvBuf: 1 << 30}, "client"},
			controlExpect{rcvbufMin: 1, rcvbufMax: -1}},
		{"rcvbuf_tiny", "boundary", "1 is raised to the kernel minimum",
			controlInput{"tcp4", "127.0.0.1", Options{RcvBuf: 1}, "client"},
			controlExpect{rcvbufMin: 2}},
		{"tos_zero", "corner", "0 leaves TOS alone",
			controlInput{"tcp4", "127.0.0.1", Options{}, "client"},
			controlExpect{ints: map[string]int{"IP_TOS": 0}}},
		{"accepted_inherits", "corner", "an accepted socket takes the listener's options",
			controlInput{"tcp4", "127.0.0.1", Options{Cong: "reno", TOS: 0x10, Priority: 3}, "accepted"},
			controlExpect{cong: "reno", ints: map[string]int{"IP_TOS": 0x10, "SO_PRIORITY": 3}}},
		{"transparent", "corner", "set with CAP_NET_ADMIN, else an error naming it",
			controlInput{"tcp4", "127.0.0.1", Options{Transparent: true}, "accepted"},
			controlExpect{ints: map[string]int{"IP_TRANSPARENT": 1}, optional: true}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fd, err := loopbackSocket(t, tc.input)
			if tc.expected.optional && err != nil && errors.Is(err, syscall.EPERM) && strings.Contains(err.Error(), "IP_TRANSPARENT") {
				return
			}
			fail := func(format string, args ...any) {
				t.Helper()
				t.Errorf("%s [%s] %s: %+v: "+format, append([]any{tc.name, tc.category, tc.description, tc.input}, args...)...)
			}
			if !errMatches(err, tc.expected.err) {
				fail("error %v, expected error containing %q", err, tc.expected.err)
				return
			}
			if err != nil {
				return
			}
			if tc.expected.cong != "" {
				got, gerr := unix.GetsockoptString(fd, unix.IPPROTO_TCP, unix.TCP_CONGESTION)
				if gerr != nil || got != tc.expected.cong {
					fail("TCP_CONGESTION %q (%v), expected %q", got, gerr, tc.expected.cong)
				}
			}
			for name, want := range tc.expected.ints {
				o := readable[name]
				got, gerr := unix.GetsockoptInt(fd, o[0], o[1])
				if gerr != nil || got != want {
					fail("%s %d (%v), expected %d", name, got, gerr, want)
				}
			}
			if tc.expected.rcvbufMin > 0 {
				got, gerr := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_RCVBUF)
				hi := tc.expected.rcvbufMax
				if hi == -1 {
					hi = 2 * rmemMax(t)
				}
				if gerr != nil || got < tc.expected.rcvbufMin || (hi > 0 && got > hi) {
					fail("SO_RCVBUF %d (%v), expected %d..%d", got, gerr, tc.expected.rcvbufMin, hi)
				}
			}
		})
	}
}

// readable maps an option name to its getsockopt level and name.
var readable = map[string][2]int{
	"IP_TOS":                  {unix.IPPROTO_IP, unix.IP_TOS},
	"IPV6_TCLASS":             {unix.IPPROTO_IPV6, unix.IPV6_TCLASS},
	"SO_PRIORITY":             {unix.SOL_SOCKET, unix.SO_PRIORITY},
	"IP_FREEBIND":             {unix.IPPROTO_IP, unix.IP_FREEBIND},
	"IP_BIND_ADDRESS_NO_PORT": {unix.IPPROTO_IP, unix.IP_BIND_ADDRESS_NO_PORT},
	"IP_TRANSPARENT":          {unix.IPPROTO_IP, unix.IP_TRANSPARENT},
}

// loopbackSocket opens a loopback connection with in.opts on the dialer
// (side "client") or the listener (side "accepted") and returns the fd of
// that side's connected socket. The connections close with the test.
func loopbackSocket(t *testing.T, in controlInput) (int, error) {
	t.Helper()
	var lc net.ListenConfig
	var d net.Dialer
	if in.side == "accepted" {
		lc.Control = in.opts.Control
	} else {
		d.Control = in.opts.Control
	}
	ln, err := lc.Listen(t.Context(), in.network, net.JoinHostPort(in.host, "0"))
	if err != nil {
		if in.side != "accepted" {
			t.Skipf("no %s loopback: %v", in.network, err)
		}
		return 0, err
	}
	t.Cleanup(func() { _ = ln.Close() })
	accepted := make(chan net.Conn, 1)
	go func() {
		c, aerr := ln.Accept()
		if aerr != nil {
			close(accepted)
			return
		}
		accepted <- c
	}()
	c, err := d.DialContext(t.Context(), in.network, ln.Addr().String())
	if err != nil {
		return 0, err
	}
	t.Cleanup(func() { _ = c.Close() })
	conn := c
	if in.side == "accepted" {
		a, ok := <-accepted
		if !ok {
			t.Fatal("accept failed")
		}
		t.Cleanup(func() { _ = a.Close() })
		conn = a
	}
	return connFD(t, conn), nil
}

// connFD is the file descriptor behind a TCP connection, valid while the
// connection is open.
func connFD(t *testing.T, c net.Conn) int {
	t.Helper()
	raw, err := c.(*net.TCPConn).SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var fd int
	if err := raw.Control(func(f uintptr) { fd = int(f) }); err != nil {
		t.Fatal(err)
	}
	return fd
}

// rmemMax is net.core.rmem_max, the most SO_RCVBUF sets unprivileged.
func rmemMax(t *testing.T) int {
	t.Helper()
	b, err := os.ReadFile("/proc/sys/net/core/rmem_max")
	if err != nil {
		t.Skipf("rmem_max: %v", err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// errMatches reports whether err is nil when want is "", or contains want.
func errMatches(err error, want string) bool {
	if want == "" {
		return err == nil
	}
	return err != nil && strings.Contains(err.Error(), want)
}
