//go:build dest_valkey

package xtcp

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	redis "github.com/redis/go-redis/v9"
)

const (
	valkeyPingTimeoutCst  = 2 * time.Second
	valkeyMaxIdleConnsCst = 20
	valkeyTimeoutCst      = 1 * time.Second
)

// valkeyDest publishes each marshalled record to a Valkey (Redis-protocol)
// pub/sub channel.
type valkeyDest struct {
	x      *XTCP
	client *redis.Client
}

func newValKeyDest(ctx context.Context, x *XTCP) (Destination, error) {
	addr := strings.Replace(x.config.Dest, "valkey:", "", 1)
	if x.debugLevel > 10 {
		log.Printf("config.Topic:%s\n", x.config.Topic)
		log.Println("config.Dest:", x.config.Dest)
		log.Println("valkey addr:", addr)
	}
	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     "",
		DB:           0,
		MaxIdleConns: valkeyMaxIdleConnsCst,
	})

	pCtx, cancel := context.WithTimeout(ctx, valkeyPingTimeoutCst)
	defer cancel()
	start := time.Now()
	if _, err := client.Ping(pCtx).Result(); err != nil {
		return nil, fmt.Errorf("newValKeyDest ping (%0.6fs): %w",
			time.Since(start).Seconds(), err)
	}
	if x.debugLevel > 10 {
		log.Printf("newValKeyDest ping time:%0.3fs", time.Since(start).Seconds())
	}
	return &valkeyDest{x: x, client: client}, nil
}

func (d *valkeyDest) Send(ctx context.Context, b *[]byte) (int, error) {
	start := time.Now()
	pCtx, cancel := context.WithTimeout(ctx, valkeyTimeoutCst)
	defer cancel()
	err := d.client.Publish(pCtx, d.x.config.Topic, *b).Err()
	dur := time.Since(start)
	if err != nil {
		d.x.pH.WithLabelValues("destValKey", "Publish", "error").Observe(dur.Seconds())
		d.x.pC.WithLabelValues("destValKey", "Publish", "error").Inc()
		return 0, err
	}
	if d.x.debugLevel > 10 {
		log.Printf("destValKey %0.6fs", dur.Seconds())
	}
	d.x.pH.WithLabelValues("destValKey", "Publish", "count").Observe(dur.Seconds())
	d.x.pC.WithLabelValues("destValKey", "Publish", "count").Inc()
	return 1, nil
}

func (d *valkeyDest) Close() error {
	if d.client != nil {
		return d.client.Close()
	}
	return nil
}

func init() {
	RegisterDestination("valkey", newValKeyDest)
}
