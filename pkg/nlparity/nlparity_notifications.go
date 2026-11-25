package nlparity

import (
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// NotificationIndexes is what of a capture a subscriber was sent, by
// index: the messages to user space, less the answers to requests.
//
// nlmon records a multicast once per socket it is delivered to, and a
// unicast answer once, with nothing on either to say which it was. What is
// left out:
//
//   - a request: NLM_F_REQUEST with nlmsg_pid 0. nexthop_notify copies the
//     request's flags into its notification (net/ipv4/nexthop.c), so a
//     NEWNEXTHOP event has NLM_F_REQUEST too; it carries the requester's
//     port id, which a request does not;
//   - a dump's part, NLM_F_MULTI, its NLMSG_DONE among them;
//   - an NLMSG_ERROR, an ack or a refusal;
//   - the answer to a get: the first message after a request of an RTM
//     GET type (a type 2 past the NEW of its family of four) with the
//     request's sequence number and the NEW type. A refusal of the get, an
//     NLMSG_ERROR naming it, ends the wait as well.
//
// The replay (internal/goip) and `nlcapgen monfile` share this: the first
// plays a capture's notifications to the monitor, the second writes them as
// the .nl stream `ip monitor file` walks.
func NotificationIndexes(msgs []Msg) []int {
	type pending struct {
		seq uint32
		typ uint16
	}
	var gets []pending
	answered := func(seq uint32, typ uint16) bool {
		for i, g := range gets {
			if g.seq == seq && g.typ == typ {
				gets = append(gets[:i], gets[i+1:]...)
				return true
			}
		}
		return false
	}
	var out []int
	for i := range msgs {
		m := &msgs[i]
		h := m.Hdr
		switch {
		case h.Flags&unix.NLM_F_REQUEST != 0 && h.Pid == 0:
			if isRtmGet(h.Type) && h.Flags&unix.NLM_F_DUMP != unix.NLM_F_DUMP {
				gets = append(gets, pending{h.Seq, h.Type - 2})
			}
		case h.Flags&unix.NLM_F_MULTI != 0:
		case h.Type == unix.NLMSG_ERROR:
			if len(m.Body) >= 4+xtcpnl.NlMsgHdrSizeCst {
				var orig xtcpnl.NlMsgHdr
				if _, err := xtcpnl.DeserializeNlMsgHdr(m.Body[4:], &orig); err == nil && isRtmGet(orig.Type) {
					answered(orig.Seq, orig.Type-2)
				}
			}
		case answered(h.Seq, h.Type):
		default:
			out = append(out, i)
		}
	}
	return out
}

// isRtmGet is an RTM_GET* type: the third of its family's four, NEW, DEL,
// GET and SET, from RTM_BASE.
func isRtmGet(t uint16) bool {
	return t >= unix.RTM_BASE && (t-unix.RTM_BASE)%4 == 2
}
