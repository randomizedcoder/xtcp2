package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type commandMonitor interface {
	healthSource
	Run(context.Context) error
	RequestRebaseline() error
}

type listenerFactory func(context.Context, string, string) (net.Listener, error)

func serve(ctx context.Context, monitor commandMonitor, registry prometheus.Gatherer,
	logger *slog.Logger, signals <-chan os.Signal, listen listenerFactory, address string,
) error {
	listener, err := listen(ctx, "tcp", address)
	if err != nil {
		return err
	}
	var stopping atomic.Bool
	server := httpServer(monitor, registry, &stopping)
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	monitorDone := make(chan error, 1)
	httpDone := make(chan error, 1)
	go func() { monitorDone <- monitor.Run(runCtx) }()
	go func() { httpDone <- server.Serve(listener) }()
	logger.Info("serving cached link metrics", "address", listener.Addr().String())
	remainingMonitor, remainingHTTP, err := supervise(ctx, monitor, logger, signals, monitorDone, httpDone)
	stopping.Store(true)
	cancel()
	// Drain concurrently with the monitor's own bounded resource cleanup.
	shutdownErr := shutdownHTTP(ctx, server, httpShutdownGrace)
	if remainingMonitor != nil {
		if runErr := <-remainingMonitor; runErr != context.Canceled {
			err = errors.Join(err, runErr)
		}
	}
	if remainingHTTP != nil {
		if serveErr := <-remainingHTTP; !errors.Is(serveErr, http.ErrServerClosed) {
			err = errors.Join(err, serveErr)
		}
	}
	return errors.Join(err, shutdownErr)
}

func shutdownHTTP(ctx context.Context, server *http.Server, grace time.Duration) error {
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), grace)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return errors.Join(err, server.Close())
	}
	return nil
}

func supervise(ctx context.Context, monitor commandMonitor, logger *slog.Logger,
	signals <-chan os.Signal, monitorDone, httpDone chan error,
) (chan error, chan error, error) {
	for {
		select {
		case <-ctx.Done():
			return monitorDone, httpDone, nil
		case received, ok := <-signals:
			if !ok {
				signals = nil
				continue
			}
			if received != syscall.SIGUSR1 {
				return monitorDone, httpDone, nil
			}
			if err := monitor.RequestRebaseline(); err != nil {
				logger.Warn("rebaseline request rejected", "error", err)
			} else {
				logger.Info("rebaseline requested; durable completion is asynchronous")
			}
		case err := <-monitorDone:
			if ctx.Err() != nil && err == context.Canceled {
				err = nil
			}
			return nil, httpDone, err
		case err := <-httpDone:
			return monitorDone, nil, err
		}
	}
}
