package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_config"
)

// envHelperReset tears down any flag-package state captured by other
// tests that called flag.X(). cmd/xtcp2 uses the global flag set, and
// re-entering defineFlags after flag.Parse would panic with
// "flag redefined". Each test that needs a fresh flagset calls this.
func envHelperReset(t *testing.T) {
	t.Helper()
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
}

// captureLog redirects the standard log output for the duration of the
// callback so debug-printf branches don't pollute test output.
func captureLog(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	orig := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(orig) })
	fn()
	return buf.String()
}

// ───────────────────────────────────────────────────────────────────────
// Typed-env helpers
// ───────────────────────────────────────────────────────────────────────

func TestEnvUint64(t *testing.T) {
	cases := []struct {
		name    string
		key     string
		set     bool
		val     string
		wantVal uint64
		wantOK  bool
	}{
		{name: "unset", key: "TEST_U64_UNSET", set: false, wantOK: false},
		{name: "valid", key: "TEST_U64_OK", set: true, val: "42", wantVal: 42, wantOK: true},
		{name: "zero", key: "TEST_U64_ZERO", set: true, val: "0", wantVal: 0, wantOK: true},
		{name: "unparseable", key: "TEST_U64_BAD", set: true, val: "abc", wantOK: false},
		{name: "empty", key: "TEST_U64_EMPTY", set: true, val: "", wantOK: false},
		// Negative values used to ParseInt-then-cast through uint64,
		// silently producing MaxUint64. Now rejected via ParseUint.
		{name: "negative", key: "TEST_U64_NEG", set: true, val: "-1", wantOK: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.set {
				t.Setenv(tc.key, tc.val)
			}
			got, ok := envUint64(tc.key)
			if ok != tc.wantOK {
				t.Fatalf("ok=%v want %v", ok, tc.wantOK)
			}
			if got != tc.wantVal {
				t.Fatalf("got %d want %d", got, tc.wantVal)
			}
		})
	}
}

func TestEnvUint32(t *testing.T) {
	t.Setenv("TEST_U32_OK", "12345")
	if v, ok := envUint32("TEST_U32_OK"); !ok || v != 12345 {
		t.Fatalf("envUint32 ok=%v v=%d", ok, v)
	}
	if _, ok := envUint32("TEST_U32_UNSET"); ok {
		t.Fatal("unset key should return ok=false")
	}
	t.Setenv("TEST_U32_BAD", "not-a-number")
	if _, ok := envUint32("TEST_U32_BAD"); ok {
		t.Fatal("unparseable should return ok=false")
	}
	// Negative values previously wrapped to MaxUint32 via Atoi+cast.
	// ParseUint rejects them.
	t.Setenv("TEST_U32_NEG", "-1")
	if _, ok := envUint32("TEST_U32_NEG"); ok {
		t.Fatal("negative value should return ok=false (would silently wrap to MaxUint32 pre-fix)")
	}
}

func TestEnvDuration(t *testing.T) {
	t.Setenv("TEST_DUR_OK", "3s")
	if d, ok := envDuration("TEST_DUR_OK"); !ok || d != 3*time.Second {
		t.Fatalf("envDuration ok=%v d=%s", ok, d)
	}
	if _, ok := envDuration("TEST_DUR_UNSET"); ok {
		t.Fatal("unset key should return ok=false")
	}
	t.Setenv("TEST_DUR_BAD", "infinity")
	if _, ok := envDuration("TEST_DUR_BAD"); ok {
		t.Fatal("unparseable should return ok=false")
	}
}

func TestEnvString(t *testing.T) {
	t.Setenv("TEST_STR_OK", "hello")
	if v, ok := envString("TEST_STR_OK"); !ok || v != "hello" {
		t.Fatalf("envString ok=%v v=%q", ok, v)
	}
	if _, ok := envString("TEST_STR_UNSET"); ok {
		t.Fatal("unset key should return ok=false")
	}
	// Empty-string set: env reads it as set with empty value.
	t.Setenv("TEST_STR_EMPTY", "")
	if v, ok := envString("TEST_STR_EMPTY"); !ok || v != "" {
		t.Fatalf("empty-string env should be ok=true v=\"\"; got ok=%v v=%q", ok, v)
	}
}

