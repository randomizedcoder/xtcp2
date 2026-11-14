package govdpa

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

const (
	usageMain = "Usage: vdpa [ OPTIONS ] OBJECT { COMMAND | help }\n" +
		"where  OBJECT := { mgmtdev | dev }\n" +
		"       OPTIONS := { -V[ersion] | -n[o-nice-names] | -j[son] | -p[retty] }\n"
	usageVstats = "Usage: vdpa dev vstats show DEV [qidx QUEUE_INDEX]\n"
)

// TestGovdpaDispatch covers the option and argument paths vdpa.c refuses or
// answers before any VDPA_CMD_* request, so no capture row holds them. The
// expected text is vdpa.c's (and glibc getopt's); the replay row only
// answers vdpa_init's family resolve, except where a row is named.
func TestGovdpaDispatch(t *testing.T) {
	tests := []struct {
		description string
		row         string // capture row; ec-001 (mgmtdev show) if empty
		args        []string
		expected    result // pending is compared as > 0
	}{
		{description: "positive: help prints the usage", args: []string{"help"},
			expected: result{stderr: usageMain, pending: 1}},
		{description: "positive: no object prints the usage", args: nil,
			expected: result{stderr: usageMain, pending: 1}},
		{description: "positive: -V prints the version before vdpa_init", args: []string{"-V"},
			expected: result{stdout: Version + "\n", pending: 1}},
		{description: "positive: --Ver is an unambiguous long-option prefix", args: []string{"--Ver"},
			expected: result{stdout: Version + "\n", pending: 1}},
		{description: "negative: an unknown short option", args: []string{"-x", "dev"},
			expected: result{stderr: "vdpa: invalid option -- 'x'\nUnknown option.\n" + usageMain, rc: 1, pending: 1}},
		{description: "negative: an unknown long option", args: []string{"--nope"},
			expected: result{stderr: "vdpa: unrecognized option '--nope'\nUnknown option.\n" + usageMain, rc: 1, pending: 1}},
		{description: "corner: -s is in the optstring but main has no case for it", args: []string{"-s"},
			expected: result{stderr: "Unknown option.\n" + usageMain, rc: 1, pending: 1}},
		{description: "corner: options may follow the object (GNU permutation)", args: []string{"mgmtdev", "-V"},
			expected: result{stdout: Version + "\n", pending: 1}},
		{description: "corner: an empty word matches no object", args: []string{""},
			expected: result{stderr: "Object \"\" not found\n", rc: 1, pending: 1}},
		{description: "positive: mgmtdev alone prints its usage", args: []string{"mgmtdev"},
			expected: result{stderr: "Usage: vdpa mgmtdev show [ DEV ]\n", pending: 1}},
		{description: "corner: abbreviated object and help", args: []string{"mg", "h"},
			expected: result{stderr: "Usage: vdpa mgmtdev show [ DEV ]\n", pending: 1}},
		{description: "negative: an unknown mgmtdev command", args: []string{"mgmtdev", "bogus"},
			expected: result{stderr: "Command \"bogus\" not found\n", rc: 1, pending: 1}},
		{description: "negative: vstats with no command prints its usage and fails", args: []string{"dev", "vstats"},
			expected: result{stderr: usageVstats, rc: 1, pending: 1}},
		{description: "positive: vstats help", args: []string{"dev", "vstats", "help"},
			expected: result{stderr: usageVstats, pending: 1}},
		{description: "negative: vstats is strcmp, so a prefix is not it", args: []string{"dev", "vstat"},
			expected: result{stderr: "Command \"vstat\" not found\n", rc: 1, pending: 1}},
		{description: "boundary: vstats show takes 1 or 3 words, not 2", args: []string{"dev", "vstats", "show", "vdpa0", "qidx"},
			expected: result{stderr: usageVstats, rc: 1, pending: 1}},
		{description: "boundary: vstats show takes 1 or 3 words, not 0", args: []string{"dev", "vstats", "show"},
			expected: result{stderr: usageVstats, rc: 1, pending: 1}},
		{description: "negative: a vstats word other than qidx", args: []string{"dev", "vstats", "show", "vdpa0", "foo", "1"},
			expected: result{stderr: "Unknown option \"foo\"\n", rc: 1, pending: 1}},
		{description: "negative: a bad qidx fails silently (get_u32)", args: []string{"dev", "vstats", "show", "vdpa0", "qidx", "x"},
			expected: result{rc: 1, pending: 1}},
		{description: "negative: two words after a dev show handle", args: []string{"dev", "show", "vdpa0", "a", "b"},
			expected: result{stderr: "Unknown option \"a\"\n", rc: 1, pending: 1}},
		{description: "corner: -j and a failure before the section still prints jsonw_destroy's newline",
			args:     []string{"-j", "dev", "vstats"},
			expected: result{stdout: "\n", stderr: usageVstats, rc: 1, pending: 1}},
		{description: "corner: a lone word after the handle is never parsed (NEXT_ARG_OK)", row: "ec-003",
			args:     []string{"dev", "show", "vdpa0", "x"},
			expected: result{stdout: "vdpa0: type network mgmtdev vdpasim_net vendor_id 0 max_vqs 3 max_vq_size 256\n"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			row := tc.row
			if row == "" {
				row = "ec-001"
			}
			got := replay(t, firstRow(t, row), tc.args...)
			if got.stdout != tc.expected.stdout || got.stderr != tc.expected.stderr || got.rc != tc.expected.rc ||
				(got.pending > 0) != (tc.expected.pending > 0) {
				t.Fatalf("vdpa %q\n got  %#v\n want %#v", tc.args, got, tc.expected)
			}
		})
	}
}

