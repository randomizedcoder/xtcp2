package nlmon

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

func le32(v uint32) []byte { return binary.LittleEndian.AppendUint32(nil, v) }

func cat(bs ...[]byte) []byte {
	var out []byte
	for _, b := range bs {
		out = append(out, b...)
	}
	return out
}

// hdr is an nlmsghdr with the given length field.
func hdr(typ, flags uint16, seq, pid, length uint32) []byte {
	b := le32(length)
	b = binary.LittleEndian.AppendUint16(b, typ)
	b = binary.LittleEndian.AppendUint16(b, flags)
	b = binary.LittleEndian.AppendUint32(b, seq)
	return binary.LittleEndian.AppendUint32(b, pid)
}

// rec is a message with body, its length right, padded to 4 bytes.
func rec(typ uint16, body []byte) []byte {
	b := cat(hdr(typ, 0, 0, 0, uint32(xtcpnl.NlMsgHdrSizeCst+len(body))), body)
	return append(b, make([]byte, align(len(b))-len(b))...)
}

// types is the walk's "type/nsid " trace.
func types(walk func(func(Msg) error) error) (string, error) {
	var b strings.Builder
	err := walk(func(m Msg) error {
		fmt.Fprintf(&b, "%d/%d ", m.Hdr.Type, m.Nsid)
		return nil
	})
	return b.String(), err
}

// TestDatagram is rtnl_listen's walk of one datagram.
//
// go test ./internal/nlmon/ -run TestDatagram
func TestDatagram(t *testing.T) {
	type result struct {
		types string
		err   string
		kind  error
	}
	big := rec(unix.RTM_NEWTFILTER, make([]byte, RecvBufCst-xtcpnl.NlMsgHdrSizeCst))
	tests := []struct {
		description string
		datagram    []byte
		truncated   bool
		expected    result
	}{
		{"positive: three messages in order", cat(rec(unix.RTM_NEWQDISC, le32(1)), rec(unix.RTM_DELQDISC, nil),
			rec(unix.RTM_NEWTFILTER, nil)), false, result{types: "36/5 37/5 44/5 "}},
		{"boundary: an empty datagram", nil, false, result{}},
		{"positive: the last body unpadded", rec(unix.RTM_NEWLINK, []byte{1})[:17], false, result{types: "16/5 "}},
		{"negative: a length past the datagram", hdr(unix.RTM_NEWLINK, 0, 0, 0, 64), false,
			result{err: "!!!malformed message: len=64", kind: ErrMalformed}},
		{"negative: a length under the header's", hdr(unix.RTM_NEWLINK, 0, 0, 0, 4), false,
			result{err: "!!!malformed message: len=4", kind: ErrMalformed}},
		{"negative: a length past a truncated datagram", cat(rec(unix.RTM_NEWQDISC, nil), hdr(unix.RTM_NEWTFILTER, 0, 0, 0, 40000)),
			true, result{types: "36/5 ", err: "Truncated message", kind: ErrTruncated}},
		{"corner: a remnant under a header", cat(rec(unix.RTM_NEWLINK, nil), []byte{1, 2, 3}), false,
			result{types: "16/5 ", err: "!!!Remnant of size 3", kind: ErrRemnant}},
		{"corner: a truncated datagram skips the remnant check", cat(rec(unix.RTM_NEWLINK, nil), []byte{1, 2, 3}), true,
			result{types: "16/5 "}},
		{"boundary: a message of exactly the buffer", big, false, result{types: "44/5 "}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			s, err := types(func(fn func(Msg) error) error { return Datagram(tc.datagram, tc.truncated, 5, fn) })
			got := result{types: s}
			if err != nil {
				got.err = err.Error()
				if !errors.Is(err, tc.expected.kind) {
					t.Errorf("error %v is not %v", err, tc.expected.kind)
				}
				got.kind = tc.expected.kind
			}
			if got != tc.expected {
				t.Errorf("got %+v, want %+v", got, tc.expected)
			}
		})
	}
	t.Run("corner: fn's error ends the walk as it is", func(t *testing.T) {
		stop := errors.New("stop")
		n := 0
		err := Datagram(cat(rec(1, nil), rec(2, nil), rec(3, nil)), false, -1, func(Msg) error {
			n++
			if n == 2 {
				return stop
			}
			return nil
		})
		if !errors.Is(err, stop) || n != 2 {
			t.Errorf("got %v after %d, want stop after 2", err, n)
		}
	})
}

