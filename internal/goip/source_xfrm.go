package goip

import (
	"errors"
	"fmt"
	"io"

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

// XfrmMonitorSource is the NETLINK_XFRM side `ip xfrm monitor` needs: a
// socket of its own in the groups asked for, every message on which is
// handed to fn until fn fails or the source ends.
type XfrmMonitorSource interface {
	XfrmListen(groups uint32, allNsid bool, errOut io.Writer, fn func(monitorMsg) error) error
}

// XfrmListen is do_xfrm_monitor's rtnl_open_byproto with the groups,
// rtnl_listen_all_nsid when asked, then rtnl_listen, on a socket of its
// own. A socket that will not open or bind, or a refused all-nsid, is
// ErrMonitorSetup, exit 1; a receive error ends the listen, exit 2.
func (s *NetlinkSource) XfrmListen(groups uint32, allNsid bool, errOut io.Writer, fn func(monitorMsg) error) error {
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_CLOEXEC, unix.NETLINK_XFRM)
	if err != nil {
		return fmt.Errorf("%w: Cannot open netlink socket: %w", ErrMonitorSetup, err)
	}
	err = func() error {
		if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_RCVBUF, 1<<20); err != nil {
			return fmt.Errorf("%w: SO_RCVBUF: %w", ErrMonitorSetup, err)
		}
		if err := unix.Bind(fd, &unix.SockaddrNetlink{Family: unix.AF_NETLINK, Groups: groups}); err != nil {
			return fmt.Errorf("%w: Cannot bind netlink socket: %w", ErrMonitorSetup, err)
		}
		if allNsid {
			if err := unix.SetsockoptInt(fd, unix.SOL_NETLINK, unix.NETLINK_LISTEN_ALL_NSID, 1); err != nil {
				return fmt.Errorf("%w: setsockopt NETLINK_LISTEN_ALL_NSID: %w", ErrMonitorSetup, err)
			}
		}
		return listenFd(fd, allNsid, errOut, fn)
	}()
	return errors.Join(err, unix.Close(fd))
}

// XfrmListen on a replay hands fn every XFRM message of the capture the
// kernel sent, in capture order, whatever the groups: the events the
// monitor got, and the ACKs of the trigger's writes, which
// xfrm_accept_msg prints nothing for. Nothing in an event says which
// group it came on.
func (s *ReplaySource) XfrmListen(_ uint32, _ bool, _ io.Writer, fn func(monitorMsg) error) error {
	for i := range s.xmsgs {
		m := &s.xmsgs[i]
		if m.Hdr.Flags&unix.NLM_F_REQUEST != 0 {
			continue
		}
		if err := fn(monitorMsg{hdr: m.Hdr, body: m.Body, nsid: -1}); err != nil {
			return err
		}
	}
	return nil
}
