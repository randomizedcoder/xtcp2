package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/randomizedcoder/xtcp2/pkg/listener"
)

func secretFile(t *testing.T, value string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte(value+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAdminResolve(t *testing.T) {
	token := secretFile(t, "admin-secret")
	for _, tc := range []struct {
		name, category, description string
		args                        []string
		env                         map[string]string
		expected                    string // error substring; empty means success
		endpoint                    bool
		enforced                    bool
		mode                        os.FileMode
	}{
		{"disabled", "positive", "no admin flags", nil, nil, "", false, false, 0},
		{"unix", "positive", "unix socket with default mode", []string{"-admin.address=/run/x/admin.sock"}, nil, "", true, false, 0o660},
		{"octal", "positive", "explicit octal mode", []string{"-admin.address=/s", "-admin.unix-socket-mode=0600"}, nil, "", true, false, 0o600},
		{"loopback", "positive", "loopback TCP without credentials", []string{"-admin.network=tcp", "-admin.address=127.0.0.1:9102"}, nil, "", true, false, 0},
		{"localhost", "positive", "localhost TCP without credentials", []string{"-admin.network=tcp", "-admin.address=localhost:9102"}, nil, "", true, false, 0},
		{"ipv6 loopback", "positive", "IPv6 loopback TCP", []string{"-admin.network=tcp", "-admin.address=[::1]:9102"}, nil, "", true, false, 0},
		{"authenticated", "positive", "wildcard TCP with an admin credential", []string{"-admin.network=tcp", "-admin.address=:9102", "-auth.admin-mode=raw", "-auth.admin-token-file=" + token}, nil, "", true, true, 0},
		{"insecure", "corner", "wildcard TCP explicitly insecure", []string{"-admin.network=tcp", "-admin.address=:9102", "-admin.insecure"}, nil, "", true, false, 0},
		{"environment", "positive", "address and credential from the environment", nil,
			map[string]string{adminAddressEnvironment: "/s", adminModeEnvironment: "raw", adminTokenEnvironment: token}, "", true, true, 0o660},
		{"read only", "positive", "read credential without an admin listener protects metrics", []string{"-auth.read-mode=raw", "-auth.read-token-file=" + token}, nil, "", false, true, 0},
		{"wildcard", "negative", "wildcard TCP without a credential", []string{"-admin.network=tcp", "-admin.address=:9102"}, nil, "not loopback", false, false, 0},
		{"public", "negative", "public address without a credential", []string{"-admin.network=tcp", "-admin.address=192.0.2.1:9102"}, nil, "not loopback", false, false, 0},
		{"no port", "negative", "TCP address without a port is not loopback", []string{"-admin.network=tcp", "-admin.address=127.0.0.1"}, nil, "not loopback", false, false, 0},
		{"network", "negative", "unknown network", []string{"-admin.network=udp", "-admin.address=x"}, nil, "unix or tcp", false, false, 0},
		{"mode word", "negative", "non-octal socket mode", []string{"-admin.address=/s", "-admin.unix-socket-mode=rw"}, nil, "octal", false, false, 0},
		{"mode digit", "negative", "decimal digit in socket mode", []string{"-admin.address=/s", "-admin.unix-socket-mode=0668"}, nil, "octal", false, false, 0},
		{"mode high", "boundary", "socket mode above 0777", []string{"-admin.address=/s", "-admin.unix-socket-mode=1000"}, nil, "octal", false, false, 0},
		{"mode max", "boundary", "socket mode 0777", []string{"-admin.address=/s", "-admin.unix-socket-mode=0777"}, nil, "", true, false, 0o777},
		{"admin without address", "negative", "admin credential with no admin listener", []string{"-auth.admin-mode=raw", "-auth.admin-token-file=" + token}, nil, "requires admin.address", false, false, 0},
		{"file without mode", "negative", "secret file but no mode", []string{"-auth.read-token-file=" + token}, nil, "mode is required", false, false, 0},
		{"bad mode", "negative", "unknown auth mode", []string{"-auth.read-mode=kerberos"}, nil, "auth.read-mode", false, false, 0},
		{"missing file", "negative", "unreadable secret file", []string{"-auth.read-mode=raw", "-auth.read-token-file=/nonexistent/token"}, nil, "auth.read secret", false, false, 0},
		{"raw without token", "negative", "raw mode with no token", []string{"-auth.read-mode=raw"}, nil, "token", false, false, 0},
		{"flag over env", "corner", "explicit empty address overrides the environment", []string{"-admin.address="}, map[string]string{adminAddressEnvironment: "/s"}, "", false, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %q", tc.category, tc.description, tc.expected)
			opts, err := parseOptions(tc.args, &strings.Builder{}, environment(tc.env))
			if err != nil {
				t.Fatal(err)
			}
			got, err := opts.admin.resolve()
			if tc.expected != "" {
				if err == nil || !strings.Contains(err.Error(), tc.expected) {
					t.Fatalf("err = %v, want %q", err, tc.expected)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if (got.endpoint != nil) != tc.endpoint || got.policy.Enabled() != tc.enforced {
				t.Fatalf("endpoint = %+v, enforced = %v", got.endpoint, got.policy.Enabled())
			}
			if got.endpoint != nil && got.endpoint.Network == listener.NetworkUnix {
				if got.endpoint.UnixSocketMode != tc.mode || !got.endpoint.UnlinkStaleUnixSocket {
					t.Fatalf("endpoint = %+v", got.endpoint)
				}
			}
		})
	}
}

func TestProtectRoutes(t *testing.T) {
	token := secretFile(t, "read-secret")
	opts, err := parseOptions([]string{"-auth.read-mode=raw", "-auth.read-token-file=" + token}, &strings.Builder{}, environment(nil))
	if err != nil {
		t.Fatal(err)
	}
	access, err := opts.admin.resolve()
	if err != nil {
		t.Fatal(err)
	}
	served := protectRoutes(endpoints(":0", exposureOptions{allPath: defaultAllPath}, prometheus.NewRegistry(), prometheus.NewRegistry(), nil, nil), access.policy)
	var stopping atomic.Bool
	handler := httpServer(&fakeMonitor{}, &stopping, served[0].routes...).Handler
	for _, tc := range []struct {
		name, category, description string
		path, authorization         string
		expected                    int
	}{
		{"core", "positive", "core metrics with the read token", metricsPath, "Bearer read-secret", http.StatusOK},
		{"all", "positive", "all metrics with the read token", defaultAllPath, "Bearer read-secret", http.StatusOK},
		{"health", "positive", "liveness stays open", "/healthz", "", http.StatusOK},
		{"missing", "negative", "core metrics without a token", metricsPath, "", http.StatusUnauthorized},
		{"wrong", "negative", "all metrics with a wrong token", defaultAllPath, "Bearer nope", http.StatusUnauthorized},
		{"ready", "corner", "readiness stays open and reports not ready", "/readyz", "", http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %d", tc.category, tc.description, tc.expected)
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tc.path, nil)
			if tc.authorization != "" {
				request.Header.Set("Authorization", tc.authorization)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != tc.expected {
				t.Fatalf("status = %d", recorder.Code)
			}
		})
	}
	t.Run("unenforced", func(t *testing.T) {
		t.Log("corner: no credential configured; expected: routes unchanged")
		plain, err := adminOptions{}.resolve()
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range protectRoutes(singleEndpoint(":0"), plain.policy)[0].routes {
			if r.protect != nil {
				t.Fatal("route wrapped without a credential")
			}
		}
	})
}

// fakeGRPC serves until stopped. GracefulStop blocks for gracefulDelay to
// model calls that outlive the grace period.
type fakeGRPC struct {
	serveErr      error
	gracefulDelay time.Duration
	once          sync.Once
	stop          chan struct{}
	graceful      atomic.Bool
	forced        atomic.Bool
}

func newFakeGRPC() *fakeGRPC { return &fakeGRPC{stop: make(chan struct{})} }

func (f *fakeGRPC) Serve(l net.Listener) error {
	defer l.Close()
	if f.serveErr != nil {
		return f.serveErr
	}
	<-f.stop
	return nil
}

func (f *fakeGRPC) GracefulStop() {
	f.graceful.Store(true)
	select {
	case <-time.After(f.gracefulDelay):
	case <-f.stop:
	}
	f.once.Do(func() { close(f.stop) })
}

func (f *fakeGRPC) Stop() {
	f.forced.Store(true)
	f.once.Do(func() { close(f.stop) })
}

func TestServeAdmin(t *testing.T) {
	failure := errors.New("injected failure")
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		bindFails, serveFails                        bool
		expected                                     error
		started                                      bool
	}{
		{"cancel", "positive", "host cancels with an admin listener", "gRPC drained gracefully, monitor ran", false, false, nil, true},
		{"bind", "negative", "admin listener cannot bind", "HTTP listener closed and monitor never starts", true, false, failure, false},
		{"serve", "negative", "gRPC server exits after start", "process stops with its error", false, true, failure, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			var started atomic.Bool
			m := &fakeMonitor{run: func(runCtx context.Context) error {
				started.Store(true)
				if !tc.serveFails {
					cancel()
				}
				<-runCtx.Done()
				return nil
			}}
			var httpListener net.Listener
			listen := func(ctx context.Context, network, address string) (net.Listener, error) {
				var lc net.ListenConfig
				var err error
				httpListener, err = lc.Listen(ctx, network, address)
				return httpListener, err
			}
			server := newFakeGRPC()
			if tc.serveFails {
				server.serveErr = failure
			}
			admin := &adminEndpoint{name: "test", server: server, listen: func(ctx context.Context) (net.Listener, error) {
				if tc.bindFails {
					return nil, failure
				}
				var lc net.ListenConfig
				return lc.Listen(ctx, "tcp", "127.0.0.1:0")
			}}
			err := serve(ctx, m, quietLogger(), nil, listen, singleEndpoint("127.0.0.1:0"), admin)
			if !errors.Is(err, tc.expected) || (tc.expected == nil && err != nil) || started.Load() != tc.started {
				t.Fatal(err, started.Load())
			}
			if _, err := httpListener.Accept(); !errors.Is(err, net.ErrClosed) {
				t.Fatal("HTTP listener remains open", err)
			}
			if !tc.bindFails && (!server.graceful.Load() || server.forced.Load()) {
				t.Fatalf("graceful = %v, forced = %v", server.graceful.Load(), server.forced.Load())
			}
		})
	}
}

