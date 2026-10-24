package goip

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// tdGenl is the C2 generic netlink corpus, one directory per row.
const tdGenl = "../../pkg/xtcpnl/testdata/7_1_4/link-write/genl"

// genlRow is one C2 row's ip.pcap.
func genlRow(row string) string { return filepath.Join(tdGenl, row, "ip.pcap") }

// recordedGenl returns a capture's genl exchanges, read with nlparity rather
// than the replay source under test.
func recordedGenl(t *testing.T, path string) (nlparity.Capture, []nlparity.Txn) {
	t.Helper()
	c, err := nlparity.ParseCaptureFile(path, unix.NETLINK_GENERIC)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return c, nlparity.SegmentCapture(c).Txns
}

// asSent rebuilds a recorded request as goip would send it, under its own
// sequence number.
func asSent(m nlparity.Msg, seq uint32) []byte {
	h := m.Hdr
	h.Seq, h.Pid = seq, 0
	return wholeMsg(h, m.Body)
}

// getFamilyName is the CTRL_ATTR_FAMILY_NAME a recorded CTRL_CMD_GETFAMILY
// asks for, or "" for one by id.
func getFamilyName(m nlparity.Msg) string {
	if m.Hdr.Type != unix.GENL_ID_CTRL || len(m.Body) < 4 || m.Body[0] != unix.CTRL_CMD_GETFAMILY {
		return ""
	}
	_, attrs, _ := nlparity.DecodeMsg(m)
	for _, a := range attrs {
		if a.BareType() == unix.CTRL_ATTR_FAMILY_NAME {
			return strings.TrimRight(string(a.Val), "\x00")
		}
	}
	return ""
}

// TestGenlFamilyMatchesIp checks goip's family resolution against every
// CTRL_CMD_GETFAMILY by name in each C2 family's ip.pcap: the request byte for
// byte, its sequence number aside, and the id it resolves through a replay of
// that capture.
//
// go test ./internal/goip/ -run TestGenlFamilyMatchesIp
func TestGenlFamilyMatchesIp(t *testing.T) {
	tests := []struct {
		description string
		prefix      string
		family      string
		rows        int // rows whose ip.pcap resolves the family by name
	}{
		{description: "positive: fou, a 3-byte name", prefix: "fou_", family: "fou", rows: 21},
		{description: "positive: ila", prefix: "ila_", family: "ila", rows: 15},
		{description: "positive: IOAM6, a 5-byte name, padded to 12", prefix: "ioam_", family: "IOAM6", rows: 21},
		{description: "positive: l2tp", prefix: "l2tp_", family: "l2tp", rows: 24},
		{description: "positive: macsec", prefix: "macsec_", family: "macsec", rows: 32},
		{description: "positive: mptcp_pm", prefix: "mptcp_", family: "mptcp_pm", rows: 27},
		{description: "positive: SEG6", prefix: "sr_", family: "SEG6", rows: 11},
		{description: "boundary: tcp_metrics, 11 bytes and its terminator, no padding", prefix: "tcpm_", family: "tcp_metrics", rows: 14},
	}

	dirs, err := os.ReadDir(tdGenl)
	if err != nil {
		t.Fatalf("read %s: %v", tdGenl, err)
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			rows := 0
			for _, d := range dirs {
				if !strings.HasPrefix(d.Name(), tc.prefix) {
					continue
				}
				path := genlRow(d.Name())
				c, txns := recordedGenl(t, path)
				asked := 0
				for _, txn := range txns {
					name := getFamilyName(txn.Request)
					if name == "" {
						continue
					}
					asked++
					if name != tc.family {
						t.Errorf("%s: asks for %q, want %q", path, name, tc.family)
					}
					seq := txn.Request.Hdr.Seq
					got, err := xtcpnl.BuildGetFamilyRequestVersion(name, genlGetFamilyVersionCst, seq)
					if err != nil {
						t.Fatalf("build: %v", err)
					}
					if want := asSent(txn.Request, seq); !bytes.Equal(got, want) {
						t.Errorf("%s: request\n  got  %x\n  want %x", path, got, want)
					}
				}
				if asked == 0 {
					continue
				}
				rows++

				src, err := OpenReplay(path)
				if err != nil {
					t.Fatalf("%s: %v", path, err)
				}
				rc := newRunCtx(nil, io.Discard, io.Discard)
				rc.src = src
				id, err := rc.genlFamily(tc.family)
				if err != nil {
					t.Fatalf("%s: genlFamily: %v", path, err)
				}
				if want := c.GenlFamilies; want[id] != tc.family {
					t.Errorf("%s: resolved id %d, the capture names %v", path, id, want)
				}
			}
			if rows != tc.rows {
				t.Errorf("%d rows resolve %q, want %d", rows, tc.family, tc.rows)
			}
		})
	}
}

