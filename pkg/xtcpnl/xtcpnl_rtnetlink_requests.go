package xtcpnl

// This file holds the per-family rtnetlink request builders that need more than
// a family byte: an extended-attribute mask, an interface index, a routing
// table, or an interface name.
//
// The three original builders (BuildDumpLinkRequest, BuildDumpAddrRequest,
// BuildDumpRouteRequest) stay in xtcpnl_rtnetlink.go — they are the
// family-byte-only forms xtcp2's own enrichment path uses. Everything here
// exists because `ip` sends something more specific, and the goip parity work
// compares request bytes for full equality.
//
// # Every shape here was verified against captured bytes or iproute2 source
//
// The two forms with committed captures are marked; the rest cite the iproute2
// function they mirror, and their fixtures are the gap Item 7 of the goip plan
// closes. Nothing here was written from a guess about what the kernel accepts.
//
//	BuildDumpLinkRequestExt      lib/libnetlink.c rtnl_linkdump_req_filter{,_fn}
//	BuildGetLinkByIndexRequest   lib/ll_map.c     ll_link_get
//	BuildGetLinkByNameRequest    lib/ll_map.c     ll_link_get
//	BuildIplinkGetRequest        ip/iplink.c      iplink_get
//	BuildDumpAddrRequestIndex    ip/ipaddress.c   ipaddr_list_flush_or_save
//	BuildDumpRouteRequestFilter  ip/iproute.c     iproute_dump_filter
//	BuildDumpNeighRequest        lib/libnetlink.c rtnl_neighdump_req
//	BuildDumpNeighRequestFilter  ip/ipneigh.c     ipneigh_dump_filter
//
// # Oversend is not reproduced, and that is deliberate
//
// iproute2 declares its request as a struct with a trailing `char buf[...]`
// and, for most commands, sends `sizeof(req)` rather than `nlmsg_len` — so the
// datagram carries a zeroed tail past the message: 128 bytes for addr and
// route, 256 for neigh, 0 for `ip link show` (rtnl_linkdump_req_filter_fn sends
// req.nlh.nlmsg_len, lib/libnetlink.c:618). The kernel ignores the tail.
// Builders here emit exactly nlmsg_len bytes; the tail is recorded by
// pkg/nlparity as DatagramLen/TailBytes and reported informationally, never
// gated. See docs/netlink/coverage-expansion.md.
//
// # One known iproute2 version skew, on the ll_link_get paths only
//
// The captured IFLA_EXT_MASK is 0x09, RTEXT_FILTER_VF|RTEXT_FILTER_SKIP_STATS,
// and that is what every released iproute2 sends. But the reading fork already
// carries commit de91e928 "ll_map: add RTEXT_FILTER_NAME_ONLY to ll_link_get()
// and ll_init_map()" (2026-05-20, in no tag), which makes the mask 0x109 on
// exactly two paths: the single-get in ll_link_get (lib/ll_map.c:277) and the
// up-front link dump in ll_init_map (lib/ll_map.c:398) that `ip neigh show`
// and `ip route show` issue to populate the index cache.
//
// RTEXT_FILTER_NAME_ONLY = (1 << 8) is itself newer than the host kernel:
// linux/include/uapi/linux/rtnetlink.h stops at RTEXT_FILTER_MST (1 << 7), so
// the bit exists only in iproute2's bundled copy of the header and this kernel
// ignores it. Hence the mask is a CALLER argument on every builder here, and
// the parity allowlist needs a version-skew entry citing de91e928 rather than
// a constant baked into the wire layer. The `ip link show` and `ip addr show`
// dump path is unaffected — iplink_filter_req still computes 0x09.

import (
	"encoding/binary"
	"errors"
	"strings"

	"golang.org/x/sys/unix"
)

var (
	// ErrBadIfName indicates an interface name that cannot go in IFLA_IFNAME:
	// empty, IfNameSizeCst or longer once NUL-terminated, or containing a
	// character the kernel rejects.
	ErrBadIfName = errors.New("xtcpnl: interface name not valid for IFLA_IFNAME")
)

const (
	// IfNameSizeCst is the kernel's IFNAMSIZ: the size of the buffer a device
	// name lives in, NUL included. So a name is at most 15 characters.
	//
	// linux/include/uapi/linux/if.h
	IfNameSizeCst = 16

	// reqAttrBufCst sizes the local attribute buffer these builders encode
	// into. The largest attribute stream any of them produces is an
	// IFLA_EXT_MASK (8) plus an IFLA_IFNAME (up to 4+16 = 20, padded), so 32 is
	// ample and keeps the builders allocation-free apart from the request
	// itself.
	reqAttrBufCst = 32
)

