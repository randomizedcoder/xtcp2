package xtcp

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	xio "github.com/randomizedcoder/xtcp2/pkg/io_uring"
	"github.com/randomizedcoder/xtcp2/pkg/xtcp_config"
)

// xioRingNew creates a Ring sized small enough for tests. Returns the
// New error so tests can Skip when the kernel doesn't support io_uring.
func xioRingNew(t testing.TB) (*xio.Ring, error) {
	t.Helper()
	return xio.New(xio.Config{RecvBatchSize: 4, CQEBatchSize: 16})
}

// withRing stashes a Ring under the ringCtxKey so io_uring destination
// functions can find it. Mirrors what netlinkerIoUring does in prod.
func withRing(ctx context.Context, r *xio.Ring) context.Context {
	return context.WithValue(ctx, ringCtxKey{}, r)
}

// destSetupResult is what each row's setup closure returns.
type destSetupResult struct {
	dest    string // value to assign to x.config.Dest, e.g. "udp:127.0.0.1:12345"
	recv    func() ([]byte, error)
	cleanup func()
}

// destCase describes one row of the destination-round-trip table.
type destCase struct {
	name        string
	scheme      string // "null", "udp", "unix", "unixgram"
	setup       func(t *testing.T, dir string) destSetupResult
	expectFrame func(payload []byte) []byte // identity for null/udp/unixgram; varint-prefixed for unix
}

// newTestXTCP builds the minimal XTCP fixture needed to drive a destination:
// fresh Prometheus registry (so counters don't collide across rows), the
// destination function maps populated by InitDests' first half, and a fatalf
// hook that flips startup failures into t.Fatalf.
func newTestXTCP(t *testing.T, dest string) *XTCP {
	t.Helper()

	x := new(XTCP)
	x.config = &xtcp_config.XtcpConfig{Dest: dest}
	x.debugLevel = 0
	x.fatalf = func(format string, args ...any) {
		t.Fatalf(format, args...)
	}

	reg := prometheus.NewRegistry()
	x.pC = promauto.With(reg).NewCounterVec(
		prometheus.CounterOpts{Subsystem: "xtcp_test", Name: promNameCounts, Help: "test counts"},
		promLabels,
	)
	x.pH = promauto.With(reg).NewSummaryVec(
		prometheus.SummaryOpts{
			Subsystem: "xtcp_test", Name: promNameHistograms, Help: "test histograms",
			Objectives: map[float64]float64{0.5: quantileError, 0.99: quantileError},
			MaxAge:     summaryVecMaxAge,
		},
		promLabels,
	)

	return x
}

// setupNullDest returns a no-op setup — destNull doesn't need a listener.
func setupNullDest(_ *testing.T, _ string) destSetupResult {
	return destSetupResult{
		dest:    "null:",
		recv:    func() ([]byte, error) { return nil, nil },
		cleanup: func() {},
	}
}

// setupUDPDest spins up a UDP listener on a free localhost port, returns a
// recv() closure that reads one datagram with a short deadline.
func setupUDPDest(t *testing.T, _ string) destSetupResult {
	t.Helper()

	var lc net.ListenConfig
	pc, err := lc.ListenPacket(context.Background(), "udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket udp: %v", err)
	}
	addr := pc.LocalAddr().String()

	return destSetupResult{
		dest: "udp:" + addr,
		recv: func() ([]byte, error) {
			if derr := pc.SetReadDeadline(time.Now().Add(2 * time.Second)); derr != nil {
				return nil, derr
			}
			buf := make([]byte, 1<<16)
			n, _, rerr := pc.ReadFrom(buf)
			if rerr != nil {
				return nil, rerr
			}
			return buf[:n], nil
		},
		cleanup: func() { _ = pc.Close() },
	}
}

// setupUnixGramDest creates a SOCK_DGRAM Unix socket under dir, listens for
// datagrams. recv() reads one datagram with a deadline.
func setupUnixGramDest(t *testing.T, dir string) destSetupResult {
	t.Helper()

	path := filepath.Join(dir, "ug.sock")
	addr := &net.UnixAddr{Name: path, Net: "unixgram"}
	conn, err := net.ListenUnixgram("unixgram", addr)
	if err != nil {
		t.Fatalf("ListenUnixgram %s: %v", path, err)
	}

	return destSetupResult{
		dest: "unixgram:" + path,
		recv: func() ([]byte, error) {
			if derr := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); derr != nil {
				return nil, derr
			}
			buf := make([]byte, 1<<16)
			n, _, rerr := conn.ReadFromUnix(buf)
			if rerr != nil {
				return nil, rerr
			}
			return buf[:n], nil
		},
		cleanup: func() { _ = conn.Close() },
	}
}

