//go:build dest_nsq

package xtcp

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

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

// nsqDest publishes each marshalled record to an NSQ topic.
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

func (d *nsqDest) Send(_ context.Context, b *[]byte) (int, error) {
	start := time.Now()
	err := d.producer.Publish(d.x.config.Topic, *b)
	dur := time.Since(start)
	if err != nil {
		d.x.pH.WithLabelValues("destNSQ", "Publish", "error").Observe(dur.Seconds())
		d.x.pC.WithLabelValues("destNSQ", "Publish", "error").Inc()
		return 0, err
	}
	if d.x.debugLevel > 10 {
		log.Printf("destNSQ %0.6fs", dur.Seconds())
	}
	d.x.pH.WithLabelValues("destNSQ", "Publish", "count").Observe(dur.Seconds())
	d.x.pC.WithLabelValues("destNSQ", "Publish", "count").Inc()
	return 1, nil
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
