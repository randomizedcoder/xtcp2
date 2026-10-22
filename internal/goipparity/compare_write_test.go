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
	tdAddrFlush             = "../../pkg/xtcpnl/testdata/7_1_4/link-write/addr-flush"
	linkWriteSlugPrefixCst  = "link_write_"
	routeWriteSlugPrefixCst = "route_write_"
	addrWriteSlugPrefixCst  = "addr_write_"
	// addrFlushSlugPrefixCst names a flush row, whose corpus slug is
	// flush_ and the rest.
	addrFlushSlugPrefixCst = "addr_flush_"
	// addrRestoreSlugPrefixCst names a restore row, whose corpus slug is
	// restore_ and the rest, under tdAddrSave.
	addrRestoreSlugPrefixCst = "addr_restore_"
	// routeRestoreSlugCst is G2-T02's one route restore row.
	routeRestoreSlugCst = "route_restore"
	// routeFlushSlugCst and routeFlushStatsSlugCst are G2-T03's flush write
	// rows, both on route_flush's capture.
	routeFlushSlugCst      = "route_flush"
	routeFlushStatsSlugCst = "route_flush_stats"
	// neighWriteSlugPrefixCst names G3-T01's neigh write rows, each on the
	// capture of its own slug under tdRtnl; neighFlushSlugCst and
	// neighFlushStatsSlugCst are its flush write rows, both on neigh_flush.
	neighWriteSlugPrefixCst = "neigh_"
	neighFlushSlugCst       = "neigh_flush"
	neighFlushStatsSlugCst  = "neigh_flush_stats"
	tdAddrSave              = "../../pkg/xtcpnl/testdata/7_1_4/link-write/addr-save"
	// tdRtnl is the C1 rtnetlink corpus, route_restore\'s among it.
	tdRtnl           = "../../pkg/xtcpnl/testdata/7_1_4/link-write/rtnl"
	pcapGlobalHdrCst = 24
	pcapRecordHdrCst = 16
	// nlmonHdrCst is the cooked header LINKTYPE_NETLINK puts before each
	// datagram.
	nlmonHdrCst = 16
	// rtmNewAddrCst, rtmDelAddrCst and nlmFRequestCst are RTM_NEWADDR,
	// RTM_DELADDR and NLM_F_REQUEST.
	rtmNewAddrCst  = 20
	rtmNewRouteCst = 24
	rtmDelRouteCst = 25
	rtmDelAddrCst  = 21
	rtmDelNeighCst = 29
	nlmFRequestCst = 0x1
	// nlMsgHdrCst is sizeof(struct nlmsghdr). Spelled here because this
	// package may not import pkg/xtcpnl (TestParityCannotOpenASocket).
	nlMsgHdrCst = 16
)

// dropLastRecords returns a pcap without its last n records. The write
// corpus holds one datagram per record, and a write and its NLMSG_ERROR are
// the last two, so dropping them leaves exactly the lookups goip would send.
func dropLastRecords(t *testing.T, b []byte, n int) []byte {
	t.Helper()
	offs := pcapRecords(t, b)
	if len(offs) < n {
		t.Fatalf("pcap holds %d records, cannot drop %d", len(offs), n)
	}
	if len(offs) == n {
		return append([]byte(nil), b[:pcapGlobalHdrCst]...)
	}
	return append([]byte(nil), b[:offs[len(offs)-n]]...)
}

// pcapRecords returns the byte offset of each record in a pcap.
func pcapRecords(t *testing.T, b []byte) []int {
	t.Helper()
	var offs []int
	for off := pcapGlobalHdrCst; off < len(b); {
		if off+pcapRecordHdrCst > len(b) {
			t.Fatalf("truncated pcap record header at %d", off)
		}
		offs = append(offs, off)
		off += pcapRecordHdrCst + int(binary.LittleEndian.Uint32(b[off+8:off+12]))
	}
	return offs
}

