package main

import (
	"log"
	"net"
	"sync/atomic"
	"time"
)

// ipRejectLogEvery bounds the log of connections a loopback listener closes,
// so a peer retrying in a loop cannot flood the log.
const ipRejectLogEvery = time.Minute

// rateLimitedRejectLog logs a connection a loopback listener closed because
// its peer is not loopback, at most once per every, with the count closed
// since the last line, and the close error of the connection that line
// reports, if any. name is the listener's.
func rateLimitedRejectLog(name string, every time.Duration) func(net.Addr, error) {
	var last atomic.Int64
	var count atomic.Int64
	return func(a net.Addr, closeErr error) {
		n := count.Add(1)
		now := time.Now().UnixNano()
		prev := last.Load()
		if now-prev < int64(every) || !last.CompareAndSwap(prev, now) {
			return
		}
		count.Add(-n)
		if closeErr != nil {
			log.Printf("listener %q: closed %d connection(s) from non-loopback peers, last from %s: %v", name, n, a, closeErr)
			return
		}
		log.Printf("listener %q: closed %d connection(s) from non-loopback peers, last from %s", name, n, a)
	}
}
