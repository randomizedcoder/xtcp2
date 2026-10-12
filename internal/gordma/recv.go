package gordma

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

var (
	// errFail is a failure gordma has already reported, as rdma does
	// before it exits 1.
	errFail = errors.New("gordma: failed")
	// errCB is a callback's MNL_CB_ERROR: a reply rdma cannot print.
	errCB = errors.New("gordma: unprintable reply")
)

// nlmsgerrLen is sizeof(struct nlmsgerr): the int and the echoed header.
const nlmsgerrLen = 4 + netlinkHeaderLen

// nlmsgerrAttrMsg is NLMSGERR_ATTR_MSG.
const nlmsgerrAttrMsg = 1

// cbRun is mnl_cb_run2 with mnlu_cb_array (lib/mnl_utils.c) over one
// reply, until a callback stops it. It returns nil for a reply ended by
// NLMSG_DONE or an ACK, or a recvError holding the errno rdma's perror
// would print: 0 when a data callback failed, which leaves errno alone.
func (r *rd) cbRun(rep Reply, cb func(Msg) error) error {
	for _, m := range rep.Msgs {
		if done, err := r.cbOne(m, rep.Seq, rep.PortID, cb); done || err != nil {
			return err
		}
	}
	if rep.Errno != 0 {
		return recvError{rep.Errno}
	}
	// A live reply always ends; a recorded one that does not is a capture
	// cut short, which rdma would have waited out.
	return fmt.Errorf("%w: a reply ends without NLMSG_DONE or an ACK", ErrReplay)
}

// cbOne is mnl_cb_run2's step for one message: done when it ends the
// reply, an error when it fails it.
func (r *rd) cbOne(m Msg, seq, portID uint32, cb func(Msg) error) (bool, error) {
	switch {
	case m.PID != 0 && portID != 0 && m.PID != portID:
		return true, recvError{unix.ESRCH}
	case m.Seq != 0 && seq != 0 && m.Seq != seq:
		return true, recvError{unix.EPROTO}
	case m.Flags&unix.NLM_F_DUMP_INTR != 0:
		return true, recvError{unix.EINTR}
	}
	switch m.Type {
	case unix.NLMSG_NOOP, unix.NLMSG_OVERRUN:
	case unix.NLMSG_ERROR:
		return true, r.cbError(m)
	case unix.NLMSG_DONE:
		return true, r.cbDone(m)
	default:
		if m.Type < unix.NLMSG_MIN_TYPE {
			return false, nil
		}
		if err := cb(m); err != nil {
			if errors.Is(err, errCB) {
				return true, recvError{0}
			}
			return true, err
		}
	}
	return false, nil
}

// recvError is mnlu_socket_recv_run's -1 and the errno it leaves.
type recvError struct{ errno unix.Errno }

func (e recvError) Error() string { return "error: " + strerror(e.errno) }

// cbError is mnlu_cb_error: an ACK stops the reply, an errno fails it, and
// an extack message is printed either way.
func (r *rd) cbError(m Msg) error {
	if len(m.Body) < nlmsgerrLen {
		return nil
	}
	e, err := xtcpnl.ParseNlMsgErr(m.Flags, m.Body)
	if err != nil {
		e = xtcpnl.NlMsgErr{Errno: int32(binary.LittleEndian.Uint32(m.Body))} //nolint:gosec // the wire int
	}
	errno := e.Errno
	if errno < 0 {
		errno = -errno
	}
	if m.Flags&unix.NLM_F_ACK_TLVS != 0 && e.HasMsg && e.Msg != "" {
		r.extack(e.Errno != 0, e.Msg)
	}
	if e.Errno == 0 {
		return nil
	}
	return recvError{unix.Errno(errno)}
}

// cbDone is mnlu_cb_stop: a negative int after NLMSG_DONE fails the dump,
// with nl_dump_ext_ack_done's message.
func (r *rd) cbDone(m Msg) error {
	if len(m.Body) < 4 {
		return nil
	}
	n := int32(binary.LittleEndian.Uint32(m.Body)) //nolint:gosec // the wire int
	if n >= 0 {
		return nil
	}
	if attrs, err := xtcpnl.ParseNetlinkAttributes(m.Body[4:]); err == nil {
		for _, a := range attrs {
			if a.ID() == nlmsgerrAttrMsg {
				if s := strings.TrimRight(string(a.Data), "\x00"); s != "" {
					r.extack(true, s)
				}
			}
		}
	}
	return recvError{unix.Errno(-n)}
}

// extack is print_ext_ack_msg (lib/libnetlink.c).
func (r *rd) extack(isErr bool, msg string) {
	kind := "Warning"
	if isErr {
		kind = "Error"
	}
	if !strings.HasSuffix(msg, ".") {
		msg += "."
	}
	r.errf("%s: %s\n", kind, msg)
}

// recvMsg is rd_recv_msg: cbRun, and perror("error") on a failure unless
// errors are suppressed, as an id filter's doit suppresses them.
func (r *rd) recvMsg(rep Reply, cb func(Msg) error) error {
	err := r.cbRun(rep, cb)
	var re recvError
	if errors.As(err, &re) {
		if !r.suppressErrors {
			r.errf("%s\n", re.Error())
		}
		return errFail
	}
	return err
}

// strerror is glibc's strerror for errno: x/sys/unix's strings, which are
// glibc's with the first letter lowered, raised again.
func strerror(errno unix.Errno) string {
	if errno == 0 {
		return "Success"
	}
	s := errno.Error()
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
