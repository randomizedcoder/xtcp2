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

var (
	// ErrVrfUsage is a refusal ip/ipvrf.c ends with usage's exit(-1), 255:
	// `vrf help` (do_ipvrf's help arm, :652-653) and two or more names
	// (ipvrf_show's argc>1, :587-589).
	ErrVrfUsage = errors.New("vrf usage")
	// ErrVrfInvalid is ipvrf_show's NAME branch with a zero table
	// (ip/ipvrf.c:592-595): "Invalid VRF" on stderr, exit 1.
	ErrVrfInvalid = errors.New("vrf invalid")
)

const (
	// vrfUsageCst is usage()'s text (ip/ipvrf.c:33-42), tabs and all.
	vrfUsageCst = "Usage:\tip vrf show [NAME] ...\n" +
		"\tip vrf exec [NAME] cmd ...\n" +
		"\tip vrf identify [PID]\n" +
		"\tip vrf pids [NAME]\n"

	// vrfDefaultNameCst is the name ipvrf_get_table's ENODEV arm treats
	// specially: the "default" vrf resolves the main table
	// (ip/iplink_vrf.c:136-142).
	vrfDefaultNameCst = "default"
)

func runVrf(c *runCtx, args []string) error {
	if len(args) > 0 {
		v, ok := resolveVerb(vrfVerbs, args[0])
		switch {
		case !ok:
			return fmt.Errorf("vrf %q: %w", args[0], ErrNotImplemented)
		case v.kind == verbShow:
			args = args[1:]
		case v.name == helpVerb.name:
			return fmt.Errorf("%w: %s", ErrVrfUsage, vrfUsageCst)
		default:
			return fmt.Errorf("vrf %q: not a read-only netlink query: %w", v.name, ErrNotImplemented)
		}
	}
	// A bare `ip vrf` is a show (ip/ipvrf.c:630-631, argc==0).
	name, err := parseVrfShowArgs(args)
	if err != nil {
		return err
	}
	if name != "" {
		return vrfShowName(c, name)
	}
	return vrfShow(c)
}

// parseVrfShowArgs is ipvrf_show's argc check (ip/ipvrf.c:587-589): zero names
// is the dump, one is the ipvrf_get_table lookup, and two or more are usage's
// exit 255. It returns the NAME, "" for the dump form.
func parseVrfShowArgs(args []string) (string, error) {
	switch len(args) {
	case 0:
		return "", nil
	case 1:
		return args[0], nil
	default:
		return "", fmt.Errorf("%w: %s", ErrVrfUsage, vrfUsageCst)
	}
}

// vrfShowName is ipvrf_show's NAME branch (ip/ipvrf.c:590-600): one
// ipvrf_get_table lookup, its table printed as "NAME table" with the name as
// typed. The branch returns before new_json_obj is ever called, so -j, -br and
// -o change nothing here — the plain line is printed under every output mode
// (decision 4, docs/netlink/vrf-show-name.md). The family flag passes through
// to the request's ifi_family byte (decision 1, docs/netlink/
// vrf-family-forms.md): the kernel resolves the name without consulting the
// family (net/core/rtnetlink.c:4346-4355), so -4, -6 and -0 print the same
// line the plain form does, and the dump form's family refusal stays distinct
// (the family changes the dump's shape).
func vrfShowName(c *runCtx, name string) error {
	svc := service.New(c.src, c.nextSeq)
	table, err := svc.VrfTableByName(c.family, name)
	if err != nil {
		switch {
		case errors.Is(err, xtcpnl.ErrBadIfName):
			// ipvrf_get_table never validates the name; the kernel answers ENODEV
			// for one dev_valid_name rejects, and `ip` prints Invalid VRF. The
			// builder's refusal takes the ENODEV arm rather than surfacing as a
			// build error (decision 6).
			table = vrfTableForFailure(name, unix.ENODEV)
		default:
			var errno unix.Errno
			if !errors.As(err, &errno) {
				// Not the kernel's answer — a decode error or a closed socket.
				// Surfaced loudly rather than hidden behind Invalid VRF (decision 3).
				return err
			}
			// A kernel errno is the answer (decision 3): ENODEV's `default` rule
			// is ipvrf_get_table's own (ip/iplink_vrf.c:134-143), every other
			// errno a zero table.
			table = vrfTableForFailure(name, errno)
		}
	}
	if table == 0 {
		// ipvrf_show's Invalid VRF (ip/ipvrf.c:592-595): the line goes to stderr
		// and the rc is 1.
		if _, werr := fmt.Fprintln(c.errOut, "Invalid VRF"); werr != nil {
			return werr
		}
		return ErrVrfInvalid
	}
	_, err = fmt.Fprintf(c.out, "%s %d\n", name, table)
	return err
}

// vrfTableForFailure is ipvrf_get_table's talk-failure arm
// (ip/iplink_vrf.c:134-143): errno ENODEV with the name exactly "default"
// resolves the main table id — rtnl_rttable_a2n("main"), 254 — and any other
// failure is a zero table.
func vrfTableForFailure(name string, errno unix.Errno) uint32 {
	if errno == unix.ENODEV && name == vrfDefaultNameCst {
		return unix.RT_TABLE_MAIN
	}
	return 0
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
