package gotc

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/randomizedcoder/xtcp2/internal/nlmon"
	"github.com/randomizedcoder/xtcp2/internal/wsreplay"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// rcvbufCst and sndbufCst are rtnl_open's socket buffers
// (lib/libnetlink.c: rcvbuf = 1024 * 1024, sndbuf = 32768).
const (
	rcvbufCst = 1 << 20
	sndbufCst = 32768
)

// Subscribe is rtnl_open(&rth, groups): a new socket bound to the groups,
// with rtnl_open's buffers and extended acks. errOut takes what
// rtnl_listen prints and carries on after.
func (s *LiveSource) Subscribe(groups uint32, errOut io.Writer) (MonitorConn, error) {
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_CLOEXEC, unix.NETLINK_ROUTE)
	if err != nil {
		return nil, fmt.Errorf("%w: socket: %w", errLive, err)
	}
	c := &liveMonitorConn{fd: fd, errOut: errOut, buf: make([]byte, 1<<20)}
	abandon := func(cause error) error { return errors.Join(cause, c.Close()) }
	if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_SNDBUF, sndbufCst); err != nil {
		return nil, abandon(fmt.Errorf("%w: SO_SNDBUF: %w", errLive, err))
	}
	if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_RCVBUF, rcvbufCst); err != nil {
		return nil, abandon(fmt.Errorf("%w: SO_RCVBUF: %w", errLive, err))
	}
	if err := unix.SetsockoptInt(fd, unix.SOL_NETLINK, unix.NETLINK_EXT_ACK, 1); err != nil {
		return nil, abandon(fmt.Errorf("%w: NETLINK_EXT_ACK: %w", errLive, err))
	}
	if err := unix.Bind(fd, &unix.SockaddrNetlink{Family: unix.AF_NETLINK, Groups: groups}); err != nil {
		return nil, abandon(fmt.Errorf("%w: bind: %w", errLive, err))
	}
	sa, err := unix.Getsockname(fd)
	if err != nil {
		return nil, abandon(fmt.Errorf("%w: getsockname: %w", errLive, err))
	}
	if nl, ok := sa.(*unix.SockaddrNetlink); ok {
		c.pid = nl.Pid
	}
	return c, nil
}

// liveMonitorConn is the subscribed socket.
type liveMonitorConn struct {
	fd     int
	pid    uint32 // the socket's port id, rth->local.nl_pid
	errOut io.Writer
	buf    []byte
}

// Close releases the socket.
func (c *liveMonitorConn) Close() error {
	if c.fd < 0 {
		return nil
	}
	err := unix.Close(c.fd)
	c.fd = -1
	return err
}

// Talk sends req and reads its reply as rtnl_dump_filter does on a
// subscribed socket: a message not to this socket's port id with the
// request's sequence number is a notification, and is skipped.
func (c *liveMonitorConn) Talk(req []byte) (Reply, error) {
	if len(req) < nlHeaderLen {
		return Reply{}, fmt.Errorf("%w: a request of %d bytes", errLive, len(req))
	}
	seq := xtcpnl.NativeEndian().Uint32(req[8:12])
	if err := unix.Sendto(c.fd, req, 0, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		return Reply{}, fmt.Errorf("%w: send: %w", errLive, err)
	}
	var rep Reply
	for {
		n, _, err := unix.Recvfrom(c.fd, c.buf, 0)
		if err != nil {
			return Reply{}, fmt.Errorf("%w: recv: %w", errLive, err)
		}
		for _, m := range wsreplay.SplitMsgs(c.buf[:n]) {
			if m.PID != c.pid || m.Seq != seq {
				continue
			}
			m.Body = append([]byte(nil), m.Body...)
			rep.Msgs = append(rep.Msgs, m)
			if m.Type == unix.NLMSG_DONE || m.Type == unix.NLMSG_ERROR || m.Flags&unix.NLM_F_MULTI == 0 {
				return rep, nil
			}
		}
	}
}

// Listen is rtnl_listen on the socket.
func (c *liveMonitorConn) Listen(fn func(nlmon.Msg) error) error {
	return nlmon.Listen(c.fd, nlmon.ListenOpts{ErrOut: c.errOut}, fn)
}

// Subscribe on a replay is its capture's notifications, as internal/nlmon
// classifies them, whatever the groups: the capture holds what the kernel
// sent the monitor that ran, the trigger's requests and acks among it.
func (s *ReplaySource) Subscribe(uint32, io.Writer) (MonitorConn, error) {
	msgs, err := nlmon.PcapMsgs(s.path)
	if err != nil {
		return nil, errors.Join(ErrReplay, err)
	}
	return &replayMonitorConn{msgs: msgs, notes: nlmon.Notifications(msgs)}, nil
}

// replayMonitorConn plays a monitor capture: the dump run on the
// subscription, then the notifications after it.
type replayMonitorConn struct {
	msgs  []nlmon.Msg
	notes []int
	start int // past the last dump answered: what came before is lost
}

// Talk answers a dump from the capture: the first request from start on
// with the same type and flags and the same body, then every message with
// its sequence number that is not a request, through its NLMSG_DONE or
// NLMSG_ERROR. The trigger's requests in between are another process's,
// so this does not go through the source's in-order replay.
func (c *replayMonitorConn) Talk(req []byte) (Reply, error) {
	if len(req) < nlHeaderLen {
		return Reply{}, fmt.Errorf("%w: a request of %d bytes", ErrReplay, len(req))
	}
	ne := xtcpnl.NativeEndian()
	typ, flags := ne.Uint16(req[4:6]), ne.Uint16(req[6:8])
	at := -1
	for i := c.start; i < len(c.msgs); i++ {
		h := &c.msgs[i].Hdr
		if h.Type == typ && h.Flags == flags && h.Pid == 0 && bytes.Equal(c.msgs[i].Body, req[nlHeaderLen:]) {
			at = i
			break
		}
	}
	if at < 0 {
		return Reply{}, fmt.Errorf("%w: the monitor's request %x is not in the capture", ErrReplay, req)
	}
	seq := c.msgs[at].Hdr.Seq
	var rep Reply
	for i := at + 1; i < len(c.msgs); i++ {
		m := &c.msgs[i]
		h := m.Hdr
		if h.Seq != seq || (h.Flags&unix.NLM_F_REQUEST != 0 && h.Pid == 0) {
			continue
		}
		rep.Msgs = append(rep.Msgs, Msg{Type: h.Type, Flags: h.Flags, Seq: h.Seq, PID: h.Pid, Body: m.Body})
		if h.Type == unix.NLMSG_DONE || h.Type == unix.NLMSG_ERROR || h.Flags&unix.NLM_F_MULTI == 0 {
			c.start = i + 1
			return rep, nil
		}
	}
	return Reply{}, fmt.Errorf("%w: the monitor's dump has no NLMSG_DONE", ErrReplay)
}

// Listen hands each notification from start on to fn, through
// rtnl_listen's 16384-byte buffer (MO5).
func (c *replayMonitorConn) Listen(fn func(nlmon.Msg) error) error {
	return nlmon.Replay(c.msgs, c.notes, c.start, 0, fn)
}

// Close has nothing to release.
func (c *replayMonitorConn) Close() error { return nil }