// setupUnixDest creates a SOCK_STREAM Unix socket listener and accepts a
// single client connection in a goroutine. recv() reads one length-prefixed
// (varint) record off that connection.
func setupUnixDest(t *testing.T, dir string) destSetupResult {
	t.Helper()

	path := filepath.Join(dir, "u.sock")
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "unix", path)
	if err != nil {
		t.Fatalf("Listen unix %s: %v", path, err)
	}

	connCh := make(chan net.Conn, 1)
	errCh := make(chan error, 1)
	go func() {
		c, aerr := ln.Accept()
		if aerr != nil {
			errCh <- aerr
			return
		}
		connCh <- c
	}()

	var (
		clientConn net.Conn
		acceptOnce sync.Once
	)
	getConn := func() (net.Conn, error) {
		var firstErr error
		acceptOnce.Do(func() {
			select {
			case c := <-connCh:
				clientConn = c
			case eerr := <-errCh:
				firstErr = eerr
			case <-time.After(2 * time.Second):
				firstErr = fmt.Errorf("timeout waiting for client to dial unix socket")
			}
		})
		if firstErr != nil {
			return nil, firstErr
		}
		return clientConn, nil
	}

	return destSetupResult{
		dest: "unix:" + path,
		recv: func() ([]byte, error) {
			c, gerr := getConn()
			if gerr != nil {
				return nil, gerr
			}
			if derr := c.SetReadDeadline(time.Now().Add(2 * time.Second)); derr != nil {
				return nil, derr
			}
			br := newByteReader(c)
			length, lerr := binary.ReadUvarint(br)
			if lerr != nil {
				return nil, fmt.Errorf("read varint: %w", lerr)
			}
			payload := make([]byte, length)
			if _, rerr := io.ReadFull(c, payload); rerr != nil {
				return nil, fmt.Errorf("read payload: %w", rerr)
			}
			return payload, nil
		},
		cleanup: func() {
			if clientConn != nil {
				_ = clientConn.Close()
			}
			_ = ln.Close()
		},
	}
}

// byteReader adapts a net.Conn to io.ByteReader so binary.ReadUvarint can
// consume the length prefix one byte at a time without over-reading into the
// payload.
type byteReader struct{ r io.Reader }

func newByteReader(r io.Reader) *byteReader { return &byteReader{r: r} }

func (br *byteReader) ReadByte() (byte, error) {
	var b [1]byte
	if _, err := io.ReadFull(br.r, b[:]); err != nil {
		return 0, err
	}
	return b[0], nil
}

// runDestRow exercises one row: build the registered destination via its
// factory, write payload(s) through dest.Send, verify what landed on the
// receiver matches what was written.
func runDestRow(t *testing.T, c destCase, payloads [][]byte) {
	t.Helper()

	dir := t.TempDir()
	setup := c.setup(t, dir)
	defer setup.cleanup()

	x := newTestXTCP(t, setup.dest)
	ctx := context.Background()

	factory, status := lookupDestinationFactory(c.scheme)
	if status != destLookupFound {
		t.Fatalf("scheme %q not registered: status %v", c.scheme, status)
	}
	dest, err := factory(ctx, x)
	if err != nil {
		t.Fatalf("factory(%s): %v", c.scheme, err)
	}
	x.dest = dest
	defer x.closeDestination()

	for i, payload := range payloads {
		buf := append([]byte(nil), payload...)
		n, serr := dest.Send(ctx, &buf)
		if serr != nil {
			t.Fatalf("payload[%d] Send err: %v", i, serr)
		}
		if c.scheme == schemeNull {
			if n != len(payload) {
				t.Errorf("payload[%d] null n=%d want=%d", i, n, len(payload))
			}
			continue
		}
		if n != 1 {
			t.Errorf("payload[%d] Send n=%d want=1", i, n)
		}

		got, rerr := setup.recv()
		if rerr != nil {
			t.Fatalf("payload[%d] recv err: %v", i, rerr)
		}
		want := c.expectFrame(payload)
		if !bytes.Equal(got, want) {
			t.Errorf("payload[%d] bytes mismatch\n got: %x\nwant: %x", i, got, want)
		}
	}
}

