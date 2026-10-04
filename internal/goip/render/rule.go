package render

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// RuleView is one line of `ip rule show`, transcribed from print_rule
// (ip/iprule.c:279-600).
//
// # This is the only object goip renders with no NameTab
//
// print_rule resolves no index to a name, because there is no index to
// resolve: FRA_IIFNAME and FRA_OIFNAME arrive as strings. That is also why
// iprule_list_flush_or_save calls no ll_init_map, and why `ip rule show` is
// one transaction where `ip neigh show` is two. A NameTab parameter here would
// be an argument no code path could use.
//
// # Field order is print order, twice over
//
// Once for the text, because print_rule builds a single line out of nineteen
// optional runs, so a token in the wrong position is a diff that names the
// wrong branch. And once for JSON, because `ip -j` writes keys in print order
// — see MarshalJSON, which does NOT rely on struct tags for that.
//
// The ordering fact worth stating outright, because it reads backwards:
// `realms` (:509-521) comes AFTER the `lookup TABLE` block (:479-505), and the
// action token (:524-548) after both. A rule with a table, a realm and an
// action prints them in that order regardless of how the command line named
// them.
type RuleView struct {
	Priority uint32

	// Not is the FIB_RULE_INVERT token, print_null at :313-314 — the only
	// token on the line that precedes the source.
	Not bool

	// Src is never empty. print_rule's three-way branch at :316-331 ends in
	// the literal "all", so every rule has a src key. Dst's else-arm prints
	// nothing at all (:332-344), which is the asymmetry that keeps these two
	// from sharing a helper.
	Src    string
	SrcLen *uint8
	Dst    string
	DstLen *uint8

	Tos string

	// Fwmark and Fwmask are strings because print_0xhex writes `0x1234` and
	// `ip -j` emits that same string rather than a number.
	Fwmark string
	Fwmask string

	Iif         string
	IifDetached bool
	Oif         string
	OifDetached bool

	// L3mdev is the ` lookup [l3mdev-table]` token (:391-399). It is the one
	// place in print_rule where a PRESENT attribute carrying zero prints
	// nothing: the guard is `if (mdev)`, on the value, inside a guard on the
	// attribute.
	L3mdev bool

	UIDStart *uint32
	UIDEnd   *uint32

	// The port blocks carry three fields each because the single-value arm
	// and the range arm use different JSON KEYS, not merely different text
	// (:412-472). Mask is set only on the single-value arm, where iproute2
	// reads it; the range arm never looks at FRA_*PORT_MASK even when the
	// kernel sent one, and it does send one.
	Sport      *uint16
	SportMask  *uint16
	SportStart *uint16
	SportEnd   *uint16
	Dport      *uint16
	DportMask  *uint16
	DportStart *uint16
	DportEnd   *uint16

	TunID *uint64

	// Table is the resolved name or number, and it is a JSON STRING even when
	// it is a number: print_string writes `"table": "100"` because
	// rtnl_rttable_n2a's fallback formats into a buffer. Empty means table 0,
	// where `if (table)` (:479) skips the block and the two suppress tokens
	// nested inside it.
	Table string

	SuppressPrefixlen *int32
	SuppressIfgroup   *string

	FlowFrom string
	FlowTo   string

	// The action arms, :524-548, mutually exclusive and in source order.
	// Three of the four emit a print_null key rather than a value, which is
	// why they are separate bools instead of one string: `nop` and
	// `masquerade` are keys, while `blackhole` is the VALUE of the key
	// "action". A single field cannot be both.
	NatGateway string
	Masquerade bool
	// Goto is `any` because the two arms disagree about type: with FRA_GOTO
	// it is print_uint and marshals as a number, without it is print_string
	// and marshals as the string "none" (:536-541).
	Goto       any
	Unresolved bool
	Nop        bool
	Action     string

	Protocol string

	Flowlabel     string
	FlowlabelMask string
}

// ruleSuppressUnsetCst is the sentinel print_rule compares the two suppress
// attributes against before printing them (:487 and :495).
//
// iproute2 reads each into an `int` and tests `!= -1`, so the wire value being
// filtered is 0xFFFFFFFF. Written as the signed constant because the
// comparison is the signed one; testing an unsigned value against 4294967295
// is the same test but stops looking like the C it came from.
const ruleSuppressUnsetCst int32 = -1

