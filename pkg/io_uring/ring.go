package io_uring

import (
	"errors"
	"fmt"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/randomizedcoder/giouring"
)

// Setup flags chosen for "lighter on the system" semantics on a periodic
// netlink workload:
//   - SetupSingleIssuer  : kernel skips locking on the SQ side (Linux 6.0+).
//   - SetupDeferTaskrun  : completion task work runs only when the owner
//     calls Submit/Wait, instead of at IRQ time (Linux 6.1+).
//   - SetupCoopTaskrun   : no inter-processor interrupt on CQE completion;
//     wake on the next WaitCQE naturally.
//
// Deliberately NOT using SetupSQPoll — a kernel poll thread burns one CPU
// per ring continuously, which is catastrophic for a 1Hz polling tool.
// Document for future readers: SQPoll is for storage-style sustained
// submit workloads, not periodic dumps.
const setupFlags uint32 = giouring.SetupSingleIssuer |
	giouring.SetupDeferTaskrun |
	giouring.SetupCoopTaskrun

// Required opcodes — if any are missing we refuse to enable io_uring mode.
// Centralised here so the panic message and the probe check stay in sync.
var requiredOps = []uint8{
	giouring.OpRecvmsg,
	giouring.OpSend,
	giouring.OpWritev,
}

// Config tunes the ring's queue depths.
type Config struct {
	// RecvBatchSize is the number of recvmsg SQEs we keep in flight.
	// Higher reduces syscalls per dump cycle on high-fanout hosts at the
	// cost of pinned packet-pool buffers.
	RecvBatchSize int
	// CQEBatchSize bounds each PeekBatchCQE call.
	CQEBatchSize int
}

// Result is what a drainer hands back to the netlinker for one CQE.
type Result struct {
	Op Operation
	// Res is the kernel's CQE result: bytes transferred when positive,
	// -errno when negative.
	Res int32
	// Buf is the buffer the operation owned. For OpRead, it points at the
	// packet pool buffer that received bytes (caller slices to Res and
	// returns to packetBufferPool). For send/writev ops, it's the
	// destBytesPool buffer that was just written (caller returns to pool).
	Buf *[]byte
	// HdrBytes is the small per-send header for OpSendUnix (varint prefix);
	// the caller doesn't need to do anything with it — the Ring owns it.
	HdrBytes []byte
}

// inFlight tracks every SQE we've submitted but whose CQE hasn't arrived.
// The structs it points at (Msghdr, Iovec) must outlive submission, so we
// stash them here keyed by RequestID.
type inFlight struct {
	op  Operation
	buf *[]byte
	// For OpRead: the Msghdr and its single Iovec, kept alive for the
	// kernel to fill.
	msg *syscall.Msghdr
	iov *syscall.Iovec
	// For OpSendUnix: the two-element iovec array (header + payload)
	// passed to writev. Both must remain valid until the CQE.
	wvIov *[2]syscall.Iovec
	wvHdr []byte // backing storage for the varint length header
}

// Ring is xtcp2's per-Netlinker io_uring wrapper. It is NOT safe for
// concurrent use — every method must run on the goroutine that created
// it.
type Ring struct {
	r           *giouring.Ring
	cfg         Config
	cqeBuf      []*giouring.CompletionQueueEvent
	nextReqID   uint32
	inFlight    map[uint32]inFlight
	inFlightCap int
}

// New creates a Ring sized for the given config. sqEntries is
// max(RecvBatchSize*2, 256) so that refills never spill the SQ during
// drain. Panics with a clear "kernel too old" message if the probe shows
// any required opcode is missing — caller opted into io_uring, silent
// fallback would hide the fault.
func New(cfg Config) (*Ring, error) {
	if cfg.RecvBatchSize < 1 {
		return nil, errors.New("io_uring.New: RecvBatchSize must be >= 1")
	}
	if cfg.CQEBatchSize < 1 {
		return nil, errors.New("io_uring.New: CQEBatchSize must be >= 1")
	}

	if err := requireProbe(); err != nil {
		return nil, err
	}

	sqEntries := uint32(cfg.RecvBatchSize * 2)
	if sqEntries < 256 {
		sqEntries = 256
	}

	g := giouring.NewRing()
	if err := g.QueueInit(sqEntries, setupFlags); err != nil {
		return nil, fmt.Errorf("QueueInit(%d, flags=0x%x): %w", sqEntries, setupFlags, err)
	}

	r := &Ring{
		r:           g,
		cfg:         cfg,
		cqeBuf:      make([]*giouring.CompletionQueueEvent, cfg.CQEBatchSize),
		inFlight:    make(map[uint32]inFlight, sqEntries),
		inFlightCap: int(sqEntries) * 2, // generous; refuse to leak unbounded
	}
	return r, nil
}

