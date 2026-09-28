package nlparity

// The differ: three tiers that cannot mask each other, and a locus a human can
// act on.
//
// # Why three independent levels rather than one deep compare
//
// A single recursive equality check answers "are these the same?" and nothing
// else. The questions a coverage gate actually needs answered are different in
// kind, and each has a distinct failure mode:
//
//	L1  Transaction COUNT, positional. The highest-value assertion in the
//	    package. It is what catches "goip forgot the RTM_GETLINK dump before
//	    RTM_GETADDR" and "goip does, or does not, do ll_link_get" — both of
//	    which a per-message compare would report as a pile of mismatches with
//	    no statement of what went wrong.
//	L2  REQUESTS, positional, full equality including values. A request is
//	    100% tool-controlled and byte-deterministic modulo seq and pid, so
//	    anything at all is a finding. This is the tier that proves goip asks
//	    the same question.
//	L3  REPLIES, paired by object key rather than by position. Position is
//	    meaningless here: the kernel walks its own hash tables, so two runs can
//	    legitimately order the same links differently. What must hold is
//	    key-set equality, key-SEQUENCE equality, and per-pair ordered
//	    attribute-type equality.
//
// They are separate findings so that one cannot hide another. If L1 fails, L2
// and L3 still run over the transactions that do line up — a truncated run
// should say "one transaction short, and the ones present match", not collapse
// into noise.
//
// # Locus and diagnostics are deliberately different things
//
// The locus is the allowlist key (see goip-parity-allowlist.json). It therefore
// has to be STABLE across re-captures and reboots, and it has to STOP MATCHING
// when a divergence moves. Those two pull against each other, and the split
// below is how they are reconciled:
//
//   - The locus names the asserted quantity: side, message type, attribute,
//     and for a request the role that distinguishes two call sites of the same
//     type. Nothing else.
//   - The transaction index and the object key go in Divergence.Txn and
//     Divergence.Object, which String() prints and Key() ignores.
//
// Concretely: an ifindex is assigned at boot, so a locus containing
// `ifindex=3` would silently stop matching after a reboot — which looks like
// the allowlist working ("a divergence that moves stops being allowlisted")
// while actually being an entry that evaporated for an unrelated reason. Per
// attribute is the right granularity, because "IFLA_MTU values are noisy" is a
// claim about the attribute, not about one device.
//
// # The request role, and why it is derived rather than labeled
//
// The committed allowlist distinguishes two RTM_GETLINK request loci:
// iproute2's ll_init_map (lib/ll_map.c:394) issues a DUMP, and ll_link_get
// (:276) issues a single-get, and the two carry different ext-mask
// expectations across releases — and different values at the pin, 0x01 against
// 0x09. Both line numbers are in the PINNED tree, which is what the fixtures
// were captured against; at iproute2's tip :394 is a blank line.
//
// A comparator cannot know a C function name — but it can see NLM_F_DUMP, and
// that is exactly the distinction. So the role renders as `dump` or `get`,
// derived from the request's own flags, and the iproute2 call site stays in
// the entry's `reason` where prose belongs.
//
// # Ordering, once more
//
// Diff normalizes; it does not segment. The caller passes two Segmentations,
// which means attribution has already happened and seq/pid may safely be
// zeroed. Normalizing first would destroy the evidence — see the header of
// nlparity_segment.go.

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// ErrNoTransactions reports a segmentation with nothing to compare.
//
// This is not pedantry. pkg/xtcpnl/testdata/.../netlink_route_getlink_dump.pcap
// is the extracted reply stream with the request discarded, so it segments into
// twelve orphans and zero transactions — and a differ handed two of those would
// compare nothing and report clean. Refusing an empty segmentation is what
// stops "no transactions" from reading as "no divergences".
var ErrNoTransactions = errors.New("nlparity: segmentation holds no transactions, so there is nothing to compare")

// Level is which tier produced a finding.
type Level uint8

// The three tiers, numbered as the plan numbers them so a report and the design
// document use the same words.
const (
	// LevelTxnCount is L1: how many transactions each side ran.
	LevelTxnCount Level = iota + 1
	// LevelRequest is L2: the request bytes, positionally.
	LevelRequest
	// LevelReply is L3: the replies, paired by object key.
	LevelReply
	// LevelHygiene is the capture-quality tier: notifications and orphans,
	// which are facts about the capture rather than about either tool.
	LevelHygiene
)

// String names the level.
func (l Level) String() string {
	switch l {
	case LevelTxnCount:
		return "L1"
	case LevelRequest:
		return "L2"
	case LevelReply:
		return "L3"
	case LevelHygiene:
		return "hygiene"
	default:
		return fmt.Sprintf("Level(%d)", uint8(l))
	}
}