// ruleL3mdevTableCst is the literal print_rule writes in place of a table name
// for an l3mdev rule (:397-398). A fixed string and not a lookup: the table
// such a rule uses is chosen per packet from the l3mdev device, so there is no
// number to name.
const ruleL3mdevTableCst = "[l3mdev-table]"

// rulePortMaxMaskCst is iproute2's PORT_MAX_MASK, the all-ones 16-bit port
// mask. A single port whose mask equals it prints as a decimal port with no
// mask token, which is the ordinary case — the kernel attaches
// FRA_DPORT_MASK = 0xffff to every `dport N` rule.
const rulePortMaxMaskCst uint16 = 0xFFFF

// ruleFlowlabelMaxMaskCst is iproute2's LABEL_MAX_MASK, the 20-bit IPv6 flow
// label. A mask equal to it goes to JSON only (:591-596).
const ruleFlowlabelMaxMaskCst uint32 = 0x000FFFFF

// RuleViewOf builds the view for one decoded rule.
//
// Every branch is in print_rule's order and cites its line. The two attributes
// goip decodes and does not render — FRA_IP_PROTO and FRA_DSCP — are absent
// here on purpose and are refused before this is reached, by
// checkRuleRenderable in the goip package.
func RuleViewOf(ri xtcpnl.RuleInfo) RuleView {
	hostLen := afBitLen(ri.Family)

	v := RuleView{
		Priority: ri.Priority,
		Not:      ri.Flags&unix.FIB_RULE_INVERT != 0,
	}

	// Source, :316-331. Three arms, the last of which is the literal "all".
	switch {
	case ri.Src != nil:
		v.Src = addrString(ri.Src, ri.Family)
		if int(ri.SrcLen) != hostLen {
			v.SrcLen = ruleU8Ptr(ri.SrcLen)
		}
	case ri.SrcLen != 0:
		v.Src = "0"
		v.SrcLen = ruleU8Ptr(ri.SrcLen)
	default:
		v.Src = "all"
	}

	// Destination, :332-344. No "all" arm: a rule with neither FRA_DST nor a
	// dst_len prints no `to` token at all.
	switch {
	case ri.Dst != nil:
		v.Dst = addrString(ri.Dst, ri.Family)
		if int(ri.DstLen) != hostLen {
			v.DstLen = ruleU8Ptr(ri.DstLen)
		}
	case ri.DstLen != 0:
		v.Dst = "0"
		v.DstLen = ruleU8Ptr(ri.DstLen)
	}

	// :346-350. Guarded on the VALUE, because tos is a header field and so is
	// always present.
	if ri.Tos != 0 {
		v.Tos = dsfieldName(ri.Tos)
	}

	setRuleFwmark(&v, ri)

	if ri.HasIifName {
		v.Iif = ri.IifName
		v.IifDetached = ri.Flags&unix.FIB_RULE_IIF_DETACHED != 0
	}
	if ri.HasOifName {
		v.Oif = ri.OifName
		v.OifDetached = ri.Flags&unix.FIB_RULE_OIF_DETACHED != 0
	}
	v.L3mdev = ri.HasL3mdev && ri.L3mdev != 0

	if ri.HasUidRange {
		v.UIDStart = ruleU32Ptr(ri.UidRange.Start)
		v.UIDEnd = ruleU32Ptr(ri.UidRange.End)
	}

	setRulePorts(&v, ri)

	if ri.HasTunID {
		tid := ri.TunID
		v.TunID = &tid
	}

	setRuleTable(&v, ri)
	setRuleFlow(&v, ri)
	setRuleAction(&v, ri)

	// :551-557. The `-d` half of the guard is applied by the caller, which
	// clears HasProtocol when neither condition holds; see ruleShow in the
	// goip package. Keeping it there is what lets this function stay a pure
	// function of decoded bytes, with no CLI state threaded in.
	if ri.HasProtocol {
		v.Protocol = routeProtoName(ri.Protocol)
	}

	setRuleFlowlabel(&v, ri)

	return v
}

