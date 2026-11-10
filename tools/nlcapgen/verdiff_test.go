package main

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
	"testing/fstest"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// verNlmsg is one netlink message: its header and body.
func verNlmsg(typ, flags uint16, seq uint32, body []byte) []byte {
	m := make([]byte, xtcpnl.NlMsgHdrSizeCst, xtcpnl.NlMsgHdrSizeCst+len(body))
	binary.LittleEndian.PutUint32(m[0:4], uint32(xtcpnl.NlMsgHdrSizeCst+len(body)))
	binary.LittleEndian.PutUint16(m[4:6], typ)
	binary.LittleEndian.PutUint16(m[6:8], flags)
	binary.LittleEndian.PutUint32(m[8:12], seq)
	binary.LittleEndian.PutUint32(m[12:16], seq+1000)
	return append(m, body...)
}

// verPcap is a DLT_NETLINK pcap of one NETLINK_ROUTE datagram per message.
func verPcap(msgs ...[]byte) []byte {
	out := make([]byte, xtcpnl.PcapHeaderSizeCst)
	binary.LittleEndian.PutUint32(out[0:4], xtcpnl.PcapMagicMicrosCst)
	binary.LittleEndian.PutUint16(out[4:6], 2)
	binary.LittleEndian.PutUint16(out[6:8], 4)
	binary.LittleEndian.PutUint32(out[16:20], 262144)
	binary.LittleEndian.PutUint32(out[20:24], 253)
	for _, m := range msgs {
		rec := make([]byte, xtcpnl.NetlinkCookedHeaderSizeCst, xtcpnl.NetlinkCookedHeaderSizeCst+len(m))
		binary.BigEndian.PutUint16(rec[xtcpnl.SllProtocolOffsetCst:], unix.NETLINK_ROUTE)
		rec = append(rec, m...)
		rh := make([]byte, xtcpnl.PcapRecordHeaderSizeCst)
		binary.LittleEndian.PutUint32(rh[8:12], uint32(len(rec)))
		binary.LittleEndian.PutUint32(rh[12:16], uint32(len(rec)))
		out = append(append(out, rh...), rec...)
	}
	return out
}

// verRun is a run of one row, r/x, with the given rc, capture and output.
func verRun(rc string, pcap []byte, out string) fstest.MapFS {
	return fstest.MapFS{
		"r/x/cmd.json":  {Data: []byte("{}\n")},
		"r/x/ip.rc":     {Data: []byte(rc + "\n")},
		"r/x/ip.pcap":   {Data: pcap},
		"r/x/dump.out":  {Data: []byte(out)},
		"r/x/notes.txt": {Data: []byte("not compared")},
	}
}

