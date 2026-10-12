package xtcpnl

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
	"testing"
)

// The RDMA corpus (testdata/7_1_4/rdma) is strace output, not pcap, because
// nlmon cannot tap NETLINK_RDMA (netlink_filter_tap, net/netlink/af_netlink.c).
// xtcp2-nlcap runs `strace -f -e trace=socket,sendto,sendmsg,recvfrom,recvmsg
// -e write=all -e read=all`, which prints every syscall line followed by a hex
// dump of the bytes it moved. parseStrace turns that back into datagrams.

// straceDatagram is the bytes one sendto/sendmsg sent or one
// recvfrom/recvmsg received on a NETLINK_RDMA socket.
type straceDatagram struct {
	send bool
	fd   int
	b    []byte
}

var (
	straceSocketRe = regexp.MustCompile(`^(?:\d+ +)?socket\(AF_NETLINK, [A-Z_|]+, NETLINK_RDMA\) = (\d+)$`)
	straceCallRe   = regexp.MustCompile(`^(?:\d+ +)?(sendto|sendmsg|recvfrom|recvmsg)\((\d+), .* = (-?\d+|\?)(?: .*)?$`)
	straceHexRe    = regexp.MustCompile(`^ \| [0-9a-f]{5}  `)

	errStraceShort = errors.New("strace: the hex dump does not add up to the return value")
)

// straceHexCols is where a dump line's hex columns are: " | 00000  " and
// then sixteen "xx " groups with one extra space after the eighth.
const (
	straceHexStart = 10
	straceHexEnd   = straceHexStart + 16*3 + 1
)