// logEnv only prints when debugLevel > 10. Both branches are tested
// here so the function reaches 100%.
func TestLogEnv(t *testing.T) {
	out := captureLog(t, func() { logEnv("KEY", "x", 11) })
	if !strings.Contains(out, "key:KEY") || !strings.Contains(out, "x") {
		t.Fatalf("logEnv should have printed; got %q", out)
	}
	out = captureLog(t, func() { logEnv("KEY", "x", 0) })
	if out != "" {
		t.Fatalf("logEnv should have been silent at low debug; got %q", out)
	}
}

// ───────────────────────────────────────────────────────────────────────
// Per-category env override helpers
// ───────────────────────────────────────────────────────────────────────

func TestEnvOverridePolling(t *testing.T) {
	c := &xtcp_config.XtcpConfig{}
	t.Setenv("NLTIMEOUTMS", "250")
	t.Setenv("POLL_FREQUENCY", "5s")
	t.Setenv("POLL_TIMEOUT", "2s")
	t.Setenv("MAX_LOOPS", "100")
	t.Setenv("MODULUS", "7")
	envOverridePolling(c, 0)
	if c.NlTimeoutMilliseconds != 250 {
		t.Errorf("NlTimeoutMilliseconds = %d, want 250", c.NlTimeoutMilliseconds)
	}
	if c.PollFrequency == nil || c.PollFrequency.AsDuration() != 5*time.Second {
		t.Errorf("PollFrequency = %v", c.PollFrequency)
	}
	if c.PollTimeout == nil || c.PollTimeout.AsDuration() != 2*time.Second {
		t.Errorf("PollTimeout = %v", c.PollTimeout)
	}
	if c.MaxLoops != 100 {
		t.Errorf("MaxLoops = %d, want 100", c.MaxLoops)
	}
	if c.Modulus != 7 {
		t.Errorf("Modulus = %d, want 7", c.Modulus)
	}
}

func TestEnvOverridePolling_unset(t *testing.T) {
	// With no env vars set, the config struct keeps its zero values.
	c := &xtcp_config.XtcpConfig{NlTimeoutMilliseconds: 999}
	envOverridePolling(c, 0)
	if c.NlTimeoutMilliseconds != 999 {
		t.Errorf("unset env should leave NlTimeoutMilliseconds at 999, got %d",
			c.NlTimeoutMilliseconds)
	}
}

func TestEnvOverrideNetlinker(t *testing.T) {
	c := &xtcp_config.XtcpConfig{}
	t.Setenv("NETLINKERS", "8")
	t.Setenv("NETLINKERS_DONE_CHAN_SIZE", "64")
	t.Setenv("NLMSQSEQ", "1234")
	envOverrideNetlinker(c, 0)
	if c.Netlinkers != 8 || c.NetlinkersDoneChanSize != 64 || c.NlmsgSeq != 1234 {
		t.Errorf("envOverrideNetlinker mismatch: %+v", c)
	}
}

func TestEnvOverridePacket(t *testing.T) {
	c := &xtcp_config.XtcpConfig{}
	t.Setenv("PACKET_SIZE", "4096")
	t.Setenv("PACKETSIZEMPLY", "2")
	t.Setenv("WRITEFILES", "5")
	t.Setenv("CAPTUREPATH", "/tmp/captures/")
	envOverridePacket(c, 0)
	if c.PacketSize != 4096 || c.PacketSizeMply != 2 || c.WriteFiles != 5 ||
		c.CapturePath != "/tmp/captures/" {
		t.Errorf("envOverridePacket mismatch: %+v", c)
	}
}

