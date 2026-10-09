package goip

import (
	"encoding/json"
	"fmt"

	"github.com/randomizedcoder/xtcp2/internal/goip/model"
	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// netconfShowVerbs are the spellings do_ipnetconf accepts for the listing, in its
// order (ip/ipnetconf.c:234-239): show, lst, list, all matched with matches() as
// neigh and ntable are. change/write verbs do not exist for netconf and help is
// refused, goip being read-only.
var netconfShowVerbs = []string{"show", "lst", "list"}

// runNetconf is do_ipnetconf restricted to the listing path (ip/ipnetconf.c:233).
// A bare `ip netconf` is do_show(0, NULL) (:243), so an empty verb is the listing.
func runNetconf(c *runCtx, args []string) error {
	if len(args) > 0 {
		if !matchesAny(args[0], netconfShowVerbs) {
			return fmt.Errorf("netconf %q: %w", args[0], ErrNotImplemented)
		}
		args = args[1:]
	}
	dev, err := parseNetconfShowArgs(args)
	if err != nil {
		return err
	}
	return netconfShow(c, dev)
}

// parseNetconfShowArgs is do_show's argument loop (ip/ipnetconf.c:173-182). The
// sole keyword is `dev STRING`; iproute2 silently skips unknown tokens, but goip
// refuses them so an unexpected argument never passes for an unfiltered dump (the
// ntable stance). `dev` is strcmp'd there, not matched, so it does not abbreviate.
func parseNetconfShowArgs(args []string) (string, error) {
	var dev string
	for i := 0; i < len(args); i++ {
		if args[i] != devKeywordCst {
			return "", fmt.Errorf("netconf show %q: %w", args[i], ErrNotImplemented)
		}
		if i+1 >= len(args) {
			return "", fmt.Errorf("netconf show dev: missing device name: %w", ErrNotImplemented)
		}
		i++
		dev = args[i]
	}
	return dev, nil
}

// netconfShow sends the requests `ip netconf show` sends — ll_init_map's link
// dump (ip/ipnetconf.c:186) then the netconf transaction — and renders the
// replies. iproute2's :188 branch chooses the form: an explicit family AND a dev
// is a point GET; a bare dev is a dump filtered to the ifindex client-side; a bare
// show dumps every family (AF_UNSPEC) and drops none.
func netconfShow(c *runCtx, dev string) error {
	svc := service.New(c.src, c.nextSeq)

	links, err := svc.NetconfLinks()
	if err != nil {
		return err
	}
	decoded := make([]xtcpnl.LinkInfo, len(links))
	for i := range links {
		decoded[i] = xtcpnl.LinkInfo(links[i])
	}
	c.lltab.Fill(decoded)

	var devIdx int32
	if dev != "" {
		// ll_name_to_index, with 0 the "device does not exist" answer
		// (ip/ipnetconf.c:175-181). Resolved before the branch, as iproute2 does.
		devIdx = c.lltab.NameToIndex(dev)
		if devIdx == 0 {
			return fmt.Errorf("netconf show: cannot find device %q: %w", dev, ErrNotImplemented)
		}
	}

	// ip/ipnetconf.c:188 picks the point get only when both an ifindex and an
	// explicit family are set; everything else is the dump path (with a
	// client-side ifindex filter when a dev was named).
	if dev != "" && c.family != unix.AF_UNSPEC {
		nc, gerr := svc.NetconfByIndex(c.family, devIdx)
		if gerr != nil {
			return gerr
		}
		return netconfRender(c, []model.Netconf{nc})
	}

	records, err := svc.Netconfs(c.family)
	if err != nil {
		return err
	}
	kept := records[:0]
	for i := range records {
		ni := xtcpnl.NetconfInfo(records[i])
		// Client-side family drop (ip/ipnetconf.c:69-70) and ifindex drop
		// (:78-79). A bare show keeps every record (c.family AF_UNSPEC, no dev).
		if c.family != unix.AF_UNSPEC && c.family != ni.Family {
			continue
		}
		if dev != "" && (!ni.HasIfindex || ni.Ifindex != devIdx) {
			continue
		}
		kept = append(kept, records[i])
	}
	return netconfRender(c, kept)
}

// netconfRender builds the views and emits them. goip always emits a single JSON
// array, even when the (unreachable-here) old-kernel fallback would have run;
// iproute2's fallback emits two concatenated arrays — malformed JSON — which goip
// deliberately does not reproduce. Parity only ever runs on the modern capture
// kernel, where one AF_UNSPEC dump answers every family in one array.
func netconfRender(c *runCtx, records []model.Netconf) error {
	views := make([]render.NetconfView, 0, len(records))
	for i := range records {
		views = append(views, render.NetconfViewOf(xtcpnl.NetconfInfo(records[i]), c.lltab))
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
