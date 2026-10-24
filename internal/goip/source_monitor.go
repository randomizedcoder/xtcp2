package goip

import (
	"errors"
	"fmt"
	"io"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// MonitorSource is the narrow sibling `ip monitor` needs: a subscription to
// multicast groups, whose messages are then handed over one at a time.
type MonitorSource interface {
	Subscribe(sub monitorSub) (MonitorConn, error)
}

// MonitorConn is one subscription. Dump runs a dump on it, as ll_init_map
// does on the monitor's own socket, so the notifications that come while
// the dump runs are lost, as rtnl_dump_filter skips them. Listen hands every
// message after to fn until fn fails or the source ends: a socket ends only
// on an error, a replay at the end of its capture.
type MonitorConn interface {
	Source
	Listen(fn func(monitorMsg) error) error
	Close() error
}

// monitorRecvBufCst is rtnl_listen's buffer.
const monitorRecvBufCst = 16384

// monitorCmsgBufCst is room for rtnl_listen's one control message, an int.
const monitorCmsgBufCst = 64

// Subscribe is rtnl_open with the groups, then each rtnl_add_nl_group and,
// for all-nsid, rtnl_listen_all_nsid. The socket is a new one, as `ip`'s
// is after rtnl_close: the source's own socket keeps answering the dumps.
func (s *NetlinkSource) Subscribe(sub monitorSub) (MonitorConn, error) {
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_CLOEXEC, unix.NETLINK_ROUTE)
	if err != nil {
		return nil, fmt.Errorf("%w: Cannot open netlink socket: %w", ErrMonitorSetup, err)
	}
	conn := &netlinkMonitorConn{fd: fd, allNsid: sub.allNsid, errOut: sub.errOut}
	abandon := func(cause error) error {
		return errors.Join(cause, conn.Close())
	}
	if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_RCVBUF, 1<<20); err != nil {
		return nil, abandon(fmt.Errorf("%w: SO_RCVBUF: %w", ErrMonitorSetup, err))
	}
	if err := unix.Bind(fd, &unix.SockaddrNetlink{Family: unix.AF_NETLINK, Groups: sub.groups}); err != nil {
		return nil, abandon(fmt.Errorf("%w: Cannot bind netlink socket: %w", ErrMonitorSetup, err))
	}
	for _, a := range sub.adds {
		err := unix.SetsockoptInt(fd, unix.SOL_NETLINK, unix.NETLINK_ADD_MEMBERSHIP, int(a.group))
		if err != nil && a.needed && !errors.Is(err, unix.EINVAL) {
			return nil, abandon(fmt.Errorf("%w: %s", ErrMonitorSetup, a.errMsg))
		}
	}
	if sub.allNsid {
		if err := unix.SetsockoptInt(fd, unix.SOL_NETLINK, unix.NETLINK_LISTEN_ALL_NSID, 1); err != nil {
			return nil, abandon(fmt.Errorf("%w: NETLINK_LISTEN_ALL_NSID: %w", ErrMonitorSetup, err))
		}
	}
	return conn, nil
}

// netlinkMonitorConn is the subscribed socket.
type netlinkMonitorConn struct {
	fd      int
	allNsid bool
	errOut  io.Writer
}

// Close releases the socket.
func (c *netlinkMonitorConn) Close() error {
	if c.fd < 0 {
		return nil
	}
	err := unix.Close(c.fd)
	c.fd = -1
	return err
}