// fakeGenl answers GenlTalk with a fixed reply and counts the requests.
type fakeGenl struct {
	typ   uint16
	body  []byte
	err   error
	talks int
	last  []byte
}

func (f *fakeGenl) Dump([]byte, uint16) ([][]byte, error) { return nil, nil }

func (f *fakeGenl) GenlDump([]byte) ([][]byte, error) { return nil, nil }

func (f *fakeGenl) GenlTalk(req []byte) (uint16, []byte, error) {
	f.talks++
	f.last = req
	return f.typ, f.body, f.err
}

// dumpOnly is a Source with no genl side.
type dumpOnly struct{}

func (dumpOnly) Dump([]byte, uint16) ([][]byte, error) { return nil, nil }

// newFamilyBody is a CTRL_CMD_NEWFAMILY body naming a family.
func newFamilyBody(cmd uint8, id uint16, name string) []byte {
	b := []byte{cmd, 2, 0, 0}
	if id != 0 {
		b = append(b, nla(unix.CTRL_ATTR_FAMILY_ID, binary.LittleEndian.AppendUint16(nil, id))...)
	}
	return append(b, nla(unix.CTRL_ATTR_FAMILY_NAME, []byte(name+"\x00"))...)
}

// TestGenlFamily is genl_init_handle's resolution over a fake source: the
// request, the answer's checks and the run's cache.
//
// go test ./internal/goip/ -run TestGenlFamily$
func TestGenlFamily(t *testing.T) {
	good := newFamilyBody(unix.CTRL_CMD_NEWFAMILY, 41, "fou")
	tests := []struct {
		description string
		src         Source
		name        string
		calls       int // genlFamily calls in a row
		wantID      uint16
		wantErr     error
		wantTalks   int
		wantErrText string
	}{
		{description: "positive: one GETFAMILY resolves the id", src: &fakeGenl{typ: unix.GENL_ID_CTRL, body: good},
			name: "fou", calls: 1, wantID: 41, wantTalks: 1},
		{description: "positive: the second ask is the cache's", src: &fakeGenl{typ: unix.GENL_ID_CTRL, body: good},
			name: "fou", calls: 2, wantID: 41, wantTalks: 1},
		{description: "negative: the kernel's refusal, a family not registered", src: &fakeGenl{err: unix.ENOENT},
			name: "fou", calls: 1, wantErr: unix.ENOENT, wantTalks: 1},
		{description: "corner: a refusal is not cached, so it is asked again", src: &fakeGenl{err: unix.ENOENT},
			name: "fou", calls: 2, wantErr: ErrGenlResolve, wantTalks: 2},
		{description: "negative: an answer from another family", src: &fakeGenl{typ: 41, body: good},
			name: "fou", calls: 1, wantErr: ErrGenlResolve, wantTalks: 1, wantErrText: "not a controller message, nlmsg_type=0x29"},
		{description: "negative: an ACK and no answer", src: &fakeGenl{},
			name: "fou", calls: 1, wantErr: ErrGenlResolve, wantTalks: 1},
		{description: "negative: a NEWFAMILY without its id", src: &fakeGenl{typ: unix.GENL_ID_CTRL, body: newFamilyBody(unix.CTRL_CMD_NEWFAMILY, 0, "fou")},
			name: "fou", calls: 1, wantErr: xtcpnl.ErrGenericNetlink, wantTalks: 1},
		{description: "negative: another controller command", src: &fakeGenl{typ: unix.GENL_ID_CTRL, body: newFamilyBody(unix.CTRL_CMD_GETFAMILY, 41, "fou")},
			name: "fou", calls: 1, wantErr: ErrGenlResolve, wantTalks: 1},
		{description: "boundary: a 15-byte name, GENL_NAMSIZ less its terminator", src: &fakeGenl{typ: unix.GENL_ID_CTRL, body: newFamilyBody(unix.CTRL_CMD_NEWFAMILY, 42, "abcdefghijklmno")},
			name: "abcdefghijklmno", calls: 1, wantID: 42, wantTalks: 1},
		{description: "boundary: a 16-byte name is refused before it is sent", src: &fakeGenl{typ: unix.GENL_ID_CTRL, body: good},
			name: "abcdefghijklmnop", calls: 1, wantErr: ErrGenlResolve, wantTalks: 0},
		{description: "corner: a source with no genl side", src: dumpOnly{},
			name: "fou", calls: 1, wantErr: ErrNotImplemented},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			rc := newRunCtx(nil, io.Discard, io.Discard)
			rc.src = tc.src
			var id uint16
			var err error
			for range tc.calls {
				id, err = rc.genlFamily(tc.name)
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil && !errors.Is(err, ErrNotImplemented) && !errors.Is(err, ErrGenlResolve) {
				t.Errorf("err = %v, want it to wrap ErrGenlResolve", err)
			}
			if tc.wantErrText != "" && !strings.Contains(err.Error(), tc.wantErrText) {
				t.Errorf("err = %v, want %q in it", err, tc.wantErrText)
			}
			if id != tc.wantID {
				t.Errorf("id = %d, want %d", id, tc.wantID)
			}
			if f, ok := tc.src.(*fakeGenl); ok {
				if f.talks != tc.wantTalks {
					t.Errorf("%d requests, want %d", f.talks, tc.wantTalks)
				}
				if f.talks != 0 && (f.last[17] != genlGetFamilyVersionCst || f.last[16] != unix.CTRL_CMD_GETFAMILY) {
					t.Errorf("request genlmsghdr %x, want CTRL_CMD_GETFAMILY version 0", f.last[16:20])
				}
			}
		})
	}
}

