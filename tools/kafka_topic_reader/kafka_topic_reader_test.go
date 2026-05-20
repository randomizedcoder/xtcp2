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
	// connecting; PollFetches with a canceled ctx returns an err. Loop
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
		t.Fatal("pollLoop did not exit on pre-canceled ctx")
	}
}

// PollLoop with an active (uncancelled) ctx + an unreachable broker:
// PollFetches returns a fetch error each loop iteration; the loop logs
// + continues. Cancel ctx after a few iterations so the loop exits via
// the ctx.Err()-after-Err branch.
func TestPollLoop_fetchErrorThenCancel(t *testing.T) {
	client, err := kgo.NewClient(
		kgo.SeedBrokers("localhost:0"),
		kgo.ConsumerGroup("test-group"),
		kgo.ConsumeTopics("test-topic"),
	)
	if err != nil {
		t.Skipf("kgo.NewClient: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		pollLoop(ctx, client, 50*time.Millisecond)
		close(done)
	}()
	// Let a few fetch errors happen before cancellation triggers the
	// ctx.Err()-after-fetch-err branch.
	time.Sleep(150 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("pollLoop did not exit after cancel")
	}
}

func TestHandleRecord_emptyValue(t *testing.T) {
	rec := &kgo.Record{Topic: "xtcp", Value: nil}
	dst := &xtcp_flat_record.Envelope_XtcpFlatRecord{}
	handleRecord(0, 1, 1, rec, dst)
	// Empty bytes are a valid empty proto message; nothing to assert beyond
	// no-panic.
}

// runMain with debugLevel=0 (-d 0) skips the verbose stdout dump so the
// `if debugLevel > 10` branch's false-arm is covered.
func TestRunMain_quiet(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var stdout, stderr strings.Builder
	rc := runMain(ctx, []string{"-d", "0", "-broker", "localhost:0", "-consumeTimeout", "50ms"}, &stdout, &stderr)
	if rc != 0 {
		t.Errorf("rc = %d, want 0; stderr=%s", rc, stderr.String())
	}
	if strings.Contains(stdout.String(), "*broker:") {
		t.Errorf("verbose output should be suppressed; got %q", stdout.String())
	}
}

// pollLoop driven by a kgo client that successfully fetches records.
// Without a real broker we can't deliver records, but we can exercise
// the loop's iteration index increment + empty-fetch path. Combined
// with the existing fetch-error coverage, this fills the gap.
func TestPollLoop_emptyFetches(t *testing.T) {
	client, err := kgo.NewClient(kgo.SeedBrokers("localhost:0"))
	if err != nil {
		t.Skipf("kgo.NewClient: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() {
		pollLoop(ctx, client, 25*time.Millisecond)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("pollLoop did not exit after ctx timeout")
	}
}

// fakeFetcher implements the kafkaFetcher interface so pollLoop can be
// driven with synthetic Fetches — exercises the EachRecord closure
// body (j++; records++; handleRecord(...)) that broker-bound tests
// couldn't reach.
type fakeFetcher struct {
	fetches  []kgo.Fetches
	calls    int
	onCancel context.CancelFunc
}

func (f *fakeFetcher) PollFetches(_ context.Context) kgo.Fetches {
	f.calls++
	if f.calls > len(f.fetches) {
		if f.onCancel != nil {
			f.onCancel()
		}
		return kgo.Fetches{}
	}
	return f.fetches[f.calls-1]
}

func makeFetchWithRecord(value []byte) kgo.Fetches {
	return kgo.Fetches{
		{
			Topics: []kgo.FetchTopic{
				{
					Topic: "test-topic",
					Partitions: []kgo.FetchPartition{
						{Records: []*kgo.Record{{Value: value}}},
					},
				},
			},
		},
	}
}

// TestPollLoop_eachRecordClosureFires drives pollLoop with a synthetic
// Fetch containing a single record. handleRecord will fail to unmarshal
// the random bytes but that's fine — it returns nil and the closure
// completes. After the fake exhausts its fetches it cancels ctx.
func TestPollLoop_eachRecordClosureFires(t *testing.T) {
	value := []byte{0x00, 0x01, 0x02}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	fake := &fakeFetcher{
		fetches:  []kgo.Fetches{makeFetchWithRecord(value)},
		onCancel: cancel,
	}
	done := make(chan struct{})
	go func() {
		pollLoop(ctx, fake, 50*time.Millisecond)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("pollLoop did not exit after fake fetcher exhausted")
	}
	if fake.calls < 1 {
		t.Errorf("expected ≥1 PollFetches call; got %d", fake.calls)
	}
}
