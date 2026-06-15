package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/protobuf/proto"
)

func TestProcessRecord_tooShort(t *testing.T) {
	cases := [][]byte{nil, {}, {0x00}, {0x00, 0x00, 0x00, 0x00, 0x00}}
	for i, value := range cases {
		if err := processRecord(value, 0); !errors.Is(err, ErrRecordTooShort) {
			t.Errorf("case %d (%d bytes): err = %v, want ErrRecordTooShort", i, len(value), err)
		}
	}
}

func TestProcessRecord_badProto(t *testing.T) {
	// 1 magic + 4 schema ID + 1 length byte. Length byte == 0 makes proto.Unmarshal
	// receive an empty buffer which IS valid (empty envelope), so use 0xFF to force
	// a malformed varint instead.
	value := []byte{0x00, 0x00, 0x00, 0x00, 0x07, 0xFF, 0xFF, 0xFF, 0xFF}
	if err := processRecord(value, 0); err == nil {
		t.Error("malformed protobuf should produce error")
	}
}

func TestProcessRecord_happy(t *testing.T) {
	envelope := &xtcp_flat_record.Envelope{
		Row: []*xtcp_flat_record.Envelope_XtcpFlatRecord{{Hostname: "test-host"}},
	}
	envBytes, err := proto.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	value := make([]byte, KafkaHeaderSizeCst+len(envBytes))
	value[0] = 0x00 // magic
	binary.BigEndian.PutUint32(value[1:5], 42)
	value[5] = 0x00 // unused length prefix
	copy(value[KafkaHeaderSizeCst:], envBytes)
	if err := processRecord(value, 0); err != nil {
		t.Errorf("happy path: err = %v", err)
	}
}

func TestProcessRecord_debugLogging(t *testing.T) {
	// debugLvl > 10 triggers the schemaID + header log paths.
	envBytes, _ := proto.Marshal(&xtcp_flat_record.Envelope{}) //nolint:errcheck // test plumbing
	value := append([]byte{0x00, 0x00, 0x00, 0x00, 0x01, 0x00}, envBytes...)
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
// synthetic Fetch containing a single valid Confluent-framed record.
// The EachRecord closure (processRecord call) fires, then the second
// PollFetches call signals the fake to cancel ctx.
func TestPollLoop_eachRecordClosureFires(t *testing.T) {
	value := make([]byte, KafkaHeaderSizeCst+1)
	value[0] = 0x00 // magic
	// schemaID bytes [1:5] = 0; length byte [5] = 0 → empty proto
	// envelope; processRecord parses it successfully.

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