// Divergence is one finding.
//
// Ref is the reference side (the pinned `ip`) and Sub the subject (goip). The
// names are not A and B because which side is which decides whether a finding
// reads "goip omitted it" or "goip invented it", and a report that got them
// backwards would send someone to fix the wrong tool.
type Divergence struct {
	// Command is the argv both tools were driven with.
	Command string
	// Level is the tier.
	Level Level
	// Class is what kind of difference it is, and therefore whether it may
	// ever be allowlisted. See DivergenceClass.Suppressible.
	Class DivergenceClass
	// Locus is the allowlist key's second half. Stable by construction; see
	// the header.
	Locus string
	// Txn is the transaction index the finding was found in, or -1 for a
	// finding about the capture as a whole. Diagnostic only: NOT in Key.
	Txn int
	// Object is the reply object key, for an L3 finding. Diagnostic only.
	Object string
	// Ref and Sub are the two sides as rendered text, empty where the finding
	// is an absence.
	Ref, Sub string
}

// Key is the finding's identity, used both for allowlist lookup and for the
// D_control subtraction. It excludes Txn and Object on purpose — see the
// header — and excludes Ref and Sub necessarily, since a control diff's values
// differ from a test diff's by definition and a key that included them would
// never match.
func (d Divergence) Key() string {
	return fmt.Sprintf("%s|%s|%s", d.Level, d.Class, d.Locus)
}

// String renders a report line: the key, then the diagnostics that locate it,
// then the two sides.
func (d Divergence) String() string {
	var b strings.Builder
	// Level and Class are both printed, except where they are the same word.
	// That happens for exactly one pair — LevelHygiene with DivergenceHygiene —
	// and rendering it produced "hygiene hygiene hygiene:goip:notifications",
	// because the locus is prefixed with its level too. Key() is unchanged and
	// still carries all three, since it is the persisted identity and shortening
	// it would repoint every hygiene key.
	if lvl, cls := d.Level.String(), d.Class.String(); lvl == cls {
		fmt.Fprintf(&b, "%s %s", lvl, d.Locus)
	} else {
		fmt.Fprintf(&b, "%s %s %s", lvl, cls, d.Locus)
	}
	if d.Txn >= 0 {
		fmt.Fprintf(&b, " txn[%d]", d.Txn)
	}
	if d.Object != "" {
		fmt.Fprintf(&b, " %s", d.Object)
	}
	switch {
	case d.Ref != "" && d.Sub != "":
		fmt.Fprintf(&b, ": ip=%s goip=%s", d.Ref, d.Sub)
	case d.Ref != "":
		fmt.Fprintf(&b, ": ip=%s goip=<absent>", d.Ref)
	case d.Sub != "":
		fmt.Fprintf(&b, ": ip=<absent> goip=%s", d.Sub)
	}
	return b.String()
}

// CheckUsable reports whether a segmentation can be compared at all.
func CheckUsable(s Segmentation) error {
	if len(s.Txns) == 0 {
		return fmt.Errorf("%w (%d notifications, %d orphans)",
			ErrNoTransactions, len(s.Notifications), len(s.Orphans))
	}
	return nil
}

// Diff compares two segmentations of the same command and returns the findings,
// most structural first: L1, then L2, then L3.
//
// It normalizes internally, so the caller hands over raw segmentations. It does
// NOT consult the allowlist and does not subtract a control diff; those are
// Compare's job, kept separate so a control diff is produced by the same code
// path as a test diff.
func Diff(command string, ref, sub Segmentation) []Divergence {
	var out []Divergence

	// L1 first, and unconditionally: the count is the finding, and the tiers
	// below still run over the transactions that do line up.
	if len(ref.Txns) != len(sub.Txns) {
		out = append(out, Divergence{
			Command: command,
			Level:   LevelTxnCount,
			Class:   DivergenceTransactionCount,
			Locus:   "txn-count",
			Txn:     -1,
			Ref:     fmt.Sprintf("%d", len(ref.Txns)),
			Sub:     fmt.Sprintf("%d", len(sub.Txns)),
		})
	}

	n := min(len(ref.Txns), len(sub.Txns))
	for i := range n {
		out = append(out, diffRequest(command, i, ref.Txns[i].Request, sub.Txns[i].Request)...)
		out = append(out, diffReplies(command, i, ref.Txns[i], sub.Txns[i])...)
	}

	return out
}