// TestDestinations exercises every destination we can stand up with stdlib
// only: null, udp, unix, unixgram. Kafka, nsq, nats, valkey are deferred to
// a follow-up that brings in embedded servers / testcontainers.
func TestDestinations(t *testing.T) {
	identity := func(p []byte) []byte { return p }

	cases := []destCase{
		{name: schemeNull, scheme: schemeNull, setup: setupNullDest, expectFrame: identity},
		{name: "udp_round_trip", scheme: schemeUDP, setup: setupUDPDest, expectFrame: identity},
		{name: "udp_multiple", scheme: schemeUDP, setup: setupUDPDest, expectFrame: identity},
		{name: "unixgram_round_trip", scheme: schemeUnixgram, setup: setupUnixGramDest, expectFrame: identity},
		{name: "unixgram_multiple", scheme: schemeUnixgram, setup: setupUnixGramDest, expectFrame: identity},
		// For unix, recv() already strips the varint length prefix and returns
		// the raw payload; the framing is exercised inside recv()'s
		// binary.ReadUvarint + io.ReadFull. So the comparison is identity.
		{name: "unix_round_trip", scheme: schemeUnix, setup: setupUnixDest, expectFrame: identity},
		{name: "unix_multiple", scheme: schemeUnix, setup: setupUnixDest, expectFrame: identity},
	}

	single := [][]byte{[]byte("hello-xtcp2-record")}
	triple := [][]byte{
		[]byte("first record"),
		[]byte("second record with slightly more bytes"),
		[]byte("third"),
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			payloads := single
			switch c.name {
			case "udp_multiple", "unixgram_multiple", "unix_multiple":
				payloads = triple
			}
			runDestRow(t, c, payloads)
		})
	}
}

// TestDestinationsIoUring mirrors TestDestinations but for the io_uring
// destination variants. Each row spins up a real listener, dials, opts
// the XTCP fixture into config.IoUring, drives a per-Netlinker ring, and
// confirms records round-trip via the new code paths. Skipped on kernels
// that don't support the required io_uring opcodes.
func TestDestinationsIoUring(t *testing.T) {
	identity := func(p []byte) []byte { return p }

	cases := []destCase{
		{name: "udp_round_trip_iouring", scheme: schemeUDP, setup: setupUDPDest, expectFrame: identity},
		{name: "udp_multiple_iouring", scheme: schemeUDP, setup: setupUDPDest, expectFrame: identity},
		{name: "unixgram_round_trip_iouring", scheme: schemeUnixgram, setup: setupUnixGramDest, expectFrame: identity},
		{name: "unixgram_multiple_iouring", scheme: schemeUnixgram, setup: setupUnixGramDest, expectFrame: identity},
		{name: "unix_round_trip_iouring", scheme: schemeUnix, setup: setupUnixDest, expectFrame: identity},
		{name: "unix_multiple_iouring", scheme: schemeUnix, setup: setupUnixDest, expectFrame: identity},
	}

	single := [][]byte{[]byte("hello-iouring-record")}
	triple := [][]byte{
		[]byte("io-uring-first"),
		[]byte("io-uring-second-with-more-bytes"),
		[]byte("io-uring-third"),
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			payloads := single
			switch c.name {
			case "udp_multiple_iouring", "unixgram_multiple_iouring", "unix_multiple_iouring":
				payloads = triple
			}
			runIoUringDestRow(t, c, payloads)
		})
	}
}

