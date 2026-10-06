package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/pprof" // registered explicitly on the prom mux in initPromHandler (see there)
	"os"
	"os/signal"
	"runtime"
	runtimeDebug "runtime/debug"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/bufbuild/protovalidate-go"
	"github.com/grafana/pyroscope-go"
	"github.com/pkg/profile"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
	"github.com/randomizedcoder/xtcp2/pkg/health"
	"github.com/randomizedcoder/xtcp2/pkg/listener"
	"github.com/randomizedcoder/xtcp2/pkg/listenerauth"
	"github.com/randomizedcoder/xtcp2/pkg/misc"
	"github.com/randomizedcoder/xtcp2/pkg/xtcp"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
)

const (
	debugLevelCst = 111

	signalChannelSizeCst = 10
	cancelSleepTimeCst   = 5 * time.Second

	// reconfigureEnvKey carries a full XtcpConfig (protojson) across a
	// soft-restart syscall.Exec. When set at startup it wins over flags/env.
	reconfigureEnvKey = "XTCP_CONFIG_JSON"
	// reconfigureGraceDelay is how long the reconfigure hook waits before
	// canceling the run context, so the gRPC Set response flushes to the
	// client before graceful shutdown + re-exec begin.
	reconfigureGraceDelay = 200 * time.Millisecond

	promListenCst                = ":9088" // [::1]:9088
	promPathCst                  = "/metrics"
	promMaxRequestsInFlight      = 10
	promEnableOpenMetrics        = true
	listenNetworkCst             = ""
	unixSocketModeCst       uint = 0o600
	unlinkStaleSocketCst         = true

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

	// protobufList (Kafka path, length-delimited Envelope), protoJson,
	// protoText, msgpack (per-record debug formats).
	marshalCst = "protobufList"

	// envelopeFlushBytesCst caps the in-flight protobufList Envelope's
	// uncompressed marshaled byte size before deserialize.go triggers
	// an early mid-poll flush. Use as a safety net against pathological
	// per-record sizes; for everyday batch sizing prefer the row cap
	// below. 0 = use daemon's compile-time default
	// (EnvelopeFlushThresholdBytesCst, currently 768 KiB).
	envelopeFlushBytesCst = 0

	// envelopeFlushRowsCst caps the in-flight Envelope's row count
	// before deserialize.go triggers an early mid-poll flush. Cheap
	// (O(1) per append) and predictable. Whichever cap (rows or bytes)
	// trips first wins. 0 = use daemon's compile-time default
	// (EnvelopeFlushThresholdRowsCst, currently 10000 — aligned with
	// ClickHouse's kafka_max_rows_per_message).
	envelopeFlushRowsCst = 0

	// kafkaCompressionCst chooses the producer-batch compression codec.
	// Default "" → franz-go's broker-negotiated preference list (zstd
	// preferred). Explicit codec name pins one. See xtcp_config.proto
	// for the full menu; resolveKafkaCompression in destinations_kafka.go
	// validates the value at startup.
	kafkaCompressionCst = ""

	// s3parquet destination defaults. All empty/zero by default — only
	// kick in when -dest is s3parquet:... and the operator sets these
	// via flag or env. Picked up by the dest_s3parquet build-tagged
	// destination; on a binary built without -tags dest_s3parquet
	// these fields are wired through harmlessly.
	s3EndpointCst                        = ""
	s3BucketCst                          = ""
	s3PrefixCst                          = ""
	s3AccessKeyCst                       = ""
	s3SecretKeyCst                       = ""
	s3RegionCst                          = ""
	s3SkipBucketProbeCst                 = false
	s3ParquetFlushThresholdBytesCst uint = 0

	// Fleet jitter & upload backoff defaults (thundering-herd avoidance). See
	// docs/design-jitter-and-backoff.md. A 0 pct disables the corresponding
	// jitter (restoring deterministic behavior); the two 0-valued durations
	// mean "derive from poll_frequency" (see the s3parquet destination).
	pollJitterPctCst             uint = 20
	s3FlushIntervalCst                = 0 * time.Second
	s3FlushJitterPctCst          uint = 20
	s3FlushThresholdJitterPctCst uint = 20
	s3UploadMaxAttemptsCst       uint = 10
	s3UploadBackoffCapCst             = 0 * time.Second

	// Namespace-reconcile cadence defaults (Method B /proc-scan discovery).
	// reconcileBeforePoll ties discovery to poll cadence (reconcile before each
	// poll) and is the real discovery mechanism. The background reconcile is now
	// just an occasional safety-net / confirmation: with the pre-poll reconcile
	// carrying discovery, it is expected to find nothing (mapReconciler dels/
	// stores stay 0 in Prometheus), so the default is deliberately long — 6h,
	// just enough to catch a hypothetical missed namespace and to let operators
	// confirm from the counters that the background pass is redundant. 0 disables
	// it entirely (the startup reconcile still runs once).
	reconcileFrequencyCst  = 6 * time.Hour
	reconcileBeforePollCst = true

	// Pyroscope continuous-profiling defaults. Agent disabled when
	// pyroscopeUrlCst is empty; flip on via -pyroscopeUrl (or
	// PYROSCOPE_URL env, see environmentOverride).
	pyroscopeUrlCst            = ""
	pyroscopeAppNameCst        = "xtcp2"
	pyroscopeSampleHzCst  uint = 100
	pyroscopeUploadSecCst uint = 15

	// destCst is the `-dest` fallback used when the binary compiles in
	// several library destinations (the "full" flavor) — kafka is the
	// historical default. A binary with exactly one library destination
	// defaults to that destination instead; one with none defaults to
	// stdoutDestCst. See defaultDest.
	destCst = "kafka:redpanda-0:9092"
	// stdoutDestCst is the `-dest` default for the "min" flavor (no library
	// destinations compiled in): an always-compiled sink so the binary boots
	// without the operator having to pass -dest.
	stdoutDestCst = "stdout"
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

	locationCst           = ""
	hostnameCst           = ""
	resolveContainerIdCst = false

	// Best-effort metadata enrichment (all off by default; a failed
	// socket/sysfs read logs + bumps a counter and leaves the column empty).
	// Socket-path defaults match the fleet (see the enrichment spec).
	enrichContainerCst       = false
	dockerSocketCst          = "/run/docker.sock"
	enrichLldpCst            = false
	lldpdSocketCst           = "/run/lldpd.socket"
	lldpdVersionHintCst      = ""
	enrichNicCst             = false
	uplinkCountCst      uint = 2
	// uplinkInterfacesCst is a comma-separated override for the uplink NIC
	// selection; empty = auto-detect from the default routes.
	uplinkInterfacesCst = ""
	populateNsidCst     = false
	enrichLocalityCst   = false
	// localityRefreshIntervalCst throttles the full per-namespace rtnetlink
	// re-discovery: every namespace is re-dumped at most this often on the
	// reconcile path so interface/route changes are picked up within a minute.
	// New namespaces are dumped on the next reconcile regardless, and failed or
	// loopback-only namespaces retry on their own 30s-5m backoff. 0 = discover
	// each namespace once and never refresh (static topologies only).
	localityRefreshIntervalCst = 60 * time.Second
	// ASN enrichment is off by default and the artifact path has no sensible
	// default. asnRefreshIntervalCst re-stats the artifact hourly and reloads it
	// only when its size/mtime changed, so a refreshed — or late-arriving —
	// ipfeed-collector file is picked up without a restart. 0 = load once.
	enrichAsnCst           = false
	asnDbPathCst           = ""
	asnRefreshIntervalCst  = 1 * time.Hour
	ipmetaBootstrapPathCst = "/share/xtcp2/ipmeta/bootstrap.lookup.parquet.zst"
	ipmetaCachePathCst     = "/var/lib/xtcp2/ipmeta/current.lookup.parquet.zst"

	ipv4TtlCst      uint = 0
	ipv6HopLimitCst uint = 0

	// "default" enables every deserializer except the off-by-default ones
	// (meminfo, redundant with sk_mem_info). Use "all" to force everything.
	deserializersCst = "default"

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
	nltimeout           *uint64
	pollFrequency       *time.Duration
	pollTimeout         *time.Duration
	maxLoops            *uint64
	netlinkers          *uint
	nlmsgSeq            *uint
	packetSize          *uint64
	packetSizeMply      *uint
	writeFiles          *uint
	capturePath         *string
	modulus             *uint64
	marshal             *string
	columns             *string
	envelopeFlushBytes  *uint
	envelopeFlushRows   *uint
	kafkaCompression    *string
	s3Endpoint          *string
	s3Bucket            *string
	s3Prefix            *string
	s3AccessKey         *string
	s3SecretKey         *string
	s3Region            *string
	s3SkipBucketProbe   *bool
	s3ParquetFlushBytes *uint

	pollJitterPct             *uint
	s3FlushInterval           *time.Duration
	s3FlushJitterPct          *uint
	s3FlushThresholdJitterPct *uint
	s3UploadMaxAttempts       *uint
	s3UploadBackoffCap        *time.Duration

	reconcileFrequency  *time.Duration
	reconcileBeforePoll *bool

	dest               *string
	destWriteFiles     *uint
	topic              *string
	xtcpProtoFile      *string
	kafkaSchemaUrl     *string
	produceTimeout     *time.Duration
	label              *string
	tag                *string
	location           *string
	hostname           *string
	resolveContainerId *bool

	enrichContainer  *bool
	dockerSocket     *string
	enrichLldp       *bool
	lldpdSocket      *string
	lldpdVersionHint *string
	enrichNic        *bool
	uplinkCount      *uint
	uplinkInterfaces *string
	populateNsid     *bool

	enrichAsn           *bool
	asnDbPath           *string
	asnRefreshInterval  *time.Duration
	ipmetaBootstrapPath *string
	ipmetaCachePath     *string

	enrichLocality          *bool
	localityRefreshInterval *time.Duration

	ipv4Ttl            *uint
	ipv6HopLimit       *uint
	listenerAuthMode   *string
	listenerRawToken   *string
	listenerHMACKey    *string
	listenerSignedSkew *uint
	listenerJitterMin  *time.Duration
	listenerJitterMax  *time.Duration
	grpcPort           *uint
	grpcListenNetwork  *string
	grpcListenAddress  *string
	grpcUnixSocketMode *uint
	grpcUnlinkStaleUDS *bool
	deserializers      *string
	promListen         *string
	promListenNetwork  *string
	promUnixSocketMode *uint
	promUnlinkStaleUDS *bool
	promPath           *string
	healthcheck        *bool
	goMaxProcs         *uint
	maxThreads         *int
	profileMode        *string
	pyroscopeUrl       *string
	pyroscopeAppName   *string
	pyroscopeSampleHz  *uint
	pyroscopeUploadSec *uint
	v                  *bool
	conf               *bool
	d                  *uint
	ioUring            *bool
	ioUringRecvBatch   *uint
	ioUringCqeBatch    *uint
}

// defaultDestFor picks the `-dest` default from the library destinations
// compiled into this binary: the sole one's registered default when exactly
// one is present, stdoutDestCst when none are (the "min" flavor), and destCst
// (kafka) when several are (the "full" flavor). lookup resolves a scheme to
// its registered default (xtcp.LibraryDefaultDest in production; a stub in
// tests). Kept pure so the branching is unit-testable without global state.
func defaultDestFor(libs []string, lookup func(string) string) string {
	switch len(libs) {
	case 1:
		return lookup(libs[0])
	case 0:
		return stdoutDestCst
	default:
		return destCst
	}
}

// enricherBuildNote is the sentence appended to a compile-time-gated
// enricher's flag usage so `-help` describes the artifact in front of the
// operator. Without it a slim image advertises ASN/locality enrichment it
// physically cannot perform, and the operator only finds out when the daemon
// refuses to start.
func enricherBuildNote(name string) string {
	if xtcp.EnricherCompiledIn(name) {
		return " COMPILED IN to this binary."
	}
	return " NOT COMPILED IN to this binary: setting this flag is a startup error; " +
		"rebuild with -tags enrich_" + name + " or use an image whose tag carries the enrichment flavor."
}

