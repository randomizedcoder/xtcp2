package xtcp

import (
	"bytes"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"syscall"

	xio "github.com/randomizedcoder/xtcp2/pkg/io_uring"
	"golang.org/x/sys/unix"
)

// The socket-count gate reads each tracked namespace's
// /proc/<pid>/net/sockstat before the dump. The file is opened once, on the
// thread that is in the namespace (netNamespaceInstance), so the fd is bound
// to that namespace for its lifetime (proc_create_net_single); each poll only
// re-reads it at offset 0.
//
// sockstat_seq_show (net/ipv4/proc.c) prints, per namespace:
//
//	TCP: inuse %d orphan %d tw %d alloc %d mem %ld
//
// inuse is sock_prot_inuse_get(net, &tcp_prot): hashed TCP sockets in this
// namespace (listeners and established; never negative). tw is that
// namespace's tw_refcount - 1 (TIME_WAIT). orphan, alloc and mem are
// host-wide. Request sockets (SYN_RECV) are in neither, so a SYN flood isn't
// visible here; the per-namespace record cap (-maxRecordsPerNetns) still
// bounds it.

// sockstatBufSize holds the whole file (about 200 bytes) with room to spare.
const sockstatBufSize = 1024

// sockstatPath is the per-thread sockstat: after Setns, the calling thread's
// namespace. /proc/self/net would follow the thread-group leader instead.
const sockstatPath = "/proc/thread-self/net/sockstat"

// portRangePath is the calling thread's namespace's
// net.ipv4.ip_local_port_range. A /proc/sys/net file resolves to the
// opener's namespace (net_ctl_header_lookup, net/sysctl_net.c) and the open
// file keeps that namespace's table entry (proc_sys_call_handler reads
// PROC_I(inode)->sysctl_entry), so like sockstat, a pread later reads the
// namespace it was opened in.
const portRangePath = "/proc/sys/net/ipv4/ip_local_port_range"

// portRangeBufSize holds "65535\t65535\n" with room to spare.
const portRangeBufSize = 32

// statHandle owns one namespace's gate fds: its sockstat and its
// ip_local_port_range. The poller reads them under the read lock; the
// namespace's teardown closes them under the write lock. So the poller
// never reads an fd number after it is closed, when it may already be
// reused for another file or socket.
type statHandle struct {
	mu      sync.RWMutex
	fd      int
	rangeFD int // -1: not open; shards then split the default range
}

// newStatHandle wraps the sockstat fd and the port-range fd; nil when the
// sockstat fd isn't open (<= 0). rangeFD may be -1.
func newStatHandle(fd, rangeFD int) *statHandle {
	if fd <= 0 {
		return nil
	}
	return &statHandle{fd: fd, rangeFD: rangeFD}
}

// portRange reads the namespace's ip_local_port_range into buf. The caller
// holds the read lock (acquire). ok is false when the fd isn't open, the
// read fails or the contents don't parse.
func (h *statHandle) portRange(buf []byte) (portRange, bool) {
	if h == nil || h.rangeFD <= 0 {
		return portRange{}, false
	}
	n, err := unix.Pread(h.rangeFD, buf, 0)
	if err != nil || n <= 0 {
		return portRange{}, false
	}
	return parsePortRange(buf[:n])
}

// parsePortRange parses ip_local_port_range ("lo\thi\n"). The kernel
// keeps 1 <= lo <= hi <= 65535 (ipv4_local_port_range); anything else is
// rejected.
func parsePortRange(b []byte) (portRange, bool) {
	var v [2]uint64
	i := 0
	for f := range v {
		for i < len(b) && (b[i] == ' ' || b[i] == '\t') {
			i++
		}
		start := i
		for i < len(b) && b[i] >= '0' && b[i] <= '9' {
			v[f] = v[f]*10 + uint64(b[i]-'0')
			if v[f] > 65535 {
				return portRange{}, false
			}
			i++
		}
		if i == start {
			return portRange{}, false
		}
	}
	for ; i < len(b); i++ {
		if b[i] != ' ' && b[i] != '\t' && b[i] != '\n' {
			return portRange{}, false
		}
	}
	if v[0] == 0 || v[0] > v[1] {
		return portRange{}, false
	}
	return portRange{lo: uint16(v[0]), hi: uint16(v[1])}, true
}

