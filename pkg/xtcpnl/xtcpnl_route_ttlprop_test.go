package xtcpnl

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// NP2: the AF_MPLS route attributes on reads (goip-parity-next.md).
// RTA_NEWDST and RTA_VIA were decoded already; RTA_TTL_PROPAGATE is new.
// The kernel sends it after RTA_DST, and only for a route that set it
// (mpls_dump_route, net/mpls/af_mpls.c). The captured rows are mpls show
// readbacks at 7.2.0; the rest are hand-built wire edges.

// mplsShowPcap is the n-th RTM_NEWROUTE body of an mpls row's show
// readback.
func mplsShowPcap(t *testing.T, row string, n int) []byte {
	t.Helper()
	p := filepath.Join(tdBase, "7_3_1", "ws", "mpls", row, "netlink_route_show.pcap")
	for _, m := range pcapNlMsgs(t, p) {
		if m.h.Type != unix.RTM_NEWROUTE {
			continue
		}
		if n == 0 {
			return m.body
		}
		n--
	}
	t.Fatalf("%s: too few RTM_NEWROUTE", p)
	return nil
}

// mplsTTLSummary is what TestParseNewRouteTTLPropagate compares.
func mplsTTLSummary(ri RouteInfo) string {
	ttl := "absent"
	if ri.HasTTLPropagate {
		ttl = strconv.Itoa(int(ri.TTLPropagate))
	}
	bos := ""
	for _, l := range ri.NewDst {
		if l.BoS {
			bos += "S"
		} else {
			bos += "-"
		}
	}
	return fmt.Sprintf("ttl %s, newdst %d %s, hops %d", ttl, len(ri.NewDst), bos, len(ri.Multipath))
}

func TestParseNewRouteTTLPropagate(t *testing.T) {
	// rtmsg {AF_MPLS, dst_len 20, table main, proto boot, scope universe,
	// unicast} + RTA_DST 100 (BoS) + RTA_OIF 3.
	const hdr = "1c140000fe030001000000000800010000064100"
	const oif = "0800040003000000"
	labels := make([]uint32, 30)
	for i := range labels {
		labels[i] = uint32(100 + i)
	}
	stack30 := hex.EncodeToString(mplsStack(labels...)) // 100/101/…/129
	tests := []struct {
		description string
		pcapRow     string // the mpls row the body was captured in, or ""
		pcapIndex   int    // which RTM_NEWROUTE of its show
		body        string
		expected    string
	}{
		{
			description: "positive: a captured ttl-propagate enabled, before RTA_NEWDST",
			pcapRow:     "pw-011",
			body:        hdr + "05001a0001000000" + "08001300000c8100" + oif,
			expected:    "ttl 1, newdst 1 S, hops 0",
		},
		{
			description: "positive: a captured ttl-propagate disabled, label 102",
			pcapRow:     "show", pcapIndex: 2,
			body:     "1c140000fe03000100000000080001000006610005001a0000000000" + oif,
			expected: "ttl 0, newdst 0 , hops 0",
		},
		{
			description: "positive: a captured ttl-propagate on a multipath route",
			pcapRow:     "pw-023",
			body: hdr + "05001a0001000000" + "380009001400000003000000" + "0a00120002000a0000020000" +
				"2000000003000000" + "160012000a0020010db80000000000000000000000020000",
			expected: "ttl 1, newdst 0 , hops 2",
		},
		{
			description: "corner: a captured route that set no ttl-propagate",
			pcapRow:     "ec-001",
			body:        hdr + "0a00120002000a0000020000" + oif,
			expected:    "ttl absent, newdst 0 , hops 0",
		},
		{
			description: "boundary: a captured 30-label stack, BoS on the last alone",
			pcapRow:     "bd-002",
			body:        "1c140000fe0300010000000008000100003e7100" + "7c001300" + stack30 + "0a00120002000a0000020000" + oif,
			expected:    "ttl absent, newdst 30 -----------------------------S, hops 0",
		},
		{
			description: "corner: a value of 2, which the kernel refuses on input, is kept",
			body:        hdr + "05001a0002000000" + oif,
			expected:    "ttl 2, newdst 0 , hops 0",
		},
		{
			description: "corner: a u32 payload reads its first byte, as rta_getattr_u8 does",
			body:        hdr + "08001a0001000000" + oif,
			expected:    "ttl 1, newdst 0 , hops 0",
		},
		{
			description: "negative: an empty payload is dropped, as for the other scalars",
			body:        hdr + "04001a00" + oif,
			expected:    "ttl absent, newdst 0 , hops 0",
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			body := mustHex(t, tc.body)
			if tc.pcapRow != "" {
				if got := mplsShowPcap(t, tc.pcapRow, tc.pcapIndex); !bytes.Equal(got, body) {
					t.Fatalf("the body is not the one captured in mpls/%s:\n got  %x\n want %x", tc.pcapRow, body, got)
				}
			}
			ri, err := ParseNewRoute(body)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := mplsTTLSummary(ri); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
			if ri.Family != unix.AF_MPLS || len(ri.Dst) != MPLSLabelEntrySizeCst {
				t.Errorf("family %d, RTA_DST %x: the route's own label was lost", ri.Family, ri.Dst)
			}
		})
	}
}