// defaultDest is the `-dest` flag default, derived from the destinations
// linked into this binary. init() funcs in pkg/xtcp populate the registry
// before defineFlags runs, so the compiled-in set is known here. An explicit
// -dest flag or DEST env var still overrides this (it is only the default).
func defaultDest() string {
	return defaultDestFor(xtcp.CompiledInLibrarySchemes(), xtcp.LibraryDefaultDest)
}

// defineFlags registers all 92 CLI flags on the global flagset and returns the
// pointer bundle main() reads them through.
//
// It was 79 statements, over funlen's 70. Four helpers now carry 42 of them -
// defineS3Flags, defineListenerFlags, defineRuntimeFlags and the pre-existing
// defineEnrichmentFlags - leaving 40 here. Each helper takes *mainFlags,
// mutates in place and returns nothing, which is defineEnrichmentFlags'
// established shape.
//
// Every helper must use the package-level flag.X functions rather than a local
// flagset: cmd/xtcp2 registers on flag.CommandLine, so a helper with its own
// flagset would silently stop registering anything. The name, default and usage
// of all 92 are pinned by cmd/xtcp2/testdata/defineflags-golden.txt.
func defineFlags() *mainFlags {
	f := &mainFlags{}
	f.nltimeout = flag.Uint64("nltimeout", nltimeoutCst, "Netlink socket timeout in milliseconds.  Zero(0) for no timeout")
	f.pollFrequency = flag.Duration("frequency", pollFrequencyCst, "Poll frequency")
	f.pollTimeout = flag.Duration("timeout", pollTimeoutCst, "Poll timeout per name space")
	f.pollJitterPct = flag.Uint("pollJitterPct", pollJitterPctCst, "Poll-schedule jitter as a percent (0-100) of -frequency, applied to the startup delay and each tick to avoid a synchronized fleet. 0 disables (immediate first poll, fixed interval).")
	f.maxLoops = flag.Uint64("maxLoops", maxLoopsCst, "Maximum number of loops, or zero (0) for forever")
	f.netlinkers = flag.Uint("netlinkers", netlinkersCst, "netlinkers which read netlink messages from each socket. increase this if you have many flows")
	f.nlmsgSeq = flag.Uint("nlmsgSeq", nlmsgSeqCst, "nlmsgSeq sequence number (start), which should be uint32")
	// packetSize of the buffer the netlinkers syscall.Recvfrom to read into
	f.packetSize = flag.Uint64("packetSize", packetSizeCst, "netlinker packetSize.  buffer size = packetSize * packetSizeMply. Use zero (0) for syscall.Getpagesize()")
	f.packetSizeMply = flag.Uint("packetSizeMply", packetSizeMplyCst, "netlinker packetSize multiplier.  buffer size = packetSize * packetSizeMply")
	f.writeFiles = flag.Uint("writeFiles", WriteFilesCst, "Write netlink packets to writeFiles number of files ( to generate test data ) per netlinker")
	f.capturePath = flag.String("capturePath", capturePathCst, "Write files path")
	f.modulus = flag.Uint64("modulus", modulusCst, "modulus. Report every X inetd messages to output")
	f.marshal = flag.String("marshal", marshalCst, "Marshaling of the exported data (protobufList, protoJson, protoText, msgpack, jsonl, csv, tsv)")
	f.columns = flag.String("columns", "", "csv/tsv only: comma-separated subset of XtcpFlatRecord json field names (e.g. hostname,inetDiagMsgSocketSourcePort,inetDiagMsgState,tcpInfoRtt); empty = all")
	f.envelopeFlushBytes = flag.Uint("envelopeFlushBytes", envelopeFlushBytesCst, "Safety-net cap on the in-flight protobufList Envelope's UNCOMPRESSED proto size in bytes (franz-go compresses post-flush, so wire size is typically 3-8x smaller). 0 = use daemon default (768 KiB). Whichever cap (bytes/rows) trips first wins.")
	f.envelopeFlushRows = flag.Uint("envelopeFlushRows", envelopeFlushRowsCst, "Primary cap on the in-flight protobufList Envelope's row count. 0 = use daemon default (10000). Cheap, predictable; pairs with -envelopeFlushBytes as a safety net.")
	f.kafkaCompression = flag.String("kafkaCompression", kafkaCompressionCst, "Kafka producer compression codec. '' or 'auto' = preference list [zstd,lz4,snappy,none] negotiated with broker; or pin one of: zstd, lz4, snappy, gzip, none. All codecs are decodable by Redpanda + ClickHouse's Kafka engine.")
	defineS3Flags(f)
	f.reconcileFrequency = flag.Duration("reconcileFrequency", reconcileFrequencyCst, "Period of the background namespace-reconcile ticker (Method B /proc scan). With -reconcileBeforePoll carrying discovery this is a rare safety-net expected to find nothing (watch mapReconciler dels/stores in Prometheus); default is long (6h). 0 disables it (startup reconcile still runs once).")
	f.reconcileBeforePoll = flag.Bool("reconcileBeforePoll", reconcileBeforePollCst, "Reconcile namespaces immediately before each poll cycle, so a newly-appeared namespace is entered within ~1 poll interval. Ties discovery cadence to poll cadence.")
	f.dest = flag.String("dest", defaultDest(), "scheme:addr — kafka:host:9092, nats:..., nsq:..., valkey:..., udp:host:13000, tcp:host:9000, unix:/path, unixgram:/path, file:/path, http(s)://host/ingest, s3parquet:..., stdout, stderr, null (pair stdout/file/tcp with -marshal jsonl|csv|tsv)")
	f.destWriteFiles = flag.Uint("destWriteFiles", DestWriteFilesCst, "Write out the marshaled data to destWriteFiles number of files ( for debugging only )")
	f.topic = flag.String("topic", topicCst, "Kafka or NSQ topic")
	f.xtcpProtoFile = flag.String("xtcpProtoFile", xtcpProtoFileCst, "xtcpProtoFile for registering with the schema registry")
	f.kafkaSchemaUrl = flag.String("kafkaSchemaUrl", kafkaSchemaUrlCst, "kafka schema registry URL")
	f.produceTimeout = flag.Duration("produceTimeout", kafkaProduceTimeoutCst, "Kafka produce timeout (context.WithTimeout)")
	f.label = flag.String("label", labelCst, "label applied to the protobuf")
	f.tag = flag.String("tag", tagCst, "label applied to the protobuf")
	f.location = flag.String("location", locationCst, "deployment grouping/facility this daemon runs in (data center, PoP, region, site, …); stamped on every record's `location`. Falls back to LOCATION env.")
	f.hostname = flag.String("hostname", hostnameCst, "hostname stamped on records; defaults to os.Hostname(). Set this in a container, where os.Hostname() returns the container id, not the host. Falls back to XTCP_HOSTNAME env (NOT HOSTNAME).")
	f.resolveContainerId = flag.Bool("resolveContainerId", resolveContainerIdCst, "resolve each socket's owning container id from its cgroup into container_id/container_runtime; needs /sys/fs/cgroup readable (mount it + --cgroupns=host in a container). Falls back to CONTAINER_ID_RESOLVE env.")
	defineEnrichmentFlags(f)
	defineListenerFlags(f)
	f.deserializers = flag.String("deserializers", deserializersCst, fmt.Sprintf("Deserializers to enable. 'default'=%v ; 'all'=%v ; ''=none ; or a comma-separated subset", xtcp.GetDefaultDeserializers(), xtcp.GetAllDeserializers()))
	defineRuntimeFlags(f)
	f.v = flag.Bool("v", false, "show version")
	f.conf = flag.Bool("conf", false, "show config")
	f.d = flag.Uint("d", debugLevelCst, "debug level")
	return f
}

// defineS3Flags registers the s3parquet destination's flags. Split out of
// defineFlags, which was at 79 statements against funlen's 70, following
// defineEnrichmentFlags' precedent exactly: mutate in place, void return,
// called as a bare statement.
//
// Thirteen flags, all s3-prefixed. -pollJitterPct used to sit in the middle of
// this run in the source and is NOT here, because it is a poll-schedule knob;
// registration order is not observable - Go's flag package prints -help
// lexicographically via VisitAll - so it was lifted up to -frequency and
// -timeout where it belongs. printS3Flags still prints it, and says why.
func defineS3Flags(f *mainFlags) {
	f.s3Endpoint = flag.String("s3Endpoint", s3EndpointCst, "s3parquet: S3-compatible endpoint URL (e.g. http://127.0.0.1:9000 for MinIO). Falls back to S3_ENDPOINT env, or the address after `s3parquet:` in -dest. Required when -dest s3parquet:...")
	f.s3Bucket = flag.String("s3Bucket", s3BucketCst, "s3parquet: target bucket name. Falls back to S3_BUCKET env. Bucket must already exist; daemon does not auto-create.")
	f.s3Prefix = flag.String("s3Prefix", s3PrefixCst, "s3parquet: optional key prefix within the bucket. Combined with Hive-style partitioning host=…/date=…/hour=…/<file>.parquet.")
	f.s3AccessKey = flag.String("s3AccessKey", s3AccessKeyCst, "s3parquet: S3 access key. Falls back to S3_ACCESS_KEY env, or S3_ACCESS_KEY_FILE (path to a file holding the key). Never logged.")
	f.s3SecretKey = flag.String("s3SecretKey", s3SecretKeyCst, "s3parquet: S3 secret key. Falls back to S3_SECRET_KEY env, or S3_SECRET_KEY_FILE (path to a file holding the key). Never logged.")
	f.s3Region = flag.String("s3Region", s3RegionCst, "s3parquet: S3 region. Defaults to 'us-east-1' when empty; required by AWS, ignored by most MinIO setups.")
	f.s3SkipBucketProbe = flag.Bool("s3SkipBucketProbe", s3SkipBucketProbeCst, "s3parquet: skip the startup BucketExists probe (a HeadBucket needing s3:ListBucket). Set true for a write-only, s3:PutObject-only credential. Falls back to S3_SKIP_BUCKET_PROBE env.")
	f.s3ParquetFlushBytes = flag.Uint("s3ParquetFlushBytes", s3ParquetFlushThresholdBytesCst, "s3parquet: soft cap on the in-memory Parquet builder's uncompressed row bytes before finalize+upload. 0 = daemon default (63 MiB).")
	f.s3FlushInterval = flag.Duration("s3FlushInterval", s3FlushIntervalCst, "s3parquet: staleness ceiling — force-flush the in-memory Parquet object after this long even if under the byte cap. 0 = derive as max(-frequency, 30m).")
	f.s3FlushJitterPct = flag.Uint("s3FlushJitterPct", s3FlushJitterPctCst, "s3parquet: jitter as a percent (0-100) of -s3FlushInterval, applied to the timed flush so the fleet doesn't ceiling-flush in lockstep. 0 disables.")
	f.s3FlushThresholdJitterPct = flag.Uint("s3FlushThresholdJitterPct", s3FlushThresholdJitterPctCst, "s3parquet: per-object downward jitter as a percent (0-100) of the byte cap; each object finalizes at threshold*(1-rand[0,pct/100]) to de-sync the size-cap upload path. 0 disables.")
	f.s3UploadMaxAttempts = flag.Uint("s3UploadMaxAttempts", s3UploadMaxAttemptsCst, "s3parquet: max upload attempts (original + retries) before dropping the object. Retries use full-jitter exponential backoff.")
	f.s3UploadBackoffCap = flag.Duration("s3UploadBackoffCap", s3UploadBackoffCapCst, "s3parquet: cap on a single upload retry's backoff window (full jitter draws in [0,window]). 0 = derive as clamp(-frequency/10, 1s, 1h).")
}

