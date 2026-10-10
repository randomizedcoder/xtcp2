// Package service is the shared typed read layer between rtnetlink decoding
// and presentation.  It contains no argv, renderer, or protobuf knowledge.
package service

import (
	"errors"
	"fmt"

	"github.com/randomizedcoder/xtcp2/internal/goip/model"
	"github.com/randomizedcoder/xtcp2/internal/goip/req"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// Source intentionally matches goip's historical one-method source.  Replay
// fixtures and the live socket can therefore drive this layer unchanged.
type Source interface {
	Dump(request []byte, msgType uint16) ([][]byte, error)
}

type TalkSource interface {
	Talk(request []byte, msgType uint16) ([]byte, error)
}

type Service struct {
	src     Source
	nextSeq func() uint32
}

func New(src Source, nextSeq func() uint32) *Service { return &Service{src: src, nextSeq: nextSeq} }

func decode[T any](s *Service, request []byte, typ uint16, name string, parse func([]byte) (T, error)) ([]T, error) {
	bodies, err := s.src.Dump(request, typ)
	if err != nil {
		return nil, err
	}
	out := make([]T, 0, len(bodies))
	for _, body := range bodies {
		v, err := parse(body)
		if err != nil {
			return nil, fmt.Errorf("goip: decode %s: %w", name, err)
		}
		out = append(out, v)
	}
	return out, nil
}

// Links runs the `ip link show` dump.
//
// extMask is req.ExtMaskShow for a bare show and req.ExtMaskStats under `-s`.
// See those constants for why one byte is the whole request-side difference
// between the two commands, and why it is passed rather than inferred.
func (s *Service) Links(extMask uint32) ([]model.Link, error) {
	r, err := req.LinkShowDump(extMask, s.nextSeq())
	if err != nil {
		return nil, fmt.Errorf("goip: build link dump request: %w", err)
	}
	v, err := decode(s, r, uint16(unix.RTM_NEWLINK), "RTM_NEWLINK", xtcpnl.ParseNewLink)
	if err != nil {
		return nil, err
	}
	out := make([]model.Link, len(v))
	for i := range v {
		out[i] = model.Link(v[i])
	}
	// Safe here, unlike in Routes: an RTM_GETLINK dump is already ifindex-
	// ordered, so SortLinks is a no-op against a kernel and only normalizes a
	// replay source that reordered the replies.
	model.SortLinks(out)
	return out, nil
}

// Vrfs runs `ip vrf show`'s dump: the kind-filtered RTM_GETLINK (req.VrfShowDump).
// The kernel ignores the kind filter and answers with the full link list, so this
// returns every decoded link in dump order; the caller selects the VRF devices
// (Kind == "vrf"), exactly as ipvrf_print filters client-side.
func (s *Service) Vrfs() ([]model.Link, error) {
	r, err := req.VrfShowDump(s.nextSeq())
	if err != nil {
		return nil, fmt.Errorf("goip: build vrf dump request: %w", err)
	}
	v, err := decode(s, r, uint16(unix.RTM_NEWLINK), "RTM_NEWLINK", xtcpnl.ParseNewLink)
	if err != nil {
		return nil, err
	}
	out := make([]model.Link, len(v))
	for i := range v {
		out[i] = model.Link(v[i])
	}
	// ipvrf_show prints in dump order; a kernel dump is already ifindex-ordered
	// and SortLinks only normalizes a reordered replay, matching Links.
	model.SortLinks(out)
	return out, nil
}

// linkFromGet is the body every single-get link accessor shares: send the
// caller's request over Talk when the source can do single-gets, otherwise fall
// back to one dump plus exact filtering, and hold both answers to the same
// identity check.
//
// The three accessors below differ only in the request they build and in how
// they recognize the link they asked for, so the talk-or-fall-back spine lives
// here once. selector names what was asked for, and appears in both error
// messages; the reply's own name and index are reported alongside it, because a
// single-get that answers with the wrong link is a kernel- or replay-level
// surprise worth seeing in full.
func (s *Service) linkFromGet(request []byte, match func(model.Link) bool, selector string) (model.Link, error) {
	if talk, ok := s.src.(TalkSource); ok {
		body, err := talk.Talk(request, uint16(unix.RTM_NEWLINK))
		if err != nil {
			return model.Link{}, err
		}
		li, err := xtcpnl.ParseNewLink(body)
		if err != nil {
			return model.Link{}, fmt.Errorf("goip: decode RTM_NEWLINK: %w", err)
		}
		if !match(model.Link(li)) {
			return model.Link{}, fmt.Errorf("goip: link reply %q/%d does not match %s", li.Name, li.Index, selector)
		}
		return model.Link(li), nil
	}
	// ExtMaskShow unconditionally: this fallback stands in for a by-name or
	// by-index single-get, and `-s` does not reach the resolution path that
	// issues those (ll_link_get's mask is hardcoded, lib/ll_map.c:277).
	links, err := s.Links(req.ExtMaskShow)
	if err != nil {
		return model.Link{}, err
	}
	for i := range links {
		if match(links[i]) {
			return links[i], nil
		}
	}
	return model.Link{}, fmt.Errorf("goip: link %s not found", selector)
}

// matchName is the name test both by-name accessors apply to a reply.
//
// Alternative names count: `ip link show dev altname` resolves through the same
// cache, so a reply carrying the requested string in IFLA_PROP_LIST is the link
// that was asked for even though IFLA_IFNAME says otherwise.
func matchName(name string) func(model.Link) bool {
	return func(l model.Link) bool { return l.Name == name || contains(l.AltNames, name) }
}

// LinkByName performs a kernel single-get when the source supports it. Replay
// and legacy dump-only sources fall back to one link dump plus exact filtering.
//
// This is ll_link_get(name, 0) (lib/ll_map.c:264), reached from
// ll_name_to_index on a cache miss (:354-372). Its reply is never printed: the
// caller wants the ifindex, and `ip link show dev NAME` throws the rest away
// and asks again (ip/ipaddress.c:2254, then :2293). See LinkShowDev.
func (s *Service) LinkByName(name string) (model.Link, error) {
	r, err := req.LinkShowByName(name, s.nextSeq())
	if err != nil {
		return model.Link{}, fmt.Errorf("goip: build link get request: %w", err)
	}
	return s.linkFromGet(r, matchName(name), fmt.Sprintf("%q", name))
}

// LinkShowDev performs iplink_get (ip/iplink.c:1497-1515), the second and last
// request `ip link show dev NAME` sends and the one whose reply print_linkinfo
// renders (ip/ipaddress.c:2293).
//
// It is not LinkByName with a different socket. The two requests carry the same
// two attributes in opposite orders and different ifi_family values, and
// pkg/nlparity compares requests for full byte equality — so sending either
// one's bytes twice is a divergence. req.LinkShowDev has the details.
//
// extMask follows `-s`, and this is the only one of the command's two
// requests that does — LinkByName above is hardcoded because ll_link_get is.
func (s *Service) LinkShowDev(name string, extMask uint32) (model.Link, error) {
	r, err := req.LinkShowDev(name, extMask, s.nextSeq())
	if err != nil {
		return model.Link{}, fmt.Errorf("goip: build link get request: %w", err)
	}
	return s.linkFromGet(r, matchName(name), fmt.Sprintf("%q", name))
}

// LinkByIndex performs the index-addressed RTM_GETLINK used after iproute2's
// link-name cache has resolved a `dev NAME` selector.
func (s *Service) LinkByIndex(index int32) (model.Link, error) {
	r, err := req.LinkShowByIndex(index, s.nextSeq())
	if err != nil {
		return model.Link{}, fmt.Errorf("goip: build link get request: %w", err)
	}
	match := func(l model.Link) bool { return l.Index == index }
	return s.linkFromGet(r, match, fmt.Sprintf("index %d", index))
}

func contains(v []string, want string) bool {
	for _, s := range v {
		if s == want {
			return true
		}
	}
	return false
}

// AddressSnapshot performs the link dump before the address dump, matching
// iproute2 and returning both typed collections from one application call.
//
// extMask is the caller's req.ExtMaskShow / req.ExtMaskStats choice, which is
// to say `-s`. It reaches the wire only for AF_UNSPEC and AF_PACKET; the
// family arm of req.AddrShowLinkDump drops it on purpose, and that function's
// doc has the derivation. The replies carry IFLA_STATS and IFLA_STATS64 under
// both the 0x01 mask and no mask at all, so what the caller renders is its own
// decision and not one it can read off this request.
func (s *Service) AddressSnapshot(family uint8, extMask uint32) ([]model.Link, []model.Address, error) {
	r, err := req.AddrShowLinkDump(family, extMask, s.nextSeq())
	if err != nil {
		return nil, nil, fmt.Errorf("goip: build addr link dump request: %w", err)
	}
	ls, err := decode(s, r, uint16(unix.RTM_NEWLINK), "RTM_NEWLINK", xtcpnl.ParseNewLink)
	if err != nil {
		return nil, nil, err
	}
	links := make([]model.Link, len(ls))
	for i := range ls {
		links[i] = model.Link(ls[i])
	}
	var addrs []model.Address
	if family != unix.AF_PACKET {
		addrs, err = s.Addresses(family, 0)
		if err != nil {
			return nil, nil, err
		}
	}
	// Preserve link dump order for the CLI grouping; standalone address lists
	// use SortAddresses before pagination.
	return links, addrs, nil
}

// Addresses runs ip_addr_list (ip/ipaddress.c:2105-2118): the RTM_GETADDR dump
// that every `addr show` form ends with.
//
// ifindex is filter.ifindex — 0 for an unfiltered show, and the index a
// `dev NAME` selector resolved to otherwise. It is passed through to the
// request header rather than applied here, because that is where `ip` puts it;
// see req.AddrShowDump for why one builder serves both and for the socket
// option that decides whether the kernel acts on it.
//
// The replies are returned in dump order and NOT sorted, for the reason
// AddressSnapshot gives: the CLI groups them under links it prints in the
// kernel's own order, so imposing a canonical order here could only make the
// output differ from `ip`'s.
func (s *Service) Addresses(family uint8, ifindex uint32) ([]model.Address, error) {
	as, err := decode(s, req.AddrShowDump(family, ifindex, s.nextSeq()),
		uint16(unix.RTM_NEWADDR), "RTM_NEWADDR", xtcpnl.ParseNewAddr)
	if err != nil {
		return nil, err
	}
	out := make([]model.Address, len(as))
	for i := range as {
		out[i] = model.Address(as[i])
	}
	return out, nil
}

// AddrLinkGet performs ipaddr_link_get (ip/ipaddress.c:2052-2083), the
// by-index single-get `ip addr show dev NAME` sends once ll_name_to_index has
// resolved the selector.
//
// It is not LinkByIndex. That one is ll_link_get's index arm and is
// structurally AF_UNSPEC; this one carries preferred_family, so `-4` and `-6`
// change its header where they cannot change ll_link_get's. Both appear in
// this one command, one byte apart, and pkg/nlparity compares requests for
// full byte equality. req.AddrShowLinkGet has the derivation.
func (s *Service) AddrLinkGet(family uint8, index int32, extMask uint32) (model.Link, error) {
	r, err := req.AddrShowLinkGet(family, index, extMask, s.nextSeq())
	if err != nil {
		return model.Link{}, fmt.Errorf("goip: build addr link get request: %w", err)
	}
	match := func(l model.Link) bool { return l.Index == index }
	return s.linkFromGet(r, match, fmt.Sprintf("index %d", index))
}

// Routes performs the RTM_GETROUTE dump behind `ip route show`, with
// iproute_dump_filter's two optional attributes applied: table (RT_TABLE_UNSPEC
// for none) and oif (0 for none). req.RouteShowDump has the derivation,
// including why `iif` is deliberately absent from this signature.
func (s *Service) Routes(family uint8, table, oif uint32) ([]model.Route, error) {
	r, err := req.RouteShowDump(family, table, oif, s.nextSeq())
	if err != nil {
		return nil, err
	}
	v, err := decode(s, r, uint16(unix.RTM_NEWROUTE), "RTM_NEWROUTE", xtcpnl.ParseNewRoute)
	if err != nil {
		return nil, err
	}
	out := make([]model.Route, len(v))
	for i := range v {
		out[i] = model.Route(v[i])
	}
	// Deliberately NOT model.SortRoutes: `ip route show` prints the kernel's
	// dump order, and for routes the two orders differ. The committed
	// `table all` golden (pkg/xtcpnl/testdata/7_1_4/dumps/ip_route_table_all)
	// is grouped v4-main, v4-local, v6-main, v6-local — family first, table
	// second — because the dump walks fib_trie and then fib6 in turn. Sorting
	// keys on Table first, so it would interleave the two families' main
	// tables ahead of either local table and produce output `ip` never emits.
	//
	// SortRoutes is for callers that need a canonical order across sources
	// regardless of how the kernel happened to walk its tables. Rendering is
	// the opposite requirement, so this path leaves the order alone — the same
	// policy AddressSnapshot follows, and for the same reason.
	return out, nil
}

// NexthopByID performs the RTM_GETNEXTHOP single-get `ip -d route show` sends
// for each distinct RTA_NH_ID (ipnh_cache_add). family is preferred_family.
//
// It mirrors linkFromGet's talk-or-fall-back spine: a live source does the
// kernel single-get, while a dump-only replay source dumps every RTM_NEWNEXTHOP
// and filters to the id — which works because the committed detail pcap records
// the one nexthop reply the capture's `ip -d route show` fetched. Either way the
// reply's own id is checked against the request, because a get that answers with
// a different nexthop is a kernel- or replay-level surprise worth seeing.
func (s *Service) NexthopByID(family uint8, id uint32) (model.Nexthop, error) {
	r, err := req.NexthopGetByID(family, id, s.nextSeq())
	if err != nil {
		return model.Nexthop{}, fmt.Errorf("goip: build nexthop get request: %w", err)
	}
	if talk, ok := s.src.(TalkSource); ok {
		body, terr := talk.Talk(r, uint16(unix.RTM_NEWNEXTHOP))
		if terr != nil {
			return model.Nexthop{}, terr
		}
		nh, perr := xtcpnl.ParseNewNexthop(body)
		if perr != nil {
			return model.Nexthop{}, fmt.Errorf("goip: decode RTM_NEWNEXTHOP: %w", perr)
		}
		if nh.ID != id {
			return model.Nexthop{}, fmt.Errorf("goip: nexthop reply id %d does not match requested %d", nh.ID, id)
		}
		return model.Nexthop(nh), nil
	}

	bodies, derr := s.src.Dump(r, uint16(unix.RTM_NEWNEXTHOP))
	if derr != nil {
		return model.Nexthop{}, derr
	}
	for _, body := range bodies {
		nh, perr := xtcpnl.ParseNewNexthop(body)
		if perr != nil {
			return model.Nexthop{}, fmt.Errorf("goip: decode RTM_NEWNEXTHOP: %w", perr)
		}
		if nh.ID == id {
			return model.Nexthop(nh), nil
		}
	}
	return model.Nexthop{}, fmt.Errorf("goip: no nexthop with id %d in the capture", id)
}

// Nexthops is the RTM_GETNEXTHOP dump behind `ip nexthop show`: every nexthop
// object the kernel walks, in dump order. family is preferred_family.
//
// It leaves the order alone for the same reason Routes does — `ip nexthop show`
// prints the kernel's walk, not a sorted view — and does not filter, because the
// bare command sends no selectors.
func (s *Service) Nexthops(family uint8) ([]model.Nexthop, error) {
	r, err := req.NexthopDump(family, s.nextSeq())
	if err != nil {
		return nil, fmt.Errorf("goip: build nexthop dump request: %w", err)
	}
	v, err := decode(s, r, uint16(unix.RTM_NEWNEXTHOP), "RTM_NEWNEXTHOP", xtcpnl.ParseNewNexthop)
	if err != nil {
		return nil, err
	}
	out := make([]model.Nexthop, len(v))
	for i := range v {
		out[i] = model.Nexthop(v[i])
	}
	return out, nil
}

// NexthopShowLinks is ll_init_map's link dump before a filtered `ip nexthop show
// { dev | master | vrf }`. It is the nexthop-path twin of NeighborLinks: the same
// ll_init_map request, split out so a `dev`/`master`/`vrf` name is resolved from
// its replies between the link dump and the filtered nexthop dump.
func (s *Service) NexthopShowLinks() ([]model.Link, error) {
	r, err := req.NexthopShowLinkDump(s.nextSeq())
	if err != nil {
		return nil, fmt.Errorf("goip: build nexthop link dump request: %w", err)
	}
	ls, err := decode(s, r, uint16(unix.RTM_NEWLINK), "RTM_NEWLINK", xtcpnl.ParseNewLink)
	if err != nil {
		return nil, err
	}
	links := make([]model.Link, len(ls))
	for i := range ls {
		links[i] = model.Link(ls[i])
	}
	return links, nil
}

// NexthopsFiltered is the wire-filtered RTM_GETNEXTHOP dump behind
// `ip nexthop show { dev | master | vrf | groups | fdb }`: the kernel narrows the
// set by the filter attrs, and the replies are decoded in dump order exactly as
// Nexthops does for the bare command. `protocol` is a client-side filter the
// caller applies after render, not part of this request.
func (s *Service) NexthopsFiltered(family uint8, f xtcpnl.NexthopDumpFilter) ([]model.Nexthop, error) {
	r, err := req.NexthopDumpFiltered(family, f, s.nextSeq())
	if err != nil {
		return nil, fmt.Errorf("goip: build nexthop dump request: %w", err)
	}
	v, err := decode(s, r, uint16(unix.RTM_NEWNEXTHOP), "RTM_NEWNEXTHOP", xtcpnl.ParseNewNexthop)
	if err != nil {
		return nil, err
	}
	out := make([]model.Nexthop, len(v))
	for i := range v {
		out[i] = model.Nexthop(v[i])
	}
	return out, nil
}

// Neighbors is the RTM_GETNEIGH dump, optionally filtered to one interface and
// optionally asking for the proxy table instead of the neighbor table.
//
// ifindex 0 is the unfiltered form; see req.NeighShowDump for why the index
// travels as an NDA_IFINDEX attribute rather than in ndm_ifindex.
//
// ndmFlags is NTF_PROXY or zero. It is not a filter over these replies — it
// picks which table the kernel walks — so a caller cannot get the same answer
// by passing zero and discarding rows afterwards.
func (s *Service) Neighbors(family, ndmFlags uint8, ifindex uint32) ([]model.Neighbor, error) {
	r, err := req.NeighShowDump(family, ndmFlags, ifindex, s.nextSeq())
	if err != nil {
		return nil, fmt.Errorf("goip: build neighbor dump request: %w", err)
	}
	v, err := decode(s, r, uint16(unix.RTM_NEWNEIGH), "RTM_NEWNEIGH", xtcpnl.ParseNeigh)
	if err != nil {
		return nil, err
	}
	out := make([]model.Neighbor, len(v))
	for i := range v {
		out[i] = model.Neighbor(v[i])
	}
	model.SortNeighbors(out)
	return out, nil
}

// NeighborLinks is ll_init_map's link dump (ip/ipneigh.c:597), which every
// `ip neigh show` sends before the neighbor dump whether or not a device was
// named.
//
// It is split out from the neighbor dump rather than paired with it because
// `dev NAME` is resolved BETWEEN the two, out of this dump's own replies —
// which is the whole reason the selector costs no extra transaction. A
// combined call would have to take the name and duplicate the index cache's
// lookup to do it.
func (s *Service) NeighborLinks() ([]model.Link, error) {
	r, err := req.NeighShowLinkDump(s.nextSeq())
	if err != nil {
		return nil, fmt.Errorf("goip: build neighbor link dump request: %w", err)
	}
	ls, err := decode(s, r, uint16(unix.RTM_NEWLINK), "RTM_NEWLINK", xtcpnl.ParseNewLink)
	if err != nil {
		return nil, err
	}
	links := make([]model.Link, len(ls))
	for i := range ls {
		links[i] = model.Link(ls[i])
	}
	return links, nil
}

// Rules is `ip rule show`'s single dump.
//
// The shortest method in this file, and the shape is the point: no
// ll_init_map beforehand, no by-index side-gets afterwards, and no filtering
// argument, because iprule_list_flush_or_save applies every selector
// client-side. Compare Neighbors, whose caller must send NeighborLinks first,
// and Routes, whose replies can each cost a side-get to name an interface.
//
// The replies are left in wire order. The kernel's rule list is maintained in
// preference order, so that IS the order `ip` prints; see model.Rule for why
// there is no SortRules.
func (s *Service) Rules(family uint8) ([]model.Rule, error) {
	r, err := req.RuleShowDump(family, s.nextSeq())
	if err != nil {
		return nil, fmt.Errorf("goip: build rule dump request: %w", err)
	}
	v, err := decode(s, r, uint16(unix.RTM_NEWRULE), "RTM_NEWRULE", xtcpnl.ParseRule)
	if err != nil {
		return nil, err
	}
	out := make([]model.Rule, len(v))
	for i := range v {
		out[i] = model.Rule(v[i])
	}
	return out, nil
}

// AddrLabels runs the `ip addrlabel show` dump (RTM_GETADDRLABEL). No sort: the
// kernel dump order is stable and iproute2 renders it as-is.
func (s *Service) AddrLabels(family uint8) ([]model.AddrLabel, error) {
	r := req.AddrLabelShowDump(family, s.nextSeq())
	v, err := decode(s, r, uint16(unix.RTM_NEWADDRLABEL), "RTM_NEWADDRLABEL", xtcpnl.ParseNewAddrLabel)
	if err != nil {
		return nil, err
	}
	out := make([]model.AddrLabel, len(v))
	for i := range v {
		out[i] = model.AddrLabel(v[i])
	}
	return out, nil
}

// NeighTableLinks is ll_init_map's link dump before the ntable dump
// (ip/ipntable.c:685), the NeighborLinks twin. It is split out so obj_ntable can
// Fill the index cache between the link dump and the table dump, which is how a
// device-specific parameter set's NDTPA_IFINDEX resolves with no extra request.
func (s *Service) NeighTableLinks() ([]model.Link, error) {
	r, err := req.NeighTblShowLinkDump(s.nextSeq())
	if err != nil {
		return nil, fmt.Errorf("goip: build ntable link dump request: %w", err)
	}
	ls, err := decode(s, r, uint16(unix.RTM_NEWLINK), "RTM_NEWLINK", xtcpnl.ParseNewLink)
	if err != nil {
		return nil, err
	}
	links := make([]model.Link, len(ls))
	for i := range ls {
		links[i] = model.Link(ls[i])
	}
	return links, nil
}

// NeighTables is `ip ntable show`'s dump. Like Rules it has no Sort companion:
// the kernel's table list is in a stable order, so wire order is render order.
func (s *Service) NeighTables(family uint8) ([]model.NeighTbl, error) {
	r := req.NeighTblShowDump(family, s.nextSeq())
	v, err := decode(s, r, uint16(unix.RTM_NEWNEIGHTBL), "RTM_NEWNEIGHTBL", xtcpnl.ParseNewNeighTbl)
	if err != nil {
		return nil, err
	}
	out := make([]model.NeighTbl, len(v))
	for i := range v {
		out[i] = model.NeighTbl(v[i])
	}
	return out, nil
}

// NetconfLinks is ll_init_map's link dump (ip/ipnetconf.c:186), called
// unconditionally before the netconf transaction, the NeighTableLinks twin. It is
// split out so obj_netconf can Fill the index cache between the link dump and the
// netconf dump, which is how NETCONFA_IFINDEX resolves to a name — and how a
// `dev NAME` selector resolves to an ifindex — with no extra request.
func (s *Service) NetconfLinks() ([]model.Link, error) {
	r, err := req.NetconfShowLinkDump(s.nextSeq())
	if err != nil {
		return nil, fmt.Errorf("goip: build netconf link dump request: %w", err)
	}
	ls, err := decode(s, r, uint16(unix.RTM_NEWLINK), "RTM_NEWLINK", xtcpnl.ParseNewLink)
	if err != nil {
		return nil, err
	}
	links := make([]model.Link, len(ls))
	for i := range ls {
		links[i] = model.Link(ls[i])
	}
	return links, nil
}

// Netconfs is `ip netconf show`'s dump. family is preferred_family: AF_UNSPEC for
// a bare show, which a modern kernel answers with every family (inet, inet6, and
// mpls if loaded) in one dump. Like NeighTables it does no sorting — wire order is
// render order.
//
// The two-pass fallback mirrors do_show's goto-dump loop (ip/ipnetconf.c:200-227):
// a kernel too old to dump AF_UNSPEC answers EOPNOTSUPP, so the request is retried
// as AF_INET and then AF_INET6 and the results concatenated. That path cannot run
// on a kernel new enough to dump AF_UNSPEC and is uncapturable here, so it is
// tested as a contract (a fake error-injecting Source), not claimed as
// capture-grounded. EOPNOTSUPP matches through the error chain because netlinkErr
// wraps syscall.Errno and NetlinkSource.Dump wraps it with %w.
func (s *Service) Netconfs(family uint8) ([]model.Netconf, error) {
	if family != unix.AF_UNSPEC {
		return s.netconfDump(family)
	}
	out, err := s.netconfDump(unix.AF_UNSPEC)
	if err == nil {
		return out, nil
	}
	if !errors.Is(err, unix.EOPNOTSUPP) {
		return nil, err
	}
	v4, err := s.netconfDump(unix.AF_INET)
	if err != nil {
		return nil, err
	}
	v6, err := s.netconfDump(unix.AF_INET6)
	if err != nil {
		return nil, err
	}
	return append(v4, v6...), nil
}

func (s *Service) netconfDump(family uint8) ([]model.Netconf, error) {
	r := req.NetconfShowDump(family, s.nextSeq())
	v, err := decode(s, r, uint16(unix.RTM_NEWNETCONF), "RTM_NEWNETCONF", xtcpnl.ParseNewNetconf)
	if err != nil {
		return nil, err
	}
	out := make([]model.Netconf, len(v))
	for i := range v {
		out[i] = model.Netconf(v[i])
	}
	return out, nil
}

// NetconfByIndex is the non-dump RTM_GETNETCONF point get `ip -4 netconf show dev
// X` sends (ip/ipnetconf.c:188-198). It mirrors NexthopByID's talk-or-fall-back
// spine: a live source does the kernel single-get, a dump-only replay source (no
// Talk) dumps every RTM_NEWNETCONF and filters to the ifindex — the committed
// dev4 pcap records the one reply the capture's point get fetched. The reply's own
// NETCONFA_IFINDEX is checked against the request.
func (s *Service) NetconfByIndex(family uint8, ifindex int32) (model.Netconf, error) {
	r, err := req.NetconfGetByIndex(family, ifindex, s.nextSeq())
	if err != nil {
		return model.Netconf{}, fmt.Errorf("goip: build netconf get request: %w", err)
	}
	if talk, ok := s.src.(TalkSource); ok {
		body, terr := talk.Talk(r, uint16(unix.RTM_NEWNETCONF))
		if terr != nil {
			return model.Netconf{}, terr
		}
		ni, perr := xtcpnl.ParseNewNetconf(body)
		if perr != nil {
			return model.Netconf{}, fmt.Errorf("goip: decode RTM_NEWNETCONF: %w", perr)
		}
		if !ni.HasIfindex || ni.Ifindex != ifindex {
			return model.Netconf{}, fmt.Errorf("goip: netconf reply ifindex %d does not match requested %d", ni.Ifindex, ifindex)
		}
		return model.Netconf(ni), nil
	}

	bodies, derr := s.src.Dump(r, uint16(unix.RTM_NEWNETCONF))
	if derr != nil {
		return model.Netconf{}, derr
	}
	for _, body := range bodies {
		ni, perr := xtcpnl.ParseNewNetconf(body)
		if perr != nil {
			return model.Netconf{}, fmt.Errorf("goip: decode RTM_NEWNETCONF: %w", perr)
		}
		if ni.HasIfindex && ni.Ifindex == ifindex {
			return model.Netconf(ni), nil
		}
	}
	return model.Netconf{}, fmt.Errorf("goip: no netconf record for ifindex %d in the capture", ifindex)
}

// StatsLinks is the link dump ipstats triggers lazily to resolve ifindex to name
// (ip/ipstats.c:761), the NetconfLinks twin. It is split out so obj_stats can Fill
// the index cache between the link dump and the stats dump — how a stats record's
// ifindex resolves to a name, and a `dev NAME` selector to an ifindex.
func (s *Service) StatsLinks() ([]model.Link, error) {
	r, err := req.StatsShowLinkDump(s.nextSeq())
	if err != nil {
		return nil, fmt.Errorf("goip: build stats link dump request: %w", err)
	}
	ls, err := decode(s, r, uint16(unix.RTM_NEWLINK), "RTM_NEWLINK", xtcpnl.ParseNewLink)
	if err != nil {
		return nil, err
	}
	links := make([]model.Link, len(ls))
	for i := range ls {
		links[i] = model.Link(ls[i])
	}
	return links, nil
}

// IfStats is `ip stats show group link`'s dump. Unlike Netconfs it is a plain
// single dump: stats has no EOPNOTSUPP family fallback (the request is always
// PF_UNSPEC and the kernel answers every interface). It does no sorting — wire
// order (ifindex) is render order.
func (s *Service) IfStats() ([]model.IfStats, error) {
	r := req.StatsShowDump(s.nextSeq())
	v, err := decode(s, r, uint16(unix.RTM_NEWSTATS), "RTM_NEWSTATS", xtcpnl.ParseNewStats)
	if err != nil {
		return nil, err
	}
	out := make([]model.IfStats, len(v))
	for i := range v {
		out[i] = model.IfStats(v[i])
	}
	return out, nil
}

// IfStatsXstats is `ip stats show group xstats`'s dump: the same plain single
// dump as IfStats but requesting the link-xstats group (filter_mask 0x2). The
// decode is the same ParseNewStats, which descends the bridge vlan/mcast bodies.
func (s *Service) IfStatsXstats() ([]model.IfStats, error) {
	r := req.StatsShowXstatsDump(s.nextSeq())
	v, err := decode(s, r, uint16(unix.RTM_NEWSTATS), "RTM_NEWSTATS", xtcpnl.ParseNewStats)
	if err != nil {
		return nil, err
	}
	out := make([]model.IfStats, len(v))
	for i := range v {
		out[i] = model.IfStats(v[i])
	}
	return out, nil
}

// IfStatsByIndex is the non-dump RTM_GETSTATS point get `ip stats show group link
// dev X` sends (ip/ipstats.c:831-851). It mirrors NetconfByIndex's talk-or-fall-
// back spine: a live source does the kernel single-get, a dump-only replay source
// (no Talk) dumps every RTM_NEWSTATS and filters to the ifindex — the committed
// dev pcap records the one reply the capture's point get fetched.
func (s *Service) IfStatsByIndex(ifindex uint32) (model.IfStats, error) {
	r, err := req.StatsGetByIndex(ifindex, s.nextSeq())
	if err != nil {
		return model.IfStats{}, fmt.Errorf("goip: build stats get request: %w", err)
	}
	if talk, ok := s.src.(TalkSource); ok {
		body, terr := talk.Talk(r, uint16(unix.RTM_NEWSTATS))
		if terr != nil {
			return model.IfStats{}, terr
		}
		si, perr := xtcpnl.ParseNewStats(body)
		if perr != nil {
			return model.IfStats{}, fmt.Errorf("goip: decode RTM_NEWSTATS: %w", perr)
		}
		if si.Ifindex != ifindex {
			return model.IfStats{}, fmt.Errorf("goip: stats reply ifindex %d does not match requested %d", si.Ifindex, ifindex)
		}
		return model.IfStats(si), nil
	}

	bodies, derr := s.src.Dump(r, uint16(unix.RTM_NEWSTATS))
	if derr != nil {
		return model.IfStats{}, derr
	}
	for _, body := range bodies {
		si, perr := xtcpnl.ParseNewStats(body)
		if perr != nil {
			return model.IfStats{}, fmt.Errorf("goip: decode RTM_NEWSTATS: %w", perr)
		}
		if si.Ifindex == ifindex {
			return model.IfStats(si), nil
		}
	}
	return model.IfStats{}, fmt.Errorf("goip: no stats record for ifindex %d in the capture", ifindex)
}
