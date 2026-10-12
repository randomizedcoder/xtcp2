package gordma

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"

	"github.com/randomizedcoder/xtcp2/pkg/nlstrace"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// Msg is one netlink message a request was answered with, or one event.
type Msg struct {
	Type  uint16
	Flags uint16
	Seq   uint32
	PID   uint32
	Body  []byte // past the nlmsghdr
}

// Reply is a request's messages in wire order, up to and including the
// NLMSG_DONE or NLMSG_ERROR that ends it, and what they are checked
// against: the request's Seq, and PortID, the socket's port id, which is 0
// when the source cannot know it (a replay).
type Reply struct {
	Msgs   []Msg
	Seq    uint32
	PortID uint32
	// Errno is a receive that failed after Msgs, as mnl_socket_recvfrom's.
	Errno unix.Errno
}

// Source is where gordma's requests go. rdma opens a socket per request
// (rd_send_msg, rdma/utils.c), so Talk is one request on one socket.
// Listen joins group and hands every message it reads to fn until fn
// returns false or the source has no more; it is `rdma monitor`.
type Source interface {
	Talk(req []byte) (Reply, error)
	Listen(group uint32, fn func(Msg) bool) (unix.Errno, error)
}

// Errors a source returns. rdma prints its own words for the first two.
var (
	ErrSocket = errors.New("gordma: NETLINK_RDMA socket")
	ErrSend   = errors.New("gordma: send")
	// ErrReplay is a replay that does not match: a request the capture did
	// not make, or more requests than it has. Run exits 2 on it.
	ErrReplay = errors.New("gordma: replay")
)

// netlinkHeaderLen is sizeof(struct nlmsghdr).
const netlinkHeaderLen = xtcpnl.NlMsgHdrSizeCst

// splitMsgs splits a datagram into messages; a remainder too short to be
// one ends it, as mnl_cb_run2's mnl_nlmsg_ok does.
func splitMsgs(b []byte) []Msg {
	var out []Msg
	for len(b) >= netlinkHeaderLen {
		l := int(binary.LittleEndian.Uint32(b[0:4]))
		if l < netlinkHeaderLen || l > len(b) {
			break
		}
		out = append(out, Msg{
			Type:  binary.LittleEndian.Uint16(b[4:6]),
			Flags: binary.LittleEndian.Uint16(b[6:8]),
			Seq:   binary.LittleEndian.Uint32(b[8:12]),
			PID:   binary.LittleEndian.Uint32(b[12:16]),
			Body:  b[netlinkHeaderLen:l],
		})
		b = b[min(len(b), l+xtcpnl.FourByteAlignPadding(l)):]
	}
	return out
}

// ends reports whether m ends a request's reply, as mnlu_cb_array's
// handlers do: NLMSG_DONE always, NLMSG_ERROR always (an ACK or an
// error).
func ends(m Msg) bool { return m.Type == unix.NLMSG_DONE || m.Type == unix.NLMSG_ERROR }

// recvBufSize is MNL_SOCKET_BUFFER_SIZE, rdma's receive buffer: the page
// size, capped at 8 KiB. The kernel sizes dump messages to what the reader
// asks for, so a larger buffer would change the wire.
func recvBufSize() int { return min(os.Getpagesize(), 8192) }

// LiveSource is the kernel's NETLINK_RDMA.
type LiveSource struct{}

// open is mnlu_socket_open: CAP_ACK and EXT_ACK on, bound to an auto port.
func (LiveSource) open(groups uint32) (int, uint32, error) {
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_CLOEXEC, unix.NETLINK_RDMA)
	if err != nil {
		return -1, 0, errors.Join(ErrSocket, err)
	}
	for _, opt := range []int{unix.NETLINK_CAP_ACK, unix.NETLINK_EXT_ACK} {
		if err := unix.SetsockoptInt(fd, unix.SOL_NETLINK, opt, 1); err != nil {
			return -1, 0, errors.Join(ErrSocket, err, unix.Close(fd))
		}
	}
	if err := unix.Bind(fd, &unix.SockaddrNetlink{Family: unix.AF_NETLINK, Groups: groups}); err != nil {
		return -1, 0, errors.Join(ErrSocket, err, unix.Close(fd))
	}
	sa, err := unix.Getsockname(fd)
	if err != nil {
		return -1, 0, errors.Join(ErrSocket, err, unix.Close(fd))
	}
	nl, ok := sa.(*unix.SockaddrNetlink)
	if !ok {
		return -1, 0, errors.Join(ErrSocket, unix.Close(fd))
	}
	return fd, nl.Pid, nil
}