// BuildDumpLinkRequestExt builds an RTM_GETLINK dump request carrying
// IFLA_EXT_MASK, which is what `ip link show` and `ip addr show` send.
//
// extMask == 0 omits the attribute entirely, which is not a special case but
// the exact shape iproute2 emits for `ip -4 addr show` / `ip -6 addr show`:
// both rtnl_linkdump_req_filter and rtnl_linkdump_req_filter_fn take their
// attribute-carrying path only for certain families (AF_UNSPEC|AF_BRIDGE and
// AF_UNSPEC|AF_PACKET respectively, lib/libnetlink.c:566,595) and otherwise
// fall through to __rtnl_linkdump_req, which sends a bare 32-byte
// nlmsghdr+ifinfomsg.
//
// The family-to-mask policy is the CALLER's, not this function's. Which
// families iproute2 decides to attach a mask to is a property of iproute2, not
// of the wire, and baking it in here would make the builder unable to express a
// request the kernel accepts perfectly well. Three real captured requests, all
// reproduced by this one function:
//
//	ip link show      AF_PACKET, 0x09  -> 40 bytes  (getlink.pcap rec 0)
//	ip -4 addr show   AF_INET,   0     -> 32 bytes  (getaddr.pcap rec 62)
//	ip -6 addr show   AF_INET6,  0     -> 32 bytes  (getaddr.pcap rec 70)
//
// The 0x09 is RTEXT_FILTER_VF|RTEXT_FILTER_SKIP_STATS, and SKIP_STATS is why
// IFLA_STATS and IFLA_STATS64 are absent from every reply in the fixtures.
func BuildDumpLinkRequestExt(family uint8, extMask, seq uint32) ([]byte, error) {
	hdr := make([]byte, IfInfomsgSizeCst)
	hdr[0] = family // ifi_family

	attrs, err := extMaskAttrs(extMask)
	if err != nil {
		return nil, err
	}
	return BuildRequest(uint16(unix.RTM_GETLINK), uint16(unix.NLM_F_DUMP), seq, hdr, attrs)
}

// BuildGetLinkByIndexRequest builds a single-get RTM_GETLINK for one interface
// index: NLM_F_REQUEST with no NLM_F_DUMP, ifi_index set, and IFLA_EXT_MASK.
//
// This is iproute2's ll_link_get (lib/ll_map.c:264), the lookup `ip` does when
// it has to render a name it does not have cached — IFLA_LINK's @peer suffix
// and IFLA_MASTER both trigger it. The captured form is 40 bytes with
// flags=0x0001, ifi_family=AF_UNSPEC and extMask=0x09
// (getroute.pcap records 4, 6, 8, 10, 12, 14, 16, 18, 20, 22 — ten of them, one
// per interface the route dump had to name). This is one of the two paths the
// version skew noted at the top of this file moves to 0x109.
//
// The reply is a single non-multipart message with no NLMSG_DONE, which is why
// TalkRtnetlink exists: DumpRtnetlink would block waiting for a DONE that never
// comes.
func BuildGetLinkByIndexRequest(family uint8, ifindex int32, extMask, seq uint32) ([]byte, error) {
	hdr := make([]byte, IfInfomsgSizeCst)
	hdr[0] = family // ifi_family
	binary.LittleEndian.PutUint32(hdr[4:8], uint32(ifindex))

	attrs, err := extMaskAttrs(extMask)
	if err != nil {
		return nil, err
	}
	return BuildRequest(uint16(unix.RTM_GETLINK), 0, seq, hdr, attrs)
}

