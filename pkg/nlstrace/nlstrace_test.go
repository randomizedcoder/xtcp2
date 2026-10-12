package nlstrace

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
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
			expectedErr: ErrShort},
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
			got, err := Parse(strings.NewReader(tc.in))
			if !errors.Is(err, tc.expectedErr) {
				t.Fatalf("err %v, want %v", err, tc.expectedErr)
			}
			if len(got) != len(tc.expected) {
				t.Fatalf("%d datagrams, want %d", len(got), len(tc.expected))
			}
			for i, w := range tc.expected {
				g := got[i]
				if g.Send != w.send || g.Fd != w.fd || hex.EncodeToString(g.B) != w.hex {
					t.Errorf("datagram %d: got {%v %d %x}, want %+v", i, g.Send, g.Fd, g.B, w)
				}
			}
		})
	}
}

func TestMsgs(t *testing.T) {
	hdr := func(l byte, typ byte) string {
		return string([]byte{l, 0, 0, 0, typ, 0x14, 0, 0}) + strings.Repeat("\x00", 8)
	}
	tests := []struct {
		description string
		in          []Datagram
		expected    []string // "send/fd/type/len" per message
		expectedErr error
	}{
		{description: "positive: one message per datagram",
			in:       []Datagram{{Send: true, Fd: 3, B: []byte(hdr(16, 1))}, {Fd: 3, B: []byte(hdr(16, 2))}},
			expected: []string{"true/3/0x1401/16", "false/3/0x1402/16"}},
		{description: "positive: two messages in one datagram, the first padded to four bytes",
			in:       []Datagram{{Fd: 4, B: []byte(hdr(17, 1) + "\x01\x00\x00\x00" + hdr(16, 2))}},
			expected: []string{"false/4/0x1401/17", "false/4/0x1402/16"}},
		{description: "negative: nlmsg_len past the datagram", in: []Datagram{{B: []byte(hdr(32, 1))}}, expectedErr: ErrMsgLen},
		{description: "negative: nlmsg_len shorter than the header", in: []Datagram{{B: []byte(hdr(8, 1))}}, expectedErr: ErrMsgLen},
		{description: "boundary: no datagrams", in: nil},
		{description: "corner: a trailer shorter than a header is dropped",
			in:       []Datagram{{B: []byte(hdr(16, 1) + "\x00\x00")}},
			expected: []string{"false/0/0x1401/16"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := Msgs(tc.in)
			if !errors.Is(err, tc.expectedErr) {
				t.Fatalf("err %v, want %v", err, tc.expectedErr)
			}
			var s []string
			for _, m := range got {
				s = append(s, fmt.Sprintf("%v/%d/%#x/%d", m.Send, m.Fd, m.Type, len(m.Raw)))
				if len(m.Body) != len(m.Raw)-nlMsgHdrLen {
					t.Errorf("body %d bytes of a %d-byte message", len(m.Body), len(m.Raw))
				}
			}
			if strings.Join(s, " ") != strings.Join(tc.expected, " ") {
				t.Fatalf("got %v, want %v", s, tc.expected)
			}
		})
	}
}

func TestParseFile(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.strace")
	if err := os.WriteFile(good, []byte("socket(AF_NETLINK, SOCK_RAW, NETLINK_RDMA) = 3\n"+
		"sendto(3, {...}, 16, 0, NULL, 0) = 16\n"+
		" | 00000  10 00 00 00 01 14 05 03  6a 68 cc 6a 00 00 00 00  ........jh.j.... |\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		description string
		path        string
		expected    int
		expectErr   bool
	}{
		{description: "positive: a file with one send", path: good, expected: 1},
		{description: "negative: a missing file", path: filepath.Join(dir, "nosuch"), expectErr: true},
		{description: "boundary: a directory is not a capture", path: dir, expectErr: true},
		{description: "corner: the committed 7.1.4 dev_show capture", path: "../xtcpnl/testdata/7_1_4/rdma/dev_show.strace", expected: 6},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := ParseFile(tc.path)
			if (err != nil) != tc.expectErr || len(got) != tc.expected {
				t.Fatalf("%d datagrams, %v; want %d, error %v", len(got), err, tc.expected, tc.expectErr)
			}
		})
	}
}
