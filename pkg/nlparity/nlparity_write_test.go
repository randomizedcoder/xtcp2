package nlparity

import (
	"encoding/binary"
	"errors"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// tdLinkWrite is the link-write corpus: one `ip link …` write per directory.
const tdLinkWrite = "../xtcpnl/testdata/7_1_4/link-write/write"

func segmentLinkWrite(t *testing.T, slug string) Segmentation {
	t.Helper()
	c, err := ParseRouteCaptureFile(tdLinkWrite + "/" + slug + "/ip.pcap")
	if err != nil {
		t.Fatal(err)
	}
	return SegmentCapture(c)
}

// wireOf lays a message out as the bytes GOIP_WRITE_OUT would hold.
func wireOf(m Msg) []byte {
	b := make([]byte, xtcpnl.NlMsgHdrSizeCst+len(m.Body))
	binary.LittleEndian.PutUint32(b[0:4], uint32(len(b))) //nolint:gosec // a captured message
	binary.LittleEndian.PutUint16(b[4:6], m.Hdr.Type)
	binary.LittleEndian.PutUint16(b[6:8], m.Hdr.Flags)
	binary.LittleEndian.PutUint32(b[8:12], 1) // goip's own seq
	copy(b[xtcpnl.NlMsgHdrSizeCst:], m.Body)
	return b
}

// TestSplitWrite pins where the write is: the last transaction, and the only
// one, of every corpus row.
//
// go test ./pkg/nlparity/ -run TestSplitWrite
func TestSplitWrite(t *testing.T) {
	type result struct {
		lookups   int
		writeType uint16
		err       error
	}
	twoWrites := func(t *testing.T) Segmentation {
		s := segmentLinkWrite(t, "add_dummy")
		s.Txns = append(s.Txns, s.Txns[0])
		return s
	}
	readOnly := func(t *testing.T) Segmentation {
		s := segmentLinkWrite(t, "enslave_down")
		s.Txns = s.Txns[:2]
		return s
	}
	tests := []struct {
		description string
		seg         func(t *testing.T) Segmentation
		expected    result
	}{
		{description: "positive: add resolves nothing, so the write is the only transaction",
			seg:      func(t *testing.T) Segmentation { return segmentLinkWrite(t, "add_dummy") },
			expected: result{lookups: 0, writeType: unix.RTM_NEWLINK}},
		{description: "positive: enslave resolves master then dev, then writes",
			seg:      func(t *testing.T) Segmentation { return segmentLinkWrite(t, "enslave_down") },
			expected: result{lookups: 2, writeType: unix.RTM_NEWLINK}},
		{description: "positive: a refused write is still the last transaction",
			seg:      func(t *testing.T) Segmentation { return segmentLinkWrite(t, "enslave_up") },
			expected: result{lookups: 2, writeType: unix.RTM_NEWLINK}},
		{description: "positive: delete is an RTM_DELLINK after one lookup",
			seg:      func(t *testing.T) Segmentation { return segmentLinkWrite(t, "del_bond") },
			expected: result{lookups: 1, writeType: unix.RTM_DELLINK}},
		{description: "negative: lookups only, the write never sent", seg: readOnly,
			expected: result{err: ErrNoWriteTxn}},
		{description: "negative: an empty capture",
			seg:      func(*testing.T) Segmentation { return Segmentation{} },
			expected: result{err: ErrNoWriteTxn}},
		{description: "corner: two writes in one capture", seg: twoWrites,
			expected: result{err: ErrNoWriteTxn}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			lookups, write, err := SplitWrite(tc.seg(t))
			var got result
			if err != nil {
				got.err = tc.expected.err
				if !errors.Is(err, tc.expected.err) {
					t.Fatalf("err = %v, want %v", err, tc.expected.err)
				}
			} else {
				got.lookups, got.writeType = len(lookups.Txns), write.Request.Hdr.Type
			}
			if got != tc.expected {
				t.Fatalf("got %+v, want %+v", got, tc.expected)
			}
		})
	}
}

