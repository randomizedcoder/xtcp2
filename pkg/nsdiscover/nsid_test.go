package nsdiscover

import (
	"testing"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// buildNsidReply lays out a synthetic RTM_NEWNSID reply carrying a single
// NETNSA_NSID attribute with the given id, mirroring what the kernel returns.
//
// These are constructed bytes rather than a captured fixture because there is
// no nlmon capture of RTM_GETNSID in the corpus: the id is namespace-relative
// and almost always unassigned on a host, so a capture would record the
// uninteresting case. The layout is pinned against the kernel UAPI structs by
// TestBuildGetNsidRequest, which asserts the request this package sends.
func buildNsidReply(id int32) []byte {
	return buildNsidReplySeq(id, nsidSeqCst)
}

// buildNsidReplySeq is buildNsidReply with an explicit nlmsg_seq, so a row can
// exercise the sequence filter.
func buildNsidReplySeq(id int32, seq uint32) []byte {
	total := xtcpnl.NlMsgHdrSizeCst + rtgenLen + 8 // + one 8-byte NETNSA_NSID attr
	b := make([]byte, total)
	nativeEndian.PutUint32(b[0:4], uint32(total))
	nativeEndian.PutUint16(b[4:6], rtmNewNsid)
	// flags left zero
	nativeEndian.PutUint32(b[8:12], seq)
	// pid left zero; rtgenmsg at 16..20 zero
	nativeEndian.PutUint16(b[20:22], 8) // nla_len
	nativeEndian.PutUint16(b[22:24], netnsaNsid)
	nativeEndian.PutUint32(b[24:28], uint32(id))
	return b
}

// bareMsg lays out a single header-only message of the given type at
// nsidSeqCst, for the NLMSG_ERROR / NLMSG_DONE rows.
func bareMsg(msgType uint16) []byte {
	b := make([]byte, xtcpnl.NlMsgHdrSizeCst)
	nativeEndian.PutUint32(b[0:4], xtcpnl.NlMsgHdrSizeCst)
	nativeEndian.PutUint16(b[4:6], msgType)
	nativeEndian.PutUint32(b[8:12], nsidSeqCst)
	return b
}

// TestBuildGetNsidRequest pins the request bytes field by field. It is the
// counterpart to the reply table below: the request is the only half of this
// exchange this package fully controls.
//
// go test ./pkg/nsdiscover/ -run TestBuildGetNsidRequest
func TestBuildGetNsidRequest(t *testing.T) {
	req := buildGetNsidRequest(7)

	if len(req) != xtcpnl.NlMsgHdrSizeCst+rtgenLen+fdAttrLen {
		t.Fatalf("request len = %d, want %d", len(req),
			xtcpnl.NlMsgHdrSizeCst+rtgenLen+fdAttrLen)
	}

	tests := []struct {
		description string
		got         uint64
		want        uint64
	}{
		{"positive: nlmsg_len is the whole request", uint64(nativeEndian.Uint32(req[0:4])), uint64(len(req))},
		{"positive: nlmsg_type is RTM_GETNSID", uint64(nativeEndian.Uint16(req[4:6])), rtmGetNsid},
		{"positive: nlmsg_flags is NLM_F_REQUEST with no NLM_F_DUMP",
			uint64(nativeEndian.Uint16(req[6:8])), uint64(uint16(unix.NLM_F_REQUEST))},
		{"positive: nlmsg_seq is the value parseNsidResponse filters on",
			uint64(nativeEndian.Uint32(req[8:12])), nsidSeqCst},
		{"positive: nlmsg_pid is 0, the kernel fills the peer pid",
			uint64(nativeEndian.Uint32(req[12:16])), 0},
		{"positive: nla_len covers the header plus an int32",
			uint64(nativeEndian.Uint16(req[20:22])), fdAttrLen},
		{"positive: nla_type is NETNSA_FD", uint64(nativeEndian.Uint16(req[22:24])), netnsaFd},
		{"positive: the NETNSA_FD payload is the fd passed in",
			uint64(int32(nativeEndian.Uint32(req[24:28]))), 7},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("got %d (0x%x), want %d (0x%x)", tt.got, tt.got, tt.want, tt.want)
			}
		})
	}
}

