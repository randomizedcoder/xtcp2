//go:build linux

package rdmacaps

import (
	"context"
	"encoding/binary"
	"errors"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/linuxio"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// fakeUMAD is a umad device over a SOCK_SEQPACKET pair: the exchange writes,
// polls, reads and closes a real descriptor, and the fake's kernel side
// answers each request as it is written. resolve, open and ioctl are scripted.
type fakeUMAD struct {
	tb testing.TB

	name                                      string
	abi                                       uint64
	resolveErr, openErr, pkeyErr, registerErr error
	writeErr, unregisterErr, closeErr         error
	shortWrite                                bool
	// answer turns the reply the kernel would queue into what it queues;
	// nil queues nothing.
	answer func(reply []byte) []byte
	// written runs after a request is written, before it is answered.
	written func()

	mu                             sync.Mutex
	peers                          map[int]*fakeSlot
	slots                          [fakeSlots]fakeSlot
	agents                         map[int]bool
	nextAgent                      uint32
	resolved, opened, closed       int
	pkey, registered, unregistered int
	polls                          []int
	sentTimeout                    uint32
}

// fakeSlots bounds the descriptors open at once: enough for every
// goroutine of BenchmarkUMADExchange/contended.
const fakeSlots = 64

func newFakeUMAD(tb testing.TB) *fakeUMAD {
	return &fakeUMAD{tb: tb, name: "umad0", abi: umadABIVersion, nextAgent: 7, peers: make(map[int]*fakeSlot, 4), agents: make(map[int]bool, 4), polls: make([]int, 0, 8),
		answer: func(b []byte) []byte { return b }}
}

func (f *fakeUMAD) resolve(_ context.Context, device string, port uint32) (xtcpnl.RdmaChardev, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resolved++
	if device != "mlx5_0" || port != 1 {
		f.tb.Errorf("resolve(%q, %d)", device, port)
	}
	ch := xtcpnl.RdmaChardev{ABI: f.abi}
	ch.NameLen = uint8(copy(ch.Name[:], f.name))
	return ch, f.resolveErr
}

func (f *fakeUMAD) open(*xtcpnl.RdmaChardev) (int, error) {
	if f.openErr != nil {
		return -1, f.openErr
	}
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return -1, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.slots {
		if slot := &f.slots[i]; !slot.used {
			slot.used, slot.peer = true, fds[1]
			f.opened++
			f.peers[fds[0]] = slot
			return fds[0], nil
		}
	}
	f.tb.Errorf("more than %d descriptors open", fakeSlots)
	return -1, errors.Join(syscall.EMFILE, unix.Close(fds[0]), unix.Close(fds[1]))
}

func (f *fakeUMAD) ioctl(fd int, request uintptr, arg unsafe.Pointer) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.peers[fd]; !ok {
		f.tb.Errorf("ioctl %#x on unopened fd %d", request, fd)
	}
	switch request {
	case umadEnablePKey:
		if f.agents[fd] {
			f.tb.Error("ENABLE_PKEY after registration")
		}
		f.pkey++
		return f.pkeyErr
	case umadRegisterAgent2:
		reg := (*[umadRegReq2Len]byte)(arg)
		if reg[umadRegReq2OffMgmtClass] != 0x81 || reg[umadRegReq2OffVersion] != 1 || binary.NativeEndian.Uint32(reg[umadRegReq2OffQPN:]) != 0 {
			f.tb.Errorf("registration %x", reg[:])
		}
		if f.registerErr != nil {
			return f.registerErr
		}
		binary.NativeEndian.PutUint32(reg[umadRegReq2OffID:], f.nextAgent)
		f.nextAgent++
		f.registered++
		f.agents[fd] = true
		return nil
	case umadUnregisterAgent:
		f.unregistered++
		delete(f.agents, fd)
		return f.unregisterErr
	}
	f.tb.Errorf("ioctl %#x", request)
	return syscall.ENOTTY
}

