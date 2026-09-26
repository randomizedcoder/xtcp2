package xtcp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// destinations_publisher_test.go exercises sendViaPublisher, the untagged
// shared Send body behind the nats, nsq and valkey sinks.
//
// This file carries NO build tag on purpose. sendViaPublisher references no
// broker client type, so it is compiled into every flavor — including the
// default build with no dest_* tags, where the three tagged destinations
// themselves are absent. Testing it here means the shared body has coverage
// even in the builds that cannot exercise it end to end, and it is the only
// place the label/metric contract is asserted directly rather than through a
// flavor.
//
// The per-flavor tests (destinations_{nats,nsq,valkey}_test.go) still assert
// their own Send behavior against their own fakes. Those are the tests that
// prove the delegation is wired up; this one proves the thing being delegated
// to is correct.

// blockUntilCtxDone is the boundary-case publisher: it ignores the payload
// and simply waits for the caller's context to be canceled, returning
// whatever the context failed with. A helper rather than an inline closure so
// the timeout row reads as a deliberate case.
func blockUntilCtxDone(ctx context.Context, _ string, _ []byte) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestSendViaPublisher_table(t *testing.T) {
	t.Parallel()

	sentinelErr := errors.New("broker refused the publish")

	cases := []struct {
		description string
		category    string
		label       string
		topic       string
		payload     []byte
		timeout     time.Duration
		pub         publishFn

		// expected outcome
		wantN          int
		wantErr        error // errors.Is target; nil means no error expected
		wantOKCounter  float64
		wantErrCounter float64
		wantPublished  bool   // publisher was actually invoked
		wantTopic      string // topic the publisher was handed
		wantDeadline   bool   // publisher saw a context with a deadline
	}{
		{
			description: "positive: publish succeeds, returns one record and bumps the count metric",
			category:    "positive",
			label:       "destNATS",
			topic:       "xtcp-test",
			payload:     []byte("payload"),
			timeout:     0,
			pub:         func(context.Context, string, []byte) error { return nil },

			wantN:          1,
			wantErr:        nil,
			wantOKCounter:  1,
			wantErrCounter: 0,
			wantPublished:  true,
			wantTopic:      "xtcp-test",
			wantDeadline:   false,
		},
		{
			description: "negative: publisher error is returned verbatim and bumps the error metric only",
			category:    "negative",
			label:       "destNSQ",
			topic:       "xtcp-test",
			payload:     []byte("payload"),
			timeout:     0,
			pub:         func(context.Context, string, []byte) error { return sentinelErr },

			wantN:          0,
			wantErr:        sentinelErr,
			wantOKCounter:  0,
			wantErrCounter: 1,
			wantPublished:  true,
			wantTopic:      "xtcp-test",
			wantDeadline:   false,
		},
		{
			description: "boundary: a non-zero timeout reaches the publisher as a real deadline and bounds a blocked publish",
			category:    "boundary",
			label:       "destValKey",
			topic:       "xtcp-test",
			payload:     []byte("payload"),
			timeout:     20 * time.Millisecond,
			pub:         blockUntilCtxDone,

			wantN:          0,
			wantErr:        context.DeadlineExceeded,
			wantOKCounter:  0,
			wantErrCounter: 1,
			wantPublished:  true,
			wantTopic:      "xtcp-test",
			wantDeadline:   true,
		},
		{
			description: "boundary: timeout of zero leaves the caller's context untouched, no deadline is imposed",
			category:    "boundary",
			label:       "destNATS",
			topic:       "xtcp-test",
			payload:     []byte("payload"),
			timeout:     0,
			pub:         func(context.Context, string, []byte) error { return nil },

			wantN:          1,
			wantErr:        nil,
			wantOKCounter:  1,
			wantErrCounter: 0,
			wantPublished:  true,
			wantTopic:      "xtcp-test",
			wantDeadline:   false,
		},
		{
			description: "corner: an empty payload is still published and still counted, not skipped",
			category:    "corner",
			label:       "destNSQ",
			topic:       "xtcp-test",
			payload:     []byte{},
			timeout:     0,
			pub:         func(context.Context, string, []byte) error { return nil },

			wantN:          1,
			wantErr:        nil,
			wantOKCounter:  1,
			wantErrCounter: 0,
			wantPublished:  true,
			wantTopic:      "xtcp-test",
			wantDeadline:   false,
		},
		{
			description: "corner: an empty topic is forwarded as-is; sendViaPublisher does not validate config",
			category:    "corner",
			label:       "destValKey",
			topic:       "",
			payload:     []byte("payload"),
			timeout:     0,
			pub:         func(context.Context, string, []byte) error { return nil },

			wantN:          1,
			wantErr:        nil,
			wantOKCounter:  1,
			wantErrCounter: 0,
			wantPublished:  true,
			wantTopic:      "",
			wantDeadline:   false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.label, func(t *testing.T) {
			t.Parallel()

			x := newTestXTCP(t, "null:")
			x.config.Topic = tc.topic

			var (
				published   bool
				gotTopic    string
				gotBody     []byte
				gotDeadline bool
			)
			pub := func(ctx context.Context, topic string, body []byte) error {
				published = true
				gotTopic = topic
				gotBody = body
				_, gotDeadline = ctx.Deadline()
				return tc.pub(ctx, topic, body)
			}

			payload := tc.payload
			n, err := sendViaPublisher(context.Background(), x, tc.label, tc.timeout, pub, &payload)

			if n != tc.wantN {
				t.Errorf("%s: n = %d, want %d", tc.description, n, tc.wantN)
			}
			switch {
			case tc.wantErr == nil && err != nil:
				t.Errorf("%s: err = %v, want nil", tc.description, err)
			case tc.wantErr != nil && !errors.Is(err, tc.wantErr):
				t.Errorf("%s: err = %v, want %v", tc.description, err, tc.wantErr)
			}
			if published != tc.wantPublished {
				t.Errorf("%s: publisher invoked = %v, want %v", tc.description, published, tc.wantPublished)
			}
			if gotTopic != tc.wantTopic {
				t.Errorf("%s: topic = %q, want %q", tc.description, gotTopic, tc.wantTopic)
			}
			if string(gotBody) != string(tc.payload) {
				t.Errorf("%s: body = %q, want %q", tc.description, gotBody, tc.payload)
			}
			if gotDeadline != tc.wantDeadline {
				t.Errorf("%s: publisher saw ctx deadline = %v, want %v",
					tc.description, gotDeadline, tc.wantDeadline)
			}

			// The label is scrape-visible: a helper that silently relabeled
			// would break dashboards without failing any build. Assert against
			// the exact label the flavor passes in.
			gotOK := testutil.ToFloat64(x.pC.WithLabelValues(tc.label, "Publish", "count"))
			gotErrC := testutil.ToFloat64(x.pC.WithLabelValues(tc.label, "Publish", "error"))
			if gotOK != tc.wantOKCounter {
				t.Errorf("%s: %s/count counter = %v, want %v",
					tc.description, tc.label, gotOK, tc.wantOKCounter)
			}
			if gotErrC != tc.wantErrCounter {
				t.Errorf("%s: %s/error counter = %v, want %v",
					tc.description, tc.label, gotErrC, tc.wantErrCounter)
			}
		})
	}
}