// TestWalkFile is rtnl_from_file.
//
// go test ./internal/nlmon/ -run TestWalkFile
func TestWalkFile(t *testing.T) {
	type result struct {
		types string
		err   string
	}
	two := cat(rec(unix.RTM_NEWQDISC, le32(1)), rec(unix.RTM_NEWTFILTER, []byte{1}))
	tests := []struct {
		description string
		data        []byte
		kind        error
		expected    result
	}{
		{"positive: two records", two, nil, result{types: "36/-1 44/-1 "}},
		{"boundary: an empty file", nil, nil, result{}},
		{"negative: cut inside the header", two[:len(two)-20+4], ErrFileTruncated,
			result{types: "36/-1 ", err: "rtnl-from_file: truncated message"}},
		{"negative: cut inside the body", two[:len(two)-1], ErrFileTruncated,
			result{types: "36/-1 ", err: "rtnl-from_file: truncated message"}},
		{"negative: a length of 8", cat(rec(unix.RTM_NEWQDISC, nil), hdr(unix.RTM_NEWQDISC, 0, 0, 0, 8)), ErrMalformed,
			result{types: "36/-1 ", err: "!!!malformed message: len=8 @32"}},
		{"boundary: a length over the buffer", hdr(unix.RTM_NEWQDISC, 0, 0, 0, RecvBufCst+1), ErrMalformed,
			result{err: "!!!malformed message: len=16385 @16"}},
		{"corner: the last body unpadded is cut short", rec(unix.RTM_NEWLINK, []byte{1})[:17], ErrFileTruncated,
			result{err: "rtnl-from_file: truncated message"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			s, err := types(func(fn func(Msg) error) error { return WalkFile(tc.data, fn) })
			got := result{types: s}
			if err != nil {
				got.err = err.Error()
			}
			if got != tc.expected {
				t.Errorf("got %+v, want %+v", got, tc.expected)
			}
			if (err == nil) != (tc.kind == nil) || (tc.kind != nil && !errors.Is(err, tc.kind)) {
				t.Errorf("error %v, want kind %v", err, tc.kind)
			}
		})
	}
}

// TestReadFile is the fopen ahead of rtnl_from_file.
//
// go test ./internal/nlmon/ -run TestReadFile
func TestReadFile(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "f")
	if err := os.WriteFile(good, rec(unix.RTM_NEWQDISC, nil), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		description string
		path        string
		types, err  string
		kind        error
	}{
		{"positive: a file of one record", good, "36/-1 ", "", nil},
		{"negative: a missing file", filepath.Join(dir, "none"), "", "Cannot fopen: No such file or directory", ErrOpen},
		{"negative: a directory", dir, "", "Cannot fopen: Is a directory", ErrOpen},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			s, err := types(func(fn func(Msg) error) error { return ReadFile(tc.path, fn) })
			var e string
			if err != nil {
				e = err.Error()
			}
			if s != tc.types || e != tc.err {
				t.Errorf("got %q %q, want %q %q", s, e, tc.types, tc.err)
			}
			if tc.kind != nil && !errors.Is(err, tc.kind) {
				t.Errorf("error %v is not %v", err, tc.kind)
			}
		})
	}
}

// TestTimestamp is print_timestamp's two forms.
//
// go test ./internal/nlmon/ -run TestTimestamp
func TestTimestamp(t *testing.T) {
	t.Setenv("TZ", "UTC")
	at := time.Date(2026, 11, 25, 10, 0, 0, 123456789, time.UTC)
	tests := []struct {
		description string
		t           time.Time
		short       bool
		expected    string
	}{
		{"positive: long", at, false, "Timestamp: Wed Nov 25 10:00:00 2026 123456 usec\n"},
		{"positive: short", at, true, "[2026-11-25T10:00:00.123456] "},
		{"boundary: no microseconds", at.Truncate(time.Second), false, "Timestamp: Wed Nov 25 10:00:00 2026 0 usec\n"},
		{"corner: a one-digit day pads with a space", time.Date(2026, 11, 5, 1, 2, 3, 7000, time.UTC), false,
			"Timestamp: Thu Nov  5 01:02:03 2026 7 usec\n"},
		{"corner: short pads the microseconds to six", time.Date(2026, 11, 5, 1, 2, 3, 7000, time.UTC), true,
			"[2026-11-05T01:02:03.000007] "},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := Timestamp(tc.t.In(time.Local), tc.short); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

// TestNowFunc is the replay clock.
//
// go test ./internal/nlmon/ -run TestNowFunc
func TestNowFunc(t *testing.T) {
	tests := []struct {
		description string
		env         string
		fixed       bool
	}{
		{"positive: an RFC3339 instant pins it", "2026-11-25T10:00:00Z", true},
		{"negative: garbage is the wall clock", "yesterday", false},
		{"boundary: unset is the wall clock", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Setenv("NLMON_TEST_NOW", tc.env)
			got := NowFunc("NLMON_TEST_NOW")()
			want := time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)
			if got.Equal(want) != tc.fixed {
				t.Errorf("got %v, fixed %v", got, tc.fixed)
			}
		})
	}
}

