package xtcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	nats "github.com/nats-io/nats.go"
	nsq "github.com/nsqio/go-nsq"
	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
	redis "github.com/redis/go-redis/v9"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sr"
	"github.com/twmb/franz-go/plugin/kprom"
)

const (
	kafkaPingTimeoutCst    = 5 * time.Second
	kafkaPingRetriesCst    = 5
	kafkaPingRetrySleepCst = 1 * time.Second
	//kafkaClientProduceTimeoutCst = 100 * time.Millisecond

	natsReconnectsCst = 5
	//natsReconnectWaitCst = 1 * time.Second
	natsTimeoutCst = 1 * time.Second

	valkeyPingTimeoutCst  = 2 * time.Second
	valkeyMaxIdleConnsCst = 20
	valkeyTimeoutCst      = 1 * time.Second
)

var (
	validDestinationsMap = map[string]bool{
		"null":   true,
		"kafka":  true,
		"nsq":    true,
		"udp":    true,
		"nats":   true,
		"valkey": true,
	}
)

func validDestinations() (dests string) {
	for key := range validDestinationsMap {
		dests = dests + key + ","
	}
	return strings.TrimSuffix(dests, ",")
}

// InitDestinations parses the destination config, to determine
// which of the destination modules to use
// This function is using sync.Maps as the way to map to the actual
// function to be used
// This will in future allow the destination to be changed dyanmically
// at runtime (TODO implement this)
func (x *XTCP) InitDests(ctx context.Context, wg *sync.WaitGroup) {

	defer wg.Done()

	dest, _, _ := strings.Cut(x.config.Dest, ":")
	if _, ok := validDestinationsMap[dest]; !ok {
		log.Fatalf("InitDestinations XTCP Dest invalid:%s, must be one of:%s", dest, validDestinations())
	}

	x.Destinations.Store("null", func(ctx context.Context, xtcpRecordBinary *[]byte) (n int, err error) {
		return x.destNull(ctx, xtcpRecordBinary)
	})
	x.Destinations.Store("kafka", func(ctx context.Context, xtcpRecordBinary *[]byte) (n int, err error) {
		return x.destKafka(ctx, xtcpRecordBinary)
	})
	x.Destinations.Store("nsq", func(ctx context.Context, xtcpRecordBinary *[]byte) (n int, err error) {
		return x.destNSQ(ctx, xtcpRecordBinary)
	})
	x.Destinations.Store("udp", func(ctx context.Context, xtcpRecordBinary *[]byte) (n int, err error) {
		return x.destUDP(ctx, xtcpRecordBinary)
	})
	x.Destinations.Store("nats", func(ctx context.Context, xtcpRecordBinary *[]byte) (n int, err error) {
		return x.destNATS(ctx, xtcpRecordBinary)
	})
	x.Destinations.Store("valkey", func(ctx context.Context, xtcpRecordBinary *[]byte) (n int, err error) {
		return x.destValKey(ctx, xtcpRecordBinary)
	})

	f, ok := x.Destinations.Load(dest)
	if !ok {
		log.Fatalf("InitDestinations XTCP Dest load invalid:%s, must be one of:%s", dest, validDestinations())
	}
	x.Destination = f.(func(ctx context.Context, xtcpRecordBinary *[]byte) (n int, err error))

	// no null initilizer
	x.InitDestinations.Store("kafka", func(ctx context.Context) {
		x.InitDestKafka(ctx)
	})
	x.InitDestinations.Store("nsq", func(ctx context.Context) {
		x.InitDestNSQ(ctx)
	})
	x.InitDestinations.Store("udp", func(ctx context.Context) {
		x.InitDestUDP(ctx)
	})
	x.InitDestinations.Store("nats", func(ctx context.Context) {
		x.InitDestNATS(ctx)
	})
	x.InitDestinations.Store("valkey", func(ctx context.Context) {
		x.InitDestValKey(ctx)
	})

	if f, ok := x.InitDestinations.Load(dest); ok {
		f.(func(ctx context.Context))(ctx)
	}

	// please note that at this point we _could_ remove any destinations from the map
	// that we aren't using

	x.DestinationReady <- struct{}{}

}

