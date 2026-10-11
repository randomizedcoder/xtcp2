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
	signals <-chan os.Signal, listen listenerFactory, endpoints []endpoint, admins ...*adminEndpoint,
) error {
	// Bind every listener before the monitor acquires anything.
	listeners := make([]net.Listener, 0, len(endpoints)+len(admins))
	bind := func(open func() (net.Listener, error)) error {
		l, err := open()
		if err != nil {
			for _, bound := range listeners {
				err = errors.Join(err, bound.Close())
			}
			return err
		}
		listeners = append(listeners, l)
		return nil
	}
	for _, e := range endpoints {
		if err := bind(func() (net.Listener, error) { return listen(ctx, "tcp", e.address) }); err != nil {
			return err
		}
	}
	for _, a := range admins {
		if err := bind(func() (net.Listener, error) { return a.listen(ctx) }); err != nil {
			return err
		}
	}
	adminListeners := listeners[len(endpoints):]
	var stopping atomic.Bool
	servers := make([]*http.Server, len(endpoints))
	for i, e := range endpoints {
		servers[i] = httpServer(monitor, &stopping, e.routes...)
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	monitorDone := make(chan error, 1)
	// HTTP and gRPC servers report on one channel; either kind exiting stops
	// the process.
	httpDone := make(chan error, len(servers)+len(admins))
	go func() { monitorDone <- monitor.Run(runCtx) }()
	for i, server := range servers {
		go func() { httpDone <- server.Serve(listeners[i]) }()
		paths := make([]string, len(endpoints[i].routes))
		for j, r := range endpoints[i].routes {
			paths[j] = r.path
		}
		logger.Info("serving cached link metrics", "paths", paths, "address", listeners[i].Addr().String())
	}
	for i, a := range admins {
		go func() { httpDone <- a.server.Serve(adminListeners[i]) }()
		logger.Info("serving link monitor admin API", "address", a.name)
	}
	remainingMonitor, remainingHTTP, err := supervise(ctx, monitor, logger, signals, monitorDone, httpDone)
	stopping.Store(true)
	cancel()
	// Drain concurrently with the monitor's own bounded resource cleanup.
	shutdownErr := shutdownServers(ctx, servers, admins, httpShutdownGrace)
	if remainingMonitor != nil {
		if runErr := <-remainingMonitor; runErr != context.Canceled {
			err = errors.Join(err, runErr)
		}
	}
	// supervise consumed one result when a server failed first.
	pending := len(servers) + len(admins)
	if remainingHTTP == nil {
		pending--
	}
	for range pending {
		// A stopped gRPC server returns nil; a shut-down HTTP server returns
		// ErrServerClosed.
		if serveErr := <-httpDone; serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			err = errors.Join(err, serveErr)
		}
	}
	return errors.Join(err, shutdownErr)
}

// shutdownServers drains every server concurrently within one grace period.
func shutdownServers(ctx context.Context, servers []*http.Server, admins []*adminEndpoint, grace time.Duration) error {
	results := make(chan error, len(servers)+len(admins))
	for _, server := range servers {
		go func() { results <- shutdownHTTP(ctx, server, grace) }()
	}
	for _, a := range admins {
		go func() { results <- shutdownGRPC(a.server, grace) }()
	}
	var err error
	for range len(servers) + len(admins) {
		err = errors.Join(err, <-results)
	}
	return err
}

// errGRPCGrace reports a gRPC server whose calls outlived the grace period
// and were cut off by Stop.
var errGRPCGrace = errors.New("admin gRPC calls exceeded the shutdown grace period")

func shutdownGRPC(server grpcServer, grace time.Duration) error {
	done := make(chan struct{})
	go func() {
		server.GracefulStop()
		close(done)
	}()
	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case <-done:
		return nil
	case <-timer.C:
		server.Stop()
		<-done
		return errGRPCGrace
	}
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