func TestShutdownGRPC(t *testing.T) {
	for _, tc := range []struct {
		name, category, description string
		delay, grace                time.Duration
		expected                    error
		forced                      bool
	}{
		{"graceful", "positive", "calls finish inside the grace period", 0, time.Second, nil, false},
		{"forced", "negative", "calls outlive the grace period", time.Minute, 20 * time.Millisecond, errGRPCGrace, true},
		{"zero grace", "boundary", "zero grace forces a stop", time.Minute, 0, errGRPCGrace, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %v forced=%v", tc.category, tc.description, tc.expected, tc.forced)
			server := newFakeGRPC()
			server.gracefulDelay = tc.delay
			if err := shutdownGRPC(server, tc.grace); !errors.Is(err, tc.expected) {
				t.Fatal(err)
			}
			if server.forced.Load() != tc.forced {
				t.Fatalf("forced = %v", server.forced.Load())
			}
		})
	}
}

func TestCtlUsage(t *testing.T) {
	for _, tc := range []struct {
		name, category, description string
		args                        []string
		env                         map[string]string
		expected                    int
		contains                    string
	}{
		{"help", "positive", "ctl help lists commands and flags", []string{"-h"}, nil, 0, "-auth.admin-token-file"},
		{"no command", "negative", "ctl without a command", []string{"-address=/nonexistent.sock"}, nil, 2, "Commands:"},
		{"network", "negative", "unknown network", []string{"-network=udp", "status"}, nil, 2, "unix or tcp"},
		{"address", "negative", "empty address", []string{"-address=", "status"}, nil, 2, "must not be empty"},
		{"timeout", "boundary", "zero timeout", []string{"-timeout=0", "status"}, nil, 2, "must be positive"},
		{"secret", "negative", "missing admin token file from the environment", []string{"status"},
			map[string]string{adminTokenEnvironment: "/nonexistent/token"}, 2, "admin credential"},
		{"flag", "negative", "unknown ctl flag", []string{"-bogus"}, nil, 2, "flag provided but not defined"},
		{"unavailable", "corner", "no daemon on the socket", []string{"-address=" + filepath.Join(t.TempDir(), "none.sock"), "-timeout=5s", "status"}, nil, 1, "Unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: exit %d", tc.category, tc.description, tc.expected)
			var stdout, stderr strings.Builder
			if got := command(append([]string{ctlCommand}, tc.args...), &stdout, &stderr, environment(tc.env)); got != tc.expected {
				t.Fatalf("exit = %d\nstdout: %s\nstderr: %s", got, stdout.String(), stderr.String())
			}
			if !strings.Contains(stdout.String()+stderr.String(), tc.contains) {
				t.Fatalf("output missing %q:\n%s%s", tc.contains, stdout.String(), stderr.String())
			}
		})
	}
}