// acquire read-locks h and returns its fd, or -1 once closed. Every
// acquire on a non-nil handle needs a release. nil-safe.
func (h *statHandle) acquire() int {
	if h == nil {
		return -1
	}
	h.mu.RLock()
	return h.fd
}

func (h *statHandle) release() {
	if h != nil {
		h.mu.RUnlock()
	}
}

// close waits out a poll's read, then closes the fds. nil-safe, idempotent.
func (h *statHandle) close(closeFD func(int)) {
	if h == nil {
		return
	}
	h.mu.Lock()
	fd, rangeFD := h.fd, h.rangeFD
	h.fd, h.rangeFD = -1, -1
	h.mu.Unlock()
	if fd > 0 {
		closeFD(fd)
	}
	if rangeFD > 0 {
		closeFD(rangeFD)
	}
}

// sockstatCount is one namespace's TCP socket count.
type sockstatCount struct {
	inuse uint64
	tw    uint64
	known bool
}

// total is the number of records a full-state dump would return, at least
// (request sockets aren't counted).
func (c sockstatCount) total() uint64 { return c.inuse + c.tw }

var sockstatTCPPrefix = []byte("TCP:")

// parseSockstat returns the TCP inuse and tw counts from a sockstat file.
// It allocates nothing. ok is false when the TCP line, or either field, is
// missing or malformed. A negative tw (tw_refcount - 1 = -1 while a
// namespace is being torn down) reads as 0.
func parseSockstat(b []byte) (inuse, tw uint64, ok bool) {
	line := b
	for !bytes.HasPrefix(line, sockstatTCPPrefix) {
		i := bytes.IndexByte(line, '\n')
		if i < 0 {
			return 0, 0, false
		}
		line = line[i+1:]
	}
	// The kernel always ends the line; without the newline a short read
	// may have cut a number.
	i := bytes.IndexByte(line, '\n')
	if i < 0 {
		return 0, 0, false
	}
	line = line[:i]
	fields := line[len(sockstatTCPPrefix):]
	var haveInuse, haveTW bool
	for len(fields) > 0 {
		var key, val []byte
		key, fields = nextField(fields)
		if key == nil {
			break
		}
		val, fields = nextField(fields)
		if val == nil {
			return 0, 0, false
		}
		switch string(key) { // no allocation: compiler-recognized conversion
		case "inuse":
			n, neg, good := parseDecimal(val)
			if !good || neg {
				return 0, 0, false
			}
			inuse, haveInuse = n, true
		case "tw":
			n, neg, good := parseDecimal(val)
			if !good {
				return 0, 0, false
			}
			if neg {
				n = 0
			}
			tw, haveTW = n, true
		}
	}
	return inuse, tw, haveInuse && haveTW
}

// nextField returns the next space-separated token and the rest.
func nextField(b []byte) (tok, rest []byte) {
	for len(b) > 0 && (b[0] == ' ' || b[0] == '\t') {
		b = b[1:]
	}
	if len(b) == 0 {
		return nil, nil
	}
	end := 0
	for end < len(b) && b[end] != ' ' && b[end] != '\t' {
		end++
	}
	return b[:end], b[end:]
}

// parseDecimal parses an optionally negative decimal of at most 19 digits.
func parseDecimal(b []byte) (n uint64, neg, ok bool) {
	if len(b) > 0 && b[0] == '-' {
		neg = true
		b = b[1:]
	}
	if len(b) == 0 || len(b) > 19 {
		return 0, false, false
	}
	for _, c := range b {
		if c < '0' || c > '9' {
			return 0, false, false
		}
		n = n*10 + uint64(c-'0')
	}
	return n, neg, true
}

// sockstatReader fills counts[i] from fds[i]. An fd <= 0 (never opened; 0
// is stdin and never a sockstat fd) or a failed read leaves counts[i]
// unknown. It returns how many reads failed, and an error when the reader
// itself is no longer usable (the caller falls back to the syscall reader).
type sockstatReader interface {
	read(fds []int, counts []sockstatCount) (failed int, err error)
	close()
}