// HygieneDiff reports the capture-quality facts of ONE segmentation.
//
// It is not a comparison: a notification or an orphan is a statement about the
// capture window, not about either tool, so there is no ref-versus-sub to
// render. The plan is explicit that these must be reported and must fail rather
// than be dropped — a comparator that silently discards what it cannot
// attribute reports green on a capture it did not understand.
func HygieneDiff(command, side string, s Segmentation) []Divergence {
	var out []Divergence
	add := func(locus, detail string) {
		out = append(out, Divergence{
			Command: command,
			Level:   LevelHygiene,
			Class:   DivergenceHygiene,
			Locus:   "hygiene:" + side + ":" + locus,
			Txn:     -1,
			Sub:     detail,
		})
	}

	if len(s.Notifications) != 0 {
		add("notifications", fmt.Sprintf("%d multicast notifications in the capture window",
			len(s.Notifications)))
	}
	if len(s.Orphans) != 0 {
		add("orphans", fmt.Sprintf("%d replies with no open transaction", len(s.Orphans)))
	}
	for i := range s.Txns {
		if s.Txns[i].Ambiguous {
			add("ambiguous", fmt.Sprintf("transaction %d could not be attributed unambiguously", i))
			break
		}
	}
	for i := range s.Txns {
		if !s.Txns[i].Closed() {
			add("open", fmt.Sprintf("transaction %d has no terminator", i))
			break
		}
	}
	// There is deliberately NO "more than one port id answered" arm, and that
	// is a correction the corpus forced rather than a simplification.
	//
	// The plan listed it as a hygiene failure on the reasoning that a second
	// port id means the capture caught a second tool. Measured, it means
	// nothing of the kind: iproute2's ll_link_get calls rtnl_open(&rth, 0) on
	// every invocation (lib/ll_map.c), so ONE `ip` process answers on as many
	// port ids as it resolves names. Eight of the eighteen committed guest
	// captures have more than one, in the CLEAN namespace, with zero orphans
	// and zero notifications:
	//
	//	netlink_route_getlink_dev.pcap             2 pids, 2 txns
	//	netlink_route_getroute.pcap                2 pids, 2 txns
	//	netlink_route_getroute_table_all.pcap      3 pids, 3 txns
	//	mesh/netlink_route_getlink_dev.pcap        4 pids, 4 txns
	//
	// Since Failed() treats hygiene as fatal regardless of gating — correctly,
	// because a capture nobody understood cannot be staged around — keeping
	// the arm would make `route show` and `link show dev` permanently red, and
	// unfixably so by design: DivergenceHygiene is unsuppressible, so no
	// allowlist entry could ever clear it.
	//
	// What the plan actually wanted is still caught, and by the arms above. A
	// foreign tool's replies have no request in the window, so they land in
	// Orphans; its notifications land in Notifications; and if both halves of
	// its exchange were captured, the extra transaction is an L1 count
	// finding, which is also unsuppressible. The port-id count survives as
	// Report.RefPids/SubPids, which is a sentinel like ControlSize: reported
	// on every run, never a failure on its own.
	return out
}

// requestRole distinguishes the two call sites of one request type by the only
// thing on the wire that separates them: NLM_F_DUMP. See the header.
func requestRole(m Msg) string {
	if m.Hdr.Flags&uint16(unix.NLM_F_DUMP) == uint16(unix.NLM_F_DUMP) {
		return "dump"
	}
	return "get"
}

// diffRequest is L2: positional, full equality.
func diffRequest(command string, txn int, ref, sub Msg) []Divergence {
	role := requestRole(ref)
	base := "request:" + MsgTypeName(ref.Hdr.Type)
	var out []Divergence

	// A different message type at the same position is not a value: the
	// request that should be there is absent. Reported as presence, which is
	// unsuppressible, and the tiers below are skipped because comparing an
	// RTM_GETADDR's body against an RTM_GETLINK's would produce a second pile
	// of findings that all say the same thing.
	if ref.Hdr.Type != sub.Hdr.Type {
		return append(out, Divergence{
			Command: command, Level: LevelRequest, Class: DivergencePresence,
			Locus: base + ":" + role, Txn: txn,
			Ref: MsgTypeName(ref.Hdr.Type), Sub: MsgTypeName(sub.Hdr.Type),
		})
	}

	if ref.Hdr.Flags != sub.Hdr.Flags {
		out = append(out, Divergence{
			Command: command, Level: LevelRequest, Class: DivergenceValue,
			Locus: base + ":header.flags:" + role, Txn: txn,
			Ref: fmt.Sprintf("0x%04x", ref.Hdr.Flags),
			Sub: fmt.Sprintf("0x%04x", sub.Hdr.Flags),
		})
	}

	refHdr, refAttrs, refRem := DecodeAttrs(ref.Hdr.Type, ref.Body)
	subHdr, subAttrs, subRem := DecodeAttrs(sub.Hdr.Type, sub.Body)

	if !bytes.Equal(refHdr, subHdr) {
		out = append(out, Divergence{
			Command: command, Level: LevelRequest, Class: DivergenceValue,
			Locus: base + ":familyhdr:" + role, Txn: txn,
			Ref: hex.EncodeToString(refHdr), Sub: hex.EncodeToString(subHdr),
		})
	}

	// The remainder is the zeroed oversend tail inside the message body, which
	// is different from Datagram.TailBytes: this is a body too short for its
	// own attributes. Informational on the datagram, a finding here.
	if len(refRem) != len(subRem) {
		out = append(out, Divergence{
			Command: command, Level: LevelRequest, Class: DivergenceValue,
			Locus: base + ":body-remainder:" + role, Txn: txn,
			Ref: fmt.Sprintf("%d bytes", len(refRem)),
			Sub: fmt.Sprintf("%d bytes", len(subRem)),
		})
	}

	out = append(out, diffAttrs(command, LevelRequest, txn, "", base, ":"+role,
		ref.Hdr.Type, refAttrs, subAttrs)...)
	return out
}

