package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"os"
	"slices"
	"strconv"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

// capturedNlMsg is one netlink message out of a pcap, header and body, with
// the netlink protocol its record's cooked header names.
type capturedNlMsg struct {
	proto uint16
	h     NlMsgHdr
	body  []byte
}

// pcapNlMsgs returns every netlink message in a DLT 253 pcap, in capture
// order, across every datagram.
func pcapNlMsgs(t testing.TB, path string) []capturedNlMsg {
	t.Helper()
	var out []capturedNlMsg
	for _, d := range pcapNlDatagrams(t, path) {
		out = append(out, d...)
	}
	return out
}

// pcapNlDatagrams returns the netlink messages of a DLT 253 pcap one
// datagram at a time, in capture order.
func pcapNlDatagrams(t testing.TB, path string) [][]capturedNlMsg {
	t.Helper()
	bs, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_, recs, err := ParseNetlinkPcap(bs)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	out := make([][]capturedNlMsg, 0, len(recs))
	for _, r := range recs {
		proto, p, perr := r.NetlinkPayload()
		if perr != nil {
			t.Fatalf("%s: %v", path, perr)
		}
		var d []capturedNlMsg
		for len(p) >= NlMsgHdrSizeCst {
			// ip's dump requests are sent as their whole request struct,
			// so a message can be followed by zeros that are no message.
			if !slices.ContainsFunc(p, func(b byte) bool { return b != 0 }) {
				break
			}
			var h NlMsgHdr
			if _, err := DeserializeNlMsgHdr(p, &h); err != nil {
				t.Fatalf("%s: %v", path, err)
			}
			mlen := int(h.Len)
			if mlen < NlMsgHdrSizeCst || mlen > len(p) {
				t.Fatalf("%s: nlmsg_len %d in a %d-byte datagram", path, mlen, len(p))
			}
			d = append(d, capturedNlMsg{proto: proto, h: h, body: p[NlMsgHdrSizeCst:mlen]})
			if adv := nlmsgAlign(mlen); adv < len(p) {
				p = p[adv:]
			} else {
				p = nil
			}
		}
		out = append(out, d)
	}
	return out
}

// nsidOf decodes NETNSA_NSID from an RTM_NEWNSID or RTM_DELNSID body with
// ParseNsid.
func nsidOf(t *testing.T, body []byte) uint32 {
	t.Helper()
	ni, err := ParseNsid(body)
	if err != nil {
		t.Fatal(err)
	}
	if !ni.HasNSID {
		t.Fatal("no NETNSA_NSID")
	}
	return uint32(ni.NSID)
}

// answerTo finds the NLMSG_ERROR that answers req (same seq, echoing its
// type) and returns its errno, 0 for an ACK.
func answerTo(t *testing.T, msgs []capturedNlMsg, req NlMsgHdr) syscall.Errno {
	t.Helper()
	for _, m := range msgs {
		if m.h.Type != unix.NLMSG_ERROR || m.h.Seq != req.Seq {
			continue
		}
		e, err := ParseNlMsgErr(m.h.Flags, m.body)
		if err != nil {
			t.Fatal(err)
		}
		if e.RequestType != req.Type {
			continue
		}
		if e.Errno == 0 {
			return 0
		}
		return syscall.Errno(-e.Errno)
	}
	t.Fatalf("no NLMSG_ERROR answers seq %d type %d", req.Seq, req.Type)
	return 0
}

func u32s(v uint32) string { return strconv.FormatUint(uint64(v), 10) }

// rowRequestTypes returns the distinct types `ip` sent as requests, in the
// order each first appears, and the first error the kernel answered with:
// an NLMSG_ERROR that is not an ACK, or a dump's NLMSG_DONE whose int is
// negative. 0 when there was none.
func rowRequestTypes(t *testing.T, msgs []capturedNlMsg) ([]uint16, syscall.Errno) {
	t.Helper()
	var types []uint16
	var errno syscall.Errno
	for _, m := range msgs {
		if m.h.Flags&unix.NLM_F_REQUEST != 0 && !slices.Contains(types, m.h.Type) {
			types = append(types, m.h.Type)
		}
		if m.h.Type == unix.NLMSG_ERROR && errno == 0 {
			e, err := ParseNlMsgErr(m.h.Flags, m.body)
			if err != nil {
				t.Fatal(err)
			}
			if e.Errno != 0 {
				errno = syscall.Errno(-e.Errno)
			}
		}
		if m.h.Type == unix.NLMSG_DONE && errno == 0 && len(m.body) >= 4 {
			var v int32
			if err := binary.Read(bytes.NewReader(m.body[:4]), binary.LittleEndian, &v); err != nil {
				t.Fatal(err)
			}
			if v < 0 {
				errno = syscall.Errno(-v)
			}
		}
	}
	return types, errno
}