// BuildGetLinkByNameRequest is the by-name half of ll_link_get: ifi_index left
// 0, and an IFLA_IFNAME attribute after the IFLA_EXT_MASK.
//
// The attribute ORDER is load-bearing for parity, not cosmetic. ll_link_get
// calls addattr32(IFLA_EXT_MASK) and only then addattr_l(IFLA_IFNAME)
// (lib/ll_map.c:289-293), and pkg/nlparity compares requests for full byte
// equality, so emitting the two the other way round is a divergence.
//
// Names are validated here rather than truncated: IFLA_IFNAME is a
// NUL-terminated string in an IFNAMSIZ buffer, and the kernel rejects a name
// containing '/' or whitespace (dev_valid_name). iproute2 additionally falls
// back to IFLA_ALT_IFNAME for names its check_ifname rejects; alternative names
// are out of scope here, so such a name is an error rather than a silently
// different request.
//
// There is no committed capture of this form — every single-get in the corpus
// is the by-index one, because `ip` already had the index. Item 7 of the goip
// plan adds `ip link show dev lo` to the capture script; until then its test
// rows are structural and say so.
func BuildGetLinkByNameRequest(family uint8, name string, extMask, seq uint32) ([]byte, error) {
	if err := validIfName(name); err != nil {
		return nil, err
	}

	hdr := make([]byte, IfInfomsgSizeCst)
	hdr[0] = family // ifi_family; ifi_index stays 0 — the name is the selector

	var raw [reqAttrBufCst]byte
	ab := NewAttrBuilder(raw[:])
	if extMask != 0 {
		if err := ab.PutU32(uint16(unix.IFLA_EXT_MASK), extMask); err != nil {
			return nil, err
		}
	}
	if err := ab.PutString(uint16(unix.IFLA_IFNAME), name); err != nil {
		return nil, err
	}
	return BuildRequest(uint16(unix.RTM_GETLINK), 0, seq, hdr, ab.Bytes())
}

// BuildIplinkGetRequest is iplink_get (ip/iplink.c:1497-1515), the SECOND and
// last request `ip link show dev NAME` sends — the one whose reply is printed.
//
// It is not BuildGetLinkByNameRequest with different arguments. Three things
// differ, and pkg/nlparity compares requests for full byte equality, so the
// first two are divergences rather than details:
//
//   - THE ATTRIBUTE ORDER IS REVERSED. iplink_get adds IFLA_IFNAME first and
//     IFLA_EXT_MASK second; ll_link_get adds the mask first
//     (lib/ll_map.c:289-293). The same two attributes, opposite order, inside
//     one command — which is why `ip link show dev lo` cannot be served by
//     sending one request's bytes twice.
//   - ifi_family is preferred_family, not AF_UNSPEC, and for this command that
//     is AF_PACKET: ipaddr_list_link assigns it at ip/ipaddress.c:2416 before
//     any argument is parsed. That is also why `ip -4 link show dev lo` still
//     sends AF_PACKET — the -4 is overridden, not honored.
//   - It goes on the MAIN rtnl socket, where ll_link_get opens a throwaway one
//     (rtnl_open, lib/ll_map.c:281). nlmon cannot observe socket identity, so
//     this difference is invisible on the wire; it is recorded so it is not
//     rediscovered as a bug.
//
// # Unlike ll_link_get, this path carries no version skew
//
// Commit de91e928 added RTEXT_FILTER_NAME_ONLY to ll_link_get and ll_init_map
// and NOT to iplink_get, so this request's mask is 0x09 at the pinned 7.1.0
// and at the reading fork alike. filt_mask arrives as RTEXT_FILTER_VF —
// filter.vfinfo is set unconditionally at ip/ipaddress.c:2153 and cleared only
// by `novf` — and iplink_get ors in RTEXT_FILTER_SKIP_STATS when !show_stats.
// So of the two requests this command sends, only the first one moves when
// iproute2 does.
//
// # The mask attribute is unconditional here
//
// BuildGetLinkByNameRequest omits IFLA_EXT_MASK for a zero mask because
// ll_link_get's addattr32 is reached only with a mask that is never zero.
// iplink_get calls addattr32 unconditionally (:1514), so a caller passing 0
// gets a four-byte zero attribute rather than no attribute — which is what
// `ip -s novf link show dev lo` puts on the wire.
func BuildIplinkGetRequest(family uint8, name string, extMask, seq uint32) ([]byte, error) {
	if err := validIfName(name); err != nil {
		return nil, err
	}

	hdr := make([]byte, IfInfomsgSizeCst)
	hdr[0] = family // ifi_family; ifi_index stays 0 — the name is the selector

	var raw [reqAttrBufCst]byte
	ab := NewAttrBuilder(raw[:])
	if err := ab.PutString(uint16(unix.IFLA_IFNAME), name); err != nil {
		return nil, err
	}
	if err := ab.PutU32(uint16(unix.IFLA_EXT_MASK), extMask); err != nil {
		return nil, err
	}
	return BuildRequest(uint16(unix.RTM_GETLINK), 0, seq, hdr, ab.Bytes())
}

