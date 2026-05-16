package io_uring

import (
	"runtime"
	"sync"
	"syscall"
	"testing"
)

// payloadSize matches a small INET_DIAG response — large enough to be
// realistic, small enough to fit a high-fanout benchmark in a few seconds.
const payloadSize = 256

// recvBufSize is what each pool buffer is sized to — matches the default
// xtcp2 packet buffer (~32 KB), large enough for many netlink messages.
const recvBufSize = 32 * 1024

// rusageDelta captures user/system CPU time around the benchmark body.
type rusageDelta struct {
	user int64 // microseconds
	sys  int64
	maj  int64
	nvcs int64
	nivs int64
}

func snapshotRusage(b *testing.B) rusageDelta {
	b.Helper()
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		b.Fatalf("Getrusage: %v", err)
	}
	return rusageDelta{
		user: ru.Utime.Sec*1e6 + ru.Utime.Usec,
		sys:  ru.Stime.Sec*1e6 + ru.Stime.Usec,
		maj:  ru.Majflt,
		nvcs: ru.Nvcsw,
		nivs: ru.Nivcsw,
	}
}

func reportRusage(b *testing.B, before, after rusageDelta) {
	b.Helper()
	div := float64(b.N)
	if div == 0 {
		div = 1
	}
	b.ReportMetric(float64(after.user-before.user)/div, "user_us/op")
	b.ReportMetric(float64(after.sys-before.sys)/div, "sys_us/op")
	b.ReportMetric(float64(after.nvcs-before.nvcs)/div, "nvcsw/op")
	b.ReportMetric(float64(after.nivs-before.nivs)/div, "nivcsw/op")
}

func makePayload() []byte {
	p := make([]byte, payloadSize)
	for i := range p {
		p[i] = byte(i)
	}
	return p
}

func newSendBufPool() *sync.Pool {
	return &sync.Pool{New: func() any {
		b := make([]byte, payloadSize)
		return &b
	}}
}

func newRecvBufPool() *sync.Pool {
	return &sync.Pool{New: func() any {
		b := make([]byte, recvBufSize)
		return &b
	}}
}

func drainerLoop(b *testing.B, fd int, stop <-chan struct{}) {
	b.Helper()
	go func() {
		buf := make([]byte, recvBufSize)
		for {
			select {
			case <-stop:
				return
			default:
			}
			if _, err := syscall.Read(fd, buf); err != nil {
				return
			}
		}
	}()
}

// BenchmarkSyscallSend baseline: one syscall.Write per record.
func BenchmarkSyscallSend(b *testing.B) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	srv, cli := socketpair(b)
	stop := make(chan struct{})
	defer close(stop)
	drainerLoop(b, srv, stop)

	payload := makePayload()
	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	before := snapshotRusage(b)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if _, err := syscall.Write(cli, payload); err != nil {
			b.Fatalf("write: %v", err)
		}
	}

	b.StopTimer()
	after := snapshotRusage(b)
	reportRusage(b, before, after)
}

// benchmarkIoUringSend pre-fills a window of `batch` send SQEs, submits
// them with one Submit call, drains `batch` CQEs, then refills — the
// realistic high-fanout pattern. The in-flight count stays bounded by
// `batch`, so we never hit the in-flight cap.
func benchmarkIoUringSend(b *testing.B, batch int) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if batch < 1 {
		batch = 1
	}
	r, err := New(Config{RecvBatchSize: batch, CQEBatchSize: batch})
	if err != nil {
		b.Skipf("io_uring not available: %v", err)
	}
	defer r.Close(100_000_000, nil)

	srv, cli := socketpair(b)
	stop := make(chan struct{})
	defer close(stop)
	drainerLoop(b, srv, stop)

	pool := newSendBufPool()
	payload := makePayload()

	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	before := snapshotRusage(b)
	b.ResetTimer()

	sent := 0
	for sent < b.N {
		// Fill a window of `batch` sends (bounded by remaining work).
		window := batch
		if sent+window > b.N {
			window = b.N - sent
		}
		for j := 0; j < window; j++ {
			buf := pool.Get().(*[]byte)
			copy(*buf, payload)
			*buf = (*buf)[:len(payload)]
			if _, eerr := r.EnqueueSend(cli, buf, OpSendUnixGram); eerr != nil {
				b.Fatalf("EnqueueSend: %v", eerr)
			}
		}
		if _, serr := r.Submit(); serr != nil {
			b.Fatalf("Submit: %v", serr)
		}
		// Drain the whole window before refilling.
		drained := 0
		for drained < window {
			results, werr := r.WaitOne()
			if werr != nil {
				b.Fatalf("WaitOne: %v", werr)
			}
			for _, res := range results {
				if res.Buf != nil {
					*res.Buf = (*res.Buf)[:cap(*res.Buf)]
					pool.Put(res.Buf)
				}
			}
			drained += len(results)
		}
		sent += window
	}

	b.StopTimer()
	after := snapshotRusage(b)
	reportRusage(b, before, after)
	b.ReportMetric(float64(batch), "batch")
}

