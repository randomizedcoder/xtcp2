package nsdiscover

import (
	"testing"

	"golang.org/x/sys/unix"
)

// buildNsidReply lays out a synthetic RTM_NEWNSID reply carrying a single
// NETNSA_NSID attribute with the given id, mirroring what the kernel returns.
func buildNsidReply(id int32) []byte {
	const total = nlmsgHdrLen + rtgenLen + 8 // + one 8-byte NETNSA_NSID attr
	b := make([]byte, total)
	nativeEndian.PutUint32(b[0:4], uint32(total))
	nativeEndian.PutUint16(b[4:6], rtmNewNsid)
	// flags/seq/pid left zero
	// rtgenmsg at 16..20 zero
	nativeEndian.PutUint16(b[20:22], 8) // nla_len
	nativeEndian.PutUint16(b[22:24], netnsaNsid)
	nativeEndian.PutUint32(b[24:28], uint32(id))
	return b
}

func TestBuildGetNsidRequest(t *testing.T) {
	req := buildGetNsidRequest(7)
	if len(req) != nlmsgHdrLen+rtgenLen+fdAttrLen {
		t.Fatalf("request len = %d, want %d", len(req), nlmsgHdrLen+rtgenLen+fdAttrLen)
	}
	if got := nativeEndian.Uint32(req[0:4]); got != uint32(len(req)) {
		t.Fatalf("nlmsg_len = %d, want %d", got, len(req))
	}
	if got := nativeEndian.Uint16(req[4:6]); got != rtmGetNsid {
		t.Fatalf("nlmsg_type = %d, want RTM_GETNSID %d", got, rtmGetNsid)
	}
	if got := nativeEndian.Uint16(req[6:8]); got != uint16(unix.NLM_F_REQUEST) {
		t.Fatalf("nlmsg_flags = %d, want NLM_F_REQUEST", got)
	}
	if got := nativeEndian.Uint16(req[20:22]); got != fdAttrLen {
		t.Fatalf("nla_len = %d, want %d", got, fdAttrLen)
	}
	if got := nativeEndian.Uint16(req[22:24]); got != netnsaFd {
		t.Fatalf("nla_type = %d, want NETNSA_FD %d", got, netnsaFd)
	}
	if got := int32(nativeEndian.Uint32(req[24:28])); got != 7 {
		t.Fatalf("NETNSA_FD payload = %d, want 7", got)
	}
}

func TestParseNsidResponse(t *testing.T) {
	// Build the error/done single-header messages for the negative cases.
	errMsg := make([]byte, nlmsgHdrLen)
	nativeEndian.PutUint32(errMsg[0:4], nlmsgHdrLen)
	nativeEndian.PutUint16(errMsg[4:6], uint16(unix.NLMSG_ERROR))

	doneMsg := make([]byte, nlmsgHdrLen)
	nativeEndian.PutUint32(doneMsg[0:4], nlmsgHdrLen)
	nativeEndian.PutUint16(doneMsg[4:6], uint16(unix.NLMSG_DONE))

	// A NEWNSID with no attributes (payload = rtgenmsg only).
	noAttr := make([]byte, nlmsgHdrLen+rtgenLen)
	nativeEndian.PutUint32(noAttr[0:4], uint32(len(noAttr)))
	nativeEndian.PutUint16(noAttr[4:6], rtmNewNsid)

	// A NEWNSID whose declared nlmsg_len overruns the buffer (truncated).
	truncated := buildNsidReply(5)
	nativeEndian.PutUint32(truncated[0:4], uint32(len(truncated)+16))

	tests := []struct {
		name   string
		buf    []byte
		wantID int32
		wantOK bool
	}{
		{"positive id 0", buildNsidReply(0), 0, true},
		{"positive id 42", buildNsidReply(42), 42, true},
		{"not assigned (-1)", buildNsidReply(-1), 0, false},
		{"error message", errMsg, 0, false},
		{"done message", doneMsg, 0, false},
		{"newnsid no attrs", noAttr, 0, false},
		{"empty buffer", nil, 0, false},
		{"short buffer", make([]byte, 8), 0, false},
		{"truncated declared len", truncated, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, ok := parseNsidResponse(tt.buf)
			if ok != tt.wantOK || id != tt.wantID {
				t.Fatalf("parseNsidResponse = (%d,%t), want (%d,%t)", id, ok, tt.wantID, tt.wantOK)
			}
		})
	}
}

func TestNsid_invalidFD(t *testing.T) {
	// Negative fd must short-circuit to (0,false) without opening a socket.
	if id, ok := Nsid(-1); ok || id != 0 {
		t.Fatalf("Nsid(-1) = (%d,%t), want (0,false)", id, ok)
	}
}