func TestDiffVerRows(t *testing.T) {
	const req = unix.NLM_F_REQUEST | unix.NLM_F_ACK
	link := []byte{0, 0, 0, 0, 3, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	link2 := bytes.Clone(link)
	link2[4] = 4
	base := verPcap(verNlmsg(unix.RTM_NEWLINK, req, 1, link), verNlmsg(unix.NLMSG_ERROR, 0, 1, make([]byte, 20)))
	cases := []struct {
		description string
		a, b        fstest.MapFS
		expected    string // each difference as row kind detail, joined by "; "
	}{
		{"positive: identical runs", verRun("0", base, "d0"), verRun("0", base, "d0"), ""},
		{"positive: one request byte changed", verRun("0", base, "d0"),
			verRun("0", verPcap(verNlmsg(unix.RTM_NEWLINK, req, 1, link2), verNlmsg(unix.NLMSG_ERROR, 0, 1, make([]byte, 20))), "d0"),
			"r/x request 1 (type 16) at byte 20"},
		{"positive: an rc changed", verRun("0", base, "d0"), verRun("2", base, "d0"), "r/x rc 0 -> 2"},
		{"positive: a readback changed", verRun("0", base, "d0"), verRun("0", base, "d0 lacp_strict off"), "r/x out dump.out"},
		{"positive: the types changed, so no request is compared", verRun("0", base, "d0"),
			verRun("0", verPcap(verNlmsg(unix.RTM_NEWLINK, req, 1, link2)), "d0"), "r/x types 0:16R 0:2 -> 0:16R"},
		{"negative: a row in only one run", verRun("0", base, "d0"), fstest.MapFS{"q/y/cmd.json": {Data: []byte("{}")}},
			"q/y missing only in -b; r/x missing only in -a"},
		{"corner: seq and pid differ", verRun("0", base, "d0"),
			verRun("0", verPcap(verNlmsg(unix.RTM_NEWLINK, req, 7, link), verNlmsg(unix.NLMSG_ERROR, 0, 7, make([]byte, 20))), "d0"), ""},
		{"corner: random link-layer addresses", verRun("0", base, "link/ether 32:d3:65:88:85:d2 brd ff:ff:ff:ff:ff:ff"),
			verRun("0", base, "link/ether 62:29:c0:72:9f:87 brd ff:ff:ff:ff:ff:ff"), ""},
		{"corner: a bridge id's unpadded octets", verRun("0", base, "bridge_id 8000.ba:bf:53:28:e6:9a"),
			verRun("0", base, "bridge_id 8000.a:eb:1:4d:b3:9b"), ""},
		{"negative: an IPv6 address is kept", verRun("0", base, "via 2001:db8::2"), verRun("0", base, "via 2001:db8::3"), "r/x out dump.out"},
		{"boundary: five octets aren't an address", verRun("0", base, "id 00:11:22:33:44"), verRun("0", base, "id 00:11:22:33:45"), "r/x out dump.out"},
		{"corner: the flags differ", verRun("0", base, "d0"),
			verRun("0", verPcap(verNlmsg(unix.RTM_NEWLINK, unix.NLM_F_REQUEST, 1, link), verNlmsg(unix.NLMSG_ERROR, 0, 1, make([]byte, 20))), "d0"),
			"r/x request 1 (type 16) at byte 6"},
		{"boundary: a longer request", verRun("0", base, "d0"),
			verRun("0", verPcap(verNlmsg(unix.RTM_NEWLINK, req, 1, append(bytes.Clone(link), 0, 0, 0, 0)), verNlmsg(unix.NLMSG_ERROR, 0, 1, make([]byte, 20))), "d0"),
			"r/x request 1 (type 16) at byte 0"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			a, err := loadVerRows(tc.a)
			if err != nil {
				t.Fatal(err)
			}
			b, err := loadVerRows(tc.b)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, d := range diffVerRows(a, b) {
				got = append(got, d.Row+" "+d.Kind+" "+d.Detail)
			}
			if s := strings.Join(got, "; "); s != tc.expected {
				t.Errorf("got %q, want %q", s, tc.expected)
			}
		})
	}
}

func TestWriteVerdiff(t *testing.T) {
	a := map[string]verRow{"r/x": {}, "r/y": {}}
	cases := []struct {
		description string
		diffs       []verDiff
		expected    []string // lines the report holds
	}{
		{"positive: the counts and one line per difference", []verDiff{{"r/x", "rc", "0 -> 2"}, {"r/x", "out", "dump.out"}},
			[]string{"# h", "# 2 rows in -a, 2 in -b; 1 differ.", "# rc: 1", "# out: 1", "# types: 0", "r/x\trc\t0 -> 2", "r/x\tout\tdump.out"}},
		{"boundary: no differences", nil, []string{"# 2 rows in -a, 2 in -b; 0 differ.", "# missing: 0"}},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			var buf bytes.Buffer
			if err := writeVerdiff(&buf, []string{"h"}, a, a, tc.diffs); err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(buf.String(), "\n")
			for _, want := range tc.expected {
				found := false
				for _, l := range lines {
					found = found || l == want
				}
				if !found {
					t.Errorf("no line %q in\n%s", want, buf.String())
				}
			}
		})
	}
}
