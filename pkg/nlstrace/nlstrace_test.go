package nlstrace

import (
	"bytes"
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
		sock int
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
		{description: "positive: a call split by another thread is joined back by pid", in: sock +
			"678   sendto(3, {nlmsg_len=16, ...}, 16, 0, {sa_family=AF_NETLINK}, 12 <unfinished ...>\n" +
			"679   recvfrom(5, 0x1, 4096, 0, 0x2, [112]) = -1 EAGAIN (Resource temporarily unavailable)\n" +
			"678   <... sendto resumed>) = 16\n" +
			" | 00000  10 00 00 00 01 14 05 03  6a 68 cc 6a 00 00 00 00  ........jh.j.... |\n",
			expected: []dg{{send: true, fd: 3, hex: "1000000001140503" + "6a68cc6a00000000"}}},
		{description: "positive: a split socket call still makes an RDMA socket",
			in: "678   socket(AF_NETLINK, SOCK_RAW|SOCK_CLOEXEC, NETLINK_RDMA <unfinished ...>\n" +
				"678   <... socket resumed>) = 3\n" + send16,
			expected: []dg{{send: true, fd: 3, hex: "1000000001140503" + "6a68cc6a00000000"}}},
		{description: "negative: a resumed call with no unfinished half is skipped", in: sock +
			"678   <... sendto resumed>) = 16\n" +
			" | 00000  10 00 00 00 01 14 05 03  6a 68 cc 6a 00 00 00 00  ........jh.j.... |\n"},
		{description: "negative: an RDMA fd reused by another protocol's socket is no longer RDMA",
			in: sock + "678   socket(AF_NETLINK, SOCK_RAW, NETLINK_ROUTE) = 3\n" + send16},
		{description: "boundary: a fd reused by a second RDMA socket is a new Sock",
			in:       sock + send16 + sock + send16,
			expected: []dg{{send: true, fd: 3, hex: "1000000001140503" + "6a68cc6a00000000"}, {send: true, fd: 3, sock: 1, hex: "1000000001140503" + "6a68cc6a00000000"}}},
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
				if g.Send != w.send || g.Fd != w.fd || g.Sock != w.sock || hex.EncodeToString(g.B) != w.hex {
					t.Errorf("datagram %d: got {%v %d %d %x}, want %+v", i, g.Send, g.Fd, g.Sock, g.B, w)
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

func TestTransactions(t *testing.T) {
	// One socket per fd, unless a row says otherwise with sendOn/recvOn.
	send := func(fd int, b string) Datagram { return Datagram{Send: true, Fd: fd, Sock: fd, B: []byte(b)} }
	recv := func(fd int, b string) Datagram { return Datagram{Fd: fd, Sock: fd, B: []byte(b)} }
	recvOn := func(fd, sock int, b string) Datagram { return Datagram{Fd: fd, Sock: sock, B: []byte(b)} }
	// show renders txns as "fd:req>recv,recv" and events as "ev:b".
	show := func(txns []Txn, events [][]byte) string {
		var s []string
		for _, tx := range txns {
			var r []string
			for _, b := range tx.Recvs {
				r = append(r, string(b))
			}
			s = append(s, fmt.Sprintf("%d:%s>%s", tx.Fd, tx.Req, strings.Join(r, ",")))
		}
		for _, e := range events {
			s = append(s, "ev:"+string(e))
		}
		return strings.Join(s, " ")
	}
	tests := []struct {
		description string
		in          []Datagram
		expected    string
	}{
		{description: "positive: one dump on one fd", in: []Datagram{send(3, "q"), recv(3, "a"), recv(3, "b"), recv(3, "done")},
			expected: "3:q>a,b,done"},
		{description: "positive: one socket per request", in: []Datagram{send(3, "q1"), recv(3, "a1"), send(4, "q2"), recv(4, "a2")},
			expected: "3:q1>a1 4:q2>a2"},
		{description: "positive: the monitor's events are reads on a socket that never sent",
			in: []Datagram{send(3, "init"), recv(3, "dev"), recv(4, "e1"), recv(4, "e2")}, expected: "3:init>dev ev:e1 ev:e2"},
		{description: "negative: a read before any send is an event", in: []Datagram{recv(5, "x")}, expected: "ev:x"},
		{description: "boundary: no datagrams", in: nil, expected: ""},
		{description: "boundary: a send with no reply", in: []Datagram{send(3, "q")}, expected: "3:q>"},
		{description: "corner: a fd reused for a second request", in: []Datagram{send(3, "q1"), recv(3, "a1"), send(3, "q2"), recv(3, "a2")},
			expected: "3:q1>a1 3:q2>a2"},
		{description: "corner: a fd closed and reused by a new socket starts afresh: the monitor's socket after a request's",
			in:       []Datagram{send(4, "init"), recv(4, "dev"), recvOn(4, 9, "e1"), recvOn(4, 9, "e2")},
			expected: "4:init>dev ev:e1 ev:e2"},
		{description: "corner: interleaved fds keep their own replies", in: []Datagram{send(3, "q1"), send(4, "q2"), recv(3, "a1"), recv(4, "a2")},
			expected: "3:q1>a1 4:q2>a2"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := show(Transactions(tc.in)); got != tc.expected {
				t.Fatalf("got %q, want %q", got, tc.expected)
			}
		})
	}
	t.Run("corner: every 7.1.4 capture: each send is a transaction and no read is lost", func(t *testing.T) {
		captures, err := filepath.Glob("../xtcpnl/testdata/7_1_4/rdma/*.strace")
		if err != nil || len(captures) == 0 {
			t.Fatalf("%d captures, %v", len(captures), err)
		}
		for _, c := range captures {
			dgs, err := ParseFile(c)
			if err != nil {
				t.Fatal(err)
			}
			sends, reads := 0, 0
			for _, d := range dgs {
				if d.Send {
					sends++
				} else {
					reads++
				}
			}
			txns, events := Transactions(dgs)
			got := len(events)
			for _, tx := range txns {
				got += len(tx.Recvs)
			}
			if len(txns) != sends || got != reads {
				t.Errorf("%s: %d txns of %d sends, %d reads of %d", filepath.Base(c), len(txns), sends, got, reads)
			}
		}
	})
}

func TestZeroSeqPID(t *testing.T) {
	hdr := []byte{20, 0, 0, 0, 1, 0x14, 5, 3, 7, 0, 0, 0, 9, 0, 0, 0, 0xaa, 0xbb, 0xcc, 0xdd}
	tests := []struct {
		description string
		in          []byte
		expected    []byte
	}{
		{description: "positive: seq and pid cleared, the rest kept", in: hdr,
			expected: []byte{20, 0, 0, 0, 1, 0x14, 5, 3, 0, 0, 0, 0, 0, 0, 0, 0, 0xaa, 0xbb, 0xcc, 0xdd}},
		{description: "boundary: exactly a header", in: hdr[:16], expected: []byte{20, 0, 0, 0, 1, 0x14, 5, 3, 0, 0, 0, 0, 0, 0, 0, 0}},
		{description: "corner: shorter than a header is unchanged", in: hdr[:10], expected: hdr[:10]},
		{description: "corner: nil", in: nil, expected: []byte{}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := ZeroSeqPID(tc.in); !bytes.Equal(got, tc.expected) {
				t.Fatalf("got % x, want % x", got, tc.expected)
			}
		})
	}
	t.Run("negative: the input is not modified", func(t *testing.T) {
		in := append([]byte(nil), hdr...)
		ZeroSeqPID(in)
		if !bytes.Equal(in, hdr) {
			t.Fatalf("input changed to % x", in)
		}
	})
}

