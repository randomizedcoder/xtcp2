package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/pkg/profile"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/randomizedcoder/xtcp2/pkg/config"
	"github.com/randomizedcoder/xtcp2/pkg/misc"
	"github.com/randomizedcoder/xtcp2/pkg/xtcp"
)

const (
	debugLevelCst = 11

	signalChannelSizeCst = 10
	cancelSleepTimeCst   = 15 * time.Second

	promListenCst           = ":9009" // [::1]:9009
	promPathCst             = "/metrics"
	promMaxRequestsInFlight = 10
	promEnableOpenMetrics   = true

	WriteFilesCst = 0

	capturePathCst = "./"
	// capturePathCst = "../../pkg/xtcpnl/testdata/netlink_packets_capture/"

	modulusCst = 1 // 2000

	// proto, protojson, prototext,
	marshalCst = "proto"

	// Redpanda
	// destCst = "kafka:localhost:19092"
	destCst = "kafka:redpanda-0:9092"
	// destCst = "kafka:kafka:9092"
	// destCst = "udp:127.0.0.1:13000"
	// destCst = "nsq:nsqd:4150"
	// destCst = "nats:nats:8222"
	// destCst = "valkey:valkey:6379"

	topicCst = "xtcp"

	// startSleepCst = 10 * time.Second
)

var (
	// Passed by "go build -ldflags" for the show version
	commit  string
	date    string
	version string

	debugLevel int
)