// defineListenerFlags registers the flags for xtcp2's own inbound listeners:
// the two outgoing TTL/hop-limit knobs, the six shared listener-auth flags, the
// five gRPC endpoint flags, the five Prometheus endpoint flags, and
// -healthcheck.
//
// Nineteen flags, split out of defineFlags for funlen. This one function pairs
// with TWO print helpers, printListenerAuthFlags and
// printListenerEndpointFlags, because printFlags emits the auth run and the
// endpoint run far apart with other domains between them. Stated here rather
// than implied, so the next reader does not go looking for the missing
// printListenerFlags.
func defineListenerFlags(f *mainFlags) {
	f.ipv4Ttl = flag.Uint("ipv4Ttl", ipv4TtlCst, "outgoing IPv4 TTL for xtcp2's TCP listeners (Prometheus + gRPC); 0 = kernel default. A low value keeps replies from traveling far if the host is internet-exposed. Falls back to IPV4_TTL env.")
	f.ipv6HopLimit = flag.Uint("ipv6HopLimit", ipv6HopLimitCst, "outgoing IPv6 unicast hop limit for xtcp2's TCP listeners; 0 = kernel default. Falls back to IPV6_HOP_LIMIT env.")
	f.listenerAuthMode = flag.String("listenerAuthMode", "", "shared listener auth mode: disabled, raw, hmac-utc-minute. Falls back to LISTENER_AUTH_MODE env.")
	f.listenerRawToken = flag.String("listenerRawToken", "", "listener auth raw bearer token. Prefer LISTENER_RAW_TOKEN or LISTENER_RAW_TOKEN_FILE; never logged.")
	f.listenerHMACKey = flag.String("listenerHMACSharedKey", "", "listener auth HMAC shared key. Prefer LISTENER_HMAC_SHARED_KEY or LISTENER_HMAC_SHARED_KEY_FILE; never logged.")
	f.listenerSignedSkew = flag.Uint("listenerSignedSkewMinutes", uint(listenerauth.DefaultSignedTokenSkewMinutes), "HMAC UTC-minute signed token skew in minutes (0-5). Falls back to LISTENER_SIGNED_SKEW_MINUTES env.")
	f.listenerJitterMin = flag.Duration("listenerAuthFailureJitterMin", listenerauth.DefaultFailureJitterMin, "minimum context-aware auth-failure jitter. Falls back to LISTENER_AUTH_FAILURE_JITTER_MIN env.")
	f.listenerJitterMax = flag.Duration("listenerAuthFailureJitterMax", listenerauth.DefaultFailureJitterMax, "maximum context-aware auth-failure jitter. Falls back to LISTENER_AUTH_FAILURE_JITTER_MAX env.")
	f.grpcPort = flag.Uint("grpcPort", grpcPortCst, "GRPC listening port")
	f.grpcListenNetwork = flag.String("grpcListenNetwork", listenNetworkCst, "gRPC listener network: empty/tcp for TCP, unix for a Unix domain socket. Falls back to GRPC_LISTEN_NETWORK env.")
	f.grpcListenAddress = flag.String("grpcListenAddress", "", "gRPC TCP address or Unix socket path. Empty derives TCP from -grpcPort. Falls back to GRPC_LISTEN_ADDRESS env.")
	f.grpcUnixSocketMode = flag.Uint("grpcUnixSocketMode", unixSocketModeCst, "gRPC Unix socket file mode, e.g. 0600 or 0660. Falls back to GRPC_UNIX_SOCKET_MODE env.")
	f.grpcUnlinkStaleUDS = flag.Bool("grpcUnlinkStaleUnixSocket", unlinkStaleSocketCst, "remove an existing stale gRPC Unix socket before binding. Falls back to GRPC_UNLINK_STALE_UNIX_SOCKET env.")
	f.promListen = flag.String("promListen", promListenCst, "Prometheus http listening socket")
	f.promListenNetwork = flag.String("promListenNetwork", listenNetworkCst, "Prometheus listener network: empty/tcp for TCP, unix for a Unix domain socket. Falls back to PROM_LISTEN_NETWORK env.")
	f.promUnixSocketMode = flag.Uint("promUnixSocketMode", unixSocketModeCst, "Prometheus Unix socket file mode, e.g. 0600 or 0660. Falls back to PROM_UNIX_SOCKET_MODE env.")
	f.promUnlinkStaleUDS = flag.Bool("promUnlinkStaleUnixSocket", unlinkStaleSocketCst, "remove an existing stale Prometheus Unix socket before binding. Falls back to PROM_UNLINK_STALE_UNIX_SOCKET env.")
	f.promPath = flag.String("promPath", promPathCst, "Prometheus http path")
	f.healthcheck = flag.Bool("healthcheck", false, "probe the local /readyz endpoint and exit 0 (ready) / 1 (not ready or unreachable), then exit WITHOUT starting the daemon. For a container HEALTHCHECK on the scratch image (no shell/curl); uses -promListen / PROM_LISTEN to find the port.")
}

// defineRuntimeFlags registers the process-level knobs that are about how this
// binary runs rather than what it collects: GOMAXPROCS, the OS-thread cap,
// -profile.mode, the four Pyroscope flags and the three io_uring flags.
//
// Ten flags, split out of defineFlags for funlen. -v, -conf and -d sit between
// the Pyroscope and io_uring runs in the source but stay in defineFlags; they
// are not runtime tuning, and since registration order is unobservable there
// was no reason to drag them along to keep a contiguous block.
func defineRuntimeFlags(f *mainFlags) {
	// Maximum number of CPUs that can be executing simultaneously
	// https://golang.org/pkg/runtime/#GOMAXPROCS -> zero (0) means default
	f.goMaxProcs = flag.Uint("goMaxProcs", 4, "goMaxProcs = https://golang.org/pkg/runtime/#GOMAXPROCS")
	// Caps the Go runtime's OS thread pool. >0 sets via debug.SetMaxThreads.
	// 0 (default) leaves Go's built-in 10000 in place. See the call site
	// for why this exists (soak-detected thread accumulation).
	f.maxThreads = flag.Int("maxThreads", 2000, "cap on Go runtime OS threads (debug.SetMaxThreads); 0 = use Go default 10000")
	// ./xtcp2 --profile.mode cpu
	// timeout 1h ./xtcp2 --profile.mode cpu
	f.profileMode = flag.String("profile.mode", "", "enable profiling mode, one of [cpu, mem, mutex, block]")
	// Pyroscope continuous profiling. Empty -pyroscopeUrl disables
	// the agent (zero overhead). Set per-environment via env vars or
	// the systemd drop-in; we never ship credentials in argv.
	f.pyroscopeUrl = flag.String("pyroscopeUrl", pyroscopeUrlCst, "Pyroscope server URL (e.g. http://127.0.0.1:4040). Empty disables the agent. Falls back to PYROSCOPE_URL env.")
	f.pyroscopeAppName = flag.String("pyroscopeAppName", pyroscopeAppNameCst, "Application name registered with Pyroscope. Falls back to PYROSCOPE_APP_NAME env.")
	f.pyroscopeSampleHz = flag.Uint("pyroscopeSampleHz", pyroscopeSampleHzCst, "CPU sampling rate in Hz fed to runtime.SetCPUProfileRate.")
	f.pyroscopeUploadSec = flag.Uint("pyroscopeUploadSec", pyroscopeUploadSecCst, "Seconds between batched profile uploads to Pyroscope.")
	f.ioUring = flag.Bool("ioUring", false, "Opt in to io_uring for netlink reads and raw-socket destination writes (Linux 6.1+)")
	f.ioUringRecvBatch = flag.Uint("ioUringRecvBatch", 64, "io_uring recvmsg SQEs kept in flight per Netlinker (1-4096). Higher reduces syscalls on high-fanout hosts.")
	f.ioUringCqeBatch = flag.Uint("ioUringCqeBatch", 128, "io_uring max CQEs reaped per PeekBatchCQE call (1-4096)")
}

// defineEnrichmentFlags registers the best-effort metadata-enrichment flags.
// Split out of defineFlags to keep that function under the funlen threshold and
// to group the enrichment knobs together.
func defineEnrichmentFlags(f *mainFlags) {
	f.enrichContainer = flag.Bool("enrichContainer", enrichContainerCst, "best-effort: map each socket's netns to its owning container via the Docker Engine API, stamping container_id/runtime/name/image. Needs the docker socket mounted (see -dockerSocket). Non-fatal if unreachable. Falls back to ENRICH_CONTAINER env.")
	f.dockerSocket = flag.String("dockerSocket", dockerSocketCst, "path to the Docker Engine API unix socket used by -enrichContainer. Falls back to DOCKER_SOCKET env.")
	f.enrichLldp = flag.Bool("enrichLldp", enrichLldpCst, "best-effort: read LLDP neighbors from lldpd once at startup and stamp each uplink's switch/port (uplink*_lldp_*). Needs the lldpd control socket mounted (see -lldpdSocket). Non-fatal if unreachable. Falls back to ENRICH_LLDP env.")
	f.lldpdSocket = flag.String("lldpdSocket", lldpdSocketCst, "path to the lldpd control unix socket used by -enrichLldp. Falls back to LLDPD_SOCKET env.")
	f.lldpdVersionHint = flag.String("lldpdVersionHint", lldpdVersionHintCst, "optional lldpd version hint (e.g. \"1.0.18\") to disambiguate the control-protocol struct layout; empty = auto-detect. Falls back to LLDPD_VERSION_HINT env.")
	f.enrichNic = flag.Bool("enrichNic", enrichNicCst, "best-effort: read NIC identity (driver/model/pci/speed/firmware) from sysfs+ethtool once at startup and stamp each uplink's nic_* columns. Non-fatal on read failure. Falls back to ENRICH_NIC env.")
	f.uplinkCount = flag.Uint("uplinkCount", uplinkCountCst, "number of uplink slots to populate for LLDP/NIC enrichment (hosts are typically dual-homed = 2; max 2). Falls back to UPLINK_COUNT env.")
	f.uplinkInterfaces = flag.String("uplinkInterfaces", uplinkInterfacesCst, "comma-separated explicit uplink interface names (e.g. \"eth0,eth1\") overriding default-route auto-detection for -enrichLldp/-enrichNic. Falls back to UPLINK_INTERFACES env.")
	f.populateNsid = flag.Bool("populateNsid", populateNsidCst, "best-effort: query each namespace's NETNSA_NSID via rtnetlink into the nsid column (usually 0/unassigned for docker/containerd; netns_inode is the stable key). Falls back to POPULATE_NSID env.")
	f.enrichAsn = flag.Bool("enrichAsn", enrichAsnCst, "best-effort: longest-prefix-match each socket's destination against the ipfeed-collector Parquet artifact (-asnDbPath) and stamp enrich_socket_dest_asn / enrich_socket_dest_network_owner. Self and local-subnet destinations (see -enrichLocality) skip the lookup. Non-fatal when the artifact is missing. Falls back to ENRICH_ASN env."+enricherBuildNote(xtcp.EnricherAsn))
	f.asnDbPath = flag.String("asnDbPath", asnDbPathCst, "path to the ipfeed-collector Parquet artifact (prefix -> asn, network_owner) used by -enrichAsn. Falls back to ASN_DB_PATH env.")
	f.asnRefreshInterval = flag.Duration("asnRefreshInterval", asnRefreshIntervalCst, "how often -enrichAsn re-stats -asnDbPath and reloads it when its size/mtime changed (also how often a missing artifact is retried); 0 = load once at startup, never reload. Falls back to ASN_REFRESH_INTERVAL env.")
	f.ipmetaBootstrapPath = flag.String("ipmetaBootstrapPath", ipmetaBootstrapPathCst, "optional zstd-compressed lookup artifact baked into the image and used by -enrichAsn before live refresh. Falls back to IPMETA_BOOTSTRAP_PATH env.")
	f.ipmetaCachePath = flag.String("ipmetaCachePath", ipmetaCachePathCst, "optional writable zstd-compressed lookup artifact used by -enrichAsn before the image bootstrap and refreshed after successful live reloads. Falls back to IPMETA_CACHE_PATH env.")
	f.enrichLocality = flag.Bool("enrichLocality", enrichLocalityCst, "best-effort: per namespace, dump the local addresses + routing table via rtnetlink and classify each socket's destination as self/local-subnet/remote, stamping the enrich_socket_dest_locality + interface-name columns (bound idiag_if and route egress). Non-fatal on read failure. Falls back to ENRICH_LOCALITY env."+enricherBuildNote(xtcp.EnricherLocality))
	f.localityRefreshInterval = flag.Duration("localityRefreshInterval", localityRefreshIntervalCst, "how often -enrichLocality re-dumps every namespace's addresses/routes on the reconcile path (new namespaces are always dumped on the next reconcile; failed or loopback-only namespaces retry on a 30s-5m backoff); 0 = discover each namespace once, never refresh. Falls back to LOCALITY_REFRESH_INTERVAL env.")
}

