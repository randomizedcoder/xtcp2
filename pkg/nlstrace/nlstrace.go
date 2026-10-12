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
// received on a NETLINK_RDMA socket. Fd is the socket's descriptor and Sock
// the socket itself, numbered in the order the capture opened them: a
// process that closes a socket gets its descriptor back for the next one
// (gordma does; rdma keeps its sockets open), so only Sock tells the two
// apart.
type Datagram struct {
	Send bool
	Fd   int
	Sock int
	B    []byte
}

var (
	// callRe is a whole call: pid (with -f), name, arguments, return value.
	callRe = regexp.MustCompile(`^(?:(\d+) +)?(socket|sendto|sendmsg|recvfrom|recvmsg)\((.*) = (-?\d+|\?)(?: .*)?$`)
	// unfinishedRe and resumedRe are a call strace split in two because
	// another traced thread made a call in between (-f; a Go program's
	// threads do).
	unfinishedRe = regexp.MustCompile(`^(?:(\d+) +)?(socket|sendto|sendmsg|recvfrom|recvmsg)\((.*) <unfinished \.\.\.>$`)
	resumedRe    = regexp.MustCompile(`^(?:(\d+) +)?<\.\.\. (socket|sendto|sendmsg|recvfrom|recvmsg) resumed>(.*) = (-?\d+|\?)(?: .*)?$`)
	fdRe         = regexp.MustCompile(`^(\d+), `)
	hexRe        = regexp.MustCompile(`^ \| [0-9a-f]{5}  `)

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
// dump, so each datagram is one the process actually consumed. A call strace
// split into "<unfinished ...>" and "<... resumed>" is joined back, by pid.
func Parse(r io.Reader) ([]Datagram, error) {
	rdmaSocks := map[int]int{} // fd -> Sock, for the fds that are RDMA sockets now
	socks := 0
	pending := map[string]string{} // pid -> the unfinished call's "name(args"
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
		var call, args, ret string
		if m := unfinishedRe.FindStringSubmatch(line); m != nil {
			pending[m[1]] = m[2] + "(" + m[3]
			continue
		} else if m := resumedRe.FindStringSubmatch(line); m != nil {
			head, ok := pending[m[1]]
			delete(pending, m[1])
			if !ok || !strings.HasPrefix(head, m[2]+"(") {
				continue
			}
			call, args, ret = m[2], strings.TrimPrefix(head, m[2]+"(")+m[3], m[4]
		} else if m := callRe.FindStringSubmatch(line); m != nil {
			call, args, ret = m[2], m[3], m[4]
		} else {
			continue
		}
		n, err := strconv.Atoi(ret)
		if err != nil || n < 0 {
			continue
		}
		if call == "socket" {
			if strings.HasPrefix(args, "AF_NETLINK, ") && strings.HasSuffix(args, ", NETLINK_RDMA)") {
				rdmaSocks[n] = socks
				socks++
			} else {
				delete(rdmaSocks, n)
			}
			continue
		}
		fm := fdRe.FindStringSubmatch(args)
		if fm == nil {
			continue
		}
		fd, err := strconv.Atoi(fm[1])
		if err != nil {
			return nil, err
		}
		sock, ok := rdmaSocks[fd]
		if n == 0 || !ok || strings.Contains(args, "MSG_PEEK") {
			continue
		}
		out = append(out, Datagram{Send: strings.HasPrefix(call, "send"), Fd: fd, Sock: sock})
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

// Txn is one request and every datagram read back on its socket until the
// socket's next request: what rdma's one-socket-per-request rd_send_msg
// (rdma/utils.c) sent and received.
type Txn struct {
	Fd    int
	Req   []byte
	Recvs [][]byte
}

// Transactions groups datagrams by socket. A send opens a transaction on its
// socket, and each read on that socket joins the socket's latest
// transaction. Reads on a socket that never sent are events, `rdma
// monitor`'s stream, returned in order.
func Transactions(dgs []Datagram) (txns []Txn, events [][]byte) {
	open := map[int]int{} // Sock -> index of its latest transaction
	for _, d := range dgs {
		if d.Send {
			open[d.Sock] = len(txns)
			txns = append(txns, Txn{Fd: d.Fd, Req: d.B})
			continue
		}
		if i, ok := open[d.Sock]; ok {
			txns[i].Recvs = append(txns[i].Recvs, d.B)
			continue
		}
		events = append(events, d.B)
	}
	return txns, events
}

// ZeroSeqPID is a copy of b with nlmsg_seq and nlmsg_pid zeroed, the two
// header fields that differ between two runs of the same request. A b
// shorter than an nlmsghdr is returned as a copy, unchanged.
func ZeroSeqPID(b []byte) []byte {
	out := append([]byte(nil), b...)
	if len(out) >= nlMsgHdrLen {
		clear(out[8:16])
	}
	return out
}