// Dump sends the request on the subscribed socket and collects every
// reply body of msgType; a notification is not of the request's sequence
// number, and is dropped.
func (c *netlinkMonitorConn) Dump(request []byte, msgType uint16) ([][]byte, error) {
	var out [][]byte
	kernel := &unix.SockaddrNetlink{Family: unix.AF_NETLINK}
	err := xtcpnl.DumpRtnetlinkMsgsDoneErr(c.fd, request, kernel, func(h xtcpnl.NlMsgHdr, body []byte) error {
		if h.Type == msgType {
			out = append(out, xtcpnl.CopyBytes(body))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("goip: dump: %w", err)
	}
	return out, nil
}

// Listen is rtnl_listen on the subscribed socket.
func (c *netlinkMonitorConn) Listen(fn func(monitorMsg) error) error {
	return listenFd(c.fd, c.allNsid, c.errOut, fn)
}

// listenFd is rtnl_listen: EINTR and EAGAIN are retried, ENOBUFS, the
// kernel's word for messages dropped on a full queue, is reported and
// retried, and any other receive error ends the listen.
func listenFd(fd int, allNsid bool, errOut io.Writer, fn func(monitorMsg) error) error {
	buf := make([]byte, monitorRecvBufCst)
	oob := make([]byte, monitorCmsgBufCst)
	for {
		n, oobn, _, _, err := unix.Recvmsg(fd, buf, oob, 0)
		switch {
		case errors.Is(err, unix.EINTR), errors.Is(err, unix.EAGAIN):
			continue
		case errors.Is(err, unix.ENOBUFS):
			if _, werr := fmt.Fprintf(errOut, "netlink receive error %s (%d)\n", unix.ENOBUFS.Error(), int(unix.ENOBUFS)); werr != nil {
				return werr
			}
			continue
		case err != nil:
			return fmt.Errorf("netlink receive error %w", err)
		case n == 0:
			return errors.New("EOF on netlink")
		}
		nsid := int32(-1)
		if allNsid {
			nsid = listenNsid(oob[:oobn])
		}
		if err := monitorDatagram(buf[:n], nsid, fn); err != nil {
			return err
		}
	}
}

// listenNsid is the netns id a NETLINK_LISTEN_ALL_NSID control message
// carries, the last one if more came, or -1.
func listenNsid(oob []byte) int32 {
	nsid := int32(-1)
	cmsgs, err := unix.ParseSocketControlMessage(oob)
	if err != nil {
		return nsid
	}
	for _, cm := range cmsgs {
		if cm.Header.Level == unix.SOL_NETLINK && cm.Header.Type == unix.NETLINK_LISTEN_ALL_NSID &&
			len(cm.Data) >= 4 {
			nsid = int32(xtcpnl.NativeEndian().Uint32(cm.Data[:4]))
		}
	}
	return nsid
}

// monitorDatagram is rtnl_listen's walk of one datagram: each message to
// fn, until one's length is under its header or past the datagram.
func monitorDatagram(b []byte, nsid int32, fn func(monitorMsg) error) error {
	for len(b) >= xtcpnl.NlMsgHdrSizeCst {
		var h xtcpnl.NlMsgHdr
		if _, err := xtcpnl.DeserializeNlMsgHdr(b, &h); err != nil {
			return err
		}
		if h.Len < xtcpnl.NlMsgHdrSizeCst || int(h.Len) > len(b) {
			return fmt.Errorf("!!!malformed message: len=%d", int32(h.Len))
		}
		if err := fn(monitorMsg{hdr: h, body: b[xtcpnl.NlMsgHdrSizeCst:h.Len], nsid: nsid}); err != nil {
			return err
		}
		next := nlmsgAlign(int(h.Len))
		if next > len(b) {
			return nil
		}
		b = b[next:]
	}
	if len(b) != 0 {
		return fmt.Errorf("!!!Remnant of size %d", len(b))
	}
	return nil
}

// Subscribe on a replay is its capture's notifications, whatever the
// groups: the capture holds what the kernel sent the subscriber that was
// running, and nothing in a message says which group it came on.
func (s *ReplaySource) Subscribe(monitorSub) (MonitorConn, error) {
	msgs := s.cap.Msgs()
	return &replayMonitorConn{src: s, msgs: msgs, notes: replayNotifications(msgs)}, nil
}

// replayMonitorConn plays notifications: those from start on, start being
// past the end of the dump run on it.
type replayMonitorConn struct {
	src   *ReplaySource
	msgs  []nlparity.Msg
	notes []int
	start int
}

// Dump is the source's, and moves start past the recorded dump's
// NLMSG_DONE: the first request of the request's type with NLM_F_DUMP,
// then the first DONE after it with its sequence number. A capture with no
// such pair leaves start where it was.
func (c *replayMonitorConn) Dump(request []byte, msgType uint16) ([][]byte, error) {
	if len(request) >= xtcpnl.NlMsgHdrSizeCst {
		typ := xtcpnl.NativeEndian().Uint16(request[4:6])
		c.start = max(c.start, replayDumpEnd(c.msgs, typ))
	}
	return c.src.Dump(request, msgType)
}

// replayDumpEnd is the index past a recorded dump of typ's NLMSG_DONE, or
// 0 when the capture has none.
func replayDumpEnd(msgs []nlparity.Msg, typ uint16) int {
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

// Listen hands each notification from start on to fn, as from a socket
// with no NETLINK_LISTEN_ALL_NSID.
func (c *replayMonitorConn) Listen(fn func(monitorMsg) error) error {
	for _, i := range c.notes {
		if i < c.start {
			continue
		}
		m := &c.msgs[i]
		if err := fn(monitorMsg{hdr: m.Hdr, body: m.Body, nsid: -1}); err != nil {
			return err
		}
	}
	return nil
}

// Close has nothing to release.
func (c *replayMonitorConn) Close() error { return nil }

// replayNotifications is what of a capture a subscriber was sent, by
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
func replayNotifications(msgs []nlparity.Msg) []int {
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
