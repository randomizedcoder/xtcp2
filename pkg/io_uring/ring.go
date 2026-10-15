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
// Centralized here so the panic message and the probe check stay in sync.
var requiredOps = []uint8{
	giouring.OpRecvmsg,
	giouring.OpRecv,
	giouring.OpAsyncCancel,
	giouring.OpSend,
	giouring.OpWritev,
}

// Config tunes the ring's queue depths.
type Config struct {
	// RecvBatchSize sizes the queues, and with RecvBufSize is the number
	// of provided receive buffers (rounded up to a power of two).
	RecvBatchSize int
	// CQEBatchSize bounds each PeekBatchCQE call.
	CQEBatchSize int
	// RecvBufSize, when > 0, gives the ring a provided-buffer ring for
	// EnqueueRecvMultishot: RecvBatchSize buffers (rounded up to a power
	// of two) of RecvBufSize bytes each.
	RecvBufSize int
}

// recvBufGroup is the buffer group ID of the ring's provided buffers.
const recvBufGroup = 0

// maxRecvBufs is the kernel's limit on entries in one buffer ring.
const maxRecvBufs = 1 << 15

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
	// More is set when the request stays armed and will post more CQEs
	// (IORING_CQE_F_MORE, multishot only). A multishot recv CQE without
	// it is the request's last; the caller re-arms it.
	More bool
	// Data is the received packet, for a CQE that consumed one of the
	// ring's provided buffers (IORING_CQE_F_BUFFER). It aliases that
	// buffer: the caller copies what it needs, then hands BufID to
	// RecycleRecvBuf. Nil otherwise.
	Data []byte
	// BufID is the provided buffer Data is in.
	BufID uint16
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

	// The provided-buffer ring (Config.RecvBufSize > 0): bufRingMem is
	// the mmap'd ring the kernel takes buffers from, recvBufs the
	// buffers themselves, recvBufCount of them, each recvBufSize bytes.
	bufRing      *giouring.BufAndRing
	bufRingMem   []byte
	recvBufs     []byte
	recvBufCount int
	recvBufSize  int
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
	if cfg.RecvBufSize > 0 {
		if err := r.setupRecvBufs(cfg.RecvBatchSize, cfg.RecvBufSize); err != nil {
			g.QueueExit()
			return nil, err
		}
	}
	return r, nil
}

// recvBufCountFor rounds n up to the power of two a buffer ring needs,
// capped at maxRecvBufs.
func recvBufCountFor(n int) int {
	c := 1
	for c < n && c < maxRecvBufs {
		c <<= 1
	}
	return c
}

// setupRecvBufs registers a provided-buffer ring of n (rounded up to a
// power of two) buffers of size bytes, all handed to the kernel. The ring
// memory is mmap'd here, rather than by giouring.SetupBufRing, so Close
// can unmap it.
func (r *Ring) setupRecvBufs(n, size int) error {
	count := recvBufCountFor(n)
	entrySize := int(unsafe.Sizeof(giouring.BufAndRing{}))
	mem, err := syscall.Mmap(-1, 0, count*entrySize,
		syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_ANONYMOUS|syscall.MAP_PRIVATE)
	if err != nil {
		return fmt.Errorf("io_uring buffer ring mmap(%d entries): %w", count, err)
	}
	br := (*giouring.BufAndRing)(unsafe.Pointer(&mem[0]))
	reg := giouring.BufReg{
		RingAddr:    uint64(uintptr(unsafe.Pointer(br))),
		RingEntries: uint32(count),
		Bgid:        recvBufGroup,
	}
	if _, err := r.r.RegisterBufferRing(&reg, 0); err != nil {
		if uerr := syscall.Munmap(mem); uerr != nil {
			err = errors.Join(err, uerr)
		}
		return fmt.Errorf("io_uring register buffer ring (Linux 5.19+): %w", err)
	}
	br.BufRingInit()

	r.bufRing = br
	r.bufRingMem = mem
	r.recvBufs = make([]byte, count*size)
	r.recvBufCount = count
	r.recvBufSize = size
	mask := giouring.BufRingMask(uint32(count))
	for i := range count {
		br.BufRingAdd(r.recvBufAddr(uint16(i)), uint32(size), uint16(i), mask, i)
	}
	br.BufRingAdvance(count)
	return nil
}

// recvBufAddr is the address of provided buffer bid.
func (r *Ring) recvBufAddr(bid uint16) uintptr {
	return uintptr(unsafe.Pointer(&r.recvBufs[int(bid)*r.recvBufSize]))
}

// recvBuf is provided buffer bid.
func (r *Ring) recvBuf(bid uint16) []byte {
	off := int(bid) * r.recvBufSize
	return r.recvBufs[off : off+r.recvBufSize : off+r.recvBufSize]
}

