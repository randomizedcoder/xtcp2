package render

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// This file is print_linkinfo's `if (show_details)` block — the run of tokens
// `ip -d link show` appends to the `link/` line, and nothing else
// (ip/ipaddress.c:1153-1284, plus print_linktype at :211-268 and print_af_spec
// at :158-190, both of which it calls).
//
// # Why it needed no capture
//
// Every attribute here is in the committed fixtures already. `-d` changes no
// request byte: show_details is not one of the two variables that build
// IFLA_EXT_MASK (:2017-2026), so `ip link show` and `ip -d link show` send the
// same 40-byte datagram and the kernel answers both with the same attributes.
// The `_n` sidecars — ip_link_n, ip_addr_n, ip_addr_v4_n, ip_addr_v6_n — were
// captured with `-d` from the start and have sat unread for exactly as long as
// this file did not exist.
//
// # What is out of scope, and why the boundary is enforced rather than noted
//
// IFLA_INFO_DATA is the per-kind blob print_linktype hands to one of forty
// print_opt implementations (ip/iplink_bridge.c and friends), and it prints
// onto the SAME line as the kind token. So a renderer that emitted the kind and
// stopped would not be short by a line — it would emit a line that says
// something different from `ip`'s. DetailUnsupportedKind is that condition, and
// obj_link refuses rather than truncating, on the `-s -s` precedent: an
// explicit error names the missing feature where it was asked for, while a
// plausible-looking wrong line is reported by the parity harness as a rendering
// bug with nothing to say a whole subsystem was absent.
//
// Measured: no link in the 7_1_4 guest topology carries IFLA_INFO_DATA, so
// nothing there is refused. Seven of the eleven links in the 7_1_8 host dump
// are likewise clean — lo, the three NICs, two unenslaved veths and nlmon0 —
// and the four that are not are the two bridges, a third bridge and the one
// bridge port.
//
// IFLA_DPLL_PIN is out of scope for a different reason, and the reason is
// version rather than effort: the `dpll-pin` block does not exist in iproute2
// 7.1.0, which is the pin. It is in the 7.2.0 reference clone, which is why
// reading only that clone makes this look like a gap. See xtcpnl.LinkDetail
// for the second, independent reason it would print nothing regardless.
//
// # A note on the citations in this file
//
// The line numbers are the 7.2.0 clone's, matching every other citation in
// this repo. The detail block is byte-identical between 7.1.0 and 7.2.0 apart
// from that dpll block and a print_hexstring refactor with the same output, so
// the transcription holds for the pin; only the offsets differ, by about 18
// lines. The one place where 7.1.0 and 7.2.0 BEHAVE differently in this area
// is print_queuelen's ioctl fallback, which is not in this file — see
// internal/goip/goip_details_test.go's sidecarWithoutIoctlQlen.