// TestGovdpaRefusals pins the verbs govdpa does not clone: refused, rc 1,
// nothing sent past the family resolve.
func TestGovdpaRefusals(t *testing.T) {
	tests := []struct {
		description string
		args        []string
		expected    string
	}{
		{"negative: dev add is a write", []string{"dev", "add", "name", "x", "mgmtdev", "vdpasim_net"}, "govdpa: dev add is not implemented\n"},
		{"negative: dev del is a write", []string{"dev", "del", "vdpa0"}, "govdpa: dev del is not implemented\n"},
		{"negative: dev set is a write", []string{"dev", "set", "vdpa0"}, "govdpa: dev set is not implemented\n"},
		{"boundary: a targeted mgmtdev show has no capture", []string{"mgmtdev", "show", "vdpasim_net"}, "govdpa: mgmtdev show DEV is not implemented\n"},
		{"corner: an empty DEV the builder cannot express", []string{"dev", "show", ""}, "govdpa: an empty DEV is not implemented\n"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := replay(t, firstRow(t, "ec-001"), tc.args...)
			if got.rc != 1 || got.stderr != tc.expected || got.stdout != "" {
				t.Fatalf("got %#v, want rc 1, stderr %q", got, tc.expected)
			}
		})
	}
}

func TestGetU32(t *testing.T) {
	tests := []struct {
		description string
		in          string
		expected    uint32
		ok          bool
	}{
		{"positive: zero", "0", 0, true},
		{"positive: a queue index", "2", 2, true},
		{"boundary: the largest u32", "4294967295", 4294967295, true},
		{"boundary: one past the largest u32", "4294967296", 0, false},
		{"negative: empty", "", 0, false},
		{"negative: trailing junk", "7x", 0, false},
		{"negative: hex is not base 10", "0x10", 0, false},
		{"negative: a sign alone", "-", 0, false},
		{"negative: past ULONG_MAX", "99999999999999999999", 0, false},
		{"corner: strtoul skips leading space", " \t7", 7, true},
		{"corner: a plus sign", "+7", 7, true},
		{"corner: -0 wraps to 0", "-0", 0, true},
		{"corner: -1 wraps past the u32 bound", "-1", 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, ok := getU32(tc.in)
			if got != tc.expected || ok != tc.ok {
				t.Fatalf("getU32(%q) = %d, %v; want %d, %v", tc.in, got, ok, tc.expected, tc.ok)
			}
		})
	}
}

func attr(typ uint16, data []byte) xtcpnl.NetlinkAttribute {
	return xtcpnl.NetlinkAttribute{Type: typ, Data: data}
}