// RecvBufCount is the number of provided buffers, 0 without a buffer ring.
func (r *Ring) RecvBufCount() int {
	return r.recvBufCount
}

// RecycleRecvBuf gives provided buffer bid back to the kernel. Call it
// once the Result.Data that aliases it has been copied.
func (r *Ring) RecycleRecvBuf(bid uint16) {
	r.bufRing.BufRingAdd(r.recvBufAddr(bid), uint32(r.recvBufSize), bid,
		giouring.BufRingMask(uint32(r.recvBufCount)), 0)
	r.bufRing.BufRingAdvance(1)
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
// closeDrainStep caps each blocking WaitCQETimeout call inside the
// Close drain loop. Kept small so a sluggish kernel can't stall ring
// teardown past the caller's overall deadline.
const closeDrainStep = 50 * time.Millisecond

// waitForNextDrainCQE blocks for one CQE up to `step` (or `deadline`,
// whichever is shorter). Returns drainContinue when the caller should
// loop again (CQE arrived OR ETIME timeout), drainAbort when a non-
// timeout error means we should stop trying.
type drainOutcome int

const (
	drainContinue drainOutcome = iota
	drainAbort
)

// waitForNextDrainCQE handles the "no CQE waiting; block for one"
// branch of the Close drain loop. Returns (results, outcome) where
// results may be empty (timeout) — caller checks outcome before doing
// anything.
func (r *Ring) waitForNextDrainCQE(deadline time.Time) ([]Result, drainOutcome) {
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return nil, drainAbort
	}
	step := remaining
	if step > closeDrainStep {
		step = closeDrainStep
	}
	ts := syscall.NsecToTimespec(int64(step))
	if _, err := r.r.WaitCQETimeout(&ts); err != nil {
		// ETIME (timeout) is expected; anything else stops the drain.
		if !errors.Is(err, syscall.ETIME) && err.Error() != "errno 62" {
			return nil, drainAbort
		}
		// Timeout — nothing to drain this pass; caller continues.
		return nil, drainContinue
	}
	results, _ := r.drainOnce()
	return results, drainContinue
}

// dispatchDrainResults invokes onDrain for each result, if onDrain is
// non-nil. Skipped entirely when callers don't care about the bytes.
func dispatchDrainResults(results []Result, onDrain func(Result)) {
	if onDrain == nil {
		return
	}
	for _, res := range results {
		onDrain(res)
	}
}

// dispatchAbandonedInFlight hands every still-in-flight entry to the
// onDrain callback with a synthetic ETIME-style Res, then clears the
// inFlight map. MUST be called AFTER QueueExit so the kernel no
// longer references those buffers — otherwise a late CQE could write
// into a buffer that's already been returned to the pool.
func (r *Ring) dispatchAbandonedInFlight(onDrain func(Result)) {
	if onDrain == nil {
		// No callback → just clear so a future Close call doesn't
		// see stale entries.
		for reqID := range r.inFlight {
			delete(r.inFlight, reqID)
		}
		return
	}
	for reqID, entry := range r.inFlight {
		onDrain(Result{
			Op:       entry.op,
			Res:      -int32(syscall.ETIME),
			Buf:      entry.buf,
			HdrBytes: entry.wvHdr,
		})
		delete(r.inFlight, reqID)
	}
}

func (r *Ring) Close(drainTimeout time.Duration, onDrain func(Result)) {
	if r == nil || r.r == nil {
		return
	}
	// A multishot recv stays armed until canceled, so the drain below
	// would otherwise wait out drainTimeout for it. If the cancel can't
	// be submitted, skip the drain: QueueExit cancels everything.
	if err := r.cancelMultishot(); err != nil {
		drainTimeout = 0
	}
	deadline := time.Now().Add(drainTimeout)
	for time.Now().Before(deadline) && len(r.inFlight) > 0 {
		// First reap anything already arrived (non-blocking).
		results, _ := r.drainOnce()
		if len(results) == 0 {
			var outcome drainOutcome
			results, outcome = r.waitForNextDrainCQE(deadline)
			if outcome == drainAbort {
				break
			}
		}
		dispatchDrainResults(results, onDrain)
	}
	// QueueExit BEFORE handing in-flight buffers back: the kernel still
	// owns those buffers until the ring is torn down. Releasing them to
	// packetBufferPool before QueueExit would create a use-after-free
	// window where a CQE could land in a buffer that the pool has
	// already handed to a fresh netlinker. After QueueExit, the kernel
	// has no reference to the io_uring fd or its mapped buffers, so
	// onDrain can safely Put them back. Mark these as a synthetic
	// ETIME-style result (Res=-syscall.ETIME) so the callback can tell
	// apart "normal CQE with bytes" from "abandoned at teardown".
	r.r.QueueExit()
	r.r = nil
	r.dispatchAbandonedInFlight(onDrain)
	r.freeRecvBufs()
}

