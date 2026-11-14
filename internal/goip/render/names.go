package render

import (
	"fmt"
	"strconv"
)

// The name tables iproute2 reads from files, compiled in
// (goip-parity-next.md, NX7f). ip reads /etc/iproute2/rt_dsfield (else its
// CONF_USR_DIR copy) and /etc/protocols on whichever machine runs it; goip
// carries the files the parity guest has: the rt_dsfield iproute2 7.2.0
// ships, and the protocols file of NixOS's iana-etc 20251215. A name a host
// adds to either file is unknown to goip, which prints the number as ip
// would without that line.

// dsfieldNames is etc/iproute2/rt_dsfield as iproute2 7.2.0 and 7.3.0 ship
// it, by value.
var dsfieldNames = map[uint8]string{
	0x00: dsfieldDefaultCst,
	0x28: "AF11", 0x30: "AF12", 0x38: "AF13",
	0x48: "AF21", 0x50: "AF22", 0x58: "AF23",
	0x68: "AF31", 0x70: "AF32", 0x78: "AF33",
	0x88: "AF41", 0x90: "AF42", 0x98: "AF43",
	0x20: "CS1", 0x40: "CS2", 0x60: "CS3", 0x80: "CS4",
	0xA0: "CS5", 0xC0: "CS6", 0xE0: "CS7",
	0xB8: "EF",
}

// dsfieldDefaultCst is rt_dsfield's name for DS field 0.
const dsfieldDefaultCst = "default"

// protoICMPCst is /etc/protocols' name for protocol 1.
const protoICMPCst = "icmp"

// DsfieldByName is rtnl_dsfield_a2n's name lookup, which loads the file
// first, so "default" is 0 and "0" is no name.
func DsfieldByName(name string) (uint8, bool) {
	for v, n := range dsfieldNames {
		if n == name {
			return v, true
		}
	}
	return 0, false
}

// Dsfield is one ip process's rtnl_rtdsfield_tab (lib/rt_names.c:588-644).
// The table starts as the one entry `[0] = "0"` and the file is loaded on
// the first lookup of an empty slot, or by rtnl_dsfield_a2n. Slot 0 is
// never empty, so DS field 0 prints "0" until something else has loaded the
// file and "default" after: `ip rule show` prints `dscp 0` for a rule whose
// dscp is the first lookup, and `dscp default` once an earlier rule's tos or
// dscp has loaded it. The zero value is a process that has looked nothing
// up.
type Dsfield struct {
	loaded bool
}

// Load is rtnl_rtdsfield_initialize, as rtnl_dsfield_a2n calls it.
func (d *Dsfield) Load() { d.loaded = true }

// name is rtnl_dsfield_get_name.
func (d *Dsfield) name(id int) (string, bool) {
	if id < 0 || id >= 256 {
		return "", false
	}
	if id == 0 && !d.loaded {
		return "0", true
	}
	d.loaded = true
	n, ok := dsfieldNames[uint8(id)]
	return n, ok
}

// N2A is rtnl_dsfield_n2a: the name, else `0x%02x`.
func (d *Dsfield) N2A(id uint8) string {
	if n, ok := d.name(int(id)); ok {
		return n
	}
	return fmt.Sprintf("0x%02x", id)
}

// DscpN2A is rtnl_dscp_n2a: the name of the DS field the DSCP fills, else
// the DSCP in decimal. A DSCP over 63 shifts past the table and loads
// nothing.
func (d *Dsfield) DscpN2A(dscp uint8) string {
	if n, ok := d.name(int(dscp) << 2); ok {
		return n
	}
	return strconv.FormatUint(uint64(dscp), 10)
}

// ipProtocol is one line of /etc/protocols: the name, the number and the
// alias.
type ipProtocol struct {
	num         uint8
	name, alias string
}

