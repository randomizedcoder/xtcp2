package main

import (
	"bytes"
	"context"
	"net"
	"strings"
	"syscall"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
	"github.com/randomizedcoder/xtcp2/pkg/listener"
	"golang.org/x/sys/unix"
)

func TestHopLimitOptions(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expected string
		args                                  []string
		env                                   map[string]string
		ipv4TTL, ipv6HopLimit                 uint
		err                                   string
	}{
		{"defaults", "positive", "no flags or environment", "both limits 3", nil, nil, 3, 3, ""},
		{"flags", "positive", "both flags", "flag values", []string{"-ipv4-ttl=5", "-ipv6-hop-limit=7"}, nil, 5, 7, ""},
		{"environment", "positive", "both variables", "environment values", nil,
			map[string]string{ipv4TTLEnvironment: "9", ipv6HopsEnvironment: "11"}, 9, 11, ""},
		{"flag wins", "corner", "flag and variable both set", "flag value; the other limit takes its variable", []string{"-ipv4-ttl=4"},
			map[string]string{ipv4TTLEnvironment: "9", ipv6HopsEnvironment: "11"}, 4, 11, ""},
		{"explicit flag hides bad variable", "corner", "flag set and its variable unparseable", "flag value, no error", []string{"-ipv4-ttl=4"},
			map[string]string{ipv4TTLEnvironment: "many"}, 4, 3, ""},
		{"empty variable", "boundary", "empty variables", "defaults kept", nil,
			map[string]string{ipv4TTLEnvironment: "", ipv6HopsEnvironment: ""}, 3, 3, ""},
		{"zero", "boundary", "0 for both", "kernel default kept", []string{"-ipv4-ttl=0", "-ipv6-hop-limit=0"}, nil, 0, 0, ""},
		{"maximum", "boundary", "255 for both", "accepted", nil,
			map[string]string{ipv4TTLEnvironment: "255", ipv6HopsEnvironment: "255"}, 255, 255, ""},
		{"ttl over range", "negative", "-ipv4-ttl=256", "range error naming the flag", []string{"-ipv4-ttl=256"}, nil, 0, 0, "ipv4-ttl 256 must be 0-255"},
		{"hops over range", "negative", "variable 256", "range error naming the flag", nil,
			map[string]string{ipv6HopsEnvironment: "256"}, 0, 0, "ipv6-hop-limit 256 must be 0-255"},
		{"negative flag", "negative", "-ipv6-hop-limit=-1", "parse error", []string{"-ipv6-hop-limit=-1"}, nil, 0, 0, "invalid value"},
		{"bad variable", "negative", "unparseable variable", "error naming the variable", nil,
			map[string]string{ipv4TTLEnvironment: "three"}, 0, 0, ipv4TTLEnvironment},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			var out bytes.Buffer
			opts, err := parseOptions(tc.args, &out, environment(tc.env))
			if tc.err != "" {
				if err == nil || !strings.Contains(err.Error()+out.String(), tc.err) {
					t.Fatalf("err = %v, output %q; want %q", err, out.String(), tc.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if opts.hops.ipv4TTL != tc.ipv4TTL || opts.hops.ipv6HopLimit != tc.ipv6HopLimit {
				t.Fatalf("hops = %+v, want %d/%d", opts.hops, tc.ipv4TTL, tc.ipv6HopLimit)
			}
		})
	}
}

