package main

import (
	"context"
	"encoding/binary"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/protobuf/proto"
)

const (
	schemaRegistryURLCst = "http://localhost:18081"
	brokerCst            = "localhost:19092"
	topicCst             = "xtcp"
	groupID              = "xtcp-consumer-group"
	debugLevelCst        = 11
	signalChannelSizeCst = 10
	KafkaHeaderSizeCst   = 6
)

var (
	debugLevel uint
)

func main() {

	debugLevelPtr := flag.Uint("d", debugLevelCst, "debug level")
	flag.Parse()

	debugLevel = *debugLevelPtr

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	complete := make(chan struct{}, signalChannelSizeCst)
	go initSignalHandler(cancel, complete)

	// regClient, err := sr.NewClient(sr.URLs(schemaRegistryURLCst))
	// if err != nil {
	// 	log.Fatalf("unable to create schema registry client: %v", err)
	// }

	//serde := sr.NewSerde(regClient)

	opts := []kgo.Opt{
		kgo.SeedBrokers(brokerCst),
		kgo.ConsumerGroup(groupID),
		kgo.ConsumeTopics(topicCst),
		kgo.ClientID("xtcp-consumer"),
		//kgo.RecordDeserializer(serde.RecordDeserializer()),
	}

	cl, err := kgo.NewClient(opts...)
	if err != nil {
		log.Fatalf("unable to create client: %v", err)
	}
	defer cl.Close()

	if debugLevel > 10 {
		log.Println("kgo.NewClient complete")
	}

	for i := 0; ; i++ {

		if debugLevel > 10 {
			log.Printf("i:%d, PollFetches", i)
		}

		fetches := cl.PollFetches(ctx)
		if errs := fetches.Errors(); len(errs) > 0 {
			log.Printf("fetch errors: %v", errs)
			continue
		}

		fetches.EachRecord(func(record *kgo.Record) {

			if debugLevel > 10 {
				log.Printf("record.Value header: % X", record.Value[:KafkaHeaderSizeCst])
			}
			// if debugLevel > 100 {
			// 	log.Printf("record.Value:% X", record.Value)
			// }

			if len(record.Value) < 6 {
				log.Println("Skipping record: Value too short to contain Confluent header")
				return
			}

			schemaID := binary.BigEndian.Uint32(record.Value[1:5])
			if debugLevel > 10 {
				log.Printf("schemaID:%d", schemaID)
			}

			var envelope xtcp_flat_record.Envelope
			err := proto.Unmarshal(record.Value[6:], &envelope)
			if err != nil {
				log.Printf("Failed to unmarshal protobuf: %v", err)
				return
			}

			for _, row := range envelope.Row {
				log.Printf("Received row: %+v", row)
			}
		})
	}
}

// initSignalHandler sets up signal handling for the process, and
// will call cancel() when received
func initSignalHandler(cancel context.CancelFunc, complete <-chan struct{}) {

	c := make(chan os.Signal, signalChannelSizeCst)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	<-c
	log.Printf("Signal caught, closing application")
	cancel()

	log.Printf("Signal caught, cancel() called, and sleeping to allow goroutines to close")

	select {
	case <-complete:
		log.Printf("<-complete exit(0)")
	default:
		log.Printf("Sleep complete, goodbye! exit(0)")
	}

	os.Exit(0)
}
