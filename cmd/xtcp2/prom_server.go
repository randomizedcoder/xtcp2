package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/pprof" // registered explicitly on the prom mux in promHandler (see there)
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
	"github.com/randomizedcoder/xtcp2/pkg/health"
	"github.com/randomizedcoder/xtcp2/pkg/listener"
	"github.com/randomizedcoder/xtcp2/pkg/listenerauth"
)

// promShutdownGrace bounds the drain of in-flight scrapes at shutdown. It is
// under cancelSleepTimeCst, so the drain finishes before the signal handler
// gives up and exits.
const promShutdownGrace = 3 * time.Second

// promParams configures the Prometheus, health and pprof listener.
type promParams struct {
	path                  string
	listener              *xtcp_config.ListenerEndpoint
	listen                string
	ipv4TTL, ipv6HopLimit uint32
	auth, adminAuth       *xtcp_config.ListenerAuth
}

// promHandlerStarter is the indirection point for the prom server. Default
// starts the real server; tests swap it for a no-op to skip the port-bind.
var promHandlerStarter = startPromServer

// fatalf is the package-level abort handler. Defaults to log.Fatalf;
// tests swap this in for a capture so the serve error branch is
// exercisable without exiting.
var fatalf = log.Fatalf

// startPromServer binds the prom listener now, so a bad address or a port in
// use fails startup before the daemon runs, then serves it on a worker that
// joins wg and drains when ctx ends.
func startPromServer(ctx context.Context, wg *sync.WaitGroup, p promParams) error {
	handler, err := promHandler(p)
	if err != nil {
		return err
	}
	ep, err := listener.EndpointFromProto(p.listener, listener.NetworkTCP, p.listen)
	if err != nil {
		return fmt.Errorf("prometheus listener config: %w", err)
	}
	ln, err := listener.Listen(ctx, ep, p.ipv4TTL, p.ipv6HopLimit)
	if err != nil {
		return fmt.Errorf("prometheus error, listen: %w", err)
	}
	wg.Add(1)
	go servePromHandler(ctx, wg, newPromServer(handler), ln, promShutdownGrace)
	return nil
}

// promHandler builds the prom listener's handler behind the listener auth
// policy.
func promHandler(p promParams) (http.Handler, error) {
	// A dedicated mux (not http.DefaultServeMux) for this listener. This is what
	// lets pprof be registered EXPLICITLY below without the blank
	// `_ "net/http/pprof"` side-effect import (which trips gosec G108): that
	// import's init() registers /debug/pprof/* on DefaultServeMux, so serving
	// our own mux both keeps DefaultServeMux out of the request path and avoids
	// a double-registration panic.
	mux := http.NewServeMux()
	mux.Handle(p.path, promhttp.HandlerFor(
		prometheus.DefaultGatherer,
		promhttp.HandlerOpts{
			EnableOpenMetrics:   promEnableOpenMetrics,
			MaxRequestsInFlight: promMaxRequestsInFlight,
		},
	))
	// Liveness + readiness for container/k8s deployment, on the same listener.
	mux.HandleFunc("/healthz", health.Healthz)
	mux.HandleFunc("/readyz", health.Readyz)
	// pprof on the prom listener for on-demand forensic stack snapshots.
	// pprof.Index also serves the named runtime profiles
	// (/debug/pprof/goroutine, /heap, …).
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	// Every other path is the embedded link monitor's, once the daemon has
	// attached it; the patterns above are more specific and win. A promPath
	// of "/" already serves every path.
	if p.path != "/" {
		mux.Handle("/", &linkMonitorHTTP)
	}
	// pprof exposes command lines, heap contents and CPU profiles, so it is
	// an admin path; metrics and health checks are read paths.
	policy, err := listenerauth.NewPolicy(p.auth, p.adminAuth, nil)
	if err != nil {
		return nil, fmt.Errorf("listener auth config invalid: %w", err)
	}
	return policy.WrapHTTP(mux, listenerauth.PathAccess("/debug/pprof/")), nil
}

func newPromServer(handler http.Handler) *http.Server {
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

// servePromHandler serves ln until ctx ends, then drains in-flight requests
// for at most grace before closing what remains. A serve failure while ctx is
// live invokes fatalf: without the listener there are no health checks, so
// the daemon must not keep running unobserved.
func servePromHandler(ctx context.Context, wg *sync.WaitGroup, srv *http.Server, ln net.Listener, grace time.Duration) {
	serveHTTP(ctx, wg, "prometheus", srv, ln, grace, func(err error) { fatalf("prometheus error: %v", err) })
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
