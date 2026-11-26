package goip

import (
	"errors"
	"fmt"
	"io"

	"github.com/randomizedcoder/xtcp2/internal/nlmon"
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

// listenFd is nlmon.Listen with each message as a monitorMsg.
func listenFd(fd int, allNsid bool, errOut io.Writer, fn func(monitorMsg) error) error {
	return nlmon.Listen(fd, nlmon.ListenOpts{AllNsid: allNsid, ErrOut: errOut}, func(m nlmon.Msg) error {
		return fn(monitorMsg{hdr: m.Hdr, body: m.Body, nsid: m.Nsid})
	})
}

// Subscribe on a replay is its capture's notifications, whatever the
// groups: the capture holds what the kernel sent the subscriber that was
// running, and nothing in a message says which group it came on.
func (s *ReplaySource) Subscribe(monitorSub) (MonitorConn, error) {
	msgs := nlmonMsgs(s.cap.Msgs())
	return &replayMonitorConn{src: s, msgs: msgs, notes: nlmon.Notifications(msgs)}, nil
}

// replayMonitorConn plays notifications: those from start on, start being
// past the end of the dump run on it.
type replayMonitorConn struct {
	src   *ReplaySource
	msgs  []nlmon.Msg
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
		c.start = max(c.start, nlmon.DumpEnd(c.msgs, typ))
	}
	return c.src.Dump(request, msgType)
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

// nlmonMsgs is a capture's messages as nlmon walks them.
func nlmonMsgs(msgs []nlparity.Msg) []nlmon.Msg {
	out := make([]nlmon.Msg, len(msgs))
	for i := range msgs {
		out[i] = nlmon.Msg{Hdr: msgs[i].Hdr, Body: msgs[i].Body, Nsid: -1}
	}
	return out
}
