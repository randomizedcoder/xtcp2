package xtcpnl

import (
	"encoding/binary"
	"os"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

// The XFRM policy, event and SPD message types and attributes
// (linux/xfrm.h) that C3-T02 adds to xtcpnl_xfrm_state_test.go's.
const (
	xfrmMsgNewPolicy   = 19
	xfrmMsgDelPolicy   = 20
	xfrmMsgGetPolicy   = 21
	xfrmMsgAcquire     = 23
	xfrmMsgExpire      = 24
	xfrmMsgUpdPolicy   = 25
	xfrmMsgPolExpire   = 27
	xfrmMsgFlushPolicy = 29
	xfrmMsgNewAE       = 30
	xfrmMsgNewSPDInfo  = 36
	xfrmMsgGetSPDInfo  = 37
	xfrmMsgMapping     = 38
	xfrmMsgSetDefault  = 39
	xfrmMsgGetDefault  = 40

	xfrmAttrEncap      = 4
	xfrmAttrTmpl       = 5
	xfrmAttrSA         = 6
	xfrmAttrPolicy     = 7
	xfrmAttrPolicyType = 16
	xfrmAttrMark       = 21
	xfrmAttrIfID       = 31
	xfrmAttrSADir      = 33

	xfrmAttrSPDInfo        = 1
	xfrmAttrSPDHInfo       = 2
	xfrmAttrSPDIPv4HThresh = 3
	xfrmAttrSPDIPv6HThresh = 4

	// sizeof(struct xfrm_userpolicy_info), (struct xfrm_userpolicy_id),
	// (struct xfrm_user_tmpl), (struct xfrm_user_acquire),
	// (struct xfrm_user_expire), (struct xfrm_user_polexpire),
	// (struct xfrm_aevent_id), (struct xfrm_user_mapping) and
	// (struct xfrm_userpolicy_default) on x86_64, and the offsets in them.
	// The two expires end in a u8 hard flag, padded to the 8 their
	// lifetimes align to.
	xfrmPolicyInfoLen = 168
	xfrmPolicyIDLen   = 64
	xfrmTmplLen       = 64
	xfrmAcquireLen    = 280
	xfrmExpireLen     = 232
	xfrmPolExpireLen  = 176
	xfrmAeventIDLen   = 48
	xfrmMappingLen    = 64
	// struct xfrm_encap_tmpl: a host-order type, network-order ports,
	// and the original address at 8, after 2 bytes of padding.
	xfrmEncapLen       = 24
	xfrmEncapOAOff     = 8
	xfrmPolicyDefLen   = 3
	xfrmPolicyLftOff   = 56
	xfrmPolicyCurOff   = 120
	xfrmAcquirePolOff  = 96
	xfrmAcquireAlgsOff = 264
)

// xfrmSel renders an xfrm_selector: source/prefix-destination/prefix, its
// family and protocol, and a port only where its mask is set.
func xfrmSel(sel []byte) string {
	out := xfrmAddr(sel[16:32]) + "/" + strconv.Itoa(int(sel[43])) + "-" + xfrmAddr(sel[:16]) + "/" +
		strconv.Itoa(int(sel[42])) + ",family=" + strconv.Itoa(int(binary.LittleEndian.Uint16(sel[40:]))) +
		",proto=" + strconv.Itoa(int(sel[44]))
	if binary.BigEndian.Uint16(sel[34:]) != 0 {
		out += ",dport=" + strconv.Itoa(int(binary.BigEndian.Uint16(sel[32:])))
	}
	if binary.BigEndian.Uint16(sel[38:]) != 0 {
		out += ",sport=" + strconv.Itoa(int(binary.BigEndian.Uint16(sel[36:])))
	}
	return out
}

// xfrmLft renders the limits of an xfrm_lifetime_cfg that are neither 0
// nor infinite, as lft{...}, or nothing.
func xfrmLft(b []byte) string {
	var lft []string
	for i, name := range xfrmLftNames {
		if v := binary.LittleEndian.Uint64(b[8*i:]); v != 0 && v != xfrmInf {
			lft = append(lft, name+"="+strconv.FormatUint(v, 10))
		}
	}
	if len(lft) == 0 {
		return ""
	}
	return " lft{" + strings.Join(lft, ",") + "}"
}

// xfrmPolicyInfo renders an xfrm_userpolicy_info: its direction, action,
// priority, index, flags and share, the selector, and the lifetime limits
// that are set. The current lifetime is left out but for its bytes and
// packets, since it holds the time the policy was added.
func xfrmPolicyInfo(b []byte) string {
	return "dir=" + strconv.Itoa(int(b[160])) + " action=" + strconv.Itoa(int(b[161])) +
		" priority=" + u32s(binary.LittleEndian.Uint32(b[152:])) + " index=" + u32s(binary.LittleEndian.Uint32(b[156:])) +
		" flags=" + strconv.Itoa(int(b[162])) + " share=" + strconv.Itoa(int(b[163])) +
		" sel=" + xfrmSel(b[:xfrmSelLen]) + xfrmLft(b[xfrmPolicyLftOff:]) +
		" curlft=" + strconv.FormatUint(binary.LittleEndian.Uint64(b[xfrmPolicyCurOff:]), 10) + "B/" +
		strconv.FormatUint(binary.LittleEndian.Uint64(b[xfrmPolicyCurOff+8:]), 10) + "P"
}

// xfrmPolicyID renders an xfrm_userpolicy_id: the selector, or the index
// where there is one, and the direction.
func xfrmPolicyID(b []byte) string {
	return "sel=" + xfrmSel(b[:xfrmSelLen]) + " index=" + u32s(binary.LittleEndian.Uint32(b[56:])) +
		" dir=" + strconv.Itoa(int(b[60]))
}

// xfrmTmpl renders one xfrm_user_tmpl.
func xfrmTmpl(b []byte) string {
	return "tmpl{dst=" + xfrmAddr(b[:xfrmAddrLen]) + " spi=0x" + strconv.FormatUint(uint64(binary.BigEndian.Uint32(b[16:])), 16) +
		" proto=" + strconv.Itoa(int(b[20])) + " family=" + strconv.Itoa(int(binary.LittleEndian.Uint16(b[24:]))) +
		" src=" + xfrmAddr(b[28:44]) + " reqid=" + u32s(binary.LittleEndian.Uint32(b[44:])) +
		" mode=" + strconv.Itoa(int(b[48])) + " share=" + strconv.Itoa(int(b[49])) +
		" optional=" + strconv.Itoa(int(b[50])) + " algos=" + xfrmAlgos(b[52:]) + "}"
}

// xfrmAlgos renders the three u32 algorithm masks a template or an
// ACQUIRE carries (auth, encryption, compression) in hex.
func xfrmAlgos(b []byte) string {
	return "0x" + strconv.FormatUint(uint64(binary.LittleEndian.Uint32(b)), 16) +
		"/0x" + strconv.FormatUint(uint64(binary.LittleEndian.Uint32(b[4:])), 16) +
		"/0x" + strconv.FormatUint(uint64(binary.LittleEndian.Uint32(b[8:])), 16)
}

// xfrmPolicyAttr renders the XFRMA_* attributes that policies and events
// carry: an SA's UDP encap, templates, the SA a DELSA event nests, the policy a DELPOLICY
// event nests, and the rest a policy can have. An attribute this does not
// know is attr<N>.
func xfrmPolicyAttr(id uint16, d []byte) string {
	switch id {
	case xfrmAttrEncap:
		if len(d) < xfrmEncapLen {
			break
		}
		return "encap=" + strconv.Itoa(int(binary.LittleEndian.Uint16(d))) + "/" +
			strconv.Itoa(int(binary.BigEndian.Uint16(d[2:]))) + "-" +
			strconv.Itoa(int(binary.BigEndian.Uint16(d[4:]))) + "/" + xfrmAddr(d[xfrmEncapOAOff:])
	case xfrmAttrTmpl:
		var out []string
		for off := 0; off+xfrmTmplLen <= len(d); off += xfrmTmplLen {
			out = append(out, xfrmTmpl(d[off:]))
		}
		return strings.Join(out, " ")
	case xfrmAttrSA:
		return "sa{" + xfrmSAInfo(d) + "}"
	case xfrmAttrPolicy:
		return "policy{" + xfrmPolicyInfo(d) + "}"
	case xfrmAttrPolicyType:
		return "ptype=" + strconv.Itoa(int(d[0]))
	case xfrmAttrMark:
		return "mark=" + u32s(binary.LittleEndian.Uint32(d)) + "/" + u32s(binary.LittleEndian.Uint32(d[4:]))
	case xfrmAttrIfID:
		return "if_id=" + u32s(binary.LittleEndian.Uint32(d))
	case xfrmAttrSADir:
		return "sa_dir=" + strconv.Itoa(int(d[0]))
	}
	return "attr" + strconv.Itoa(int(id))
}

// xfrmSPDAttr renders one XFRMA_SPD_* attribute of a NEWSPDINFO.
func xfrmSPDAttr(id uint16, d []byte) string {
	u32 := func(i int) string { return u32s(binary.LittleEndian.Uint32(d[4*i:])) }
	switch id {
	case xfrmAttrSPDInfo:
		return "spd_info=" + u32(0) + "/" + u32(1) + "/" + u32(2) + "/" + u32(3) + "/" + u32(4) + "/" + u32(5)
	case xfrmAttrSPDHInfo:
		return "spd_hinfo=" + u32(0) + "/" + u32(1)
	case xfrmAttrSPDIPv4HThresh:
		return "hthresh4=" + strconv.Itoa(int(d[0])) + "/" + strconv.Itoa(int(d[1]))
	case xfrmAttrSPDIPv6HThresh:
		return "hthresh6=" + strconv.Itoa(int(d[0])) + "/" + strconv.Itoa(int(d[1]))
	}
	return "attr" + strconv.Itoa(int(id))
}

// xfrmPolicyBody renders the body of a policy, event, SPD or default
// message for xfrmMsg: its words, where its attributes start, and their
// numbering. ok is false for a type it does not know, or a body too short
// for its type.
func xfrmPolicyBody(typ uint16, b []byte) (words []string, attrOff int, space xfrmAttrSpace, ok bool) {
	need := map[uint16]int{
		xfrmMsgNewPolicy: xfrmPolicyInfoLen, xfrmMsgUpdPolicy: xfrmPolicyInfoLen,
		xfrmMsgDelPolicy: 0, xfrmMsgGetPolicy: 0, xfrmMsgFlushPolicy: 0,
		xfrmMsgAcquire: xfrmAcquireLen, xfrmMsgExpire: xfrmExpireLen,
		xfrmMsgPolExpire: xfrmPolExpireLen, xfrmMsgNewAE: xfrmAeventIDLen,
		xfrmMsgNewSPDInfo: 4, xfrmMsgGetSPDInfo: 4, xfrmMsgMapping: xfrmMappingLen,
		xfrmMsgSetDefault: xfrmPolicyDefLen, xfrmMsgGetDefault: xfrmPolicyDefLen,
	}
	n, known := need[typ]
	if !known || len(b) < n {
		return nil, 0, xfrmSpaceSA, false
	}
	switch typ {
	case xfrmMsgNewPolicy, xfrmMsgUpdPolicy:
		words = []string{map[uint16]string{xfrmMsgNewPolicy: "newpolicy", xfrmMsgUpdPolicy: "updpolicy"}[typ], xfrmPolicyInfo(b)}
		attrOff = xfrmPolicyInfoLen
	case xfrmMsgGetPolicy, xfrmMsgDelPolicy:
		// A dump has no body; a get or delete, and a DELPOLICY event, an
		// xfrm_userpolicy_id.
		words = []string{map[uint16]string{xfrmMsgGetPolicy: "getpolicy", xfrmMsgDelPolicy: "delpolicy"}[typ]}
		if len(b) >= xfrmPolicyIDLen {
			words = append(words, xfrmPolicyID(b))
			attrOff = xfrmPolicyIDLen
		}
	case xfrmMsgFlushPolicy:
		words = []string{"flushpolicy"}
	case xfrmMsgAcquire:
		// seq is the kernel's acquire counter, which is global, so it is
		// left out.
		words = []string{"acquire", "dst=" + xfrmAddr(b[:xfrmAddrLen]) + " spi=0x" +
			strconv.FormatUint(uint64(binary.BigEndian.Uint32(b[16:])), 16) + " proto=" + strconv.Itoa(int(b[20])) +
			" src=" + xfrmAddr(b[24:40]) + " sel=" + xfrmSel(b[40:40+xfrmSelLen]) +
			" policy{" + xfrmPolicyInfo(b[xfrmAcquirePolOff:]) + "} algos=" + xfrmAlgos(b[xfrmAcquireAlgsOff:])}
		attrOff = xfrmAcquireLen
	case xfrmMsgExpire:
		words = []string{"expire", "hard=" + strconv.Itoa(int(b[xfrmUsersaInfoLen])), xfrmSAInfo(b)}
		attrOff = xfrmExpireLen
	case xfrmMsgPolExpire:
		words = []string{"polexpire", "hard=" + strconv.Itoa(int(b[xfrmPolicyInfoLen])), xfrmPolicyInfo(b)}
		attrOff = xfrmPolExpireLen
	case xfrmMsgNewAE:
		words = []string{"newae", "dst=" + xfrmAddr(b[:xfrmAddrLen]) + " spi=0x" +
			strconv.FormatUint(uint64(binary.BigEndian.Uint32(b[16:])), 16) + " family=" +
			strconv.Itoa(int(binary.LittleEndian.Uint16(b[20:]))) + " proto=" + strconv.Itoa(int(b[22])) +
			" src=" + xfrmAddr(b[24:40]) + " flags=0x" + strconv.FormatUint(uint64(binary.LittleEndian.Uint32(b[40:])), 16) +
			" reqid=" + u32s(binary.LittleEndian.Uint32(b[44:]))}
		attrOff = xfrmAeventIDLen
	case xfrmMsgMapping:
		// The SA, then the NAT-T peer's address and port as the SA has
		// them and as the packet came.
		words = []string{"mapping", "dst=" + xfrmAddr(b[:xfrmAddrLen]) + " spi=0x" +
			strconv.FormatUint(uint64(binary.BigEndian.Uint32(b[16:])), 16) + " family=" +
			strconv.Itoa(int(binary.LittleEndian.Uint16(b[20:]))) + " proto=" + strconv.Itoa(int(b[22])) +
			" reqid=" + u32s(binary.LittleEndian.Uint32(b[24:])) +
			" old=" + xfrmAddr(b[28:44]) + ":" + strconv.Itoa(int(binary.BigEndian.Uint16(b[60:]))) +
			" new=" + xfrmAddr(b[44:60]) + ":" + strconv.Itoa(int(binary.BigEndian.Uint16(b[62:])))}
		attrOff = xfrmMappingLen
	case xfrmMsgGetSPDInfo, xfrmMsgNewSPDInfo:
		words = []string{map[uint16]string{xfrmMsgGetSPDInfo: "getspdinfo", xfrmMsgNewSPDInfo: "newspdinfo"}[typ],
			"flags=0x" + strconv.FormatUint(uint64(binary.LittleEndian.Uint32(b)), 16)}
		attrOff = 4
		space = xfrmSpaceSPD
	case xfrmMsgSetDefault, xfrmMsgGetDefault:
		words = []string{map[uint16]string{xfrmMsgSetDefault: "setdefault", xfrmMsgGetDefault: "getdefault"}[typ],
			"in=" + strconv.Itoa(int(b[0])) + " fwd=" + strconv.Itoa(int(b[1])) + " out=" + strconv.Itoa(int(b[2]))}
		attrOff = xfrmPolicyDefLen
	}
	return words, attrOff, space, true
}

// xfrmAll describes every message in a pcap in the order it came, a
// request as > and anything else as <, with NLMSG_ERROR as its errno and
// NLMSG_DONE as done: for the event rows, whose events come between their
// trigger's requests and ACKs.
func xfrmAll(t *testing.T, rel string) string {
	t.Helper()
	msgs := pcapNlMsgs(t, tdXfrmWrite_7_1_4+"/"+rel)
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		dir := "< "
		if m.h.Flags&unix.NLM_F_REQUEST != 0 {
			dir = "> "
		}
		switch m.h.Type {
		case unix.NLMSG_ERROR:
			e, err := ParseNlMsgErr(m.h.Flags, m.body)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, dir+"error "+strconv.Itoa(int(e.Errno)))
		case unix.NLMSG_DONE:
			out = append(out, dir+"done")
		default:
			out = append(out, dir+xfrmMsg(t, m))
		}
	}
	return strings.Join(out, "|")
}