func le16(v uint16) []byte { return binary.LittleEndian.AppendUint16(nil, v) }
func le32(v uint32) []byte { return binary.LittleEndian.AppendUint32(nil, v) }
func le64(v uint64) []byte { return binary.LittleEndian.AppendUint64(nil, v) }

// TestVstatsWalk pins pr_out_dev_net_vstats' single pass: queue_type from the
// MAX_VQP and features seen so far, the u16 read of QUEUE_INDEX, and the
// NULL-name value.
func TestVstatsWalk(t *testing.T) {
	ctrl := le64(1 << virtioNetFCtrlVQ)
	tests := []struct {
		description string
		attrs       []xtcpnl.NetlinkAttribute
		expected    string
	}{
		{"positive: the kernel's order without MAX_VQP: qidx 0 is control_vq",
			[]xtcpnl.NetlinkAttribute{attr(vdpaAttrNegFeatures, ctrl), attr(vdpaAttrQueueIndex, le32(0))}, "queue_type control_vq "},
		{"positive: MAX_VQP 1 makes qidx 2 the control vq",
			[]xtcpnl.NetlinkAttribute{attr(vdpaAttrMaxVQP, le16(1)), attr(vdpaAttrNegFeatures, ctrl), attr(vdpaAttrQueueIndex, le32(2))}, "queue_type control_vq "},
		{"positive: an odd qidx is tx",
			[]xtcpnl.NetlinkAttribute{attr(vdpaAttrNegFeatures, ctrl), attr(vdpaAttrQueueIndex, le32(1))}, "queue_type tx "},
		{"negative: without CTRL_VQ, qidx 0 is rx",
			[]xtcpnl.NetlinkAttribute{attr(vdpaAttrNegFeatures, le64(0)), attr(vdpaAttrQueueIndex, le32(0))}, "queue_type rx "},
		{"boundary: a MAX_VQP after QUEUE_INDEX is not yet seen",
			[]xtcpnl.NetlinkAttribute{attr(vdpaAttrNegFeatures, ctrl), attr(vdpaAttrQueueIndex, le32(2)), attr(vdpaAttrMaxVQP, le16(1))}, "queue_type rx "},
		{"corner: qidx 65537 reads as the u16 1",
			[]xtcpnl.NetlinkAttribute{attr(vdpaAttrQueueIndex, le32(65537))}, "queue_type tx "},
		{"positive: a name/value pair",
			[]xtcpnl.NetlinkAttribute{attr(vdpaAttrVendorName, []byte("rx packets\x00")), attr(vdpaAttrVendorValue, le64(3))}, "rx packets 3 "},
		{"corner: a value no name preceded prints (null)",
			[]xtcpnl.NetlinkAttribute{attr(vdpaAttrVendorValue, le64(5))}, "(null) 5 "},
		{"corner: a name is reused by the next value",
			[]xtcpnl.NetlinkAttribute{attr(vdpaAttrVendorName, []byte("a\x00")), attr(vdpaAttrVendorValue, le64(1)), attr(vdpaAttrVendorValue, le64(2))}, "a 1 a 2 "},
		{"corner: nla_type is compared raw, so a flagged QUEUE_INDEX is skipped",
			[]xtcpnl.NetlinkAttribute{attr(vdpaAttrQueueIndex|0x8000, le32(0))}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var b bytes.Buffer
			w := bufio.NewWriter(&b)
			r := &runner{o: &out{w: w}}
			r.vstatsWalk(tc.attrs)
			if err := w.Flush(); err != nil {
				t.Fatal(err)
			}
			if b.String() != tc.expected {
				t.Fatalf("got %q, want %q", b.String(), tc.expected)
			}
		})
	}
}

// tlv is one netlink attribute, padded.
func tlv(typ uint16, p []byte) []byte {
	n := 4 + len(p)
	b := binary.LittleEndian.AppendUint16(nil, uint16(n))
	b = binary.LittleEndian.AppendUint16(b, typ)
	b = append(b, p...)
	for len(b)%4 != 0 {
		b = append(b, 0)
	}
	return b
}

