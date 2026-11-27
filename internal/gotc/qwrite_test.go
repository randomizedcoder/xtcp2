//go:build gotc_write

package gotc

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// ack is the kernel's NLMSG_ERROR of 0.
func ack() Msg { return nlErr(0) }

// sentWrite is what TestGotcQwrite reads back from the last request.
type sentWrite struct {
	typ, flags     uint16
	ifindex        uint32
	handle, parent uint32
	attrs          []byte
}

func lastWrite(t *testing.T, reqs [][]byte) sentWrite {
	t.Helper()
	if len(reqs) == 0 {
		t.Fatal("no request sent")
	}
	b := reqs[len(reqs)-1]
	if len(b) < nlHeaderLen+xtcpnl.TcMsgSizeCst {
		t.Fatalf("request of %d bytes", len(b))
	}
	tc := b[nlHeaderLen:]
	return sentWrite{
		typ: binary.LittleEndian.Uint16(b[4:6]), flags: binary.LittleEndian.Uint16(b[6:8]),
		ifindex: binary.LittleEndian.Uint32(tc[4:8]), handle: binary.LittleEndian.Uint32(tc[8:12]),
		parent: binary.LittleEndian.Uint32(tc[12:16]), attrs: tc[xtcpnl.TcMsgSizeCst:],
	}
}

const (
	fReq = unix.NLM_F_REQUEST | unix.NLM_F_ACK
)

