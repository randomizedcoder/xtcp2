package xtcp

import (
	"encoding/binary"
	"fmt"
	"os"
	"runtime"
	"testing"

	xio "github.com/randomizedcoder/xtcp2/pkg/io_uring"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// benchSockstatFDs opens n fds on this process's sockstat.
func benchSockstatFDs(b *testing.B, n int) []int {
	b.Helper()
	fds := make([]int, n)
	for i := range fds {
		f, err := os.Open("/proc/self/net/sockstat")
		if err != nil {
			b.Skipf("sockstat (n=%d; raise ulimit -n?): %v", n, err)
		}
		b.Cleanup(func() { _ = f.Close() })
		fds[i] = int(f.Fd())
	}
	return fds
}

// BenchmarkReadSockstat: reading and parsing n namespaces' sockstat, by
// pread and by io_uring batches. ns/op is one poll's pre-pass; ns/netns is
// per namespace. Each read is O(CPUs) in the kernel (per-CPU sums), so run
// it on a many-core host too.
func BenchmarkReadSockstat(b *testing.B) {
	for _, n := range []int{64, 1024, 4096} {
		b.Run(fmt.Sprintf("syscall/n=%d", n), func(b *testing.B) {
			fds := benchSockstatFDs(b, n)
			counts := make([]sockstatCount, n)
			r := newSyscallSockstatReader()
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if failed, _ := r.read(fds, counts); failed != 0 {
					b.Fatalf("%d failed", failed)
				}
			}
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(n), "ns/netns")
		})
		for _, batch := range []int{1, 16, 64, 128} {
			b.Run(fmt.Sprintf("io_uring/n=%d/batch=%d", n, batch), func(b *testing.B) {
				fds := benchSockstatFDs(b, n)
				counts := make([]sockstatCount, n)
				runtime.LockOSThread()
				defer runtime.UnlockOSThread() //nolint:forbidigo // safe: benchmark pins the SINGLE_ISSUER ring to one thread; no netns change
				ring, err := xio.New(xio.Config{RecvBatchSize: batch, CQEBatchSize: batch})
				if err != nil {
					b.Skipf("io_uring: %v", err)
				}
				defer ring.Close(0, nil)
				if _, _, err := ring.SetIOWQMaxWorkers(4, 4); err != nil {
					b.Fatal(err)
				}
				rb := newRingBatcher(ring, batch)
				b.ReportAllocs()
				b.ResetTimer()
				var submits int
				for b.Loop() {
					failed, s, err := rb.read(fds, counts)
					if err != nil || failed != 0 {
						b.Fatalf("failed=%d err=%v", failed, err)
					}
					submits = s
				}
				b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(n), "ns/netns")
				b.ReportMetric(float64(submits), "submits/op")
			})
		}
	}
}

// BenchmarkPlanPoll: planning n namespaces with mixed counts, over budget.
func BenchmarkPlanPoll(b *testing.B) {
	for _, n := range []int{1, 64, 4096} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			counts := make([]sockstatCount, n)
			for i := range counts {
				counts[i] = known(uint64(i*37%5000), uint64(i*11%800))
			}
			counts[0] = known(500_000, 50_000)
			var p pollPlanner
			b.ReportAllocs()
			loop := uint64(0)
			for b.Loop() {
				p.plan(counts, 200_000, 262_144, loop)
				loop++
			}
		})
	}
}

// BenchmarkPollPrepass: the whole gate before a poll (map walk, pread of
// every sockstat, plan, requests) for n namespaces.
func BenchmarkPollPrepass(b *testing.B) {
	for _, n := range []int{64, 4096} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			fds := benchSockstatFDs(b, n)
			x := newPollerFixture(b)
			x.config.MaxRecordsPerPoll = 200_000
			x.config.MaxRecordsPerNetns = 262_144
			req := make([]byte, xtcpnl.InetDiagRequestSizeCst)
			binary.LittleEndian.PutUint32(req[20:24], 0x3fff)
			x.nlRequest = &req
			for i, fd := range fds {
				x.nsMap.Store(uint64(i+1), netNSitem{inode: uint64(i + 1), socketFD: 100000 + i, stat: newStatHandle(fd)})
			}
			defer x.closeGate()
			b.ReportAllocs()
			for b.Loop() {
				if got := len(x.planGatedPoll()); got != n {
					b.Fatalf("planned %d, expected %d", got, n)
				}
			}
		})
	}
}
