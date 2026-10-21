package xtcpnl

import (
	"encoding/binary"
	"encoding/hex"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

// Offsets in struct xfrm_usersa_info on x86_64. The message types, the
// attributes and the struct sizes are xtcpnl_xfrm*.go's.
const (
	xfrmLftOff    = 96
	xfrmLftFields = 8
	xfrmCurlftOff = 160
	xfrmSeqOff    = 204
	xfrmInf       = ^uint64(0)
)

// xfrmLftNames are xfrm_lifetime_cfg's eight limits, in order.
var xfrmLftNames = [xfrmLftFields]string{
	"byte-soft", "byte-hard", "packet-soft", "packet-hard",
	"add-soft", "add-hard", "use-soft", "use-hard",
}

// xfrmAddr renders an xfrm_address_t: 0 when it is all zeros, IPv4 when
// only its first four bytes are set, else IPv6.
func xfrmAddr(b []byte) string {
	if strings.Trim(string(b), "\x00") == "" {
		return "0"
	}
	if strings.Trim(string(b[4:XfrmAddressSizeCst]), "\x00") == "" {
		return net.IP(b[:4]).String()
	}
	return net.IP(b[:XfrmAddressSizeCst]).String()
}

// xfrmKey renders key bytes as <byte>*<count> when every byte is the
// same, which every key in the corpus is, else as hex.
func xfrmKey(b []byte) string {
	if len(b) > 0 && strings.Trim(string(b), string(b[:1])) == "" {
		return hex.EncodeToString(b[:1]) + "*" + strconv.Itoa(len(b))
	}
	return hex.EncodeToString(b)
}

// xfrmAlg renders an xfrm_algo (name, key length in bits, key), or with
// extra set, xfrm_algo_aead or xfrm_algo_auth, whose third word is the
// ICV or truncation length: name/bits[/extra]:key.
func xfrmAlg(b []byte, extra bool) string {
	name := strings.TrimRight(string(b[:xfrmAlgoNameLen]), "\x00")
	bits := binary.LittleEndian.Uint32(b[xfrmAlgoNameLen:])
	out := name + "/" + u32s(bits)
	keyOff := xfrmAlgoNameLen + 4
	if extra {
		out += "/" + u32s(binary.LittleEndian.Uint32(b[keyOff:]))
		keyOff += 4
	}
	return out + ":" + xfrmKey(b[keyOff:])
}

// xfrmSAInfo renders an xfrm_usersa_info: its id (destination, SPI and
// protocol), source, family, mode, reqid, flags and replay window, the
// selector, and the lifetime limits that are neither 0 nor infinite. The
// current lifetime is left out but for its bytes and packets, since it
// holds the time the SA was added.
func xfrmSAInfo(b []byte) string {
	sel := b[:XfrmSelectorSizeCst]
	words := []string{
		"dst=" + xfrmAddr(b[XfrmSelectorSizeCst:XfrmSelectorSizeCst+XfrmAddressSizeCst]),
		"spi=0x" + strconv.FormatUint(uint64(binary.BigEndian.Uint32(b[XfrmSelectorSizeCst+16:])), 16),
		"proto=" + strconv.Itoa(int(b[XfrmSelectorSizeCst+20])),
		"src=" + xfrmAddr(b[80:96]),
		"family=" + strconv.Itoa(int(binary.LittleEndian.Uint16(b[212:]))),
		"mode=" + strconv.Itoa(int(b[214])),
		"reqid=" + u32s(binary.LittleEndian.Uint32(b[208:])),
		"seq=" + u32s(binary.LittleEndian.Uint32(b[xfrmSeqOff:])),
		"replay_window=" + strconv.Itoa(int(b[215])),
		"flags=" + strconv.Itoa(int(b[216])),
		"sel=" + xfrmSel(sel),
	}
	if lft := xfrmLft(b[xfrmLftOff:]); lft != "" {
		words = append(words, strings.TrimSpace(lft))
	}
	words = append(words, "curlft="+strconv.FormatUint(binary.LittleEndian.Uint64(b[xfrmCurlftOff:]), 10)+"B/"+
		strconv.FormatUint(binary.LittleEndian.Uint64(b[xfrmCurlftOff+8:]), 10)+"P")
	return strings.Join(words, " ")
}

// xfrmAttrSpace is which numbering a message's attributes use: XFRMA_*,
// a NEWSADINFO's XFRMA_SAD_* or a NEWSPDINFO's XFRMA_SPD_*.
type xfrmAttrSpace int

const (
	xfrmSpaceSA xfrmAttrSpace = iota
	xfrmSpaceSAD
	xfrmSpaceSPD
)

// xfrmAttrs renders XFRMA_* attributes in the order they came, or in
// space's numbering. An attribute this does not know is attr<N>.
func xfrmAttrs(t *testing.T, data []byte, space xfrmAttrSpace) string {
	t.Helper()
	attrs, err := ParseNetlinkAttributes(data)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(attrs))
	for _, a := range attrs {
		d := a.Data
		var w string
		switch {
		case space == xfrmSpaceSAD && a.ID() == XfrmSADAttrCnt:
			w = "sad_cnt=" + u32s(binary.LittleEndian.Uint32(d))
		case space == xfrmSpaceSAD && a.ID() == XfrmSADAttrHInfo:
			w = "sad_hinfo=" + u32s(binary.LittleEndian.Uint32(d)) + "/" + u32s(binary.LittleEndian.Uint32(d[4:]))
		case space == xfrmSpaceSPD:
			w = xfrmSPDAttr(a.ID(), d)
		case space != xfrmSpaceSA:
			w = "attr" + strconv.Itoa(int(a.ID()))
		case a.ID() == XfrmAttrAlgAuth:
			w = "auth=" + xfrmAlg(d, false)
		case a.ID() == XfrmAttrAlgCrypt:
			w = "enc=" + xfrmAlg(d, false)
		case a.ID() == XfrmAttrAlgAEAD:
			w = "aead=" + xfrmAlg(d, true)
		case a.ID() == XfrmAttrAlgAuthTrunc:
			w = "auth_trunc=" + xfrmAlg(d, true)
		case a.ID() == XfrmAttrSrcAddr:
			w = "srcaddr=" + xfrmAddr(d)
		case a.ID() == XfrmAttrLtimeVal:
			w = "ltime=" + strconv.FormatUint(binary.LittleEndian.Uint64(d), 10) + "B/" +
				strconv.FormatUint(binary.LittleEndian.Uint64(d[8:]), 10) + "P"
		case a.ID() == XfrmAttrReplayVal:
			w = "replay=" + u32s(binary.LittleEndian.Uint32(d)) + "/" + u32s(binary.LittleEndian.Uint32(d[4:])) +
				"/" + u32s(binary.LittleEndian.Uint32(d[8:]))
		case a.ID() == XfrmAttrReplayThresh:
			w = "replay_thresh=" + u32s(binary.LittleEndian.Uint32(d))
		case a.ID() == XfrmAttrEtimerThresh:
			w = "etimer_thresh=" + u32s(binary.LittleEndian.Uint32(d))
		case a.ID() == XfrmAttrLastUsed:
			w = "lastused"
		case a.ID() == XfrmAttrProto:
			w = "proto=" + strconv.Itoa(int(d[0]))
		case a.ID() == XfrmAttrAddressFilter:
			w = "filter=" + xfrmAddr(d[:16]) + "/" + strconv.Itoa(int(d[34])) + "-" + xfrmAddr(d[16:32]) + "/" +
				strconv.Itoa(int(d[35])) + ",family=" + strconv.Itoa(int(binary.LittleEndian.Uint16(d[32:])))
		case a.ID() == XfrmAttrPad:
			continue
		default:
			w = xfrmPolicyAttr(a.ID(), d)
		}
		out = append(out, w)
	}
	return strings.Join(out, " ")
}

