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
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	// protovalidate "github.com/bufbuild/protovalidate-go"
	"github.com/bufbuild/protovalidate-go"
	"github.com/pkg/profile"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/randomizedcoder/xtcp2/pkg/misc"
	"github.com/randomizedcoder/xtcp2/pkg/xtcp"
	"github.com/randomizedcoder/xtcp2/pkg/xtcp_config"
	"google.golang.org/protobuf/types/known/durationpb"
)

const (
	debugLevelCst = 111

	signalChannelSizeCst = 10
	cancelSleepTimeCst   = 5 * time.Second

	promListenCst           = ":9088" // [::1]:9088
	promPathCst             = "/metrics"
	promMaxRequestsInFlight = 10
	promEnableOpenMetrics   = true

	nltimeoutCst      = 1000
	pollFrequencyCst  = 10 * time.Second
	pollTimeoutCst    = 5 * time.Second
	maxLoopsCst       = 0
	netlinkersCst     = 4
	nlmsgSeqCst       = 666
	packetSizeCst     = 0
	packetSizeMplyCst = 8

	WriteFilesCst     = 0
	DestWriteFilesCst = 10

	capturePathCst = "./"
	// capturePathCst = "../../pkg/xtcpnl/testdata/netlink_packets_capture/"

	modulusCst = 1 // 2000

	// protobufList, protobufSingle, protoDelim, protoJson, protoText, msgpack
	marshalCst                   = "protobufSingle"
	protobufListLengthDelimitCst = false

	// Redpanda
	destCst = "kafka:redpanda-0:9092"
	// destCst = "udp:127.0.0.1:13000"
	// destCst = "nsq:nsqd:4150"
	// destCst = "nats:nats:8222"
	// destCst = "valkey:valkey:6379"
	// destCst = "null"

	topicCst = "xtcp"

	// relative to the container
	xtcpProtoFileCst  = "/xtcp_flat_record.proto"
	kafkaSchemaUrlCst = "http://localhost:18081"

	kafkaProduceTimeoutCst = 0 // not sure why this isn't working
	// kafkaProduceTimeoutCst = 30 * time.Second

	labelCst = ""
	tagCst   = ""

	deserializersCst = "all"

	grpcPortCst = 8889

	netlinkerDoneChSizeCst = 100

	// startSleepCst = 10 * time.Second

	base10    = 10
	sixtyFour = 64
)

