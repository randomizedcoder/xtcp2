package linuxio

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/netlink"
	"golang.org/x/sys/unix"
)

func respondingSocket() *scriptSocket {
	s := &scriptSocket{}
	s.onSend = func(b []byte) {
		seq := binary.LittleEndian.Uint32(b[8:])
		s.batches = []batch{{data: [][]byte{frame(unix.RTM_NEWLINK, 0, seq, word(7))}}}
	}
	return s
}

func TestClientRecoveryAndSequence(t *testing.T) {
	rows := []struct {
		name, description string
		sequence          uint32
		reset             bool
		failure           error
		wantEpoch         uint64
		wantSeq           uint32
	}{
		{"reuse", "healthy socket increments its sequence", 1, false, nil, 1, 2},
		{"last_sequence", "maximum sequence is issued once", math.MaxUint32 - 1, false, nil, 1, math.MaxUint32},
		{"wrap", "wrap replaces socket before sequence one is reused", math.MaxUint32, false, nil, 2, 1},
		{"reset", "external recovery opens a new epoch", 1, true, nil, 2, 1},
		{"timeout", "timeout retires ownership before next request", 1, false, context.DeadlineExceeded, 2, 1},
		{"send_failure", "send failure retires ownership too", 1, false, unix.EPIPE, 2, 1},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Log(row.description)
			first, second := respondingSocket(), respondingSocket()
			c := scriptedClient(unix.NETLINK_ROUTE, first, second)
			t.Cleanup(func() {
				if err := c.Close(); err != nil {
					t.Error(err)
				}
			})
			if _, err := execute(context.Background(), c, numberRequest(false, false)); err != nil {
				t.Fatal(err)
			}
			c.sequence = row.sequence
			if row.reset {
				if err := c.Reset(); err != nil {
					t.Fatal(err)
				}
			}
			if row.failure != nil {
				if errors.Is(row.failure, unix.EPIPE) {
					first.sendErr = row.failure
				} else {
					first.onSend = nil
					first.batches = nil
				}
				if _, err := execute(context.Background(), c, numberRequest(false, false)); !errors.Is(err, row.failure) {
					t.Fatalf("error=%v", err)
				}
			}
			result, err := execute(context.Background(), c, numberRequest(false, false))
			if err != nil || result.Epoch != row.wantEpoch || result.Sequence != row.wantSeq || len(result.Values) != 1 {
				t.Fatalf("result=%+v error=%v", result, err)
			}
			if row.wantEpoch == 2 && first.closes != 1 {
				t.Fatalf("old socket closed %d times", first.closes)
			}
		})
	}
}

func TestClientRejectsLateReplies(t *testing.T) {
	s := respondingSocket()
	c := scriptedClient(unix.NETLINK_ROUTE, s)
	defer c.Close()
	if _, err := execute(context.Background(), c, numberRequest(false, false)); err != nil {
		t.Fatal(err)
	}
	s.onSend = func([]byte) {
		s.batches = []batch{{data: [][]byte{
			frame(unix.RTM_NEWLINK, 0, 1, word(999)),
			frame(unix.RTM_NEWLINK, 0, 2, word(7)),
		}}}
	}
	got, err := execute(context.Background(), c, numberRequest(false, false))
	if err != nil || len(got.Values) != 1 || got.Values[0] != 7 {
		t.Fatalf("late reply accepted: %+v %v", got, err)
	}
}

func TestClientAcquisitionFailures(t *testing.T) {
	rows := []struct {
		name, description string
		setup             func(*Client, *scriptSocket) context.Context
		wantErr           error
		wantClose         int
	}{
		{"canceled", "pre-canceled request never opens a socket", func(*Client, *scriptSocket) context.Context {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			return ctx
		}, context.Canceled, 0},
		{"closed", "permanent close prevents reopening", func(c *Client, _ *scriptSocket) context.Context {
			if err := c.Close(); err != nil {
				t.Fatal(err)
			}
			return context.Background()
		}, os.ErrClosed, 0},
		{"epoch_exhausted", "epoch never wraps", func(c *Client, _ *scriptSocket) context.Context {
			c.epoch = math.MaxUint64
			return context.Background()
		}, ErrEpoch, 0},
		{"open_failure", "open errno remains visible", func(c *Client, _ *scriptSocket) context.Context {
			c.open = func(context.Context, int) (transport, error) { return nil, unix.EMFILE }
			return context.Background()
		}, unix.EMFILE, 0},
		{"wrap_close_failure", "failed cleanup stops sequence renewal", func(c *Client, s *scriptSocket) context.Context {
			c.conn = s
			c.epoch = 1
			c.sequence = math.MaxUint32
			s.closeErr = unix.EIO
			return context.Background()
		}, unix.EIO, 1},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Log(row.description)
			s := respondingSocket()
			c := scriptedClient(unix.NETLINK_ROUTE, s)
			ctx := row.setup(c, s)
			result, err := execute(ctx, c, numberRequest(false, false))
			if !errors.Is(err, row.wantErr) || result.Values != nil || len(s.sent) != 0 || s.closes != row.wantClose {
				t.Fatalf("result=%+v err=%v sent=%d closes=%d", result, err, len(s.sent), s.closes)
			}
		})
	}
}