// requireProbe asks the kernel which opcodes are supported and panics
// with a clear message if any of the ones we depend on are missing.
func requireProbe() error {
	p, err := giouring.GetProbe()
	if err != nil {
		return fmt.Errorf("io_uring probe failed (kernel too old or io_uring disabled?): %w", err)
	}
	for _, op := range requiredOps {
		if !p.IsSupported(op) {
			return fmt.Errorf("io_uring opcode %d not supported by this kernel — need Linux 6.1+ for the configured setup flags (SingleIssuer+DeferTaskrun+CoopTaskrun)", op)
		}
	}
	return nil
}

// Close drains pending CQEs (best-effort, up to drainTimeout), releases
// any in-flight pool buffers back to the caller's drain callback, then
// unmaps the ring. Safe to call multiple times.
func (r *Ring) Close(drainTimeout time.Duration, onDrain func(Result)) {
	if r == nil || r.r == nil {
		return
	}
	deadline := time.Now().Add(drainTimeout)
	for time.Now().Before(deadline) && len(r.inFlight) > 0 {
		// First reap anything already arrived (non-blocking).
		results, _ := r.drainOnce()
		if len(results) == 0 {
			// Nothing yet — block for one CQE with a short timeout.
			remaining := time.Until(deadline)
			if remaining <= 0 {
				break
			}
			step := remaining
			if step > 50*time.Millisecond {
				step = 50 * time.Millisecond
			}
			ts := syscall.NsecToTimespec(int64(step))
			if _, err := r.r.WaitCQETimeout(&ts); err != nil {
				// ETIME (timeout) is expected; anything else stops us.
				if !errors.Is(err, syscall.ETIME) && err.Error() != "errno 62" {
					break
				}
				continue
			}
			results, _ = r.drainOnce()
		}
		if onDrain != nil {
			for _, res := range results {
				onDrain(res)
			}
		}
	}
	r.r.QueueExit()
	r.r = nil
}

// NextRequestID returns a fresh per-ring monotonic counter value.
func (r *Ring) NextRequestID() uint32 {
	r.nextReqID++
	return r.nextReqID
}

// EnqueueRecvMsg builds an SQE that asks the kernel to do recvmsg(fd, buf)
// when data is available. The buf and the supporting Msghdr/Iovec stay
// pinned in the in-flight map until the CQE arrives. Returns the
// RequestID stamped in the SQE userdata.
func (r *Ring) EnqueueRecvMsg(fd int, buf *[]byte) (uint32, error) {
	if buf == nil || len(*buf) == 0 {
		return 0, errors.New("io_uring.EnqueueRecvMsg: empty buffer")
	}
	if len(r.inFlight) >= r.inFlightCap {
		return 0, fmt.Errorf("io_uring in-flight cap exceeded (%d) — SQEs submitted faster than CQEs drained", r.inFlightCap)
	}
	sqe := r.r.GetSQE()
	if sqe == nil {
		return 0, errors.New("io_uring.EnqueueRecvMsg: SQ full (GetSQE returned nil)")
	}

	iov := &syscall.Iovec{Base: &(*buf)[0], Len: uint64(len(*buf))}
	msg := &syscall.Msghdr{
		Iov:    iov,
		Iovlen: 1,
	}
	sqe.PrepareRecvMsg(fd, msg, 0)
	reqID := r.NextRequestID()
	sqe.SetData64(serialize(EncodedRequest{Operation: OpRead, RequestID: reqID}))

	r.inFlight[reqID] = inFlight{op: OpRead, buf: buf, msg: msg, iov: iov}
	return reqID, nil
}

// EnqueueSend builds a `send(2)` SQE. For UDP / unixgram destinations the
// kernel preserves the message boundary. Op is one of OpSendUDP or
// OpSendUnixGram.
func (r *Ring) EnqueueSend(fd int, buf *[]byte, op Operation) (uint32, error) {
	if buf == nil {
		return 0, errors.New("io_uring.EnqueueSend: nil buffer")
	}
	if op != OpSendUDP && op != OpSendUnixGram {
		return 0, fmt.Errorf("io_uring.EnqueueSend: unsupported op %d (want OpSendUDP or OpSendUnixGram)", op)
	}
	if len(r.inFlight) >= r.inFlightCap {
		return 0, fmt.Errorf("io_uring in-flight cap exceeded (%d) — SQEs submitted faster than CQEs drained", r.inFlightCap)
	}
	sqe := r.r.GetSQE()
	if sqe == nil {
		return 0, errors.New("io_uring.EnqueueSend: SQ full (GetSQE returned nil)")
	}

	addr := uintptr(0)
	length := uint32(len(*buf))
	if length > 0 {
		addr = uintptr(unsafe.Pointer(&(*buf)[0]))
	}
	sqe.PrepareSend(fd, addr, length, 0)
	reqID := r.NextRequestID()
	sqe.SetData64(serialize(EncodedRequest{Operation: op, RequestID: reqID}))

	r.inFlight[reqID] = inFlight{op: op, buf: buf}
	return reqID, nil
}

