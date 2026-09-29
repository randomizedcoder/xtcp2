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
		// with it, and it is the reason this command does not join
		// gated_commands here — it earns that on its own measured runs, once
		// the noise has been observed rather than predicted.
		//
		// The stdout half is not noisy in the same way, because
		// FacetStatsHeaders compares the column HEADINGS and not the
		// counters or their widths. See stdout.go.
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
