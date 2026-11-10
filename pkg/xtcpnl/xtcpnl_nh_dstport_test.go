package xtcpnl

import (
	"bytes"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// NP1: NHA_DST_PORT on reads (goip-parity-next.md). The kernel echoes an
// fdb nexthop's port, be16, after NHA_FDB, and only when it is nonzero
// (nh_fill_node, net/ipv4/nexthop.c). The captured rows are the 7.3.0
// v730nh shows; the rest are hand-built wire edges.

// nhDstPortPcap is the RTM_NEWNEXTHOP body of a v730nh row's show readback
// at 7.3.0.
func nhDstPortPcap(t *testing.T, row string) []byte {
	t.Helper()
	p := filepath.Join(tdBase, "7_3_1", "ws-ip730", "v730nh", row, "netlink_route_show.pcap")
	for _, m := range pcapNlMsgs(t, p) {
		if m.h.Type == unix.RTM_NEWNEXTHOP {
			return m.body
		}
	}
	t.Fatalf("%s: no RTM_NEWNEXTHOP", p)
	return nil
}

func TestParseNewNexthopDstPort(t *testing.T) {
	const head = "02fd000000000000" + "0800010001000000" + "04000b00" // AF_INET, id 1, NHA_FDB
	const gw = "08000600c0000202"                                     // NHA_GATEWAY 192.0.2.2
	tests := []struct {
		description string
		pcapRow     string // the v730nh row the body was captured in, or ""
		body        string
		wantErr     error
		wantPort    uint16
		wantHas     bool
		wantFamily  uint8
	}{
		{
			description: "positive: a captured dst_port 4789",
			pcapRow:     "4789",
			body:        head + "0600120012b50000" + gw,
			wantPort:    4789, wantHas: true, wantFamily: unix.AF_INET,
		},
		{
			description: "positive: a captured inet6 fdb nexthop with dst_port 4789",
			pcapRow:     "inet6",
			body: "0afd000000000000" + "0800010001000000" + "04000b00" + "0600120012b50000" +
				"14000600" + "20010db8000100000000000000000002",
			wantPort: 4789, wantHas: true, wantFamily: unix.AF_INET6,
		},
		{
			description: "boundary: the largest port, captured",
			pcapRow:     "65535",
			body:        head + "06001200ffff0000" + gw,
			wantPort:    65535, wantHas: true, wantFamily: unix.AF_INET,
		},
		{
			description: "corner: a captured fdb nexthop with no port",
			pcapRow:     "fdb",
			body:        head + gw,
			wantFamily:  unix.AF_INET,
		},
		{
			description: "corner: port 0, which the kernel never sends, is present and 0",
			body:        head + "0600120000000000" + gw,
			wantHas:     true, wantFamily: unix.AF_INET,
		},
		{
			description: "boundary: port 1 is big-endian, not 256",
			body:        head + "0600120000010000" + gw,
			wantPort:    1, wantHas: true, wantFamily: unix.AF_INET,
		},
		{
			description: "negative: a one-byte attribute",
			body:        head + "0500120012000000" + gw,
			wantErr:     ErrNhDstPortLen,
		},
		{
			description: "negative: a four-byte attribute",
			body:        head + "08001200000012b5" + gw,
			wantErr:     ErrNhDstPortLen,
		},
		{
			description: "negative: an empty attribute",
			body:        head + "04001200" + gw,
			wantErr:     ErrNhDstPortLen,
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			body := mustHex(t, tc.body)
			if tc.pcapRow != "" {
				if got := nhDstPortPcap(t, tc.pcapRow); !bytes.Equal(got, body) {
					t.Fatalf("the body is not the one captured in v730nh/%s:\n got  %x\n want %x", tc.pcapRow, body, got)
				}
			}
			nh, err := ParseNewNexthop(body)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if nh.DstPort != tc.wantPort || nh.HasDstPort != tc.wantHas {
				t.Errorf("DstPort = %d (present %v), want %d (present %v)", nh.DstPort, nh.HasDstPort, tc.wantPort, tc.wantHas)
			}
			if nh.Family != tc.wantFamily || nh.ID != 1 || !nh.Fdb || nh.Gateway == nil {
				t.Errorf("family %d id %d fdb %v gateway %x: the attributes around the port were lost", nh.Family, nh.ID, nh.Fdb, nh.Gateway)
			}
		})
	}
}

// nhShowPort finds the dst_port ip printed on a show line.
var nhShowPort = regexp.MustCompile(`\bdst_port (\d+)\b`)

// TestWsNexthopDstPortCorpus decodes every RTM_NEWNEXTHOP of every v730nh
// show, at both versions, and holds the port to the one ip printed.
func TestWsNexthopDstPortCorpus(t *testing.T) {
	var checked, ported int
	for _, dir := range wsRows(t) {
		if !strings.Contains(filepath.ToSlash(dir), "/v730nh/") {
			continue
		}
		out, err := os.ReadFile(filepath.Join(dir, "show.out"))
		if err != nil {
			t.Fatal(err)
		}
		lines := map[uint32]string{}
		for l := range strings.Lines(string(out)) {
			f := strings.Fields(l)
			if len(f) < 2 || f[0] != "id" {
				continue
			}
			id, err := strconv.ParseUint(f[1], 10, 32)
			if err != nil {
				t.Fatalf("%s: %q: %v", dir, l, err)
			}
			lines[uint32(id)] = l
		}
		for _, m := range pcapNlMsgs(t, filepath.Join(dir, "netlink_route_show.pcap")) {
			if m.h.Type != unix.RTM_NEWNEXTHOP {
				continue
			}
			nh, err := ParseNewNexthop(m.body)
			if err != nil {
				t.Fatalf("%s: %v", dir, err)
			}
			l, ok := lines[nh.ID]
			if !ok {
				t.Fatalf("%s: id %d is not in show.out", dir, nh.ID)
			}
			want := -1
			if sm := nhShowPort.FindStringSubmatch(l); sm != nil {
				want, _ = strconv.Atoi(sm[1])
			}
			got := -1
			if nh.HasDstPort {
				got = int(nh.DstPort)
				ported++
			}
			if got != want {
				t.Errorf("%s: id %d port %d, ip printed %d", dir, nh.ID, got, want)
			}
			checked++
		}
	}
	t.Logf("checked %d nexthops, %d with a port", checked, ported)
	if checked == 0 || ported == 0 {
		t.Error("checked nothing: are the v730nh rows under testdata/*/ws{,-ip730}?")
	}
}

// FuzzParseNewNexthop ensures ParseNewNexthop never panics, and that a body
// it accepts decodes the same way twice.
func FuzzParseNewNexthop(f *testing.F) {
	for _, s := range []string{
		"02fd000000000000080001000100000004000b000600120012b5000008000600c0000202",
		"0afd000000000000080001000100000004000b000600120012b500001400060020010db8000100000000000000000002",
		"02fd000000000000080001000100000004000b0008000600c0000202",
		"02fd000000000000080001000100000004000b000500120012000000",
		"0000000000000000080001000a0000000600030000000000140002000100000000000000020000000000000008000e0000000080",
	} {
		b, err := hex.DecodeString(s)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(b)
	}
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, body []byte) {
		a, err := ParseNewNexthop(body)
		if err != nil {
			return
		}
		b, err := ParseNewNexthop(body)
		if err != nil || !reflect.DeepEqual(a, b) {
			t.Fatalf("two decodes differ: %+v / %+v (%v)", a, b, err)
		}
	})
}
