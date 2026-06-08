//go:build dest_nats

package xtcp

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	nats "github.com/nats-io/nats.go"
)

const (
	natsReconnectsCst = 5
	natsTimeoutCst    = 1 * time.Second
)

// natsDest publishes each marshalled record to a NATS subject.
type natsDest struct {
	x      *XTCP
	client *nats.Conn
}

func newNATSDest(_ context.Context, x *XTCP) (Destination, error) {
	addr := strings.Replace(x.config.Dest, "nats:", "", 1)
	if x.debugLevel > 10 {
		log.Println("config.Topic:", x.config.Topic)
		log.Println("config.Dest:", x.config.Dest)
		log.Println("nats addr:", addr)
	}
	opts := nats.Options{
		Url:                  addr,
		AllowReconnect:       true,
		MaxReconnect:         natsReconnectsCst,
		ReconnectWait:        2 * time.Second,
		ReconnectJitter:      100 * time.Millisecond,
		RetryOnFailedConnect: true,
		Timeout:              natsTimeoutCst,
	}
	client, err := opts.Connect()
	if err != nil {
		return nil, fmt.Errorf("newNATSDest opts.Connect: %w", err)
	}
	return &natsDest{x: x, client: client}, nil
}

func (d *natsDest) Send(_ context.Context, b *[]byte) (int, error) {
	start := time.Now()
	err := d.client.Publish(d.x.config.Topic, *b)
	dur := time.Since(start)
	if err != nil {
		d.x.pH.WithLabelValues("destNATS", "Publish", "error").Observe(dur.Seconds())
		d.x.pC.WithLabelValues("destNATS", "Publish", "error").Inc()
		return 0, err
	}
	if d.x.debugLevel > 10 {
		log.Printf("destNATS %0.6fs", dur.Seconds())
	}
	d.x.pH.WithLabelValues("destNATS", "Publish", "count").Observe(dur.Seconds())
	d.x.pC.WithLabelValues("destNATS", "Publish", "count").Inc()
	return 1, nil
}

func (d *natsDest) Close() error {
	if d.client != nil {
		d.client.Close()
	}
	return nil
}

func init() {
	RegisterDestination("nats", newNATSDest)
}
