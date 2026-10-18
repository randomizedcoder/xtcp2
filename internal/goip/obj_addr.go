package goip

import (
	"encoding/json"
	"fmt"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// runAddress handles `ip address ...`: the show verbs, and the four write
// verbs as a dry run.
//
// iproute2's verb list for this object is longer than link's and, because
// every entry goes through matches(), the abbreviations interact. do_ipaddr
// tests in this order: add, change (plus the exact-match alias "chg"),
// replace, delete, list|show|lst, flush, save, showdump, restore, help.
//
// So `ip a s` is **show**, because show is tested before save; `ip a sa` is
// **save**, because "sa" is not a prefix of "show"; and `ip a r` is
// **replace**, not restore. lookupAddrWriteVerb is the first four arms in
// that order, so goip resolves all three the same way.
//
// add, change, replace and delete build `ip`'s request and write it to
// GOIP_WRITE_OUT (addrWrite); nothing is applied. flush is still refused as
// read-only, and the default arm covers save/showdump/restore, which are
// read-only but not implemented.
func runAddress(c *runCtx, args []string) error {
	if len(args) == 0 {
		return addrShow(c, nil)
	}
	verb := args[0]
	if v := lookupAddrWriteVerb(verb); v != nil {
		return addrWrite(c, v, args[1:])
	}
	switch {
	case matchesPrefix(verb, showVerbCst), matchesPrefix(verb, lstVerbCst), matchesPrefix(verb, listVerbCst):
		return addrShow(c, args[1:])
	case matchesPrefix(verb, "flush"):
		// Named explicitly, and refused for a stronger reason than "not
		// written yet": a flush is a dump and then one RTM_DELADDR per
		// address, and no dry run of it exists yet.
		return fmt.Errorf("address %q: goip is read-only: %w", verb, ErrNotImplemented)
	default:
		return fmt.Errorf("address %q: %w", verb, ErrNotImplemented)
	}
}

// addrShow dispatches `dev NAME` to addrShowDev and otherwise runs the two
// dumps and renders them.
//
// # Two transactions, in this order, and the order is the assertion
//
// This is the first command in goip that sends more than one request, which
// makes it the first real test of the parity harness's L1 finding — positional
// transaction count. iproute2 sends the link dump first
// (`ip_link_list`, ip/ipaddress.c:2306) and the address dump second
// (`ip_addr_list`, :2314), and it needs both: the output is a loop over
// *links*, with each link's addresses filtered out of the address list
// (:2320-2334). A goip that dumped addresses only, or that dumped them in the
// other order, would produce plausible output and diverge on the wire.
//
// The second dump is skipped entirely when the family is AF_PACKET (:2310).
// That arm is reached by `-0`, which goip parses (goip.go:61) — this comment
// used to say it was unreachable, which was true only until the option
// landed. See the family argument threading through req.AddrShowLinkDump,
// where AF_PACKET selects a different request shape too.
//
// # Why the index cache is filled between the dumps and not after
//
// Same reason as link show: rendering `master br0` resolves an index, and an
// unresolved index sends iproute2 off to do a live single-get. Filling from
// the link dump's own replies means the cache is complete before any
// rendering, so goip emits exactly the two transactions and no side traffic.
// Here it matters more than for link show, because the address dump arrives
// between the fill and the render and it would be easy to render as replies
// are decoded.
//
// # Selectors
//
// The arguments are parseAddrShowArgs (addr_filter.go), the keyword loop of
// ipaddr_list_flush_or_save. Its `master`, `vrf` and device lookups go
// through newLinkNameResolver, ll_name_to_index with its cache, so they are
// on the wire as `ip` sends them: before the dumps, in argument order, with
// the device last. A resolved device takes addrShowDev's path.
//
// Otherwise the link dump carries what iplink_filter_req adds (the ext mask
// with `novf` applied, IFLA_MASTER, the kind nest) under preferred_family,
// and the address dump is sent with filter.family. Those are c.family and
// f.family, and `to PREFIX` is what makes them differ.
func addrShow(c *runCtx, args []string) error {
	// -br and -o parse but do not render yet (A3-T02). Refused before any
	// request, so nothing reaches the wire for a render goip cannot print.
	if opt := c.outputMode(); opt != "" {
		return fmt.Errorf("address show: %s render not implemented: %w", opt, ErrNotImplemented)
	}
	svc := service.New(c.src, c.nextSeq)
	f, err := parseAddrShowArgs(args, c.family, addrShowLookups(c, svc))
	if err != nil {
		return err
	}
	if f.ifindex != 0 {
		return addrShowDev(c, svc, &f)
	}

	linkResources, addrResources, err := svc.AddressSnapshotFilter(c.family, f.family, f.extMask(c.linkExtMask()), f.linkFilter())
	if err != nil {
		return err
	}
	links := make([]xtcpnl.LinkInfo, len(linkResources))
	for i := range linkResources {
		links[i] = xtcpnl.LinkInfo(linkResources[i])
	}
	c.lltab.Fill(links)

	var addrs []xtcpnl.AddrInfo
	if f.family != unix.AF_PACKET {
		addrs = make([]xtcpnl.AddrInfo, len(addrResources))
		for i := range addrResources {
			addrs[i] = xtcpnl.AddrInfo(addrResources[i])
		}
	}
	return renderAddrGroups(c, svc, &f, links, addrs)
}

// addrShowLookups is the parser's I/O: ll_name_to_index for `master`, `vrf`
// and the device, and name_is_vrf, which carries preferred_family.
func addrShowLookups(c *runCtx, svc *service.Service) addrLookups {
	return addrLookups{
		index: newLinkNameResolverRemember(svc, func(li xtcpnl.LinkInfo) {
			c.lltab.Fill([]xtcpnl.LinkInfo{li})
		}),
		isVRF: func(name string) (bool, error) { return svc.IsVRF(c.family, name) },
	}
}

// addrShowDev serves `ip addr show dev NAME` as the three transactions
// iproute2 sends, in iproute2's order.
//
// # Three requests, and the middle one is not the one you would guess
//
// `dev NAME` lands in the same catch-all else-arm every `addr` filter argument
// falls through to (ip/ipaddress.c:2241-2247) and is resolved by
// ll_name_to_index at :2253. On a cache miss that issues ll_link_get(name, 0)
// on a throwaway socket, exactly as `link show dev` does — request one, its
// reply thrown away apart from the cache entry. That request is the parser's
// (addrShowLookups), so by the time this runs f.ifindex is set; a bare device
// name and `dev NAME` arrive here the same way.
//
// filter.ifindex is now set, so :2302 takes the ipaddr_link_get branch instead
// of ip_link_list's dump: request two is a single-get addressed BY THE INDEX
// just learned, not by the name. It carries filter.family, not
// preferred_family, so `to PREFIX` reaches its header, and the `novf` mask. That is the one place the two `dev` commands
// diverge — `link show dev` re-fetches by name through iplink_get (:2293),
// this one re-fetches by index — and the difference is entirely in request
// bytes, so only pkg/nlparity's byte comparison can see it.
//
// Request three is the address dump, with the index written into ifa_index by
// ipaddr_dump_filter (:1954-1958). So the kernel does the address filtering
// here where `addr show` does it client-side, and addrBelongsTo below runs
// anyway, because `ip` applies both too.
//
// # The side-gets come after the dump, and the order is observable
//
// print_linkinfo runs in the loop at :2323-2336, which is after ip_addr_list
// at :2314. So a link with an IFLA_MASTER or an unshadowed IFLA_LINK emits its
// lazy ll_index_to_name single-gets as transactions FOUR and later, behind the
// address dump — where linkShowDev, having no dump to send, emits them second.
// Same function, same laziness, different position in the capture, and the
// harness compares positionally. Hence resolveLinkRefs is called here after
// Addresses returns and not before.
//
// The gated clean topology has neither a master nor a peer, so it sends
// exactly three; the mesh namespace is where the fourth would appear.
//
// # What `-s` adds here, and why it was one item rather than two
//
// `-s` reaches this command in `ip` twice over: ipaddr_link_get clears
// RTEXT_FILTER_SKIP_STATS from its mask (:2066-2067), and the print loop calls
// print_link_stats at :2333 under `!do_link && show_stats`. goip once
// implemented neither, and they had to land together — the mask alone would
// have made the request right and the output wrong, and the render alone the
// reverse.
//
// Both are here now. The mask arrives as c.linkExtMask() on the AddrLinkGet
// call below, and unlike the dump in addrShow it is stats-sensitive for EVERY
// family, because ipaddr_link_get addattr32s unconditionally where
// rtnl_linkdump_req_filter_fn skips the attribute entirely for a non-AF_UNSPEC
// family. Under `-s -4 addr show dev NAME` the two requests of this one
// command therefore DISAGREE, and that is correct; req.AddrShowLinkDump has
// the derivation.
//
// The render is AddrGroupView.Stats, and its position is the subtle half.
// :2333 comes AFTER print_selected_addrinfo at :2332, so `ip -s addr show`
// puts the stats block BELOW the address lines where `ip -s link show` puts it
// directly under the link stanza. LinkView.WithStats renders the second
// layout, so it is deliberately NOT used here: the block text comes from
// render.LinkStatsText and AddrGroupView emits it after its address lines.
// The newline moves with it — print_link_stats is block-then-newline
// (:840-848) where print_linkinfo is newline-then-block (:1297-1300).
func addrShowDev(c *runCtx, svc *service.Service, f *addrFilter) error {
	// Request two: ipaddr_link_get, by index, carrying filter.family.
	shown, err := svc.AddrLinkGet(f.family, f.ifindex, f.extMask(c.linkExtMask()))
	if err != nil {
		return err
	}
	link := xtcpnl.LinkInfo(shown)
	c.lltab.Fill([]xtcpnl.LinkInfo{link})

	// Request three: the address dump, filtered to this index by the kernel.
	// Skipped under AF_PACKET, the same condition and the same reason as in
	// addrShow — ip/ipaddress.c:2310 guards both.
	var addrs []xtcpnl.AddrInfo
	if f.family != unix.AF_PACKET {
		addrResources, aerr := svc.Addresses(f.family, uint32(link.Index)) //nolint:gosec // ifi_index into ifa_index
		if aerr != nil {
			return aerr
		}
		addrs = make([]xtcpnl.AddrInfo, len(addrResources))
		for i := range addrResources {
			addrs[i] = xtcpnl.AddrInfo(addrResources[i])
		}
	}

	return renderAddrGroups(c, svc, f, []xtcpnl.LinkInfo{link}, addrs)
}

// renderAddrGroups is the half of `addr show` that is the same whether one
// link arrived from a single-get or all of them arrived from a dump: apply
// ipaddr_filter and group_filter, then print_linkinfo's and print_addrinfo's
// tests per link, group the addresses under their links, and write.
//
// addrs is nil under AF_PACKET, which is not a special case here — the
// per-link inner loop simply runs zero times, and ipaddr_filter is skipped
// because `ip` skips it for that family too (ip/ipaddress.c:2310-2318 guards
// the dump and the filter together). group_filter is not skipped.
//
// Every family test here is filter.family, f.family, including the one that
// decides whether the `link/` line prints: print_linkinfo reads
// filter.family, so `to 192.0.2.0/24` drops that line although the link dump
// went out AF_UNSPEC.
func renderAddrGroups(c *runCtx, svc *service.Service, f *addrFilter, links []xtcpnl.LinkInfo, addrs []xtcpnl.AddrInfo) error {
	links = f.keepLinks(links, addrs, c.lltab.IndexToName, f.family != unix.AF_PACKET)

	// Both of these loops range by index rather than by value. Both resource
	// structs are large, and an AF_UNSPEC `addr show` runs the inner loop
	// len(links) x len(addrs) times — so the value form copies both structs
	// on every one of those iterations to read a few fields off each.
	groups := make([]render.AddrGroupView, 0, len(links))
	printed := make([]xtcpnl.LinkInfo, 0, len(links))
	for i := range links {
		mode := f.linkMode(&links[i])
		if mode == addrLinkSkip {
			continue
		}
		g := render.AddrGroupView{
			// Non-nil so the JSON is `"addr_info": []` rather than null for a
			// link with no addresses. iproute2 opens the array
			// unconditionally (open_json_array at the top of
			// print_selected_addrinfo), so an empty array is what `ip -j`
			// emits and null would be a key-shape divergence.
			AddrInfo:   []render.AddrView{},
			Headerless: mode == addrLinkHeaderless,
		}
		if mode == addrLinkPrint {
			// print_linkinfo's side-gets, so a link it skips, or one `label`
			// leaves headerless, sends none. After every dump, in link
			// order, as `ip` prints. Nothing is sent when the link dump was
			// complete; a dump the kernel filtered by `type` or `master`
			// can leave a master out of the cache.
			resolveLinkRefs(c, svc, links[i])
			// Two different flags on two lines, and they are easy to read as
			// one.
			//
			// LinkViewForAddr's last argument is show_details, which here
			// restores the `link/` line that -4 and -6 suppress
			// (ip/ipaddress.c:1060). WithDetail's is do_link, which is FALSE
			// on this path however many -d's were given: do_link is set by
			// the `ip link show` entry point alone (:2417), and it suppresses
			// exactly one token of the detail run, addrgenmode, via
			// print_af_spec's guard at :1185-1186. The committed pair is the
			// evidence — ip_link_n has addrgenmode on every link and ip_addr_n
			// on none.
			lv := render.LinkViewForAddr(links[i], c.lltab, f.family, c.detailed())
			if c.detailed() {
				lv = lv.WithDetail(links[i], false, c.lltab)
			}
			g.LinkView = lv
			printed = append(printed, links[i])
		}
		for j := range addrs {
			if !f.matchAddr(&addrs[j], &links[i], c.lltab.IndexToName) {
				continue
			}
			g.AddrInfo = append(g.AddrInfo, render.AddrViewOf(addrs[j]))
		}
		// After the addresses, never through LinkView.WithStats: `ip` calls
		// print_link_stats at ip/ipaddress.c:2333, one line BELOW
		// print_selected_addrinfo at :2332, so the block sits under the
		// address lines rather than under the link stanza. The caller's half
		// of the condition is `res > 0 && !do_link && show_stats`, and do_link
		// is false on every addr path however many -s were given.
		if mode == addrLinkPrint && c.showStats > 0 {
			g = g.WithStats(links[i])
		}
		groups = append(groups, g)
	}
	// Only a printed header can carry the -d detail goip refuses.
	if err := checkDetailSupported(c, printed); err != nil {
		return err
	}

	if c.json {
		return json.NewEncoder(c.out).Encode(groups)
	}
	for i := range groups {
		if _, werr := fmt.Fprint(c.out, groups[i].Text()); werr != nil {
			return werr
		}
	}
	return nil
}
