package nlparity

// Wire names, for loci a human acts on.
//
// # Why names and not numbers
//
// A locus is the allowlist's key (goip-parity-allowlist.json keys on
// command + locus), and the committed entries spell theirs
// `request:RTM_GETLINK:IFLA_EXT_MASK:ll_init_map`. So names are not a
// convenience here: they are part of a persisted contract, and a report that
// said `IFLA:29` would make every reviewer open a kernel header to decide
// whether an entry was still correct.
//
// That makes the tables' completeness load-bearing in a way a diagnostic
// label's would not be. TestAttrNameCoversCorpus asserts every attribute type
// present in the guest corpus resolves to a name, so the fallback form can only
// be reached by traffic no fixture has ever recorded — which is also traffic no
// allowlist entry can be about yet.
//
// # How these were produced, and the off-by-one they would otherwise carry
//
// Generated from the kernel UAPI enums in ~/Downloads/linux at eed108edc117 —
// include/uapi/linux/if_link.h, if_addr.h, rtnetlink.h, and the NDA_* header
// beside them (whose filename the kernel spells with the British -our, so it
// is named here in prose rather than quoted) — and then
// cross-checked value by value against golang.org/x/sys/unix v0.47.0, which is
// independently generated from a kernel header. Every value x/sys knows agrees.
//
// The cross-check earned its keep immediately. `IFLA_TARGET_NETNSID` is
// declared in if_link.h as `IFLA_TARGET_NETNSID = IFLA_IF_NETNSID`, an alias
// rather than a new enumerator, so counting enum members positionally puts
// everything from IFLA_CARRIER_UP_COUNT onward one too high. x/sys confirms
// both names are 0x2e. Only the alias's canonical spelling is in the table,
// because two map keys of the same value do not compile — which is a nice
// property: the table cannot silently acquire the bug.
//
// The corpus confirms it a second time, independently. The link replies carry
// nested attribute types 0x803e and 0x8041; with the off-by-one those decode as
// a nested IFLA_ALLMULTI (a u8 flag, which cannot be a nest) and a nested
// IFLA_GRO_IPV4_MAX_SIZE (a u32). With the corrected values they are
// IFLA_DEVLINK_PORT and IFLA_DPLL_PIN, both of which are nests.
//
// Fourteen constants are absent from x/sys v0.47.0 and so appear as literals
// with the enum position they were generated from; two more — RTA_NH_ID and
// IFA_PROTO — are already hand-declared with kernel citations in pkg/xtcpnl and
// are referenced rather than re-declared.

