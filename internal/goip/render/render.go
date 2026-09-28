// Package render turns decoded netlink objects into the text and JSON `ip`
// prints.
//
// The package is deliberately free of netlink: it takes a decoded
// xtcpnl.LinkInfo (and, later, AddrInfo and RouteInfo) plus a name lookup, and
// returns bytes. That is what makes the formatter testable from a committed
// pcap with no socket, no root and no VM — the same trick iproute2 uses for
// `ip addr showdump` (lib/libnetlink.c:1322).
//
// # What this package does and does not promise
//
// The goip plan's standing decision is that **netlink parity gates and stdout
// is informational**. This package is the informational half, and the
// distinction is not an excuse: every format string here is transcribed from a
// named iproute2 print_* call, and the tests cite the sidecar line each
// expectation reproduces. What it does mean is that a divergence found here is
// a bug report, not a broken build.
//
// Three divergences are known, deliberate, and listed once here rather than
// repeated at each site:
//
//   - **No config files are read.** `ip` resolves device group names through
//     /etc/iproute2/group then /usr/lib/iproute2/group
//     (rtnl_group_initialize, lib/rt_names.c:701-711). The shipped table holds
//     exactly one entry, "0 default", which is baked in below; a host that has
//     added group names will diverge.
//   - **No netns name resolution.** For a link with IFLA_LINK_NETNSID, `ip`
//     calls get_name_from_nsid and prints "link-netns <name>" when /run/netns
//     holds a matching entry, falling back to "link-netnsid <id>". Resolving
//     the name costs an RTM_GETNSID exchange per namespace, which would be
//     side traffic inside a parity capture window, so goip always prints the
//     numeric form. The committed sidecar happens to print the numeric form
//     too, because /run/netns was empty on the capture host.
//   - **`ip -d` detail attributes are not rendered at all**, by scope. The
//     committed sidecars were captured with `-d`, so a naive diff against them
//     shows a large difference that is expected; the plan's Item 7 adds plain
//     sidecars as the matched pair.
//   - **No SIOCGIFTXQLEN fallback.** When a reply carries no IFLA_TXQLEN the
//     pinned `ip` reaches for an ioctl rather than printing nothing, so it
//     emits "qlen 1000" where goip emits nothing. This is reachable on
//     exactly one command — `ip -6 addr show`, whose replies come from the
//     kernel's inet6_dump_ifinfo and have no IFLA_TXQLEN — and it is the
//     second half of the same version skew documented at RenderQlenZero.
//     goip does not do it because an ioctl is a syscall the parity capture
//     cannot see, and reproducing an unobservable side channel to match a
//     line of output is the wrong trade; the fork that is the reading
//     reference deleted the fallback as dead code.
//
// # One version skew that changes output
//
// See RenderQlenZero.
package render

