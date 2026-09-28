// Package service is the shared typed read layer between rtnetlink decoding
// and presentation.  It contains no argv, renderer, or protobuf knowledge.
package service

import (
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
func (s *Service) AddressSnapshot(family uint8) ([]model.Link, []model.Address, error) {
	r, err := req.AddrShowLinkDump(family, s.nextSeq())
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
		as, err := decode(s, req.AddrShowDump(family, s.nextSeq()), uint16(unix.RTM_NEWADDR), "RTM_NEWADDR", xtcpnl.ParseNewAddr)
		if err != nil {
			return nil, nil, err
		}
		addrs = make([]model.Address, len(as))
		for i := range as {
			addrs[i] = model.Address(as[i])
		}
	}
	// Preserve link dump order for the CLI grouping; standalone address lists
	// use SortAddresses before pagination.
	return links, addrs, nil
}

func (s *Service) Routes(family uint8, table uint32) ([]model.Route, error) {
	r, err := req.RouteShowDump(family, table, s.nextSeq())
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

func (s *Service) Neighbors(family uint8) ([]model.Neighbor, error) {
	v, err := decode(s, req.NeighShowDump(family, s.nextSeq()), uint16(unix.RTM_NEWNEIGH), "RTM_NEWNEIGH", xtcpnl.ParseNeigh)
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

// NeighborSnapshot obtains the interface-name map before the neighbor dump,
// preserving iproute2's transaction order.
func (s *Service) NeighborSnapshot(family uint8) ([]model.Link, []model.Neighbor, error) {
	r, err := req.NeighShowLinkDump(s.nextSeq())
	if err != nil {
		return nil, nil, fmt.Errorf("goip: build neighbor link dump request: %w", err)
	}
	ls, err := decode(s, r, uint16(unix.RTM_NEWLINK), "RTM_NEWLINK", xtcpnl.ParseNewLink)
	if err != nil {
		return nil, nil, err
	}
	links := make([]model.Link, len(ls))
	for i := range ls {
		links[i] = model.Link(ls[i])
	}
	neighbors, err := s.Neighbors(family)
	if err != nil {
		return nil, nil, err
	}
	return links, neighbors, nil
}
