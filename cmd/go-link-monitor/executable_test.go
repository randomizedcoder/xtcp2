package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The same process tests exercise either the test-linked command or the exact
// Nix output executable, in a clean environment without provider path overrides.
func executableCommand(ctx context.Context, t *testing.T, args ...string) *exec.Cmd {
	t.Helper()
	binary := "./go-link-monitor-artifact"
	if os.Getenv("LINKMONITOR_TEST_ARTIFACT") != "1" {
		var err error
		binary, err = os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		args = append([]string{"-test.run=^TestExecutableHelper$", "--"}, args...)
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = []string{"LINKMONITOR_EXEC_HELPER=1", "GOMAXPROCS=2"}
	return cmd
}

func TestExecutableHelper(t *testing.T) {
	if os.Getenv("LINKMONITOR_EXEC_HELPER") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			os.Exit(command(os.Args[i+1:], os.Stdout, os.Stderr, os.Getenv))
		}
	}
	t.Fatal("missing command arguments")
}

func TestExecutableErrors(t *testing.T) {
	corrupt := filepath.Join(t.TempDir(), "baseline.json")
	if err := os.WriteFile(corrupt, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	var lc net.ListenConfig
	occupied, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		args                                         []string
		code                                         int
		contains                                     string
	}{
		{"help", "positive", "help without runtime", "successful help", []string{"-help"}, 0, "max-speed-exceptions"},
		{"version", "positive", "build identity without runtime", "successful identity", []string{"-version"}, 0, "revision="},
		{"invalid", "negative", "invalid duration", "usage exit", []string{"-resync=0"}, 2, "invalid configuration"},
		{"backend", "negative", "explicit unavailable backend", "runtime failure without fallback", []string{"-io-backend=io_uring", "-metrics-addr=127.0.0.1:0"}, 1, "backend unavailable"},
		{"bind", "negative", "invalid listen address", "runtime failure before monitoring", []string{"-metrics-addr=127.0.0.1:99999"}, 1, "monitor stopped"},
		{"occupied", "corner", "address already bound", "runtime failure before acquiring baseline", []string{"-metrics-addr=" + occupied.Addr().String(), "-baseline-file=" + corrupt}, 1, "address already in use"},
		{"all occupied", "corner", "all-metrics address already bound", "runtime failure before acquiring baseline", []string{"-metrics-addr=127.0.0.1:0", "-metrics-all-addr=" + occupied.Addr().String(), "-baseline-file=" + corrupt}, 1, "address already in use"},
		{"invalid exposure", "negative", "malformed core include", "usage exit before listening", []string{"-metrics.core-include=["}, 2, "include regexp"},
		{"baseline", "negative", "corrupt baseline", "runtime failure preserves file", []string{"-metrics-addr=127.0.0.1:0", "-baseline-file=" + corrupt}, 1, "monitor stopped"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			output, err := executableCommand(ctx, t, tc.args...).CombinedOutput()
			code := 0
			if err != nil {
				var exit *exec.ExitError
				if !errors.As(err, &exit) {
					t.Fatal(err)
				}
				code = exit.ExitCode()
			}
			if code != tc.code || !strings.Contains(string(output), tc.contains) {
				t.Fatal(code, string(output))
			}
		})
	}
	contents, err := os.ReadFile(corrupt)
	if err != nil || string(contents) != "invalid" {
		t.Fatal("baseline changed", err)
	}
}

type executableProcess struct {
	cmd      *exec.Cmd
	logs     bytes.Buffer
	readDone chan struct{}
	address  chan string
	control  chan struct{}
}

func startExecutable(ctx context.Context, t *testing.T, baseline string, args ...string) *executableProcess {
	t.Helper()
	args = append([]string{"-metrics-addr=127.0.0.1:0", "-baseline-file=" + baseline, "-settle=0", "-collection.mode=poll", "-stats-interval=100ms"}, args...)
	p := &executableProcess{
		// One address is logged per listener.
		cmd:      executableCommand(ctx, t, args...),
		readDone: make(chan struct{}), address: make(chan string, 2), control: make(chan struct{}, 1),
	}
	stderr, err := p.cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := p.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		defer close(p.readDone)
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			p.logs.WriteString(line + "\n")
			if strings.Contains(line, "rebaseline requested;") || strings.Contains(line, "rebaseline request rejected") {
				p.control <- struct{}{}
			}
			if strings.Contains(line, "serving cached link metrics") {
				_, address, _ := strings.Cut(line, "address=")
				p.address <- strings.Trim(address, "\"")
			}
		}
	}()
	return p
}

