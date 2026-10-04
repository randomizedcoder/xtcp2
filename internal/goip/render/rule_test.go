package render

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// The twenty positive rows in TestRuleViewOfText are the twenty lines of
// pkg/xtcpnl/testdata/7_1_4/dumps/ip_rule, the `ip rule show` sidecar captured
// in the microVM topology, transcribed token for token:
//
//	0:	from all lookup local
//	100:	from 192.0.2.0/24 lookup 100
//	200:	from all to 198.51.100.0/24 iif goip0 lookup 200
//	300:	from all fwmark 0x1234/0xff00 lookup 300
//	400:	from all oif goip0 lookup 400
//	500:	from all uidrange 1000-2000 lookup 500
//	600:	from all sport 1000-2000 dport 80 lookup 600
//	700:	from all lookup main suppress_prefixlength 0
//	800:	from all blackhole
//	900:	from all goto 32766
//	1000:	not from 203.0.113.0/24 lookup main
//	1100:	from all nop
//	1200:	from all lookup [l3mdev-table]
//	1300:	from all tun_id 42 lookup 1300
//	1400:	from all lookup main suppress_ifgroup 5
//	1500:	from all lookup 1500 realms 1/2
//	1600:	from all fwmark 0x10 lookup 1600
//	1700:	from all lookup main masquerade
//	32766:	from all lookup main
//	32767:	from all lookup default
//
// and the four of ip_rule6:
//
//	0:	from all lookup local
//	100:	from 2001:db8::/64 lookup 100
//	200:	from all lookup 200 flowlabel 0x12345
//	32766:	from all lookup main
//
// The separator after the preference is a TAB, because print_rule's first
// format string is "%u:\t" (ip/iprule.c:305-308). Every other separator on the
// line is a single leading space.
//
// # What the rows leave out on purpose, and why that is not a shortcut
//
// Every reply in the capture carries FRA_PROTOCOL, and none of the twenty
// lines above prints a `proto` token, because print_rule's guard is
// `(protocol && protocol != RTPROT_KERNEL) || show_details > 0` (:551-557).
// goip applies the `show_details` half by CLEARING HasProtocol before calling
// RuleViewOf — see ruleShow in the goip package — so the rows transcribed from
// ip_rule leave HasProtocol false, which is the state the renderer is actually
// handed. The `-d` form, where the same bytes do print a token, is
// TestRuleViewOfTextDetails below; the two together are what make the
// presence-versus-value distinction visible.
//
// Every reply also carries FRA_SUPPRESS_PREFIXLEN, twenty times out of twenty,
// nineteen of them holding the sentinel 0xFFFFFFFF. It is spelled out on every
// row rather than defaulted, because a row that omitted it would be asserting
// the renderer's behavior on bytes no kernel sends.
const ruleSentinelCst = 0xFFFFFFFF