// printFlags echoes every flag xtcp2 will act on to stdout at startup.
//
// It prints 73 of the 92 registered flags, in an order that is NOT registration
// order - the Pyroscope block, the promListen/promPath pair, goMaxProcs, the
// enrichment block and the gRPC/Prometheus tail are each hoisted or demoted
// relative to defineFlags. So the four helpers below carve contiguous runs of
// the PRINTED sequence, which is why one of them pairs with two define
// functions and another prints a flag its define counterpart does not register.
//
// Split out at 74 statements, over funlen's 70. The output is pinned
// byte-for-byte by cmd/xtcp2/testdata/printflags-golden.txt, which is the only
// reason a mechanical carve of this function was safe to make.
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
	fmt.Println("*columns:", *f.columns)
	fmt.Println("*envelopeFlushBytes:", *f.envelopeFlushBytes)
	fmt.Println("*envelopeFlushRows:", *f.envelopeFlushRows)
	fmt.Println("*kafkaCompression:", *f.kafkaCompression)
	printS3Flags(f)
	fmt.Println("*pyroscopeUrl:", *f.pyroscopeUrl)
	fmt.Println("*pyroscopeAppName:", *f.pyroscopeAppName)
	fmt.Println("*pyroscopeSampleHz:", *f.pyroscopeSampleHz)
	fmt.Println("*pyroscopeUploadSec:", *f.pyroscopeUploadSec)
	fmt.Println("*dest:", *f.dest)
	fmt.Println("*destWriteFiles:", *f.destWriteFiles)
	fmt.Println("*topic:", *f.topic)
	fmt.Println("*xtcpProtoFile:", *f.xtcpProtoFile)
	fmt.Println("*kafkaSchemaUrl:", *f.kafkaSchemaUrl)
	fmt.Println("*produceTimeout:", *f.produceTimeout)
	fmt.Println("*promListen:", *f.promListen)
	fmt.Println("*promPath:", *f.promPath)
	printListenerAuthFlags(f)
	fmt.Println("*goMaxProcs:", *f.goMaxProcs)
	printEnrichmentFlags(f)
	printListenerEndpointFlags(f)
	fmt.Println("*d:", *f.d)
}

// printS3Flags prints the s3parquet destination's run, s3Endpoint through
// reconcileBeforePoll, and pairs with defineS3Flags.
//
// It prints fourteen lines for thirteen registered flags. *pollJitterPct is the
// fourteenth: it is a poll-schedule knob with nothing to do with S3, so
// defineS3Flags does not register it, but it has always PRINTED here and
// printFlags's output order is observable - it is what a startup log, a
// lifecycle scraper and testdata/printflags-golden.txt all read. Moving the
// line would be a behavior change for no gain, so the asymmetry is recorded
// here instead of being tidied away.
func printS3Flags(f *mainFlags) {
	fmt.Println("*s3Endpoint:", *f.s3Endpoint)
	fmt.Println("*s3Bucket:", *f.s3Bucket)
	fmt.Println("*s3Prefix:", *f.s3Prefix)
	// *f.s3AccessKey and *f.s3SecretKey intentionally NOT printed —
	// they would leak via console logs, lifecycle test scrapers, etc.
	fmt.Println("*s3Region:", *f.s3Region)
	fmt.Println("*s3SkipBucketProbe:", *f.s3SkipBucketProbe)
	fmt.Println("*s3ParquetFlushBytes:", *f.s3ParquetFlushBytes)
	fmt.Println("*pollJitterPct:", *f.pollJitterPct)
	fmt.Println("*s3FlushInterval:", *f.s3FlushInterval)
	fmt.Println("*s3FlushJitterPct:", *f.s3FlushJitterPct)
	fmt.Println("*s3FlushThresholdJitterPct:", *f.s3FlushThresholdJitterPct)
	fmt.Println("*s3UploadMaxAttempts:", *f.s3UploadMaxAttempts)
	fmt.Println("*s3UploadBackoffCap:", *f.s3UploadBackoffCap)
	fmt.Println("*reconcileFrequency:", *f.reconcileFrequency)
	fmt.Println("*reconcileBeforePoll:", *f.reconcileBeforePoll)
}

// printListenerAuthFlags prints the shared listener-auth run, listenerAuthMode
// through listenerAuthFailureJitterMax.
//
// The two secrets are DERIVED to a bool here, not omitted: an operator needs to
// know whether a token is configured, and `set: <bool>` answers that without
// the value reaching stdout. That is deliberately a different treatment from
// the S3 credentials in printS3Flags, which get no line at all - see the
// comment there. The derivation must stay `!= ""`, so an unset secret reports
// false rather than vanishing.
func printListenerAuthFlags(f *mainFlags) {
	fmt.Println("*listenerAuthMode:", *f.listenerAuthMode)
	fmt.Println("*listenerRawToken: set:", *f.listenerRawToken != "")
	fmt.Println("*listenerHMACSharedKey: set:", *f.listenerHMACKey != "")
	fmt.Println("*listenerSignedSkewMinutes:", *f.listenerSignedSkew)
	fmt.Println("*listenerAuthFailureJitterMin:", *f.listenerJitterMin)
	fmt.Println("*listenerAuthFailureJitterMax:", *f.listenerJitterMax)
}

// printEnrichmentFlags prints the metadata-enrichment run, enrichContainer
// through localityRefreshInterval, and pairs with defineEnrichmentFlags and
// envOverrideEnrichment.
//
// compiledInEnrichers is the last line and is not a flag at all. It stays here
// because the flags and the compiled-in set are only meaningful together:
// either one alone implies the enrich_* columns will be populated when it may
// not be.
func printEnrichmentFlags(f *mainFlags) {
	fmt.Println("*enrichContainer:", *f.enrichContainer)
	fmt.Println("*dockerSocket:", *f.dockerSocket)
	fmt.Println("*enrichLldp:", *f.enrichLldp)
	fmt.Println("*lldpdSocket:", *f.lldpdSocket)
	fmt.Println("*lldpdVersionHint:", *f.lldpdVersionHint)
	fmt.Println("*enrichNic:", *f.enrichNic)
	fmt.Println("*uplinkCount:", *f.uplinkCount)
	fmt.Println("*uplinkInterfaces:", *f.uplinkInterfaces)
	fmt.Println("*populateNsid:", *f.populateNsid)
	fmt.Println("*enrichAsn:", *f.enrichAsn)
	fmt.Println("*asnDbPath:", *f.asnDbPath)
	fmt.Println("*asnRefreshInterval:", *f.asnRefreshInterval)
	fmt.Println("*ipmetaBootstrapPath:", *f.ipmetaBootstrapPath)
	fmt.Println("*ipmetaCachePath:", *f.ipmetaCachePath)
	fmt.Println("*enrichLocality:", *f.enrichLocality)
	fmt.Println("*localityRefreshInterval:", *f.localityRefreshInterval)
	// Which enrichers this artifact actually contains. Printed next to the
	// flags because the two together are what determine whether the enrich_*
	// columns will be populated; either one alone is misleading.
	fmt.Println("compiledInEnrichers:", xtcp.CompiledInEnrichers())
}

// printListenerEndpointFlags prints the gRPC and Prometheus endpoint run,
// grpcListenNetwork through promUnlinkStaleUnixSocket.
//
// This is the SECOND of two runs that pair with defineListenerFlags: that one
// function registers both this group and printListenerAuthFlags's, because
// printFlags does not print in registration order. The pairing is deliberately
// stated as partial rather than implied to be one-to-one.
//
// It must not dereference f.healthcheck, which defineListenerFlags registers
// and printFlags has never printed. TestPrintFlagsNilFields pins why: every
// existing caller leaves that pointer nil.
func printListenerEndpointFlags(f *mainFlags) {
	fmt.Println("*grpcListenNetwork:", *f.grpcListenNetwork)
	fmt.Println("*grpcListenAddress:", *f.grpcListenAddress)
	fmt.Println("*grpcUnixSocketMode:", *f.grpcUnixSocketMode)
	fmt.Println("*grpcUnlinkStaleUnixSocket:", *f.grpcUnlinkStaleUDS)
	fmt.Println("*promListenNetwork:", *f.promListenNetwork)
	fmt.Println("*promUnixSocketMode:", *f.promUnixSocketMode)
	fmt.Println("*promUnlinkStaleUnixSocket:", *f.promUnlinkStaleUDS)
}