// TestParseWriteRequest covers the GOIP_WRITE_OUT file's shape.
//
// go test ./pkg/nlparity/ -run TestParseWriteRequest
func TestParseWriteRequest(t *testing.T) {
	_, w, err := SplitWrite(segmentLinkWrite(t, "add_dummy"))
	if err != nil {
		t.Fatal(err)
	}
	good := wireOf(w.Request)
	mutate := func(f func(b []byte) []byte) []byte { return f(append([]byte(nil), good...)) }
	tests := []struct {
		description string
		file        []byte
		expected    error
	}{
		{description: "positive: one RTM_NEWLINK request", file: good},
		{description: "boundary: a header and nothing else is short of nothing but a body",
			file: mutate(func(b []byte) []byte {
				binary.LittleEndian.PutUint32(b, xtcpnl.NlMsgHdrSizeCst)
				return b[:xtcpnl.NlMsgHdrSizeCst]
			})},
		{description: "negative: fewer bytes than a header", file: good[:xtcpnl.NlMsgHdrSizeCst-1],
			expected: ErrBadWriteFile},
		{description: "negative: trailing bytes after the message",
			file: append(append([]byte(nil), good...), 0, 0, 0, 0), expected: ErrBadWriteFile},
		{description: "negative: NLM_F_REQUEST missing",
			file: mutate(func(b []byte) []byte {
				binary.LittleEndian.PutUint16(b[6:8], unix.NLM_F_ACK)
				return b
			}), expected: ErrBadWriteFile},
		{description: "negative: a GET is not a write",
			file: mutate(func(b []byte) []byte {
				binary.LittleEndian.PutUint16(b[4:6], unix.RTM_GETLINK)
				return b
			}), expected: ErrBadWriteFile},
		{description: "negative: empty", file: nil, expected: ErrBadWriteFile},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			_, err := ParseWriteRequest(tc.file)
			if !errors.Is(err, tc.expected) {
				t.Fatalf("err = %v, want %v", err, tc.expected)
			}
		})
	}
}

