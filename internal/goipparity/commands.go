// Package goipparity is the implementation behind cmd/goip-parity: the
// comparator that decides whether goip asked the kernel the same questions
// `ip` did, and whether it rendered the same objects back.
//
// It compares; it never captures. That split is the plan's Risk 8 and it is
// structural rather than conventional: a comparator that could open a netlink
// socket could put its own traffic inside a capture window and have it
// attributed to goip. imports_test.go asserts the property three ways — no
// direct import of internal/goip, no path to it through the import closure,
// and no socket call written in this package at all.
//
// Three files, three jobs:
//
//   - commands.go is the command table: the single source of truth for what
//     gets captured, what the capture files are called, and the allowlist key.
//     `goip-parity commands` prints it so the guest driver iterates the same
//     list the comparator reads.
//   - stdout.go is the structural stdout comparison, the plan's Risk 1
//     override. Netlink parity alone is reply-independent: a goip that sends
//     byte-identical requests and discards every reply is a perfect green.
//   - compare.go walks a capture directory, runs both comparisons per command
//     and renders the sentinels the in-guest runner greps for.
//
// The netlink comparison itself is pkg/nlparity's; nothing here duplicates it.
package goipparity

import (
	"fmt"
	"sort"
	"strings"
)

// Command is one row of the parity harness's command table: a command both
// tools are driven with, the capture files it produces, and whether goip
// implements it yet.
//
// # Why one table, read by both the capture driver and the comparator
//
// The guest driver has to know which commands to capture, and the comparator
// has to know which captures to expect and what to call them when it looks an
// entry up in the allowlist. Those are the same list, and keeping two copies
// of it in two languages is how a command quietly stops being compared: the
// shell captures `ip -6 route show`, the comparator looks for `route_show_v6`,
// nothing matches, and a skipped command looks exactly like a passing one.
//
// So the table lives here, in Go, and `goip-parity commands` prints it for the
// shell to iterate. There is one list and the driver cannot disagree with it.
type Command struct {
	// Name is the allowlist key and the human-facing name. It is the argv
	// spelled as a phrase, because that is what the allowlist's `command`
	// field already holds and what a report reads best.
	Name string

	// Slug is the filename stem. Deliberately a separate field rather than
	// derived from Name by substitution: a derivation has to decide what to do
	// with the space in `route show table all` and the dash in `-6 addr show`,
	// and a rule that silently maps two names onto one filename would have one
	// capture overwrite the other. TestCommandTable asserts they are unique.
	Slug string

	// Args is the argv, minus the program name. Both tools get exactly this,
	// which is the property that makes the comparison meaningful — see
	// internal/goip's matchesPrefix, which exists so goip parses it the way
	// iproute2 does.
	Args []string

	// NeedsDev marks a command whose argv ends in the topology's device name,
	// appended by the driver. Kept out of Args because the device is a
	// property of the capture namespace, not of the command.
	NeedsDev bool

	// Floor is the minimum datagram count a usable capture of this command
	// holds, passed through to the guest's `xtcp2-nlcap cap`. A capture under
	// its floor is discarded rather than written, so a missed capture window
	// cannot silently replace a good fixture.
	Floor int

	// Implemented is whether goip can run this command today. A command that
	// is not implemented is REPORTED AND SKIPPED, never silently absent: the
	// distinction between "goip has not got there yet" and "this command was
	// never attempted" is the same one internal/goip draws between
	// ErrNotImplemented and ErrUnknownObject, and for the same reason.
	Implemented bool
}

// Sides are the three captures of the control triple, in capture order.
//
// The order is load-bearing and is the plan's: ip, then goip, then ip again.
// D_control = diff(ip_a, ip_b) is then a measurement of what changed across a
// window that CONTAINS the goip run, rather than across an adjacent one, so a
// kernel value that drifted during the goip capture is inside the control's
// interval rather than beside it.
const (
	SideIPA  = "ip_a"
	SideGoip = "goip"
	SideIPB  = "ip_b"
)

// Sides in capture order.
func Sides() []string { return []string{SideIPA, SideGoip, SideIPB} }

