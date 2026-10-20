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

// runLink handles `ip link ...`: the write verbs (link_write.go) and the
// show verbs.
//
// do_iplink tests the write verbs first, all through matches()
// (ip/iplink.c:1515-1533), so `ip link s` is `set`, not `show`; `sh` and `l`
// are the shortest show spellings. A bare `ip link` with no verb is a show:
// do_iplink calls ipaddr_list_link when argc is 0.
func runLink(c *runCtx, args []string) error {
	if len(args) == 0 {
		return linkShow(c, nil)
	}
	v, ok := resolveVerb(linkVerbs, args[0])
	if !ok {
		return fmt.Errorf("link %q: %w", args[0], ErrNotImplemented)
	}
	if v.kind == verbShow {
		return linkShow(c, args[1:])
	}
	if w := lookupLinkWriteVerb(v.name); w != nil {
		return linkWrite(c, w, args[1:])
	}
	return fmt.Errorf("link %q: %w", v.name, ErrNotImplemented)
}

// linkTypeKeywordCst is `type`, compared with strcmp (ip/ipaddress.c:2232),
// so it does not abbreviate.
const linkTypeKeywordCst = "type"

// linkShowSelectors is what goip's link show takes from the keyword loop of
// ipaddr_list_flush_or_save, which `ip link show` shares with `ip address
// show` (ipaddr_list_link, ip/ipaddress.c:2420-2425): the device and the
// kind. filter holds the kind in addrFilter's kind and slaveKind fields, so
// the `_slave` split and the match are address show's.
type linkShowSelectors struct {
	dev     string
	haveDev bool
	filter  addrFilter
}

// kindFiltered is print_linkinfo's two kind tests (ip/ipaddress.c:1020-1024).
func (s *linkShowSelectors) kindFiltered(li *xtcpnl.LinkInfo) bool {
	f := &s.filter
	return f.haveKind && !linkKindMatches(li, f.kind, false) ||
		f.haveSlaveKind && !linkKindMatches(li, f.slaveKind, true)
}

// parseLinkShowArgs walks the selectors after `show` in the loop's order
// (ip/ipaddress.c:2170-2253), for the two arms goip's link show implements.
//
//   - `type T` is strcmp and NEXT_ARG, with no duparg: a second `type`
//     overwrites the first, except that a `_slave` word sets
//     filter.slave_kind and leaves filter.kind alone, so `type veth type
//     bond_slave` keeps both (addrFilterType has the suffix arithmetic).
//   - The final arm is `[dev] NAME`: `dev` by strcmp and NEXT_ARG, `help` by
//     matches(), and anything else is the device. A second device is
//     duparg2.
//
// Every other keyword of the loop, and every get_filter flag name, is
// rejected rather than ignored: they are ip's filters, not device names.
// The device is not looked up here; linkShowDev's first get is that lookup.
func parseLinkShowArgs(args []string) (linkShowSelectors, error) {
	var s linkShowSelectors
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == linkTypeKeywordCst {
			if i+1 >= len(args) {
				return s, incompleteErr()
			}
			i++
			if err := addrFilterType(&s.filter, args[i], addrLookups{}); err != nil {
				return s, err
			}
			continue
		}
		var probe addrFilter
		if lookupAddrShowKeyword(arg) != nil || probe.getFilter(arg) {
			return s, fmt.Errorf("link show %q: %w", arg, ErrNotImplemented)
		}
		if arg == devKeywordCst {
			if i+1 >= len(args) {
				return s, incompleteErr()
			}
			i++
			arg = args[i]
		} else if matchesPrefix(arg, routeHelpKeywordCst) {
			return s, fmt.Errorf("link show %q: %w", arg, ErrNotImplemented)
		}
		if s.haveDev {
			return s, duparg2Err(devKeywordCst, arg)
		}
		s.dev, s.haveDev = arg, true
	}
	return s, nil
}