// LinkDetailView is the `-d` token run for one link.
//
// # Pointers, and why omitempty is load-bearing here in a way it usually isn't
//
// Every counter is a *uint32 rather than a uint32, because ZERO IS A PRINTED
// VALUE: `ip -d link show` opens lo's detail run with "promiscuity 0 allmulti 0
// minmtu 0 maxmtu 0". What has to be distinguished from that is an attribute
// the reply did not carry, and `omitempty` on a non-nil pointer to zero keeps
// the key — which is the only shape in encoding/json that can say both things.
//
// That case is real and committed, not defensive. `ip -d -6 addr show` prints
// the `link/` line and NOT ONE token after it, because an AF_INET6 link dump is
// answered by the kernel's inet6_dump_ifinfo with six attributes and none of
// these is among them. ip_addr_v6_n is that output; ip_addr_v4_n, whose dump is
// answered by rtnl_dump_ifinfo, has the full run. Same flag, same renderer, two
// results, decided entirely by what arrived.
//
// # Field order is print order and that IS the contract
//
// Unlike the JSON keys — which `ip -j`'s consumers sort, so LinkView's doc can
// say order is not worth reproducing — the TEXT tokens run together on one
// line, so their order is the output. The order here is print_linkinfo's, and
// the two interruptions in it are the interesting part: the kind token opens a
// new line in the middle of the run, and addrgenmode lands after it.
type LinkDetailView struct {
	Promiscuity *uint32 `json:"promiscuity,omitempty"`
	AllMulti    *uint32 `json:"allmulti,omitempty"`
	MinMTU      *uint32 `json:"min_mtu,omitempty"`
	MaxMTU      *uint32 `json:"max_mtu,omitempty"`

	// NetnsImmutable is print_bool(PRINT_ANY, "netns-immutable", …), a token
	// with no value. A plain bool with omitempty is right here where pointers
	// are right above, because iproute2 tests the attribute's VALUE and not its
	// presence (:1176-1179) — so "false" and "absent" really are one state.
	// All three links in the 7_1_4 dump carry IFLA_NETNS_IMMUTABLE and only lo
	// carries a 1.
	NetnsImmutable bool `json:"netns-immutable,omitempty"`

	// LinkInfo is print_linktype's open_json_object("linkinfo") (:219), the
	// one part of this run that is nested in JSON as well as in the text.
	LinkInfo *LinkKindView `json:"linkinfo,omitempty"`

	// AddrGenMode is print_af_spec's "addrgenmode" (:170-190), and it is the
	// one token in this run that `ip -d addr show` does not print. The guard is
	// `do_link && tb[IFLA_AF_SPEC]` (:1185-1186) and do_link is set only by the
	// `ip link show` entry point (:2417) — the same variable that decides `mode
	// DEFAULT`, and the same reason LinkViewForAddr exists. The committed pair
	// shows it: ip_link_n has addrgenmode on all three links, ip_addr_n on
	// none.
	AddrGenMode string `json:"inet6_addr_gen_mode,omitempty"`

	NumTxQueues    *uint32 `json:"num_tx_queues,omitempty"`
	NumRxQueues    *uint32 `json:"num_rx_queues,omitempty"`
	GSOMaxSize     *uint32 `json:"gso_max_size,omitempty"`
	GSOMaxSegs     *uint32 `json:"gso_max_segs,omitempty"`
	TSOMaxSize     *uint32 `json:"tso_max_size,omitempty"`
	TSOMaxSegs     *uint32 `json:"tso_max_segs,omitempty"`
	GROMaxSize     *uint32 `json:"gro_max_size,omitempty"`
	GSOIPv4MaxSize *uint32 `json:"gso_ipv4_max_size,omitempty"`
	GROIPv4MaxSize *uint32 `json:"gro_ipv4_max_size,omitempty"`

	PhysPortName string `json:"phys_port_name,omitempty"`
	PhysPortID   string `json:"phys_port_id,omitempty"`
	PhysSwitchID string `json:"phys_switch_id,omitempty"`
	ParentBus    string `json:"parentbus,omitempty"`
	ParentDev    string `json:"parentdev,omitempty"`
}

// LinkKindView is print_linktype's JSON object: the device kind, and what the
// device is to its master.
//
// The two are not alternatives. A bridge port carries both — `veth` on one
// continuation line and `bridge_slave` on the next — and iproute2 emits them as
// two keys of one object (ip/ipaddress.c:222-224, :250-258).
type LinkKindView struct {
	Kind      string `json:"info_kind,omitempty"`
	SlaveKind string `json:"info_slave_kind,omitempty"`
}

// addrGenModeNoneCst is print_af_spec's spelling of IN6_ADDR_GEN_MODE_NONE
// (ip/ipaddress.c:177). Its own constant, not shared with the identically
// spelled routeTypeUnspecNameCst (route.go) or ruleGotoUnsetCst (rule.go) —
// see the note on routeTypeUnspecNameCst for why the three stay apart.
const addrGenModeNoneCst = "none"

// addrGenModeName is print_af_spec's switch (ip/ipaddress.c:171-196).
//
// The default arm is transcribed rather than skipped: iproute2 prints the raw
// number for a mode it does not know, and that is the arm a kernel newer than
// the pinned iproute2 will take.
func addrGenModeName(mode uint8) string {
	switch mode {
	case xtcpnl.In6AddrGenModeEUI64:
		return "eui64"
	case xtcpnl.In6AddrGenModeNone:
		return addrGenModeNoneCst
	case xtcpnl.In6AddrGenModeStablePrivacy:
		// "stable_secret", not "stable_privacy". The enum and the token
		// disagree, and iproute2's token is the one `ip` prints.
		return "stable_secret"
	case xtcpnl.In6AddrGenModeRandom:
		return "random"
	default:
		// snprintf(b1, sizeof(b1), "%#.2hhx", mode) (ip/ipaddress.c:197) —
		// alternate form for the 0x, precision 2 for the minimum digit count,
		// hh for unsigned char. Go's %#.2x is the same three things, and the
		// one C case where they differ cannot arise: "%#x" of ZERO omits the
		// prefix in C, and zero is IN6_ADDR_GEN_MODE_EUI64, which never
		// reaches this arm.
		return fmt.Sprintf("%#.2x", mode)
	}
}