// TestGotcQwrite is tc_qdisc_modify and tc_class_modify's framework: the
// verbs' flags, the argument loops and their leads, the kind dispatch and
// the ack (gotc 17W).
//
// go test -tags gotc_write ./internal/gotc/ -run TestGotcQwrite
func TestGotcQwrite(t *testing.T) {
	pfifo := kind("pfifo")
	tests := []struct {
		description string
		args        []string
		replies     []Reply
		stderr      string
		rc          int
		reqs        int
		expected    *sentWrite // nil: no write checked
	}{
		// Positive: do_qdisc's and do_class's flags.
		{"positive: qdisc add is EXCL|CREATE", []string{"qdisc", "add", "root", "pfifo"}, []Reply{rep(ack())}, "", 0, 1,
			&sentWrite{typ: unix.RTM_NEWQDISC, flags: fReq | unix.NLM_F_EXCL | unix.NLM_F_CREATE, parent: tcHRoot, attrs: pfifo}},
		{"positive: qdisc change is 0", []string{"qdisc", "change", "root", "pfifo"}, []Reply{rep(ack())}, "", 0, 1,
			&sentWrite{typ: unix.RTM_NEWQDISC, flags: fReq, parent: tcHRoot, attrs: pfifo}},
		{"positive: qdisc replace is CREATE|REPLACE", []string{"qdisc", "replace", "root", "pfifo"}, []Reply{rep(ack())}, "", 0, 1,
			&sentWrite{typ: unix.RTM_NEWQDISC, flags: fReq | unix.NLM_F_CREATE | unix.NLM_F_REPLACE, parent: tcHRoot, attrs: pfifo}},
		{"positive: qdisc link is REPLACE alone", []string{"qdisc", "link", "root", "pfifo"}, []Reply{rep(ack())}, "", 0, 1,
			&sentWrite{typ: unix.RTM_NEWQDISC, flags: fReq | unix.NLM_F_REPLACE, parent: tcHRoot, attrs: pfifo}},
		{"positive: qdisc del is RTM_DELQDISC, 0", []string{"qdisc", "del", "root"}, []Reply{rep(ack())}, "", 0, 1,
			&sentWrite{typ: unix.RTM_DELQDISC, flags: fReq, parent: tcHRoot}},
		{"positive: class replace is CREATE alone, after ll_init_map", []string{"class", "replace", "classid", "1:1"},
			[]Reply{rep(done()), rep(ack())}, "", 0, 2,
			&sentWrite{typ: unix.RTM_NEWTCLASS, flags: fReq | unix.NLM_F_CREATE, handle: 0x10001}},
		{"positive: class delete with dev looks d0 up", []string{"class", "del", "dev", "d0", "classid", "1:1"},
			[]Reply{rep(linkMsg(2, "d0")), rep(ack())}, "", 0, 2,
			&sentWrite{typ: unix.RTM_DELTCLASS, flags: fReq, ifindex: 2, handle: 0x10001}},
		{"positive: blocks come after the kind", []string{"qdisc", "add", "ingress_block", "5", "egress_block", "0x6", "root", "pfifo"},
			[]Reply{rep(ack())}, "", 0, 1, &sentWrite{typ: unix.RTM_NEWQDISC, flags: fReq | unix.NLM_F_EXCL | unix.NLM_F_CREATE,
				parent: tcHRoot, attrs: cat(pfifo, attr(xtcpnl.TcaIngressBlock, u32le(5)), attr(xtcpnl.TcaEgressBlock, u32le(6)))}},
		// Negative.
		{"negative: class add on pfifo is classless, rc 1 (W4)", []string{"class", "add", "classid", "1:1", "pfifo"}, nil,
			"Error: Qdisc \"pfifo\" is classless.\n", 1, 0, nil},
		{"negative: a kernel EEXIST is RTNETLINK answers, rc 2", []string{"qdisc", "add", "root", "pfifo"},
			[]Reply{rep(nlErr(unix.EEXIST))}, "RTNETLINK answers: File exists\n", 2, 1, nil},
		{"negative: a write of a deferred kind is refused by name", []string{"qdisc", "add", "root", "red"}, nil,
			notImpl + "tc qdisc write of kind red\n", 255, 0, nil},
		{"negative: a class write of a deferred classful kind is refused", []string{"class", "add", "classid", "1:1", "drr"}, nil,
			notImpl + "tc class write of kind drr\n", 255, 0, nil},
		{"negative: an unknown kind with options is parse_noqopt's, rc 1", []string{"qdisc", "add", "root", "nosuch", "x"}, nil,
			"Unknown qdisc \"nosuch\", hence option \"x\" is unparsable\n", 1, 0, nil},
		{"negative: pfifo_fast takes no options, rc 255 (W8)", []string{"qdisc", "add", "root", "pfifo_fast", "x"}, nil,
			"qdisc 'pfifo_fast' does not support option parsing\n", 255, 0, nil},
		{"negative: class handle says try classid", []string{"class", "add", "handle", "1:"}, nil,
			"Error: try \"classid\" instead of \"handle\"\n", 255, 0, nil},
		{"negative: root twice", []string{"qdisc", "add", "root", "root"}, nil, "Error: \"root\" is duplicate parent ID\n", 255, 0, nil},
		{"negative: class root twice has a full stop", []string{"class", "add", "root", "root"}, nil,
			"Error: \"root\" is duplicate parent ID.\n", 255, 0, nil},
		{"negative: ingress after parent", []string{"qdisc", "add", "parent", "1:", "ingress"}, nil,
			"Error: \"ingress\" is a duplicate parent ID\n", 255, 0, nil},
		{"negative: ingress_block 0", []string{"qdisc", "add", "ingress_block", "0"}, nil,
			"Error: argument \"0\" is wrong: invalid ingress block index value\n", 255, 0, nil},
		{"negative: estimator out of range", []string{"qdisc", "add", "est", "10s", "1s", "pfifo"}, nil,
			"Error: estimator parameters are out of range.\n", 255, 0, nil},
		{"negative: estimator twice is duparg", []string{"qdisc", "add", "est", "1s", "8s", "est", "1s", "8s"}, nil,
			"Error: duplicate \"estimator\": \"1s\" is the second value.\n", 255, 0, nil},
		{"negative: stab help prints and fails", []string{"qdisc", "add", "stab", "help"}, nil,
			"Usage: ... stab [ mtu BYTES ] [ tsize SLOTS ] [ mpu BYTES ]\n" +
				"                [ overhead BYTES ] [ linklayer TYPE ] ...\n" +
				"   mtu       : max packet size we create rate map for {2047}\n" +
				"   tsize     : how many slots should size table have {512}\n" +
				"   mpu       : minimum packet size used in rate computations\n" +
				"   overhead  : per-packet size overhead used in rate computations\n" +
				"   linklayer : adapting to a linklayer e.g. atm\n" +
				"Example: ... stab overhead 20 linklayer atm\n", 255, 0, nil},
		{"negative: a missing device is nodev, rc 1", []string{"qdisc", "add", "dev", "zz", "root", "pfifo"},
			[]Reply{rep(nlErr(unix.ENODEV))}, "Cannot find device \"zz\"\n", 1, 1, nil},
		// Boundary.
		{"boundary: handle 0: then handle 1: is accepted (W3)", []string{"qdisc", "add", "handle", "0:", "handle", "1:", "root", "pfifo"},
			[]Reply{rep(ack())}, "", 0, 1, &sentWrite{typ: unix.RTM_NEWQDISC, flags: fReq | unix.NLM_F_EXCL | unix.NLM_F_CREATE,
				handle: 0x10000, parent: tcHRoot, attrs: pfifo}},
		{"boundary: handle 1: twice is duparg naming the word handle", []string{"qdisc", "add", "handle", "1:", "handle", "2:"}, nil,
			"Error: duplicate \"handle\": \"handle\" is the second value.\n", 255, 0, nil},
		{"boundary: a stab of mtu alone fails silently (W14)", []string{"qdisc", "add", "stab", "mtu", "100", "pfifo"}, nil, "", 255, 0, nil},
		{"boundary: a stab whose size table never ends is refused (W13)",
			[]string{"qdisc", "add", "stab", "mpu", "1", "tsize", "1", "mtu", "2147483648", "root", "pfifo"}, nil,
			notImpl + "stab tsize 1 with mtu 2147483648 (tc_calc_size_table never returns, W13)\n", 255, 0, nil},
		{"boundary: the kind keeps 15 bytes", []string{"qdisc", "add", "root", "abcdefghijklmnopq"}, []Reply{rep(ack())}, "", 0, 1,
			&sentWrite{typ: unix.RTM_NEWQDISC, flags: fReq | unix.NLM_F_EXCL | unix.NLM_F_CREATE, parent: tcHRoot,
				attrs: kind("abcdefghijklmno")}},
		// Corner.
		{"corner: help mid-line prints usage and goes on (W1)", []string{"qdisc", "add", "help", "root", "pfifo"},
			[]Reply{rep(ack())}, qdiscUsageText, 0, 1, nil},
		{"corner: handle 5: then ingress takes ingress's handle (W2)", []string{"qdisc", "add", "handle", "5:", "ingress"},
			[]Reply{rep(ack())}, "", 0, 1, &sentWrite{typ: unix.RTM_NEWQDISC, flags: fReq | unix.NLM_F_EXCL | unix.NLM_F_CREATE,
				handle: 0xffff0000, parent: tcHIngress, attrs: kind("ingress")}},
		{"corner: words after ingress go to its parser (W2)", []string{"qdisc", "add", "ingress", "dev", "d0"}, nil,
			"What is \"dev\"?\nUsage: ... ingress\n", 1, 0, nil},
		{"corner: ingress skips handle pairs", []string{"qdisc", "add", "ingress", "handle", "ffff:"}, []Reply{rep(ack())}, "", 0, 1, nil},
		{"corner: clsact refuses any word", []string{"qdisc", "add", "clsact", "foo"}, nil,
			"What is \"foo\"?\nUsage: ... clsact\n", 1, 0, nil},
		{"corner: an unknown class kind is classless", []string{"class", "add", "classid", "1:1", "nosuch"}, nil,
			"Error: Qdisc \"nosuch\" is classless.\n", 1, 0, nil},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Parallel()
			src := &fakeSource{replies: tc.replies}
			var out, errb bytes.Buffer
			rc := run(config{src: src, stdin: strings.NewReader(""), sys: emptySys(t)}, tc.args, &out, &errb)
			if rc != tc.rc || errb.String() != tc.stderr || len(src.reqs) != tc.reqs || out.String() != "" {
				t.Fatalf("rc %d reqs %d stdout %q stderr %q; want rc %d reqs %d stderr %q",
					rc, len(src.reqs), out.String(), errb.String(), tc.rc, tc.reqs, tc.stderr)
			}
			if tc.expected == nil {
				return
			}
			got := lastWrite(t, src.reqs)
			if got.typ != tc.expected.typ || got.flags != tc.expected.flags || got.ifindex != tc.expected.ifindex ||
				got.handle != tc.expected.handle || got.parent != tc.expected.parent ||
				!bytes.Equal(got.attrs, tc.expected.attrs) && tc.expected.attrs != nil {
				t.Errorf("sent %+v\nwant %+v", got, *tc.expected)
			}
		})
	}
}