// runIoUringDestRow drives the io_uring write path end-to-end: spins up
// a Ring, populates the corresponding x.<scheme>FD, calls the io_uring
// destination function (which enqueues an SQE), Submits, then drains
// the CQE and reads back from the listener.
//
// LockOSThread pins this test goroutine for the lifetime of the ring so
// that Go's scheduler can't migrate it across OS threads — io_uring
// state is per-task, and a ring created on thread A submitting from
// thread B can return EEXIST or worse.
func runIoUringDestRow(t *testing.T, c destCase, payloads [][]byte) {
	t.Helper()

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	dir := t.TempDir()
	setup := c.setup(t, dir)
	defer setup.cleanup()

	x := newTestXTCP(t, setup.dest)
	x.config.IoUring = true
	ctx := context.Background()

	// Build the destination via its factory. The udp/unix/unixgram factories
	// dial the socket and (because config.IoUring is true) extract the fd
	// internally so Send takes the io_uring branch.
	factory, status := lookupDestinationFactory(c.scheme)
	if status != destLookupFound {
		t.Fatalf("scheme %q not registered: status %v", c.scheme, status)
	}
	dest, err := factory(ctx, x)
	if err != nil {
		t.Fatalf("factory(%s): %v", c.scheme, err)
	}
	x.dest = dest
	defer x.closeDestination()

	// One ring covers the whole row. Sized small so test exits quickly.
	ring, rerr := xioRingNew(t)
	if rerr != nil {
		t.Skipf("io_uring not available: %v", rerr)
	}
	defer ring.Close(100*time.Millisecond, nil)
	ringCtx := withRing(ctx, ring)

	for i, payload := range payloads {
		buf := append([]byte(nil), payload...)
		n, serr := dest.Send(ringCtx, &buf)
		if serr != nil {
			t.Fatalf("payload[%d] Send err: %v", i, serr)
		}
		if n != 1 {
			t.Errorf("payload[%d] Send n=%d want=1", i, n)
		}
		// Submit + drain the CQE so the receiver can read.
		if _, srerr := ring.Submit(); srerr != nil {
			t.Fatalf("payload[%d] Submit: %v", i, srerr)
		}
		results, werr := ring.WaitOne()
		if werr != nil {
			t.Fatalf("payload[%d] WaitOne: %v", i, werr)
		}
		if len(results) != 1 {
			t.Fatalf("payload[%d] got %d CQEs want 1", i, len(results))
		}
		if results[0].Res < 0 {
			t.Errorf("payload[%d] CQE Res=%d (error)", i, results[0].Res)
		}

		got, gerr := setup.recv()
		if gerr != nil {
			t.Fatalf("payload[%d] recv: %v", i, gerr)
		}
		want := c.expectFrame(payload)
		if !bytes.Equal(got, want) {
			t.Errorf("payload[%d] mismatch\n got: %x\nwant: %x", i, got, want)
		}
	}
	if ring.InFlightLen() != 0 {
		t.Errorf("in-flight len=%d, want 0", ring.InFlightLen())
	}
}

// TestDestUnix_StreamFraming sends records of varying sizes through the
// stream socket and confirms each is recovered intact. Exercises the
// multi-byte varint path (~50KB record produces a 3-byte length prefix).
func TestDestUnix_StreamFraming(t *testing.T) {
	dir := t.TempDir()
	setup := setupUnixDest(t, dir)
	defer setup.cleanup()

	x := newTestXTCP(t, setup.dest)
	ctx := context.Background()
	dest, err := newUnixDest(ctx, x)
	if err != nil {
		t.Fatalf("newUnixDest: %v", err)
	}
	x.dest = dest
	defer x.closeDestination()

	sizes := []int{1, 256, 50 * 1024}
	for _, size := range sizes {
		payload := make([]byte, size)
		for i := range payload {
			payload[i] = byte(i & 0xff)
		}
		buf := append([]byte(nil), payload...)
		if _, serr := dest.Send(ctx, &buf); serr != nil {
			t.Fatalf("size=%d Send err: %v", size, serr)
		}
		got, rerr := setup.recv()
		if rerr != nil {
			t.Fatalf("size=%d recv err: %v", size, rerr)
		}
		if !bytes.Equal(got, payload) {
			t.Errorf("size=%d payload mismatch (first 16 bytes got=%x want=%x)", size, got[:min(16, len(got))], payload[:min(16, len(payload))])
		}
	}
}

// TestDestUnixGram_MissingSocket confirms the unixgram factory returns an
// error when the socket file doesn't exist — that's our "fail loudly at
// startup" contract.
func TestDestUnixGram_MissingSocket(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "does-not-exist.sock")

	x := newTestXTCP(t, "unixgram:"+missing)
	if _, err := newUnixGramDest(context.Background(), x); err == nil {
		t.Fatalf("expected error for missing socket %q", missing)
	}
}

// TestDestUnix_MissingDaemon confirms the unix factory returns an error when
// nothing is listening on the path.
func TestDestUnix_MissingDaemon(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "no-daemon.sock")

	x := newTestXTCP(t, "unix:"+missing)
	if _, err := newUnixDest(context.Background(), x); err == nil {
		t.Fatalf("expected error for missing daemon at %q", missing)
	}
}

