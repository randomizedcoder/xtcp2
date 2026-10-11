package xtcpnl

import (
	"encoding/binary"
	"errors"
	"os"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

// errBody lays out an NLMSG_ERROR body by hand: the int error, an echoed
// nlmsghdr of reqLen bytes, the echoed payload, then TLVs.
func errBody(errno int32, reqLen uint32, payload []byte, tlvs ...[]byte) []byte {
	b := binary.LittleEndian.AppendUint32(nil, uint32(errno)) //nolint:gosec // the wire int
	b = binary.LittleEndian.AppendUint32(b, reqLen)
	b = binary.LittleEndian.AppendUint16(b, unix.RTM_NEWLINK)
	b = binary.LittleEndian.AppendUint16(b, unix.NLM_F_REQUEST|unix.NLM_F_ACK)
	b = binary.LittleEndian.AppendUint32(b, 7) // seq
	b = binary.LittleEndian.AppendUint32(b, 0) // pid
	b = append(b, payload...)
	for _, t := range tlvs {
		b = append(b, t...)
	}
	return b
}

func errTLV(atype uint16, payload []byte) []byte {
	b := binary.LittleEndian.AppendUint16(nil, uint16(4+len(payload))) //nolint:gosec // test sizes
	b = binary.LittleEndian.AppendUint16(b, atype)
	b = append(b, payload...)
	for len(b)%4 != 0 {
		b = append(b, 0)
	}
	return b
}

// TestParseNlMsgErr covers the NLMSG_ERROR layouts netlink_ack writes.
// TestDecodeNetlinkErrorExtack runs the same decoder over the captured
// refusals.
//
// go test ./pkg/xtcpnl/ -run TestParseNlMsgErr
func TestParseNlMsgErr(t *testing.T) {
	const (
		capped = uint16(unix.NLM_F_CAPPED)
		tlvs   = uint16(unix.NLM_F_ACK_TLVS)
		eperm  = -int32(unix.EPERM)
	)
	msg := errTLV(unix.NLMSGERR_ATTR_MSG, []byte("Device can not be enslaved while up\x00"))
	offs := errTLV(unix.NLMSGERR_ATTR_OFFS, binary.LittleEndian.AppendUint32(nil, 32))
	type result struct {
		e   NlMsgErr
		err error
	}
	base := func(errno int32, isCapped bool) NlMsgErr {
		return NlMsgErr{Errno: errno, RequestType: unix.RTM_NEWLINK,
			RequestFlags: unix.NLM_F_REQUEST | unix.NLM_F_ACK, RequestSeq: 7, Capped: isCapped}
	}
	withMsg := func(e NlMsgErr) NlMsgErr {
		e.Msg, e.HasMsg = "Device can not be enslaved while up", true
		return e
	}
	tests := []struct {
		description string
		flags       uint16
		body        []byte
		expected    result
	}{
		{description: "positive: a capped ACK", flags: capped, body: errBody(0, 40, nil),
			expected: result{e: base(0, true)}},
		{description: "positive: a capped refusal with a message", flags: capped | tlvs,
			body:     errBody(eperm, 40, nil, msg),
			expected: result{e: withMsg(base(eperm, true))}},
		{description: "positive: message and offset", flags: capped | tlvs,
			body: errBody(eperm, 40, nil, msg, offs),
			expected: result{e: func() NlMsgErr {
				e := withMsg(base(eperm, true))
				e.Offset, e.HasOffset = 32, true
				return e
			}()}},
		{description: "corner: uncapped, the echoed payload is skipped before the TLVs", flags: tlvs,
			body:     errBody(eperm, 16+24, make([]byte, 24), msg),
			expected: result{e: withMsg(base(eperm, false))}},
		{description: "corner: an uncapped payload is skipped to its aligned end", flags: tlvs,
			body:     errBody(eperm, 16+21, make([]byte, 24), msg),
			expected: result{e: withMsg(base(eperm, false))}},
		{description: "corner: TLVs present but the flag absent are not read", flags: capped,
			body:     errBody(eperm, 40, nil, msg),
			expected: result{e: base(eperm, true)}},
		{description: "corner: an unknown TLV is skipped", flags: capped | tlvs,
			body:     errBody(eperm, 40, nil, errTLV(unix.NLMSGERR_ATTR_COOKIE, []byte{1, 2, 3, 4}), msg),
			expected: result{e: withMsg(base(eperm, true))}},
		{description: "boundary: the fixed part and nothing else", flags: capped,
			body: errBody(eperm, 16, nil), expected: result{e: base(eperm, true)}},
		{description: "boundary: one byte short of the fixed part", flags: capped,
			body: errBody(eperm, 16, nil)[:nlMsgErrFixedCst-1], expected: result{err: ErrNlMsgErrShort}},
		{description: "negative: an uncapped body shorter than its echoed request", flags: 0,
			body: errBody(eperm, 16+24, make([]byte, 8)), expected: result{err: ErrNlMsgErrShort}},
		{description: "negative: an echoed length below a header", flags: 0,
			body: errBody(eperm, 8, nil), expected: result{err: ErrNlMsgErrShort}},
		{description: "negative: empty", body: nil, expected: result{err: ErrNlMsgErrShort}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := ParseNlMsgErr(tc.flags, tc.body)
			if tc.expected.err != nil {
				if !errors.Is(err, tc.expected.err) {
					t.Fatalf("err = %v, want %v", err, tc.expected.err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.expected.e {
				t.Fatalf("got %+v\nwant %+v", got, tc.expected.e)
			}
		})
	}
}

// lastNlMsgErr returns the last NLMSG_ERROR in a capture, which in a
// link-write row is the kernel's answer to the write, with the request it
// answers.
func lastNlMsgErr(t *testing.T, path string) (flags uint16, body, request []byte) {
	t.Helper()
	bs, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_, recs, err := ParseNetlinkPcap(bs)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	for _, r := range recs {
		_, p, perr := r.NetlinkPayload()
		if perr != nil {
			t.Fatalf("%s: %v", path, perr)
		}
		for len(p) >= NlMsgHdrSizeCst {
			var h NlMsgHdr
			if _, err := DeserializeNlMsgHdr(p, &h); err != nil {
				t.Fatalf("%s: %v", path, err)
			}
			mlen := int(h.Len)
			if mlen < NlMsgHdrSizeCst || mlen > len(p) {
				t.Fatalf("%s: nlmsg_len %d in a %d-byte datagram", path, mlen, len(p))
			}
			switch {
			case h.Flags&unix.NLM_F_REQUEST != 0:
				request = p[:mlen]
			case h.Type == unix.NLMSG_ERROR:
				flags, body = h.Flags, p[NlMsgHdrSizeCst:mlen]
			default:
				// a lookup's RTM_NEWLINK reply
			}
			if adv := nlmsgAlign(mlen); adv < len(p) {
				p = p[adv:]
			} else {
				p = nil
			}
		}
	}
	if body == nil {
		t.Fatalf("%s: no NLMSG_ERROR", path)
	}
	return flags, body, request
}

// TestDecodeNetlinkErrorExtack runs ParseNlMsgErr over the kernel's answer
// to every write in the link-write corpus: the ACKs, and the refusals whose
// extack message is what `ip` printed after "Error:" in ip.err.
//
// offAttr is the attribute type found at NLMSGERR_ATTR_OFFS in the request
// `ip` sent, the one the kernel objected to; 0 when there is no offset.
//
// go test ./pkg/xtcpnl/ -run TestDecodeNetlinkErrorExtack
func TestDecodeNetlinkErrorExtack(t *testing.T) {
	type result struct {
		errno   syscall.Errno // 0 for an ACK
		msg     string
		offAttr uint16
	}
	tests := []struct {
		description string
		slug        string
		expected    result
	}{
		{description: "positive: add dummy is ACKed", slug: "add_dummy"},
		{description: "positive: add veth is ACKed", slug: "add_veth"},
		{description: "positive: add bare bond is ACKed", slug: "add_bond_bare"},
		{description: "positive: worked example 1 is ACKed, no TLVs", slug: "add_bond_8023ad"},
		{description: "positive: every 802.3ad option is ACKed", slug: "add_bond_all_lacp"},
		{description: "positive: active-backup with ARP targets is ACKed", slug: "add_bond_ab_arp"},
		{description: "positive: numeric mode is ACKed", slug: "add_bond_numeric_mode"},
		{description: "positive: enslave while down is ACKed", slug: "enslave_down"},
		{description: "positive: nomaster is ACKed", slug: "nomaster"},
		{description: "positive: bond_slave queue_id is ACKed", slug: "slave_queue_id"},
		{description: "positive: bond_slave actor_port_prio is ACKed", slug: "slave_actor_port_prio"},
		{description: "positive: set up is ACKed", slug: "set_up"},
		{description: "positive: set down is ACKed", slug: "set_down"},
		{description: "positive: rename, mtu and alias are ACKed", slug: "rename_mtu_alias"},
		{description: "positive: RTM_DELLINK is ACKed", slug: "del_bond"},
		{description: "negative: enslave while up, a message with no offset", slug: "enslave_up",
			expected: result{errno: unix.EPERM, msg: "Device can not be enslaved while up"}},
		{description: "negative: lacp_rate on an up bond points at LACP_RATE", slug: "set_lacp_rate_up",
			expected: result{errno: unix.EBUSY, msg: "unable to set option because the bond is up",
				offAttr: unix.IFLA_BOND_AD_LACP_RATE}},
		{description: "negative: mode with ports points at MODE", slug: "set_mode_with_ports",
			expected: result{errno: unix.ENOTEMPTY, msg: "unable to set option because the bond device has slaves",
				offAttr: unix.IFLA_BOND_MODE}},
		{description: "negative: lacp_rate in balance-rr points at LACP_RATE", slug: "set_lacp_rate_rr",
			expected: result{errno: unix.EACCES, msg: "option not supported in mode",
				offAttr: unix.IFLA_BOND_AD_LACP_RATE}},
		{description: "negative: arp_interval with miimon points at ARP_INTERVAL", slug: "arp_and_miimon",
			expected: result{errno: unix.EINVAL, msg: "ARP monitoring cannot be used with MII monitoring",
				offAttr: unix.IFLA_BOND_ARP_INTERVAL}},
		{description: "corner: replace refused with a bare errno and no TLVs at all", slug: "replace_existing",
			expected: result{errno: unix.EOPNOTSUPP}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			flags, body, request := lastNlMsgErr(t, tdLinkWrite_7_1_4+"/write/"+tc.slug+"/ip.pcap")
			e, err := ParseNlMsgErr(flags, body)
			if err != nil {
				t.Fatal(err)
			}
			var got result
			if e.Errno != 0 {
				got.errno = syscall.Errno(-e.Errno)
			}
			got.msg = e.Msg
			if e.HasOffset {
				off := int(e.Offset)
				if off+4 > len(request) {
					t.Fatalf("offset %d past the %d-byte request", off, len(request))
				}
				got.offAttr = binary.LittleEndian.Uint16(request[off+2:off+4]) & NlaTypeMaskCst
			}
			if got != tc.expected {
				t.Errorf("got %+v, want %+v", got, tc.expected)
			}
			// An ACK is always capped and carries no TLVs; a refusal on
			// iproute2's socket is never capped.
			if e.Capped != (e.Errno == 0) {
				t.Errorf("Capped = %v for errno %d", e.Capped, e.Errno)
			}
			if e.HasMsg != (tc.expected.msg != "") {
				t.Errorf("HasMsg = %v, want %v", e.HasMsg, tc.expected.msg != "")
			}
			if seq := binary.LittleEndian.Uint32(request[8:12]); e.RequestSeq != seq {
				t.Errorf("answers seq %d, the write was seq %d", e.RequestSeq, seq)
			}
			// What `ip` printed: the extack message as "Error: %s.\n"
			// (print_ext_ack_msg, lib/libnetlink.c:72), and nothing on an ACK.
			stderr, err := os.ReadFile(tdLinkWrite_7_1_4 + "/write/" + tc.slug + "/ip.err")
			if err != nil {
				t.Fatal(err)
			}
			if e.HasMsg && string(stderr) != "Error: "+e.Msg+".\n" {
				t.Errorf("ip.err = %q, want the decoded message %q", stderr, e.Msg)
			}
			if e.Errno == 0 && len(stderr) != 0 {
				t.Errorf("ip.err = %q after an ACK", stderr)
			}
		})
	}
}