// TestListenerHopLimits binds listeners the way the command does and reads
// the limits back from the connection a client's dial accepts, since that is
// the socket replies leave on.
func TestListenerHopLimits(t *testing.T) {
	m, err := linkmonitor.New(linkmonitor.DefaultConfig(), linkmonitor.Options{})
	if err != nil {
		t.Fatal(err)
	}
	plain, err := adminOptions{}.resolve()
	if err != nil {
		t.Fatal(err)
	}
	defaults := hopLimits{ipv4TTL: defaultHopLimit, ipv6HopLimit: defaultHopLimit}
	metrics := func(h hopLimits) func(context.Context, string) (net.Listener, error) {
		return func(ctx context.Context, address string) (net.Listener, error) {
			lc := net.ListenConfig{Control: h.control()}
			return lc.Listen(ctx, "tcp", address)
		}
	}
	admin := func(h hopLimits) func(context.Context, string) (net.Listener, error) {
		return func(ctx context.Context, address string) (net.Listener, error) {
			a, err := newAdminEndpoint(listener.Endpoint{Network: listener.NetworkTCP, Address: address}, h, plain.policy, m, quietLogger(), prometheus.NewRegistry())
			if err != nil {
				return nil, err
			}
			return a.listen(ctx)
		}
	}
	for _, tc := range []struct {
		name, category, description, expected string
		listen                                func(context.Context, string) (net.Listener, error)
		address, dial                         string
		level, option                         int
		want                                  int // -1 is the kernel default
	}{
		{"metrics ipv4", "positive", "metrics listener on 127.0.0.1, default limits", "IP_TTL 3",
			metrics(defaults), "127.0.0.1:0", "127.0.0.1", unix.IPPROTO_IP, unix.IP_TTL, 3},
		{"metrics ipv6", "positive", "metrics listener on ::1, default limits", "IPV6_UNICAST_HOPS 3",
			metrics(defaults), "[::1]:0", "::1", unix.IPPROTO_IPV6, unix.IPV6_UNICAST_HOPS, 3},
		{"metrics dual stack", "corner", "wildcard listener as for :9101, IPv4 client", "IP_TTL 3 on the mapped connection",
			metrics(defaults), ":0", "127.0.0.1", unix.IPPROTO_IP, unix.IP_TTL, 3},
		{"admin ipv4", "positive", "TCP admin listener, default limits", "IP_TTL 3",
			admin(defaults), "127.0.0.1:0", "127.0.0.1", unix.IPPROTO_IP, unix.IP_TTL, 3},
		{"maximum", "boundary", "metrics listener, 255", "IP_TTL 255",
			metrics(hopLimits{ipv4TTL: 255, ipv6HopLimit: 255}), "127.0.0.1:0", "127.0.0.1", unix.IPPROTO_IP, unix.IP_TTL, 255},
		{"zero ipv4", "boundary", "metrics listener, 0", "kernel default IP_TTL",
			metrics(hopLimits{}), "127.0.0.1:0", "127.0.0.1", unix.IPPROTO_IP, unix.IP_TTL, -1},
		{"zero ipv6", "boundary", "metrics listener, 0", "kernel default IPV6_UNICAST_HOPS",
			metrics(hopLimits{}), "[::1]:0", "::1", unix.IPPROTO_IPV6, unix.IPV6_UNICAST_HOPS, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			ln, err := tc.listen(t.Context(), tc.address)
			if err != nil {
				if strings.Contains(tc.address, "::") {
					t.Skipf("IPv6 listen unavailable: %v", err)
				}
				t.Fatal(err)
			}
			defer ln.Close()
			want := tc.want
			if want < 0 {
				want = kernelDefault(t, tc.address, tc.dial, tc.level, tc.option)
			}
			if got := acceptedOption(t, ln, tc.dial, tc.level, tc.option); got != want {
				t.Fatalf("option = %d, want %d", got, want)
			}
		})
	}
}

// kernelDefault reads an option from a connection accepted by a listener bound
// without the hook. The listener itself may be MPTCP, which refuses IP_TTL.
func kernelDefault(t *testing.T, address, host string, level, option int) int {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return acceptedOption(t, ln, host, level, option)
}

// acceptedOption dials ln from host and reads the option on the accepted side.
func acceptedOption(t *testing.T, ln net.Listener, host string, level, option int) int {
	t.Helper()
	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	var d net.Dialer
	client, err := d.DialContext(t.Context(), "tcp", net.JoinHostPort(host, port))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	return socketOption(t, server.(*net.TCPConn), level, option)
}

func socketOption(t *testing.T, s syscall.Conn, level, option int) int {
	t.Helper()
	rc, err := s.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var value int
	var getErr error
	if err := rc.Control(func(fd uintptr) { value, getErr = unix.GetsockoptInt(int(fd), level, option) }); err != nil {
		t.Fatal(err)
	}
	if getErr != nil {
		t.Fatal(getErr)
	}
	return value
}
