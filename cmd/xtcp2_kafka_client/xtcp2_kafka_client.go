package main

import (
	"context"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/protobuf/proto"
)

// ErrRecordTooShort is returned by processRecord when a Kafka record value is
// shorter than the Confluent wire format header (1 magic byte + 4 schema ID
// bytes + 1 length prefix).
var ErrRecordTooShort = errors.New("kafka record too short for Confluent header")

const (
	brokerCst            = "localhost:19092"
	topicCst             = "xtcp"
	groupID              = "xtcp-consumer-group"
	debugLevelCst        = 11
	KafkaHeaderSizeCst   = 6
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

// pollLoop is the Kafka consume body. Extracted so test code can call it
// against a fake client (with a pre-cancelled ctx for a quick exit).
func pollLoop(ctx context.Context, cl *kgo.Client) {
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
			_ = processRecord(record.Value, debugLevel)
		})
	}
}

// processRecord parses one Confluent-framed Kafka record value: 1 magic byte +
// 4-byte schema ID + length-prefixed envelope. Returns ErrRecordTooShort if
// the value doesn't even contain the Confluent header, or the proto.Unmarshal
// error if the payload isn't a valid Envelope. The decoded envelope is logged
// at debugLevel>10. Extracted from main so tests can drive it with synthetic
// records (no broker needed).
func processRecord(value []byte, debugLvl uint) error {
	if len(value) < KafkaHeaderSizeCst {
		log.Println("Skipping record: Value too short to contain Confluent header")
		return ErrRecordTooShort
	}

	if debugLvl > 10 {
		log.Printf("record.Value header: % X", value[:KafkaHeaderSizeCst])
	}

	schemaID := binary.BigEndian.Uint32(value[1:5])
	if debugLvl > 10 {
		log.Printf("schemaID:%d", schemaID)
	}

	var envelope xtcp_flat_record.Envelope
	if err := proto.Unmarshal(value[KafkaHeaderSizeCst:], &envelope); err != nil {
		log.Printf("Failed to unmarshal protobuf: %v", err)
		return err
	}

	for _, row := range envelope.Row {
		log.Printf("Received row: %+v", row)
	}
	return nil
}