// BuildDumpAddrRequestIndex builds an RTM_GETADDR dump filtered to one
// interface by writing ifa_index into the REQUEST HEADER — there is no
// attribute for it (ip/ipaddress.c, ipaddr_list_flush_or_save).
//
// **The kernel honors this only on a socket with NETLINK_GET_STRICT_CHK set.**
// Without it the dump handler ignores everything past ifa_family and the caller
// silently gets every address on the host; `ip` sets the option in ip.c before
// any request. Nothing in this package sets socket options, so a caller using
// this builder must do it itself:
//
//	unix.SetsockoptInt(fd, unix.SOL_NETLINK, unix.NETLINK_GET_STRICT_CHK, 1)
//
// That option is also invisible to nlmon, so a capture cannot tell a strict
// socket from a lax one — which is the second risk recorded against the parity
// work in docs/netlink/coverage-expansion.md.
//
// ifindex 0 means "no filter" and produces exactly BuildDumpAddrRequest's
// bytes, so the two cannot disagree.
func BuildDumpAddrRequestIndex(family uint8, ifindex, seq uint32) []byte {
	hdr := make([]byte, IfAddrmsgSizeCst)
	hdr[0] = family // ifa_family; prefixlen, flags and scope stay 0
	binary.LittleEndian.PutUint32(hdr[4:8], ifindex)

	return BuildDumpRequest(uint16(unix.RTM_GETADDR), seq, hdr)
}

// BuildDumpRouteRequestFilter builds an RTM_GETROUTE dump carrying the two
// attributes iproute_dump_filter can attach: RTA_TABLE and RTA_OIF.
//
// table must be an RT_TABLE_* id or a numeric table. RT_TABLE_UNSPEC (0) omits
// the attribute, which is how `ip route show table all` asks for every table —
// and that is iproute2's own encoding, not a convention invented here:
// `filter.tb` defaults to RT_TABLE_MAIN (254) at ip/iproute.c:1836, `table all`
// sets it to 0, and iproute_dump_filter only adds RTA_TABLE `if (filter.tb)`
// (ip/iproute.c:1726).
//
// The attribute is needed because rtm_table is a single byte and cannot hold a
// table id above 255; RTA_TABLE is the u32 that supersedes it.
//
// oif is filter.oif, set by `dev NAME` or the synonym `oif NAME`
// (ip/iproute.c:1911-1913) after ll_name_to_index resolves the name
// (:2008-2016). Zero omits the attribute, for the same reason and by the same
// `if (filter.oif)` test (:1731).
//
// # One function, because iproute2 has one, and the order is why it matters
//
// iproute_dump_filter writes RTA_TABLE first and RTA_OIF second, in that
// order, each guarded by its own presence test. The parity comparator holds
// requests to full byte equality, so the order is part of the contract rather
// than an implementation detail — and two builders, one per attribute, would
// have no structural reason to agree on it. Keeping the pair in one function
// makes the order impossible to get wrong at a call site.
//
// # Captured forms
//
// getroute.pcap rec 0 (`ip route show table all`): 28 bytes, an all-zero
// rtmsg, no attributes. Note the family there is AF_UNSPEC, while plain
// `ip route show` sends AF_INET — ip/iproute.c:1998 promotes AF_UNSPEC to
// AF_INET whenever a table filter is set, so the default command is
// (AF_INET, 254, 0) and the all-tables command is (AF_UNSPEC, 0, 0).
// `ip route show dev NAME` is (AF_INET, 254, idx), 44 bytes.
func BuildDumpRouteRequestFilter(family uint8, table, oif, seq uint32) ([]byte, error) {
	hdr := make([]byte, RtMsgSizeCst)
	hdr[0] = family // rtm_family

	var attrs []byte
	if table != uint32(unix.RT_TABLE_UNSPEC) || oif != 0 {
		var raw [reqAttrBufCst]byte
		ab := NewAttrBuilder(raw[:])
		if table != uint32(unix.RT_TABLE_UNSPEC) {
			if err := ab.PutU32(uint16(unix.RTA_TABLE), table); err != nil {
				return nil, err
			}
		}
		if oif != 0 {
			if err := ab.PutU32(uint16(unix.RTA_OIF), oif); err != nil {
				return nil, err
			}
		}
		attrs = ab.Bytes()
	}
	return BuildRequest(uint16(unix.RTM_GETROUTE), uint16(unix.NLM_F_DUMP), seq, hdr, attrs)
}

