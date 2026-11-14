package main

import (
	"bytes"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// familyRows generates fam's rows, validated and keyed by id.
func familyRows(t *testing.T, fam string) map[string]row {
	t.Helper()
	rows, err := matrixRows(families[fam](), matrixOpts{Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	byID := make(map[string]row, len(rows))
	for i := range rows {
		if err := rows[i].validate(); err != nil {
			t.Fatal(err)
		}
		if _, dup := byID[rows[i].ID]; dup {
			t.Fatalf("id %s twice", rows[i].ID)
		}
		byID[rows[i].ID] = rows[i]
	}
	return byID
}

// sameTwice checks fam's rows file comes out byte for byte the same twice.
func sameTwice(t *testing.T, fam string) {
	t.Helper()
	var out [2]bytes.Buffer
	for i := range out {
		rows, err := matrixRows(families[fam](), matrixOpts{Seed: 1})
		if err != nil {
			t.Fatal(err)
		}
		if err := writeRows(&out[i], nil, rows); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(out[0].Bytes(), out[1].Bytes()) {
		t.Error("two runs differ")
	}
}

// TestLwtencSpaces holds the lwtenc family to NC8-T03's tables
// (docs/netlink/goip-parity-next.md), row by row: each form's cmd and rc,
// a duplicate per kind, the readbacks and the bpf object's path.
//
// go test ./tools/nlcapgen/ -run TestLwtencSpaces
func TestLwtencSpaces(t *testing.T) {
	byID := familyRows(t, "lwtenc")
	cases := []struct {
		description string
		id          string
		expected    string // cmd, then rc
	}{
		{"positive: rplroute one", "rplroute/ec-001", "ip -6 route add 2001:db8:2::/64 encap rpl segs 2001:db8:9::1 dev d0 0"},
		{"positive: rplroute three", "rplroute/ec-002", "ip -6 route add 2001:db8:2::/64 encap rpl segs 2001:db8:9::1,2001:db8:9::2,2001:db8:9::3 dev d0 0"},
		{"corner: rplroute no-segs", "rplroute/ec-003", "ip -6 route add 2001:db8:2::/64 encap rpl dev d0 0"},
		{"negative: rplroute ipv4-seg", "rplroute/ng-001", "ip -6 route add 2001:db8:2::/64 encap rpl segs 192.0.2.1 dev d0 1"},
		{"negative: rplroute dup-segs", "rplroute/ng-002", "ip -6 route add 2001:db8:2::/64 encap rpl segs 2001:db8:9::1 segs 2001:db8:9::2 dev d0 255"},
		{"negative: rplroute mcast", "rplroute/ng-003", "ip -6 route add 2001:db8:2::/64 encap rpl segs ff02::1 dev d0 2"},
		{"negative: rplroute ipv4-route", "rplroute/ng-004", "ip -4 route add 198.51.100.0/24 encap rpl segs 2001:db8:9::1 dev d0 2"},
		{"negative: a duplicate rplroute one", "rplroute/dup-1", "ip -6 route add 2001:db8:2::/64 encap rpl segs 2001:db8:9::1 dev d0 2"},
		{"positive: ioam6route inline", "ioam6route/ec-001", "ip -6 route add 2001:db8:2::/64 encap ioam6 trace prealloc type 0x800000 ns 1 size 12 dev d0 0"},
		{"positive: ioam6route encap", "ioam6route/ec-002", "ip -6 route add 2001:db8:2::/64 encap ioam6 freq 1/2 mode encap tunsrc 2001:db8:1::10 tundst 2001:db8:9::1 trace prealloc type 0x800000 ns 1 size 12 dev d0 0"},
		{"positive: ioam6route encap-no-tunsrc", "ioam6route/ec-003", "ip -6 route add 2001:db8:2::/64 encap ioam6 mode encap tundst 2001:db8:9::1 trace prealloc type 0x800000 ns 1 size 12 dev d0 0"},
		{"positive: ioam6route auto", "ioam6route/ec-004", "ip -6 route add 2001:db8:2::/64 encap ioam6 mode auto tundst 2001:db8:9::1 trace prealloc type 0x800000 ns 1 size 12 dev d0 0"},
		{"positive: ioam6route type-opaque", "ioam6route/ec-005", "ip -6 route add 2001:db8:2::/64 encap ioam6 trace prealloc type 0x000002 ns 1 size 12 dev d0 0"},
		{"boundary: ioam6route freq-max", "ioam6route/ec-006", "ip -6 route add 2001:db8:2::/64 encap ioam6 freq 1000000/1000000 trace prealloc type 0x800000 ns 1 size 12 dev d0 0"},
		{"boundary: ioam6route size-4", "ioam6route/ec-007", "ip -6 route add 2001:db8:2::/64 encap ioam6 trace prealloc type 0x800000 ns 1 size 4 dev d0 0"},
		{"boundary: ioam6route size-244", "ioam6route/ec-008", "ip -6 route add 2001:db8:2::/64 encap ioam6 trace prealloc type 0x800000 ns 1 size 244 dev d0 0"},
		{"boundary: ioam6route ns-0", "ioam6route/ec-009", "ip -6 route add 2001:db8:2::/64 encap ioam6 trace prealloc type 0x800000 ns 0 size 12 dev d0 0"},
		{"boundary: ioam6route ns-65535", "ioam6route/ec-010", "ip -6 route add 2001:db8:2::/64 encap ioam6 trace prealloc type 0x800000 ns 65535 size 12 dev d0 0"},
		{"corner: ioam6route ns-added", "ioam6route/ec-011", "ip -6 route add 2001:db8:2::/64 encap ioam6 trace prealloc type 0x800000 ns 1 size 12 dev d0 0"},
		{"negative: ioam6route size-248", "ioam6route/ng-001", "ip -6 route add 2001:db8:2::/64 encap ioam6 trace prealloc type 0x800000 ns 1 size 248 dev d0 255"},
		{"negative: ioam6route size-13", "ioam6route/ng-002", "ip -6 route add 2001:db8:2::/64 encap ioam6 trace prealloc type 0x800000 ns 1 size 13 dev d0 255"},
		{"negative: ioam6route k-over-n", "ioam6route/ng-003", "ip -6 route add 2001:db8:2::/64 encap ioam6 freq 3/2 trace prealloc type 0x800000 ns 1 size 12 dev d0 255"},
		{"negative: ioam6route freq-over", "ioam6route/ng-004", "ip -6 route add 2001:db8:2::/64 encap ioam6 freq 1/1000001 trace prealloc type 0x800000 ns 1 size 12 dev d0 255"},
		{"negative: ioam6route tundst-inline", "ioam6route/ng-005", "ip -6 route add 2001:db8:2::/64 encap ioam6 mode inline tundst 2001:db8:9::1 trace prealloc type 0x800000 ns 1 size 12 dev d0 255"},
		{"negative: ioam6route encap-no-tundst", "ioam6route/ng-006", "ip -6 route add 2001:db8:2::/64 encap ioam6 mode encap trace prealloc type 0x800000 ns 1 size 12 dev d0 255"},
		{"negative: ioam6route mode-unknown", "ioam6route/ng-007", "ip -6 route add 2001:db8:2::/64 encap ioam6 mode foo trace prealloc type 0x800000 ns 1 size 12 dev d0 255"},
		{"negative: ioam6route type-bit12", "ioam6route/ng-008", "ip -6 route add 2001:db8:2::/64 encap ioam6 trace prealloc type 0x800 ns 1 size 12 dev d0 2"},
		{"negative: ioam6route ipv4-route", "ioam6route/ng-009", "ip -4 route add 198.51.100.0/24 encap ioam6 trace prealloc type 0x800000 ns 1 size 12 dev d0 2"},
		{"positive: bpfroute in", "bpfroute/ec-001", "ip -4 route add 198.51.100.0/24 encap bpf in obj /run/nlcap-ws/lwt_bpf.o sec lwt_in dev d0 0"},
		{"positive: bpfroute out", "bpfroute/ec-002", "ip -4 route add 198.51.100.0/24 encap bpf out obj /run/nlcap-ws/lwt_bpf.o sec lwt_out dev d0 0"},
		{"positive: bpfroute xmit", "bpfroute/ec-003", "ip -4 route add 198.51.100.0/24 encap bpf xmit obj /run/nlcap-ws/lwt_bpf.o sec lwt_xmit dev d0 0"},
		{"positive: bpfroute all", "bpfroute/ec-004", "ip -4 route add 198.51.100.0/24 encap bpf in obj /run/nlcap-ws/lwt_bpf.o sec lwt_in out obj /run/nlcap-ws/lwt_bpf.o sec lwt_out xmit obj /run/nlcap-ws/lwt_bpf.o sec lwt_xmit headroom 16 dev d0 0"},
		{"boundary: bpfroute headroom-1", "bpfroute/ec-005", "ip -4 route add 198.51.100.0/24 encap bpf xmit obj /run/nlcap-ws/lwt_bpf.o sec lwt_xmit headroom 1 dev d0 0"},
		{"boundary: bpfroute headroom-256", "bpfroute/ec-006", "ip -4 route add 198.51.100.0/24 encap bpf xmit obj /run/nlcap-ws/lwt_bpf.o sec lwt_xmit headroom 256 dev d0 0"},
		{"corner: bpfroute in-from-xmit-sec", "bpfroute/ec-007", "ip -4 route add 198.51.100.0/24 encap bpf in obj /run/nlcap-ws/lwt_bpf.o sec lwt_xmit dev d0 0"},
		{"corner: bpfroute in-twice", "bpfroute/ec-008", "ip -4 route add 198.51.100.0/24 encap bpf in obj /run/nlcap-ws/lwt_bpf.o sec lwt_in in obj /run/nlcap-ws/lwt_bpf.o sec lwt_out dev d0 0"},
		{"corner: bpfroute headroom-twice", "bpfroute/ec-009", "ip -4 route add 198.51.100.0/24 encap bpf xmit obj /run/nlcap-ws/lwt_bpf.o sec lwt_xmit headroom 8 headroom 9 dev d0 0"},
		{"negative: bpfroute headroom-0", "bpfroute/ng-001", "ip -4 route add 198.51.100.0/24 encap bpf xmit obj /run/nlcap-ws/lwt_bpf.o sec lwt_xmit headroom 0 dev d0 255"},
		{"negative: bpfroute headroom-257", "bpfroute/ng-002", "ip -4 route add 198.51.100.0/24 encap bpf xmit obj /run/nlcap-ws/lwt_bpf.o sec lwt_xmit headroom 257 dev d0 2"},
		{"negative: bpfroute headroom-only", "bpfroute/ng-003", "ip -4 route add 198.51.100.0/24 encap bpf headroom 16 dev d0 2"},
		{"negative: bpfroute sec-nosuch", "bpfroute/ng-004", "ip -4 route add 198.51.100.0/24 encap bpf in obj /run/nlcap-ws/lwt_bpf.o sec nosuch dev d0 255"},
		{"negative: bpfroute obj-nosuch", "bpfroute/ng-005", "ip -4 route add 198.51.100.0/24 encap bpf in obj /nonexistent/lwt.o sec lwt_in dev d0 255"},
		{"positive: lwtencnh rpl", "lwtencnh/ec-001", "ip -6 nexthop add id 81 encap rpl segs 2001:db8:9::1 dev d0 0"},
		{"positive: lwtencnh ioam6", "lwtencnh/ec-002", "ip -6 nexthop add id 82 encap ioam6 trace prealloc type 0x800000 ns 1 size 12 dev d0 0"},
		{"positive: lwtencnh bpf", "lwtencnh/ec-003", "ip -6 nexthop add id 83 encap bpf in obj /run/nlcap-ws/lwt_bpf.o sec lwt_in dev d0 0"},
		{"positive: lwtencnh bpf-headroom", "lwtencnh/ec-004", "ip -6 nexthop add id 84 encap bpf xmit obj /run/nlcap-ws/lwt_bpf.o sec lwt_xmit headroom 32 dev d0 0"},
		{"negative: a duplicate lwtencnh bpf", "lwtencnh/dup-3", "ip -6 nexthop add id 83 encap bpf in obj /run/nlcap-ws/lwt_bpf.o sec lwt_in dev d0 2"},
		{"positive: lwtenchop rpl", "lwtenchop/ec-001", "ip -6 route add 2001:db8:2::/64 nexthop via 2001:db8:1::1 dev d0 encap rpl segs 2001:db8:9::1 nexthop via 2001:db8:1::2 dev d0 0"},
		{"positive: lwtenchop ioam6", "lwtenchop/ec-002", "ip -6 route add 2001:db8:2::/64 nexthop via 2001:db8:1::1 encap ioam6 trace prealloc type 0x800000 ns 1 size 12 dev d0 nexthop via 2001:db8:1::2 dev d0 0"},
		{"positive: lwtenchop bpf", "lwtenchop/ec-003", "ip -6 route add 2001:db8:2::/64 nexthop via 2001:db8:1::1 dev d0 encap bpf in obj /run/nlcap-ws/lwt_bpf.o sec lwt_in nexthop via 2001:db8:1::2 dev d0 0"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			r, ok := byID[tc.id]
			if !ok {
				t.Fatalf("no row %s", tc.id)
			}
			if got := r.Cmd + " " + strconv.Itoa(*r.Expect.RC); got != tc.expected {
				t.Errorf("%s: %q, want %q", tc.id, got, tc.expected)
			}
		})
	}
	t.Run("positive: the family is the forms and the duplicates", func(t *testing.T) {
		const forms, dups = 48, 15
		var formRows, dupRows int
		rcs := map[int]int{}
		for id, r := range byID {
			if strings.Contains(id, "/dup-") {
				dupRows++
				continue
			}
			formRows++
			rcs[*r.Expect.RC]++
		}
		if formRows != forms || dupRows != dups {
			t.Errorf("%d form rows and %d duplicates, want %d and %d", formRows, dupRows, forms, dups)
		}
		if rcs[0] != 30 || rcs[2] != 6 || rcs[1] != 1 || rcs[255] != 11 || len(rcs) != 4 {
			t.Errorf("form rcs %v, want 30 at 0, 6 at 2, 1 at 1 and 11 at 255", rcs)
		}
	})
	t.Run("positive: every row reads its object back plain, as -j and as -d", func(t *testing.T) {
		for id, r := range byID {
			var got []string
			for _, rb := range r.Readback {
				got = append(got, rb.Name+"="+rb.Cmd)
			}
			show := "ip -6 route show table all"
			switch {
			case strings.HasPrefix(id, "bpfroute/"):
				show = "ip -4 route show table all"
			case strings.HasPrefix(id, "lwtencnh/"):
				show = "ip nexthop show"
			}
			rest := strings.TrimPrefix(show, "ip ")
			want := []string{"dump=" + show, "dump_j=ip -j " + rest, "dump_d=ip -d " + rest}
			if !slices.Equal(got, want) {
				t.Errorf("%s: readbacks %v, want %v", id, got, want)
			}
		}
	})
	t.Run("boundary: every bpf program is a lwt section of the linked object, but the missing ones", func(t *testing.T) {
		for id, r := range byID {
			if !strings.Contains(r.Cmd, " obj ") || id == "bpfroute/ng-004" || id == "bpfroute/ng-005" {
				continue
			}
			for _, w := range strings.Split(r.Cmd, " obj ")[1:] {
				if !strings.HasPrefix(w, lwtencBPF+" sec lwt_") {
					t.Errorf("%s: obj %q", id, w)
				}
			}
		}
	})
	t.Run("corner: only ns-added makes an ioam namespace", func(t *testing.T) {
		for id, r := range byID {
			has := slices.ContainsFunc(r.Pre, func(p preStep) bool { return strings.Contains(p.Cmd, "ioam namespace") })
			if want := id == "ioam6route/ec-011"; has != want {
				t.Errorf("%s: namespace step %v, want %v", id, has, want)
			}
		}
	})
	t.Run("negative: every setup step is ip and tolerates nothing", func(t *testing.T) {
		for id, r := range byID {
			for _, p := range r.Pre {
				for step := range strings.SplitSeq(p.Cmd, " && ") {
					if !strings.HasPrefix(step, "ip ") || len(p.OK) != 0 {
						t.Errorf("%s: step %q (ok %v)", id, step, p.OK)
					}
				}
			}
		}
	})
	t.Run("corner: no command holds a doubled space", func(t *testing.T) {
		for id, r := range byID {
			if strings.Contains(r.Cmd, "  ") {
				t.Errorf("%s: %q", id, r.Cmd)
			}
		}
	})
	t.Run("corner: the same bytes twice", func(t *testing.T) { sameTwice(t, "lwtenc") })
}