func (f *fakeUMAD) write(fd int, p []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	if f.shortWrite {
		return len(p) - 1, nil
	}
	n, err := unix.Write(fd, p)
	if err != nil {
		return n, err
	}
	f.mu.Lock()
	slot := f.peers[fd]
	f.mu.Unlock()
	peer, req, reply := slot.peer, &slot.req, &slot.reply
	if m, err := unix.Read(peer, req[:]); err != nil || m != len(req) {
		f.tb.Errorf("kernel side read %d, %v", m, err)
	}
	var h umadHeader
	h.decode((*[umadHdrLen]byte)(req[:umadHdrLen]))
	f.mu.Lock()
	f.sentTimeout = h.TimeoutMs
	f.mu.Unlock()
	if h.LID != 0xffff || h.QPN != 0 || h.Retries != 0 {
		f.tb.Errorf("request header %+v", h)
	}
	if f.written != nil {
		f.written()
	}
	// The kernel answers with the agent's header and a GetResp.
	*reply = *req
	clear(reply[umadHdrOffTimeout : umadHdrOffLength+4])
	mad := portReply([256]byte(req[umadHdrLen:]))
	copy(reply[umadHdrLen:], mad[:])
	if out := f.answer(reply[:]); out != nil {
		if _, err := unix.Write(peer, out); err != nil {
			f.tb.Errorf("kernel side write: %v", err)
		}
	}
	return n, nil
}

func (f *fakeUMAD) poll(fd int, timeoutMs int) (bool, error) {
	f.mu.Lock()
	if len(f.polls) < cap(f.polls) {
		f.polls = append(f.polls, timeoutMs) // bounded, so the benchmark counts no fake allocation
	}
	f.mu.Unlock()
	fds := [1]unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	n, err := unix.Poll(fds[:], timeoutMs)
	return n > 0, err
}

func (f *fakeUMAD) read(fd int, p []byte) (int, error) { return unix.Read(fd, p) }

func (f *fakeUMAD) close(fd int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed++
	slot, ok := f.peers[fd]
	if !ok {
		f.tb.Errorf("close of unopened fd %d", fd)
		return syscall.EBADF
	}
	delete(f.peers, fd)
	slot.used = false
	if err := unix.Close(slot.peer); err != nil {
		f.tb.Errorf("closing the kernel side: %v", err)
	}
	if err := unix.Close(fd); err != nil {
		return err
	}
	return f.closeErr
}

// fakeSlot is one open descriptor's kernel side and its buffers, held in the
// fake so an exchange over it allocates nothing of the fake's.
type fakeSlot struct {
	used       bool
	peer       int
	req, reply umadPacket
}

// status sets the reply header's status and cuts the read to n bytes.
func status(s uint32, n int) func([]byte) []byte {
	return func(b []byte) []byte {
		binary.NativeEndian.PutUint32(b[umadHdrOffStatus:], s)
		return b[:n]
	}
}

