package xtcpnl

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// NP4: SEG6_IPTUNNEL_TABLE on reads (goip-parity-next.md), with the rest of
// an `encap seg6` payload: the mode, the SRH and tunsrc. The captured rows
// are the 7.3.0 v730seg6 shows and a 7.2.0 seg6route6 dump; the rest are
// hand-built.

// seg6RouteEncap is the RTA_ENCAP payload of the first route with one in a
// pcap under testdata.
func seg6RouteEncap(t *testing.T, rel string) []byte {
	t.Helper()
	for _, m := range pcapNlMsgs(t, filepath.Join(tdBase, rel)) {
		if m.h.Type != unix.RTM_NEWROUTE {
			continue
		}
		ri, err := ParseNewRoute(m.body)
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		if ri.Encap != nil {
			return ri.Encap.Raw
		}
	}
	t.Fatalf("%s: no route with an encap", rel)
	return nil
}

// seg6Summary is what TestParseSeg6Encap compares.
func seg6Summary(s Seg6EncapInfo) string {
	table := "no table"
	if s.HasLookup {
		table = "table " + strconv.FormatUint(uint64(s.Lookup), 10)
	}
	srh := "no srh"
	if s.HasSRH {
		srh = fmt.Sprintf("mode %d, srh %d bytes", s.Mode, len(s.SRH))
	}
	return fmt.Sprintf("%s, tunsrc %d bytes, %s", srh, len(s.TunSrc), table)
}