// commands is the table.
//
// The names match pkg/nlparity/goip-parity-allowlist.json's `command` field
// exactly, and TestCommandsCoverAllowlist fails if an allowlist entry names a
// command that is not here — an entry for a command nobody runs suppresses
// nothing and would sit there looking like a decision.
//
// Floors are the ones the capture driver already uses for the same commands
// (nix/microvms/scripts/capture-netlink-dumps.exp), because they were measured
// against the same topology.
var commands = withArgs([]Command{
	{
		Name: "link show", Slug: "link_show",
		// Two datagrams: the request and the multipart reply. `link show`
		// has no oversend and no side transaction — see internal/goip's
		// linkShow, which fills the index cache from the dump's own replies
		// precisely so that stays true.
		Floor: 2, Implemented: true,
	},
	{
		Name: "-4 link show", Slug: "link_show_v4",
		// Two, and byte-identical to `link show`'s two — which is the whole
		// point of the row.
		//
		// ipaddr_list_link OVERWRITES preferred_family with AF_PACKET
		// (ip/ipaddress.c:2416) before it parses a single argument, so
		// whatever -4 set is gone by the time the request is built. `-4 link
		// show`, `-6 link show` and `link show` therefore put the same 40
		// bytes on the wire, and `ip -0 link show` would too.
		//
		// # What this row can find that no stdout comparison can
		//
		// The output is identical too, so a goip that honored -4 here — by
		// filtering links, or by setting ifi_family in the request — would
		// print the same text on this topology, where every link has both
		// families. The L2 request comparison sees the changed byte
		// immediately. goip models the overwrite by having req.LinkShowDump
		// ignore c.family entirely, and this is the row that says so out loud
		// rather than in a comment.
		//
		// No new fixture: the harness captures its own triple at runtime.
		Floor: 2, Implemented: true,
	},
	{
		Name: "-6 link show", Slug: "link_show_v6",
		// The other arm, and not a duplicate of the row above: `-4` and `-6`
		// are separate assignments in the option loop, so a goip that
		// forwarded one and not the other is a state this table can reach and
		// a single row cannot.
		Floor: 2, Implemented: true,
	},
	{
		Name: "-s link show", Slug: "link_show_stats",
		// Two, exactly as `link show`: `-s` changes one byte of the request
		// and nothing about how many are sent. It clears
		// RTEXT_FILTER_SKIP_STATS from IFLA_EXT_MASK
		// (ip/ipaddress.c:2017-2026) and does not reach ll_link_get or
		// ll_init_map, whose masks are local constants
		// (lib/ll_map.c:277,395).
		//
		// # Expected to be CONTROL_NOISY, permanently
		//
		// Every other command here is noisy by accident, when a counter
		// happens to be sampled between the two reference captures. This one
		// is noisy by construction: its replies carry live byte and packet
		// counters, so IFLA_STATS and IFLA_STATS64 differ between any two
		// runs and D_control must absorb them every time. That makes it the
		// first real test of the control subtraction rather than a problem
		// with it, and it is the reason this command did not join
		// gated_commands when it was written — it had to earn that on its own
		// measured runs, once the noise was observed rather than predicted.
		//
		// It has, and it is gated. Three back-to-back runs of one unmodified
		// tree measured nl=2, nl=2, nl=6, zero findings every time, with every
		// other command's control count unmoved. The prediction above is
		// therefore confirmed in the only way it could be: by runs that
		// DISAGREE. Three identical quiet runs would not separate "D_control
		// absorbed the delta" from "there was no delta to absorb", so for this
		// command varying samples are the stronger evidence and identical ones
		// the weaker. Run 3's extra four loci were ifindex 3 ticking as well as
		// ifindex 2, which dragged IFLA_INET6_STATS and IFLA_INET6_ICMP6STATS
		// in behind IFLA_STATS and IFLA_STATS64; the values are recorded in
		// pkg/nlparity/goip-parity-allowlist.json's _comment.
		//
		// The stdout half is not noisy in the same way, because
		// FacetStatsHeaders compares the column HEADINGS and not the
		// counters or their widths. See stdout.go.
		Floor: 2, Implemented: true,
	},
	{
		Name: "-d link show", Slug: "link_show_details",
		// Two, exactly as `link show`, and this time the request is not one
		// byte different — it is IDENTICAL. show_details is not one of the
		// two variables iplink_filter_req reads (ip/ipaddress.c:2017-2026),
		// so `-d link show` and `link show` put the same 40 bytes on the
		// wire and the kernel answers both the same way.
		//
		// # This row is stdout, and it is the widest stdout row in the table
		//
		// That makes it the mirror image of `-4 link show`, whose whole value
		// is on the request side because its output is identical. Here the
		// request comparison is a control — it should be byte-identical to
		// link_show's, and a goip that changed a request byte for -d would be
		// wrong — while the output grows by sixteen tokens per link, two of
		// them opening continuation lines.
		//
		// # Why it is worth a live triple when eight goldens already exist
		//
		// TestLinkShowDetailMatchesSidecar compares the same render against
		// the committed ip_link_n, and has to substitute one field to do it:
		// nlmon0's promiscuity, which the pcap records as 1 because tcpdump
		// was listening and the text sidecar records as 0 because it was not.
		// Here both sides run inside one capture window, so the substitution
		// is unnecessary and the comparison is exact — the only place in the
		// project where that field is checked rather than excused.
		Floor: 2, Implemented: true,
	},
	{
		Name: "link show dev", Slug: "link_show_dev",
		NeedsDev: true,
		// Four datagrams, not two: this command sends TWO single-gets, each
		// with its own reply. ll_name_to_index resolves `dev NAME` with
		// ll_link_get on a throwaway socket (ip/ipaddress.c:2254), and
		// iplink_get then re-fetches the same link on the main socket, whose
		// reply is the one print_linkinfo renders (:2293).
		//
		// The floor was 2, which is a floor that cannot do its job: a capture
		// window that caught only one of the two transactions would clear it
		// and be written as a good fixture. The capture driver's value moved
		// with this one (nix/microvms/scripts/capture-netlink-dumps.exp:137).
		Floor: 4, Implemented: true,
	},
	{
		Name: "addr show", Slug: "addr_show",
		// Four: two transactions (ll_init_map's link dump, then the
		// addresses), each a request and at least one reply datagram.
		Floor: 4, Implemented: true,
	},
	{
		Name: "-4 addr show", Slug: "addr_show_v4",
		Floor: 4, Implemented: true,
	},
	{
		Name: "-6 addr show", Slug: "addr_show_v6",
		Floor: 4, Implemented: true,
	},
	{
		Name: "-s addr show", Slug: "addr_show_stats",
		// Four, like the bare form: `-s` adds no transaction, it changes one
		// byte of one request. The AF_UNSPEC addr dump's link half carries
		// IFLA_EXT_MASK, so clearing RTEXT_FILTER_SKIP_STATS sends 0x01 where
		// `addr show` sends 0x09 (ip/ipaddress.c:2017-2026).
		//
		// Expected CONTROL_NOISY on the netlink side for the same reason
		// `-s link show` is: the replies now carry IFLA_STATS and
		// IFLA_STATS64, which differ between any two captures. The stdout
		// half should be quiet, because FacetStatsHeaders compares the column
		// HEADINGS and not the counters under them.
		//
		// MEASURED over three runs, and the prediction holds: stdout 0 every
		// time, and nl 2, 2, **6**. The two are IFLA_STATS64 and IFLA_STATS
		// on ifindex 2; the six are that pair on ifindex 2 AND on ifindex 3,
		// plus IFLA_AF_SPEC:AF_INET6's IFLA_INET6_STATS and
		// IFLA_INET6_ICMP6STATS — ifindex 3 being the v6-active link, so a
		// tick there drags the nest in with it. That is `-s link show`'s
		// run-3 shape attribute for attribute, which the allowlist _comment
		// already describes for that row, and this is the second command
		// measured showing it.
		//
		// Two runs would have recorded this row as stable at 2 and been
		// wrong about which KIND of row it is: permanently noisy with
		// variance, not quiet. The third run caught it, which is why a moved
		// sentinel mandates one.
		//
		// `addr show` measures nl=0, so `-s` is what added the noise — and
		// `-4 addr show` is the control that keeps that from being
		// over-read, because it measures nl=2 WITHOUT `-s`: a non-AF_UNSPEC
		// family sends no mask at all, so RTEXT_FILTER_SKIP_STATS is absent
		// and the counters arrive unbidden.
		Floor: 4, Implemented: true,
	},
	{
		Name: "-s -6 addr show", Slug: "addr_show_v6_stats",
		// Four, and the request is BYTE-IDENTICAL to `-6 addr show` — not
		// merely the same length. rtnl_linkdump_req_filter_fn skips filter_fn
		// unless the family is AF_UNSPEC or AF_PACKET
		// (lib/libnetlink.c:595), so neither form sends a mask at all.
		//
		// Which makes this the one `-s` row where the request half asserts a
		// no-op and the stdout half asserts a feature. The reply is identical
		// too: inet6_fill_ifinfo calls inet6_fill_ifla6_attrs with
		// ext_filter_mask hardcoded to 0 (net/ipv6/addrconf.c:6110), so
		// IFLA_INET6_STATS is on the wire with or without `-s` and only the
		// rendering gate differs.
		//
		// Expected noisy on the netlink side anyway, WITHOUT `-s` being in
		// the request — the MIB counters tick on their own.
		//
		// MEASURED, and that prediction was WRONG: `control: nl=0 stdout=0`.
		// Quiet on both halves, where `-s addr show` measures `nl=2`. The
		// reason is the same two-counter-sets fact from the other direction —
		// IFLA_INET6_STATS is an IPv6 MIB, and in a namespace that sends no
		// IPv6 during the run its counters do not move, where the sysfs link
		// counters in IFLA_STATS64 move constantly. "A stats attribute is on
		// the wire" does not imply "it ticks"; which counter it is decides
		// that, and this row is the pair's control.
		//
		// What it DID find is a real divergence, which is the better outcome
		// for a row held out of gated_commands: GOIP_PARITY_WARN with
		// `stdout:keyword:qlen: ip=1000 x2 goip=<absent>`. That is
		// faceb326's ioctl fallback reaching a second command — structural,
		// unmatchable by a netlink-only tool, and already the `-6 addr show`
		// allowlist entry. It now has its own entry, earned by this run
		// rather than predicted into existence.
		//
		// The counters it prints are IPv6 MIB counters, not link counters,
		// because a PF_INET6 link dump is answered by inet6_fill_ifinfo
		// (net/ipv6/addrconf.c:6073-6117) and never by rtnl_fill_ifinfo. The
		// `dev` form of the same command prints real link counters under the
		// same header; see docs/netlink/coverage-status.md.
		Floor: 4, Implemented: true,
	},
	{
		Name: "-d addr show", Slug: "addr_show_details",
		// Four, the same as `addr show`: -d reaches no request.
		//
		// # The row that isolates do_link
		//
		// Its output is `-d link show`'s detail run minus exactly one token,
		// addrgenmode, because print_af_spec is guarded on `do_link &&
		// tb[IFLA_AF_SPEC]` (ip/ipaddress.c:1185-1186) and do_link is set
		// only by the `ip link show` entry point (:2417). The same variable
		// also removes `mode DEFAULT` from the stanza line, so one flag
		// accounts for two absences in two different parts of the output.
		//
		// Worth its own triple because goip passes that flag explicitly —
		// obj_addr calls WithDetail(li, false) where obj_link calls it with
		// true — and an inverted argument would show up here and in exactly
		// one committed golden.
		Floor: 4, Implemented: true,
	},
	{
		Name: "addr show dev", Slug: "addr_show_dev",
		NeedsDev: true,
		// Six, and the count is half the assertion. `link show dev` sends two
		// single-gets; this sends the first of those, then a DIFFERENT second
		// one, then a dump: ll_link_get to resolve the name
		// (ip/ipaddress.c:2253), ipaddr_link_get by INDEX rather than
		// iplink_get by name (:2302), and the address dump with that index in
		// ifa_index (:2314, :1954-1958). Three transactions, each a request
		// and at least one reply.
		//
		// Measured: seven DATAGRAMS on the clean topology, carrying ten
		// MESSAGES — three requests, two single-get replies, and an address
		// dump of four RTM_NEWADDR plus NLMSG_DONE packed into two. The
		// floor counts datagrams, so six leaves one of margin, which is what
		// a floor is for. The two numbers differ because the kernel packs a
		// multipart dump, and a floor written against the message count
		// would have been a floor this command could not clear.
		//
		// # The one command where two single-gets for one interface differ
		//
		// Requests one and two ask the kernel about the same link within
		// microseconds and are not the same bytes. ll_link_get's ifinfomsg is
		// a designated initializer naming ifi_index alone, so its family is
		// AF_UNSPEC; ipaddr_link_get sets ifi_family = filter.family (:2058).
		// Nothing in either reply, and nothing in any output, reflects that
		// byte. Full request equality is the only thing that can see it,
		// which makes this row a test of the comparator as much as of goip.
		//
		// The stdout half is nearly free: the stanza is print_linkinfo's with
		// do_link still 0, which is the rendering `addr show` already gets
		// right, restricted to one link.
		Floor: 6, Implemented: true,
	},
	{
		Name: "route show", Slug: "route_show",
		// Four: the route dump, then one lazy RTM_GETLINK single-get for the
		// one ifindex the listing mentions. iproute_list_flush_or_save never
		// calls ll_init_map (ip/iproute.c:1819), so there is no up-front link
		// dump to count — obj_route.go reproduces that laziness rather than
		// dumping, because a dump would keep the datagram count plausible
		// while sending an entirely different request.
		Floor: 4, Implemented: true,
	},
	{
		Name: "route show table all", Slug: "route_show_table_all",
		// Six in the gated topology, because table all reaches lo as well.
		// The floor stays 4: it is a floor, and the second device is a
		// property of the topology rather than of the command.
		Floor: 4, Implemented: true,
	},
	{
		Name: "-6 route show", Slug: "route_show_v6",
		Floor: 4, Implemented: true,
	},
	{
		Name: "-s route show", Slug: "route_show_stats",
		// A NO-OP row, and the reason is worth stating because the sweep that
		// added it predicted the opposite.
		//
		// print_rta_cacheinfo gates exactly three members on show_stats —
		// rta_clntref (`users`), rta_used and rta_lastuse (`age`),
		// ip/iproute.c:500-532 — and all three are written only inside
		// rtnl_put_cacheinfo's `if (dst)` arm
		// (net/core/rtnetlink.c:1028-1052). No route DUMP ever takes that
		// arm: rt_fill_info passes a dst on a `route get` only and the v4 FIB
		// dump never calls it at all (net/ipv4/route.c:3074), while
		// rt6_fill_node serves both the v6 dump and the get but passes a dst
		// only on the get (net/ipv6/route.c:5944). So the three members `-s`
		// would print are structurally zero on every command here.
		//
		// Measured, not assumed: v4 dumps carry no RTA_CACHEINFO at all, v6
		// dumps carry one per route with all 32 bytes zero, 48 of 74 routes
		// in the committed corpus, every one all-zero.
		//
		// Expected as quiet as `route show` — control nl=0 stdout=0. The
		// sweep's plan predicted this row would be the harness's first
		// stdout-noisy command; it is not, and `-s neigh show` below is the
		// only new row that can be.
		//
		// What the measurement DID find is in the ungated form: rta_expires
		// is the one member reachable without a dst
		// (net/ipv6/route.c:5931) and print_rta_cacheinfo prints it OUTSIDE
		// show_stats, so plain `-6 route show` was dropping `expires Nsec`.
		// Fixed there, not here. This row cannot see it — no route in the
		// gated topology has a finite lifetime, which is exactly why the bug
		// survived as long as it did.
		//
		// MEASURED: `control: nl=0 stdout=0`, identical to `route show`'s.
		// The no-op claim holds on the wire and on stdout, which is what the
		// corrected premise predicted. The plan's ORIGINAL prediction for
		// this row was "noisy on the stdout side", and it was wrong twice
		// over — once because `-s route show` renders nothing new at all,
		// and once because the three members it would have rendered are
		// structurally zero on a dump.
		Floor: 4, Implemented: true,
	},
	{
		Name: "-d route show", Slug: "route_show_details",
		// Four, the same as `route show`, and the request is identical:
		// show_details reaches nothing iproute_dump_filter writes.
		//
		// # A different KIND of -d from the link one
		//
		// The link object's -d adds attributes to the output. This one adds
		// nothing new at all — it UNSUPPRESSES four tokens the plain form
		// hides because their value is the default: the route type at
		// ip/iproute.c:828, `table` at :903, `proto` at :909 and `scope` at
		// :916, each behind the identical guard `(X != DEFAULT ||
		// show_details > 0)`.
		//
		// So the delta on this topology is `unicast` on all six lines and
		// `proto boot scope global` on the five that were defaulting. Every
		// value involved was already decoded and already correct; what -d
		// tests is the suppression logic, which nothing else can reach —
		// a renderer that printed the defaults unconditionally passes every
		// plain golden only because the guards exist.
		//
		// `table main` is NOT in that delta, because the token needs
		// filter.tb == 0 as well and a bare `route show` defaults it to
		// RT_TABLE_MAIN. Only `-d route show table all` shows it, which is
		// why the offline test covers that form and this row does not: the
		// harness compares one argv, and this is the one whose suppression
		// arithmetic has three conjuncts rather than two.
		Floor: 4, Implemented: true,
	},
	{
		Name: "route show dev", Slug: "route_show_dev",
		NeedsDev: true,
		// Four: two transactions, each a request and at least one reply
		// datagram. ll_name_to_index's throwaway ll_link_get
		// (ip/iproute.c:2008), then the dump with RTA_TABLE and RTA_OIF.
		//
		// # The only row whose selector makes the command CHEAPER
		//
		// Measured on the clean topology: five datagrams carrying ten
		// messages — exactly what the bare `route show` measures. The two are
		// not the same five, and the difference is the entire point of this
		// row. `route show` sends the dump FIRST and then one lazy
		// RTM_GETLINK at the END, by index, to name the interface every route
		// prints. `route show dev` sends an RTM_GETLINK FIRST, by name, to
		// resolve the selector — and then sends none at all, because
		// print_route's `dev` token is guarded on `filter.oifmask != -1`
		// (:900-901) and that token is the only caller of ll_index_to_name
		// for RTA_OIF.
		//
		// So the get moved from the back to the front and changed from
		// by-index to by-name, and a counting comparator sees nothing. Only
		// positional request equality can tell these two commands apart on
		// the wire, which makes this row a test of the comparator as much as
		// of goip — the same property `addr show dev` carries for a different
		// reason.
		//
		// It also means the saving GROWS with the answer: the bare form costs
		// one get per distinct output interface, this one costs exactly two
		// transactions however many routes come back. The mesh capture, whose
		// device owns no routes at all, still sends the same two requests and
		// lands exactly on the floor of four.
		//
		// The stdout half is not a slice of `route show`'s. The main lines
		// LOSE their `dev NAME` token, while a multipath route KEEPS one on
		// every nexthop — the nexthop tokens at :743 and :751 have no guard —
		// so the word the command named appears nowhere on the lines it
		// selected and everywhere on the lines it did not.
		Floor: 4, Implemented: true,
	},
	{
		Name: "neigh show", Slug: "neigh_show",
		Floor: 4, Implemented: true,
	},
	{
		Name: "-s neigh show", Slug: "neigh_show_stats",
		// Four, and the request is byte-identical to `neigh show`:
		// req.NeighShowDump takes family, ndmFlags, ifindex and seq, and no
		// mask. `-s` is output-only here, gating both the cacheinfo block and
		// `probes` (ip/ipneigh.c:453-460).
		//
		// # The first row expected to be noisy on the STDOUT side
		//
		// Every other CONTROL_NOISY row in this table is noisy on the netlink
		// side. This one's output carries used/confirmed/updated, which are
		// all "ticks since" and move between ip_a and ip_b by construction,
		// so D_control has to absorb a steady-state stdout delta — a path
		// that has never carried one before. That makes this row a test of
		// the stdout control subtraction as much as of goip, which is the
		// reason it is in the table at all and not just in a golden.
		//
		// `-s route show` above was predicted to be the other such row and
		// turned out to be a no-op, so this is the only one.
		//
		// Two formatting facts it is the only live check of: the block lands
		// BETWEEN the flag run and the state rather than at the end of the
		// line (:453-465), and print_cacheinfo's formats carry a leading
		// space with no trailing one while `probes %u ` is the reverse, so a
		// conforming renderer emits two spaces after the lladdr and runs the
		// counters straight into `probes`. A keyword-set comparison passes
		// either way; only a live diff against `ip` sees it.
		//
		// MEASURED: `control: nl=2 stdout=0`. The stdout half was predicted
		// NOISY — `used`, `confirmed` and `updated` are "seconds since" and
		// were expected to tick between ip_a and ip_b — and it was quiet. The
		// prediction forgot the division: print_cacheinfo divides each by
		// USER_HZ and prints an integer, so all three only move once per
		// second, and the three invocations of a triple complete in well
		// under that. So the stdout control-diffing path that this row was
		// meant to be the first real test of is STILL untested, and this row
		// does not establish that it works — it establishes that no command
		// in the table has yet produced steady-state stdout noise.
		//
		// The netlink nl=2 is not `-s`'s doing either: plain `neigh show`
		// measures nl=2 as well, from ll_init_map's link dump, whose mask is
		// RTEXT_FILTER_VF with or without `-s`. The pair of rows is what
		// says so; this row alone would have read as `-s` adding noise.
		Floor: 4, Implemented: true,
	},
	{
		Name: "-d neigh show", Slug: "neigh_show_details",
		// Four, and the only row in this table whose OUTPUT is expected to
		// be byte-identical to another row's.
		//
		// # A row that asserts an absence, live
		//
		// ip/ipneigh.c does not contain the identifier show_details — not
		// once — so `ip -d neigh show` and `ip neigh show` print the same
		// bytes. The committed pair agrees: ip_neigh and ip_neigh_n are the
		// same file.
		//
		// That is exactly why it needs a triple rather than a unit test. "-d
		// changed nothing" and "-d was dropped on the floor" produce the same
		// goip output here, and the offline test can only compare goip
		// against goip or against a golden captured from the same claim. This
		// row compares goip against a LIVE `ip` that was also given -d, which
		// is the only evidence that the absence is iproute2's and not an
		// assumption both sides of the fixture inherited.
		//
		// It is also the cheapest row to be wrong about: if a future
		// iproute2 adds a detail token to print_neigh, this fails and the
		// three other -d rows do not.
		Floor: 4, Implemented: true,
	},
	{
		Name: "neigh show dev", Slug: "neigh_show_dev",
		NeedsDev: true,
		// Four, the SAME floor as the row above — and that equality is the
		// assertion rather than an oversight.
		//
		// # The only `dev` row whose selector is free
		//
		// The other three cost something. `link show dev` and `addr show dev`
		// each add a throwaway ll_link_get at the front; `route show dev`
		// adds one there and deletes the lazy ones at the back. This one adds
		// nothing, because do_show_or_flush calls ll_init_map(&rth)
		// unconditionally at ip/ipneigh.c:597 — for the bare command as much
		// as for this one — and only then resolves the name at :600, out of
		// the cache that dump just filled. ll_get_by_name hits, so
		// ll_link_get is never reached (lib/ll_map.c:354-359).
		//
		// Two transactions either way, the first byte-identical, and the
		// whole difference is 8 bytes of NDA_IFINDEX on the second. That is
		// the narrowest delta any row in this table asserts, and it is
		// invisible to each half of the comparison in turn: the request delta
		// produces no output, and the output delta — the `dev` token,
		// suppressed by print_neigh's :415 guard — produces no request.
		// Neither half alone would catch a goip that implemented one and not
		// the other.
		//
		// The index also travels as an ATTRIBUTE rather than in the
		// ndm_ifindex field `struct ndmsg` already has (:493). A goip that
		// filled the field would send a well-formed 28-byte request the
		// kernel filters on nothing, and only byte equality would say so.
		Floor: 4, Implemented: true,
	},
	{
		Name: "neigh show proxy", Slug: "neigh_show_proxy",
		// Four again, for the third time on this object, and again the
		// equality is the point: `proxy` changes one BYTE of one request and
		// nothing else about the shape of the exchange.
		//
		// # The narrowest request delta in the table, and the widest reply one
		//
		// `neigh show dev` was the previous narrowest at 8 bytes. This is one:
		// ndm_flags at offset 10 of a struct the bare command already sends
		// (ip/ipneigh.c:490), so both datagrams are 28 bytes and differ in a
		// single position. No attribute appears and no transaction moves.
		//
		// What comes back is not a subset. The kernel tests that byte for
		// EQUALITY with NTF_PROXY (net/core/neighbour.c:2956) and dispatches to
		// pneigh_dump_table instead of neigh_dump_table, so the two commands
		// walk DIFFERENT tables and return disjoint sets. That asymmetry is
		// what makes the row worth a capture: a goip that dropped the byte
		// would send a well-formed request, get a well-formed reply, and print
		// the wrong table's contents — and the stdout half would catch it only
		// because the two tables happen to be disjoint in this topology.
		//
		// # What the harness would actually see if goip got the byte wrong
		//
		// Both sides set NETLINK_GET_STRICT_CHK — `ip` at ip/ip.c:312, goip
		// at internal/goip/source.go:93 — and under it the kernel rejects
		// `ndm_flags & ~NTF_PROXY` with EINVAL rather than falling back to
		// the equality above (net/core/neighbour.c:2903-2906). So a goip that
		// or-ed NTF_PROXY into another bit fails LOUDLY: a one-byte request
		// divergence and an empty, errored goip side. A goip that dropped the
		// byte entirely fails quietly instead — a valid request, a valid
		// reply, and the wrong table's contents — and only the request half
		// of the comparison names the cause.
		Floor: 4, Implemented: true,
	},
	{
		Name: "rule show", Slug: "rule_show",
		// TWO, and it is the only object in this table with that floor on its
		// bare listing — every other one is four.
		//
		// # One transaction, because there is no name to resolve
		//
		// iprule_list_flush_or_save calls no ll_init_map
		// (ip/iprule.c:745-800), and it cannot need one: FRA_IIFNAME and
		// FRA_OIFNAME arrive as STRINGS, so print_rule has no index to turn
		// into a name. `ip neigh show` needs two transactions for exactly the
		// attribute this object does not have. So the whole exchange is one
		// request and one multipart reply, and the floor is the request plus
		// the datagram carrying NLMSG_DONE.
		//
		// # The request is 28 bytes with no attribute, and cannot be anything else
		//
		// rtnl_ruledump_req (lib/libnetlink.c:407-421) sends a 16-byte
		// nlmsghdr and a 12-byte fib_rule_hdr with only the family byte set.
		// The kernel does not merely ignore an attribute here, it REFUSES
		// one: fib_valid_dumprule_req errors with "Invalid data after header
		// in fib rule dump request" whenever nlmsg_attrlen is nonzero
		// (net/core/fib_rules.c:1278-1281), and rejects a nonzero dst_len,
		// src_len, tos, table, res1, res2, action or flags at :1271-1276.
		//
		// That makes this the most constrained request in the table: there is
		// exactly one legal encoding, and eight of the twelve header bytes
		// are validator-enforced zeros.
		//
		// # Every selector is client-side, which is why none of them is a row
		//
		// `from`, `to`, `iif`, `oif`, `pref`, `fwmark`, `uidrange` and the
		// rest are parsed into `filter` and applied to REPLIES by filter_nlmsg
		// (ip/iprule.c:98-243). None reaches the wire — they could not, per
		// the paragraph above — so a `rule show pref N` row would compare two
		// identical requests and a narrowed listing, which is a stdout test
		// wearing a triple's clothes. goip rejects them rather than filtering,
		// and obj_rule.go's parseRuleShowArgs says why.
		Floor: 2, Implemented: true,
	},
	{
		Name: "-4 rule show", Slug: "rule_show_v4",
		// Two, and byte-identical to the row above in BOTH halves.
		//
		// iprule_list_flush_or_save substitutes AF_INET for AF_UNSPEC before
		// it builds anything (ip/iprule.c:748-752), so a bare `ip rule show`
		// already asks for IPv4 rules and `-4` changes nothing at all — not
		// one byte of the request, not one character of the output.
		//
		// A row that asserts an identity, then, and the identity is load
		// bearing rather than decorative. The kernel's strict-mode validator
		// checks every fib_rule_hdr field EXCEPT family (:1271-1276), so a
		// goip that skipped the substitution would send AF_UNSPEC, be
		// answered, and print every family's rules — a longer listing that
		// errors nowhere. This row and the one above are what make that
		// visible: the request halves must match each other, and the stdout
		// halves must too.
		Floor: 2, Implemented: true,
	},
	{
		Name: "-6 rule show", Slug: "rule_show_v6",
		// Two, and the one family byte is the whole delta — the same shape as
		// `-6 route show`, at a quarter the datagram count.
		//
		// The reply is where it stops being symmetric. IPv6 ships TWO default
		// rules, local and main, where IPv4 ships three: fib_default_rules_init
		// adds a `default` rule at priority 32767 for IPv4 only
		// (net/ipv4/fib_rules.c) and IPv6 has no equivalent
		// (net/ipv6/fib6_rules.c). So the two listings differ in length before
		// the topology adds anything, which is a fact about the kernel that
		// only a live comparison states.
		Floor: 2, Implemented: true,
	},
	{
		Name: "-d rule show", Slug: "rule_show_details",
		// Two, and the request is IDENTICAL — show_details reaches nothing
		// rtnl_ruledump_req writes, the same as the -d rows for route and
		// neigh.
		//
		// # The cleanest presence-versus-value case in the corpus
		//
		// print_rule's protocol guard is `(protocol && protocol !=
		// RTPROT_KERNEL) || show_details` (ip/iprule.c:551-557), and every
		// rule the topology adds carries FRA_PROTOCOL with value ZERO. So the
		// attribute is present, decoded, and printed nowhere without -d, and
		// prints ` proto unspec` with it.
		//
		// That is a state a decoder holding a bare uint8 cannot represent:
		// "absent" and "present, zero" render identically on the plain form
		// and differently under -d. xtcpnl.RuleInfo.HasProtocol exists for
		// this, and this row is the live evidence that it has to.
		//
		// The delta is therefore ` proto unspec` on every user-added rule and
		// ` proto kernel` on each default — one token per line, on every line,
		// from an attribute the plain golden proves nothing about.
		Floor: 2, Implemented: true,
	},
	{
		Name: "-s rule show", Slug: "rule_show_stats",
		// Two, and the cheapest no-op in the whole `-s` sweep.
		//
		// `grep -n show_stats ip/iprule.c` returns NOTHING — against 5 hits
		// in iproute.c, 4 in ipneigh.c and 15 in ipaddress.c — so `-s`
		// reaches no print decision for this object. On the request side
		// req.RuleShowDump takes family and seq and no mask, so it cannot
		// reach the wire either.
		//
		// Expected `control nl=0 stdout=0`, exactly as quiet as `rule show`.
		// If it is not, the no-op claim is wrong, and this row is the only
		// thing that would say so: goip accepts `-s` silently, so "-s
		// changed nothing" and "-s was dropped on the floor" look identical
		// from the goip side alone. Comparing against a live `ip` that was
		// also given `-s` is what makes the absence iproute2's rather than an
		// assumption both sides inherited — the same argument the
		// `-d neigh show` row above makes for its own absence.
		//
		// MEASURED: `control: nl=0 stdout=0`, identical to `rule show`,
		// `-4 rule show`, `-6 rule show` and `-d rule show`. Step 5's no-op
		// claim holds live, on the cheapest row in the table.
		Floor: 2, Implemented: true,
	},
})