// ipProtocols is NixOS's iana-etc 20251215 /etc/protocols, in file order.
// Every number and name in it is unique, and each line has one alias.
var ipProtocols = []ipProtocol{
	{num: 0, name: "hopopt", alias: "HOPOPT"},
	{num: 1, name: protoICMPCst, alias: "ICMP"},
	{num: 2, name: "igmp", alias: "IGMP"},
	{num: 3, name: "ggp", alias: "GGP"},
	{num: 4, name: "ipv4", alias: "IPv4"},
	{num: 5, name: "st", alias: "ST"},
	{num: 6, name: "tcp", alias: "TCP"},
	{num: 7, name: "cbt", alias: "CBT"},
	{num: 8, name: "egp", alias: "EGP"},
	{num: 9, name: "igp", alias: "IGP"},
	{num: 10, name: "bbn-rcc-mon", alias: "BBN-RCC-MON"},
	{num: 11, name: "nvp-ii", alias: "NVP-II"},
	{num: 12, name: "pup", alias: "PUP"},
	{num: 14, name: "emcon", alias: "EMCON"},
	{num: 15, name: "xnet", alias: "XNET"},
	{num: 16, name: "chaos", alias: "CHAOS"},
	{num: 17, name: "udp", alias: "UDP"},
	{num: 18, name: "mux", alias: "MUX"},
	{num: 19, name: "dcn-meas", alias: "DCN-MEAS"},
	{num: 20, name: "hmp", alias: "HMP"},
	{num: 21, name: "prm", alias: "PRM"},
	{num: 22, name: "xns-idp", alias: "XNS-IDP"},
	{num: 23, name: "trunk-1", alias: "TRUNK-1"},
	{num: 24, name: "trunk-2", alias: "TRUNK-2"},
	{num: 25, name: "leaf-1", alias: "LEAF-1"},
	{num: 26, name: "leaf-2", alias: "LEAF-2"},
	{num: 27, name: "rdp", alias: "RDP"},
	{num: 28, name: "irtp", alias: "IRTP"},
	{num: 29, name: "iso-tp4", alias: "ISO-TP4"},
	{num: 30, name: "netblt", alias: "NETBLT"},
	{num: 31, name: "mfe-nsp", alias: "MFE-NSP"},
	{num: 32, name: "merit-inp", alias: "MERIT-INP"},
	{num: 33, name: "dccp", alias: "DCCP"},
	{num: 34, name: "3pc", alias: "3PC"},
	{num: 35, name: "idpr", alias: "IDPR"},
	{num: 36, name: "xtp", alias: "XTP"},
	{num: 37, name: "ddp", alias: "DDP"},
	{num: 38, name: "idpr-cmtp", alias: "IDPR-CMTP"},
	{num: 39, name: "tp++", alias: "TP++"},
	{num: 40, name: "il", alias: "IL"},
	{num: 41, name: "ipv6", alias: "IPv6"},
	{num: 42, name: "sdrp", alias: "SDRP"},
	{num: 43, name: "ipv6-route", alias: "IPv6-Route"},
	{num: 44, name: "ipv6-frag", alias: "IPv6-Frag"},
	{num: 45, name: "idrp", alias: "IDRP"},
	{num: 46, name: "rsvp", alias: "RSVP"},
	{num: 47, name: "gre", alias: "GRE"},
	{num: 48, name: "dsr", alias: "DSR"},
	{num: 49, name: "bna", alias: "BNA"},
	{num: 50, name: "esp", alias: "ESP"},
	{num: 51, name: "ah", alias: "AH"},
	{num: 52, name: "i-nlsp", alias: "I-NLSP"},
	{num: 54, name: "narp", alias: "NARP"},
	{num: 55, name: "min-ipv4", alias: "Min-IPv4"},
	{num: 56, name: "tlsp", alias: "TLSP"},
	{num: 57, name: "skip", alias: "SKIP"},
	{num: 58, name: "ipv6-icmp", alias: "IPv6-ICMP"},
	{num: 59, name: "ipv6-nonxt", alias: "IPv6-NoNxt"},
	{num: 60, name: "ipv6-opts", alias: "IPv6-Opts"},
	{num: 62, name: "cftp", alias: "CFTP"},
	{num: 64, name: "sat-expak", alias: "SAT-EXPAK"},
	{num: 65, name: "kryptolan", alias: "KRYPTOLAN"},
	{num: 66, name: "rvd", alias: "RVD"},
	{num: 67, name: "ippc", alias: "IPPC"},
	{num: 69, name: "sat-mon", alias: "SAT-MON"},
	{num: 70, name: "visa", alias: "VISA"},
	{num: 71, name: "ipcv", alias: "IPCV"},
	{num: 72, name: "cpnx", alias: "CPNX"},
	{num: 73, name: "cphb", alias: "CPHB"},
	{num: 74, name: "wsn", alias: "WSN"},
	{num: 75, name: "pvp", alias: "PVP"},
	{num: 76, name: "br-sat-mon", alias: "BR-SAT-MON"},
	{num: 77, name: "sun-nd", alias: "SUN-ND"},
	{num: 78, name: "wb-mon", alias: "WB-MON"},
	{num: 79, name: "wb-expak", alias: "WB-EXPAK"},
	{num: 80, name: "iso-ip", alias: "ISO-IP"},
	{num: 81, name: "vmtp", alias: "VMTP"},
	{num: 82, name: "secure-vmtp", alias: "SECURE-VMTP"},
	{num: 83, name: "vines", alias: "VINES"},
	{num: 84, name: "iptm", alias: "IPTM"},
	{num: 85, name: "nsfnet-igp", alias: "NSFNET-IGP"},
	{num: 86, name: "dgp", alias: "DGP"},
	{num: 87, name: "tcf", alias: "TCF"},
	{num: 88, name: "eigrp", alias: "EIGRP"},
	{num: 89, name: "ospfigp", alias: "OSPFIGP"},
	{num: 90, name: "sprite-rpc", alias: "Sprite-RPC"},
	{num: 91, name: "larp", alias: "LARP"},
	{num: 92, name: "mtp", alias: "MTP"},
	{num: 93, name: "ax.25", alias: "AX.25"},
	{num: 94, name: "ipip", alias: "IPIP"},
	{num: 96, name: "scc-sp", alias: "SCC-SP"},
	{num: 97, name: "etherip", alias: "ETHERIP"},
	{num: 98, name: encapCst, alias: "ENCAP"},
	{num: 100, name: "gmtp", alias: "GMTP"},
	{num: 101, name: "ifmp", alias: "IFMP"},
	{num: 102, name: "pnni", alias: "PNNI"},
	{num: 103, name: "pim", alias: "PIM"},
	{num: 104, name: "aris", alias: "ARIS"},
	{num: 105, name: "scps", alias: "SCPS"},
	{num: 106, name: "qnx", alias: "QNX"},
	{num: 107, name: "a/n", alias: "A/N"},
	{num: 108, name: "ipcomp", alias: "IPComp"},
	{num: 109, name: "snp", alias: "SNP"},
	{num: 110, name: "compaq-peer", alias: "Compaq-Peer"},
	{num: 111, name: "ipx-in-ip", alias: "IPX-in-IP"},
	{num: 112, name: "vrrp", alias: "VRRP"},
	{num: 113, name: "pgm", alias: "PGM"},
	{num: 115, name: "l2tp", alias: "L2TP"},
	{num: 116, name: "ddx", alias: "DDX"},
	{num: 117, name: "iatp", alias: "IATP"},
	{num: 118, name: "stp", alias: "STP"},
	{num: 119, name: "srp", alias: "SRP"},
	{num: 120, name: "uti", alias: "UTI"},
	{num: 121, name: "smp", alias: "SMP"},
	{num: 123, name: "ptp", alias: "PTP"},
	{num: 125, name: "fire", alias: "FIRE"},
	{num: 126, name: "crtp", alias: "CRTP"},
	{num: 127, name: "crudp", alias: "CRUDP"},
	{num: 128, name: "sscopmce", alias: "SSCOPMCE"},
	{num: 129, name: "iplt", alias: "IPLT"},
	{num: 130, name: "sps", alias: "SPS"},
	{num: 131, name: "pipe", alias: "PIPE"},
	{num: 132, name: "sctp", alias: "SCTP"},
	{num: 133, name: "fc", alias: "FC"},
	{num: 134, name: "rsvp-e2e-ignore", alias: "RSVP-E2E-IGNORE"},
	{num: 136, name: "udplite", alias: "UDPLite"},
	{num: 137, name: "mpls-in-ip", alias: "MPLS-in-IP"},
	{num: 138, name: "manet", alias: "manet"},
	{num: 139, name: "hip", alias: "HIP"},
	{num: 140, name: "shim6", alias: "Shim6"},
	{num: 141, name: "wesp", alias: "WESP"},
	{num: 142, name: "rohc", alias: "ROHC"},
	{num: 143, name: "ethernet", alias: "Ethernet"},
	{num: 144, name: "aggfrag", alias: "AGGFRAG"},
	{num: 145, name: "nsh", alias: "NSH"},
	{num: 146, name: "homa", alias: "Homa"},
	{num: 147, name: "bit-emu", alias: "BIT-EMU"},
}

