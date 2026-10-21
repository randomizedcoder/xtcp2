package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
	"github.com/randomizedcoder/xtcp2/pkg/xtcp"
)

// unixClient is an HTTP client for the prom listener on sock.
func unixClient(sock string) *http.Client {
	return &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", sock)
		}},
	}
}

func getStatus(t *testing.T, c *http.Client, path string) int {
	t.Helper()
	resp, err := c.Get("http://prom" + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

// waitReturns fails t unless wg.Wait returns within d.
func waitReturns(t *testing.T, wg *sync.WaitGroup, d time.Duration) {
	t.Helper()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("WaitGroup still waiting after %v", d)
	}
}

// TestStartPromServer covers the prom listener's lifecycle: it binds before
// returning, serves until the run context ends, then drains and leaves the
// WaitGroup.
func TestStartPromServer(t *testing.T) {
	tests := []struct {
		name, category, description, expectedOutcome string
		params                                       func(t *testing.T, sock string) promParams
		wantErr                                      string
		requests                                     map[string]int
	}{
		{
			name: "serves_and_drains", category: "positive",
			description:     "a unix socket listener serves metrics, health and pprof, then stops on cancel",
			expectedOutcome: "200s, 404 for link monitor paths with no daemon attached, Wait returns",
			params: func(_ *testing.T, sock string) promParams {
				return promParams{path: "/metrics", listener: &xtcp_config.ListenerEndpoint{Network: xtcp_config.ListenerNetwork_LISTENER_NETWORK_UNIX, Address: sock}}
			},
			requests: map[string]int{"/metrics": http.StatusOK, "/healthz": http.StatusOK, "/debug/pprof/": http.StatusOK, "/metrics/link": http.StatusNotFound, "/metrics-all": http.StatusNotFound},
		},
		{
			name: "bad_address", category: "negative",
			description:     "an unparsable TCP address fails startup",
			expectedOutcome: "error, no worker",
			params: func(*testing.T, string) promParams {
				return promParams{path: "/metrics", listen: "invalid-host:-1"}
			},
			wantErr: "prometheus",
		},
		{
			name: "port_in_use", category: "negative",
			description:     "an address already bound fails startup instead of exiting later",
			expectedOutcome: "listen error, no worker",
			params: func(t *testing.T, _ string) promParams {
				ln, err := net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = ln.Close() })
				return promParams{path: "/metrics", listen: ln.Addr().String()}
			},
			wantErr: "listen",
		},
		{
			name: "bad_auth", category: "negative",
			description:     "an invalid listener auth config fails before binding",
			expectedOutcome: "auth config error",
			params: func(*testing.T, string) promParams {
				return promParams{path: "/metrics", listen: "127.0.0.1:0", auth: &xtcp_config.ListenerAuth{Mode: xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_RAW_TOKEN}}
			},
			wantErr: "listener auth config invalid",
		},
		{
			name: "root_prom_path", category: "corner",
			description:     "a promPath of / serves metrics on every path and mounts no link monitor routes",
			expectedOutcome: "no mux conflict panic, metrics on /anything",
			params: func(_ *testing.T, sock string) promParams {
				return promParams{path: "/", listener: &xtcp_config.ListenerEndpoint{Network: xtcp_config.ListenerNetwork_LISTENER_NETWORK_UNIX, Address: sock}}
			},
			requests: map[string]int{"/anything": http.StatusOK, "/healthz": http.StatusOK},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			sock := filepath.Join(t.TempDir(), "prom.sock")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var wg sync.WaitGroup
			err := startPromServer(ctx, &wg, tc.params(t, sock))
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
			c := unixClient(sock)
			for path, want := range tc.requests {
				if got := getStatus(t, c, path); got != want {
					t.Errorf("GET %s = %d, want %d", path, got, want)
				}
			}
			cancel()
			waitReturns(t, &wg, promShutdownGrace+time.Second)
			if _, err := c.Get("http://prom/healthz"); err == nil {
				t.Error("listener still serving after shutdown")
			}
		})
	}
}