func TestEnvOverrideMarshalAndDest(t *testing.T) {
	c := &xtcp_config.XtcpConfig{}
	t.Setenv("MARSHAL", "protoJson")
	t.Setenv("DEST", "null")
	t.Setenv("DEST_WRITE_FILES", "3")
	envOverrideMarshalAndDest(c, 0)
	if c.MarshalTo != "protoJson" ||
		c.Dest != "null" || c.DestWriteFiles != 3 {
		t.Errorf("envOverrideMarshalAndDest mismatch: %+v", c)
	}
}

func TestEnvOverrideKafka(t *testing.T) {
	c := &xtcp_config.XtcpConfig{}
	t.Setenv("TOPIC", "xtcp-test")
	t.Setenv("XTCP_PROTO_FILE", "/srv/proto/xtcp.proto")
	t.Setenv("KAFKA_SCHEMA_URL", "http://schema.local:8081")
	t.Setenv("KAFKA_PRODUCE_TIMEOUT", "750ms")
	envOverrideKafka(c, 0)
	if c.Topic != "xtcp-test" || c.XtcpProtoFile != "/srv/proto/xtcp.proto" ||
		c.KafkaSchemaUrl != "http://schema.local:8081" {
		t.Errorf("envOverrideKafka string fields mismatch: %+v", c)
	}
	if c.KafkaProduceTimeout == nil || c.KafkaProduceTimeout.AsDuration() != 750*time.Millisecond {
		t.Errorf("KafkaProduceTimeout = %v, want 750ms", c.KafkaProduceTimeout)
	}
}

func TestEnvOverrideLabeling(t *testing.T) {
	c := &xtcp_config.XtcpConfig{}
	t.Setenv("LABEL", "prod")
	t.Setenv("TAG", "host=foo")
	t.Setenv("GRPC_PORT", "9000")
	envOverrideLabeling(c, 0)
	if c.Label != "prod" || c.Tag != "host=foo" || c.GrpcPort != 9000 {
		t.Errorf("envOverrideLabeling mismatch: %+v", c)
	}
}

// environmentOverrideConfig wires the six helpers above together. One
// integrated test verifies every category gets dispatched.
func TestEnvironmentOverrideConfig(t *testing.T) {
	c := &xtcp_config.XtcpConfig{}
	t.Setenv("NLTIMEOUTMS", "111")
	t.Setenv("NETLINKERS", "2")
	t.Setenv("PACKET_SIZE", "8192")
	t.Setenv("MARSHAL", "msgpack")
	t.Setenv("TOPIC", "xtcp")
	t.Setenv("LABEL", "qa")
	environmentOverrideConfig(c, 0)
	if c.NlTimeoutMilliseconds != 111 || c.Netlinkers != 2 || c.PacketSize != 8192 ||
		c.MarshalTo != "msgpack" || c.Topic != "xtcp" || c.Label != "qa" {
		t.Errorf("environmentOverrideConfig dispatch failed: %+v", c)
	}
}

// ───────────────────────────────────────────────────────────────────────
// Pointer-mutating overrides for prom + goMaxProcs + debugLevel
// ───────────────────────────────────────────────────────────────────────

func TestEnvironmentOverrideProm(t *testing.T) {
	listen := ":9000"
	path := "/m"
	t.Setenv("PROM_LISTEN", ":9999")
	t.Setenv("PROM_PATH", "/metrics2")
	environmentOverrideProm(&listen, &path, 0)
	if listen != ":9999" || path != "/metrics2" {
		t.Errorf("environmentOverrideProm mismatch: listen=%q path=%q", listen, path)
	}
}

func TestEnvironmentOverrideProm_debugLog(t *testing.T) {
	listen := ":9000"
	path := "/m"
	t.Setenv("PROM_LISTEN", ":1111")
	t.Setenv("PROM_PATH", "/p")
	environmentOverrideProm(&listen, &path, 11) // > 10 → log.Printf branch
	if listen != ":1111" || path != "/p" {
		t.Errorf("debug-log run still must set values; got listen=%q path=%q", listen, path)
	}
}