// xfrmMsg renders one XFRM message: its type, dump and ack when asked
// for, its body and its attributes.
func xfrmMsg(t *testing.T, m capturedNlMsg) string {
	t.Helper()
	b := m.body
	words := make([]string, 0, 4)
	attrOff := 0
	space := xfrmSpaceSA
	switch m.h.Type {
	case XfrmMsgNewSA, XfrmMsgUpdSA:
		words = append(words, map[uint16]string{XfrmMsgNewSA: "newsa", XfrmMsgUpdSA: "updsa"}[m.h.Type], xfrmSAInfo(b))
		attrOff = XfrmUsersaInfoSizeCst
	case XfrmMsgAllocSPI:
		words = append(words, "allocspi", xfrmSAInfo(b), "min=0x"+strconv.FormatUint(uint64(binary.LittleEndian.Uint32(b[XfrmUsersaInfoSizeCst:])), 16)+
			" max=0x"+strconv.FormatUint(uint64(binary.LittleEndian.Uint32(b[XfrmUsersaInfoSizeCst+4:])), 16))
		attrOff = XfrmUserspiInfoSizeCst
	case XfrmMsgGetSA, XfrmMsgDelSA:
		words = append(words, map[uint16]string{XfrmMsgGetSA: "getsa", XfrmMsgDelSA: "delsa"}[m.h.Type])
		if m.h.Flags&unix.NLM_F_DUMP != unix.NLM_F_DUMP {
			words = append(words, "dst="+xfrmAddr(b[:XfrmAddressSizeCst])+" spi=0x"+
				strconv.FormatUint(uint64(binary.BigEndian.Uint32(b[16:])), 16)+" family="+
				strconv.Itoa(int(binary.LittleEndian.Uint16(b[20:])))+" proto="+strconv.Itoa(int(b[22])))
			attrOff = XfrmUsersaIDSizeCst
		}
	case XfrmMsgFlushSA:
		words = append(words, "flushsa", "proto="+strconv.Itoa(int(b[0])))
		attrOff = len(b)
	case XfrmMsgGetSADInfo, XfrmMsgNewSADInfo:
		words = append(words, map[uint16]string{XfrmMsgGetSADInfo: "getsadinfo", XfrmMsgNewSADInfo: "newsadinfo"}[m.h.Type],
			"flags=0x"+strconv.FormatUint(uint64(binary.LittleEndian.Uint32(b)), 16))
		attrOff = 4
		space = xfrmSpaceSAD
	default:
		var ok bool
		if words, attrOff, space, ok = xfrmPolicyBody(m.h.Type, b); !ok {
			return "type" + strconv.Itoa(int(m.h.Type))
		}
	}
	if m.h.Flags&unix.NLM_F_DUMP == unix.NLM_F_DUMP && m.h.Flags&unix.NLM_F_REQUEST != 0 {
		words = append(words[:1], append([]string{"dump"}, words[1:]...)...)
	}
	if m.h.Flags&unix.NLM_F_ACK != 0 {
		words = append(words[:1], append([]string{"ack"}, words[1:]...)...)
	}
	if attrOff < len(b) {
		if a := xfrmAttrs(t, b[nlmsgAlign(attrOff):], space); a != "" {
			words = append(words, a)
		}
	}
	return strings.Join(words, " ")
}

