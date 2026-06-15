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

// valkeyPublisher is the surface valkeyDest needs from a Valkey/Redis
// client: an error-returning Publish, Ping, and Close. *redis.Client
// returns *redis.IntCmd / *redis.StatusCmd chains; we adapt those to
// flat error returns via redisClientAdapter below so tests can mock
// the whole thing without standing up a real Valkey server.
type valkeyPublisher interface {
	Publish(ctx context.Context, channel string, msg []byte) error
	Ping(ctx context.Context) error
	Close() error
}

// redisClientAdapter wraps a *redis.Client so it satisfies
// valkeyPublisher. Production-only; the test fake bypasses this.
type redisClientAdapter struct {
	c *redis.Client
}

func (a *redisClientAdapter) Publish(ctx context.Context, channel string, msg []byte) error {
	return a.c.Publish(ctx, channel, msg).Err()
}

func (a *redisClientAdapter) Ping(ctx context.Context) error {
	_, err := a.c.Ping(ctx).Result()
	return err
}

func (a *redisClientAdapter) Close() error { return a.c.Close() }

// valkeyDest publishes each marshalled record to a Valkey (Redis-protocol)
// pub/sub channel.
type valkeyDest struct {
	x      *XTCP
	client valkeyPublisher
}

// newValkeyClientFn is the factory tests swap to inject a fake
// valkeyPublisher without spinning up a real *redis.Client. Production
// callers leave this at the default (newValkeyClientReal).
var newValkeyClientFn = newValkeyClientReal

// newValkeyClientReal is the production factory: builds a real
// *redis.Client wrapped in redisClientAdapter so it satisfies
// valkeyPublisher.
func newValkeyClientReal(addr string) valkeyPublisher {
	return &redisClientAdapter{c: redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     "",
		DB:           0,
		MaxIdleConns: valkeyMaxIdleConnsCst,
	})}
}

func newValKeyDest(ctx context.Context, x *XTCP) (Destination, error) {
	addr := strings.Replace(x.config.Dest, "valkey:", "", 1)
	if x.debugLevel > 10 {
		log.Printf("config.Topic:%s\n", x.config.Topic)
		log.Println("config.Dest:", x.config.Dest)
		log.Println("valkey addr:", addr)
	}
	client := newValkeyClientFn(addr)

	pCtx, cancel := context.WithTimeout(ctx, valkeyPingTimeoutCst)
	defer cancel()
	start := time.Now()
	if err := client.Ping(pCtx); err != nil {
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
	err := d.client.Publish(pCtx, d.x.config.Topic, *b)
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