var (
	// Passed by "go build -ldflags" for the show version
	commit  string
	date    string
	version string

	debugLevel uint
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
// mainFlags holds the pointers returned by flag.X(...) for every CLI
// arg main() consumes. Bundling them keeps the top-level orchestration
// short and lets the per-section helpers (printFlags, buildConfig,
// startProfile) take a single argument instead of 30 positional ones.
type mainFlags struct {
	nltimeout                 *uint64
	pollFrequency             *time.Duration
	pollTimeout               *time.Duration
	maxLoops                  *uint64
	netlinkers                *uint
	nlmsgSeq                  *uint
	packetSize                *uint64
	packetSizeMply            *uint
	writeFiles                *uint
	capturePath               *string
	modulus                   *uint64
	marshal                   *string
	protobufListLengthDelimit *bool
	dest                      *string
	destWriteFiles            *uint
	topic                     *string
	xtcpProtoFile             *string
	kafkaSchemaUrl            *string
	produceTimeout            *time.Duration
	label                     *string
	tag                       *string
	grpcPort                  *uint
	deserializers             *string
	promListen                *string
	promPath                  *string
	goMaxProcs                *uint
	profileMode               *string
	v                         *bool
	conf                      *bool
	d                         *uint
	ioUring                   *bool
	ioUringRecvBatch          *uint
	ioUringCqeBatch           *uint
}

func defineFlags() *mainFlags {
	f := &mainFlags{}
	f.nltimeout = flag.Uint64("nltimeout", nltimeoutCst, "Netlink socket timeout in milliseconds.  Zero(0) for no timeout")
	f.pollFrequency = flag.Duration("frequency", pollFrequencyCst, "Poll frequency")
	f.pollTimeout = flag.Duration("timeout", pollTimeoutCst, "Poll timeout per name space")
	f.maxLoops = flag.Uint64("maxLoops", maxLoopsCst, "Maximum number of loops, or zero (0) for forever")
	f.netlinkers = flag.Uint("netlinkers", netlinkersCst, "netlinkers which read netlink messages from each socket. increase this if you have many flows")
	f.nlmsgSeq = flag.Uint("nlmsgSeq", nlmsgSeqCst, "nlmsgSeq sequence number (start), which should be uint32")
	// packetSize of the buffer the netlinkers syscall.Recvfrom to read into
	f.packetSize = flag.Uint64("packetSize", packetSizeCst, "netlinker packetSize.  buffer size = packetSize * packetSizeMply. Use zero (0) for syscall.Getpagesize()")
	f.packetSizeMply = flag.Uint("packetSizeMply", packetSizeMplyCst, "netlinker packetSize multiplier.  buffer size = packetSize * packetSizeMply")
	f.writeFiles = flag.Uint("writeFiles", WriteFilesCst, "Write netlink packets to writeFiles number of files ( to generate test data ) per netlinker")
	f.capturePath = flag.String("capturePath", capturePathCst, "Write files path")
	f.modulus = flag.Uint64("modulus", modulusCst, "modulus. Report every X inetd messages to output")
	f.marshal = flag.String("marshal", marshalCst, "Marshaling of the exported data (protobufList, protoJson, protoText, msgpack)")
	f.protobufListLengthDelimit = flag.Bool("protobufListLengthDelimit", protobufListLengthDelimitCst, "protobufListLengthDelimit")
	f.dest = flag.String("dest", destCst, "kafka:127.0.0.1:9092, udp:127.0.0.1:13000, or nsq:127.0.0.1:4150")
	f.destWriteFiles = flag.Uint("destWriteFiles", DestWriteFilesCst, "Write out the marshaled data to destWriteFiles number of files ( for debugging only )")
	f.topic = flag.String("topic", topicCst, "Kafka or NSQ topic")
	f.xtcpProtoFile = flag.String("xtcpProtoFile", xtcpProtoFileCst, "xtcpProtoFile for registering with the schema registry")
	f.kafkaSchemaUrl = flag.String("kafkaSchemaUrl", kafkaSchemaUrlCst, "kafka schema registry URL")
	f.produceTimeout = flag.Duration("produceTimeout", kafkaProduceTimeoutCst, "Kafka produce timeout (context.WithTimeout)")
	f.label = flag.String("label", labelCst, "label applied to the protobuf")
	f.tag = flag.String("tag", tagCst, "label applied to the protobuf")
	f.grpcPort = flag.Uint("grpcPort", grpcPortCst, "GRPC listening port")
	f.deserializers = flag.String("deserializers", deserializersCst, fmt.Sprintf("Comma separated list of deserializers,%v", xtcp.GetAllDeserializers()))
	f.promListen = flag.String("promListen", promListenCst, "Prometheus http listening socket")
	f.promPath = flag.String("promPath", promPathCst, "Prometheus http path")
	// Maximum number of CPUs that can be executing simultaneously
	// https://golang.org/pkg/runtime/#GOMAXPROCS -> zero (0) means default
	f.goMaxProcs = flag.Uint("goMaxProcs", 4, "goMaxProcs = https://golang.org/pkg/runtime/#GOMAXPROCS")
	// ./xtcp2 --profile.mode cpu
	// timeout 1h ./xtcp2 --profile.mode cpu
	f.profileMode = flag.String("profile.mode", "", "enable profiling mode, one of [cpu, mem, mutex, block]")
	f.v = flag.Bool("v", false, "show version")
	f.conf = flag.Bool("conf", false, "show config")
	f.d = flag.Uint("d", debugLevelCst, "debug level")
	f.ioUring = flag.Bool("ioUring", false, "Opt in to io_uring for netlink reads and raw-socket destination writes (Linux 6.1+)")
	f.ioUringRecvBatch = flag.Uint("ioUringRecvBatch", 64, "io_uring recvmsg SQEs kept in flight per Netlinker (1-4096). Higher reduces syscalls on high-fanout hosts.")
	f.ioUringCqeBatch = flag.Uint("ioUringCqeBatch", 128, "io_uring max CQEs reaped per PeekBatchCQE call (1-4096)")
	return f
}

func printFlags(f *mainFlags) {
	fmt.Println("*nltimeout(ms):", *f.nltimeout)
	fmt.Println("*pollFrequency:", *f.pollFrequency)
	fmt.Println("*pollTimeout:", *f.pollTimeout)
	fmt.Println("*maxLoops:", *f.maxLoops)
	fmt.Println("*netlinkers:", *f.netlinkers)
	fmt.Println("*nlmsgSeq:", *f.nlmsgSeq)
	fmt.Println("*packetSize:", *f.packetSize)
	fmt.Println("*packetSizeMply:", *f.packetSizeMply)
	fmt.Println("*writeFiles:", *f.writeFiles)
	fmt.Println("*capturePath:", *f.capturePath)
	fmt.Println("*modulus:", *f.modulus)
	fmt.Println("*marshal:", *f.marshal)
	fmt.Println("*protobufListLengthDelimit:", *f.protobufListLengthDelimit)
	fmt.Println("*dest:", *f.dest)
	fmt.Println("*destWriteFiles:", *f.destWriteFiles)
	fmt.Println("*topic:", *f.topic)
	fmt.Println("*xtcpProtoFile:", *f.xtcpProtoFile)
	fmt.Println("*kafkaSchemaUrl:", *f.kafkaSchemaUrl)
	fmt.Println("*produceTimeout:", *f.produceTimeout)
	fmt.Println("*promListen:", *f.promListen)
	fmt.Println("*promPath:", *f.promPath)
	fmt.Println("*goMaxProcs:", *f.goMaxProcs)
	fmt.Println("*d:", *f.d)
}

func buildConfig(f *mainFlags, des *xtcp_config.EnabledDeserializers) *xtcp_config.XtcpConfig {
	return &xtcp_config.XtcpConfig{
		NlTimeoutMilliseconds:  *f.nltimeout,
		PollFrequency:          durationpb.New(*f.pollFrequency),
		PollTimeout:            durationpb.New(*f.pollTimeout),
		MaxLoops:               *f.maxLoops,
		Netlinkers:             uint32(*f.netlinkers),
		NetlinkersDoneChanSize: netlinkerDoneChSizeCst,
		NlmsgSeq:               uint32(*f.nlmsgSeq),
		PacketSize:             *f.packetSize,
		PacketSizeMply:         uint32(*f.packetSizeMply),
		WriteFiles:             uint32(*f.writeFiles),
		CapturePath:            *f.capturePath,
		Modulus:                *f.modulus,
		MarshalTo:              *f.marshal,
		Dest:                   *f.dest,
		DestWriteFiles:         uint32(*f.destWriteFiles),
		Topic:                  *f.topic,
		XtcpProtoFile:          *f.xtcpProtoFile,
		KafkaSchemaUrl:         *f.kafkaSchemaUrl,
		KafkaProduceTimeout:    durationpb.New(*f.produceTimeout),
		DebugLevel:             uint32(*f.d),
		Label:                  *f.label,
		Tag:                    *f.tag,
		GrpcPort:               uint32(*f.grpcPort),
		EnabledDeserializers:   des,

		IoUring:              *f.ioUring,
		IoUringRecvBatchSize: uint32(*f.ioUringRecvBatch),
		IoUringCqeBatchSize:  uint32(*f.ioUringCqeBatch),
	}
}

// startProfile installs the pkg/profile hook selected by `mode` and
// returns the corresponding Stop closure for the caller to defer.
// "github.com/pkg/profile"
// https://dave.cheney.net/2013/07/07/introducing-profile-super-simple-profiling-for-go-programs
// e.g. ./xtcp -profile.mode trace; go tool trace trace.out
// e.g. ./xtcp -profile.mode cpu; go tool pprof -http=":8081" xtcp cpu.pprof
func startProfile(mode string, debugLevel uint) func() {
	if debugLevel > 10 {
		log.Println("*profileMode:", mode)
	}
	var p interface{ Stop() }
	switch mode {
	case "cpu":
		p = profile.Start(profile.CPUProfile, profile.ProfilePath("."))
	case "mem":
		p = profile.Start(profile.MemProfile, profile.ProfilePath("."))
	case "memheap":
		p = profile.Start(profile.MemProfileHeap, profile.ProfilePath("."))
	case "mutex":
		p = profile.Start(profile.MutexProfile, profile.ProfilePath("."))
	case "block":
		p = profile.Start(profile.BlockProfile, profile.ProfilePath("."))
	case "trace":
		p = profile.Start(profile.TraceProfile, profile.ProfilePath("."))
	case "goroutine":
		p = profile.Start(profile.GoroutineProfile, profile.ProfilePath("."))
	default:
		if debugLevel > 1000 {
			log.Println("No profiling")
		}
		return func() {}
	}
	return p.Stop
}

// versionString builds the -v output line. Exposed (lowercase but in the
// same package, called from tests) so the version-flag path is testable
// without a subprocess.
func versionString() string {
	return fmt.Sprintf("xtcp commit:%s\tdate(UTC):%s\tversion:%s",
		commit, date, version)
}

// prepareConfig runs the env-override + validation portion of main() up
// to (but not including) the NewXTCP / RunWithPoller daemon-start step.
// Returns the built *xtcp_config.XtcpConfig and a `done` flag set when
// either -v or -conf short-circuited the run (caller should exit 0).
func prepareConfig(f *mainFlags) (*xtcp_config.XtcpConfig, bool) {
	if *f.v {
		log.Print(versionString())
		return nil, true
	}

	environmentOverrideDebugLevel(f.d, *f.d)
	debugLevel = *f.d

	if debugLevel > 10 {
		printFlags(f)
	}

	c := buildConfig(f, getDeserializers(*f.deserializers))

	if debugLevel > 100 {
		printConfig(c, "Before environmentOverrideConfig")
	}
	environmentOverrideConfig(c, debugLevel)
	if debugLevel > 100 {
		printConfig(c, "After environmentOverrideConfig")
	}

	if *f.conf {
		printConfig(c, "conf argument")
		return c, true
	}
	return c, false
}

// daemonRunner builds the xtcp daemon and runs it. Defaults to the
// production xtcp.NewXTCP + RunWithPoller path; tests substitute a stub
// that returns immediately so cmd/xtcp2 tests don't need real netlink.
var daemonRunner = runDaemonDefault

// promHandlerStarter is the indirection point for the prom-handler
// goroutine launch. Default starts the real handler; tests swap it for
// a no-op to skip the port-bind.
var promHandlerStarter = func(promPath, promListen string) {
	go initPromHandler(promPath, promListen)
}

func main() {
	misc.DieIfNotLinux()
	os.Exit(runMain(context.Background()))
}

// runMain wires the production main body. Extracted so tests can run
// it with stubbed daemonRunner / promHandlerStarter.
func runMain(parentCtx context.Context) int {
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	complete := make(chan struct{}, signalChannelSizeCst)
	go initSignalHandler(cancel, complete)

	f := defineFlags()
	flag.Parse()

	c, done := prepareConfig(f)
	if done {
		return 0
	}

	environmentOverrideGoMaxProcs(f.goMaxProcs, debugLevel)
	if runtime.NumCPU() > int(*f.goMaxProcs) {
		mp := runtime.GOMAXPROCS(int(*f.goMaxProcs))
		if debugLevel > 10 {
			log.Printf("Main runtime.GOMAXPROCS now:%d was:%d\n", *f.goMaxProcs, mp)
		}
	}

	defer startProfile(*f.profileMode, debugLevel)()

	environmentOverrideProm(f.promListen, f.promPath, debugLevel)
	promHandlerStarter(*f.promPath, *f.promListen)
	if debugLevel > 10 {
		log.Println("Prometheus http listener started on:", *f.promListen, *f.promPath)
	}

	if err := protovalidate.Validate(c); err != nil {
		fatalf("config validation failed: %v", err)
		return 1
	}
	if debugLevel > 10 {
		log.Println("config validation succeeded")
	}

	daemonRunner(ctx, cancel, c)
	select {
	case complete <- struct{}{}:
	default:
	}
	if debugLevel > 10 {
		log.Println("xtcp2.go Main complete - farewell")
	}
	return 0
}

// runDaemonDefault is the production daemon body: build an xtcp instance
// and run RunWithPoller until ctx cancels the WG.
func runDaemonDefault(ctx context.Context, cancel context.CancelFunc, c *xtcp_config.XtcpConfig) {
	x := xtcp.NewXTCP(ctx, cancel, c)
	if debugLevel > 10 {
		log.Println("xtcp.Run(ctx, &wg)")
	}
	var wg sync.WaitGroup
	wg.Add(1)
	x.RunWithPoller(ctx, &wg)
	if debugLevel > 10 {
		log.Println("xtcp.Run(ctx) complete. wg.Wait()")
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

// awaitSignalAndShutdown blocks on `sigs`, calls cancel(), then waits for
// either `complete` or `timeout` before optionally calling os.Exit(0).
// Split out so tests can drive it with a synthetic sigs channel and
// doExit=false (without raising real OS signals or terminating the test
// process).
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

	log.Printf("Signal caught, cancel() called, and sleeping to allow goroutines to close, sleeping:%s",
		timeout.String())
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
// fatalf is the package-level abort handler. Defaults to log.Fatalf;
// tests swap this in for a capture so servePromHandler's
// ListenAndServe error branch is exercisable without exiting.
var fatalf = log.Fatalf

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

// servePromHandler runs the prom HTTP server on promListen. On
// ListenAndServe failure it invokes fatalf (default log.Fatalf in
// production, swapped to a capture by tests). Extracted from
// initPromHandler so tests can drive the error path in isolation.
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

// environmentOverrideProm MUTATES promListen, promPath, if the environment
// variables exist.  This allows over riding the cli flags
func environmentOverrideProm(promListen, promPath *string, debugLevel uint) {
	key := "PROM_LISTEN"
	if value, exists := os.LookupEnv(key); exists {
		*promListen = value
		if debugLevel > 10 {
			log.Printf("key:%s, c.PromListen:%s", key, *promListen)
		}
	}

	key = "PROM_PATH"
	if value, exists := os.LookupEnv(key); exists {
		*promPath = value
		if debugLevel > 10 {
			log.Printf("key:%s, c.PromPath:%s", key, *promPath)
		}
	}
}

// environmentOverrideDebugLevel MUTATES d if env var is set.
//
// Atoi+uint(i) wraps negative values to MaxUint (the bug 11 trap from
// the prior envUint{32,64} fix); ParseUint rejects "-1" up front so
// DEBUG_LEVEL=-5 doesn't silently turn every debug check into "yes".
func environmentOverrideDebugLevel(d *uint, debugLevel uint) {
	key := "DEBUG_LEVEL"
	if value, exists := os.LookupEnv(key); exists {
		if i, err := strconv.ParseUint(value, base10, sixtyFour); err == nil {
			*d = uint(i)
			if debugLevel > 10 {
				log.Printf("key:%s, d:%d", key, *d)
			}
		}
	}
}

// environmentOverrideGoMaxProcs MUTATES goMaxProcs if env var is set.
// Same fix shape as environmentOverrideDebugLevel above — ParseUint
// rejects negative values that previously wrapped via Atoi + uint(i).
func environmentOverrideGoMaxProcs(goMaxProcs *uint, debugLevel uint) {
	key := "GOMAXPROCS"
	if value, exists := os.LookupEnv(key); exists {
		if i, err := strconv.ParseUint(value, base10, sixtyFour); err == nil {
			*goMaxProcs = uint(i)
			if debugLevel > 10 {
				log.Printf("key:%s, goMaxProcs:%d", key, *goMaxProcs)
			}
		}
	}
}

// environmentOverrideConfig MUTATES the config if environment variables exist
// this is to allow the environment variables to override the arguments
// (probably poor form to be mutatating)
func environmentOverrideConfig(c *xtcp_config.XtcpConfig, debugLevel uint) {
	envOverridePolling(c, debugLevel)
	envOverrideNetlinker(c, debugLevel)
	envOverridePacket(c, debugLevel)
	envOverrideMarshalAndDest(c, debugLevel)
	envOverrideKafka(c, debugLevel)
	envOverrideLabeling(c, debugLevel)
}

// envUint64 parses an env var as base-10 int64 and yields it as uint64.
// Returns ok=false when the var is unset or unparseable.
func envUint64(key string) (uint64, bool) {
	v, ok := os.LookupEnv(key)
	if !ok {
		return 0, false
	}
	// ParseUint (not ParseInt) so a negative env value like "-1" is
	// rejected. Previously: ParseInt + uint64(i) → -1 became MaxUint64.
	u, err := strconv.ParseUint(v, base10, sixtyFour)
	if err != nil {
		return 0, false
	}
	return u, true
}

// envUint32 parses an env var as decimal int and yields it as uint32.
func envUint32(key string) (uint32, bool) {
	v, ok := os.LookupEnv(key)
	if !ok {
		return 0, false
	}
	// Same fix as envUint64: ParseUint rejects negative values that
	// previously wrapped to a huge unsigned via Atoi + uint32(i).
	u, err := strconv.ParseUint(v, base10, 32)
	if err != nil {
		return 0, false
	}
	return uint32(u), true
}

// envDuration parses an env var via time.ParseDuration.
func envDuration(key string) (time.Duration, bool) {
	v, ok := os.LookupEnv(key)
	if !ok {
		return 0, false
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, false
	}
	return d, true
}

// envString returns the env value if set.
func envString(key string) (string, bool) {
	return os.LookupEnv(key)
}

func logEnv(key, msg string, debugLevel uint) {
	if debugLevel > 10 {
		log.Printf("key:%s, %s", key, msg)
	}
}

func envOverridePolling(c *xtcp_config.XtcpConfig, debugLevel uint) {
	if v, ok := envUint64("NLTIMEOUTMS"); ok {
		c.NlTimeoutMilliseconds = v
		logEnv("NLTIMEOUTMS", fmt.Sprintf("c.NlTimeoutMilliseconds:%d", v), debugLevel)
	}
	if d, ok := envDuration("POLL_FREQUENCY"); ok {
		c.PollFrequency = durationpb.New(d)
		logEnv("POLL_FREQUENCY", fmt.Sprintf("c.PollingFrequency:%s", c.PollFrequency.String()), debugLevel)
	}
	if d, ok := envDuration("POLL_TIMEOUT"); ok {
		c.PollTimeout = durationpb.New(d)
		logEnv("POLL_TIMEOUT", fmt.Sprintf("c.PollingFrequency:%s", c.PollTimeout.String()), debugLevel)
	}
	if v, ok := envUint64("MAX_LOOPS"); ok {
		c.MaxLoops = v
		logEnv("MAX_LOOPS", fmt.Sprintf("c.MaxLoops:%d", v), debugLevel)
	}
	if v, ok := envUint64("MODULUS"); ok {
		c.Modulus = v
		logEnv("MODULUS", fmt.Sprintf("c.Modulus:%d", v), debugLevel)
	}
}

func envOverrideNetlinker(c *xtcp_config.XtcpConfig, debugLevel uint) {
	if v, ok := envUint32("NETLINKERS"); ok {
		c.Netlinkers = v
		logEnv("NETLINKERS", fmt.Sprintf("c.Netlinkers:%d", v), debugLevel)
	}
	if v, ok := envUint32("NETLINKERS_DONE_CHAN_SIZE"); ok {
		c.NetlinkersDoneChanSize = v
		logEnv("NETLINKERS_DONE_CHAN_SIZE", fmt.Sprintf("c.NetlinkersDoneChanSize:%d", v), debugLevel)
	}
	if v, ok := envUint32("NLMSQSEQ"); ok {
		c.NlmsgSeq = v
		logEnv("NLMSQSEQ", fmt.Sprintf("c.NlmsgSeq:%d", v), debugLevel)
	}
}

func envOverridePacket(c *xtcp_config.XtcpConfig, debugLevel uint) {
	if v, ok := envUint64("PACKET_SIZE"); ok {
		c.PacketSize = v
		logEnv("PACKET_SIZE", fmt.Sprintf("c.PacketSize:%d", v), debugLevel)
	}
	if v, ok := envUint32("PACKETSIZEMPLY"); ok {
		c.PacketSizeMply = v
		logEnv("PACKETSIZEMPLY", fmt.Sprintf("c.PacketSizeMply:%d", v), debugLevel)
	}
	if v, ok := envUint32("WRITEFILES"); ok {
		c.WriteFiles = v
		logEnv("WRITEFILES", fmt.Sprintf("c.WriteFiles:%d", v), debugLevel)
	}
	if v, ok := envString("CAPTUREPATH"); ok {
		c.CapturePath = v
		logEnv("CAPTUREPATH", fmt.Sprintf("c.CapturePath:%s", v), debugLevel)
	}
}

func envOverrideMarshalAndDest(c *xtcp_config.XtcpConfig, debugLevel uint) {
	if v, ok := envString("MARSHAL"); ok {
		c.MarshalTo = v
		logEnv("MARSHAL", fmt.Sprintf("c.Marshal:%s", v), debugLevel)
	}
	if _, ok := os.LookupEnv("PROTOBUF_LIST_LENGTH_DELIMIT"); ok {
		c.ProtobufListLengthDelimit = true
		logEnv("PROTOBUF_LIST_LENGTH_DELIMIT", fmt.Sprintf("c.ProtobufListLengthDelimit:%t", c.ProtobufListLengthDelimit), debugLevel)
	}
	if v, ok := envString("DEST"); ok {
		c.Dest = v
		logEnv("DEST", fmt.Sprintf("c.Dest:%s", v), debugLevel)
	}
	if v, ok := envUint32("DEST_WRITE_FILES"); ok {
		c.DestWriteFiles = v
		logEnv("DEST_WRITE_FILES", fmt.Sprintf("c.DestWriteFiles:%d", v), debugLevel)
	}
}

func envOverrideKafka(c *xtcp_config.XtcpConfig, debugLevel uint) {
	if v, ok := envString("TOPIC"); ok {
		c.Topic = v
		logEnv("TOPIC", fmt.Sprintf("c.Topic:%s", v), debugLevel)
	}
	if v, ok := envString("XTCP_PROTO_FILE"); ok {
		c.XtcpProtoFile = v
		logEnv("XTCP_PROTO_FILE", fmt.Sprintf("c.XtcpProtoFile:%s", v), debugLevel)
	}
	if v, ok := envString("KAFKA_SCHEMA_URL"); ok {
		c.KafkaSchemaUrl = v
		logEnv("KAFKA_SCHEMA_URL", fmt.Sprintf("c.KafkaSchemaUrl:%s", v), debugLevel)
	}
	if d, ok := envDuration("KAFKA_PRODUCE_TIMEOUT"); ok {
		c.KafkaProduceTimeout = durationpb.New(d)
		logEnv("KAFKA_PRODUCE_TIMEOUT", fmt.Sprintf("c.KafkaProduceTimeout:%s", c.KafkaProduceTimeout.AsDuration()), debugLevel)
	}
}

func envOverrideLabeling(c *xtcp_config.XtcpConfig, debugLevel uint) {
	if v, ok := envString("LABEL"); ok {
		c.Label = v
		logEnv("LABEL", fmt.Sprintf("c.Label:%s", v), debugLevel)
	}
	if v, ok := envString("TAG"); ok {
		c.Tag = v
		logEnv("TAG", fmt.Sprintf("c.Tag:%s", v), debugLevel)
	}
	if v, ok := envUint32("GRPC_PORT"); ok {
		c.GrpcPort = v
		logEnv("GRPC_PORT", fmt.Sprintf("c.GrpcPort:%d", v), debugLevel)
	}
}

func printConfig(c *xtcp_config.XtcpConfig, comment string) {
	fmt.Println(comment)
	fmt.Println("c.NlTimeoutMilliseconds:", c.NlTimeoutMilliseconds)
	fmt.Println("c.PollFrequency:", c.PollFrequency.AsDuration())
	fmt.Println("c.PollTimeout:", c.PollTimeout.AsDuration())
	fmt.Println("c.MaxLoops:", c.MaxLoops)
	fmt.Println("c.Netlinkers:", c.Netlinkers)
	fmt.Println("c.NlmsgSeq:", c.NlmsgSeq)
	fmt.Println("c.PacketSize:", c.PacketSize)
	fmt.Println("c.PacketSizeMply:", c.PacketSizeMply)
	fmt.Println("c.WriteFiles:", c.WriteFiles)
	fmt.Println("c.CapturePath:", c.CapturePath)
	fmt.Println("c.Modulus:", c.Modulus)
	fmt.Println("c.MarshalTo:", c.MarshalTo)
	fmt.Println("c.ProtobufListLengthDelimit:", c.ProtobufListLengthDelimit)
	fmt.Println("c.Dest:", c.Dest)
	fmt.Println("c.DestWriteFiles:", c.DestWriteFiles)
	fmt.Println("c.Topic:", c.Topic)
	fmt.Println("c.XtcpProtoFile:", c.XtcpProtoFile)
	fmt.Println("c.KafkaSchemaUrl:", c.KafkaSchemaUrl)
	fmt.Println("c.KafkaProduceTimeout:", c.KafkaProduceTimeout.AsDuration())
	fmt.Println("c.DebugLevel:", c.DebugLevel)
	fmt.Println("c.Label:", c.Label)
	fmt.Println("c.Tag:", c.Tag)
	fmt.Println("c.GrpcPort:", c.GrpcPort)
	fmt.Println("c.EnabledDeserializers:", c.EnabledDeserializers)
}

func getDeserializers(str string) *xtcp_config.EnabledDeserializers {

	key := "DESERIALIZERS"
	if value, exists := os.LookupEnv(key); exists {
		str = value
		if debugLevel > 10 {
			log.Printf("key:%s, str:%s", key, str)
		}
	}

	des := &xtcp_config.EnabledDeserializers{
		Enabled: make(map[string]bool),
	}

	if str == "all" {
		for _, item := range xtcp.GetAllDeserializers() {
			des.Enabled[item] = true
		}
		return des
	}

	if str == "" {
		return des
	}

	s := strings.Split(str, ",")
	for _, item := range s {
		des.Enabled[item] = true
	}

	return des
}