// TestListenNsid is the nsid a NETLINK_LISTEN_ALL_NSID control message
// carries.
//
// go test ./internal/nlmon/ -run TestListenNsid
func TestListenNsid(t *testing.T) {
	// cmsg is a cmsghdr on a 64-bit host, its length a size_t, then data
	// padded to 8 bytes.
	cmsg := func(level, typ int32, data []byte) []byte {
		b := cat(binary.LittleEndian.AppendUint64(nil, uint64(unix.CmsgLen(len(data)))),
			le32(uint32(level)), le32(uint32(typ)), data)
		return append(b, make([]byte, unix.CmsgSpace(len(data))-len(b))...)
	}
	tests := []struct {
		description string
		oob         []byte
		expected    int32
	}{
		{"positive: nsid 9", cmsg(unix.SOL_NETLINK, unix.NETLINK_LISTEN_ALL_NSID, le32(9)), 9},
		{"boundary: none is -1", nil, -1},
		{"negative: another level is -1", cmsg(unix.SOL_SOCKET, unix.NETLINK_LISTEN_ALL_NSID, le32(9)), -1},
		{"negative: a short value is -1", cmsg(unix.SOL_NETLINK, unix.NETLINK_LISTEN_ALL_NSID, []byte{9}), -1},
		{"corner: the last of two", cat(cmsg(unix.SOL_NETLINK, unix.NETLINK_LISTEN_ALL_NSID, le32(9)),
			cmsg(unix.SOL_NETLINK, unix.NETLINK_LISTEN_ALL_NSID, le32(2))), 2},
		{"negative: garbage is -1", []byte{1, 2, 3}, -1},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := ListenNsid(tc.oob); got != tc.expected {
				t.Errorf("got %d, want %d", got, tc.expected)
			}
		})
	}
}

// TestListen is rtnl_listen over a socketpair standing in for the netlink
// socket: datagrams in order, then the receive error that ends it.
//
// go test ./internal/nlmon/ -run TestListen
func TestListen(t *testing.T) {
	tests := []struct {
		description string
		datagrams   [][]byte
		bufSize     int
		types       string
		errOut      string
		err         string
	}{
		{"positive: two datagrams, then EOF", [][]byte{cat(rec(unix.RTM_NEWQDISC, nil), rec(unix.RTM_DELQDISC, nil)),
			rec(unix.RTM_NEWTFILTER, nil)}, 0, "36/-1 37/-1 44/-1 ", "", "EOF on netlink"},
		{"boundary: a datagram over the buffer is cut, then Truncated message", [][]byte{
			rec(unix.RTM_NEWTFILTER, make([]byte, 64))}, 32, "", "", "Truncated message"},
		{"corner: a truncated datagram whose messages fit carries on", [][]byte{
			cat(rec(unix.RTM_NEWQDISC, nil), make([]byte, 40))}, 24, "36/-1 ", "Message truncated\n", "EOF on netlink"},
		{"negative: a malformed message", [][]byte{hdr(unix.RTM_NEWQDISC, 0, 0, 0, 8)}, 0, "", "",
			"!!!malformed message: len=8"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, d := range tc.datagrams {
				if _, err := unix.Write(fds[1], d); err != nil {
					t.Fatal(err)
				}
			}
			if err := unix.Close(fds[1]); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := unix.Close(fds[0]); err != nil {
					t.Error(err)
				}
			}()
			var errOut strings.Builder
			s, err := types(func(fn func(Msg) error) error {
				return Listen(fds[0], ListenOpts{BufSize: tc.bufSize, ErrOut: &errOut}, fn)
			})
			var e string
			if err != nil {
				e = err.Error()
			}
			if s != tc.types || errOut.String() != tc.errOut || e != tc.err {
				t.Errorf("got %q %q %q, want %q %q %q", s, errOut.String(), e, tc.types, tc.errOut, tc.err)
			}
		})
	}
}

// TestListenErrors is rtnl_listen's other endings and its all-nsid path.
//
// go test ./internal/nlmon/ -run TestListenErrors
func TestListenErrors(t *testing.T) {
	t.Run("negative: a closed descriptor is a receive error", func(t *testing.T) {
		err := Listen(-1, ListenOpts{}, func(Msg) error { return nil })
		if err == nil || err.Error() != "netlink receive error Bad file descriptor (9)" || !errors.Is(err, ErrRecv) {
			t.Errorf("got %v", err)
		}
	})
	t.Run("boundary: all-nsid with no control message names -1", func(t *testing.T) {
		fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := unix.Write(fds[1], rec(unix.RTM_NEWQDISC, nil)); err != nil {
			t.Fatal(err)
		}
		if err := unix.Close(fds[1]); err != nil {
			t.Fatal(err)
		}
		s, err := types(func(fn func(Msg) error) error { return Listen(fds[0], ListenOpts{AllNsid: true}, fn) })
		if s != "36/-1 " || err == nil || err.Error() != "EOF on netlink" {
			t.Errorf("got %q %v", s, err)
		}
		if err := unix.Close(fds[0]); err != nil {
			t.Error(err)
		}
	})
}

// TestStrerror is glibc's text for an errno.
//
// go test ./internal/nlmon/ -run TestStrerror
func TestStrerror(t *testing.T) {
	tests := []struct {
		description string
		errno       unix.Errno
		expected    string
	}{
		{"positive: ENOENT", unix.ENOENT, "No such file or directory"},
		{"positive: ENOBUFS", unix.ENOBUFS, "No buffer space available"},
		{"boundary: 0 is Success", 0, "Success"},
		{"corner: an errno Go has no text for", unix.Errno(4000), "Unknown error 4000"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := Strerror(tc.errno); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}
