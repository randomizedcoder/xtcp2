package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	_ "net/http/pprof"

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
)

func main() {

	misc.DieIfNotLinux()

	ctx, cancel := context.WithCancel(context.Background())

	complete := make(chan struct{}, signalChannelSizeCst)
	go initSignalHandler(cancel, complete)

	promListen := flag.String("promListen", promListenCst, "Prometheus http listening socket")
	promPath := flag.String("promPath", promPathCst, "Prometheus http path")
	// curl -s http://[::1]:9000/metrics 2>&1 | grep -v "#"
	// curl -s http://127.0.0.1:9000/metrics 2>&1 | grep -v "#"

	// ./ns --profile.mode cpu
	// timeout 1h ./ns --profile.mode cpu
	profileMode := flag.String("profile.mode", "", "enable profiling mode, one of [cpu, mem, mutex, block]")

	v := flag.Bool("v", false, "show version")

	d := flag.Uint("d", debugLevelCst, "debug level")

	flag.Parse()

	// Print version information passed in via ldflags in the Makefile
	if *v {
		log.Printf("xtcp commit:%s\tdate(UTC):%s\tversion:%s", commit, date, version)
		os.Exit(0)
	}

	debugLevel = *d

	// go func() {
	// 	// https://go.dev/blog/pprof
	// 	// go tool pprof http://localhost:6060/debug/pprof/profile?seconds=60
	// 	// go tool pprof http://localhost:6060/debug/pprof/heap?seconds=60
	// 	// go tool pprof http://localhost:6060/debug/pprof/block?seconds=60
	// 	// https://pkg.go.dev/runtime/pprof#Profile
	// 	log.Println(http.ListenAndServe("0.0.0.0:6060", nil))
	// }()

	if *d > 10 {
		log.Println("*profileMode:", *profileMode)
	}
	switch *profileMode {
	case "cpu":
		defer profile.Start(profile.CPUProfile, profile.ProfilePath(".")).Stop()
	case "mem":
		defer profile.Start(profile.MemProfile, profile.ProfilePath(".")).Stop()
	case "memheap":
		defer profile.Start(profile.MemProfileHeap, profile.ProfilePath(".")).Stop()
	case "mutex":
		defer profile.Start(profile.MutexProfile, profile.ProfilePath(".")).Stop()
	case "block":
		defer profile.Start(profile.BlockProfile, profile.ProfilePath(".")).Stop()
	case "trace":
		defer profile.Start(profile.TraceProfile, profile.ProfilePath(".")).Stop()
	case "goroutine":
		defer profile.Start(profile.GoroutineProfile, profile.ProfilePath(".")).Stop()
	default:
		if *d > 10 {
			log.Println("No profiling")
		}
	}

	go initPromHandler(*promPath, *promListen)
	if *d > 10 {
		log.Println("Prometheus http listener started on:", *promListen, *promPath)
	}

	xNS := xtcp.NewNsTestingXTCP(ctx, cancel, uint32(debugLevel))

	var wg sync.WaitGroup
	wg.Add(1)

	go xNS.RunNoPoller(ctx, &wg)

	if debugLevel > 10 {
		log.Println("xNS.RunNSOnly(ctx, &wg) complete. wg.Wait()")
	}

	wg.Wait()
	complete <- struct{}{}

	if debugLevel > 10 {
		log.Println("ns.go Main complete - farewell")
	}
}

// initSignalHandler sets up signal handling for the process, and
// will call cancel() when received
func initSignalHandler(cancel context.CancelFunc, complete <-chan struct{}) {

	c := make(chan os.Signal, signalChannelSizeCst)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	<-c
	log.Printf("Signal caught, closing application")
	cancel()

	log.Printf("Signal caught, cancel() called, and sleeping to allow goroutines to close")
	timer := time.NewTimer(cancelSleepTimeCst)

	select {
	case <-complete:
		log.Printf("<-complete exit(0)")
	case <-timer.C:
		// if we exit here, this means all the other go routines didn't shutdown
		// need to investigate why
		log.Printf("Sleep complete, goodbye! exit(0)")
	}

	os.Exit(0)
}

// initPromHandler starts the prom handler with error checking
// https: //pkg.go.dev/github.com/prometheus/client_golang/prometheus/promhttp?tab=doc#HandlerOpts
func initPromHandler(promPath string, promListen string) {
	http.Handle(promPath, promhttp.HandlerFor(
		prometheus.DefaultGatherer,
		promhttp.HandlerOpts{
			EnableOpenMetrics:   promEnableOpenMetrics,
			MaxRequestsInFlight: promMaxRequestsInFlight,
		},
	))
	go func() {
		err := http.ListenAndServe(promListen, nil)
		if err != nil {
			log.Fatal("prometheus error", err)
		}
	}()
}