func u32le(v uint32) []byte { return binary.LittleEndian.AppendUint32(nil, v) }

// TestGotcPercentRate is parse_percent_rate over a sysfs speed file (W9).
func TestGotcPercentRate(t *testing.T) {
	tests := []struct {
		description string
		speed       *string // nil: no file
		str, dev    string
		expected    uint64
		ok          bool
		stderr      string
	}{
		{"positive: 10% of 1000 Mbit/s", ptr("1000\n"), "10%", "d0", 12500000, true, ""},
		{"positive: 100% of 10 Mbit/s", ptr("10"), "100%", "d0", 1250000, true, ""},
		{"negative: no device", ptr("1000"), "10%", "", 0, false,
			"No device specified; specify device to rate limit by percentage\n"},
		{"negative: a speed that reads empty is unknown (the dummy's EINVAL)", ptr(""), "10%", "d0", 0, false,
			"property \"speed\" in file /sys/class/net/d0/speed is currently unknown\n" +
				"Failed to parse /sys/class/net/d0/speed\n"},
		{"negative: no speed file is fopen's errno", nil, "10%", "d0", 0, false,
			"fopen /sys/class/net/d0/speed: No such file or directory\n"},
		{"negative: a speed that is not a number", ptr("fast\n"), "10%", "d0", 0, false,
			"value \"fast\" in file /sys/class/net/d0/speed is not a number\nFailed to parse /sys/class/net/d0/speed\n"},
		{"negative: over 100%", ptr("1000"), "150%", "d0", 0, false,
			"Invalid rate specified; should be between [0,100]% but is 150%\n"},
		{"negative: malformed", ptr("1000"), "x%", "d0", 0, false, "Specified rate value could not be read or is malformed\n"},
		{"boundary: -1 speed gives a negative rate get_rate refuses", ptr("-1"), "10%", "d0", 0, false, ""},
		{"corner: a huge speed overflows r_str[20]", ptr("99999999999999"), "100%", "d0", 0, false,
			"Unable to parse calculated rate\n"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			root := t.TempDir()
			if tc.speed != nil {
				d := filepath.Join(root, "sys", "class", "net", "d0")
				if err := os.MkdirAll(d, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(d, "speed"), []byte(*tc.speed), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			var errb bytes.Buffer
			r := &runner{stderr: &errb, sys: sysFS{root: root}}
			got, ok := r.getPercentRate64(tc.str, tc.dev)
			if got != tc.expected || ok != tc.ok || errb.String() != tc.stderr {
				t.Errorf("got %d %t %q; want %d %t %q", got, ok, errb.String(), tc.expected, tc.ok, tc.stderr)
			}
		})
	}
}

// TestGotcWriteOut is the kernel socket's path: GOTC_WRITE_OUT or
// ErrWriteNotApplied, and never the socket.
func TestGotcWriteOut(t *testing.T) {
	path := filepath.Join(t.TempDir(), "w.bin")
	tests := []struct {
		description string
		env         *string
		writes      int
		expected    error
		size        int // file bytes after
	}{
		{"negative: no GOTC_WRITE_OUT is ErrWriteNotApplied", nil, 1, ErrWriteNotApplied, -1},
		{"negative: an empty GOTC_WRITE_OUT is ErrWriteNotApplied", ptr(""), 1, ErrWriteNotApplied, -1},
		{"positive: one write is one request", &path, 1, nil, 52},
		{"corner: a second write appends; a new run truncates", &path, 2, nil, 104},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if tc.env == nil {
				t.Setenv(writeOutEnvCst, "")
				if err := os.Unsetenv(writeOutEnvCst); err != nil {
					t.Fatal(err)
				}
			} else {
				t.Setenv(writeOutEnvCst, *tc.env)
			}
			r := &runner{src: &LiveSource{fd: -1}, stderr: &bytes.Buffer{}}
			w := xtcpnl.TcWrite{MsgType: unix.RTM_NEWQDISC, Kind: "pfifo_fast"}
			var err error
			for range tc.writes {
				if err = r.sendWrite(&w); err != nil {
					break
				}
			}
			if !errors.Is(err, tc.expected) {
				t.Fatalf("err %v, want %v", err, tc.expected)
			}
			if tc.size < 0 {
				return
			}
			st, err := os.Stat(path)
			if err != nil || int(st.Size()) != tc.size {
				t.Fatalf("file %v %v, want %d bytes", st, err, tc.size)
			}
		})
	}
}