// Talk sends req on a new socket and reads until its reply ends.
func (s LiveSource) Talk(req []byte) (r Reply, err error) {
	if len(req) < netlinkHeaderLen {
		return Reply{}, ErrSend
	}
	fd, portID, err := s.open(0)
	if err != nil {
		return Reply{}, err
	}
	defer func() { err = errors.Join(err, unix.Close(fd)) }()
	if err := unix.Sendto(fd, req, 0, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		return Reply{}, errors.Join(ErrSend, err)
	}
	r = Reply{Seq: binary.LittleEndian.Uint32(req[8:12]), PortID: portID}
	buf := make([]byte, recvBufSize())
	for {
		n, _, err := unix.Recvfrom(fd, buf, 0)
		if err != nil {
			r.Errno = errnoOf(err)
			return r, nil
		}
		if n == 0 {
			return r, nil
		}
		for _, m := range splitMsgs(bytes.Clone(buf[:n])) {
			r.Msgs = append(r.Msgs, m)
			if ends(m) {
				return r, nil
			}
		}
	}
}

// Listen reads group's events until fn says stop or the read fails.
func (s LiveSource) Listen(group uint32, fn func(Msg) bool) (_ unix.Errno, err error) {
	fd, _, err := s.open(1 << (group - 1))
	if err != nil {
		return 0, err
	}
	defer func() { err = errors.Join(err, unix.Close(fd)) }()
	buf := make([]byte, recvBufSize())
	for {
		n, _, err := unix.Recvfrom(fd, buf, 0)
		if err != nil {
			return errnoOf(err), nil
		}
		if n == 0 {
			return 0, nil
		}
		for _, m := range splitMsgs(bytes.Clone(buf[:n])) {
			if !fn(m) {
				return 0, nil
			}
		}
	}
}

func errnoOf(err error) unix.Errno {
	var e unix.Errno
	if errors.As(err, &e) {
		return e
	}
	return unix.EIO
}

// ReplaySource answers from an strace capture of rdma (pkg/nlstrace): each
// request must be the one rdma sent next, byte for byte once nlmsg_seq and
// nlmsg_pid are zeroed, and is answered with what rdma read on that socket.
// A capture's reads on a socket it never sent on are the monitor's events.
type ReplaySource struct {
	txs    []replayTx
	events []Msg
	next   int
}

type replayTx struct {
	req  []byte
	msgs []Msg
}

// NewReplaySource reads the capture at path.
func NewReplaySource(path string) (*ReplaySource, error) {
	dgs, err := nlstrace.ParseFile(path)
	if err != nil {
		return nil, errors.Join(ErrReplay, err)
	}
	return newReplay(dgs), nil
}

func newReplay(dgs []nlstrace.Datagram) *ReplaySource {
	s := &ReplaySource{}
	txns, events := nlstrace.Transactions(dgs)
	for _, tx := range txns {
		r := replayTx{req: tx.Req}
		for _, b := range tx.Recvs {
			r.msgs = append(r.msgs, splitMsgs(b)...)
		}
		s.txs = append(s.txs, r)
	}
	for _, b := range events {
		s.events = append(s.events, splitMsgs(b)...)
	}
	return s
}

// Talk answers req with the next recorded transaction, or ErrReplay.
func (s *ReplaySource) Talk(req []byte) (Reply, error) {
	if s.next >= len(s.txs) {
		return Reply{}, fmt.Errorf("%w: request %d is past the %d the capture has", ErrReplay, s.next, len(s.txs))
	}
	tx := s.txs[s.next]
	if !bytes.Equal(nlstrace.ZeroSeqPID(req), nlstrace.ZeroSeqPID(tx.req)) {
		return Reply{}, fmt.Errorf("%w: request %d differs from the capture's\n gordma %x\n rdma   %x",
			ErrReplay, s.next, nlstrace.ZeroSeqPID(req), nlstrace.ZeroSeqPID(tx.req))
	}
	s.next++
	r := Reply{Msgs: tx.msgs}
	if len(tx.req) >= netlinkHeaderLen {
		r.Seq = binary.LittleEndian.Uint32(tx.req[8:12])
	}
	return r, nil
}

// Listen hands over the capture's events, then returns as if the stream
// ended, which a live monitor never does: rdma was stopped by a signal.
func (s *ReplaySource) Listen(_ uint32, fn func(Msg) bool) (unix.Errno, error) {
	for _, m := range s.events {
		if !fn(m) {
			break
		}
	}
	return 0, nil
}

// Pending is how many recorded requests were not asked for.
func (s *ReplaySource) Pending() int { return len(s.txs) - s.next }
