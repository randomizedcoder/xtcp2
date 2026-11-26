package nlmon

import (
	"bytes"
	"errors"
	"os"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// ErrPcap is a capture PcapMsgs cannot read.
var ErrPcap = errors.New("nlmon: pcap")

// PcapMsgs is every NETLINK_ROUTE message of the nlmon pcap at path, in
// capture order, each datagram walked as rtnl_listen walks it; a message
// that does not fit ends its datagram.
func PcapMsgs(path string) ([]Msg, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Join(ErrPcap, err)
	}
	_, recs, err := xtcpnl.ParseNetlinkPcap(data)
	if err != nil {
		return nil, errors.Join(ErrPcap, err)
	}
	var out []Msg
	for _, r := range recs {
		family, dg, perr := r.NetlinkPayload()
		if perr != nil {
			return nil, errors.Join(ErrPcap, perr)
		}
		if family != unix.NETLINK_ROUTE {
			continue
		}
		collect := func(m Msg) error {
			out = append(out, m)
			return nil
		}
		// A datagram cut short in the capture ends where it stops making
		// sense; what came before it stays.
		if derr := Datagram(dg, true, -1, collect); derr != nil && !errors.Is(derr, ErrTruncated) {
			return nil, errors.Join(ErrPcap, derr)
		}
	}
	return out, nil
}

// DumpEnd is the index past a recorded dump of typ's NLMSG_DONE: the first
// request of typ with NLM_F_DUMP and nlmsg_pid 0, then the first DONE after
// it with its sequence number; 0 when the capture has none.
func DumpEnd(msgs []Msg, typ uint16) int {
	req := -1
	for i := range msgs {
		h := &msgs[i].Hdr
		switch {
		case req < 0 && h.Type == typ && h.Flags&unix.NLM_F_DUMP == unix.NLM_F_DUMP && h.Pid == 0:
			req = i
		case req >= 0 && h.Type == unix.NLMSG_DONE && h.Seq == msgs[req].Hdr.Seq:
			return i + 1
		}
	}
	return 0
}

// Notifications is what of a capture a subscriber was sent, by index: the
// messages to user space, less the answers to requests.
//
// nlmon records a multicast once per socket it is delivered to, and a
// unicast answer once, with nothing on either to say which it was. What is
// left out:
//
//   - a request: NLM_F_REQUEST with nlmsg_pid 0. nexthop_notify, and tc's
//     filter and action notifications (tfilter_notify, tcf_add_notify),
//     copy the request's flags into the notification, so those carry
//     NLM_F_REQUEST too; they carry the requester's port id, which a
//     request does not;
//   - a dump's part, NLM_F_MULTI, its NLMSG_DONE among them;
//   - an NLMSG_ERROR, an ack or a refusal;
//   - the answer to a get: the first message after a request of an RTM
//     GET type (a type 2 past the NEW of its family of four) with the
//     request's sequence number and the NEW type. A refusal of the get, an
//     NLMSG_ERROR naming it, ends the wait as well;
//   - the echo of a request with NLM_F_ECHO: nlmsg_notify unicasts the
//     requester a copy of the multicast, the same bytes, so of two equal
//     messages with the request's sequence number after it, one is the
//     echo.
func Notifications(msgs []Msg) []int {
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
	type echo struct {
		seq  uint32
		seen int // the index of the first copy, or -1
	}
	var echoes []echo
	echoed := func(i int) bool {
		m := &msgs[i]
		for k, e := range echoes {
			if e.seq != m.Hdr.Seq {
				continue
			}
			if e.seen < 0 {
				echoes[k].seen = i
				return false
			}
			f := &msgs[e.seen]
			if f.Hdr == m.Hdr && bytes.Equal(f.Body, m.Body) {
				echoes = append(echoes[:k], echoes[k+1:]...)
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
			if IsRtmGet(h.Type) && h.Flags&unix.NLM_F_DUMP != unix.NLM_F_DUMP {
				gets = append(gets, pending{h.Seq, h.Type - 2})
			}
			if h.Flags&unix.NLM_F_ECHO != 0 && !IsRtmGet(h.Type) {
				echoes = append(echoes, echo{seq: h.Seq, seen: -1})
			}
		case h.Flags&unix.NLM_F_MULTI != 0:
		case h.Type == unix.NLMSG_ERROR:
			if len(m.Body) >= 4+xtcpnl.NlMsgHdrSizeCst {
				var orig xtcpnl.NlMsgHdr
				if _, err := xtcpnl.DeserializeNlMsgHdr(m.Body[4:], &orig); err == nil && IsRtmGet(orig.Type) {
					answered(orig.Seq, orig.Type-2)
				}
			}
		case answered(h.Seq, h.Type):
		case echoed(i):
		default:
			out = append(out, i)
		}
	}
	return out
}

// IsRtmGet is an RTM_GET* type: the third of its family's four, NEW, DEL,
// GET and SET, from RTM_BASE. The tc types keep the rule: GETQDISC 38,
// GETTCLASS 42, GETTFILTER 46, GETACTION 50, GETCHAIN 102.
func IsRtmGet(t uint16) bool {
	return t >= unix.RTM_BASE && (t-unix.RTM_BASE)%4 == 2
}
