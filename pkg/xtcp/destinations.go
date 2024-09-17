package xtcp

import (
	"context"
	"log"
	"net"
	"os"
	"strings"
	"time"

	nats "github.com/nats-io/nats.go"
	nsq "github.com/nsqio/go-nsq"
	redis "github.com/redis/go-redis/v9"
	"github.com/twmb/franz-go/pkg/kgo"
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

func (x *XTCP) InitDestinations(ctx context.Context) {

	x.Destations.Store("kafka", func(ctx context.Context, xtcpRecordBinary *[]byte) (n int, err error) {
		return x.destKafka(ctx, xtcpRecordBinary)
	})
	x.Destations.Store("nsq", func(ctx context.Context, xtcpRecordBinary *[]byte) (n int, err error) {
		return x.destNSQ(ctx, xtcpRecordBinary)
	})
	x.Destations.Store("udp", func(ctx context.Context, xtcpRecordBinary *[]byte) (n int, err error) {
		return x.destUDP(ctx, xtcpRecordBinary)
	})
	x.Destations.Store("nats", func(ctx context.Context, xtcpRecordBinary *[]byte) (n int, err error) {
		return x.destNATS(ctx, xtcpRecordBinary)
	})
	x.Destations.Store("valkey", func(ctx context.Context, xtcpRecordBinary *[]byte) (n int, err error) {
		return x.destValKey(ctx, xtcpRecordBinary)
	})

	dest, _, _ := strings.Cut(*x.config.Dest, ":")

	if f, ok := x.Destations.Load(dest); ok {
		x.Destation = f.(func(ctx context.Context, xtcpRecordBinary *[]byte) (n int, err error))
	} else {
		log.Fatalf("InitDestinations XTCP Dest must be one of proto, protojson, or prototext:%s", dest)
	}

	x.InitDestations.Store("kafka", func(ctx context.Context) {
		x.InitDestKafka(ctx)
	})
	x.InitDestations.Store("nsq", func(ctx context.Context) {
		x.InitDestNSQ(ctx)
	})
	x.InitDestations.Store("udp", func(ctx context.Context) {
		x.InitDestUDP(ctx)
	})
	x.InitDestations.Store("nats", func(ctx context.Context) {
		x.InitDestNATS(ctx)
	})
	x.InitDestations.Store("valkey", func(ctx context.Context) {
		x.InitDestValKey(ctx)
	})

	if f, ok := x.InitDestations.Load(dest); ok {
		f.(func(ctx context.Context))(ctx)
	}

}

// InitDestKafka creates the franz-go kafka client
func (x *XTCP) InitDestKafka(ctx context.Context) {

	// https://github.com/twmb/franz-go/tree/master/plugin/kprom
	kgoMetrics := kprom.NewMetrics("kgo")

	// initialize the kafka client
	// https://pkg.go.dev/github.com/twmb/franz-go/pkg/kgo#ProducerOpt

	broker := strings.Replace(*x.config.Dest, "kafka:", "", 1)

	if x.debugLevel > 10 {
		log.Printf("config.Topic:%s\n", *x.config.Topic)
		log.Println("config.Dest:", *x.config.Dest)
		log.Println("broker:", broker)
	}

	opts := []kgo.Opt{
		// https://pkg.go.dev/github.com/twmb/franz-go/pkg/kgo#DefaultProduceTopic
		kgo.DefaultProduceTopic(*x.config.Topic),
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
		// maxBrokerWriteBytes: 100 << 20, // Kafka socket.request.max.bytes default is 100<<20 = 104857600
		// https://github.com/twmb/franz-go/blob/v1.17.1/pkg/kgo/config.go#L483C3-L483C87
		// https://www.wolframalpha.com/input?i=1+%3C%3C+10
		kgo.BrokerMaxWriteBytes(100 << 18),

		// https://pkg.go.dev/github.com/twmb/franz-go/pkg/kgo#ProducerBatchMaxBytes
		// Copied from the benchmark
		// https://github.com/twmb/franz-go/blob/master/examples/bench/main.go#L104
		kgo.ProducerBatchMaxBytes(1000000),

		kgo.WithLogger(kgo.BasicLogger(os.Stderr, kgo.LogLevelDebug, func() string {
			return time.Now().Format("[2006-01-02 15:04:05.999] ")
		})),
	}
	var err error
	x.kClient, err = kgo.NewClient(opts...)
	if err != nil {
		log.Fatalf("unable to create client:%v", err)
	}

	// https://pkg.go.dev/github.com/twmb/franz-go/pkg/kgo#Client.AllowRebalance
	x.kClient.AllowRebalance()

	errP := x.pingKafkaWithRetries(ctx, kafkaPingRetriesCst, kafkaPingRetrySleepCst)
	if errP != nil {
		log.Fatalf("InitDestKafka pingKafkaWithRetries errP:%v", errP)
	}

}

func (x *XTCP) InitDestNSQ(ctx context.Context) {

	nsqServer := strings.Replace(*x.config.Dest, "nsq:", "", 1)

	if x.debugLevel > 10 {
		log.Printf("config.Topic:%s\n", *x.config.Topic)
		log.Println("config.Dest:", *x.config.Dest)
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

	dest := strings.Replace(*x.config.Dest, "udp:", "", 1)

	if x.debugLevel > 10 {
		log.Printf("config.Topic:%s\n", *x.config.Topic)
		log.Println("config.Dest:", *x.config.Dest)
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

	dest := strings.Replace(*x.config.Dest, "nats:", "", 1)

	if x.debugLevel > 10 {
		log.Printf("config.Topic:%s\n", *x.config.Topic)
		log.Println("config.Dest:", *x.config.Dest)
		log.Println("dest:", dest)
	}

	var err error
	// https://github.com/nats-io/nats.go?tab=readme-ov-file#advanced-usage
	// https://pkg.go.dev/github.com/nats-io/nats.go@v1.37.0#Connect
	//x.natsClient, err = nats.Connect(nats.DefaultURL)

	opts := nats.Options{
		Url:                  dest,
		AllowReconnect:       true,
		MaxReconnect:         natsReconnectsCst,
		ReconnectWait:        2 * time.Second,        // default
		ReconnectJitter:      100 * time.Millisecond, //default
		RetryOnFailedConnect: true,
		Timeout:              natsTimeoutCst,
	}

	x.natsClient, err = opts.Connect()
	if err != nil {
		log.Fatalf("InitDestNATS err:%v", err)
	}
}

// InitDestValKey creates the nats client
// https://redis.uptrace.dev/guide/go-redis.html#installation
// https://github.com/redis/go-redis?tab=readme-ov-file#quickstart
func (x *XTCP) InitDestValKey(ctx context.Context) {

	dest := strings.Replace(*x.config.Dest, "valkey:", "", 1)

	if x.debugLevel > 10 {
		log.Printf("config.Topic:%s\n", *x.config.Topic)
		log.Println("config.Dest:", *x.config.Dest)
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

// destKafka sends the protobuf to kafka
func (x *XTCP) destKafka(ctx context.Context, xtcpRecordBinary *[]byte) (n int, err error) {

	kgoRecord := x.kgoRecordPool.Get().(*kgo.Record)
	// defer x.kgoRecordPool.Put(kgoRecord)

	kgoRecord.Topic = *x.config.Topic
	kgoRecord.Value = *xtcpRecordBinary

	// I don't understand why setting a context with a timeout doesn't work,
	// but it definitely doesn't.  It always says the context is canceled. ?!
	//ctxP, cancelP := context.WithTimeout(ctx, kafkaClientProduceTimeoutCst)
	// defer cancelP()

	kafkaStartTime := time.Now()

	x.kClient.Produce(context.Background(),
		kgoRecord,
		func(kgoRecord *kgo.Record, err error) {
			dur := time.Since(kafkaStartTime)
			x.kgoRecordPool.Put(kgoRecord)
			//cancelP()
			if err != nil {
				x.pH.WithLabelValues("destKafka", "Produce", "error").Observe(dur.Seconds())
				x.pC.WithLabelValues("destKafka", "Produce", "error").Inc()
				if x.debugLevel > 10 {
					log.Printf("destKafka %0.6fs Produce err:%v", dur.Seconds(), err)
				}
				return
			}

			if x.debugLevel > 10 {
				log.Printf("destKafka %0.6fs", dur.Seconds())
			}

			x.pH.WithLabelValues("destKafka", "Produce", "count").Observe(time.Since(kafkaStartTime).Seconds())
			x.pC.WithLabelValues("destKafka", "Produce", "count").Inc()
		},
	)

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

	return 1, err
}

// destNSQ sends the protobuf to a NSQ
// https://nsq.io/
func (x *XTCP) destNSQ(ctx context.Context, xtcpRecordBinary *[]byte) (n int, err error) {

	nsqStartTime := time.Now()
	err = x.nsqProducer.Publish(*x.config.Topic, *xtcpRecordBinary)
	dur := time.Since(nsqStartTime)
	if err != nil {
		x.pH.WithLabelValues("destNSQ", "Publish", "error").Observe(dur.Seconds())
		x.pC.WithLabelValues("destNSQ", "Publish", "error").Inc()
		return 0, err
	}

	if x.debugLevel > 10 {
		log.Printf("destNSQ %0.6fs", dur.Seconds())
	}

	x.pH.WithLabelValues("destNSQ", "Publish", "count").Observe(dur.Seconds())
	x.pC.WithLabelValues("destNSQ", "Publish", "count").Inc()

	return 1, err
}

// destUDP sends the protobuf to the Edgio UDP destination
func (x *XTCP) destUDP(ctx context.Context, xtcpRecordBinary *[]byte) (n int, err error) {

	udpBytesWritten, err := x.udpConn.Write(*xtcpRecordBinary)
	if err != nil {
		x.pC.WithLabelValues("Inetdiager", "udpConn.Write", "error").Inc()
		if x.debugLevel > 100 {
			log.Printf("udpConn.Write(XtcpRecordBinary) err:%v", err)
		}
		return 0, err
	}

	x.pC.WithLabelValues("Inetdiager", "udpWrites", "count").Inc()
	x.pC.WithLabelValues("Inetdiager", "udpWriteBytes", "count").Add(float64(udpBytesWritten))

	return 1, err
}

// destNATS sends the protobuf to the NATS destination
// https://nats.io/
func (x *XTCP) destNATS(ctx context.Context, xtcpRecordBinary *[]byte) (n int, err error) {

	natsStartTime := time.Now()
	err = x.natsClient.Publish(*x.config.Topic, *xtcpRecordBinary)
	dur := time.Since(natsStartTime)
	if err != nil {
		x.pH.WithLabelValues("destNATS", "Publish", "error").Observe(dur.Seconds())
		x.pC.WithLabelValues("destNATS", "Publish", "error").Inc()
		return 0, err
	}

	if x.debugLevel > 10 {
		log.Printf("destNATS %0.6fs", dur.Seconds())
	}

	x.pH.WithLabelValues("destNATS", "Publish", "count").Observe(dur.Seconds())
	x.pC.WithLabelValues("destNATS", "Publish", "count").Inc()

	return 1, err
}

// destValKey sends the protobuf to valkey ( new redis )
// https://valkey.io/
// https://redis.uptrace.dev/guide/go-redis-pubsub.html
func (x *XTCP) destValKey(ctx context.Context, xtcpRecordBinary *[]byte) (n int, err error) {

	valkeyStartTime := time.Now()

	ctxP, cancelP := context.WithTimeout(ctx, valkeyTimeoutCst)
	defer cancelP()

	err = x.valKeyClient.Publish(ctxP, *x.config.Topic, *xtcpRecordBinary).Err()
	dur := time.Since(valkeyStartTime)
	if err != nil {
		x.pH.WithLabelValues("destValKey", "Publish", "error").Observe(dur.Seconds())
		x.pC.WithLabelValues("destValKey", "Publish", "error").Inc()
		return 0, err
	}

	if x.debugLevel > 10 {
		log.Printf("destValKey %0.6fs", dur.Seconds())
	}

	x.pH.WithLabelValues("destValKey", "Publish", "count").Observe(dur.Seconds())
	x.pC.WithLabelValues("destValKey", "Publish", "count").Inc()

	return 1, err
}
