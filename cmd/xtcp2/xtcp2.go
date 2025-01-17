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

	//protovalidate "github.com/bufbuild/protovalidate-go"
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
	debugLevelCst = 11

	signalChannelSizeCst = 10
	cancelSleepTimeCst   = 5 * time.Second

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
	// destCst = "udp:127.0.0.1:13000"
	// destCst = "nsq:nsqd:4150"
	// destCst = "nats:nats:8222"
	// destCst = "valkey:valkey:6379"
	// destCst = "null"

	topicCst = "xtcp"

	kafkaProduceTimeoutCst = 0 * time.Second

	labelCst = ""
	tagCst   = ""

	grpcPortCst = 8888

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

	nltimeout := flag.Uint64("nltimeout", 1000, "Netlink socket timeout in milliseconds.  Zero(0) for no timeout")
	pollFrequency := flag.Duration("frequency", 10*time.Second, "Poll frequency")
	pollTimeout := flag.Duration("timeout", 5*time.Second, "Poll timeout per name space")
	maxLoops := flag.Uint64("maxLoops", 0, "Maximum number of loops, or zero (0) for forever")
	netlinkers := flag.Uint("netlinkers", 2, "netlinkers which read netlink messages from each socket. increase this if you have many flows")
	nlmsgSeq := flag.Uint("nlmsgSeq", 666, "nlmsgSeq sequence number (start), which should be uint32")
	// packetSize of the buffer the netlinkers syscall.Recvfrom to read into
	packetSize := flag.Uint64("packetSize", 0, "netlinker packetSize.  buffer size = packetSize * packetSizeMply. Use zero (0) for syscall.Getpagesize()")
	packetSizeMply := flag.Uint("packetSizeMply", 8, "netlinker packetSize multiplier.  buffer size = packetSize * packetSizeMply")

	writeFiles := flag.Uint("writeFiles", WriteFilesCst, "Write netlink packets to writeFiles number of files ( to generate test data ) per netlinker")
	capturePath := flag.String("capturePath", capturePathCst, "Write files path")

	modulus := flag.Uint64("modulus", modulusCst, "modulus. Report every X inetd messages to output")
	marshal := flag.String("marshal", marshalCst, "Marshalling of the exported data (proto,json,prototext)")
	dest := flag.String("dest", destCst, "kafka:127.0.0.1:9092, udp:127.0.0.1:13000, or nsq:127.0.0.1:4150")
	topic := flag.String("topic", topicCst, "Kafka or NSQ topic")
	produceTimeout := flag.Duration("produceTimeout", kafkaProduceTimeoutCst, "Kafka produce timeout (context.WithTimeout)")
	label := flag.String("label", labelCst, "label applied to the protobuf")
	tag := flag.String("tag", tagCst, "label applied to the protobuf")

	grpcPort := flag.Uint("grpcPort", grpcPortCst, "GRPC listening port")

	deserializers := flag.String("deserializers", "info,cong", "Comma seperated list of deserializers")
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

	flag.Parse()

	// Print version information passed in via ldflags in the Makefile
	if *v {
		log.Printf("xtcp commit:%s\tdate(UTC):%s\tversion:%s", commit, date, version)
		os.Exit(0)
	}

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
		fmt.Println("*dest:", *dest)
		fmt.Println("*topic:", *topic)
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
		PollFrequency:        durationpb.New(*pollFrequency),
		PollTimeout:          durationpb.New(*pollTimeout),
		MaxLoops:             *maxLoops,
		Netlinkers:           uint32(*netlinkers),
		NlmsgSeq:             uint32(*nlmsgSeq),
		PacketSize:           *packetSize,
		PacketSizeMply:       uint32(*packetSizeMply),
		WriteFiles:           uint32(*writeFiles),
		CapturePath:          *capturePath,
		Modulus:              *modulus,
		MarshalTo:            *marshal,
		Dest:                 *dest,
		Topic:                *topic,
		KafkaProduceTimeout:  durationpb.New(*produceTimeout),
		DebugLevel:           uint32(*d),
		Label:                *label,
		Tag:                  *tag,
		GrpcPort:             uint32(*grpcPort),
		EnabledDeserializers: des,
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

	environmentOverrideProm(promPath, promListen, debugLevel)
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
		err := http.ListenAndServe(promListen, nil)
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
		promListen = &value
		if debugLevel > 10 {
			log.Printf("key:%s, c.PromListen:%s", key, *promListen)
		}
	}

	key = "PROM_PATH"
	if value, exists := os.LookupEnv(key); exists {
		promPath = &value
		if debugLevel > 10 {
			log.Printf("key:%s, c.PromListen:%s", key, *promPath)
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
	key := "NLTIMEOUTMS"
	if value, exists := os.LookupEnv(key); exists {
		if i, err := strconv.ParseInt(value, base10, sixtyFour); err == nil {
			c.NlTimeoutMilliseconds = uint64(i)
			if debugLevel > 10 {
				log.Printf("key:%s, c.NlTimeoutMilliseconds:%d", key, c.NlTimeoutMilliseconds)
			}
		}
	}

	key = "POLL_FREQUENCY"
	if value, exists := os.LookupEnv(key); exists {
		if d, err := time.ParseDuration(value); err == nil {
			c.PollFrequency = durationpb.New(d)
			if debugLevel > 10 {
				log.Printf("key:%s, c.PollingFrequency:%s", key, c.PollFrequency.String())
			}
		}
	}

	key = "POLL_TIMEOUT"
	if value, exists := os.LookupEnv(key); exists {
		if d, err := time.ParseDuration(value); err == nil {
			c.PollTimeout = durationpb.New(d)
			if debugLevel > 10 {
				log.Printf("key:%s, c.PollingFrequency:%s", key, c.PollTimeout.String())
			}
		}
	}

	key = "MAX_LOOPS"
	if value, exists := os.LookupEnv(key); exists {
		if i, err := strconv.ParseInt(value, base10, sixtyFour); err == nil {
			c.MaxLoops = uint64(i)
			if debugLevel > 10 {
				log.Printf("key:%s, c.MaxLoops:%d", key, c.MaxLoops)
			}
		}
	}

	key = "NETLINKERS"
	if value, exists := os.LookupEnv(key); exists {
		if i, err := strconv.Atoi(value); err == nil {
			c.Netlinkers = uint32(i)
			if debugLevel > 10 {
				log.Printf("key:%s, c.Netlinkers:%d", key, c.Netlinkers)
			}
		}
	}

	key = "NLMSQSEQ"
	if value, exists := os.LookupEnv(key); exists {
		if i, err := strconv.Atoi(value); err == nil {
			c.NlmsgSeq = uint32(i)
			if debugLevel > 10 {
				log.Printf("key:%s, c.NlmsgSeq:%d", key, c.NlmsgSeq)
			}
		}
	}

	key = "PACKET_SIZE"
	if value, exists := os.LookupEnv(key); exists {
		if i, err := strconv.ParseInt(value, base10, sixtyFour); err == nil {
			c.PacketSize = uint64(i)
			if debugLevel > 10 {
				log.Printf("key:%s, c.PacketSize:%d", key, c.PacketSize)
			}
		}
	}

	key = "PACKETSIZEMPLY"
	if value, exists := os.LookupEnv(key); exists {
		if i, err := strconv.Atoi(value); err == nil {
			c.PacketSizeMply = uint32(i)
			if debugLevel > 10 {
				log.Printf("key:%s, c.PacketSizeMply:%d", key, c.PacketSizeMply)
			}
		}
	}

	key = "WRITEFILES"
	if value, exists := os.LookupEnv(key); exists {
		if i, err := strconv.Atoi(value); err == nil {
			c.WriteFiles = uint32(i)
			if debugLevel > 10 {
				log.Printf("key:%s, c.WriteFiles:%d", key, c.WriteFiles)
			}
		}
	}

	key = "CAPTUREPATH"
	if value, exists := os.LookupEnv(key); exists {
		c.CapturePath = value
		if debugLevel > 10 {
			log.Printf("key:%s, c.CapturePath:%s", key, c.CapturePath)
		}
	}

	key = "MODULUS"
	if value, exists := os.LookupEnv(key); exists {
		if i, err := strconv.ParseInt(value, base10, sixtyFour); err == nil {
			c.Modulus = uint64(i)
			if debugLevel > 10 {
				log.Printf("key:%s, c.Modulus:%d", key, c.Modulus)
			}
		}
	}

	key = "MARSHAL"
	if value, exists := os.LookupEnv(key); exists {
		c.MarshalTo = value
		if debugLevel > 10 {
			log.Printf("key:%s, c.Marshal:%s", key, c.MarshalTo)
		}
	}

	key = "DEST"
	if value, exists := os.LookupEnv(key); exists {
		c.Dest = value
		if debugLevel > 10 {
			log.Printf("key:%s, c.Dest:%s", key, c.Dest)
		}
	}

	key = "TOPIC"
	if value, exists := os.LookupEnv(key); exists {
		c.Topic = value
		if debugLevel > 10 {
			log.Printf("key:%s, c.Topic:%s", key, c.Topic)
		}
	}

	key = "KAFKA_PRODUCE_TIMEOUT"
	if value, exists := os.LookupEnv(key); exists {
		if d, err := time.ParseDuration(value); err == nil {
			c.KafkaProduceTimeout = durationpb.New(d)
			if debugLevel > 10 {
				log.Printf("key:%s, c.KafkaProduceTimeout:%s", key, c.KafkaProduceTimeout.AsDuration())
			}
		}
	}

	key = "LABEL"
	if value, exists := os.LookupEnv(key); exists {
		c.Label = value
		if debugLevel > 10 {
			log.Printf("key:%s, c.Label:%s", key, c.Label)
		}
	}

	key = "TAG"
	if value, exists := os.LookupEnv(key); exists {
		c.Tag = value
		if debugLevel > 10 {
			log.Printf("key:%s, c.Tag:%s", key, c.Tag)
		}
	}

	key = "GRPC_PORT"
	if value, exists := os.LookupEnv(key); exists {
		if i, err := strconv.Atoi(value); err == nil {
			c.GrpcPort = uint32(i)
			if debugLevel > 10 {
				log.Printf("key:%s, c.GrpcPort:%d", key, c.GrpcPort)
			}
		}
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
	fmt.Println("c.Dest:", c.Dest)
	fmt.Println("c.Topic:", c.Topic)
	fmt.Println("c.KafkaProduceTimeout:", c.KafkaProduceTimeout.AsDuration())
	fmt.Println("c.KafkaProduceTimeout:", c.KafkaProduceTimeout.AsDuration())
	fmt.Println("c.DebugLevel:", c.DebugLevel)
	fmt.Println("c.Label:", c.Label)
	fmt.Println("c.Tag:", c.Tag)
	fmt.Println("c.GrpcPort:", c.GrpcPort)
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

	s := strings.Split(str, ",")
	for _, item := range s {
		des.Enabled[item] = true
	}

	return des
}
