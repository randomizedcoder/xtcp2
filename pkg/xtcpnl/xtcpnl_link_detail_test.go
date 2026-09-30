package xtcpnl

import (
	"reflect"
	"testing"

	"golang.org/x/sys/unix"
)

// TestIflaNetnsImmutableValue pins the one detail attribute number that had to
// be counted out of the kernel header rather than read off x/sys/unix.
//
// The pin is RELATIVE, against the last IFLA_* x/sys does define, for the same
// reason TestNdaFlagsExtValue is: an absolute `== 67` restates the constant and
// fails for no reason that helps, while this fails with the arithmetic in it.
// The enum runs IFLA_GRO_IPV4_MAX_SIZE 64, IFLA_DPLL_PIN 65,
// IFLA_MAX_PACING_OFFLOAD_HORIZON 66, IFLA_NETNS_IMMUTABLE 67.
//
// A miscount here is not a decode error, which is why it needs a test at all:
// 65 is IFLA_DPLL_PIN, a nest, and reading its first byte would yield a
// perfectly plausible 0 or 1.
//
// go test ./pkg/xtcpnl/ -run TestIflaNetnsImmutableValue
func TestIflaNetnsImmutableValue(t *testing.T) {
	tests := []struct {
		description string
		got         uint16
		want        uint16
	}{
		{
			description: "positive: IFLA_NETNS_IMMUTABLE is three past the last IFLA_* x/sys defines",
			got:         IflaNetnsImmutable,
			want:        uint16(unix.IFLA_GRO_IPV4_MAX_SIZE) + 3,
		},
		{
			description: "boundary: it is not IFLA_DPLL_PIN, the nest two below it",
			got:         IflaNetnsImmutable - 2,
			want:        uint16(unix.IFLA_GRO_IPV4_MAX_SIZE) + 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("got %d, want %d", tc.got, tc.want)
			}
		})
	}
}

// afSpecInet6 wraps an IFLA_INET6_* attribute stream in the two levels
// IFLA_AF_SPEC actually has: an outer entry keyed by ADDRESS FAMILY, and the
// IFLA_INET6_* nest inside it.
//
// The outer key being a family and not an attribute type is the whole subtlety
// of setAddrGenMode, so the helper spells it rather than hiding it.
func afSpecInet6(inner []byte) []byte {
	return rtattr(uint16(unix.AF_INET6), inner)
}