// setRuleFwmark is :352-366, whose shape is easy to get subtly wrong.
//
// The block runs when EITHER attribute is present and the mark defaults to
// zero, so a rule carrying only FRA_FWMASK still prints ` fwmark 0x0`. The
// mask prints only when it is present and not 0xFFFFFFFF, and iproute2 writes
// that as an assignment inside the condition — so a present all-ones mask
// falls to the one-token arm rather than printing `/0xffffffff`.
func setRuleFwmark(v *RuleView, ri xtcpnl.RuleInfo) {
	if !ri.HasFwmark && !ri.HasFwmask {
		return
	}
	v.Fwmark = hex0x(uint64(ri.Fwmark))
	if ri.HasFwmask && ri.Fwmask != 0xFFFFFFFF {
		v.Fwmask = hex0x(uint64(ri.Fwmask))
	}
}

// setRulePorts is :412-472, two blocks with the same three-way shape.
//
// A range whose ends differ takes the `_start`/`_end` keys and IGNORES the
// mask attribute entirely. A range whose ends are equal takes the single key,
// and then looks at the mask: present and equal to PORT_MAX_MASK prints a
// decimal port, present and narrower prints two hex tokens, absent prints a
// decimal port with no mask key at all.
//
// All three arms are reachable from the committed corpus for dport, because
// the kernel attaches FRA_DPORT_MASK = 0xffff to a plain `dport N` rule
// without being asked — which is why the JSON golden has a `dport_mask` key
// on a line whose text shows no mask.
func setRulePorts(v *RuleView, ri xtcpnl.RuleInfo) {
	if ri.HasSportRange {
		if ri.SportRange.Start == ri.SportRange.End {
			v.Sport = ruleU16Ptr(ri.SportRange.Start)
			if ri.HasSportMask {
				v.SportMask = ruleU16Ptr(ri.SportMask)
			}
		} else {
			v.SportStart = ruleU16Ptr(ri.SportRange.Start)
			v.SportEnd = ruleU16Ptr(ri.SportRange.End)
		}
	}
	if ri.HasDportRange {
		if ri.DportRange.Start == ri.DportRange.End {
			v.Dport = ruleU16Ptr(ri.DportRange.Start)
			if ri.HasDportMask {
				v.DportMask = ruleU16Ptr(ri.DportMask)
			}
		} else {
			v.DportStart = ruleU16Ptr(ri.DportRange.Start)
			v.DportEnd = ruleU16Ptr(ri.DportRange.End)
		}
	}
}

// setRuleTable is :479-505. The two suppress tokens are NESTED inside the
// `if (table)` guard, so a rule with suppress_prefixlength and table 0 prints
// neither — which is not hypothetical, it is exactly what an l3mdev rule is.
func setRuleTable(v *RuleView, ri xtcpnl.RuleInfo) {
	if ri.Table == 0 {
		return
	}
	v.Table = routeTableName(ri.Table)

	if ri.HasSuppressPrefixlen {
		if pl := int32(ri.SuppressPrefixlen); pl != ruleSuppressUnsetCst {
			v.SuppressPrefixlen = &pl
		}
	}
	if ri.HasSuppressIfgroup {
		if int32(ri.SuppressIfgroup) != ruleSuppressUnsetCst {
			name := groupName(ri.SuppressIfgroup)
			v.SuppressIfgroup = &name
		}
	}
}

// setRuleFlow is :509-521, the realms pair.
//
// FRA_FLOW packs two 16-bit realms as from<<16 | to. When `from` is zero
// iproute2 prints the literal " realms " through PRINT_FP and emits no
// "flow_from" key, so the text keeps the keyword while the JSON loses half the
// pair. That asymmetry is why FlowFrom is a field rather than a prefix baked
// into FlowTo.
func setRuleFlow(v *RuleView, ri xtcpnl.RuleInfo) {
	if !ri.HasFlow {
		return
	}
	if from := ri.Flow >> 16; from != 0 {
		v.FlowFrom = realmName(from)
	}
	v.FlowTo = realmName(ri.Flow & 0xFFFF)
}