func BenchmarkIoUringSendBatch1(b *testing.B)   { benchmarkIoUringSend(b, 1) }
func BenchmarkIoUringSendBatch16(b *testing.B)  { benchmarkIoUringSend(b, 16) }
func BenchmarkIoUringSendBatch64(b *testing.B)  { benchmarkIoUringSend(b, 64) }
func BenchmarkIoUringSendBatch256(b *testing.B) { benchmarkIoUringSend(b, 256) }

// BenchmarkSyscallRecv baseline: one syscall.Recvfrom per record, using a
// single reused buffer (so allocs/op is zero, fair vs the io_uring path
// that uses a sync.Pool).
func BenchmarkSyscallRecv(b *testing.B) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	srv, cli := socketpair(b)
	payload := makePayload()

	go func() {
		for i := 0; i < b.N; i++ {
			if _, werr := syscall.Write(srv, payload); werr != nil {
				return
			}
		}
	}()

	buf := make([]byte, recvBufSize)
	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	before := snapshotRusage(b)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if _, _, err := syscall.Recvfrom(cli, buf, 0); err != nil {
			b.Fatalf("Recvfrom: %v", err)
		}
	}

	b.StopTimer()
	after := snapshotRusage(b)
	reportRusage(b, before, after)
}

// benchmarkIoUringRecv pre-fills a window of `batch` recv SQEs from a
// sync.Pool, drains them in a batch, returns buffers to the pool, and
// refills. Mirrors the design intent: many recvs per Submit/Drain syscall.
func benchmarkIoUringRecv(b *testing.B, batch int) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if batch < 1 {
		batch = 1
	}
	r, err := New(Config{RecvBatchSize: batch, CQEBatchSize: batch})
	if err != nil {
		b.Skipf("io_uring not available: %v", err)
	}
	defer r.Close(100_000_000, nil)

	srv, cli := socketpair(b)
	payload := makePayload()

	go func() {
		for i := 0; i < b.N; i++ {
			if _, werr := syscall.Write(srv, payload); werr != nil {
				return
			}
		}
	}()

	pool := newRecvBufPool()

	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	before := snapshotRusage(b)
	b.ResetTimer()

	processed := 0
	for processed < b.N {
		window := batch
		if processed+window > b.N {
			window = b.N - processed
		}
		for j := 0; j < window; j++ {
			buf := pool.Get().(*[]byte)
			*buf = (*buf)[:recvBufSize]
			if _, eerr := r.EnqueueRecvMsg(cli, buf); eerr != nil {
				b.Fatalf("EnqueueRecvMsg: %v", eerr)
			}
		}
		if _, serr := r.Submit(); serr != nil {
			b.Fatalf("Submit: %v", serr)
		}
		drained := 0
		for drained < window {
			results, werr := r.WaitOne()
			if werr != nil {
				b.Fatalf("WaitOne: %v", werr)
			}
			for _, res := range results {
				if res.Buf != nil {
					pool.Put(res.Buf)
				}
			}
			drained += len(results)
		}
		processed += window
	}

	b.StopTimer()
	after := snapshotRusage(b)
	reportRusage(b, before, after)
	b.ReportMetric(float64(batch), "batch")
}

func BenchmarkIoUringRecvBatch1(b *testing.B)   { benchmarkIoUringRecv(b, 1) }
func BenchmarkIoUringRecvBatch16(b *testing.B)  { benchmarkIoUringRecv(b, 16) }
func BenchmarkIoUringRecvBatch64(b *testing.B)  { benchmarkIoUringRecv(b, 64) }
func BenchmarkIoUringRecvBatch256(b *testing.B) { benchmarkIoUringRecv(b, 256) }
