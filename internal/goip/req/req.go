// Package req builds the netlink requests goip sends.
//
// Every function here is pure: argv-derived arguments in, bytes out, no socket
// and no global state. That is the whole reason the package exists separately
// from the code that sends them — Tier A of the parity harness compares these
// bytes against the requests a real `ip` was recorded emitting, and a Tier A
// test that had to open a netlink socket could not run under `go test` on a
// developer's machine, let alone inside a nix build.
//
// # Where the iproute2 policy lives
//
// pkg/xtcpnl's builders deliberately take the family and the ext-mask as
// caller arguments, because which family iproute2 decides to attach an
// IFLA_EXT_MASK to is a property of iproute2 and not of the wire. This package
// is that caller: it is where "an `ip link show` dump is AF_PACKET with mask
// 0x09" is written down, once, as a constant with a source citation.
//
// # One skew worth knowing about before reading the numbers below
//
// The parity target is the pinned nixpkgs `ip`, and the reading reference is a
// newer fork. They agree on every request byte this package produces, but not
// on all rendering — see the RenderQlenZero note in the render package.
package req

import (
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// ExtMaskShow is the IFLA_EXT_MASK every `show` dump carries:
// RTEXT_FILTER_VF|RTEXT_FILTER_SKIP_STATS == 0x09.
//
// Both bits are unconditional for a bare `show`, and the source says so
// without a version qualifier. `ipaddr_list_flush_or_save` sets
// `filter.vfinfo = 1` at the top of the function (ip/ipaddress.c:2153), shared
// by `link show` and `addr show` alike, and `iplink_filter_req`
// (ip/ipaddress.c:2017-2026) is:
//
//	if (filter.vfinfo)  filt_mask |= RTEXT_FILTER_VF;
//	if (!show_stats)    filt_mask |= RTEXT_FILTER_SKIP_STATS;
//
// `show_stats` is the `-s` flag, so SKIP_STATS is set for a bare `show` and
// cleared under `-s`. That is what keeps IFLA_STATS and IFLA_STATS64 out of
// every reply to this form; ExtMaskStats below is the other value.
//
// Verified against the wire, not just the source: record 0 of
// pkg/xtcpnl/testdata/7_1_8/netlink_route_getlink.pcap carries
// `0800 1d00 09000000` — rta_len 8, rta_type 29 (IFLA_EXT_MASK), value 0x09.
const ExtMaskShow uint32 = xtcpnl.RTEXT_FILTER_VF | xtcpnl.RTEXT_FILTER_SKIP_STATS

// ExtMaskStats is the same mask under `ip -s`: RTEXT_FILTER_VF alone, 0x01.
//
// The entire difference between `ip link show` and `ip -s link show` on the
// wire is this byte. Both send the same 40-byte datagram — same nlmsghdr,
// same ifinfomsg with ifi_family = AF_PACKET, same IFLA_EXT_MASK attribute
// header — differing only in the attribute's value at offset 36, 0x09 against
// 0x01. The reply delta is not small at all: clearing RTEXT_FILTER_SKIP_STATS
// makes the kernel attach IFLA_STATS (96 bytes) and IFLA_STATS64 (200 bytes)
// to every link it returns.
//
// RTEXT_FILTER_VF survives because `filter.vfinfo` is unrelated to `-s`: it
// defaults to 1 (ip/ipaddress.c:2153) and is cleared only by the `novf`
// argument (:2239), which is a separate command form.
//
// The same `!show_stats` toggle appears in ipaddr_link_get (:2066-2067) and
// iplink_get (ip/iplink.c:1513-1514), so `-s` composes with `link show dev`.
// It does NOT reach ll_link_get or ll_init_map, whose masks are hardcoded
// (lib/ll_map.c:277, :395) — so the throwaway resolution get that `link show
// dev` sends first stays at 0x09 even under `-s`.
const ExtMaskStats uint32 = xtcpnl.RTEXT_FILTER_VF

// LinkShowDump builds the request for `ip link show`.
//
// # The family is AF_PACKET even under -4 and -6
//
// This is not the obvious behavior and it is worth one paragraph, because
// getting it wrong is a one-byte divergence that no amount of output
// comparison would find. `ipaddr_list_link` sets
// `preferred_family = AF_PACKET` **unconditionally** (ip/ipaddress.c:2414-2418)
// before dispatching, and `ip_link_list` then passes `preferred_family` — not
// `filter.family` — to `rtnl_linkdump_req_filter_fn` (:2091). So `-4` and `-6`
// are overwritten for this command and `ip -4 link show` puts 17 in
// ifi_family, exactly as a bare `ip link show` does.
//
// That also keeps the request on the attribute-carrying path:
// `rtnl_linkdump_req_filter_fn` only calls its filter_fn for
// AF_UNSPEC or AF_PACKET (lib/libnetlink.c:595) and otherwise falls through to
// a bare 32-byte `__rtnl_linkdump_req`. `ip addr show`'s link dump is the
// command where the family does survive, and where the 32-byte form appears.
//
// The result is 40 bytes, and iproute2 sends exactly 40: this is the one
// command with no oversend, because `rtnl_linkdump_req_filter_fn` ends in
// `send(rth->fd, &req, req.nlh.nlmsg_len, 0)` (:618) rather than
// `sizeof(req)`.
// The mask is a parameter rather than ExtMaskShow baked in, because `-s`
// changes it and nothing else about this request. Making the caller name the
// value keeps the one byte that distinguishes the two commands visible at the
// call site instead of hidden behind a boolean.
func LinkShowDump(extMask, seq uint32) ([]byte, error) {
	return xtcpnl.BuildDumpLinkRequestExt(unix.AF_PACKET, extMask, seq)
}

// AddrShowLinkDump builds the FIRST of the two requests `ip addr show` sends:
// the link dump, whose replies supply the stanza line every address is printed
// under.
//
// # Why the family survives here and does not for `link show`
//
// `ipaddr_list_flush_or_save` sets `filter.family = preferred_family`
// (ip/ipaddress.c:2152) and never overrides it, and `ip_link_list` passes
// `preferred_family` straight to `rtnl_linkdump_req_filter_fn` (:2090). So
// unlike `ip link show`, which forces AF_PACKET, `-4` and `-6` reach the wire
// here. That single byte selects between two different request shapes and, on
// the reply side, between two different kernel functions:
//
//	family     request                          answered by
//	AF_UNSPEC  40 B, IFLA_EXT_MASK = 0x09       rtnl_dump_ifinfo   (full)
//	AF_INET    32 B, no attributes              rtnl_dump_ifinfo   (full)
//	AF_INET6   32 B, no attributes              inet6_dump_ifinfo  (minimal)
//
// The request-side split is `rtnl_linkdump_req_filter_fn`, which calls its
// filter_fn — the only thing that appends IFLA_EXT_MASK — for AF_UNSPEC and
// AF_PACKET only and otherwise falls through to the bare
// `__rtnl_linkdump_req` (lib/libnetlink.c:591-618). The reply-side split is in
// the kernel and is documented at xtcpnl.BuildDumpLinkRequestFamily.
//
// One consequence worth stating because it is invisible in the request: the
// AF_INET form carries no ext mask, so RTEXT_FILTER_SKIP_STATS is clear and
// the replies **do** carry IFLA_STATS and IFLA_STATS64 — the attributes plan
// fact 3 observed to be absent from `ip link show`'s replies. goip renders
// neither (no -s), so this costs nothing but it does mean an `addr show` reply
// is materially bigger than a `link show` one.
func AddrShowLinkDump(family uint8, seq uint32) ([]byte, error) {
	if family == unix.AF_UNSPEC || family == unix.AF_PACKET {
		return xtcpnl.BuildDumpLinkRequestExt(family, ExtMaskShow, seq)
	}
	return xtcpnl.BuildDumpLinkRequestFamily(family, seq), nil
}

// AddrShowDump builds the SECOND request `ip addr show` sends: the address
// dump itself.
//
// 24 bytes — an nlmsghdr and an ifaddrmsg whose only non-zero field is
// ifa_family — with no attributes. `rtnl_addrdump_req` takes a filter_fn and
// `ip_addr_list` passes `ipaddr_dump_filter` (ip/ipaddress.c:2107), but that
// function assigns `ifa->ifa_index = filter.ifindex` and nothing else
// (:2060-2067), so for a `show` with no `dev` argument it writes the zero
// that was already there.
//
// # The 128 bytes goip does not send
//
// `rtnl_addrdump_req`'s struct ends in `char buf[128]` and it sends
// `sizeof(req)` rather than `nlh->nlmsg_len` (lib/libnetlink.c:313-336), so
// the real `ip` puts a 152-byte datagram on the wire whose last 128 bytes are
// zero. That is the tail pkg/nlparity's walker exists to tolerate. goip sends
// 24 bytes, deliberately: the plan's decision is that oversend is recorded as
// DatagramLen/TailBytes and reported informationally, never gated, so that
// the allowlist never acquires an entry for something structural. Matching the
// padding would be reproducing a bug in the parity target.
//
// Note that this request is only sent when the family is not AF_PACKET:
// `ip -0 addr show` (preferred_family AF_PACKET) skips the address dump
// entirely (ip/ipaddress.c:2310) and degenerates to `link show` without the
// linkmode field. goip rejects `-0` rather than implementing it, so the caller
// here never passes AF_PACKET.
func AddrShowDump(family uint8, seq uint32) []byte {
	return xtcpnl.BuildDumpAddrRequest(family, seq)
}

// RouteShowDump applies iproute2's route-show table policy to the verified
// rtnetlink builder.  RT_TABLE_UNSPEC means table all.
func RouteShowDump(family uint8, table uint32, seq uint32) ([]byte, error) {
	return xtcpnl.BuildDumpRouteRequestTable(family, table, seq)
}

// NeighShowDump delegates to the verified ndmsg dump builder.
func NeighShowDump(family uint8, seq uint32) []byte {
	return xtcpnl.BuildDumpNeighRequest(family, seq)
}

// NeighShowLinkDump is ll_init_map's link dump before a neighbor dump. Unlike
// link/addr show it requests VF information but does not set SKIP_STATS.
func NeighShowLinkDump(seq uint32) ([]byte, error) {
	return xtcpnl.BuildDumpLinkRequestExt(unix.AF_UNSPEC, xtcpnl.RTEXT_FILTER_VF, seq)
}

// LinkShowByIndex builds the single-get behind `ip link show dev X` once the
// name has been resolved to an index, and behind `ll_link_get`'s index-cache
// fills.
//
// This is a GET, not a DUMP: flags are NLM_F_REQUEST alone, and the kernel
// answers with exactly one message carrying neither NLM_F_MULTI nor
// NLMSG_DONE. Reading that reply needs xtcpnl.TalkRtnetlink; DumpRtnetlink
// blocks until SO_RCVTIMEO on it.
//
// # The family here is AF_UNSPEC, not AF_PACKET, and the capture is why we know
//
// It would be natural to reuse LinkShowDump's AF_PACKET, and that is wrong.
// `ll_link_get`'s request is a designated initializer that sets `ifi_index`
// and nothing else (lib/ll_map.c:265-275), so `ifi_family` stays 0. The ten
// single-gets recorded in netlink_route_getroute.pcap all carry 0 in that
// byte, and Tier A's byte-for-byte comparison is what surfaced it — the
// difference is invisible in any output and would have been a silent one-byte
// divergence on every `ip link show dev X`.
//
// One aside the same function makes visible: ll_link_get calls
// `rtnl_open(&rth, 0)` and gets its own socket per lookup. That is why ten
// single-gets share the route dump's sequence number, and why the parity
// comparator must never key a transaction on seq alone.
func LinkShowByIndex(index int32, seq uint32) ([]byte, error) {
	return xtcpnl.BuildGetLinkByIndexRequest(unix.AF_UNSPEC, index, ExtMaskShow, seq)
}

// LinkShowByName is LinkShowByIndex addressed by IFLA_IFNAME instead, which is
// what `ll_link_get` sends when it has a name and no index.
//
// Attribute order is load-bearing and is the builder's, not this function's:
// `ll_link_get` emits IFLA_EXT_MASK first and the name second
// (lib/ll_map.c:289-293), and the parity comparator holds requests to full
// byte equality, so the other order is a divergence rather than a detail.
// AF_UNSPEC for the same reason as LinkShowByIndex — it is the same request
// struct.
//
// # One iproute2 behavior deliberately not reproduced
//
// ll_link_get picks the attribute by validity:
// `!check_ifname(name) ? IFLA_IFNAME : IFLA_ALT_IFNAME` (:287-289). A name
// that is not a valid interface name is sent as an *alternative* name instead
// of being rejected, which is how `ip link show dev enxe04f43e628ef` works for
// a 15-character altname and how a longer altname would work too. goip rejects
// an invalid name here rather than falling back, because the fallback needs
// IFLA_ALT_IFNAME on the request side and there is no captured example of one
// to check against. Recorded rather than silently diverged: a by-name
// single-get fixture is on the plan's Item 7 list.
func LinkShowByName(name string, seq uint32) ([]byte, error) {
	return xtcpnl.BuildGetLinkByNameRequest(unix.AF_UNSPEC, name, ExtMaskShow, seq)
}

// LinkShowDev is the SECOND request of `ip link show dev NAME` — iplink_get,
// the one whose reply is printed. LinkShowByName above is the first, and it
// exists only to fill the index cache.
//
// # Why AF_PACKET here and AF_UNSPEC there, inside one command
//
// The two requests are built by different functions with different notions of
// family. ll_link_get zero-initializes its ifinfomsg, so the first stays
// AF_UNSPEC. iplink_get sets `.i.ifi_family = preferred_family`
// (ip/iplink.c:1502), and ipaddr_list_link has already forced that to
// AF_PACKET at ip/ipaddress.c:2416 — before argument parsing, which is why no
// `-4` or `-6` can change it. One command therefore puts two different
// families in the same header field, and a shared helper would get one of them
// wrong.
//
// # Why only this half of the command takes a mask
//
// `-s` reaches iplink_get and stops there. Its `if (!show_stats) filt_mask |=
// RTEXT_FILTER_SKIP_STATS` (ip/iplink.c:1513-1514) is the same toggle
// ipaddr_list_link has at ip/ipaddress.c:2017-2026, so this request follows
// `-s` exactly as the dump does. The FIRST request does not: ll_link_get
// builds `RTEXT_FILTER_VF | RTEXT_FILTER_SKIP_STATS | RTEXT_FILTER_NAME_ONLY`
// as a local constant (lib/ll_map.c:276-277) with no reference to show_stats
// at all, so LinkShowByName above keeps ExtMaskShow under every option.
//
// That asymmetry is the whole reason the parameter is here rather than on a
// shared helper: `ip -s link show dev X` sends one request that changed and
// one that did not, and a mask threaded through both would make the first
// wrong in a way no output could show.
//
// Only the ll_link_get half is a version-skew locus. See
// xtcpnl.BuildIplinkGetRequest.
func LinkShowDev(name string, extMask, seq uint32) ([]byte, error) {
	return xtcpnl.BuildIplinkGetRequest(unix.AF_PACKET, name, extMask, seq)
}
