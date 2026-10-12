// Package nlstrace reads netlink traffic back out of strace output.
//
// The RDMA corpus (pkg/xtcpnl/testdata/<kernel>/rdma) is strace output, not
// pcap, because nlmon cannot tap NETLINK_RDMA (netlink_filter_tap,
// net/netlink/af_netlink.c). xtcp2-nlcap runs `strace -f -e
// trace=socket,sendto,sendmsg,recvfrom,recvmsg -e write=all -e read=all`,
// which prints every syscall line followed by a hex dump of the bytes it
// moved. Parse turns that back into datagrams, and Msgs into netlink
// messages. pkg/xtcpnl's tests and gordma's replay source both read it, so it
// imports nothing first-party: xtcpnl's own tests could not import it
// otherwise.
package nlstrace

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// Datagram is the bytes one sendto/sendmsg sent or one recvfrom/recvmsg
// received on a NETLINK_RDMA socket, Fd being the socket's descriptor.
type Datagram struct {
	Send bool
	Fd   int
	B    []byte
}

var (
	socketRe = regexp.MustCompile(`^(?:\d+ +)?socket\(AF_NETLINK, [A-Z_|]+, NETLINK_RDMA\) = (\d+)$`)
	callRe   = regexp.MustCompile(`^(?:\d+ +)?(sendto|sendmsg|recvfrom|recvmsg)\((\d+), .* = (-?\d+|\?)(?: .*)?$`)
	hexRe    = regexp.MustCompile(`^ \| [0-9a-f]{5}  `)

	// ErrShort is returned when a call's hex dump is not as long as the
	// call's return value.
	ErrShort = errors.New("nlstrace: the hex dump does not add up to the return value")
	// ErrMsgLen is returned for an nlmsg_len that does not fit its datagram.
	ErrMsgLen = errors.New("nlstrace: nlmsg_len does not fit the datagram")
)

// hexStart and hexEnd are where a dump line's hex columns are: " | 00000  "
// and then sixteen "xx " groups with one extra space after the eighth.
const (
	hexStart = 10
	hexEnd   = hexStart + 16*3 + 1
	// nlMsgHdrLen is sizeof(struct nlmsghdr).
	nlMsgHdrLen = 16
)

// Parse returns the NETLINK_RDMA datagrams in r, in order. A call that
// failed, returned nothing, or peeked (MSG_PEEK) is skipped along with its
// dump, so each datagram is one the process actually consumed.
func Parse(r io.Reader) ([]Datagram, error) {
	rdmaFds := map[int]bool{}
	var out []Datagram
	var cur *Datagram
	want := 0
	flush := func() error {
		if cur != nil && len(cur.B) != want {
			return ErrShort
		}
		cur = nil
		return nil
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if hexRe.MatchString(line) {
			if cur == nil || len(line) < hexEnd {
				continue
			}
			b, err := hex.DecodeString(strings.ReplaceAll(line[hexStart:hexEnd], " ", ""))
			if err != nil {
				return nil, err
			}
			cur.B = append(cur.B, b...)
			continue
		}
		if strings.HasPrefix(line, " * ") {
			continue // "* N bytes in buffer K": one iov of the current call
		}
		if err := flush(); err != nil {
			return nil, err
		}
		if m := socketRe.FindStringSubmatch(line); m != nil {
			fd, err := strconv.Atoi(m[1])
			if err != nil {
				return nil, err
			}
			rdmaFds[fd] = true
			continue
		}
		m := callRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		fd, err := strconv.Atoi(m[2])
		if err != nil {
			return nil, err
		}
		n, err := strconv.Atoi(m[3])
		if err != nil || n <= 0 || !rdmaFds[fd] || strings.Contains(line, "MSG_PEEK") {
			continue
		}
		out = append(out, Datagram{Send: strings.HasPrefix(m[1], "send"), Fd: fd})
		cur, want = &out[len(out)-1], n
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return out, nil
}

// ParseFile is Parse of the file at path.
func ParseFile(path string) (dgs []Datagram, err error) {
	f, err := os.Open(path) //nolint:gosec // the caller names the capture to read
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, f.Close())
	}()
	return Parse(f)
}

// Msg is one netlink message of a datagram, its header fields decoded.
type Msg struct {
	Send  bool
	Fd    int
	Type  uint16
	Flags uint16
	Raw   []byte // the whole message, nlmsghdr included
	Body  []byte // Raw past the nlmsghdr
}

// Msgs splits datagrams into netlink messages, in order. A remainder shorter
// than an nlmsghdr ends its datagram, as it ends the kernel's own walk.
func Msgs(dgs []Datagram) ([]Msg, error) {
	var out []Msg
	for _, d := range dgs {
		b := d.B
		for len(b) >= nlMsgHdrLen {
			l := int(binary.LittleEndian.Uint32(b[0:4]))
			if l < nlMsgHdrLen || l > len(b) {
				return nil, ErrMsgLen
			}
			out = append(out, Msg{
				Send:  d.Send,
				Fd:    d.Fd,
				Type:  binary.LittleEndian.Uint16(b[4:6]),
				Flags: binary.LittleEndian.Uint16(b[6:8]),
				Raw:   b[:l],
				Body:  b[nlMsgHdrLen:l],
			})
			b = b[min(len(b), (l+3)&^3):]
		}
	}
	return out, nil
}