// errBody is a struct nlmsgerr (error, then the echoed request header whose
// nlmsg_len is reqLen) followed by the echoed payload and the TLVs.
func errBody(errno int32, reqLen int, tlvs ...[]byte) []byte {
	b := binary.LittleEndian.AppendUint32(nil, uint32(errno))
	hdr := make([]byte, netlinkHeaderLen)
	binary.LittleEndian.PutUint32(hdr, uint32(reqLen))
	b = append(b, hdr...)
	b = append(b, make([]byte, max(reqLen-netlinkHeaderLen, 0))...)
	for _, t := range tlvs {
		b = append(b, t...)
	}
	return b
}

// TestRunReply pins mnl_cb_run2 with mnlu_utils' control callbacks and
// nl_dump_ext_ack's printing.
func TestRunReply(t *testing.T) {
	const tlvsCapped = unix.NLM_F_ACK_TLVS | unix.NLM_F_CAPPED
	msgTLV := func(s string) []byte { return tlv(nlmsgerrAttrMsg, append([]byte(s), 0)) }
	done := Msg{Type: unix.NLMSG_DONE, Body: le32(0)}
	tests := []struct {
		description string
		msgs        []Msg
		errno       unix.Errno // 0 with wantErr false: success
		replayErr   bool
		stderr      string
	}{
		{description: "positive: an ack stops cleanly",
			msgs: []Msg{{Type: unix.NLMSG_ERROR, Flags: unix.NLM_F_CAPPED, Body: errBody(0, netlinkHeaderLen)}}},
		{description: "positive: a dump's NLMSG_DONE stops cleanly", msgs: []Msg{done}},
		{description: "negative: an error with an extack message gains a full stop",
			msgs:   []Msg{{Type: unix.NLMSG_ERROR, Flags: tlvsCapped, Body: errBody(-19, netlinkHeaderLen, msgTLV("vdpa: device not found"))}},
			errno:  unix.ENODEV,
			stderr: "Error: vdpa: device not found.\n"},
		{description: "corner: a message already ending in a full stop gains none",
			msgs:   []Msg{{Type: unix.NLMSG_ERROR, Flags: tlvsCapped, Body: errBody(-22, netlinkHeaderLen, msgTLV("bad."))}},
			errno:  unix.EINVAL,
			stderr: "Error: bad.\n"},
		{description: "negative: an error without NLM_F_ACK_TLVS prints no extack",
			msgs:  []Msg{{Type: unix.NLMSG_ERROR, Flags: unix.NLM_F_CAPPED, Body: errBody(-22, netlinkHeaderLen, msgTLV("hidden"))}},
			errno: unix.EINVAL},
		{description: "corner: a positive errno is read the same",
			msgs:  []Msg{{Type: unix.NLMSG_ERROR, Flags: unix.NLM_F_CAPPED, Body: errBody(34, netlinkHeaderLen)}},
			errno: unix.ERANGE},
		{description: "corner: an ack with a message is a warning and still succeeds",
			msgs:   []Msg{{Type: unix.NLMSG_ERROR, Flags: tlvsCapped, Body: errBody(0, netlinkHeaderLen, msgTLV("careful"))}},
			stderr: "Warning: careful.\n"},
		{description: "boundary: uncapped, the TLVs follow the echoed request",
			msgs:   []Msg{{Type: unix.NLMSG_ERROR, Flags: unix.NLM_F_ACK_TLVS, Body: errBody(-22, netlinkHeaderLen+8, msgTLV("x"))}},
			errno:  unix.EINVAL,
			stderr: "Error: x.\n"},
		{description: "boundary: an NLMSG_ERROR shorter than struct nlmsgerr stops",
			msgs: []Msg{{Type: unix.NLMSG_ERROR, Body: le32(0xffffffea)}}},
		{description: "negative: an extack attribute past NLMSGERR_ATTR_MAX",
			msgs:   []Msg{{Type: unix.NLMSG_ERROR, Flags: tlvsCapped, Body: errBody(-22, netlinkHeaderLen, tlv(7, le32(0)))}},
			errno:  unix.EINVAL,
			stderr: "Invalid extack attribute\n"},
		{description: "negative: an extack message without its NUL fails validation",
			msgs:   []Msg{{Type: unix.NLMSG_ERROR, Flags: tlvsCapped, Body: errBody(-22, netlinkHeaderLen, tlv(nlmsgerrAttrMsg, []byte("abcd")))}},
			errno:  unix.EINVAL,
			stderr: "extack attribute 1 failed validation\n"},
		{description: "positive: a missing-attribute extack",
			msgs:   []Msg{{Type: unix.NLMSG_ERROR, Flags: tlvsCapped, Body: errBody(-22, netlinkHeaderLen, tlv(nlmsgerrAttrMissType, le32(17)))}},
			errno:  unix.EINVAL,
			stderr: "Missing required attribute type 17\n"},
		{description: "negative: a dump that ends in error, with its extack",
			msgs:   []Msg{{Type: unix.NLMSG_DONE, Body: append(le32(0xffffffea), msgTLV("dump failed")...)}},
			errno:  unix.EINVAL,
			stderr: "Error: dump failed.\n"},
		{description: "corner: NLM_F_DUMP_INTR is EINTR",
			msgs:  []Msg{{Type: unix.NLMSG_DONE, Flags: unix.NLM_F_DUMP_INTR, Body: le32(0)}},
			errno: unix.EINTR},
		{description: "corner: NOOP and OVERRUN are skipped",
			msgs: []Msg{{Type: unix.NLMSG_NOOP}, {Type: unix.NLMSG_OVERRUN}, done}},
		{description: "negative: a reply with no terminating message is a replay error",
			msgs: []Msg{{Type: unix.NLMSG_NOOP}}, replayErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var errb bytes.Buffer
			r := &runner{o: &out{}, stderr: &errb}
			err := r.runReply(Reply{Msgs: tc.msgs}, nil)
			var ce *cbError
			switch {
			case tc.replayErr:
				if !errors.Is(err, ErrReplay) {
					t.Fatalf("err %v, want ErrReplay", err)
				}
			case tc.errno != 0:
				if !errors.As(err, &ce) || ce.errno != tc.errno {
					t.Fatalf("err %v, want errno %v", err, tc.errno)
				}
			default:
				if err != nil {
					t.Fatalf("err %v, want nil", err)
				}
			}
			if errb.String() != tc.stderr {
				t.Fatalf("stderr %q, want %q", errb.String(), tc.stderr)
			}
		})
	}
}

