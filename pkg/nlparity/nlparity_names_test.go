package nlparity

import (
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// The name tables' table — the half of §8.9 that makes a locus a contract.
//
// These functions look like a diagnostic convenience and are not. The committed
// allowlist matches on loci such as
// "request:RTM_GETLINK:IFLA_EXT_MASK:dump", so every name here is a persisted
// key: a table that spells an attribute differently tomorrow silently unmatches
// an entry, and the check goes green for a reason nobody chose.
//
// That is why TestAttrNameCoversCorpus exists and why it walks every committed
// guest capture rather than a sampled one. It is the assertion that the FAMILY:n
// fallback is reachable only by traffic no fixture has recorded — if a real
// reply ever resolves to "IFLA:71", the table is behind the kernel and the
// locus for that attribute is a number that will change meaning under us.

// nameRow is one naming case.
type nameRow struct {
	description string
	msgType     uint16
	attrType    uint16
	want        string
}

// TestMsgTypeName covers the type half of a locus.
//
// go test ./pkg/nlparity/ -run TestMsgTypeName
func TestMsgTypeName(t *testing.T) {
	tests := []struct {
		description string
		msgType     uint16
		want        string
	}{
		{
			description: "positive: RTM_GETLINK, the request type every command in the corpus opens with",
			msgType:     uint16(unix.RTM_GETLINK),
			want:        "RTM_GETLINK",
		},
		{
			description: "positive: RTM_NEWNEIGH, a reply type — all three verbs of a family are named",
			msgType:     uint16(unix.RTM_NEWNEIGH),
			want:        "RTM_NEWNEIGH",
		},
		{
			description: "positive: NLMSG_DONE, the terminator that ends every dump in the corpus",
			msgType:     uint16(unix.NLMSG_DONE),
			want:        "NLMSG_DONE",
		},
		{
			description: "positive: NLMSG_ERROR, which is also the ACK for a request with errno 0",
			msgType:     uint16(unix.NLMSG_ERROR),
			want:        "NLMSG_ERROR",
		},
		{
			// The four NLMSG_* control types are 1..4 (NOOP, ERROR, DONE,
			// OVERRUN) and NLMSG_MIN_TYPE is 0x10, so 5 is the first value in
			// the reserved gap between them and the RTM_* block. It is the
			// lowest type MsgTypeName can legitimately refuse to name.
			description: "boundary: type 5 — the first value above NLMSG_OVERRUN and below NLMSG_MIN_TYPE",
			msgType:     5,
			want:        "type(5)",
		},
		{
			description: "negative: RTM_NEWRULE is real rtnetlink this package deliberately does not model",
			msgType:     uint16(unix.RTM_NEWRULE),
			want:        "type(32)",
		},
		{
			description: "corner: 0xFFFF renders as a number rather than being invented a name",
			msgType:     0xFFFF,
			want:        "type(65535)",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := MsgTypeName(tc.msgType); got != tc.want {
				t.Errorf("MsgTypeName(%d) = %q, want %q", tc.msgType, got, tc.want)
			}
		})
	}
}