// u32Ptr converts a decoded presence-carrying attribute to the pointer shape
// this view uses, so the whole "absent vs zero" question is answered once.
func u32Ptr(a xtcpnl.U32Attr) *uint32 {
	if !a.Present {
		return nil
	}
	v := a.Value
	return &v
}

// WithDetail attaches the `-d` token run, and is the only way one gets
// attached — the LinkView.Stats precedent, for the reason recorded at the
// embedded field.
//
// linkObject is `do_link`: true for the `ip link show` entry point and false
// for `ip addr show`. It gates exactly one token, addrgenmode, and it is a
// parameter rather than a second constructor because it is the only difference
// — LinkViewForAddr exists for the two differences that are structural.
func (v LinkView) WithDetail(li xtcpnl.LinkInfo, linkObject bool) LinkView {
	d := li.Detail
	dv := &LinkDetailView{
		Promiscuity:    u32Ptr(d.Promiscuity),
		AllMulti:       u32Ptr(d.AllMulti),
		MinMTU:         u32Ptr(d.MinMTU),
		MaxMTU:         u32Ptr(d.MaxMTU),
		NetnsImmutable: d.NetnsImmutable,
		NumTxQueues:    u32Ptr(d.NumTxQueues),
		NumRxQueues:    u32Ptr(d.NumRxQueues),
		GSOMaxSize:     u32Ptr(d.GSOMaxSize),
		GSOMaxSegs:     u32Ptr(d.GSOMaxSegs),
		TSOMaxSize:     u32Ptr(d.TSOMaxSize),
		TSOMaxSegs:     u32Ptr(d.TSOMaxSegs),
		GROMaxSize:     u32Ptr(d.GROMaxSize),
		GSOIPv4MaxSize: u32Ptr(d.GSOIPv4MaxSize),
		GROIPv4MaxSize: u32Ptr(d.GROIPv4MaxSize),
		PhysPortName:   d.PhysPortName,
		ParentBus:      d.ParentDevBusName,
		ParentDev:      d.ParentDevName,
	}
	if len(d.PhysPortID) > 0 {
		dv.PhysPortID = hex.EncodeToString(d.PhysPortID)
	}
	if len(d.PhysSwitchID) > 0 {
		dv.PhysSwitchID = hex.EncodeToString(d.PhysSwitchID)
	}
	// The nest is emitted only when it holds something, because
	// print_linktype's two prints are each guarded and an IFLA_LINKINFO with
	// neither kind is an empty JSON object rather than a missing key. lo
	// carries no IFLA_LINKINFO at all, which is the same render.
	if li.Kind != "" || li.SlaveKind != "" {
		dv.LinkInfo = &LinkKindView{Kind: li.Kind, SlaveKind: li.SlaveKind}
	}
	if linkObject && d.HasAddrGenMode {
		dv.AddrGenMode = addrGenModeName(d.AddrGenMode)
	}

	v.LinkDetailView = dv
	return v
}