// with returns a new slice so table rows never share a backing array.
func with(base []string, args ...string) []string {
	return append(append([]string(nil), base...), args...)
}

func runCtl(ctx context.Context, t *testing.T, args ...string) (int, string) {
	t.Helper()
	output, err := executableCommand(ctx, t, append([]string{ctlCommand}, args...)...).CombinedOutput()
	if err == nil {
		return 0, string(output)
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatal(err)
	}
	return exit.ExitCode(), string(output)
}

func TestExecutableAdmin(t *testing.T) {
	t.Log("positive/negative: live process with an admin socket and both credentials; expected: ctl reads, rebaselines and resyncs, read token denied admin, metrics need the read token")
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	dir := t.TempDir()
	socket := filepath.Join(dir, "admin.sock")
	readToken, adminToken := secretFile(t, "read-secret"), secretFile(t, "admin-secret")
	p := startExecutable(ctx, t, filepath.Join(dir, "baseline.json"), "-admin.address="+socket, "-admin.unix-socket-mode=0600",
		"-auth.read-mode=raw", "-auth.read-token-file="+readToken, "-auth.admin-mode=raw", "-auth.admin-token-file="+adminToken)
	var address string
	select {
	case address = <-p.address:
	case <-p.readDone:
		t.Fatal("startup failed", p.logs.String())
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	base := []string{"-address=" + socket, "-timeout=10s"}
	read := with(base, "-auth.read-token-file="+readToken)
	both := with(read, "-auth.admin-token-file="+adminToken)
	// Wait for the control loop. Waiting rows need more than that: a resync
	// needs readable sysfs (absent in the Nix build sandbox) and a durable
	// baseline needs healthy collection (absent without RDMA event coverage).
	// Those rows skip where the host cannot provide them; the library tests
	// cover completion unconditionally.
	needs := map[string]bool{}
	for {
		code, out := runCtl(ctx, t, with(read, "status")...)
		if code == 0 && strings.Contains(out, "running=true") {
			// The first reconciliation takes milliseconds on a host with
			// sysfs; allow a bounded window before treating it as absent.
			for deadline := time.Now().Add(3 * time.Second); strings.Contains(out, "last_resync: unknown") && time.Now().Before(deadline); {
				time.Sleep(50 * time.Millisecond)
				_, out = runCtl(ctx, t, with(read, "status")...)
			}
			needs["resync"] = !strings.Contains(out, "last_resync: unknown")
			needs["healthy"] = strings.Contains(out, "collection_healthy=true")
			t.Logf("host provides resync=%v healthy=%v", needs["resync"], needs["healthy"])
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("never reconciled", code, out, p.logs.String())
		case <-time.After(50 * time.Millisecond):
		}
	}
	info, err := os.Stat(socket)
	if err != nil || info.Mode().Perm() != 0o600 || info.Mode()&os.ModeSocket == 0 {
		t.Fatal("socket mode", info, err)
	}
	for _, tc := range []struct {
		name, category, description string
		args                        []string
		expected                    int
		contains                    string
		needs                       string // "", "resync" or "healthy"
	}{
		{"status", "positive", "status with the read token", with(read, "status"), 0, "snapshot:", ""},
		{"interfaces", "positive", "interfaces as JSON", with(read, "interfaces", "-format", "json"), 0, "{", ""},
		{"collectors", "positive", "collectors include host rows", with(read, "collectors"), 0, "(host)", ""},
		{"rebaseline", "positive", "admin rebaseline is accepted", with(both, "rebaseline", "-reason", "executable test"), 0, "accepted", ""},
		{"rebaseline wait", "positive", "admin rebaseline waits for the durable save", with(both, "rebaseline", "-reason", "executable test", "-wait", "20s"), 0, "completed; expected=", "healthy"},
		{"resync", "positive", "admin resync waits for completion", with(both, "resync", "-reason", "executable test", "-wait", "20s"), 0, "completed", "resync"},
		{"read admin", "negative", "read token on an admin command", with(read, "rebaseline", "-reason", "x"), 1, "PermissionDenied", ""},
		{"no token", "negative", "no token on a read command", with(base, "status"), 1, "Unauthenticated", ""},
		{"admin reads", "corner", "admin token alone reaches read commands", with(base, "-auth.admin-token-file="+adminToken, "status"), 0, "snapshot:", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: exit %d with %q", tc.category, tc.description, tc.expected, tc.contains)
			if tc.needs != "" && !needs[tc.needs] {
				t.Skipf("host cannot provide %q (no sysfs, or no RDMA event coverage)", tc.needs)
			}
			if code, out := runCtl(ctx, t, tc.args...); code != tc.expected || !strings.Contains(out, tc.contains) {
				t.Fatal(code, out)
			}
		})
	}
	for _, tc := range []struct {
		name, category, description, path string
		expected                          int
	}{
		{"metrics", "negative", "metrics without the read token", metricsPath, http.StatusUnauthorized},
		{"health", "positive", "health stays open", "/healthz", http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %d", tc.category, tc.description, tc.expected)
			if status, body, err := fetchStatus(ctx, address, tc.path); err != nil || status != tc.expected {
				t.Fatal(status, err, body)
			}
		})
	}
	if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	<-p.readDone
	if err := p.cmd.Wait(); err != nil {
		t.Fatal(err, p.logs.String())
	}
	logs := p.logs.String()
	for _, want := range []string{"serving link monitor admin API", "link monitor admin request", "reason=\"executable test\"", "peer=\"uid="} {
		if !strings.Contains(logs, want) {
			t.Errorf("logs missing %q", want)
		}
	}
	if _, err := os.Stat(socket); !errors.Is(err, os.ErrNotExist) {
		t.Error("socket left behind", err)
	}
}