// TestAttrFamilyOf asserts the namespace dispatch, which both AttrName and
// normalizeAttrs key on — so a wrong answer here normalizes the wrong offsets
// AND names the wrong attribute, in agreement, which is the worst way to be
// wrong.
//
// go test ./pkg/nlparity/ -run TestAttrFamilyOf
func TestAttrFamilyOf(t *testing.T) {
	tests := []struct {
		description string
		msgType     uint16
		want        AttrFamily
	}{
		{"positive: RTM_NEWLINK is IFLA", uint16(unix.RTM_NEWLINK), AttrFamilyIFLA},
		{"positive: RTM_DELLINK is IFLA — the DEL verb shares the namespace", uint16(unix.RTM_DELLINK), AttrFamilyIFLA},
		{"positive: RTM_GETLINK is IFLA — the GET verb too, which is what makes requests nameable", uint16(unix.RTM_GETLINK), AttrFamilyIFLA},
		{"positive: RTM_NEWADDR is IFA", uint16(unix.RTM_NEWADDR), AttrFamilyIFA},
		{"positive: RTM_GETADDR is IFA", uint16(unix.RTM_GETADDR), AttrFamilyIFA},
		{"positive: RTM_NEWROUTE is RTA", uint16(unix.RTM_NEWROUTE), AttrFamilyRTA},
		{"positive: RTM_GETROUTE is RTA", uint16(unix.RTM_GETROUTE), AttrFamilyRTA},
		{"positive: RTM_NEWNEIGH is NDA", uint16(unix.RTM_NEWNEIGH), AttrFamilyNDA},
		{"positive: RTM_GETNEIGH is NDA", uint16(unix.RTM_GETNEIGH), AttrFamilyNDA},
		{
			// NLMSG_DONE's payload is a four-byte errno-ish int, not an
			// attribute stream, so naming its bytes would be a lie rather than
			// a gap.
			description: "negative: NLMSG_DONE has no attribute namespace",
			msgType:     uint16(unix.NLMSG_DONE),
			want:        AttrFamilyNone,
		},
		{
			description: "negative: NLMSG_ERROR has no attribute namespace either",
			msgType:     uint16(unix.NLMSG_ERROR),
			want:        AttrFamilyNone,
		},
		{
			description: "boundary: RTM_NEWRULE is adjacent to the modeled block and still None",
			msgType:     uint16(unix.RTM_NEWRULE),
			want:        AttrFamilyNone,
		},
		{
			description: "corner: message type 0 is NLMSG_NOOP, not a family",
			msgType:     0,
			want:        AttrFamilyNone,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := AttrFamilyOf(tc.msgType); got != tc.want {
				t.Errorf("AttrFamilyOf(%d) = %q, want %q", tc.msgType, got, tc.want)
			}
		})
	}
}

