package nlparity

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// The normalizer's table.
//
// Every positive row reads a real guest capture and asserts BOTH halves of the
// claim: the bytes that had to be zeroed are zero, and the bytes that had to
// survive still hold what the capture recorded. Asserting only the first half
// would pass just as well for a normalizer that blanked the whole payload,
// which is precisely the shortcut nlparity_normalize.go's header argues
// against — a blanked field is a field a goip bug can hide behind.
//
// So the rows are written as span pairs. wantZeroed says "this moved between
// runs and must not be compared"; wantPreserved says "this is stable and IS
// compared", and it is checked against the pre-normalization bytes rather than
// against a literal, so the row cannot drift out of step with the fixture.

// span is a half-open byte range within an attribute payload.
type span struct {
	off, n int
}

// normRow is one normalization case.
//
// filename and msg are mutually exclusive, and a positive row may not set msg
// — the provenance rule the walker and segmenter tables already enforce. A
// hand-built "positive" for a normalizer is circular: it asserts that the
// author zeroed the same offsets the code zeroes.
type normRow struct {
	description string

	filename string // real capture; every message of msgType is examined
	msgType  uint16
	msg      *Msg // constructed message; non-positive rows only

	// find locates the payload under test within one message. It is applied
	// to the original and to the normalized copy, and the two are compared
	// span by span.
	find func(m Msg) [][]byte

	wantZeroed    []span
	wantPreserved []span

	// wantZeroedWasZero records that the zeroed spans were ALREADY zero in
	// the capture, so the zeroing assertion is vacuous for this row and only
	// the preserved half carries weight.
	//
	// It is a field rather than a comment because the default is checked: a
	// row without it must find at least one payload whose original bytes in
	// each zeroed span were non-zero, or the row is asserting that the
	// normalizer zeroed something that was already zero — which passes just
	// as well when the normalizer does nothing at all. RTA_CACHEINFO is the
	// one honest case: measured as 32 zero bytes on every route.
	wantZeroedWasZero bool

	// wantFound is the exact number of payloads the row expects across the
	// capture. A row that found none would otherwise pass by asserting
	// nothing, which is the failure mode a normalization table is most
	// exposed to.
	wantFound int

	// check is for the structural rows, whose claim is about the Msg rather
	// than about a payload.
	check func(t *testing.T, before, after Msg)
}

// topAttr returns every top-level attribute payload of a bare type.
func topAttr(bare uint16) func(Msg) [][]byte {
	return func(m Msg) [][]byte {
		_, attrs, _ := DecodeAttrs(m.Hdr.Type, m.Body)
		var out [][]byte
		for _, a := range attrs {
			if a.BareType() == bare {
				out = append(out, a.Val)
			}
		}
		return out
	}
}

// inet6Attr returns every IFLA_AF_SPEC -> AF_INET6 -> <bare> payload. It is the
// two-level descent SubAttrs is exported for, written out here so the test
// walks the nest independently of normalizeAFSpec rather than calling it.
func inet6Attr(bare uint16) func(Msg) [][]byte {
	return func(m Msg) [][]byte {
		_, attrs, _ := DecodeAttrs(m.Hdr.Type, m.Body)
		var out [][]byte
		for _, a := range attrs {
			if a.BareType() != uint16(unix.IFLA_AF_SPEC) {
				continue
			}
			fams, _ := SubAttrs(a.Val)
			for _, fam := range fams {
				if fam.BareType() != unix.AF_INET6 {
					continue
				}
				inner, _ := SubAttrs(fam.Val)
				for _, in := range inner {
					if in.BareType() == bare {
						out = append(out, in.Val)
					}
				}
			}
		}
		return out
	}
}

// checkNormProvenance enforces normRow's two invariants before anything runs.
func checkNormProvenance(t *testing.T, description, filename string, msg *Msg) {
	t.Helper()

	switch {
	case filename != "" && msg != nil:
		t.Fatalf("row %q sets both filename and msg; they are mutually exclusive", description)
	case filename == "" && msg == nil:
		t.Fatalf("row %q sets neither filename nor msg", description)
	}
	if classOf(description) == "positive" && msg != nil {
		t.Fatalf("row %q is positive but uses a constructed message; a hand-built "+
			"normalization positive only asserts that the author and the code "+
			"picked the same offsets", description)
	}
}

