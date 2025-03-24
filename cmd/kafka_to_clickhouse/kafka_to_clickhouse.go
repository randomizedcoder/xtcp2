package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/clickhouse_protolist"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/plugin/kprom"
	"google.golang.org/protobuf/encoding/protodelim"
)

// 	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
//import "google.golang.org/protobuf/encoding/protowire"
//import "google.golang.org/protobuf/encoding/protodelim"

const (
	schemaRegistryURLCst = "http://localhost:18081"

	brokerCst = "127.0.0.1:9092"
	//brokerCst   = "redpanda-0:9092"
	topicCst    = "clickhouse_protolist"
	clientIDCst = "dave"

	loopsCst     = 10
	loopSleepCst = 10 * time.Second

	debugLevelCst = 11
)

var (
	// Passed by "go build -ldflags" for the show version
	commit  string
	date    string
	version string

	kClient       *kgo.Client
	kgoRecordPool sync.Pool
)

type config struct {
	envelope bool
	kafka    bool

	filename string
	values   []uint

	broker   string
	topic    string
	clientID string
	subject  string

	loops      int
	loopsSleep time.Duration

	debugDump    bool
	dumpFilename string

	debugLevel int
}

func main() {

	filename := flag.String("filename", "protoBytes.bin", "filename")
	valueStr := flag.String("values", "1", "values uints -> uint32, comma seperated")

	envelope := flag.Bool("envelope", true, "envelope")
	kafka := flag.Bool("kafka", true, "kafka")

	broker := flag.String("broker", brokerCst, "broker string")
	topic := flag.String("topic", topicCst, "kafka topic")
	clientID := flag.String("clientID", clientIDCst, "clientID")

	loops := flag.Int("loops", loopsCst, "loops")
	loopsSleep := flag.Duration("loopsSleep", loopSleepCst, "loops sleep duration")

	dump := flag.Bool("dump", false, "dump proto for debug")
	dumpFilename := flag.String("dumpFileName", "dump.bin", "dump file name")

	d := flag.Int("d", debugLevelCst, "debug level")

	v := flag.Bool("v", false, "show version")

	flag.Parse()

	// Print version information passed in via ldflags in the Makefile
	if *v {
		log.Printf("commit:%s\tdate(UTC):%s\tversion:%s", commit, date, version)
		os.Exit(0)
	}

	ctx := context.TODO()

	valueStrs := strings.Split(*valueStr, ",")
	var values []uint
	for _, str := range valueStrs {
		v, err := strconv.ParseUint(str, 10, 32)
		if err != nil {
			log.Fatalf("Invalid value: %v", err)
		}
		values = append(values, uint(v))
	}

	for i, v := range values {
		log.Printf("values: %d:%v", i, v)
	}

	c := config{
		filename:     *filename,
		values:       values,
		envelope:     *envelope,
		kafka:        *kafka,
		broker:       *broker,
		topic:        *topic,
		clientID:     *clientID,
		subject:      fmt.Sprintf("%s-value", *topic),
		loops:        *loops,
		loopsSleep:   *loopsSleep,
		debugDump:    *dump,
		dumpFilename: *dumpFilename,
		debugLevel:   *d,
	}

	kgoRecordPool = sync.Pool{
		New: func() interface{} {
			return new(kgo.Record)
		},
	}

	InitDestKafka(ctx, c)

	primaryFunction(ctx, c)
}

func primaryFunction(ctx context.Context, c config) {

	id, err := getLatestSchemaID(c.subject)
	if err != nil {
		log.Printf("getLatestSchemaID err:%v", err)
	}
	if c.debugLevel > 10 {
		log.Printf("getLatestSchemaID id:%d", id)
	}

	for i := 0; i < c.loops; i++ {

		binaryData := prepareBinary(ctx, c, id)

		incrementSlice(c, &c.values, 1)

		if c.debugLevel > 10 {
			log.Printf("primaryFunction i:%d", i)
		}
		fileOrKafka(ctx, c, &binaryData)
		time.Sleep(c.loopsSleep)
	}

}

func incrementSlice(c config, values *[]uint, amount uint) {
	for i := range *values {
		(*values)[i] += amount
		if c.debugLevel > 10 {
			log.Printf("incrementSlice (*values)[%d]+=%d -> %d", i, amount, (*values)[i])
		}
	}
}