// InitDestKafka creates the franz-go kafka client
func (x *XTCP) InitDestKafka(ctx context.Context) {

	x.registerProtobufSchema(ctx)

	schemaID, err := x.getLatestSchemaID()
	if err != nil {
		log.Fatalf("InitDestKafka x.getLatestSchemaID() err:%v", err)
	}

	if schemaID != x.schemaID {
		log.Fatalf("InitDestKafka schemaID:%d != x.schemaID:%d", schemaID, x.schemaID)
	}

	x.kSerde.Register(
		x.schemaID,
		&xtcp_flat_record.Envelope{},
		sr.EncodeFn(func(v any) ([]byte, error) {
			return *x.protobufListMarshal(v.(*xtcp_flat_record.Envelope)), nil
		}),
		sr.Index(0),
		// No need to decode currently
		// sr.DecodeFn(func(b []byte, v any) error {
		// 	return avro.Unmarshal(avroSchema, b, v)
		// }),
	)
	// https://github.com/cloudhut/owl-shop/blob/7095131ece7a0fee9a58d00b4fbc9f820a0d13be/pkg/shop/order_service.go#L184
	// code lifted from
	// https://github.com/twmb/franz-go/blob/35ab5e5f5327ca190b49d4b14f326db4365abb9f/examples/schema_registry/schema_registry.go#L65C1-L74C3
	// https://pkg.go.dev/github.com/twmb/franz-go/pkg/sr@v1.3.0#Index

	// https://github.com/twmb/franz-go/tree/master/plugin/kprom
	kgoMetrics := kprom.NewMetrics("kgo")

	// initialize the kafka client
	// https://pkg.go.dev/github.com/twmb/franz-go/pkg/kgo#ProducerOpt

	broker := strings.Replace(x.config.Dest, "kafka:", "", 1)

	if x.debugLevel > 10 {
		log.Println("config.Topic:", x.config.Topic)
		log.Println("config.Dest:", x.config.Dest)
		log.Println("config.KafkaSchemaUrl:", x.config.KafkaSchemaUrl)
		log.Println("broker:", broker)
		log.Println("x.schemaID:", x.schemaID)
	}

	opts := []kgo.Opt{
		// https://pkg.go.dev/github.com/twmb/franz-go/pkg/kgo#DefaultProduceTopic
		kgo.DefaultProduceTopic(x.config.Topic),
		kgo.ClientID("xtcp2"),

		// https://pkg.go.dev/github.com/twmb/franz-go/pkg/kgo#SeedBrokers
		kgo.SeedBrokers(broker),

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
		kgo.WithLogger(kgo.BasicLogger(os.Stderr, kgo.LogLevelDebug, func() string {
			return time.Now().Format("[2006-01-02 15:04:05.999] ")
		})),
	}

	var errK error
	x.kClient, errK = kgo.NewClient(opts...)
	if errK != nil {
		log.Fatalf("unable to create client:%v", errK)
	}

	// https://pkg.go.dev/github.com/twmb/franz-go/pkg/kgo#Client.AllowRebalance
	x.kClient.AllowRebalance()

	errP := x.pingKafkaWithRetries(ctx, kafkaPingRetriesCst, kafkaPingRetrySleepCst)
	if errP != nil {
		log.Fatalf("InitDestKafka pingKafkaWithRetries errP:%v", errP)
	}

}