func TestRuleViewOfText(t *testing.T) {
	tests := []struct {
		description string
		in          xtcpnl.RuleInfo
		want        string
	}{
		{
			// The only rule in the dump with no FRA_PRIORITY: the kernel omits
			// the attribute when the preference is zero, so HasPriority is
			// false and the line still opens "0:". Nothing in the renderer
			// reads HasPriority, and that is the point of the row — Priority's
			// zero value and an absent attribute must render the same, because
			// print_rule reads frh->pref through a variable it initialized to 0.
			description: "positive: ip_rule:1 — the local default rule, table from the header and no priority attribute",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				RawTable: unix.RT_TABLE_LOCAL, Table: unix.RT_TABLE_LOCAL,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: "0:\tfrom all lookup local\n",
		},
		{
			description: "positive: ip_rule:2 — a source prefix shorter than the family's host length prints its length",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				SrcLen: 24, Src: []byte{192, 0, 2, 0},
				Priority: 100, HasPriority: true,
				RawTable: 100, Table: 100,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: "100:\tfrom 192.0.2.0/24 lookup 100\n",
		},
		{
			// `to` and `iif` on one line, and the asymmetry between them and
			// `from`: a rule with no source prints "from all", a rule with no
			// destination prints no `to` token at all (:316-344).
			description: "positive: ip_rule:3 — a destination and an inbound interface, the source still printing the literal all",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				DstLen: 24, Dst: []byte{198, 51, 100, 0},
				IifName: "goip0", HasIifName: true,
				Priority: 200, HasPriority: true,
				RawTable: 200, Table: 200,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: "200:\tfrom all to 198.51.100.0/24 iif goip0 lookup 200\n",
		},
		{
			// A narrowed mask prints, and the table id is above 255 — so the
			// header says RT_TABLE_COMPAT and the value comes from FRA_TABLE.
			// RawTable is spelled out rather than left zero because leaving it
			// zero would make the row pass for a reason the kernel never
			// supplies.
			description: "positive: ip_rule:4 — fwmark with a narrowed mask, and a table id that did not fit in the header",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				Fwmark: 0x1234, HasFwmark: true, Fwmask: 0xff00, HasFwmask: true,
				Priority: 300, HasPriority: true,
				RawTable: unix.RT_TABLE_COMPAT, Table: 300,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: "300:\tfrom all fwmark 0x1234/0xff00 lookup 300\n",
		},
		{
			description: "positive: ip_rule:5 — an outbound interface, with no [detached] flag because the device exists",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				OifName: "goip0", HasOifName: true,
				Priority: 400, HasPriority: true,
				RawTable: unix.RT_TABLE_COMPAT, Table: 400,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: "400:\tfrom all oif goip0 lookup 400\n",
		},
		{
			description: "positive: ip_rule:6 — a uid range, whose two halves are host byte order",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				UidRange: xtcpnl.FibRuleUidRange{Start: 1000, End: 2000}, HasUidRange: true,
				Priority: 500, HasPriority: true,
				RawTable: unix.RT_TABLE_COMPAT, Table: 500,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: "500:\tfrom all uidrange 1000-2000 lookup 500\n",
		},
		{
			// The row that pins both port arms at once. sport has two distinct
			// endpoints and takes the range arm; dport has one and takes the
			// single arm, where the kernel's unrequested FRA_DPORT_MASK =
			// 0xffff must NOT turn `dport 80` into `dport 0x50/0xffff`.
			description: "positive: ip_rule:7 — a sport range and a single dport whose all-ones mask prints nothing",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				SportRange: xtcpnl.FibRulePortRange{Start: 1000, End: 2000}, HasSportRange: true,
				DportRange: xtcpnl.FibRulePortRange{Start: 80, End: 80}, HasDportRange: true,
				DportMask: 0xffff, HasDportMask: true,
				Priority: 600, HasPriority: true,
				RawTable: unix.RT_TABLE_COMPAT, Table: 600,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: "600:\tfrom all sport 1000-2000 dport 80 lookup 600\n",
		},
		{
			// The one rule out of twenty whose suppress_prefixlen is not the
			// sentinel. Zero is a real value here and means "suppress a route
			// whose prefix is 0 bits", so it must print — an omitempty on this
			// field would delete the only line that uses it.
			description: "boundary: ip_rule:8 — suppress_prefixlength 0 is a value, not an absence",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				Priority: 700, HasPriority: true,
				RawTable: unix.RT_TABLE_MAIN, Table: unix.RT_TABLE_MAIN,
				HasSuppressPrefixlen: true, SuppressPrefixlen: 0,
			},
			want: "700:\tfrom all lookup main suppress_prefixlength 0\n",
		},
		{
			description: "positive: ip_rule:9 — an action that is not FR_ACT_TO_TBL prints its type name and no table",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.RTN_BLACKHOLE,
				Priority: 800, HasPriority: true,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: "800:\tfrom all blackhole\n",
		},
		{
			description: "positive: ip_rule:10 — goto prints its target preference and no [unresolved] suffix",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_GOTO,
				Goto: 32766, HasGoto: true,
				Priority: 900, HasPriority: true,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: "900:\tfrom all goto 32766\n",
		},
		{
			// `not` is the only token that precedes the source, and it is a
			// header FLAG rather than an attribute. It is also the only nonzero
			// frh_flags in the whole capture.
			description: "positive: ip_rule:11 — FIB_RULE_INVERT prints before the source",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL, Flags: unix.FIB_RULE_INVERT,
				SrcLen: 24, Src: []byte{203, 0, 113, 0},
				Priority: 1000, HasPriority: true,
				RawTable: unix.RT_TABLE_MAIN, Table: unix.RT_TABLE_MAIN,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: "1000:\tnot from 203.0.113.0/24 lookup main\n",
		},
		{
			description: "positive: ip_rule:12 — nop is an action arm of its own, ahead of the type-name catch-all",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_NOP,
				Priority: 1100, HasPriority: true,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: "1100:\tfrom all nop\n",
		},
		{
			// FR_ACT_TO_TBL with table 0, which would print no table at all,
			// except that FRA_L3MDEV substitutes a literal for the number. The
			// table a packet actually uses is chosen from the l3mdev device it
			// arrived on, so there is nothing to name.
			description: "corner: ip_rule:13 — l3mdev prints a literal in place of a table name",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				L3mdev: 1, HasL3mdev: true,
				Priority: 1200, HasPriority: true,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: "1200:\tfrom all lookup [l3mdev-table]\n",
		},
		{
			description: "positive: ip_rule:14 — tun_id prints in decimal, having been decoded from network order",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				TunID: 42, HasTunID: true,
				Priority: 1300, HasPriority: true,
				RawTable: unix.RT_TABLE_COMPAT, Table: 1300,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: "1300:\tfrom all tun_id 42 lookup 1300\n",
		},
		{
			// Both suppress tokens are nested inside the `if (table)` guard,
			// and this rule has a table, so the one that is set prints. The
			// sentinel-valued prefixlen beside it does not — the two are
			// independent even though they share the guard.
			description: "positive: ip_rule:15 — suppress_ifgroup prints while its sentinel-valued sibling does not",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				SuppressIfgroup: 5, HasSuppressIfgroup: true,
				Priority: 1400, HasPriority: true,
				RawTable: unix.RT_TABLE_MAIN, Table: unix.RT_TABLE_MAIN,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: "1400:\tfrom all lookup main suppress_ifgroup 5\n",
		},
		{
			// The ordering fact that reads backwards: realms comes AFTER the
			// lookup block, not before it, even though the command line names
			// them the other way round (`ip rule add realms 1/2 lookup 1500`).
			description: "corner: ip_rule:16 — realms prints after the table, not before",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				Flow: 1<<16 | 2, HasFlow: true,
				Priority: 1500, HasPriority: true,
				RawTable: unix.RT_TABLE_COMPAT, Table: 1500,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: "1500:\tfrom all lookup 1500 realms 1/2\n",
		},
		{
			// `ip rule add fwmark 0x10` sends no mask and the kernel replies
			// with FRA_FWMASK = 0xFFFFFFFF anyway. The mask is present, and it
			// must still print nothing.
			description: "boundary: ip_rule:17 — an all-ones fwmask is present and prints no second token",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				Fwmark: 0x10, HasFwmark: true, Fwmask: 0xffffffff, HasFwmask: true,
				Priority: 1600, HasPriority: true,
				RawTable: unix.RT_TABLE_COMPAT, Table: 1600,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: "1600:\tfrom all fwmark 0x10 lookup 1600\n",
		},
		{
			// RTN_NAT with no gateway. The kernel accepts `nat ADDR`, drops the
			// address, and never sends FRA_UNUSED2 back — so this is the only
			// NAT rendering reachable from a live kernel, and `map-to` below is
			// constructed.
			description: "corner: ip_rule:18 — a NAT rule with no gateway prints masquerade, after the table",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.RTN_NAT,
				Priority: 1700, HasPriority: true,
				RawTable: unix.RT_TABLE_MAIN, Table: unix.RT_TABLE_MAIN,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: "1700:\tfrom all lookup main masquerade\n",
		},
		{
			description: "positive: ip_rule:19 — the main default rule",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				Priority: 32766, HasPriority: true,
				RawTable: unix.RT_TABLE_MAIN, Table: unix.RT_TABLE_MAIN,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: "32766:\tfrom all lookup main\n",
		},
		{
			description: "positive: ip_rule:20 — the default-table rule, which IPv6 does not have",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				Priority: 32767, HasPriority: true,
				RawTable: unix.RT_TABLE_DEFAULT, Table: unix.RT_TABLE_DEFAULT,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: "32767:\tfrom all lookup default\n",
		},

		// ---- ip_rule6 ------------------------------------------------------

		{
			// The family byte changes the prefix rendering and nothing else on
			// this line — which is worth a row precisely because it looks
			// identical to ip_rule:1 in the sidecar.
			description: "positive: ip_rule6:1 — the v6 local rule renders the same text as the v4 one",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET6, Action: unix.FR_ACT_TO_TBL,
				RawTable: unix.RT_TABLE_LOCAL, Table: unix.RT_TABLE_LOCAL,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: "0:\tfrom all lookup local\n",
		},
		{
			// /64 is not AF_INET6's host length, so the suffix prints. afBitLen
			// is what makes this differ from the v4 rows: a 24-bit v4 prefix
			// prints "/24" because 24 != 32, and a 128-bit v6 prefix would print
			// nothing at all.
			description: "positive: ip_rule6:2 — a v6 source prefix renders in v6 form with its length",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET6, Action: unix.FR_ACT_TO_TBL,
				SrcLen:   64,
				Src:      []byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
				Priority: 100, HasPriority: true,
				RawTable: 100, Table: 100,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: "100:\tfrom 2001:db8::/64 lookup 100\n",
		},
		{
			// The mask is LABEL_MAX_MASK, so the text shows one token where the
			// JSON shows two keys (:591-596). `ip rule add flowlabel
			// 0x12345/0xfffff` therefore round-trips to a line that names no
			// mask at all.
			description: "boundary: ip_rule6:3 — a full-width flowlabel mask prints no mask token",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET6, Action: unix.FR_ACT_TO_TBL,
				Flowlabel: 0x12345, HasFlowlabel: true,
				FlowlabelMask: 0xfffff, HasFlowlabelMask: true,
				Priority: 200, HasPriority: true,
				RawTable: 200, Table: 200,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: "200:\tfrom all lookup 200 flowlabel 0x12345\n",
		},
		{
			description: "positive: ip_rule6:4 — the v6 main rule, the last of only two kernel defaults",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET6, Action: unix.FR_ACT_TO_TBL,
				Priority: 32766, HasPriority: true,
				RawTable: unix.RT_TABLE_MAIN, Table: unix.RT_TABLE_MAIN,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: "32766:\tfrom all lookup main\n",
		},

		// ---- arms the topology cannot reach --------------------------------
		//
		// Everything below is constructed. Each names why a live kernel does
		// not produce it, because an unreachable row that stops being
		// unreachable should be moved up rather than left here.

		{
			// The kernel drops the NAT address, so no capture can carry
			// FRA_UNUSED2 — but print_rule reads it (RTA_GATEWAY and
			// FRA_UNUSED2 are both 5) and goip decodes it, so the arm exists
			// and has to be right. It also prints INSTEAD of masquerade, not
			// alongside it.
			description: "corner: constructed — a NAT rule carrying a gateway prints map-to and not masquerade",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.RTN_NAT,
				Gateway: []byte{192, 0, 2, 99}, HasGateway: true,
				Priority: 1700, HasPriority: true,
				RawTable: unix.RT_TABLE_MAIN, Table: unix.RT_TABLE_MAIN,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: "1700:\tfrom all lookup main map-to 192.0.2.99\n",
		},
		{
			// A narrowed port mask changes the BASE of the port itself, not
			// just the token count: the same port 100 prints as 0x64. Nothing
			// in the topology can produce it, because iproute2's `sport`
			// parser only emits FRA_SPORT_MASK for a masked single port and the
			// captured rule uses a range.
			description: "corner: constructed — a narrowed sport mask prints the port in hex, not decimal",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				SportRange: xtcpnl.FibRulePortRange{Start: 100, End: 100}, HasSportRange: true,
				SportMask: 0xff00, HasSportMask: true,
				Priority: 50, HasPriority: true,
				RawTable: unix.RT_TABLE_MAIN, Table: unix.RT_TABLE_MAIN,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: "50:\tfrom all sport 0x64/0xff00 lookup main\n",
		},
		{
			// FIB_RULE_IIF_DETACHED is set by the kernel when the interface the
			// rule names is deleted while the rule stays. The capture cannot
			// reach it because deleting goip0 would end the capture.
			description: "corner: constructed — a detached iif appends [detached] after the name",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				Flags:   unix.FIB_RULE_IIF_DETACHED,
				IifName: "gone0", HasIifName: true,
				Priority: 60, HasPriority: true,
				RawTable: unix.RT_TABLE_MAIN, Table: unix.RT_TABLE_MAIN,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: "60:\tfrom all iif gone0 [detached] lookup main\n",
		},
		{
			description: "corner: constructed — a detached oif appends [detached] the same way",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				Flags:   unix.FIB_RULE_OIF_DETACHED,
				OifName: "gone0", HasOifName: true,
				Priority: 61, HasPriority: true,
				RawTable: unix.RT_TABLE_MAIN, Table: unix.RT_TABLE_MAIN,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: "61:\tfrom all oif gone0 [detached] lookup main\n",
		},
		{
			// FIB_RULE_UNRESOLVED means the goto target no longer exists. The
			// capture's goto points at pref 32766, which always exists.
			description: "corner: constructed — an unresolved goto appends [unresolved] after the target",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_GOTO, Flags: unix.FIB_RULE_UNRESOLVED,
				Goto: 9999, HasGoto: true,
				Priority: 62, HasPriority: true,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: "62:\tfrom all goto 9999 [unresolved]\n",
		},
		{
			// FR_ACT_GOTO with no FRA_GOTO. print_rule prints the literal
			// "none" through print_string where the other arm prints a number
			// through print_uint (:536-541), which is why RuleView.Goto is
			// `any` — the JSON types differ, not just the text.
			description: "corner: constructed — a goto with no target attribute prints the literal none",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_GOTO,
				Priority: 63, HasPriority: true,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: "63:\tfrom all goto none\n",
		},
		{
			// Realm 0 as the `from` half. iproute2 writes the " realms "
			// keyword through PRINT_FP unconditionally and the from value only
			// when nonzero, so the text keeps the keyword and drops the slash.
			description: "corner: constructed — a realms pair whose from half is zero prints only the to half",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				Flow: 2, HasFlow: true,
				Priority: 64, HasPriority: true,
				RawTable: unix.RT_TABLE_MAIN, Table: unix.RT_TABLE_MAIN,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: "64:\tfrom all lookup main realms 2\n",
		},
		{
			// A narrowed flowlabel mask, the arm ip_rule6:3 does not reach.
			description: "corner: constructed — a narrowed flowlabel mask prints both tokens",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET6, Action: unix.FR_ACT_TO_TBL,
				Flowlabel: 0x12345, HasFlowlabel: true,
				FlowlabelMask: 0xff000, HasFlowlabelMask: true,
				Priority: 65, HasPriority: true,
				RawTable: unix.RT_TABLE_MAIN, Table: unix.RT_TABLE_MAIN,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: "65:\tfrom all lookup main flowlabel 0x12345/0xff000\n",
		},
		{
			// tos is a header field, so it is always present and the guard is
			// on the VALUE. dsfieldName is pinned to its numeric fallback,
			// because the name table lives in /etc/iproute2/rt_dsfield and
			// goip reads no config files.
			description: "corner: constructed — a nonzero tos prints its numeric dsfield name",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL, Tos: 0x10,
				Priority: 66, HasPriority: true,
				RawTable: unix.RT_TABLE_MAIN, Table: unix.RT_TABLE_MAIN,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: "66:\tfrom all tos 0x10 lookup main\n",
		},
		{
			// A source prefix at the family's host length prints no suffix
			// (:319-321). No rule in the topology has one, and getting this
			// backwards would append "/32" to every host-route rule.
			description: "boundary: constructed — a /32 v4 source prints no length suffix",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				SrcLen: 32, Src: []byte{192, 0, 2, 1},
				Priority: 67, HasPriority: true,
				RawTable: unix.RT_TABLE_MAIN, Table: unix.RT_TABLE_MAIN,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: "67:\tfrom 192.0.2.1 lookup main\n",
		},
		{
			// The middle arm of print_rule's three-way source branch: a nonzero
			// src_len with no FRA_SRC, which prints the literal "0" and the
			// length. The kernel does not send this pair, and the arm exists
			// because `ip rule save`/`restore` can.
			description: "boundary: constructed — a source length with no address prints the literal 0",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				SrcLen:   24,
				Priority: 68, HasPriority: true,
				RawTable: unix.RT_TABLE_MAIN, Table: unix.RT_TABLE_MAIN,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: "68:\tfrom 0/24 lookup main\n",
		},
		{
			// The fwmark block runs when EITHER attribute is present, and the
			// mark defaults to zero — so a rule carrying only FRA_FWMASK prints
			// ` fwmark 0x0`, with hex0x writing "0" and not "0x0" for the zero.
			description: "corner: constructed — a mask with no mark still prints a fwmark token",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				Fwmask: 0xff, HasFwmask: true,
				Priority: 69, HasPriority: true,
				RawTable: unix.RT_TABLE_MAIN, Table: unix.RT_TABLE_MAIN,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: "69:\tfrom all fwmark 0/0xff lookup main\n",
		},
		{
			// FRA_L3MDEV present with value zero. The guard is `if (mdev)`,
			// inside a guard on the attribute, so this prints nothing — the one
			// place in print_rule where a present attribute holding zero is
			// silent.
			description: "negative: constructed — FRA_L3MDEV carrying zero prints no token",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				L3mdev: 0, HasL3mdev: true,
				Priority: 70, HasPriority: true,
				RawTable: unix.RT_TABLE_MAIN, Table: unix.RT_TABLE_MAIN,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: "70:\tfrom all lookup main\n",
		},
		{
			// Both suppress tokens are nested inside `if (table)`, so a rule
			// with table 0 prints neither however they are set. That is not
			// hypothetical — it is what an l3mdev rule with a suppress selector
			// would be.
			description: "negative: constructed — suppress tokens with table 0 print nothing, being nested inside the table guard",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				SuppressIfgroup: 7, HasSuppressIfgroup: true,
				HasSuppressPrefixlen: true, SuppressPrefixlen: 0,
				Priority: 71, HasPriority: true,
			},
			want: "71:\tfrom all\n",
		},
		{
			// The catch-all action arm's guard is `!= FR_ACT_TO_TBL`, not a
			// default. A renderer that called routeTypeName unconditionally
			// would append " unicast" to every table rule in the dump.
			description: "negative: constructed — FR_ACT_TO_TBL prints no action token, though it has a type name",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				Priority: 72, HasPriority: true,
				RawTable: unix.RT_TABLE_MAIN, Table: unix.RT_TABLE_MAIN,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: "72:\tfrom all lookup main\n",
		},
		{
			// A rule with nothing but a preference. Two tokens survive: the
			// "%u:\t" prefix and the literal "all", both unconditional.
			description: "boundary: constructed — the emptiest renderable rule still prints a preference and from all",
			in:          xtcpnl.RuleInfo{Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL},
			want:        "0:\tfrom all\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := RuleViewOf(tt.in).Text(); got != tt.want {
				t.Errorf("Text() =\n  %q\nwant\n  %q", got, tt.want)
			}
		})
	}
}