func prepareBinary(_ context.Context, c config, id int) (binaryData []byte) {

	var b bytes.Buffer

	b.WriteByte(0) // magic byte
	if err := binary.Write(&b, binary.BigEndian, int32(id)); err != nil {
		log.Fatalf("failed to write schema ID: %v", err)
	}

	if !c.envelope {

		r := &clickhouse_protolist.Record{
			MyUint32: uint32(c.values[0]),
		}

		if _, err := protodelim.MarshalTo(&b, r); err != nil {
			log.Fatal("protodelim.MarshalTo(r):", err)
		}

		binaryData = b.Bytes()
		return binaryData
	}

	if c.envelope {
		envelope := &clickhouse_protolist.Envelope{}
		for _, v := range c.values {
			envelope.Rows = append(envelope.Rows,
				&clickhouse_protolist.Envelope_Record{
					MyUint32: uint32(v),
				},
			)
		}

		if _, err := protodelim.MarshalTo(&b, envelope); err != nil {
			log.Fatal("protodelim.MarshalTo(r):", err)
		}

		binaryData = b.Bytes()

		if c.debugDump {
			errW := os.WriteFile(c.dumpFilename+".envelope", binaryData, 0644)
			if errW != nil {
				log.Fatalf("Failed to write protobuf envelope data: %v", errW)
			}
		}

	}

	return binaryData
}

func fileOrKafka(ctx context.Context, c config, binaryData *[]byte) {

	if !c.kafka {
		errW := writeDataToFile(ctx, c.filename, *binaryData)
		if errW != nil {
			log.Println("Error:", errW)
		}
		os.Exit(0)
	}

	// if c.db {
	// 	log.Fatal("db not implemented, cos it's hard to insert protobuf with the clickhouse library")
	// }

	n, errK := destKafka(ctx, c, binaryData)
	if errK != nil {
		log.Println("errk:", errK)
	}

	if c.debugLevel > 10 {
		log.Printf("destKafka n:%d", n)
	}
}

func writeDataToFile(_ context.Context, filename string, data []byte) error {

	err := os.WriteFile(filename, data, 0644) // 0644 permissions (rw-r--r--)
	if err != nil {
		return fmt.Errorf("error writing to file: %w", err) // Wrap the error
	}
	return nil

}

// destKafka sends the protobuf to kafka
func destKafka(_ context.Context, c config, xtcpRecordBinary *[]byte) (n int, err error) {

	if c.debugLevel > 10 {
		log.Println("destKafka start")
	}

	kgoRecord := kgoRecordPool.Get().(*kgo.Record)
	// defer x.kgoRecordPool.Put(kgoRecord)

	kgoRecord.Topic = c.topic
	kgoRecord.Value = *xtcpRecordBinary
	len := len(*xtcpRecordBinary)

	var ctxP context.Context
	//var cancelP context.CancelFunc
	// if x.config.KafkaProduceTimeout.AsDuration() != 0 {
	// 	// I don't understand why setting a context with a timeout doesn't work,
	// 	// but it definitely doesn't.  It always says the context is canceled. ?!
	// 	ctxP, cancelP = context.WithTimeout(ctx, x.config.KafkaProduceTimeout.AsDuration())
	// 	defer cancelP()
	// }
	// https://pkg.go.dev/google.golang.org/protobuf/types/known/durationpb

	kafkaStartTime := time.Now()

	if c.debugLevel > 10 {
		log.Printf("destKafka kafkaStartTime:%s", kafkaStartTime.String())
	}

	var wg sync.WaitGroup
	wg.Add(1)

	kClient.Produce(
		ctxP,
		kgoRecord,
		func(kgoRecord *kgo.Record, err error) {
			defer func() {
				wg.Done()
				kgoRecordPool.Put(kgoRecord)
			}()

			dur := time.Since(kafkaStartTime)
			//cancelP()
			if err != nil {
				if c.debugLevel > 10 {
					log.Printf("destKafka %0.6fs Produce err:%v", dur.Seconds(), err)
				}
				return
			}

			if c.debugLevel > 10 {
				log.Printf("destKafka len:%d %0.6fs %dms", len, dur.Seconds(), dur.Milliseconds())
			}
		},
	)

	wg.Wait()

	// if err := x.kClient.ProduceSync(ctxP, kgoRecord).FirstErr(); err != nil {
	// 	dur := time.Since(kafkaStartTime)
	// 	x.kgoRecordPool.Put(kgoRecord)
	// 	cancelP()
	// 	x.pH.WithLabelValues("destKafka", "ProduceSync", "error").Observe(dur.Seconds())
	// 	x.pC.WithLabelValues("destKafka", "ProduceSync", "error").Inc()
	// 	if x.debugLevel > 10 {
	// 		log.Printf("destKafka %0.6fs ProduceSync err:%v", dur.Seconds(), err)
	// 	}
	// 	return 0, err
	// }
	// x.pH.WithLabelValues("destKafka", "ProduceSync", "count").Observe(time.Since(kafkaStartTime).Seconds())
	// x.pC.WithLabelValues("destKafka", "ProduceSync", "count").Inc()

	// x.kgoRecordPool.Put(kgoRecord)
	// cancelP()

	if c.debugLevel > 10 {
		log.Println("destKafka complete")
	}

	return 1, err
}

