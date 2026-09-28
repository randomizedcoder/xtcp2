// Package model contains the transport-neutral resources returned by the
// goip read application.  They deliberately have the decoder's field layout:
// conversion to protobuf or an iproute2 render view belongs at the edges.
package model

import (
	"bytes"
	"sort"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

type Link xtcpnl.LinkInfo
type Address xtcpnl.AddrInfo
type Route xtcpnl.RouteInfo
type Neighbor xtcpnl.NeighInfo

func SortLinks(v []Link) { sort.SliceStable(v, func(i, j int) bool { return v[i].Index < v[j].Index }) }

func SortAddresses(v []Address) {
	sort.SliceStable(v, func(i, j int) bool {
		a, b := &v[i], &v[j]
		if a.Index != b.Index {
			return a.Index < b.Index
		}
		if a.Family != b.Family {
			return a.Family < b.Family
		}
		if c := bytes.Compare(a.Local, b.Local); c != 0 {
			return c < 0
		}
		return a.Prefixlen < b.Prefixlen
	})
}

func SortRoutes(v []Route) {
	sort.SliceStable(v, func(i, j int) bool {
		a, b := &v[i], &v[j]
		if a.Table != b.Table {
			return a.Table < b.Table
		}
		if a.Family != b.Family {
			return a.Family < b.Family
		}
		if c := bytes.Compare(a.Dst, b.Dst); c != 0 {
			return c < 0
		}
		if a.DstLen != b.DstLen {
			return a.DstLen < b.DstLen
		}
		if a.Priority != b.Priority {
			return a.Priority < b.Priority
		}
		if a.Type != b.Type {
			return a.Type < b.Type
		}
		if a.Oif != b.Oif {
			return a.Oif < b.Oif
		}
		return bytes.Compare(a.Gateway, b.Gateway) < 0
	})
}

func SortNeighbors(v []Neighbor) {
	sort.SliceStable(v, func(i, j int) bool {
		a, b := &v[i], &v[j]
		if a.Ifindex != b.Ifindex {
			return a.Ifindex < b.Ifindex
		}
		if a.Family != b.Family {
			return a.Family < b.Family
		}
		return bytes.Compare(a.Dst, b.Dst) < 0
	})
}
