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

func (s *Service) Links() ([]model.Link, error) {
	r, err := req.LinkShowDump(s.nextSeq())
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
	model.SortLinks(out)
	return out, nil
}

// LinkByName performs a kernel single-get when the source supports it. Replay
// and legacy dump-only sources fall back to one link dump plus exact filtering.
func (s *Service) LinkByName(name string) (model.Link, error) {
	r, err := req.LinkShowByName(name, s.nextSeq())
	if err != nil {
		return model.Link{}, fmt.Errorf("goip: build link get request: %w", err)
	}
	if talk, ok := s.src.(TalkSource); ok {
		body, err := talk.Talk(r, uint16(unix.RTM_NEWLINK))
		if err != nil {
			return model.Link{}, err
		}
		li, err := xtcpnl.ParseNewLink(body)
		if err != nil {
			return model.Link{}, fmt.Errorf("goip: decode RTM_NEWLINK: %w", err)
		}
		if li.Name == name || contains(li.AltNames, name) {
			return model.Link(li), nil
		}
		return model.Link{}, fmt.Errorf("goip: link reply does not match %q", name)
	}
	links, err := s.Links()
	if err != nil {
		return model.Link{}, err
	}
	for i := range links {
		if links[i].Name == name || contains(links[i].AltNames, name) {
			return links[i], nil
		}
	}
	return model.Link{}, fmt.Errorf("goip: link %q not found", name)
}

// LinkByIndex performs the index-addressed RTM_GETLINK used after iproute2's
// link-name cache has resolved a `dev NAME` selector.
func (s *Service) LinkByIndex(index int32) (model.Link, error) {
	r, err := req.LinkShowByIndex(index, s.nextSeq())
	if err != nil {
		return model.Link{}, fmt.Errorf("goip: build link get request: %w", err)
	}
	if talk, ok := s.src.(TalkSource); ok {
		body, err := talk.Talk(r, uint16(unix.RTM_NEWLINK))
		if err != nil {
			return model.Link{}, err
		}
		li, err := xtcpnl.ParseNewLink(body)
		if err != nil {
			return model.Link{}, fmt.Errorf("goip: decode RTM_NEWLINK: %w", err)
		}
		if li.Index != index {
			return model.Link{}, fmt.Errorf("goip: link reply index %d, want %d", li.Index, index)
		}
		return model.Link(li), nil
	}
	links, err := s.Links()
	if err != nil {
		return model.Link{}, err
	}
	for i := range links {
		if links[i].Index == index {
			return links[i], nil
		}
	}
	return model.Link{}, fmt.Errorf("goip: link index %d not found", index)
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
	model.SortRoutes(out)
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