// TestIsGetRequest is the kernel socket's guard: only RTM_GET* goes out.
func TestIsGetRequest(t *testing.T) {
	hdr := func(typ uint16) []byte {
		b := make([]byte, nlHeaderLen)
		binary.LittleEndian.PutUint16(b[4:], typ)
		return b
	}
	tests := []struct {
		description string
		req         []byte
		expected    bool
	}{
		{"positive: RTM_GETQDISC", hdr(unix.RTM_GETQDISC), true},
		{"positive: RTM_GETLINK", hdr(unix.RTM_GETLINK), true},
		{"positive: RTM_GETCHAIN", hdr(unix.RTM_GETCHAIN), true},
		{"negative: RTM_NEWQDISC", hdr(unix.RTM_NEWQDISC), false},
		{"negative: RTM_DELTCLASS", hdr(unix.RTM_DELTCLASS), false},
		{"negative: RTM_SETLINK", hdr(unix.RTM_SETLINK), false},
		{"boundary: a short request", make([]byte, nlHeaderLen-1), false},
		{"corner: NLMSG_DONE (3) is under RTM_BASE", hdr(unix.NLMSG_DONE), false},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := isGetRequest(tc.req); got != tc.expected {
				t.Errorf("got %t, want %t", got, tc.expected)
			}
		})
	}
}