// diffReplies is L3: paired by object key, with membership, sequence and
// per-pair attribute order as three separate findings.
func diffReplies(command string, txn int, ref, sub Txn) []Divergence {
	var out []Divergence

	// Ambiguous attribution means the reply side is a guess, so it is excluded
	// from comparison entirely — the plan's rule, and the reason Txn.Ambiguous
	// exists rather than the reply simply being dropped. HygieneDiff has
	// already reported the ambiguity, so this is not a silent skip.
	if ref.Ambiguous || sub.Ambiguous {
		return nil
	}

	refKeys, refByKey := keyReplies(ref.Replies)
	subKeys, subByKey := keyReplies(sub.Replies)

	// Membership, both directions. A key on one side only is the finding that
	// must never be suppressible: it is how "goip skipped a link" looks.
	for _, k := range refKeys {
		if _, ok := subByKey[k]; !ok {
			out = append(out, Divergence{
				Command: command, Level: LevelReply, Class: DivergenceKeySet,
				Locus: "reply:key-set", Txn: txn, Object: k, Ref: k,
			})
		}
	}
	for _, k := range subKeys {
		if _, ok := refByKey[k]; !ok {
			out = append(out, Divergence{
				Command: command, Level: LevelReply, Class: DivergenceKeySet,
				Locus: "reply:key-set", Txn: txn, Object: k, Sub: k,
			})
		}
	}

	// Sequence, only once membership matches. Comparing the order of two
	// different sets would report an ordering problem that is really a
	// membership problem, which is the masking this tier is split to prevent.
	if sameStrings(refKeys, subKeys) {
		if !equalStrings(refKeys, subKeys) {
			out = append(out, Divergence{
				Command: command, Level: LevelReply, Class: DivergenceKeyOrder,
				Locus: "reply:key-order", Txn: txn,
				Ref: strings.Join(refKeys, ","), Sub: strings.Join(subKeys, ","),
			})
		}
	}

	// Per pair. Only keys present on both sides, and only where the two sides
	// hold the same number of messages for that key: a count mismatch is a
	// membership finding, already reported by keyReplies' duplicate keys.
	for _, k := range refKeys {
		rs, ss := refByKey[k], subByKey[k]
		if len(rs) == 0 || len(ss) == 0 || len(rs) != len(ss) {
			continue
		}
		for i := range rs {
			out = append(out, diffReplyPair(command, txn, k, rs[i], ss[i])...)
		}
	}

	return out
}

// diffReplyPair compares two replies already known to describe the same object.
func diffReplyPair(command string, txn int, object string, ref, sub Msg) []Divergence {
	rn, sn := NormalizeMsg(ref), NormalizeMsg(sub)
	base := "reply:" + MsgTypeName(rn.Hdr.Type)
	var out []Divergence

	if rn.Hdr.Flags != sn.Hdr.Flags {
		out = append(out, Divergence{
			Command: command, Level: LevelReply, Class: DivergenceValue,
			Locus: base + ":header.flags", Txn: txn, Object: object,
			Ref: fmt.Sprintf("0x%04x", rn.Hdr.Flags),
			Sub: fmt.Sprintf("0x%04x", sn.Hdr.Flags),
		})
	}

	refHdr, refAttrs, _ := DecodeAttrs(rn.Hdr.Type, rn.Body)
	subHdr, subAttrs, _ := DecodeAttrs(sn.Hdr.Type, sn.Body)

	if !bytes.Equal(refHdr, subHdr) {
		out = append(out, Divergence{
			Command: command, Level: LevelReply, Class: DivergenceValue,
			Locus: base + ":familyhdr", Txn: txn, Object: object,
			Ref: hex.EncodeToString(refHdr), Sub: hex.EncodeToString(subHdr),
		})
	}

	out = append(out, diffAttrs(command, LevelReply, txn, object, base, "",
		rn.Hdr.Type, refAttrs, subAttrs)...)
	return out
}