func buildConfig(f *mainFlags, des *xtcp_config.EnabledDeserializers) *xtcp_config.XtcpConfig {
	grpcListener := buildOptionalListenerEndpoint(*f.grpcListenNetwork, *f.grpcListenAddress, *f.grpcUnixSocketMode, *f.grpcUnlinkStaleUDS, true)
	prometheusListener := buildOptionalListenerEndpoint(*f.promListenNetwork, *f.promListen, *f.promUnixSocketMode, *f.promUnlinkStaleUDS, false)
	return &xtcp_config.XtcpConfig{
		NlTimeoutMilliseconds:        *f.nltimeout,
		PollFrequency:                durationpb.New(*f.pollFrequency),
		PollTimeout:                  durationpb.New(*f.pollTimeout),
		MaxLoops:                     *f.maxLoops,
		Netlinkers:                   uint32(*f.netlinkers),
		NetlinkersDoneChanSize:       netlinkerDoneChSizeCst,
		NlmsgSeq:                     uint32(*f.nlmsgSeq),
		PacketSize:                   *f.packetSize,
		PacketSizeMply:               uint32(*f.packetSizeMply),
		WriteFiles:                   uint32(*f.writeFiles),
		CapturePath:                  *f.capturePath,
		Modulus:                      *f.modulus,
		MarshalTo:                    *f.marshal,
		CsvColumns:                   *f.columns,
		EnvelopeFlushThresholdBytes:  uint32(*f.envelopeFlushBytes),
		EnvelopeFlushThresholdRows:   uint32(*f.envelopeFlushRows),
		KafkaCompression:             *f.kafkaCompression,
		S3Endpoint:                   *f.s3Endpoint,
		S3Bucket:                     *f.s3Bucket,
		S3Prefix:                     *f.s3Prefix,
		S3AccessKey:                  *f.s3AccessKey,
		S3SecretKey:                  *f.s3SecretKey,
		S3Region:                     *f.s3Region,
		S3SkipBucketProbe:            *f.s3SkipBucketProbe,
		S3ParquetFlushThresholdBytes: uint32(*f.s3ParquetFlushBytes),
		PollJitterPct:                uint32(*f.pollJitterPct),
		S3FlushInterval:              durationpb.New(*f.s3FlushInterval),
		S3FlushJitterPct:             uint32(*f.s3FlushJitterPct),
		S3FlushThresholdJitterPct:    uint32(*f.s3FlushThresholdJitterPct),
		S3UploadMaxAttempts:          uint32(*f.s3UploadMaxAttempts),
		S3UploadBackoffCap:           durationpb.New(*f.s3UploadBackoffCap),
		ReconcileFrequency:           durationpb.New(*f.reconcileFrequency),
		ReconcileBeforePoll:          *f.reconcileBeforePoll,
		PyroscopeUrl:                 *f.pyroscopeUrl,
		PyroscopeAppName:             *f.pyroscopeAppName,
		PyroscopeSampleHz:            uint32(*f.pyroscopeSampleHz),
		PyroscopeUploadIntervalSec:   uint32(*f.pyroscopeUploadSec),
		Dest:                         *f.dest,
		DestWriteFiles:               uint32(*f.destWriteFiles),
		Topic:                        *f.topic,
		XtcpProtoFile:                *f.xtcpProtoFile,
		KafkaSchemaUrl:               *f.kafkaSchemaUrl,
		KafkaProduceTimeout:          durationpb.New(*f.produceTimeout),
		DebugLevel:                   uint32(*f.d),
		Label:                        *f.label,
		Tag:                          *f.tag,
		Location:                     *f.location,
		Hostname:                     *f.hostname,
		// DaemonVersion: build provenance (-ldflags) stamped on every record.
		DaemonVersion:           versionString(),
		ResolveContainerId:      *f.resolveContainerId,
		EnrichContainerEnable:   *f.enrichContainer,
		DockerSocketPath:        *f.dockerSocket,
		EnrichLldpEnable:        *f.enrichLldp,
		LldpdSocketPath:         *f.lldpdSocket,
		LldpdVersionHint:        *f.lldpdVersionHint,
		EnrichNicEnable:         *f.enrichNic,
		UplinkCount:             uint32(*f.uplinkCount),
		UplinkInterfaces:        splitCSV(*f.uplinkInterfaces),
		PopulateNsid:            *f.populateNsid,
		EnrichAsnEnable:         *f.enrichAsn,
		AsnDbPath:               *f.asnDbPath,
		AsnRefreshInterval:      durationpb.New(*f.asnRefreshInterval),
		IpmetaBootstrapPath:     *f.ipmetaBootstrapPath,
		IpmetaCachePath:         *f.ipmetaCachePath,
		EnrichLocalityEnable:    *f.enrichLocality,
		LocalityRefreshInterval: durationpb.New(*f.localityRefreshInterval),
		Ipv4Ttl:                 uint32(*f.ipv4Ttl),
		Ipv6HopLimit:            uint32(*f.ipv6HopLimit),
		ListenerAuth:            buildListenerAuth(f),
		GrpcPort:                uint32(*f.grpcPort),
		GrpcListener:            grpcListener,
		PrometheusListener:      prometheusListener,
		EnabledDeserializers:    des,

		IoUring:              *f.ioUring,
		IoUringRecvBatchSize: uint32(*f.ioUringRecvBatch),
		IoUringCqeBatchSize:  uint32(*f.ioUringCqeBatch),
	}
}