// BuildDumpNexthopRequest builds the RTM_GETNEXTHOP dump `ip nexthop show`
// sends: rtnl_nexthopdump_req (lib/libnetlink.c:261), REQUEST|DUMP over an
// nhmsg whose nh_family is preferred_family and no attributes.
//
// The captured request is 24 bytes (NLMSG_LENGTH(sizeof(struct nhmsg)) = 24),
// flags 0x0301, nh_family=AF_UNSPEC for the bare command (preferred_family
// unset); netlink_route_getnexthop.pcap. Unlike the by-id get above this is a
// multipart dump terminated by NLMSG_DONE, so it is read with DumpRtnetlink.
func BuildDumpNexthopRequest(family uint8, seq uint32) []byte {
	hdr := make([]byte, NhMsgSizeCst)
	hdr[0] = family // nh_family; scope, protocol, resvd and flags stay 0

	return BuildDumpRequest(uint16(unix.RTM_GETNEXTHOP), seq, hdr)
}

// BuildGetNexthopByIDRequest builds the single-get RTM_GETNEXTHOP that
// `ip -d route show` sends for a route carrying RTA_NH_ID: NLM_F_REQUEST with no
// NLM_F_DUMP, an nhmsg whose nh_family is preferred_family, and two attributes.
//
// This is ipnh_get_id (ip/ipnexthop.c), reached from print_cache_nexthop_id ->
// ipnh_cache_add (ip/iproute.c:1002). The captured request is 40 bytes with
// flags=0x0001, nh_family=AF_UNSPEC (plain `route show` leaves preferred_family
// unset), NHA_ID then NHA_OP_FLAGS=0 — the op-flags attribute the reading fork
// sends unconditionally, so it is emitted here to match rather than guessed
// away (netlink_route_getroute_detail.pcap).
//
// The reply is a single non-multipart message, so reading it needs
// TalkRtnetlink; DumpRtnetlink would block on a NLMSG_DONE that never comes.
func BuildGetNexthopByIDRequest(family uint8, id, seq uint32) ([]byte, error) {
	hdr := make([]byte, NhMsgSizeCst)
	hdr[0] = family // nh_family; scope, protocol, resvd and flags stay 0

	var raw [reqAttrBufCst]byte
	ab := NewAttrBuilder(raw[:])
	if err := ab.PutU32(NhaID, id); err != nil {
		return nil, err
	}
	if err := ab.PutU32(NhaOpFlags, 0); err != nil {
		return nil, err
	}
	return BuildRequest(uint16(unix.RTM_GETNEXTHOP), 0, seq, hdr, ab.Bytes())
}

// NexthopDumpFilter is the wire-filter set `ip nexthop show` appends to the dump
// request (nh_dump_filter, ip/ipnexthop.c:70): OIF for `dev`, Master for `master`
// and `vrf` (a VRF is just a master), and the zero-length flag attributes Groups
// and Fdb. `protocol` is NOT here — iproute2 filters it client-side on the reply
// (print_cache_nexthop, :821), so it never reaches the request.
type NexthopDumpFilter struct {
	OIF    uint32 // NHA_OIF, 0 absent (index 0 is not a device)
	Master uint32 // NHA_MASTER, 0 absent
	Groups bool   // NHA_GROUPS flag
	Fdb    bool   // NHA_FDB flag
}

// BuildGetNexthopDumpRequest builds the filtered RTM_GETNEXTHOP dump
// (NLM_F_REQUEST|NLM_F_DUMP) `ip nexthop show SELECTOR` sends: an nhmsg whose
// nh_family is preferred_family, then the filter attrs in nh_dump_filter order —
// NHA_OIF, NHA_GROUPS, NHA_MASTER, NHA_FDB. An empty filter is byte-identical to
// BuildDumpNexthopRequest's bare dump.
func BuildGetNexthopDumpRequest(family uint8, f NexthopDumpFilter, seq uint32) ([]byte, error) {
	hdr := make([]byte, NhMsgSizeCst)
	hdr[0] = family // nh_family; scope, protocol, resvd and flags stay 0

	var raw [reqAttrBufCst]byte
	ab := NewAttrBuilder(raw[:])
	if f.OIF != 0 {
		if err := ab.PutU32(NhaOIF, f.OIF); err != nil {
			return nil, err
		}
	}
	if f.Groups {
		if err := ab.PutBytes(NhaGroups, nil); err != nil {
			return nil, err
		}
	}
	if f.Master != 0 {
		if err := ab.PutU32(NhaMaster, f.Master); err != nil {
			return nil, err
		}
	}
	if f.Fdb {
		if err := ab.PutBytes(NhaFdb, nil); err != nil {
			return nil, err
		}
	}
	return BuildRequest(uint16(unix.RTM_GETNEXTHOP), uint16(unix.NLM_F_DUMP), seq, hdr, ab.Bytes())
}

