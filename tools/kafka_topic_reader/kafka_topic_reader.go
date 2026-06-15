package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/protobuf/proto"
)

const (
	debugLevelCst = 11

	brokerCst  = "localhost:19092"
	topicCst   = "xtcp" // Kafka topic name
	groupIDCst = "xtcp-consumer-group-ID"

	consumeTimeoutCst = 5 * time.Second
)

var (
	// Passed by "go build -ldflags" for the show version
	commit string
	date   string

	debugLevel int
)

func main() {
	os.Exit(runMain(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

// runMain wires flag parsing + Kafka client + poll loop. Extracted so tests
// can drive it with synthetic args + a cancellable ctx (without actually
// connecting to Kafka). Returns the process exit code.
func runMain(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("kafka_topic_reader", flag.ContinueOnError)
	fs.SetOutput(stderr)
	broker := fs.String("broker", brokerCst, "broker")
	topic := fs.String("topic", topicCst, "topic")
	groupID := fs.String("groupID", groupIDCst, "groupID")
	consumeTimeout := fs.Duration("consumeTimeout", consumeTimeoutCst, "consume context timeout")
	version := fs.Bool("version", false, "show version")
	d := fs.Int("d", debugLevelCst, "debug level")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *version {
		fmt.Fprintf(stdout, "xtcp commit:%s\tdate(UTC):%s\n", commit, date)
		return 0
	}

	debugLevel = *d

	if debugLevel > 10 {
		fmt.Fprintln(stdout, "*broker:", *broker)
		fmt.Fprintln(stdout, "*topic:", *topic)
		fmt.Fprintln(stdout, "*groupID:", *groupID)
	}

	client, err := kgo.NewClient(
		kgo.SeedBrokers(*broker),
		kgo.ConsumerGroup(*groupID),
		kgo.ConsumeTopics(*topic),
	)
	if err != nil {
		fmt.Fprintf(stderr, "Error creating Kafka client: %v\n", err)
		return 1
	}
	defer client.Close()

	pollLoop(ctx, client, *consumeTimeout)
	return 0
}

// pollLoop is the Kafka consumer body. Extracted so test code can call it
// against a fake client (or skip it entirely via the runMain happy paths).
// kafkaFetcher is the surface pollLoop needs from a Kafka consumer
// client. Lifting it to an interface lets tests drive pollLoop's
// happy-path EachRecord closure (which calls handleRecord) without a
// real broker. *kgo.Client satisfies this interface.
type kafkaFetcher interface {
	PollFetches(ctx context.Context) kgo.Fetches
}

func pollLoop(ctx context.Context, client kafkaFetcher, consumeTimeout time.Duration) {
	kgoFetchesPool := sync.Pool{
		New: func() any {
			return new(kgo.Fetches)
		},
	}
	kgoFetches, _ := kgoFetchesPool.Get().(*kgo.Fetches) //nolint:errcheck // pool.Get returns the type from pool.New
	defer kgoFetchesPool.Put(kgoFetches)

	xtcpRecordPool := sync.Pool{
		New: func() any {
			return new(xtcp_flat_record.Envelope_XtcpFlatRecord)
		},
	}
	xtcpRecord, _ := xtcpRecordPool.Get().(*xtcp_flat_record.Envelope_XtcpFlatRecord) //nolint:errcheck // pool.Get returns the type from pool.New
	defer xtcpRecordPool.Put(xtcpRecord)

	records := 0
	for i := 0; ; i++ {
		select {
		case <-ctx.Done():
			return
		default:
		}

		ctxC, cancelC := context.WithTimeout(ctx, consumeTimeout)
		*kgoFetches = client.PollFetches(ctxC)
		cancelC()
		if ferr := kgoFetches.Err(); ferr != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("i:%d Error fetching messages: %v", i, ferr)
			continue
		}

		j := 0
		kgoFetches.EachRecord(func(record *kgo.Record) {
			j++
			records++
			handleRecord(i, j, records, record, xtcpRecord)
		})
	}
}

// handleRecord logs receipt metadata and attempts to decode the record's
// value into xtcpRecord. Extracted from the EachRecord callback so tests
// can exercise the decode happy + bad-proto paths without a Kafka client.
func handleRecord(i, j, records int, record *kgo.Record, xtcpRecord *xtcp_flat_record.Envelope_XtcpFlatRecord) {
	fmt.Printf("i:%d j:%d records:%d Received message from topic %s, partition %d, offset %d\n",
		i, j, records, record.Topic, record.Partition, record.Offset)

	// proto.Unmarshal merges into the destination; without Reset, fields
	// that were SET on the previous record but UNSET on this one would
	// stay populated, producing decoded output that mixes adjacent
	// records. xtcpRecord is reused across the consume loop (pool entry)
	// so this is reachable in practice.
	proto.Reset(xtcpRecord)
	if err := proto.Unmarshal(record.Value, xtcpRecord); err != nil {
		log.Printf("Error unmarshalling protobuf message: %v", err)
		return
	}

	fmt.Printf("i:%d j:%d records:%d Decoded protobuf message:%v\n", i, j, records, xtcpRecord)
}