func buildOptionalListenerEndpoint(networkValue, address string, unixSocketMode uint, unlinkStale bool, allowAddressOnly bool) *xtcp_config.ListenerEndpoint {
	if strings.TrimSpace(networkValue) == "" && (!allowAddressOnly || strings.TrimSpace(address) == "") {
		return nil
	}
	ep, err := listener.ProtoEndpoint(networkValue, address, unixSocketMode, unlinkStale)
	if err != nil {
		fatalf("listener config: %v", err)
		return nil
	}
	return ep
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

// startPyroscope starts the Pyroscope continuous-profiling agent if a
// server URL is configured. Returns a stop function (no-op when the
// agent is disabled). All five profile types are enabled so a single
// scrape gives operators CPU, memory, goroutine, mutex, and block data
// — essential for diagnosing the kind of OS-thread accumulation that
// killed the first 12 h soak.
func startPyroscope(url, appName string, sampleHz, uploadSec uint, debugLevel uint) func() {
	if url == "" {
		if debugLevel > 1000 {
			log.Println("Pyroscope disabled (empty -pyroscopeUrl)")
		}
		return func() {}
	}
	if appName == "" {
		appName = pyroscopeAppNameCst
	}
	if sampleHz == 0 {
		sampleHz = 100
	}
	if uploadSec == 0 {
		uploadSec = 15
	}
	cfg := pyroscope.Config{
		ApplicationName: appName,
		ServerAddress:   url,
		UploadRate:      time.Duration(uploadSec) * time.Second,
		SampleRate:      uint32(sampleHz),
		ProfileTypes: []pyroscope.ProfileType{
			pyroscope.ProfileCPU,
			pyroscope.ProfileAllocObjects,
			pyroscope.ProfileAllocSpace,
			pyroscope.ProfileInuseObjects,
			pyroscope.ProfileInuseSpace,
			pyroscope.ProfileGoroutines,
			pyroscope.ProfileMutexCount,
			pyroscope.ProfileMutexDuration,
			pyroscope.ProfileBlockCount,
			pyroscope.ProfileBlockDuration,
		},
	}
	p, err := pyroscope.Start(cfg)
	if err != nil {
		// Profiling is observability, never block startup on it.
		log.Printf("pyroscope agent disabled: %v", err)
		return func() {}
	}
	if debugLevel > 10 {
		log.Printf("Pyroscope agent started: server=%s app=%s sampleHz=%d uploadInterval=%ds",
			url, appName, sampleHz, uploadSec)
	}
	return func() {
		if err := p.Stop(); err != nil {
			log.Printf("pyroscope stop: %v", err)
		}
	}
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
	warnListenerAuthSecretSource(f)
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
var promHandlerStarter = func(promPath string, prometheusListener *xtcp_config.ListenerEndpoint, promListen string, ipv4TTL, ipv6HopLimit uint32, auth *xtcp_config.ListenerAuth) {
	go initPromHandler(promPath, prometheusListener, promListen, ipv4TTL, ipv6HopLimit, auth)
}

func main() {
	misc.DieIfNotLinux()
	os.Exit(runMain(context.Background()))
}

// runMain wires the production main body. Extracted so tests can run
// it with stubbed daemonRunner / promHandlerStarter.
func runMain(ctx context.Context) int {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	complete := make(chan struct{}, signalChannelSizeCst)
	go initSignalHandler(cancel, complete)

	f := defineFlags()
	flag.Parse()
	healthAuth := buildListenerAuth(f)
	envOverrideListenerAuth(&xtcp_config.XtcpConfig{ListenerAuth: healthAuth}, 0)

	// Container HEALTHCHECK mode: probe the local /readyz and exit, without
	// starting the daemon. The scratch image has no shell/curl, so the binary
	// self-checks (same pattern as the docker_custom_scrape exporter).
	if *f.healthcheck {
		authn, err := listenerauth.New(healthAuth)
		if err != nil {
			log.Printf("healthcheck: invalid listener auth config: %v", err)
			return 1
		}
		return runHealthcheck(runCtx, *f.promListenNetwork, *f.promListen, authn, listenerauth.ClientAuth{RawToken: healthAuth.GetRawToken(), HMACSharedKey: healthAuth.GetHmacSharedKey()})
	}

	c, done := prepareConfig(f)
	if done {
		return 0
	}

	// Soft-restart carry: if a prior ConfigService.Set re-exec'd us with a full
	// config in the environment, that config wins over the flag/env-derived one.
	// A malformed value is fatal — the restart intent is explicit, so silently
	// falling back to the flag config would be surprising and wrong.
	if envc, ok, err := loadReconfigureConfig(); err != nil {
		fatalf("reconfigure: invalid %s: %v", reconfigureEnvKey, err)
		return 1
	} else if ok {
		c = envc
		if debugLevel > 10 {
			log.Printf("reconfigure: loaded config from %s (soft restart)", reconfigureEnvKey)
		}
	}

	environmentOverrideGoMaxProcs(f.goMaxProcs, debugLevel)
	if runtime.NumCPU() > int(*f.goMaxProcs) {
		mp := runtime.GOMAXPROCS(int(*f.goMaxProcs))
		if debugLevel > 10 {
			log.Printf("Main runtime.GOMAXPROCS now:%d was:%d\n", *f.goMaxProcs, mp)
		}
	}

	// Cap the Go-runtime OS thread pool. Default is 10000, which means a
	// thread-accumulating bug (xtcp2 burned ~1121 threads in a 1h soak with
	// 4-per-sec ns churn) silently grows until clone(2) returns EAGAIN and
	// the runtime *fatal-errors* with `failed to create new OS thread`.
	// 2000 is high enough for legitimate burst load (4 netlinkers × hundreds
	// of namespaces) but catches the leak before it crashes; SetMaxThreads
	// converts the leak into a controlled abort with the same diagnostic.
	// Override via -maxThreads if a future deployment legitimately needs
	// more (e.g. >500 simultaneous namespaces).
	if *f.maxThreads > 0 {
		debugSetMaxThreads(*f.maxThreads, debugLevel)
	}

	defer startProfile(*f.profileMode, debugLevel)()
	defer startPyroscope(c.PyroscopeUrl, c.PyroscopeAppName,
		uint(c.PyroscopeSampleHz), uint(c.PyroscopeUploadIntervalSec),
		debugLevel)()

	environmentOverrideProm(f.promListen, f.promPath, debugLevel)
	applyPrometheusListenerAddress(c, *f.promListen)
	if _, err := listenerauth.New(c.ListenerAuth); err != nil {
		fatalf("listener auth config invalid: %v", err)
		return 1
	}
	promHandlerStarter(*f.promPath, c.PrometheusListener, *f.promListen, c.Ipv4Ttl, c.Ipv6HopLimit, c.ListenerAuth)
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

	daemonRunner(runCtx, cancel, c)
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

	// Soft-restart wiring: the gRPC ConfigService.Set handler calls this hook
	// with a validated new config. Record it, then (after a short grace so the
	// Set response reaches the client) cancel the run context to start graceful
	// shutdown. Buffered channel + non-blocking send: only the first request in
	// a shutdown window is honored.
	restartCh := make(chan *xtcp_config.XtcpConfig, 1)
	x.OnReconfigure(func(newCfg *xtcp_config.XtcpConfig) {
		select {
		case restartCh <- newCfg:
		default:
		}
		time.AfterFunc(reconfigureGraceDelay, cancel)
	})

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

	// Graceful shutdown is complete here — the Poller's deferred flush and
	// closeDestination have drained all buffered records, so no data is lost.
	// If a reconfigure was requested, re-exec into the new config now (same
	// binary, same container/PID). execSelfWithConfig returns only on error.
	select {
	case newCfg := <-restartCh:
		execSelfWithConfig(newCfg)
	default:
	}
}

// loadReconfigureConfig returns the full XtcpConfig carried across a
// soft-restart re-exec via the reconfigureEnvKey environment variable. ok is
// false when the variable is unset/empty (normal first boot). A non-nil error
// means the variable was present but unparseable.
func loadReconfigureConfig() (c *xtcp_config.XtcpConfig, ok bool, err error) {
	v, present := os.LookupEnv(reconfigureEnvKey)
	if !present || v == "" {
		return nil, false, nil
	}
	c = &xtcp_config.XtcpConfig{}
	if err := protojson.Unmarshal([]byte(v), c); err != nil {
		return nil, false, err
	}
	return c, true, nil
}

// execSelfWithConfig re-execs this binary in place (same PID/container),
// carrying newCfg as protojson in reconfigureEnvKey so the fresh process boots
// with it. os.Args is preserved, so all process-level flags carry unchanged;
// only the XtcpConfig is overridden at startup. On success syscall.Exec does
// not return; on failure it logs and returns so the caller exits normally.
func execSelfWithConfig(newCfg *xtcp_config.XtcpConfig) {
	exe, err := os.Executable()
	if err != nil {
		log.Printf("reconfigure: os.Executable failed, cannot re-exec: %v", err)
		return
	}
	js, err := protojson.Marshal(newCfg)
	if err != nil {
		log.Printf("reconfigure: protojson.Marshal failed, cannot re-exec: %v", err)
		return
	}
	env := append(envWithoutKey(os.Environ(), reconfigureEnvKey), reconfigureEnvKey+"="+string(js))
	log.Printf("reconfigure: re-exec %s (soft restart, config %d bytes)", exe, len(js))
	// G702 is a false positive here: this is an intentional in-place restart of
	// THIS binary (os.Executable), preserving our own os.Args and environment
	// plus a config we validated and serialized ourselves — not execution of an
	// external, attacker-chosen command. The new config was protovalidate'd in
	// the Set handler before reaching this path.
	if err := syscall.Exec(exe, os.Args, env); err != nil { //nolint:gosec // G702: re-exec of self with validated config; see comment above
		log.Printf("reconfigure: syscall.Exec failed: %v", err)
	}
}

// envWithoutKey returns env with any existing KEY=... entries for key removed,
// so a re-exec replaces rather than duplicates the carried config.
func envWithoutKey(env []string, key string) []string {
	prefix := key + "="
	out := env[:0:0]
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			continue
		}
		out = append(out, e)
	}
	return out
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

func initPromHandler(promPath string, prometheusListener *xtcp_config.ListenerEndpoint, promListen string, ipv4TTL, ipv6HopLimit uint32, auth *xtcp_config.ListenerAuth) {
	// A dedicated mux (not http.DefaultServeMux) for this listener. This is what
	// lets pprof be registered EXPLICITLY below without the blank
	// `_ "net/http/pprof"` side-effect import (which trips gosec G108): that
	// import's init() registers /debug/pprof/* on DefaultServeMux, so serving
	// our own mux both keeps DefaultServeMux out of the request path and avoids
	// a double-registration panic.
	mux := http.NewServeMux()
	mux.Handle(promPath, promhttp.HandlerFor(
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
	authn, err := listenerauth.New(auth)
	if err != nil {
		fatalf("listener auth config invalid: %v", err)
		return
	}
	go servePromHandler(authn.WrapHTTP(mux), prometheusListener, promListen, ipv4TTL, ipv6HopLimit)
}

// servePromHandler runs the prom HTTP server on promListen. On
// ListenAndServe failure it invokes fatalf (default log.Fatalf in
// production, swapped to a capture by tests). Extracted from
// initPromHandler so tests can drive the error path in isolation.
func servePromHandler(handler http.Handler, prometheusListener *xtcp_config.ListenerEndpoint, promListen string, ipv4TTL, ipv6HopLimit uint32) {
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	ep, err := listener.EndpointFromProto(prometheusListener, listener.NetworkTCP, promListen)
	if err != nil {
		fatalf("prometheus listener config: %v", err)
		return
	}
	ln, err := listener.Listen(context.Background(), ep, ipv4TTL, ipv6HopLimit)
	if err != nil {
		fatalf("prometheus error, listen: %v", err)
		return
	}
	if err := srv.Serve(ln); err != nil {
		fatalf("prometheus error: %v", err)
	}
}

// runHealthcheck probes the local /readyz endpoint and returns a process exit
// code: 0 when ready (HTTP 200), 1 otherwise (not ready, or unreachable). It is
// the `-healthcheck` mode used by the container HEALTHCHECK on the scratch image
// (no shell/curl to run an external probe). The port comes from PROM_LISTEN or
// the -promListen flag; the daemon listens on 0.0.0.0, so 127.0.0.1 reaches it.
func runHealthcheck(ctx context.Context, promNetwork, promListen string, authn *listenerauth.Authenticator, clientAuth listenerauth.ClientAuth) int {
	network := promNetwork
	if v, ok := os.LookupEnv("PROM_LISTEN_NETWORK"); ok && v != "" {
		network = v
	}
	addr := promListen
	if v, ok := os.LookupEnv("PROM_LISTEN"); ok && v != "" {
		addr = v
	}
	parsed, err := listener.ParseNetwork(network)
	if err != nil {
		log.Printf("healthcheck: %v", err)
		return 1
	}
	if parsed == xtcp_config.ListenerNetwork_LISTENER_NETWORK_UNIX {
		return runUnixHealthcheck(ctx, addr)
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		port = "9088" // promListenCst default
	}
	url := "http://127.0.0.1:" + port + "/readyz"
	reqCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		log.Printf("healthcheck: new request %s: %v", url, err)
		return 1
	}
	if authn != nil {
		listenerauth.HTTPRequestWithClientAuth(req, clientAuth, time.Now())
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("healthcheck: GET %s: %v", url, err)
		return 1
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusOK {
		return 0
	}
	log.Printf("healthcheck: %s -> %d", url, resp.StatusCode)
	return 1
}

func runUnixHealthcheck(ctx context.Context, socketPath string) int {
	if socketPath == "" {
		log.Printf("healthcheck: unix socket path is empty")
		return 1
	}
	reqCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, "http://unix/readyz", nil)
	if err != nil {
		log.Printf("healthcheck: new unix request: %v", err)
		return 1
	}
	client := &http.Client{
		Timeout: 3 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, listener.NetworkUnix, socketPath)
			},
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("healthcheck: GET unix:%s /readyz: %v", socketPath, err)
		return 1
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusOK {
		return 0
	}
	log.Printf("healthcheck: unix:%s /readyz -> %d", socketPath, resp.StatusCode)
	return 1
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
// debugSetMaxThreads bounds the Go runtime's OS thread pool. Default Go is
// 10000; soak runs showed a thread-accumulation pattern that crashed near
// 1121 (RLIMIT_NPROC, errno=11). Capping at a chosen value via runtime/debug
// keeps the failure mode explicit + configurable instead of trip-wired by
// whatever ulimit happens to be in effect.
func debugSetMaxThreads(n int, debugLevel uint) {
	prev := runtimeDebug.SetMaxThreads(n)
	if debugLevel > 10 {
		log.Printf("debug.SetMaxThreads: cap=%d (previous=%d)", n, prev)
	}
}

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
	envOverrideEnrichment(c, debugLevel)
	envOverrideListeners(c, debugLevel)
	envOverrideListenerAuth(c, debugLevel)
}

func buildListenerAuth(f *mainFlags) *xtcp_config.ListenerAuth {
	return &xtcp_config.ListenerAuth{
		Mode:                   listenerAuthModeValue(*f.listenerAuthMode),
		RawToken:               *f.listenerRawToken,
		HmacSharedKey:          *f.listenerHMACKey,
		SignedTokenSkewMinutes: proto.Uint32(uint32(*f.listenerSignedSkew)),
		FailureJitterMin:       durationpb.New(*f.listenerJitterMin),
		FailureJitterMax:       durationpb.New(*f.listenerJitterMax),
	}
}

func listenerAuthModeValue(s string) xtcp_config.ListenerAuthMode {
	mode, err := listenerauth.ParseMode(s)
	if err != nil {
		return xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_UNSPECIFIED
	}
	return mode
}

func envOverrideListenerAuth(c *xtcp_config.XtcpConfig, debugLevel uint) {
	a := c.ListenerAuth
	if a == nil {
		a = &xtcp_config.ListenerAuth{}
		c.ListenerAuth = a
	}
	if v, ok := envString("LISTENER_AUTH_MODE"); ok {
		if mode, err := listenerauth.ParseMode(v); err == nil {
			a.Mode = mode
			logEnv("LISTENER_AUTH_MODE", "listener auth mode set", debugLevel)
		}
	}
	if v, ok := secretEnv("LISTENER_RAW_TOKEN", "LISTENER_RAW_TOKEN_FILE"); ok {
		a.RawToken = v
		logEnv("LISTENER_RAW_TOKEN", "listener raw token set", debugLevel)
	}
	if v, ok := secretEnv("LISTENER_HMAC_SHARED_KEY", "LISTENER_HMAC_SHARED_KEY_FILE"); ok {
		a.HmacSharedKey = v
		logEnv("LISTENER_HMAC_SHARED_KEY", "listener HMAC key set", debugLevel)
	}
	if v, ok := envUint32("LISTENER_SIGNED_SKEW_MINUTES"); ok {
		a.SignedTokenSkewMinutes = proto.Uint32(v)
	}
	if v, ok := envDuration("LISTENER_AUTH_FAILURE_JITTER_MIN"); ok {
		a.FailureJitterMin = durationpb.New(v)
	}
	if v, ok := envDuration("LISTENER_AUTH_FAILURE_JITTER_MAX"); ok {
		a.FailureJitterMax = durationpb.New(v)
	}
}

func secretEnv(inlineKey, fileKey string) (string, bool) {
	if path, ok := os.LookupEnv(fileKey); ok && path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return "", false
		}
		return strings.TrimSpace(string(b)), true
	}
	return os.LookupEnv(inlineKey)
}

func warnListenerAuthSecretSource(f *mainFlags) {
	if *f.listenerRawToken != "" && !envSet("LISTENER_RAW_TOKEN", "LISTENER_RAW_TOKEN_FILE") {
		log.Print("listener auth raw token came from CLI/config; prefer LISTENER_RAW_TOKEN or LISTENER_RAW_TOKEN_FILE")
	}
	if *f.listenerHMACKey != "" && !envSet("LISTENER_HMAC_SHARED_KEY", "LISTENER_HMAC_SHARED_KEY_FILE") {
		log.Print("listener auth HMAC key came from CLI/config; prefer LISTENER_HMAC_SHARED_KEY or LISTENER_HMAC_SHARED_KEY_FILE")
	}
}