// BuildDumpNeighRequest builds an RTM_GETNEIGH dump request (ndmsg) for the
// given address family, completing the set of four dump builders and closing
// TODO-SOON.md §17.
//
// It matters beyond `ip neigh show`: a multicast listener must re-dump to
// resync after ENOBUFS, and for neighbors there was previously nothing to
// re-dump with. The package could parse RTM_NEWNEIGH both solicited and
// unsolicited but could never ask for the current table.
//
// Shape from lib/libnetlink.c rtnl_neighdump_req: nlmsg_len = 28, flags
// REQUEST|DUMP, ndm_family set, everything else zero. iproute2 sends it inside
// a 284-byte datagram (`char buf[256]`); see the oversend note at the top of
// this file.
func BuildDumpNeighRequest(family uint8, seq uint32) []byte {
	hdr := make([]byte, NdMsgSizeCst)
	hdr[0] = family // ndm_family

	return BuildDumpRequest(uint16(unix.RTM_GETNEIGH), seq, hdr)
}

// BuildDumpNeighRequestFilter is BuildDumpNeighRequest with the two filters
// goip can reach: the device of `ip neigh show dev NAME` and the ndm_flags of
// `ip neigh show proxy`.
//
// ifindex 0 and ndmFlags 0 together produce exactly BuildDumpNeighRequest's
// bytes, which is how the bare command is spelled and why the two cannot
// disagree.
//
// # ndmFlags is a field write, and that makes `proxy` the cheapest selector here
//
// NTF_PROXY does not grow the datagram at all: it is one byte at offset 10 of a
// struct the bare command already sends, so `ip neigh show` and `ip neigh show
// proxy` are both 28 bytes and differ in exactly one of them. Contrast the
// device filter below, which adds 8. Neither changes the transaction count.
//
// The kernel reads that byte to pick a TABLE, not to filter one
// (net/core/neighbour.c:2956): it sends neigh_dump_info down pneigh_dump_table
// instead of neigh_dump_table, so the two commands return disjoint sets rather
// than a subset and a superset. A request that dropped the byte would answer
// with the wrong table's contents and still look well-formed.
//
// That test is `ndm_flags == NTF_PROXY`, an EQUALITY and not a mask test, so
// NTF_PROXY may not be or-ed with anything. It is also guarded on
// `nlmsg_len(nlh) >= sizeof(struct ndmsg)`, which holds here only because this
// builder always sends the full 12-byte struct — a caller that trimmed the
// header to the rtgenmsg the kernel will otherwise accept would lose the
// selector without changing a visible byte of it.
//
// # The index is an ATTRIBUTE here, not the ndm_ifindex field
//
// This is the trap, and it is worth more than the byte it costs. `struct ndmsg`
// has an `ndm_ifindex` member sitting at offset 4, exactly where
// BuildDumpAddrRequestIndex writes `ifa_index` for the addr equivalent — and
// iproute2 leaves it zero. ipneigh_dump_filter (ip/ipneigh.c:485-504) writes
// `addattr32(nlh, reqlen, NDA_IFINDEX, filter.index)` instead, so the request
// grows by 8 bytes rather than filling a field it already has.
//
// # Both traps are LOUD, and only because the socket is in strict-dump mode
//
// neigh_valid_dump_req (net/core/neighbour.c:2880-2907) rejects a dump request
// outright under strict check: EINVAL for a nonzero ndm_pad1, ndm_pad2,
// ndm_ifindex, ndm_state or ndm_type, and a separate EINVAL for
// `ndm_flags & ~NTF_PROXY`. So a builder that filled ndm_ifindex, or that
// or-ed NTF_PROXY into some other bit, does not get a well-formed dump of the
// wrong thing — it gets an error with a message naming the field.
//
// That is a property of the SOCKET and not of these bytes. `ip` sets
// NETLINK_GET_STRICT_CHK on its main handle at ip/ip.c:312 and goip sets it at
// internal/goip/source.go:93, so the loud behavior is what both sides see;
// drop the option and the same request is instead parsed leniently, where
// ndm_ifindex is simply unread and any extra flag bit falls through the
// equality above and silently returns the REGULAR table. Two failure modes for
// one mistake, chosen by a setsockopt made somewhere else, which is the reason
// to write the bytes correctly here rather than rely on either.
//
// # The other two things ipneigh_dump_filter writes, and their order
//
// Recorded here rather than in a commit message, because the parity comparator
// holds requests to full byte equality and the order is part of that contract:
//
//  1. `ndm->ndm_flags = filter.ndm_flags` (:490) — always, into the base
//     struct at offset 10, and BEFORE either attribute. That is the ndmFlags
//     parameter.
//  2. NDA_MASTER (:497-501), AFTER NDA_IFINDEX, for `master`/`vrf`. Same rule:
//     it goes here, second, or the byte order stops matching.
func BuildDumpNeighRequestFilter(family, ndmFlags uint8, ifindex, seq uint32) ([]byte, error) {
	hdr := make([]byte, NdMsgSizeCst)
	hdr[0] = family // ndm_family; ndm_ifindex deliberately stays 0, see above
	// ndm_flags, offset 10. Written unconditionally, exactly as :490 does:
	// zero is the value the bare command sends, not the absence of a value.
	hdr[NdMsgFlagsOffCst] = ndmFlags

	var attrs []byte
	if ifindex != 0 {
		var raw [reqAttrBufCst]byte
		ab := NewAttrBuilder(raw[:])
		if err := ab.PutU32(uint16(unix.NDA_IFINDEX), ifindex); err != nil {
			return nil, err
		}
		attrs = ab.Bytes()
	}
	return BuildRequest(uint16(unix.RTM_GETNEIGH), uint16(unix.NLM_F_DUMP), seq, hdr, attrs)
}