// TestSetLinkDetailAttr drives the `ip -d` attribute group one synthetic
// message at a time.
//
// The message is always a bare ifinfomsg plus the attributes under test, so a
// row's want is the whole LinkDetail and an attribute leaking into the wrong
// field fails rather than being masked by a neighbor's value.
//
// go test ./pkg/xtcpnl/ -run TestSetLinkDetailAttr
func TestSetLinkDetailAttr(t *testing.T) {
	tests := []struct {
		description string
		attrs       []byte
		want        LinkDetail
	}{
		{
			description: "positive: the four tokens before the kind decode into the first run",
			attrs: concat(
				rtattr(unix.IFLA_PROMISCUITY, le32(0)),
				rtattr(unix.IFLA_ALLMULTI, le32(0)),
				rtattr(unix.IFLA_MIN_MTU, le32(68)),
				rtattr(unix.IFLA_MAX_MTU, le32(16334)),
			),
			want: LinkDetail{
				Promiscuity: U32Attr{0, true},
				AllMulti:    U32Attr{0, true},
				MinMTU:      U32Attr{68, true},
				MaxMTU:      U32Attr{16334, true},
			},
		},
		{
			description: "positive: the nine tokens after the kind decode into the second run",
			attrs: concat(
				rtattr(unix.IFLA_NUM_TX_QUEUES, le32(32)),
				rtattr(unix.IFLA_NUM_RX_QUEUES, le32(24)),
				rtattr(unix.IFLA_GSO_MAX_SIZE, le32(65536)),
				rtattr(unix.IFLA_GSO_MAX_SEGS, le32(65535)),
				rtattr(unix.IFLA_TSO_MAX_SIZE, le32(524280)),
				rtattr(unix.IFLA_TSO_MAX_SEGS, le32(65535)),
				rtattr(unix.IFLA_GRO_MAX_SIZE, le32(65536)),
				rtattr(unix.IFLA_GSO_IPV4_MAX_SIZE, le32(65536)),
				rtattr(unix.IFLA_GRO_IPV4_MAX_SIZE, le32(65536)),
			),
			want: LinkDetail{
				NumTxQueues:    U32Attr{32, true},
				NumRxQueues:    U32Attr{24, true},
				GSOMaxSize:     U32Attr{65536, true},
				GSOMaxSegs:     U32Attr{65535, true},
				TSOMaxSize:     U32Attr{524280, true},
				TSOMaxSegs:     U32Attr{65535, true},
				GROMaxSize:     U32Attr{65536, true},
				GSOIPv4MaxSize: U32Attr{65536, true},
				GROIPv4MaxSize: U32Attr{65536, true},
			},
		},
		{
			// The distinction the whole U32Attr type exists for. `ip -d link
			// show` opens lo's run with "promiscuity 0", so zero is a printed
			// value, and Present is the only thing separating it from an
			// attribute that never arrived.
			description: "boundary: a zero-valued attribute is present, not absent",
			attrs:       rtattr(unix.IFLA_PROMISCUITY, le32(0)),
			want:        LinkDetail{Promiscuity: U32Attr{0, true}},
		},
		{
			description: "negative: no attribute at all leaves every field absent",
			attrs:       nil,
			want:        LinkDetail{},
		},
		{
			// A three-byte payload is treated as ABSENT rather than as zero,
			// which is the conservative arm: rendering "promiscuity 0" from a
			// truncated attribute would state a fact the wire did not carry,
			// while omitting the token states nothing.
			description: "boundary: a short u32 payload leaves the attribute absent",
			attrs:       rtattr(unix.IFLA_PROMISCUITY, []byte{0x01, 0x02, 0x03}),
			want:        LinkDetail{},
		},
		{
			// The other side of the length boundary: a longer payload decodes
			// from the first four bytes and the rest is ignored, which is what
			// rta_getattr_u32 does too.
			description: "boundary: an over-long u32 payload decodes its first four bytes",
			attrs:       rtattr(unix.IFLA_PROMISCUITY, []byte{0x07, 0x00, 0x00, 0x00, 0xff, 0xff, 0xff, 0xff}),
			want:        LinkDetail{Promiscuity: U32Attr{7, true}},
		},
		{
			// 0xFFFFFFFF, the widest a u32 goes. Worth a row because the
			// renderer formats with %d and a signed conversion anywhere on the
			// path would print -1.
			description: "corner: a u32 at its maximum survives as 4294967295, not -1",
			attrs:       rtattr(unix.IFLA_GSO_MAX_SIZE, []byte{0xff, 0xff, 0xff, 0xff}),
			want:        LinkDetail{GSOMaxSize: U32Attr{4294967295, true}},
		},
		{
			description: "positive: IFLA_NETNS_IMMUTABLE with a 1 sets the flag",
			attrs:       rtattr(IflaNetnsImmutable, []byte{1}),
			want:        LinkDetail{NetnsImmutable: true},
		},
		{
			// THE row that makes IFLA_NETNS_IMMUTABLE different from
			// everything around it. iproute2 tests the value, not the presence
			// (ip/ipaddress.c:1176-1179), and all three links in the 7_1_4
			// dump carry the attribute while only lo carries a 1 — so a
			// presence test would print the token three times where `ip`
			// prints it once.
			description: "negative: IFLA_NETNS_IMMUTABLE with a 0 is present and still false",
			attrs:       rtattr(IflaNetnsImmutable, []byte{0}),
			want:        LinkDetail{NetnsImmutable: false},
		},
		{
			description: "positive: IFLA_AF_SPEC yields the addrgenmode inside its AF_INET6 entry",
			attrs: rtattr(uint16(unix.IFLA_AF_SPEC), afSpecInet6(
				rtattr(uint16(unix.IFLA_INET6_ADDR_GEN_MODE), []byte{In6AddrGenModeNone}),
			)),
			want: LinkDetail{AddrGenMode: In6AddrGenModeNone, HasAddrGenMode: true},
		},
		{
			// Presence again, and again because zero is a real value:
			// IN6_ADDR_GEN_MODE_EUI64 is 0 and is what almost every link in
			// the corpus reports, so HasAddrGenMode is the only thing that
			// separates "eui64" from "no AF_INET6 entry".
			description: "boundary: addrgenmode eui64 is zero and must still read as present",
			attrs: rtattr(uint16(unix.IFLA_AF_SPEC), afSpecInet6(
				rtattr(uint16(unix.IFLA_INET6_ADDR_GEN_MODE), []byte{In6AddrGenModeEUI64}),
			)),
			want: LinkDetail{AddrGenMode: In6AddrGenModeEUI64, HasAddrGenMode: true},
		},
		{
			// The outer level of IFLA_AF_SPEC is keyed by address family, not
			// by attribute type, and AF_INET is 2 — which is also
			// IFLA_ADDRESS. Reading the outer level as IFLA_* is a
			// type-punning bug that compiles, so this row hands it the AF_INET
			// devconf block alone and demands nothing come out.
			description: "negative: an IFLA_AF_SPEC holding only AF_INET yields no addrgenmode",
			attrs: rtattr(uint16(unix.IFLA_AF_SPEC), rtattr(uint16(unix.AF_INET),
				rtattr(1, le32(0x11223344)),
			)),
			want: LinkDetail{},
		},
		{
			// First-wins inside the nest, matching parse_rtattr
			// (lib/libnetlink.c:1554) — the rule everywhere in this package
			// except IFLA_PROP_LIST, which iproute2 itself walks differently.
			description: "corner: a repeated addrgenmode keeps the first, as parse_rtattr does",
			attrs: rtattr(uint16(unix.IFLA_AF_SPEC), afSpecInet6(concat(
				rtattr(uint16(unix.IFLA_INET6_ADDR_GEN_MODE), []byte{In6AddrGenModeRandom}),
				rtattr(uint16(unix.IFLA_INET6_ADDR_GEN_MODE), []byte{In6AddrGenModeEUI64}),
			))),
			want: LinkDetail{AddrGenMode: In6AddrGenModeRandom, HasAddrGenMode: true},
		},
		{
			// A malformed AF_SPEC costs one token and not the link. Same
			// tolerance walkNestTolerant applies to IFLA_LINKINFO, and the
			// reason is the same: an undecodable devconf blob is no reason to
			// drop an interface a renderer could otherwise print.
			description: "corner: an IFLA_AF_SPEC whose inner rta_len overruns loses only the addrgenmode",
			attrs: rtattr(uint16(unix.IFLA_AF_SPEC),
				[]byte{0x40, 0x00, 0x0a, 0x00, 0xaa, 0xaa, 0xaa, 0xaa}),
			want: LinkDetail{},
		},
		{
			description: "positive: the physical-device tail decodes its strings and its hex",
			attrs: concat(
				rtattr(uint16(unix.IFLA_PHYS_PORT_NAME), append([]byte("p0"), 0)),
				rtattr(uint16(unix.IFLA_PHYS_SWITCH_ID), []byte{0xd0, 0xd8, 0xcf, 0xff, 0xff, 0x73, 0x09, 0x04}),
				rtattr(uint16(unix.IFLA_PARENT_DEV_BUS_NAME), append([]byte("pci"), 0)),
				rtattr(uint16(unix.IFLA_PARENT_DEV_NAME), append([]byte("0000:23:00.0"), 0)),
			),
			want: LinkDetail{
				PhysPortName:     "p0",
				PhysSwitchID:     []byte{0xd0, 0xd8, 0xcf, 0xff, 0xff, 0x73, 0x09, 0x04},
				ParentDevBusName: "pci",
				ParentDevName:    "0000:23:00.0",
			},
		},
		{
			// The four attributes the kernel sends on every link in the 7_1_4
			// dump and iproute2 7.1.0 prints none of: IFLA_DPLL_PIN (a
			// zero-length nest), IFLA_MAX_PACING_OFFLOAD_HORIZON,
			// IFLA_HEADROOM and IFLA_TAILROOM. They must reach no field,
			// because setLinkDetailAttr's job includes ignoring things and a
			// stray case would render a token `ip` does not.
			description: "negative: attributes iproute2 7.1.0 does not print reach no field",
			attrs: concat(
				rtattr(65, nil),           // IFLA_DPLL_PIN, as the wire sends it: empty
				rtattr(66, le32(0)),       // IFLA_MAX_PACING_OFFLOAD_HORIZON
				rtattr(68, []byte{0, 0}),  // IFLA_HEADROOM
				rtattr(69, []byte{16, 0}), // IFLA_TAILROOM
			),
			want: LinkDetail{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			body := concat(
				ifinfomsgHdr(unix.AF_UNSPEC, unix.ARPHRD_ETHER, 1, unix.IFF_UP),
				tc.attrs,
			)
			li, err := ParseNewLink(body)
			if err != nil {
				t.Fatalf("ParseNewLink: %v", err)
			}
			if !reflect.DeepEqual(li.Detail, tc.want) {
				t.Errorf("Detail =\n  %+v\nwant\n  %+v", li.Detail, tc.want)
			}
		})
	}
}

