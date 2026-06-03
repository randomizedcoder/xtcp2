package xtcp

import (
	"context"
	"encoding/binary"
	"log"
	"net"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

// Destination functions are invoked from a single deserializer goroutine in
// production. The implementations below assume serial access to their
// underlying net.Conn / client field. Concurrent callers are not supported
// without adding a mutex.

// destNull sends the protobuf to nowhere!
func (x *XTCP) destNull(_ context.Context, xtcpRecordBinary *[]byte) (n int, err error) {

	x.pC.WithLabelValues("destNull", "start", "count").Inc()

	return len(*xtcpRecordBinary), nil
}

// // destKafkaProto sends the protobuf in protobuf format to kafka
// // this is to test the serdes method of franz-go
// func (x *XTCP) destKafkaProto(ctx context.Context, e *xtcp_flat_record.Envelope) (n int, err error) {

// 	kgoRecord := x.kgoRecordPool.Get().(*kgo.Record)
// 	// defer x.kgoRecordPool.Put(kgoRecord)

// 	kgoRecord.Topic = "serdeTest"
// 	kgoRecord.Value = x.kSerde.MustEncode(e)
// 	//kgoRecord.Value = x.kSerde.MustEncode(*xtcpRecordBinary)
// 	len := len(kgoRecord.Value)

// 	var (
// 		ctxP    context.Context
// 		cancelP context.CancelFunc
// 	)
// 	if x.config.KafkaProduceTimeout.AsDuration() != 0 {
// 		// I don't understand why setting a context with a timeout doesn't work,
// 		// but it definitely doesn't.  It always says the context is canceled. ?!
// 		ctxP, cancelP = context.WithTimeout(ctx, x.config.KafkaProduceTimeout.AsDuration())
// 		defer cancelP()
// 	}
// 	// https://pkg.go.dev/google.golang.org/protobuf/types/known/durationpb

// 	kafkaStartTime := time.Now()

// 	x.kClient.Produce(
// 		ctxP,
// 		kgoRecord,
// 		func(kgoRecord *kgo.Record, err error) {
// 			dur := time.Since(kafkaStartTime)

// 			x.kgoRecordPool.Put(kgoRecord)

// 			//cancelP()
// 			if err != nil {
// 				x.pH.WithLabelValues("destKafkaProto", "Produce", "error").Observe(dur.Seconds())
// 				x.pC.WithLabelValues("destKafkaProto", "Produce", "error").Inc()
// 				if x.debugLevel > 10 {
// 					log.Printf("destKafkaProto %0.6fs Produce err:%v", dur.Seconds(), err)
// 				}
// 				return
// 			}

// 			x.pH.WithLabelValues("destKafkaProto", "Produce", "count").Observe(dur.Seconds())
// 			x.pC.WithLabelValues("destKafkaProto", "Produce", "count").Inc()

// 			if x.debugLevel > 10 {
// 				log.Printf("destKafkaProto len:%d %0.6fs %dms", len, dur.Seconds(), dur.Milliseconds())
// 			}
// 		},
// 	)

// 	return 1, err
// }

// destKafka sends the protobuf to kafka
func (x *XTCP) destKafka(ctx context.Context, xtcpRecordBinary *[]byte) (n int, err error) {

	// if x.debugLevel > 10 {
	// 	log.Printf("destKafka header bytes: % X", (*xtcpRecordBinary)[:KafkaHeaderSizeCst])
	// }

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
		ctxP, cancelP = context.WithTimeout(ctx, x.config.KafkaProduceTimeout.AsDuration())
	}
	// https://pkg.go.dev/google.golang.org/protobuf/types/known/durationpb

	// if x.debugLevel > 10 {
	// 	if deadline, ok := ctxP.Deadline(); ok {
	// 		log.Printf("destKafka, ctxP.Deadline():%s, until:%0.3f", deadline.String(), time.Until(deadline).Seconds())
	// 	} else {
	// 		log.Printf("destKafka, ctxP.Deadline() is none")
	// 	}
	// }

	kafkaStartTime := time.Now()

	x.kClient.Produce(
		ctxP,
		kgoRecord,
		func(kgoRecord *kgo.Record, err error) {
			dur := time.Since(kafkaStartTime)

			x.kgoRecordPool.Put(kgoRecord)
			*xtcpRecordBinary = (*xtcpRecordBinary)[:0]
			x.destBytesPool.Put(xtcpRecordBinary)

			if err != nil {
				x.pH.WithLabelValues("destKafka", "Produce", "error").Observe(dur.Seconds())
				x.pC.WithLabelValues("destKafka", "Produce", "error").Inc()
				if x.debugLevel > 10 {
					log.Printf("destKafka %0.6fs Produce err:%v", dur.Seconds(), err)
				}

				// if x.debugLevel > 10 {
				// 	if deadline, ok := ctxP.Deadline(); ok {
				// 		log.Printf("destKafka, Produce ctxP.Deadline():%s, until:%0.3f", deadline.String(), time.Until(deadline).Seconds())
				// 	} else {
				// 		log.Printf("destKafka, Produce ctxP.Deadline() is none")
				// 	}
				// }

				cancelP()
				return
			}

			x.pH.WithLabelValues("destKafka", "Produce", "count").Observe(dur.Seconds())
			x.pC.WithLabelValues("destKafka", "Produce", "count").Inc()

			if x.debugLevel > 10 {
				log.Printf("destKafka len:%d %0.6fs %dms", len, dur.Seconds(), dur.Milliseconds())
			}
		},
	)

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

// destUnixGram sends the protobuf record to a Unix datagram socket.
// One Write == one datagram == one record; no framing is required because
// the kernel preserves message boundaries. Records exceeding SO_SNDBUF
// (≈208 KB on Linux by default) fail with EMSGSIZE; xtcp records today
// are well below that.
//
// TODO: reconnect on persistent write failure (currently dial-once, fail-
// loudly at startup; runtime errors are logged and the next record is
// attempted).
func (x *XTCP) destUnixGram(_ context.Context, xtcpRecordBinary *[]byte) (n int, err error) {

	written, err := x.unixGramConn.Write(*xtcpRecordBinary)
	if err != nil {
		x.pC.WithLabelValues("destUnixGram", "Write", "error").Inc()
		if x.debugLevel > 100 {
			log.Printf("destUnixGram Write err:%v", err)
		}
		return 0, err
	}

	x.pC.WithLabelValues("destUnixGram", "Writes", "count").Inc()
	x.pC.WithLabelValues("destUnixGram", "WriteBytes", "count").Add(float64(written))

	return 1, nil
}

// destUnix sends the protobuf record to a Unix stream socket, framed with
// a varint length prefix so the daemon reader can recover record
// boundaries. Wire format per record:
//
//	[varint(len(payload))] [payload bytes...]
//
// Daemon-side: read the varint via binary.ReadUvarint, then exactly that
// many payload bytes via io.ReadFull.
//
// Header and payload are written through a net.Buffers, which the standard
// library lowers to a single writev(2) on a *net.UnixConn. That keeps the
// frame atomic on the wire: a partial-write failure can't leave a varint
// header on the receiver without its payload, which would otherwise wedge
// the receiver's binary.ReadUvarint + io.ReadFull recovery loop.
//
// TODO: reconnect on persistent write failure (currently dial-once, fail-
// loudly at startup; runtime errors are logged and the next record is
// attempted).
func (x *XTCP) destUnix(_ context.Context, xtcpRecordBinary *[]byte) (n int, err error) {

	var hdr [binary.MaxVarintLen64]byte
	hdrLen := binary.PutUvarint(hdr[:], uint64(len(*xtcpRecordBinary)))

	bufs := net.Buffers{hdr[:hdrLen], *xtcpRecordBinary}
	written, err := bufs.WriteTo(x.unixConn)
	if err != nil {
		x.pC.WithLabelValues("destUnix", "Write", "error").Inc()
		if x.debugLevel > 100 {
			log.Printf("destUnix WriteTo err:%v written:%d", err, written)
		}
		return 0, err
	}

	x.pC.WithLabelValues("destUnix", "Writes", "count").Inc()
	x.pC.WithLabelValues("destUnix", "WriteBytes", "count").Add(float64(written))

	return 1, nil
}
