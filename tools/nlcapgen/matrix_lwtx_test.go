package main

import (
	"bytes"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func lwtxRows(t *testing.T) map[string]row {
	t.Helper()
	rows, err := matrixRows(families["lwtx"](), matrixOpts{Seed: 1})
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

// TestLwtxSpaces holds the lwtx family to goip-lwt-srv6.md's forms table,
// row by row: each form's cmd and rc, the duplicates, the setup and the
// modules. LX3-T01.
//
// go test ./tools/nlcapgen/ -run TestLwtxSpaces
func TestLwtxSpaces(t *testing.T) {
	byID := lwtxRows(t)
	cases := []struct {
		description string
		id          string
		expected    string // cmd, then rc
	}{
		{"positive: seg6route6 encap", "seg6route6/ec-001", "ip -6 route add 2001:db8:2::/64 encap seg6 mode encap segs 2001:db8:9::1 dev d0 0"},
		{"positive: seg6route6 encap-three", "seg6route6/ec-002", "ip -6 route add 2001:db8:2::/64 encap seg6 mode encap segs 2001:db8:9::1,2001:db8:9::2,2001:db8:9::3 dev d0 0"},
		{"positive: seg6route6 inline", "seg6route6/ec-003", "ip -6 route add 2001:db8:2::/64 encap seg6 mode inline segs 2001:db8:9::1 dev d0 0"},
		{"positive: seg6route6 l2encap", "seg6route6/ec-004", "ip -6 route add 2001:db8:2::/64 encap seg6 mode l2encap segs 2001:db8:9::1 dev d0 0"},
		{"positive: seg6route6 encap-red", "seg6route6/ec-005", "ip -6 route add 2001:db8:2::/64 encap seg6 mode encap.red segs 2001:db8:9::1,2001:db8:9::2,2001:db8:9::3 dev d0 0"},
		{"positive: seg6route6 l2encap-red", "seg6route6/ec-006", "ip -6 route add 2001:db8:2::/64 encap seg6 mode l2encap.red segs 2001:db8:9::1 dev d0 0"},
		{"positive: seg6route6 hmac", "seg6route6/ec-007", "ip -6 route add 2001:db8:2::/64 encap seg6 mode encap segs 2001:db8:9::1 hmac 7 dev d0 0"},
		{"positive: seg6route6 tunsrc", "seg6route6/ec-008", "ip -6 route add 2001:db8:2::/64 encap seg6 mode encap tunsrc 2001:db8:1::10 segs 2001:db8:9::1 dev d0 0"},
		{"corner: seg6route6 hmac-x", "seg6route6/ec-009", "ip -6 route add 2001:db8:2::/64 encap seg6 mode encap segs 2001:db8:9::1 hmac x dev d0 0"},
		{"negative: seg6route6 no-mode", "seg6route6/ng-001", "ip -6 route add 2001:db8:2::/64 encap seg6 hmac 1 dev d0 2"},
		{"negative: seg6route6 tunsrc-inline", "seg6route6/ng-002", "ip -6 route add 2001:db8:2::/64 encap seg6 mode inline tunsrc 2001:db8:1::10 segs 2001:db8:9::1 dev d0 255"},
		{"negative: seg6route6 segs-first", "seg6route6/ng-003", "ip -6 route add 2001:db8:2::/64 encap seg6 segs 2001:db8:9::1 mode encap dev d0 255"},
		{"negative: seg6route6 mode-unknown", "seg6route6/ng-004", "ip -6 route add 2001:db8:2::/64 encap seg6 mode foo segs 2001:db8:9::1 dev d0 255"},
		{"positive: seg6route4 encap", "seg6route4/ec-001", "ip -4 route add 198.51.100.0/24 encap seg6 mode encap segs 2001:db8:9::1 dev d0 0"},
		{"negative: seg6route4 inline", "seg6route4/ng-001", "ip -4 route add 198.51.100.0/24 encap seg6 mode inline segs 2001:db8:9::1 dev d0 2"},
		{"positive: seg6local end", "seg6local/ec-001", "ip -6 route add 2001:db8:5::/64 encap seg6local action End dev d0 0"},
		{"positive: seg6local end-count", "seg6local/ec-002", "ip -6 route add 2001:db8:5::/64 encap seg6local action End count dev d0 0"},
		{"positive: seg6local end-csid", "seg6local/ec-003", "ip -6 route add 2001:db8:5::/64 encap seg6local action End flavors next-csid lblen 32 nflen 16 dev d0 0"},
		{"positive: seg6local end-psp", "seg6local/ec-004", "ip -6 route add 2001:db8:5::/64 encap seg6local action End flavors psp dev d0 0"},
		{"positive: seg6local end-x", "seg6local/ec-005", "ip -6 route add 2001:db8:5::/64 encap seg6local action End.X nh6 2001:db8:1::1 oif d0 dev d0 0"},
		{"positive: seg6local end-t", "seg6local/ec-006", "ip -6 route add 2001:db8:5::/64 encap seg6local action End.T table 100 dev d0 0"},
		{"positive: seg6local end-dx2", "seg6local/ec-007", "ip -6 route add 2001:db8:5::/64 encap seg6local action End.DX2 oif d0 dev d0 0"},
		{"positive: seg6local end-dx6", "seg6local/ec-008", "ip -6 route add 2001:db8:5::/64 encap seg6local action End.DX6 nh6 2001:db8:1::1 dev d0 0"},
		{"positive: seg6local end-dx4", "seg6local/ec-009", "ip -6 route add 2001:db8:5::/64 encap seg6local action End.DX4 nh4 192.0.2.1 dev d0 0"},
		{"positive: seg6local end-dt6", "seg6local/ec-010", "ip -6 route add 2001:db8:5::/64 encap seg6local action End.DT6 table 100 dev d0 0"},
		{"positive: seg6local end-b6", "seg6local/ec-011", "ip -6 route add 2001:db8:5::/64 encap seg6local action End.B6 srh segs 2001:db8:9::1,2001:db8:9::2,2001:db8:9::3 dev d0 0"},
		{"positive: seg6local end-b6-encaps", "seg6local/ec-012", "ip -6 route add 2001:db8:5::/64 encap seg6local action End.B6.Encaps srh segs 2001:db8:9::1 hmac 7 dev d0 0"},
		{"negative: seg6local end-usp", "seg6local/ng-001", "ip -6 route add 2001:db8:5::/64 encap seg6local action End flavors usp dev d0 2"},
		{"negative: seg6local end-x-bare", "seg6local/ng-002", "ip -6 route add 2001:db8:5::/64 encap seg6local action End.X dev d0 2"},
		{"negative: seg6local end-dt4", "seg6local/ng-003", "ip -6 route add 2001:db8:5::/64 encap seg6local action End.DT4 vrftable 100 dev d0 2"},
		{"negative: seg6local action-unknown", "seg6local/ng-004", "ip -6 route add 2001:db8:5::/64 encap seg6local action End.Q dev d0 255"},
		{"negative: seg6local no-action", "seg6local/ng-005", "ip -6 route add 2001:db8:5::/64 encap seg6local table 100 dev d0 255"},
		{"negative: seg6local oif-nosuch", "seg6local/ng-006", "ip -6 route add 2001:db8:5::/64 encap seg6local action End.DX2 oif nosuch dev d0 255"},
		{"positive: ila luid", "ila/ec-001", "ip -6 route add 2001:db8:0:1::/64 encap ila 2001:0:0:2 ident-type luid dev d0 0"},
		{"positive: ila every", "ila/ec-002", "ip -6 route add 2001:db8:0:1::/64 encap ila 2001:0:0:2 csum-mode no-action ident-type luid hook-type output dev d0 0"},
		{"positive: ila adj-transport", "ila/ec-003", "ip -6 route add 2001:db8:0:1::/64 encap ila 2001:0:0:2 csum-mode adj-transport ident-type luid dev d0 0"},
		{"negative: ila use-format", "ila/ng-001", "ip -6 route add 2001:db8:0:1::/64 encap ila 2001:0:0:2 dev d0 2"},
		{"negative: ila ipv4", "ila/ng-002", "ip -4 route add 198.51.100.0/24 encap ila 2001:0:0:2 ident-type luid dev d0 2"},
		{"negative: ila five-groups", "ila/ng-003", "ip -6 route add 2001:db8:0:1::/64 encap ila 2001:0:0:2:3 dev d0 1"},
		{"negative: ila csum-unknown", "ila/ng-004", "ip -6 route add 2001:db8:0:1::/64 encap ila 2001:0:0:2 csum-mode foo dev d0 255"},
		{"positive: xfrm if-id", "xfrm/ec-001", "ip -6 route add 2001:db8:2::/64 dev d0 encap xfrm if_id 1 0"},
		{"positive: xfrm link-dev", "xfrm/ec-002", "ip -6 route add 2001:db8:2::/64 dev d0 encap xfrm if_id 1 link_dev d0 0"},
		{"positive: xfrm ipv4", "xfrm/ec-003", "ip -4 route add 198.51.100.0/24 dev d0 encap xfrm if_id 7 0"},
		{"negative: xfrm swallowed", "xfrm/ng-001", "ip -6 route add 2001:db8:2::/64 encap xfrm if_id 1 dev d0 2"},
		{"negative: xfrm if-id-0", "xfrm/ng-002", "ip -6 route add 2001:db8:2::/64 dev d0 encap xfrm if_id 0 255"},
		{"negative: xfrm no-if-id", "xfrm/ng-003", "ip -6 route add 2001:db8:2::/64 dev d0 encap xfrm link_dev d0 255"},
		{"negative: lwtxreq ioam6-inline", "lwtxreq/ec-001", "ip -6 route add 2001:db8:2::/64 encap ioam6 trace prealloc type 0x800000 ns 1 size 12 dev d0 2"},
		{"negative: lwtxreq ioam6-encap", "lwtxreq/ec-002", "ip -6 route add 2001:db8:2::/64 encap ioam6 freq 1/2 mode encap tunsrc 2001:db8:1::10 tundst 2001:db8:9::1 trace prealloc type 0x800000 ns 1 size 12 dev d0 2"},
		{"negative: lwtxreq ioam6-tundst-inline", "lwtxreq/ec-003", "ip -6 route add 2001:db8:2::/64 encap ioam6 mode inline tundst 2001:db8:9::1 trace prealloc type 0x800000 ns 1 size 12 dev d0 255"},
		{"negative: lwtxreq ioam6-size-13", "lwtxreq/ec-004", "ip -6 route add 2001:db8:2::/64 encap ioam6 trace prealloc type 0x800000 ns 1 size 13 dev d0 255"},
		{"negative: lwtxreq ioam6-k-over-n", "lwtxreq/ec-005", "ip -6 route add 2001:db8:2::/64 encap ioam6 freq 3/2 trace prealloc type 0x800000 ns 1 size 12 dev d0 255"},
		{"negative: lwtxreq rpl", "lwtxreq/ec-006", "ip -6 route add 2001:db8:2::/64 encap rpl segs 2001:db8:9::1,2001:db8:9::2 dev d0 2"},
		{"positive: lwtxhop hop6", "lwtxhop/ec-001", "ip -6 route add 2001:db8:2::/64 nexthop via 2001:db8:1::1 dev d0 encap seg6 mode encap segs 2001:db8:9::1 nexthop via 2001:db8:1::2 dev d0 0"},
		{"positive: lwtxhop hop4", "lwtxhop/ec-002", "ip -4 route add 198.51.100.0/24 nexthop via 192.0.2.1 dev d0 encap seg6 mode encap segs 2001:db8:9::1 nexthop via 192.0.2.2 dev d0 0"},
		{"positive: lwtxnh seg6", "lwtxnh/ec-001", "ip -6 nexthop add id 71 encap seg6 mode encap segs 2001:db8:9::1 dev d0 0"},
		{"positive: lwtxnh seg6local", "lwtxnh/ec-002", "ip -6 nexthop add id 72 encap seg6local action End dev d0 0"},
		{"negative: a duplicate seg6 add", "seg6route6/dup-1", "ip -6 route add 2001:db8:2::/64 encap seg6 mode encap segs 2001:db8:9::1 dev d0 2"},
		{"negative: a duplicate multipath add", "lwtxhop/dup-2", "ip -4 route add 198.51.100.0/24 nexthop via 192.0.2.1 dev d0 encap seg6 mode encap segs 2001:db8:9::1 nexthop via 192.0.2.2 dev d0 2"},
		{"negative: a duplicate nexthop add", "lwtxnh/dup-2", "ip -6 nexthop add id 72 encap seg6local action End dev d0 2"},
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
		const forms, dups = 56, 17
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
		if rcs[0] != 32 || rcs[2] != 11 || rcs[1] != 1 || rcs[255] != 12 || len(rcs) != 4 {
			t.Errorf("form rcs %v, want 32 at 0, 11 at 2, 1 at 1 and 12 at 255", rcs)
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
	t.Run("boundary: every row loads ila and xfrm_interface", func(t *testing.T) {
		want := []string{"ila", "xfrm_interface"}
		for id, r := range byID {
			if !slices.Equal(r.Modules, want) {
				t.Errorf("%s: modules %v, want %v", id, r.Modules, want)
			}
		}
	})
	t.Run("corner: ioam6 and rpl, which the guest kernel lacks, are ENOTSUP", func(t *testing.T) {
		for _, id := range []string{"lwtxreq/ec-001", "lwtxreq/ec-002", "lwtxreq/ec-006"} {
			if r := byID[id]; r.Expect.Errno != "ENOTSUP" || !strings.HasPrefix(r.Desc, "negative:") {
				t.Errorf("%s: errno %q, desc %q, want ENOTSUP and a negative", id, r.Expect.Errno, r.Desc)
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
	t.Run("corner: the same bytes twice", func(t *testing.T) {
		var out [2]bytes.Buffer
		for i := range out {
			rows, err := matrixRows(families["lwtx"](), matrixOpts{Seed: 1})
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
	})
}
