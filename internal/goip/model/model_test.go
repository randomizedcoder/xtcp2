package model

import (
	"reflect"
	"testing"
)

// The Sort* functions give the read layer an order that does not depend on how
// a source happened to emit its replies. That is useful for comparing two
// sources and useless for rendering, because `ip` prints the kernel's dump
// order — see service.Routes for the one place where the two orders actually
// disagree. These tests pin the documented key order of each function, and the
// SortRoutes table includes the row that demonstrates the disagreement.

func TestSortLinks(t *testing.T) {
	const stabilityRow = "corner: equal ifindexes keep their relative input order"

	tests := []struct {
		description string
		in          []Link
		wantIndexes []int32
	}{
		{
			description: "positive: links are ordered by ifindex",
			in:          []Link{{Index: 9}, {Index: 1}, {Index: 4}},
			wantIndexes: []int32{1, 4, 9},
		},
		{
			description: "negative: an already-ordered slice is left alone",
			in:          []Link{{Index: 1}, {Index: 2}, {Index: 3}},
			wantIndexes: []int32{1, 2, 3},
		},
		{
			description: "boundary: an empty slice sorts without panicking",
			in:          []Link{},
			wantIndexes: []int32{},
		},
		{
			description: "boundary: a single link is unchanged",
			in:          []Link{{Index: 161}},
			wantIndexes: []int32{161},
		},
		{
			// SliceStable, so equal keys keep their input order. Two links
			// cannot really share an ifindex, but a replay source stitched from
			// two captures can present the same index twice, and dropping to an
			// unstable sort would make that output vary run to run.
			description: stabilityRow,
			in:          []Link{{Index: 2, Name: "first"}, {Index: 1}, {Index: 2, Name: "second"}},
			wantIndexes: []int32{1, 2, 2},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			SortLinks(tc.in)
			got := make([]int32, 0, len(tc.in))
			for i := range tc.in {
				got = append(got, tc.in[i].Index)
			}
			if !reflect.DeepEqual(got, tc.wantIndexes) {
				t.Errorf("indexes = %v, want %v", got, tc.wantIndexes)
			}
			if tc.description == stabilityRow && (tc.in[1].Name != "first" || tc.in[2].Name != "second") {
				t.Errorf("stability lost: %q then %q", tc.in[1].Name, tc.in[2].Name)
			}
		})
	}
}

func TestSortRoutes(t *testing.T) {
	// Each route is tagged through Oif so the assertion can name the expected
	// permutation without restating every field.
	tests := []struct {
		description string
		in          []Route
		wantOrder   []uint32
	}{
		{
			description: "positive: table wins over family, family over destination",
			in: []Route{
				{Oif: 1, Table: 254, Family: 10, Dst: []byte{2}, Priority: 2},
				{Oif: 2, Table: 100, Family: 2, Dst: []byte{3}},
				{Oif: 3, Table: 254, Family: 2, Dst: []byte{1}, Priority: 1},
			},
			wantOrder: []uint32{2, 3, 1},
		},
		{
			// The row this test exists for. The input below is the order the
			// kernel dumps `route show table all` in — v4 main, v4 local, v6
			// main, v6 local, as the committed ip_route_table_all golden shows
			// — and SortRoutes moves the v6 main route ahead of the v4 local
			// one, because Table outranks Family. Rendering must therefore not
			// call this function; see the comment in service.Routes.
			description: "negative: the kernel's table-all dump order is NOT a fixed point of SortRoutes",
			in: []Route{
				{Oif: 1, Table: 254, Family: 2, Dst: []byte{192, 0, 2, 0}},
				{Oif: 2, Table: 255, Family: 2, Dst: []byte{127, 0, 0, 0}},
				{Oif: 3, Table: 254, Family: 10, Dst: []byte{0x20, 0x01}},
				{Oif: 4, Table: 255, Family: 10, Dst: []byte{0, 1}},
			},
			wantOrder: []uint32{1, 3, 2, 4},
		},
		{
			description: "boundary: an empty slice sorts without panicking",
			in:          []Route{},
			wantOrder:   []uint32{},
		},
		{
			// Dst is compared with bytes.Compare, so a nil destination — a
			// default route — sorts ahead of every prefix in the same table and
			// family, which is where `ip` prints it too.
			description: "boundary: a nil Dst (the default route) sorts first within its table and family",
			in: []Route{
				{Oif: 1, Table: 254, Family: 2, Dst: []byte{10, 0, 0, 0}, DstLen: 8},
				{Oif: 2, Table: 254, Family: 2, Dst: nil, DstLen: 0},
			},
			wantOrder: []uint32{2, 1},
		},
		{
			// Same table, family and destination bytes: the tiebreakers run in
			// order — DstLen, then Priority, then Type, then Oif, then Gateway.
			// Without them two routes to one prefix would compare equal and
			// their order would depend on the input.
			description: "corner: routes sharing table, family and Dst fall through to DstLen then Priority",
			in: []Route{
				{Oif: 1, Table: 254, Family: 2, Dst: []byte{10, 0, 0, 0}, DstLen: 8, Priority: 200},
				{Oif: 2, Table: 254, Family: 2, Dst: []byte{10, 0, 0, 0}, DstLen: 8, Priority: 100},
				{Oif: 3, Table: 254, Family: 2, Dst: []byte{10, 0, 0, 0}, DstLen: 24},
			},
			wantOrder: []uint32{2, 1, 3},
		},
		{
			// bytes.Compare is byte-wise, so a shorter Dst that is a prefix of
			// a longer one sorts first regardless of DstLen. Comparing raw
			// address bytes across families would be meaningless, which is why
			// Family is ranked above Dst.
			description: "corner: a Dst that is a byte-prefix of another sorts ahead of it",
			in: []Route{
				{Oif: 1, Table: 254, Family: 2, Dst: []byte{10, 0}},
				{Oif: 2, Table: 254, Family: 2, Dst: []byte{10}},
			},
			wantOrder: []uint32{2, 1},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			SortRoutes(tc.in)
			got := make([]uint32, 0, len(tc.in))
			for i := range tc.in {
				got = append(got, tc.in[i].Oif)
			}
			if !reflect.DeepEqual(got, tc.wantOrder) {
				t.Errorf("order = %v, want %v", got, tc.wantOrder)
			}
		})
	}
}

