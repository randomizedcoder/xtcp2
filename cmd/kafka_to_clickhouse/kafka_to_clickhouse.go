package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/clickhouse_protolist"
	"github.com/randomizedcoder/xtcp2/pkg/xsync"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/plugin/kprom"
	"google.golang.org/protobuf/encoding/protodelim"
)

// 	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
// import "google.golang.org/protobuf/encoding/protowire"
// import "google.golang.org/protobuf/encoding/protodelim"

const (
	schemaRegistryURLCst = "http://localhost:18081"

	brokerCst = "127.0.0.1:9092"
	// brokerCst   = "redpanda-0:9092"
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
	kgoRecordPool *xsync.Pool[*kgo.Record]

	// fatalf is the package-level abort handler. Defaults to log.Fatalf;
	// tests swap this in for a capture so the dump-file write error
	// branch in prepareBinary is exercisable.
	fatalf = log.Fatalf
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
	os.Exit(runMain(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

// runMain wires flag parsing + InitDestKafka + primaryFunction. Extracted
// so tests can drive it with synthetic args + a cancellable ctx without
// connecting to Kafka or exiting.
func runMain(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("kafka_to_clickhouse", flag.ContinueOnError)
	fs.SetOutput(stderr)
	filename := fs.String("filename", "protoBytes.bin", "filename")
	valueStr := fs.String("values", "1", "values uints -> uint32, comma separated")
	envelope := fs.Bool("envelope", true, "envelope")
	kafka := fs.Bool("kafka", true, "kafka")
	broker := fs.String("broker", brokerCst, "broker string")
	topic := fs.String("topic", topicCst, "kafka topic")
	clientID := fs.String("clientID", clientIDCst, "clientID")
	loops := fs.Int("loops", loopsCst, "loops")
	loopsSleep := fs.Duration("loopsSleep", loopSleepCst, "loops sleep duration")
	dump := fs.Bool("dump", false, "dump proto for debug")
	dumpFilename := fs.String("dumpFileName", "dump.bin", "dump file name")
	d := fs.Int("d", debugLevelCst, "debug level")
	v := fs.Bool("v", false, "show version")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *v {
		fmt.Fprintf(stdout, "commit:%s\tdate(UTC):%s\tversion:%s\n", commit, date, version)
		return 0
	}

	valueStrs := strings.Split(*valueStr, ",")
	values := make([]uint, 0, len(valueStrs))
	for _, str := range valueStrs {
		parsed, err := strconv.ParseUint(str, 10, 32)
		if err != nil {
			fmt.Fprintf(stderr, "Invalid value: %v\n", err)
			return 1
		}
		values = append(values, uint(parsed))
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

	kgoRecordPool = xsync.NewPool(func() *kgo.Record {
		return new(kgo.Record)
	})

	if c.kafka {
		if err := InitDestKafka(ctx, c); err != nil {
			fmt.Fprintf(stderr, "InitDestKafka: %v\n", err)
			return 1
		}
		// Flush + Close the producer on the way out. Without this the
		// final batch of records in franz-go's send buffer was dropped
		// on process exit (same shape as bug 28 in pkg/xtcp). Flush is
		// bounded by a 5s timeout so a wedged broker doesn't block
		// shutdown indefinitely.
		defer func() {
			// Derive flushCtx from the caller's ctx so cancellation
			// propagates correctly; cap at 5s so a wedged broker
			// doesn't block teardown indefinitely.
			flushCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			if err := kClient.Flush(flushCtx); err != nil {
				fmt.Fprintf(stderr, "kgo Flush on exit: %v\n", err)
			}
			kClient.Close()
		}()
	}
	primaryFunction(ctx, c)
	return 0
}

func primaryFunction(ctx context.Context, c config) {
	id, err := getLatestSchemaID(ctx, c.subject)
	if err != nil {
		log.Printf("getLatestSchemaID err:%v", err)
	}
	if c.debugLevel > 10 {
		log.Printf("getLatestSchemaID id:%d", id)
	}

	for i := 0; i < c.loops; i++ {
		select {
		case <-ctx.Done():
			return
		default:
		}
		binaryData := prepareBinary(ctx, c, id)
		incrementSlice(c, &c.values, 1)
		if c.debugLevel > 10 {
			log.Printf("primaryFunction i:%d", i)
		}
		fileOrKafka(ctx, c, &binaryData)
		// Bug fix: time.Sleep ignored ctx, so SIGTERM took up to
		// loopsSleep (default 10s) to be observed. Use a ctx-aware
		// wait so shutdown is prompt.
		select {
		case <-ctx.Done():
			return
		case <-time.After(c.loopsSleep):
		}
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
			errW := os.WriteFile(c.dumpFilename+".envelope", binaryData, 0600) // gosec G306
			if errW != nil {
				fatalf("Failed to write protobuf envelope data: %v", errW)
			}
		}

	}

	return binaryData
}

func fileOrKafka(ctx context.Context, c config, binaryData *[]byte) {
	if !c.kafka {
		if err := writeDataToFile(ctx, c.filename, *binaryData); err != nil {
			log.Println("Error:", err)
		}
		return
	}

	n, errK := destKafka(ctx, c, binaryData)
	if errK != nil {
		log.Println("errk:", errK)
	}
	if c.debugLevel > 10 {
		log.Printf("destKafka n:%d", n)
	}
}

func writeDataToFile(_ context.Context, filename string, data []byte) error {

	err := os.WriteFile(filename, data, 0600) // 0600 permissions (rw-------) per gosec G306
	if err != nil {
		return fmt.Errorf("error writing to file: %w", err) // Wrap the error
	}
	return nil

}

// destKafka sends the protobuf to kafka
func destKafka(ctx context.Context, c config, xtcpRecordBinary *[]byte) (n int, err error) {

	if c.debugLevel > 10 {
		log.Println("destKafka start")
	}

	kgoRecord := kgoRecordPool.Get()
	// defer x.kgoRecordPool.Put(kgoRecord)

	// Reset the pooled record to zero before re-populating. Without this
	// the previous send's Partition / Timestamp / etc fields persisted
	// (set internally by franz-go's Produce), pinning every recycled
	// record to one partition + freezing the timestamp. See bug 55 in
	// pkg/xtcp/destinations_kafka.go for the longer write-up — same
	// fix shape in this binary.
	*kgoRecord = kgo.Record{
		Topic: c.topic,
		Value: *xtcpRecordBinary,
	}
	binaryLen := len(*xtcpRecordBinary)

	// Propagate the caller's context to kClient.Produce so cancellation
	// flows correctly. Previously a nil `ctxP` was passed (contextcheck).
	// var cancelP context.CancelFunc
	// if x.config.KafkaProduceTimeout.AsDuration() != 0 {
	// 	ctx, cancelP = context.WithTimeout(ctx, x.config.KafkaProduceTimeout.AsDuration())
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
		ctx,
		kgoRecord,
		func(kgoRecord *kgo.Record, err error) {
			defer func() {
				wg.Done()
				kgoRecordPool.Put(kgoRecord)
			}()

			dur := time.Since(kafkaStartTime)
			// cancelP()
			if err != nil {
				if c.debugLevel > 10 {
					log.Printf("destKafka %0.6fs Produce err:%v", dur.Seconds(), err)
				}
				return
			}

			if c.debugLevel > 10 {
				log.Printf("destKafka len:%d %0.6fs %dms", binaryLen, dur.Seconds(), dur.Milliseconds())
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
func InitDestKafka(ctx context.Context, c config) error {

	if !c.kafka {
		return nil
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
		return fmt.Errorf("kgo.NewClient: %w", err)
	}
	kClient.AllowRebalance()
	return nil
}

func getLatestSchemaID(ctx context.Context, subject string) (int, error) {
	return getLatestSchemaIDAt(ctx, http.DefaultClient, schemaRegistryURLCst, subject)
}

// getLatestSchemaIDAt fetches the latest schema ID for `subject` via the
// supplied HTTP client + base URL. Extracted so tests can drive it against
// an httptest.Server instead of the hardcoded schemaRegistryURLCst.
func getLatestSchemaIDAt(ctx context.Context, client *http.Client, baseURL, subject string) (int, error) {
	url := fmt.Sprintf("%s/subjects/%s/versions/latest", baseURL, subject)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	// Schema Registries return a JSON error body on 4xx/5xx; decoding it
	// into the {id int} struct silently yields id:0. Reject non-2xx
	// upfront so kafka_to_clickhouse's downstream Produce path doesn't
	// stamp every Kafka record with a bogus schemaID:0 magic header.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("getLatestSchemaIDAt %s: unexpected status:%d", url, resp.StatusCode)
	}

	var result struct {
		ID int `json:"id"`
	}

	if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}

	return result.ID, nil
}