// diffAttrs compares two attribute streams: presence both ways, then order,
// then values.
//
// The type used throughout is Attr.Type UNMASKED, so a missing NLA_F_NESTED
// surfaces as one attribute absent and another extra rather than as nothing at
// all. AttrName renders the flag into the locus to match.
func diffAttrs(command string, level Level, txn int, object, base, suffix string,
	msgType uint16, ref, sub []Attr,
) []Divergence {
	var out []Divergence

	refIdx := indexAttrs(ref)
	subIdx := indexAttrs(sub)

	for _, a := range ref {
		if _, ok := subIdx[a.Type]; !ok {
			out = append(out, Divergence{
				Command: command, Level: level, Class: DivergencePresence,
				Locus: base + ":" + AttrName(msgType, a.Type) + suffix,
				Txn:   txn, Object: object,
				Ref: fmt.Sprintf("%d bytes", len(a.Val)),
			})
		}
	}
	for _, a := range sub {
		if _, ok := refIdx[a.Type]; !ok {
			out = append(out, Divergence{
				Command: command, Level: level, Class: DivergencePresence,
				Locus: base + ":" + AttrName(msgType, a.Type) + suffix,
				Txn:   txn, Object: object,
				Sub: fmt.Sprintf("%d bytes", len(a.Val)),
			})
		}
	}

	refTypes, subTypes := attrTypeList(msgType, ref), attrTypeList(msgType, sub)
	if sameStrings(refTypes, subTypes) && !equalStrings(refTypes, subTypes) {
		out = append(out, Divergence{
			Command: command, Level: level, Class: DivergenceAttrOrder,
			Locus: base + ":attr-order" + suffix, Txn: txn, Object: object,
			Ref: strings.Join(refTypes, ","), Sub: strings.Join(subTypes, ","),
		})
	}

	// Values last, and only for a type present exactly once on each side. A
	// repeated type — IFLA_ALT_IFNAME inside IFLA_PROP_LIST is the real case —
	// has no unambiguous pairing, and guessing one would produce a value
	// finding that is really an ordering question.
	for _, a := range ref {
		rs, rok := refIdx[a.Type]
		ss, sok := subIdx[a.Type]
		if !rok || !sok || len(rs) != 1 || len(ss) != 1 {
			continue
		}
		if bytes.Equal(rs[0], ss[0]) {
			continue
		}

		// IFLA_AF_SPEC is descended into rather than reported as one blob,
		// because a locus has to be narrow enough to allowlist. Normalization
		// deliberately leaves IFLA_INET6_CACHEINFO's reachable_time compared —
		// the kernel recomputes it every few minutes, so two adjacent captures
		// can straddle a recompute, and the intent is that it surfaces as a
		// volatile-fallback entry rather than being silently blanked. An entry
		// at `reply:RTM_NEWLINK:IFLA_AF_SPEC` would suppress every value
		// difference anywhere under AF_SPEC to get it, which is exactly the
		// over-broad forgetting this allowlist is built to prevent.
		if AttrFamilyOf(msgType) == AttrFamilyIFLA &&
			a.BareType() == uint16(unix.IFLA_AF_SPEC) {
			out = append(out, diffAFSpec(command, level, txn, object,
				base+":"+AttrName(msgType, a.Type), suffix, rs[0], ss[0])...)
			continue
		}

		out = append(out, Divergence{
			Command: command, Level: level, Class: DivergenceValue,
			Locus: base + ":" + AttrName(msgType, a.Type) + suffix,
			Txn:   txn, Object: object,
			Ref: hex.EncodeToString(rs[0]), Sub: hex.EncodeToString(ss[0]),
		})
	}

	return out
}