// InitDestKafka creates the franz-go kafka client
func InitDestKafka(ctx context.Context, c config) {

	if !c.kafka {
		return
	}

	// https://github.com/twmb/franz-go/tree/master/plugin/kprom
	kgoMetrics := kprom.NewMetrics("kgo")

	// initialize the kafka client
	// https://pkg.go.dev/github.com/twmb/franz-go/pkg/kgo#ProducerOpt

	if c.debugLevel > 10 {
		log.Printf("c.topic:%s\n", c.topic)
		log.Println("c.broker:", c.broker)
	}

	opts := []kgo.Opt{
		// https://pkg.go.dev/github.com/twmb/franz-go/pkg/kgo#DefaultProduceTopic
		kgo.DefaultProduceTopic(c.topic),
		kgo.ClientID(c.clientID),

		// https://pkg.go.dev/github.com/twmb/franz-go/pkg/kgo#SeedBrokers
		kgo.SeedBrokers(c.broker),

		// https://pkg.go.dev/github.com/twmb/franz-go/pkg/kgo#ProducerBatchCompression
		// https://pkg.go.dev/github.com/twmb/franz-go/pkg/kgo#Lz4Compression
		kgo.ProducerBatchCompression(
			kgo.ZstdCompression(),
			kgo.Lz4Compression(),
			kgo.SnappyCompression(),
			kgo.NoCompression(),
		),

		// https://pkg.go.dev/github.com/twmb/franz-go/pkg/kgo#AllowAutoTopicCreation
		kgo.AllowAutoTopicCreation(),

		// https://pkg.go.dev/github.com/twmb/franz-go/pkg/kgo#WithHooks
		kgo.WithHooks(kgoMetrics),

		// https://pkg.go.dev/github.com/twmb/franz-go/pkg/kgo#MaxBufferedRecords
		// kgo.MaxBufferedRecords(250<<20 / *recordBytes + 1), // default is 10k records
		// https://pkg.go.dev/github.com/twmb/franz-go/pkg/kgo#ProducerBatchMaxBytes
		// kgo.ProducerBatchMaxBytes(int32(*batchMaxBytes)),  // default is ~1MB
		// https://pkg.go.dev/github.com/twmb/franz-go/pkg/kgo#RetryBackoffFn
		// default jittery exponential backoff that ranges from 250ms min to 2.5s max

		// https://pkg.go.dev/github.com/twmb/franz-go/pkg/kgo#DisableIdempotentWrite
		kgo.DisableIdempotentWrite(),

		// https://pkg.go.dev/github.com/twmb/franz-go/pkg/kgo#BrokerMaxWriteBytes
		// maxBrokerWriteBytes: 100 << 20,
		// Kafka socket.request.max.bytes default is 100<<20 = 104857600 = 100 MB
		// https://github.com/twmb/franz-go/blob/v1.17.1/pkg/kgo/config.go#L483C3-L483C87
		// https://www.wolframalpha.com/input?i=1+%3C%3C+10
		kgo.BrokerMaxWriteBytes(100 << 18), // 26214400 = 26 MB

		// https://pkg.go.dev/github.com/twmb/franz-go/pkg/kgo#ProducerBatchMaxBytes
		// Copied from the benchmark
		// https://github.com/twmb/franz-go/blob/master/examples/bench/main.go#L104
		kgo.ProducerBatchMaxBytes(1000000), // 1 MB

		// Debugging in the kgo client
		// kgo.WithLogger(kgo.BasicLogger(os.Stderr, kgo.LogLevelDebug, func() string {
		// 	return time.Now().Format("[2006-01-02 15:04:05.999] ")
		// })),
	}
	var err error
	kClient, err = kgo.NewClient(opts...)
	if err != nil {
		log.Fatalf("unable to create client:%v", err)
	}

	// https://pkg.go.dev/github.com/twmb/franz-go/pkg/kgo#Client.AllowRebalance
	kClient.AllowRebalance()

	// errP := x.pingKafkaWithRetries(ctx, kafkaPingRetriesCst, kafkaPingRetrySleepCst)
	// if errP != nil {
	// 	log.Fatalf("InitDestKafka pingKafkaWithRetries errP:%v", errP)
	// }

}

func getLatestSchemaID(subject string) (int, error) {
	fmt.Println("getLatestSchemaID")

	url := fmt.Sprintf("%s/subjects/%s/versions/latest", schemaRegistryURLCst, subject)

	resp, err := http.Get(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var result struct {
		ID int `json:"id"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}

	return result.ID, nil
}
