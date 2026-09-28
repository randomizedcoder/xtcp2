package model

import "testing"

func TestStableResourceOrdering(t *testing.T) {
	links := []Link{{Index: 9}, {Index: 1}, {Index: 4}}
	SortLinks(links)
	if links[0].Index != 1 || links[1].Index != 4 || links[2].Index != 9 {
		t.Fatalf("links not ordered by ifindex: %#v", links)
	}

	routes := []Route{
		{Table: 254, Family: 10, Dst: []byte{2}, Priority: 2},
		{Table: 100, Family: 2, Dst: []byte{3}},
		{Table: 254, Family: 2, Dst: []byte{1}, Priority: 1},
	}
	SortRoutes(routes)
	if routes[0].Table != 100 || routes[1].Family != 2 || routes[2].Family != 10 {
		t.Fatalf("routes not canonically ordered: %#v", routes)
	}
}
