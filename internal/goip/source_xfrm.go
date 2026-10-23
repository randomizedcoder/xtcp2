package goip

import (
	"fmt"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// NETLINK_XFRM, the protocol `ip xfrm` speaks. Each of ip/xfrm_*.c's
// functions opens it with rtnl_open_byproto, beside the route socket ip.c
// opens for every command; goip opens one per run, the first time an xfrm
// object asks. Its types are fixed, XFRM_MSG_*, with no lookup ahead of
// them, and a dump answers in other types than its request's: a GETSA dump
// in NEWSAs.

// xfrmMsg is one answer of an XFRM exchange: its type, flags and body.
type xfrmMsg struct {
	typ, flags uint16
	body       []byte
}

// XfrmSource is the NETLINK_XFRM side of a source. A Source that is not
// one has no xfrm socket, and an xfrm object is not implemented on it.
type XfrmSource interface {
	// XfrmDump sends a dump request on the xfrm socket and returns every
	// answer but the NLMSG_DONE that ends it, in wire order. A dump ended
	// by an errno fails with it.
	XfrmDump(request []byte) ([]xfrmMsg, error)
	// XfrmTalk sends a request on the xfrm socket and returns its one
	// answer's type and body, both zero for an ACK. A refusal is an error.
	XfrmTalk(request []byte) (uint16, []byte, error)
}

// xfrmSource is the run's xfrm side.
func xfrmSource(c *runCtx) (XfrmSource, error) {
	xs, ok := c.src.(XfrmSource)
	if !ok {
		return nil, fmt.Errorf("xfrm: %w", ErrNotImplemented)
	}
	return xs, nil
}

// xfrm returns the source's xfrm socket, opening it on first use.
func (s *NetlinkSource) xfrm() (int, error) {
	if s.xopen {
		return s.xfd, nil
	}
	fd, err := openByProto(unix.NETLINK_XFRM, "xfrm netlink", s.rcvTimeout)
	if err != nil {
		return -1, err
	}
	s.xfd, s.xopen = fd, true
	return fd, nil
}

// XfrmDump sends an XFRM dump and collects its answers.
func (s *NetlinkSource) XfrmDump(request []byte) ([]xfrmMsg, error) {
	if len(request) < xtcpnl.NlMsgHdrSizeCst {
		return nil, xtcpnl.ErrShortRequest
	}
	fd, err := s.xfrm()
	if err != nil {
		return nil, err
	}
	var out []xfrmMsg
	err = xtcpnl.DumpRtnetlinkMsgsDoneErr(fd, request, s.sa, func(h xtcpnl.NlMsgHdr, body []byte) error {
		out = append(out, xfrmMsg{typ: h.Type, flags: h.Flags, body: xtcpnl.CopyBytes(body)})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("goip: xfrm dump: %w", err)
	}
	return out, nil
}

// XfrmTalk sends an XFRM request and returns its single answer.
func (s *NetlinkSource) XfrmTalk(request []byte) (uint16, []byte, error) {
	fd, err := s.xfrm()
	if err != nil {
		return 0, nil, err
	}
	typ, body, err := xtcpnl.TalkRtnetlink(fd, request, s.sa)
	if err != nil {
		return 0, nil, fmt.Errorf("goip: xfrm talk: %w", err)
	}
	return typ, xtcpnl.CopyBytes(body), nil
}

// XfrmDump replays a recorded XFRM dump: its answers but the DONE, or the
// errno its NLMSG_DONE or NLMSG_ERROR carried.
func (s *ReplaySource) XfrmDump(request []byte) ([]xfrmMsg, error) {
	t, err := replayExchange(s.xseg, &s.xused, request, "xfrm")
	if err != nil {
		return nil, err
	}
	var out []xfrmMsg
	for _, m := range t.Replies {
		switch m.Hdr.Type {
		case unix.NLMSG_DONE, unix.NLMSG_ERROR:
			if err := xtcpnl.DoneErr(m.Body); err != nil {
				return nil, fmt.Errorf("goip: xfrm dump: %w", err)
			}
		default:
			out = append(out, xfrmMsg{typ: m.Hdr.Type, flags: m.Hdr.Flags, body: xtcpnl.CopyBytes(m.Body)})
		}
	}
	return out, nil
}

// XfrmTalk replays a recorded XFRM request's answer: its type and body,
// both zero for an ACK, or the errno a refusal carried.
func (s *ReplaySource) XfrmTalk(request []byte) (uint16, []byte, error) {
	t, err := replayExchange(s.xseg, &s.xused, request, "xfrm")
	if err != nil {
		return 0, nil, err
	}
	if len(t.Replies) == 0 {
		return 0, nil, fmt.Errorf("%w: the recorded exchange has no reply", ErrNoReplay)
	}
	m := t.Replies[0]
	switch m.Hdr.Type {
	case unix.NLMSG_DONE, unix.NLMSG_ERROR:
		if err := xtcpnl.DoneErr(m.Body); err != nil {
			return 0, nil, fmt.Errorf("goip: xfrm talk: %w", err)
		}
		return 0, nil, nil
	}
	return m.Hdr.Type, xtcpnl.CopyBytes(m.Body), nil
}
