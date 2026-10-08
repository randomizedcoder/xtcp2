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
// fact 3 observed to be absent from `ip link show`'s replies. Under `-s` goip
// now renders them (see renderAddrGroups), and for AF_INET it does so from
// counters it never had to ask for.
//
// # extMask is honored on one arm and DELIBERATELY DROPPED on the other
//
// This is not an oversight, and it is the asymmetry most likely to be
// "cleaned up" by a later reader. It is `rtnl_linkdump_req_filter_fn`'s own
// shape: the filter fn is the only thing that appends IFLA_EXT_MASK, it runs
// for AF_UNSPEC and AF_PACKET only, and every other family falls through to
// the bare `__rtnl_linkdump_req` (lib/libnetlink.c:591-618). So `ip -s -4 addr
// show` sends bytes IDENTICAL to `ip -4 addr show` — there is no attribute in
// which to carry the mask — while `ip -s addr show` sends 0x01 where plain
// `addr show` sends 0x09.
//
// The stats still come back on the family arm, because an absent mask leaves
// RTEXT_FILTER_SKIP_STATS clear, which is why `-s -4 addr show` can render a
// stats block from a request that never asked for one. Passing extMask into
// the family arm would therefore change nothing about what is rendered and
// everything about whether the bytes match `ip`'s.
//
// Contrast AddrShowLinkGet, which is stats-sensitive for EVERY family:
// ipaddr_link_get addattr32s the mask unconditionally (ip/ipaddress.c:2066).
// The two live one call apart in `ip -s addr show dev NAME` and disagree, and
// pkg/nlparity compares requests for full byte equality.
func AddrShowLinkDump(family uint8, extMask, seq uint32) ([]byte, error) {
	if family == unix.AF_UNSPEC || family == unix.AF_PACKET {
		return xtcpnl.BuildDumpLinkRequestExt(family, extMask, seq)
	}
	return xtcpnl.BuildDumpLinkRequestFamily(family, seq), nil
}

// AddrShowDump builds the LAST request `ip addr show` sends: the address dump
// itself. It is the second request of the bare form and the third of
// `addr show dev NAME`.
//
// 24 bytes — an nlmsghdr and an ifaddrmsg whose only non-zero fields are
// ifa_family and, under `dev NAME`, ifa_index — with no attributes.
// `rtnl_addrdump_req` takes a filter_fn and `ip_addr_list` passes
// `ipaddr_dump_filter` (ip/ipaddress.c:2107), which assigns
// `ifa->ifa_index = filter.ifindex` and nothing else (:1954-1958).
//
// # Why ifindex is a parameter and not a second function
//
// Because iproute2 has one function here too. `ipaddr_dump_filter` writes
// filter.ifindex unconditionally, and filter.ifindex is 0 for a `show` with no
// `dev` argument — so the bare form is not "the request without the field", it
// is "the request with the field set to zero". Those produce identical bytes
// (xtcpnl.BuildDumpAddrRequestIndex says so explicitly), and modeling them as
// two builders would invite the two to drift apart over a distinction the
// source does not draw.
//
// The kernel honors ifa_index only on a socket with NETLINK_GET_STRICT_CHK
// set. `ip` sets it once on the main handle (ip/ip.c:312) and goip sets it at
// source.go:92, so both dumps come back filtered; on a lax socket the field is
// ignored and the caller silently receives every address on the host. That is
// invisible to nlmon, so no capture can distinguish the two — the client-side
// filter in addrBelongsTo is what makes goip's output correct either way.
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
// linkmode field. goip implements `-0` and skips it in the same place.
func AddrShowDump(family uint8, ifindex uint32, seq uint32) []byte {
	return xtcpnl.BuildDumpAddrRequestIndex(family, ifindex, seq)
}