// TestParseNewLinkDetailRealFixture asserts the decoded `ip -d` group against
// the real 7_1_8 host dump, row by row, with each want transcribed from the
// line of ip_link_n it is quoted beside.
//
// This is the half TestParseNewLinkRealFixture deliberately clears, split out
// so that neither test's want literal has to carry the other's fields; see the
// comment there.
//
// # The corpus this needs, and why it is the host one
//
// 7_1_8 is the only capture with physical NICs in it, and four of the five
// tail attributes — portname, switchid, parentbus, parentdev — exist on
// nothing else. The 7_1_4 guest topology has three links, all virtual, and
// covers the rest.
//
// go test ./pkg/xtcpnl/ -run TestParseNewLinkDetailRealFixture
func TestParseNewLinkDetailRealFixture(t *testing.T) {
	bodies, sawDone := readDumpFixture(t, tdRouteGetLinkDump_7_1_8, uint16(unix.RTM_NEWLINK))
	if !sawDone {
		t.Fatalf("%s: dump not terminated by NLMSG_DONE", tdRouteGetLinkDump_7_1_8)
	}

	byName := make(map[string]LinkInfo, len(bodies))
	for i, b := range bodies {
		li, err := ParseNewLink(b)
		if err != nil {
			t.Fatalf("ParseNewLink(msg %d): %v", i, err)
		}
		byName[li.Name] = li
	}

	tests := []struct {
		description string
		name        string
		want        LinkDetail
		wantKind    string
		wantSlave   string
	}{
		{
			// ip_link_n:2 "    link/loopback … promiscuity 0 allmulti 0 minmtu 0
			// maxmtu 0 netns-immutable addrgenmode eui64 numtxqueues 1 …"
			//
			// The only link in this dump with netns-immutable set and no
			// IFLA_LINKINFO at all — the two halves of "lo is special" — so it
			// is the row that proves the flag is read from the value and the
			// kind nest is genuinely optional.
			description: "positive: lo carries netns-immutable, no kind, and the widest tso_max_size",
			name:        "lo",
			want: LinkDetail{
				Promiscuity: U32Attr{0, true}, AllMulti: U32Attr{0, true},
				MinMTU: U32Attr{0, true}, MaxMTU: U32Attr{0, true},
				NetnsImmutable: true,
				AddrGenMode:    In6AddrGenModeEUI64, HasAddrGenMode: true,
				NumTxQueues: U32Attr{1, true}, NumRxQueues: U32Attr{1, true},
				GSOMaxSize: U32Attr{65536, true}, GSOMaxSegs: U32Attr{65535, true},
				TSOMaxSize: U32Attr{524280, true}, TSOMaxSegs: U32Attr{65535, true},
				GROMaxSize:     U32Attr{65536, true},
				GSOIPv4MaxSize: U32Attr{65536, true}, GROIPv4MaxSize: U32Attr{65536, true},
			},
		},
		{
			// ip_link_n:4 "… minmtu 68 maxmtu 16334 addrgenmode none numtxqueues
			// 32 numrxqueues 32 … parentbus pci parentdev 0000:01:00.0"
			//
			// The only link in the whole corpus whose addrgenmode is not
			// eui64, which is why the mode is a decoded value rather than an
			// assumption, and the only one with a parent device and no port
			// name — parentbus/parentdev and portname/switchid are separate
			// attributes and this link has one pair without the other.
			description: "positive: enp1s0 is the corpus's only addrgenmode none, with a PCI parent",
			name:        "enp1s0",
			want: LinkDetail{
				Promiscuity: U32Attr{0, true}, AllMulti: U32Attr{0, true},
				MinMTU: U32Attr{68, true}, MaxMTU: U32Attr{16334, true},
				AddrGenMode: In6AddrGenModeNone, HasAddrGenMode: true,
				NumTxQueues: U32Attr{32, true}, NumRxQueues: U32Attr{32, true},
				GSOMaxSize: U32Attr{65536, true}, GSOMaxSegs: U32Attr{65535, true},
				TSOMaxSize: U32Attr{65536, true}, TSOMaxSegs: U32Attr{65535, true},
				GROMaxSize:     U32Attr{65536, true},
				GSOIPv4MaxSize: U32Attr{65536, true}, GROIPv4MaxSize: U32Attr{65536, true},
				ParentDevBusName: "pci", ParentDevName: "0000:01:00.0",
			},
		},
		{
			// ip_link_n:6 "… portname p0 switchid d0d8cfffff730904 parentbus pci
			// parentdev 0000:23:00.0"
			//
			// The full tail, and the only place in the corpus the two hex
			// tokens occur. The switch id is shared with enp35s0f1np1 — two
			// ports of one ASIC — which is exactly what the attribute means.
			description: "positive: enp35s0f0np0 carries the whole physical tail including a switch id",
			name:        "enp35s0f0np0",
			want: LinkDetail{
				Promiscuity: U32Attr{0, true}, AllMulti: U32Attr{0, true},
				MinMTU: U32Attr{68, true}, MaxMTU: U32Attr{9978, true},
				AddrGenMode: In6AddrGenModeEUI64, HasAddrGenMode: true,
				NumTxQueues: U32Attr{192, true}, NumRxQueues: U32Attr{24, true},
				GSOMaxSize: U32Attr{65536, true}, GSOMaxSegs: U32Attr{65535, true},
				TSOMaxSize: U32Attr{524280, true}, TSOMaxSegs: U32Attr{65535, true},
				GROMaxSize:     U32Attr{65536, true},
				GSOIPv4MaxSize: U32Attr{65536, true}, GROIPv4MaxSize: U32Attr{65536, true},
				PhysPortName:     "p0",
				PhysSwitchID:     []byte{0xd0, 0xd8, 0xcf, 0xff, 0xff, 0x73, 0x09, 0x04},
				ParentDevBusName: "pci", ParentDevName: "0000:23:00.0",
			},
		},
		{
			// ip_link_n:21-23 "… promiscuity 1 allmulti 1 …" then "    veth "
			// then "    bridge_slave state forwarding …".
			//
			// The two-kind row, and the only link in the corpus with a
			// non-zero promiscuity or allmulti — it is a bridge port, so the
			// bridge put it in promiscuous mode. Both counters being 1 is what
			// makes this the row that would catch a decoder reading presence
			// where it should read a value.
			// The slave kind ON THE WIRE is "bridge"; the sidecar's
			// "bridge_slave" is iproute2's format string `"    %s_slave "`
			// adding a suffix that its own `-j` output does not get. Asserting
			// the unsuffixed value here is what keeps the two forms separable.
			description: "positive: bridge port veth179a698 has both kinds and promiscuity 1",
			name:        "veth179a698",
			wantKind:    "veth",
			wantSlave:   "bridge",
			want: LinkDetail{
				Promiscuity: U32Attr{1, true}, AllMulti: U32Attr{1, true},
				MinMTU: U32Attr{68, true}, MaxMTU: U32Attr{65535, true},
				AddrGenMode: In6AddrGenModeEUI64, HasAddrGenMode: true,
				NumTxQueues: U32Attr{24, true}, NumRxQueues: U32Attr{24, true},
				GSOMaxSize: U32Attr{65536, true}, GSOMaxSegs: U32Attr{65535, true},
				TSOMaxSize: U32Attr{524280, true}, TSOMaxSegs: U32Attr{65535, true},
				GROMaxSize:     U32Attr{65536, true},
				GSOIPv4MaxSize: U32Attr{65536, true}, GROIPv4MaxSize: U32Attr{65536, true},
			},
		},
		{
			// ip_link_n:32-34 "    link/netlink  promiscuity 0 allmulti 0 minmtu
			// 16 maxmtu 0 " then "    nlmon addrgenmode eui64 …".
			//
			// The boundary row for minmtu: 16 where every other link reports 0
			// or 68, and the one link whose maxmtu is 0 while its minmtu is
			// not. A decoder that swapped the two attribute numbers passes
			// every other row in this table.
			//
			// # promiscuity is 1 here and 0 in the sidecar, permanently
			//
			// The want below deliberately CONTRADICTS the line quoted above,
			// and it is the sidecar that is out of date rather than the pcap.
			// nlmon0 is the interface the capture is taken on: tcpdump opens a
			// packet socket on it and the kernel raises IFF_PROMISC for the
			// lifetime of that socket, so every reply recorded INSIDE a
			// capture window reports promiscuity 1. The text sidecars are
			// written by a separate invocation with no tcpdump running, which
			// reports 0.
			//
			// No re-capture can reconcile them, because capturing the pcap is
			// what sets the bit. It is causal, it is one field on one
			// interface, and it was invisible until -d existed — plain `ip
			// link show` prints no promiscuity at all, which is why a corpus
			// this old has carried the discrepancy unnoticed. The guest
			// corpus has it too, on the same interface and for the same
			// reason; see TestLinkShowDetailMatchesSidecar in internal/goip,
			// which states the substitution once and requires it to apply.
			//
			// The parity harness is unaffected: it runs `ip` and `goip`
			// through nlcap's capio, so both sides are inside a capture window
			// and both see 1.
			description: "boundary: nlmon0 reports minmtu 16 with maxmtu 0, and promiscuity 1 because it is the capture interface",
			name:        "nlmon0",
			wantKind:    "nlmon",
			want: LinkDetail{
				Promiscuity: U32Attr{1, true}, AllMulti: U32Attr{0, true},
				MinMTU: U32Attr{16, true}, MaxMTU: U32Attr{0, true},
				AddrGenMode: In6AddrGenModeEUI64, HasAddrGenMode: true,
				NumTxQueues: U32Attr{1, true}, NumRxQueues: U32Attr{1, true},
				GSOMaxSize: U32Attr{65536, true}, GSOMaxSegs: U32Attr{65535, true},
				TSOMaxSize: U32Attr{65536, true}, TSOMaxSegs: U32Attr{65535, true},
				GROMaxSize:     U32Attr{65536, true},
				GSOIPv4MaxSize: U32Attr{65536, true}, GROIPv4MaxSize: U32Attr{65536, true},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			li, ok := byName[tc.name]
			if !ok {
				t.Fatalf("no link named %q in %s", tc.name, tdRouteGetLinkDump_7_1_8)
			}
			if !reflect.DeepEqual(li.Detail, tc.want) {
				t.Errorf("%s Detail =\n  %+v\nwant\n  %+v", tc.name, li.Detail, tc.want)
			}
			if li.Kind != tc.wantKind {
				t.Errorf("%s Kind = %q, want %q", tc.name, li.Kind, tc.wantKind)
			}
			if li.SlaveKind != tc.wantSlave {
				t.Errorf("%s SlaveKind = %q, want %q", tc.name, li.SlaveKind, tc.wantSlave)
			}
		})
	}
}