func TestParseSeg6Encap(t *testing.T) {
	// SEG6_IPTUNNEL_SRH: mode encap, then a one-segment SRH (8 + 16 bytes).
	const srh = "2000010001000000" + "0002040000000000" + "20010db8000900000000000000000001"
	tests := []struct {
		description string
		pcap        string // the pcap under testdata the payload was captured in, or ""
		encap       string
		wantErr     error
		expected    string
	}{
		{
			description: "positive: a captured lookup 100",
			pcap:        "7_3_1/ws-ip730/v730seg6/100/netlink_route_show.pcap",
			encap:       srh + "0800030064000000",
			expected:    "mode 1, srh 24 bytes, tunsrc 0 bytes, table 100",
		},
		{
			description: "positive: a captured lookup main",
			pcap:        "7_3_1/ws-ip730/v730seg6/main/netlink_route_show.pcap",
			encap:       srh + "08000300fe000000",
			expected:    "mode 1, srh 24 bytes, tunsrc 0 bytes, table 254",
		},
		{
			description: "positive: a captured inline lookup 100, the SRH's extra slot zero",
			pcap:        "7_3_1/ws-ip730/v730seg6/inline/netlink_route_show.pcap",
			encap: "3000010000000000" + "0004040101000000" + "00000000000000000000000000000000" +
				"20010db80009000000000000000000010800030064000000",
			expected: "mode 0, srh 40 bytes, tunsrc 0 bytes, table 100",
		},
		{
			description: "corner: a captured encap with no table (7.2.0)",
			pcap:        "7_3_0/ws/seg6route6/ec-001/netlink_route_dump.pcap",
			encap:       srh,
			expected:    "mode 1, srh 24 bytes, tunsrc 0 bytes, no table",
		},
		{
			description: "positive: tunsrc and the largest table",
			encap:       srh + "1400020020010db80000000000000000000000ff" + "08000300ffffffff",
			expected:    "mode 1, srh 24 bytes, tunsrc 16 bytes, table 4294967295",
		},
		{
			description: "corner: a table before the SRH",
			encap:       "0800030064000000" + srh,
			expected:    "mode 1, srh 24 bytes, tunsrc 0 bytes, table 100",
		},
		{
			description: "corner: the first of two tables wins",
			encap:       srh + "0800030064000000" + "08000300fe000000",
			expected:    "mode 1, srh 24 bytes, tunsrc 0 bytes, table 100",
		},
		{
			description: "corner: a table and no SRH, which ip prints nothing for",
			encap:       "0800030064000000",
			expected:    "no srh, tunsrc 0 bytes, table 100",
		},
		{
			description: "boundary: an SRH of the mode alone",
			encap:       "0800010003000000",
			expected:    "mode 3, srh 0 bytes, tunsrc 0 bytes, no table",
		},
		{
			description: "boundary: the mode is a host-order int, so 256 is not 0",
			encap:       "0800010000010000",
			expected:    "mode 256, srh 0 bytes, tunsrc 0 bytes, no table",
		},
		{
			description: "boundary: the mode is signed",
			encap:       "08000100ffffffff",
			expected:    "mode -1, srh 0 bytes, tunsrc 0 bytes, no table",
		},
		{
			description: "negative: a short table is dropped, as for the route's scalars",
			encap:       srh + "0600030064000000",
			expected:    "mode 1, srh 24 bytes, tunsrc 0 bytes, no table",
		},
		{
			description: "negative: an SRH shorter than its mode",
			encap:       "0700010001000000" + "0800030064000000",
			wantErr:     ErrSeg6EncapSRH,
		},
		{
			description: "negative: an attribute longer than the payload left",
			encap:       srh + "0800030064",
			wantErr:     ErrRTAttrSmall,
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			encap := mustHex(t, tc.encap)
			if tc.pcap != "" {
				if got := seg6RouteEncap(t, tc.pcap); !bytes.Equal(got, encap) {
					t.Fatalf("the payload is not the one captured in %s:\n got  %x\n want %x", tc.pcap, encap, got)
				}
			}
			s, err := ParseSeg6Encap(encap)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := seg6Summary(s); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

// seg6ModeNames are format_seg6mode_type's names, by SEG6_IPTUN_MODE_*.
var seg6ModeNames = map[int32]string{
	Seg6ModeInline: "inline", Seg6ModeEncap: "encap", Seg6ModeL2Encap: "l2encap",
	Seg6ModeEncapRed: "encap.red", Seg6ModeL2EncapRed: "l2encap.red",
}

// seg6TableName is the table as rtnl_rttable_n2a names it.
func seg6TableName(id uint32) string {
	switch id {
	case unix.RT_TABLE_MAIN:
		return "main"
	case unix.RT_TABLE_LOCAL:
		return "local"
	case unix.RT_TABLE_DEFAULT:
		return "default"
	}
	return strconv.FormatUint(uint64(id), 10)
}

// seg6Printed is what ip prints for a route-level seg6 encap: the mode and
// the lookup, read from a show line.
func seg6Printed(line string) string {
	f := strings.Fields(line)
	mode, lookup := "", ""
	for i := 0; i+1 < len(f); i++ {
		switch f[i] {
		case "mode":
			if mode == "" {
				mode = f[i+1]
			}
		case "lookup":
			lookup = f[i+1]
		}
	}
	return "mode " + mode + " lookup " + lookup
}

// TestWsSeg6EncapCorpus decodes every seg6 encap of a route or a hop in
// every workbench readback and holds its mode and table to the line ip
// printed. A dump prints its routes in message order, each hop under its
// route, so the seg6 lines and the seg6 encaps pair up in order.
func TestWsSeg6EncapCorpus(t *testing.T) {
	var encapsN, tables int
	for _, dir := range wsRows(t) {
		outs, err := filepath.Glob(filepath.Join(dir, "*.out"))
		if err != nil {
			t.Fatal(err)
		}
		for _, outPath := range outs {
			name := strings.TrimSuffix(filepath.Base(outPath), ".out")
			pcap := filepath.Join(dir, "netlink_route_"+name+".pcap")
			if _, err := os.Stat(pcap); err != nil {
				continue
			}
			out, err := os.ReadFile(outPath)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.HasPrefix(out, []byte("[")) || bytes.HasPrefix(out, []byte("{")) {
				continue // a -j readback
			}
			// A route's line or a hop's under it, not a nexthop object's
			// (`id 1 encap seg6 …`, an RTM_NEWNEXTHOP).
			var want []string
			for l := range strings.Lines(string(out)) {
				if strings.Contains(l, " encap seg6 ") && !strings.HasPrefix(l, "id ") {
					want = append(want, seg6Printed(l))
				}
			}
			var got []string
			for _, m := range pcapNlMsgs(t, pcap) {
				if m.h.Type != unix.RTM_NEWROUTE {
					continue
				}
				ri, err := ParseNewRoute(m.body)
				if err != nil {
					t.Fatalf("%s: %v", pcap, err)
				}
				encaps := []*RouteEncap{ri.Encap}
				for _, h := range ri.Multipath {
					encaps = append(encaps, h.Encap)
				}
				for _, e := range encaps {
					if e == nil || e.Seg6 == nil {
						continue
					}
					lookup := ""
					if e.Seg6.HasLookup {
						lookup = seg6TableName(e.Seg6.Lookup)
						tables++
					}
					got = append(got, "mode "+seg6ModeNames[e.Seg6.Mode]+" lookup "+lookup)
					encapsN++
				}
			}
			if strings.Join(got, "\n") != strings.Join(want, "\n") {
				t.Errorf("%s:\n decoded %q\n ip printed %q", pcap, got, want)
			}
		}
	}
	t.Logf("checked %d seg6 encaps, routes' and hops', %d with a table", encapsN, tables)
	if encapsN == 0 || tables == 0 {
		t.Error("checked nothing: are the seg6 rows under testdata/*/ws{,-ip730}?")
	}
}

// FuzzParseSeg6Encap ensures ParseSeg6Encap never panics, and that a payload
// it accepts decodes the same way twice.
func FuzzParseSeg6Encap(f *testing.F) {
	for _, s := range []string{
		"2000010001000000000204000000000020010db80009000000000000000000010800030064000000",
		"300001000000000000040401010000000000000000000000000000000000000020010db80009000000000000000000010800030064000000",
		"2000010001000000000204000000000020010db8000900000000000000000001",
		"0700010001000000",
	} {
		b, err := hex.DecodeString(s)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(b)
	}
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, val []byte) {
		a, err := ParseSeg6Encap(val)
		if err != nil {
			return
		}
		b, err := ParseSeg6Encap(val)
		if err != nil || !reflect.DeepEqual(a, b) {
			t.Fatalf("two decodes differ: %+v / %+v (%v)", a, b, err)
		}
	})
}

// srhHex is a hex SRH with first_segment and flags set, then segs.
func srhHex(hdrlen, first, flags string, segs ...string) string {
	return "00" + hdrlen + "04" + first + first + flags + "0000" + strings.Join(segs, "")
}

// NX6a: print_srh's read of an SRH, for route show's encap.
//
// go test ./pkg/xtcpnl/ -run TestParseSRH
func TestParseSRH(t *testing.T) {
	const (
		s1 = "20010db8000900000000000000000001"
		s2 = "20010db8000900000000000000000002"
		// hmacTLV is sr6_tlv_hmac with key id 7 and its 32 bytes zero.
		hmacTLV = "05260000" + "00000007" + "0000000000000000000000000000000000000000000000000000000000000000"
	)
	tests := []struct {
		description string
		srh         string
		expected    string // the segments in print order, then the hmac
		wantErr     error
	}{
		{"positive: one segment", srhHex("02", "00", "00", s1), "[2001:db8:9::1]", nil},
		{"positive: two segments print last slot first", srhHex("04", "01", "00", s1, s2), "[2001:db8:9::2 2001:db8:9::1]", nil},
		{"positive: an hmac key id", srhHex("07", "00", "08", s1, hmacTLV), "[2001:db8:9::1] hmac 7", nil},
		{"positive: inline's zero slot 0 still prints", srhHex("04", "01", "00", strings.Repeat("0", 32), s1), "[2001:db8:9::1 ::]", nil},
		{"boundary: one byte short of first_segment's slot", srhHex("02", "00", "00", s1)[:46], "", ErrSRH},
		{"negative: too short for its first_segment", srhHex("04", "01", "00", s1), "", ErrSRH},
		{"negative: the hmac flag with no TLV", srhHex("02", "00", "08", s1), "", ErrSRH},
		{"negative: the hmac TLV's offset inside the header", srhHex("00", "00", "08", s1), "", ErrSRH},
		{"corner: shorter than the header", "00020400", "", ErrSRH},
		{"corner: empty", "", "", ErrSRH},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			b, err := hex.DecodeString(tc.srh)
			if err != nil {
				t.Fatal(err)
			}
			s, err := ParseSRH(b)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err %v, want %v", err, tc.wantErr)
			}
			if err != nil {
				return
			}
			segs := make([]string, len(s.Segs))
			for i, a := range s.Segs {
				segs[i] = netipAddr(t, a)
			}
			got := fmt.Sprint(segs)
			if s.HasHmac {
				got += fmt.Sprintf(" hmac %d", s.HmacKeyID)
			}
			if got != tc.expected {
				t.Errorf("got %q, expected %q", got, tc.expected)
			}
		})
	}
}

