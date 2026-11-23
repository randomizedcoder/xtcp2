package xtcp

import (
	"context"
	"encoding/binary"
	"errors"
	"maps"
	"net"
	"net/netip"
	"strconv"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// diagRow is the part of one inet_diag_msg these tests read.
type diagRow struct {
	family       uint8
	sport, dport uint16
	dst          netip.Addr
}

// realDump sends each request to a real NETLINK_SOCK_DIAG socket in the
// test's own namespace (no privilege needed) and returns every TCP socket
// the kernel reported, reading each dump to its NLMSG_DONE.
func realDump(t *testing.T, reqs []*[]byte) []diagRow {
	t.Helper()
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_CLOEXEC, unix.NETLINK_SOCK_DIAG)
	if err != nil {
		t.Skipf("no NETLINK_SOCK_DIAG socket: %v", err)
	}
	defer func() { _ = unix.Close(fd) }()
	tv := unix.NsecToTimeval((5 * time.Second).Nanoseconds())
	if err := unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &tv); err != nil {
		t.Fatalf("SO_RCVTIMEO: %v", err)
	}
	var rows []diagRow
	buf := make([]byte, 64*1024)
	for _, req := range reqs {
		if err := unix.Sendto(fd, *req, 0, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
			t.Fatalf("sendto: %v", err)
		}
		for done := false; !done; {
			n, _, err := unix.Recvfrom(fd, buf, 0)
			if err != nil {
				t.Fatalf("recvfrom: %v", err)
			}
			for off := 0; off+xtcpnl.NlMsgHdrSizeCst <= n; {
				l := int(binary.LittleEndian.Uint32(buf[off : off+4]))
				typ := binary.LittleEndian.Uint16(buf[off+4 : off+6])
				if l < xtcpnl.NlMsgHdrSizeCst || off+l > n {
					t.Fatalf("bad nlmsg_len %d", l)
				}
				body := buf[off+xtcpnl.NlMsgHdrSizeCst : off+l]
				switch typ {
				case xtcpnl.NlMsgHdrTypeDoneCst:
					done = true
				case unix.NLMSG_ERROR:
					t.Fatalf("NLMSG_ERROR %d", int32(binary.LittleEndian.Uint32(body[0:4])))
				case xtcpnl.NlMsgHdrTypeInetDiagCst:
					// inet_diag_msg: family, state, timer, retrans, then
					// inet_diag_sockid: sport, dport (big endian), src[16], dst[16].
					r := diagRow{
						family: body[0],
						sport:  binary.BigEndian.Uint16(body[4:6]),
						dport:  binary.BigEndian.Uint16(body[6:8]),
					}
					if r.family == unix.AF_INET {
						r.dst = netip.AddrFrom4([4]byte(body[24:28]))
					} else {
						r.dst = netip.AddrFrom16([16]byte(body[24:40]))
					}
					rows = append(rows, r)
				}
				off += nlmsgAlign(l)
			}
		}
	}
	return rows
}

// loopbackSockets opens one row's sockets and returns the listener's port.
type loopbackSockets struct {
	listenNet, listenAddr string
	v6only                bool
	dialNet, dialAddr     string // "" for no client
	dialRefused           bool   // the dial is expected to fail
}

func (s loopbackSockets) open(t *testing.T) uint16 {
	t.Helper()
	lc := net.ListenConfig{}
	if s.v6only {
		lc.Control = func(_, _ string, c syscall.RawConn) error {
			var serr error
			if err := c.Control(func(fd uintptr) {
				serr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IPV6, unix.IPV6_V6ONLY, 1)
			}); err != nil {
				return err
			}
			return serr
		}
	}
	ln, err := lc.Listen(context.Background(), s.listenNet, s.listenAddr)
	if err != nil {
		t.Skipf("listen %s %s: %v (no IPv6 loopback?)", s.listenNet, s.listenAddr, err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	port := uint16(ln.Addr().(*net.TCPAddr).Port)
	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err == nil {
			accepted <- c
		}
	}()
	if s.dialNet == "" {
		return port
	}
	d := net.Dialer{Timeout: time.Second}
	c, err := d.Dial(s.dialNet, net.JoinHostPort(s.dialAddr, strconv.Itoa(int(port))))
	if s.dialRefused {
		if err == nil {
			_ = c.Close()
			t.Fatalf("dial %s %s: connected, expected refused", s.dialNet, s.dialAddr)
		}
		if !errors.Is(err, syscall.ECONNREFUSED) {
			t.Fatalf("dial %s %s: %v, expected ECONNREFUSED", s.dialNet, s.dialAddr, err)
		}
		return port
	}
	if err != nil {
		t.Fatalf("dial %s %s: %v", s.dialNet, s.dialAddr, err)
	}
	t.Cleanup(func() { resetClose(c) })
	select {
	case a := <-accepted:
		t.Cleanup(func() { resetClose(a) })
	case <-time.After(2 * time.Second):
		t.Fatal("accept timed out")
	}
	return port
}