// TestServePromHandler covers the serve worker's two exits: a serve failure
// while running, and a shutdown whose drain outlasts the grace.
func TestServePromHandler(t *testing.T) {
	tests := []struct {
		name, category, description, expectedOutcome string
		closeFirst, stall                            bool
		wantFatal                                    string
	}{
		{"serve_failure", "negative", "the listener fails while the daemon runs", "fatalf names the prometheus error", true, false, "prometheus error"},
		{"drain_past_grace", "boundary", "a request stalls past the shutdown grace", "server closed, worker returns, no fatal", false, true, ""},
		{"idle_shutdown", "positive", "shutdown with nothing in flight", "worker returns at once, no fatal", false, false, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			prev := fatalf
			var fatal atomic.Value
			fatal.Store("")
			fatalf = func(format string, args ...any) { fatal.Store(fmt.Sprintf(format, args...)) }
			t.Cleanup(func() { fatalf = prev })

			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			release := make(chan struct{})
			defer close(release)
			entered := make(chan struct{}, 1)
			srv := newPromServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				entered <- struct{}{}
				<-release
			}))
			if tc.closeFirst {
				_ = ln.Close()
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var wg sync.WaitGroup
			wg.Add(1)
			go servePromHandler(ctx, &wg, srv, ln, 50*time.Millisecond)
			if tc.closeFirst {
				waitReturns(t, &wg, time.Second)
			} else {
				if tc.stall {
					go func() {
						resp, err := http.Get("http://" + ln.Addr().String() + "/")
						if err == nil {
							_ = resp.Body.Close()
						}
					}()
					<-entered
				}
				cancel()
				waitReturns(t, &wg, 2*time.Second)
			}
			if got := fatal.Load().(string); !strings.Contains(got, tc.wantFatal) || (tc.wantFatal == "" && got != "") {
				t.Fatalf("fatal = %q, want %q", got, tc.wantFatal)
			}
		})
	}
}

// TestLinkMonitorRoutes covers the late-bound link monitor routes and the
// SIGUSR1 target as a daemon comes and goes.
func TestLinkMonitorRoutes(t *testing.T) {
	tests := []struct {
		name, category, description, expectedOutcome string
		attach, detach                               bool
		wantCode                                     int
		wantLog                                      string
	}{
		{"nothing_attached", "negative", "before a daemon starts", "404, SIGUSR1 ignored", false, false, http.StatusNotFound, "no daemon running"},
		{"daemon_without_monitor", "negative", "a daemon run with -linkMonitor=false", "404, SIGUSR1 rejected as disabled", true, false, http.StatusNotFound, "link monitor is disabled"},
		{"after_detach", "corner", "after the daemon stops, as on a reconfigure", "404, SIGUSR1 ignored", true, true, http.StatusNotFound, "no daemon running"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
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
			rec := httptest.NewRecorder()
			l.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics/link", nil))
			if rec.Code != tc.wantCode {
				t.Errorf("GET /metrics/link = %d, want %d", rec.Code, tc.wantCode)
			}
			out := captureLog(t, l.rebaseline)
			if !strings.Contains(out, tc.wantLog) {
				t.Errorf("rebaseline log %q, want %q", out, tc.wantLog)
			}
		})
	}
}

// TestAwaitSignalAndShutdown_sigusr1 checks SIGUSR1 rebaselines without
// shutting down, and a later SIGTERM still shuts down.
func TestAwaitSignalAndShutdown_sigusr1(t *testing.T) {
	tests := []struct {
		name, category, description, expectedOutcome string
		usr1                                         int
		rebaseline                                   bool
	}{
		{"two_rebaselines", "positive", "two SIGUSR1 then SIGTERM", "two rebaselines, then cancel", 2, true},
		{"no_rebaseline_func", "corner", "SIGUSR1 with no rebaseline target", "ignored, then cancel", 1, false},
		{"terminate_only", "boundary", "SIGTERM alone", "no rebaseline, cancel", 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			sigs := make(chan os.Signal, tc.usr1+1)
			complete := make(chan struct{}, 1)
			var rebaselines, cancels atomic.Int32
			var rebaseline func()
			if tc.rebaseline {
				rebaseline = func() { rebaselines.Add(1) }
			}
			done := make(chan struct{})
			go func() {
				awaitSignalAndShutdown(sigs, func() { cancels.Add(1) }, complete, time.Second, false, rebaseline)
				close(done)
			}()
			for range tc.usr1 {
				sigs <- syscall.SIGUSR1
			}
			sigs <- syscall.SIGTERM
			complete <- struct{}{}
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("awaitSignalAndShutdown did not return")
			}
			want := int32(0)
			if tc.rebaseline {
				want = int32(tc.usr1)
			}
			if got := rebaselines.Load(); got != want {
				t.Errorf("rebaselines = %d, want %d", got, want)
			}
			if cancels.Load() != 1 {
				t.Errorf("cancels = %d, want 1", cancels.Load())
			}
		})
	}
}
