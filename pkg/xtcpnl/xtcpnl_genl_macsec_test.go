package xtcpnl

import (
	"encoding/binary"
	"encoding/hex"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

// The macsec commands (linux/if_macsec.h), which x/sys/unix does not
// define. The attributes are in macsecSchema.
const (
	macsecCmdGetTxsc    = 0
	macsecCmdAddRxsc    = 1
	macsecCmdDelRxsc    = 2
	macsecCmdUpdRxsc    = 3
	macsecCmdAddTxsa    = 4
	macsecCmdDelTxsa    = 5
	macsecCmdUpdTxsa    = 6
	macsecCmdAddRxsa    = 7
	macsecCmdDelRxsa    = 8
	macsecCmdUpdRxsa    = 9
	macsecCmdUpdOffload = 10
	macsecAttrTxscStats = 7
	macsecAttrSecyStats = 8
	macsecSecyAttrPad   = 14
	macsecPnLen         = 4
	macsecXpnPnLen      = 8
	macsecTopLevel      = "top"
	macsecKindU8        = "u8"
	macsecKindU32       = "u32"
	macsecKindPN        = "pn"
	macsecKindU64Hex    = "u64hex"
	macsecKindHex       = "hex"
	macsecKindStats     = "stats"
	macsecKindPad       = "pad"
	macsecNestPrefix    = "nest:"
	macsecListPrefix    = "list:"
	macsecUnknown       = "attr"
)

// macsecField is how an attribute of one macsec attribute set renders:
// its name and its kind, a scalar kind above or nest:/list: and the set
// its contents are in.
type macsecField struct{ name, kind string }

// macsecSchema is linux/if_macsec.h's attribute sets: the top level
// (macsec_attrs), the SecY, a receive channel, an SA and offload.
var macsecSchema = map[string]map[uint16]macsecField{
	macsecTopLevel: {
		1: {"ifindex", macsecKindU32}, 2: {"rxsc", macsecNestPrefix + "rxsc"}, 3: {"sa", macsecNestPrefix + "sa"},
		4: {"secy", macsecNestPrefix + "secy"}, 5: {"txsa_list", macsecListPrefix + "sa"},
		6: {"rxsc_list", macsecListPrefix + "rxsc"}, macsecAttrTxscStats: {"txsc_stats", macsecKindStats},
		macsecAttrSecyStats: {"secy_stats", macsecKindStats}, 9: {"offload", macsecNestPrefix + "offload"},
	},
	"secy": {
		1: {"sci", macsecKindHex}, 2: {"encoding_sa", macsecKindU8}, 3: {"window", macsecKindU32},
		4: {"cipher", macsecKindU64Hex}, 5: {"icv_len", macsecKindU8}, 6: {"protect", macsecKindU8},
		7: {"replay", macsecKindU8}, 8: {"oper", macsecKindU8}, 9: {"validate", macsecKindU8},
		10: {"encrypt", macsecKindU8}, 11: {"inc_sci", macsecKindU8}, 12: {"es", macsecKindU8},
		13: {"scb", macsecKindU8}, macsecSecyAttrPad: {"pad", macsecKindPad},
	},
	"rxsc": {
		1: {"sci", macsecKindHex}, 2: {"active", macsecKindU8}, 3: {"sa_list", macsecListPrefix + "sa"},
		4: {"stats", macsecKindStats}, 5: {"pad", macsecKindPad},
	},
	"sa": {
		1: {"an", macsecKindU8}, 2: {"active", macsecKindU8}, 3: {"pn", macsecKindPN}, 4: {"key", macsecKindHex},
		5: {"keyid", macsecKindHex}, 6: {"stats", macsecKindStats}, 7: {"pad", macsecKindPad},
		8: {"ssci", macsecKindHex}, 9: {"salt", macsecKindHex},
	},
	"offload": {1: {"type", macsecKindU8}, 2: {"pad", macsecKindPad}},
}

// macsecParse parses a nest's attributes, failing the test on error.
func macsecParse(t *testing.T, data []byte) []NetlinkAttribute {
	t.Helper()
	attrs, err := ParseNetlinkAttributes(data)
	if err != nil {
		t.Fatal(err)
	}
	return attrs
}

// macsecStats renders a statistics nest as <counters>/<non-zero>: the
// SecY's and channels' counters are 64-bit and an SA's 32-bit, so a
// counter is any attribute with data, and its padding none.
func macsecStats(t *testing.T, data []byte) string {
	t.Helper()
	n, nonzero := 0, 0
	for _, a := range macsecParse(t, data) {
		if len(a.Data) == 0 {
			continue
		}
		n++
		if strings.Trim(string(a.Data), "\x00") != "" {
			nonzero++
		}
	}
	return strconv.Itoa(n) + "/" + strconv.Itoa(nonzero)
}

// macsecAttrs renders attributes of the set named set in the order they
// came, each as name=value, a nest as name{...} and a list of nests as
// name[...;...]. u8 and u32 are decimal, a packet number decimal whatever
// its length (4 bytes, or 8 on an XPN SecY), the cipher suite as a hex
// number, and SCIs, keys, key ids, SSCIs and salts as their bytes in hex.
func macsecAttrs(t *testing.T, set string, attrs []NetlinkAttribute) string {
	t.Helper()
	var out []string
	for _, a := range attrs {
		f, ok := macsecSchema[set][a.ID()]
		if !ok {
			out = append(out, macsecUnknown+strconv.Itoa(int(a.ID())))
			continue
		}
		var v string
		switch {
		case strings.HasPrefix(f.kind, macsecNestPrefix):
			v = "{" + macsecAttrs(t, strings.TrimPrefix(f.kind, macsecNestPrefix), macsecParse(t, a.Data)) + "}"
		case strings.HasPrefix(f.kind, macsecListPrefix):
			var items []string
			for _, item := range macsecParse(t, a.Data) {
				items = append(items, macsecAttrs(t, strings.TrimPrefix(f.kind, macsecListPrefix), macsecParse(t, item.Data)))
			}
			v = "[" + strings.Join(items, ";") + "]"
		case f.kind == macsecKindU8:
			v = "=" + strconv.Itoa(int(a.Data[0]))
		case f.kind == macsecKindU32:
			v = "=" + u32s(binary.LittleEndian.Uint32(a.Data))
		case f.kind == macsecKindPN && len(a.Data) == macsecPnLen:
			v = "=" + u32s(binary.LittleEndian.Uint32(a.Data))
		case f.kind == macsecKindPN && len(a.Data) == macsecXpnPnLen:
			v = "=" + strconv.FormatUint(binary.LittleEndian.Uint64(a.Data), 10) + "(xpn)"
		case f.kind == macsecKindU64Hex:
			v = "=0x" + strconv.FormatUint(binary.LittleEndian.Uint64(a.Data), 16)
		case f.kind == macsecKindHex:
			v = "=" + hex.EncodeToString(a.Data)
		case f.kind == macsecKindStats:
			v = "=" + macsecStats(t, a.Data)
		case f.kind == macsecKindPad:
			v = ""
		default:
			v = "=?" + strconv.Itoa(len(a.Data))
		}
		out = append(out, f.name+v)
	}
	return strings.Join(out, " ")
}

// macsecMsgs describes each macsec message in a pcap, requests when
// request is true and the kernel's answers otherwise, in the order they
// came: the kernel dumps the SecYs by ifindex.
func macsecMsgs(t *testing.T, rel string, request bool) string {
	t.Helper()
	msgs := pcapNlMsgs(t, tdGenlWrite_7_1_4+"/"+rel)
	fam := genlFamilyID(t, msgs, "macsec")
	var out []string
	for _, m := range msgs {
		if fam == 0 || m.h.Type != fam || (m.h.Flags&unix.NLM_F_REQUEST != 0) != request {
			continue
		}
		g, err := ParseGenericNetlink(m.body)
		if err != nil {
			t.Fatal(err)
		}
		words := []string{map[uint8]string{
			macsecCmdGetTxsc: "get_txsc", macsecCmdAddRxsc: "add_rxsc", macsecCmdDelRxsc: "del_rxsc",
			macsecCmdUpdRxsc: "upd_rxsc", macsecCmdAddTxsa: "add_txsa", macsecCmdDelTxsa: "del_txsa",
			macsecCmdUpdTxsa: "upd_txsa", macsecCmdAddRxsa: "add_rxsa", macsecCmdDelRxsa: "del_rxsa",
			macsecCmdUpdRxsa: "upd_rxsa", macsecCmdUpdOffload: "upd_offload",
		}[g.Command]}
		if request && m.h.Flags&unix.NLM_F_DUMP == unix.NLM_F_DUMP {
			words = append(words, "dump")
		}
		if request && m.h.Flags&unix.NLM_F_ACK != 0 {
			words = append(words, "ack")
		}
		if a := macsecAttrs(t, macsecTopLevel, g.Attributes); a != "" {
			words = append(words, a)
		}
		out = append(out, strings.Join(words, " "))
	}
	return strings.Join(out, "|")
}

// macsecErrno is the errno the kernel answered a row's first macsec
// request with, then its ip.rc.
func macsecErrno(t *testing.T, slug string) string {
	t.Helper()
	msgs := pcapNlMsgs(t, tdGenlWrite_7_1_4+"/"+slug+"/ip.pcap")
	fam := genlFamilyID(t, msgs, "macsec")
	for _, m := range msgs {
		if fam != 0 && m.h.Type == fam && m.h.Flags&unix.NLM_F_REQUEST != 0 {
			return strconv.Itoa(int(answerTo(t, msgs, m.h))) + " rc " + tcpmFile(t, slug+"/ip.rc")
		}
	}
	return "no request rc " + tcpmFile(t, slug+"/ip.rc")
}

// macsecRows is every C2-T08 row, in the corpus's order.
var macsecRows = []string{
	"macsec_show_empty", "macsec_show", "macsec_show_dev", "macsec_show_unknown_dev",
	"macsec_txsa_add", "macsec_txsa_add_off", "macsec_txsa_add_dup", "macsec_txsa_add_no_key",
	"macsec_txsa_add_no_pn", "macsec_txsa_add_long_key", "macsec_txsa_add_an4",
	"macsec_txsa_add_not_macsec", "macsec_txsa_set", "macsec_txsa_set_key", "macsec_txsa_del_active",
	"macsec_txsa_del", "macsec_txsa_del_missing", "macsec_rxsc_add", "macsec_rxsc_add_port",
	"macsec_rxsc_add_dup", "macsec_rxsc_set", "macsec_rxsc_del", "macsec_rxsc_del_missing",
	"macsec_rxsa_add", "macsec_rxsa_add_no_sc", "macsec_rxsa_set", "macsec_rxsa_del_active",
	"macsec_rxsa_del", "macsec_xpn_txsa_add", "macsec_xpn_txsa_pn32", "macsec_offload_off",
	"macsec_offload_mac",
}

// TestGenlMacsecCorpus checks the C2-T08 rows (testdata/7_1_4/link-write/
// genl/, scripts/genl-write-corpus.exp; docs/netlink/goip-parity-gaps.md):
// `ip macsec`'s SAs, receive channels and offload, each command's request
// and the kernel's answer.
func TestGenlMacsecCorpus(t *testing.T) {
	req := func(rel string) func(t *testing.T) string {
		return func(t *testing.T) string { return macsecMsgs(t, rel, true) }
	}
	replies := func(rel string) func(t *testing.T) string {
		return func(t *testing.T) string { return macsecMsgs(t, rel, false) }
	}
	errno := func(slug string) func(t *testing.T) string {
		return func(t *testing.T) string { return macsecErrno(t, slug) }
	}
	file := func(rel string) func(t *testing.T) string {
		return func(t *testing.T) string { return tcpmFile(t, rel) }
	}
	ack := "0 rc 0"
	// secy1 and secy2 are the two SecYs' defaults: GCM-AES-128, protect
	// and send_sci on, validate strict, replay and encryption off.
	secy1 := "secy{sci=0102030405060001 cipher=0x80020001000001 icv_len=16 oper=1 protect=1 replay=0 validate=2 " +
		"encrypt=0 inc_sci=1 es=0 scb=0 encoding_sa=0}"
	secy2 := strings.Replace(secy1, "0102030405060001", "0102030405060002", 1)
	// txKey is the tx SA's key id and key, as ip sends them.
	txKey := "keyid=01000000000000000000000000000000 key=81818181818181818181818181818181"
	// dump is one SecY as GET_TXSC dumps it, with offload off, no traffic
	// and the given tx SAs and receive channels.
	dump := func(ifindex int, secy, txsa, rxsc string) string {
		return "get_txsc ifindex=" + strconv.Itoa(ifindex) + " offload{type=0} " + secy +
			" txsc_stats=4/0 secy_stats=8/0 txsa_list[" + txsa + "] rxsc_list[" + rxsc + "]"
	}
	ebusy := strconv.Itoa(int(syscall.EBUSY)) + " rc 2"
	enodev := strconv.Itoa(int(syscall.ENODEV)) + " rc 2"
	tests := []struct {
		description string
		got         func(t *testing.T) string
		expected    string
	}{
		{"positive: every row resolves macsec by name inside its own pcap",
			func(t *testing.T) string {
				var missing []string
				for _, slug := range macsecRows {
					if genlFamilyID(t, pcapNlMsgs(t, tdGenlWrite_7_1_4+"/"+slug+"/ip.pcap"), "macsec") == 0 {
						missing = append(missing, slug)
					}
				}
				return strings.Join(missing, " ")
			}, ""},
		{"positive: show is a GET_TXSC dump", req("macsec_show_empty/ip.pcap"), "get_txsc dump"},
		{"boundary: with no SecY the dump is empty", replies("macsec_show_empty/ip.pcap"), ""},
		{"positive: a dump of two SecYs, in ifindex order", replies("macsec_show/ip.pcap"), dump(4, secy1, "", "") + "|" + dump(5, secy2, "", "")},
		{"corner: show DEV is the same dump", req("macsec_show_dev/ip.pcap"), "get_txsc dump"},
		{"corner: and both SecYs come back", replies("macsec_show_dev/ip.pcap"), dump(4, secy1, "", "") + "|" + dump(5, secy2, "", "")},
		{"corner: of which ip prints the one asked for", file("macsec_show_dev/ip.out"), "5: macsec1: protect on validate strict sc off sa off encrypt off send_sci on end_station off scb off replay off \n" +
			"    cipher suite: GCM-AES-128, using ICV length 16\n    TXSC: 0102030405060002 on SA 0\n    offload: off"},
		{"negative: an unknown dev is refused by ip after the lookup, exit 1", errno("macsec_show_unknown_dev"), "no request rc 1"},
		{"positive: tx SA add is an ADD_TXSA with the ifindex and the SA", req("macsec_txsa_add/ip.pcap"), "add_txsa ack ifindex=4 sa{an=0 pn=1 " + txKey + " active=1}"},
		{"positive: answered with an ACK", errno("macsec_txsa_add"), ack},
		{"positive: and the SA reads back, with its key id and no key", replies("macsec_txsa_add/netlink_generic_macsec.pcap"), dump(4, secy1, "stats=2/0 an=0 pn=1 keyid=01000000000000000000000000000000 active=1", "")},
		{"positive: which ip prints", file("macsec_txsa_add/ip_macsec"), "4: macsec0: protect on validate strict sc off sa off encrypt off send_sci on end_station off scb off replay off \n" +
			"    cipher suite: GCM-AES-128, using ICV length 16\n    TXSC: 0102030405060001 on SA 0\n" +
			"        0: PN 1, state on, key 01000000000000000000000000000000\n    offload: off"},
		{"positive: an SA added off sends ACTIVE 0", req("macsec_txsa_add_off/ip.pcap"), "add_txsa ack ifindex=4 sa{an=3 pn=100 " + txKey + " active=0}"},
		{"corner: and reads back off, the SecY still operational since its encoding SA 0 was never set", replies("macsec_txsa_add_off/netlink_generic_macsec.pcap"), dump(4, secy1, "stats=2/0 an=3 pn=100 keyid=01000000000000000000000000000000 active=0", "")},
		{"negative: an SA number already in use is EBUSY", errno("macsec_txsa_add_dup"), ebusy},
		{"negative: an add with no key is refused by ip, exit 1", errno("macsec_txsa_add_no_key"), "no request rc 1"},
		{"negative: and says why", file("macsec_txsa_add_no_key/ip.err"), "cannot create SA without key"},
		{"negative: an add with no packet number is refused by ip, exit 1", errno("macsec_txsa_add_no_pn"), "no request rc 1"},
		{"negative: a 256-bit key on a 128-bit SecY is EINVAL", errno("macsec_txsa_add_long_key"), strconv.Itoa(int(syscall.EINVAL)) + " rc 2"},
		{"negative: SA number 4 is refused by ip, exit 255", errno("macsec_txsa_add_an4"), "no request rc 255"},
		{"negative: a device that is not macsec is ENODEV", errno("macsec_txsa_add_not_macsec"), enodev},
		{"positive: tx SA set is an UPD_TXSA with the new packet number and ACTIVE 0, and no key", req("macsec_txsa_set/ip.pcap"), "upd_txsa ack ifindex=4 sa{an=0 pn=50 active=0}"},
		{"positive: and reads back, the SecY no longer operational since its encoding SA is off", replies("macsec_txsa_set/netlink_generic_macsec.pcap"), dump(4, strings.Replace(secy1, "oper=1", "oper=0", 1), "stats=2/0 an=0 pn=50 keyid=01000000000000000000000000000000 active=0", "")},
		{"negative: a set with a key is refused by ip, exit 1", errno("macsec_txsa_set_key"), "no request rc 1"},
		{"negative: deleting an active tx SA is EBUSY", errno("macsec_txsa_del_active"), ebusy},
		{"positive: tx SA delete is a DEL_TXSA with the SA number alone", req("macsec_txsa_del/ip.pcap"), "del_txsa ack ifindex=4 sa{an=0}"},
		{"positive: and the SA is gone", replies("macsec_txsa_del/netlink_generic_macsec.pcap"), dump(4, secy1, "", "")},
		{"negative: deleting a missing tx SA is ENODEV", errno("macsec_txsa_del_missing"), enodev},
		{"positive: rx channel add is an ADD_RXSC with the SCI and ACTIVE", req("macsec_rxsc_add/ip.pcap"), "add_rxsc ack ifindex=4 rxsc{sci=0a0b0c0d0e0f0001 active=1}"},
		{"positive: and the channel reads back", replies("macsec_rxsc_add/netlink_generic_macsec.pcap"), dump(4, secy1, "", "active=1 sci=0a0b0c0d0e0f0001 stats=10/0 sa_list[]")},
		{"corner: port and address make the SCI, the address then the port in network order", req("macsec_rxsc_add_port/ip.pcap"), "add_rxsc ack ifindex=4 rxsc{sci=0200000000010001}"},
		{"corner: and reads back under that SCI", replies("macsec_rxsc_add_port/netlink_generic_macsec.pcap"), dump(4, secy1, "", "active=1 sci=0200000000010001 stats=10/0 sa_list[]")},
		{"negative: a channel the kernel already has is EEXIST", errno("macsec_rxsc_add_dup"), strconv.Itoa(int(syscall.EEXIST)) + " rc 2"},
		{"positive: rx channel set is an UPD_RXSC", req("macsec_rxsc_set/ip.pcap"), "upd_rxsc ack ifindex=4 rxsc{sci=0a0b0c0d0e0f0001 active=0}"},
		{"positive: and reads back off", replies("macsec_rxsc_set/netlink_generic_macsec.pcap"), dump(4, secy1, "", "active=0 sci=0a0b0c0d0e0f0001 stats=10/0 sa_list[]")},
		{"positive: rx channel delete is a DEL_RXSC with the SCI alone", req("macsec_rxsc_del/ip.pcap"), "del_rxsc ack ifindex=4 rxsc{sci=0a0b0c0d0e0f0001}"},
		{"positive: and the channel is gone", replies("macsec_rxsc_del/netlink_generic_macsec.pcap"), dump(4, secy1, "", "")},
		{"negative: deleting a missing channel is ENODEV", errno("macsec_rxsc_del_missing"), enodev},
		{"positive: rx SA add is an ADD_RXSA with the channel and the SA", req("macsec_rxsa_add/ip.pcap"), "add_rxsa ack ifindex=4 rxsc{sci=0a0b0c0d0e0f0001} sa{an=0 pn=1 keyid=02000000000000000000000000000000 key=82828282828282828282828282828282 active=1}"},
		{"positive: and the SA reads back under its channel", replies("macsec_rxsa_add/netlink_generic_macsec.pcap"), dump(4, secy1, "", "active=1 sci=0a0b0c0d0e0f0001 stats=10/0 sa_list[stats=5/0 an=0 pn=1 keyid=02000000000000000000000000000000 active=1]")},
		{"negative: an rx SA with no channel is ENODEV", errno("macsec_rxsa_add_no_sc"), enodev},
		{"positive: rx SA set is an UPD_RXSA", req("macsec_rxsa_set/ip.pcap"), "upd_rxsa ack ifindex=4 rxsc{sci=0a0b0c0d0e0f0001} sa{an=0 pn=60 active=0}"},
		{"positive: and the rx SA reads back off", replies("macsec_rxsa_set/netlink_generic_macsec.pcap"), dump(4, secy1, "", "active=1 sci=0a0b0c0d0e0f0001 stats=10/0 sa_list[stats=5/0 an=0 pn=60 keyid=02000000000000000000000000000000 active=0]")},
		{"negative: deleting an active rx SA is EBUSY", errno("macsec_rxsa_del_active"), ebusy},
		{"positive: rx SA delete is a DEL_RXSA", req("macsec_rxsa_del/ip.pcap"), "del_rxsa ack ifindex=4 rxsc{sci=0a0b0c0d0e0f0001} sa{an=0}"},
		{"positive: and the rx SA is gone, its channel left", replies("macsec_rxsa_del/netlink_generic_macsec.pcap"), dump(4, secy1, "", "active=1 sci=0a0b0c0d0e0f0001 stats=10/0 sa_list[]")},
		{"positive: an XPN SA sends a 64-bit packet number, the salt and the SSCI", req("macsec_xpn_txsa_add/ip.pcap"), "add_txsa ack ifindex=4 sa{an=0 pn=1(xpn) salt=0102030405060708090a0b0c ssci=00000001 " + txKey + " active=1}"},
		{"positive: and reads back with the SSCI and no salt", replies("macsec_xpn_txsa_add/netlink_generic_macsec.pcap"), dump(4, strings.Replace(secy1, "cipher=0x80020001000001", "cipher=0x80c20001000003", 1), "stats=2/0 an=0 pn=1(xpn) keyid=01000000000000000000000000000000 ssci=00000001 active=1", "")},
		{"negative: a 32-bit packet number on an XPN SecY is EINVAL", errno("macsec_xpn_txsa_pn32"), strconv.Itoa(int(syscall.EINVAL)) + " rc 2"},
		{"corner: the refused XPN add sends the 4-byte packet number, and no salt or SSCI, which ip sends only with xpn", req("macsec_xpn_txsa_pn32/ip.pcap"), "add_txsa ack ifindex=4 sa{an=0 pn=1 " + txKey + "}"},
		{"positive: offload off is an UPD_OFFLOAD with type 0", req("macsec_offload_off/ip.pcap"), "upd_offload ack ifindex=4 offload{type=0}"},
		{"positive: and offload reads back off", replies("macsec_offload_off/netlink_generic_macsec.pcap"), dump(4, secy1, "", "")},
		{"corner: ACKed on a SecY already off", errno("macsec_offload_off"), ack},
		{"negative: offload to a MAC that cannot is EOPNOTSUPP", errno("macsec_offload_mac"), strconv.Itoa(int(syscall.EOPNOTSUPP)) + " rc 2"},
		{"corner: no attribute or attribute set the schema does not know, across every pcap",
			func(t *testing.T) string {
				var out []string
				for _, slug := range macsecRows {
					for _, rel := range []string{slug + "/ip.pcap", slug + "/netlink_generic_macsec.pcap"} {
						if _, err := os.Stat(tdGenlWrite_7_1_4 + "/" + rel); err != nil {
							continue
						}
						for _, m := range strings.Split(macsecMsgs(t, rel, true)+"|"+macsecMsgs(t, rel, false), "|") {
							if strings.Contains(m, macsecUnknown) || strings.Contains(m, "=?") {
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