// TestAttrName is the locus-spelling table.
//
// go test ./pkg/nlparity/ -run TestAttrName$
func TestAttrName(t *testing.T) {
	tests := []nameRow{
		{
			// The attribute the whole parity effort turns on, and the one the
			// committed allowlist names in two entries.
			description: "positive: IFLA_EXT_MASK on an RTM_GETLINK request",
			msgType:     uint16(unix.RTM_GETLINK),
			attrType:    uint16(unix.IFLA_EXT_MASK),
			want:        "IFLA_EXT_MASK",
		},
		{
			description: "positive: IFA_CACHEINFO on an RTM_NEWADDR reply",
			msgType:     uint16(unix.RTM_NEWADDR),
			attrType:    uint16(unix.IFA_CACHEINFO),
			want:        "IFA_CACHEINFO",
		},
		{
			description: "positive: RTA_PRIORITY on an RTM_NEWROUTE reply",
			msgType:     uint16(unix.RTM_NEWROUTE),
			attrType:    uint16(unix.RTA_PRIORITY),
			want:        "RTA_PRIORITY",
		},
		{
			description: "positive: NDA_LLADDR on an RTM_NEWNEIGH reply",
			msgType:     uint16(unix.RTM_NEWNEIGH),
			attrType:    uint16(unix.NDA_LLADDR),
			want:        "NDA_LLADDR",
		},
		{
			// IFLA_TARGET_NETNSID is an ALIAS of IFLA_IF_NETNSID in
			// if_link.h, not a new enumerator — counting the enum positionally
			// puts everything from IFLA_CARRIER_UP_COUNT onward one too high.
			// This row pins the resolution: 0x2e is the netnsid, and 0x2f is
			// the carrier counter immediately after it.
			description: "boundary: 0x2e is IFLA_TARGET_NETNSID, the aliased value the enum walk got wrong",
			msgType:     uint16(unix.RTM_NEWLINK),
			attrType:    0x2e,
			want:        "IFLA_TARGET_NETNSID",
		},
		{
			description: "boundary: 0x2f is IFLA_CARRIER_UP_COUNT — the value directly after the alias",
			msgType:     uint16(unix.RTM_NEWLINK),
			attrType:    0x2f,
			want:        "IFLA_CARRIER_UP_COUNT",
		},
		{
			// 0x803e is NLA_F_NESTED|0x3e. Under an off-by-one table this
			// resolved to IFLA_ALLMULTI, a u8 that cannot be a nest — which is
			// how the alias bug was caught a second time, independently.
			description: "corner: NLA_F_NESTED is part of the name, so 0x803e is IFLA_DEVLINK_PORT+NESTED",
			msgType:     uint16(unix.RTM_NEWLINK),
			attrType:    0x803e,
			want:        "IFLA_DEVLINK_PORT+NESTED",
		},
		{
			description: "corner: NLA_F_NET_BYTEORDER is part of the name too",
			msgType:     uint16(unix.RTM_NEWROUTE),
			attrType:    uint16(unix.NLA_F_NET_BYTEORDER) | uint16(unix.RTA_DST),
			want:        "RTA_DST+NETORDER",
		},
		{
			description: "corner: both flags render, in a fixed order, so one locus never spells two ways",
			msgType:     uint16(unix.RTM_NEWLINK),
			attrType:    uint16(unix.NLA_F_NESTED) | uint16(unix.NLA_F_NET_BYTEORDER) | uint16(unix.IFLA_AF_SPEC),
			want:        "IFLA_AF_SPEC+NESTED+NETORDER",
		},
		{
			// 16000 is far above IFLA_MAX and under the flag bits, so it
			// reaches the fallback without touching them.
			description: "negative: an unnamed IFLA type renders as FAMILY:n, not as a guess",
			msgType:     uint16(unix.RTM_NEWLINK),
			attrType:    16000,
			want:        "IFLA:16000",
		},
		{
			description: "negative: an attribute on a type with no namespace renders as attr(n)",
			msgType:     uint16(unix.NLMSG_DONE),
			attrType:    3,
			want:        "attr(3)",
		},
		{
			description: "boundary: attribute type 0 is the family's UNSPEC, which is named rather than falling back",
			msgType:     uint16(unix.RTM_NEWADDR),
			attrType:    0,
			want:        "IFA_UNSPEC",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := AttrName(tc.msgType, tc.attrType); got != tc.want {
				t.Errorf("AttrName(%d, 0x%04x) = %q, want %q", tc.msgType, tc.attrType, got, tc.want)
			}
		})
	}
}

// TestSubAttrName covers the nested namespace, which has exactly one member
// today because normalization descends into exactly one nest.
//
// go test ./pkg/nlparity/ -run TestSubAttrName
func TestSubAttrName(t *testing.T) {
	tests := []struct {
		description string
		nest        map[uint16]string
		attrType    uint16
		want        string
	}{
		{
			description: "positive: IFLA_INET6_CACHEINFO, the one nested attribute normalization reaches",
			nest:        inet6Names,
			attrType:    uint16(unix.IFLA_INET6_CACHEINFO),
			want:        "IFLA_INET6_CACHEINFO",
		},
		{
			description: "positive: IFLA_INET6_FLAGS, its sibling in the same nest",
			nest:        inet6Names,
			attrType:    uint16(unix.IFLA_INET6_FLAGS),
			want:        "IFLA_INET6_FLAGS",
		},
		{
			description: "corner: the flag bits are masked off here, unlike AttrName — a nest member's flags are the nest's business",
			nest:        inet6Names,
			attrType:    uint16(unix.NLA_F_NESTED) | uint16(unix.IFLA_INET6_CACHEINFO),
			want:        "IFLA_INET6_CACHEINFO",
		},
		{
			description: "negative: an unnamed member renders as nested:n rather than claiming knowledge",
			nest:        inet6Names,
			attrType:    99,
			want:        "nested:99",
		},
		{
			description: "boundary: an empty nest table falls back for everything, including type 0",
			nest:        map[uint16]string{},
			attrType:    0,
			want:        "nested:0",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := SubAttrName(tc.nest, tc.attrType); got != tc.want {
				t.Errorf("SubAttrName(_, 0x%04x) = %q, want %q", tc.attrType, got, tc.want)
			}
		})
	}
}