func (x *XTCP) getLatestSchemaID() (int, error) {

	url := fmt.Sprintf("%s/subjects/%s-value/versions/latest", x.config.KafkaSchemaUrl, x.config.Topic)

	if x.debugLevel > 10 {
		log.Printf("getLatestSchemaID url:%s\n", url)
	}

	resp, err := http.Get(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		log.Fatal("getLatestSchemaID http.StatusNotFound")
	}

	var result struct {
		ID int `json:"id"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}

	if x.debugLevel > 10 {
		log.Printf("getLatestSchemaID result.ID:%d", result.ID)
	}

	return result.ID, nil
}

func (x *XTCP) registerProtobufSchema(ctx context.Context) {

	var err error
	x.kRegClient, err = sr.NewClient(sr.URLs(x.config.KafkaSchemaUrl))
	if err != nil {
		log.Fatalf("unable to create schema registry client: %v", err)
	}

	schemaBytes, errF := os.ReadFile(x.config.XtcpProtoFile)
	if errF != nil {
		log.Fatalf("registerProtobufSchema failed to read proto file: %v", errF)
	}

	schema := sr.Schema{
		Schema: string(schemaBytes),
		Type:   sr.TypeProtobuf,
	}

	s, errC := x.kRegClient.CreateSchema(ctx, x.config.Topic+"-value", schema)
	if errC != nil {
		log.Fatalf("registerProtobufSchema CreateSchema er: %v", errC)
	}

	x.schemaID = s.ID

	if x.debugLevel > 10 {
		log.Printf("registerProtobufSchema schema registered, x.schemaID:%d\n", x.schemaID)
	}

}

//lint:ignore U1000 not used yet. this was my original implmentation
func (x *XTCP) registerProtobufSchemaRestful() {

	url := fmt.Sprintf("%s/subjects/%s-value/versions", x.config.KafkaSchemaUrl, x.config.Topic)

	if x.debugLevel > 10 {
		log.Printf("registerProtobufSchema url:%s\n", url)
	}

	data, err := os.ReadFile(x.config.XtcpProtoFile)
	if err != nil {
		log.Fatalf("registerProtobufSchema failed to read proto file: %v", err)
	}

	// SchemaRequest represents the payload to send to the schema registry
	type SchemaRequest struct {
		Schema     string `json:"schema"`
		SchemaType string `json:"schemaType"`
		//Name       string `json:"name"` // Name only exists for Confluent kafka
	}

	reqBody := SchemaRequest{
		Schema:     string(data),
		SchemaType: "PROTOBUF",
		//Name:       "xtcp_flat_record.v1.Envelope",
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		log.Fatalf("registerProtobufSchema failed to marshal request: %v", err)
	}

	resp, err := http.Post(url, "application/vnd.schemaregistry.v1+json", bytes.NewReader(bodyBytes))
	if err != nil {
		log.Fatalf("registerProtobufSchema failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Fatalf("registerProtobufSchema unexpected status code: %d", resp.StatusCode)
	}

	if x.debugLevel > 10 {
		log.Printf("Schema registered successfully under subject:%s-value", x.config.Topic)
	}
}

func (x *XTCP) InitDestNSQ(ctx context.Context) {

	nsqServer := strings.Replace(x.config.Dest, "nsq:", "", 1)

	if x.debugLevel > 10 {
		log.Printf("config.Topic:%s\n", x.config.Topic)
		log.Println("config.Dest:", x.config.Dest)
		log.Println("nsqServer:", nsqServer)
	}

	config := nsq.NewConfig()

	var err error
	x.nsqProducer, err = nsq.NewProducer(nsqServer, config)
	if err != nil {
		log.Fatalf("unable to nsq.NewProducer:%v", err)
	}

}

// InitDestUDP creates a UDP socket to send protobufs over
func (x *XTCP) InitDestUDP(ctx context.Context) {

	dest := strings.Replace(x.config.Dest, "udp:", "", 1)

	if x.debugLevel > 10 {
		log.Printf("config.Topic:%s\n", x.config.Topic)
		log.Println("config.Dest:", x.config.Dest)
		log.Println("dest:", dest)
	}

	var err error
	x.udpConn, err = net.Dial("udp", dest)
	if err != nil {
		log.Fatalf("unable to net.Dial:%v", err)
	}
	//defer udpConn.Close()
}

// InitDestNATS creates the nats client
// https://github.com/nats-io/nats.go?tab=readme-ov-file#basic-usage
func (x *XTCP) InitDestNATS(ctx context.Context) {

	dest := strings.Replace(x.config.Dest, "nats:", "", 1)

	if x.debugLevel > 10 {
		log.Printf("config.Topic:%s\n", x.config.Topic)
		log.Println("config.Dest:", x.config.Dest)
		log.Println("dest:", dest)
	}

	// https://github.com/nats-io/nats.go?tab=readme-ov-file#advanced-usage
	// https://pkg.go.dev/github.com/nats-io/nats.go@v1.37.0#Connect
	//x.natsClient, err = nats.Connect(nats.DefaultURL)

	opts := nats.Options{
		Url:                  dest,
		AllowReconnect:       true,
		MaxReconnect:         natsReconnectsCst,
		ReconnectWait:        2 * time.Second,        // default
		ReconnectJitter:      100 * time.Millisecond, // default
		RetryOnFailedConnect: true,
		Timeout:              natsTimeoutCst,
	}

	var err error
	x.natsClient, err = opts.Connect()
	if err != nil {
		log.Fatalf("InitDestNATS err:%v", err)
	}
}

// InitDestValKey creates the nats client
// https://redis.uptrace.dev/guide/go-redis.html#installation
// https://github.com/redis/go-redis?tab=readme-ov-file#quickstart
func (x *XTCP) InitDestValKey(ctx context.Context) {

	dest := strings.Replace(x.config.Dest, "valkey:", "", 1)

	if x.debugLevel > 10 {
		log.Printf("config.Topic:%s\n", x.config.Topic)
		log.Println("config.Dest:", x.config.Dest)
		log.Println("dest:", dest)
	}

	// https://pkg.go.dev/github.com/redis/go-redis/v9@v9.6.1#Options
	x.valKeyClient = redis.NewClient(&redis.Options{
		Addr:         dest,
		Password:     "", // no password set
		DB:           0,  // use default DB
		MaxIdleConns: valkeyMaxIdleConnsCst,
	})

	ctxP, cancelP := context.WithTimeout(ctx, valkeyPingTimeoutCst)
	defer cancelP()

	pTime := time.Now()
	_, err := x.valKeyClient.Ping(ctxP).Result()
	dur := time.Since(pTime)
	if err != nil {
		log.Fatalf("InitDestValKey time:%0.6fs err:%v", dur.Seconds(), err)
	}
	if x.debugLevel > 10 {
		log.Printf("InitDestValKey time:%0.3fs", dur.Seconds())
	}

}

// pingKafkaWithRetries pings kafka with a retry loop, and sleeps
func (x *XTCP) pingKafkaWithRetries(ctx context.Context, retries int, sleepDuration time.Duration) (err error) {
	for i := 0; i < retries; i++ {
		err = x.pingKafka(ctx)
		if err != nil {
			s := sleepDuration * time.Duration(i+1)
			if x.debugLevel > 10 {
				log.Printf("pingKafkaWithRetries i:%d sleep:%0.3fs", i, s.Seconds())
			}
			time.Sleep(s)
			continue
		}
		break
	}
	return err
}

// pingKafka performs a kafka ping ( although I don't really know what this does )
func (x *XTCP) pingKafka(ctx context.Context) (err error) {
	pCst, pCancel := context.WithTimeout(ctx, kafkaPingTimeoutCst)
	defer pCancel()
	pTime := time.Now()
	err = x.kClient.Ping(pCst)
	if err != nil {
		log.Printf("pingKafka unable to kafka ping:%v time:%0.6fs", err, time.Since(pTime).Seconds())
		return err
	}
	if x.debugLevel > 10 {
		log.Printf("pingKafka kafka ping time:%0.6fs\n", time.Since(pTime).Seconds())
	}
	return err
}
