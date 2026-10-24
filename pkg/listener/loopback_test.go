package listener

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"testing"
)

func TestParseLoopback_table(t *testing.T) {
	tests := []struct {
		category    string
		description string
		address     string
		expected    string // the parsed AddrPort, or "" for ErrNotLoopback
	}{
		{"positive", "IPv4 loopback", "127.0.0.1:9089", "127.0.0.1:9089"},
		{"positive", "IPv6 loopback", "[::1]:9089", "[::1]:9089"},
		{"positive", "another 127/8 address", "127.0.0.2:9089", "127.0.0.2:9089"},
		{"negative", "every address: empty host", ":9089", ""},
		{"negative", "IPv4 wildcard", "0.0.0.0:9089", ""},
		{"negative", "IPv6 wildcard", "[::]:9089", ""},
		{"negative", "a host address", "10.0.0.1:9089", ""},
		{"negative", "localhost is a name, not a literal", "localhost:9089", ""},
		{"negative", "a service name port", "127.0.0.1:http", ""},
		{"negative", "no port", "127.0.0.1", ""},
		{"negative", "empty", "", ""},
		{"boundary", "lowest port", "127.0.0.1:1", "127.0.0.1:1"},
		{"boundary", "highest port", "127.0.0.1:65535", "127.0.0.1:65535"},
		{"boundary", "port 0 picks an ephemeral port nobody can find", "127.0.0.1:0", ""},
		{"boundary", "port past 65535", "127.0.0.1:65536", ""},
		{"boundary", "top of 127/8", "127.255.255.254:9089", "127.255.255.254:9089"},
		{"boundary", "just past 127/8", "128.0.0.1:9089", ""},
		{"corner", "IPv4-mapped loopback", "[::ffff:127.0.0.1]:9089", "[::ffff:127.0.0.1]:9089"},
		{"corner", "IPv4-mapped host address", "[::ffff:10.0.0.1]:9089", ""},
		{"corner", "negative port", "127.0.0.1:-1", ""},
		{"corner", "unbracketed IPv6", "::1:9089", ""},
	}
	for _, tt := range tests {
		t.Run(tt.category+"/"+tt.description, func(t *testing.T) {
			got, err := ParseLoopback(tt.address)
			if tt.expected == "" {
				if !errors.Is(err, ErrNotLoopback) {
					t.Fatalf("ParseLoopback(%q) = %v, %v; want ErrNotLoopback", tt.address, got, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseLoopback(%q): %v", tt.address, err)
			}
			if got.String() != tt.expected {
				t.Fatalf("ParseLoopback(%q) = %s, want %s", tt.address, got, tt.expected)
			}
		})
	}
}

func tcpAddr(s string) *net.TCPAddr {
	return net.TCPAddrFromAddrPort(netip.MustParseAddrPort(s))
}

func TestPeerIsLoopback_table(t *testing.T) {
	tests := []struct {
		category    string
		description string
		addr        net.Addr
		expected    bool
	}{
		{"positive", "IPv4 loopback", tcpAddr("127.0.0.1:40000"), true},
		{"positive", "IPv6 loopback", tcpAddr("[::1]:40000"), true},
		{"negative", "a container on a docker bridge", tcpAddr("172.17.0.2:40000"), false},
		{"negative", "a public address", tcpAddr("203.0.113.9:40000"), false},
		{"negative", "a UDS peer", &net.UnixAddr{Name: "@", Net: "unix"}, false},
		{"boundary", "top of 127/8", tcpAddr("127.255.255.255:1"), true},
		{"boundary", "just past 127/8", tcpAddr("128.0.0.0:1"), false},
		{"corner", "IPv4-mapped loopback", tcpAddr("[::ffff:127.0.0.1]:1"), true},
		{"corner", "nil TCP address", (*net.TCPAddr)(nil), false},
		{"corner", "TCP address with no IP", &net.TCPAddr{Port: 1}, false},
		{"corner", "no address at all", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.category+"/"+tt.description, func(t *testing.T) {
			if got := PeerIsLoopback(tt.addr); got != tt.expected {
				t.Fatalf("PeerIsLoopback(%v) = %t, want %t", tt.addr, got, tt.expected)
			}
		})
	}
}

// fakeConn is a connection from remote that records its Close and returns
// closeErr from it.
type fakeConn struct {
	net.Conn
	remote   net.Addr
	closeErr error
	closed   bool
}

func (c *fakeConn) RemoteAddr() net.Addr { return c.remote }
func (c *fakeConn) Close() error         { c.closed = true; return c.closeErr }

// fakeListener hands out conns in order, then errEnd.
type fakeListener struct {
	net.Listener
	conns []*fakeConn
}

var (
	errEnd   = errors.New("no more connections")
	errClose = errors.New("close failed")
)

func (l *fakeListener) Accept() (net.Conn, error) {
	if len(l.conns) == 0 {
		return nil, errEnd
	}
	c := l.conns[0]
	l.conns = l.conns[1:]
	return c, nil
}

func TestLoopbackOnly_table(t *testing.T) {
	tests := []struct {
		category    string
		description string
		peers       []string
		failClose   bool     // every Close returns errClose
		expected    []string // peers Accept returns, in order
		rejected    []string // peers closed and reported, with any close error
	}{
		{
			category: "positive", description: "a loopback peer is accepted",
			peers: []string{"127.0.0.1:1"}, expected: []string{"127.0.0.1:1"},
		},
		{
			category: "negative", description: "a bridge peer is closed and reported",
			peers: []string{"172.17.0.2:1"}, rejected: []string{"172.17.0.2:1 <nil>"},
		},
		{
			category: "negative", description: "a failed close is reported with the peer",
			peers: []string{"172.17.0.2:1"}, failClose: true, rejected: []string{"172.17.0.2:1 close failed"},
		},
		{
			category: "boundary", description: "no connections: the listener's error passes through",
		},
		{
			category: "corner", description: "rejects are skipped until a loopback peer arrives",
			peers:    []string{"172.17.0.2:1", "[::1]:2", "10.0.0.1:3", "127.0.0.9:4"},
			expected: []string{"[::1]:2", "127.0.0.9:4"},
			rejected: []string{"172.17.0.2:1 <nil>", "10.0.0.1:3 <nil>"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.category+"/"+tt.description, func(t *testing.T) {
			fl := &fakeListener{}
			for _, p := range tt.peers {
				c := &fakeConn{remote: tcpAddr(p)}
				if tt.failClose {
					c.closeErr = errClose
				}
				fl.conns = append(fl.conns, c)
			}
			all := append([]*fakeConn(nil), fl.conns...)
			var rejected []string
			ln := LoopbackOnly(fl, func(a net.Addr, err error) { rejected = append(rejected, fmt.Sprint(a, " ", err)) })
			var accepted []string
			for {
				c, err := ln.Accept()
				if err != nil {
					if !errors.Is(err, errEnd) {
						t.Fatalf("Accept: %v", err)
					}
					break
				}
				accepted = append(accepted, c.RemoteAddr().String())
			}
			if !equal(accepted, tt.expected) || !equal(rejected, tt.rejected) {
				t.Fatalf("accepted %v rejected %v, want %v and %v", accepted, rejected, tt.expected, tt.rejected)
			}
			for _, c := range all {
				if c.closed == PeerIsLoopback(c.remote) {
					t.Fatalf("peer %s closed=%t", c.remote, c.closed)
				}
			}
		})
	}
}

func TestLoopbackOnly_nilRejected(t *testing.T) {
	c := &fakeConn{remote: tcpAddr("10.0.0.1:1")}
	fl := &fakeListener{conns: []*fakeConn{c}}
	if _, err := LoopbackOnly(fl, nil).Accept(); !errors.Is(err, errEnd) {
		t.Fatalf("Accept = %v, want errEnd after closing the rejected peer", err)
	}
	if !c.closed {
		t.Fatal("the rejected peer was not closed")
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
