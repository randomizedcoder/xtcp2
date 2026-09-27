package nlparity

import (
	"encoding/binary"
	"os"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// Byte builders for the constructed rows.
//
// Constructed bytes are used ONLY for the negative, boundary and corner rows —
// truncation, malformed lengths and oversend tails. Every positive row reads a
// real nlmon capture, because a hand-built "positive" only ever asserts that
// the test author and the decoder share an assumption. That split is enforced
// by the tables themselves: see checkRowProvenance.

// nlmsg builds one netlink message. len is passed explicitly rather than
// derived, because half the point of these tables is a nlmsg_len that disagrees
// with the bytes present.
func nlmsg(msgLen uint32, msgType, flags uint16, seq, pid uint32, body []byte) []byte {
	b := make([]byte, xtcpnl.NlMsgHdrSizeCst+len(body))
	binary.LittleEndian.PutUint32(b[0:4], msgLen)
	binary.LittleEndian.PutUint16(b[4:6], msgType)
	binary.LittleEndian.PutUint16(b[6:8], flags)
	binary.LittleEndian.PutUint32(b[8:12], seq)
	binary.LittleEndian.PutUint32(b[12:16], pid)
	copy(b[xtcpnl.NlMsgHdrSizeCst:], body)
	return b
}

// wellFormed builds a message whose nlmsg_len matches its own size.
func wellFormed(msgType, flags uint16, body []byte) []byte {
	return nlmsg(uint32(xtcpnl.NlMsgHdrSizeCst+len(body)), msgType, flags, 1, 0, body)
}

// getaddrRequest builds the 24-byte RTM_GETADDR dump request iproute2 sends:
// an nlmsghdr plus an 8-byte ifaddrmsg with only ifa_family set.
func getaddrRequest(family uint8) []byte {
	hdr := make([]byte, xtcpnl.IfAddrmsgSizeCst)
	hdr[0] = family
	return nlmsg(uint32(xtcpnl.NlMsgHdrSizeCst+xtcpnl.IfAddrmsgSizeCst),
		uint16(unix.RTM_GETADDR),
		uint16(unix.NLM_F_REQUEST|unix.NLM_F_ROOT|unix.NLM_F_MATCH),
		1, 0, hdr)
}

// zeros is the oversent stack buffer: iproute2's rtnl_addrdump_req and
// rtnl_routedump_req send sizeof(req) over a `char buf[128]`, and
// rtnl_neighdump_req over a `char buf[256]`, so the bytes past nlmsg_len are
// whatever the zero-initialized struct held.
func zeros(n int) []byte { return make([]byte, n) }

// sllRecord wraps a netlink datagram in the 16-byte Linux SLL cooked header
// every DLT_NETLINK record carries. Only the protocol field at offset 14 has to
// be right, and it is big-endian — a link-layer field, unlike everything else
// this package reads.
func sllRecord(family uint16, datagram []byte) []byte {
	rec := make([]byte, xtcpnl.NetlinkCookedHeaderSizeCst+len(datagram))
	// rec[2:4] would be ARPHRD_NETLINK (824); nothing reads it, so it stays 0
	// to make clear the tests do not depend on it.
	binary.BigEndian.PutUint16(rec[xtcpnl.SllProtocolOffsetCst:xtcpnl.SllProtocolOffsetCst+2], family)
	copy(rec[xtcpnl.NetlinkCookedHeaderSizeCst:], datagram)
	return rec
}

// pcapFile assembles a little-endian, microsecond-resolution pcap with the
// given link type and records. Constructed rather than committed, because the
// rows using it are about the *container* being wrong — a non-netlink link
// type, a zero-record file, a truncated record — and there is no honest way to
// capture those.
func pcapFile(linkType uint32, records [][]byte) []byte {
	hdr := make([]byte, xtcpnl.PcapHeaderSizeCst)
	binary.LittleEndian.PutUint32(hdr[0:4], xtcpnl.PcapMagicMicrosCst)
	binary.LittleEndian.PutUint16(hdr[4:6], 2) // version_major
	binary.LittleEndian.PutUint16(hdr[6:8], 4) // version_minor
	binary.LittleEndian.PutUint32(hdr[16:20], 262144)
	binary.LittleEndian.PutUint32(hdr[20:24], linkType)

	out := hdr
	for _, rec := range records {
		rh := make([]byte, xtcpnl.PcapRecordHeaderSizeCst)
		binary.LittleEndian.PutUint32(rh[0:4], 1)                  // ts_sec
		binary.LittleEndian.PutUint32(rh[4:8], 0)                  // ts_usec
		binary.LittleEndian.PutUint32(rh[8:12], uint32(len(rec)))  // caplen
		binary.LittleEndian.PutUint32(rh[12:16], uint32(len(rec))) // len
		out = append(out, rh...)
		out = append(out, rec...)
	}
	return out
}

func concat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// routeDatagram returns the netlink datagram of one record of a DLT_NETLINK
// pcap, counting only NETLINK_ROUTE records.
//
// It deliberately uses xtcpnl's pcap reader rather than this package's
// ParseCapture, so the walker under test is never used to construct its own
// input.
//
// The receiver is testing.TB rather than *testing.T so the fuzz seed corpus can
// call it: *testing.F has Helper and Fatalf but no Run.
func routeDatagram(t testing.TB, path string, idx int) []byte {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	_, records, err := xtcpnl.ParseNetlinkPcap(data)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	n := 0
	for _, rec := range records {
		fam, body, perr := rec.NetlinkPayload()
		if perr != nil || fam != uint16(unix.NETLINK_ROUTE) {
			continue
		}
		if n == idx {
			return body
		}
		n++
	}

	t.Fatalf("%s: no NETLINK_ROUTE record at index %d (found %d)", path, idx, n)
	return nil
}