// setRuleAction is :524-548: four mutually exclusive arms, in source order.
//
// The first tests RTN_NAT, which is 10 and so is not in the FR_ACT_* space at
// all — that enum stops at FR_ACT_PROHIBIT = 8. iproute2 reuses the RTN_*
// numbering for this one legacy action, and a modern kernel accepts the action
// while dropping the address, so the arm that fires on a live box is
// `masquerade` rather than `map-to`.
//
// The last arm's guard is `!= FR_ACT_TO_TBL` rather than a default, because
// FR_ACT_TO_TBL is the ordinary case and prints nothing: a renderer that
// called routeTypeName unconditionally would append ` unicast` to every table
// rule.
func setRuleAction(v *RuleView, ri xtcpnl.RuleInfo) {
	switch {
	case ri.Action == unix.RTN_NAT:
		if ri.HasGateway {
			v.NatGateway = addrString(ri.Gateway, ri.Family)
		} else {
			v.Masquerade = true
		}
	case ri.Action == unix.FR_ACT_GOTO:
		if ri.HasGoto {
			v.Goto = ri.Goto
		} else {
			v.Goto = "none"
		}
		v.Unresolved = ri.Flags&unix.FIB_RULE_UNRESOLVED != 0
	case ri.Action == unix.FR_ACT_NOP:
		v.Nop = true
	case ri.Action != unix.FR_ACT_TO_TBL:
		v.Action = routeTypeName(ri.Action)
	}
}

// setRuleFlowlabel is :583-596, the third place in print_rule where the two
// output modes emit a different NUMBER of values rather than a different
// spelling of one.
//
// Both attributes or neither — the comment above the C says so and the kernel
// honors it — and when the mask is LABEL_MAX_MASK it goes to JSON only, so the
// text shows one token where the JSON shows two keys.
func setRuleFlowlabel(v *RuleView, ri xtcpnl.RuleInfo) {
	if !ri.HasFlowlabel || !ri.HasFlowlabelMask {
		return
	}
	v.Flowlabel = hex0x(uint64(ri.Flowlabel))
	v.FlowlabelMask = hex0x(uint64(ri.FlowlabelMask))
}

// Text renders the line, token by token, in print_rule's order.
//
// Written as appends rather than one format string because almost every token
// is optional and several carry their separator inside the C format string.
// The two that matter:
//
//   - the priority's format is "%u:\t", so the separator after it is a TAB.
//     Every other separator on the line is one leading space.
//   - the `from `, ` to `, ` iif ` and ` oif ` keywords are written by a
//     PRINT_FP call separate from the value's print_color_string, so in C an
//     empty value would leave a dangling keyword. Here keyword and value are
//     written together, which is the same output and cannot dangle.
func (v RuleView) Text() string {
	var b strings.Builder

	b.WriteString(strconv.FormatUint(uint64(v.Priority), 10))
	b.WriteString(":\t")
	if v.Not {
		b.WriteString("not ")
	}

	b.WriteString("from ")
	b.WriteString(v.Src)
	writeSlashU8(&b, v.SrcLen)

	if v.Dst != "" {
		b.WriteString(" to ")
		b.WriteString(v.Dst)
		writeSlashU8(&b, v.DstLen)
	}
	if v.Tos != "" {
		b.WriteString(" tos ")
		b.WriteString(v.Tos)
	}
	if v.Fwmark != "" {
		b.WriteString(" fwmark ")
		b.WriteString(v.Fwmark)
		if v.Fwmask != "" {
			b.WriteByte('/')
			b.WriteString(v.Fwmask)
		}
	}
	writeRuleIfaceText(&b, "iif", v.Iif, v.IifDetached)
	writeRuleIfaceText(&b, "oif", v.Oif, v.OifDetached)

	if v.L3mdev {
		b.WriteString(" lookup ")
		b.WriteString(ruleL3mdevTableCst)
	}
	if v.UIDStart != nil {
		b.WriteString(" uidrange ")
		b.WriteString(strconv.FormatUint(uint64(*v.UIDStart), 10))
		b.WriteByte('-')
		b.WriteString(strconv.FormatUint(uint64(*v.UIDEnd), 10))
	}

	writeRulePortText(&b, "sport", v.Sport, v.SportMask, v.SportStart, v.SportEnd)
	writeRulePortText(&b, "dport", v.Dport, v.DportMask, v.DportStart, v.DportEnd)

	if v.TunID != nil {
		b.WriteString(" tun_id ")
		b.WriteString(strconv.FormatUint(*v.TunID, 10))
	}
	if v.Table != "" {
		b.WriteString(" lookup ")
		b.WriteString(v.Table)
		if v.SuppressPrefixlen != nil {
			b.WriteString(" suppress_prefixlength ")
			b.WriteString(strconv.FormatInt(int64(*v.SuppressPrefixlen), 10))
		}
		if v.SuppressIfgroup != nil {
			b.WriteString(" suppress_ifgroup ")
			b.WriteString(*v.SuppressIfgroup)
		}
	}
	if v.FlowTo != "" {
		b.WriteString(" realms ")
		if v.FlowFrom != "" {
			b.WriteString(v.FlowFrom)
			b.WriteByte('/')
		}
		b.WriteString(v.FlowTo)
	}

	writeRuleActionText(&b, v)

	if v.Protocol != "" {
		b.WriteString(" proto ")
		b.WriteString(v.Protocol)
	}
	if v.Flowlabel != "" {
		b.WriteString(" flowlabel ")
		b.WriteString(v.Flowlabel)
		// The mask reaches the terminal only when it is NOT the full 20-bit
		// mask (:591-596).
		if v.FlowlabelMask != hex0x(uint64(ruleFlowlabelMaxMaskCst)) {
			b.WriteByte('/')
			b.WriteString(v.FlowlabelMask)
		}
	}

	b.WriteByte('\n')
	return b.String()
}