// dropDelRecords returns a pcap without the records that carry RTM_DELADDR
// or RTM_DELROUTE requests: a flush capture less its DEL batch, which is what goip sends
// while it writes the batch to GOIP_WRITE_OUT.
func dropDelRecords(t *testing.T, b []byte) []byte {
	t.Helper()
	out := append([]byte(nil), b[:pcapGlobalHdrCst]...)
	offs := pcapRecords(t, b)
	for i, off := range offs {
		end := len(b)
		if i+1 < len(offs) {
			end = offs[i+1]
		}
		msg := off + pcapRecordHdrCst + nlmonHdrCst
		typ := uint16(0)
		if msg+8 <= end {
			typ = binary.LittleEndian.Uint16(b[msg+4 : msg+6])
		}
		if (typ == rtmDelAddrCst || typ == rtmDelRouteCst || typ == rtmDelNeighCst) &&
			binary.LittleEndian.Uint16(b[msg+6:msg+8])&nlmFRequestCst != 0 {
			continue
		}
		out = append(out, b[off:end]...)
	}
	return out
}

// keepBeforeRequest returns a pcap cut at the first record that carries a
// request of type typ: a restore capture less its writes and their acks,
// which is the link dump goip sends while it writes the rest.
func keepBeforeRequest(t *testing.T, b []byte, typ uint16) []byte {
	t.Helper()
	for _, off := range pcapRecords(t, b) {
		msg := off + pcapRecordHdrCst + nlmonHdrCst
		if msg+8 <= len(b) && binary.LittleEndian.Uint16(b[msg+4:msg+6]) == typ &&
			binary.LittleEndian.Uint16(b[msg+6:msg+8])&nlmFRequestCst != 0 {
			return append([]byte(nil), b[:off]...)
		}
	}
	return append([]byte(nil), b...)
}

