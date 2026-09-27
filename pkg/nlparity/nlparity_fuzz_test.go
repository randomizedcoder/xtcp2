package nlparity

import (
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// FuzzWalkDatagram fuzzes the tolerant walker over arbitrary bytes.
//
// This is the highest-value fuzz target in the package because it is the only
// code here that parses genuinely untrusted input: a pcap file, which in the
// in-guest parity runner is produced by tcpdump and read back without any
// validation of its own.
//
// The invariants asserted are the ones a differ depends on, not just
// "no panic":
//
//  1. every byte is accounted for — the messages plus the tail must sum to the
//     datagram length, or a divergence could hide in the gap;
//  2. no message may overlap its neighbor or run past the end;
//  3. TailFlagged and TailAllZero must stay consistent, because the whole
//     oversend-versus-truncation distinction rests on them.
//
// go test ./pkg/nlparity/ -run FuzzWalkDatagram -fuzz FuzzWalkDatagram
func FuzzWalkDatagram(f *testing.F) {
	ifinfo := make([]byte, xtcpnl.IfInfomsgSizeCst)
	ifinfo[0] = unix.AF_PACKET

	f.Add([]byte(nil))
	f.Add(zeros(16))
	f.Add(concat(getaddrRequest(unix.AF_INET), zeros(128)))
	f.Add(wellFormed(uint16(unix.RTM_GETLINK), uint16(unix.NLM_F_REQUEST), ifinfo))
	f.Add(nlmsg(0, 0x03e7, 0, 0, 0, nil))
	f.Add(nlmsg(0xffffffff, uint16(unix.RTM_GETLINK), 0, 0, 0, ifinfo))

	// The real request datagrams, so the corpus starts from traffic a kernel
	// actually produced rather than only from hand-built edges.
	f.Add(routeDatagram(f, tdBulkGetLink, 0))
	f.Add(routeDatagram(f, tdBulkGetRoute, 0))

	f.Fuzz(func(t *testing.T, data []byte) {
		d, err := WalkDatagram(uint16(unix.NETLINK_ROUTE), data)
		if err != nil {
			if !IsBadCapture(err) {
				t.Fatalf("err = %v, want ErrBadHead or ErrShortDatagram", err)
			}
			return
		}

		if d.Len != len(data) {
			t.Fatalf("Len = %d, want %d", d.Len, len(data))
		}

		consumed := 0
		for i, m := range d.Msgs {
			if m.Offset < consumed {
				t.Fatalf("message[%d] at offset %d overlaps the previous message, "+
					"which ended at %d", i, m.Offset, consumed)
			}
			end := m.Offset + int(m.Hdr.Len)
			if end > len(data) {
				t.Fatalf("message[%d] ends at %d, past the %d-byte datagram", i, end, len(data))
			}
			if int(m.Hdr.Len) < xtcpnl.NlMsgHdrSizeCst {
				t.Fatalf("message[%d] nlmsg_len = %d, below a bare header", i, m.Hdr.Len)
			}
			if len(m.Body) != int(m.Hdr.Len)-xtcpnl.NlMsgHdrSizeCst {
				t.Fatalf("message[%d] body = %d bytes, want %d",
					i, len(m.Body), int(m.Hdr.Len)-xtcpnl.NlMsgHdrSizeCst)
			}
			consumed = end
		}

		if d.TailBytes < 0 || d.TailBytes > len(data) {
			t.Fatalf("TailBytes = %d, outside 0..%d", d.TailBytes, len(data))
		}
		// Everything not in a message, and not alignment padding between
		// messages, is the tail. So the tail can never start before the last
		// message ended.
		if len(data)-d.TailBytes < consumed {
			t.Fatalf("the tail starts at %d, before the last message ended at %d: "+
				"bytes went missing", len(data)-d.TailBytes, consumed)
		}

		if d.TailFlagged && d.TailAllZero {
			t.Fatalf("tail is both flagged and all-zero; an all-zero tail is an "+
				"oversend, which is exactly what must not be flagged (%d bytes)",
				d.TailBytes)
		}
		if d.TailFlagged && d.TailBytes < xtcpnl.NlMsgHdrSizeCst {
			t.Fatalf("tail of %d bytes is flagged, but a flag means a full header "+
				"was present and unusable", d.TailBytes)
		}
	})
}

// FuzzDecodeAttrs fuzzes the attribute splitter. Its inputs are message bodies,
// which in production come straight out of a datagram the walker already
// accepted, so the interesting failures are length arithmetic rather than
// framing.
//
// go test ./pkg/nlparity/ -run FuzzDecodeAttrs -fuzz FuzzDecodeAttrs
func FuzzDecodeAttrs(f *testing.F) {
	f.Add(uint16(unix.RTM_GETLINK), []byte(nil))
	f.Add(uint16(unix.RTM_GETLINK), zeros(xtcpnl.IfInfomsgSizeCst))
	f.Add(uint16(unix.RTM_GETADDR), zeros(xtcpnl.IfAddrmsgSizeCst))
	f.Add(uint16(0xffff), zeros(64))

	f.Fuzz(func(t *testing.T, msgType uint16, body []byte) {
		hdr, attrs, remainder := DecodeAttrs(msgType, body)

		if len(hdr) > len(body) {
			t.Fatalf("family header = %d bytes, longer than the %d-byte body",
				len(hdr), len(body))
		}
		if len(remainder) > len(body) {
			t.Fatalf("remainder = %d bytes, longer than the %d-byte body",
				len(remainder), len(body))
		}

		// An unknown message type must yield nothing decoded and the whole body
		// back, rather than guessing a family header size.
		if FamilyHdrLen(msgType) < 0 {
			if len(attrs) != 0 || len(remainder) != len(body) {
				t.Fatalf("unknown msgType %d decoded %d attrs and returned a %d-byte "+
					"remainder; want 0 attrs and the whole %d-byte body",
					msgType, len(attrs), len(remainder), len(body))
			}
			return
		}

		total := len(hdr)
		for i, a := range attrs {
			if len(a.Val) > len(body) {
				t.Fatalf("attr[%d] value = %d bytes, longer than the body", i, len(a.Val))
			}
			total += xtcpnl.RTAttrSizeCst + len(a.Val)
		}
		if total > len(body) {
			t.Fatalf("header plus attributes = %d bytes, more than the %d-byte body",
				total, len(body))
		}
	})
}