// TestReplayGenl replays C2 captures' genl exchanges by request.
//
// go test ./internal/goip/ -run TestReplayGenl
func TestReplayGenl(t *testing.T) {
	const seq = 0x5eed

	tests := []struct {
		description string
		row         string
		txn         int // recorded exchange to send
		dump        bool
		edit        func([]byte) []byte
		twice       bool
		wantType    uint16
		wantBodies  int // dump replies, or 1 for a talk's reply
		wantErr     error
	}{
		{description: "positive: the GETFAMILY answered by its NEWFAMILY", row: "fou_add", txn: 0,
			wantType: unix.GENL_ID_CTRL, wantBodies: 1},
		{description: "positive: fou's ADD answered by its ACK", row: "fou_add", txn: 1,
			wantType: 0, wantBodies: 0},
		{description: "negative: a refused ADD is its errno", row: "fou_add_dup", txn: 1,
			wantErr: unix.EADDRINUSE},
		{description: "positive: a get answered by its reply", row: "sr_tunsrc_show", txn: 1,
			wantType: 36, wantBodies: 1},
		{description: "positive: a dump of two entries", row: "sr_hmac_show", txn: 1, dump: true,
			wantBodies: 2},
		{description: "boundary: an empty dump is no entries and no error", row: "sr_hmac_show_empty", txn: 1, dump: true,
			wantBodies: 0},
		{description: "negative: a request one byte off was not recorded", row: "fou_add", txn: 1,
			edit: func(b []byte) []byte { b[len(b)-1] ^= 1; return b }, wantErr: ErrNoReplay},
		{description: "corner: an exchange replays once", row: "fou_add", txn: 0, twice: true,
			wantErr: ErrNoReplay},
		{description: "negative: a request shorter than its header", row: "fou_add", txn: 0,
			edit: func(b []byte) []byte { return b[:15] }, wantErr: xtcpnl.ErrShortRequest},
		{description: "boundary: an nlmsg_len past the bytes given", row: "fou_add", txn: 0,
			edit: func(b []byte) []byte { return b[:len(b)-4] }, wantErr: xtcpnl.ErrShortRequest},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			path := genlRow(tc.row)
			_, txns := recordedGenl(t, path)
			src, err := OpenReplay(path)
			if err != nil {
				t.Fatal(err)
			}
			req := asSent(txns[tc.txn].Request, seq)
			if tc.edit != nil {
				req = tc.edit(req)
			}
			send := func() (uint16, int, error) {
				if tc.dump {
					bodies, err := src.GenlDump(req)
					return 0, len(bodies), err
				}
				typ, body, err := src.GenlTalk(req)
				n := 0
				if body != nil {
					n = 1
				}
				return typ, n, err
			}
			if tc.twice {
				if _, _, err := send(); err != nil {
					t.Fatalf("first send: %v", err)
				}
			}
			typ, n, err := send()
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if typ != tc.wantType || n != tc.wantBodies {
				t.Errorf("type %d and %d bodies, want %d and %d", typ, n, tc.wantType, tc.wantBodies)
			}
		})
	}
}

