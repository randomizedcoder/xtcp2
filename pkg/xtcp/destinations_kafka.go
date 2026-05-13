//go:build dest_kafka

package xtcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sr"
	"github.com/twmb/franz-go/plugin/kprom"
)

const (
	kafkaPingTimeoutCst    = 5 * time.Second
	kafkaPingRetriesCst    = 5
	kafkaPingRetrySleepCst = 1 * time.Second

	// For protobuf the size is at least 6, not 5
	// https://docs.confluent.io/platform/current/schema-registry/fundamentals/serdes-develop/index.html#wire-format
	KafkaHeaderSizeCst = 6
)

// kafkaDest produces each marshalled record to a Kafka topic via franz-go.
// Construction registers the proto schema with the Schema Registry, dials
// the broker, and primes a sync.Pool of kgo.Record so each send avoids
// allocation.
type kafkaDest struct {
	x          *XTCP
	client     *kgo.Client
	regClient  *sr.Client
	schemaID   int
	recordPool sync.Pool
}

func newKafkaDest(ctx context.Context, x *XTCP) (Destination, error) {
	d := &kafkaDest{
		x: x,
		recordPool: sync.Pool{
			New: func() any { return new(kgo.Record) },
		},
	}

	if err := d.registerProtobufSchema(ctx); err != nil {
		return nil, err
	}

	schemaID, err := d.getLatestSchemaID()
	if err != nil {
		return nil, fmt.Errorf("newKafkaDest getLatestSchemaID: %w", err)
	}
	if schemaID != d.schemaID {
		return nil, fmt.Errorf("newKafkaDest schemaID:%d != d.schemaID:%d", schemaID, d.schemaID)
	}

	kgoMetrics := kprom.NewMetrics("kgo")
	broker := strings.Replace(x.config.Dest, "kafka:", "", 1)

	if x.debugLevel > 10 {
		log.Println("config.Topic:", x.config.Topic)
		log.Println("config.Dest:", x.config.Dest)
		log.Println("config.KafkaSchemaUrl:", x.config.KafkaSchemaUrl)
		log.Println("broker:", broker)
		log.Println("d.schemaID:", d.schemaID)
	}

	opts := []kgo.Opt{
		kgo.DefaultProduceTopic(x.config.Topic),
		kgo.ClientID("xtcp2"),
		kgo.SeedBrokers(broker),
		kgo.ProducerBatchCompression(
			kgo.ZstdCompression(),
			kgo.Lz4Compression(),
			kgo.SnappyCompression(),
			kgo.NoCompression(),
		),
		kgo.AllowAutoTopicCreation(),
		kgo.WithHooks(kgoMetrics),
		kgo.DisableIdempotentWrite(),
		kgo.BrokerMaxWriteBytes(1 << 20),
		kgo.ProducerBatchMaxBytes(1000000),
		kgo.WithLogger(kgo.BasicLogger(os.Stderr, kgo.LogLevelDebug, func() string {
			return time.Now().Format("[2006-01-02 15:04:05.999] ")
		})),
	}

	d.client, err = kgo.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("newKafkaDest kgo.NewClient: %w", err)
	}
	d.client.AllowRebalance()

	if err := d.pingKafkaWithRetries(ctx, kafkaPingRetriesCst, kafkaPingRetrySleepCst); err != nil {
		return nil, fmt.Errorf("newKafkaDest pingKafka: %w", err)
	}
	return d, nil
}

func (d *kafkaDest) Send(ctx context.Context, b *[]byte) (int, error) {
	rec := d.recordPool.Get().(*kgo.Record)
	rec.Topic = d.x.config.Topic
	rec.Value = *b
	n := len(rec.Value)

	var (
		ctxP    context.Context
		cancelP context.CancelFunc
	)
	if d.x.config.KafkaProduceTimeout.AsDuration() != 0 {
		ctxP, cancelP = context.WithTimeout(ctx, d.x.config.KafkaProduceTimeout.AsDuration())
	} else {
		ctxP = ctx
		cancelP = func() {}
	}

	start := time.Now()
	d.client.Produce(
		ctxP,
		rec,
		func(rec *kgo.Record, err error) {
			dur := time.Since(start)
			d.recordPool.Put(rec)
			*b = (*b)[:0]
			d.x.destBytesPool.Put(b)
			if err != nil {
				d.x.pH.WithLabelValues("destKafka", "Produce", "error").Observe(dur.Seconds())
				d.x.pC.WithLabelValues("destKafka", "Produce", "error").Inc()
				if d.x.debugLevel > 10 {
					log.Printf("destKafka %0.6fs Produce err:%v", dur.Seconds(), err)
				}
				cancelP()
				return
			}
			d.x.pH.WithLabelValues("destKafka", "Produce", "count").Observe(dur.Seconds())
			d.x.pC.WithLabelValues("destKafka", "Produce", "count").Inc()
			if d.x.debugLevel > 10 {
				log.Printf("destKafka len:%d %0.6fs %dms", n, dur.Seconds(), dur.Milliseconds())
			}
		},
	)
	return 1, nil
}

