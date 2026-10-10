package linkmonitor_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	client "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
	monitorprom "github.com/randomizedcoder/xtcp2/pkg/linkmonitor/prometheus"
	"golang.org/x/sys/unix"
)

func TestEmbeddingHostProcess(t *testing.T) {
	t.Log("positive/corner: isolated host owns HTTP, signals, logger and registry; expected: monitor cleanup leaves host operational and globals unchanged")
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "-test.run=^TestEmbeddingHostHelper$", "-test.v")
	cmd.Env = append(os.Environ(), "LINKMONITOR_EMBEDDING_HELPER=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("host: %v\n%s", err, output)
	}
}

type embeddingWriter struct {
	mu    sync.Mutex
	bytes int
}

func (w *embeddingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.bytes += len(p)
	return len(p), nil
}

func TestEmbeddingHostHelper(t *testing.T) {
	if os.Getenv("LINKMONITOR_EMBEDDING_HELPER") != "1" {
		return
	}
	globalLogger, globalMux := slog.Default(), http.DefaultServeMux
	procs := runtime.GOMAXPROCS(0)
	globalNames := embeddingGlobalNames(t)
	defaultRoutes := embeddingDefaultRoutes()
	listeners := embeddingListeners(t)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGUSR1)
	defer signal.Stop(signals)
	w := new(embeddingWriter)
	cfg := linkmonitor.DefaultConfig()
	cfg.BaselineFile = filepath.Join(t.TempDir(), "baseline.json")
	cfg.Settle, cfg.StatsInterval = 0, 50*time.Millisecond
	m, err := linkmonitor.New(cfg, linkmonitor.Options{Logger: slog.New(slog.NewTextHandler(w, nil))})
	if err != nil {
		t.Fatal(err)
	}
	r := monitorRegistry(t, m)
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(r, promhttp.HandlerOpts{ErrorHandling: promhttp.HTTPErrorOnError}))
	// Verify collection itself opens no TCP listener, before the host starts HTTP.
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- m.Run(ctx) }()
	embeddingHostReady(t, m, done)
	embeddingHostSignal(t, signals)
	if got := embeddingListeners(t); !reflect.DeepEqual(got, listeners) {
		t.Fatal("library opened listener", got, listeners)
	}
	host := httptest.NewServer(mux)
	defer host.Close()
	embeddingRequest(t, host)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("monitor did not join shutdown")
	}
	if m.Health().Running {
		t.Fatal("monitor still running")
	}
	embeddingRequest(t, host)
	embeddingHostSignal(t, signals)
	w.mu.Lock()
	count := w.bytes
	w.mu.Unlock()
	if count == 0 {
		t.Fatal("provided logger received no messages")
	}
	if slog.Default() != globalLogger || http.DefaultServeMux != globalMux || runtime.GOMAXPROCS(0) != procs {
		t.Fatal("library changed host globals")
	}
	if !reflect.DeepEqual(embeddingGlobalNames(t), globalNames) {
		t.Fatal("library registered process-global metrics")
	}
	if !reflect.DeepEqual(embeddingDefaultRoutes(), defaultRoutes) {
		t.Fatal("library changed default HTTP routes")
	}
}

func embeddingHostReady(t *testing.T, m *linkmonitor.Monitor, done <-chan error) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(10 * time.Second)
	defer timeout.Stop()
	for {
		found := false
		m.Snapshot().RangeSamples(func(s linkmonitor.SampleView) bool { found = true; return false })
		if found {
			return
		}
		select {
		case err := <-done:
			t.Fatal("early monitor exit", err)
		case <-timeout.C:
			t.Fatal("host samples not published")
		case <-ticker.C:
		}
	}
}

func embeddingRequest(t *testing.T, host *httptest.Server) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, host.URL+"/metrics", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := host.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatal("host scrape failed", response.Status)
	}
}

func embeddingHostSignal(t *testing.T, signals <-chan os.Signal) {
	t.Helper()
	if err := syscall.Kill(os.Getpid(), syscall.SIGUSR1); err != nil {
		t.Fatal(err)
	}
	select {
	case <-signals:
	case <-time.After(time.Second):
		t.Fatal("host signal handler lost")
	}
}

func embeddingDefaultRoutes() []string {
	paths := []string{"/metrics", "/healthz", "/readyz", "/debug/pprof/"}
	routes := make([]string, 0, len(paths))
	for _, path := range paths {
		_, pattern := http.DefaultServeMux.Handler(httptest.NewRequest(http.MethodGet, path, nil))
		routes = append(routes, pattern)
	}
	return routes
}

func embeddingGlobalNames(t *testing.T) []string {
	t.Helper()
	families, err := client.DefaultGatherer.Gather()
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(families))
	for _, family := range families {
		names = append(names, family.GetName())
	}
	return names
}

func embeddingListeners(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	var listeners []string
	for _, entry := range entries {
		fd, err := strconv.Atoi(entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		listening, err := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_ACCEPTCONN)
		if err != nil || listening == 0 {
			continue
		}
		listeners = append(listeners, fmt.Sprint(fd))
	}
	return listeners
}

func TestEmbeddingFixedCollision(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		secondMonitor                                bool
	}{
		{"host metric", "negative", "host uses fixed monitor family", "Gather error", false},
		{"second monitor", "corner", "second monitor shares registry without isolation", "Gather rejects duplicate policy series", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			m, err := linkmonitor.New(linkmonitor.DefaultConfig(), linkmonitor.Options{})
			if err != nil {
				t.Fatal(err)
			}
			r := monitorRegistry(t, m)
			var c client.Collector = client.NewGauge(client.GaugeOpts{Name: "go_link_monitor_collection_healthy", Help: "Host health."})
			if tc.secondMonitor {
				other, err := linkmonitor.New(linkmonitor.DefaultConfig(), linkmonitor.Options{})
				if err != nil {
					t.Fatal(err)
				}
				c = monitorprom.NewCollector(other)
			}
			if err := r.Register(c); err != nil {
				t.Fatal(err)
			}
			if _, err := r.Gather(); err == nil {
				t.Fatal("collision accepted")
			}
		})
	}
}
