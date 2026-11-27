package gotc

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/randomizedcoder/xtcp2/internal/wsreplay"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// Msg is one netlink message of a reply.
type Msg = wsreplay.Msg

// Reply is a request's answer, up to and including its NLMSG_DONE or
// NLMSG_ERROR (or its one message, for a get).
type Reply = wsreplay.Reply

// Source answers gotc's netlink requests: a ws pcap replay, or the
// kernel.
type Source interface {
	Talk(req []byte) (Reply, error)
}

// ErrReplay is every replay failure; Run maps it to exit 2.
var ErrReplay = errors.New("gotc: replay")

// ReplaySource answers from a ws pcap capture, request by request.
type ReplaySource struct {
	*wsreplay.Replay
	path string // the capture, which a monitor's Subscribe reads again
}

// NewReplaySource opens the ws pcap at path.
func NewReplaySource(path string) (*ReplaySource, error) {
	r, err := wsreplay.Open(path, wsreplay.Options{Err: ErrReplay, Clone: "gotc", Tool: "tc"})
	if err != nil {
		return nil, err
	}
	return &ReplaySource{Replay: r, path: path}, nil
}

// errLive wraps a live socket failure.
var errLive = errors.New("gotc: netlink")

// LiveSource talks to the kernel over one NETLINK_ROUTE socket. gotc
// only reads: its requests are dumps and gets.
type LiveSource struct {
	fd  int
	buf []byte
}

// NewLiveSource opens the socket, with extended acks on as rtnl_open sets
// them.
func NewLiveSource() (*LiveSource, error) {
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_CLOEXEC, unix.NETLINK_ROUTE)
	if err != nil {
		return nil, fmt.Errorf("%w: socket: %w", errLive, err)
	}
	if err := unix.SetsockoptInt(fd, unix.SOL_NETLINK, unix.NETLINK_EXT_ACK, 1); err != nil {
		return nil, errors.Join(fmt.Errorf("%w: NETLINK_EXT_ACK: %w", errLive, err), unix.Close(fd))
	}
	if err := unix.Bind(fd, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		return nil, errors.Join(fmt.Errorf("%w: bind: %w", errLive, err), unix.Close(fd))
	}
	return &LiveSource{fd: fd, buf: make([]byte, 1<<20)}, nil
}

// Close closes the socket.
func (s *LiveSource) Close() error { return unix.Close(s.fd) }

// Talk sends req and reads until the dump's NLMSG_DONE or NLMSG_ERROR, or a
// get's one reply.
func (s *LiveSource) Talk(req []byte) (Reply, error) {
	if !isGetRequest(req) {
		return Reply{}, fmt.Errorf("%w: refusing to send a request that is not a get or dump", errLive)
	}
	if err := unix.Sendto(s.fd, req, 0, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		return Reply{}, fmt.Errorf("%w: send: %w", errLive, err)
	}
	var rep Reply
	for {
		n, _, err := unix.Recvfrom(s.fd, s.buf, 0)
		if err != nil {
			return Reply{}, fmt.Errorf("%w: recv: %w", errLive, err)
		}
		for _, m := range wsreplay.SplitMsgs(s.buf[:n]) {
			m.Body = append([]byte(nil), m.Body...)
			rep.Msgs = append(rep.Msgs, m)
			if m.Type == unix.NLMSG_DONE || m.Type == unix.NLMSG_ERROR || m.Flags&unix.NLM_F_MULTI == 0 {
				return rep, nil
			}
		}
	}
}

// nlHeaderLen is the nlmsghdr length.
const nlHeaderLen = xtcpnl.NlMsgHdrSizeCst

// isGetRequest reports whether req is an rtnetlink RTM_GET*: every get
// type is 2 mod 4 (RTM_BASE is 16, and each object's NEW, DEL, GET, SET
// come in fours). The kernel socket never carries a write, whatever the
// build (increment 17: writes are dry-run).
func isGetRequest(req []byte) bool {
	if len(req) < nlHeaderLen {
		return false
	}
	t := binary.LittleEndian.Uint16(req[4:6])
	return t >= unix.RTM_BASE && t%4 == unix.RTM_GETLINK%4
}