// errSockstatRingBroken: a submit or wait on the sockstat ring failed, so
// reads may still be in flight into its buffers; it is not used again.
var errSockstatRingBroken = errors.New("sockstat ring: submit/wait failed")

// readSockstatInto parses buf[:n] into c.
func readSockstatInto(c *sockstatCount, buf []byte, n int) bool {
	if n <= 0 {
		return false
	}
	inuse, tw, ok := parseSockstat(buf[:n])
	if !ok {
		return false
	}
	*c = sockstatCount{inuse: inuse, tw: tw, known: true}
	return true
}

// syscallSockstatReader does one pread per namespace.
type syscallSockstatReader struct {
	buf   []byte
	pread func(fd int, p []byte, off int64) (int, error)
}

func newSyscallSockstatReader() *syscallSockstatReader {
	return &syscallSockstatReader{buf: make([]byte, sockstatBufSize), pread: unix.Pread}
}

func (r *syscallSockstatReader) read(fds []int, counts []sockstatCount) (failed int, err error) {
	for i, fd := range fds {
		counts[i] = sockstatCount{}
		if fd <= 0 {
			continue
		}
		n, err := r.pread(fd, r.buf, 0)
		if err != nil || !readSockstatInto(&counts[i], r.buf, n) {
			failed++
		}
	}
	return failed, nil
}

func (*syscallSockstatReader) close() {}

// ringSockstatReader batches the reads through an io_uring: one
// io_uring_enter per batch. procfs files can't be read without blocking
// (no FMODE_NOWAIT; io_uring/rw.c), so each read goes to an io-wq worker;
// the SQEs set IOSQE_ASYNC to skip the doomed inline attempt, and the
// ring caps its io-wq workers so a batch doesn't spawn a thread per read.
//
// The ring is SINGLE_ISSUER, so one goroutine, locked to its thread,
// owns it and serves read calls over a channel.
type ringSockstatReader struct {
	reqs  chan ringSockstatReq
	reply chan ringSockstatReply
	done  chan struct{}
}

type ringSockstatReq struct {
	fds    []int
	counts []sockstatCount
	reply  chan ringSockstatReply
}

type ringSockstatReply struct {
	failed  int
	submits int
	err     error
}

// newRingSockstatReader starts the ring's owner goroutine. It returns an
// error, and starts nothing, if the ring can't be set up.
func newRingSockstatReader(batch, workers uint32) (*ringSockstatReader, error) {
	if batch == 0 {
		return nil, errors.New("sockstat ring: batch size 0")
	}
	r := &ringSockstatReader{
		reqs:  make(chan ringSockstatReq),
		reply: make(chan ringSockstatReply, 1),
		done:  make(chan struct{}),
	}
	ready := make(chan error, 1)
	go r.serve(int(batch), workers, ready)
	if err := <-ready; err != nil {
		return nil, err
	}
	return r, nil
}

func (r *ringSockstatReader) serve(batch int, workers uint32, ready chan<- error) {
	defer close(r.done)
	// The ring must be used from the task that created it (SINGLE_ISSUER).
	// This goroutine never changes namespace, so the thread is reusable.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread() //nolint:forbidigo // safe: this thread never calls setns; the lock only pins the SINGLE_ISSUER ring to one task

	ring, err := xio.New(xio.Config{RecvBatchSize: batch, CQEBatchSize: batch})
	if err != nil {
		ready <- fmt.Errorf("sockstat ring: %w", err)
		return
	}
	if workers > 0 {
		if _, _, err := ring.SetIOWQMaxWorkers(workers, workers); err != nil {
			ring.Close(0, nil) // nothing submitted yet
			ready <- err
			return
		}
	}
	ready <- nil

	b := newRingBatcher(ring, batch)
	defer func() {
		if b.broken {
			brokenSockstatRingsMu.Lock()
			brokenSockstatRings = append(brokenSockstatRings, b)
			brokenSockstatRingsMu.Unlock()
			return
		}
		// Every batch waited for all its completions: nothing in flight.
		ring.Close(0, nil)
	}()
	for req := range r.reqs {
		failed, submits, err := b.read(req.fds, req.counts)
		req.reply <- ringSockstatReply{failed: failed, submits: submits, err: err}
	}
}

