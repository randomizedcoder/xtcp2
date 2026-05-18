package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/pkg/profile"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/randomizedcoder/xtcp2/pkg/misc"
	"github.com/randomizedcoder/xtcp2/pkg/xtcp"
)

// "github.com/randomizedcoder/xtcp2/pkg/ns"

const (
	debugLevelCst = 11

	signalChannelSizeCst = 10
	cancelSleepTimeCst   = 5 * time.Second

	promListenCst           = ":9010" // [::1]:9010
	promPathCst             = "/metrics"
	promMaxRequestsInFlight = 10
	promEnableOpenMetrics   = true
	// curl -s http://localhost:9010/metrics 2>&1 | grep -v '#'
)

var (
	// Passed by "go build -ldflags" for the show version
	commit  string
	date    string
	version string

	debugLevel uint

	// fatalf is the package-level abort handler. Defaults to log.Fatalf
	// (which prints + os.Exit(1)). Tests swap this in for a capture so
	// the error branches of initPromHandler can be exercised without
	// terminating the test process.
	fatalf = log.Fatalf

	// daemonRunner builds + runs the underlying xtcp.NewNsTestingXTCP
	// instance. The default impl wires up production behavior; tests
	// replace it with a stub that returns without touching real netlink
	// or /run/netns. Injected as a var (not a parameter) so the cmd's
	// existing main() stays a 1-liner.
	daemonRunner = runDaemonDefault

	// promHandlerStarter wraps go-routine launch of the Prometheus HTTP
	// handler. Tests swap it for a no-op so runMain doesn't try to bind
	// a real port.
	promHandlerStarter = func(promPath, promListen string) {
		go initPromHandler(promPath, promListen)
	}
)