// xfrmEphemeral is the source port a UDP socket was given, which differs
// from run to run.
var xfrmEphemeral = regexp.MustCompile(`sport[ =][0-9]+`)

// xfrmPolicyRows is every C3-T02 row, in the corpus's order.
var xfrmPolicyRows = []string{
	"policy_list_empty", "policy_add", "policy_add_v6_block", "policy_add_dup", "policy_add_no_dir",
	"policy_add_bad_dir", "policy_update", "policy_update_new", "policy_get", "policy_get_index",
	"policy_get_missing", "policy_get_sel_and_index", "policy_get_no_sel", "policy_delete",
	"policy_delete_missing", "policy_list", "policy_list_dir_in", "policy_deleteall", "policy_deleteall_empty",
	"policy_flush", "policy_flush_empty", "policy_count", "policy_count_empty", "policy_count_stats",
	"policy_set_hthresh", "policy_set_hthresh_bad", "policy_getdefault", "policy_setdefault",
	"policy_setdefault_bad", "monitor_sa_policy", "monitor_acquire", "monitor_expire", "monitor_polexpire",
	"monitor_aevent", "monitor_mapping", "monitor_unknown",
}

// TestXfrmPolicyCorpus checks the C3-T02 rows (testdata/7_1_4/link-write/
// xfrm/, scripts/xfrm-write-corpus.exp; docs/netlink/goip-parity-gaps.md):
// `ip xfrm policy`'s requests and the kernel's answers, and the events
// `ip xfrm monitor` reads.
func TestXfrmPolicyCorpus(t *testing.T) {
	req := func(rel string) func(t *testing.T) string {
		return func(t *testing.T) string { return xfrmMsgs(t, rel, true) }
	}
	replies := func(rel string) func(t *testing.T) string {
		return func(t *testing.T) string { return xfrmMsgs(t, rel, false) }
	}
	all := func(rel string) func(t *testing.T) string {
		return func(t *testing.T) string { return xfrmAll(t, rel) }
	}
	errno := func(slug string) func(t *testing.T) string {
		return func(t *testing.T) string { return xfrmErrno(t, slug) }
	}
	file := func(rel string) func(t *testing.T) string {
		return func(t *testing.T) string { return xfrmFile(t, rel) }
	}
	ack := "0 rc 0"
	enoent := strconv.Itoa(int(syscall.ENOENT)) + " rc 2"

	// P1 as ip sends it (index 0) and as the kernel has it, and P2.
	tmpl := "tmpl{dst=192.0.2.2 spi=0x0 proto=50 family=2 src=192.0.2.1 reqid=1 mode=0 share=0 optional=0 algos=0xffffffff/0xffffffff/0xffffffff}"
	p1Sel := "sel=192.0.2.1/32-192.0.2.2/32,family=2,proto=0"
	p1 := func(index, priority, lft string) string {
		return "dir=1 action=0 priority=" + priority + " index=" + index + " flags=0 share=0 " + p1Sel + lft + " curlft=0B/0P"
	}
	p1ID := p1Sel + " index=0 dir=1"
	p2 := "newpolicy dir=0 action=1 priority=10 index=8 flags=0 share=0 sel=2001:db8::1/128-2001:db8::2/128,family=10,proto=6,dport=443 curlft=0B/0P"
	p1Kernel := "newpolicy " + p1("1", "0", "") + " " + tmpl

	// SA 1 as ip sends it and as the kernel has it.
	sa1 := "dst=192.0.2.2 spi=0x100 proto=50 src=192.0.2.1 family=2 mode=0 reqid=1 seq=0 replay_window=0 flags=0"
	sa1Req := sa1 + " sel=0/0-0/0,family=0,proto=0 curlft=0B/0P auth=sha256/256:11*32 enc=aes/128:22*16"
	sa1Kernel := sa1 + " sel=0/0-0/0,family=2,proto=0 curlft=0B/0P"
	sa1Algs := "auth=hmac(sha256)/256:11*32 auth_trunc=hmac(sha256)/256/96:11*32 enc=cbc(aes)/128:22*16 replay=0/0/0"

	tests := []struct {
		description string
		got         func(t *testing.T) string
		expected    string
	}{
		{"boundary: list on an empty SPD is a dump with no body", req("policy_list_empty/ip.pcap"), "getpolicy dump"},
		{"boundary: answered by NLMSG_DONE alone", replies("policy_list_empty/ip.pcap"), ""},
		{"positive: add is a NEWPOLICY with the selector and template, and no index", req("policy_add/ip.pcap"), "newpolicy ack " + p1("0", "0", "") + " " + tmpl},
		{"positive: answered with an ACK", errno("policy_add"), ack},
		{"positive: and reads back with index 1, the first out policy's", replies("policy_add/netlink_xfrm_policy.pcap"), p1Kernel},
		{"positive: which ip prints", file("policy_add/ip_policy"), "src 192.0.2.1/32 dst 192.0.2.2/32 \n\tdir out priority 0 \n\ttmpl src 192.0.2.1 dst 192.0.2.2\n\t\tproto esp reqid 1 mode transport"},
		{"positive: an IPv6 block with a port and no template", req("policy_add_v6_block/ip.pcap"), "newpolicy ack dir=0 action=1 priority=10 index=0 flags=0 share=0 sel=2001:db8::1/128-2001:db8::2/128,family=10,proto=6,dport=443 curlft=0B/0P"},
		{"corner: and reads back with index 8, the first in policy's", replies("policy_add_v6_block/netlink_xfrm_policy.pcap"), p2},
		{"negative: a policy the kernel already has is EEXIST", errno("policy_add_dup"), strconv.Itoa(int(syscall.EEXIST)) + " rc 2"},
		{"negative: an add with no DIR is refused by ip, which sends nothing", errno("policy_add_no_dir"), "no request rc 1"},
		{"negative: and says why", file("policy_add_no_dir/ip.err"), "Not enough information: DIR is required."},
		{"negative: a bad DIR is invarg, exit(-1)", errno("policy_add_bad_dir"), "no request rc 255"},
		{"negative: and says why", file("policy_add_bad_dir/ip.err"), "Error: argument \"sideways\" is wrong: DIR value is invalid"},
		{"positive: update is an UPDPOLICY with the new priority", req("policy_update/ip.pcap"), "updpolicy ack " + p1("0", "5", "") + " " + tmpl},
		{"positive: and the policy keeps its index", replies("policy_update/netlink_xfrm_policy.pcap"), "newpolicy " + p1("1", "5", "") + " " + tmpl},
		{"corner: an update of a missing policy inserts it", errno("policy_update_new"), ack},
		{"corner: as an add would", replies("policy_update_new/netlink_xfrm_policy.pcap"), p1Kernel},
		{"positive: get is a GETPOLICY by selector and direction", req("policy_get/ip.pcap"), "getpolicy " + p1ID},
		{"positive: answered with the policy", replies("policy_get/ip.pcap"), p1Kernel},
		{"positive: get by index sends an empty IPv4 selector", req("policy_get_index/ip.pcap"), "getpolicy sel=0/0-0/0,family=2,proto=0 index=1 dir=1"},
		{"positive: and is answered the same", replies("policy_get_index/ip.pcap"), p1Kernel},
		{"negative: a get of a missing policy is ENOENT", errno("policy_get_missing"), enoent},
		{"negative: a selector and an index together is duparg2, exit(-1)", errno("policy_get_sel_and_index"), "no request rc 255"},
		{"negative: and says why", file("policy_get_sel_and_index/ip.err"), "Error: either \"SELECTOR\" is duplicate, or \"INDEX\" is garbage."},
		{"negative: a get with neither is refused by ip", errno("policy_get_no_sel"), "no request rc 1"},
		{"negative: and says why", file("policy_get_no_sel/ip.err"), "Not enough information: either SELECTOR or INDEX is required."},
		{"positive: delete is a DELPOLICY, as get", req("policy_delete/ip.pcap"), "delpolicy ack " + p1ID},
		{"positive: and the SPD is then empty", replies("policy_delete/netlink_xfrm_policy.pcap"), ""},
		{"negative: a delete of a missing policy is ENOENT", errno("policy_delete_missing"), enoent},
		{"positive: a dump of two policies, in before out", replies("policy_list/ip.pcap"), p2 + "|" + p1Kernel},
		{"positive: list dir in is the same request", req("policy_list_dir_in/ip.pcap"), "getpolicy dump"},
		{"positive: and the same answer", replies("policy_list_dir_in/ip.pcap"), p2 + "|" + p1Kernel},
		{"positive: which ip filters itself", file("policy_list_dir_in/ip.out"), "src 2001:db8::1/128 dst 2001:db8::2/128 proto tcp dport 443 \n\tdir in action block priority 10"},
		{"positive: deleteall dumps, deletes each policy by index without asking for an ACK, and dumps again", req("policy_deleteall/ip.pcap"), "getpolicy dump|delpolicy sel=2001:db8::1/128-2001:db8::2/128,family=10,proto=6,dport=443 index=8 dir=0|delpolicy " + p1Sel + " index=1 dir=1|getpolicy dump"},
		{"positive: and the SPD is then empty", replies("policy_deleteall/netlink_xfrm_policy.pcap"), ""},
		{"boundary: deleteall with nothing stops after its dump", req("policy_deleteall_empty/ip.pcap"), "getpolicy dump"},
		{"positive: flush is a FLUSHPOLICY with no body", req("policy_flush/ip.pcap"), "flushpolicy ack"},
		{"positive: and the SPD is then empty", replies("policy_flush/netlink_xfrm_policy.pcap"), ""},
		{"boundary: a flush of an empty SPD is ACKed", errno("policy_flush_empty"), ack},
		{"positive: count is a GETSPDINFO", req("policy_count/ip.pcap"), "getspdinfo flags=0xffffffff"},
		{"positive: answered with the counts, the hash table and its thresholds", replies("policy_count/ip.pcap"), "newspdinfo flags=0xffffffff spd_info=1/1/0/0/0/0 spd_hinfo=7/1048576 hthresh4=32/32 hthresh6=128/128"},
		{"positive: which ip prints", file("policy_count/ip.out"), "SPD IN  1 OUT 1 FWD 0"},
		{"boundary: an empty SPD counts 0", replies("policy_count_empty/ip.pcap"), "newspdinfo flags=0xffffffff spd_info=0/0/0/0/0/0 spd_hinfo=7/1048576 hthresh4=32/32 hthresh6=128/128"},
		{"corner: ip -s -s prints the socket counts, the hash table and the thresholds too", file("policy_count_stats/ip.out"), "SPD IN  1 OUT 1 FWD 0 (Sock: IN 0 OUT 0 FWD 0)\n\t SPD buckets: count 7 Max 1048576\n\t SPD IPv4 thresholds: local 32 remote 32\n\t SPD IPv6 thresholds: local 128 remote 128"},
		{"positive: set is a NEWSPDINFO with both thresholds", req("policy_set_hthresh/ip.pcap"), "newspdinfo ack flags=0xffffffff hthresh4=24/16 hthresh6=64/48"},
		{"positive: and reads back with them", replies("policy_set_hthresh/netlink_xfrm_spd.pcap"), "newspdinfo flags=0xffffffff spd_info=0/0/0/0/0/0 spd_hinfo=7/1048576 hthresh4=24/16 hthresh6=64/48"},
		{"boundary: a 33-bit IPv4 threshold is invarg", errno("policy_set_hthresh_bad"), "no request rc 255"},
		{"positive: getdefault is a GETDEFAULT with every direction unspecified", req("policy_getdefault/ip.pcap"), "getdefault in=0 fwd=0 out=0"},
		{"positive: answered with accept, 2, in each", replies("policy_getdefault/ip.pcap"), "getdefault in=2 fwd=2 out=2"},
		{"positive: setdefault is a SETDEFAULT, block 1 and accept 2", req("policy_setdefault/ip.pcap"), "setdefault ack in=1 fwd=1 out=2"},
		{"positive: and reads back", replies("policy_setdefault/netlink_xfrm_default.pcap"), "getdefault in=1 fwd=1 out=2"},
		{"positive: which ip prints", file("policy_setdefault/ip_default"), "Default policies:\n in:  block\n fwd: block\n out: accept"},
		{"negative: drop is not a default action", errno("policy_setdefault_bad"), "no request rc 255"},
		{"positive: every write's request, ACK and event, in order: NEWSA, NEWPOLICY, UPDSA, UPDPOLICY, a DELSA and a DELPOLICY that carry what they deleted, the two adds again, FLUSHSA, FLUSHPOLICY and the GETDEFAULT a setdefault announces",
			all("monitor_sa_policy/ip.pcap"), strings.Join([]string{
				"> newsa ack " + sa1Req, "< newsa " + sa1Kernel + " " + sa1Algs, "< error 0",
				"> newpolicy ack " + p1("0", "0", "") + " " + tmpl, "< " + p1Kernel, "< error 0",
				"> updsa ack " + sa1Req, "< updsa " + sa1Kernel + " " + sa1Algs, "< error 0",
				"> updpolicy ack " + p1("0", "5", "") + " " + tmpl, "< updpolicy " + p1("1", "5", "") + " " + tmpl, "< error 0",
				"> delsa ack dst=192.0.2.2 spi=0x100 family=2 proto=50 srcaddr=192.0.2.1",
				"< delsa dst=192.0.2.2 spi=0x100 family=2 proto=50 sa{" + sa1Kernel + "} " + sa1Algs, "< error 0",
				"> delpolicy ack " + p1ID, "< delpolicy " + p1ID + " policy{" + p1("1", "5", "") + "} " + tmpl, "< error 0",
				"> newsa ack " + sa1Req, "< newsa " + sa1Kernel + " " + sa1Algs, "< error 0",
				"> newpolicy ack " + p1("0", "0", "") + " " + tmpl, "< newpolicy " + p1("9", "0", "") + " " + tmpl, "< error 0",
				"> flushsa ack proto=0", "< flushsa proto=0", "< error 0",
				"> flushpolicy ack", "< flushpolicy", "< error 0",
				"> setdefault ack in=2 fwd=2 out=2", "< getdefault in=2 fwd=2 out=2", "< error 0",
			}, "|")},
		{"corner: a policy added again gets the next out index, 9, not 1", func(t *testing.T) string {
			return strconv.Itoa(strings.Count(xfrmAll(t, "monitor_sa_policy/ip.pcap"), "< newpolicy "+p1("9", "0", "")))
		}, "1"},
		{"positive: which ip monitor prints, ending with the flushes and the defaults", func(t *testing.T) string {
			lines := strings.Split(xfrmFile(t, "monitor_sa_policy/ip.out"), "\n")
			return strings.Join(lines[len(lines)-6:], "\n")
		}, "Flushed state proto 0\nFlushed policy \nDefault policies:\n in:  accept\n fwd: accept\n out: accept"},
		{"positive: traffic P1 needs an SA for is an ACQUIRE, with the flow's selector, P1 and its template; the source port is the socket's",
			func(t *testing.T) string {
				return xfrmEphemeral.ReplaceAllString(xfrmAll(t, "monitor_acquire/ip.pcap"), "sport=N")
			},
			"< acquire dst=192.0.2.2 spi=0x0 proto=50 src=192.0.2.1 sel=192.0.2.1/32-192.0.2.2/32,family=2,proto=17,dport=9,sport=N policy{" + p1("1", "0", "") + "} algos=0xffffffff/0xffffffff/0xffffffff " + tmpl},
		{"positive: which ip monitor prints",
			func(t *testing.T) string {
				return xfrmEphemeral.ReplaceAllString(xfrmFile(t, "monitor_acquire/ip.out"), "sport N")
			},
			"acquire proto esp \n  sel src 192.0.2.1/32 dst 192.0.2.2/32 proto udp sport N dport 9 dev d0 \n  policy src 192.0.2.1/32 dst 192.0.2.2/32 \n    \tdir out priority 0 \n    \ttmpl src 192.0.2.1 dst 192.0.2.2\n    \t\tproto esp reqid 1 mode transport"},
		{"positive: an SA with a 1 s soft and 2 s hard lifetime expires soft, then hard, with no DELSA",
			all("monitor_expire/ip.pcap"), strings.Join([]string{
				"> newsa ack " + sa1 + " sel=0/0-0/0,family=0,proto=0 lft{add-soft=1,add-hard=2} curlft=0B/0P auth=sha256/256:11*32 enc=aes/128:22*16", "< error 0",
				"< expire hard=0 " + sa1 + " sel=0/0-0/0,family=2,proto=0 lft{add-soft=1,add-hard=2} curlft=0B/0P",
				"< expire hard=1 " + sa1 + " sel=0/0-0/0,family=2,proto=0 lft{add-soft=1,add-hard=2} curlft=0B/0P",
			}, "|")},
		{"positive: a policy with a 1 s soft and 3 s hard lifetime expires the same way, with POLEXPIRE",
			all("monitor_polexpire/ip.pcap"), strings.Join([]string{
				"> newpolicy ack " + p1("0", "0", " lft{add-soft=1,add-hard=3}") + " " + tmpl, "< error 0",
				"< polexpire hard=0 " + p1("1", "0", " lft{add-soft=1,add-hard=3}") + " " + tmpl,
				"< polexpire hard=1 " + p1("1", "0", " lft{add-soft=1,add-hard=3}") + " " + tmpl,
			}, "|")},
		{"positive: ESP datagrams out through SA 1 after its replay timer has run are a NEWAE timer expired (0x20) at output sequence 1, then a replay update (0x10) at 3, two on",
			all("monitor_aevent/ip.pcap"), "< newae dst=192.0.2.2 spi=0x100 family=2 proto=50 src=192.0.2.1 flags=0x20 reqid=1 replay=1/0/0 ltime=0B/0P|" +
				"< newae dst=192.0.2.2 spi=0x100 family=2 proto=50 src=192.0.2.1 flags=0x10 reqid=1 replay=3/0/0 ltime=20B/2P"},
		{"positive: which ip monitor prints", file("monitor_aevent/ip.out"), "Async event  (0x20)  timer expired \n\tsrc 192.0.2.1 dst 192.0.2.2  reqid 0x1 protocol esp  SPI 0x100\nAsync event  (0x10)  replay update \n\tsrc 192.0.2.1 dst 192.0.2.2  reqid 0x1 protocol esp  SPI 0x100"},
		{"positive: an ESP-in-UDP datagram from a port the SA's encap does not have is a MAPPING, after the SA's request, NEWSA and ACK",
			all("monitor_mapping/ip.pcap"), "> newsa ack dst=192.0.2.2 spi=0x300 proto=50 src=192.0.2.1 family=2 mode=0 reqid=0 seq=0 replay_window=0 flags=0 sel=0/0-0/0,family=0,proto=0 curlft=0B/0P enc=cipher_null/0: encap=2/4500-4500/0|" +
				"< newsa dst=192.0.2.2 spi=0x300 proto=50 src=192.0.2.1 family=2 mode=0 reqid=0 seq=0 replay_window=0 flags=0 sel=0/0-0/0,family=2,proto=0 curlft=0B/0P enc=ecb(cipher_null)/0: encap=2/4500-4500/0 replay=0/0/0|" +
				"< error 0|" +
				"< mapping dst=192.0.2.2 spi=0x300 family=2 proto=50 reqid=0 old=192.0.2.1:4500 new=192.0.2.1:4501"},
		{"positive: which ip monitor prints, after the helper's packet: the old side is the SA's address and encap port, the new the packet's",
			file("monitor_mapping/ip.out"), "000003000000000100000000000000000000003b\nsrc 192.0.2.1 dst 192.0.2.2\n\tproto esp spi 0x00000300 reqid 0 mode transport\n\treplay-window 0 \n\tenc ecb(cipher_null) \n\tencap type espinudp sport 4500 dport 4500 addr 0.0.0.0\n\tanti-replay context: seq 0x0, oseq 0x0, bitmap 0x00000000\n\tsel src 0.0.0.0/0 dst 0.0.0.0/0 \nMapping change 192.0.2.1:4500 -> 192.0.2.1:4501\n\tdst 192.0.2.2  reqid 0x0 protocol esp  SPI 0x300"},
		{"negative: an unknown monitor group is refused by ip, which opens nothing", errno("monitor_unknown"), "no request rc 255"},
		{"negative: and says why", file("monitor_unknown/ip.err"), "Argument \"bogus\" is unknown, try \"ip xfrm monitor help\"."},
		{"corner: no attribute this does not know, and no other message type, across every pcap",
			func(t *testing.T) string {
				var out []string
				for _, slug := range xfrmPolicyRows {
					for _, name := range []string{"ip", "netlink_xfrm_policy", "netlink_xfrm_spd", "netlink_xfrm_default"} {
						rel := slug + "/" + name + ".pcap"
						if _, err := os.Stat(tdXfrmWrite_7_1_4 + "/" + rel); err != nil {
							continue
						}
						for _, m := range strings.Split(xfrmAll(t, rel), "|") {
							if strings.Contains(m, "attr") || strings.HasPrefix(strings.TrimLeft(m, "<> "), "type") {
								out = append(out, slug+": "+m)
							}
						}
					}
				}
				return strings.Join(out, " ")
			}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := tc.got(t); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}