func (r *ringSockstatReader) read(fds []int, counts []sockstatCount) (failed int, err error) {
	r.reqs <- ringSockstatReq{fds: fds, counts: counts, reply: r.reply}
	rep := <-r.reply
	return rep.failed, rep.err
}

func (r *ringSockstatReader) close() {
	close(r.reqs)
	<-r.done
}

// sockstatRing is the part of *xio.Ring the batcher uses (a seam for
// tests).
type sockstatRing interface {
	EnqueueReadAt(fd int, buf *[]byte, off uint64, async bool) (uint32, error)
	SubmitAndWait(waitNr uint32) (int, error)
	DrainBatch() []xio.Result
	WaitOne() ([]xio.Result, error)
}

// retryEINTR retries f while a signal interrupts the io_uring_enter.
func retryEINTR(f func() error) error {
	for {
		err := f()
		if !errors.Is(err, syscall.EINTR) {
			return err
		}
	}
}

// brokenSockstatRings keeps a broken ring and its buffers reachable for
// the life of the process. Reads may still be running in io-wq workers
// and will write into the buffers; closing the ring or letting the GC
// reuse the buffers would let the kernel write into freed memory. The
// gate falls back to pread for good after a break, so this holds at most
// one ring.
var (
	brokenSockstatRingsMu sync.Mutex
	brokenSockstatRings   []*ringBatcher
)

// ringBatcher queues up to batch reads, submits them in one
// io_uring_enter, and matches each CQE to its namespace by request ID.
type ringBatcher struct {
	ring   sockstatRing
	batch  int
	bufs   []*[]byte
	slot   map[uint32]ringSlot // request ID → namespace index and buffer
	broken bool
}

type ringSlot struct {
	idx int
	buf *[]byte
}

func newRingBatcher(ring sockstatRing, batch int) *ringBatcher {
	b := &ringBatcher{ring: ring, batch: batch, bufs: make([]*[]byte, batch), slot: make(map[uint32]ringSlot, batch)}
	for i := range b.bufs {
		buf := make([]byte, sockstatBufSize)
		b.bufs[i] = &buf
	}
	return b
}

// read returns the failed-read count, the number of io_uring_enter
// submits it made, and errSockstatRingBroken once a submit or wait fails.
func (b *ringBatcher) read(fds []int, counts []sockstatCount) (failed, submits int, err error) {
	for start := 0; start < len(fds); start += b.batch {
		end := min(start+b.batch, len(fds))
		if b.broken {
			clear(counts[start:])
			return failed + len(fds) - start, submits, errSockstatRingBroken
		}
		f, s := b.readBatch(fds[start:end], counts[start:end])
		failed += f
		submits += s
	}
	if b.broken {
		return failed, submits, errSockstatRingBroken
	}
	return failed, submits, nil
}

func (b *ringBatcher) readBatch(fds []int, counts []sockstatCount) (failed, submits int) {
	clear(b.slot)
	for i, fd := range fds {
		counts[i] = sockstatCount{}
		if fd <= 0 {
			continue
		}
		buf := b.bufs[len(b.slot)]
		id, err := b.ring.EnqueueReadAt(fd, buf, 0, true)
		if err != nil {
			failed++
			continue
		}
		b.slot[id] = ringSlot{idx: i, buf: buf}
	}
	want := len(b.slot)
	if want == 0 {
		return failed, 0
	}
	if err := retryEINTR(func() error { _, err := b.ring.SubmitAndWait(uint32(want)); return err }); err != nil {
		b.broken = true
		return failed + want, 1
	}
	for got := 0; got < want; {
		res := b.ring.DrainBatch()
		if len(res) == 0 {
			err := retryEINTR(func() error {
				var err error
				res, err = b.ring.WaitOne()
				return err
			})
			if err != nil {
				b.broken = true
				return failed + want - got, 1
			}
		}
		for _, x := range res {
			sl, ok := b.slot[x.RequestID]
			if !ok || x.Op != xio.OpReadAt {
				continue
			}
			delete(b.slot, x.RequestID)
			got++
			if !readSockstatInto(&counts[sl.idx], *sl.buf, int(x.Res)) {
				failed++
			}
		}
	}
	return failed, 1
}