// AddrShowLinkGet builds the SECOND of the three requests
// `ip addr show dev NAME` sends: ipaddr_link_get (ip/ipaddress.c:2052-2083),
// the single-get whose reply supplies the one link stanza that gets printed.
//
// # Three requests for one interface, and none of the three is redundant
//
// `addr show dev NAME` takes the same `filter_dev` else-arm every other form
// does (:2241-2247) and then resolves it with ll_name_to_index (:2253). On a
// cache miss that is ll_link_get(name, 0) on a throwaway socket — LinkShowByName
// above, byte for byte, because it is literally the same function. Only then,
// with filter.ifindex set, does :2302 take the single-get branch instead of
// ip_link_list's dump, and :2314 dump the addresses.
//
// So the command's shape is: resolve the name, re-fetch the link by the index
// just learned, dump that index's addresses. `link show dev NAME` re-fetches
// BY NAME instead (iplink_get, LinkShowDev above), which is the one place the
// two `dev` commands part company — and it is a difference in request bytes,
// not in behavior, so only a byte comparison can see it.
//
// # Why this is not LinkShowByIndex with a family argument
//
// It is the same builder, and deliberately a different function here, because
// the family is decided somewhere else and decided differently. LinkShowByIndex
// is ll_link_get's index arm, whose ifinfomsg is a designated initializer that
// names ifi_index and nothing else (lib/ll_map.c:265-275) — so ifi_family is
// structurally AF_UNSPEC and there is no knob. ipaddr_link_get sets
// `.i.ifi_family = filter.family` (:2058), which is preferred_family
// (:2152), which `-4` and `-6` do reach on this command. One command can
// therefore send an AF_UNSPEC get and an AF_INET6 get back to back, and a
// shared helper with a default would get one of them wrong.
//
// The mask is ExtMaskShow without `-s` and ExtMaskStats with it:
// `filter.vfinfo` is 1 unless `novf` was given (:2153, :2239) and
// `!show_stats` ors in RTEXT_FILTER_SKIP_STATS (:2066-2067). It is a parameter
// rather than a constant for the same reason LinkShowDev's is — `-s` reaches
// this call site in `ip`, and goip's addr object now passes it through
// (obj_addr.go's addrShowDev).
//
// Unlike AddrShowLinkDump, this one is stats-sensitive for EVERY family.
// ipaddr_link_get addattr32s the mask unconditionally, with no
// rtnl_linkdump_req_filter_fn in the way to skip it for a non-AF_UNSPEC
// family. So under `ip -s -4 addr show dev NAME` the dump carries no
// IFLA_EXT_MASK while this get carries 0x01 — two requests of one command,
// disagreeing, and that disagreement is the correct behavior rather than a
// bug to normalize away.
func AddrShowLinkGet(family uint8, index int32, extMask, seq uint32) ([]byte, error) {
	return xtcpnl.BuildGetLinkByIndexRequest(family, index, extMask, seq)
}

// RouteShowDump applies iproute2's route-show filter policy to the verified
// rtnetlink builder. RT_TABLE_UNSPEC means table all; oif 0 means no device
// filter.
//
// # Two selectors, one request, and the second one has a prerequisite
//
// `table N` needs nothing but parsing. `dev NAME` needs an ifindex, and
// iproute2 gets it with ll_name_to_index (ip/iproute.c:2008-2016) — which, on
// a command that never calls ll_init_map, is a throwaway ll_link_get(name, 0)
// on its own socket. So `route show dev NAME` is TWO transactions where
// `route show` is one, and the first of the two is LinkShowByName above, byte
// for byte, exactly as it is for `addr show dev` and `link show dev`.
//
// The `dev` spelling is compared with strcmp, not matches(), and `oif` is an
// exact synonym compared the same way (ip/iproute.c:1911-1913). Neither
// abbreviates: `ip route show d eth0` is not a device filter, it is a
// destination prefix, because the else-arm at :2158 reads an unrecognized
// token as an address.
//
// # What this request does NOT carry
//
// `iif NAME` resolves the same way but lands in filter.iif, which
// iproute_dump_filter never writes — it is a client-side filter only
// (:325-331). So `route show iif NAME` sends the same bytes as a bare
// `route show` and differs only in which replies survive. goip does not
// implement it; the point of recording it here is that a future
// implementation must NOT reach for an RTA_IIF attribute on the request.
func RouteShowDump(family uint8, table, oif, seq uint32) ([]byte, error) {
	return xtcpnl.BuildDumpRouteRequestFilter(family, table, oif, seq)
}