func TestUMADExchange(t *testing.T) {
	full := len(umadPacket{})
	background := func(*testing.T) context.Context { return context.Background() }
	within := func(d time.Duration) func(*testing.T) context.Context {
		return func(t *testing.T) context.Context {
			ctx, cancel := context.WithTimeout(context.Background(), d)
			t.Cleanup(cancel)
			return ctx
		}
	}
	for _, tc := range []struct {
		name, category, description, expected string
		ctx                                   func(*testing.T) context.Context
		setup                                 func(*fakeUMAD)
		want                                  []error
		reply                                 bool // a valid reply is returned, even with err
		check                                 func(*testing.T, *fakeUMAD)
	}{
		// The assessment's umad transport table.
		{"reply", "positive", "Valid GetResp for the request", "PortInfo decoded as by the cgo fixture",
			background, nil, nil, true, nil},
		{"tid_rewritten", "positive", "Upper TID half changed by the kernel", "accepted",
			background, func(f *fakeUMAD) {
				f.answer = func(b []byte) []byte { b[umadHdrLen+8] ^= 0xff; return b }
			}, nil, true, nil},
		{"timeout_status", "negative", "Header with status ETIMEDOUT and the 24-byte MAD header", "DeadlineExceeded; agent unregistered, fd closed",
			background, func(f *fakeUMAD) { f.answer = status(uint32(syscall.ETIMEDOUT), umadHdrLen+24) },
			[]error{context.DeadlineExceeded}, false, nil},
		{"short_read", "negative", "Reply of 255 MAD bytes", "EPROTO, cleanup balanced",
			background, func(f *fakeUMAD) { f.answer = status(0, full-1) }, []error{syscall.EPROTO}, false, nil},
		{"wrong_agent", "negative", "Reply for another agent id", "EPROTO",
			background, func(f *fakeUMAD) {
				f.answer = func(b []byte) []byte { b[umadHdrOffID]++; return b }
			}, []error{syscall.EPROTO}, false, nil},
		{"register_enotsupp", "negative", "Registration returns EPROTONOSUPPORT, as on RoCE", "EPROTONOSUPPORT, which the collector reports as unsupported",
			background, func(f *fakeUMAD) { f.registerErr = syscall.EPROTONOSUPPORT }, []error{syscall.EPROTONOSUPPORT}, false,
			func(t *testing.T, f *fakeUMAD) {
				if f.registered != 0 || f.unregistered != 0 {
					t.Fatal("unregistered an agent never registered")
				}
			}},
		{"deadline_zero", "boundary", "Context already expired", "DeadlineExceeded with no resolve or open",
			func(t *testing.T) context.Context {
				ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
				t.Cleanup(cancel)
				return ctx
			}, nil, []error{context.DeadlineExceeded}, false,
			func(t *testing.T, f *fakeUMAD) {
				if f.resolved != 0 || f.opened != 0 {
					t.Fatalf("resolved %d, opened %d", f.resolved, f.opened)
				}
			}},
		{"deadline_cap", "boundary", "Context deadline an hour away", "send timeout and first poll 250 ms",
			within(time.Hour), nil, nil, true,
			func(t *testing.T, f *fakeUMAD) {
				if f.sentTimeout != 250 || len(f.polls) == 0 || f.polls[0] != 250 {
					t.Fatalf("timeout %d, polls %v", f.sentTimeout, f.polls)
				}
			}},
		{"close_error", "corner", "close fails after a good reply", "reply kept, close error reported",
			background, func(f *fakeUMAD) { f.closeErr = syscall.EIO }, []error{syscall.EIO}, true, nil},
		// The twelve rows of nix/tests/linkmonitor-umad-test.c. FLAGS and
		// ALLOC have no Go step of their own: O_CLOEXEC is an open flag and
		// the buffers are one Go allocation. FLAGS maps to ENABLE_PKEY, the
		// setup between open and registration, and ALLOC to a short write,
		// the failure between registration and a sent request.
		{"c_none", "positive", "C NONE: successful exchange", "copy, then unregister and close",
			background, nil, nil, true, nil},
		{"c_open", "negative", "C OPEN: device permission denied", "EACCES; nothing to close",
			background, func(f *fakeUMAD) { f.openErr = syscall.EACCES }, []error{syscall.EACCES}, false, nil},
		{"c_flags", "negative", "C FLAGS, as ENABLE_PKEY failing", "EIO; fd closed, nothing registered",
			background, func(f *fakeUMAD) { f.pkeyErr = syscall.EIO }, []error{syscall.EIO}, false,
			func(t *testing.T, f *fakeUMAD) {
				if f.registered != 0 {
					t.Fatal("registered after ENABLE_PKEY failed")
				}
			}},
		{"c_register", "negative", "C REGISTER: registration denied", "EPERM; close without unregister",
			background, func(f *fakeUMAD) { f.registerErr = syscall.EPERM }, []error{syscall.EPERM}, false, nil},
		{"c_alloc", "boundary", "C ALLOC, as a short write", "EIO; unregister and close",
			background, func(f *fakeUMAD) { f.shortWrite = true }, []error{syscall.EIO}, false, nil},
		{"c_send", "negative", "C SEND: write fails", "EIO; every resource released once",
			background, func(f *fakeUMAD) { f.writeErr = syscall.EIO }, []error{syscall.EIO}, false, nil},
		{"c_receive", "boundary", "C RECEIVE: no reply within the 250 ms cap", "ETIMEDOUT after a bounded wait; cleanup",
			background, func(f *fakeUMAD) { f.answer = func([]byte) []byte { return nil } }, []error{syscall.ETIMEDOUT}, false, nil},
		{"c_agent", "negative", "C AGENT: wrong response agent", "EPROTO; cleanup",
			background, func(f *fakeUMAD) {
				f.answer = func(b []byte) []byte { b[umadHdrOffID] += 2; return b }
			}, []error{syscall.EPROTO}, false, nil},
		{"c_length", "boundary", "C LENGTH: 255-byte response", "EPROTO",
			background, func(f *fakeUMAD) { f.answer = status(0, full-1) }, []error{syscall.EPROTO}, false, nil},
		{"c_status", "negative", "C STATUS: kernel transport status EIO", "EIO preserved",
			background, func(f *fakeUMAD) { f.answer = status(uint32(syscall.EIO), full) }, []error{syscall.EIO}, false, nil},
		{"c_unregister", "corner", "C UNREGISTER: agent cleanup fails", "still closed; reply kept with the cleanup error",
			background, func(f *fakeUMAD) { f.unregisterErr = syscall.EIO }, []error{syscall.EIO}, true, nil},
		{"c_close", "corner", "C CLOSE: fd close fails", "reply kept with the cleanup error, no retry",
			background, func(f *fakeUMAD) { f.closeErr = syscall.EIO }, []error{syscall.EIO}, true, nil},
		// Resolution, before any descriptor.
		{"resolve_error", "negative", "GET_CHARDEV finds no such device", "ENODEV; nothing opened",
			background, func(f *fakeUMAD) { f.resolveErr = syscall.ENODEV }, []error{syscall.ENODEV}, false, nil},
		{"not_umad", "negative", "The reply names uverbs0", "ErrReply; nothing opened",
			background, func(f *fakeUMAD) { f.name = "uverbs0" }, []error{ErrReply}, false, nil},
		{"umad_no_digits", "boundary", "The reply names umad", "ErrReply; nothing opened",
			background, func(f *fakeUMAD) { f.name = "umad" }, []error{ErrReply}, false, nil},
		{"umad_suffix", "corner", "The reply names umad1x", "ErrReply; nothing opened",
			background, func(f *fakeUMAD) { f.name = "umad1x" }, []error{ErrReply}, false, nil},
		{"abi_old", "boundary", "umad ABI 4", "EOPNOTSUPP, as libibumad; nothing opened",
			background, func(f *fakeUMAD) { f.abi = umadABIVersion - 1 }, []error{syscall.EOPNOTSUPP}, false, nil},
		{"abi_new", "corner", "umad ABI 6", "no ENABLE_PKEY: a later ABI has the 64-byte header",
			background, func(f *fakeUMAD) { f.abi = umadABIVersion + 1 }, nil, true,
			func(t *testing.T, f *fakeUMAD) {
				if f.pkey != 0 {
					t.Fatal("ENABLE_PKEY on ABI 6")
				}
			}},
		{"identity", "negative", "The node's dev_t is not the one GET_CHARDEV named", "ErrReply wrapping linuxio.ErrIdentity",
			background, func(f *fakeUMAD) { f.openErr = linuxio.ErrIdentity }, []error{ErrReply, linuxio.ErrIdentity}, false, nil},
		{"deadline_short", "boundary", "Context deadline 30 ms away and no reply", "DeadlineExceeded within the context's deadline",
			within(30 * time.Millisecond), func(f *fakeUMAD) { f.answer = func([]byte) []byte { return nil } },
			[]error{context.DeadlineExceeded}, false,
			func(t *testing.T, f *fakeUMAD) {
				if f.sentTimeout > 30 || len(f.polls) == 0 || f.polls[0] > 30 {
					t.Fatalf("timeout %d, polls %v", f.sentTimeout, f.polls)
				}
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			f := newFakeUMAD(t)
			if tc.setup != nil {
				tc.setup(f)
			}
			req := request(1)
			reply, err := exchangeUMAD(tc.ctx(t), f, "mlx5_0", 1, req)
			for _, want := range tc.want {
				if !errors.Is(err, want) {
					t.Fatalf("%s: got %v, want %v", tc.expected, err, want)
				}
			}
			if len(tc.want) == 0 && err != nil {
				t.Fatalf("%s: %v", tc.expected, err)
			}
			if got := validateReply(req, reply) == nil; got != tc.reply {
				t.Fatalf("%s: valid reply %v", tc.expected, got)
			}
			if f.opened != f.closed || f.registered != f.unregistered || len(f.peers) != 0 {
				t.Fatalf("%s: opened %d closed %d, registered %d unregistered %d", tc.expected, f.opened, f.closed, f.registered, f.unregistered)
			}
			if tc.check != nil {
				tc.check(t, f)
			}
		})
	}
}