// BuildDumpRuleRequest builds `ip rule show`'s only request: RTM_GETRULE with
// NLM_F_DUMP and a fib_rule_hdr whose family byte is the only thing set.
//
// It is rtnl_ruledump_req (lib/libnetlink.c:407-421) and that function is
// unusually short, so it is quoted whole rather than paraphrased:
//
//	struct {
//		struct nlmsghdr nlh;
//		struct fib_rule_hdr frh;
//	} req = {
//		.nlh.nlmsg_len = NLMSG_LENGTH(sizeof(struct fib_rule_hdr)),
//		.nlh.nlmsg_type = RTM_GETRULE,
//		.nlh.nlmsg_flags = NLM_F_DUMP | NLM_F_REQUEST,
//		.nlh.nlmsg_seq = rth->dump = ++rth->seq,
//		.frh.family = family
//	};
//	return send(rth->fd, &req, sizeof(req), 0);
//
// # Three things make this the simplest dump in the corpus
//
// There is no attribute stream, and there CANNOT be one. Every `ip rule show`
// selector — `from`, `to`, `iif`, `oif`, `fwmark`, `pref`, `uidrange` and the
// rest — is applied client-side in filter_nlmsg (ip/iprule.c:98-243), against
// replies; and under strict check the kernel rejects a rule dump carrying any
// attribute at all, `if (nlmsg_attrlen(nlh, sizeof(*frh)))` →
// "Invalid data after header in fib rule dump request"
// (net/core/fib_rules.c:1278-1281). So unlike neigh, where `dev NAME` costs
// eight bytes of NDA_IFINDEX, and unlike route, where `dev NAME` moves a whole
// transaction, every form of `ip rule show` sends these same 28 bytes — and
// the nil attrs argument below is the only value the kernel accepts, not a
// convenience. That makes the request comparison a statement about the family
// byte and nothing else.
//
// Second, there is no ll_init_map. iprule_list_flush_or_save never calls it,
// which is why `ip rule show` prints an FRA_IIFNAME as the string the kernel
// sent rather than resolving an index — the attribute is a NAME on the wire,
// not an ifindex. One transaction, total.
//
// Third, the designated initializer leaves dst_len, src_len, tos, table, res1,
// res2, action and flags all zero, and fib_valid_dumprule_req tests exactly
// that eight-way disjunction (net/core/fib_rules.c:1271-1276) before it will
// dump anything. So the zeros are load-bearing rather than incidental, the
// same relationship BuildDumpNeighRequestFilter documents for ndm_ifindex.
// Note which field is NOT in that list: family. The validator never looks at
// it, so a wrong family byte yields an empty dump rather than an error — the
// quiet failure mode, and the reason the substitution below is worth a test.
//
// # The family byte is never AF_UNSPEC from `ip`, and that is the caller's job
//
// iprule_list_flush_or_save substitutes AF_INET for AF_UNSPEC before it gets
// here (ip/iprule.c:749-752), so a bare `ip rule show` asks for IPv4 rules and
// not for every family. This builder does NOT do that substitution, because
// rtnl_ruledump_req does not: it sends whatever family it is handed. The
// substitution lives with its `ip` counterpart, in internal/goip/obj_rule.go.
// Splitting it that way keeps each function comparable to the C one it is
// named after, and leaves this builder usable for the AF_UNSPEC dump that
// `ip` never sends but the kernel will happily answer.
func BuildDumpRuleRequest(family uint8, seq uint32) ([]byte, error) {
	hdr := make([]byte, FibRuleHdrSizeCst)
	hdr[0] = family // frh.family; every other byte stays zero, see above

	return BuildRequest(uint16(unix.RTM_GETRULE), uint16(unix.NLM_F_DUMP), seq, hdr, nil)
}

