package goipparity

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
)

// tdLinkWrite, tdRouteWrite and tdAddrWrite are the link-write corpus's
// link, route and address rows. Each write row of the table names its corpus
// directory in its slug, after linkWriteSlugPrefixCst,
// routeWriteSlugPrefixCst or addrWriteSlugPrefixCst.
const (
	tdLinkWrite             = "../../pkg/xtcpnl/testdata/7_1_4/link-write/write"
	tdRouteWrite            = "../../pkg/xtcpnl/testdata/7_1_4/link-write/route"
	tdAddrWrite             = "../../pkg/xtcpnl/testdata/7_1_4/link-write/addr"
	linkWriteSlugPrefixCst  = "link_write_"
	routeWriteSlugPrefixCst = "route_write_"
	addrWriteSlugPrefixCst  = "addr_write_"
	pcapGlobalHdrCst        = 24
	pcapRecordHdrCst        = 16
	// nlMsgHdrCst is sizeof(struct nlmsghdr). Spelled here because this
	// package may not import pkg/xtcpnl (TestParityCannotOpenASocket).
	nlMsgHdrCst = 16
)

// dropLastRecords returns a pcap without its last n records. The write
// corpus holds one datagram per record, and a write and its NLMSG_ERROR are
// the last two, so dropping them leaves exactly the lookups goip would send.
func dropLastRecords(t *testing.T, b []byte, n int) []byte {
	t.Helper()
	var offs []int
	for off := pcapGlobalHdrCst; off < len(b); {
		if off+pcapRecordHdrCst > len(b) {
			t.Fatalf("truncated pcap record header at %d", off)
		}
		offs = append(offs, off)
		off += pcapRecordHdrCst + int(binary.LittleEndian.Uint32(b[off+8:off+12]))
	}
	if len(offs) < n {
		t.Fatalf("pcap holds %d records, cannot drop %d", len(offs), n)
	}
	if len(offs) == n {
		return append([]byte(nil), b[:pcapGlobalHdrCst]...)
	}
	return append([]byte(nil), b[:offs[len(offs)-n]]...)
}

// writeRowFiles is one write row's files as a perfect run would leave them:
// ip_a and ip_b are the corpus capture, goip is the same capture without the
// write, and goip's GOIP_WRITE_OUT file is ip's write request. edit, when set,
// changes them before they are written.
type writeRowFiles struct {
	pcaps map[string][]byte
	rcs   map[string]string
	write []byte
}

func corpusWriteRow(t *testing.T, c Command) writeRowFiles {
	t.Helper()
	dir := filepath.Join(tdLinkWrite, strings.TrimPrefix(c.Slug, linkWriteSlugPrefixCst))
	if slug, ok := strings.CutPrefix(c.Slug, routeWriteSlugPrefixCst); ok {
		dir = filepath.Join(tdRouteWrite, slug)
	}
	if slug, ok := strings.CutPrefix(c.Slug, addrWriteSlugPrefixCst); ok {
		dir = filepath.Join(tdAddrWrite, slug)
	}
	ipPcap := mustReadBytes(t, filepath.Join(dir, "ip.pcap"))
	rc := strings.TrimSpace(mustRead(t, filepath.Join(dir, "ip.rc")))

	capture, err := nlparity.ParseRouteCaptureFile(filepath.Join(dir, "ip.pcap"))
	if err != nil {
		t.Fatal(err)
	}
	w, err := nlparity.SplitWrites(nlparity.SegmentCapture(capture), 1)
	if err != nil {
		t.Fatal(err)
	}
	req := make([]byte, nlMsgHdrCst+len(w.Writes[0].Body))
	binary.LittleEndian.PutUint32(req[0:4], uint32(len(req))) //nolint:gosec // a captured message
	binary.LittleEndian.PutUint16(req[4:6], w.Writes[0].Hdr.Type)
	binary.LittleEndian.PutUint16(req[6:8], w.Writes[0].Hdr.Flags)
	binary.LittleEndian.PutUint32(req[8:12], 1)
	copy(req[nlMsgHdrCst:], w.Writes[0].Body)

	return writeRowFiles{
		pcaps: map[string][]byte{SideIPA: ipPcap, SideGoip: dropLastRecords(t, ipPcap, 2), SideIPB: ipPcap},
		rcs:   map[string]string{SideIPA: rc, SideGoip: "0", SideIPB: rc},
		write: req,
	}
}

func (f writeRowFiles) lay(t *testing.T, dir string, c Command) {
	t.Helper()
	for _, side := range Sides() {
		writeInDir(t, dir, c.CaptureName(side), f.pcaps[side])
		writeInDir(t, dir, c.StdoutName(side), nil)
		if rc, ok := f.rcs[side]; ok {
			writeInDir(t, dir, c.RCName(side), []byte(rc+"\n"))
		}
	}
	if f.write != nil {
		writeInDir(t, dir, c.WriteName(), f.write)
	}
}