// TestExchangeNative covers the kernel-backed entry point up to its first
// system call: a context that is already done never reaches the device.
func TestExchangeNative(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	for _, tc := range []struct {
		name, category, description, expected string
		ctx                                   context.Context
		want                                  error
	}{
		{"canceled", "negative", "Context canceled before the exchange", "context.Canceled, no system call", canceled, context.Canceled},
		{"expired", "boundary", "Context deadline already passed", "DeadlineExceeded, no system call", expired, context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			if _, err := exchangeNative(tc.ctx, "mlx5_0", 1, request(1)); !errors.Is(err, tc.want) {
				t.Fatalf("%s: got %v", tc.expected, err)
			}
		})
	}
}

// TestUMADQueryNative runs Query's path, query then exchangeUMAD, so the
// reply row's capabilities are the cgo fixture's.
func TestUMADQueryNative(t *testing.T) {
	f := newFakeUMAD(t)
	got, err := query(t.Context(), "mlx5_0", 1, func(ctx context.Context, device string, port uint32, req [256]byte) ([256]byte, error) {
		return exchangeUMAD(ctx, f, device, port, req)
	})
	if err != nil || got.Supported != 7 || got.Enabled != 4 || got.Active != 4 || !got.Complete {
		t.Fatalf("positive: query over the fake device; expected: the cgo fixture's capabilities; got %+v, %v", got, err)
	}
}