func main() {
	misc.DieIfNotLinux()
	os.Exit(runMain(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

// nsFlags is the parsed flag set used by runMain.
type nsFlags struct {
	promListen   string
	promPath     string
	profileMode  string
	v            bool
	d            uint
	enablePprof  bool
	startPpprof  bool // pprof handlers actually registered
	startPromSrv bool // prom HTTP server actually started
}

// parseNsFlags is split out so tests can drive flag parsing and assert
// the resulting struct without needing the rest of runMain to fire.
func parseNsFlags(args []string, stderr io.Writer) (*nsFlags, int) {
	fs := flag.NewFlagSet("ns", flag.ContinueOnError)
	fs.SetOutput(stderr)
	promListen := fs.String("promListen", promListenCst, "Prometheus http listening socket")
	promPath := fs.String("promPath", promPathCst, "Prometheus http path")
	profileMode := fs.String("profile.mode", "", "enable profiling mode, one of [cpu, mem, mutex, block]")
	v := fs.Bool("v", false, "show version")
	d := fs.Uint("d", debugLevelCst, "debug level")
	enablePprof := fs.Bool("pprof", false, "expose /debug/pprof on the prometheus listener (off by default; was unconditional, gosec G108)")
	if err := fs.Parse(args); err != nil {
		return nil, 2
	}
	return &nsFlags{
		promListen:   *promListen,
		promPath:     *promPath,
		profileMode:  *profileMode,
		v:            *v,
		d:            *d,
		enablePprof:  *enablePprof,
		startPpprof:  true,
		startPromSrv: true,
	}, 0
}

func runMain(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	f, rc := parseNsFlags(args, stderr)
	if f == nil {
		return rc
	}

	if f.v {
		fmt.Fprintf(stdout, "xtcp commit:%s\tdate(UTC):%s\tversion:%s\n", commit, date, version)
		return 0
	}

	debugLevel = f.d
	if f.enablePprof && f.startPpprof {
		registerPprof(f.promListen)
	}

	if f.d > 10 {
		log.Println("*profileMode:", f.profileMode)
	}
	if stopper := startProfile(f.profileMode, f.d); stopper != nil {
		defer stopper()
	}

	if f.startPromSrv {
		promHandlerStarter(f.promPath, f.promListen)
	}
	if f.d > 10 {
		log.Println("Prometheus http listener started on:", f.promListen, f.promPath)
	}

	dctx, cancel := context.WithCancel(ctx)
	complete := make(chan struct{}, signalChannelSizeCst)
	go initSignalHandler(cancel, complete)

	daemonRunner(dctx, cancel, debugLevel)

	select {
	case complete <- struct{}{}:
	default:
	}

	if debugLevel > 10 {
		log.Println("ns.go Main complete - farewell")
	}
	return 0
}

// registerPprof installs the pprof handlers on http.DefaultServeMux. Split
// out so tests can call it once (or not at all) — registering twice on the
// default mux would panic.
func registerPprof(promListen string) {
	http.HandleFunc("/debug/pprof/", pprof.Index)
	http.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	http.HandleFunc("/debug/pprof/profile", pprof.Profile)
	http.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	http.HandleFunc("/debug/pprof/trace", pprof.Trace)
	log.Println("pprof endpoints registered at /debug/pprof on", promListen)
}

// startProfile starts the optional profiler matching `mode` and returns
// its Stop closure (or nil if no profiling). Extracted so tests can
// exercise the switch's branches without holding a deferred profiler.
func startProfile(mode string, d uint) func() {
	switch mode {
	case "cpu": //nolint:goconst // pprof mode names are exact CLI inputs; consts add no value here
		return profile.Start(profile.CPUProfile, profile.ProfilePath(".")).Stop
	case "mem":
		return profile.Start(profile.MemProfile, profile.ProfilePath(".")).Stop
	case "memheap":
		return profile.Start(profile.MemProfileHeap, profile.ProfilePath(".")).Stop
	case "mutex":
		return profile.Start(profile.MutexProfile, profile.ProfilePath(".")).Stop
	case "block":
		return profile.Start(profile.BlockProfile, profile.ProfilePath(".")).Stop
	case "trace":
		return profile.Start(profile.TraceProfile, profile.ProfilePath(".")).Stop
	case "goroutine":
		return profile.Start(profile.GoroutineProfile, profile.ProfilePath(".")).Stop
	}
	if d > 10 {
		log.Println("No profiling")
	}
	return nil
}

// runDaemonDefault is the production daemon body: build an xtcp instance
// and run the NoPoller variant until the wg is released by ctx cancel.
// Tests substitute this via the daemonRunner package var.
func runDaemonDefault(ctx context.Context, cancel context.CancelFunc, debugLvl uint) {
	xNS := xtcp.NewNsTestingXTCP(ctx, cancel, uint32(debugLvl))

	var wg sync.WaitGroup
	wg.Add(1)
	go xNS.RunNoPoller(ctx, &wg)

	if debugLvl > 10 {
		log.Println("xNS.RunNSOnly(ctx, &wg) complete. wg.Wait()")
	}
	wg.Wait()
}

// initSignalHandler sets up signal handling for the process, and
// will call cancel() when received
func initSignalHandler(cancel context.CancelFunc, complete <-chan struct{}) {
	c := make(chan os.Signal, signalChannelSizeCst)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	awaitSignalAndShutdown(c, cancel, complete, cancelSleepTimeCst, true)
}

// awaitSignalAndShutdown blocks on `sigs`, then calls cancel() and waits for
// either `complete` or `timeout` before optionally calling os.Exit(0). Split
// out from initSignalHandler so tests can drive it without raising real OS
// signals (and without process-exit).
func awaitSignalAndShutdown(
	sigs <-chan os.Signal,
	cancel context.CancelFunc,
	complete <-chan struct{},
	timeout time.Duration,
	doExit bool,
) {
	<-sigs
	log.Printf("Signal caught, closing application")
	cancel()

	log.Printf("Signal caught, cancel() called, and sleeping to allow goroutines to close")
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-complete:
		log.Printf("<-complete exit(0)")
	case <-timer.C:
		log.Printf("Sleep complete, goodbye! exit(0)")
	}

	if doExit {
		os.Exit(0) //nolint:gocritic // intentional process exit; deferred timer.Stop is moot once the process terminates
	}
}

// initPromHandler starts the prom handler with error checking
// https://pkg.go.dev/github.com/prometheus/client_golang/prometheus/promhttp?tab=doc#HandlerOpts
func initPromHandler(promPath string, promListen string) {
	http.Handle(promPath, promhttp.HandlerFor(
		prometheus.DefaultGatherer,
		promhttp.HandlerOpts{
			EnableOpenMetrics:   promEnableOpenMetrics,
			MaxRequestsInFlight: promMaxRequestsInFlight,
		},
	))
	go servePromHandler(promListen)
}

// servePromHandler runs the prom HTTP server on `promListen`. On
// ListenAndServe failure it calls fatalf (default log.Fatalf in
// production; swapped to a capture by tests). Extracted from
// initPromHandler so tests can drive it with an unavailable port.
func servePromHandler(promListen string) {
	srv := &http.Server{
		Addr:              promListen,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil {
		fatalf("prometheus error: %v", err)
	}
}
