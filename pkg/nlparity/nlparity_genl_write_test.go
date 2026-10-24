package nlparity

import (
	"bytes"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// tcpmSeg reads a C2-T01 tcp_metrics row's ip.pcap, segmented.
func tcpmSeg(t *testing.T, row string) (Capture, Segmentation) {
	t.Helper()
	c, err := ParseCaptureFile(filepath.Join(tdGenl, row, "ip.pcap"), unix.NETLINK_GENERIC)
	if err != nil {
		t.Fatal(err)
	}
	return c, SegmentCapture(c)
}

// cloneTxns copies a segmentation's transactions deep enough that a test
// may reorder or edit one side's replies and bodies.
func cloneTxns(s Segmentation) Segmentation {
	out := s
	out.Txns = make([]Txn, len(s.Txns))
	for i, txn := range s.Txns {
		txn.Replies = slices.Clone(txn.Replies)
		for j := range txn.Replies {
			txn.Replies[j].Body = slices.Clone(txn.Replies[j].Body)
		}
		out.Txns[i] = txn
	}
	return out
}

// TestIsWrite checks the write test over both protocols.
//
// go test ./pkg/nlparity/ -run TestIsWrite
func TestIsWrite(t *testing.T) {
	genl := func(name string, cmd uint8) Msg {
		return Msg{Proto: unix.NETLINK_GENERIC, Genl: name, Hdr: xtcpnl.NlMsgHdr{Type: 31}, Body: []byte{cmd, 1, 0, 0}}
	}
	xfrm := func(typ uint16) Msg {
		return Msg{Proto: unix.NETLINK_XFRM, Hdr: xtcpnl.NlMsgHdr{Type: typ, Flags: unix.NLM_F_REQUEST}}
	}
	tests := []struct {
		description string
		m           Msg
		expected    bool
	}{
		{"positive: a tcp_metrics DEL", genl(xtcpnl.TCPMetricsGenlName, xtcpnl.TCPMetricsCmdDel), true},
		{"positive: an RTM_NEWLINK on rtnetlink", Msg{Hdr: xtcpnl.NlMsgHdr{Type: unix.RTM_NEWLINK}}, true},
		{"negative: a tcp_metrics GET", genl(xtcpnl.TCPMetricsGenlName, xtcpnl.TCPMetricsCmdGet), false},
		{"negative: a family never named", genl("", xtcpnl.TCPMetricsCmdDel), false},
		{"negative: RTM_NEWLINK's number on generic netlink is the controller",
			Msg{Proto: unix.NETLINK_GENERIC, Hdr: xtcpnl.NlMsgHdr{Type: unix.RTM_NEWLINK}, Body: []byte{3, 0, 0, 0}}, false},
		{"negative: an RTM_GETLINK", Msg{Hdr: xtcpnl.NlMsgHdr{Type: unix.RTM_GETLINK}}, false},
		{"boundary: a DEL too short for its genlmsghdr",
			Msg{Proto: unix.NETLINK_GENERIC, Genl: xtcpnl.TCPMetricsGenlName, Hdr: xtcpnl.NlMsgHdr{Type: 31}, Body: []byte{2, 1, 0}}, false},
		{"corner: NLMSG_ERROR on generic netlink",
			Msg{Proto: unix.NETLINK_GENERIC, Hdr: xtcpnl.NlMsgHdr{Type: unix.NLMSG_ERROR}, Body: []byte{2, 1, 0, 0}}, false},
		{"positive: an XFRM DELSA", xfrm(xtcpnl.XfrmMsgDelSA), true},
		{"positive: an XFRM FLUSHSA", xfrm(xtcpnl.XfrmMsgFlushSA), true},
		{"positive: an XFRM ALLOCSPI", xfrm(xtcpnl.XfrmMsgAllocSPI), true},
		{"negative: an XFRM GETSA", xfrm(xtcpnl.XfrmMsgGetSA), false},
		{"positive: an XFRM NEWPOLICY", xfrm(xtcpnl.XfrmMsgNewPolicy), true},
		{"positive: an XFRM SETDEFAULT", xfrm(xtcpnl.XfrmMsgSetDefault), true},
		{"negative: an XFRM GETDEFAULT", xfrm(xtcpnl.XfrmMsgGetDefault), false},
		{"positive: an XFRM FLUSHPOLICY, no header", xfrm(xtcpnl.XfrmMsgFlushPolicy), true},
		{"corner: UPDSA, RTM_GETROUTE's number, is a write on XFRM", xfrm(xtcpnl.XfrmMsgUpdSA), true},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := IsWrite(tc.m); got != tc.expected {
				t.Errorf("IsWrite = %v, want %v", got, tc.expected)
			}
		})
	}
}