var (
	// ip prints the key misspelled, with an o for the second a
	// (ip/iproute.c); the class matches it without spelling it out.
	mplsShowTTL    = regexp.MustCompile(`\bttl-prop[ao]gate (enabled|disabled)\b`)
	mplsShowNewDst = regexp.MustCompile(`^\d+ as to ([0-9/]+) `)
)

// TestWsMplsShowCorpus decodes every AF_MPLS route of every mpls row's
// show, and holds its ttl-propagate and its top-level `as` stack to what ip
// printed on the route's first line.
func TestWsMplsShowCorpus(t *testing.T) {
	var routes, ttls, stacks int
	for _, dir := range wsRows(t) {
		if !strings.HasPrefix(wsLoadRow(t, dir).ID, "mpls/") {
			continue
		}
		out, err := os.ReadFile(filepath.Join(dir, "show.out"))
		if err != nil {
			t.Fatal(err)
		}
		lines := map[uint32]string{}
		for l := range strings.Lines(string(out)) {
			f := strings.Fields(l)
			if len(f) == 0 || l[0] == '\t' {
				continue
			}
			label, err := strconv.ParseUint(f[0], 10, 32)
			if err != nil {
				t.Fatalf("%s: %q: %v", dir, l, err)
			}
			lines[uint32(label)] = l
		}
		for _, m := range pcapNlMsgs(t, filepath.Join(dir, "netlink_route_show.pcap")) {
			if m.h.Type != unix.RTM_NEWROUTE || len(m.body) == 0 || m.body[0] != unix.AF_MPLS {
				continue
			}
			ri, err := ParseNewRoute(m.body)
			if err != nil {
				t.Fatalf("%s: %v", dir, err)
			}
			dst, err := ParseMPLSLabels(ri.Dst)
			if err != nil || len(dst) != 1 {
				t.Fatalf("%s: RTA_DST %x: %v", dir, ri.Dst, err)
			}
			l, ok := lines[dst[0].Label]
			if !ok {
				t.Fatalf("%s: label %d is not in show.out", dir, dst[0].Label)
			}
			wantTTL, gotTTL := "", ""
			if sm := mplsShowTTL.FindStringSubmatch(l); sm != nil {
				wantTTL = sm[1]
			}
			if ri.HasTTLPropagate {
				gotTTL = map[uint8]string{0: "disabled", 1: "enabled"}[ri.TTLPropagate]
				ttls++
			}
			wantDst := ""
			if sm := mplsShowNewDst.FindStringSubmatch(l); sm != nil {
				wantDst = sm[1]
			}
			gotDst := make([]string, len(ri.NewDst))
			for i, nl := range ri.NewDst {
				gotDst[i] = strconv.FormatUint(uint64(nl.Label), 10)
			}
			if len(gotDst) != 0 {
				stacks++
			}
			if gotTTL != wantTTL || strings.Join(gotDst, "/") != wantDst {
				t.Errorf("%s: label %d: ttl %q, as %q; ip printed ttl %q, as %q", dir, dst[0].Label, gotTTL, strings.Join(gotDst, "/"), wantTTL, wantDst)
			}
			routes++
		}
	}
	t.Logf("checked %d routes: %d with ttl-propagate, %d with a stack", routes, ttls, stacks)
	if routes == 0 || ttls == 0 || stacks == 0 {
		t.Error("checked nothing: are the mpls rows under testdata/*/ws?")
	}
}