// linkShow runs the dump and renders it.
//
// # Exactly one netlink transaction, and the argument order that guarantees it
//
// The dump is sent first and the index cache is filled from its own replies,
// before any rendering happens. That ordering is the whole reason `link show`
// emits a single transaction: rendering `master br-3a5828b2963a` or a `@peer`
// suffix needs an index resolved, and iproute2's ll_index_to_name will issue a
// live RTM_GETLINK single-get for an index it does not know
// (lib/ll_map.c:320). Filling first means every index the replies reference is
// already cached, so goip never takes that path — which matters because a side
// transaction inside a capture window is an L1 transaction-count divergence,
// the harness's highest-value assertion.
//
// That reasoning holds *because* the dump fills the cache. linkShowDev below
// sends no dump, so its cache holds one link and it takes the side-get path on
// purpose — `ip` takes it there too, and for the same reason.
//
// `dev X` and a bare `X` use the two-single-get path in linkShowDev. Other
// filtering arguments (`up`, `group G`, `master M`) are not implemented and
// are rejected rather than ignored; see parseLinkShowArgs.
//
// # `type`: on the wire, then again on every reply
//
// With a kind, the dump carries it (req.LinkShowDumpKind), and a kernel
// that knows the kind returns only its links. One that does not (the
// kind's module is not loaded) returns every link, and print_linkinfo's
// kind test drops the rest, so the kind is tested again here. A slave kind
// is never sent; it is only that test.
//
// A dropped link still costs `ip -j` an object: the print loop opens and
// closes one around print_linkinfo whatever it returns (ip/ipaddress.c:
// 2336-2347), so each dropped link is a `{}`, and text prints nothing.
//
// A kernel-filtered dump leaves the cache without the links it skipped, so
// a printed link's IFLA_LINK or master may need print_linkinfo's side-get,
// sent in print order (resolveLinkRefs). A full dump caches every index and
// sends none.
func linkShow(c *runCtx, args []string) error {
	sel, err := parseLinkShowArgs(args)
	if err != nil {
		return err
	}
	if sel.haveDev {
		return linkShowDev(c, sel.dev, &sel)
	}

	svc := service.New(c.src, c.nextSeq)
	var resources []model.Link
	if sel.filter.haveKind {
		resources, err = svc.LinksKind(c.linkExtMask(), sel.filter.kind)
	} else {
		resources, err = svc.Links(c.linkExtMask())
	}
	if err != nil {
		return err
	}
	links := make([]xtcpnl.LinkInfo, len(resources))
	for i := range resources {
		links[i] = xtcpnl.LinkInfo(resources[i])
	}
	c.lltab.Fill(links)

	if sel.filter.haveKind || sel.filter.haveSlaveKind {
		return linkShowKind(c, svc, &sel, links)
	}

	if err := checkDetailSupported(c, links); err != nil {
		return err
	}

	// Ranged by index: both structs are deliberately large, so the value form
	// of either loop would copy the whole resource once per link for nothing.
	views := make([]render.LinkView, 0, len(links))
	for i := range links {
		views = append(views, linkView(c, links[i]))
	}

	if c.json {
		enc := json.NewEncoder(c.out)
		return enc.Encode(views)
	}
	for i := range views {
		if _, werr := fmt.Fprint(c.out, views[i].Text()); werr != nil {
			return werr
		}
	}
	return nil
}

// linkShowKind renders a `type` dump: the kind test on every reply, a `{}`
// for each dropped link under -j, and the side-gets of the printed ones.
func linkShowKind(c *runCtx, svc *service.Service, sel *linkShowSelectors, links []xtcpnl.LinkInfo) error {
	kept := make([]xtcpnl.LinkInfo, 0, len(links))
	for i := range links {
		if !sel.kindFiltered(&links[i]) {
			kept = append(kept, links[i])
		}
	}
	if err := checkDetailSupported(c, kept); err != nil {
		return err
	}
	out := make([]any, 0, len(links))
	for i := range links {
		if sel.kindFiltered(&links[i]) {
			out = append(out, struct{}{})
			continue
		}
		resolveLinkRefs(c, svc, links[i])
		out = append(out, linkView(c, links[i]))
	}
	return writeLinkShow(c, out)
}

// linkView is one printed link: the stanza, then -s, then -d.
func linkView(c *runCtx, li xtcpnl.LinkInfo) render.LinkView {
	v := render.LinkViewOf(li, c.lltab)
	if c.showStats > 0 {
		v = v.WithStats(li)
	}
	if c.detailed() {
		v = v.WithDetail(li, true, c.lltab)
	}
	return v
}

// writeLinkShow writes a filtered link show: a JSON array whose dropped
// links are `{}`, or the printed stanzas as text.
func writeLinkShow(c *runCtx, out []any) error {
	if c.json {
		return json.NewEncoder(c.out).Encode(out)
	}
	for i := range out {
		if v, ok := out[i].(render.LinkView); ok {
			if _, err := fmt.Fprint(c.out, v.Text()); err != nil {
				return err
			}
		}
	}
	return nil
}

