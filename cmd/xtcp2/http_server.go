package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

// httpShutdownGrace bounds the drain of in-flight requests at shutdown. It is
// under cancelSleepTimeCst, so the drain finishes before the signal handler
// gives up and exits.
const httpShutdownGrace = 3 * time.Second

// fatalf is the package-level abort handler. Defaults to log.Fatalf;
// tests swap this in for a capture so the serve error branch is
// exercisable without exiting.
var fatalf = log.Fatalf

func newHTTPServer(handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		// The link monitor's detail paths answer within their own 10s
		// timeout; the write deadline leaves room to send the body.
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  30 * time.Second,
	}
}

// serveHTTP serves ln until ctx ends, then drains in-flight requests for at
// most grace before closing what remains. A serve failure while ctx is live
// calls failed. name prefixes the shutdown logs.
func serveHTTP(ctx context.Context, wg *sync.WaitGroup, name string, srv *http.Server, ln net.Listener, grace time.Duration, failed func(error)) {
	defer wg.Done()
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	select {
	case err := <-served:
		failed(err)
		return
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), grace)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("%s shutdown: %v; closing", name, err)
		if err := srv.Close(); err != nil {
			log.Printf("%s close: %v", name, err)
		}
	}
	if err := <-served; !errors.Is(err, http.ErrServerClosed) {
		log.Printf("%s serve: %v", name, err)
	}
}