// cancelMultishot submits an async cancel for each armed multishot recv.
// Each then posts a final -ECANCELED CQE, which the drain reaps.
func (r *Ring) cancelMultishot() error {
	queued := false
	for reqID, entry := range r.inFlight {
		if entry.op != OpRecvMultishot {
			continue
		}
		sqe := r.r.GetSQE()
		if sqe == nil {
			return errors.New("io_uring.Close: SQ full, can't cancel the multishot recv")
		}
		sqe.PrepareCancel64(serialize(EncodedRequest{Operation: OpRecvMultishot, RequestID: reqID}), 0)
		sqe.SetData64(serialize(EncodedRequest{Operation: OpCancel}))
		queued = true
	}
	if !queued {
		return nil
	}
	_, err := r.r.Submit()
	return err
}

// freeRecvBufs unmaps the buffer ring. Only after QueueExit: the ring's
// teardown unregisters it.
func (r *Ring) freeRecvBufs() {
	if r.bufRingMem == nil {
		return
	}
	// Munmap only fails for a mapping syscall.Mmap didn't make.
	if err := syscall.Munmap(r.bufRingMem); err != nil {
		panic(fmt.Sprintf("io_uring buffer ring munmap: %v", err))
	}
	r.bufRing = nil
	r.bufRingMem = nil
	r.recvBufs = nil
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

// EnqueueRecvMultishot arms one multishot recv on fd that receives into
// the ring's provided buffers (Config.RecvBufSize). It posts a CQE per
// packet, in the order the socket queued them, each with More set while
// it stays armed. Once a CQE comes without More (e.g. -ENOBUFS when every
// buffer is in use), the caller arms it again.
func (r *Ring) EnqueueRecvMultishot(fd int) (uint32, error) {
	if r.bufRing == nil {
		return 0, errors.New("io_uring.EnqueueRecvMultishot: no buffer ring (Config.RecvBufSize is 0)")
	}
	if len(r.inFlight) >= r.inFlightCap {
		return 0, fmt.Errorf("io_uring in-flight cap exceeded (%d) — SQEs submitted faster than CQEs drained", r.inFlightCap)
	}
	sqe := r.r.GetSQE()
	if sqe == nil {
		return 0, errors.New("io_uring.EnqueueRecvMultishot: SQ full (GetSQE returned nil)")
	}
	sqe.PrepareRecvMultishot(fd, 0, 0, 0)
	sqe.Flags |= giouring.SqeBufferSelect
	sqe.BufIG = recvBufGroup
	reqID := r.NextRequestID()
	sqe.SetData64(serialize(EncodedRequest{Operation: OpRecvMultishot, RequestID: reqID}))

	r.inFlight[reqID] = inFlight{op: OpRecvMultishot}
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

	// When the payload is empty (proto.Marshal of a zero-valued message
	// can return an empty slice), passing iov[1]={Base:nil, Len:0} with
	// iovcnt=2 is undefined per writev(2): the kernel may EFAULT on the
	// nil Base even though Len is 0. Submit iovcnt=1 in that case so the
	// kernel only sees the header iovec.
	iovCount := uint32(2)
	if len(*payload) > 0 {
		iov[1] = syscall.Iovec{Base: &(*payload)[0], Len: uint64(len(*payload))}
	} else {
		iovCount = 1
	}

	iovPtr := uintptr(unsafe.Pointer(&iov[0]))
	sqe.PrepareWritev(fd, iovPtr, iovCount, 0)
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
		more := cqe.Flags&giouring.CQEFMore != 0
		entry, ok := r.inFlight[req.RequestID]
		if ok && !more {
			delete(r.inFlight, req.RequestID)
		}
		res := Result{
			Op:       req.Operation,
			Res:      cqe.Res,
			Buf:      entry.buf,
			HdrBytes: entry.wvHdr,
			More:     more,
		}
		if cqe.Flags&giouring.CQEFBuffer != 0 && r.recvBufs != nil {
			res.BufID = uint16(cqe.Flags >> giouring.CQEBufferShift)
			res.Data = r.recvBuf(res.BufID)[:max(cqe.Res, 0)]
		}
		out = append(out, res)
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

// WaitOneTimeout blocks until at least one CQE is available or the
// timeout fires. Returns a syscall.ETIME-like error on timeout so callers
// can distinguish "kernel had no data" from a real failure.
func (r *Ring) WaitOneTimeout(d time.Duration) ([]Result, error) {
	if d <= 0 {
		return r.WaitOne()
	}
	ts := syscall.NsecToTimespec(int64(d))
	if _, err := r.r.WaitCQETimeout(&ts); err != nil {
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
