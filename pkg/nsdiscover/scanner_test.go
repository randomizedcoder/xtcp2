package nsdiscover

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// buildProcTree creates p pid dirs each with an ns/net symlink to net:[inode],
// the inode drawn from `distinct` namespaces, plus non-pid noise like real /proc.
func buildProcTree(tb testing.TB, p, distinct int) string {
	tb.Helper()
	proc := tb.TempDir()
	for _, name := range []string{"cpuinfo", "meminfo", "sys", "self", "stat"} {
		if err := os.Mkdir(filepath.Join(proc, name), 0o755); err != nil {
			tb.Fatal(err)
		}
	}
	const base = 4026531840
	for i := range p {
		pidDir := filepath.Join(proc, strconv.Itoa(1000+i))
		if err := os.MkdirAll(filepath.Join(pidDir, "ns"), 0o755); err != nil {
			tb.Fatal(err)
		}
		target := "net:[" + strconv.Itoa(base+(i%distinct)) + "]"
		if err := os.Symlink(target, filepath.Join(pidDir, "ns", "net")); err != nil {
			tb.Fatal(err)
		}
	}
	return proc
}

func TestScanner_dedup(t *testing.T) {
	proc := buildProcTree(t, 1000, 100)
	s := NewScanner(proc)
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})

	got := make(map[uint64]int) // inode -> pid
	skipped := s.Scan(func(ns Namespace) { got[ns.Inode] = ns.Pid })

	if skipped != 0 {
		t.Fatalf("skipped = %d, want 0 (all synthetic links readable)", skipped)
	}
	if len(got) != 100 {
		t.Fatalf("distinct namespaces = %d, want 100", len(got))
	}
	const base = 4026531840
	for i := range 100 {
		if _, ok := got[uint64(base+i)]; !ok {
			t.Fatalf("missing inode %d", base+i)
		}
	}
}

func TestScanner_rescanIsStable(t *testing.T) {
	proc := buildProcTree(t, 200, 20)
	s := NewScanner(proc)
	t.Cleanup(func() { _ = s.Close() })

	count := func() int {
		n := 0
		s.Scan(func(Namespace) { n++ })
		return n
	}
	first := count()
	second := count() // rewind + re-scan must yield the same set
	if first != 20 || second != 20 {
		t.Fatalf("scan counts = (%d, %d), want (20, 20)", first, second)
	}
}

func TestScanner_badProcRoot(t *testing.T) {
	s := NewScanner(filepath.Join(t.TempDir(), "does-not-exist"))
	t.Cleanup(func() { _ = s.Close() })
	if n := s.Scan(func(Namespace) { t.Fatal("fn called on a bad proc root") }); n != 0 {
		t.Fatalf("skipped = %d, want 0", n)
	}
}

func TestParseNetInode(t *testing.T) {
	tests := []struct {
		name   string
		target string
		want   uint64
		ok     bool
	}{
		{"positive typical", "net:[4026531840]", 4026531840, true},
		{"positive min", "net:[1]", 1, true},
		{"negative no brackets", "net:4026531840", 0, false},
		{"negative empty brackets", "net:[]", 0, false},
		{"negative non-numeric", "net:[abc]", 0, false},
		{"corner other nstype", "pid:[4026531836]", 4026531836, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseNetInode([]byte(tt.target))
			if ok != tt.ok || got != tt.want {
				t.Fatalf("parseNetInode(%q) = (%d,%v), want (%d,%v)", tt.target, got, ok, tt.want, tt.ok)
			}
		})
	}
}

// TestScanner_zeroAlloc pins the steady-state (reused) allocation to zero.
func TestScanner_zeroAlloc(t *testing.T) {
	proc := buildProcTree(t, 500, 50)
	s := NewScanner(proc)
	t.Cleanup(func() { _ = s.Close() })
	sink := 0
	fn := func(Namespace) { sink++ }
	s.Scan(fn) // warm
	if a := testing.AllocsPerRun(20, func() { s.Scan(fn) }); a != 0 {
		t.Fatalf("Scan allocs/op = %v, want 0", a)
	}
	_ = sink
}

func BenchmarkScanner(b *testing.B) {
	for _, p := range []int{100, 1000, 10000} {
		b.Run("pids="+strconv.Itoa(p), func(b *testing.B) {
			proc := buildProcTree(b, p, 100)
			s := NewScanner(proc)
			b.Cleanup(func() { _ = s.Close() })
			fn := func(Namespace) {}
			s.Scan(fn) // warm
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				s.Scan(fn)
			}
		})
	}
}