func TestExecutableLifetime(t *testing.T) {
	t.Log("positive/corner: clean-environment live process; expected: cached host metrics, signal request, joined shutdown and restart")
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	baseline := filepath.Join(t.TempDir(), "baseline.json")
	for range 2 {
		p := startExecutable(ctx, t, baseline)
		var address string
		select {
		case address = <-p.address:
		case <-p.readDone:
			t.Fatal("startup failed", p.logs.String())
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		if err := waitExecutableMetrics(ctx, address); err != nil {
			t.Fatal(err)
		}
		if err := p.cmd.Process.Signal(syscall.SIGUSR1); err != nil {
			t.Fatal(err)
		}
		select {
		case <-p.control:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		if _, err := fetchPath(ctx, address, "/readyz"); err != nil {
			t.Fatal(err)
		}
		if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
			t.Fatal(err)
		}
		<-p.readDone
		if err := p.cmd.Wait(); err != nil {
			t.Fatal(err, p.logs.String())
		}
		logs := p.logs.String()
		// One build: RDMA events from netlink and PortInfo through the umad
		// device, with no cgo binding to report.
		for _, want := range []string{"rdma_events_backend=netlink", "rdma_umad_backend=native"} {
			if !strings.Contains(logs, want) {
				t.Fatal("missing build diagnostics", want, logs)
			}
		}
		if strings.Contains(logs, "rdma_bindings=") {
			t.Fatal("stale cgo binding diagnostic", logs)
		}
	}
}

func waitExecutableMetrics(ctx context.Context, address string) error {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		all, err := fetchPath(ctx, address, defaultAllPath)
		if err != nil {
			return err
		}
		if strings.Contains(all, "go_link_monitor_netstat_") && strings.Contains(all, "go_link_monitor_build_info") {
			// Bulk host detail is served only on the all-metrics path.
			core, err := fetchPath(ctx, address, metricsPath)
			if err != nil {
				return err
			}
			if strings.Contains(core, "go_link_monitor_netstat_") || !strings.Contains(core, "go_link_monitor_build_info") {
				return fmt.Errorf("core exposition is not the core set:\n%s", core)
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func fetchPath(ctx context.Context, address, path string) (string, error) {
	_, body, err := fetchStatus(ctx, address, path)
	return body, err
}

func freeAddress(t *testing.T) string {
	t.Helper()
	var lc net.ListenConfig
	listener, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}

func TestExecutableSeparateListener(t *testing.T) {
	t.Log("positive/corner: live process with -metrics-all-addr; expected: each listener serves only its metrics path plus health")
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	allAddress := freeAddress(t)
	p := startExecutable(ctx, t, filepath.Join(t.TempDir(), "baseline.json"), "-metrics-all-addr="+allAddress)
	var core string
	for range 2 {
		select {
		case address := <-p.address:
			if address != allAddress {
				core = address
			}
		case <-p.readDone:
			t.Fatal("startup failed", p.logs.String())
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	if core == "" {
		t.Fatal("core listener not logged", p.logs.String())
	}
	// Wait until host detail is cached so absence from /metrics is meaningful.
	for {
		_, body, err := fetchStatus(ctx, allAddress, defaultAllPath)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(body, "go_link_monitor_netstat_") {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(20 * time.Millisecond):
		}
	}
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		address, path                                string
		status                                       int
		contains, absent                             string
	}{
		{"core", "positive", "core listener /metrics", "core set without bulk detail", core, metricsPath, 200, "go_link_monitor_build_info", "go_link_monitor_netstat_"},
		{"all", "positive", "all listener all-path", "full set", allAddress, defaultAllPath, 200, "go_link_monitor_netstat_", ""},
		{"core health", "positive", "core listener /healthz", "alive", core, "/healthz", 200, "", ""},
		{"all health", "positive", "all listener /healthz", "alive", allAddress, "/healthz", 200, "", ""},
		{"core all-path", "negative", "all-path on the core listener", "not found", core, defaultAllPath, 404, "", ""},
		{"all /metrics", "corner", "/metrics on the all listener", "not found", allAddress, metricsPath, 404, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			status, body, err := fetchStatus(ctx, tc.address, tc.path)
			if err != nil || status != tc.status || !strings.Contains(body, tc.contains) || (tc.absent != "" && strings.Contains(body, tc.absent)) {
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
}

func fetchStatus(ctx context.Context, address, path string) (int, string, error) {
	request, err := http.NewRequestWithContext(ctx, "GET", "http://"+address+path, nil)
	if err != nil {
		return 0, "", err
	}
	client := &http.Client{Timeout: 3 * time.Second}
	defer client.CloseIdleConnections()
	response, err := client.Do(request)
	if err != nil {
		return 0, "", err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	return response.StatusCode, string(body), err
}