// TestParseSRHRoundTrip reads back what SRH builds.
//
// go test ./pkg/xtcpnl/ -run TestParseSRHRoundTrip
func TestParseSRHRoundTrip(t *testing.T) {
	a := [16]byte{0x20, 0x01, 0x0d, 0xb8, 15: 1}
	b := [16]byte{0x20, 0x01, 0x0d, 0xb8, 15: 2}
	tests := []struct {
		description string
		nsegs       int
		segs        [][16]byte
		hmac        uint32
		expected    string
	}{
		{"positive: encap's one segment", 1, [][16]byte{a}, 0, "[2001:db8::1]"},
		{"positive: two segments, argv order", 2, [][16]byte{a, b}, 0, "[2001:db8::1 2001:db8::2]"},
		{"positive: with hmac", 2, [][16]byte{a, b}, 0xfffffffe, "[2001:db8::1 2001:db8::2] hmac 4294967294"},
		{"corner: inline's extra slot", 2, [][16]byte{a}, 0, "[2001:db8::1 ::]"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			raw, err := SRH(tc.nsegs, tc.segs, tc.hmac)
			if err != nil {
				t.Fatal(err)
			}
			s, err := ParseSRH(raw)
			if err != nil {
				t.Fatal(err)
			}
			segs := make([]string, len(s.Segs))
			for i, x := range s.Segs {
				segs[i] = netipAddr(t, x)
			}
			got := fmt.Sprint(segs)
			if s.HasHmac {
				got += fmt.Sprintf(" hmac %d", s.HmacKeyID)
			}
			if got != tc.expected {
				t.Errorf("got %q, expected %q", got, tc.expected)
			}
		})
	}
}