func (d *kafkaDest) Close() error {
	if d.client != nil {
		d.client.Close()
	}
	return nil
}

func (d *kafkaDest) getLatestSchemaID() (int, error) {
	url := fmt.Sprintf("%s/subjects/%s-value/versions/latest",
		d.x.config.KafkaSchemaUrl, d.x.config.Topic)
	if d.x.debugLevel > 10 {
		log.Printf("getLatestSchemaID url:%s\n", url)
	}
	resp, err := http.Get(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return 0, fmt.Errorf("getLatestSchemaID http.StatusNotFound url:%s", url)
	}
	var result struct {
		ID int `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}
	return result.ID, nil
}

func (d *kafkaDest) registerProtobufSchema(ctx context.Context) error {
	var err error
	d.regClient, err = sr.NewClient(sr.URLs(d.x.config.KafkaSchemaUrl))
	if err != nil {
		return fmt.Errorf("registerProtobufSchema sr.NewClient: %w", err)
	}
	schemaBytes, err := os.ReadFile(d.x.config.XtcpProtoFile)
	if err != nil {
		return fmt.Errorf("registerProtobufSchema read proto: %w", err)
	}
	schema := sr.Schema{
		Schema: string(schemaBytes),
		Type:   sr.TypeProtobuf,
	}
	s, err := d.regClient.CreateSchema(ctx, d.x.config.Topic+"-value", schema)
	if err != nil {
		return fmt.Errorf("registerProtobufSchema CreateSchema: %w", err)
	}
	d.schemaID = s.ID
	if d.x.debugLevel > 10 {
		log.Printf("registerProtobufSchema schema registered, d.schemaID:%d\n", d.schemaID)
	}
	return nil
}

// registerProtobufSchemaRestful is the original direct-HTTP implementation,
// preserved for reference. Not used.
//
//lint:ignore U1000 historical reference; not called
func (d *kafkaDest) registerProtobufSchemaRestful() error {
	url := fmt.Sprintf("%s/subjects/%s-value/versions",
		d.x.config.KafkaSchemaUrl, d.x.config.Topic)
	data, err := os.ReadFile(d.x.config.XtcpProtoFile)
	if err != nil {
		return err
	}
	type SchemaRequest struct {
		Schema     string `json:"schema"`
		SchemaType string `json:"schemaType"`
	}
	reqBody := SchemaRequest{
		Schema:     string(data),
		SchemaType: "PROTOBUF",
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}
	resp, err := http.Post(url, "application/vnd.schemaregistry.v1+json", bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("registerProtobufSchemaRestful status:%d", resp.StatusCode)
	}
	return nil
}

func (d *kafkaDest) pingKafkaWithRetries(ctx context.Context, retries int, sleepDuration time.Duration) (err error) {
	for i := 0; i < retries; i++ {
		err = d.pingKafka(ctx)
		if err != nil {
			s := sleepDuration * time.Duration(i+1)
			if d.x.debugLevel > 10 {
				log.Printf("pingKafkaWithRetries i:%d sleep:%0.3fs", i, s.Seconds())
			}
			time.Sleep(s)
			continue
		}
		break
	}
	return err
}

func (d *kafkaDest) pingKafka(ctx context.Context) error {
	pCst, pCancel := context.WithTimeout(ctx, kafkaPingTimeoutCst)
	defer pCancel()
	pTime := time.Now()
	if err := d.client.Ping(pCst); err != nil {
		log.Printf("pingKafka unable to kafka ping:%v time:%0.6fs",
			err, time.Since(pTime).Seconds())
		return err
	}
	if d.x.debugLevel > 10 {
		log.Printf("pingKafka kafka ping time:%0.6fs\n", time.Since(pTime).Seconds())
	}
	return nil
}

func init() {
	RegisterDestination("kafka", newKafkaDest)
}