func TestEnvironmentOverrideDebugLevel_debugLog(t *testing.T) {
	var d uint = 5
	t.Setenv("DEBUG_LEVEL", "9")
	environmentOverrideDebugLevel(&d, 11) // > 10 → log branch
	if d != 9 {
		t.Errorf("d = %d, want 9", d)
	}
}

func TestEnvironmentOverrideGoMaxProcs_debugLog(t *testing.T) {
	var p uint = 4
	t.Setenv("GOMAXPROCS", "8")
	environmentOverrideGoMaxProcs(&p, 11) // > 10 → log branch
	if p != 8 {
		t.Errorf("p = %d, want 8", p)
	}
}

func TestAwaitSignalAndShutdown_completeBeforeTimeout(t *testing.T) {
	sigs := make(chan os.Signal, 1)
	complete := make(chan struct{}, 1)
	_, cancel := context.WithCancel(context.Background())
	var cancelCalled bool
	wrap := func() {
		cancelCalled = true
		cancel()
	}
	done := make(chan struct{})
	go func() {
		awaitSignalAndShutdown(sigs, wrap, complete, 200*time.Millisecond, false)
		close(done)
	}()
	sigs <- syscall.SIGTERM
	time.Sleep(20 * time.Millisecond)
	complete <- struct{}{}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("awaitSignalAndShutdown did not return on complete")
	}
	if !cancelCalled {
		t.Error("cancel() was not called")
	}
}

// servePromHandler error path with an invalid address forces
// ListenAndServe to fail; fatalf captures the message.
func TestServePromHandler_bindError(t *testing.T) {
	prev := fatalf
	var captured string
	fatalf = func(format string, args ...any) {
		captured = fmt.Sprintf(format, args...)
	}
	t.Cleanup(func() { fatalf = prev })

	servePromHandler("invalid-host:-1")
	if !strings.Contains(captured, "prometheus error") {
		t.Errorf("fatalf not invoked; got %q", captured)
	}
}

// runMain with a -v flag short-circuits before any daemon launch.
// Reset flag.CommandLine + os.Args around the call so we don't disturb
// global state.
func TestRunMain_version(t *testing.T) {
	envHelperReset(t)
	prevArgs := os.Args
	os.Args = []string{"xtcp2", "-v"}
	t.Cleanup(func() { os.Args = prevArgs })

	// Stub the prom handler starter so it doesn't bind a port.
	prevProm := promHandlerStarter
	promHandlerStarter = func(_, _ string) {}
	t.Cleanup(func() { promHandlerStarter = prevProm })

	// runMain spawns a signal-handler goroutine that blocks on signal.Notify.
	// The goroutine leak is fine for a test that exits quickly.
	captureLog(t, func() {
		if rc := runMain(t.Context()); rc != 0 {
			t.Errorf("rc = %d, want 0", rc)
		}
	})
}

// runMain with -conf short-circuits after building config (also returns 0).
func TestRunMain_conf(t *testing.T) {
	envHelperReset(t)
	prevArgs := os.Args
	os.Args = []string{"xtcp2", "-conf"}
	t.Cleanup(func() { os.Args = prevArgs })

	prevProm := promHandlerStarter
	promHandlerStarter = func(_, _ string) {}
	t.Cleanup(func() { promHandlerStarter = prevProm })

	captureLog(t, func() {
		if rc := runMain(t.Context()); rc != 0 {
			t.Errorf("rc = %d, want 0", rc)
		}
	})
}

