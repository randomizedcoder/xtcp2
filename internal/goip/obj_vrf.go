package goip

import (
	"encoding/json"
	"fmt"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// vrfShowVerbs are the spellings do_ipvrf accepts for the listing
// (ip/ipvrf.c:642-644): show, lst, list. All three call ipvrf_show.
var vrfShowVerbs = []string{"show", "lst", "list"}

// vrfRefusedVerbs are do_ipvrf's other subcommands (ip/ipvrf.c:633-640):
// identify and pids read /proc, exec runs a command under the VRF. None is a
// read-only netlink query, so goip refuses them.
var vrfRefusedVerbs = []string{"identify", "pids", "exec"}

func runVrf(c *runCtx, args []string) error {
	if len(args) > 0 {
		switch {
		case matchesAny(args[0], vrfRefusedVerbs):
			return fmt.Errorf("vrf %q: not a read-only netlink query: %w", args[0], ErrNotImplemented)
		case matchesAny(args[0], vrfShowVerbs):
			args = args[1:]
		default:
			return fmt.Errorf("vrf %q: %w", args[0], ErrNotImplemented)
		}
	}
	// A bare `ip vrf` is a show (ip/ipvrf.c:630-631, argc==0).
	if err := parseVrfShowArgs(args); err != nil {
		return err
	}
	return vrfShow(c)
}

// parseVrfShowArgs refuses the single-NAME form. `ip vrf show NAME` takes the
// ipvrf_get_table path (ip/ipvrf.c:590-600), a name->table lookup that is not the
// link dump; goip grounds only the dump, so any trailing token is refused.
func parseVrfShowArgs(args []string) error {
	if len(args) == 0 {
		return nil
	}
	return fmt.Errorf("vrf show %q: %w", args[0], ErrNotImplemented)
}

// vrfShow sends `ip vrf show`'s kind-filtered link dump and renders the VRF rows.
// The kernel does not honor the IFLA_INFO_KIND filter, so the reply is the full
// link list and the kind == "vrf" selection is done here, exactly as ipvrf_print
// filters client-side (ip/ipvrf.c:553-569).
func vrfShow(c *runCtx) error {
	// `ip vrf show` leaves preferred_family unset; -4/-6 would change the dump to
	// a minimal inet6 link dump whose replies carry no IFLA_LINKINFO, a shape
	// goip does not ground.
	if c.family != unix.AF_UNSPEC {
		return fmt.Errorf("vrf show with -4/-6: family selection changes the dump shape: %w", ErrNotImplemented)
	}

	svc := service.New(c.src, c.nextSeq)
	links, err := svc.Vrfs()
	if err != nil {
		return err
	}

	views := make([]render.VrfView, 0, len(links))
	for i := range links {
		li := xtcpnl.LinkInfo(links[i])
		// ipvrf_print keeps only kind == "vrf" rows with a non-zero table id; a
		// zero/absent table is iproute2's BUG case and is skipped, not printed.
		if li.Kind != vrfKindCst || !li.VrfTable.Present || li.VrfTable.Value == 0 {
			continue
		}
		views = append(views, render.VrfView{Name: li.Name, Table: li.VrfTable.Value})
	}

	if c.json {
		return json.NewEncoder(c.out).Encode(views)
	}
	_, err = fmt.Fprint(c.out, render.RenderVrfText(views))
	return err
}

// vrfKindCst is IFLA_INFO_KIND for a VRF device, the string ipvrf_print tests
// (ip/ipvrf.c:561). It matches the unexported xtcpnl constant of the same value.
const vrfKindCst = "vrf"
