package goip

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// Generic netlink, the transport the genl objects (fou, ila, ioam, l2tp,
// macsec, mptcp, sr, tcp_metrics) share.
//
// `ip` opens a second socket for them, NETLINK_GENERIC beside the route one,
// and asks the controller for the family's id by name before its first
// request: genl_init_handle, once per family per run (lib/libgenl.c:142),
// which is genl_resolve_family's one CTRL_CMD_GETFAMILY (lib/libgenl.c:50).
// The id is the boot's, so every request after it is built on the answer.

// ErrGenlResolve is a family the controller did not resolve: the request
// failed, the kernel refused it (ENOENT for a family not registered, a module
// not loaded), or the answer was not a CTRL_CMD_NEWFAMILY with an id.
var ErrGenlResolve = errors.New("goip: generic netlink family not resolved")

// GenlSource is the generic netlink side of a source. A Source that is not
// one has no genl socket, and an object that needs one is not implemented on
// it.
type GenlSource interface {
	// GenlDump sends a dump request on the genl socket and returns the body
	// of every reply of the request's own type, the family id, in wire
	// order. A dump ended by an errno fails with it.
	GenlDump(request []byte) ([][]byte, error)
	// GenlTalk sends a request on the genl socket and returns its one
	// reply's type and body, both zero for an ACK. A refusal is an error.
	GenlTalk(request []byte) (uint16, []byte, error)
}

// GenlMonitorSource is the genl side a monitor needs: a multicast group
// joined on the genl socket, as genl_add_mcast_grp's rtnl_add_nl_group, and
// every message after handed to fn, as rtnl_listen hands it on that socket,
// until fn fails or the source ends: a socket on an error, a replay at the
// end of its capture.
type GenlMonitorSource interface {
	GenlJoin(group uint32) error
	GenlListen(errOut io.Writer, fn func(monitorMsg) error) error
}

// genlSndBufCst is rtnl_open_byproto's SO_SNDBUF (lib/libnetlink.c:197).
const genlSndBufCst = 32768

// genlGetFamilyVersionCst is the version iproute2's GENL_REQUEST puts on a
// CTRL_CMD_GETFAMILY (lib/libgenl.c:52).
const genlGetFamilyVersionCst = 0

// openGenl opens the genl socket as rtnl_open_byproto does
// (lib/libnetlink.c:193): SO_SNDBUF, SO_RCVBUF at 1 MiB and NETLINK_EXT_ACK,
// which a kernel without it refuses with ENOPROTOOPT and `ip` ignores. Unlike
// the route socket it gets no NETLINK_GET_STRICT_CHK: ip.c sets that on rth
// alone. A source opened with a receive timeout gives it the same one.
func openGenl(rcvTimeout time.Duration) (int, error) {
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_CLOEXEC, unix.NETLINK_GENERIC)
	if err != nil {
		return -1, fmt.Errorf("goip: generic netlink socket: %w", err)
	}
	abandon := func(cause error) error {
		if cerr := unix.Close(fd); cerr != nil {
			return errors.Join(cause, fmt.Errorf("goip: generic netlink close: %w", cerr))
		}
		return cause
	}

	if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_SNDBUF, genlSndBufCst); err != nil {
		return -1, abandon(fmt.Errorf("goip: generic netlink SO_SNDBUF: %w", err))
	}
	if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_RCVBUF, 1<<20); err != nil {
		return -1, abandon(fmt.Errorf("goip: generic netlink SO_RCVBUF: %w", err))
	}
	if err := unix.SetsockoptInt(fd, unix.SOL_NETLINK, unix.NETLINK_EXT_ACK, 1); err != nil &&
		!errors.Is(err, unix.ENOPROTOOPT) {
		return -1, abandon(fmt.Errorf("goip: generic netlink extended ack: %w", err))
	}
	if err := setRcvTimeout(fd, rcvTimeout); err != nil {
		return -1, abandon(fmt.Errorf("goip: generic netlink %w", err))
	}
	if err := unix.Bind(fd, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		return -1, abandon(fmt.Errorf("goip: generic netlink bind: %w", err))
	}
	return fd, nil
}

