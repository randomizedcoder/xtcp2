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

// nsqDest publishes each marshalled record to an NSQ topic.
type nsqDest struct {
	x        *XTCP
	producer *nsq.Producer
}

func newNSQDest(_ context.Context, x *XTCP) (Destination, error) {
	addr := strings.Replace(x.config.Dest, "nsq:", "", 1)
	if x.debugLevel > 10 {
		log.Printf("config.Topic:%s\n", x.config.Topic)
		log.Println("config.Dest:", x.config.Dest)
		log.Println("nsq addr:", addr)
	}
	cfg := nsq.NewConfig()
	producer, err := nsq.NewProducer(addr, cfg)
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
}