// TestReplayGenlRouteCapture is a route capture's replay asked for genl: it
// recorded no exchange.
//
// go test ./internal/goip/ -run TestReplayGenlRouteCapture
func TestReplayGenlRouteCapture(t *testing.T) {
	src, err := OpenReplay(genlRow("fou_add"))
	if err != nil {
		t.Fatal(err)
	}
	route, err := OpenReplay("../../pkg/xtcpnl/testdata/7_1_8/netlink_route_getlink.pcap")
	if err != nil {
		t.Fatal(err)
	}
	req, err := xtcpnl.BuildGetFamilyRequestVersion("fou", genlGetFamilyVersionCst, 1)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(genlRow("fou_add"))
	if err != nil {
		t.Fatal(err)
	}
	mem, err := NewReplayFromBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		description string
		src         *ReplaySource
		wantErr     error
	}{
		{description: "positive: the genl capture recorded it", src: src, wantErr: nil},
		{description: "positive: the same capture from bytes in memory", src: mem, wantErr: nil},
		{description: "negative: the route capture did not", src: route, wantErr: ErrNoReplay},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if _, _, err := tc.src.GenlTalk(req); !errors.Is(err, tc.wantErr) {
				t.Errorf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// TestNetlinkGenl resolves families on a live genl socket. The controller is
// always registered, as itself at GENL_ID_CTRL.
//
// go test ./internal/goip/ -run TestNetlinkGenl
func TestNetlinkGenl(t *testing.T) {
	s, err := OpenNetlink()
	if err != nil {
		t.Skipf("no netlink socket here: %v", err)
	}
	t.Cleanup(func() {
		if cerr := s.Close(); cerr != nil {
			t.Errorf("Close: %v", cerr)
		}
	})
	rc := newRunCtx(nil, io.Discard, io.Discard)
	rc.src = s

	tests := []struct {
		description string
		name        string
		wantID      uint16
		wantErr     error
	}{
		{description: "positive: the controller is nlctrl at GENL_ID_CTRL", name: "nlctrl", wantID: unix.GENL_ID_CTRL},
		{description: "negative: a family nothing registered is ENOENT", name: "goip_no_such", wantErr: unix.ENOENT},
		{description: "positive: asked again, from the cache", name: "nlctrl", wantID: unix.GENL_ID_CTRL},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			id, err := rc.genlFamily(tc.name)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if id != tc.wantID {
				t.Errorf("id = %d, want %d", id, tc.wantID)
			}
		})
	}

	t.Run("positive: the controller's family dump holds nlctrl", func(t *testing.T) {
		req := []byte{20, 0, 0, 0, unix.GENL_ID_CTRL, 0, 0x01, 0x03, 9, 0, 0, 0, 0, 0, 0, 0,
			unix.CTRL_CMD_GETFAMILY, 0, 0, 0}
		bodies, err := s.GenlDump(req)
		if err != nil {
			t.Fatalf("GenlDump: %v", err)
		}
		found := false
		for _, b := range bodies {
			if f, err := xtcpnl.ParseGenericNetlinkFamily(b); err == nil && f.Name == "nlctrl" {
				found = true
			}
		}
		if !found {
			t.Errorf("%d families, none nlctrl", len(bodies))
		}
	})
}

