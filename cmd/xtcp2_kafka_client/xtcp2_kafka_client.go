package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/protobuf/encoding/protodelim"
)

// ErrRecordTooShort is returned by processRecord when a Kafka record value is
// too short to contain a length-delimited Envelope.
var ErrRecordTooShort = errors.New("kafka record too short for length-delimited envelope")

const (
	brokerCst     = "localhost:19092"
	topicCst      = "xtcp"
	groupID       = "xtcp-consumer-group"
	debugLevelCst = 11
)

var (
	debugLevel uint
)

func main() {
	os.Exit(runMain(context.Background(), os.Args[1:], os.Stderr))
}

// runMain wires flag parsing + Kafka client + poll loop. Extracted so tests
// can drive it with synthetic args + a cancellable ctx (no broker needed).
func runMain(ctx context.Context, args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("xtcp2_kafka_client", flag.ContinueOnError)
	fs.SetOutput(stderr)
	debugLevelPtr := fs.Uint("d", debugLevelCst, "debug level")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	debugLevel = *debugLevelPtr

	opts := []kgo.Opt{
		kgo.SeedBrokers(brokerCst),
		kgo.ConsumerGroup(groupID),
		kgo.ConsumeTopics(topicCst),
		kgo.ClientID("xtcp-consumer"),
	}
	cl, err := kgo.NewClient(opts...)
	if err != nil {
		fmt.Fprintf(stderr, "unable to create client: %v\n", err)
		return 1
	}
	defer cl.Close()

	pollLoop(ctx, cl)
	return 0
}

// kafkaFetcher is the surface pollLoop needs from a Kafka consumer
// client. Lifting it to an interface lets tests drive pollLoop's
// happy-path EachRecord closure without a real broker. *kgo.Client
// satisfies this interface.
type kafkaFetcher interface {
	PollFetches(ctx context.Context) kgo.Fetches
}

// pollLoop is the Kafka consume body. Extracted so test code can call it
// against a fake client (with a pre-canceled ctx for a quick exit).
func pollLoop(ctx context.Context, cl kafkaFetcher) {
	for i := 0; ; i++ {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if debugLevel > 10 {
			log.Printf("i:%d, PollFetches", i)
		}
		fetches := cl.PollFetches(ctx)
		if ctx.Err() != nil {
			return
		}
		if errs := fetches.Errors(); len(errs) > 0 {
			log.Printf("fetch errors: %v", errs)
			continue
		}
		fetches.EachRecord(func(record *kgo.Record) {
			_ = processRecord(record.Value, debugLevel) //nolint:errcheck // processRecord logs internally; nothing actionable here
		})
	}
}

// processRecord parses one length-delimited Envelope Kafka record value:
// varint(envelope_size) || encoded_Envelope. The xtcp daemon produces
// this exact shape (see pkg/xtcp/marshallers.go protobufListMarshal +
// pkg/xtcp/poller.go flushEnvelope) and ClickHouse's
// kafka_format='ProtobufList' decodes it on the consumer side. No
// Confluent schema-registry header is prepended on the wire; the
// schema registry registration in xtcp's destinations_kafka is
// informational only.
//
// Returns ErrRecordTooShort if the value can't contain even an empty
// envelope (varint of 0 = 1 byte) or the protodelim error if the
// length-delimited frame is malformed.
func processRecord(value []byte, debugLvl uint) error {
	if len(value) < 1 {
		log.Println("Skipping record: empty value")
		return ErrRecordTooShort
	}

	if debugLvl > 10 {
		head := len(value)
		if head > 16 {
			head = 16
		}
		log.Printf("record.Value head:% X (%d bytes total)", value[:head], len(value))
	}

	var envelope xtcp_flat_record.Envelope
	if err := protodelim.UnmarshalFrom(bytes.NewReader(value), &envelope); err != nil {
		log.Printf("Failed to unmarshal length-delimited envelope: %v", err)
		return err
	}

	for _, row := range envelope.Row {
		log.Printf("Received row: %+v", row)
	}
	return nil
}