// TestParseWriteRequestsProto reads tcp_metrics write files: the captured
// DELs back to back, labeled by the families map.
//
// go test ./pkg/nlparity/ -run TestParseWriteRequestsProto
func TestParseWriteRequestsProto(t *testing.T) {
	_, seg := tcpmSeg(t, "tcpm_flush_prefix")
	split, err := SplitWrites(seg, 2)
	if err != nil {
		t.Fatal(err)
	}
	file := make([]byte, 0, len(split.Writes)*genlHdrLen)
	for _, m := range split.Writes {
		file = append(file, wholeRequest(m)...)
	}
	id := split.Writes[0].Hdr.Type
	get := wholeRequest(seg.Txns[1].Request)
	tests := []struct {
		description string
		b           []byte
		proto       uint16
		families    map[uint16]string
		expected    int
		err         error
	}{
		{description: "positive: the two flush DELs", b: file, proto: unix.NETLINK_GENERIC,
			families: map[uint16]string{id: xtcpnl.TCPMetricsGenlName}, expected: 2},
		{description: "negative: the family unnamed", b: file, proto: unix.NETLINK_GENERIC, err: ErrBadWriteFile},
		{description: "negative: named for another family", b: file, proto: unix.NETLINK_GENERIC,
			families: map[uint16]string{id: "nlctrl"}, err: ErrBadWriteFile},
		{description: "negative: the dump's GET", b: get, proto: unix.NETLINK_GENERIC,
			families: map[uint16]string{id: xtcpnl.TCPMetricsGenlName}, err: ErrBadWriteFile},
		{description: "boundary: read as rtnetlink, type 31 is RTM_NEWNEIGHTBL's GET side, not a write", b: file,
			proto: unix.NETLINK_ROUTE, err: ErrBadWriteFile},
		{description: "corner: empty", proto: unix.NETLINK_GENERIC, err: ErrBadWriteFile},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			msgs, err := ParseWriteRequestsProto(tc.b, tc.proto, tc.families)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err %v, want %v", err, tc.err)
			}
			if len(msgs) != tc.expected {
				t.Fatalf("%d messages, want %d", len(msgs), tc.expected)
			}
			for i, m := range msgs {
				if m.Genl != xtcpnl.TCPMetricsGenlName || !bytes.Equal(m.Body, split.Writes[i].Body) {
					t.Errorf("message %d: %s %x", i, MsgName(m), m.Body)
				}
			}
		})
	}
}

// wholeRequest is a captured message's bytes as it was sent.
func wholeRequest(m Msg) []byte {
	b := make([]byte, xtcpnl.NlMsgHdrSizeCst, xtcpnl.NlMsgHdrSizeCst+len(m.Body))
	h := m.Hdr
	b[0], b[1], b[2], b[3] = byte(h.Len), byte(h.Len>>8), byte(h.Len>>16), byte(h.Len>>24)
	b[4], b[5] = byte(h.Type), byte(h.Type>>8)
	b[6], b[7] = byte(h.Flags), byte(h.Flags>>8)
	return append(b, m.Body...)
}