// runMain happy path with stubbed daemon: parses flags, runs through
// the full setup, then daemonRunner returns immediately.
func TestRunMain_stubbedDaemon(t *testing.T) {
	envHelperReset(t)
	prevArgs := os.Args
	os.Args = []string{"xtcp2", "-dest", "null"}
	t.Cleanup(func() { os.Args = prevArgs })

	prevProm := promHandlerStarter
	promHandlerStarter = func(_, _ string) {}
	t.Cleanup(func() { promHandlerStarter = prevProm })

	prevDaemon := daemonRunner
	called := false
	daemonRunner = func(_ context.Context, _ context.CancelFunc, _ *xtcp_config.XtcpConfig) {
		called = true
	}
	t.Cleanup(func() { daemonRunner = prevDaemon })

	captureLog(t, func() {
		if rc := runMain(t.Context()); rc != 0 {
			t.Errorf("rc = %d, want 0", rc)
		}
	})
	if !called {
		t.Error("daemonRunner stub was not invoked")
	}
}

func TestInitPromHandler_smoke(t *testing.T) {
	prevMux := http.DefaultServeMux
	http.DefaultServeMux = http.NewServeMux()
	t.Cleanup(func() { http.DefaultServeMux = prevMux })

	prevFatalf := fatalf
	fatalf = func(string, ...any) {} // swallow
	t.Cleanup(func() { fatalf = prevFatalf })

	initPromHandler("/metrics", ":0")
	time.Sleep(10 * time.Millisecond)
}

func TestAwaitSignalAndShutdown_timeoutPath(t *testing.T) {
	sigs := make(chan os.Signal, 1)
	complete := make(chan struct{}) // never signaled
	_, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		awaitSignalAndShutdown(sigs, cancel, complete, 30*time.Millisecond, false)
		close(done)
	}()
	sigs <- syscall.SIGINT
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout path did not fire")
	}
}

func TestEnvironmentOverrideGoMaxProcs_garbage(t *testing.T) {
	var p uint = 4
	t.Setenv("GOMAXPROCS", "not-a-number")
	environmentOverrideGoMaxProcs(&p, 0)
	if p != 4 {
		t.Errorf("garbage env should leave p alone; got %d", p)
	}
}

func TestEnvironmentOverrideProm_unset(t *testing.T) {
	listen := ":9000"
	path := "/m"
	environmentOverrideProm(&listen, &path, 0)
	if listen != ":9000" || path != "/m" {
		t.Errorf("unset env should preserve values; got listen=%q path=%q", listen, path)
	}
}

func TestEnvironmentOverrideDebugLevel(t *testing.T) {
	cases := []struct {
		name        string
		envValue    string
		initial     uint
		want        uint
		description string
	}{
		{"set_to_20", "20", 5, 20, "valid value overwrites"},
		{"garbage_left_alone", "garbage", 7, 7, "unparseable env leaves value alone"},
		// Bug 60 regression: Atoi+uint(i) wrapped negative values to
		// MaxUint, silently turning every `if debugLevel > 10` check
		// into "yes". ParseUint rejects the negative input outright.
		{"negative_rejected", "-5", 9, 9, "negative env rejected, value left alone"},
		{"zero_accepted", "0", 11, 0, "zero is a valid debug level"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := tc.initial
			t.Setenv("DEBUG_LEVEL", tc.envValue)
			environmentOverrideDebugLevel(&d, 0)
			if d != tc.want {
				t.Errorf("%s: d = %d, want %d", tc.description, d, tc.want)
			}
		})
	}
}

func TestEnvironmentOverrideGoMaxProcs(t *testing.T) {
	cases := []struct {
		name     string
		envValue string
		initial  uint
		want     uint
	}{
		{"set_to_16", "16", 4, 16},
		{"garbage_left_alone", "abc", 8, 8},
		{"negative_rejected", "-1", 12, 12}, // bug 60 regression
		{"zero_accepted", "0", 6, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := tc.initial
			t.Setenv("GOMAXPROCS", tc.envValue)
			environmentOverrideGoMaxProcs(&p, 0)
			if p != tc.want {
				t.Errorf("p = %d, want %d", p, tc.want)
			}
		})
	}
}