// TestGotcGarbage is the "Garbage instead of arguments" arm the loops
// never reach (W5), called directly: the class one has no newline.
func TestGotcGarbage(t *testing.T) {
	tests := []struct {
		description string
		word        string
		qdisc       bool
		expected    string
	}{
		{"positive: qdisc ends in a newline", "x", true, "Garbage instead of arguments \"x ...\". Try \"tc qdisc help\".\n"},
		{"negative: class has none (W5)", "x", false, "Garbage instead of arguments \"x ...\". Try \"tc class help\"."},
		{"corner: help prints usage first", "help", true,
			qdiscUsageText + "Garbage instead of arguments \"help ...\". Try \"tc qdisc help\".\n"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var errb bytes.Buffer
			r := &runner{stderr: &errb}
			var rc retCode
			if err := r.garbage(tc.word, tc.qdisc); !errors.As(err, &rc) || rc != -1 || errb.String() != tc.expected {
				t.Errorf("got %v %q; want return -1 %q", err, errb.String(), tc.expected)
			}
		})
	}
}

// TestGotcStabDefaultTable is a stab whose tsize defaults to 512: its
// TCA_STAB_DATA is 512 __u16s, after TCA_STAB_BASE.
func TestGotcStabDefaultTable(t *testing.T) {
	tests := []struct {
		description string
		args        []string
		expected    int // TCA_STAB payload bytes
	}{
		{"positive: linklayer atm sends the default 512-slot table", []string{"qdisc", "add", "stab", "linklayer", "atm", "root", "pfifo"},
			4 + xtcpnl.TcSizespecSizeCst + 4 + 2*512},
		{"boundary: overhead alone sends BASE only", []string{"qdisc", "add", "stab", "overhead", "4", "root", "pfifo"},
			4 + xtcpnl.TcSizespecSizeCst},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := &fakeSource{replies: []Reply{rep(ack())}}
			var out, errb bytes.Buffer
			if rc := run(config{src: src, stdin: strings.NewReader(""), sys: emptySys(t)}, tc.args, &out, &errb); rc != 0 {
				t.Fatalf("rc %d: %s", rc, errb.String())
			}
			a := lastWrite(t, src.reqs).attrs[len(kind("pfifo")):]
			if got := int(binary.LittleEndian.Uint16(a)) - 4; got != tc.expected {
				t.Errorf("TCA_STAB payload %d, want %d", got, tc.expected)
			}
		})
	}
}
