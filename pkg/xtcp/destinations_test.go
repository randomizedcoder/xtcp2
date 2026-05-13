package xtcp

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/randomizedcoder/xtcp2/pkg/xtcp_config"
)

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
		prometheus.CounterOpts{Subsystem: "xtcp_test", Name: "counts", Help: "test counts"},
		[]string{"function", "variable", "type"},
	)
	x.pH = promauto.With(reg).NewSummaryVec(
		prometheus.SummaryOpts{
			Subsystem: "xtcp_test", Name: "histograms", Help: "test histograms",
			Objectives: map[float64]float64{0.5: quantileError, 0.99: quantileError},
			MaxAge:     summaryVecMaxAge,
		},
		[]string{"function", "variable", "type"},
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

	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket udp: %v", err)
	}
	addr := pc.LocalAddr().String()

	return destSetupResult{
		dest: "udp:" + addr,
		recv: func() ([]byte, error) {
			if err := pc.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
				return nil, err
			}
			buf := make([]byte, 1<<16)
			n, _, err := pc.ReadFrom(buf)
			if err != nil {
				return nil, err
			}
			return buf[:n], nil
		},
		cleanup: func() { pc.Close() },
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
			if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
				return nil, err
			}
			buf := make([]byte, 1<<16)
			n, _, err := conn.ReadFromUnix(buf)
			if err != nil {
				return nil, err
			}
			return buf[:n], nil
		},
		cleanup: func() { conn.Close() },
	}
}

