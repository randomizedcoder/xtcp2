// Package nlmon is the monitor core the iproute2 clones share (goip's
// `ip monitor` and its xfrm, genl and mptcp siblings, gotc's `tc monitor`):
// lib/libnetlink.c's rtnl_listen and rtnl_from_file, lib/utils.c's
// print_timestamp, and the split of an nlmon capture into what a subscriber
// was sent.
//
// Errors carry the tool's own text, so a clone prints err.Error() as the C
// prints its message, and a Kind sentinel a clone maps to its exit status.
package nlmon

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// RecvBufCst is rtnl_listen's and rtnl_from_file's `char buf[16384]`.
const RecvBufCst = 16384

// cmsgBufCst is room for rtnl_listen's one control message, an int.
const cmsgBufCst = 64

// Msg is one message a monitor's accept function is handed: its header,
// its body past the header, and the netns id a NETLINK_LISTEN_ALL_NSID
// socket names it with, -1 for none. Off is where it starts in its
// datagram, so a replay can tell what a receive buffer would have cut.
type Msg struct {
	Hdr  xtcpnl.NlMsgHdr
	Body []byte
	Nsid int32
	Off  int
}

// Kinds of Error, for a clone's exit status.
var (
	// ErrOpen is rtnl_from_file's caller failing to fopen the file.
	ErrOpen = errors.New("nlmon: open")
	// ErrFileTruncated is rtnl_from_file's short read: -1.
	ErrFileTruncated = errors.New("nlmon: file truncated")
	// ErrMalformed is a length under the header's or past the data: in
	// rtnl_listen exit(1), in rtnl_from_file -1.
	ErrMalformed = errors.New("nlmon: malformed")
	// ErrTruncated is rtnl_listen's MSG_TRUNC on a message cut short: -1.
	ErrTruncated = errors.New("nlmon: truncated")
	// ErrRemnant is rtnl_listen's bytes left past the last message: exit(1).
	ErrRemnant = errors.New("nlmon: remnant")
	// ErrRecv is rtnl_listen's receive error or EOF: -1.
	ErrRecv = errors.New("nlmon: receive")
)

// Error is a failure with the C's message as its text.
type Error struct {
	Kind error
	Text string
}

// Error is the C's message.
func (e *Error) Error() string { return e.Text }

// Unwrap is the kind.
func (e *Error) Unwrap() error { return e.Kind }

func errorf(kind error, format string, a ...any) error {
	return &Error{Kind: kind, Text: fmt.Sprintf(format, a...)}
}

// align is NLMSG_ALIGN.
func align(n int) int { return (n + 3) &^ 3 }

// Datagram is rtnl_listen's walk of one datagram: each message to fn, in
// order, until fn fails. A length under the header's or past the datagram
// is ErrTruncated when the datagram was cut short (MSG_TRUNC), else
// ErrMalformed. A truncated datagram whose messages all fit ends with nil
// and no remnant check, as rtnl_listen's `continue` skips it; the caller
// prints "Message truncated". Otherwise bytes left over are ErrRemnant.
func Datagram(b []byte, truncated bool, nsid int32, fn func(Msg) error) error {
	off := 0
	for len(b) >= xtcpnl.NlMsgHdrSizeCst {
		var h xtcpnl.NlMsgHdr
		if _, err := xtcpnl.DeserializeNlMsgHdr(b, &h); err != nil {
			return err
		}
		if h.Len < xtcpnl.NlMsgHdrSizeCst || int(h.Len) > len(b) {
			if truncated {
				return errorf(ErrTruncated, "Truncated message")
			}
			return errorf(ErrMalformed, "!!!malformed message: len=%d", int32(h.Len))
		}
		if err := fn(Msg{Hdr: h, Body: b[xtcpnl.NlMsgHdrSizeCst:h.Len], Nsid: nsid, Off: off}); err != nil {
			return err
		}
		next := align(int(h.Len))
		if next > len(b) {
			return nil
		}
		b = b[next:]
		off += next
	}
	if truncated {
		return nil
	}
	if len(b) != 0 {
		return errorf(ErrRemnant, "!!!Remnant of size %d", len(b))
	}
	return nil
}

// ListenOpts is what differs between rtnl_listen's callers.
type ListenOpts struct {
	// BufSize is the receive buffer; 0 is RecvBufCst.
	BufSize int
	// AllNsid reads the netns id from each datagram's control message.
	AllNsid bool
	// ErrOut takes the messages rtnl_listen prints and carries on after.
	ErrOut io.Writer
}