// ───────────────────────────────────────────────────────────────────────
// getDeserializers — pure comma-separated parsing
// ───────────────────────────────────────────────────────────────────────

func TestGetDeserializers(t *testing.T) {
	cases := []struct {
		name        string
		input       string
		envOverride string
		envSet      bool
		want        []string // keys that must be true; rest must be false/absent
	}{
		{name: "empty", input: "", want: nil},
		{name: "single", input: "info", want: []string{"info"}},
		{name: "multiple", input: "info,cong,vegas", want: []string{"info", "cong", "vegas"}},
		{name: "all_keyword", input: "all", want: nil /* checked separately */},
		{name: "env_overrides_arg", input: "info", envSet: true, envOverride: "vegas",
			want: []string{"vegas"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.envSet {
				t.Setenv("DESERIALIZERS", tc.envOverride)
			} else {
				os.Unsetenv("DESERIALIZERS")
			}
			got := getDeserializers(tc.input)
			if got == nil {
				t.Fatal("getDeserializers returned nil")
			}
			if tc.name == "all_keyword" {
				if len(got.Enabled) < 5 {
					t.Errorf("all_keyword should enable many deserializers; got %d", len(got.Enabled))
				}
				return
			}
			for _, want := range tc.want {
				if !got.Enabled[want] {
					t.Errorf("expected %q enabled; map=%+v", want, got.Enabled)
				}
			}
			if tc.input == "" && len(got.Enabled) != 0 {
				t.Errorf("empty input should produce empty map; got %+v", got.Enabled)
			}
		})
	}
}

// ───────────────────────────────────────────────────────────────────────
// printConfig / printFlags — observability sanity (output not parsed,
// just exercise the branches)
// ───────────────────────────────────────────────────────────────────────

func TestPrintConfig(t *testing.T) {
	c := &xtcp_config.XtcpConfig{
		MarshalTo: "protobufList",
		Dest:      "null",
	}
	r, w, _ := os.Pipe()
	orig := os.Stdout
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = orig })
	done := make(chan struct{})
	var out strings.Builder
	go func() {
		_, _ = io.Copy(&out, r)
		close(done)
	}()
	printConfig(c, "test snapshot")
	_ = w.Close()
	<-done
	if !strings.Contains(out.String(), "test snapshot") || !strings.Contains(out.String(), "protobufList") {
		t.Errorf("printConfig should include comment + fields; got %q", out.String())
	}
}

// printFlags exercises every flag.Println — verifies the function
// doesn't panic on a populated mainFlags.
func TestPrintFlags(t *testing.T) {
	envHelperReset(t)
	f := &mainFlags{}
	// allocate every field as the real defineFlags would.
	n64 := uint64(0)
	d := time.Second
	n := uint(0)
	s := ""
	b := false
	f.nltimeout = &n64
	f.pollFrequency = &d
	f.pollTimeout = &d
	f.maxLoops = &n64
	f.netlinkers = &n
	f.nlmsgSeq = &n
	f.packetSize = &n64
	f.packetSizeMply = &n
	f.writeFiles = &n
	f.capturePath = &s
	f.modulus = &n64
	f.marshal = &s
	f.envelopeFlushBytes = &n
	f.envelopeFlushRows = &n
	f.kafkaCompression = &s
	f.s3Endpoint = &s
	f.s3Bucket = &s
	f.s3Prefix = &s
	f.s3AccessKey = &s
	f.s3SecretKey = &s
	f.s3Region = &s
	f.s3ParquetFlushBytes = &n
	f.pyroscopeUrl = &s
	f.pyroscopeAppName = &s
	f.pyroscopeSampleHz = &n
	f.pyroscopeUploadSec = &n
	f.dest = &s
	f.destWriteFiles = &n
	f.topic = &s
	f.xtcpProtoFile = &s
	f.kafkaSchemaUrl = &s
	f.produceTimeout = &d
	f.label = &s
	f.tag = &s
	f.grpcPort = &n
	f.deserializers = &s
	f.promListen = &s
	f.promPath = &s
	f.goMaxProcs = &n
	f.profileMode = &s
	f.v = &b
	f.conf = &b
	f.d = &n
	f.ioUring = &b
	f.ioUringRecvBatch = &n
	f.ioUringCqeBatch = &n
	// Redirect stdout so the call doesn't litter test output.
	r, w, _ := os.Pipe()
	orig := os.Stdout
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = orig })
	done := make(chan struct{})
	var sink sync.WaitGroup
	sink.Add(1)
	go func() {
		defer sink.Done()
		_, _ = io.Copy(io.Discard, r)
		close(done)
	}()
	printFlags(f)
	_ = w.Close()
	<-done
}