// genl returns the source's genl socket, opening it on first use as
// genl_init_handle does: a run that touches no genl object opens none.
func (s *NetlinkSource) genl() (int, error) {
	if s.gopen {
		return s.gfd, nil
	}
	fd, err := openGenl(s.rcvTimeout)
	if err != nil {
		return -1, err
	}
	s.gfd, s.gopen = fd, true
	return fd, nil
}

// GenlDump sends a genl dump and collects every reply body of the request's
// type.
func (s *NetlinkSource) GenlDump(request []byte) ([][]byte, error) {
	if len(request) < xtcpnl.NlMsgHdrSizeCst {
		return nil, xtcpnl.ErrShortRequest
	}
	fd, err := s.genl()
	if err != nil {
		return nil, err
	}
	typ := binary.LittleEndian.Uint16(request[4:6])
	var out [][]byte
	err = xtcpnl.DumpRtnetlinkMsgsDoneErr(fd, request, s.sa, func(h xtcpnl.NlMsgHdr, body []byte) error {
		if h.Type == typ {
			out = append(out, xtcpnl.CopyBytes(body))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("goip: genl dump: %w", err)
	}
	return out, nil
}

// GenlTalk sends a genl request and returns its single reply.
func (s *NetlinkSource) GenlTalk(request []byte) (uint16, []byte, error) {
	fd, err := s.genl()
	if err != nil {
		return 0, nil, err
	}
	typ, body, err := xtcpnl.TalkRtnetlink(fd, request, s.sa)
	if err != nil {
		return 0, nil, fmt.Errorf("goip: genl talk: %w", err)
	}
	return typ, xtcpnl.CopyBytes(body), nil
}

// GenlJoin is rtnl_add_nl_group on the genl socket.
func (s *NetlinkSource) GenlJoin(group uint32) error {
	fd, err := s.genl()
	if err != nil {
		return err
	}
	return unix.SetsockoptInt(fd, unix.SOL_NETLINK, unix.NETLINK_ADD_MEMBERSHIP, int(group))
}

// GenlListen is rtnl_listen on the genl socket.
func (s *NetlinkSource) GenlListen(errOut io.Writer, fn func(monitorMsg) error) error {
	fd, err := s.genl()
	if err != nil {
		return err
	}
	return listenFd(fd, false, errOut, fn)
}

// genlSource is the run's genl side, which genlFamily has already found.
func genlSource(c *runCtx) (GenlSource, error) {
	gs, ok := c.src.(GenlSource)
	if !ok {
		return nil, fmt.Errorf("generic netlink: %w", ErrNotImplemented)
	}
	return gs, nil
}

// genlJSONAppend adds one object to the -j array.
func genlJSONAppend(arr *[]byte, obj []byte) {
	if len(*arr) != 0 {
		*arr = append(*arr, ',')
	}
	*arr = append(*arr, obj...)
}

// genlJSONEnd is delete_json_obj: under -j the array and its newline.
func genlJSONEnd(c *runCtx, arr []byte) error {
	if !c.json {
		return nil
	}
	_, err := fmt.Fprintf(c.out, "[%s]\n", arr)
	return err
}

// genlFamily is genl_init_handle: the id of the named family, from one
// CTRL_CMD_GETFAMILY the first time it is asked for and from the run's cache
// after, as `ip` keeps the id in the object's genl_family.
//
// The answer is read as genl_parse_getfamily reads it: a controller message,
// CTRL_CMD_NEWFAMILY, with CTRL_ATTR_FAMILY_ID (lib/libgenl.c:14).
func (c *runCtx) genlFamily(name string) (uint16, error) {
	if id, ok := c.genlIDs[name]; ok {
		return id, nil
	}
	gs, ok := c.src.(GenlSource)
	if !ok {
		return 0, fmt.Errorf("generic netlink: %w", ErrNotImplemented)
	}
	req, err := xtcpnl.BuildGetFamilyRequestVersion(name, genlGetFamilyVersionCst, c.nextSeq())
	if err != nil {
		return 0, fmt.Errorf("%w: %q: %w", ErrGenlResolve, name, err)
	}
	typ, body, err := gs.GenlTalk(req)
	if err != nil {
		return 0, fmt.Errorf("%w: %s: %w", ErrGenlResolve, name, err)
	}
	if typ != unix.GENL_ID_CTRL {
		return 0, fmt.Errorf("%w: %s: not a controller message, nlmsg_type=0x%x", ErrGenlResolve, name, typ)
	}
	f, err := xtcpnl.ParseGenericNetlinkFamily(body)
	if err != nil {
		return 0, fmt.Errorf("%w: %s: %w", ErrGenlResolve, name, err)
	}
	if c.genlIDs == nil {
		c.genlIDs = map[string]uint16{}
	}
	c.genlIDs[name] = f.ID
	return f.ID, nil
}

// genlExchange finds the recorded genl transaction a request replays: the
// first not yet replayed whose request is the same bytes, nlmsg_seq and
// nlmsg_pid aside. Matching the whole request is the attribution a genl
// replay has; the portid filter is the route socket's, and `ip`'s genl socket
// is another port.
func (s *ReplaySource) genlExchange(request []byte) (nlparity.Txn, error) {
	want, ok := canonRequest(request)
	if !ok {
		return nlparity.Txn{}, xtcpnl.ErrShortRequest
	}
	for i, t := range s.gseg.Txns {
		if s.gused[i] {
			continue
		}
		h := t.Request.Hdr
		h.Seq, h.Pid = 0, 0
		if bytes.Equal(wholeMsg(h, t.Request.Body), want) {
			if s.gused == nil {
				s.gused = map[int]bool{}
			}
			s.gused[i] = true
			return t, nil
		}
	}
	return nlparity.Txn{}, fmt.Errorf("%w: no recorded generic netlink exchange for type %d",
		ErrNoReplay, binary.LittleEndian.Uint16(request[4:6]))
}

// canonRequest is a request's nlmsg_len bytes with nlmsg_seq and nlmsg_pid
// zeroed, or false for one shorter than its header or its own length.
func canonRequest(request []byte) ([]byte, bool) {
	if len(request) < xtcpnl.NlMsgHdrSizeCst {
		return nil, false
	}
	n := int(binary.LittleEndian.Uint32(request[0:4]))
	if n < xtcpnl.NlMsgHdrSizeCst || n > len(request) {
		return nil, false
	}
	out := append([]byte(nil), request[:n]...)
	clear(out[8:16])
	return out, true
}

// GenlDump replays a recorded genl dump: the bodies of its replies of the
// request's type, or the errno its NLMSG_DONE or NLMSG_ERROR carried.
func (s *ReplaySource) GenlDump(request []byte) ([][]byte, error) {
	t, err := s.genlExchange(request)
	if err != nil {
		return nil, err
	}
	var out [][]byte
	for _, m := range t.Replies {
		switch m.Hdr.Type {
		case unix.NLMSG_DONE, unix.NLMSG_ERROR:
			if err := xtcpnl.DoneErr(m.Body); err != nil {
				return nil, fmt.Errorf("goip: genl dump: %w", err)
			}
		case t.Request.Hdr.Type:
			out = append(out, xtcpnl.CopyBytes(m.Body))
		}
	}
	return out, nil
}

// GenlTalk replays a recorded genl request's reply: its type and body, both
// zero for an ACK, or the errno a refusal carried.
func (s *ReplaySource) GenlTalk(request []byte) (uint16, []byte, error) {
	t, err := s.genlExchange(request)
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
			return 0, nil, fmt.Errorf("goip: genl talk: %w", err)
		}
		return 0, nil, nil
	}
	return m.Hdr.Type, xtcpnl.CopyBytes(m.Body), nil
}

// GenlJoin on a replay joins nothing: the capture holds what the kernel
// sent the subscriber that ran.
func (s *ReplaySource) GenlJoin(uint32) error { return nil }

// GenlListen hands each of the capture's genl notifications, the messages
// from the kernel that answer no request, to fn.
func (s *ReplaySource) GenlListen(_ io.Writer, fn func(monitorMsg) error) error {
	for i := range s.gseg.Notifications {
		m := &s.gseg.Notifications[i]
		if err := fn(monitorMsg{hdr: m.Hdr, body: m.Body, nsid: -1}); err != nil {
			return err
		}
	}
	return nil
}