import (
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// RenderQlenZero selects which iproute2 generation's qlen behavior to
// reproduce, and it exists because the two disagree on a real link.
//
// iproute2 commit faceb326 "ip: drop unnecessary fallback to ioctl for tx
// queue length" (2026-07-28, **contained in no tag** — verified with
// `git tag --contains`) rewrote print_queuelen. Before it:
//
//	if (qlen)
//		print_int(PRINT_ANY, "txqlen", "qlen %d", qlen);
//
// After it, the guard is gone and the print is unconditional once
// IFLA_TXQLEN is present. So a zero txqlen prints nothing on every released
// `ip` and prints "qlen 0" on the development branch.
//
// This is not hypothetical on the committed fixture: docker0, br-3a5828b2963a
// and veth179a698 all carry IFLA_TXQLEN with value 0, and ip_link_n's stanzas
// for all three end at "group default " with no qlen — which is the evidence
// that the pinned `ip` that produced the sidecar predates faceb326.
//
// false — the default and the parity target — reproduces the released
// behavior. The knob exists so the divergence is a named, flippable fact with
// a test row on each side, rather than a silent choice; the parity allowlist
// gets a version-skew entry citing faceb326 next to the one citing de91e928.
//
// # The same commit has a second effect, and this knob does not cover it
//
// faceb326 removed 22 lines and added 3. The guard above is the small half;
// the large half is an ioctl fallback for when IFLA_TXQLEN is absent
// altogether:
//
//	strcpy(ifr.ifr_name, rta_getattr_str(tb[IFLA_IFNAME]));
//	if (ioctl(s, SIOCGIFTXQLEN, &ifr) < 0) { … }
//	qlen = ifr.ifr_qlen;
//
// That path is unreachable on every command whose replies come from
// rtnl_dump_ifinfo, which is why it read as dead code. It is *not* dead on
// `ip -6 addr show`: those replies come from inet6_dump_ifinfo and carry no
// IFLA_TXQLEN, so the pinned `ip` prints "qlen 1000" for lo from an ioctl
// while the fork prints nothing. Verified against both.
//
// goip has no knob for it and never prints the ioctl value — see the package
// doc's divergence list for why — so the allowlist entry for faceb326 has to
// name two loci, not one.
var RenderQlenZero = false

// NameTab is what a renderer needs from the interface index cache, and no
// more: iproute2's ll_index_to_name and ll_index_to_flags.
//
// It is an interface here rather than a concrete type so this package stays
// free of the netlink socket the real cache is filled from, and so the
// formatter tests can supply a fixed map.
type NameTab interface {
	// IndexToName resolves an interface index to a name, with iproute2's
	// fallbacks: "*" for index 0 (lib/ll_map.c:313) and "if%u" for an index
	// that is not in the cache (:327).
	IndexToName(idx int32) string

	// IndexToFlags returns the cached ifi_flags for an index, or -1 when the
	// index is not cached, exactly as ll_index_to_flags does
	// (lib/ll_map.c:343-352). The -1 is load-bearing and must not be
	// normalized to 0 — see LinkView's M-DOWN handling.
	IndexToFlags(idx int32) int64
}

// operStates are the RFC 2863 names, indexed by IFLA_OPERSTATE.
// ip/ipaddress.c:120-123.
var operStates = [...]string{
	"UNKNOWN", "NOTPRESENT", "DOWN", "LOWERLAYERDOWN",
	"TESTING", "DORMANT", "UP",
}

// linkModes are IFLA_LINKMODE's names. ip/ipaddress.c:170-172.
var linkModes = [...]string{"DEFAULT", "DORMANT"}

// linkFlagNames is the IFF_* table in the order print_link_flags emits it,
// which is NOT numeric order — ip/ipaddress.c:94-111. UP comes after
// NOTRAILERS and before LOWER_UP, so `<BROADCAST,MULTICAST,UP,LOWER_UP>`
// rather than `<UP,BROADCAST,MULTICAST,LOWER_UP>`. Reordering this table
// changes output on every link in the fixture.
//
// IFF_RUNNING is absent on purpose: print_link_flags consumes it to decide
// NO-CARRIER and then clears it, so it never appears as a token of its own.
var linkFlagNames = []struct {
	bit  uint32
	name string
}{
	{unix.IFF_LOOPBACK, "LOOPBACK"},
	{unix.IFF_BROADCAST, "BROADCAST"},
	{unix.IFF_POINTOPOINT, "POINTOPOINT"},
	{unix.IFF_MULTICAST, "MULTICAST"},
	{unix.IFF_NOARP, "NOARP"},
	{unix.IFF_ALLMULTI, "ALLMULTI"},
	{unix.IFF_PROMISC, "PROMISC"},
	{unix.IFF_MASTER, "MASTER"},
	{unix.IFF_SLAVE, "SLAVE"},
	{unix.IFF_DEBUG, "DEBUG"},
	{unix.IFF_DYNAMIC, "DYNAMIC"},
	{unix.IFF_AUTOMEDIA, "AUTOMEDIA"},
	{unix.IFF_PORTSEL, "PORTSEL"},
	{unix.IFF_NOTRAILERS, "NOTRAILERS"},
	{unix.IFF_UP, "UP"},
	{unix.IFF_LOWER_UP, "LOWER_UP"},
	{unix.IFF_DORMANT, "DORMANT"},
	{unix.IFF_ECHO, "ECHO"},
}

// FlagTokens renders ifi_flags the way print_link_flags does
// (ip/ipaddress.c:84-118), returning the tokens without the surrounding
// angle brackets so the JSON renderer can use the same list.
//
// The three rules that are easy to get wrong, all transcribed rather than
// inferred:
//
//   - **NO-CARRIER is synthetic and comes first.** It is emitted when IFF_UP
//     is set and IFF_RUNNING is clear — an administratively-up link with no
//     carrier — and there is no IFF_NO_CARRIER bit behind it.
//   - **IFF_RUNNING is then cleared and never named.** virbr0 in the committed
//     dump is the case: flags 0x1003, UP without RUNNING, rendering
//     `<NO-CARRIER,BROADCAST,MULTICAST,UP>`.
//   - **Unrecognized bits are printed as bare lowercase hex**, with no name
//     and no separator of their own (`print_hex(PRINT_ANY, NULL, "%x", flags)`
//     after the table). They are a token here so the join handles the comma.
//
// mdown is print_name_and_link's return value; see LinkView.
func FlagTokens(flags uint32, mdown bool) []string {
	var out []string
	if flags&unix.IFF_UP != 0 && flags&unix.IFF_RUNNING == 0 {
		out = append(out, "NO-CARRIER")
	}
	rest := flags & ^uint32(unix.IFF_RUNNING)
	for _, f := range linkFlagNames {
		if rest&f.bit != 0 {
			rest &= ^f.bit
			out = append(out, f.name)
		}
	}
	if rest != 0 {
		out = append(out, strconv.FormatUint(uint64(rest), 16))
	}
	if mdown {
		out = append(out, "M-DOWN")
	}
	return out
}

// operStateName is print_operstate's text form: a name from the table, or
// `%#llx` for a state the table does not cover (ip/ipaddress.c:125-149).
func operStateName(state uint8) string {
	if int(state) < len(operStates) {
		return operStates[state]
	}
	return fmt.Sprintf("%#x", state)
}

// linkModeName is print_linkmode's text form, falling back to the decimal
// index (ip/ipaddress.c:174-188).
func linkModeName(mode uint8) string {
	if int(mode) < len(linkModes) {
		return linkModes[mode]
	}
	return strconv.FormatUint(uint64(mode), 10)
}

// groupName is rtnl_group_n2a over the table iproute2 actually ships, which
// has exactly one entry (etc/iproute2/group: "0 default"). Anything else falls
// through to the decimal id, which is also what `ip` does on a host with no
// group file at all (lib/rt_names.c:748-768).
func groupName(group uint32) string {
	if group == 0 {
		return "default"
	}
	return strconv.FormatUint(uint64(group), 10)
}

// joinFlags wraps FlagTokens' output in the angle brackets and trailing space
// print_link_flags closes with (`close_json_array(PRINT_ANY, "> ")`).
func joinFlags(tokens []string) string {
	return "<" + strings.Join(tokens, ",") + "> "
}