// TestNetlinkGenlClose checks the genl socket is opened on first use only, and
// closed with the route one.
//
// go test ./internal/goip/ -run TestNetlinkGenlClose
func TestNetlinkGenlClose(t *testing.T) {
	tests := []struct {
		description string
		resolve     bool
		wantOpen    bool
	}{
		{description: "positive: a run that resolves a family opens the genl socket, and Close closes it", resolve: true, wantOpen: true},
		{description: "corner: a run that resolves none opens no genl socket", resolve: false, wantOpen: false},
	}
	t.Run("corner: a second request reuses the genl socket", func(t *testing.T) {
		s, err := OpenNetlink()
		if err != nil {
			t.Skipf("no netlink socket here: %v", err)
		}
		t.Cleanup(func() {
			if cerr := s.Close(); cerr != nil {
				t.Errorf("Close: %v", cerr)
			}
		})
		req, err := xtcpnl.BuildGetFamilyRequestVersion("nlctrl", genlGetFamilyVersionCst, 1)
		if err != nil {
			t.Fatal(err)
		}
		fds := make([]int, 0, 2)
		for range 2 {
			if _, _, err := s.GenlTalk(req); err != nil {
				t.Fatalf("GenlTalk: %v", err)
			}
			fds = append(fds, s.gfd)
		}
		if fds[0] != fds[1] {
			t.Errorf("genl fds %v, want one socket", fds)
		}
	})
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			s, err := OpenNetlink()
			if err != nil {
				t.Skipf("no netlink socket here: %v", err)
			}
			if tc.resolve {
				rc := newRunCtx(nil, io.Discard, io.Discard)
				rc.src = s
				if _, err := rc.genlFamily("nlctrl"); err != nil {
					t.Fatalf("genlFamily: %v", err)
				}
			}
			if s.gopen != tc.wantOpen {
				t.Fatalf("genl socket open = %v, want %v", s.gopen, tc.wantOpen)
			}
			gfd := s.gfd
			if err := s.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}
			if !tc.wantOpen {
				return
			}
			if _, err := unix.FcntlInt(uintptr(gfd), unix.F_GETFD, 0); !errors.Is(err, unix.EBADF) {
				t.Errorf("genl fd %d after Close: %v, want EBADF", gfd, err)
			}
		})
	}
}