// linkShowDev serves `ip link show dev NAME` as the two by-name single-gets,
// and no dump, that iproute2 sends.
//
// # Why one link costs two requests
//
// ipaddr_list_link resolves the selector and prints the result through paths
// that do not share a reply. `dev NAME` is parsed into filter.ifindex by
// ll_name_to_index (ip/ipaddress.c:2254), which on a cache miss issues
// ll_link_get(name, 0) on a throwaway socket (lib/ll_map.c:264, :354-372)
// purely to learn the index. Then, filter.ifindex being set, iplink_get
// re-fetches that same link — by name again, not by the index just learned —
// on the main socket (:2293), and that second reply is the one print_linkinfo
// renders. The first is discarded apart from the cache entry it leaves behind.
//
// The redundancy is iproute2's, and goip reproduces it rather than improving
// on it: pkg/nlparity compares the two tools' requests for byte equality, so
// sending one request where `ip` sends two is an L1 transaction-count
// divergence before any attribute is even looked at.
//
// # Why not dump, resolve locally, then get by index
//
// That is the shape this function replaced, and it is instructive: it keeps
// the transaction count at two, so it passes L1, and it prints the same bytes,
// so stdout parity is clean — while both of its requests are wrong at L2. A
// dump carries NLM_F_DUMP, AF_PACKET and no IFLA_IFNAME; a by-index get
// carries ifi_index where iproute2 carries a name. Only full request equality
// catches it, which is why that assertion exists.
//
// It was also over-generous with the cache: dumping filled lltab with every
// link on the box, so any later name lookup this command made would hit in
// memory and send nothing, hiding precisely the side-gets the harness is built
// to observe. Here only the link that was asked for is cached, which is all
// `ip` has at print time too.
//
// `type` adds nothing to either request: iplink_get sends no filter
// (ip/iplink.c:1193-1211). print_linkinfo still applies it, before it
// prints or resolves anything, so a link of another kind prints nothing,
// `[{}]` under -j, and sends no side-get.
func linkShowDev(c *runCtx, name string, sel *linkShowSelectors) error {
	svc := service.New(c.src, c.nextSeq)

	// First get: ll_name_to_index. Nothing is rendered from this reply — it is
	// sent for the cache entry, exactly as in iproute2.
	resolved, err := svc.LinkByName(name)
	if err != nil {
		return err
	}
	c.lltab.Fill([]xtcpnl.LinkInfo{xtcpnl.LinkInfo(resolved)})

	// Second get: iplink_get. This reply is the one that gets printed, and
	// the only one of the two that `-s` changes — ll_link_get above builds
	// its mask as a local constant (lib/ll_map.c:276-277).
	shown, err := svc.LinkShowDev(name, c.linkExtMask())
	if err != nil {
		return err
	}
	link := xtcpnl.LinkInfo(shown)
	c.lltab.Fill([]xtcpnl.LinkInfo{link})
	if sel.kindFiltered(&link) {
		return writeLinkShow(c, []any{struct{}{}})
	}
	resolveLinkRefs(c, svc, link)

	if err := checkDetailSupported(c, []xtcpnl.LinkInfo{link}); err != nil {
		return err
	}

	view := linkView(c, link)
	if c.json {
		return json.NewEncoder(c.out).Encode([]render.LinkView{view})
	}
	_, err = fmt.Fprint(c.out, view.Text())
	return err
}

