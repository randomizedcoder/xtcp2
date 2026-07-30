package main

import (
	"bytes"
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/durationpb"
)

// fakeConfigServer records the last request per RPC so tests can assert the CLI
// translated flags → request correctly. Get returns a fixed config.
type fakeConfigServer struct {
	xtcp_config.UnimplementedConfigServiceServer
	lastSetPollFreq *xtcp_config.SetPollFrequencyRequest
	lastBurst       *xtcp_config.TriggerPollBurstRequest
	lastS3          *xtcp_config.SetS3UploadRequest
	lastSet         *xtcp_config.SetRequest
	triggered       bool
}

func (f *fakeConfigServer) Get(context.Context, *xtcp_config.GetRequest) (*xtcp_config.GetResponse, error) {
	return &xtcp_config.GetResponse{Config: &xtcp_config.XtcpConfig{
		Dest:          "kafka:127.0.0.1:9092",
		Tag:           "live-tag",
		PollFrequency: durationpb.New(10 * time.Second),
	}}, nil
}

func (f *fakeConfigServer) SetPollFrequency(_ context.Context, in *xtcp_config.SetPollFrequencyRequest) (*xtcp_config.SetPollFrequencyResponse, error) {
	f.lastSetPollFreq = in
	return &xtcp_config.SetPollFrequencyResponse{}, nil
}

func (f *fakeConfigServer) TriggerPoll(context.Context, *xtcp_config.TriggerPollRequest) (*xtcp_config.TriggerPollResponse, error) {
	f.triggered = true
	return &xtcp_config.TriggerPollResponse{}, nil
}

func (f *fakeConfigServer) TriggerPollBurst(_ context.Context, in *xtcp_config.TriggerPollBurstRequest) (*xtcp_config.TriggerPollBurstResponse, error) {
	f.lastBurst = in
	return &xtcp_config.TriggerPollBurstResponse{Count: in.Count, Interval: in.Interval}, nil
}

func (f *fakeConfigServer) SetS3Upload(_ context.Context, in *xtcp_config.SetS3UploadRequest) (*xtcp_config.SetS3UploadResponse, error) {
	f.lastS3 = in
	return &xtcp_config.SetS3UploadResponse{}, nil
}

func (f *fakeConfigServer) Set(_ context.Context, in *xtcp_config.SetRequest) (*xtcp_config.SetResponse, error) {
	f.lastSet = in
	return &xtcp_config.SetResponse{Config: in.Config}, nil
}

// startFakeServer spins up the fake ConfigService on an in-memory bufconn and
// points the CLI's dialFunc at it. Returns the server + a cleanup.
func startFakeServer(t *testing.T) *fakeConfigServer {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	fake := &fakeConfigServer{}
	xtcp_config.RegisterConfigServiceServer(srv, fake)
	go func() { _ = srv.Serve(lis) }()

	prevDial := dialFunc
	dialFunc = func(string) (*grpc.ClientConn, error) {
		return grpc.NewClient(
			"passthrough:///bufnet",
			grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
				return lis.DialContext(ctx)
			}),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
	}
	t.Cleanup(func() {
		dialFunc = prevDial
		srv.Stop()
		_ = lis.Close()
	})
	return fake
}

func run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	rc := runMain(context.Background(), args, &out, &errb)
	return rc, out.String(), errb.String()
}

func TestGet(t *testing.T) {
	startFakeServer(t)
	rc, out, errStr := run(t, "get")
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%s", rc, errStr)
	}
	if !strings.Contains(out, "live-tag") || !strings.Contains(out, "kafka:127.0.0.1:9092") {
		t.Errorf("get output missing expected fields:\n%s", out)
	}
}

func TestSetPollFrequency(t *testing.T) {
	fake := startFakeServer(t)
	rc, _, errStr := run(t, "set-poll-frequency", "-frequency", "30s", "-timeout", "5s")
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%s", rc, errStr)
	}
	if fake.lastSetPollFreq == nil ||
		fake.lastSetPollFreq.PollFrequency.AsDuration() != 30*time.Second ||
		fake.lastSetPollFreq.PollTimeout.AsDuration() != 5*time.Second {
		t.Errorf("unexpected SetPollFrequency request: %+v", fake.lastSetPollFreq)
	}
}