// diffAFSpec compares two IFLA_AF_SPEC payloads one address family at a time,
// and one attribute at a time within each.
//
// Two levels is all it goes, which is the depth normalization reaches and the
// depth the corpus justifies: IFLA_AF_SPEC -> the AF_* member -> the per-family
// attribute. Below that a difference is still reported, as a value on the
// innermost named attribute, because inventing names for a third level would be
// claiming knowledge this package does not have.
//
// The kernel sets NLA_F_NESTED on none of these, so BareType drives the walk;
// see SubAttrs. Only AF_INET6's members get real names, because it is the only
// nest with a table — everything else renders as nested:n, which is a stable
// locus even though it is not a readable one.
func diffAFSpec(command string, level Level, txn int, object, base, suffix string,
	ref, sub []byte,
) []Divergence {
	var out []Divergence

	refFams, _ := SubAttrs(ref)
	subFams, _ := SubAttrs(sub)
	refByFam := indexAttrs(refFams)
	subByFam := indexAttrs(subFams)

	locus := func(parts ...string) string {
		return base + ":" + strings.Join(parts, ":") + suffix
	}

	// A whole address family present on one side only. Presence, so
	// unsuppressible: a goip that dropped the AF_INET6 member entirely is not
	// a noisy value.
	for _, f := range refFams {
		if _, ok := subByFam[f.Type]; !ok {
			out = append(out, Divergence{
				Command: command, Level: level, Class: DivergencePresence,
				Locus: locus(afName(f.BareType())), Txn: txn, Object: object,
				Ref: fmt.Sprintf("%d bytes", len(f.Val)),
			})
		}
	}
	for _, f := range subFams {
		if _, ok := refByFam[f.Type]; !ok {
			out = append(out, Divergence{
				Command: command, Level: level, Class: DivergencePresence,
				Locus: locus(afName(f.BareType())), Txn: txn, Object: object,
				Sub: fmt.Sprintf("%d bytes", len(f.Val)),
			})
		}
	}

	for _, f := range refFams {
		rs, ss := refByFam[f.Type], subByFam[f.Type]
		if len(rs) != 1 || len(ss) != 1 || bytes.Equal(rs[0], ss[0]) {
			continue
		}

		fam := afName(f.BareType())
		refIn, _ := SubAttrs(rs[0])
		subIn, _ := SubAttrs(ss[0])
		refInIdx := indexAttrs(refIn)
		subInIdx := indexAttrs(subIn)

		// A family member that is not an attribute stream at all — AF_INET's
		// IFLA_INET_CONF is a bare ipv4_devconf array — decodes to nothing,
		// and reporting it whole is the honest answer.
		if len(refIn) == 0 && len(subIn) == 0 {
			out = append(out, Divergence{
				Command: command, Level: level, Class: DivergenceValue,
				Locus: locus(fam), Txn: txn, Object: object,
				Ref: hex.EncodeToString(rs[0]), Sub: hex.EncodeToString(ss[0]),
			})
			continue
		}

		for _, in := range refIn {
			if _, ok := subInIdx[in.Type]; !ok {
				out = append(out, Divergence{
					Command: command, Level: level, Class: DivergencePresence,
					Locus: locus(fam, nestName(f.BareType(), in.Type)), Txn: txn, Object: object,
					Ref: fmt.Sprintf("%d bytes", len(in.Val)),
				})
			}
		}
		for _, in := range subIn {
			if _, ok := refInIdx[in.Type]; !ok {
				out = append(out, Divergence{
					Command: command, Level: level, Class: DivergencePresence,
					Locus: locus(fam, nestName(f.BareType(), in.Type)), Txn: txn, Object: object,
					Sub: fmt.Sprintf("%d bytes", len(in.Val)),
				})
			}
		}
		for _, in := range refIn {
			rv, rok := refInIdx[in.Type]
			sv, sok := subInIdx[in.Type]
			if !rok || !sok || len(rv) != 1 || len(sv) != 1 || bytes.Equal(rv[0], sv[0]) {
				continue
			}
			out = append(out, Divergence{
				Command: command, Level: level, Class: DivergenceValue,
				Locus: locus(fam, nestName(f.BareType(), in.Type)), Txn: txn, Object: object,
				Ref: hex.EncodeToString(rv[0]), Sub: hex.EncodeToString(sv[0]),
			})
		}
	}

	return out
}

// afName names an IFLA_AF_SPEC member. Only the two families the corpus carries
// are named; anything else keeps its number, which is a stable locus.
func afName(family uint16) string {
	switch family {
	case unix.AF_INET:
		return "AF_INET"
	case unix.AF_INET6:
		return "AF_INET6"
	default:
		return fmt.Sprintf("af(%d)", family)
	}
}

// nestName names an attribute inside one AF_SPEC member.
func nestName(family, attrType uint16) string {
	if family == unix.AF_INET6 {
		return SubAttrName(inet6Names, attrType)
	}
	return SubAttrName(nil, attrType)
}

// indexAttrs groups values by unmasked type, preserving order within a type.
func indexAttrs(attrs []Attr) map[uint16][][]byte {
	out := make(map[uint16][][]byte, len(attrs))
	for _, a := range attrs {
		out[a.Type] = append(out[a.Type], a.Val)
	}
	return out
}

// attrTypeList renders the ordered attribute-type list, which is what L3 gates
// on.
func attrTypeList(msgType uint16, attrs []Attr) []string {
	out := make([]string, 0, len(attrs))
	for _, a := range attrs {
		out = append(out, AttrName(msgType, a.Type))
	}
	return out
}

// keyReplies computes each reply's object key, returning the keys in capture
// order (deduplicated, so the order list is comparable) plus the messages
// grouped by key.
func keyReplies(msgs []Msg) ([]string, map[string][]Msg) {
	byKey := make(map[string][]Msg, len(msgs))
	var order []string
	for i := range msgs {
		k := ObjectKey(msgs[i])
		if _, seen := byKey[k]; !seen {
			order = append(order, k)
		}
		byKey[k] = append(byKey[k], msgs[i])
	}
	return order, byKey
}