// ───────────────────────────────────────────────────────────────────────
// buildConfig wraps every flag pointer into the protobuf config struct.
// Verify field-for-field mapping with non-default values.
// ───────────────────────────────────────────────────────────────────────

func TestBuildConfig(t *testing.T) {
	envHelperReset(t)
	nl := uint64(150)
	pf := 7 * time.Second
	pt := 3 * time.Second
	ml := uint64(99)
	nlk := uint(3)
	seq := uint(7777)
	psz := uint64(5000)
	psm := uint(2)
	wf := uint(11)
	cp := "/tmp/cap/"
	mod := uint64(13)
	mar := "protoText"
	dst := "udp:127.0.0.1:13000"
	dwf := uint(4)
	topic := "topic1"
	xp := "x.proto"
	ksu := "http://sr"
	pto := 200 * time.Millisecond
	label := "lbl"
	tag := "host=a"
	gp := uint(8888)
	pl := ":9088"
	pp := "/metrics"
	gmp := uint(8)
	pm := ""
	v := false
	conf := false
	d := uint(11)
	iu := true
	iurb := uint(64)
	iucb := uint(128)
	ds := "info,cong"
	f := &mainFlags{
		nltimeout: &nl, pollFrequency: &pf, pollTimeout: &pt, maxLoops: &ml,
		netlinkers: &nlk, nlmsgSeq: &seq, packetSize: &psz, packetSizeMply: &psm,
		writeFiles: &wf, capturePath: &cp, modulus: &mod, marshal: &mar,
		envelopeFlushBytes: &wf, envelopeFlushRows: &wf,
		kafkaCompression:    &mar,
		s3Endpoint:          &mar,
		s3Bucket:            &mar,
		s3Prefix:            &mar,
		s3AccessKey:         &mar,
		s3SecretKey:         &mar,
		s3Region:            &mar,
		s3ParquetFlushBytes: &wf,
		pyroscopeUrl:        &mar,
		pyroscopeAppName:    &mar,
		pyroscopeSampleHz:   &wf,
		pyroscopeUploadSec:  &wf,
		dest:                &dst, destWriteFiles: &dwf,
		topic: &topic, xtcpProtoFile: &xp, kafkaSchemaUrl: &ksu,
		produceTimeout: &pto, label: &label, tag: &tag, grpcPort: &gp,
		deserializers: &ds, promListen: &pl, promPath: &pp, goMaxProcs: &gmp,
		profileMode: &pm, v: &v, conf: &conf, d: &d,
		ioUring: &iu, ioUringRecvBatch: &iurb, ioUringCqeBatch: &iucb,
	}
	des := getDeserializers(*f.deserializers)
	c := buildConfig(f, des)
	checks := []struct {
		field string
		got   any
		want  any
	}{
		{"NlTimeoutMilliseconds", c.NlTimeoutMilliseconds, uint64(150)},
		{"MaxLoops", c.MaxLoops, uint64(99)},
		{"Netlinkers", c.Netlinkers, uint32(3)},
		{"NlmsgSeq", c.NlmsgSeq, uint32(7777)},
		{"PacketSize", c.PacketSize, uint64(5000)},
		{"PacketSizeMply", c.PacketSizeMply, uint32(2)},
		{"WriteFiles", c.WriteFiles, uint32(11)},
		{"CapturePath", c.CapturePath, "/tmp/cap/"},
		{"Modulus", c.Modulus, uint64(13)},
		{"MarshalTo", c.MarshalTo, "protoText"},
		{"Dest", c.Dest, "udp:127.0.0.1:13000"},
		{"DestWriteFiles", c.DestWriteFiles, uint32(4)},
		{"Topic", c.Topic, "topic1"},
		{"XtcpProtoFile", c.XtcpProtoFile, "x.proto"},
		{"KafkaSchemaUrl", c.KafkaSchemaUrl, "http://sr"},
		{"DebugLevel", c.DebugLevel, uint32(11)},
		{"Label", c.Label, "lbl"},
		{"Tag", c.Tag, "host=a"},
		{"GrpcPort", c.GrpcPort, uint32(8888)},
	}
	for _, ck := range checks {
		if ck.got != ck.want {
			t.Errorf("buildConfig: %s = %v, want %v", ck.field, ck.got, ck.want)
		}
	}
	if c.EnabledDeserializers == nil || !c.EnabledDeserializers.Enabled["info"] {
		t.Errorf("EnabledDeserializers should include info: %+v", c.EnabledDeserializers)
	}
}