// writeRuleIfaceText writes ` iif NAME` or ` oif NAME` with its optional
// `[detached]` suffix. The two blocks (:381-405) differ only in the keyword.
func writeRuleIfaceText(b *strings.Builder, keyword, name string, detached bool) {
	if name == "" {
		return
	}
	b.WriteByte(' ')
	b.WriteString(keyword)
	b.WriteByte(' ')
	b.WriteString(name)
	if detached {
		b.WriteString(" [detached]")
	}
}

// writeRuleActionText is the text half of setRuleAction's four arms, kept
// beside it in the same order so the two cannot drift apart.
func writeRuleActionText(b *strings.Builder, v RuleView) {
	switch {
	case v.NatGateway != "":
		b.WriteString(" map-to ")
		b.WriteString(v.NatGateway)
	case v.Masquerade:
		b.WriteString(" masquerade")
	case v.Goto != nil:
		b.WriteString(" goto ")
		b.WriteString(ruleGotoText(v.Goto))
		if v.Unresolved {
			b.WriteString(" [unresolved]")
		}
	case v.Nop:
		b.WriteString(" nop")
	case v.Action != "":
		b.WriteByte(' ')
		b.WriteString(v.Action)
	}
}

// ruleGotoText renders the Goto field's two possible dynamic types. It is a
// helper rather than a fmt.Sprint because a third type reaching this field
// should be a visible wrong answer in a test, not a plausible one.
func ruleGotoText(g any) string {
	switch t := g.(type) {
	case uint32:
		return strconv.FormatUint(uint64(t), 10)
	case string:
		return t
	}
	return ""
}

// writeRulePortText writes one of the two port blocks (:412-441 and :443-472).
//
// The single-value arm has three sub-arms and they differ in BASE as well as
// in token count: a narrowed mask prints the port itself in hex, where the
// unmasked form prints it in decimal. That is not a formatting nicety — the
// same port renders as `80` or as `0x50` depending on an attribute that is not
// the port.
func writeRulePortText(b *strings.Builder, keyword string, single, mask, start, end *uint16) {
	switch {
	case single != nil:
		b.WriteByte(' ')
		b.WriteString(keyword)
		b.WriteByte(' ')
		if mask != nil && *mask != rulePortMaxMaskCst {
			b.WriteString(hex0x(uint64(*single)))
			b.WriteByte('/')
			b.WriteString(hex0x(uint64(*mask)))
			return
		}
		b.WriteString(strconv.FormatUint(uint64(*single), 10))
	case start != nil:
		b.WriteByte(' ')
		b.WriteString(keyword)
		b.WriteByte(' ')
		b.WriteString(strconv.FormatUint(uint64(*start), 10))
		b.WriteByte('-')
		b.WriteString(strconv.FormatUint(uint64(*end), 10))
	}
}