// ObjectKey identifies what a reply is ABOUT, so two captures can be paired
// without relying on the kernel walking its tables in the same order twice.
//
// The key is built from the family header plus the one or two attributes that
// name the object, per family:
//
//	link   ifi_index
//	addr   ifa_family, ifa_index, ifa_prefixlen, IFA_ADDRESS
//	route  rtm_family, rtm_table, rtm_dst_len, RTA_DST, RTA_OIF, RTA_PRIORITY
//	neigh  ndm_family, ndm_ifindex, NDA_DST
//
// A message with no object — NLMSG_DONE, NLMSG_ERROR — keys on its type alone.
// That is sufficient because a transaction has at most one terminator, and it
// keeps the terminator in the key sequence where an L3 ordering check can see
// whether it arrived last.
//
// The route key includes RTA_PRIORITY because metric is part of a route's
// identity — two routes to the same prefix out the same interface differ only
// by it — and RTA_TABLE is taken from the header's rtm_table, which is the
// 8-bit form. A table id above 255 lives in RTA_TABLE instead, so both are
// read and the attribute wins when present.
func ObjectKey(m Msg) string {
	switch AttrFamilyOf(m.Hdr.Type) {
	case AttrFamilyIFLA:
		var ifi xtcpnl.IfInfomsg
		if _, err := xtcpnl.DeserializeIfInfomsg(m.Body, &ifi); err != nil {
			return shortKey(m)
		}
		return fmt.Sprintf("link/ifindex=%d", ifi.Index)

	case AttrFamilyIFA:
		var ifa xtcpnl.IfAddrmsg
		if _, err := xtcpnl.DeserializeIfAddrmsg(m.Body, &ifa); err != nil {
			return shortKey(m)
		}
		_, attrs, _ := DecodeAttrs(m.Hdr.Type, m.Body)
		addr := attrValue(attrs, uint16(unix.IFA_ADDRESS))
		return fmt.Sprintf("addr/family=%d/index=%d/prefixlen=%d/addr=%s",
			ifa.Family, ifa.Index, ifa.Prefixlen, hex.EncodeToString(addr))

	case AttrFamilyRTA:
		var rtm xtcpnl.RtMsg
		if _, err := xtcpnl.DeserializeRtMsg(m.Body, &rtm); err != nil {
			return shortKey(m)
		}
		_, attrs, _ := DecodeAttrs(m.Hdr.Type, m.Body)
		table := uint32(rtm.Table)
		if v := attrValue(attrs, uint16(unix.RTA_TABLE)); len(v) == 4 {
			table = le32(v)
		}
		return fmt.Sprintf("route/family=%d/table=%d/dstlen=%d/dst=%s/oif=%d/priority=%d",
			rtm.Family, table, rtm.DstLen,
			hex.EncodeToString(attrValue(attrs, uint16(unix.RTA_DST))),
			attrU32(attrs, uint16(unix.RTA_OIF)),
			attrU32(attrs, uint16(unix.RTA_PRIORITY)))

	case AttrFamilyNDA:
		var nd xtcpnl.NdMsg
		if _, err := xtcpnl.DeserializeNdMsg(m.Body, &nd); err != nil {
			return shortKey(m)
		}
		_, attrs, _ := DecodeAttrs(m.Hdr.Type, m.Body)
		return fmt.Sprintf("neigh/family=%d/ifindex=%d/dst=%s",
			nd.Family, nd.Ifindex,
			hex.EncodeToString(attrValue(attrs, uint16(unix.NDA_DST))))

	case AttrFamilyNone:
		return MsgTypeName(m.Hdr.Type)
	}
	return MsgTypeName(m.Hdr.Type)
}

// shortKey is the key for a message whose family header did not fit. It carries
// the body length so two truncated messages of different lengths do not pair,
// which would turn a truncation into a value divergence.
func shortKey(m Msg) string {
	return fmt.Sprintf("%s/short=%d", MsgTypeName(m.Hdr.Type), len(m.Body))
}

// attrValue returns the first value for a bare attribute type, or nil.
func attrValue(attrs []Attr, bare uint16) []byte {
	for _, a := range attrs {
		if a.BareType() == bare {
			return a.Val
		}
	}
	return nil
}

// attrU32 returns a u32 attribute's value, or 0 when absent or the wrong width.
// Zero is a safe default in a key: an absent RTA_PRIORITY means metric 0, which
// is what the kernel means by it too.
func attrU32(attrs []Attr, bare uint16) uint32 {
	if v := attrValue(attrs, bare); len(v) == 4 {
		return le32(v)
	}
	return 0
}

// le32 reads a little-endian u32. Netlink payloads are host order, and every
// target this builds for is little-endian; a big-endian payload carries
// NLA_F_NET_BYTEORDER, which AttrName renders into the locus rather than
// silently reinterpreting.
func le32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

// sameStrings reports whether two slices hold the same multiset.
func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]int, len(a))
	for _, s := range a {
		seen[s]++
	}
	for _, s := range b {
		seen[s]--
		if seen[s] < 0 {
			return false
		}
	}
	return true
}

