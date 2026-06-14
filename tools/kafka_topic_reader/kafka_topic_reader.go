package main

import (
	"context"
	"flag"
	"fmt"
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	broker := flag.String("broker", brokerCst, "broker")
	topic := flag.String("topic", topicCst, "topic")
	groupID := flag.String("groupID", groupIDCst, "groupID")
	consumeTimeout := flag.Duration("consumeTimeout", consumeTimeoutCst, "consume context timeout")

	version := flag.Bool("version", false, "show version")

	d := flag.Int("d", debugLevelCst, "debug level")

	flag.Parse()

	// Print version information passed in via ldflags in the Makefile
	if *version {
		log.Println("xtcp commit:", commit, "\tdate(UTC):", date)
		os.Exit(0) //nolint:gocritic // exitAfterDefer: -version prints and exits; deferred cancel() is moot at process shutdown
	}

	debugLevel = *d

	if debugLevel > 10 {
		fmt.Println("*broker:", *broker)
		fmt.Println("*topic:", *topic)
		fmt.Println("*groupID:", *groupID)
	}

	// Initialize Kafka consumer client
	client, err := kgo.NewClient(
		kgo.SeedBrokers(*broker),
		kgo.ConsumerGroup(*groupID),
		kgo.ConsumeTopics(*topic),
	)
	if err != nil {
		log.Fatalf("Error creating Kafka client: %v", err)
	}
	defer client.Close()

	kgoFetchesPool := sync.Pool{
		New: func() interface{} {
			return new(kgo.Fetches)
		},
	}
	kgoFetches, _ := kgoFetchesPool.Get().(*kgo.Fetches) //nolint:errcheck // pool.Get returns the type from pool.New
	defer kgoFetchesPool.Put(kgoFetches)

	xtcpRecordPool := sync.Pool{
		New: func() interface{} {
			return new(xtcp_flat_record.Envelope_XtcpFlatRecord)
		},
	}

	xtcpRecord, _ := xtcpRecordPool.Get().(*xtcp_flat_record.Envelope_XtcpFlatRecord) //nolint:errcheck // pool.Get returns the type from pool.New
	defer xtcpRecordPool.Put(xtcpRecord)

	records := 0
	for i := 0; ; i++ {

		ctxC, cancelC := context.WithTimeout(ctx, *consumeTimeout)

		*kgoFetches = client.PollFetches(ctxC)
		cancelC()
		if ferr := kgoFetches.Err(); ferr != nil {
			log.Printf("i:%d Error fetching messages: %v", i, ferr)
			continue
		}

		j := 0
		kgoFetches.EachRecord(func(record *kgo.Record) {
			j++
			records++

			fmt.Printf("i:%d j:%d records:%d Received message from topic %s, partition %d, offset %d\n",
				i, j, records, record.Topic, record.Partition, record.Offset)

			if uerr := proto.Unmarshal(record.Value, xtcpRecord); uerr != nil {
				log.Printf("Error unmarshalling protobuf message: %v", uerr)
				return
			}

			fmt.Printf("i:%d j:%d records:%d Decoded protobuf message:%v\n", i, j, records, xtcpRecord)
		})
	}
}