// withArgs fills every row's Args from its Name.
//
// The table used to spell the argv out a second time, and the invariant that
// the two agreed was a test assertion. Deriving it makes them unable to
// disagree — and it removes the only reason `show`, `addr` and `route` appeared
// as repeated literals, which is a duplication goconst was right to name.
//
// This works because every command in the table is a sequence of
// whitespace-free words, which is true of iproute2's whole read-only argv
// grammar. A command needing an argument with a space in it would have to
// carry its Args explicitly again; TestCommandTable's join assertion stays in
// place to catch a Name that does not round-trip through strings.Fields.
func withArgs(rows []Command) []Command {
	for i := range rows {
		rows[i].Args = strings.Fields(rows[i].Name)
	}
	return rows
}

// Commands returns the table in capture order.
//
// A copy, because the caller is a CLI that has no business mutating it and a
// shared slice header is the cheapest way to let it.
func Commands() []Command {
	out := make([]Command, len(commands))
	copy(out, commands)
	return out
}

// ErrUnknownCommand is returned for a name not in the table.
var ErrUnknownCommand = fmt.Errorf("goipparity: no such command")

// Lookup finds a command by Name. Exact match, not prefix: this is the
// allowlist key, and an allowlist matched by prefix would be an allowlist
// whose entries silently widen.
func Lookup(name string) (Command, error) {
	for _, c := range commands {
		if c.Name == name {
			return c, nil
		}
	}
	return Command{}, fmt.Errorf("%q: %w", name, ErrUnknownCommand)
}

