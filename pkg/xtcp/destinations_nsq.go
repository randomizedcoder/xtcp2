//go:build dest_nsq

package xtcp

import (
	"context"
	"fmt"
	"log"
	"strings"

	nsq "github.com/nsqio/go-nsq"
)

// nsqProducer captures the surface of *nsq.Producer that nsqDest
// actually calls. Lifting it to an interface lets the destination's
// Send/Close paths run against an in-process fake without a real
// nsqd — see destinations_nsq_test.go. *nsq.Producer satisfies this
// interface via its concrete methods.
type nsqProducer interface {
	Publish(topic string, body []byte) error
	Stop()
}

// nsqDest publishes each marshaled record to an NSQ topic.
type nsqDest struct {
	x        *XTCP
	producer nsqProducer
}

// newNSQProducerFn is the factory tests swap to inject a fake
// nsqProducer without spinning up an nsqd. Production callers leave
// this at the default (newNSQProducerReal).
var newNSQProducerFn = newNSQProducerReal

// newNSQProducerReal is the production factory: nsq.NewProducer is
// lazy (no dial at construction), so this is a pure wrapper.
func newNSQProducerReal(addr string, cfg *nsq.Config) (nsqProducer, error) {
	return nsq.NewProducer(addr, cfg)
}

func newNSQDest(_ context.Context, x *XTCP) (Destination, error) {
	addr := strings.Replace(x.config.Dest, "nsq:", "", 1)
	if x.debugLevel > 10 {
		log.Printf("config.Topic:%s\n", x.config.Topic)
		log.Println("config.Dest:", x.config.Dest)
		log.Println("nsq addr:", addr)
	}
	cfg := nsq.NewConfig()
	producer, err := newNSQProducerFn(addr, cfg)
	if err != nil {
		return nil, fmt.Errorf("newNSQDest nsq.NewProducer: %w", err)
	}
	return &nsqDest{x: x, producer: producer}, nil
}

// Send hands the record to sendViaPublisher, which owns the timing, metrics
// and debug-log boilerplate shared with the nats and valkey sinks. The
// timeout is 0 because nsq.Producer.Publish takes no context — it blocks on
// the producer's own response channel and is bounded by the nsq.Config
// write/dial timeouts, not by ours.
func (d *nsqDest) Send(ctx context.Context, b *[]byte) (int, error) {
	return sendViaPublisher(ctx, d.x, "destNSQ", 0,
		func(_ context.Context, topic string, body []byte) error {
			return d.producer.Publish(topic, body)
		}, b)
}

func (d *nsqDest) Close() error {
	if d.producer != nil {
		d.producer.Stop()
	}
	return nil
}

func init() {
	RegisterDestination("nsq", newNSQDest)
	RegisterLibraryDefaultDest("nsq", "nsq:nsqd:4150")
}