// writeCleanWriteRow lays out a write row as a perfect goip run would.
func writeCleanWriteRow(t *testing.T, dir string, c Command) {
	t.Helper()
	corpusWriteRow(t, c).lay(t, dir, c)
}

func mustReadBytes(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestCompareWriteRow is the comparator over write rows: the clean run every
// row of the table produces from its corpus capture, and the ways a side can
// rule the comparison out.
//
// go test ./internal/goipparity/ -run TestCompareWriteRow
func TestCompareWriteRow(t *testing.T) {
	al := testAllowlist(t)
	enslave := lookupPtr(t, "link set la1 down master bond0")
	refused := lookupPtr(t, "link set la1 master bond0")
	add := lookupPtr(t, "link add d0 type dummy")
	routeNFS := lookupPtr(t, "route replace 192.0.2.50/32 dev d0 congctl lock cubic rto_min 3ms initcwnd 32 initrwnd 32 quickack 1")
	blackhole := lookupPtr(t, "route add blackhole 198.51.100.0/24")
	type result struct {
		status Status
		err    error
	}
	tests := []struct {
		description string
		c           *Command
		edit        func(f *writeRowFiles)
		expected    result
	}{
		{description: "positive: enslave, two lookups and the write, is a pass", c: enslave,
			expected: result{status: StatusPass}},
		{description: "positive: add, the write file alone, is a pass", c: add,
			expected: result{status: StatusPass}},
		{description: "positive: a write the kernel refused passes on its request", c: refused,
			expected: result{status: StatusPass}},
		{description: "positive: a route write, the d0 lookup and the metrics, is a pass", c: routeNFS,
			expected: result{status: StatusPass}},
		{description: "positive: a dev-less route write, the file alone, is a pass", c: blackhole,
			expected: result{status: StatusPass}},
		// testAllowlist gates only `link show`, so a divergence on this row
		// is a WARN here; on the committed allowlist it gates and fails.
		{description: "negative: a route write with RTAX_LOCK changed is a divergence", c: routeNFS,
			edit:     func(f *writeRowFiles) { f.write[len(f.write)-1] ^= 0xff },
			expected: result{status: StatusWarn}},
		{description: "negative: different write flags are an ungated warning", c: enslave,
			edit: func(f *writeRowFiles) {
				binary.LittleEndian.PutUint16(f.write[6:8], binary.LittleEndian.Uint16(f.write[6:8])&^0x4)
			},
			expected: result{status: StatusWarn}},
		{description: "negative: goip exiting non-zero fails", c: enslave,
			edit:     func(f *writeRowFiles) { f.rcs[SideGoip] = "1" },
			expected: result{status: StatusFail, err: ErrWriteSide}},
		{description: "negative: the two ip runs disagreeing fails", c: refused,
			edit:     func(f *writeRowFiles) { f.rcs[SideIPB] = "0" },
			expected: result{status: StatusFail, err: ErrWriteSide}},
		{description: "negative: a write file that is not a request fails", c: enslave,
			edit:     func(f *writeRowFiles) { f.write = f.write[:8] },
			expected: result{status: StatusFail, err: nlparity.ErrBadWriteFile}},
		{description: "negative: goip putting the write on the wire fails", c: enslave,
			edit:     func(f *writeRowFiles) { f.pcaps[SideGoip] = f.pcaps[SideIPA] },
			expected: result{status: StatusFail}},
		{description: "boundary: no write file is MISSING", c: enslave,
			edit:     func(f *writeRowFiles) { f.write = nil },
			expected: result{status: StatusMissing}},
		{description: "corner: no exit status is MISSING", c: add,
			edit:     func(f *writeRowFiles) { delete(f.rcs, SideIPB) },
			expected: result{status: StatusMissing}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			dir := t.TempDir()
			f := corpusWriteRow(t, *tc.c)
			if tc.edit != nil {
				tc.edit(&f)
			}
			f.lay(t, dir, *tc.c)
			r := compareOne(dir, *tc.c, al)
			if r.Status != tc.expected.status {
				t.Fatalf("status = %v, want %v (err %v, findings %v)", r.Status, tc.expected.status,
					r.Err, r.Netlink.Findings)
			}
			if tc.expected.err != nil && !errors.Is(r.Err, tc.expected.err) {
				t.Fatalf("err = %v, want %v", r.Err, tc.expected.err)
			}
		})
	}

	t.Run("positive: every write row in the table passes on its own corpus capture", func(t *testing.T) {
		for _, c := range Commands() {
			if !c.Write {
				continue
			}
			dir := t.TempDir()
			writeCleanWriteRow(t, dir, c)
			if r := compareOne(dir, c, al); r.Status != StatusPass {
				t.Errorf("%s: status %v, err %v, findings %v", c.Slug, r.Status, r.Err, r.Netlink.Findings)
			}
		}
	})
}