// TestLinkInfoDataPresenceRealFixture pins which links in the corpus carry a
// per-kind data blob, because that set is what decides where `-d` is refused.
//
// It is an assertion about the FIXTURES as much as about the decoder, and that
// is the point: render/link_detail.go's claim that nothing in the guest
// topology is refused is only true for as long as the guest topology has no
// device with INFO_DATA. A re-capture that added one would make `-d link show`
// start erroring in the parity harness, and this fails first and says why.
//
// go test ./pkg/xtcpnl/ -run TestLinkInfoDataPresenceRealFixture
func TestLinkInfoDataPresenceRealFixture(t *testing.T) {
	tests := []struct {
		description string
		fixture     string
		// wantData is every link name whose nest holds IFLA_INFO_DATA or
		// IFLA_INFO_SLAVE_DATA. Every other link in the dump must hold
		// neither.
		wantData []string
	}{
		{
			// The clean guest namespace, and the row that licenses the
			// refusal being cheap. lo has no IFLA_LINKINFO; nlmon0 and goip0
			// have one holding IFLA_INFO_KIND alone. So `goip -d link show`
			// renders all three in full, and the parity harness — which runs
			// only in this namespace — never meets the refusal.
			description: "negative: no link in the 7_1_4 guest topology carries a per-kind data blob",
			fixture:     tdDumpGetLink_7_1_4,
			wantData:    nil,
		},
		{
			// The host dump, where four of eleven do: three bridges by their
			// own kind and one veth by its slave kind. These are the links
			// whose iproute2 render is a kind token plus a screenful of
			// print_opt output on the same line, and so the links goip must
			// refuse rather than half-render.
			description: "positive: the three bridges and the one bridge port in 7_1_8 carry one",
			fixture:     tdRouteGetLinkDump_7_1_8,
			wantData:    []string{"virbr0", "docker0", "br-3a5828b2963a", "veth179a698"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			// linksIn rather than readDumpFixture, because the two corpora are
			// different SHAPES of file: a dump-set capture holds the request
			// as well as the replies, and readDumpFixture's caller asserts
			// NLMSG_DONE, which linksIn returns instead of requiring.
			links, done := linksIn(t, tc.fixture)
			if !done {
				t.Fatalf("%s: dump not terminated by NLMSG_DONE", tc.fixture)
			}
			want := make(map[string]bool, len(tc.wantData))
			for _, n := range tc.wantData {
				want[n] = true
			}
			var got []string
			for i := range links {
				if links[i].HasInfoData || links[i].HasInfoSlaveData {
					got = append(got, links[i].Name)
				}
			}
			if len(got) != len(tc.wantData) {
				t.Fatalf("links with per-kind data = %v, want %v", got, tc.wantData)
			}
			for _, n := range got {
				if !want[n] {
					t.Errorf("link %q carries a per-kind data blob and was not expected to", n)
				}
			}
		})
	}
}