// writeRowFiles is one write row's files as a perfect run would leave them:
// ip_a and ip_b are the corpus capture, goip is the same capture without the
// write, and goip's GOIP_WRITE_OUT file is ip's write request. edit, when set,
// changes them before they are written.
type writeRowFiles struct {
	pcaps map[string][]byte
	rcs   map[string]string
	write []byte
	// stdout is every side's stdout: empty, except a flush row's ip.out.
	stdout []byte
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
	if slug, ok := strings.CutPrefix(c.Slug, addrFlushSlugPrefixCst); ok {
		return corpusFlushRow(t, c, filepath.Join(tdAddrFlush, "flush_"+slug))
	}
	if slug, ok := strings.CutPrefix(c.Slug, addrRestoreSlugPrefixCst); ok {
		return corpusRestoreRow(t, c, filepath.Join(tdAddrSave, "restore_"+slug), rtmNewAddrCst)
	}
	if c.Slug == routeFlushSlugCst || c.Slug == routeFlushStatsSlugCst {
		return corpusFlushRow(t, c, filepath.Join(tdRtnl, "route_flush"))
	}
	if c.Slug == routeRestoreSlugCst {
		return corpusRestoreRow(t, c, filepath.Join(tdRtnl, "route_restore"), rtmNewRouteCst)
	}
	if c.Slug == neighFlushSlugCst || c.Slug == neighFlushStatsSlugCst {
		return corpusFlushRow(t, c, filepath.Join(tdRtnl, neighFlushSlugCst))
	}
	if strings.HasPrefix(c.Slug, neighWriteSlugPrefixCst) {
		dir = filepath.Join(tdRtnl, c.Slug)
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

	return writeRowFiles{
		pcaps: map[string][]byte{SideIPA: ipPcap, SideGoip: dropLastRecords(t, ipPcap, 2), SideIPB: ipPcap},
		rcs:   map[string]string{SideIPA: rc, SideGoip: "0", SideIPB: rc},
		write: writeRequest(w.Writes[0], 1),
	}
}

// corpusFlushRow is a flush row's files as a perfect run would leave them:
// goip's capture is ip's less the DEL batch, and its file is the batch, every
// DEL the row's Writes count names. Its round-2 reply stays ip's (empty);
// goip's real one is the pre-flush dump, which CompareWrites leaves out by
// rule, and TestCompareWritesBatch is where that is shown.
func corpusFlushRow(t *testing.T, c Command, dir string) writeRowFiles {
	t.Helper()
	ipPcap := mustReadBytes(t, filepath.Join(dir, "ip.pcap"))
	rc := strings.TrimSpace(mustRead(t, filepath.Join(dir, "ip.rc")))
	capture, err := nlparity.ParseRouteCaptureFile(filepath.Join(dir, "ip.pcap"))
	if err != nil {
		t.Fatal(err)
	}
	w, err := nlparity.SplitWrites(nlparity.SegmentCapture(capture), c.WriteCount())
	if err != nil {
		t.Fatal(err)
	}
	file := make([]byte, 0, len(w.Writes)*nlMsgHdrCst)
	var seq uint32
	for _, m := range w.Writes {
		seq++
		file = append(file, writeRequest(m, seq)...)
	}
	return writeRowFiles{
		pcaps:  map[string][]byte{SideIPA: ipPcap, SideGoip: dropDelRecords(t, ipPcap), SideIPB: ipPcap},
		rcs:    map[string]string{SideIPA: rc, SideGoip: "0", SideIPB: rc},
		write:  file,
		stdout: mustReadBytes(t, filepath.Join(dir, "ip.out")),
	}
}

// corpusRestoreRow is a restore row's files as a perfect run would leave
// them: goip's capture is ip's link dump alone, and its file is ip's writes
// of type typ, each at the next seq. ip's stdout is empty; its stderr, the
// kernel's EEXIST answers, is not compared.
func corpusRestoreRow(t *testing.T, c Command, dir string, typ uint16) writeRowFiles {
	t.Helper()
	f := corpusFlushRow(t, c, dir)
	f.pcaps[SideGoip] = keepBeforeRequest(t, f.pcaps[SideIPA], typ)
	return f
}

// writeRequest is one captured write as goip writes it to GOIP_WRITE_OUT,
// with its own seq.
func writeRequest(m nlparity.Msg, seq uint32) []byte {
	req := make([]byte, nlMsgHdrCst+len(m.Body))
	binary.LittleEndian.PutUint32(req[0:4], uint32(len(req))) //nolint:gosec // a captured message
	binary.LittleEndian.PutUint16(req[4:6], m.Hdr.Type)
	binary.LittleEndian.PutUint16(req[6:8], m.Hdr.Flags)
	binary.LittleEndian.PutUint32(req[8:12], seq)
	copy(req[nlMsgHdrCst:], m.Body)
	return req
}

func (f writeRowFiles) lay(t *testing.T, dir string, c Command) {
	t.Helper()
	for _, side := range Sides() {
		writeInDir(t, dir, c.CaptureName(side), f.pcaps[side])
		writeInDir(t, dir, c.StdoutName(side), f.stdout)
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
	flushDev := lookupPtr(t, "addr flush dev d0")
	flushMany := lookupPtr(t, "addr flush d0")
	flushStats := lookupPtr(t, "-s addr flush dev d0")
	flushPrimary := lookupPtr(t, "addr flush dev d0 primary")
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
		{description: "positive: a flush, four DELs and a round-2 dump, is a pass", c: flushDev,
			expected: result{status: StatusPass}},
		{description: "positive: 120 DELs in three of ip's datagrams are a pass", c: flushMany,
			expected: result{status: StatusPass}},
		{description: "positive: primary, one DEL and no round 2, is a pass", c: flushPrimary,
			expected: result{status: StatusPass}},
		{description: "positive: -s's round text is compared on stdout and passes", c: flushStats,
			expected: result{status: StatusPass}},
		{description: "negative: one DEL missing from goip's file fails, unsuppressibly", c: flushDev,
			edit:     func(f *writeRowFiles) { f.write = f.write[:len(f.write)*3/4] },
			expected: result{status: StatusFail}},
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

	t.Run("negative: a flush row whose Writes says 1 cannot split ip's batch of four", func(t *testing.T) {
		dir := t.TempDir()
		corpusWriteRow(t, *flushDev).lay(t, dir, *flushDev)
		one := *flushDev
		one.Writes = 1
		r := compareOne(dir, one, al)
		if r.Status != StatusFail || !errors.Is(r.Err, nlparity.ErrWriteCount) {
			t.Fatalf("status = %v, err = %v; want FAIL with ErrWriteCount", r.Status, r.Err)
		}
	})

	t.Run("positive: flush_many is 123 transactions on both sides: the lookup, two dumps and 120 DELs", func(t *testing.T) {
		dir := t.TempDir()
		corpusWriteRow(t, *flushMany).lay(t, dir, *flushMany)
		r := compareOne(dir, *flushMany, al)
		if got := [2]int{r.Netlink.RefTxns, r.Netlink.SubTxns}; got != [2]int{123, 123} {
			t.Fatalf("txns = %v, want [123 123]", got)
		}
	})

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
