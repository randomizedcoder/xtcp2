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
	"github.com/randomizedcoder/xtcp2/pkg/listeners"
	"github.com/randomizedcoder/xtcp2/pkg/xtcp"
)

// unixClient is an HTTP client for the listener on sock.
func unixClient(sock string) *http.Client {
	return &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", sock)
		}},
	}
}

// getStatus GETs path, with a bearer token unless token is empty.
func getStatus(t *testing.T, c *http.Client, path, token string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://listener"+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.Do(req)
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

// writeToken writes a 0600 token file and returns its path.
func writeToken(t *testing.T, dir, token string) string {
	t.Helper()
	p := filepath.Join(dir, "token")
	if err := os.WriteFile(p, []byte(token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// unixListener is a listener on dir/<name>.sock serving classes.
func unixListener(dir, name string, auth *xtcp_config.ListenerAuth, classes ...xtcp_config.EndpointClass) *xtcp_config.Listener {
	return &xtcp_config.Listener{
		Name:     name,
		Endpoint: &xtcp_config.ListenerEndpoint{Address: filepath.Join(dir, name+".sock")},
		Auth:     auth,
		Serves:   classes,
	}
}

var authOff = &xtcp_config.ListenerAuth{Mode: xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_DISABLED}

// TestStartListeners binds real listeners from a resolved config and checks
// what each serves: a path of a class a listener does not serve is 404, an
// unauthenticated request to an authenticated one is 401, and a bind
// failure fails startup with nothing left bound.
func TestStartListeners(t *testing.T) {
	type req struct {
		listener, path, token string
		expected              int
	}
	tests := []struct {
		name, category, description string
		listeners                   func(t *testing.T, dir string) []*xtcp_config.Listener
		ipPaths                     bool
		promPath                    string
		requests                    []req
		grpc                        int
		wantErr                     string
	}{
		{
			name: "default_shape", category: "positive",
			description: "health, metrics and metrics-detail on a socket; pprof and /ip are not served",
			listeners: func(_ *testing.T, dir string) []*xtcp_config.Listener {
				return []*xtcp_config.Listener{unixListener(dir, "metrics", authOff, listeners.Health, listeners.Metrics, listeners.MetricsDetail)}
			},
			requests: []req{
				{"metrics", "/metrics", "", http.StatusOK},
				{"metrics", "/healthz", "", http.StatusOK},
				{"metrics", "/debug/pprof/", "", http.StatusNotFound},
				{"metrics", "/ip/link", "", http.StatusNotFound},
			},
		},
		{
			name: "raw_token", category: "negative",
			description: "a listener with a token refuses a request without it",
			listeners: func(t *testing.T, dir string) []*xtcp_config.Listener {
				a := &xtcp_config.ListenerAuth{Mode: xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_RAW_TOKEN, RawTokenFile: writeToken(t, dir, "s3cret")}
				return []*xtcp_config.Listener{unixListener(dir, "metrics", a, listeners.Health, listeners.Metrics)}
			},
			requests: []req{
				{"metrics", "/metrics", "", http.StatusUnauthorized},
				{"metrics", "/metrics", "wrong", http.StatusUnauthorized},
				{"metrics", "/metrics", "s3cret", http.StatusOK},
			},
		},
		{
			name: "debug_listener", category: "positive",
			description: "pprof only on the debug listener, never cmdline; the metrics listener has no pprof",
			listeners: func(t *testing.T, dir string) []*xtcp_config.Listener {
				a := &xtcp_config.ListenerAuth{Mode: xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_RAW_TOKEN, RawTokenFile: writeToken(t, dir, "admin")}
				return []*xtcp_config.Listener{
					unixListener(dir, "metrics", authOff, listeners.Health, listeners.Metrics),
					unixListener(dir, "debug", a, listeners.Debug),
				}
			},
			requests: []req{
				{"debug", "/debug/pprof/", "admin", http.StatusOK},
				{"debug", "/debug/pprof/cmdline", "admin", http.StatusNotFound},
				{"debug", "/debug/pprof/", "", http.StatusUnauthorized},
				{"debug", "/metrics", "admin", http.StatusNotFound},
				{"metrics", "/debug/pprof/", "", http.StatusNotFound},
			},
		},
		{
			name: "ip_listener", category: "positive",
			description: "the ip class serves /ip and /i (404 until a daemon attaches) and nothing else",
			ipPaths:     true,
			listeners: func(_ *testing.T, dir string) []*xtcp_config.Listener {
				return []*xtcp_config.Listener{
					unixListener(dir, "metrics", authOff, listeners.Health, listeners.Metrics),
					unixListener(dir, "ip", authOff, listeners.IP),
				}
			},
			requests: []req{
				{"ip", "/ip/link", "", http.StatusNotFound},
				{"ip", "/metrics", "", http.StatusNotFound},
				{"ip", "/healthz", "", http.StatusNotFound},
			},
		},
		{
			name: "root_prom_path", category: "corner",
			description: "a promPath of / serves metrics on every path and mounts no detail routes",
			promPath:    "/",
			listeners: func(_ *testing.T, dir string) []*xtcp_config.Listener {
				return []*xtcp_config.Listener{unixListener(dir, "metrics", authOff, listeners.Health, listeners.Metrics, listeners.MetricsDetail)}
			},
			requests: []req{{"metrics", "/anything", "", http.StatusOK}, {"metrics", "/healthz", "", http.StatusOK}},
		},
		{
			name: "grpc_returned", category: "boundary",
			description: "a gRPC listener is bound and handed to the daemon, not served over HTTP",
			listeners: func(_ *testing.T, dir string) []*xtcp_config.Listener {
				return []*xtcp_config.Listener{
					unixListener(dir, "metrics", authOff, listeners.Health),
					unixListener(dir, "grpc", authOff, listeners.GRPCRead),
				}
			},
			grpc: 1,
		},
		{
			name: "ip_prom_path_collision", category: "negative",
			description: "a promPath that is an ip route on the same listener is refused before binding",
			ipPaths:     true,
			promPath:    "/ip",
			listeners: func(_ *testing.T, dir string) []*xtcp_config.Listener {
				return []*xtcp_config.Listener{unixListener(dir, "metrics", authOff, listeners.Health, listeners.Metrics, listeners.IP)}
			},
			wantErr: "is an ip route",
		},
		{
			name: "second_bind_fails", category: "corner",
			description: "a port in use fails startup and the listener bound before it is closed",
			listeners: func(t *testing.T, dir string) []*xtcp_config.Listener {
				ln, err := net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = ln.Close() })
				return []*xtcp_config.Listener{
					unixListener(dir, "metrics", authOff, listeners.Health, listeners.Metrics),
					{Name: "tcp", Serves: []xtcp_config.EndpointClass{listeners.MetricsDetail}, Auth: authOff,
						Endpoint: &xtcp_config.ListenerEndpoint{Network: xtcp_config.ListenerNetwork_LISTENER_NETWORK_TCP, Address: ln.Addr().String()}},
				}
			},
			wantErr: `listener "tcp"`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Logf("%s: %s", tc.category, tc.description)
			dir, err := os.MkdirTemp("/tmp", "xtcp2-ls-") // short: sun_path is 108 bytes
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.RemoveAll(dir) })
			c := &xtcp_config.XtcpConfig{Listeners: tc.listeners(t, dir), LinkMonitor: &xtcp_config.LinkMonitorConfig{IpPaths: tc.ipPaths}}
			specs, _, err := listeners.Resolve(c, readSecretFile)
			if err != nil {
				t.Fatal(err)
			}
			promPath := tc.promPath
			if promPath == "" {
				promPath = "/metrics"
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var wg sync.WaitGroup
			grpcLns, err := startListeners(ctx, &wg, specs, promPath)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("%s: err = %v, want %q", tc.description, err, tc.wantErr)
				}
				waitReturns(t, &wg, time.Second)
				if _, err := os.Lstat(filepath.Join(dir, "metrics.sock")); !os.IsNotExist(err) {
					t.Errorf("%s: metrics.sock left behind (%v)", tc.description, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer closeGRPCListeners(grpcLns)
			if len(grpcLns) != tc.grpc {
				t.Errorf("%s: %d gRPC listeners returned, want %d", tc.description, len(grpcLns), tc.grpc)
			}
			for _, r := range tc.requests {
				c := unixClient(filepath.Join(dir, r.listener+".sock"))
				if got := getStatus(t, c, r.path, r.token); got != r.expected {
					t.Errorf("%s: %s GET %s (token %q) = %d, want %d", tc.description, r.listener, r.path, r.token, got, r.expected)
				}
			}
			cancel()
			waitReturns(t, &wg, httpShutdownGrace+time.Second)
		})
	}
}

// TestServeHTTP covers the serve worker's two exits: a serve failure while
// running, and a shutdown whose drain outlasts the grace.
func TestServeHTTP(t *testing.T) {
	tests := []struct {
		name, category, description, expectedOutcome string
		classes                                      []xtcp_config.EndpointClass
		closeFirst, stall                            bool
		wantFatal                                    string
	}{
		{"serve_failure", "negative", "the metrics listener fails while the daemon runs", "fatalf names the listener",
			[]xtcp_config.EndpointClass{listeners.Metrics}, true, false, `listener "l"`},
		{"serve_failure_ip", "corner", "losing an ip-only listener is logged, not fatal", "no fatal",
			[]xtcp_config.EndpointClass{listeners.IP}, true, false, ""},
		{"drain_past_grace", "boundary", "a request stalls past the shutdown grace", "server closed, worker returns, no fatal",
			[]xtcp_config.EndpointClass{listeners.Metrics}, false, true, ""},
		{"idle_shutdown", "positive", "shutdown with nothing in flight", "worker returns at once, no fatal",
			[]xtcp_config.EndpointClass{listeners.Metrics}, false, false, ""},
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
			srv := newHTTPServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
			failed := httpServeFailed(listeners.Spec{Name: "l", Classes: tc.classes})
			captureLog(t, func() {
				go serveHTTP(ctx, &wg, "l", srv, ln, 50*time.Millisecond, failed)
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
			})
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
