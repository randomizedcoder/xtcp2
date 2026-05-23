package main

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/protobuf/encoding/protodelim"
)

// marshalEnvelopeForTest produces the on-wire bytes the daemon would
// emit for a given Envelope: varint(envelope_size) || envelope_bytes.
// Used by the happy-path / debug-log / pollLoop tests to construct
// fake Kafka records.
func marshalEnvelopeForTest(t *testing.T, env *xtcp_flat_record.Envelope) []byte {
	t.Helper()
	var buf bytes.Buffer
	if _, err := protodelim.MarshalTo(&buf, env); err != nil {
		t.Fatalf("protodelim.MarshalTo: %v", err)
	}
	return buf.Bytes()
}

func TestProcessRecord_tooShort(t *testing.T) {
	// Only a fully empty record value is too short for the new wire
	// format — even one varint byte represents an empty envelope and
	// parses cleanly.
	if err := processRecord(nil, 0); !errors.Is(err, ErrRecordTooShort) {
		t.Errorf("nil value: err = %v, want ErrRecordTooShort", err)
	}
	if err := processRecord([]byte{}, 0); !errors.Is(err, ErrRecordTooShort) {
		t.Errorf("empty value: err = %v, want ErrRecordTooShort", err)
	}
}

func TestProcessRecord_badProto(t *testing.T) {
	// Length-delimited frame claiming 1000 bytes follows but only a
	// handful are present — protodelim returns an io.ErrUnexpectedEOF.
	value := []byte{0xE8, 0x07, 0xFF, 0xFF, 0xFF}
	if err := processRecord(value, 0); err == nil {
		t.Error("truncated length-delimited frame should produce error")
	}
}

func TestProcessRecord_happy(t *testing.T) {
	envelope := &xtcp_flat_record.Envelope{
		Row: []*xtcp_flat_record.XtcpFlatRecord{{Hostname: "test-host"}},
	}
	value := marshalEnvelopeForTest(t, envelope)
	if err := processRecord(value, 0); err != nil {
		t.Errorf("happy path: err = %v", err)
	}
}

func TestProcessRecord_debugLogging(t *testing.T) {
	// debugLvl > 10 triggers the head-bytes log path.
	value := marshalEnvelopeForTest(t, &xtcp_flat_record.Envelope{})
	if err := processRecord(value, 11); err != nil {
		t.Errorf("debug-level processRecord err: %v", err)
	}
}

func TestErrRecordTooShort_message(t *testing.T) {
	if ErrRecordTooShort.Error() == "" {
		t.Error("ErrRecordTooShort should have a message")
	}
}

func TestRunMain_invalidFlag(t *testing.T) {
	var stderr bytes.Buffer
	if rc := runMain(t.Context(), []string{"-not-a-flag"}, &stderr); rc != 2 {
		t.Errorf("rc = %d, want 2", rc)
	}
}

func TestRunMain_cancellable(t *testing.T) {
	// Pre-canceled ctx → pollLoop exits via ctx.Done() before fetching.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if rc := runMain(ctx, []string{"-d", "0"}, &bytes.Buffer{}); rc != 0 {
		t.Errorf("rc = %d, want 0", rc)
	}
}

func TestPollLoop_cancelledCtx(t *testing.T) {
	cl, err := kgo.NewClient(kgo.SeedBrokers("localhost:0"))
	if err != nil {
		t.Skipf("kgo.NewClient: %v", err)
	}
	defer cl.Close()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	done := make(chan struct{})
	go func() {
		pollLoop(ctx, cl)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("pollLoop did not exit on cancel")
	}
}

// Drive pollLoop with debugLevel>10 so the "i:%d, PollFetches" log
// branch fires on each iteration before cancel.
func TestPollLoop_debugLogPath(t *testing.T) {
	prev := debugLevel
	debugLevel = 20
	t.Cleanup(func() { debugLevel = prev })

	cl, err := kgo.NewClient(
		kgo.SeedBrokers("localhost:0"),
		kgo.ConsumerGroup("test-group"),
		kgo.ConsumeTopics("test-topic"),
	)
	if err != nil {
		t.Skipf("kgo.NewClient: %v", err)
	}
	defer cl.Close()

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		pollLoop(ctx, cl)
		close(done)
	}()
	time.Sleep(120 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("pollLoop did not exit after cancel")
	}
}

// Drive pollLoop with an active ctx against an unreachable broker so
// PollFetches returns fetch errors each iteration; cancel after a few
// iterations to exit via the ctx-cancel path. This exercises the
// fetches.Errors() != 0 branch.
func TestPollLoop_fetchErrorsThenCancel(t *testing.T) {
	cl, err := kgo.NewClient(
		kgo.SeedBrokers("localhost:0"),
		kgo.ConsumerGroup("test-group"),
		kgo.ConsumeTopics("test-topic"),
	)
	if err != nil {
		t.Skipf("kgo.NewClient: %v", err)
	}
	defer cl.Close()

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		pollLoop(ctx, cl)
		close(done)
	}()
	time.Sleep(150 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("pollLoop did not exit after cancel")
	}
}

// fakeFetcher implements the kafkaFetcher interface so pollLoop can be
// driven with synthetic records — exercises the EachRecord closure
// body that broker-bound tests can't reach without real kafka.
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

// TestPollLoop_eachRecordClosureFires drives pollLoop with one
// synthetic Fetch containing a single valid length-delimited Envelope
// record (the shape the xtcp daemon emits via protobufListMarshal).
// The EachRecord closure (processRecord call) fires, then the second
// PollFetches call signals the fake to cancel ctx.
func TestPollLoop_eachRecordClosureFires(t *testing.T) {
	value := marshalEnvelopeForTest(t, &xtcp_flat_record.Envelope{})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	fake := &fakeFetcher{
		fetches:  []kgo.Fetches{makeFetchWithRecord(value)},
		onCancel: cancel,
	}
	done := make(chan struct{})
	go func() {
		pollLoop(ctx, fake)
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

// TestPollLoop_fakeFetcherErrors drives pollLoop with a Fetches that
// surfaces an error via FetchPartition.Err, then exhausts to cancel.
// Exercises the `if errs := fetches.Errors(); ...` branch with a
// non-empty Errors() result.
func TestPollLoop_fakeFetcherErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	errFetch := kgo.Fetches{
		{Topics: []kgo.FetchTopic{
			{Topic: "test-topic", Partitions: []kgo.FetchPartition{
				{Err: errors.New("fetch err")},
			}},
		}},
	}
	fake := &fakeFetcher{
		fetches:  []kgo.Fetches{errFetch},
		onCancel: cancel,
	}
	done := make(chan struct{})
	go func() {
		pollLoop(ctx, fake)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("pollLoop did not exit on fake-fetcher exhaust")
	}
}