func TestSetPollFrequency_missingArgs(t *testing.T) {
	startFakeServer(t)
	if rc, _, _ := run(t, "set-poll-frequency"); rc != 2 {
		t.Errorf("expected rc=2 for missing args, got %d", rc)
	}
}

func TestTriggerPoll(t *testing.T) {
	fake := startFakeServer(t)
	if rc, _, errStr := run(t, "trigger-poll"); rc != 0 {
		t.Fatalf("rc=%d stderr=%s", rc, errStr)
	}
	if !fake.triggered {
		t.Error("TriggerPoll was not called")
	}
}

func TestPollBurst(t *testing.T) {
	fake := startFakeServer(t)
	rc, _, errStr := run(t, "poll-burst", "-count", "6", "-interval", "10s")
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%s", rc, errStr)
	}
	if fake.lastBurst == nil || fake.lastBurst.Count != 6 || fake.lastBurst.Interval.AsDuration() != 10*time.Second {
		t.Errorf("unexpected burst request: %+v", fake.lastBurst)
	}
}

func TestSetS3_bothFields(t *testing.T) {
	fake := startFakeServer(t)
	rc, _, errStr := run(t, "set-s3", "-flush-interval", "5s", "-threshold-bytes", "1024")
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%s", rc, errStr)
	}
	if fake.lastS3 == nil || fake.lastS3.S3FlushInterval.AsDuration() != 5*time.Second || fake.lastS3.S3ParquetFlushThresholdBytes != 1024 {
		t.Errorf("unexpected set-s3 request: %+v", fake.lastS3)
	}
}

// Only the flag the operator set is populated; the other stays nil/zero.
func TestSetS3_onlyInterval(t *testing.T) {
	fake := startFakeServer(t)
	if rc, _, errStr := run(t, "set-s3", "-flush-interval", "5s"); rc != 0 {
		t.Fatalf("rc=%d stderr=%s", rc, errStr)
	}
	if fake.lastS3.S3FlushInterval == nil || fake.lastS3.S3ParquetFlushThresholdBytes != 0 {
		t.Errorf("expected only interval set: %+v", fake.lastS3)
	}
}

func TestSetS3_noFields(t *testing.T) {
	startFakeServer(t)
	if rc, _, _ := run(t, "set-s3"); rc != 2 {
		t.Errorf("expected rc=2 when no fields given, got %d", rc)
	}
}

func TestReconfigure_fromStdin(t *testing.T) {
	fake := startFakeServer(t)
	prev := stdinReader
	stdinReader = strings.NewReader(`{"dest":"kafka:127.0.0.1:9092","tag":"INC-9"}`)
	t.Cleanup(func() { stdinReader = prev })

	rc, out, errStr := run(t, "reconfigure", "-file", "-")
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%s", rc, errStr)
	}
	if fake.lastSet == nil || fake.lastSet.Config.Tag != "INC-9" {
		t.Errorf("unexpected Set request: %+v", fake.lastSet)
	}
	if !strings.Contains(out, "soft-restarting") {
		t.Errorf("expected soft-restart notice, got: %s", out)
	}
}

func TestReconfigure_badJSON(t *testing.T) {
	startFakeServer(t)
	prev := stdinReader
	stdinReader = strings.NewReader(`{not json`)
	t.Cleanup(func() { stdinReader = prev })
	if rc, _, _ := run(t, "reconfigure"); rc != 1 {
		t.Errorf("expected rc=1 for bad JSON, got %d", rc)
	}
}

func TestUnknownCommand(t *testing.T) {
	if rc, _, errStr := run(t, "frobnicate"); rc != 2 || !strings.Contains(errStr, "unknown command") {
		t.Errorf("expected rc=2 + unknown-command message, got rc=%d err=%s", rc, errStr)
	}
}

func TestNoArgs(t *testing.T) {
	if rc, _, _ := run(t); rc != 2 {
		t.Errorf("expected rc=2 for no args, got %d", rc)
	}
}