// xfrmMsgs describes each XFRM message in a pcap, requests when request
// is true and the kernel's answers otherwise, in the order they came;
// NLMSG_ERROR and NLMSG_DONE are left to xfrmErrno.
func xfrmMsgs(t *testing.T, rel string, request bool) string {
	t.Helper()
	var out []string
	for _, m := range pcapNlMsgs(t, tdXfrmWrite_7_1_4+"/"+rel) {
		if m.h.Type == unix.NLMSG_ERROR || m.h.Type == unix.NLMSG_DONE ||
			(m.h.Flags&unix.NLM_F_REQUEST != 0) != request {
			continue
		}
		out = append(out, xfrmMsg(t, m))
	}
	return strings.Join(out, "|")
}

// xfrmFile is one of the XFRM fixtures' files, trimmed.
func xfrmFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(tdXfrmWrite_7_1_4 + "/" + rel)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(b))
}

// xfrmErrno is the errno the kernel answered a row's first request with,
// then its ip.rc; "no request" when ip sent none.
func xfrmErrno(t *testing.T, slug string) string {
	t.Helper()
	msgs := pcapNlMsgs(t, tdXfrmWrite_7_1_4+"/"+slug+"/ip.pcap")
	for _, m := range msgs {
		if m.h.Flags&unix.NLM_F_REQUEST != 0 {
			return strconv.Itoa(int(answerTo(t, msgs, m.h))) + " rc " + xfrmFile(t, slug+"/ip.rc")
		}
	}
	return "no request rc " + xfrmFile(t, slug+"/ip.rc")
}