func envSet(inlineKey, fileKey string) bool {
	if v, ok := os.LookupEnv(fileKey); ok && v != "" {
		return true
	}
	_, ok := os.LookupEnv(inlineKey)
	return ok
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

// envBool parses a boolean env var via strconv.ParseBool (accepts
// 1/t/T/TRUE/true/True and 0/f/F/FALSE/false/False). Returns (false, false)
// when the var is unset or unparseable, so a typo leaves the config default
// untouched rather than silently flipping the flag.
func envBool(key string) (bool, bool) {
	v, ok := os.LookupEnv(key)
	if !ok {
		return false, false
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, false
	}
	return b, true
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

// splitCSV parses a comma-separated flag/env value into a trimmed, non-empty
// string slice (e.g. "eth0, eth1" -> ["eth0","eth1"]). An empty or all-blank
// input yields nil so an unset -uplinkInterfaces leaves UplinkInterfaces unset.
func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// envStringFromFile reads a secret from the file whose path is the value of
// the `key` env var (the Docker `_FILE` convention, e.g.
// S3_SECRET_KEY_FILE=/run/secrets/s3). This keeps the secret out of the
// process environment (visible via `docker inspect` / /proc/<pid>/environ)
// and out of argv — the env only names a path, the bytes live in a file.
//
// The file contents are trimmed of surrounding whitespace (handles a trailing
// newline from `echo`/`aws ssm get-parameter`). Returns ("", false) when the
// env var is unset/empty, or when the named file can't be read — in the latter
// case it logs a non-secret error so a misconfiguration is visible, and the
// downstream destination fails fast on the resulting empty credential rather
// than running half-configured. The file's contents are never logged.
func envStringFromFile(key string) (string, bool) {
	path, ok := os.LookupEnv(key)
	if !ok || path == "" {
		return "", false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		log.Printf("key:%s error reading secret file: %v", key, err)
		return "", false
	}
	return strings.TrimSpace(string(b)), true
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
	if v, ok := envUint32("POLL_JITTER_PCT"); ok {
		c.PollJitterPct = v
		logEnv("POLL_JITTER_PCT", fmt.Sprintf("c.PollJitterPct:%d", v), debugLevel)
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
	if v, ok := envUint32("ENVELOPE_FLUSH_BYTES"); ok {
		c.EnvelopeFlushThresholdBytes = v
		logEnv("ENVELOPE_FLUSH_BYTES", fmt.Sprintf("c.EnvelopeFlushThresholdBytes:%d", v), debugLevel)
	}
	if v, ok := envUint32("ENVELOPE_FLUSH_ROWS"); ok {
		c.EnvelopeFlushThresholdRows = v
		logEnv("ENVELOPE_FLUSH_ROWS", fmt.Sprintf("c.EnvelopeFlushThresholdRows:%d", v), debugLevel)
	}
	if v, ok := envString("KAFKA_COMPRESSION"); ok {
		c.KafkaCompression = v
		logEnv("KAFKA_COMPRESSION", fmt.Sprintf("c.KafkaCompression:%s", v), debugLevel)
	}
	if v, ok := envString("S3_ENDPOINT"); ok {
		c.S3Endpoint = v
		logEnv("S3_ENDPOINT", fmt.Sprintf("c.S3Endpoint:%s", v), debugLevel)
	}
	if v, ok := envString("S3_BUCKET"); ok {
		c.S3Bucket = v
		logEnv("S3_BUCKET", fmt.Sprintf("c.S3Bucket:%s", v), debugLevel)
	}
	if v, ok := envString("S3_PREFIX"); ok {
		c.S3Prefix = v
		logEnv("S3_PREFIX", fmt.Sprintf("c.S3Prefix:%s", v), debugLevel)
	}
	if v, ok := envString("S3_ACCESS_KEY"); ok {
		c.S3AccessKey = v
		// Intentionally NOT logging the access key value — only that
		// the env var was set. Same for S3_SECRET_KEY below.
		logEnv("S3_ACCESS_KEY", "set", debugLevel)
	}
	if v, ok := envString("S3_SECRET_KEY"); ok {
		c.S3SecretKey = v
		logEnv("S3_SECRET_KEY", "set", debugLevel)
	}
	// File-based variants (Docker `_FILE` convention). Applied after the
	// inline S3_ACCESS_KEY / S3_SECRET_KEY blocks so a mounted/baked secret
	// file wins when both are set. The env var holds only the path; the
	// credential bytes live in the file. Value is never logged.
	if v, ok := envStringFromFile("S3_ACCESS_KEY_FILE"); ok {
		c.S3AccessKey = v
		logEnv("S3_ACCESS_KEY_FILE", "set", debugLevel)
	}
	if v, ok := envStringFromFile("S3_SECRET_KEY_FILE"); ok {
		c.S3SecretKey = v
		logEnv("S3_SECRET_KEY_FILE", "set", debugLevel)
	}
	if v, ok := envString("S3_REGION"); ok {
		c.S3Region = v
		logEnv("S3_REGION", fmt.Sprintf("c.S3Region:%s", v), debugLevel)
	}
	if v, ok := envBool("S3_SKIP_BUCKET_PROBE"); ok {
		c.S3SkipBucketProbe = v
		logEnv("S3_SKIP_BUCKET_PROBE", fmt.Sprintf("c.S3SkipBucketProbe:%t", v), debugLevel)
	}
	if v, ok := envUint32("S3_PARQUET_FLUSH_BYTES"); ok {
		c.S3ParquetFlushThresholdBytes = v
		logEnv("S3_PARQUET_FLUSH_BYTES", fmt.Sprintf("c.S3ParquetFlushThresholdBytes:%d", v), debugLevel)
	}
	if d, ok := envDuration("S3_FLUSH_INTERVAL"); ok {
		c.S3FlushInterval = durationpb.New(d)
		logEnv("S3_FLUSH_INTERVAL", fmt.Sprintf("c.S3FlushInterval:%s", c.S3FlushInterval.String()), debugLevel)
	}
	if v, ok := envUint32("S3_FLUSH_JITTER_PCT"); ok {
		c.S3FlushJitterPct = v
		logEnv("S3_FLUSH_JITTER_PCT", fmt.Sprintf("c.S3FlushJitterPct:%d", v), debugLevel)
	}
	if v, ok := envUint32("S3_FLUSH_THRESHOLD_JITTER_PCT"); ok {
		c.S3FlushThresholdJitterPct = v
		logEnv("S3_FLUSH_THRESHOLD_JITTER_PCT", fmt.Sprintf("c.S3FlushThresholdJitterPct:%d", v), debugLevel)
	}
	if v, ok := envUint32("S3_UPLOAD_MAX_ATTEMPTS"); ok {
		c.S3UploadMaxAttempts = v
		logEnv("S3_UPLOAD_MAX_ATTEMPTS", fmt.Sprintf("c.S3UploadMaxAttempts:%d", v), debugLevel)
	}
	if d, ok := envDuration("S3_UPLOAD_BACKOFF_CAP"); ok {
		c.S3UploadBackoffCap = durationpb.New(d)
		logEnv("S3_UPLOAD_BACKOFF_CAP", fmt.Sprintf("c.S3UploadBackoffCap:%s", c.S3UploadBackoffCap.String()), debugLevel)
	}
	if d, ok := envDuration("RECONCILE_FREQUENCY"); ok {
		c.ReconcileFrequency = durationpb.New(d)
		logEnv("RECONCILE_FREQUENCY", fmt.Sprintf("c.ReconcileFrequency:%s", c.ReconcileFrequency.String()), debugLevel)
	}
	if v, ok := envBool("RECONCILE_BEFORE_POLL"); ok {
		c.ReconcileBeforePoll = v
		logEnv("RECONCILE_BEFORE_POLL", fmt.Sprintf("c.ReconcileBeforePoll:%v", v), debugLevel)
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

// envOverrideLabeling applies the five env vars that stamp WHO and WHERE on
// every record: LABEL, TAG, LOCATION, XTCP_HOSTNAME and CONTAINER_ID_RESOLVE.
//
// It used to apply twenty-four, of which its name covered five. The other
// nineteen were the sixteen ENRICH_*/UPLINK_*/IPMETA_*/ASN_*/LLDP*/NSID vars,
// now in envOverrideEnrichment, and three that were simply misfiled -
// IPV4_TTL, IPV6_HOP_LIMIT and GRPC_PORT, which configure xtcp2's own TCP
// listeners and are now in envOverrideListeners where the rest of that domain
// lives. The split was forced by funlen at 72 statements, but the seam it
// needed was already mis-drawn.
func envOverrideLabeling(c *xtcp_config.XtcpConfig, debugLevel uint) {
	if v, ok := envString("LABEL"); ok {
		c.Label = v
		logEnv("LABEL", fmt.Sprintf("c.Label:%s", v), debugLevel)
	}
	if v, ok := envString("TAG"); ok {
		c.Tag = v
		logEnv("TAG", fmt.Sprintf("c.Tag:%s", v), debugLevel)
	}
	if v, ok := envString("LOCATION"); ok {
		c.Location = v
		logEnv("LOCATION", fmt.Sprintf("c.Location:%s", v), debugLevel)
	}
	if v, ok := envString("XTCP_HOSTNAME"); ok {
		c.Hostname = v
		logEnv("XTCP_HOSTNAME", fmt.Sprintf("c.Hostname:%s", v), debugLevel)
	}
	if v, ok := envBool("CONTAINER_ID_RESOLVE"); ok {
		c.ResolveContainerId = v
		logEnv("CONTAINER_ID_RESOLVE", fmt.Sprintf("c.ResolveContainerId:%t", v), debugLevel)
	}
}

// envOverrideEnrichment applies the sixteen best-effort metadata-enrichment env
// vars - exactly the set defineEnrichmentFlags registers, which is what makes
// the pairing checkable rather than asserted.
//
// Split out of envOverrideLabeling, whose name covered five of its twenty-four
// vars and whose 72 statements were over funlen's ceiling. Registered in
// environmentOverrideConfig immediately after envOverrideLabeling so the
// 5-then-16 relative order is preserved; the vars write disjoint fields, so the
// order is not load-bearing, but keeping it means the split cannot be the cause
// if something downstream turns out to care.
func envOverrideEnrichment(c *xtcp_config.XtcpConfig, debugLevel uint) {
	if v, ok := envBool("ENRICH_CONTAINER"); ok {
		c.EnrichContainerEnable = v
		logEnv("ENRICH_CONTAINER", fmt.Sprintf("c.EnrichContainerEnable:%t", v), debugLevel)
	}
	if v, ok := envString("DOCKER_SOCKET"); ok {
		c.DockerSocketPath = v
		logEnv("DOCKER_SOCKET", fmt.Sprintf("c.DockerSocketPath:%s", v), debugLevel)
	}
	if v, ok := envBool("ENRICH_LLDP"); ok {
		c.EnrichLldpEnable = v
		logEnv("ENRICH_LLDP", fmt.Sprintf("c.EnrichLldpEnable:%t", v), debugLevel)
	}
	if v, ok := envString("LLDPD_SOCKET"); ok {
		c.LldpdSocketPath = v
		logEnv("LLDPD_SOCKET", fmt.Sprintf("c.LldpdSocketPath:%s", v), debugLevel)
	}
	if v, ok := envString("LLDPD_VERSION_HINT"); ok {
		c.LldpdVersionHint = v
		logEnv("LLDPD_VERSION_HINT", fmt.Sprintf("c.LldpdVersionHint:%s", v), debugLevel)
	}
	if v, ok := envBool("ENRICH_NIC"); ok {
		c.EnrichNicEnable = v
		logEnv("ENRICH_NIC", fmt.Sprintf("c.EnrichNicEnable:%t", v), debugLevel)
	}
	if v, ok := envBool("ENRICH_ASN"); ok {
		c.EnrichAsnEnable = v
		logEnv("ENRICH_ASN", fmt.Sprintf("c.EnrichAsnEnable:%t", v), debugLevel)
	}
	if v, ok := envString("ASN_DB_PATH"); ok {
		c.AsnDbPath = v
		logEnv("ASN_DB_PATH", fmt.Sprintf("c.AsnDbPath:%s", v), debugLevel)
	}
	if d, ok := envDuration("ASN_REFRESH_INTERVAL"); ok {
		c.AsnRefreshInterval = durationpb.New(d)
		logEnv("ASN_REFRESH_INTERVAL", fmt.Sprintf("c.AsnRefreshInterval:%s", c.AsnRefreshInterval.String()), debugLevel)
	}
	if v, ok := envString("IPMETA_BOOTSTRAP_PATH"); ok {
		c.IpmetaBootstrapPath = v
		logEnv("IPMETA_BOOTSTRAP_PATH", fmt.Sprintf("c.IpmetaBootstrapPath:%s", v), debugLevel)
	}
	if v, ok := envString("IPMETA_CACHE_PATH"); ok {
		c.IpmetaCachePath = v
		logEnv("IPMETA_CACHE_PATH", fmt.Sprintf("c.IpmetaCachePath:%s", v), debugLevel)
	}
	if v, ok := envBool("ENRICH_LOCALITY"); ok {
		c.EnrichLocalityEnable = v
		logEnv("ENRICH_LOCALITY", fmt.Sprintf("c.EnrichLocalityEnable:%t", v), debugLevel)
	}
	if d, ok := envDuration("LOCALITY_REFRESH_INTERVAL"); ok {
		c.LocalityRefreshInterval = durationpb.New(d)
		logEnv("LOCALITY_REFRESH_INTERVAL", fmt.Sprintf("c.LocalityRefreshInterval:%s", c.LocalityRefreshInterval.String()), debugLevel)
	}
	if v, ok := envUint32("UPLINK_COUNT"); ok {
		c.UplinkCount = v
		logEnv("UPLINK_COUNT", fmt.Sprintf("c.UplinkCount:%d", v), debugLevel)
	}
	if v, ok := envString("UPLINK_INTERFACES"); ok {
		c.UplinkInterfaces = splitCSV(v)
		logEnv("UPLINK_INTERFACES", fmt.Sprintf("c.UplinkInterfaces:%v", c.UplinkInterfaces), debugLevel)
	}
	if v, ok := envBool("POPULATE_NSID"); ok {
		c.PopulateNsid = v
		logEnv("POPULATE_NSID", fmt.Sprintf("c.PopulateNsid:%t", v), debugLevel)
	}
}

func envOverrideListeners(c *xtcp_config.XtcpConfig, debugLevel uint) {
	if v, ok := envString("GRPC_LISTEN_NETWORK"); ok {
		ep := ensureGrpcListener(c)
		n, err := listener.ParseNetwork(v)
		if err == nil {
			ep.Network = n
			logEnv("GRPC_LISTEN_NETWORK", fmt.Sprintf("c.GrpcListener.Network:%s", n.String()), debugLevel)
		}
	}
	if v, ok := envString("GRPC_LISTEN_ADDRESS"); ok {
		ep := ensureGrpcListener(c)
		ep.Address = v
		logEnv("GRPC_LISTEN_ADDRESS", fmt.Sprintf("c.GrpcListener.Address:%s", v), debugLevel)
	}
	if v, ok := envUint32("GRPC_UNIX_SOCKET_MODE"); ok {
		ep := ensureGrpcListener(c)
		ep.UnixSocketMode = v
		logEnv("GRPC_UNIX_SOCKET_MODE", fmt.Sprintf("c.GrpcListener.UnixSocketMode:%#o", v), debugLevel)
	}
	if v, ok := envBool("GRPC_UNLINK_STALE_UNIX_SOCKET"); ok {
		ep := ensureGrpcListener(c)
		ep.UnlinkStaleUnixSocket = v
		logEnv("GRPC_UNLINK_STALE_UNIX_SOCKET", fmt.Sprintf("c.GrpcListener.UnlinkStaleUnixSocket:%t", v), debugLevel)
	}

	if v, ok := envString("PROM_LISTEN_NETWORK"); ok && strings.TrimSpace(v) != "" {
		ep := ensurePrometheusListener(c)
		n, err := listener.ParseNetwork(v)
		if err == nil {
			ep.Network = n
			logEnv("PROM_LISTEN_NETWORK", fmt.Sprintf("c.PrometheusListener.Network:%s", n.String()), debugLevel)
		}
	}
	if v, ok := envString("PROM_LISTEN"); ok && c.PrometheusListener != nil {
		c.PrometheusListener.Address = v
		logEnv("PROM_LISTEN", fmt.Sprintf("c.PrometheusListener.Address:%s", v), debugLevel)
	}
	if v, ok := envUint32("PROM_UNIX_SOCKET_MODE"); ok {
		ep := ensurePrometheusListener(c)
		ep.UnixSocketMode = v
		logEnv("PROM_UNIX_SOCKET_MODE", fmt.Sprintf("c.PrometheusListener.UnixSocketMode:%#o", v), debugLevel)
	}
	if v, ok := envBool("PROM_UNLINK_STALE_UNIX_SOCKET"); ok {
		ep := ensurePrometheusListener(c)
		ep.UnlinkStaleUnixSocket = v
		logEnv("PROM_UNLINK_STALE_UNIX_SOCKET", fmt.Sprintf("c.PrometheusListener.UnlinkStaleUnixSocket:%t", v), debugLevel)
	}

	// IPV4_TTL, IPV6_HOP_LIMIT and GRPC_PORT, moved here from
	// envOverrideLabeling, which had no claim on them: the first two set the
	// outgoing TTL/hop limit for xtcp2's OWN TCP listeners (Prometheus +
	// gRPC), per their flag usage, and the third is the gRPC port.
	//
	// They are the only writes in this function that go to a top-level scalar
	// rather than through ensureGrpcListener/ensurePrometheusListener, so none
	// of them needs an endpoint to exist first. c.GrpcPort is read only by
	// pkg/xtcp/grpc_server.go, as the TCP fallback when GrpcListener carries no
	// address, and that is long after every override has run - which is why
	// moving GRPC_PORT from before the GRPC_LISTEN_* vars to after them is
	// behavior-preserving rather than merely untested.
	if v, ok := envUint32("IPV4_TTL"); ok {
		c.Ipv4Ttl = v
		logEnv("IPV4_TTL", fmt.Sprintf("c.Ipv4Ttl:%d", v), debugLevel)
	}
	if v, ok := envUint32("IPV6_HOP_LIMIT"); ok {
		c.Ipv6HopLimit = v
		logEnv("IPV6_HOP_LIMIT", fmt.Sprintf("c.Ipv6HopLimit:%d", v), debugLevel)
	}
	if v, ok := envUint32("GRPC_PORT"); ok {
		c.GrpcPort = v
		logEnv("GRPC_PORT", fmt.Sprintf("c.GrpcPort:%d", v), debugLevel)
	}
}

func ensureGrpcListener(c *xtcp_config.XtcpConfig) *xtcp_config.ListenerEndpoint {
	if c.GrpcListener == nil {
		c.GrpcListener = &xtcp_config.ListenerEndpoint{UnlinkStaleUnixSocket: unlinkStaleSocketCst}
	}
	return c.GrpcListener
}

func ensurePrometheusListener(c *xtcp_config.XtcpConfig) *xtcp_config.ListenerEndpoint {
	if c.PrometheusListener == nil {
		c.PrometheusListener = &xtcp_config.ListenerEndpoint{UnlinkStaleUnixSocket: unlinkStaleSocketCst}
	}
	return c.PrometheusListener
}

func applyPrometheusListenerAddress(c *xtcp_config.XtcpConfig, promListen string) {
	if c.PrometheusListener != nil && c.PrometheusListener.Address == "" {
		c.PrometheusListener.Address = promListen
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
	fmt.Println("c.EnvelopeFlushThresholdBytes:", c.EnvelopeFlushThresholdBytes)
	fmt.Println("c.EnvelopeFlushThresholdRows:", c.EnvelopeFlushThresholdRows)
	fmt.Println("c.KafkaCompression:", c.KafkaCompression)
	fmt.Println("c.S3Endpoint:", c.S3Endpoint)
	fmt.Println("c.S3Bucket:", c.S3Bucket)
	fmt.Println("c.S3Prefix:", c.S3Prefix)
	// c.S3AccessKey / c.S3SecretKey intentionally NOT printed.
	fmt.Println("c.S3Region:", c.S3Region)
	fmt.Println("c.S3ParquetFlushThresholdBytes:", c.S3ParquetFlushThresholdBytes)
	fmt.Println("c.PollJitterPct:", c.PollJitterPct)
	fmt.Println("c.S3FlushInterval:", c.S3FlushInterval.AsDuration())
	fmt.Println("c.S3FlushJitterPct:", c.S3FlushJitterPct)
	fmt.Println("c.S3FlushThresholdJitterPct:", c.S3FlushThresholdJitterPct)
	fmt.Println("c.S3UploadMaxAttempts:", c.S3UploadMaxAttempts)
	fmt.Println("c.S3UploadBackoffCap:", c.S3UploadBackoffCap.AsDuration())
	fmt.Println("c.ReconcileFrequency:", c.ReconcileFrequency.AsDuration())
	fmt.Println("c.ReconcileBeforePoll:", c.ReconcileBeforePoll)
	fmt.Println("c.Dest:", c.Dest)
	fmt.Println("c.DestWriteFiles:", c.DestWriteFiles)
	fmt.Println("c.Topic:", c.Topic)
	fmt.Println("c.XtcpProtoFile:", c.XtcpProtoFile)
	fmt.Println("c.KafkaSchemaUrl:", c.KafkaSchemaUrl)
	fmt.Println("c.KafkaProduceTimeout:", c.KafkaProduceTimeout.AsDuration())
	fmt.Println("c.DebugLevel:", c.DebugLevel)
	fmt.Println("c.Ipv4Ttl:", c.Ipv4Ttl)
	fmt.Println("c.Ipv6HopLimit:", c.Ipv6HopLimit)
	if c.ListenerAuth == nil {
		fmt.Println("c.ListenerAuth.Mode: disabled")
		fmt.Println("c.ListenerAuth.RawToken: set: false")
		fmt.Println("c.ListenerAuth.HmacSharedKey: set: false")
	} else {
		fmt.Println("c.ListenerAuth.Mode:", listenerauth.ModeString(c.ListenerAuth.GetMode()))
		fmt.Println("c.ListenerAuth.RawToken: set:", c.ListenerAuth.GetRawToken() != "")
		fmt.Println("c.ListenerAuth.HmacSharedKey: set:", c.ListenerAuth.GetHmacSharedKey() != "")
		fmt.Println("c.ListenerAuth.SignedTokenSkewMinutes:", c.ListenerAuth.GetSignedTokenSkewMinutes())
	}
	fmt.Println("c.Label:", c.Label)
	fmt.Println("c.Tag:", c.Tag)
	fmt.Println("c.Location:", c.Location)
	fmt.Println("c.Hostname:", c.Hostname)
	fmt.Println("c.DaemonVersion:", c.DaemonVersion)
	fmt.Println("c.ResolveContainerId:", c.ResolveContainerId)
	fmt.Println("c.EnrichContainerEnable:", c.EnrichContainerEnable)
	fmt.Println("c.DockerSocketPath:", c.DockerSocketPath)
	fmt.Println("c.EnrichLldpEnable:", c.EnrichLldpEnable)
	fmt.Println("c.LldpdSocketPath:", c.LldpdSocketPath)
	fmt.Println("c.LldpdVersionHint:", c.LldpdVersionHint)
	fmt.Println("c.EnrichNicEnable:", c.EnrichNicEnable)
	fmt.Println("c.UplinkCount:", c.UplinkCount)
	fmt.Println("c.UplinkInterfaces:", c.UplinkInterfaces)
	fmt.Println("c.PopulateNsid:", c.PopulateNsid)
	fmt.Println("c.EnrichAsnEnable:", c.EnrichAsnEnable)
	fmt.Println("c.AsnDbPath:", c.AsnDbPath)
	fmt.Println("c.AsnRefreshInterval:", c.AsnRefreshInterval)
	fmt.Println("c.EnrichLocalityEnable:", c.EnrichLocalityEnable)
	fmt.Println("compiledInEnrichers:", xtcp.CompiledInEnrichers())
	fmt.Println("c.LocalityRefreshInterval:", c.LocalityRefreshInterval)
	fmt.Println("c.GrpcPort:", c.GrpcPort)
	if c.GrpcListener == nil {
		fmt.Println("c.GrpcListener: unset")
	} else {
		fmt.Printf("c.GrpcListener: network=%s address=%s mode=%#o unlinkStale=%t\n", c.GrpcListener.GetNetwork(), c.GrpcListener.GetAddress(), c.GrpcListener.GetUnixSocketMode(), c.GrpcListener.GetUnlinkStaleUnixSocket())
	}
	if c.PrometheusListener == nil {
		fmt.Println("c.PrometheusListener: unset")
	} else {
		fmt.Printf("c.PrometheusListener: network=%s address=%s mode=%#o unlinkStale=%t\n", c.PrometheusListener.GetNetwork(), c.PrometheusListener.GetAddress(), c.PrometheusListener.GetUnixSocketMode(), c.PrometheusListener.GetUnlinkStaleUnixSocket())
	}
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

	if str == "default" {
		for _, item := range xtcp.GetDefaultDeserializers() {
			des.Enabled[item] = true
		}
		return des
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