// setupUnixDest creates a SOCK_STREAM Unix socket listener and accepts a
// single client connection in a goroutine. recv() reads one length-prefixed
// (varint) record off that connection.
func setupUnixDest(t *testing.T, dir string) destSetupResult {
	t.Helper()

	path := filepath.Join(dir, "u.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("Listen unix %s: %v", path, err)
	}

	connCh := make(chan net.Conn, 1)
	errCh := make(chan error, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			errCh <- err
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
			case err := <-errCh:
				firstErr = err
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
			c, err := getConn()
			if err != nil {
				return nil, err
			}
			if err := c.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
				return nil, err
			}
			br := newByteReader(c)
			length, err := binary.ReadUvarint(br)
			if err != nil {
				return nil, fmt.Errorf("read varint: %w", err)
			}
			payload := make([]byte, length)
			if _, err := io.ReadFull(c, payload); err != nil {
				return nil, fmt.Errorf("read payload: %w", err)
			}
			return payload, nil
		},
		cleanup: func() {
			if clientConn != nil {
				clientConn.Close()
			}
			ln.Close()
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

// runDestRow exercises one row: setup → init → write payload(s) → verify.
func runDestRow(t *testing.T, c destCase, payloads [][]byte) {
	t.Helper()

	dir := t.TempDir()
	setup := c.setup(t, dir)
	defer setup.cleanup()

	x := newTestXTCP(t, setup.dest)
	ctx := context.Background()

	// Register both runtime closures and init closures so we can both dial
	// and write through x.Destination — this mirrors what InitDests does in
	// production for the relevant schemes.
	switch c.scheme {
	case "null":
		x.Destinations.Store("null", func(ctx context.Context, b *[]byte) (int, error) {
			return x.destNull(ctx, b)
		})
		f, _ := x.Destinations.Load("null")
		x.Destination = f.(func(context.Context, *[]byte) (int, error))
	case "udp":
		x.Destinations.Store("udp", func(ctx context.Context, b *[]byte) (int, error) {
			return x.destUDP(ctx, b)
		})
		x.InitDestUDP(ctx)
		f, _ := x.Destinations.Load("udp")
		x.Destination = f.(func(context.Context, *[]byte) (int, error))
	case "unix":
		x.Destinations.Store("unix", func(ctx context.Context, b *[]byte) (int, error) {
			return x.destUnix(ctx, b)
		})
		x.InitDestUnix(ctx)
		f, _ := x.Destinations.Load("unix")
		x.Destination = f.(func(context.Context, *[]byte) (int, error))
	case "unixgram":
		x.Destinations.Store("unixgram", func(ctx context.Context, b *[]byte) (int, error) {
			return x.destUnixGram(ctx, b)
		})
		x.InitDestUnixGram(ctx)
		f, _ := x.Destinations.Load("unixgram")
		x.Destination = f.(func(context.Context, *[]byte) (int, error))
	default:
		t.Fatalf("unknown scheme %q", c.scheme)
	}
	defer x.closeDestination()

	for i, payload := range payloads {
		buf := append([]byte(nil), payload...)
		n, err := x.Destination(ctx, &buf)
		if err != nil {
			t.Fatalf("payload[%d] Destination err: %v", i, err)
		}
		if c.scheme == "null" {
			if n != len(payload) {
				t.Errorf("payload[%d] destNull n=%d want=%d", i, n, len(payload))
			}
			continue
		}
		if n != 1 {
			t.Errorf("payload[%d] Destination n=%d want=1", i, n)
		}

		got, err := setup.recv()
		if err != nil {
			t.Fatalf("payload[%d] recv err: %v", i, err)
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
		{name: "null", scheme: "null", setup: setupNullDest, expectFrame: identity},
		{name: "udp_round_trip", scheme: "udp", setup: setupUDPDest, expectFrame: identity},
		{name: "udp_multiple", scheme: "udp", setup: setupUDPDest, expectFrame: identity},
		{name: "unixgram_round_trip", scheme: "unixgram", setup: setupUnixGramDest, expectFrame: identity},
		{name: "unixgram_multiple", scheme: "unixgram", setup: setupUnixGramDest, expectFrame: identity},
		// For unix, recv() already strips the varint length prefix and returns
		// the raw payload; the framing is exercised inside recv()'s
		// binary.ReadUvarint + io.ReadFull. So the comparison is identity.
		{name: "unix_round_trip", scheme: "unix", setup: setupUnixDest, expectFrame: identity},
		{name: "unix_multiple", scheme: "unix", setup: setupUnixDest, expectFrame: identity},
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

// TestDestUnix_StreamFraming sends records of varying sizes through the
// stream socket and confirms each is recovered intact. Exercises the
// multi-byte varint path (~50KB record produces a 3-byte length prefix).
func TestDestUnix_StreamFraming(t *testing.T) {
	dir := t.TempDir()
	setup := setupUnixDest(t, dir)
	defer setup.cleanup()

	x := newTestXTCP(t, setup.dest)
	ctx := context.Background()
	x.InitDestUnix(ctx)
	defer x.closeDestination()
	x.Destinations.Store("unix", func(ctx context.Context, b *[]byte) (int, error) {
		return x.destUnix(ctx, b)
	})

	sizes := []int{1, 256, 50 * 1024}
	for _, size := range sizes {
		payload := make([]byte, size)
		for i := range payload {
			payload[i] = byte(i & 0xff)
		}
		buf := append([]byte(nil), payload...)
		if _, err := x.destUnix(ctx, &buf); err != nil {
			t.Fatalf("size=%d destUnix err: %v", size, err)
		}
		got, err := setup.recv()
		if err != nil {
			t.Fatalf("size=%d recv err: %v", size, err)
		}
		if !bytes.Equal(got, payload) {
			t.Errorf("size=%d payload mismatch (first 16 bytes got=%x want=%x)", size, got[:min(16, len(got))], payload[:min(16, len(payload))])
		}
	}
}

// TestDestUnixGram_MissingSocket confirms InitDestUnixGram fails when the
// socket file doesn't exist — that's our "fail loudly at startup" contract.
func TestDestUnixGram_MissingSocket(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "does-not-exist.sock")

	x := newTestXTCP(t, "unixgram:"+missing)
	// Override fatalf to capture instead of failing the test.
	var captured string
	x.fatalf = func(format string, args ...any) {
		captured = fmt.Sprintf(format, args...)
	}
	x.InitDestUnixGram(context.Background())
	if captured == "" {
		t.Fatalf("expected fatalf to be called for missing socket %q", missing)
	}
}

// TestDestUnix_MissingDaemon confirms InitDestUnix fails when nothing is
// listening on the path.
func TestDestUnix_MissingDaemon(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "no-daemon.sock")

	x := newTestXTCP(t, "unix:"+missing)
	var captured string
	x.fatalf = func(format string, args ...any) {
		captured = fmt.Sprintf(format, args...)
	}
	x.InitDestUnix(context.Background())
	if captured == "" {
		t.Fatalf("expected fatalf to be called for missing daemon at %q", missing)
	}
}

// Benchmarks. Each allocates a 256-byte payload (representative of an xtcp
// record) and runs b.N writes through the destination; a goroutine on the
// receiver side drains so the write side isn't blocked by kernel buffer
// saturation. b.SetBytes() reports per-record throughput.

func benchDest(b *testing.B, setup func(t testing.TB, dir string) destSetupResult, fn func(*XTCP, context.Context, *[]byte) (int, error)) {
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
		prometheus.CounterOpts{Subsystem: "xtcp_bench", Name: "counts", Help: "bench counts"},
		[]string{"function", "variable", "type"},
	)
	x.pH = promauto.With(reg).NewSummaryVec(
		prometheus.SummaryOpts{Subsystem: "xtcp_bench", Name: "histograms", Help: "bench histograms"},
		[]string{"function", "variable", "type"},
	)

	ctx := context.Background()
	switch {
	case bytes.HasPrefix([]byte(s.dest), []byte("udp:")):
		x.InitDestUDP(ctx)
	case bytes.HasPrefix([]byte(s.dest), []byte("unix:")):
		x.InitDestUnix(ctx)
	case bytes.HasPrefix([]byte(s.dest), []byte("unixgram:")):
		x.InitDestUnixGram(ctx)
	}
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
		if _, err := fn(x, ctx, &buf); err != nil {
			b.Fatalf("write err: %v", err)
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
	return destSetupResult{dest: "null:", recv: nil, cleanup: func() {}}
}
func setupUDPDestTB(t testing.TB, dir string) destSetupResult {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket udp: %v", err)
	}
	addr := pc.LocalAddr().String()
	return destSetupResult{
		dest: "udp:" + addr,
		recv: func() ([]byte, error) {
			pc.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			buf := make([]byte, 1<<16)
			n, _, err := pc.ReadFrom(buf)
			if err != nil {
				return nil, err
			}
			return buf[:n], nil
		},
		cleanup: func() { pc.Close() },
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
			conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			buf := make([]byte, 1<<16)
			n, _, err := conn.ReadFromUnix(buf)
			if err != nil {
				return nil, err
			}
			return buf[:n], nil
		},
		cleanup: func() { conn.Close() },
	}
}
func setupUnixDestTB(t testing.TB, dir string) destSetupResult {
	path := filepath.Join(dir, "u.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("Listen unix %s: %v", path, err)
	}
	type connOrErr struct {
		c   net.Conn
		err error
	}
	ch := make(chan connOrErr, 1)
	go func() {
		c, err := ln.Accept()
		ch <- connOrErr{c, err}
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
			c.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			br := newByteReader(c)
			length, err := binary.ReadUvarint(br)
			if err != nil {
				return nil, err
			}
			payload := make([]byte, length)
			if _, err := io.ReadFull(c, payload); err != nil {
				return nil, err
			}
			return payload, nil
		},
		cleanup: func() {
			if conn != nil {
				conn.Close()
			}
			ln.Close()
		},
	}
}

func BenchmarkDestNull(b *testing.B) {
	benchDest(b, setupNullDestTB, (*XTCP).destNull)
}
func BenchmarkDestUDP(b *testing.B) {
	benchDest(b, setupUDPDestTB, (*XTCP).destUDP)
}
func BenchmarkDestUnixGram(b *testing.B) {
	benchDest(b, setupUnixGramDestTB, (*XTCP).destUnixGram)
}
func BenchmarkDestUnix(b *testing.B) {
	benchDest(b, setupUnixDestTB, (*XTCP).destUnix)
}