// TestAttrNameCoversCorpus is the completeness assertion.
//
// Every top-level attribute of every message in every committed guest capture
// must resolve to a real name. A FAMILY:n here means the tables are behind the
// kernel the fixtures were taken on, which matters because a locus is a
// persisted allowlist key: "IFLA:71" is a locus whose meaning changes the day
// the table learns the name, silently unmatching any entry that cited it.
//
// The mesh corpus is included deliberately. It is the advisory topology — veth
// pairs with a bridge master — and it carries IFLA_MASTER, IFLA_LINK and the
// bridge AF_SPEC that the clean namespace never produces, so it is where an
// unnamed attribute is most likely to appear first.
//
// go test ./pkg/nlparity/ -run TestAttrNameCoversCorpus
func TestAttrNameCoversCorpus(t *testing.T) {
	captures := []string{
		"netlink_route_getlink.pcap",
		"netlink_route_getlink_dev.pcap",
		// `ip -s link show`, the only capture whose request asked for
		// counters. It is the corpus's first source of IFLA_STATS and
		// IFLA_STATS64 on an RTM_GETLINK dump, so an unnamed attribute
		// arriving with the stats would show here first.
		"netlink_route_getlink_stats.pcap",
		"netlink_route_getaddr.pcap",
		"netlink_route_getaddr_v4.pcap",
		"netlink_route_getaddr_v6.pcap",
		"netlink_route_getroute.pcap",
		"netlink_route_getroute6.pcap",
		"netlink_route_getroute_table_all.pcap",
		"netlink_route_getneigh.pcap",
	}

	// Both namespaces: the clean one the gate uses, and the mesh one that
	// exists precisely to carry the attributes the clean one cannot.
	var total, named int
	for _, dir := range []string{"", "mesh/"} {
		for _, name := range captures {
			path := tdGuest + "/" + dir + name
			c, err := ParseRouteCaptureFile(path)
			if err != nil {
				t.Fatalf("%s: %v", path, err)
			}
			for _, m := range c.Msgs() {
				if AttrFamilyOf(m.Hdr.Type) == AttrFamilyNone {
					// A message with no namespace still has to have a named
					// TYPE, or the first half of every locus is a number.
					if got := MsgTypeName(m.Hdr.Type); strings.HasPrefix(got, "type(") {
						t.Errorf("%s: message type %d resolves to %q; add it to MsgTypeName",
							path, m.Hdr.Type, got)
					}
					continue
				}
				_, attrs, _ := DecodeAttrs(m.Hdr.Type, m.Body)
				for _, a := range attrs {
					total++
					got := AttrName(m.Hdr.Type, a.Type)
					bare := strings.TrimSuffix(strings.TrimSuffix(got, "+NETORDER"), "+NESTED")
					if strings.Contains(bare, ":") {
						t.Errorf("%s: %s attribute 0x%04x resolves to %q; the name table is behind the kernel",
							path, MsgTypeName(m.Hdr.Type), a.Type, got)
						continue
					}
					named++
				}
			}
		}
	}

	// A floor, so a corpus that stopped being read cannot pass by naming
	// nothing. The two namespaces together carried 2102 top-level attributes
	// when this was written; 1000 is deliberately slack, because the number is
	// a tripwire for "the loop ran", not a second fixture census — the
	// per-capture message floors live in nix/capture-netlink-fixtures.nix.
	const minAttrsCst = 1000
	if total < minAttrsCst {
		t.Errorf("walked only %d attributes across %d captures, want at least %d — "+
			"the corpus is not being read", total, 2*len(captures), minAttrsCst)
	}
	t.Logf("%d of %d top-level attributes resolved to a name", named, total)
}
