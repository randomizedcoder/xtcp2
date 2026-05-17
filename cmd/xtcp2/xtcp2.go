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
func main() {

	misc.DieIfNotLinux()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	complete := make(chan struct{}, signalChannelSizeCst)
	go initSignalHandler(cancel, complete)

	nltimeout := flag.Uint64("nltimeout", nltimeoutCst, "Netlink socket timeout in milliseconds.  Zero(0) for no timeout")
	pollFrequency := flag.Duration("frequency", pollFrequencyCst, "Poll frequency")
	pollTimeout := flag.Duration("timeout", pollTimeoutCst, "Poll timeout per name space")
	maxLoops := flag.Uint64("maxLoops", maxLoopsCst, "Maximum number of loops, or zero (0) for forever")
	netlinkers := flag.Uint("netlinkers", netlinkersCst, "netlinkers which read netlink messages from each socket. increase this if you have many flows")
	nlmsgSeq := flag.Uint("nlmsgSeq", nlmsgSeqCst, "nlmsgSeq sequence number (start), which should be uint32")
	// packetSize of the buffer the netlinkers syscall.Recvfrom to read into
	packetSize := flag.Uint64("packetSize", packetSizeCst, "netlinker packetSize.  buffer size = packetSize * packetSizeMply. Use zero (0) for syscall.Getpagesize()")
	packetSizeMply := flag.Uint("packetSizeMply", packetSizeMplyCst, "netlinker packetSize multiplier.  buffer size = packetSize * packetSizeMply")

	writeFiles := flag.Uint("writeFiles", WriteFilesCst, "Write netlink packets to writeFiles number of files ( to generate test data ) per netlinker")
	capturePath := flag.String("capturePath", capturePathCst, "Write files path")

	modulus := flag.Uint64("modulus", modulusCst, "modulus. Report every X inetd messages to output")
	marshal := flag.String("marshal", marshalCst, "Marshaling of the exported data (protobufList, protoJson, protoText, msgpack)")
	protobufListLengthDelimit := flag.Bool("protobufListLengthDelimit", protobufListLengthDelimitCst, "protobufListLengthDelimit")
	dest := flag.String("dest", destCst, "kafka:127.0.0.1:9092, udp:127.0.0.1:13000, or nsq:127.0.0.1:4150")
	destWriteFiles := flag.Uint("destWriteFiles", DestWriteFilesCst, "Write out the marshaled data to destWriteFiles number of files ( for debugging only )")
	topic := flag.String("topic", topicCst, "Kafka or NSQ topic")
	xtcpProtoFile := flag.String("xtcpProtoFile", xtcpProtoFileCst, "xtcpProtoFile for registering with the schema registry")
	kafkaSchemaUrl := flag.String("kafkaSchemaUrl", kafkaSchemaUrlCst, "kafka schema registry URL")
	produceTimeout := flag.Duration("produceTimeout", kafkaProduceTimeoutCst, "Kafka produce timeout (context.WithTimeout)")
	label := flag.String("label", labelCst, "label applied to the protobuf")
	tag := flag.String("tag", tagCst, "label applied to the protobuf")

	grpcPort := flag.Uint("grpcPort", grpcPortCst, "GRPC listening port")

	deserializers := flag.String("deserializers", deserializersCst, fmt.Sprintf("Comma separated list of deserializers,%v", xtcp.GetAllDeserializers()))
	// deserializers := flag.String("deserializers", "info", "Comma separated list of deserializers")
	// deserializers := flag.String("deserializers", "info,cong", "Comma separated list of deserializers")
	// meminfo := flag.Bool("meminfo", false, "meminfo")
	// info := flag.Bool("info", false, "info")
	// vegas := flag.Bool("vegas", false, "vegas")
	// cong := flag.Bool("cong", false, "cong")
	// tos := flag.Bool("tos", false, "tos")
	// tc := flag.Bool("tc", false, "tc")

	promListen := flag.String("promListen", promListenCst, "Prometheus http listening socket")
	promPath := flag.String("promPath", promPathCst, "Prometheus http path")
	// curl -s http://[::1]:9000/metrics 2>&1 | grep -v "#"
	// curl -s http://127.0.0.1:9000/metrics 2>&1 | grep -v "#"

	// Maximum number of CPUs that can be executing simultaneously
	// https://golang.org/pkg/runtime/#GOMAXPROCS -> zero (0) means default
	goMaxProcs := flag.Uint("goMaxProcs", 4, "goMaxProcs = https://golang.org/pkg/runtime/#GOMAXPROCS")

	// ./xtcp2 --profile.mode cpu
	// timeout 1h ./xtcp2 --profile.mode cpu
	profileMode := flag.String("profile.mode", "", "enable profiling mode, one of [cpu, mem, mutex, block]")

	v := flag.Bool("v", false, "show version")

	conf := flag.Bool("conf", false, "show config")

	d := flag.Uint("d", debugLevelCst, "debug level")

	ioUring := flag.Bool("ioUring", false, "Opt in to io_uring for netlink reads and raw-socket destination writes (Linux 6.1+)")
	ioUringRecvBatch := flag.Uint("ioUringRecvBatch", 64, "io_uring recvmsg SQEs kept in flight per Netlinker (1-4096). Higher reduces syscalls on high-fanout hosts.")
	ioUringCqeBatch := flag.Uint("ioUringCqeBatch", 128, "io_uring max CQEs reaped per PeekBatchCQE call (1-4096)")

	flag.Parse()

	// Print version information passed in via ldflags in the Makefile
	if *v {
		log.Printf("xtcp commit:%s\tdate(UTC):%s\tversion:%s", commit, date, version)
		os.Exit(0) //nolint:gocritic // exitAfterDefer: -v prints version and exits; deferred cancel() is moot at process shutdown
	}

	environmentOverrideDebugLevel(d, *d)

	debugLevel = *d

	if debugLevel > 10 {
		fmt.Println("*nltimeout(ms):", *nltimeout)
		fmt.Println("*pollFrequency:", *pollFrequency)
		fmt.Println("*pollTimeout:", *pollTimeout)
		fmt.Println("*maxLoops:", *maxLoops)
		fmt.Println("*netlinkers:", *netlinkers)
		fmt.Println("*nlmsgSeq:", *nlmsgSeq)
		fmt.Println("*packetSize:", *packetSize)
		fmt.Println("*packetSizeMply:", *packetSizeMply)
		fmt.Println("*writeFiles:", *writeFiles)
		fmt.Println("*capturePath:", *capturePath)
		fmt.Println("*modulus:", *modulus)
		fmt.Println("*marshal:", *marshal)
		fmt.Println("*protobufListLengthDelimit:", *protobufListLengthDelimit)
		fmt.Println("*dest:", *dest)
		fmt.Println("*destWriteFiles:", *destWriteFiles)
		fmt.Println("*topic:", *topic)
		fmt.Println("*xtcpProtoFile:", *xtcpProtoFile)
		fmt.Println("*kafkaSchemaUrl:", *kafkaSchemaUrl)
		fmt.Println("*produceTimeout:", *produceTimeout)
		fmt.Println("*promListen:", *promListen)
		fmt.Println("*promPath:", *promPath)
		fmt.Println("*goMaxProcs:", *goMaxProcs)
		fmt.Println("*d:", *d)
	}

	des := getDeserializers(*deserializers)

	c := &xtcp_config.XtcpConfig{
		NlTimeoutMilliseconds: *nltimeout,
		// https://pkg.go.dev/google.golang.org/protobuf/types/known/durationpb
		PollFrequency:          durationpb.New(*pollFrequency),
		PollTimeout:            durationpb.New(*pollTimeout),
		MaxLoops:               *maxLoops,
		Netlinkers:             uint32(*netlinkers),
		NetlinkersDoneChanSize: netlinkerDoneChSizeCst,
		NlmsgSeq:               uint32(*nlmsgSeq),
		PacketSize:             *packetSize,
		PacketSizeMply:         uint32(*packetSizeMply),
		WriteFiles:             uint32(*writeFiles),
		CapturePath:            *capturePath,
		Modulus:                *modulus,
		MarshalTo:              *marshal,
		Dest:                   *dest,
		DestWriteFiles:         uint32(*destWriteFiles),
		Topic:                  *topic,
		XtcpProtoFile:          *xtcpProtoFile,
		KafkaSchemaUrl:         *kafkaSchemaUrl,
		KafkaProduceTimeout:    durationpb.New(*produceTimeout),
		DebugLevel:             uint32(*d),
		Label:                  *label,
		Tag:                    *tag,
		GrpcPort:               uint32(*grpcPort),
		EnabledDeserializers:   des,

		IoUring:              *ioUring,
		IoUringRecvBatchSize: uint32(*ioUringRecvBatch),
		IoUringCqeBatchSize:  uint32(*ioUringCqeBatch),
	}

	if debugLevel > 100 {
		printConfig(c, "Before environmentOverrideConfig")
	}

	environmentOverrideConfig(c, debugLevel)

	if debugLevel > 100 {
		printConfig(c, "After environmentOverrideConfig")
	}

	if *conf {
		printConfig(c, "conf argument")
		os.Exit(0)
	}

	environmentOverrideGoMaxProcs(goMaxProcs, debugLevel)
	if runtime.NumCPU() > int(*goMaxProcs) {
		mp := runtime.GOMAXPROCS(int(*goMaxProcs))
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
		if debugLevel > 1000 {
			log.Println("No profiling")
		}
	}

	environmentOverrideProm(promListen, promPath, debugLevel)
	go initPromHandler(*promPath, *promListen)
	if debugLevel > 10 {
		log.Println("Prometheus http listener started on:", *promListen, *promPath)
	}

	if err := protovalidate.Validate(c); err != nil {
		log.Fatal("config validation failed:", err)
	}

	if debugLevel > 10 {
		log.Println("config validation succeeded")
	}

	xtcp := xtcp.NewXTCP(ctx, cancel, c)

	if debugLevel > 10 {
		log.Println("xtcp.Run(ctx, &wg)")
	}

	var wg sync.WaitGroup
	wg.Add(1)
	xtcp.RunWithPoller(ctx, &wg)

	if debugLevel > 10 {
		log.Println("xtcp.Run(ctx) complete. wg.Wait()")
	}

	wg.Wait()
	complete <- struct{}{}

	if debugLevel > 10 {
		log.Println("xtcp2.go Main complete - farewell")
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

	log.Printf("Signal caught, cancel() called, and sleeping to allow goroutines to close, sleeping:%s",
		cancelSleepTimeCst.String())
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
// https://pkg.go.dev/github.com/prometheus/client_golang/prometheus/promhttp?tab=doc#HandlerOpts
func initPromHandler(promPath string, promListen string) {
	http.Handle(promPath, promhttp.HandlerFor(
		prometheus.DefaultGatherer,
		promhttp.HandlerOpts{
			EnableOpenMetrics:   promEnableOpenMetrics,
			MaxRequestsInFlight: promMaxRequestsInFlight,
		},
	))
	go func() {
		srv := &http.Server{
			Addr:              promListen,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       10 * time.Second,
			WriteTimeout:      10 * time.Second,
			IdleTimeout:       30 * time.Second,
		}
		err := srv.ListenAndServe()
		if err != nil {
			log.Fatal("prometheus error", err)
		}
	}()
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

// environmentOverrideDebugLevel MUTATES d if env var is set
func environmentOverrideDebugLevel(d *uint, debugLevel uint) {
	key := "DEBUG_LEVEL"
	if value, exists := os.LookupEnv(key); exists {
		if i, err := strconv.Atoi(value); err == nil {
			*d = uint(i)
			if debugLevel > 10 {
				log.Printf("key:%s, d:%d", key, d)
			}
		}
	}
}

// environmentOverrideGoMaxProcs MUTATES goMaxProcs if env var is set
func environmentOverrideGoMaxProcs(goMaxProcs *uint, debugLevel uint) {
	key := "GOMAXPROCS"
	if value, exists := os.LookupEnv(key); exists {
		if i, err := strconv.Atoi(value); err == nil {
			*goMaxProcs = uint(i)
			if debugLevel > 10 {
				log.Printf("key:%s, goMaxProcs:%d", key, goMaxProcs)
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
	i, err := strconv.ParseInt(v, base10, sixtyFour)
	if err != nil {
		return 0, false
	}
	return uint64(i), true
}

// envUint32 parses an env var as decimal int and yields it as uint32.
func envUint32(key string) (uint32, bool) {
	v, ok := os.LookupEnv(key)
	if !ok {
		return 0, false
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return 0, false
	}
	return uint32(i), true
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