// udpDest.Send error path: write to a closed connection returns an error
// without crashing. Exercises the err branch in Send (50% coverage prior).
func TestUDPDest_SendAfterClose(t *testing.T) {
	res := setupUDPDest(t, "")
	defer res.cleanup()

	x := newTestXTCP(t, res.dest)
	d, err := newUDPDest(context.Background(), x)
	if err != nil {
		t.Fatalf("newUDPDest: %v", err)
	}
	if cerr := d.Close(); cerr != nil {
		t.Fatalf("Close: %v", cerr)
	}
	x.debugLevel = 200 // hit the log.Printf branch
	buf := []byte("after-close")
	if _, err := d.Send(context.Background(), &buf); err == nil {
		t.Error("expected error sending after Close")
	}
}

// udpDest.Send io_uring branch with no ring in ctx returns errNoRingInCtx
// without trying to enqueue.
func TestUDPDest_SendIoUringNoRing(t *testing.T) {
	res := setupUDPDest(t, "")
	defer res.cleanup()

	x := newTestXTCP(t, res.dest)
	x.config.IoUring = true
	d, err := newUDPDest(context.Background(), x)
	if err != nil {
		t.Fatalf("newUDPDest: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() }) //nolint:errcheck // test plumbing

	buf := []byte("ioring")
	// Pass a bare ctx (no ring stashed) → expect errNoRingInCtx.
	if _, err := d.Send(context.Background(), &buf); err == nil {
		t.Error("expected errNoRingInCtx when no ring in context")
	}
}

// unixGramDest.Send error path.
func TestUnixGramDest_SendAfterClose(t *testing.T) {
	dir := t.TempDir()
	res := setupUnixGramDest(t, dir)
	defer res.cleanup()

	x := newTestXTCP(t, res.dest)
	d, err := newUnixGramDest(context.Background(), x)
	if err != nil {
		t.Fatalf("newUnixGramDest: %v", err)
	}
	if cerr := d.Close(); cerr != nil {
		t.Fatalf("Close: %v", cerr)
	}
	x.debugLevel = 200
	buf := []byte("after-close")
	if _, err := d.Send(context.Background(), &buf); err == nil {
		t.Error("expected error sending after Close")
	}
}

// unixGramDest.Send io_uring branch with no ring in ctx.
func TestUnixGramDest_SendIoUringNoRing(t *testing.T) {
	dir := t.TempDir()
	res := setupUnixGramDest(t, dir)
	defer res.cleanup()

	x := newTestXTCP(t, res.dest)
	x.config.IoUring = true
	d, err := newUnixGramDest(context.Background(), x)
	if err != nil {
		t.Fatalf("newUnixGramDest: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() }) //nolint:errcheck // test plumbing

	buf := []byte("ioring")
	if _, err := d.Send(context.Background(), &buf); err == nil {
		t.Error("expected errNoRingInCtx when no ring in context")
	}
}

// Benchmarks. Each allocates a 256-byte payload (representative of an xtcp
// record) and runs b.N writes through the destination; a goroutine on the
// receiver side drains so the write side isn't blocked by kernel buffer
// saturation. b.SetBytes() reports per-record throughput.

func benchDest(b *testing.B, scheme string, setup func(t testing.TB, dir string) destSetupResult) {
	b.Helper()

	dir := b.TempDir()
	tb := testingTB{TB: b}
	s := setup(tb, dir)
	defer s.cleanup()

	x := new(XTCP)
	x.config = &xtcp_config.XtcpConfig{Dest: s.dest}
	x.fatalf = func(format string, args ...any) {
		b.Fatalf(format, args...)
	}
	reg := prometheus.NewRegistry()
	x.pC = promauto.With(reg).NewCounterVec(
		prometheus.CounterOpts{Subsystem: "xtcp_bench", Name: promNameCounts, Help: "bench counts"},
		promLabels,
	)
	x.pH = promauto.With(reg).NewSummaryVec(
		prometheus.SummaryOpts{Subsystem: "xtcp_bench", Name: promNameHistograms, Help: "bench histograms"},
		promLabels,
	)

	ctx := context.Background()
	factory, status := lookupDestinationFactory(scheme)
	if status != destLookupFound {
		b.Fatalf("scheme %q not registered: status %v", scheme, status)
	}
	dest, err := factory(ctx, x)
	if err != nil {
		b.Fatalf("factory(%s): %v", scheme, err)
	}
	x.dest = dest
	defer x.closeDestination()

	// Drain receiver in the background so the writer doesn't block on kernel
	// buffer saturation. For "null" there's no recv.
	stop := make(chan struct{})
	if s.recv != nil {
		go func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				_, _ = s.recv()
			}
		}()
	}
	defer close(stop)

	payload := make([]byte, 256)
	for i := range payload {
		payload[i] = byte(i)
	}

	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		buf := append([]byte(nil), payload...)
		if _, serr := dest.Send(ctx, &buf); serr != nil {
			b.Fatalf("write err: %v", serr)
		}
	}
}

// testingTB lets the setup helpers (which take *testing.T) be reused from
// benchmarks. testing.TB is the common interface.
type testingTB struct{ testing.TB }

func (t testingTB) Helper()                           {}
func (t testingTB) TempDir() string                   { return t.TB.TempDir() }
func (t testingTB) Fatalf(format string, args ...any) { t.TB.Fatalf(format, args...) }

// Adapt the *testing.T setup signatures to testing.TB.
func setupNullDestTB(t testing.TB, _ string) destSetupResult {
	return destSetupResult{dest: schemeNullPrefix, recv: nil, cleanup: func() {}}
}
func setupUDPDestTB(t testing.TB, dir string) destSetupResult {
	var lc net.ListenConfig
	pc, err := lc.ListenPacket(context.Background(), "udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket udp: %v", err)
	}
	addr := pc.LocalAddr().String()
	return destSetupResult{
		dest: "udp:" + addr,
		recv: func() ([]byte, error) {
			_ = pc.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			buf := make([]byte, 1<<16)
			n, _, rerr := pc.ReadFrom(buf)
			if rerr != nil {
				return nil, rerr
			}
			return buf[:n], nil
		},
		cleanup: func() { _ = pc.Close() },
	}
}
func setupUnixGramDestTB(t testing.TB, dir string) destSetupResult {
	path := filepath.Join(dir, "ug.sock")
	addr := &net.UnixAddr{Name: path, Net: "unixgram"}
	conn, err := net.ListenUnixgram("unixgram", addr)
	if err != nil {
		t.Fatalf("ListenUnixgram %s: %v", path, err)
	}
	return destSetupResult{
		dest: "unixgram:" + path,
		recv: func() ([]byte, error) {
			_ = conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			buf := make([]byte, 1<<16)
			n, _, rerr := conn.ReadFromUnix(buf)
			if rerr != nil {
				return nil, rerr
			}
			return buf[:n], nil
		},
		cleanup: func() { _ = conn.Close() },
	}
}
func setupUnixDestTB(t testing.TB, dir string) destSetupResult {
	path := filepath.Join(dir, "u.sock")
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "unix", path)
	if err != nil {
		t.Fatalf("Listen unix %s: %v", path, err)
	}
	type connOrErr struct {
		c   net.Conn
		err error
	}
	ch := make(chan connOrErr, 1)
	go func() {
		c, aerr := ln.Accept()
		ch <- connOrErr{c, aerr}
	}()
	var (
		conn       net.Conn
		acceptOnce sync.Once
	)
	getConn := func() net.Conn {
		acceptOnce.Do(func() {
			ce := <-ch
			if ce.err != nil {
				t.Fatalf("Accept: %v", ce.err)
			}
			conn = ce.c
		})
		return conn
	}
	return destSetupResult{
		dest: "unix:" + path,
		recv: func() ([]byte, error) {
			c := getConn()
			_ = c.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			br := newByteReader(c)
			length, lerr := binary.ReadUvarint(br)
			if lerr != nil {
				return nil, lerr
			}
			payload := make([]byte, length)
			if _, rerr := io.ReadFull(c, payload); rerr != nil {
				return nil, rerr
			}
			return payload, nil
		},
		cleanup: func() {
			if conn != nil {
				_ = conn.Close()
			}
			_ = ln.Close()
		},
	}
}

func BenchmarkDestNull(b *testing.B) {
	benchDest(b, schemeNull, setupNullDestTB)
}
func BenchmarkDestUDP(b *testing.B) {
	benchDest(b, schemeUDP, setupUDPDestTB)
}
func BenchmarkDestUnixGram(b *testing.B) {
	benchDest(b, schemeUnixgram, setupUnixGramDestTB)
}
func BenchmarkDestUnix(b *testing.B) {
	benchDest(b, schemeUnix, setupUnixDestTB)
}