// TestRuleViewOfTextDetails is `ip -d rule show`, which adds exactly one token
// to every line and is therefore the cheapest -d in goip.
//
// It is also the clearest presence-versus-value case in the corpus. Every rule
// the topology adds carries FRA_PROTOCOL with value ZERO, and the kernel's own
// three carry RTPROT_KERNEL; the guard
// `(protocol && protocol != RTPROT_KERNEL) || show_details > 0` (:551-557)
// suppresses both without -d, so a decoder that dropped the attribute entirely
// would agree with `ip` on all twenty lines of ip_rule and disagree on all
// twenty of ip_rule_n.
//
// The rows here are the corresponding ip_rule_n lines.
func TestRuleViewOfTextDetails(t *testing.T) {
	tests := []struct {
		description string
		in          xtcpnl.RuleInfo
		want        string
	}{
		{
			description: "positive: ip_rule_n:1 — a kernel-owned rule names RTPROT_KERNEL under -d",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				RawTable: unix.RT_TABLE_LOCAL, Table: unix.RT_TABLE_LOCAL,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
				Protocol: unix.RTPROT_KERNEL, HasProtocol: true,
			},
			want: "0:\tfrom all lookup local proto kernel\n",
		},
		{
			// Protocol ZERO, present. `ip` prints "unspec", which is
			// rtnl_rtprot_n2a's name for 0 and not a fallback — the number has
			// a name, so getting here through a numeric fallback would print
			// "0" and look almost right.
			description: "positive: ip_rule_n:2 — a user-added rule carries protocol 0, which names unspec",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				SrcLen: 24, Src: []byte{192, 0, 2, 0},
				Priority: 100, HasPriority: true,
				RawTable: 100, Table: 100,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
				Protocol: 0, HasProtocol: true,
			},
			want: "100:\tfrom 192.0.2.0/24 lookup 100 proto unspec\n",
		},
		{
			// The `proto` token goes between the action and the flowlabel,
			// which is the one place in print_rule where a -d token is not
			// last. ip_rule6_n:3 is the line that shows it.
			description: "corner: ip_rule6_n:3 — proto prints BEFORE flowlabel, not at the end of the line",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET6, Action: unix.FR_ACT_TO_TBL,
				Flowlabel: 0x12345, HasFlowlabel: true,
				FlowlabelMask: 0xfffff, HasFlowlabelMask: true,
				Priority: 200, HasPriority: true,
				RawTable: 200, Table: 200,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
				Protocol: 0, HasProtocol: true,
			},
			want: "200:\tfrom all lookup 200 proto unspec flowlabel 0x12345\n",
		},
		{
			// The action token comes before proto too, so a NAT rule under -d
			// prints table, action, protocol in that order.
			description: "corner: ip_rule_n:18 — the action token precedes proto",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.RTN_NAT,
				Priority: 1700, HasPriority: true,
				RawTable: unix.RT_TABLE_MAIN, Table: unix.RT_TABLE_MAIN,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
				Protocol: 0, HasProtocol: true,
			},
			want: "1700:\tfrom all lookup main masquerade proto unspec\n",
		},
		{
			// A protocol that is neither zero nor RTPROT_KERNEL prints WITHOUT
			// -d, which is the half of the guard the twenty captured lines
			// cannot show. No rule in the topology has one.
			description: "positive: constructed — a protocol other than 0 or kernel prints with no -d at all",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				Priority: 73, HasPriority: true,
				RawTable: unix.RT_TABLE_MAIN, Table: unix.RT_TABLE_MAIN,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
				Protocol: unix.RTPROT_STATIC, HasProtocol: true,
			},
			want: "73:\tfrom all lookup main proto static\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := RuleViewOf(tt.in).Text(); got != tt.want {
				t.Errorf("Text() =\n  %q\nwant\n  %q", got, tt.want)
			}
		})
	}
}