// checkDetailSupported refuses `-d` on a link whose kind would have made
// iproute2 print more than the kind token.
//
// # Why this refuses rather than printing a short line
//
// print_linktype emits the kind and then, on the SAME line, the whole output
// of that kind's print_opt (ip/ipaddress.c:230-244). Forty of those exist and
// only vrf's and bond's are implemented here (renderableInfoData) — see the
// header of render/link_detail.go for why that boundary is where it is. The
// consequence is that a link with per-kind data has no partially-correct
// render: a line reading `    bridge ` where `ip`'s reads `    bridge
// forward_delay 200 hello_time 200 …` is not shorter, it is a different line.
//
// So this follows the `-s -s` precedent rather than the usual omitempty one.
// An explicit error names the missing feature at the point it was asked for;
// a plausible-looking wrong line reaches the parity harness as a stdout
// divergence with nothing in the report to say that a subsystem is absent
// rather than buggy.
//
// # The condition is the data, not a list of kinds
//
// Tested on IFLA_INFO_DATA / IFLA_INFO_SLAVE_DATA being present rather than on
// li.Kind appearing in a table of the kinds iproute2 has a print_opt for. A
// table would go stale every time iproute2 gained an iplink_*.c, and it would
// be wrong in both directions today: `veth` has no print_opt at all, while a
// kind that has one prints nothing from it when the nest holds no data, since
// every print_opt begins by testing its argument.
//
// Measured on the corpus, which is what makes the refusal cheap: no link in
// the 7_1_4 guest topology carries either attribute — all three have an
// IFLA_LINKINFO holding IFLA_INFO_KIND alone, or in lo's case none at all — so
// `-d link show` and `-d addr show` are refused nowhere the parity harness
// runs.
func checkDetailSupported(c *runCtx, links []xtcpnl.LinkInfo) error {
	if !c.detailed() {
		return nil
	}
	for i := range links {
		li := &links[i]
		switch {
		case li.HasInfoData && !renderableInfoData(li.Kind):
			return fmt.Errorf("-d on %s: iproute2 renders IFLA_INFO_DATA for kind %q "+
				"and goip does not: %w", li.Name, li.Kind, ErrNotImplemented)
		case li.HasInfoSlaveData && !renderableInfoData(li.SlaveKind):
			return fmt.Errorf("-d on %s: iproute2 renders IFLA_INFO_SLAVE_DATA for slave kind %q "+
				"and goip does not: %w", li.Name, li.SlaveKind, ErrNotImplemented)
		}
	}
	return nil
}

// renderableInfoData reports whether goip reproduces the per-kind print_opt
// output for a kind: vrf (a single table id) and bond (bond_print_opt and
// bond_slave_print_opt, grounded on the 7_1_4 link-write fixtures). Every
// other kind's blob stays undecoded and its -d render refused, which keeps
// the gate kind-agnostic for everything goip has not verified against a
// capture. The list is render's, so the gate and the renderer cannot
// disagree about which kinds have a print_opt.
func renderableInfoData(kind string) bool {
	return kind == render.VrfKindCst || kind == render.BondKindCst
}

// resolveLinkRefs sends the side-gets print_linkinfo issues for the two
// indexes a link stanza can name besides its own.
//
// Both go through ll_index_to_name, so both are lazy in exactly the sense
// resolveIndexName implements: nothing is sent for index 0 or for an index the
// cache already holds. The order is print_linkinfo's own —
// print_name_and_link at ip/ipaddress.c:1018 runs before the IFLA_MASTER block
// at :1032 — and it is observable, because two gets on one socket appear in a
// capture in the order they were sent.
//
// The IFLA_LINK case is conditional in a way the master case is not.
// print_name_and_link resolves the peer only when IFLA_LINK_NETNSID is absent
// (lib/utils.c:1310-1320); with a netnsid present it calls ll_idx_n2a, which
// is an unconditional "if%u" — no cache, no netlink, not even a lookup. Every
// veth in the committed 7_1_8 dump carries a netnsid, which is why
// ip_link_n shows `ve-nfb-vpn@if2` and why `link show dev` on one of them
// sends two requests rather than three.
//
// Under -d a bond adds two more, active_slave then primary, which
// bond_print_opt passes to ll_index_to_name in that order
// (ip/iplink_bond.c:455-465, :592-600). print_linktype runs after the master
// block, so they come last. No fixture reaches them: 802.3ad sends neither
// attribute, and a full dump has every index cached anyway.
func resolveLinkRefs(c *runCtx, svc *service.Service, li xtcpnl.LinkInfo) {
	if li.Link != 0 && !li.HasLinkNetnsID {
		resolveIndexName(c, svc, li.Link)
	}
	resolveIndexName(c, svc, li.Master)
	if c.detailed() && li.Kind == render.BondKindCst && li.Bond != nil {
		for _, id := range []uint16{unix.IFLA_BOND_ACTIVE_SLAVE, unix.IFLA_BOND_PRIMARY} {
			if v := li.Bond.Value(id); v.Present {
				resolveIndexName(c, svc, int32(v.U)) //nolint:gosec // an ifindex: the kernel's int, sent as u32
			}
		}
	}
}
