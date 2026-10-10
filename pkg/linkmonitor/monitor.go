package linkmonitor

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
)

var (
	// ErrAlreadyRun means Run has already been called, including a failed run.
	ErrAlreadyRun = errors.New("link monitor has already run")
	// ErrNotRunning means startup has not accepted controls, or shutdown began.
	ErrNotRunning = errors.New("link monitor is not accepting requests")
	// ErrShutdownIncomplete means a worker still owns resources after shutdown.
	ErrShutdownIncomplete = errors.New("link monitor shutdown incomplete")
	// ErrBackendUnavailable means the selected live backend is not implemented.
	ErrBackendUnavailable = errors.New("link monitor live backend unavailable")
)

// Options contains host-owned dependencies. A nil Logger discards output.
type Options struct {
	Logger *slog.Logger
}

// Monitor owns one monitoring lifetime. It must be created by New and not copied.
type Monitor struct {
	cfg     configuration
	logger  *slog.Logger
	once    atomic.Bool
	root    atomic.Pointer[snapshotRoot]
	control controls
	open    func(context.Context) (session, error)
}

// A session owns all acquired resources, including cleanup on Run failure.
// open must unwind partial acquisition if it fails. Run must join its workers
// before returning, or return ErrShutdownIncomplete and retain their resources.
// Only the session's reducer may publish state after startup.
type session interface {
	Run(context.Context, *Monitor) error
}

// New validates configuration without reading devices, files or environment.
func New(cfg Config, opts Options) (*Monitor, error) {
	validated, err := validateConfig(cfg)
	if err != nil {
		return nil, err
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	m := &Monitor{cfg: validated, logger: logger}
	m.control.wake = make(chan struct{}, 1)
	m.open = m.openProduction
	return m, nil
}

// Run acquires resources and blocks until shutdown. It may be called only once.
// Cancellation of an otherwise successful run returns nil. A canceled context
// before resource acquisition returns its error and still consumes the run.
func (m *Monitor) Run(ctx context.Context) error {
	if !m.once.CompareAndSwap(false, true) {
		return ErrAlreadyRun
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	m.logger.DebugContext(ctx, "starting link monitor", "backend", m.cfg.IOBackend)
	live, err := m.open(ctx)
	if err != nil {
		return err
	}
	m.root.Store(&snapshotRoot{version: 1, health: Health{Running: true}})
	defer m.stopped()
	return live.Run(ctx, m)
}

func (m *Monitor) stopped() {
	m.control.stop()
	previous := m.root.Load()
	next := *previous
	next.version++
	next.health = Health{}
	m.root.Store(&next)
}

// RequestResync coalesces a reconciliation request; acceptance is not completion.
func (m *Monitor) RequestResync() error { return m.control.request(resyncRequest) }

// RequestRebaseline requests fresh reconciliation and durable baseline replacement.
// It does not synchronously perform or guarantee a successful save.
func (m *Monitor) RequestRebaseline() error { return m.control.request(rebaselineRequest) }

// Snapshot returns a coherent immutable view without collection I/O.
func (m *Monitor) Snapshot() Snapshot { return Snapshot{root: m.root.Load()} }

// Health returns health from a single published snapshot.
func (m *Monitor) Health() Health { return m.Snapshot().Health() }

type requestKind uint8

const (
	resyncRequest requestKind = 1 << iota
	rebaselineRequest
)

// controls retains one bit per operation, including while it is in flight.
// A session begins each operation once and completes it after publishing its
// result. Requests joining an active operation never queue another operation.
type controls struct {
	mu              sync.Mutex
	ctx             context.Context
	accepting       bool
	pending, active requestKind
	wake            chan struct{}
}

func (c *controls) start(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ctx, c.accepting = ctx, true
}

func (c *controls) stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.accepting = false
	c.pending, c.active = 0, 0
}

func (c *controls) request(kind requestKind) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.accepting || c.ctx.Err() != nil {
		return ErrNotRunning
	}
	if (c.pending|c.active)&kind != 0 {
		return nil
	}
	c.pending |= kind
	select {
	case c.wake <- struct{}{}:
	default:
	}
	return nil
}

func (c *controls) begin() requestKind {
	c.mu.Lock()
	defer c.mu.Unlock()
	work := c.pending
	c.active |= work
	c.pending = 0
	return work
}

func (c *controls) complete(work requestKind) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.active &^= work
}
