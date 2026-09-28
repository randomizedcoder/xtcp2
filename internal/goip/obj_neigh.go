package goip

import (
	"encoding/json"
	"fmt"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

func runNeigh(c *runCtx, args []string) error {
	if len(args) > 0 && !matchesPrefix(args[0], "show") && !matchesPrefix(args[0], "list") {
		return fmt.Errorf("neigh %q: %w", args[0], ErrNotImplemented)
	}
	if len(args) > 1 {
		return fmt.Errorf("neigh show %q: %w", args[1], ErrNotImplemented)
	}
	links, neighbors, err := service.New(c.src, c.nextSeq).NeighborSnapshot(c.family)
	if err != nil {
		return err
	}
	decodedLinks := make([]xtcpnl.LinkInfo, len(links))
	for i := range links {
		decodedLinks[i] = xtcpnl.LinkInfo(links[i])
	}
	c.lltab.Fill(decodedLinks)
	views := make([]render.NeighView, 0, len(neighbors))
	for i := range neighbors {
		n := xtcpnl.NeighInfo(neighbors[i])
		// iproute2's default state filter omits NUD_NOARP entries (notably
		// multicast neighbor-cache rows); an explicit state selector can expose
		// them when that CLI surface is added.
		if n.State&unix.NUD_NOARP != 0 {
			continue
		}
		views = append(views, render.NeighViewOf(n, c.lltab))
	}
	if c.json {
		return json.NewEncoder(c.out).Encode(views)
	}
	for i := range views {
		if _, err := fmt.Fprint(c.out, views[i].Text()); err != nil {
			return err
		}
	}
	return nil
}
