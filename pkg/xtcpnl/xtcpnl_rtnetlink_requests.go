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
//	BuildDumpRouteRequestTable   ip/iproute.c     iproute_dump_filter
//	BuildDumpNeighRequest        lib/libnetlink.c rtnl_neighdump_req
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

// BuildDumpRouteRequestTable builds an RTM_GETROUTE dump for one routing table,
// via an RTA_TABLE attribute.
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
// One captured form (getroute.pcap rec 0, `ip route show table all`): 28 bytes,
// an all-zero rtmsg, no attributes. Note the family there is AF_UNSPEC, while
// plain `ip route show` sends AF_INET — ip/iproute.c:1998 promotes AF_UNSPEC to
// AF_INET whenever a table filter is set, so the default command is
// (AF_INET, 254) and the all-tables command is (AF_UNSPEC, 0). Both are this
// one function.
func BuildDumpRouteRequestTable(family uint8, table, seq uint32) ([]byte, error) {
	hdr := make([]byte, RtMsgSizeCst)
	hdr[0] = family // rtm_family

	var attrs []byte
	if table != uint32(unix.RT_TABLE_UNSPEC) {
		var raw [reqAttrBufCst]byte
		ab := NewAttrBuilder(raw[:])
		if err := ab.PutU32(uint16(unix.RTA_TABLE), table); err != nil {
			return nil, err
		}
		attrs = ab.Bytes()
	}
	return BuildRequest(uint16(unix.RTM_GETROUTE), uint16(unix.NLM_F_DUMP), seq, hdr, attrs)
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