// equalStrings reports element-wise equality.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Subtract removes from test every finding whose Key appears in control.
//
// This is the `ip -> goip -> ip` control triple's whole mechanism:
// D_control = Diff(ip.a, ip.b) is by construction not attributable to goip, so
// gating on D_test \ D_control self-adapts across kernels and beats a
// hand-maintained volatile list.
//
// It subtracts on Key, which excludes the values — necessarily, since a control
// diff's values differ from a test diff's by definition. And Key includes the
// CLASS, which is what keeps the subtraction honest: a noisy VALUE at a locus
// cannot cancel a missing ATTRIBUTE at the same locus, because the two have
// different keys. That is the structural half of "suppression applies to value
// divergences only"; Allowlist.Suppresses is the other half.
func Subtract(test, control []Divergence) []Divergence {
	if len(control) == 0 {
		return test
	}
	noisy := make(map[string]bool, len(control))
	for _, d := range control {
		noisy[d.Key()] = true
	}
	var out []Divergence
	for _, d := range test {
		if !noisy[d.Key()] {
			out = append(out, d)
		}
	}
	return out
}

// Report is one command's comparison, with every category kept separate so a
// reader can tell a clean run from a run that had everything explained away.
type Report struct {
	// Command is the argv both tools were driven with.
	Command string
	// Gated mirrors Allowlist.IsGated: whether these findings fail a build.
	Gated bool
	// Findings survived control subtraction and allowlisting. Non-empty on a
	// gated command is a failure.
	Findings []Divergence
	// ControlSuppressed were dropped because D_control showed the same
	// key. Reported, not hidden: a large one means distrust the run.
	ControlSuppressed []Divergence
	// AllowSuppressed were dropped by an allowlist entry. Value class only,
	// by construction.
	AllowSuppressed []Divergence
	// Hygiene are the capture-quality findings from both sides. Always
	// findings — never suppressible — and kept out of Findings so that a
	// dirty capture is distinguishable from a wrong goip.
	Hygiene []Divergence
	// RefTxns and SubTxns are the L1 sentinel values, reported whether or not
	// they differ, because "both sides ran 2 transactions" is the statement
	// that makes a clean report believable.
	RefTxns, SubTxns int
	// ControlSize is |D_control|, the plan's noise sentinel.
	ControlSize int
	// RefPids and SubPids are the distinct port ids that answered on each
	// side. A sentinel, not a finding: iproute2 opens a fresh socket per
	// ll_link_get, so a command that resolves names legitimately answers on
	// several. See HygieneDiff for the measurement. They are reported so a
	// reviewer can still see "this capture is of four sockets" next to the
	// findings, which is the information the plan wanted from the check it
	// asked for.
	RefPids, SubPids []uint32
	// Err is set when a side was unusable, in which case nothing was compared.
	Err error
}

// Failed reports whether the comparison should fail a build.
//
// An unusable capture and a hygiene finding fail regardless of gating: both
// mean the run did not measure what it claimed to, and letting an ungated
// command hide that would make the gate-one-at-a-time discipline a way to
// ignore broken captures rather than a way to stage work.
func (r Report) Failed() bool {
	if r.Err != nil || len(r.Hygiene) != 0 {
		return true
	}
	return r.Gated && len(r.Findings) != 0
}

// Compare is the whole comparison: usability, hygiene, diff, control
// subtraction, allowlisting.
//
// control is D_control, normally Diff(command, ipA, ipB) over the two reference
// captures of the triple; nil when no control was taken, in which case nothing
// is subtracted and the report says so via ControlSize.
//
// al may be nil, which suppresses nothing.
func Compare(command string, ref, sub Segmentation, control []Divergence, al *Allowlist) Report {
	r := Report{
		Command:     command,
		RefTxns:     len(ref.Txns),
		SubTxns:     len(sub.Txns),
		ControlSize: len(control),
		RefPids:     ref.Pids(),
		SubPids:     sub.Pids(),
	}
	if al != nil {
		r.Gated = al.IsGated(command)
	}

	r.Hygiene = append(r.Hygiene, HygieneDiff(command, "ip", ref)...)
	r.Hygiene = append(r.Hygiene, HygieneDiff(command, "goip", sub)...)

	if err := CheckUsable(ref); err != nil {
		r.Err = fmt.Errorf("reference capture: %w", err)
		return r
	}
	if err := CheckUsable(sub); err != nil {
		r.Err = fmt.Errorf("subject capture: %w", err)
		return r
	}

	all := Diff(command, ref, sub)

	kept := Subtract(all, control)
	if len(kept) != len(all) {
		// Recover which ones went, so the report can name them rather than
		// only counting them.
		keptKeys := make(map[string]int, len(kept))
		for _, d := range kept {
			keptKeys[d.Key()]++
		}
		for _, d := range all {
			if keptKeys[d.Key()] > 0 {
				keptKeys[d.Key()]--
				continue
			}
			r.ControlSuppressed = append(r.ControlSuppressed, d)
		}
	}

	for _, d := range kept {
		if al != nil && al.Suppresses(command, d.Locus, d.Class) {
			r.AllowSuppressed = append(r.AllowSuppressed, d)
			continue
		}
		r.Findings = append(r.Findings, d)
	}

	return r
}
