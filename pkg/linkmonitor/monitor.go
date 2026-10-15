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
	demand  demands
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
	m.control.changed = make(chan struct{})
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
	m.demand.stop()
	previous := m.root.Load()
	next := *previous
	next.version++
	next.health = Health{}
	m.root.Store(&next)
}

// RequestResync coalesces a reconciliation request; acceptance is not completion.
func (m *Monitor) RequestResync() error {
	_, err := m.Submit(ControlResync)
	return err
}

// RequestRebaseline requests fresh reconciliation and durable baseline replacement.
// It does not synchronously perform or guarantee a successful save.
func (m *Monitor) RequestRebaseline() error {
	_, err := m.Submit(ControlRebaseline)
	return err
}

// Submit requests a control pass and returns a ticket for Await. A request
// joins a pass that has not begun yet. While a pass of the same kind is in
// flight, the request schedules exactly one more pass, so a completed ticket
// always reflects work that began after Submit returned. Acceptance is not
// completion.
func (m *Monitor) Submit(kind ControlKind) (ControlTicket, error) {
	bit, ok := kind.request()
	if !ok {
		return ControlTicket{}, ErrUnknownControl
	}
	generation, err := m.control.request(bit)
	if err != nil {
		return ControlTicket{}, err
	}
	return ControlTicket{kind: kind, generation: generation}, nil
}

// Await blocks until the ticket's pass completes, ctx ends, or the monitor
// stops. A rebaseline completes only once its baseline is durably saved, so a
// failing save keeps Await waiting while Snapshot().BaselineWriteErrors()
// grows. Waiters share one channel; Await allocates nothing per call.
func (m *Monitor) Await(ctx context.Context, ticket ControlTicket) error {
	bit, ok := ticket.kind.request()
	if !ok || ticket.generation == 0 {
		return ErrUnknownControl
	}
	return m.control.await(ctx, bit, ticket.generation)
}

// ControlProgress reports how many passes of kind were requested and completed
// in this monitoring lifetime.
func (m *Monitor) ControlProgress(kind ControlKind) ControlProgress {
	bit, ok := kind.request()
	if !ok {
		return ControlProgress{}
	}
	return m.control.progress(bit)
}

// Snapshot returns a coherent immutable view without collection I/O.
func (m *Monitor) Snapshot() Snapshot { return Snapshot{root: m.root.Load()} }

// Health returns health from a single published snapshot.
func (m *Monitor) Health() Health { return m.Snapshot().Health() }

// ErrUnknownControl means a ControlKind or ControlTicket was not produced by
// this package.
var ErrUnknownControl = errors.New("unknown link monitor control")

// ControlKind names an operator control.
type ControlKind uint8

const (
	// ControlResync reconciles inventory against the kernel.
	ControlResync ControlKind = iota + 1
	// ControlRebaseline reconciles and durably replaces the expected count.
	ControlRebaseline
)

func (k ControlKind) String() string {
	switch k {
	case ControlResync:
		return "resync"
	case ControlRebaseline:
		return "rebaseline"
	default:
		return unknownSetting
	}
}

func (k ControlKind) request() (requestKind, bool) {
	switch k {
	case ControlResync:
		return resyncRequest, true
	case ControlRebaseline:
		return rebaselineRequest, true
	default:
		return 0, false
	}
}

// ControlTicket identifies one requested pass. The zero value is invalid.
type ControlTicket struct {
	kind       ControlKind
	generation uint64
}

// Kind reports the control the ticket waits for.
func (t ControlTicket) Kind() ControlKind { return t.kind }

// Generation is the pass number, starting at 1 in each monitoring lifetime.
func (t ControlTicket) Generation() uint64 { return t.generation }

// ControlProgress counts passes of one kind. Completed never exceeds
// Requested, and Requested exceeds Completed by at most two (one pass in
// flight and one pending).
type ControlProgress struct {
	Requested, Completed uint64
}

type requestKind uint8

const (
	resyncRequest requestKind = 1 << iota
	rebaselineRequest
)

// controls retains one bit per operation for pending and in-flight work, plus
// per-kind generation counters. A session begins each operation once and
// completes it after publishing its result. A request joins pending work of its
// kind; while that kind is in flight it becomes pending, and it begins only
// after the in-flight pass completes. Every completion and stop closes and
// replaces changed, so any number of Await callers share one channel.
type controls struct {
	mu              sync.Mutex
	ctx             context.Context
	accepting       bool
	pending, active requestKind
	wake            chan struct{}
	changed         chan struct{}
	requested       [controlKinds]uint64
	begun           [controlKinds]uint64
	completed       [controlKinds]uint64
}

// controlKinds is the number of requestKind bits.
const controlKinds = 2

func (k requestKind) index() int {
	if k == resyncRequest {
		return 0
	}
	return 1
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
	c.broadcast()
}

func (c *controls) request(kind requestKind) (uint64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.accepting || c.ctx.Err() != nil {
		return 0, ErrNotRunning
	}
	i := kind.index()
	if c.pending&kind == 0 {
		c.pending |= kind
		c.requested[i]++
		c.signal()
	}
	return c.requested[i], nil
}

// begin starts pending work whose kind is not already in flight.
func (c *controls) begin() requestKind {
	c.mu.Lock()
	defer c.mu.Unlock()
	work := c.pending &^ c.active
	c.active |= work
	c.pending &^= work
	for _, kind := range [...]requestKind{resyncRequest, rebaselineRequest} {
		if work&kind != 0 {
			c.begun[kind.index()] = c.requested[kind.index()]
		}
	}
	return work
}

func (c *controls) complete(work requestKind) {
	if work == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	work &= c.active
	if work == 0 {
		return
	}
	c.active &^= work
	for _, kind := range [...]requestKind{resyncRequest, rebaselineRequest} {
		if work&kind != 0 {
			c.completed[kind.index()] = c.begun[kind.index()]
		}
	}
	if c.pending != 0 {
		c.signal()
	}
	c.broadcast()
}

func (c *controls) await(ctx context.Context, kind requestKind, generation uint64) error {
	i := kind.index()
	for {
		c.mu.Lock()
		done, accepting, changed := c.completed[i] >= generation, c.accepting, c.changed
		c.mu.Unlock()
		switch {
		case done:
			return nil
		case !accepting:
			return ErrNotRunning
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (c *controls) progress(kind requestKind) ControlProgress {
	c.mu.Lock()
	defer c.mu.Unlock()
	return ControlProgress{Requested: c.requested[kind.index()], Completed: c.completed[kind.index()]}
}

// signal wakes the session without blocking; one buffered token suffices.
func (c *controls) signal() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// broadcast releases every Await waiter. Callers hold mu.
func (c *controls) broadcast() {
	close(c.changed)
	c.changed = make(chan struct{})
}
