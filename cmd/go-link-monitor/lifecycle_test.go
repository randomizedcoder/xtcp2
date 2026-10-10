package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
)

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestServeFailures(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		bind, start, cleanup                         bool
	}{
		{"bind", "negative", "listen fails", "monitor never starts", true, false, false},
		{"startup", "negative", "monitor cannot acquire resources", "listener closes and failure returns", false, true, false},
		{"cancel", "positive", "host cancels active lifetime", "monitor joined and listener closed", false, false, false},
		{"cleanup", "corner", "monitor cannot finish cleanup", "incomplete shutdown propagated", false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			var started atomic.Bool
			failure := errors.New("injected failure")
			m := &fakeMonitor{run: func(ctx context.Context) error {
				started.Store(true)
				if tc.start {
					return failure
				}
				cancel()
				<-ctx.Done()
				if tc.cleanup {
					return errors.Join(context.Canceled, linkmonitor.ErrShutdownIncomplete)
				}
				return nil
			}}
			var listener net.Listener
			listen := func(ctx context.Context, network, address string) (net.Listener, error) {
				if tc.bind {
					return nil, failure
				}
				var lc net.ListenConfig
				var err error
				listener, err = lc.Listen(ctx, network, address)
				return listener, err
			}
			err := serve(ctx, m, prometheus.NewRegistry(), quietLogger(), nil, listen, "127.0.0.1:0")
			wantFailure := tc.bind || tc.start || tc.cleanup
			if (err != nil) != wantFailure || started.Load() == tc.bind {
				t.Fatal(err, started.Load())
			}
			if tc.cleanup && !errors.Is(err, linkmonitor.ErrShutdownIncomplete) {
				t.Fatal(err)
			}
			if listener != nil {
				if _, err := listener.Accept(); !errors.Is(err, net.ErrClosed) {
					t.Fatal("listener remains open", err)
				}
			}
		})
	}
}

func TestSupervision(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		reject, httpFailure                          bool
	}{
		{"rebaseline", "positive", "repeated SIGUSR1 followed by SIGTERM", "requests forwarded then clean termination", false, false},
		{"rejected", "boundary", "SIGUSR1 before controls accept", "rejection is nonfatal", true, false},
		{"http", "negative", "server fails unexpectedly", "server error initiates termination", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			signals := make(chan os.Signal, 3)
			monitorDone, httpDone := make(chan error, 1), make(chan error, 1)
			calls := 0
			m := &fakeMonitor{rebaseline: func() error {
				calls++
				if tc.reject {
					return linkmonitor.ErrNotRunning
				}
				return nil
			}}
			failure := errors.New("accept failed")
			if tc.httpFailure {
				httpDone <- failure
			} else {
				signals <- syscall.SIGUSR1
				signals <- syscall.SIGUSR1
				signals <- syscall.SIGTERM
			}
			remainingMonitor, remainingHTTP, err := supervise(t.Context(), m, quietLogger(), signals, monitorDone, httpDone)
			if remainingMonitor != monitorDone || (remainingHTTP == nil) != tc.httpFailure || (tc.httpFailure && !errors.Is(err, failure)) || (!tc.httpFailure && (err != nil || calls != 2)) {
				t.Fatal(err, calls)
			}
		})
	}
}

func TestHTTPDrain(t *testing.T) {
	t.Log("corner: slow active scrape during shutdown; expected: readiness fails immediately and HTTP drains after release")
	var stopping atomic.Bool
	m := &fakeMonitor{}
	m.ready.Store(true)
	entered, release := make(chan struct{}), make(chan struct{})
	gatherer := gatherFunc(func() ([]*dto.MetricFamily, error) { close(entered); <-release; return nil, nil })
	server := httpServer(m, gatherer, &stopping)
	var lc net.ListenConfig
	listener, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	requestDone := make(chan error, 1)
	go func() {
		request, requestErr := http.NewRequestWithContext(t.Context(), "GET", "http://"+listener.Addr().String()+"/metrics", nil)
		if requestErr != nil {
			requestDone <- requestErr
			return
		}
		client := &http.Client{Timeout: 3 * time.Second}
		response, requestErr := client.Do(request)
		if requestErr == nil {
			requestErr = response.Body.Close()
		}
		requestDone <- requestErr
	}()
	<-entered
	stopping.Store(true)
	drained := make(chan error, 1)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	go func() { drained <- server.Shutdown(ctx) }()
	select {
	case err := <-drained:
		t.Fatal("did not wait for scrape", err)
	default:
	}
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), "GET", "/readyz", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Error("shutdown remained ready")
	}
	close(release)
	if err := <-requestDone; err != nil {
		t.Error(err)
	}
	if err := <-drained; err != nil {
		t.Error(err)
	}
	if err := <-served; !errors.Is(err, http.ErrServerClosed) {
		t.Error(err)
	}
}