// ProtoByName is getprotobyname: the first line whose name or alias is
// name, which is case-sensitive.
func ProtoByName(name string) (uint8, bool) {
	for _, p := range ipProtocols {
		if p.name == name || p.alias == name {
			return p.num, true
		}
	}
	return 0, false
}

// ProtoByNumber is getprotobynumber's p_name.
func ProtoByNumber(proto uint8) (string, bool) {
	for _, p := range ipProtocols {
		if p.num == proto {
			return p.name, true
		}
	}
	return "", false
}

// InetProtoN2A is inet_proto_n2a (lib/inet_proto.c:20-40): the name, else
// `ipproto-%d`.
func InetProtoN2A(proto uint8) string {
	if n, ok := ProtoByNumber(proto); ok {
		return n
	}
	return "ipproto-" + strconv.Itoa(int(proto))
}

// StrxfProto is strxf_proto (ip/ipxfrm.c:222-237): the name, else `%u`.
func StrxfProto(proto uint8) string {
	if n, ok := ProtoByNumber(proto); ok {
		return n
	}
	return strconv.Itoa(int(proto))
}

// rtRealmsMaxCst is one past rtnl_rtrealm_tab's last slot; a larger realm
// prints as its number without a lookup.
const rtRealmsMaxCst = 256

// Realms is rtnl_rtrealm_n2a's table (lib/rt_names.c:438-470) in one ip
// process, with the lazy load Dsfield has (R2): the built-in table names
// realm 0 "unknown", and the stock rt_realms, loaded by the first lookup of
// an empty slot, renames it "cosmos" and names nothing else. So `realm 0`
// prints "unknown" until another realm below 256 has been looked up in the
// same process, and "cosmos" after. The zero value has looked nothing up.
type Realms struct {
	loaded bool
}

