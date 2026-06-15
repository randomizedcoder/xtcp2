//go:build dest_kafka

package xtcp

import (
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
)

// kafkaProducer captures the surface of *kgo.Client that kafkaDest
// actually calls. Lifting it to an interface lets the destination's
// Send/Close/pingKafkaWithRetries paths run against an in-process
// fake without a real broker — see destinations_kafka_test.go.
// Production uses *kgo.Client which satisfies this interface via its
// concrete methods.
type kafkaProducer interface {
	Produce(ctx context.Context, r *kgo.Record, promise func(*kgo.Record, error))
	Flush(ctx context.Context) error
	Close()
	Ping(ctx context.Context) error
	AllowRebalance()
}

// kafkaDest produces each marshalled record to a Kafka topic via franz-go.
// Construction registers the proto schema with the Schema Registry, dials
// the broker, and primes a sync.Pool of kgo.Record so each send avoids
// allocation.
type kafkaDest struct {
	x          *XTCP
	client     kafkaProducer
	regClient  *sr.Client
	schemaID   int
	recordPool sync.Pool
}

// newKafkaProducerFn is the factory tests swap to inject a fake
// kafkaProducer without standing up a real kgo.Client. Production
// callers leave this at the default (newKafkaProducerReal).
var newKafkaProducerFn = newKafkaProducerReal

// newKafkaProducerReal is the production factory: it constructs a real
// kgo.Client wired with the production options. Extracted so the test
// suite can substitute newKafkaProducerFn with a fake-returning
// closure and exercise newKafkaDest without a broker.
func newKafkaProducerReal(opts ...kgo.Opt) (kafkaProducer, error) {
	return kgo.NewClient(opts...)
}

func newKafkaDest(ctx context.Context, x *XTCP) (Destination, error) {
	d := &kafkaDest{
		x: x,
		recordPool: sync.Pool{
			New: func() any { return new(kgo.Record) },
		},
	}

	// Schema-registry registration is informational only — ClickHouse
	// (the production consumer) does not consult the registry to decode
	// messages; it loads xtcp_flat_record.proto from format_schemas via
	// its kafka_schema setting. Registry incompatibility with a prior
	// schema version (e.g. after the Phase 0 proto refactor that
	// collapsed Envelope.XtcpFlatRecord to top-level XtcpFlatRecord)
	// would otherwise wedge xtcp2 in a respawn loop. Log + continue.
	//
	// Counter bumps would crash here: InitDests runs before InitPromethus,
	// so x.pC is still nil. Log-only is enough for ops visibility —
	// systemd journal captures all log lines.
	if err := d.registerProtobufSchema(ctx); err != nil {
		log.Printf("newKafkaDest registerProtobufSchema (non-fatal): %v", err)
	} else if schemaID, errLookup := d.getLatestSchemaID(ctx); errLookup != nil {
		log.Printf("newKafkaDest getLatestSchemaID (non-fatal): %v", errLookup)
	} else if schemaID != d.schemaID {
		log.Printf("newKafkaDest registry schemaID:%d != local schemaID:%d (non-fatal)", schemaID, d.schemaID)
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

	compression, errComp := resolveKafkaCompression(x.config.KafkaCompression)
	if errComp != nil {
		return nil, errComp
	}

	opts := []kgo.Opt{
		kgo.DefaultProduceTopic(x.config.Topic),
		kgo.ClientID("xtcp2"),
		kgo.SeedBrokers(broker),
		kgo.ProducerBatchCompression(compression...),
		kgo.AllowAutoTopicCreation(),
		kgo.WithHooks(kgoMetrics),
		kgo.DisableIdempotentWrite(),
		kgo.BrokerMaxWriteBytes(1 << 20),
		kgo.ProducerBatchMaxBytes(1000000),
		kgo.WithLogger(kgo.BasicLogger(os.Stderr, kgo.LogLevelDebug, func() string {
			return time.Now().Format("[2006-01-02 15:04:05.999] ")
		})),
	}

	var err error
	d.client, err = newKafkaProducerFn(opts...)
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
	// Reset the pooled record to zero before re-populating. kgo.Record
	// has many fields beyond {Topic,Value} — Partition, Timestamp,
	// Attrs, ProducerEpoch/ID, LeaderEpoch, Offset, Headers, Key — and
	// the franz-go producer sets several of these (notably Partition
	// and Timestamp) during Produce. Without the reset, a recycled
	// record's previous Partition stayed assigned → every reused record
	// pinned to one partition; previous Timestamp stayed → every
	// reused record claimed the time of the first send. Topic/Value
	// overwrite below preserves the original intent.
	rec := d.recordPool.Get().(*kgo.Record)
	*rec = kgo.Record{
		Topic: d.x.config.Topic,
		Value: *b,
	}
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
			// Release the WithTimeout resources whether the produce
			// succeeded or failed; the previous code only called cancelP
			// in the err branch, leaking a goroutine + timer per
			// successful send until the timeout naturally fired.
			defer cancelP()
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
		// franz-go's Close cancels in-flight produces without waiting for
		// their broker acks — records buffered when shutdown fires were
		// silently dropped. Flush first with a bounded timeout so the
		// daemon's last poll cycle is durably delivered before teardown.
		flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := d.client.Flush(flushCtx); err != nil {
			d.x.pC.WithLabelValues("destKafka", "FlushOnClose", "error").Inc()
			if d.x.debugLevel > 10 {
				log.Printf("destKafka Flush on Close: %v", err)
			}
		}
		cancel()
		d.client.Close()
	}
	return nil
}

