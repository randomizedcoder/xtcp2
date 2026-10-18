package linuxio

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

func chardevU64(v uint64) []byte { return binary.LittleEndian.AppendUint64(nil, v) }

// chardevBody is a GET_CHARDEV reply body in nldev_get_chardev's order.
func chardevBody(driverID int64, dev, abi uint64, name string) []byte {
	var b []byte
	if driverID >= 0 {
		b = append(b, attribute(xtcpnl.RdmaNldevAttrUverbsDriverID, word(int32(driverID)))...)
	}
	b = append(b, attribute(xtcpnl.RdmaNldevAttrChardev, chardevU64(dev))...)
	b = append(b, attribute(xtcpnl.RdmaNldevAttrChardevABI, chardevU64(abi))...)
	if name != "" {
		b = append(b, attribute(xtcpnl.RdmaNldevAttrChardevName, append([]byte(name), 0))...)
	}
	return b
}

// hugeEncodeDev is the kernel's huge_encode_dev.
func hugeEncodeDev(major, minor uint32) uint64 {
	return uint64(minor&0xff) | uint64(major)<<8 | uint64(minor&^0xff)<<12
}

// mlx5Uverbs1 is host l's mlx5_1 reply: driver 1, 231:193, ABI 1, uverbs1.
var mlx5Uverbs1 = chardevBody(1, hugeEncodeDev(231, 193), 1, "uverbs1")

func TestChardevTransaction(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expected string
		protocol                              int
		dev, port                             uint32
		kind                                  xtcpnl.ChardevKind
		reply                                 func(request []byte, seq uint32) []byte
		wantErr                               error
		wantName                              string
		wantSent                              int
	}{
		{"captured_uverbs", "positive", "host l mlx5_1 reply", "uverbs1, 231:193, driver 1, ABI 1",
			unix.NETLINK_RDMA, 1, 0, xtcpnl.ChardevUverbs,
			func(_ []byte, seq uint32) []byte { return frame(rdmaChardevGet, 0, seq, mlx5Uverbs1) },
			nil, "uverbs1", 1},
		{"umad_port", "positive", "umad reply for port 1", "umad0 with no driver id",
			unix.NETLINK_RDMA, 0, 1, xtcpnl.ChardevUmad,
			func(_ []byte, seq uint32) []byte {
				return frame(rdmaChardevGet, 0, seq, chardevBody(-1, hugeEncodeDev(231, 0), 5, "umad0"))
			}, nil, "umad0", 1},
		{"kernel_einval", "negative", "kernel rejects the request", "EINVAL from the error reply",
			unix.NETLINK_RDMA, 9, 0, xtcpnl.ChardevUverbs,
			func(request []byte, seq uint32) []byte {
				return frame(unix.NLMSG_ERROR, 0, seq, append(word(-int32(unix.EINVAL)), request...))
			}, unix.EINVAL, "", 1},
		{"duplicate_name", "negative", "CHARDEV_NAME twice", "ErrReply; admitted at most once",
			unix.NETLINK_RDMA, 1, 0, xtcpnl.ChardevUverbs,
			func(_ []byte, seq uint32) []byte {
				body := append(bytes.Clone(mlx5Uverbs1), attribute(xtcpnl.RdmaNldevAttrChardevName, []byte("uverbs2\x00"))...)
				return frame(rdmaChardevGet, 0, seq, body)
			}, ErrReply, "", 1},
		{"missing_name", "negative", "no CHARDEV_NAME", "ErrReply",
			unix.NETLINK_RDMA, 1, 0, xtcpnl.ChardevUverbs,
			func(_ []byte, seq uint32) []byte {
				return frame(rdmaChardevGet, 0, seq, chardevBody(1, hugeEncodeDev(231, 193), 1, ""))
			}, ErrReply, "", 1},
		{"wrong_type", "negative", "reply typed as a device GET", "ignored; no reply before the deadline",
			unix.NETLINK_RDMA, 1, 0, xtcpnl.ChardevUverbs,
			func(_ []byte, seq uint32) []byte { return frame(rdmaDeviceGet, 0, seq, mlx5Uverbs1) },
			context.DeadlineExceeded, "", 1},
		{"umad_port_zero", "boundary", "umad with port 0", "ErrRequest, nothing sent",
			unix.NETLINK_RDMA, 0, 0, xtcpnl.ChardevUmad, nil, ErrRequest, "", 0},
		{"kind_zero", "corner", "zero ChardevKind", "ErrRequest, nothing sent",
			unix.NETLINK_RDMA, 0, 0, 0, nil, ErrRequest, "", 0},
		{"route_client", "corner", "NETLINK_ROUTE client", "ErrRequest, nothing sent",
			unix.NETLINK_ROUTE, 0, 0, xtcpnl.ChardevUverbs, nil, ErrRequest, "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			socket := &scriptSocket{}
			socket.onSend = func(data []byte) {
				if typ, flags := binary.LittleEndian.Uint16(data[4:]), binary.LittleEndian.Uint16(data[6:]); typ != rdmaChardevGet ||
					flags != unix.NLM_F_REQUEST {
					t.Errorf("request type %#x flags %#x, want GET_CHARDEV, NLM_F_REQUEST", typ, flags)
				}
				socket.batches = []batch{{data: [][]byte{tc.reply(bytes.Clone(data), binary.LittleEndian.Uint32(data[8:]))}}}
			}
			c := scriptedClient(tc.protocol, socket)
			t.Cleanup(func() {
				if err := c.Close(); err != nil {
					t.Error(err)
				}
			})
			ch, err := c.Chardev(t.Context(), tc.dev, tc.port, tc.kind)
			if tc.wantErr == nil && err != nil || tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("%s: err=%v", tc.expected, err)
			}
			if string(ch.NameBytes()) != tc.wantName || len(socket.sent) != tc.wantSent {
				t.Fatalf("%s: name %q, %d sent", tc.expected, ch.NameBytes(), len(socket.sent))
			}
		})
	}
	// GET_CHARDEV has its own builder; the read-only allowlist still refuses it.
	if xtcpnl.IsRdmaGetCmd(xtcpnl.RdmaNldevCmdGetChardev) {
		t.Fatal("IsRdmaGetCmd admits GET_CHARDEV")
	}
}