// TestGordmaCaptures reads two captures of gordma itself from a live
// gordma-parity run on 7.1.4. A Go program's threads make strace split calls
// ("<unfinished ...>", then "<... resumed>"), and gordma closes each socket,
// so the monitor's socket reuses the descriptor of the request before it.
func TestGordmaCaptures(t *testing.T) {
	tests := []struct {
		description string
		path        string
		expected    string // "<txns> txns <events> events: <request types>"
	}{
		{description: "positive: a dump whose sendto strace split is still two requests",
			path: "testdata/gordma_res_mr_split_call.strace", expected: "2 txns 0 events: 0x1401 0x140d"},
		{description: "corner: the monitor's events on a reused fd are events, not replies",
			path: "testdata/gordma_monitor_fd_reuse.strace", expected: "2 txns 3 events: 0x1401 0x1406"},
		{description: "negative: rdma's own capture of the same row is unchanged",
			path: "../xtcpnl/testdata/7_1_4/rdma/res_mr.strace", expected: "2 txns 0 events: 0x1401 0x140d"},
		{description: "boundary: rdma's monitor, whose sockets stay open, groups the same",
			path: "../xtcpnl/testdata/7_1_4/rdma/monitor_add.strace", expected: "2 txns 3 events: 0x1401 0x1406"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			dgs, err := ParseFile(tc.path)
			if err != nil {
				t.Fatal(err)
			}
			txns, events := Transactions(dgs)
			var types []string
			for _, tx := range txns {
				if len(tx.Recvs) == 0 {
					t.Errorf("transaction on fd %d has no reply", tx.Fd)
				}
				types = append(types, fmt.Sprintf("%#x", uint16(tx.Req[4])|uint16(tx.Req[5])<<8))
			}
			got := fmt.Sprintf("%d txns %d events: %s", len(txns), len(events), strings.Join(types, " "))
			if got != tc.expected {
				t.Fatalf("got %q, want %q", got, tc.expected)
			}
		})
	}
}
