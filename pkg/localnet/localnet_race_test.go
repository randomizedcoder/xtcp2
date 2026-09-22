package localnet

import (
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// TestClassifyConcurrentWithStore exercises the production hot-path contract:
// many goroutines call Classify on the currently-published snapshot while a
// writer atomically swaps in freshly-built snapshots (the refresh path). Run
// under -race to prove Classify is a safe lock-free reader and BuildSnapshot's
// output is never mutated after publication.
//
// go test ./pkg/localnet/ -race -run TestClassifyConcurrentWithStore
func TestClassifyConcurrentWithStore(t *testing.T) {
	mk := func(third byte) *Snapshot {
		return BuildSnapshot(
			[]xtcpnl.AddrInfo{{Family: unix.AF_INET, Local: []byte{10, 0, third, 5}}},
			[]xtcpnl.RouteInfo{connectedRoute(unix.AF_INET, []byte{10, 0, third, 0}, 24)},
			map[uint32]string{2: "eth0"},
		)
	}

	var cur atomic.Pointer[Snapshot]
	cur.Store(mk(0))

	const readers = 8
	const iters = 20000

	// Writer runs until the readers finish (stop closed), continuously swapping
	// the published snapshot.
	stop := make(chan struct{})
	var writer sync.WaitGroup
	writer.Add(1)
	go func() {
		defer writer.Done()
		var i byte
		for {
			select {
			case <-stop:
				return
			default:
				cur.Store(mk(i))
				i++
			}
		}
	}()

	// Readers: classify a mix of self / subnet / remote addresses, each for a
	// bounded number of iterations.
	probes := []netip.Addr{
		netip.MustParseAddr("10.0.0.5"),
		netip.MustParseAddr("10.0.0.42"),
		netip.MustParseAddr("8.8.8.8"),
		netip.MustParseAddr("127.0.0.1"),
	}
	var rwg sync.WaitGroup
	for r := 0; r < readers; r++ {
		rwg.Add(1)
		go func() {
			defer rwg.Done()
			for i := 0; i < iters; i++ {
				snap := cur.Load()
				// Exercise every lock-free reader against the swapping snapshot,
				// including the Resolve fold used by the enrichment hot path.
				_ = snap.Classify(probes[i%len(probes)])
				_, oif, _ := snap.Lookup(probes[i%len(probes)])
				_ = snap.IfName(oif)
				_ = snap.Resolve(probes[i%len(probes)], oif)
			}
		}()
	}

	rwg.Wait()  // readers done
	close(stop) // then wind down the writer
	writer.Wait()
}

var benchSink Locality

// BenchmarkClassify measures the hot-path Classify cost for the three outcomes.
//
// go test ./pkg/localnet/ -bench BenchmarkClassify -run x
func BenchmarkClassify(b *testing.B) {
	snap := BuildSnapshot(
		[]xtcpnl.AddrInfo{{Family: unix.AF_INET, Index: 2, Local: []byte{10, 0, 0, 5}}},
		[]xtcpnl.RouteInfo{oifRoute(connectedRoute(unix.AF_INET, []byte{10, 0, 0, 0}, 24), 2)},
		map[uint32]string{2: "eth0"},
	)

	cases := []struct {
		name string
		addr netip.Addr
	}{
		{"self", netip.MustParseAddr("10.0.0.5")},
		{"subnet", netip.MustParseAddr("10.0.0.42")},
		{"remote", netip.MustParseAddr("8.8.8.8")},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			var out Locality
			for i := 0; i < b.N; i++ {
				out = snap.Classify(c.addr)
			}
			benchSink = out
		})
	}
}