type parkedSocket struct {
	ready  chan struct{}
	closed chan struct{}
	once   sync.Once
	closes atomic.Int32
	budget time.Duration
}

func (s *parkedSocket) Send(ctx context.Context, _ []byte) error {
	due, ok := ctx.Deadline()
	if !ok {
		return ErrRequest
	}
	s.budget = time.Until(due)
	return nil
}
func (s *parkedSocket) Receive(ctx context.Context, _ func([]byte) error) (int, error) {
	close(s.ready)
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-s.closed:
		return 0, os.ErrClosed
	}
}
func (s *parkedSocket) Close() error {
	s.once.Do(func() { s.closes.Add(1); close(s.closed) })
	return nil
}

func TestClientConcurrentCloseAndCancellation(t *testing.T) {
	rows := []struct {
		name, description string
		cancel            bool
		wantErr           error
	}{
		{"close", "concurrent Close wakes owner and prevents reopening", false, os.ErrClosed},
		{"cancel", "context cancellation retires socket and returns no candidate", true, context.Canceled},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Log(row.description)
			s := &parkedSocket{ready: make(chan struct{}), closed: make(chan struct{})}
			c, _ := NewClient(unix.NETLINK_ROUTE)
			c.open = func(context.Context, int) (transport, error) { return s, nil }
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result := make(chan error, 1)
			go func() {
				got, err := execute(ctx, c, numberRequest(false, false))
				if got.Values != nil {
					result <- ErrReply
					return
				}
				result <- err
			}()
			select {
			case <-s.ready:
			case <-time.After(time.Second):
				t.Fatal("owner did not park")
			}
			if s.budget <= 0 || s.budget > 5*time.Second {
				t.Fatalf("request budget=%v", s.budget)
			}
			if _, err := execute(context.Background(), c, numberRequest(false, false)); !errors.Is(err, netlink.ErrConcurrent) {
				t.Fatalf("overlap=%v", err)
			}
			if err := c.Reset(); !errors.Is(err, netlink.ErrConcurrent) {
				t.Fatalf("overlapping reset=%v", err)
			}
			if row.cancel {
				cancel()
			} else {
				var wg sync.WaitGroup
				for range 4 {
					wg.Go(func() {
						if err := c.Close(); err != nil {
							t.Error(err)
						}
					})
				}
				wg.Wait()
			}
			select {
			case err := <-result:
				if !errors.Is(err, row.wantErr) {
					t.Fatalf("error=%v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("request did not stop")
			}
			if err := c.Close(); err != nil {
				t.Fatal(err)
			}
			if s.closes.Load() != 1 {
				t.Fatalf("closes=%d", s.closes.Load())
			}
		})
	}
}

func TestRequestValidation(t *testing.T) {
	rows := []struct {
		name, description string
		mutate            func([]byte)
	}{
		{"length", "builder length must describe complete request", func(b []byte) { binary.LittleEndian.PutUint32(b, 99) }},
		{"sequence", "wire sequence must equal transaction sequence", func(b []byte) { binary.LittleEndian.PutUint32(b[8:], 2) }},
		{"request", "request flag required", func(b []byte) { binary.LittleEndian.PutUint16(b[6:], 0) }},
		{"ack", "wire ACK must match completion contract", func(b []byte) { binary.LittleEndian.PutUint16(b[6:], unix.NLM_F_REQUEST|unix.NLM_F_ACK) }},
		{"dump", "wire dump must match completion contract", func(b []byte) { binary.LittleEndian.PutUint16(b[6:], unix.NLM_F_REQUEST|unix.NLM_F_DUMP) }},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Log(row.description)
			r := numberRequest(false, false)
			b, _ := r.build(1, 1)
			row.mutate(b)
			if _, err := newTransaction(r, b, 1, 1); !errors.Is(err, ErrRequest) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestACKRequestIdentity(t *testing.T) {
	rows := []struct {
		name, description string
		offset            int
		value             uint32
	}{
		{"type", "ACK for a different request type cannot complete", 20 + 4, 999},
		{"sequence", "embedded request sequence must match", 20 + 8, 99},
		{"length", "embedded request length must contain a header", 20, 0},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Log(row.description)
			r := numberRequest(false, true)
			b, _ := r.build(1, 1)
			state, _ := newTransaction(r, b, 1, 1)
			ack := control(0, 1)
			binary.LittleEndian.PutUint32(ack[row.offset:], row.value)
			if err := state.consume(1, ack); !errors.Is(err, ErrReply) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}