// Argv is the argv for one side, with the device substituted when the command
// needs one. dev may be empty for a command that does not.
func (c Command) Argv(dev string) []string {
	out := make([]string, len(c.Args), len(c.Args)+1)
	copy(out, c.Args)
	if c.NeedsDev {
		out = append(out, dev)
	}
	return out
}

// CaptureName is the pcap basename for one side of this command's triple.
func (c Command) CaptureName(side string) string {
	return c.Slug + "." + side + ".pcap"
}

// StdoutName is the basename of the recorded stdout for one side.
func (c Command) StdoutName(side string) string {
	return c.Slug + "." + side + ".out"
}

// String renders one table row for `goip-parity commands`, tab-separated so a
// shell can read it with `read -r name slug floor impl args`.
//
// Tab-separated rather than JSON because the consumer is an expect script
// driving a serial console, where a parser is a liability and `IFS=$'\t'` is
// not. The name is last precisely because it is the field that contains
// spaces, so a `read` with four leading fields captures the rest correctly.
func (c Command) String() string {
	impl := "no"
	if c.Implemented {
		impl = "yes"
	}
	dev := "no"
	if c.NeedsDev {
		dev = "yes"
	}
	return strings.Join([]string{
		c.Slug,
		fmt.Sprint(c.Floor),
		impl,
		dev,
		strings.Join(c.Args, " "),
		c.Name,
	}, "\t")
}

// slugsSorted is a test helper's worth of logic kept here so both the table
// test and any future consumer agree on it.
func slugsSorted() []string {
	out := make([]string, 0, len(commands))
	for _, c := range commands {
		out = append(out, c.Slug)
	}
	sort.Strings(out)
	return out
}