// NeighShowDump delegates to the verified ndmsg dump builder, carrying the two
// filters goip can reach: NDA_IFINDEX, for `ip neigh show dev NAME`, and
// ndm_flags, for `ip neigh show proxy`.
//
// ifindex 0 omits the attribute and ndmFlags 0 writes a zero byte the bare
// command writes too, so the two zeros together are `ip neigh show`.
//
// # `proxy` is orthogonal to `dev`, and iproute2 lets them combine
//
// do_show_or_flush parses them in one loop with no mutual exclusion
// (ip/ipneigh.c:506-594), so `ip neigh show proxy dev eth0` sets both and
// ipneigh_dump_filter writes both — ndm_flags at :490 first, NDA_IFINDEX at
// :493 second. That ordering is the request's byte layout and is fixed in the
// builder, not here.
//
// The two are not two filters over one set, though. NTF_PROXY selects a
// different TABLE in the kernel (net/core/neighbour.c:2956), so combining them
// asks for the proxy entries on one device rather than narrowing the neighbor
// entries this command otherwise returns.
//
// # `neigh show dev NAME` costs no extra transaction, and that is the point
//
// It is the third `dev NAME` form in goip and the first whose selector is free.
// `link show dev` and `addr show dev` each pay a throwaway ll_link_get, and
// `route show dev` pays one too (see RouteShowDump). This one pays nothing,
// because do_show_or_flush calls ll_init_map(&rth) at ip/ipneigh.c:597 —
// unconditionally, for the bare command as much as for this one — and only
// then resolves the name at :600. ll_init_map has already dumped every link
// into the cache (lib/ll_map.c:390-407), so ll_name_to_index's ll_get_by_name
// hits and never reaches ll_link_get (:354-359).
//
// So `neigh show` and `neigh show dev NAME` are BOTH two transactions, the
// first byte-identical, and the whole difference between the commands is the
// 8 bytes of NDA_IFINDEX on the second. That is the exact opposite of route,
// where the selector moved a transaction to the front and deleted several from
// the back, and it is why the two forms need separate positional assertions
// rather than one shared shape test.
//
// The exception, which is the negative case and not a caveat: a device NOT in
// the cache misses ll_get_by_name and DOES send ll_link_get. `ip neigh show
// dev nosuch` therefore emits a third transaction before failing. goip reports
// the failure instead, matching the position taken in routeShow — neither of
// iproute2's two remaining fallbacks (if_nametoindex, then the `if%u` spelling
// via ll_idx_a2n) asks the kernel anything, so adopting them would make goip
// answer where `ip` sent a request the capture records.
func NeighShowDump(family, ndmFlags uint8, ifindex, seq uint32) ([]byte, error) {
	return xtcpnl.BuildDumpNeighRequestFilter(family, ndmFlags, ifindex, seq)
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

// RuleShowDump is `ip rule show`'s only request, rtnl_ruledump_req
// (lib/libnetlink.c:407-421).
//
// One transaction, 28 bytes, no attributes — and no attribute is POSSIBLE,
// because under strict check the kernel rejects a rule dump carrying one
// (net/core/fib_rules.c:1278-1281). Every selector `ip rule show` accepts is
// applied client-side against replies, so this is the one show command in
// goip whose request cannot vary with its arguments at all.
//
// The family byte is the caller's; the AF_UNSPEC substitution iproute2 makes
// lives in internal/goip/obj_rule.go beside the `ip` line it comes from. See
// xtcpnl.BuildDumpRuleRequest for why the split is there and not here.
func RuleShowDump(family uint8, seq uint32) ([]byte, error) {
	return xtcpnl.BuildDumpRuleRequest(family, seq)
}

// NexthopGetByID is the single-get RTM_GETNEXTHOP `ip -d route show` sends for a
// route delegating its next hop to a nexthop object (ipnh_cache_add). family is
// preferred_family, which for a plain `route show` is AF_UNSPEC.
func NexthopGetByID(family uint8, id, seq uint32) ([]byte, error) {
	return xtcpnl.BuildGetNexthopByIDRequest(family, id, seq)
}

// NexthopDump is the RTM_GETNEXTHOP dump behind `ip nexthop show` — the
// multipart form, terminated by NLMSG_DONE, as opposed to the by-id single-get.
// family is preferred_family; the bare command passes AF_UNSPEC.
func NexthopDump(family uint8, seq uint32) ([]byte, error) {
	return xtcpnl.BuildDumpNexthopRequest(family, seq), nil
}
