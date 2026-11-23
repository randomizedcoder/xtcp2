package gotipc

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// This is tipc's generic netlink layer: lib/mnl_utils.c's
// mnlu_gen_socket_open (the family lookup), mnlu_gen_socket_sndrcv and the
// mnl_cb_run2 control callbacks, as govdpa ports them for vdpa, which uses
// the same library.

// errCBError is a data callback's MNL_CB_ERROR. Any other error a callback
// returns (a replay mismatch, tipc's crash) ends the run as it is.
var errCBError = errors.New("gotipc: MNL_CB_ERROR")

// dataCB is a libmnl data callback over one reply message.
type dataCB func(m Msg) error

// initFamily is mnlu_gen_socket_open's family_get: one CTRL_CMD_GETFAMILY
// (genl version 1, REQUEST|ACK), its reply read by get_family_cb, which
// needs CTRL_ATTR_FAMILY_ID. tipc runs it at start-up and again for each
// follow-up socket (socket.c publ_list, link.c link_mon_peer_list). It
// prints nothing on failure but an extack.
func (r *runner) initFamily() error {
	req, err := xtcpnl.BuildTIPCFamilyRequest(r.nextSeq())
	if err != nil {
		return err
	}
	rep, err := r.src.Talk(req)
	if err != nil {
		return err
	}
	var id uint16
	if err := r.runReply(rep, func(m Msg) error {
		f, perr := xtcpnl.ParseGenericNetlinkFamily(m.Body)
		if perr != nil || f.ID == 0 {
			return errCBError
		}
		id = f.ID
		return nil
	}); err != nil {
		return err
	}
	r.familyID = id
	return nil
}

// sndrcv is mnlu_gen_socket_sndrcv: send, run the reply through
// mnlu_socket_recv_run, and on failure print "kernel answers: %s".
func (r *runner) sndrcv(req []byte, cb dataCB) error {
	rep, err := r.src.Talk(req)
	if err != nil {
		return err
	}
	err = r.runReply(rep, cb)
	var ce *cbError
	if !errors.As(err, &ce) {
		return err
	}
	r.errf("kernel answers: %s\n", strerror(ce.errno))
	if ce.errno == 0 {
		// mnlu_gen_socket_sndrcv returns -errno: a callback's MNL_CB_ERROR
		// with errno 0 is reported, then succeeds (exit 0).
		return nil
	}
	return errFail
}

// cbError is a reply mnl_cb_run2 ended with MNL_CB_ERROR, and the errno
// tipc's strerror then reads.
type cbError struct{ errno unix.Errno }

func (e *cbError) Error() string { return "gotipc: MNL_CB_ERROR: " + strerror(e.errno) }

// runReply is mnl_cb_run2 over a reply with mnl_utils' control callbacks:
// data messages go to cb, NLMSG_ERROR and NLMSG_DONE end it (printing an
// extack the way nl_dump_ext_ack does), NOOP and OVERRUN are skipped.
func (r *runner) runReply(rep Reply, cb dataCB) error {
	for _, m := range rep.Msgs {
		if m.Flags&unix.NLM_F_DUMP_INTR != 0 {
			return &cbError{unix.EINTR}
		}
		if m.Type >= unix.NLMSG_MIN_TYPE {
			if cb == nil {
				continue
			}
			if err := cb(m); err != nil {
				if errors.Is(err, errCBError) {
					// A callback's MNL_CB_ERROR leaves errno as it was;
					// on tipc's read paths nothing has set it.
					return &cbError{0}
				}
				return err
			}
			continue
		}
		switch m.Type {
		case unix.NLMSG_ERROR:
			if errno := r.cbErr(m); errno != 0 {
				return &cbError{errno}
			}
			return nil
		case unix.NLMSG_DONE:
			if errno := r.cbStop(m); errno != 0 {
				return &cbError{errno}
			}
			return nil
		}
	}
	return fmt.Errorf("%w: the reply ends without NLMSG_DONE or NLMSG_ERROR", ErrReplay)
}