// BuildDumpAddrLabelRequest builds the RTM_GETADDRLABEL dump behind
// `ip addrlabel show`: an ifaddrlblmsg whose ifal_family is the caller's and no
// attributes, the bare form rtnl_addrlbldump_req sends (lib/libnetlink.c:365-379).
//
// The AF_UNSPEC -> AF_INET6 substitution iproute2 makes lives with its `ip`
// counterpart in internal/goip/obj_addrlabel.go, not here, for the reason
// BuildDumpRuleRequest documents: this builder sends whatever family it is
// handed, matching the C helper it is named after.
func BuildDumpAddrLabelRequest(family uint8, seq uint32) []byte {
	hdr := make([]byte, IfAddrlblmsgSizeCst)
	hdr[0] = family // ifal_family; every other byte stays zero

	return BuildDumpRequest(uint16(unix.RTM_GETADDRLABEL), seq, hdr)
}

// BuildDumpNeighTblRequest builds the RTM_GETNEIGHTBL dump `ip ntable show`
// sends (rtnl_neightbldump_req): a 4-byte ndtmsg with only ndtm_family set, no
// oversend and no family substitution — the one rule and addrlabel make has no
// parallel here (ipntable passes preferred_family straight through).
func BuildDumpNeighTblRequest(family uint8, seq uint32) []byte {
	hdr := make([]byte, NdtMsgSizeCst)
	hdr[0] = family // ndtm_family; the three pad bytes stay zero

	return BuildDumpRequest(uint16(unix.RTM_GETNEIGHTBL), seq, hdr)
}

// extMaskAttrs encodes a lone IFLA_EXT_MASK, or nothing at all for mask 0.
func extMaskAttrs(extMask uint32) ([]byte, error) {
	if extMask == 0 {
		return nil, nil
	}
	var raw [reqAttrBufCst]byte
	ab := NewAttrBuilder(raw[:])
	if err := ab.PutU32(uint16(unix.IFLA_EXT_MASK), extMask); err != nil {
		return nil, err
	}
	return ab.Bytes(), nil
}

// validIfName applies the kernel's dev_valid_name rules to an IFLA_IFNAME
// selector, plus one rule the kernel cannot express.
//
// The kernel's version is:
//
//	bool dev_valid_name(const char *name) {
//		if (*name == '\0')                        return false;
//		if (strnlen(name, IFNAMSIZ) == IFNAMSIZ)  return false;
//		if (!strcmp(name, ".") || !strcmp(name, "..")) return false;
//		while (*name) {
//			if (*name == '/' || *name == ':' || isspace(*name)) return false;
//			name++;
//		}
//		return true;
//	}
//
// Note ':' as well as '/' — it is excluded because `ip` uses "dev:label"
// syntax for address labels, so a colon in a device name would make an
// argument ambiguous.
//
// # The extra rule: an embedded NUL is rejected
//
// dev_valid_name's loop stops at the first NUL because it reads a C string, so
// the kernel has no way to see anything after one. A Go string can hold one,
// and that difference is a real hazard rather than a theoretical one: PutString
// appends its own terminator, so "lo\x00extra" would go on the wire as
// `lo\0extra\0` and the kernel would read the name as "lo". The request would
// succeed and answer about a *different interface than the caller named*.
// Failing loudly is the only safe answer; silently addressing "lo" is worse
// than an error.
//
// iproute2 never meets this case, because its names come from argv and execve
// cannot pass an embedded NUL. A Go library can be called with anything.
//
// net/core/dev.c dev_valid_name
func validIfName(name string) error {
	if name == "" || len(name) >= IfNameSizeCst {
		return ErrBadIfName
	}
	if name == "." || name == ".." {
		return ErrBadIfName
	}
	if strings.ContainsAny(name, "/: \t\n\v\f\r") {
		return ErrBadIfName
	}
	if strings.IndexByte(name, 0) >= 0 {
		return ErrBadIfName
	}
	return nil
}