// main function is responsible for a few key activities
// 0. Exits if we aren't running on Linux
// 1. Handles all the CLI flags
// 1.1 Populates a big cliFlags struct to make it easy to pass to other goroutines
// 3. Version printing
// 4.
// 5. Allows for profiling options
// 6. Starts the staters (the multiple metrics go routines), which includes the Prometheus metric endpoints HTTP handler
// 7. Starts the poller which is really the main loop for xtcp
func main() {

	misc.DieIfNotLinux()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	allDoneCh := make(chan struct{}, 2)
	wg.Add(1)
	go initSignalHandler(&wg, cancel, allDoneCh)

	nltimeout := flag.Int64("nltimeout", 1000, "Netlink socket timeout in milliseconds.  Zero(0) for no timeout")
	pollingFrequency := flag.Duration("frequency", 5*time.Second, "Polling frequency")
	maxLoops := flag.Int("maxLoops", 0, "Maximum number of loops, or zero (0) for forever")
	netlinkers := flag.Int("netlinkers", 4, "netlinkers")
	nlmsgSeq := flag.Int("nlmsgSeq", 666, "nlmsgSeq sequence number (start), which should be uint32")
	// packetSize of the buffer the netlinkers syscall.Recvfrom to read into
	packetSize := flag.Int("packetSize", 0, "netlinker packetSize.  buffer size = packetSize * packetSizeMply. Use zero (0) for syscall.Getpagesize()")
	packetSizeMply := flag.Int("packetSizeMply", 8, "netlinker packetSize multiplier.  buffer size = packetSize * packetSizeMply")

	writeFiles := flag.Int("writeFiles", WriteFilesCst, "Write netlink packets to writeFiles number of files ( to generate test data ) per netlinker")
	capturePath := flag.String("capturePath", capturePathCst, "Write files path")

	modulus := flag.Int("modulus", modulusCst, "modulus. Report every X inetd messages to output")
	marshal := flag.String("marshal", marshalCst, "Marshalling of the exported data (proto,json,prototext)")
	dest := flag.String("dest", destCst, "kafka:127.0.0.1:9092, udp:127.0.0.1:13000, or nsq:127.0.0.1:4150")
	topic := flag.String("topic", topicCst, "Kafka or NSQ topic")

	promListen := flag.String("promListen", promListenCst, "Prometheus http listening socket")
	promPath := flag.String("promPath", promPathCst, "Prometheus http path")
	// curl -s http://[::1]:9000/metrics 2>&1 | grep -v "#"
	// curl -s http://127.0.0.1:9000/metrics 2>&1 | grep -v "#"

	// Maximum number of CPUs that can be executing simultaneously
	// https://golang.org/pkg/runtime/#GOMAXPROCS -> zero (0) means default
	goMaxProcs := flag.Int("goMaxProcs", 4, "goMaxProcs = https://golang.org/pkg/runtime/#GOMAXPROCS")

	// ./xtcp2 --profile.mode cpu
	// timeout 1h ./xtcp2 --profile.mode cpu
	profileMode := flag.String("profile.mode", "", "enable profiling mode, one of [cpu, mem, mutex, block]")

	v := flag.Bool("v", false, "show version")

	d := flag.Int("d", debugLevelCst, "debug level")

	flag.Parse()

	// Print version information passed in via ldflags in the Makefile
	if *v {
		log.Printf("xtcp commit:%s\tdate(UTC):%s\tversion:%s", commit, date, version)
		os.Exit(0)
	}

	debugLevel = *d

	if debugLevel > 10 {
		fmt.Println("*nltimeout(ms):", *nltimeout)
		fmt.Println("*pollingFrequency:", *pollingFrequency)
		fmt.Println("*maxLoops:", *maxLoops)
		fmt.Println("*netlinkers:", *netlinkers)
		fmt.Println("*nlmsgSeq:", *nlmsgSeq)
		fmt.Println("*packetSize:", *packetSize)
		fmt.Println("*packetSizeMply:", *packetSizeMply)
		fmt.Println("*writeFiles:", *writeFiles)
		fmt.Println("*capturePath:", *capturePath)
		fmt.Println("*modulus:", *modulus)
		fmt.Println("*marshal:", *marshal)
		fmt.Println("*dest:", *dest)
		fmt.Println("*topic:", *topic)
		fmt.Println("*promListen:", *promListen)
		fmt.Println("*promPath:", *promPath)
		fmt.Println("*goMaxProcs:", *goMaxProcs)
		fmt.Println("*d:", *d)
	}

	c := config.Config{
		NLTimeout:        nltimeout,
		PollingFrequency: pollingFrequency,
		MaxLoops:         maxLoops,
		Netlinkers:       netlinkers,
		NlmsgSeq:         nlmsgSeq,
		PacketSize:       packetSize,
		PacketSizeMply:   packetSizeMply,
		WriteFiles:       writeFiles,
		CapturePath:      capturePath,
		Modulus:          modulus,
		Marshal:          marshal,
		Dest:             dest,
		Topic:            topic,
		PromListen:       promListen,
		PromPath:         promPath,
		DebugLevel:       d,
	}

	if runtime.NumCPU() > *goMaxProcs {
		mp := runtime.GOMAXPROCS(*goMaxProcs)
		if debugLevel > 10 {
			log.Printf("Main runtime.GOMAXPROCS now:%d was:%d\n", *goMaxProcs, mp)
		}
	}

	// "github.com/pkg/profile"
	// https://dave.cheney.net/2013/07/07/introducing-profile-super-simple-profiling-for-go-programs
	// e.g. ./xtcp -profile.mode trace
	// go tool trace trace.out
	// e.g. ./xtcp -profile.mode cpu
	// go tool pprof -http=":8081" xtcp cpu.pprof

	if debugLevel > 10 {
		log.Println("*profileMode:", *profileMode)
	}
	switch *profileMode {
	case "cpu":
		defer profile.Start(profile.CPUProfile, profile.ProfilePath(".")).Stop()
	case "mem":
		defer profile.Start(profile.MemProfile, profile.ProfilePath(".")).Stop()
	case "mutex":
		defer profile.Start(profile.MutexProfile, profile.ProfilePath(".")).Stop()
	case "block":
		defer profile.Start(profile.BlockProfile, profile.ProfilePath(".")).Stop()
	case "trace":
		defer profile.Start(profile.TraceProfile, profile.ProfilePath(".")).Stop()
	default:
		if debugLevel > 1000 {
			log.Println("No profiling")
		}
	}

	go initPromHandler(*promPath, *promListen)
	if debugLevel > 10 {
		log.Println("Prometheus http listener started on:", *promListen, *promPath)
	}

	// if debugLevel > 10 {
	// 	log.Printf("sleeping startSleepCst:%0.3f", startSleepCst.Seconds())
	// }
	// time.Sleep(startSleepCst)

	xtcp, err := xtcp.NewXTCP(ctx, c, &allDoneCh)
	if err != nil {
		panic(err)
	}

	xtcp.Run(ctx)

	if debugLevel > 10 {
		log.Println("Main complete - farewell")
	}

	wg.Wait()
}

// initSignalHandler sets up signal handling for the process, and
// will call cancel() when received
func initSignalHandler(wg *sync.WaitGroup, cancel context.CancelFunc, allDoneCh chan struct{}) {

	defer wg.Done()

	c := make(chan os.Signal, signalChannelSizeCst)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	<-c
	log.Printf("Signal caught, closing application")
	cancel()

	log.Printf("Signal caught, cancel() called, and sleeping to allow goroutines to close")
	timer := time.NewTimer(cancelSleepTimeCst)

	select {
	case <-timer.C:
		log.Printf("Sleep complete, goodbye! exit(0)")
	case <-allDoneCh:
		log.Printf("All go routines complete, goodbye!")
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