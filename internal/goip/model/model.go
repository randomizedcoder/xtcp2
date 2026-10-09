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

// Nexthop is a nexthop object (RTM_NEWNEXTHOP), the thing a route's RTA_NH_ID
// delegates its next hop to. It has no Sort companion: `ip -d route show`
// fetches one by id per route, never a list to order.
type Nexthop xtcpnl.NexthopInfo

// Rule is a routing policy database entry. It has no SortRules companion, and
// that absence is deliberate rather than pending.
//
// The kernel keeps each family's rule list ordered by preference —
// fib_nl_newrule walks the list and inserts ahead of the first rule with a
// higher pref (net/core/fib_rules.c) — and fib_nl_dumprule walks that same
// list. So wire order IS preference order, and a sort here could only agree
// with it or invent an order `ip` does not use. SortNeighbors exists because
// the neighbor hash has no such property and its order varies across boots.
type Rule xtcpnl.RuleInfo

// AddrLabel is one entry of the IPv6 address-label policy table
// (RTM_NEWADDRLABEL). It has no Sort companion: the kernel dumps the per-netns
// table in a stable order (net/ipv6/addrlabel.c) and iproute2 does no sorting,
// so wire order is the order to render.
type AddrLabel xtcpnl.AddrLabelInfo

// NeighTbl is one neighbor table of the RTM_GETNEIGHTBL dump
// (RTM_NEWNEIGHTBL). Like AddrLabel it has no Sort companion: the kernel dumps
// the table list in a stable order (per-family tables then device-specific
// parameter sets) and iproute2 does no sorting, so wire order is render order.
type NeighTbl xtcpnl.NeighTblInfo

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