// TestReplayGenlSegments replays constructed exchanges, for the answers the
// corpus has no capture of.
//
// go test ./internal/goip/ -run TestReplayGenlSegments
func TestReplayGenlSegments(t *testing.T) {
	const fam, pid = 36, 4242
	msg := func(typ, flags uint16, p uint32, body []byte) nlparity.Msg {
		return nlparity.Msg{Hdr: xtcpnl.NlMsgHdr{Len: uint32(xtcpnl.NlMsgHdrSizeCst + len(body)), Type: typ,
			Flags: flags, Seq: 7, Pid: p}, Body: body, Proto: unix.NETLINK_GENERIC}
	}
	errno := func(e int32) []byte { return binary.LittleEndian.AppendUint32(nil, uint32(e)) }
	req := msg(fam, 0x301, 0, []byte{1, 0, 0, 0})
	entry := msg(fam, unix.NLM_F_MULTI, pid, []byte{1, 0, 0, 0})

	tests := []struct {
		description string
		replies     []nlparity.Msg
		wantBodies  int
		wantErr     error
	}{
		{description: "positive: an entry and a clean DONE", replies: []nlparity.Msg{entry, msg(unix.NLMSG_DONE, unix.NLM_F_MULTI, pid, errno(0))},
			wantBodies: 1},
		{description: "negative: a dump the kernel ends with EPERM", replies: []nlparity.Msg{entry, msg(unix.NLMSG_DONE, unix.NLM_F_MULTI, pid, errno(-1))},
			wantErr: unix.EPERM},
		{description: "negative: a dump refused outright with EOPNOTSUPP", replies: []nlparity.Msg{msg(unix.NLMSG_ERROR, 0, pid, append(errno(-95), make([]byte, 16)...))},
			wantErr: unix.EOPNOTSUPP},
		{description: "corner: a reply of another type is not an entry", replies: []nlparity.Msg{msg(fam+1, unix.NLM_F_MULTI, pid, []byte{1, 0, 0, 0}), msg(unix.NLMSG_DONE, unix.NLM_F_MULTI, pid, errno(0))},
			wantBodies: 0},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := &ReplaySource{gseg: nlparity.Segment(append([]nlparity.Msg{req}, tc.replies...))}
			bodies, err := src.GenlDump(asSent(req, 99))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if len(bodies) != tc.wantBodies {
				t.Errorf("%d bodies, want %d", len(bodies), tc.wantBodies)
			}
		})
	}
}

// TestNetlinkGenlTimeout checks the genl socket takes the source's receive
// timeout, as the route socket does, so a goiphttp request cannot block on
// it forever. The kernel rounds a timeout up to a jiffy, so the two sockets
// are compared with each other, and with zero.
//
// go test ./internal/goip/ -run TestNetlinkGenlTimeout
func TestNetlinkGenlTimeout(t *testing.T) {
	tests := []struct {
		description string
		timeout     time.Duration // zero for OpenNetlink
		expected    bool          // a timeout set
	}{
		{description: "positive: two seconds", timeout: 2 * time.Second, expected: true},
		{description: "negative: OpenNetlink sets none", expected: false},
		{description: "boundary: one nanosecond is a timeout, not forever", timeout: time.Nanosecond, expected: true},
		{description: "corner: a microsecond and a half", timeout: 1500 * time.Nanosecond, expected: true},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var s *NetlinkSource
			var err error
			if tc.timeout == 0 {
				s, err = OpenNetlink()
			} else {
				s, err = OpenNetlinkTimeout(tc.timeout)
			}
			if err != nil {
				t.Skipf("no netlink socket here: %v", err)
			}
			t.Cleanup(func() {
				if cerr := s.Close(); cerr != nil {
					t.Errorf("Close: %v", cerr)
				}
			})
			if _, err := s.genl(); err != nil {
				t.Fatal(err)
			}
			route, err := unix.GetsockoptTimeval(s.fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO)
			if err != nil {
				t.Fatal(err)
			}
			genl, err := unix.GetsockoptTimeval(s.gfd, unix.SOL_SOCKET, unix.SO_RCVTIMEO)
			if err != nil {
				t.Fatal(err)
			}
			if *genl != *route {
				t.Errorf("genl SO_RCVTIMEO %+v, route %+v", *genl, *route)
			}
			if set := *genl != (unix.Timeval{}); set != tc.expected {
				t.Errorf("genl SO_RCVTIMEO %+v, set %v, want %v", *genl, set, tc.expected)
			}
			if tc.timeout >= time.Second && genl.Sec != int64(tc.timeout/time.Second) {
				t.Errorf("genl SO_RCVTIMEO %+v, want %v", *genl, tc.timeout)
			}
		})
	}
}