// N2A is rtnl_rtrealm_n2a.
func (r *Realms) N2A(id uint32) string {
	switch {
	case id >= rtRealmsMaxCst:
		return strconv.FormatUint(uint64(id), 10)
	case id == 0 && !r.loaded:
		return "unknown"
	}
	r.loaded = true
	if id == 0 {
		return "cosmos"
	}
	return strconv.FormatUint(uint64(id), 10)
}

// FlowView is print_rta_flow's RTA_FLOW: TO in the low 16 bits, FROM in the
// high, FROM printed only when non-zero. JSON is {"from", "to"}; the text
// is `realm TO ` or `realms FROM/TO `.
type FlowView struct {
	From string `json:"from,omitempty"`
	To   string `json:"to"`
}

// FlowViewOf names RTA_FLOW's two halves, FROM first, as ip looks them up.
func FlowViewOf(flow uint32, r *Realms) *FlowView {
	var v FlowView
	if from := flow >> 16; from != 0 {
		v.From = r.N2A(from)
	}
	v.To = r.N2A(flow & 0xFFFF)
	return &v
}

// Text is print_rta_flow's text form.
func (v *FlowView) Text() string {
	if v.From != "" {
		return "realms " + v.From + "/" + v.To + " "
	}
	return "realm " + v.To + " "
}
