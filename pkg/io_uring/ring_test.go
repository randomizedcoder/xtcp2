package io_uring

import (
	"bytes"
	"runtime"
	"syscall"
	"testing"
	"time"
)

// socketpair returns a pair of connected AF_UNIX SOCK_DGRAM fds. Datagram
// boundaries are preserved (unlike pipe(2)), which is exactly what netlink
// behaves like, so tests using this pair mimic real netlink semantics.
func socketpair(t testing.TB) (int, int) {
	t.Helper()
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_DGRAM, 0)
	if err != nil {
		t.Fatalf("socketpair: %v", err)
	}
	t.Cleanup(func() {
		_ = syscall.Close(fds[0])
		_ = syscall.Close(fds[1])
	})
	return fds[0], fds[1]
}

func newTestRing(t testing.TB, recvBatch int) *Ring {
	t.Helper()
	if recvBatch < 1 {
		recvBatch = 8
	}
	r, err := New(Config{RecvBatchSize: recvBatch, CQEBatchSize: 32})
	if err != nil {
		// Probe failure / kernel-too-old / io_uring disabled — skip so
		// CI on older kernels doesn't fail the suite.
		t.Skipf("io_uring not available on this kernel: %v", err)
	}
	t.Cleanup(func() {
		r.Close(100*time.Millisecond, nil)
	})
	return r
}

// allocBuf returns a fresh *[]byte of size n for tests; mimics a pool
// borrow but without the pool plumbing.
func allocBuf(n int) *[]byte {
	b := make([]byte, n)
	return &b
}