func (d *kafkaDest) getLatestSchemaID(ctx context.Context) (int, error) {
	url := fmt.Sprintf("%s/subjects/%s-value/versions/latest",
		d.x.config.KafkaSchemaUrl, d.x.config.Topic)
	if d.x.debugLevel > 10 {
		log.Printf("getLatestSchemaID url:%s\n", url)
	}
	// http.Get used the DefaultClient which has no timeout — a hung
	// Schema Registry would block daemon startup indefinitely. Build
	// the request with the caller's ctx + a hard 10s ceiling so the
	// init-time call observes shutdown and never wedges.
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return 0, fmt.Errorf("getLatestSchemaID http.StatusNotFound url:%s", url)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("getLatestSchemaID url:%s unexpected status:%d", url, resp.StatusCode)
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

func (d *kafkaDest) pingKafkaWithRetries(ctx context.Context, retries int, sleepDuration time.Duration) (err error) {
	for i := 0; i < retries; i++ {
		err = d.pingKafka(ctx)
		if err != nil {
			s := sleepDuration * time.Duration(i+1)
			if d.x.debugLevel > 10 {
				log.Printf("pingKafkaWithRetries i:%d sleep:%0.3fs", i, s.Seconds())
			}
			// time.Sleep would block through ctx cancellation; a
			// startup-time ctx-cancel should abort retries promptly.
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(s):
			}
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

// resolveKafkaCompression maps the string knob to a franz-go compression
// preference list. "" / "auto" preserves the original behavior — franz-go
// picks the first codec the broker advertises support for. Explicit
// values pin a single codec; an unknown value is fatal at startup so a
// typo in config doesn't silently fall through to None.
//
// All codecs are mutually decodable by Redpanda + ClickHouse's Kafka
// engine (librdkafka), so the choice is purely a producer CPU/throughput
// tradeoff. See xtcp_config.proto's KafkaCompression docs.
func resolveKafkaCompression(name string) ([]kgo.CompressionCodec, error) {
	switch name {
	case "", "auto":
		return []kgo.CompressionCodec{
			kgo.ZstdCompression(),
			kgo.Lz4Compression(),
			kgo.SnappyCompression(),
			kgo.NoCompression(),
		}, nil
	case "zstd":
		return []kgo.CompressionCodec{kgo.ZstdCompression()}, nil
	case "lz4":
		return []kgo.CompressionCodec{kgo.Lz4Compression()}, nil
	case "snappy":
		return []kgo.CompressionCodec{kgo.SnappyCompression()}, nil
	case "gzip":
		return []kgo.CompressionCodec{kgo.GzipCompression()}, nil
	case "none":
		return []kgo.CompressionCodec{kgo.NoCompression()}, nil
	default:
		return nil, fmt.Errorf("newKafkaDest unknown KafkaCompression:%q (want one of: auto, zstd, lz4, snappy, gzip, none)", name)
	}
}

func init() {
	RegisterDestination("kafka", newKafkaDest)
}