// xfrmStateRows is every C3-T01 row, in the corpus's order.
var xfrmStateRows = []string{
	"state_list_empty", "state_add", "state_add_v6_tunnel", "state_add_aead", "state_add_enc_only",
	"state_add_dup", "state_add_unknown_alg", "state_add_short_key", "state_add_no_algo", "state_update",
	"state_update_larval", "state_update_missing", "state_get", "state_get_missing", "state_delete",
	"state_delete_missing", "state_list", "state_list_dst", "state_list_nokeys", "state_deleteall",
	"state_deleteall_empty", "state_flush", "state_flush_proto_ah", "state_count", "state_count_empty",
	"state_count_stats", "state_allocspi", "state_allocspi_taken", "state_allocspi_min_gt_max",
}

// TestXfrmStateCorpus checks the C3-T01 rows (testdata/7_1_4/link-write/
// xfrm/, scripts/xfrm-write-corpus.exp; docs/netlink/goip-parity-gaps.md):
// `ip xfrm state`'s requests and the kernel's answers.
func TestXfrmStateCorpus(t *testing.T) {
	req := func(rel string) func(t *testing.T) string {
		return func(t *testing.T) string { return xfrmMsgs(t, rel, true) }
	}
	replies := func(rel string) func(t *testing.T) string {
		return func(t *testing.T) string { return xfrmMsgs(t, rel, false) }
	}
	errno := func(slug string) func(t *testing.T) string {
		return func(t *testing.T) string { return xfrmErrno(t, slug) }
	}
	file := func(rel string) func(t *testing.T) string {
		return func(t *testing.T) string { return xfrmFile(t, rel) }
	}
	ack := "0 rc 0"
	esrch := strconv.Itoa(int(syscall.ESRCH)) + " rc 2"
	tests := []struct {
		description string
		got         func(t *testing.T) string
		expected    string
	}{
		{"boundary: list on an empty SAD is a dump with an empty address filter", req("state_list_empty/ip.pcap"), "getsa dump filter=0/0-0/0,family=0"},
		{"boundary: answered by NLMSG_DONE alone", replies("state_list_empty/ip.pcap"), ""},
		{"positive: add is a NEWSA with the SA and its algorithms by compat name", req("state_add/ip.pcap"), "newsa ack dst=192.0.2.2 spi=0x100 proto=50 src=192.0.2.1 family=2 mode=0 reqid=1 seq=0 replay_window=0 flags=0 sel=0/0-0/0,family=0,proto=0 curlft=0B/0P auth=sha256/256:11*32 enc=aes/128:22*16"},
		{"positive: answered with an ACK", errno("state_add"), ack},
		{"positive: and reads back by the kernel's names", replies("state_add/netlink_xfrm_state.pcap"), "newsa dst=192.0.2.2 spi=0x100 proto=50 src=192.0.2.1 family=2 mode=0 reqid=1 seq=0 replay_window=0 flags=0 sel=0/0-0/0,family=2,proto=0 curlft=0B/0P auth=hmac(sha256)/256:11*32 auth_trunc=hmac(sha256)/256/96:11*32 enc=cbc(aes)/128:22*16 replay=0/0/0"},
		{"positive: which ip prints", file("state_add/ip_state"), "src 192.0.2.1 dst 192.0.2.2\n\tproto esp spi 0x00000100 reqid 1 mode transport\n\treplay-window 0 \n\tauth-trunc hmac(sha256) 0x1111111111111111111111111111111111111111111111111111111111111111 96\n\tenc cbc(aes) 0x22222222222222222222222222222222\n\tanti-replay context: seq 0x0, oseq 0x0, bitmap 0x00000000\n\tsel src 0.0.0.0/0 dst 0.0.0.0/0"},
		{"positive: an IPv6 tunnel SA with a truncated hmac", req("state_add_v6_tunnel/ip.pcap"), "newsa ack dst=2001:db8::2 spi=0x101 proto=50 src=2001:db8::1 family=10 mode=1 reqid=2 seq=0 replay_window=0 flags=0 sel=0/0-0/0,family=0,proto=0 curlft=0B/0P auth_trunc=sha256/256/128:11*32 enc=aes/128:22*16"},
		{"positive: and reads back", replies("state_add_v6_tunnel/netlink_xfrm_state.pcap"), "newsa dst=2001:db8::2 spi=0x101 proto=50 src=2001:db8::1 family=10 mode=1 reqid=2 seq=0 replay_window=0 flags=0 sel=0/0-0/0,family=10,proto=0 curlft=0B/0P auth=hmac(sha256)/256:11*32 auth_trunc=hmac(sha256)/256/128:11*32 enc=cbc(aes)/128:22*16 replay=0/0/0"},
		{"positive: an AEAD SA", req("state_add_aead/ip.pcap"), "newsa ack dst=192.0.2.3 spi=0x102 proto=50 src=192.0.2.1 family=2 mode=0 reqid=3 seq=0 replay_window=0 flags=0 sel=0/0-0/0,family=0,proto=0 curlft=0B/0P aead=rfc4106(gcm(aes))/160/128:33*20"},
		{"positive: and reads back", replies("state_add_aead/netlink_xfrm_state.pcap"), "newsa dst=192.0.2.3 spi=0x102 proto=50 src=192.0.2.1 family=2 mode=0 reqid=3 seq=0 replay_window=0 flags=0 sel=0/0-0/0,family=2,proto=0 curlft=0B/0P aead=rfc4106(gcm(aes))/160/128:33*20 replay=0/0/0"},
		{"positive: an SA with no auth", req("state_add_enc_only/ip.pcap"), "newsa ack dst=192.0.2.4 spi=0x103 proto=50 src=192.0.2.1 family=2 mode=0 reqid=0 seq=0 replay_window=0 flags=0 sel=0/0-0/0,family=0,proto=0 curlft=0B/0P enc=aes/128:22*16"},
		{"positive: and reads back", replies("state_add_enc_only/netlink_xfrm_state.pcap"), "newsa dst=192.0.2.4 spi=0x103 proto=50 src=192.0.2.1 family=2 mode=0 reqid=0 seq=0 replay_window=0 flags=0 sel=0/0-0/0,family=2,proto=0 curlft=0B/0P enc=cbc(aes)/128:22*16 replay=0/0/0"},
		{"negative: an SA the kernel already has is EEXIST", errno("state_add_dup"), strconv.Itoa(int(syscall.EEXIST)) + " rc 2"},
		{"negative: an unknown algorithm is ENOSYS", errno("state_add_unknown_alg"), strconv.Itoa(int(syscall.ENOSYS)) + " rc 2"},
		{"negative: a 120-bit aes key is EINVAL", errno("state_add_short_key"), strconv.Itoa(int(syscall.EINVAL)) + " rc 2"},
		{"negative: ESP with no algorithm is refused by ip, which sends nothing", errno("state_add_no_algo"), "no request rc 1"},
		{"negative: and says why", file("state_add_no_algo/ip.err"), "ALGO-TYPE value \"enc\" or \"aead\" is required with XFRM-PROTO value \"esp\""},
		{"positive: update is an UPDSA with the new hard time limit", req("state_update/ip.pcap"), "updsa ack dst=192.0.2.2 spi=0x100 proto=50 src=192.0.2.1 family=2 mode=0 reqid=1 seq=0 replay_window=0 flags=0 sel=0/0-0/0,family=0,proto=0 lft{add-hard=3600} curlft=0B/0P auth=sha256/256:11*32 enc=aes/128:22*16"},
		{"positive: and reads back with it", replies("state_update/netlink_xfrm_state.pcap"), "newsa dst=192.0.2.2 spi=0x100 proto=50 src=192.0.2.1 family=2 mode=0 reqid=1 seq=0 replay_window=0 flags=0 sel=0/0-0/0,family=2,proto=0 lft{add-hard=3600} curlft=0B/0P auth=hmac(sha256)/256:11*32 auth_trunc=hmac(sha256)/256/96:11*32 enc=cbc(aes)/128:22*16 replay=0/0/0"},
		{"positive: which ip -s prints, as an add expiry; the add time is the run's and left out",
			func(t *testing.T) string {
				var out []string
				for _, l := range strings.Split(xfrmFile(t, "state_update/ip_state"), "\n") {
					if strings.Contains(l, "expire") || strings.Contains(l, "limit:") {
						out = append(out, strings.TrimSpace(l))
					}
				}
				return strings.Join(out, "; ")
			}, "limit: soft (INF)(bytes), hard (INF)(bytes); limit: soft (INF)(packets), hard (INF)(packets); expire add: soft 0(sec), hard 3600(sec); expire use: soft 0(sec), hard 0(sec)"},
		{"positive: an update of a larval state gives it its algorithms", replies("state_update_larval/netlink_xfrm_state.pcap"), "newsa dst=192.0.2.2 spi=0x200 proto=50 src=192.0.2.1 family=2 mode=0 reqid=0 seq=0 replay_window=0 flags=0 sel=0/0-0/0,family=2,proto=0 curlft=0B/0P auth=hmac(sha256)/256:11*32 auth_trunc=hmac(sha256)/256/96:11*32 enc=cbc(aes)/128:22*16 replay=0/0/0"},
		{"negative: an update of a missing SA is ESRCH", errno("state_update_missing"), esrch},
		{"positive: get is a GETSA by destination, SPI and protocol, with the source as an attribute", req("state_get/ip.pcap"), "getsa dst=192.0.2.2 spi=0x100 family=2 proto=50 srcaddr=192.0.2.1"},
		{"positive: answered with the SA", replies("state_get/ip.pcap"), "newsa dst=192.0.2.2 spi=0x100 proto=50 src=192.0.2.1 family=2 mode=0 reqid=1 seq=0 replay_window=0 flags=0 sel=0/0-0/0,family=2,proto=0 curlft=0B/0P auth=hmac(sha256)/256:11*32 auth_trunc=hmac(sha256)/256/96:11*32 enc=cbc(aes)/128:22*16 replay=0/0/0"},
		{"negative: a get of a missing SA is ESRCH", errno("state_get_missing"), esrch},
		{"positive: delete is a DELSA, as get", req("state_delete/ip.pcap"), "delsa ack dst=192.0.2.2 spi=0x100 family=2 proto=50 srcaddr=192.0.2.1"},
		{"positive: and the SAD is then empty", replies("state_delete/netlink_xfrm_state.pcap"), ""},
		{"negative: a delete of a missing SA is ESRCH", errno("state_delete_missing"), esrch},
		{"positive: a dump of two SAs", replies("state_list/ip.pcap"), "newsa dst=2001:db8::2 spi=0x101 proto=50 src=2001:db8::1 family=10 mode=1 reqid=2 seq=0 replay_window=0 flags=0 sel=0/0-0/0,family=10,proto=0 curlft=0B/0P auth=hmac(sha256)/256:11*32 auth_trunc=hmac(sha256)/256/128:11*32 enc=cbc(aes)/128:22*16 replay=0/0/0|newsa dst=192.0.2.2 spi=0x100 proto=50 src=192.0.2.1 family=2 mode=0 reqid=1 seq=0 replay_window=0 flags=0 sel=0/0-0/0,family=2,proto=0 curlft=0B/0P auth=hmac(sha256)/256:11*32 auth_trunc=hmac(sha256)/256/96:11*32 enc=cbc(aes)/128:22*16 replay=0/0/0"},
		{"positive: list dst sends the destination as a /32 address filter", req("state_list_dst/ip.pcap"), "getsa dump filter=0/0-192.0.2.2/32,family=2"},
		{"positive: which the kernel applies", replies("state_list_dst/ip.pcap"), "newsa dst=192.0.2.2 spi=0x100 proto=50 src=192.0.2.1 family=2 mode=0 reqid=1 seq=0 replay_window=0 flags=0 sel=0/0-0/0,family=2,proto=0 curlft=0B/0P auth=hmac(sha256)/256:11*32 auth_trunc=hmac(sha256)/256/96:11*32 enc=cbc(aes)/128:22*16 replay=0/0/0"},
		{"corner: list nokeys is the same request", req("state_list_nokeys/ip.pcap"), "getsa dump filter=0/0-0/0,family=0"},
		{"corner: and ip prints no keys", file("state_list_nokeys/ip.out"), "src 2001:db8::1 dst 2001:db8::2\n\tproto esp spi 0x00000101 reqid 2 mode tunnel\n\treplay-window 0 \n\tauth-trunc hmac(sha256) <<Keys hidden>> 128\n\tenc cbc(aes) <<Keys hidden>>\n\tanti-replay context: seq 0x0, oseq 0x0, bitmap 0x00000000\n\tsel src ::/0 dst ::/0 \nsrc 192.0.2.1 dst 192.0.2.2\n\tproto esp spi 0x00000100 reqid 1 mode transport\n\treplay-window 0 \n\tauth-trunc hmac(sha256) <<Keys hidden>> 96\n\tenc cbc(aes) <<Keys hidden>>\n\tanti-replay context: seq 0x0, oseq 0x0, bitmap 0x00000000\n\tsel src 0.0.0.0/0 dst 0.0.0.0/0"},
		{"positive: deleteall dumps, deletes each SA without asking for an ACK, and dumps again", req("state_deleteall/ip.pcap"), "getsa dump|delsa dst=2001:db8::2 spi=0x101 family=10 proto=50 srcaddr=2001:db8::1|delsa dst=192.0.2.2 spi=0x100 family=2 proto=50 srcaddr=192.0.2.1|getsa dump"},
		{"positive: and the SAD is then empty", replies("state_deleteall/netlink_xfrm_state.pcap"), ""},
		{"boundary: deleteall with nothing stops after its dump", req("state_deleteall_empty/ip.pcap"), "getsa dump"},
		{"positive: flush is a FLUSHSA for any protocol", req("state_flush/ip.pcap"), "flushsa ack proto=0"},
		{"positive: and the SAD is then empty", replies("state_flush/netlink_xfrm_state.pcap"), ""},
		{"corner: a flush that matches nothing is ACKed", errno("state_flush_proto_ah"), ack},
		{"corner: and leaves both SAs", replies("state_flush_proto_ah/netlink_xfrm_state.pcap"), "newsa dst=2001:db8::2 spi=0x101 proto=50 src=2001:db8::1 family=10 mode=1 reqid=2 seq=0 replay_window=0 flags=0 sel=0/0-0/0,family=10,proto=0 curlft=0B/0P auth=hmac(sha256)/256:11*32 auth_trunc=hmac(sha256)/256/128:11*32 enc=cbc(aes)/128:22*16 replay=0/0/0|newsa dst=192.0.2.2 spi=0x100 proto=50 src=192.0.2.1 family=2 mode=0 reqid=1 seq=0 replay_window=0 flags=0 sel=0/0-0/0,family=2,proto=0 curlft=0B/0P auth=hmac(sha256)/256:11*32 auth_trunc=hmac(sha256)/256/96:11*32 enc=cbc(aes)/128:22*16 replay=0/0/0"},
		{"positive: count is a GETSADINFO", req("state_count/ip.pcap"), "getsadinfo flags=0xffffffff"},
		{"positive: answered with the count and the hash table's size", replies("state_count/ip.pcap"), "newsadinfo flags=0xffffffff sad_cnt=2 sad_hinfo=8/1048576"},
		{"positive: which ip prints", file("state_count/ip.out"), "SAD count 2"},
		{"boundary: an empty SAD counts 0", replies("state_count_empty/ip.pcap"), "newsadinfo flags=0xffffffff sad_cnt=0 sad_hinfo=8/1048576"},
		{"corner: ip -s prints the hash table too", file("state_count_stats/ip.out"), "SAD count 2 (buckets count 8 Max 1048576)"},
		{"positive: allocspi is an ALLOCSPI for one SPI", req("state_allocspi/ip.pcap"), "allocspi dst=192.0.2.2 spi=0x0 proto=50 src=192.0.2.1 family=2 mode=0 reqid=0 seq=0 replay_window=0 flags=0 sel=0/0-0/0,family=0,proto=0 curlft=0B/0P min=0x200 max=0x200"},
		{"positive: answered with the larval state", replies("state_allocspi/ip.pcap"), "newsa dst=192.0.2.2 spi=0x200 proto=50 src=192.0.2.1 family=2 mode=0 reqid=0 seq=0 replay_window=0 flags=0 sel=192.0.2.1/32-192.0.2.2/32,family=0,proto=0 lft{add-hard=30} curlft=0B/0P replay=0/0/0"},
		{"positive: which the SAD then holds", replies("state_allocspi/netlink_xfrm_state.pcap"), "newsa dst=192.0.2.2 spi=0x200 proto=50 src=192.0.2.1 family=2 mode=0 reqid=0 seq=0 replay_window=0 flags=0 sel=192.0.2.1/32-192.0.2.2/32,family=0,proto=0 lft{add-hard=30} curlft=0B/0P replay=0/0/0"},
		{"negative: allocating a taken SPI is ENOENT", errno("state_allocspi_taken"), strconv.Itoa(int(syscall.ENOENT)) + " rc 2"},
		{"negative: and ip prints the kernel's reason", file("state_allocspi_taken/ip.err"), "Error: No SPI available in the requested range."},
		{"negative: min over max is refused by ip, which sends nothing", errno("state_allocspi_min_gt_max"), "no request rc 1"},
		{"corner: no attribute this does not know, and no other message type, across every pcap",
			func(t *testing.T) string {
				var out []string
				for _, slug := range xfrmStateRows {
					for _, rel := range []string{slug + "/ip.pcap", slug + "/netlink_xfrm_state.pcap"} {
						if _, err := os.Stat(tdXfrmWrite_7_1_4 + "/" + rel); err != nil {
							continue
						}
						for _, m := range strings.Split(xfrmMsgs(t, rel, true)+"|"+xfrmMsgs(t, rel, false), "|") {
							if strings.Contains(m, "attr") || strings.HasPrefix(m, "type") {
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