// ruleKV is one key of the JSON object, in print position.
type ruleKV struct {
	key string
	// val is marshaled normally. When null is true it is ignored and the
	// value written is JSON null, which is what print_null emits — a
	// different function from print_bool, whose value would be true.
	val  any
	null bool
}

// MarshalJSON writes the keys in print_rule's emission order.
//
// This object is built key by key rather than by struct tags, which is a
// departure from LinkView and AddrView, and the reason is that four of
// print_rule's keys are print_null tokens interleaved WITH tagged fields —
// `not` between priority and src, `iif_detached` after iif, `l3mdev` after
// oif, and three of the four action keys after the realms. NeighView gets away
// with appending its null keys at the end because its happen to print last.
// Here they do not, and Go marshals struct fields in declaration order with no
// way to interleave, so a tagged struct plus a splice would need a positional
// anchor for every one of them — more machinery, and less readable, than
// simply listing the keys in the order the C emits them.
func (v RuleView) MarshalJSON() ([]byte, error) {
	kvs := []ruleKV{{key: "priority", val: v.Priority}}

	if v.Not {
		kvs = append(kvs, ruleKV{key: "not", null: true})
	}
	kvs = append(kvs, ruleKV{key: "src", val: v.Src})
	kvs = appendRuleKV(kvs, "srclen", v.SrcLen)
	if v.Dst != "" {
		kvs = append(kvs, ruleKV{key: "dst", val: v.Dst})
	}
	kvs = appendRuleKV(kvs, "dstlen", v.DstLen)
	kvs = appendRuleStr(kvs, "tos", v.Tos)
	kvs = appendRuleStr(kvs, "fwmark", v.Fwmark)
	kvs = appendRuleStr(kvs, "fwmask", v.Fwmask)

	kvs = appendRuleStr(kvs, "iif", v.Iif)
	if v.IifDetached {
		kvs = append(kvs, ruleKV{key: "iif_detached", null: true})
	}
	kvs = appendRuleStr(kvs, "oif", v.Oif)
	if v.OifDetached {
		kvs = append(kvs, ruleKV{key: "oif_detached", null: true})
	}
	if v.L3mdev {
		kvs = append(kvs, ruleKV{key: "l3mdev", null: true})
	}

	kvs = appendRuleKV(kvs, "uid_start", v.UIDStart)
	kvs = appendRuleKV(kvs, "uid_end", v.UIDEnd)
	kvs = appendRulePortKV(kvs, "sport", v.Sport, v.SportMask, v.SportStart, v.SportEnd)
	kvs = appendRulePortKV(kvs, "dport", v.Dport, v.DportMask, v.DportStart, v.DportEnd)
	kvs = appendRuleKV(kvs, "tun_id", v.TunID)

	kvs = appendRuleStr(kvs, "table", v.Table)
	kvs = appendRuleKV(kvs, "suppress_prefixlen", v.SuppressPrefixlen)
	kvs = appendRuleKV(kvs, "suppress_ifgroup", v.SuppressIfgroup)
	kvs = appendRuleStr(kvs, "flow_from", v.FlowFrom)
	kvs = appendRuleStr(kvs, "flow_to", v.FlowTo)

	kvs = appendRuleActionKV(kvs, v)
	kvs = appendRuleStr(kvs, "protocol", v.Protocol)
	kvs = appendRuleStr(kvs, "flowlabel", v.Flowlabel)
	kvs = appendRuleStr(kvs, "flowlabel_mask", v.FlowlabelMask)

	return marshalRuleKVs(kvs)
}

// appendRuleActionKV is the JSON half of setRuleAction, and the shape differs
// from the text half: three of the four arms emit a print_null KEY, while the
// catch-all emits "action" with the type name as its VALUE. So `nop` and
// `blackhole` — which read like two spellings of one thing — are a key and a
// value respectively.
func appendRuleActionKV(kvs []ruleKV, v RuleView) []ruleKV {
	switch {
	case v.NatGateway != "":
		kvs = append(kvs, ruleKV{key: "nat_gateway", val: v.NatGateway})
	case v.Masquerade:
		kvs = append(kvs, ruleKV{key: "masquerade", null: true})
	case v.Goto != nil:
		kvs = append(kvs, ruleKV{key: "goto", val: v.Goto})
		if v.Unresolved {
			kvs = append(kvs, ruleKV{key: "unresolved", null: true})
		}
	case v.Nop:
		kvs = append(kvs, ruleKV{key: "nop", null: true})
	case v.Action != "":
		kvs = append(kvs, ruleKV{key: "action", val: v.Action})
	}
	return kvs
}