// TestUMADParallelPorts is the parallel_ports row. Each exchange's request is
// answered only once both have been written, so a lane serializing them would
// never finish.
func TestUMADParallelPorts(t *testing.T) {
	t.Log("corner: two ports queried at once; expected: both succeed concurrently, no shared lock")
	f := newFakeUMAD(t)
	var both sync.WaitGroup
	both.Add(2)
	f.written = func() {
		both.Done()
		done := make(chan struct{})
		go func() { both.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("the other exchange never wrote: exchanges are serialized")
		}
	}
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			req := request(1)
			reply, err := exchangeUMAD(t.Context(), f, "mlx5_0", 1, req)
			errs <- errors.Join(err, validateReply(req, reply))
		}()
	}
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if f.opened != 2 || f.closed != 2 || f.registered != 2 || f.unregistered != 2 {
		t.Fatalf("opened %d closed %d registered %d unregistered %d", f.opened, f.closed, f.registered, f.unregistered)
	}
}

// TestUMADExchangeAllocs holds an exchange to the one allocation of its
// umadExchange; the fake allocates nothing per exchange.
func TestUMADExchangeAllocs(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expected string
		setup                                 func(*fakeUMAD)
		max                                   float64
	}{
		{"reply", "positive", "A good exchange", "1 alloc: the umadExchange", nil, 1},
		{"register_enotsupp", "negative", "Registration refused, as on RoCE", "1 alloc", func(f *fakeUMAD) { f.registerErr = syscall.EPROTONOSUPPORT }, 1},
		{"abi_old", "boundary", "Refused before open", "1 alloc", func(f *fakeUMAD) { f.abi = umadABIVersion - 1 }, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			f := newFakeUMAD(t)
			if tc.setup != nil {
				tc.setup(f)
			}
			req := request(1)
			ctx := context.Background()
			if got := testing.AllocsPerRun(50, func() { _, _ = exchangeUMAD(ctx, f, "mlx5_0", 1, req) }); got > tc.max {
				t.Fatalf("%s: %.1f allocs", tc.expected, got)
			}
		})
	}
}

// BenchmarkUMADExchange is one exchange over the fake device: the ioctls are
// scripted, the write, poll, read and close are real syscalls on a
// socketpair, and the fake's kernel side answers within the write. Its
// sub-benchmarks are named as BenchmarkPerformanceUMAD's, the cgo baseline,
// so benchstat pairs them; contended runs one exchange per goroutine at once,
// which the cgo lane serializes and native does not (the fake's own lock
// still serializes its bookkeeping).
func BenchmarkUMADExchange(b *testing.B) {
	if runtime.GOMAXPROCS(0) > fakeSlots {
		b.Skipf("GOMAXPROCS %d exceeds the fake's %d descriptors", runtime.GOMAXPROCS(0), fakeSlots)
	}
	for _, parallel := range []bool{false, true} {
		name := "uncontended"
		if parallel {
			name = "contended"
		}
		b.Run(name, func(b *testing.B) {
			f := newFakeUMAD(b)
			req := request(1)
			b.ReportAllocs()
			if !parallel {
				for b.Loop() {
					if _, err := exchangeUMAD(b.Context(), f, "mlx5_0", 1, req); err != nil {
						b.Fatal(err)
					}
				}
				return
			}
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					if _, err := exchangeUMAD(b.Context(), f, "mlx5_0", 1, req); err != nil {
						b.Error(err)
						return
					}
				}
			})
		})
	}
}