// startProfile returns a no-op closure for empty mode; cpu/mem modes
// would require disk artifacts so we only test the no-op path here.
func TestStartProfile_emptyMode(t *testing.T) {
	stop := startProfile("", 0)
	if stop == nil {
		t.Fatal("startProfile should always return a non-nil closure")
	}
	// Empty mode → noop Stop().
	stop()
}

// defineFlags must register every flag without panicking and produce
// the correct mainFlags shape (every pointer populated).
func TestDefineFlags(t *testing.T) {
	envHelperReset(t)
	f := defineFlags()
	// Spot-check a handful of pointers; if any are nil, the binary
	// would panic at parse time.
	if f.nltimeout == nil || f.pollFrequency == nil || f.dest == nil ||
		f.grpcPort == nil || f.d == nil || f.profileMode == nil {
		t.Fatalf("defineFlags returned an incomplete mainFlags: %+v", f)
	}
}

// ───────────────────────────────────────────────────────────────────────
// versionString + prepareConfig — split out of main() so the version /
// -conf short-circuits are testable.
// ───────────────────────────────────────────────────────────────────────

func TestVersionString(t *testing.T) {
	// commit/date/version are filled by -ldflags at build time; in the
	// test binary they're empty strings. The function should still
	// produce a sentence with the constant tokens.
	got := versionString()
	if !strings.Contains(got, "xtcp commit:") {
		t.Errorf("versionString missing prefix; got %q", got)
	}
}

func TestPrepareConfig_versionFlag(t *testing.T) {
	envHelperReset(t)
	f := defineFlags()
	tt := true
	f.v = &tt
	c, done := prepareConfig(f)
	if !done {
		t.Error("-v should produce done=true")
	}
	if c != nil {
		t.Error("-v should produce nil config")
	}
}

func TestPrepareConfig_confFlag(t *testing.T) {
	envHelperReset(t)
	f := defineFlags()
	tt := true
	f.conf = &tt
	c, done := prepareConfig(f)
	if !done {
		t.Error("-conf should produce done=true")
	}
	if c == nil {
		t.Error("-conf should still build the config")
	}
}

func TestPrepareConfig_runPath(t *testing.T) {
	envHelperReset(t)
	f := defineFlags()
	c, done := prepareConfig(f)
	if done {
		t.Error("no short-circuit flag → done=false")
	}
	if c == nil {
		t.Error("non-short-circuit path should produce a non-nil config")
	}
}
