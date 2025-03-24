package xtcp

import (
	"context"
	"log"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
	"github.com/twmb/franz-go/pkg/kgo"
)

// destNull sends the protobuf to nowhere!
func (x *XTCP) destNull(_ context.Context, xtcpRecordBinary *[]byte) (n int, err error) {

	x.pC.WithLabelValues("destNull", "start", "count").Inc()

	return len(*xtcpRecordBinary), nil
}

// destKafkaProto sends the protobuf in protobuf format to kafka
// this is to test the serdes method of franz-go
func (x *XTCP) destKafkaProto(ctx context.Context, e *xtcp_flat_record.Envelope) (n int, err error) {

	kgoRecord := x.kgoRecordPool.Get().(*kgo.Record)
	// defer x.kgoRecordPool.Put(kgoRecord)

	kgoRecord.Topic = "serdeTest"
	kgoRecord.Value = x.kSerde.MustEncode(e)
	//kgoRecord.Value = x.kSerde.MustEncode(*xtcpRecordBinary)
	len := len(kgoRecord.Value)

	var (
		ctxP    context.Context
		cancelP context.CancelFunc
	)
	if x.config.KafkaProduceTimeout.AsDuration() != 0 {
		// I don't understand why setting a context with a timeout doesn't work,
		// but it definitely doesn't.  It always says the context is canceled. ?!
		ctxP, cancelP = context.WithTimeout(ctx, x.config.KafkaProduceTimeout.AsDuration())
		defer cancelP()
	}
	// https://pkg.go.dev/google.golang.org/protobuf/types/known/durationpb

	kafkaStartTime := time.Now()

	x.kClient.Produce(
		ctxP,
		kgoRecord,
		func(kgoRecord *kgo.Record, err error) {
			dur := time.Since(kafkaStartTime)

			x.kgoRecordPool.Put(kgoRecord)

			//cancelP()
			if err != nil {
				x.pH.WithLabelValues("destKafkaProto", "Produce", "error").Observe(dur.Seconds())
				x.pC.WithLabelValues("destKafkaProto", "Produce", "error").Inc()
				if x.debugLevel > 10 {
					log.Printf("destKafkaProto %0.6fs Produce err:%v", dur.Seconds(), err)
				}
				return
			}

			x.pH.WithLabelValues("destKafkaProto", "Produce", "count").Observe(dur.Seconds())
			x.pC.WithLabelValues("destKafkaProto", "Produce", "count").Inc()

			if x.debugLevel > 10 {
				log.Printf("destKafkaProto len:%d %0.6fs %dms", len, dur.Seconds(), dur.Milliseconds())
			}
		},
	)

	return 1, err
}

// destKafka sends the protobuf to kafka
func (x *XTCP) destKafka(ctx context.Context, xtcpRecordBinary *[]byte) (n int, err error) {

	if x.debugLevel > 10 {
		log.Printf("destKafka header bytes: % X", (*xtcpRecordBinary)[:KafkaHeaderSizeCst])
	}

	kgoRecord := x.kgoRecordPool.Get().(*kgo.Record)
	// defer x.kgoRecordPool.Put(kgoRecord)

	kgoRecord.Topic = x.config.Topic
	kgoRecord.Value = *xtcpRecordBinary
	len := len(kgoRecord.Value)

	var (
		ctxP    context.Context
		cancelP context.CancelFunc
	)
	if x.config.KafkaProduceTimeout.AsDuration() != 0 {
		// I don't understand why setting a context with a timeout doesn't work,
		// but it definitely doesn't.  It always says the context is canceled. ?!
		ctxP, cancelP = context.WithTimeout(ctx, x.config.KafkaProduceTimeout.AsDuration())
		defer cancelP()
	}
	// https://pkg.go.dev/google.golang.org/protobuf/types/known/durationpb

	kafkaStartTime := time.Now()

	x.kClient.Produce(
		ctxP,
		kgoRecord,
		func(kgoRecord *kgo.Record, err error) {
			dur := time.Since(kafkaStartTime)

			x.kgoRecordPool.Put(kgoRecord)
			*xtcpRecordBinary = (*xtcpRecordBinary)[:0]
			x.destBytesPool.Put(xtcpRecordBinary)

			//cancelP()
			if err != nil {
				x.pH.WithLabelValues("destKafka", "Produce", "error").Observe(dur.Seconds())
				x.pC.WithLabelValues("destKafka", "Produce", "error").Inc()
				if x.debugLevel > 10 {
					log.Printf("destKafka %0.6fs Produce err:%v", dur.Seconds(), err)
				}
				return
			}

			x.pH.WithLabelValues("destKafka", "Produce", "count").Observe(dur.Seconds())
			x.pC.WithLabelValues("destKafka", "Produce", "count").Inc()

			if x.debugLevel > 10 {
				log.Printf("destKafka len:%d %0.6fs %dms", len, dur.Seconds(), dur.Milliseconds())
			}
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
func (x *XTCP) destNSQ(_ context.Context, xtcpRecordBinary *[]byte) (n int, err error) {

	nsqStartTime := time.Now()
	err = x.nsqProducer.Publish(x.config.Topic, *xtcpRecordBinary)
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
func (x *XTCP) destUDP(_ context.Context, xtcpRecordBinary *[]byte) (n int, err error) {

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
func (x *XTCP) destNATS(_ context.Context, xtcpRecordBinary *[]byte) (n int, err error) {

	natsStartTime := time.Now()
	err = x.natsClient.Publish(x.config.Topic, *xtcpRecordBinary)
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

	err = x.valKeyClient.Publish(ctxP, x.config.Topic, *xtcpRecordBinary).Err()
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