// TestRunReplyCallback pins a data callback's MNL_CB_ERROR: the run stops
// there, and vdpa's strerror reads an errno of 0.
func TestRunReplyCallback(t *testing.T) {
	tests := []struct {
		description string
		ok          []bool // the callback's answer per data message
		expected    int    // messages the callback saw
		fails       bool
	}{
		{"positive: every message is rendered", []bool{true, true}, 2, false},
		{"negative: the first MNL_CB_ERROR stops the run", []bool{false, true}, 1, true},
		{"boundary: no data messages", nil, 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var msgs []Msg
			for range tc.ok {
				msgs = append(msgs, Msg{Type: 40})
			}
			msgs = append(msgs, Msg{Type: unix.NLMSG_DONE, Body: le32(0)})
			seen := 0
			r := &runner{o: &out{}, stderr: &bytes.Buffer{}}
			err := r.runReply(Reply{Msgs: msgs}, func(Msg) bool {
				seen++
				return tc.ok[seen-1]
			})
			var ce *cbError
			if seen != tc.expected || (err != nil) != tc.fails || (tc.fails && (!errors.As(err, &ce) || ce.errno != 0)) {
				t.Fatalf("seen %d err %v; want %d, fails %v", seen, err, tc.expected, tc.fails)
			}
		})
	}
}

