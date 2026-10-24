package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
	"github.com/randomizedcoder/xtcp2/pkg/xtcp"
)

func TestIPListenAddress(t *testing.T) {
	tests := []struct {
		category, description, expected string
		config                          *xtcp_config.LinkMonitorConfig
		wantErr                         string
	}{
		{
			category: "positive", description: "loopback address with /ip on",
			expected: "127.0.0.1:9089",
			config:   &xtcp_config.LinkMonitorConfig{Enable: true, IpPaths: true, IpListen: "127.0.0.1:9089"},
		},
		{
			category: "positive", description: "IPv6 loopback",
			expected: "[::1]:9089",
			config:   &xtcp_config.LinkMonitorConfig{Enable: true, IpPaths: true, IpListen: "[::1]:9089"},
		},
		{
			category: "negative", description: "every address is refused",
			config:  &xtcp_config.LinkMonitorConfig{Enable: true, IpPaths: true, IpListen: ":9089"},
			wantErr: "loopback",
		},
		{
			category: "negative", description: "a host address is refused",
			config:  &xtcp_config.LinkMonitorConfig{Enable: true, IpPaths: true, IpListen: "10.0.0.1:9089"},
			wantErr: "loopback",
		},
		{
			category: "negative", description: "an address without /ip on is a misconfiguration",
			config:  &xtcp_config.LinkMonitorConfig{Enable: true, IpListen: "127.0.0.1:9089"},
			wantErr: "needs linkMonitorIPPaths",
		},
		{
			category: "boundary", description: "unset keeps one listener",
			expected: "",
			config:   &xtcp_config.LinkMonitorConfig{Enable: true, IpPaths: true},
		},
		{
			category: "boundary", description: "no link monitor config at all",
			expected: "",
		},
		{
			category: "corner", description: "a disabled monitor binds nothing",
			expected: "",
			config:   &xtcp_config.LinkMonitorConfig{IpPaths: true, IpListen: "127.0.0.1:9089"},
		},
		{
			category: "corner", description: "a disabled monitor still refuses a bad address",
			config:  &xtcp_config.LinkMonitorConfig{IpPaths: true, IpListen: "0.0.0.0:9089"},
			wantErr: "loopback",
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: %q %s", tc.category, tc.description, tc.expected, tc.wantErr)
			got, err := ipListenAddress(tc.config)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil || got != tc.expected {
				t.Fatalf("ipListenAddress = %q, %v; want %q", got, err, tc.expected)
			}
		})
	}
}

// freeLoopbackPort returns a loopback address with a port nothing listens on
// (for now): ip_listen refuses port 0.
func freeLoopbackPort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

// TestStartIPServer binds the real /ip listener and serves the late-bound
// routes from loopback; the peer filter itself is pkg/listener's test.
func TestStartIPServer(t *testing.T) {
	tests := []struct {
		category, description, expected string
		attach, occupied, badAddr       bool
		wantCode                        int
		wantErr                         string
	}{
		{"positive", "an attached daemon's /ip routes serve", "200", true, false, false, http.StatusOK, ""},
		{"negative", "nothing attached yet", "404", false, false, false, http.StatusNotFound, ""},
		{"negative", "a non-loopback address fails startup", "error, nothing bound", false, false, true, 0, "loopback"},
		{"boundary", "a port in use fails startup", "error naming the ip listener", false, true, false, 0, "ip listener"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			addr := freeLoopbackPort(t)
			if tc.occupied {
				ln, err := net.Listen("tcp", addr)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = ln.Close() }()
			}
			if tc.badAddr {
				addr = "0.0.0.0:9089"
			}
			if tc.attach {
				h := http.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
				linkMonitorHTTP.ip.Store(&h)
				t.Cleanup(func() { linkMonitorHTTP.ip.Store(nil) })
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var wg sync.WaitGroup
			err := startIPServer(ctx, &wg, &xtcp_config.LinkMonitorConfig{Enable: true, IpPaths: true, IpListen: addr})
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				waitReturns(t, &wg, time.Second)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			resp, err := http.Get("http://" + addr + "/ip/link")
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()
			if resp.StatusCode != tc.wantCode {
				t.Errorf("GET /ip/link = %d, want %d", resp.StatusCode, tc.wantCode)
			}
			cancel()
			waitReturns(t, &wg, promShutdownGrace+time.Second)
		})
	}
}

// TestLinkMonitorRoutesIP covers the /ip listener's late binding.
func TestLinkMonitorRoutesIP(t *testing.T) {
	tests := []struct {
		category, description, expected string
		attach, detach                  bool
	}{
		{"negative", "before a daemon starts", "404", false, false},
		{"negative", "a daemon run without a monitor", "404", true, false},
		{"corner", "after the daemon stops", "404", true, true},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			l := &linkMonitorRoutes{promPath: "/metrics"}
			if tc.attach {
				detach, err := l.attach(&xtcp.XTCP{})
				if err != nil {
					t.Fatal(err)
				}
				if tc.detach {
					detach()
				}
			}
			srv := newPromServer(l.ipRoutes())
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			go func() { _ = srv.Serve(ln) }()
			defer func() { _ = srv.Close() }()
			resp, err := http.Get("http://" + ln.Addr().String() + "/ip/link")
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("GET /ip/link = %d, want 404", resp.StatusCode)
			}
		})
	}
}

func TestRateLimitedRejectLog(t *testing.T) {
	peer := &net.TCPAddr{IP: net.IPv4(172, 17, 0, 2), Port: 40000}
	tests := []struct {
		category, description string
		every                 time.Duration
		rejects               int
		closeErr              error
		expected              []string // log lines, in order
	}{
		{"positive", "the first reject is logged at once", time.Hour, 1, nil, []string{"closed 1 connection(s)"}},
		{"negative", "a burst inside the window logs one line", time.Hour, 50, nil, []string{"closed 1 connection(s)"}},
		{"boundary", "no rejects, no log", time.Hour, 0, nil, nil},
		{"corner", "a zero window logs each reject with its own count", 0, 3, nil, []string{"closed 1 ", "closed 1 ", "closed 1 "}},
		{"corner", "a close error is on the line", time.Hour, 1, errors.New("close failed"), []string{"40000: close failed"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: %v", tc.category, tc.description, tc.expected)
			reject := rateLimitedRejectLog(tc.every)
			out := captureLog(t, func() {
				for range tc.rejects {
					reject(peer, tc.closeErr)
				}
			})
			var lines []string
			for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
				if l != "" {
					lines = append(lines, l)
				}
			}
			if len(lines) != len(tc.expected) {
				t.Fatalf("log lines = %q, want %d", lines, len(tc.expected))
			}
			for i, want := range tc.expected {
				if !strings.Contains(lines[i], want) || !strings.Contains(lines[i], "172.17.0.2:40000") {
					t.Errorf("line %d = %q, want %q and the peer", i, lines[i], want)
				}
			}
		})
	}
}