// TestParseNsidResponse covers the reply walk, which is now xtcpnl's
// WalkNlMsgs plus WalkRTAttrs rather than a second hand-rolled copy of netlink
// framing. The rows are chosen so that every way the walk can end — a match, a
// deliberate non-answer, a control message, a malformed length — is pinned,
// because every one of them collapses to the same (0, false) at the call site
// and so is invisible to the caller.
//
// go test ./pkg/nsdiscover/ -run TestParseNsidResponse
func TestParseNsidResponse(t *testing.T) {
	// A NEWNSID with no attributes (payload = rtgenmsg only).
	noAttr := make([]byte, xtcpnl.NlMsgHdrSizeCst+rtgenLen)
	nativeEndian.PutUint32(noAttr[0:4], uint32(len(noAttr)))
	nativeEndian.PutUint16(noAttr[4:6], rtmNewNsid)
	nativeEndian.PutUint32(noAttr[8:12], nsidSeqCst)

	// A NEWNSID whose declared nlmsg_len overruns the buffer (truncated).
	truncated := buildNsidReply(5)
	nativeEndian.PutUint32(truncated[0:4], uint32(len(truncated)+16))

	// A NETNSA_NSID attribute declaring only a 2-byte payload: too short to
	// hold the int32 the kernel documents.
	shortAttr := buildNsidReply(42)
	nativeEndian.PutUint16(shortAttr[20:22], 6)

	// Two NEWNSIDs in one datagram, the first unassigned. First one wins.
	twoMsgs := append(buildNsidReply(-1), buildNsidReply(42)...)

	tests := []struct {
		description string
		buf         []byte
		wantID      int32
		wantOK      bool
	}{
		{"positive: an assigned id decodes", buildNsidReply(42), 42, true},

		{"boundary: id 0 is a real id, not a zero value", buildNsidReply(0), 0, true},
		{"boundary: NETNSA_NSID_NOT_ASSIGNED (-1) is not an id", buildNsidReply(-1), 0, false},
		{"boundary: a reply of exactly one bare header", bareMsg(rtmNewNsid), 0, false},
		{"boundary: a buffer one byte short of a header", make([]byte, xtcpnl.NlMsgHdrSizeCst-1), 0, false},

		{"negative: NLMSG_ERROR ends the walk", bareMsg(uint16(unix.NLMSG_ERROR)), 0, false},
		{"negative: NLMSG_DONE ends the walk with nothing found", bareMsg(uint16(unix.NLMSG_DONE)), 0, false},
		{"negative: a NEWNSID with no attributes", noAttr, 0, false},
		{"negative: an empty buffer", nil, 0, false},
		{"negative: nlmsg_len overruns the buffer", truncated, 0, false},
		{"negative: a reply carrying someone else's nlmsg_seq is skipped",
			buildNsidReplySeq(42, nsidSeqCst+1), 0, false},

		{"corner: NETNSA_NSID with a payload too short for an int32", shortAttr, 0, false},
		{"corner: two NEWNSIDs, unassigned then assigned — the first one wins", twoMsgs, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			id, ok := parseNsidResponse(tt.buf)
			if ok != tt.wantOK || id != tt.wantID {
				t.Fatalf("parseNsidResponse = (%d,%t), want (%d,%t)", id, ok, tt.wantID, tt.wantOK)
			}
		})
	}
}

// TestNsid_invalidFD pins the one path through Nsid that needs no socket.
//
// go test ./pkg/nsdiscover/ -run TestNsid_invalidFD
func TestNsid_invalidFD(t *testing.T) {
	// Negative fd must short-circuit to (0,false) without opening a socket.
	if id, ok := Nsid(-1); ok || id != 0 {
		t.Fatalf("Nsid(-1) = (%d,%t), want (0,false)", id, ok)
	}
}
