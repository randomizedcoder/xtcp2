// Package linuxio owns read-only monitor requests and transaction completion.
// Event sockets and reconciliation policy remain separate from request clients.
package linuxio

import (
	"context"
	"errors"
	"math"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/netlink"
	"golang.org/x/sys/unix"
)

var (
	ErrRequest     = errors.New("linuxio: invalid request")
	ErrReply       = errors.New("linuxio: invalid transaction reply")
	ErrInterrupted = errors.New("linuxio: interrupted dump")
	ErrLimit       = errors.New("linuxio: transaction exceeds bounds")
	ErrEpoch       = errors.New("linuxio: stale or exhausted socket epoch")
	ErrFamily      = errors.New("linuxio: family requires discovery on this socket epoch")
)

const requestBudget = 5 * time.Second

type transport interface {
	Send(context.Context, []byte) error
	Receive(context.Context, func([]byte) error) (int, error)
	Close() error
}

// Client serializes requests on one protocol socket. Overlap is rejected rather
// than queued. Close is concurrent-safe and permanent; failures retire only the
// current socket, and the next request opens a fresh epoch without retrying work.
type Client struct {
	busy     atomic.Bool
	mu       sync.Mutex
	conn     transport
	closed   bool
	closeErr error
	protocol int
	epoch    uint64
	sequence uint32
	open     func(context.Context, int) (transport, error)
}

// NewClient creates a lazy request owner. It never subscribes to event groups.
// RDMA discovery uses its own protocol adapter, not this generic-family API.
func NewClient(protocol int) (*Client, error) {
	if protocol != unix.NETLINK_ROUTE && protocol != unix.NETLINK_GENERIC && protocol != unix.NETLINK_RDMA {
		return nil, ErrRequest
	}
	return &Client{protocol: protocol, open: func(ctx context.Context, p int) (transport, error) {
		return netlink.Open(ctx, p, nil)
	}}, nil
}

// Close stops active I/O and prevents this client from opening another socket.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed {
		c.closed = true
		c.closeErr = c.retireLocked()
	}
	return c.closeErr
}

func (c *Client) retireLocked() error {
	if c.conn == nil {
		return nil
	}
	err := c.conn.Close()
	c.conn = nil
	return err
}

func (c *Client) retire() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return errors.Join(c.retireLocked(), c.closeErr)
}

// Reset retires a socket after external family/loss notification. The next
// request opens a new epoch; previously discovered family handles then fail.
// Scheduler backoff and resubscription are the caller's responsibility.
func (c *Client) Reset() error {
	if !c.busy.CompareAndSwap(false, true) {
		return netlink.ErrConcurrent
	}
	defer c.busy.Store(false)
	return c.retire()
}

// Called only by the I/O owner. Sequence zero is never issued and no sequence
// is reused within an epoch, even after a local builder failure.
func (c *Client) acquire(ctx context.Context) (transport, uint64, uint32, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, 0, 0, errors.Join(os.ErrClosed, c.closeErr)
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, 0, err
	}
	if c.sequence == math.MaxUint32 {
		if err := c.retireLocked(); err != nil {
			return nil, 0, 0, err
		}
	}
	if c.conn == nil {
		if c.epoch == math.MaxUint64 {
			return nil, 0, 0, ErrEpoch
		}
		conn, err := c.open(ctx, c.protocol)
		if err != nil {
			return nil, 0, 0, err
		}
		c.conn = conn
		c.epoch++
		c.sequence = 0
	}
	c.sequence++
	return c.conn, c.epoch, c.sequence, nil
}

// Result contains only a fully successful candidate. Values own their storage;
// callers still apply reducer generation/revision checks before publication.
type Result[T any] struct {
	Epoch    uint64
	Sequence uint32
	Values   []T
}

func execute[T any](ctx context.Context, c *Client, r request[T]) (Result[T], error) {
	if !c.busy.CompareAndSwap(false, true) {
		return Result[T]{}, netlink.ErrConcurrent
	}
	defer c.busy.Store(false)
	ctx, cancel := context.WithTimeout(ctx, requestBudget)
	defer cancel()
	conn, epoch, seq, err := c.acquire(ctx)
	if err != nil {
		return Result[T]{}, err
	}
	data, err := r.build(seq, epoch)
	if err != nil {
		return Result[T]{}, err
	}
	t, err := newTransaction(r, data, epoch, seq)
	if err != nil {
		return Result[T]{}, err
	}
	if err = conn.Send(ctx, data); err == nil {
		err = t.receive(ctx, conn)
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return Result[T]{}, errors.Join(err, c.retire())
	}
	return Result[T]{Epoch: epoch, Sequence: seq, Values: t.values}, nil
}
