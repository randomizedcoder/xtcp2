package main

import (
	"errors"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp"
)

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
			srv := newHTTPServer(l.ipRoutes())
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
		{"positive", "the first reject is logged at once, naming the listener", time.Hour, 1, nil, []string{`listener "ip": closed 1 connection(s)`}},
		{"negative", "a burst inside the window logs one line", time.Hour, 50, nil, []string{"closed 1 connection(s)"}},
		{"boundary", "no rejects, no log", time.Hour, 0, nil, nil},
		{"corner", "a zero window logs each reject with its own count", 0, 3, nil, []string{"closed 1 ", "closed 1 ", "closed 1 "}},
		{"corner", "a close error is on the line", time.Hour, 1, errors.New("close failed"), []string{"40000: close failed"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: %v", tc.category, tc.description, tc.expected)
			reject := rateLimitedRejectLog("ip", tc.every)
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