// attrMsg builds a message carrying one attribute, for the truncation rows. The
// family header is zeroed and the attribute follows it, which is the shape
// DecodeAttrs expects.
func attrMsg(msgType uint16, hdrLen int, attrType uint16, val []byte) Msg {
	attr := make([]byte, xtcpnl.RTAttrSizeCst+len(val))
	attr[0] = byte(xtcpnl.RTAttrSizeCst + len(val))
	attr[1] = byte((xtcpnl.RTAttrSizeCst + len(val)) >> 8)
	attr[2] = byte(attrType)
	attr[3] = byte(attrType >> 8)
	copy(attr[xtcpnl.RTAttrSizeCst:], val)

	body := append(make([]byte, hdrLen), attr...)
	return Msg{
		Hdr: xtcpnl.NlMsgHdr{
			Len:  uint32(xtcpnl.NlMsgHdrSizeCst + len(body)),
			Type: msgType,
			Seq:  4242,
			Pid:  99,
		},
		Body: body,
	}
}

// TestNormalizeMsg is the normalization table.
//
// go test ./pkg/nlparity/ -run TestNormalizeMsg
func TestNormalizeMsg(t *testing.T) {
	tests := []normRow{
		{
			// Measured: ffffffff ffffffff 85060000 85060000 and five more
			// in this capture, ten across both namespaces.
			// preferred and valid are INFINITY_LIFE_TIME on every address in
			// the topology because they are static, so the lifetimes stay
			// compared and only cstamp/tstamp go. Keeping the lifetimes is
			// what proves goip read the attribute rather than synthesizing
			// "forever".
			description:   "positive: IFA_CACHEINFO keeps its two lifetimes and loses its two stamps",
			filename:      tdGuest + "/netlink_route_getaddr.pcap",
			msgType:       uint16(unix.RTM_NEWADDR),
			find:          topAttr(uint16(unix.IFA_CACHEINFO)),
			wantZeroed:    []span{{ifaCacheinfoStampsOffCst, ifaCacheinfoStampsLenCst}},
			wantPreserved: []span{{0, 8}},
			wantFound:     6,
		},
		{
			// Measured: e9010000 e9010000 e9010000 00000000, and four more
			// with all three counters differing. confirmed/used/updated are
			// USER_HZ age counters and move between any two captures; refcnt
			// was 0 on every entry, so it stays compared.
			description:   "positive: NDA_CACHEINFO loses its three age counters and keeps refcnt",
			filename:      tdGuest + "/netlink_route_getneigh.pcap",
			msgType:       uint16(unix.RTM_NEWNEIGH),
			find:          topAttr(uint16(unix.NDA_CACHEINFO)),
			wantZeroed:    []span{{ndaCacheinfoAgeOffCst, ndaCacheinfoAgeLenCst}},
			wantPreserved: []span{{12, 4}},
			wantFound:     5,
		},
		{
			// Measured: 32 zero bytes on all 9 routes here, and on all 17
			// across the corpus. Nothing here needs
			// normalizing today, which is exactly why the conservative choice
			// costs nothing — and why clntref, error and used stay compared:
			// if a future capture carries a non-zero refcount, that is a
			// finding worth seeing rather than a field already blanked.
			//
			// Because the capture is all zero, wantZeroedWasZero is set: the
			// zeroing half of this row cannot fail, and the row is really an
			// assertion that clntref/error/used/id SURVIVE. The field is what
			// stops that from being a silent hole in the table.
			description: "positive: RTA_CACHEINFO loses only its four time-derived fields",
			filename:    tdGuest + "/netlink_route_getroute_table_all.pcap",
			msgType:     uint16(unix.RTM_NEWROUTE),
			find:        topAttr(uint16(unix.RTA_CACHEINFO)),
			wantZeroed: []span{
				{rtaCacheinfoLastuseOffCst, rtaCacheinfoLastuseLenCst},
				{rtaCacheinfoTsOffCst, rtaCacheinfoTsLenCst},
			},
			wantPreserved:     []span{{0, 4}, {12, 12}},
			wantZeroedWasZero: true,
			wantFound:         9,
		},
		{
			// Measured: ffff0000 85060000 94a00000 e8030000 and two more
			// here, eight across both namespaces.
			// max_reasm_len is 0xffff and retrans_time 1000 on every link;
			// tstamp moves. reachable_time differs per link (41108, 35660,
			// 18022) and is LEFT COMPARED on purpose: the kernel recomputes it
			// every few minutes, so two adjacent captures can straddle a
			// recompute, and that is the case D_control exists to catch.
			// Letting it surface as a volatile-fallback entry with a reason
			// beats silently blanking it — the allowlist calls every such
			// entry a bug report against D_control, and that only works if the
			// bug reports get filed.
			description:   "positive: IFLA_INET6_CACHEINFO loses tstamp two nests deep and keeps reachable_time",
			filename:      tdGuest + "/netlink_route_getlink.pcap",
			msgType:       uint16(unix.RTM_NEWLINK),
			find:          inet6Attr(uint16(unix.IFLA_INET6_CACHEINFO)),
			wantZeroed:    []span{{inet6CacheinfoTstampOffCst, inet6CacheinfoTstampLenCst}},
			wantPreserved: []span{{0, 4}, {8, 8}},
			wantFound:     3,
		},
		{
			// The mesh topology's links carry the same nest, and there are
			// more of them — the row exists so the descent is exercised on a
			// body whose IFLA_AF_SPEC sits at a different offset behind
			// IFLA_MASTER and friends.
			description:   "positive: the descent works on the mesh corpus too, where AF_SPEC sits behind more attributes",
			filename:      tdGuest + "/mesh/netlink_route_getlink.pcap",
			msgType:       uint16(unix.RTM_NEWLINK),
			find:          inet6Attr(uint16(unix.IFLA_INET6_CACHEINFO)),
			wantZeroed:    []span{{inet6CacheinfoTstampOffCst, inet6CacheinfoTstampLenCst}},
			wantPreserved: []span{{0, 4}, {8, 8}},
			wantFound:     5,
		},
		{
			description: "positive: seq and pid are zeroed, and the source message is not modified",
			filename:    tdGuest + "/netlink_route_getaddr.pcap",
			msgType:     uint16(unix.RTM_NEWADDR),
			check: func(t *testing.T, before, after Msg) {
				if after.Hdr.Seq != 0 || after.Hdr.Pid != 0 {
					t.Errorf("normalized seq/pid = %d/%d, want 0/0", after.Hdr.Seq, after.Hdr.Pid)
				}
				if before.Hdr.Seq == 0 || before.Hdr.Pid == 0 {
					t.Fatalf("fixture message already had seq/pid %d/%d, so the row asserts nothing",
						before.Hdr.Seq, before.Hdr.Pid)
				}
			},
		},
		{
			// The deep copy is load-bearing: a differ normalizes both sides and
			// then still wants the originals to quote in a report. Aliasing
			// would make the report print the normalized bytes as if they were
			// what was captured.
			description: "positive: the body is deep-copied, so normalizing does not edit the capture buffer",
			filename:    tdGuest + "/netlink_route_getneigh.pcap",
			msgType:     uint16(unix.RTM_NEWNEIGH),
			check: func(t *testing.T, before, after Msg) {
				orig := topAttr(uint16(unix.NDA_CACHEINFO))(before)
				if len(orig) == 0 {
					return
				}
				if allZero(orig[0][:ndaCacheinfoAgeLenCst]) {
					t.Errorf("the ORIGINAL message's NDA_CACHEINFO age counters are zero after "+
						"normalizing a copy: %s — the body is aliased", hex.EncodeToString(orig[0]))
				}
				if len(before.Body) != 0 && len(after.Body) != 0 && &before.Body[0] == &after.Body[0] {
					t.Error("normalized body shares its backing array with the original")
				}
			},
		},
		{
			// A body too short for the field being zeroed is itself a
			// divergence the differ will report, so zeroRange leaves it alone
			// rather than panicking: replacing a finding with a crash would
			// lose the finding.
			description: "boundary: an IFA_CACHEINFO of 12 bytes cannot hold the stamps and is left untouched",
			msg:         ptr(attrMsg(uint16(unix.RTM_NEWADDR), xtcpnl.IfAddrmsgSizeCst, uint16(unix.IFA_CACHEINFO), fill(12, 0xAB))),
			find:        topAttr(uint16(unix.IFA_CACHEINFO)),
			// The whole 12 bytes survive: the range 8:16 does not fit, so
			// nothing is written, and the truncation reaches the differ.
			wantPreserved: []span{{0, 12}},
			wantFound:     1,
		},
		{
			description:   "boundary: an IFA_CACHEINFO of exactly 16 bytes is the smallest payload the stamps fit in",
			msg:           ptr(attrMsg(uint16(unix.RTM_NEWADDR), xtcpnl.IfAddrmsgSizeCst, uint16(unix.IFA_CACHEINFO), fill(16, 0xAB))),
			find:          topAttr(uint16(unix.IFA_CACHEINFO)),
			wantZeroed:    []span{{ifaCacheinfoStampsOffCst, ifaCacheinfoStampsLenCst}},
			wantPreserved: []span{{0, 8}},
			wantFound:     1,
		},
		{
			// An oversized payload is normalized at the documented offsets and
			// the excess is left alone, because the offsets are struct
			// positions rather than "the last two fields".
			description:   "boundary: an oversized 24-byte NDA_CACHEINFO zeroes the first 12 and keeps the rest",
			msg:           ptr(attrMsg(uint16(unix.RTM_NEWNEIGH), xtcpnl.NdMsgSizeCst, uint16(unix.NDA_CACHEINFO), fill(24, 0xAB))),
			find:          topAttr(uint16(unix.NDA_CACHEINFO)),
			wantZeroed:    []span{{ndaCacheinfoAgeOffCst, ndaCacheinfoAgeLenCst}},
			wantPreserved: []span{{12, 12}},
			wantFound:     1,
		},
		{
			description: "boundary: an empty body normalizes to a nil body rather than an empty non-nil slice",
			msg: ptr(Msg{Hdr: xtcpnl.NlMsgHdr{
				Len: uint32(xtcpnl.NlMsgHdrSizeCst), Type: uint16(unix.NLMSG_DONE), Seq: 7, Pid: 8,
			}}),
			check: func(t *testing.T, _, after Msg) {
				if after.Body != nil {
					t.Errorf("Body = %v, want nil", after.Body)
				}
				if after.Hdr.Seq != 0 || after.Hdr.Pid != 0 {
					t.Errorf("seq/pid = %d/%d, want 0/0", after.Hdr.Seq, after.Hdr.Pid)
				}
			},
		},
		{
			// NLMSG_DONE's four bytes are an int, not an attribute stream.
			// Normalizing them would corrupt a payload the differ compares
			// whole.
			description: "negative: a message with no attribute namespace has its body copied verbatim",
			msg: ptr(Msg{
				Hdr:  xtcpnl.NlMsgHdr{Len: 20, Type: uint16(unix.NLMSG_DONE), Seq: 1, Pid: 2},
				Body: []byte{0xDE, 0xAD, 0xBE, 0xEF},
			}),
			check: func(t *testing.T, before, after Msg) {
				if !bytes.Equal(before.Body, after.Body) {
					t.Errorf("body = %x, want %x unchanged", after.Body, before.Body)
				}
			},
		},
		{
			// The cacheinfo offsets are per-family. An IFA_CACHEINFO-shaped
			// payload sitting at NDA_CACHEINFO's type number inside an
			// RTM_NEWADDR must not be touched, or the dispatch is reading the
			// attribute number without the namespace.
			description: "negative: attribute 3 in the IFA namespace is IFA_LABEL, not NDA_CACHEINFO, and is left alone",
			msg:         ptr(attrMsg(uint16(unix.RTM_NEWADDR), xtcpnl.IfAddrmsgSizeCst, uint16(unix.NDA_CACHEINFO), fill(16, 0xAB))),
			find:        topAttr(uint16(unix.NDA_CACHEINFO)),
			// unix.NDA_CACHEINFO == 3 == unix.IFA_LABEL, and IFA_LABEL is not
			// volatile, so all 16 bytes survive.
			wantPreserved: []span{{0, 16}},
			wantFound:     1,
		},
		{
			// Normalizing twice must be identical to normalizing once, or a
			// comparator that normalizes defensively at two layers would
			// produce different bytes from one that does it at one.
			description: "corner: normalization is idempotent",
			filename:    tdGuest + "/netlink_route_getaddr.pcap",
			msgType:     uint16(unix.RTM_NEWADDR),
			check: func(t *testing.T, _, after Msg) {
				again := NormalizeMsg(after)
				if !bytes.Equal(again.Body, after.Body) {
					t.Errorf("second normalization changed the body:\n once: %x\ntwice: %x",
						after.Body, again.Body)
				}
			},
		},
		{
			// NLA_F_NESTED is set on 0x803e and friends in the very same
			// replies that carry a FLAGLESS IFLA_AF_SPEC, so a normalizer that
			// keyed on the flag would descend into the wrong attributes and
			// skip the right one. BareType is what must drive it.
			description: "corner: IFLA_AF_SPEC carries no NLA_F_NESTED, so the descent is driven by type",
			filename:    tdGuest + "/netlink_route_getlink.pcap",
			msgType:     uint16(unix.RTM_NEWLINK),
			check: func(t *testing.T, before, _ Msg) {
				_, attrs, _ := DecodeAttrs(before.Hdr.Type, before.Body)
				var sawAFSpec, sawFlagged bool
				for _, a := range attrs {
					if a.BareType() == uint16(unix.IFLA_AF_SPEC) {
						sawAFSpec = true
						if a.Nested() {
							t.Errorf("IFLA_AF_SPEC type = 0x%04x, which has NLA_F_NESTED set; "+
								"the measured corpus has it clear and normalizeAFSpec relies on that",
								a.Type)
						}
					}
					if a.Nested() {
						sawFlagged = true
					}
				}
				if !sawAFSpec {
					t.Error("no IFLA_AF_SPEC in this reply, so the row asserts nothing")
				}
				if !sawFlagged {
					t.Error("no attribute in this reply sets NLA_F_NESTED, so the row does not " +
						"establish that the flag is used elsewhere in the same message")
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			checkNormProvenance(t, tc.description, tc.filename, tc.msg)

			var msgs []Msg
			if tc.msg != nil {
				msgs = []Msg{*tc.msg}
			} else {
				c, err := ParseRouteCaptureFile(tc.filename)
				if err != nil {
					t.Fatalf("%s: %v", tc.filename, err)
				}
				for _, m := range c.Msgs() {
					if m.Hdr.Type == tc.msgType {
						msgs = append(msgs, m)
					}
				}
				if len(msgs) == 0 {
					t.Fatalf("%s holds no %s messages", tc.filename, MsgTypeName(tc.msgType))
				}
			}

			found := 0
			sawNonZero := make([]bool, len(tc.wantZeroed))
			for _, before := range msgs {
				after := NormalizeMsg(before)

				if tc.check != nil {
					tc.check(t, before, after)
				}
				if tc.find == nil {
					continue
				}

				origVals := tc.find(before)
				// find is applied to `before` AFTER normalizing a copy, so a
				// non-nil result here is also the aliasing check: if
				// NormalizeMsg had edited in place, origVals would already be
				// zeroed and wantPreserved would fail.
				normVals := tc.find(after)
				if len(origVals) != len(normVals) {
					t.Fatalf("normalization changed the attribute count: %d -> %d",
						len(origVals), len(normVals))
				}

				for i := range normVals {
					checkSpans(t, found, origVals[i], normVals[i], tc.wantZeroed, tc.wantPreserved)
					for si, s := range tc.wantZeroed {
						if s.off+s.n <= len(origVals[i]) && !allZero(origVals[i][s.off:s.off+s.n]) {
							sawNonZero[si] = true
						}
					}
					found++
				}
			}

			if tc.find == nil {
				return
			}
			if found != tc.wantFound {
				t.Errorf("found %d payloads, want %d — a row that finds none asserts nothing",
					found, tc.wantFound)
			}
			for si, s := range tc.wantZeroed {
				switch {
				case sawNonZero[si] && tc.wantZeroedWasZero:
					t.Errorf("span [%d:%d] was non-zero in the capture, but the row claims "+
						"wantZeroedWasZero; drop the field, the assertion is real",
						s.off, s.off+s.n)
				case !sawNonZero[si] && !tc.wantZeroedWasZero:
					t.Errorf("span [%d:%d] was already zero in all %d captured payloads, so "+
						"asserting it is zero after normalization proves nothing; set "+
						"wantZeroedWasZero if that is the measured state",
						s.off, s.off+s.n, found)
				}
			}
		})
	}
}

// checkSpans asserts both halves of a row's claim against one payload.
func checkSpans(t *testing.T, i int, orig, norm []byte, zeroed, preserved []span) {
	t.Helper()

	for _, s := range zeroed {
		if s.off+s.n > len(norm) {
			t.Errorf("payload %d is %d bytes, too short for the zeroed span [%d:%d]",
				i, len(norm), s.off, s.off+s.n)
			continue
		}
		if !allZero(norm[s.off : s.off+s.n]) {
			t.Errorf("payload %d bytes [%d:%d] = %s, want zero (whole payload %s)",
				i, s.off, s.off+s.n, hex.EncodeToString(norm[s.off:s.off+s.n]),
				hex.EncodeToString(norm))
		}
	}

	for _, s := range preserved {
		if s.off+s.n > len(orig) || s.off+s.n > len(norm) {
			t.Errorf("payload %d is %d bytes, too short for the preserved span [%d:%d]",
				i, len(norm), s.off, s.off+s.n)
			continue
		}
		if !bytes.Equal(orig[s.off:s.off+s.n], norm[s.off:s.off+s.n]) {
			t.Errorf("payload %d bytes [%d:%d] = %s, want %s unchanged — "+
				"a blanked stable field is a field a goip bug can hide behind",
				i, s.off, s.off+s.n,
				hex.EncodeToString(norm[s.off:s.off+s.n]),
				hex.EncodeToString(orig[s.off:s.off+s.n]))
		}
	}
}

// TestNormalizeMsgs covers the slice wrapper, whose only real decision is what
// it does with nil.
//
// go test ./pkg/nlparity/ -run TestNormalizeMsgs
func TestNormalizeMsgs(t *testing.T) {
	tests := []struct {
		description string
		in          []Msg
		wantNil     bool
		wantLen     int
	}{
		{
			description: "positive: three messages normalize to three",
			in: []Msg{
				attrMsg(uint16(unix.RTM_NEWADDR), xtcpnl.IfAddrmsgSizeCst, uint16(unix.IFA_CACHEINFO), fill(16, 1)),
				attrMsg(uint16(unix.RTM_NEWADDR), xtcpnl.IfAddrmsgSizeCst, uint16(unix.IFA_CACHEINFO), fill(16, 2)),
				attrMsg(uint16(unix.RTM_NEWADDR), xtcpnl.IfAddrmsgSizeCst, uint16(unix.IFA_CACHEINFO), fill(16, 3)),
			},
			wantLen: 3,
		},
		{
			// nil in, nil out: a transaction with no replies must not acquire
			// an empty non-nil slice, because reflect.DeepEqual in a caller's
			// test would then distinguish two segmentations that are the same.
			description: "boundary: nil stays nil rather than becoming an empty slice",
			in:          nil,
			wantNil:     true,
		},
		{
			description: "boundary: an empty non-nil slice stays empty and non-nil",
			in:          []Msg{},
			wantLen:     0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := NormalizeMsgs(tc.in)
			if tc.wantNil {
				if got != nil {
					t.Errorf("got %v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("got nil, want a non-nil slice")
			}
			if len(got) != tc.wantLen {
				t.Errorf("len = %d, want %d", len(got), tc.wantLen)
			}
			for i := range got {
				if got[i].Hdr.Seq != 0 || got[i].Hdr.Pid != 0 {
					t.Errorf("msg %d seq/pid = %d/%d, want 0/0", i, got[i].Hdr.Seq, got[i].Hdr.Pid)
				}
			}
		})
	}
}

// TestZeroRange covers the bounds tolerance directly, because the rows above
// reach it only through a truncated attribute and the off-by-one cases are
// worth stating.
//
// go test ./pkg/nlparity/ -run TestZeroRange
func TestZeroRange(t *testing.T) {
	tests := []struct {
		description string
		len         int
		off, n      int
		want        string // hex of the result
	}{
		{"positive: a range inside the slice is zeroed", 8, 2, 4, "ffff00000000ffff"},
		{"boundary: a range ending exactly at the end is zeroed", 8, 4, 4, "ffffffff00000000"},
		{"boundary: a range covering the whole slice is zeroed", 4, 0, 4, "00000000"},
		{"boundary: one byte past the end zeroes nothing", 8, 4, 5, "ffffffffffffffff"},
		{"boundary: a zero-length range is a no-op", 4, 2, 0, "ffffffff"},
		{"negative: a negative offset zeroes nothing", 4, -1, 2, "ffffffff"},
		{"negative: a negative length zeroes nothing", 4, 0, -1, "ffffffff"},
		{"corner: an empty slice with a non-empty range does not panic", 0, 0, 4, ""},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			b := fill(tc.len, 0xFF)
			zeroRange(b, tc.off, tc.n)
			if got := hex.EncodeToString(b); got != tc.want {
				t.Errorf("zeroRange(%d bytes, %d, %d) = %s, want %s", tc.len, tc.off, tc.n, got, tc.want)
			}
		})
	}
}

// fill returns n bytes of v.
func fill(n int, v byte) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = v
	}
	return b
}

// allZero reports whether every byte is zero.
func allZero(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}

// ptr takes the address of a value, so a row can write a composite literal
// inline in a *Msg field.
func ptr[T any](v T) *T { return &v }