// EnqueueWritevUnix submits a 2-iovec writev to deliver a varint-prefixed
// frame (header + payload) atomically on a SOCK_STREAM unix socket. The
// header bytes and iovec array are stashed in the in-flight map; the
// payload buffer is borrowed from destBytesPool by the caller and
// returned to the pool on CQE reap.
func (r *Ring) EnqueueWritevUnix(fd int, header []byte, payload *[]byte) (uint32, error) {
	if payload == nil {
		return 0, errors.New("io_uring.EnqueueWritevUnix: nil payload")
	}
	if len(header) == 0 {
		return 0, errors.New("io_uring.EnqueueWritevUnix: empty header")
	}
	if len(r.inFlight) >= r.inFlightCap {
		return 0, fmt.Errorf("io_uring in-flight cap exceeded (%d)", r.inFlightCap)
	}
	sqe := r.r.GetSQE()
	if sqe == nil {
		return 0, errors.New("io_uring.EnqueueWritevUnix: SQ full")
	}

	// Allocate iov on the heap so it survives until the CQE arrives;
	// taking the address of a local [2]Iovec would point at a stack
	// slot that's recycled the moment this function returns.
	iov := new([2]syscall.Iovec)
	hdrCopy := make([]byte, len(header))
	copy(hdrCopy, header)
	iov[0] = syscall.Iovec{Base: &hdrCopy[0], Len: uint64(len(hdrCopy))}
	if len(*payload) > 0 {
		iov[1] = syscall.Iovec{Base: &(*payload)[0], Len: uint64(len(*payload))}
	}

	iovPtr := uintptr(unsafe.Pointer(&iov[0]))
	sqe.PrepareWritev(fd, iovPtr, 2, 0)
	reqID := r.NextRequestID()
	sqe.SetData64(serialize(EncodedRequest{Operation: OpSendUnix, RequestID: reqID}))

	r.inFlight[reqID] = inFlight{
		op:    OpSendUnix,
		buf:   payload,
		wvIov: iov,
		wvHdr: hdrCopy,
	}
	return reqID, nil
}

// Submit flushes the SQ to the kernel in one syscall.
func (r *Ring) Submit() (int, error) {
	n, err := r.r.Submit()
	return int(n), err
}

// SubmitAndWait flushes the SQ and waits for at least `waitNr`
// completions. Used by drain loops that want a single syscall round-trip.
func (r *Ring) SubmitAndWait(waitNr uint32) (int, error) {
	n, err := r.r.SubmitAndWait(waitNr)
	return int(n), err
}

// DrainBatch reaps up to CQEBatchSize CQEs without blocking, decodes each
// back into a Result by looking up its in-flight entry, and returns the
// slice (shared backing — caller must consume before next call).
func (r *Ring) DrainBatch() []Result {
	results, _ := r.drainOnce()
	return results
}

func (r *Ring) drainOnce() ([]Result, int) {
	n := r.r.PeekBatchCQE(r.cqeBuf)
	if n == 0 {
		return nil, 0
	}
	out := make([]Result, 0, n)
	for i := uint32(0); i < n; i++ {
		cqe := r.cqeBuf[i]
		req := deserialize(cqe.GetData64())
		entry, ok := r.inFlight[req.RequestID]
		if ok {
			delete(r.inFlight, req.RequestID)
		}
		out = append(out, Result{
			Op:       req.Operation,
			Res:      cqe.Res,
			Buf:      entry.buf,
			HdrBytes: entry.wvHdr,
		})
	}
	r.r.CQAdvance(n)
	return out, int(n)
}

// WaitOne blocks (with the kernel's enter-syscall deadline) until at
// least one CQE is available, then returns DrainBatch.
func (r *Ring) WaitOne() ([]Result, error) {
	if _, err := r.r.WaitCQE(); err != nil {
		return nil, err
	}
	return r.DrainBatch(), nil
}

// InFlightLen reports how many SQEs are queued but not yet completed —
// used by tests to assert clean teardown.
func (r *Ring) InFlightLen() int {
	return len(r.inFlight)
}

// SQReady returns the number of SQEs queued but not yet submitted to the
// kernel. Useful for tests / assertions.
func (r *Ring) SQReady() uint32 {
	return r.r.SQReady()
}

// Mutex-free contract reminder: a Ring is goroutine-bound. The mutex
// field below is unused at runtime; its presence is a static signal to
// future contributors that a future concurrent-access pattern needs to
// be designed around an explicit lock or move to one ring per goroutine.
var _ sync.Mutex