// TestPrintFeatures pins print_features' naming: transport bits from
// ext_feature_strs whatever the device, the rest from net's table only for
// dev_id 1, and bit_N for a bit no table names.
func TestPrintFeatures(t *testing.T) {
	tests := []struct {
		description string
		features    uint64
		mgmtdevf    bool
		devID       uint16
		expected    string
	}{
		{"positive: net names and transport names", 1<<0 | 1<<3 | 1<<32, false, virtioIDNet,
			"\n  negotiated_features CSUM MTU VERSION_1"},
		{"positive: the mgmtdev array is dev_features", 1 << 5, true, virtioIDNet,
			"\n  dev_features MAC"},
		{"negative: a hole in net's table is bit_N", 1 << 4, false, virtioIDNet,
			"\n  negotiated_features bit_4"},
		{"negative: dev_id 0 has no table, but transport bits still name", 1<<0 | 1<<33, false, 0,
			"\n  negotiated_features bit_0 ACCESS_PLATFORM"},
		{"boundary: the first and last transport bits", 1<<28 | 1<<42, false, virtioIDNet,
			"\n  negotiated_features RING_INDIRECT_DESC bit_42"},
		{"boundary: bit 63", 1 << 63, false, virtioIDNet,
			"\n  negotiated_features SPEED_DUPLEX"},
		{"corner: an unnamed transport bit", 1 << 39, false, virtioIDNet,
			"\n  negotiated_features bit_39"},
		{"corner: no features is an empty array", 0, false, virtioIDNet,
			"\n  negotiated_features"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var b bytes.Buffer
			w := bufio.NewWriter(&b)
			o := &out{w: w}
			o.printFeatures(tc.features, tc.mgmtdevf, tc.devID)
			if err := w.Flush(); err != nil {
				t.Fatal(err)
			}
			if b.String() != tc.expected {
				t.Fatalf("got %q, want %q", b.String(), tc.expected)
			}
		})
	}
}

// genlBody is a genlmsghdr (cmd, version 1) and attributes.
func genlBody(cmd uint8, tlvs ...[]byte) []byte {
	b := []byte{cmd, 1, 0, 0}
	for _, t := range tlvs {
		b = append(b, t...)
	}
	return b
}

// TestMgmtdevCB pins pr_out_mgmtdev_show on replies the corpus lacks: the
// class loop from bit 1, a bus-qualified handle, the missing-name refusal,
// and the JSON form.
func TestMgmtdevCB(t *testing.T) {
	name := tlv(2, []byte("x\x00"))
	tests := []struct {
		description string
		body        []byte
		json        bool
		expected    string
		ok          bool
	}{
		{"positive: classes in bit order, unknown ones named as such",
			genlBody(1, name, tlv(3, le64(1<<2|1<<3))), false,
			"x: \n  supported_classes block < unknown class >\n", true},
		{"boundary: class bit 0 is never printed",
			genlBody(1, name, tlv(3, le64(1<<0|1<<1))), false,
			"x: \n  supported_classes net\n", true},
		{"positive: a bus name qualifies the handle",
			genlBody(1, tlv(1, []byte("pci\x00")), name, tlv(15, le32(4))), false,
			"pci/x: \n  max_supported_vqs 4\n", true},
		{"corner: features without the net class have no net names",
			genlBody(1, name, tlv(16, le64(1<<5|1<<32))), false,
			"x: \n  dev_features bit_5 VERSION_1\n", true},
		{"positive: JSON",
			genlBody(1, name, tlv(3, le64(1<<1)), tlv(15, le32(3))), true,
			`{"x":{"supported_classes":["net"],"max_supported_vqs":3}`, true},
		{"negative: no MGMTDEV_DEV_NAME is MNL_CB_ERROR",
			genlBody(1, tlv(3, le64(1<<1))), false, "", false},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var b bytes.Buffer
			w := bufio.NewWriter(&b)
			r := &runner{o: &out{w: w, json: tc.json}}
			if tc.json {
				r.o.newJSONObjPlain()
				r.o.openJSONObject(nil)
			}
			ok := r.mgmtdevCB(Msg{Type: 40, Body: tc.body})
			if err := w.Flush(); err != nil {
				t.Fatal(err)
			}
			if ok != tc.ok || b.String() != tc.expected {
				t.Fatalf("got %q, %v; want %q, %v", b.String(), ok, tc.expected, tc.ok)
			}
		})
	}
}