func TestRecvSingleDatagram(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread() //nolint:forbidigo // safe: ring test pins to one thread for io_uring SQE/CQE consistency, no netns mutation

	r := newTestRing(t, 4)
	srv, cli := socketpair(t)

	// Submit one recv SQE before any data is on the wire.
	buf := allocBuf(4096)
	reqID, err := r.EnqueueRecvMsg(cli, buf)
	if err != nil {
		t.Fatalf("EnqueueRecvMsg: %v", err)
	}
	if _, serr := r.Submit(); serr != nil {
		t.Fatalf("Submit: %v", serr)
	}

	payload := []byte("hello-netlink-shaped-bytes")
	if _, werr := syscall.Write(srv, payload); werr != nil {
		t.Fatalf("syscall.Write: %v", werr)
	}

	results, err := r.WaitOne()
	if err != nil {
		t.Fatalf("WaitOne: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	res := results[0]
	if res.Op != OpRead {
		t.Errorf("op=%d want OpRead", res.Op)
	}
	if res.Res != int32(len(payload)) {
		t.Errorf("res=%d want %d", res.Res, len(payload))
	}
	if !bytes.Equal((*res.Buf)[:res.Res], payload) {
		t.Errorf("payload mismatch: got %q want %q", (*res.Buf)[:res.Res], payload)
	}
	if r.InFlightLen() != 0 {
		t.Errorf("in-flight len=%d, want 0", r.InFlightLen())
	}
	_ = reqID
}

func TestRecvMultipleDatagrams(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread() //nolint:forbidigo // safe: ring test pins to one thread for io_uring SQE/CQE consistency, no netns mutation

	r := newTestRing(t, 16)
	srv, cli := socketpair(t)

	const n = 3
	for i := 0; i < n; i++ {
		if _, err := r.EnqueueRecvMsg(cli, allocBuf(4096)); err != nil {
			t.Fatalf("EnqueueRecvMsg[%d]: %v", i, err)
		}
	}
	if _, err := r.Submit(); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	payloads := [][]byte{
		[]byte("first"),
		[]byte("second-record-with-more-bytes"),
		[]byte("third"),
	}
	for _, p := range payloads {
		if _, err := syscall.Write(srv, p); err != nil {
			t.Fatalf("syscall.Write: %v", err)
		}
	}

	gotN := 0
	deadline := time.Now().Add(2 * time.Second)
	got := make([][]byte, 0, n)
	for gotN < n && time.Now().Before(deadline) {
		results, err := r.WaitOne()
		if err != nil {
			t.Fatalf("WaitOne: %v", err)
		}
		for _, res := range results {
			if res.Op != OpRead {
				t.Errorf("op=%d want OpRead", res.Op)
			}
			if res.Res <= 0 {
				t.Errorf("res=%d want > 0", res.Res)
				continue
			}
			cp := make([]byte, res.Res)
			copy(cp, (*res.Buf)[:res.Res])
			got = append(got, cp)
			gotN++
		}
	}
	if gotN != n {
		t.Fatalf("got %d records, want %d", gotN, n)
	}
	// AF_UNIX SOCK_DGRAM preserves order across one socketpair, so
	// results come back in submission order.
	for i, p := range payloads {
		if !bytes.Equal(got[i], p) {
			t.Errorf("payload[%d] mismatch: got %q want %q", i, got[i], p)
		}
	}
	if r.InFlightLen() != 0 {
		t.Errorf("in-flight len=%d, want 0", r.InFlightLen())
	}
}

func TestSendSingle(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread() //nolint:forbidigo // safe: ring test pins to one thread for io_uring SQE/CQE consistency, no netns mutation

	r := newTestRing(t, 4)
	srv, cli := socketpair(t)

	payload := []byte("test-send-via-iouring")
	buf := &payload
	if _, err := r.EnqueueSend(cli, buf, OpSendUnixGram); err != nil {
		t.Fatalf("EnqueueSend: %v", err)
	}
	if _, err := r.Submit(); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	results, err := r.WaitOne()
	if err != nil {
		t.Fatalf("WaitOne: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Res != int32(len(payload)) {
		t.Errorf("send Res=%d want %d", results[0].Res, len(payload))
	}

	rcv := make([]byte, 4096)
	n, err := syscall.Read(srv, rcv)
	if err != nil {
		t.Fatalf("syscall.Read: %v", err)
	}
	if !bytes.Equal(rcv[:n], payload) {
		t.Errorf("received %q want %q", rcv[:n], payload)
	}
	if r.InFlightLen() != 0 {
		t.Errorf("in-flight len=%d, want 0", r.InFlightLen())
	}
}

func TestSendBatch(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread() //nolint:forbidigo // safe: ring test pins to one thread for io_uring SQE/CQE consistency, no netns mutation

	r := newTestRing(t, 256)
	srv, cli := socketpair(t)

	const n = 100
	bufs := make([]*[]byte, n)
	for i := 0; i < n; i++ {
		p := []byte("batch-record-")
		p = append(p, byte('a'+(i%26)))
		bufs[i] = &p
		if _, err := r.EnqueueSend(cli, bufs[i], OpSendUnixGram); err != nil {
			t.Fatalf("EnqueueSend[%d]: %v", i, err)
		}
	}
	// One Submit for the whole batch — the io_uring point.
	if _, err := r.Submit(); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	// Drain receiver in a goroutine so the writer doesn't block on a full
	// kernel buffer. socketpair() defaults around 200KB; 100 small records
	// shouldn't overflow, but be safe.
	doneRecv := make(chan int, 1)
	go func() {
		count := 0
		buf := make([]byte, 4096)
		for count < n {
			if _, err := syscall.Read(srv, buf); err != nil {
				doneRecv <- count
				return
			}
			count++
		}
		doneRecv <- count
	}()

	// Reap all n CQEs.
	deadline := time.Now().Add(2 * time.Second)
	completions := 0
	for completions < n && time.Now().Before(deadline) {
		results, err := r.WaitOne()
		if err != nil {
			t.Fatalf("WaitOne: %v", err)
		}
		completions += len(results)
	}
	if completions != n {
		t.Errorf("got %d CQEs want %d", completions, n)
	}

	got := <-doneRecv
	if got != n {
		t.Errorf("receiver got %d records want %d", got, n)
	}
	if r.InFlightLen() != 0 {
		t.Errorf("in-flight len=%d, want 0", r.InFlightLen())
	}
}

func TestWritevUnixStream(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread() //nolint:forbidigo // safe: ring test pins to one thread for io_uring SQE/CQE consistency, no netns mutation

	// Need SOCK_STREAM for writev semantics; socketpair() above is DGRAM.
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatalf("socketpair stream: %v", err)
	}
	t.Cleanup(func() { _ = syscall.Close(fds[0]); _ = syscall.Close(fds[1]) })
	srv, cli := fds[0], fds[1]

	r := newTestRing(t, 4)

	header := []byte{0x12} // varint(18)
	payload := []byte("the-eighteen-bytes")
	buf := &payload
	if _, eerr := r.EnqueueWritevUnix(cli, header, buf); eerr != nil {
		t.Fatalf("EnqueueWritevUnix: %v", eerr)
	}
	if _, serr := r.Submit(); serr != nil {
		t.Fatalf("Submit: %v", serr)
	}

	results, err := r.WaitOne()
	if err != nil {
		t.Fatalf("WaitOne: %v", err)
	}
	if len(results) != 1 || results[0].Op != OpSendUnix {
		t.Fatalf("got %+v, want one OpSendUnix CQE", results)
	}
	wantBytes := len(header) + len(payload)
	if results[0].Res != int32(wantBytes) {
		t.Errorf("writev Res=%d want %d", results[0].Res, wantBytes)
	}

	// Receiver should see header + payload concatenated.
	rcv := make([]byte, 4096)
	n, err := syscall.Read(srv, rcv)
	if err != nil {
		t.Fatalf("syscall.Read: %v", err)
	}
	got := rcv[:n]
	wantConcat := append(append([]byte{}, header...), payload...)
	if !bytes.Equal(got, wantConcat) {
		t.Errorf("stream got %q want %q", got, wantConcat)
	}
	if r.InFlightLen() != 0 {
		t.Errorf("in-flight len=%d, want 0", r.InFlightLen())
	}
}

func TestInFlightCapEnforced(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread() //nolint:forbidigo // safe: ring test pins to one thread for io_uring SQE/CQE consistency, no netns mutation

	r := newTestRing(t, 4) // sqEntries clamped to 256, in-flight cap = 512
	_, cli := socketpair(t)

	// Submit enough recvs to blow past the in-flight cap. Don't drain.
	hit := false
	for i := 0; i < r.inFlightCap+2; i++ {
		if _, err := r.EnqueueRecvMsg(cli, allocBuf(64)); err != nil {
			hit = true
			break
		}
	}
	if !hit {
		t.Fatalf("expected EnqueueRecvMsg to refuse past in-flight cap=%d", r.inFlightCap)
	}
}

func TestTeardownDrainsCleanly(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread() //nolint:forbidigo // safe: ring test pins to one thread for io_uring SQE/CQE consistency, no netns mutation

	r, err := New(Config{RecvBatchSize: 4, CQEBatchSize: 8})
	if err != nil {
		t.Skipf("io_uring not available: %v", err)
	}
	srv, cli := socketpair(t)

	if _, eerr := r.EnqueueRecvMsg(cli, allocBuf(64)); eerr != nil {
		t.Fatalf("EnqueueRecvMsg: %v", eerr)
	}
	if _, serr := r.Submit(); serr != nil {
		t.Fatalf("Submit: %v", serr)
	}
	if _, werr := syscall.Write(srv, []byte("x")); werr != nil {
		t.Fatalf("Write: %v", werr)
	}

	var drained int
	r.Close(500*time.Millisecond, func(Result) { drained++ })
	if drained != 1 {
		t.Errorf("Close drained %d CQEs, want 1", drained)
	}
}

// Bug 47 regression: in-flight SQEs that don't get a CQE within the
// drain deadline must still be handed back via onDrain so the caller
// can return the buffers to the packet pool. Previously the buffers
// were silently abandoned at QueueExit, causing one leaked packet-pool
// buffer per outstanding recvmsg SQE.
func TestTeardownReleasesUnacknowledgedBuffers(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread() //nolint:forbidigo // safe: ring test pins to one thread for io_uring SQE/CQE consistency, no netns mutation

	r, err := New(Config{RecvBatchSize: 4, CQEBatchSize: 8})
	if err != nil {
		t.Skipf("io_uring not available: %v", err)
	}
	srv, cli := socketpair(t)
	_ = srv // we never write — the recv is intentionally never satisfied

	// Submit 3 recvmsg SQEs that will never get a real CQE (we never
	// write to srv). Close's drainTimeout has to expire, and the
	// in-flight cleanup loop has to hand each buffer back.
	for range 3 {
		if _, eerr := r.EnqueueRecvMsg(cli, allocBuf(64)); eerr != nil {
			t.Fatalf("EnqueueRecvMsg: %v", eerr)
		}
	}
	if _, serr := r.Submit(); serr != nil {
		t.Fatalf("Submit: %v", serr)
	}

	var drained int
	var nonNilBufs int
	r.Close(50*time.Millisecond, func(res Result) {
		drained++
		if res.Buf != nil {
			nonNilBufs++
		}
	})
	if drained != 3 {
		t.Errorf("drained = %d, want 3", drained)
	}
	if nonNilBufs != 3 {
		t.Errorf("non-nil Bufs = %d, want 3 (all in-flight bufs must be released)", nonNilBufs)
	}
}