// openFDs counts this process's open descriptors.
func openFDs(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}

func chardevNamed(name string, major, minor uint32) *xtcpnl.RdmaChardev {
	ch := &xtcpnl.RdmaChardev{Dev: hugeEncodeDev(major, minor)}
	ch.NameLen = uint8(copy(ch.Name[:], name))
	return ch
}

func TestOpenChardev(t *testing.T) {
	regular := t.TempDir()
	if err := os.WriteFile(filepath.Join(regular, "uverbs0"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, category, description, expected string
		dir                                   string
		ch                                    *xtcpnl.RdmaChardev
		wantErr                               error
	}{
		{"devt_match", "positive", "/dev/null as 1:3", "descriptor returned",
			"/dev", chardevNamed("null", 1, 3), nil},
		{"devt_mismatch", "negative", "/dev/null claimed as 1:5", "ErrIdentity, descriptor closed",
			"/dev", chardevNamed("null", 1, 5), ErrIdentity},
		{"regular_file", "negative", "a regular file where the node should be", "ErrIdentity, descriptor closed",
			regular, chardevNamed("uverbs0", 0, 0), ErrIdentity},
		{"missing", "negative", "no such node", "ENOENT",
			regular, chardevNamed("uverbs9", 231, 201), unix.ENOENT},
		{"slash", "corner", "name with a slash", "ErrRequest, nothing opened",
			"/dev", chardevNamed("../null", 1, 3), ErrRequest},
		{"dotdot", "corner", "name ..", "ErrRequest, nothing opened",
			"/dev", chardevNamed("..", 0, 0), ErrRequest},
		{"empty", "boundary", "empty name", "ErrRequest, nothing opened",
			"/dev", chardevNamed("", 1, 3), ErrRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			before := openFDs(t)
			fd, err := openChardev(tc.dir, tc.ch, unix.O_RDONLY)
			if tc.wantErr == nil && err != nil || tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("%s: err=%v", tc.expected, err)
			}
			if tc.wantErr == nil {
				if fd < 0 {
					t.Fatalf("%s: fd %d", tc.expected, fd)
				}
				flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
				if err != nil || flags&unix.FD_CLOEXEC == 0 {
					t.Errorf("fd flags %#x, %v: want FD_CLOEXEC", flags, err)
				}
				if err := unix.Close(fd); err != nil {
					t.Fatal(err)
				}
			} else if fd != -1 {
				t.Fatalf("%s: fd %d on error", tc.expected, fd)
			}
			if after := openFDs(t); after != before {
				t.Fatalf("%s: %d descriptors before, %d after", tc.expected, before, after)
			}
		})
	}
}