// Listen is rtnl_listen on fd: EINTR and EAGAIN are retried; ENOBUFS, the
// kernel's word for messages dropped on a full queue, is reported and
// retried; any other receive error, or EOF, ends it with ErrRecv.
func Listen(fd int, o ListenOpts, fn func(Msg) error) error {
	size := o.BufSize
	if size == 0 {
		size = RecvBufCst
	}
	buf := make([]byte, size)
	oob := make([]byte, cmsgBufCst)
	for {
		n, oobn, flags, _, err := unix.Recvmsg(fd, buf, oob, 0)
		switch {
		case errors.Is(err, unix.EINTR), errors.Is(err, unix.EAGAIN):
			continue
		case errors.Is(err, unix.ENOBUFS):
			if _, werr := fmt.Fprintf(o.ErrOut, "netlink receive error %s (%d)\n", Strerror(unix.ENOBUFS), int(unix.ENOBUFS)); werr != nil {
				return werr
			}
			continue
		case err != nil:
			var errno unix.Errno
			if errors.As(err, &errno) {
				return errorf(ErrRecv, "netlink receive error %s (%d)", Strerror(errno), int(errno))
			}
			return fmt.Errorf("netlink receive error %w", err)
		case n == 0:
			return errorf(ErrRecv, "EOF on netlink")
		}
		nsid := int32(-1)
		if o.AllNsid {
			nsid = ListenNsid(oob[:oobn])
		}
		truncated := flags&unix.MSG_TRUNC != 0
		if err := Datagram(buf[:min(n, len(buf))], truncated, nsid, fn); err != nil {
			return err
		}
		if truncated {
			if _, werr := io.WriteString(o.ErrOut, "Message truncated\n"); werr != nil {
				return werr
			}
		}
	}
}

// Strerror is glibc's strerror: x/sys/unix's strings, which are glibc's
// with the first letter lowered, raised again, and glibc's "Unknown error
// N" for one it has no text for.
func Strerror(errno unix.Errno) string {
	if errno == 0 {
		return "Success"
	}
	s := errno.Error()
	if strings.HasPrefix(s, "errno ") {
		return "Unknown error " + strings.TrimPrefix(s, "errno ")
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// ListenNsid is the netns id a NETLINK_LISTEN_ALL_NSID control message
// carries, the last one if more came, or -1.
func ListenNsid(oob []byte) int32 {
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

// ReadFile is the fopen and rtnl_from_file over path: an fopen failure is
// ErrOpen with perror's "Cannot fopen: <strerror>".
func ReadFile(path string, fn func(Msg) error) error {
	data, err := os.ReadFile(path)
	if err != nil {
		var errno unix.Errno
		if errors.As(err, &errno) {
			return errorf(ErrOpen, "Cannot fopen: %s", Strerror(errno))
		}
		return &Error{Kind: ErrOpen, Text: "Cannot fopen: " + err.Error()}
	}
	return WalkFile(data, fn)
}

// WalkFile is rtnl_from_file over data: each header, then its body to the
// 4-byte boundary, until the data ends between messages. A header or body
// cut short is ErrFileTruncated, a length under the header's or over the
// buffer ErrMalformed naming the offset past the header; fn's error ends it
// as it is.
func WalkFile(data []byte, fn func(Msg) error) error {
	off := 0
	for off < len(data) {
		if len(data)-off < xtcpnl.NlMsgHdrSizeCst {
			return errorf(ErrFileTruncated, "rtnl-from_file: truncated message")
		}
		var h xtcpnl.NlMsgHdr
		if _, err := xtcpnl.DeserializeNlMsgHdr(data[off:], &h); err != nil {
			return err
		}
		if h.Len < xtcpnl.NlMsgHdrSizeCst || h.Len > RecvBufCst {
			return errorf(ErrMalformed, "!!!malformed message: len=%d @%d", int32(h.Len), off+xtcpnl.NlMsgHdrSizeCst)
		}
		l := int(h.Len) - xtcpnl.NlMsgHdrSizeCst
		end := off + xtcpnl.NlMsgHdrSizeCst + align(l)
		if end > len(data) {
			return errorf(ErrFileTruncated, "rtnl-from_file: truncated message")
		}
		body := data[off+xtcpnl.NlMsgHdrSizeCst : off+xtcpnl.NlMsgHdrSizeCst+l]
		if err := fn(Msg{Hdr: h, Body: body, Nsid: -1}); err != nil {
			return err
		}
		off = end
	}
	return nil
}

// asctimeLayoutCst is asctime's "%.3s %.3s%3d %.2d:%.2d:%.2d %d".
const asctimeLayoutCst = "Mon Jan _2 15:04:05 2006"

// Timestamp is print_timestamp's text: asctime of the local time and the
// microseconds on a line of their own, or under -tshort the ISO form in
// brackets ahead of what follows.
func Timestamp(t time.Time, short bool) string {
	t = t.Local()
	usec := t.Nanosecond() / 1000
	if short {
		return fmt.Sprintf("[%s.%06d] ", t.Format("2006-01-02T15:04:05"), usec)
	}
	return fmt.Sprintf("Timestamp: %s %d usec\n", t.Format(asctimeLayoutCst), usec)
}

// NowFunc is the clock a monitor stamps each message with: the instant the
// environment variable env holds (RFC3339) when it parses, for a replay,
// else the wall clock, read per message.
func NowFunc(env string) func() time.Time {
	if t, err := time.Parse(time.RFC3339, os.Getenv(env)); err == nil {
		return func() time.Time { return t }
	}
	return time.Now
}