// setAttrByte flips the last byte of the first of types in a genl body, the
// low byte of an address.
func setAttrByte(t *testing.T, body []byte, types ...uint16) {
	t.Helper()
	attrs, _ := SubAttrs(body[genlHdrLen:])
	for _, a := range attrs {
		if slices.Contains(types, a.BareType()) {
			a.Val[len(a.Val)-1] ^= 1
			return
		}
	}
	t.Fatalf("no attribute of %v", types)
}

// TestTCPMetricsKeys checks tcp_metrics' object key: a dump whose entries
// come in another order, as another namespace's hash puts them, compares
// clean, and a flush's DELs pair by entry, where an rtnetlink batch keeps
// its order.
//
// go test ./pkg/nlparity/ -run TestTCPMetricsKeys
func TestTCPMetricsKeys(t *testing.T) {
	_, show := tcpmSeg(t, "tcpm_show")
	dump := 1 // the GETFAMILY, then the dump
	reversed := cloneTxns(show)
	r := reversed.Txns[dump].Replies
	slices.Reverse(r[:len(r)-1]) // the entries, NLMSG_DONE last
	edited := cloneTxns(show)
	setAttrByte(t, edited.Txns[dump].Replies[0].Body, xtcpnl.TCPMetricsAttrSaddrIPv4, xtcpnl.TCPMetricsAttrSaddrIPv6)
	dropped := cloneTxns(show)
	dropped.Txns[dump].Replies = dropped.Txns[dump].Replies[1:]

	_, flush := tcpmSeg(t, "tcpm_flush_prefix")
	split, err := SplitWrites(flush, 2)
	if err != nil {
		t.Fatal(err)
	}
	swapped := []Msg{split.Writes[1], split.Writes[0]}
	changed := slices.Clone(split.Writes)
	changed[0].Body = slices.Clone(changed[0].Body)
	setAttrByte(t, changed[0].Body, xtcpnl.TCPMetricsAttrSaddrIPv4, xtcpnl.TCPMetricsAttrSaddrIPv6)

	tests := []struct {
		description string
		diff        func() []Divergence
		expected    int // findings, 0 for clean
	}{
		{description: "positive: the dump against itself", diff: func() []Divergence { return Diff("show", show, show) }},
		{description: "positive: the entries in reverse order", diff: func() []Divergence { return Diff("show", show, reversed) }},
		{description: "negative: one entry's source changed: its key is absent from goip, a new one present",
			diff: func() []Divergence { return Diff("show", show, edited) }, expected: 2},
		{description: "negative: one entry fewer", diff: func() []Divergence { return Diff("show", show, dropped) }, expected: 1},
		{description: "positive: the flush's DELs swapped", diff: func() []Divergence {
			return DiffWrites("flush", split, split.others(), swapped)
		}},
		{description: "negative: one DEL's source changed", diff: func() []Divergence {
			return DiffWrites("flush", split, split.others(), changed)
		}, expected: 1},
		{description: "boundary: one DEL missing: the count, and the one left paired with the first", diff: func() []Divergence {
			return DiffWrites("flush", split, split.others(), split.Writes[:1])
		}, expected: 3},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := tc.diff()
			if len(got) != tc.expected {
				t.Errorf("%d divergences, want %d: %v", len(got), tc.expected, got)
			}
		})
	}
}