// cbErr is mnlu_cb_error: either way the reply ends. A payload shorter
// than struct nlmsgerr, or a zero error (after any warning extack), is
// MNL_CB_STOP; otherwise the errno, whichever sign the kernel gave it.
func (r *runner) cbErr(m Msg) unix.Errno {
	if len(m.Body) < 4+netlinkHeaderLen {
		return 0
	}
	e := int64(int32(binary.LittleEndian.Uint32(m.Body[0:4])))
	if e < 0 {
		e = -e
	}
	r.dumpExtAck(m, e != 0)
	return unix.Errno(e)
}

// cbStop is mnlu_cb_stop: NLMSG_DONE's int is the dump's result; a negative
// one is an error, whose extack (past the int) nl_dump_ext_ack_done prints.
func (r *runner) cbStop(m Msg) unix.Errno {
	if len(m.Body) < 4 {
		return 0
	}
	l := int32(binary.LittleEndian.Uint32(m.Body[0:4]))
	if l >= 0 {
		return 0
	}
	r.extAckAttrs(m.Body[4:], true)
	return unix.Errno(-int64(l))
}

// dumpExtAck is nl_dump_ext_ack with no errfn: only with NLM_F_ACK_TLVS, the
// TLVs past struct nlmsgerr (and past the echoed request unless
// NLM_F_CAPPED).
func (r *runner) dumpExtAck(m Msg, isErr bool) {
	if m.Flags&unix.NLM_F_ACK_TLVS == 0 {
		return
	}
	hlen := 4 + netlinkHeaderLen
	if m.Flags&unix.NLM_F_CAPPED == 0 {
		inner := int(binary.LittleEndian.Uint32(m.Body[4:8]))
		hlen += max(inner-netlinkHeaderLen, 0)
	}
	if hlen > len(m.Body) {
		return
	}
	r.extAckAttrs(m.Body[hlen:], isErr)
}

// NLMSGERR_ATTR_* (include/uapi/linux/netlink.h).
const (
	nlmsgerrAttrMsg      = 1
	nlmsgerrAttrOffs     = 2
	nlmsgerrAttrMissType = 5
	nlmsgerrAttrMax      = 6
)

// extAckAttrs parses extack TLVs as mnl_attr_parse with err_attr_cb does and
// prints what nl_dump_ext_ack prints: the missing-attribute line, then
// "Error: msg" (or "Warning: ") with a "." added when msg lacks one.
func (r *runner) extAckAttrs(b []byte, isErr bool) {
	var msg *string
	var missType *uint32
	for len(b) >= 4 {
		l := int(binary.LittleEndian.Uint16(b[0:2]))
		if l < 4 || l > len(b) {
			break // mnl_attr_ok ends the walk
		}
		typ := binary.LittleEndian.Uint16(b[2:4]) & 0x3fff
		p := b[4:l]
		if typ > nlmsgerrAttrMax {
			r.errf("Invalid extack attribute\n")
			return
		}
		switch typ {
		case nlmsgerrAttrMsg:
			if len(p) == 0 || p[len(p)-1] != 0 {
				r.errf("extack attribute %d failed validation\n", typ)
				return
			}
			s := string(p[:bytes.IndexByte(p, 0)]) // mnl_attr_get_str
			msg = &s
		case nlmsgerrAttrOffs:
			if len(p) != 4 {
				r.errf("extack attribute %d failed validation\n", typ)
				return
			}
		case nlmsgerrAttrMissType:
			if len(p) >= 4 {
				v := binary.LittleEndian.Uint32(p[0:4])
				missType = &v
			}
		}
		b = b[min(len(b), (l+3)&^3):]
	}
	if missType != nil {
		r.errf("Missing required attribute type %d\n", *missType)
	}
	if msg == nil || *msg == "" {
		return
	}
	label := "Warning"
	if isErr {
		label = "Error"
	}
	dot := ""
	if !strings.HasSuffix(*msg, ".") {
		dot = "."
	}
	r.errf("%s: %s%s\n", label, *msg, dot)
}

// strerror is glibc's strerror: x/sys/unix's strings, which are glibc's
// with the first letter lowered, raised again.
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