// TestRuleViewJSONOrder pins MarshalJSON against
// pkg/xtcpnl/testdata/7_1_4/dumps/ip_rule_json, the `ip -j rule show` sidecar.
//
// Order is asserted as a whole object rather than key by key, because order is
// the contract this MarshalJSON exists to keep: RuleView builds its object key
// by key precisely so the four print_null tokens can be interleaved with the
// tagged fields, and a test that only checked which keys were present would
// pass on an implementation that had lost the reason for the machinery.
//
// The values below are ip_rule_json with its whitespace removed. Three of them
// are worth naming before the table:
//
//   - "srclen": 24 is a NUMBER and "table": "100" is a STRING, on the same
//     object, because rtnl_rttable_n2a formats into a buffer even when the id
//     has no name.
//   - "suppress_ifgroup": "5" is a string and "suppress_prefixlen": 0 is a
//     number, for the same reason one goes through print_string and the other
//     through print_uint.
//   - "dport_mask": "0xffff" appears on a line whose TEXT shows no mask, since
//     print_0xhex's PRINT_JSON call runs before the PRINT_FP branch chooses.
func TestRuleViewJSONOrder(t *testing.T) {
	tests := []struct {
		description string
		in          xtcpnl.RuleInfo
		want        string
	}{
		{
			description: "positive: ip_rule_json:1 — priority, src and table, in that order",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				RawTable: unix.RT_TABLE_LOCAL, Table: unix.RT_TABLE_LOCAL,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: `{"priority":0,"src":"all","table":"local"}`,
		},
		{
			description: "corner: ip_rule_json:2 — srclen is a number on the same object where table is a string",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				SrcLen: 24, Src: []byte{192, 0, 2, 0},
				Priority: 100, HasPriority: true,
				RawTable: 100, Table: 100,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: `{"priority":100,"src":"192.0.2.0","srclen":24,"table":"100"}`,
		},
		{
			description: "positive: ip_rule_json:3 — dst and dstlen follow src, and iif follows both",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				DstLen: 24, Dst: []byte{198, 51, 100, 0},
				IifName: "goip0", HasIifName: true,
				Priority: 200, HasPriority: true,
				RawTable: 200, Table: 200,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: `{"priority":200,"src":"all","dst":"198.51.100.0","dstlen":24,` +
				`"iif":"goip0","table":"200"}`,
		},
		{
			// Both mark keys are hex STRINGS, which is print_0xhex's JSON
			// behavior and not a Go choice.
			description: "positive: ip_rule_json:4 — fwmark and fwmask are hex strings",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				Fwmark: 0x1234, HasFwmark: true, Fwmask: 0xff00, HasFwmask: true,
				Priority: 300, HasPriority: true,
				RawTable: unix.RT_TABLE_COMPAT, Table: 300,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: `{"priority":300,"src":"all","fwmark":"0x1234","fwmask":"0xff00","table":"300"}`,
		},
		{
			description: "positive: ip_rule_json:6 — the uid range is two numeric keys",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				UidRange: xtcpnl.FibRuleUidRange{Start: 1000, End: 2000}, HasUidRange: true,
				Priority: 500, HasPriority: true,
				RawTable: unix.RT_TABLE_COMPAT, Table: 500,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: `{"priority":500,"src":"all","uid_start":1000,"uid_end":2000,"table":"500"}`,
		},
		{
			// The mask key the text does not show. Both port blocks are here at
			// once, so the row also pins that the range arm emits NO mask key
			// while the single arm does.
			description: "corner: ip_rule_json:7 — dport_mask is emitted although the text prints no mask, and the sport range emits none",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				SportRange: xtcpnl.FibRulePortRange{Start: 1000, End: 2000}, HasSportRange: true,
				DportRange: xtcpnl.FibRulePortRange{Start: 80, End: 80}, HasDportRange: true,
				DportMask: 0xffff, HasDportMask: true,
				Priority: 600, HasPriority: true,
				RawTable: unix.RT_TABLE_COMPAT, Table: 600,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: `{"priority":600,"src":"all","sport_start":1000,"sport_end":2000,` +
				`"dport":80,"dport_mask":"0xffff","table":"600"}`,
		},
		{
			description: "boundary: ip_rule_json:8 — suppress_prefixlen is a number, and zero is emitted",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				Priority: 700, HasPriority: true,
				RawTable: unix.RT_TABLE_MAIN, Table: unix.RT_TABLE_MAIN,
				HasSuppressPrefixlen: true, SuppressPrefixlen: 0,
			},
			want: `{"priority":700,"src":"all","table":"main","suppress_prefixlen":0}`,
		},
		{
			// blackhole is the VALUE of the key "action", where nop and
			// masquerade three rows down are null-valued KEYS. They read like
			// three spellings of one thing and marshal two different ways.
			description: "corner: ip_rule_json:9 — blackhole is a value under the action key",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.RTN_BLACKHOLE,
				Priority: 800, HasPriority: true,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: `{"priority":800,"src":"all","action":"blackhole"}`,
		},
		{
			description: "corner: ip_rule_json:10 — goto is a NUMBER, not a string and not a null key",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_GOTO,
				Goto: 32766, HasGoto: true,
				Priority: 900, HasPriority: true,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: `{"priority":900,"src":"all","goto":32766}`,
		},
		{
			// `not` is a null key and it sits BETWEEN priority and src, which
			// is the interleaving struct tags cannot express and the reason
			// this MarshalJSON is written by hand.
			description: "corner: ip_rule_json:11 — the not key is null-valued and comes between priority and src",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL, Flags: unix.FIB_RULE_INVERT,
				SrcLen: 24, Src: []byte{203, 0, 113, 0},
				Priority: 1000, HasPriority: true,
				RawTable: unix.RT_TABLE_MAIN, Table: unix.RT_TABLE_MAIN,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: `{"priority":1000,"not":null,"src":"203.0.113.0","srclen":24,"table":"main"}`,
		},
		{
			description: "corner: ip_rule_json:12 — nop is a null-valued key, not the string nop and not true",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_NOP,
				Priority: 1100, HasPriority: true,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: `{"priority":1100,"src":"all","nop":null}`,
		},
		{
			// l3mdev emits a null key and NO table key, because the table is 0
			// and the JSON has nothing to put the literal "[l3mdev-table]" in.
			// The text and the JSON therefore disagree about how much this rule
			// says, which is iproute2's behavior and not a gap.
			description: "corner: ip_rule_json:13 — l3mdev is a null key and no table key accompanies it",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				L3mdev: 1, HasL3mdev: true,
				Priority: 1200, HasPriority: true,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: `{"priority":1200,"src":"all","l3mdev":null}`,
		},
		{
			description: "positive: ip_rule_json:14 — tun_id is a number and precedes the table key",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				TunID: 42, HasTunID: true,
				Priority: 1300, HasPriority: true,
				RawTable: unix.RT_TABLE_COMPAT, Table: 1300,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: `{"priority":1300,"src":"all","tun_id":42,"table":"1300"}`,
		},
		{
			description: "corner: ip_rule_json:15 — suppress_ifgroup is a STRING where its prefixlen sibling is a number",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				SuppressIfgroup: 5, HasSuppressIfgroup: true,
				Priority: 1400, HasPriority: true,
				RawTable: unix.RT_TABLE_MAIN, Table: unix.RT_TABLE_MAIN,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: `{"priority":1400,"src":"all","table":"main","suppress_ifgroup":"5"}`,
		},
		{
			description: "positive: ip_rule_json:16 — the realms pair is two string keys, after the table",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				Flow: 1<<16 | 2, HasFlow: true,
				Priority: 1500, HasPriority: true,
				RawTable: unix.RT_TABLE_COMPAT, Table: 1500,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: `{"priority":1500,"src":"all","table":"1500","flow_from":"1","flow_to":"2"}`,
		},
		{
			// The all-ones mask is dropped from the JSON too, because
			// setRuleFwmark never sets it — unlike the flowlabel mask, whose
			// full-width value IS emitted to JSON. Two masks, two opposite
			// rules, both from print_rule.
			description: "boundary: ip_rule_json:17 — an all-ones fwmask emits no fwmask key",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				Fwmark: 0x10, HasFwmark: true, Fwmask: 0xffffffff, HasFwmask: true,
				Priority: 1600, HasPriority: true,
				RawTable: unix.RT_TABLE_COMPAT, Table: 1600,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: `{"priority":1600,"src":"all","fwmark":"0x10","table":"1600"}`,
		},
		{
			description: "corner: ip_rule_json:18 — masquerade is a null key, where blackhole was a value",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.RTN_NAT,
				Priority: 1700, HasPriority: true,
				RawTable: unix.RT_TABLE_MAIN, Table: unix.RT_TABLE_MAIN,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: `{"priority":1700,"src":"all","table":"main","masquerade":null}`,
		},
		{
			// The mask the text suppressed. ip_rule_json holds no v6 rule — the
			// sidecar is the v4 dump — so this is transcribed from the C rather
			// than from a golden, and it is the one asymmetry in the file:
			// LABEL_MAX_MASK goes to JSON and not to the terminal (:591-596).
			description: "corner: constructed — a full-width flowlabel mask is emitted to JSON although the text drops it",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET6, Action: unix.FR_ACT_TO_TBL,
				Flowlabel: 0x12345, HasFlowlabel: true,
				FlowlabelMask: 0xfffff, HasFlowlabelMask: true,
				Priority: 200, HasPriority: true,
				RawTable: 200, Table: 200,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: `{"priority":200,"src":"all","table":"200","flowlabel":"0x12345",` +
				`"flowlabel_mask":"0xfffff"}`,
		},
		{
			description: "corner: constructed — a goto with no target marshals as the STRING none, not a number",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_GOTO,
				Priority: 63, HasPriority: true,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: `{"priority":63,"src":"all","goto":"none"}`,
		},
		{
			description: "corner: constructed — an unresolved goto appends a null key after the target",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_GOTO, Flags: unix.FIB_RULE_UNRESOLVED,
				Goto: 9999, HasGoto: true,
				Priority: 62, HasPriority: true,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: `{"priority":62,"src":"all","goto":9999,"unresolved":null}`,
		},
		{
			description: "corner: constructed — the detached keys are null and follow their interface names",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				Flags:   unix.FIB_RULE_IIF_DETACHED | unix.FIB_RULE_OIF_DETACHED,
				IifName: "gone0", HasIifName: true,
				OifName: "gone1", HasOifName: true,
				Priority: 60, HasPriority: true,
				RawTable: unix.RT_TABLE_MAIN, Table: unix.RT_TABLE_MAIN,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: `{"priority":60,"src":"all","iif":"gone0","iif_detached":null,` +
				`"oif":"gone1","oif_detached":null,"table":"main"}`,
		},
		{
			// print_rule writes no "flow_from" when the from half is zero, so
			// the JSON loses half the pair while the text keeps the keyword.
			description: "corner: constructed — a zero from-realm drops the flow_from key entirely",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				Flow: 2, HasFlow: true,
				Priority: 64, HasPriority: true,
				RawTable: unix.RT_TABLE_MAIN, Table: unix.RT_TABLE_MAIN,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
			},
			want: `{"priority":64,"src":"all","table":"main","flow_to":"2"}`,
		},
		{
			description: "positive: constructed — protocol is the last key before the flowlabel pair",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				Priority: 73, HasPriority: true,
				RawTable: unix.RT_TABLE_MAIN, Table: unix.RT_TABLE_MAIN,
				HasSuppressPrefixlen: true, SuppressPrefixlen: ruleSentinelCst,
				Protocol: unix.RTPROT_STATIC, HasProtocol: true,
			},
			want: `{"priority":73,"src":"all","table":"main","protocol":"static"}`,
		},
		{
			description: "boundary: constructed — the emptiest rule still marshals a two-key object, not an empty one",
			in:          xtcpnl.RuleInfo{Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL},
			want:        `{"priority":0,"src":"all"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got, err := json.Marshal(RuleViewOf(tt.in))
			if err != nil {
				t.Fatalf("json.Marshal: %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("MarshalJSON() =\n  %s\nwant\n  %s", got, tt.want)
			}
			// Hand-built JSON is only worth having if it is JSON. Every row is
			// round-tripped so a missing comma or a doubled key cannot pass by
			// matching the string it was written against.
			var back map[string]any
			if err := json.Unmarshal(got, &back); err != nil {
				t.Errorf("Unmarshal(%s): %v", got, err)
			}
		})
	}
}

// TestRuleGotoText sweeps ruleGotoText directly, because RuleView.Goto is the
// one `any` field in this package and a third dynamic type reaching it should
// be a visible wrong answer rather than a plausible one.
func TestRuleGotoText(t *testing.T) {
	tests := []struct {
		description string
		in          any
		want        string
	}{
		{description: "positive: a uint32 target renders in decimal", in: uint32(32766), want: "32766"},
		{description: "positive: the literal none passes through", in: "none", want: "none"},
		{description: "boundary: zero is a legal preference and renders as 0", in: uint32(0), want: "0"},
		{
			description: "boundary: the largest uint32 renders unsigned, not as -1",
			in:          uint32(0xFFFFFFFF), want: "4294967295",
		},
		{
			// Neither arm. An int is what a careless `v.Goto = 5` would put
			// here, and the empty string it produces is a diff rather than a
			// line that happens to read correctly.
			description: "negative: a type neither arm handles renders empty rather than guessing",
			in:          5, want: "",
		},
		{description: "corner: nil renders empty, which is the absence the caller already guards", in: nil, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := ruleGotoText(tt.in); got != tt.want {
				t.Errorf("ruleGotoText(%#v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestHex0x pins print_0xhex's one surprise: C's "%#llx" of zero is "0" with NO
// prefix, because the # flag is documented to prefix only nonzero values.
//
// Every hex token in a rule line goes through this — fwmark, the masked ports,
// the flowlabel pair — so a naive "0x"+FormatUint would put "0x0" on the one
// line in the corner rows above that prints a zero mark.
func TestHex0x(t *testing.T) {
	tests := []struct {
		description string
		in          uint64
		want        string
	}{
		{description: "corner: zero has no 0x prefix, matching C's %#llx", in: 0, want: "0"},
		{description: "positive: a small value is prefixed", in: 0x10, want: "0x10"},
		{description: "positive: hex digits are lower case", in: 0xabcdef, want: "0xabcdef"},
		{description: "boundary: the all-ones fwmask, the value that suppresses its own token", in: 0xFFFFFFFF, want: "0xffffffff"},
		{description: "boundary: LABEL_MAX_MASK, the flowlabel equivalent", in: 0x000FFFFF, want: "0xfffff"},
		{description: "boundary: the widest value tun_id can hold", in: ^uint64(0), want: "0xffffffffffffffff"},
		{description: "boundary: one is the smallest prefixed value", in: 1, want: "0x1"},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := hex0x(tt.in); got != tt.want {
				t.Errorf("hex0x(%d) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestRuleViewTextTabSeparator asserts the one separator on the line that is
// not a space, over every row the sidecar supplies.
//
// It is a property rather than a row because it is easy to lose in a rewrite
// and impossible to see in a diff: "0: from all" and "0:\tfrom all" render
// identically in most terminals, and the parity harness compares bytes.
func TestRuleViewTextTabSeparator(t *testing.T) {
	tests := []struct {
		description string
		in          xtcpnl.RuleInfo
	}{
		{
			description: "positive: the local rule, whose preference is a single digit",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				RawTable: unix.RT_TABLE_LOCAL, Table: unix.RT_TABLE_LOCAL,
			},
		},
		{
			description: "boundary: a five-digit preference, where a column-aligned formatter would differ",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL,
				Priority: 32767, HasPriority: true,
				RawTable: unix.RT_TABLE_DEFAULT, Table: unix.RT_TABLE_DEFAULT,
			},
		},
		{
			description: "corner: an inverted rule, where the not token sits between the tab and the source",
			in: xtcpnl.RuleInfo{
				Family: unix.AF_INET, Action: unix.FR_ACT_TO_TBL, Flags: unix.FIB_RULE_INVERT,
				Priority: 1000, HasPriority: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got := RuleViewOf(tt.in).Text()
			i := strings.IndexByte(got, ':')
			if i < 0 || i+1 >= len(got) {
				t.Fatalf("Text() = %q, want a preference followed by a colon", got)
			}
			if got[i+1] != '\t' {
				t.Errorf("Text() = %q, want a TAB after the colon, got %q", got, got[i+1])
			}
			if strings.Count(got, "\t") != 1 {
				t.Errorf("Text() = %q, want exactly one tab on the line", got)
			}
			if !strings.HasSuffix(got, "\n") || strings.Count(got, "\n") != 1 {
				t.Errorf("Text() = %q, want exactly one trailing newline", got)
			}
		})
	}
}