// netipAddr is a 16-byte address as text.
func netipAddr(t *testing.T, b []byte) string {
	t.Helper()
	a, ok := netip.AddrFromSlice(b)
	if !ok {
		t.Fatalf("%x is not an address", b)
	}
	return a.String()
}

// FuzzParseSRH ensures ParseSRH never panics, and that every segment it
// returns is 16 bytes.
func FuzzParseSRH(f *testing.F) {
	for _, s := range []string{
		srhHex("02", "00", "00", "20010db8000900000000000000000001"),
		srhHex("07", "00", "08", "20010db8000900000000000000000001",
			"0526000000000007"+strings.Repeat("0", 64)),
	} {
		b, err := hex.DecodeString(s)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(b)
	}
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, b []byte) {
		s, err := ParseSRH(b)
		if err != nil {
			return
		}
		for _, x := range s.Segs {
			if len(x) != 16 {
				t.Fatalf("a %d-byte segment from %x", len(x), b)
			}
		}
	})
}

// go test ./pkg/xtcpnl/ -run '^$' -bench BenchmarkParseSRH -benchmem
func BenchmarkParseSRH(b *testing.B) {
	raw, err := hex.DecodeString(srhHex("04", "01", "00",
		"20010db8000900000000000000000001", "20010db8000900000000000000000002"))
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := ParseSRH(raw); err != nil {
			b.Fatal(err)
		}
	}
}
