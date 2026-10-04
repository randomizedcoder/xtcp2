package xtcpnl

// This file holds the duplicate-attribute rule the rtnetlink parsers follow.
//
// # Why there is a rule at all
//
// A well-formed netlink message carries each attribute at most once, so a
// duplicate is either a kernel bug or a hostile sender. That still leaves a
// choice, and the two obvious implementations disagree:
//
//   - iproute2's parse_rtattr keeps the FIRST occurrence —
//     `if ((type <= max) && (!tb[type])) tb[type] = rta;`
//     (lib/libnetlink.c:1554).
//   - the kernel's own __nla_parse keeps the LAST, assigning `tb[type] = nla`
//     unconditionally.
//
// pkg/xtcpnl follows iproute2, because the thing it has to agree with is what
// `ip` renders. Writing it as `switch` cases that simply assign would silently
// pick the kernel's rule instead, and a plain "only assign if still zero" guard
// is wrong for a different reason: 0 is a legitimate value for IFLA_TXQLEN
// (docker0 in the committed dump has it) and for IFLA_LINKMODE and IFLA_GROUP
// (every link in the dump has those at 0).

// attrSeen is a bitset of the rtattr types one parse has already consumed.
//
// Only types below 64 are tracked. A type at or above 64 is always reported as
// first, which degrades to the kernel's last-wins rule rather than dropping the
// attribute.
//
// Two decoded attributes are already up there, both in the `ip -d link` detail
// group: IFLA_GRO_IPV4_MAX_SIZE at exactly 64, and IflaNetnsImmutable at 67.
// Neither is duplicated by any kernel, so the degradation costs nothing today,
// but the IFLA_* space has passed this bitset and will keep going — widen it
// before adding a detail attribute whose duplicate would matter. Every other
// group stays well below the line: the highest NDA_* this package reads is
// NdaFlagsExt at 15, the highest RTA_* is RTA_PREF at 20, and the highest
// FRA_* is FraDscpMask at 30.
type attrSeen uint64

// first reports whether atype has not been consumed yet, and marks it consumed.
// Every rtnetlink parser's switch is guarded by it, so a duplicated attribute
// is ignored rather than overwriting the value `ip` would have used.
func (s *attrSeen) first(atype uint16) bool {
	if atype >= 64 {
		return true
	}
	bit := attrSeen(1) << atype
	if *s&bit != 0 {
		return false
	}
	*s |= bit
	return true
}
