package xtcp

import (
	"context"
	"log"
	"time"
)

// publishFn is the one operation the broker destinations have in common:
// hand a marshaled record to a named topic/subject and report whether the
// client accepted it.
//
// The ctx argument is here for the clients that honor a deadline (valkey);
// nats and nsq buffer internally and publish asynchronously, so their
// closures ignore it. Keeping ctx in the signature lets the shared Send body
// below own the deadline logic instead of each flavor repeating it.
type publishFn func(ctx context.Context, topic string, body []byte) error

// sendViaPublisher is the shared Destination.Send body for every
// publish-to-a-topic sink — nats, nsq and valkey. It stands in the same
// relation to those three as writerDest (destinations_stdout.go) does to
// stdout/stderr/file: one copy of the timing, metrics and debug-log
// boilerplate, parameterised by a label, rather than a near-identical copy
// per flavor. The three bodies were identical enough that dupl flagged two
// of them.
//
// This file is deliberately UNTAGGED and imports only stdlib. The broker
// client libraries stay behind their dest_* build tags, reached through the
// per-flavor interfaces (natsPublisher, nsqProducer, valkeyPublisher), so a
// flavor build still links only the one library it uses.
//
// label is the metric prefix the flavor already publishes under ("destNATS",
// "destNSQ", "destValKey"). It is scrape-visible, so it must keep matching
// what the flavor emitted before.
//
// timeout of 0 means no per-send deadline: the client is fire-and-forget and
// wrapping ctx would only allocate. A non-zero timeout bounds the single
// publish call, which is what the valkey path needs.
//
// Returns (1, nil) on success — the count is records, not bytes, which is
// what the Destination contract expects from a publish sink.
func sendViaPublisher(
	ctx context.Context,
	x *XTCP,
	label string,
	timeout time.Duration,
	pub publishFn,
	b *[]byte,
) (int, error) {
	start := time.Now()

	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	err := pub(ctx, x.config.Topic, *b)
	dur := time.Since(start)
	if err != nil {
		x.pH.WithLabelValues(label, "Publish", "error").Observe(dur.Seconds())
		x.pC.WithLabelValues(label, "Publish", "error").Inc()
		return 0, err
	}
	if x.debugLevel > 10 {
		log.Printf("%s %0.6fs", label, dur.Seconds())
	}
	x.pH.WithLabelValues(label, "Publish", "count").Observe(dur.Seconds())
	x.pC.WithLabelValues(label, "Publish", "count").Inc()
	return 1, nil
}