// TestByObjectKey pins the batch order byObjectKey returns.
//
// go test ./pkg/nlparity/ -run TestByObjectKey
func TestByObjectKey(t *testing.T) {
	_, flush := tcpmSeg(t, "tcpm_flush_prefix")
	split, err := SplitWrites(flush, 2)
	if err != nil {
		t.Fatal(err)
	}
	a, b := split.Writes[0], split.Writes[1]
	route := Msg{Hdr: xtcpnl.NlMsgHdr{Type: unix.RTM_DELADDR}, Body: []byte{2, 24, 0, 0, 1, 0, 0, 0}}
	route2 := Msg{Hdr: xtcpnl.NlMsgHdr{Type: unix.RTM_DELADDR}, Body: []byte{2, 24, 0, 0, 0, 0, 0, 0}}
	tests := []struct {
		description string
		in          []Msg
		expected    []Msg
	}{
		{description: "positive: tcp_metrics DELs sorted by key", in: []Msg{a, b}, expected: []Msg{b, a}},
		{description: "positive: and the same from either order", in: []Msg{b, a}, expected: []Msg{b, a}},
		{description: "negative: rtnetlink DELs keep send order", in: []Msg{route, route2}, expected: []Msg{route, route2}},
		{description: "corner: a mixed batch keeps send order", in: []Msg{a, route, b}, expected: []Msg{a, route, b}},
		{description: "boundary: empty", in: nil, expected: []Msg{}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := byObjectKey(tc.in)
			if len(got) != len(tc.expected) {
				t.Fatalf("%d messages, want %d", len(got), len(tc.expected))
			}
			for i := range got {
				if !bytes.Equal(got[i].Body, tc.expected[i].Body) || got[i].Hdr.Type != tc.expected[i].Hdr.Type {
					t.Errorf("message %d: %s %x, want %s %x", i, MsgName(got[i]), got[i].Body,
						MsgName(tc.expected[i]), tc.expected[i].Body)
				}
			}
		})
	}
}

// tcpmReply is the reply about addr in a row's capture: a dump entry or a
// GET's answer, the first whose key names it.
func tcpmReply(t *testing.T, row, addrHex string) Msg {
	t.Helper()
	_, seg := tcpmSeg(t, row)
	for _, txn := range seg.Txns {
		for _, m := range txn.Replies {
			if k, ok := genlObjectKey(m); ok && strings.Contains(k, "/addr="+addrHex+"/") {
				return m
			}
		}
	}
	t.Fatalf("%s: no reply about %s", row, addrHex)
	return Msg{}
}

// TestNormalizeTCPMetrics checks a tcp_metrics entry's clocks and RTTs are
// normalized away and nothing else is: the same entry from two captures,
// made by two connections at two times, compares equal.
//
// go test ./pkg/nlparity/ -run TestNormalizeTCPMetrics
func TestNormalizeTCPMetrics(t *testing.T) {
	show := tcpmReply(t, "tcpm_show", "c0000201")
	get := tcpmReply(t, "tcpm_get", "c0000201")
	other := tcpmReply(t, "tcpm_show", "c0000202")
	cwnd := Msg{Proto: show.Proto, Genl: show.Genl, Hdr: show.Hdr, Body: slices.Clone(show.Body)}
	attrs, _ := SubAttrs(cwnd.Body[genlHdrLen:])
	for _, a := range attrs {
		if a.BareType() != xtcpnl.TCPMetricsAttrVals {
			continue
		}
		vals, _ := SubAttrs(a.Val)
		for _, v := range vals {
			if v.BareType() == xtcpnl.TCPMetricCwnd+1 {
				v.Val[0]++
			}
		}
	}
	fou := show
	fou.Genl = "fou"
	tests := []struct {
		description string
		a, b        Msg
		expected    bool // equal after normalization
	}{
		{description: "positive: one entry, two captures, two ages and RTTs", a: show, b: get, expected: true},
		{description: "negative: another entry", a: show, b: other},
		{description: "negative: cwnd changed", a: show, b: cwnd},
		{description: "corner: the same bytes under another family are not normalized", a: show, b: fou},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			before := slices.Clone(tc.a.Body)
			na, nb := NormalizeMsg(tc.a), NormalizeMsg(tc.b)
			if got := bytes.Equal(na.Body, nb.Body); got != tc.expected {
				t.Errorf("equal %v, want %v\n a %x\n b %x", got, tc.expected, na.Body, nb.Body)
			}
			if !bytes.Equal(tc.a.Body, before) {
				t.Error("the input was edited")
			}
		})
	}
	t.Run("boundary: the raw entries differ, so the row above tests something", func(t *testing.T) {
		if bytes.Equal(show.Body, get.Body) {
			t.Error("the two captures' entries are already equal")
		}
	})
}
