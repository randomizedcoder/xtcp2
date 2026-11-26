package nlmon

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// qdiscEvents is n NEWQDISC events of 400 bytes each in one datagram, about
// what a 16 KiB receive holds.
func qdiscEvents(n int) []byte {
	b := make([]byte, 0, 400*n)
	for range n {
		b = append(b, rec(unix.RTM_NEWQDISC, make([]byte, 400-xtcpnl.NlMsgHdrSizeCst))...)
	}
	return b
}

// fileOf is n records for FromFile.
func fileOf(n int) []byte {
	b := make([]byte, 0, 116*n)
	for range n {
		b = append(b, rec(unix.RTM_NEWTFILTER, make([]byte, 100))...)
	}
	return b
}

func discard(Msg) error { return nil }

// BenchmarkDatagram is rtnl_listen's walk of a full datagram.
//
// go test ./internal/nlmon/ -run '^$' -bench Datagram -benchmem
func BenchmarkDatagram(b *testing.B) {
	dg := qdiscEvents(40)
	b.ReportAllocs()
	b.SetBytes(int64(len(dg)))
	for b.Loop() {
		if err := Datagram(dg, false, -1, discard); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*40), "ns/msg")
}

// BenchmarkFromFile is rtnl_from_file over 1000 records.
//
// go test ./internal/nlmon/ -run '^$' -bench FromFile -benchmem
func BenchmarkFromFile(b *testing.B) {
	f := fileOf(1000)
	var r bytes.Reader
	b.ReportAllocs()
	b.SetBytes(int64(len(f)))
	for b.Loop() {
		r.Reset(f)
		if err := FromFile(&r, discard); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*1000), "ns/msg")
}

// BenchmarkNotifications classifies goip's monitor capture and every
// tcmonitor capture (gotc 14P).
//
// go test ./internal/nlmon/ -run '^$' -bench Notifications -benchmem
func BenchmarkNotifications(b *testing.B) {
	tcPcaps, err := filepath.Glob("../../pkg/xtcpnl/testdata/7_3_1/ws/tcmonitor/*/ip.pcap")
	if err != nil || len(tcPcaps) == 0 {
		b.Fatalf("tcmonitor pcaps: %d, %v", len(tcPcaps), err)
	}
	for _, bc := range []struct {
		name  string
		pcaps []string
	}{{"ipmonitor", []string{monitorAllPcap}}, {"tcmonitor", tcPcaps}} {
		caps := make([][]Msg, 0, len(bc.pcaps))
		n := 0
		for _, p := range bc.pcaps {
			ms, err := PcapMsgs(p)
			if err != nil {
				b.Fatal(err)
			}
			caps = append(caps, ms)
			n += len(ms)
		}
		b.Run(bc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				for _, ms := range caps {
					Notifications(ms)
				}
			}
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*n), "ns/msg")
		})
	}
}

// TestAllocs pins the walks' allocations: the bodies are subslices, so a
// walk allocates nothing per message; a file walk allocates its one buffer.
//
// go test ./internal/nlmon/ -run TestAllocs
func TestAllocs(t *testing.T) {
	big := rec(unix.RTM_NEWTFILTER, make([]byte, RecvBufCst-xtcpnl.NlMsgHdrSizeCst))
	dg40, file1000 := qdiscEvents(40), fileOf(1000)
	var r bytes.Reader
	tests := []struct {
		description string
		run         func()
		budget      float64
	}{
		{"positive: a datagram of 40 events", func() { mustNil(t, Datagram(dg40, false, -1, discard)) }, 0},
		{"positive: a file of 1000 records, one buffer for all", func() { r.Reset(file1000); mustNil(t, FromFile(&r, discard)) }, 1},
		{"boundary: a 16384-byte message", func() { mustNil(t, Datagram(big, false, -1, discard)) }, 0},
		{"corner: an empty datagram", func() { mustNil(t, Datagram(nil, false, -1, discard)) }, 0},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := testing.AllocsPerRun(100, tc.run); got > tc.budget {
				t.Errorf("%v allocs, budget %v", got, tc.budget)
			}
		})
	}
}

func mustNil(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