import (
	"fmt"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// iflaNames is the top-level IFLA_* enum (if_link.h). Sub-namespaces such as
// IFLA_BR_* and IFLA_VF_* reuse these numbers and are NOT here; a nest is
// resolved by descending with the nest's own table.
var iflaNames = map[uint16]string{
	uint16(unix.IFLA_UNSPEC):              "IFLA_UNSPEC",
	uint16(unix.IFLA_ADDRESS):             "IFLA_ADDRESS",
	uint16(unix.IFLA_BROADCAST):           "IFLA_BROADCAST",
	uint16(unix.IFLA_IFNAME):              "IFLA_IFNAME",
	uint16(unix.IFLA_MTU):                 "IFLA_MTU",
	uint16(unix.IFLA_LINK):                "IFLA_LINK",
	uint16(unix.IFLA_QDISC):               "IFLA_QDISC",
	uint16(unix.IFLA_STATS):               "IFLA_STATS",
	uint16(unix.IFLA_COST):                "IFLA_COST",
	uint16(unix.IFLA_PRIORITY):            "IFLA_PRIORITY",
	uint16(unix.IFLA_MASTER):              "IFLA_MASTER",
	uint16(unix.IFLA_WIRELESS):            "IFLA_WIRELESS",
	uint16(unix.IFLA_PROTINFO):            "IFLA_PROTINFO",
	uint16(unix.IFLA_TXQLEN):              "IFLA_TXQLEN",
	uint16(unix.IFLA_MAP):                 "IFLA_MAP",
	uint16(unix.IFLA_WEIGHT):              "IFLA_WEIGHT",
	uint16(unix.IFLA_OPERSTATE):           "IFLA_OPERSTATE",
	uint16(unix.IFLA_LINKMODE):            "IFLA_LINKMODE",
	uint16(unix.IFLA_LINKINFO):            "IFLA_LINKINFO",
	uint16(unix.IFLA_NET_NS_PID):          "IFLA_NET_NS_PID",
	uint16(unix.IFLA_IFALIAS):             "IFLA_IFALIAS",
	uint16(unix.IFLA_NUM_VF):              "IFLA_NUM_VF",
	uint16(unix.IFLA_VFINFO_LIST):         "IFLA_VFINFO_LIST",
	uint16(unix.IFLA_STATS64):             "IFLA_STATS64",
	uint16(unix.IFLA_VF_PORTS):            "IFLA_VF_PORTS",
	uint16(unix.IFLA_PORT_SELF):           "IFLA_PORT_SELF",
	uint16(unix.IFLA_AF_SPEC):             "IFLA_AF_SPEC",
	uint16(unix.IFLA_GROUP):               "IFLA_GROUP",
	uint16(unix.IFLA_NET_NS_FD):           "IFLA_NET_NS_FD",
	uint16(unix.IFLA_EXT_MASK):            "IFLA_EXT_MASK",
	uint16(unix.IFLA_PROMISCUITY):         "IFLA_PROMISCUITY",
	uint16(unix.IFLA_NUM_TX_QUEUES):       "IFLA_NUM_TX_QUEUES",
	uint16(unix.IFLA_NUM_RX_QUEUES):       "IFLA_NUM_RX_QUEUES",
	uint16(unix.IFLA_CARRIER):             "IFLA_CARRIER",
	uint16(unix.IFLA_PHYS_PORT_ID):        "IFLA_PHYS_PORT_ID",
	uint16(unix.IFLA_CARRIER_CHANGES):     "IFLA_CARRIER_CHANGES",
	uint16(unix.IFLA_PHYS_SWITCH_ID):      "IFLA_PHYS_SWITCH_ID",
	uint16(unix.IFLA_LINK_NETNSID):        "IFLA_LINK_NETNSID",
	uint16(unix.IFLA_PHYS_PORT_NAME):      "IFLA_PHYS_PORT_NAME",
	uint16(unix.IFLA_PROTO_DOWN):          "IFLA_PROTO_DOWN",
	uint16(unix.IFLA_GSO_MAX_SEGS):        "IFLA_GSO_MAX_SEGS",
	uint16(unix.IFLA_GSO_MAX_SIZE):        "IFLA_GSO_MAX_SIZE",
	uint16(unix.IFLA_PAD):                 "IFLA_PAD",
	uint16(unix.IFLA_XDP):                 "IFLA_XDP",
	uint16(unix.IFLA_EVENT):               "IFLA_EVENT",
	uint16(unix.IFLA_NEW_NETNSID):         "IFLA_NEW_NETNSID",
	uint16(unix.IFLA_TARGET_NETNSID):      "IFLA_TARGET_NETNSID",
	uint16(unix.IFLA_CARRIER_UP_COUNT):    "IFLA_CARRIER_UP_COUNT",
	uint16(unix.IFLA_CARRIER_DOWN_COUNT):  "IFLA_CARRIER_DOWN_COUNT",
	uint16(unix.IFLA_NEW_IFINDEX):         "IFLA_NEW_IFINDEX",
	uint16(unix.IFLA_MIN_MTU):             "IFLA_MIN_MTU",
	uint16(unix.IFLA_MAX_MTU):             "IFLA_MAX_MTU",
	uint16(unix.IFLA_PROP_LIST):           "IFLA_PROP_LIST",
	uint16(unix.IFLA_ALT_IFNAME):          "IFLA_ALT_IFNAME",
	uint16(unix.IFLA_PERM_ADDRESS):        "IFLA_PERM_ADDRESS",
	uint16(unix.IFLA_PROTO_DOWN_REASON):   "IFLA_PROTO_DOWN_REASON",
	uint16(unix.IFLA_PARENT_DEV_NAME):     "IFLA_PARENT_DEV_NAME",
	uint16(unix.IFLA_PARENT_DEV_BUS_NAME): "IFLA_PARENT_DEV_BUS_NAME",
	uint16(unix.IFLA_GRO_MAX_SIZE):        "IFLA_GRO_MAX_SIZE",
	uint16(unix.IFLA_TSO_MAX_SIZE):        "IFLA_TSO_MAX_SIZE",
	uint16(unix.IFLA_TSO_MAX_SEGS):        "IFLA_TSO_MAX_SEGS",
	uint16(unix.IFLA_ALLMULTI):            "IFLA_ALLMULTI",
	uint16(unix.IFLA_DEVLINK_PORT):        "IFLA_DEVLINK_PORT",
	uint16(unix.IFLA_GSO_IPV4_MAX_SIZE):   "IFLA_GSO_IPV4_MAX_SIZE",
	uint16(unix.IFLA_GRO_IPV4_MAX_SIZE):   "IFLA_GRO_IPV4_MAX_SIZE",
	uint16(unix.IFLA_DPLL_PIN):            "IFLA_DPLL_PIN",
	66:                                    "IFLA_MAX_PACING_OFFLOAD_HORIZON", // absent from x/sys v0.47.0
	67:                                    "IFLA_NETNS_IMMUTABLE",            // absent from x/sys v0.47.0
	68:                                    "IFLA_HEADROOM",                   // absent from x/sys v0.47.0
	69:                                    "IFLA_TAILROOM",                   // absent from x/sys v0.47.0
}

// ifaNames is the IFA_* enum (if_addr.h).
var ifaNames = map[uint16]string{
	uint16(unix.IFA_UNSPEC):         "IFA_UNSPEC",
	uint16(unix.IFA_ADDRESS):        "IFA_ADDRESS",
	uint16(unix.IFA_LOCAL):          "IFA_LOCAL",
	uint16(unix.IFA_LABEL):          "IFA_LABEL",
	uint16(unix.IFA_BROADCAST):      "IFA_BROADCAST",
	uint16(unix.IFA_ANYCAST):        "IFA_ANYCAST",
	uint16(unix.IFA_CACHEINFO):      "IFA_CACHEINFO",
	uint16(unix.IFA_MULTICAST):      "IFA_MULTICAST",
	uint16(unix.IFA_FLAGS):          "IFA_FLAGS",
	uint16(unix.IFA_RT_PRIORITY):    "IFA_RT_PRIORITY",
	uint16(unix.IFA_TARGET_NETNSID): "IFA_TARGET_NETNSID",
	xtcpnl.IfaProto:                 "IFA_PROTO",
}

// rtaNames is the RTA_* enum (rtnetlink.h).
var rtaNames = map[uint16]string{
	uint16(unix.RTA_UNSPEC):        "RTA_UNSPEC",
	uint16(unix.RTA_DST):           "RTA_DST",
	uint16(unix.RTA_SRC):           "RTA_SRC",
	uint16(unix.RTA_IIF):           "RTA_IIF",
	uint16(unix.RTA_OIF):           "RTA_OIF",
	uint16(unix.RTA_GATEWAY):       "RTA_GATEWAY",
	uint16(unix.RTA_PRIORITY):      "RTA_PRIORITY",
	uint16(unix.RTA_PREFSRC):       "RTA_PREFSRC",
	uint16(unix.RTA_METRICS):       "RTA_METRICS",
	uint16(unix.RTA_MULTIPATH):     "RTA_MULTIPATH",
	10:                             "RTA_PROTOINFO", // absent from x/sys v0.47.0
	uint16(unix.RTA_FLOW):          "RTA_FLOW",
	uint16(unix.RTA_CACHEINFO):     "RTA_CACHEINFO",
	13:                             "RTA_SESSION", // absent from x/sys v0.47.0
	14:                             "RTA_MP_ALGO", // absent from x/sys v0.47.0
	uint16(unix.RTA_TABLE):         "RTA_TABLE",
	uint16(unix.RTA_MARK):          "RTA_MARK",
	uint16(unix.RTA_MFC_STATS):     "RTA_MFC_STATS",
	uint16(unix.RTA_VIA):           "RTA_VIA",
	uint16(unix.RTA_NEWDST):        "RTA_NEWDST",
	uint16(unix.RTA_PREF):          "RTA_PREF",
	uint16(unix.RTA_ENCAP_TYPE):    "RTA_ENCAP_TYPE",
	uint16(unix.RTA_ENCAP):         "RTA_ENCAP",
	uint16(unix.RTA_EXPIRES):       "RTA_EXPIRES",
	uint16(unix.RTA_PAD):           "RTA_PAD",
	uint16(unix.RTA_UID):           "RTA_UID",
	uint16(unix.RTA_TTL_PROPAGATE): "RTA_TTL_PROPAGATE",
	uint16(unix.RTA_IP_PROTO):      "RTA_IP_PROTO",
	uint16(unix.RTA_SPORT):         "RTA_SPORT",
	uint16(unix.RTA_DPORT):         "RTA_DPORT",
	xtcpnl.RtaNhID:                 "RTA_NH_ID",
	31:                             "RTA_FLOWLABEL", // absent from x/sys v0.47.0
}

// ndaNames is the NDA_* enum, from the fourth header named at the top of this
// file.
var ndaNames = map[uint16]string{
	uint16(unix.NDA_UNSPEC):       "NDA_UNSPEC",
	uint16(unix.NDA_DST):          "NDA_DST",
	uint16(unix.NDA_LLADDR):       "NDA_LLADDR",
	uint16(unix.NDA_CACHEINFO):    "NDA_CACHEINFO",
	uint16(unix.NDA_PROBES):       "NDA_PROBES",
	uint16(unix.NDA_VLAN):         "NDA_VLAN",
	uint16(unix.NDA_PORT):         "NDA_PORT",
	uint16(unix.NDA_VNI):          "NDA_VNI",
	uint16(unix.NDA_IFINDEX):      "NDA_IFINDEX",
	uint16(unix.NDA_MASTER):       "NDA_MASTER",
	uint16(unix.NDA_LINK_NETNSID): "NDA_LINK_NETNSID",
	uint16(unix.NDA_SRC_VNI):      "NDA_SRC_VNI",
	12:                            "NDA_PROTOCOL",       // absent from x/sys v0.47.0
	13:                            "NDA_NH_ID",          // absent from x/sys v0.47.0
	14:                            "NDA_FDB_EXT_ATTRS",  // absent from x/sys v0.47.0
	15:                            "NDA_FLAGS_EXT",      // absent from x/sys v0.47.0
	16:                            "NDA_NDM_STATE_MASK", // absent from x/sys v0.47.0
	17:                            "NDA_NDM_FLAGS_MASK", // absent from x/sys v0.47.0
}

// inet6Names is the IFLA_INET6_* enum (if_link.h), the nest reached by
// descending IFLA_AF_SPEC into its AF_INET6 member. It is here because
// IFLA_INET6_CACHEINFO is one of the four volatile payloads normalization has
// to find, so the nest is walked by name rather than skipped as opaque bytes.
var inet6Names = map[uint16]string{
	uint16(unix.IFLA_INET6_UNSPEC):        "IFLA_INET6_UNSPEC",
	uint16(unix.IFLA_INET6_FLAGS):         "IFLA_INET6_FLAGS",
	uint16(unix.IFLA_INET6_CONF):          "IFLA_INET6_CONF",
	uint16(unix.IFLA_INET6_STATS):         "IFLA_INET6_STATS",
	uint16(unix.IFLA_INET6_MCAST):         "IFLA_INET6_MCAST",
	uint16(unix.IFLA_INET6_CACHEINFO):     "IFLA_INET6_CACHEINFO",
	uint16(unix.IFLA_INET6_ICMP6STATS):    "IFLA_INET6_ICMP6STATS",
	uint16(unix.IFLA_INET6_TOKEN):         "IFLA_INET6_TOKEN",
	uint16(unix.IFLA_INET6_ADDR_GEN_MODE): "IFLA_INET6_ADDR_GEN_MODE",
	uint16(unix.IFLA_INET6_RA_MTU):        "IFLA_INET6_RA_MTU",
}

// AttrFamily is the attribute namespace a message type's attributes live in.
// It is a string rather than an enum because its only two uses are selecting a
// table and prefixing a fallback locus.
type AttrFamily string

// The four namespaces this package compares, plus the empty one for a message
// type that carries no attributes it can name.
const (
	AttrFamilyIFLA AttrFamily = "IFLA"
	AttrFamilyIFA  AttrFamily = "IFA"
	AttrFamilyRTA  AttrFamily = "RTA"
	AttrFamilyNDA  AttrFamily = "NDA"
	AttrFamilyNone AttrFamily = ""
)

// AttrFamilyOf maps a message type to its attribute namespace.
//
// All three verbs of each family are listed, not just the RTM_NEW* a dump
// replies with: a comparator reads requests too, and a GET request's attributes
// are in the same namespace as the reply's. RTM_DEL* is here for completeness
// of the mapping, not because this package emits one — pkg/xtcpnl's read-only
// invariant forbids that, and BuildRequest enforces it.
func AttrFamilyOf(msgType uint16) AttrFamily {
	switch msgType {
	case uint16(unix.RTM_NEWLINK), uint16(unix.RTM_DELLINK), uint16(unix.RTM_GETLINK):
		return AttrFamilyIFLA
	case uint16(unix.RTM_NEWADDR), uint16(unix.RTM_DELADDR), uint16(unix.RTM_GETADDR):
		return AttrFamilyIFA
	case uint16(unix.RTM_NEWROUTE), uint16(unix.RTM_DELROUTE), uint16(unix.RTM_GETROUTE):
		return AttrFamilyRTA
	case uint16(unix.RTM_NEWNEIGH), uint16(unix.RTM_DELNEIGH), uint16(unix.RTM_GETNEIGH):
		return AttrFamilyNDA
	default:
		return AttrFamilyNone
	}
}

// attrTables indexes the four namespaces, so AttrName is a lookup rather than a
// second switch that could disagree with AttrFamilyOf.
var attrTables = map[AttrFamily]map[uint16]string{
	AttrFamilyIFLA: iflaNames,
	AttrFamilyIFA:  ifaNames,
	AttrFamilyRTA:  rtaNames,
	AttrFamilyNDA:  ndaNames,
}

// MsgTypeName names a netlink message type for a locus.
//
// The set is the NLMSG_* control types plus the twelve rtnetlink types this
// package can see. Anything else renders as a number, which is correct rather
// than lossy: a message type nothing in the corpus produces has no name worth
// asserting, and inventing one would make an unknown type look understood.
func MsgTypeName(msgType uint16) string {
	switch msgType {
	case uint16(unix.NLMSG_NOOP):
		return "NLMSG_NOOP"
	case uint16(unix.NLMSG_ERROR):
		return "NLMSG_ERROR"
	case uint16(unix.NLMSG_DONE):
		return "NLMSG_DONE"
	case uint16(unix.NLMSG_OVERRUN):
		return "NLMSG_OVERRUN"
	case uint16(unix.RTM_NEWLINK):
		return "RTM_NEWLINK"
	case uint16(unix.RTM_DELLINK):
		return "RTM_DELLINK"
	case uint16(unix.RTM_GETLINK):
		return "RTM_GETLINK"
	case uint16(unix.RTM_NEWADDR):
		return "RTM_NEWADDR"
	case uint16(unix.RTM_DELADDR):
		return "RTM_DELADDR"
	case uint16(unix.RTM_GETADDR):
		return "RTM_GETADDR"
	case uint16(unix.RTM_NEWROUTE):
		return "RTM_NEWROUTE"
	case uint16(unix.RTM_DELROUTE):
		return "RTM_DELROUTE"
	case uint16(unix.RTM_GETROUTE):
		return "RTM_GETROUTE"
	case uint16(unix.RTM_NEWNEIGH):
		return "RTM_NEWNEIGH"
	case uint16(unix.RTM_DELNEIGH):
		return "RTM_DELNEIGH"
	case uint16(unix.RTM_GETNEIGH):
		return "RTM_GETNEIGH"
	default:
		return fmt.Sprintf("type(%d)", msgType)
	}
}

// AttrName names one attribute of one message type, WITH its flag bits
// rendered, for use in a locus.
//
// The flags are part of the name and not stripped, because Attr.Type is
// compared unmasked: a goip that builds a nest without setting NLA_F_NESTED, or
// renders a big-endian payload as host order, is a bug, and a locus that had
// already masked the flag off would describe the two cases identically. So
// IFLA_DEVLINK_PORT and IFLA_DEVLINK_PORT+NESTED are different loci, which is
// what lets the type-list comparison report the flag difference as one
// attribute missing and another extra.
//
// An unnamed type renders as FAMILY:n, or as attr(n) when the message type has
// no namespace at all — NLMSG_DONE's four bytes, say, which are not attributes.
func AttrName(msgType uint16, attrType uint16) string {
	fam := AttrFamilyOf(msgType)
	bare := attrType & xtcpnl.NlaTypeMaskCst

	var name string
	switch tbl, ok := attrTables[fam]; {
	case !ok:
		name = fmt.Sprintf("attr(%d)", bare)
	default:
		if n, found := tbl[bare]; found {
			name = n
		} else {
			name = fmt.Sprintf("%s:%d", fam, bare)
		}
	}

	if attrType&uint16(unix.NLA_F_NESTED) != 0 {
		name += "+NESTED"
	}
	if attrType&uint16(unix.NLA_F_NET_BYTEORDER) != 0 {
		name += "+NETORDER"
	}
	return name
}

// SubAttrName names an attribute inside a nest, given the nest's own namespace.
// Only the IFLA_AF_SPEC AF_INET6 nest is named today, because it is the only
// nest normalization descends into; everything else renders as a number, which
// keeps the locus stable without claiming knowledge the package does not have.
func SubAttrName(nest map[uint16]string, attrType uint16) string {
	bare := attrType & xtcpnl.NlaTypeMaskCst
	if n, ok := nest[bare]; ok {
		return n
	}
	return fmt.Sprintf("nested:%d", bare)
}