// TestSendViaPublisher_callerCtxCancelPropagates pins the other half of the
// context contract: with timeout 0 the caller's own context is passed through
// untouched, so a cancellation upstream still reaches the publisher. Without
// this, "timeout == 0 means no deadline" could be implemented as
// context.Background() and no table row above would notice.
func TestSendViaPublisher_callerCtxCancelPropagates(t *testing.T) {
	t.Parallel()

	x := newTestXTCP(t, "null:")
	x.config.Topic = "xtcp-test"

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	payload := []byte("payload")
	n, err := sendViaPublisher(ctx, x, "destNATS", 0, blockUntilCtxDone, &payload)
	if n != 0 {
		t.Errorf("n = %d, want 0 on a canceled caller context", n)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if got := testutil.ToFloat64(x.pC.WithLabelValues("destNATS", "Publish", "error")); got != 1 {
		t.Errorf("error counter = %v, want 1", got)
	}
}

// TestSendViaPublisher_debugLog covers the debugLevel>10 branch, which is
// otherwise only reached in the per-flavor debug tests.
func TestSendViaPublisher_debugLog(t *testing.T) {
	t.Parallel()

	x := newTestXTCP(t, "null:")
	x.config.Topic = "xtcp-test"
	x.debugLevel = 11

	payload := []byte("x")
	n, err := sendViaPublisher(context.Background(), x, "destNATS", 0,
		func(context.Context, string, []byte) error { return nil }, &payload)
	if n != 1 || err != nil {
		t.Errorf("n, err = %d, %v; want 1, nil", n, err)
	}
}

func BenchmarkSendViaPublisher(b *testing.B) {
	x := newTestXTCP(&testing.T{}, "null:")
	x.config.Topic = "xtcp-bench"
	pub := func(context.Context, string, []byte) error { return nil }
	payload := []byte("payload")
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = sendViaPublisher(ctx, x, "destNATS", 0, pub, &payload)
	}
}
