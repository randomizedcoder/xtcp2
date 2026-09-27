package nlparity

import (
	"fmt"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// Msg is one netlink message located inside a captured datagram.
//
// Body aliases the datagram, which in turn aliases the pcap buffer — copy
// anything retained past the caller's use of the capture.
type Msg struct {
	Hdr    xtcpnl.NlMsgHdr
	Body   []byte // nlmsg_len - 16 bytes; the family header plus attributes
	Offset int    // byte offset of the nlmsghdr within the datagram
}

// IsRequest reports whether NLM_F_REQUEST is set.
//
// This, and not the capture direction, is how a request is identified. nlmon
// records every netlink datagram on the host with sll_pkttype PACKET_OUTGOING
// in both directions, because AF_PACKET's dev_queue_xmit_nit overwrites what
// __netlink_deliver_tap_skb set — so the link layer says nothing useful.
// Requests also carry nlmsg_pid 0 (the kernel fills the peer port id only on
// the way back), which is the second half of the direction test.
func (m Msg) IsRequest() bool { return m.Hdr.Flags&uint16(unix.NLM_F_REQUEST) != 0 }

// IsMulti reports whether NLM_F_MULTI is set, i.e. the message is one part of a
// multipart reply that ends in NLMSG_DONE.
func (m Msg) IsMulti() bool { return m.Hdr.Flags&uint16(unix.NLM_F_MULTI) != 0 }

// Canonical returns a copy with the two fields that legitimately differ between
// two runs of the same command zeroed: nlmsg_seq (iproute2 seeds it from
// time(NULL)) and nlmsg_pid (the socket's port id). Everything else in the
// header is tool-controlled and must match.
func (m Msg) Canonical() Msg {
	m.Hdr.Seq = 0
	m.Hdr.Pid = 0
	return m
}

// Datagram is one captured netlink datagram after a tolerant walk.
type Datagram struct {
	// Family is the NETLINK_* protocol from the SLL cooked header.
	Family uint16
	// Len is the size of the datagram as captured, i.e. what sendto() passed.
	// It is compared with the sum of the messages to detect an oversend.
	Len int
	// Msgs are the complete netlink messages, in capture order.
	Msgs []Msg
	// TailBytes counts trailing bytes that are not a complete message.
	//
	// Informational, never gated. iproute2's oversend is per-command: `ip link
	// show` sends exactly nlmsg_len (rtnl_linkdump_req_filter_fn,
	// lib/libnetlink.c:618) so this is 0, whereas addr and route send a whole
	// `char buf[128]` and neigh a `char buf[256]`, so this is the zeroed
	// remainder of that buffer. Gating on it would gate on a struct size in
	// someone else's source file.
	TailBytes int
	// TailAllZero reports whether the tail is entirely zero bytes, which is the
	// signature of an oversent stack buffer.
	TailAllZero bool
	// TailFlagged reports a tail that is at least a full header long and is NOT
	// all zero — so a header was present and unusable. That is not an oversend;
	// it is a truncated or corrupt datagram, and it belongs in the report.
	TailFlagged bool
}

// isAllZero reports whether every byte of b is zero. An empty slice is all
// zero, which is what makes a tail of length 0 consistent with TailAllZero.
func isAllZero(b []byte) bool {
	for _, c := range b {
		if c != 0 {
			return false
		}
	}
	return true
}

// WalkDatagram splits one netlink datagram into its messages, tolerating a
// trailing remainder instead of rejecting it.
//
// Rules, in order:
//
//   - fewer than 16 bytes in the datagram at all → ErrShortDatagram;
//   - the first message having nlmsg_len < 16, or nlmsg_len overrunning the
//     datagram → ErrBadHead (see the comment on that error);
//   - any later position that cannot yield a complete message ends the walk:
//     everything from there is the tail, classified by TailAllZero /
//     TailFlagged;
//   - NLMSG_DONE, NLMSG_ERROR and NLMSG_NOOP are returned like any other
//     message. This walker classifies nothing and terminates on nothing; the
//     segmenter decides what closes a transaction.
//
// The final message's 4-byte alignment padding may legitimately be absent, so a
// remainder shorter than the padding is consumed rather than counted as tail —
// mirroring the kernel's own NLMSG_OK walk.
//
// nlmsg_len == 0 cannot loop: zero is below the header size, so it either
// raises ErrBadHead (first message) or ends the walk (any later one). Every
// path through the loop advances by at least NlMsgHdrSizeCst or returns.
func WalkDatagram(family uint16, data []byte) (Datagram, error) {
	d := Datagram{Family: family, Len: len(data)}

	if len(data) < xtcpnl.NlMsgHdrSizeCst {
		return d, fmt.Errorf("%w: %d bytes", ErrShortDatagram, len(data))
	}

	off := 0
	for off+xtcpnl.NlMsgHdrSizeCst <= len(data) {
		var h xtcpnl.NlMsgHdr
		if _, err := xtcpnl.DeserializeNlMsgHdr(data[off:], &h); err != nil {
			// Unreachable given the loop condition, but a length check that
			// exists must be honored rather than discarded.
			break
		}

		msgLen := int(h.Len)
		if msgLen < xtcpnl.NlMsgHdrSizeCst || off+msgLen > len(data) {
			if off == 0 {
				return d, fmt.Errorf("%w: nlmsg_len %d in a %d-byte datagram",
					ErrBadHead, msgLen, len(data))
			}
			break
		}

		d.Msgs = append(d.Msgs, Msg{
			Hdr:    h,
			Body:   data[off+xtcpnl.NlMsgHdrSizeCst : off+msgLen],
			Offset: off,
		})

		adv := msgLen + xtcpnl.FourByteAlignPadding(msgLen)
		if off+adv > len(data) {
			// Last message, padding absent. Consume to the end so an unpadded
			// tail is not miscounted as an oversend.
			off = len(data)
			break
		}
		off += adv
	}

	tail := data[off:]
	d.TailBytes = len(tail)
	d.TailAllZero = isAllZero(tail)
	d.TailFlagged = d.TailBytes >= xtcpnl.NlMsgHdrSizeCst && !d.TailAllZero

	return d, nil
}
