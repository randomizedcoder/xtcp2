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
)

type commandMonitor interface {
	healthSource
	Run(context.Context) error
	RequestRebaseline() error
}

type listenerFactory func(context.Context, string, string) (net.Listener, error)

func serve(ctx context.Context, monitor commandMonitor, logger *slog.Logger,
	signals <-chan os.Signal, listen listenerFactory, endpoints []endpoint,
) error {
	// Bind every listener before the monitor acquires anything.
	listeners := make([]net.Listener, 0, len(endpoints))
	for _, e := range endpoints {
		listener, err := listen(ctx, "tcp", e.address)
		if err != nil {
			for _, open := range listeners {
				err = errors.Join(err, open.Close())
			}
			return err
		}
		listeners = append(listeners, listener)
	}
	var stopping atomic.Bool
	servers := make([]*http.Server, len(endpoints))
	for i, e := range endpoints {
		servers[i] = httpServer(monitor, &stopping, e.routes...)
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	monitorDone := make(chan error, 1)
	httpDone := make(chan error, len(servers))
	go func() { monitorDone <- monitor.Run(runCtx) }()
	for i, server := range servers {
		go func() { httpDone <- server.Serve(listeners[i]) }()
		paths := make([]string, len(endpoints[i].routes))
		for j, r := range endpoints[i].routes {
			paths[j] = r.path
		}
		logger.Info("serving cached link metrics", "paths", paths, "address", listeners[i].Addr().String())
	}
	remainingMonitor, remainingHTTP, err := supervise(ctx, monitor, logger, signals, monitorDone, httpDone)
	stopping.Store(true)
	cancel()
	// Drain concurrently with the monitor's own bounded resource cleanup.
	shutdownErr := shutdownServers(ctx, servers, httpShutdownGrace)
	if remainingMonitor != nil {
		if runErr := <-remainingMonitor; runErr != context.Canceled {
			err = errors.Join(err, runErr)
		}
	}
	// supervise consumed one result when a server failed first.
	pending := len(servers)
	if remainingHTTP == nil {
		pending--
	}
	for range pending {
		if serveErr := <-httpDone; !errors.Is(serveErr, http.ErrServerClosed) {
			err = errors.Join(err, serveErr)
		}
	}
	return errors.Join(err, shutdownErr)
}

// shutdownServers drains every server concurrently within one grace period.
func shutdownServers(ctx context.Context, servers []*http.Server, grace time.Duration) error {
	results := make(chan error, len(servers))
	for _, server := range servers {
		go func() { results <- shutdownHTTP(ctx, server, grace) }()
	}
	var err error
	for range servers {
		err = errors.Join(err, <-results)
	}
	return err
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