func TestSortAddresses(t *testing.T) {
	tests := []struct {
		description string
		in          []Address
		wantOrder   []uint8 // tagged through Scope
	}{
		{
			description: "positive: ifindex wins over family, family over local address",
			in: []Address{
				{Scope: 1, Index: 2, Family: 10, Local: []byte{0x20}},
				{Scope: 2, Index: 1, Family: 2, Local: []byte{127, 0, 0, 1}},
				{Scope: 3, Index: 2, Family: 2, Local: []byte{10, 0, 0, 1}},
			},
			wantOrder: []uint8{2, 3, 1},
		},
		{
			description: "negative: an already-ordered slice is left alone",
			in: []Address{
				{Scope: 1, Index: 1, Family: 2, Local: []byte{127, 0, 0, 1}},
				{Scope: 2, Index: 2, Family: 2, Local: []byte{10, 0, 0, 1}},
			},
			wantOrder: []uint8{1, 2},
		},
		{
			description: "boundary: an empty slice sorts without panicking",
			in:          []Address{},
			wantOrder:   []uint8{},
		},
		{
			// Prefixlen is the last tiebreaker, so two addresses with the same
			// local address and different masks — a real configuration — have a
			// defined order rather than an input-dependent one.
			description: "corner: an identical Local falls through to Prefixlen",
			in: []Address{
				{Scope: 1, Index: 2, Family: 2, Local: []byte{10, 0, 0, 1}, Prefixlen: 24},
				{Scope: 2, Index: 2, Family: 2, Local: []byte{10, 0, 0, 1}, Prefixlen: 8},
			},
			wantOrder: []uint8{2, 1},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			SortAddresses(tc.in)
			got := make([]uint8, 0, len(tc.in))
			for i := range tc.in {
				got = append(got, tc.in[i].Scope)
			}
			if !reflect.DeepEqual(got, tc.wantOrder) {
				t.Errorf("order = %v, want %v", got, tc.wantOrder)
			}
		})
	}
}

func TestSortNeighbors(t *testing.T) {
	tests := []struct {
		description string
		in          []Neighbor
		wantOrder   []uint8 // tagged through Flags
	}{
		{
			description: "positive: ifindex wins over family, family over destination",
			in: []Neighbor{
				{Flags: 1, Ifindex: 2, Family: 10, Dst: []byte{0x20}},
				{Flags: 2, Ifindex: 1, Family: 2, Dst: []byte{192, 0, 2, 1}},
				{Flags: 3, Ifindex: 2, Family: 2, Dst: []byte{192, 0, 2, 9}},
			},
			wantOrder: []uint8{2, 3, 1},
		},
		{
			description: "negative: an already-ordered slice is left alone",
			in: []Neighbor{
				{Flags: 1, Ifindex: 1, Family: 2, Dst: []byte{192, 0, 2, 1}},
				{Flags: 2, Ifindex: 2, Family: 2, Dst: []byte{192, 0, 2, 2}},
			},
			wantOrder: []uint8{1, 2},
		},
		{
			description: "boundary: an empty slice sorts without panicking",
			in:          []Neighbor{},
			wantOrder:   []uint8{},
		},
		{
			// Dst is the last key, so two entries identical through it keep
			// their input order rather than being reordered by State — which
			// matters because a neighbor's State is the field most likely to
			// differ between two captures of the same cache.
			description: "corner: entries identical through Dst keep their input order",
			in: []Neighbor{
				{Flags: 1, Ifindex: 2, Family: 2, Dst: []byte{192, 0, 2, 1}},
				{Flags: 2, Ifindex: 2, Family: 2, Dst: []byte{192, 0, 2, 1}},
			},
			wantOrder: []uint8{1, 2},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			SortNeighbors(tc.in)
			got := make([]uint8, 0, len(tc.in))
			for i := range tc.in {
				got = append(got, tc.in[i].Flags)
			}
			if !reflect.DeepEqual(got, tc.wantOrder) {
				t.Errorf("order = %v, want %v", got, tc.wantOrder)
			}
		})
	}
}