// appendRulePortKV mirrors writeRulePortText's arms. The mask key is emitted
// whenever the attribute was present, INCLUDING when it equals PORT_MAX_MASK
// and the text therefore shows no mask — print_0xhex with PRINT_JSON runs
// before the PRINT_FP branch chooses (:416-431).
func appendRulePortKV(kvs []ruleKV, keyword string, single, mask, start, end *uint16) []ruleKV {
	switch {
	case single != nil:
		kvs = append(kvs, ruleKV{key: keyword, val: *single})
		if mask != nil {
			kvs = append(kvs, ruleKV{key: keyword + "_mask", val: hex0x(uint64(*mask))})
		}
	case start != nil:
		kvs = append(kvs, ruleKV{key: keyword + "_start", val: *start})
		kvs = append(kvs, ruleKV{key: keyword + "_end", val: *end})
	}
	return kvs
}

// appendRuleStr appends a key only for a non-empty string, which is the
// omitempty every optional print_string here would have had.
func appendRuleStr(kvs []ruleKV, key, val string) []ruleKV {
	if val == "" {
		return kvs
	}
	return append(kvs, ruleKV{key: key, val: val})
}

// appendRuleKV appends a key only for a non-nil pointer. It is generic so the
// six pointer widths in RuleView do not need six copies, and it takes the
// pointer rather than the value because nil is the absence this whole view is
// built around.
func appendRuleKV[T any](kvs []ruleKV, key string, p *T) []ruleKV {
	if p == nil {
		return kvs
	}
	return append(kvs, ruleKV{key: key, val: *p})
}

// marshalRuleKVs writes the ordered pairs as a JSON object.
func marshalRuleKVs(kvs []ruleKV) ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, kv := range kvs {
		if i > 0 {
			b.WriteByte(',')
		}
		key, err := json.Marshal(kv.key)
		if err != nil {
			return nil, err
		}
		b.Write(key)
		b.WriteByte(':')
		if kv.null {
			b.WriteString("null")
			continue
		}
		val, err := json.Marshal(kv.val)
		if err != nil {
			return nil, err
		}
		b.Write(val)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// ruleU8Ptr, ruleU16Ptr and ruleU32Ptr exist for the reason link_detail.go's
// u32Ptr does — a token whose absence and whose zero are different states
// needs a pointer, and Go has no address-of for a literal — but they cannot
// reuse it, because that one takes an xtcpnl.U32Attr rather than a number.
func ruleU8Ptr(v uint8) *uint8    { return &v }
func ruleU16Ptr(v uint16) *uint16 { return &v }
func ruleU32Ptr(v uint32) *uint32 { return &v }

// writeSlashU8 writes the `/%u` suffix print_rule appends to a prefix whose
// length is not the family's host length.
func writeSlashU8(b *strings.Builder, n *uint8) {
	if n == nil {
		return
	}
	b.WriteByte('/')
	b.WriteString(strconv.FormatUint(uint64(*n), 10))
}

// hex0x is print_0xhex's `%#llx`: lower-case hex with an 0x prefix and no
// padding. C's `%#llx` of zero is "0" with NO prefix, and Go's %#x agrees;
// this is written out rather than delegated to fmt so that agreement is
// visible at the one place it matters.
func hex0x(v uint64) string {
	if v == 0 {
		return "0"
	}
	return "0x" + strconv.FormatUint(v, 16)
}

// realmName is rtnl_rtrealm_n2a (lib/rt_names.c), whose table is loaded from
// /etc/iproute2/rt_realms. goip reads no config files — see the package doc —
// so this is always the `%d` fallback, the position dsfieldName already takes.
func realmName(id uint32) string {
	return strconv.FormatUint(uint64(id), 10)
}