// TestRealDumpFamilies dumps the test's own namespace with the requests a
// poll sends and counts, per family, the sockets on one row's port: its
// listener, the client and the accepted socket. Before the AF_INET6 dump
// existed, tcp6_pair, dual_stack, v6_listener_only and v6only_listener
// failed with no family-10 rows.
func TestRealDumpFamilies(t *testing.T) {
	type expectation struct {
		perFamily map[uint8]int
		mappedDst int // AF_INET6 rows whose destination is ::ffff:127.0.0.1
	}
	tests := []struct {
		name        string
		category    string
		description string
		sockets     loopbackSockets
		watchOther  bool // count a port nothing uses, not the listener's
		expected    expectation
	}{
		{
			name: "tcp4_pair", category: "positive",
			description: "IPv4-only sockets: listener, client and accepted are AF_INET",
			sockets:     loopbackSockets{listenNet: "tcp4", listenAddr: "127.0.0.1:0", dialNet: "tcp4", dialAddr: "127.0.0.1"},
			expected:    expectation{perFamily: map[uint8]int{unix.AF_INET: 3}},
		},
		{
			name: "tcp6_pair", category: "positive",
			description: "IPv6-only sockets: listener, client and accepted are AF_INET6",
			sockets:     loopbackSockets{listenNet: "tcp6", listenAddr: "[::1]:0", dialNet: "tcp6", dialAddr: "::1"},
			expected:    expectation{perFamily: map[uint8]int{unix.AF_INET6: 3}},
		},
		{
			name: "dual_stack", category: "positive",
			description: "a dual-stack listener on [::] and an IPv4 client: listener and accepted are AF_INET6 (v4-mapped), the client AF_INET",
			sockets:     loopbackSockets{listenNet: "tcp", listenAddr: "[::]:0", dialNet: "tcp4", dialAddr: "127.0.0.1"},
			expected:    expectation{perFamily: map[uint8]int{unix.AF_INET: 1, unix.AF_INET6: 2}, mappedDst: 1},
		},
		{
			name: "foreign_port", category: "negative",
			description: "sockets on another port are not counted for this one",
			sockets:     loopbackSockets{listenNet: "tcp4", listenAddr: "127.0.0.1:0", dialNet: "tcp4", dialAddr: "127.0.0.1"},
			watchOther:  true,
			expected:    expectation{perFamily: map[uint8]int{}},
		},
		{
			name: "v6_listener_only", category: "boundary",
			description: "an IPv6 listener with no connection: one AF_INET6 row",
			sockets:     loopbackSockets{listenNet: "tcp6", listenAddr: "[::1]:0"},
			expected:    expectation{perFamily: map[uint8]int{unix.AF_INET6: 1}},
		},
		{
			name: "v6only_listener", category: "corner",
			description: "an IPV6_V6ONLY listener on [::] refuses an IPv4 client: only the listener's AF_INET6 row",
			sockets:     loopbackSockets{listenNet: "tcp6", listenAddr: "[::]:0", v6only: true, dialNet: "tcp4", dialAddr: "127.0.0.1", dialRefused: true},
			expected:    expectation{perFamily: map[uint8]int{unix.AF_INET6: 1}},
		},
	}
	mapped := netip.AddrFrom16([16]byte{10: 0xff, 11: 0xff, 12: 127, 15: 1})
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			port := tc.sockets.open(t)
			if tc.watchOther {
				port = unusedPort(t)
			}
			x := newTestRequestXTCP(enabledFromKeys(GetDefaultDeserializers()...))
			got := expectation{perFamily: map[uint8]int{}}
			for _, r := range realDump(t, x.createNetLinkRequests()) {
				if r.sport != port && r.dport != port {
					continue
				}
				got.perFamily[r.family]++
				if r.family == unix.AF_INET6 && r.dst == mapped {
					got.mappedDst++
				}
			}
			if !maps.Equal(got.perFamily, tc.expected.perFamily) || got.mappedDst != tc.expected.mappedDst {
				t.Errorf("%s (%s): %s\n port %d: got %+v, expected %+v", tc.name, tc.category, tc.description, port, got, tc.expected)
			}
		})
	}
}

// resetClose closes c with an RST (SO_LINGER 0), so neither end lingers in
// FIN_WAIT or TIME_WAIT: sockstat-based tests in this package expect a
// quiet process.
func resetClose(c net.Conn) {
	if tc, ok := c.(*net.TCPConn); ok {
		_ = tc.SetLinger(0)
	}
	_ = c.Close()
}

// unusedPort returns a loopback port nothing listens on: one the kernel
// handed out and released.
func unusedPort(t *testing.T) uint16 {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := uint16(ln.Addr().(*net.TCPAddr).Port)
	_ = ln.Close()
	return port
}