// detailText renders the token run, with no leading or trailing newline: it
// continues the `link/` line the caller has already opened.
//
// Every format string carries its own TRAILING space, transcribed rather than
// chosen, which is why a detail run ends the line with one — "gro_ipv4_max_size
// 65536 " and then end of line, as ip_link_n:2 shows. The two `print_nl()`
// calls inside print_linktype are the only newlines, and they are LEADING: the
// kind token opens a line rather than closing one, so the tokens after it
// continue that line instead of starting a third.
func (v LinkView) detailText() string {
	if v.LinkDetailView == nil {
		return ""
	}
	d := v.LinkDetailView
	var b strings.Builder

	// promiscuity is written out rather than tabulated with the three tokens
	// that follow it, because its format string is the one in this whole block
	// that carries a LEADING space as well as a trailing one:
	//
	//	print_uint(PRINT_ANY, "promiscuity", " promiscuity %u ", …)   :1154-1158
	//	print_uint(PRINT_ANY, "allmulti",    "allmulti %u ",      …)   :1160-1164
	//
	// It is the first token of the run, and the thing before it — the link/
	// line's address, or its `brd <addr>` — ends without one. So that space is
	// the only separator between them, and dropping it renders
	// `brd ff:ff:ff:ff:ff:ffpromiscuity 0`. It also explains the double space
	// in `link/netlink  promiscuity 0`, which the committed ip_link_n:3 has
	// and which looks like a typo until you notice nlmon0 has no
	// IFLA_ADDRESS: one space closes `"    link/%s "` and the other opens
	// this.
	if d.Promiscuity != nil {
		fmt.Fprintf(&b, " promiscuity %d ", *d.Promiscuity)
	}
	writeU32Tokens(&b, []u32Token{
		{"allmulti", d.AllMulti},
		{"minmtu", d.MinMTU},
		{"maxmtu", d.MaxMTU},
	})
	if d.NetnsImmutable {
		b.WriteString("netns-immutable ")
	}
	if d.LinkInfo != nil {
		// print_nl() then "    %s " (ip/ipaddress.c:221-223, :250-257). Two
		// separate lines when a link has both, which is what a bridge port
		// looks like.
		if d.LinkInfo.Kind != "" {
			fmt.Fprintf(&b, "\n    %s ", d.LinkInfo.Kind)
		}
		if d.LinkInfo.SlaveKind != "" {
			// The `_slave` suffix is in iproute2's FORMAT STRING —
			// `print_string(PRINT_ANY, "info_slave_kind", "    %s_slave ",
			// slave_kind)` (ip/ipaddress.c:254-257) — and print_string's JSON
			// arm writes the raw ARGUMENT. So the same call emits
			// "bridge_slave" to a terminal and "bridge" to `-j`, and the
			// asymmetry is invisible unless you read the format.
			//
			// This is the reason the suffix is applied here rather than in the
			// decoder: xtcpnl.LinkInfo.SlaveKind holds the wire value, which
			// is what the JSON needs, and the text form is a rendering of it.
			// The wire is also unambiguous about which is which —
			// veth179a698's IFLA_INFO_SLAVE_KIND in the 7_1_8 dump holds
			// "bridge", and its ip_link_n line reads "    bridge_slave ".
			fmt.Fprintf(&b, "\n    %s_slave ", d.LinkInfo.SlaveKind)
		}
	}
	if d.AddrGenMode != "" {
		fmt.Fprintf(&b, "addrgenmode %s ", d.AddrGenMode)
	}
	writeU32Tokens(&b, []u32Token{
		{"numtxqueues", d.NumTxQueues},
		{"numrxqueues", d.NumRxQueues},
		{"gso_max_size", d.GSOMaxSize},
		{"gso_max_segs", d.GSOMaxSegs},
		{"tso_max_size", d.TSOMaxSize},
		{"tso_max_segs", d.TSOMaxSegs},
		{"gro_max_size", d.GROMaxSize},
		{"gso_ipv4_max_size", d.GSOIPv4MaxSize},
		{"gro_ipv4_max_size", d.GROIPv4MaxSize},
	})
	// Five string tokens whose keywords are not their JSON keys — "portname"
	// for phys_port_name, "portid", "switchid", and then two that match
	// (:1243-1273). Written out rather than tabulated because the keyword and
	// the key differ three times out of five, and a table would need both
	// columns anyway.
	if d.PhysPortName != "" {
		fmt.Fprintf(&b, "portname %s ", d.PhysPortName)
	}
	if d.PhysPortID != "" {
		fmt.Fprintf(&b, "portid %s ", d.PhysPortID)
	}
	if d.PhysSwitchID != "" {
		fmt.Fprintf(&b, "switchid %s ", d.PhysSwitchID)
	}
	if d.ParentBus != "" {
		fmt.Fprintf(&b, "parentbus %s ", d.ParentBus)
	}
	if d.ParentDev != "" {
		fmt.Fprintf(&b, "parentdev %s ", d.ParentDev)
	}
	return b.String()
}

// u32Token pairs a keyword with the value that may or may not be there. The
// two runs of these are contiguous in print_linkinfo and identical in shape —
// `print_uint(PRINT_ANY, key, "key %u ", …)` under `if (tb[…])` — so they are
// written as data.
type u32Token struct {
	keyword string
	value   *uint32
}

// writeU32Tokens emits one run, skipping the attributes the reply did not
// carry. Skipping on nil and not on zero is the whole point; see
// LinkDetailView.
func writeU32Tokens(b *strings.Builder, tokens []u32Token) {
	for _, t := range tokens {
		if t.value == nil {
			continue
		}
		fmt.Fprintf(b, "%s %d ", t.keyword, *t.value)
	}
}
