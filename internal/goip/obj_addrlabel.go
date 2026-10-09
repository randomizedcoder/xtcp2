package goip

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// addrLabelShowVerbs are the three spellings do_ipaddrlabel accepts for the
// listing (ip/ipaddrlabel.c, in its order): list, lst, show. The order matches
// rule's, and like rule the abbreviations fall out of it (l -> list, s -> show,
// ls -> lst).
var addrLabelShowVerbs = []string{"list", "lst", "show"}

func runAddrLabel(c *runCtx, args []string) error {
	if len(args) > 0 {
		if !matchesAny(args[0], addrLabelShowVerbs) {
			return fmt.Errorf("addrlabel %q: %w", args[0], ErrNotImplemented)
		}
		args = args[1:]
	}
	// A bare `ip addrlabel` is a list (ip/ipaddrlabel.c:do_ipaddrlabel argc<1).
	if err := parseAddrLabelShowArgs(args); err != nil {
		return err
	}
	return addrLabelShow(c)
}

// parseAddrLabelShowArgs rejects every selector. `ip addrlabel show` takes none:
// ipaddrlabel_list parses no filter arguments, it just dumps the table.
func parseAddrLabelShowArgs(args []string) error {
	if len(args) == 0 {
		return nil
	}
	return fmt.Errorf("addrlabel show %q: %w", args[0], ErrNotImplemented)
}

// addrLabelShow sends the one request `ip addrlabel show` sends and renders the
// replies.
//
// # The family substitution
//
// ipaddrlabel_list opens with `af = preferred_family; if (af == AF_UNSPEC) af =
// AF_INET6;` (ip/ipaddrlabel.c:101-104), so a bare `ip addrlabel show` asks for
// AF_INET6 — the addrlabel table is IPv6-only. That makes `ip addrlabel show` and
// `ip -6 addrlabel show` byte-identical requests as well as identical output, the
// `rule`/`-4` relationship, which is why there is no `-6` pcap in the corpus and
// the claim is asserted by a parity row instead. The substitution is here, not in
// req.AddrLabelShowDump, because this is where `ip` does it.
func addrLabelShow(c *runCtx) error {
	family := c.family
	if family == unix.AF_UNSPEC {
		family = unix.AF_INET6
	}

	svc := service.New(c.src, c.nextSeq)
	rows, err := svc.AddrLabels(family)
	if err != nil {
		return err
	}

	views := make([]render.AddrLabelView, 0, len(rows))
	for i := range rows {
		v, verr := render.AddrLabelViewOf(xtcpnl.AddrLabelInfo(rows[i]))
		if verr != nil {
			// A per-device entry (ifal_index != 0) is ungrounded; surface the
			// render refusal as ErrNotImplemented, as obj_nexthop does.
			if errors.Is(verr, render.ErrAddrLabelPerDev) {
				return fmt.Errorf("addrlabel show: %v: %w", verr, ErrNotImplemented)
			}
			return verr
		}
		views = append(views, v)
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