// TestCompareWrite runs the write-row comparison over corpus captures, with
// the goip side built from the same capture: its lookups are ip's lookups
// and its write file is ip's write request, then one thing is changed.
//
// go test ./pkg/nlparity/ -run TestCompareWrite
func TestCompareWrite(t *testing.T) {
	type side struct {
		sub   Segmentation
		write []byte
	}
	goipOf := func(t *testing.T, slug string, edit func(*side)) (Segmentation, side) {
		ref := segmentLinkWrite(t, slug)
		lookups, w, err := SplitWrite(ref)
		if err != nil {
			t.Fatal(err)
		}
		s := side{sub: lookups, write: wireOf(w.Request)}
		if edit != nil {
			edit(&s)
		}
		return ref, s
	}
	type result struct {
		err      error
		loci     []string
		refTxns  int
		subTxns  int
		hygienic bool
	}
	tests := []struct {
		description string
		slug        string
		edit        func(*side)
		control     bool // use a control diff that saw the same change
		expected    result
	}{
		{description: "positive: identical lookups and write", slug: "enslave_down",
			expected: result{refTxns: 3, subTxns: 3, hygienic: true}},
		{description: "positive: add has no lookups on either side", slug: "add_bond_8023ad",
			expected: result{refTxns: 1, subTxns: 1, hygienic: true}},
		{description: "positive: a refused write compares on its request", slug: "enslave_up",
			expected: result{refTxns: 3, subTxns: 3, hygienic: true}},
		{description: "negative: different write flags are a write-role L2 finding", slug: "set_up",
			edit: func(s *side) { binary.LittleEndian.PutUint16(s.write[6:8], unix.NLM_F_REQUEST) },
			expected: result{refTxns: 2, subTxns: 2, hygienic: true,
				loci: []string{"request:RTM_NEWLINK:header.flags:write"}}},
		{description: "negative: a missing lookup is a transaction-count finding", slug: "enslave_down",
			edit:     func(s *side) { s.sub.Txns = s.sub.Txns[:1] },
			expected: result{refTxns: 3, subTxns: 2, hygienic: true, loci: []string{"txn-count"}}},
		{description: "negative: goip putting a write on the wire is an error", slug: "set_up",
			edit:     func(s *side) { s.sub = segmentLinkWrite(t, "set_up") },
			expected: result{err: ErrGoipSentWrite, refTxns: 2, subTxns: 3, hygienic: true}},
		{description: "corner: a change the control also saw is subtracted", slug: "set_up",
			edit:     func(s *side) { binary.LittleEndian.PutUint16(s.write[6:8], unix.NLM_F_REQUEST) },
			control:  true,
			expected: result{refTxns: 2, subTxns: 2, hygienic: true}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			ref, s := goipOf(t, tc.slug, tc.edit)
			subWrite, err := ParseWriteRequest(s.write)
			if err != nil {
				t.Fatal(err)
			}
			var control []Divergence
			if tc.control {
				lookups, w, serr := SplitWrite(ref)
				if serr != nil {
					t.Fatal(serr)
				}
				control = DiffWrite("c", lookups, s.sub, w.Request, subWrite)
			}
			r := CompareWrite("c", ref, s.sub, subWrite, control, nil)
			if !errors.Is(r.Err, tc.expected.err) {
				t.Fatalf("Err = %v, want %v", r.Err, tc.expected.err)
			}
			var loci []string
			for _, d := range r.Findings {
				loci = append(loci, d.Locus)
			}
			got := result{err: tc.expected.err, loci: loci, refTxns: r.RefTxns, subTxns: r.SubTxns,
				hygienic: len(r.Hygiene) == 0}
			if len(got.loci) != len(tc.expected.loci) || got.refTxns != tc.expected.refTxns ||
				got.subTxns != tc.expected.subTxns || got.hygienic != tc.expected.hygienic {
				t.Fatalf("got %+v, want %+v", got, tc.expected)
			}
			for i := range loci {
				if loci[i] != tc.expected.loci[i] {
					t.Fatalf("finding %d = %q, want %q", i, loci[i], tc.expected.loci[i])
				}
			}
		})
	}
}

// TestWriteControl is D_control between two `ip` captures of a write row.
// Only L2 findings are counted: two captures of fresh namespaces differ in
// reply state (a new bond's random MAC, cacheinfo times), and subtracting
// that from the test diff is what the control is for.
//
// go test ./pkg/nlparity/ -run TestWriteControl
func TestWriteControl(t *testing.T) {
	type result struct {
		findings int
		err      error
	}
	tests := []struct {
		description string
		a, b        string
		expected    result
	}{
		{description: "positive: a capture against itself is clean", a: "enslave_down", b: "enslave_down"},
		{description: "positive: up and down requests differ only in ifi_flags", a: "set_up", b: "set_down",
			expected: result{findings: 1}},
		{description: "negative: ip_b with no write", a: "set_up", b: "",
			expected: result{err: ErrNoWriteTxn}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			a := segmentLinkWrite(t, tc.a)
			var b Segmentation
			if tc.b != "" {
				b = segmentLinkWrite(t, tc.b)
			}
			d, err := WriteControl("c", a, b)
			if !errors.Is(err, tc.expected.err) {
				t.Fatalf("err = %v, want %v", err, tc.expected.err)
			}
			l2 := 0
			for _, x := range d {
				if x.Level == LevelRequest {
					l2++
				}
			}
			if got := (result{findings: l2, err: tc.expected.err}); got != tc.expected {
				t.Fatalf("got %+v (%v), want %+v", got, d, tc.expected)
			}
		})
	}
}
