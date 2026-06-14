package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/protobuf/proto"
)

func TestHandleRecord_happy(t *testing.T) {
	encoded, err := proto.Marshal(&xtcp_flat_record.Envelope_XtcpFlatRecord{Hostname: "test-host"})
	if err != nil {
		t.Fatal(err)
	}
	rec := &kgo.Record{Topic: "xtcp", Partition: 0, Offset: 1, Value: encoded}
	dst := &xtcp_flat_record.Envelope_XtcpFlatRecord{}

	handleRecord(0, 1, 1, rec, dst)
	if dst.Hostname != "test-host" {
		t.Errorf("decoded hostname = %q, want test-host", dst.Hostname)
	}
}

func TestHandleRecord_badProto(t *testing.T) {
	rec := &kgo.Record{Topic: "xtcp", Value: []byte{0xFF, 0xFF, 0xFF, 0xFF}}
	dst := &xtcp_flat_record.Envelope_XtcpFlatRecord{}

	// handleRecord swallows the decode error (logs and returns). We just
	// verify it doesn't panic on malformed input.
	handleRecord(0, 1, 1, rec, dst)
}

func TestRunMain_version(t *testing.T) {
	var stdout, stderr strings.Builder
	if rc := runMain(t.Context(), []string{"-version"}, &stdout, &stderr); rc != 0 {
		t.Errorf("rc = %d, want 0", rc)
	}
	if !strings.Contains(stdout.String(), "xtcp commit:") {
		t.Errorf("stdout = %q, want commit prefix", stdout.String())
	}
}

func TestRunMain_invalidFlag(t *testing.T) {
	var stdout, stderr strings.Builder
	if rc := runMain(t.Context(), []string{"-not-a-flag"}, &stdout, &stderr); rc != 2 {
		t.Errorf("rc = %d, want 2", rc)
	}
}

func TestRunMain_cancellable(t *testing.T) {
	// Pre-cancel + tiny consumeTimeout so pollLoop exits via ctx.Done.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var stdout, stderr strings.Builder
	rc := runMain(ctx, []string{"-broker", "localhost:0", "-consumeTimeout", "50ms"}, &stdout, &stderr)
	if rc != 0 {
		t.Errorf("rc = %d, want 0; stderr=%s", rc, stderr.String())
	}
}

func TestPollLoop_cancelledCtx(t *testing.T) {
	// kgo.NewClient on a deferred-resolution broker succeeds without actually
	// connecting; PollFetches with a cancelled ctx returns an err. Loop
	// exits via the ctx.Done() path.
	client, err := kgo.NewClient(kgo.SeedBrokers("localhost:0"))
	if err != nil {
		t.Skipf("kgo.NewClient: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithCancel(t.Context())
	cancel() // pre-cancel → first iteration takes ctx.Done() branch

	done := make(chan struct{})
	go func() {
		pollLoop(ctx, client, 50*time.Millisecond)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("pollLoop did not exit on pre-cancelled ctx")
	}
}

func TestHandleRecord_emptyValue(t *testing.T) {
	rec := &kgo.Record{Topic: "xtcp", Value: nil}
	dst := &xtcp_flat_record.Envelope_XtcpFlatRecord{}
	handleRecord(0, 1, 1, rec, dst)
	// Empty bytes are a valid empty proto message; nothing to assert beyond
	// no-panic.
}