// parseStrace returns the NETLINK_RDMA datagrams in r, in order. A call that
// failed, returned nothing, or peeked (MSG_PEEK) is skipped along with its
// dump, so each datagram is one the process actually consumed.
func parseStrace(r io.Reader) ([]straceDatagram, error) {
	rdmaFds := map[int]bool{}
	var out []straceDatagram
	var cur *straceDatagram
	want := 0
	flush := func() error {
		if cur != nil && len(cur.b) != want {
			return errStraceShort
		}
		cur = nil
		return nil
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if straceHexRe.MatchString(line) {
			if cur == nil || len(line) < straceHexEnd {
				continue
			}
			b, err := hex.DecodeString(strings.ReplaceAll(line[straceHexStart:straceHexEnd], " ", ""))
			if err != nil {
				return nil, err
			}
			cur.b = append(cur.b, b...)
			continue
		}
		if strings.HasPrefix(line, " * ") {
			continue // "* N bytes in buffer K": one iov of the current call
		}
		if err := flush(); err != nil {
			return nil, err
		}
		if m := straceSocketRe.FindStringSubmatch(line); m != nil {
			fd, _ := strconv.Atoi(m[1])
			rdmaFds[fd] = true
			continue
		}
		m := straceCallRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		fd, _ := strconv.Atoi(m[2])
		n, err := strconv.Atoi(m[3])
		if err != nil || n <= 0 || !rdmaFds[fd] || strings.Contains(line, "MSG_PEEK") {
			continue
		}
		out = append(out, straceDatagram{send: strings.HasPrefix(m[1], "send"), fd: fd})
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

// straceNlMsg is one netlink message of a datagram, header fields decoded.
type straceNlMsg struct {
	send  bool
	typ   uint16
	flags uint16
	raw   []byte // the whole message, nlmsghdr included
	body  []byte // raw past the nlmsghdr
}

// straceNlMsgs splits datagrams into netlink messages, in order.
func straceNlMsgs(t testing.TB, dgs []straceDatagram) []straceNlMsg {
	t.Helper()
	var out []straceNlMsg
	for _, d := range dgs {
		b := d.b
		for len(b) >= NlMsgHdrSizeCst {
			l := int(binary.LittleEndian.Uint32(b[0:4]))
			if l < NlMsgHdrSizeCst || l > len(b) {
				t.Fatalf("nlmsg_len %d in a %d-byte remainder", l, len(b))
			}
			out = append(out, straceNlMsg{
				send:  d.send,
				typ:   binary.LittleEndian.Uint16(b[4:6]),
				flags: binary.LittleEndian.Uint16(b[6:8]),
				raw:   b[:l],
				body:  b[NlMsgHdrSizeCst:l],
			})
			b = b[min(len(b), l+FourByteAlignPadding(l)):]
		}
	}
	return out
}

// rdmaCorpus reads testdata/7_1_4/rdma/<slug>.strace.
func rdmaCorpus(t testing.TB, slug string) []straceNlMsg {
	t.Helper()
	f, err := os.Open(tdRdma_7_1_4 + "/" + slug + ".strace")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	}()
	dgs, err := parseStrace(f)
	if err != nil {
		t.Fatal(err)
	}
	return straceNlMsgs(t, dgs)
}

func TestParseStrace(t *testing.T) {
	const sock = "678   socket(AF_NETLINK, SOCK_RAW, NETLINK_RDMA) = 3\n"
	const send16 = "678   sendto(3, {nlmsg_len=16, ...}, 16, 0, {sa_family=AF_NETLINK}, 12) = 16\n" +
		" | 00000  10 00 00 00 01 14 05 03  6a 68 cc 6a 00 00 00 00  ........jh.j.... |\n"
	type dg struct {
		send bool
		fd   int
		hex  string
	}
	tests := []struct {
		description string
		in          string
		expected    []dg
		expectedErr error
	}{
		{description: "positive: a send on an RDMA socket", in: sock + send16,
			expected: []dg{{send: true, fd: 3, hex: "1000000001140503" + "6a68cc6a00000000"}}},
		{description: "positive: a recvmsg with its iov line and a short last row", in: sock +
			"678   recvmsg(3, {msg_iov=[{iov_base=...}]}, 0) = 20\n" +
			" * 20 bytes in buffer 0\n" +
			" | 00000  14 00 00 00 03 00 02 00  6a 68 cc 6a de 02 00 00  ........jh.j.... |\n" +
			" | 00010  00 00 00 00                                       ....             |\n",
			expected: []dg{{fd: 3, hex: "1400000003000200" + "6a68cc6ade020000" + "00000000"}}},
		{description: "positive: without -f there is no pid prefix", in: "socket(AF_NETLINK, SOCK_RAW, NETLINK_RDMA) = 3\n" +
			"sendto(3, {...}, 16, 0, NULL, 0) = 16\n" +
			" | 00000  10 00 00 00 01 14 05 03  6a 68 cc 6a 00 00 00 00  ........jh.j.... |\n",
			expected: []dg{{send: true, fd: 3, hex: "1000000001140503" + "6a68cc6a00000000"}}},
		{description: "negative: a dump shorter than the return value", in: sock +
			"678   sendto(3, {...}, 32, 0, NULL, 0) = 32\n" +
			" | 00000  10 00 00 00 01 14 05 03  6a 68 cc 6a 00 00 00 00  ........jh.j.... |\n",
			expectedErr: errStraceShort},
		{description: "negative: a socket of another protocol is not RDMA",
			in: "678   socket(AF_NETLINK, SOCK_RAW, NETLINK_ROUTE) = 3\n" + send16},
		{description: "boundary: empty input", in: ""},
		{description: "corner: a failed call and its absent dump are skipped", in: sock +
			"678   recvmsg(3, {msg_namelen=12}, 0) = -1 EAGAIN (Resource temporarily unavailable)\n" + send16,
			expected: []dg{{send: true, fd: 3, hex: "1000000001140503" + "6a68cc6a00000000"}}},
		{description: "corner: an interrupted call returns ? and is skipped", in: sock +
			"678   recvmsg(3, {msg_namelen=12}, 0) = ? ERESTARTSYS (To be restarted if SA_RESTART is set)\n"},
		{description: "corner: a MSG_PEEK read is not consumed and is skipped", in: sock +
			"678   recvmsg(3, {msg_iov=[...]}, MSG_PEEK|MSG_TRUNC) = 16\n" +
			" | 00000  10 00 00 00 01 14 05 03  6a 68 cc 6a 00 00 00 00  ........jh.j.... |\n"},
		{description: "corner: a call cut off by SIGKILL is skipped, the send before it kept",
			in:       sock + send16 + "678   recvmsg(3",
			expected: []dg{{send: true, fd: 3, hex: "1000000001140503" + "6a68cc6a00000000"}}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := parseStrace(strings.NewReader(tc.in))
			if !errors.Is(err, tc.expectedErr) {
				t.Fatalf("err %v, want %v", err, tc.expectedErr)
			}
			if len(got) != len(tc.expected) {
				t.Fatalf("%d datagrams, want %d", len(got), len(tc.expected))
			}
			for i, w := range tc.expected {
				g := got[i]
				if g.send != w.send || g.fd != w.fd || hex.EncodeToString(g.b) != w.hex {
					t.Errorf("datagram %d: got {%v %d %x}, want %+v", i, g.send, g.fd, g.b, w)
				}
			}
		})
	}
}
