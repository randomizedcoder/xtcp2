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
// Four files, three jobs:
//
//   - commands.go is the command table: the single source of truth for what
//     gets captured, what the capture files are called, and the allowlist key.
//     `goip-parity commands` prints it so the guest driver iterates the same
//     list the comparator reads.
//   - stdout.go is the structural stdout comparison, the plan's Risk 1
//     override. Netlink parity alone is reply-independent: a goip that sends
//     byte-identical requests and discards every reply is a perfect green.
//     stdout_json.go is the second half of that one job: it fills the SAME
//     facets and keywords from `-j` output, which the text patterns cannot
//     match at all, and adds no locus of its own.
//   - compare.go walks a capture directory, runs both comparisons per command
//     and renders the sentinels the in-guest runner greps for.
//
// The netlink comparison itself is pkg/nlparity's; nothing here duplicates it.
package goipparity

import (
	"fmt"
	"slices"
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

	// Write marks a command that changes the namespace: `ip link add`, `set`
	// and `del` (link-write-path.md, "Parity: write rows in the matrix").
	//
	// A write row runs each side in a namespace the driver recreates and
	// prepares with Setup, so ip_a's write does not make ip_b's meet
	// -EEXIST, and the devices Setup makes take the same ifindexes on every
	// side. `ip` sends its write; goip writes the same request to the file
	// GOIP_WRITE_OUT names, WriteName, and sends only the name lookups.
	// Floor counts ip's datagrams, the lookups plus the write and its
	// NLMSG_ERROR, so the goip side's floor is Floor-2.
	Write bool

	// Writes is how many write messages a write row sends: 0 means exactly
	// one, as every single-write row does, and N > 0 means the last N, in
	// M <= N datagrams (addr-write-path.md, "Multi-write rows"). A flush
	// row must set it, even to 1, and so must a restore row, whose N writes
	// follow its link dump; TestCommandTable refuses one that does not. It stays out of String: only the host-side comparator reads
	// it, and the guest's row format is fixed at eight fields.
	Writes int

	// Setup is a row's precondition: `ip` argument lists, run in order
	// outside the capture window. Steps are joined with ';' in String, so a
	// step may not contain one.
	//
	// A write row runs Setup once per side, in that side's new namespace. A
	// read row runs it once, in one new namespace all three sides then read:
	// a read changes nothing, so sharing it keeps what Setup made (a random
	// MAC, an ifindex) the same on every side. Without Setup, a read row
	// reads the shared dump topology.
	Setup []string
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
		Name: "-d link show dev bond0", Slug: "link_show_details_bond",
		Setup: bondReadSetup,
		// The bond's IFLA_INFO_DATA rendered by -d: mode, miimon, ad_select,
		// lacp_rate, ad_actor_system and the IFLA_BOND_AD_INFO block, the
		// decode link-write-path.md phase 2 added. Four datagrams, as `link
		// show dev`: the name resolution, then the re-fetch.
		//
		// # A read row with a Setup
		//
		// The dump topology has no bond, so this row brings its own, and the
		// driver builds it ONCE for all three sides: a read changes nothing,
		// and one namespace gives every side the same bond MAC (taken from a
		// veth with a random one) and the same ifindexes. See bondReadSetup
		// for why the ports have no carrier.
		Floor: 4, Implemented: true,
	},
	{
		Name: "-d link show dev la1", Slug: "link_show_details_bond_slave",
		Setup: bondReadSetup,
		// The port's IFLA_INFO_SLAVE_DATA: state, mii_status, the link
		// failure count, the aggregator id and the actor and partner port
		// states, the field the iproute2 width fix is about.
		//
		// Eight datagrams, four transactions, measured on the first live run
		// for ip and goip alike: `link show dev`'s two, then one get each to
		// name the veth peer (`la1@lb1`, from IFLA_LINK) and the master
		// (`master bond0`, from IFLA_MASTER). `dev bond0` above prints
		// neither, so it stays at four.
		Floor: 8, Implemented: true,
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
		Name: "-0 addr show", Slug: "addr_show_packet",
		// TWO, not four, and the halved count is the assertion.
		//
		// # The only row in the table that sends ONE dump for `addr show`
		//
		// Every other addr row sends two transactions: the link dump, then the
		// address dump. This one sends the first and not the second, because
		// ipaddr_list_flush_or_save guards the whole address half on
		// `filter.family != AF_PACKET` (ip/ipaddress.c:2310) and `-0` sets
		// preferred_family to AF_PACKET. The same condition appears a second
		// time, at :2331, where it also suppresses print_selected_addrinfo —
		// so one comparison removes a transaction AND every address line, and
		// the output is link stanzas alone.
		//
		// goip splits the same condition the same way, which is why this row
		// is worth a triple rather than a stdout check. service.go:199 is the
		// wire half: AddressSnapshot never calls s.Addresses, so the second
		// dump is neither built nor issued. renderAddrGroups is the print
		// half: obj_addr.go:237-239 skips filterLinksWithAddrs exactly where
		// `ip` skips ipaddr_filter. The `dev` path guards its own request
		// separately (obj_addr.go:213), and AddrShowDump carries no guard at
		// all — req.go:200-203 is the doc note recording that the decision
		// belongs to its caller.
		//
		// A goip that skipped the render but still sent the dump would print
		// identical text, so the datagram count and the absent request are
		// the only things that would catch it.
		//
		// # The only CLI input in the project that reaches that branch
		//
		// The AF_PACKET arm existed in obj_addr.go and in
		// Service.AddressSnapshot before any option could produce it; goip.go's
		// `-0` case says so in its own comment. So this row is not an extra
		// sample of a compared path, it is the first comparison of the path at
		// all.
		//
		// `-0 link show` is deliberately NOT a row: ipaddr_list_link
		// overwrites preferred_family with AF_PACKET before parsing anything
		// (ip/ipaddress.c:2416), so it would be a third byte-identical copy of
		// `link show` after `-4` and `-6`. See the `-4 link show` row.
		Floor: 2, Implemented: true,
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
		//
		// Gated on G0-T03: noisy with variance clears `-s link show`'s bar,
		// and every run since the sweep passed it with zero findings and
		// txns 2/2.
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
		//
		// GATED as of runs 4 and 5, so the hold-out two paragraphs up is
		// history and not current state. The WARN it produced while ungated
		// is why the hold-out was worth having; it is not an argument
		// against gating now that an entry carries that divergence. Quiet
		// on all five runs, and structurally rather than luckily so: this
		// row cannot turn netlink-noisy unless the kernel starts honoring a
		// mask `-s` never puts on the wire.
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
	// A3-T03: the output modes (addr-write-path.md, "Selector and output-mode
	// read rows"). Each sends the same two dumps as `addr show`, byte for
	// byte, so the floor is four; the difference is all in the render. Under
	// -o, ipaddr_list sets no_link, so there is no link line and no side-get;
	// under -br, only IFLA_LINK is resolved. The first three read mixed_links,
	// the state their sidecars were captured on. `-o -6` reads v6_peer, a
	// nodad address, because mixed_links has no v6 address but lo's and an
	// address that runs DAD could be tentative on one side and not another.
	{
		Name: "-br addr show", Slug: "addr_show_brief",
		Setup: mixedLinksSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "-o addr show", Slug: "addr_show_oneline",
		Setup: mixedLinksSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "-br -j addr show", Slug: "addr_show_brief_json",
		Setup: mixedLinksSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "-o -6 addr show", Slug: "addr_show_oneline_v6",
		Setup: v6PeerSetup, Floor: 4, Implemented: true,
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
		Name: "addr show dev metric0", Slug: "addr_show_dev_metric",
		Setup: addrStateSetup("metric0", "addr add 192.0.2.1/24 metric 50 dev metric0"),
		// A1-T02: `metric 50`, inside the local block. Each of these four
		// rows is an addr-show state (v4_metric, v4_peer, v6_peer,
		// v4_autojoin) on a device named for it, because a row's Name is its
		// argv and must be unique. Six, as `addr show dev`.
		Floor: 6, Implemented: true,
	},
	{
		Name: "addr show dev peer4", Slug: "addr_show_dev_peer4",
		Setup: addrStateSetup("peer4", "addr add 10.0.0.1 peer 10.0.0.2/32 dev peer4"),
		Floor: 6, Implemented: true,
	},
	{
		Name: "addr show dev peer6", Slug: "addr_show_dev_peer6",
		Setup: addrStateSetup("peer6", "-6 addr add 2001:db8::1 peer 2001:db8::2/128 nodad dev peer6"),
		Floor: 6, Implemented: true,
	},
	{
		Name: "addr show dev autojoin0", Slug: "addr_show_dev_autojoin",
		Setup: addrStateSetup("autojoin0", "addr add 224.0.0.251/32 autojoin dev autojoin0"),
		Floor: 6, Implemented: true,
	},
	// A2-T03: one row per `addr show` selector (addr-write-path.md, "Selector
	// and output-mode read rows"), each on the addr-show state whose fixture
	// its unit golden replays. Setup is that state's topology file, step for
	// step, which TestAddrShowSelectorRowsGated holds it to. Four is the two
	// dumps; a row that sends more says why.
	{
		Name: "addr show to 192.0.2.0/24", Slug: "addr_show_sel_to",
		// `to` sets only filter.family, so the link dump stays AF_UNSPEC with
		// the ext mask while the address dump is AF_INET
		// (ip/ipaddress.c:2030), and no link/ether line is printed.
		Setup: v4SecondarySetup, Floor: 4, Implemented: true,
	},
	{
		Name: "addr show scope link", Slug: "addr_show_sel_scope_link",
		Setup: v6StablePrivacySetup, Floor: 4, Implemented: true,
	},
	{
		Name: "addr show scope all", Slug: "addr_show_sel_scope_all",
		Setup: v4HostScopeSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "addr show up", Slug: "addr_show_sel_up",
		Setup: mixedLinksSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "addr show down", Slug: "addr_show_sel_down",
		Setup: mixedLinksSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "addr show permanent", Slug: "addr_show_sel_permanent",
		// Reads v4_lifetimes but prints only lo's forever addresses: d0's one
		// address is finite, so ipaddr_filter drops d0. Gateable, unlike the
		// next row.
		Setup: v4LifetimesSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "addr show dynamic", Slug: "addr_show_sel_dynamic",
		// Prints d0's valid_lft, which counts down between ip_a and goip, so
		// this row stays out of gated_commands (addr-write-path.md, "Gating").
		Setup: v4LifetimesSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "addr show -tentative", Slug: "addr_show_sel_not_tentative",
		Setup: v6TentativeSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "addr show tentative", Slug: "addr_show_sel_tentative",
		// v0 is never up, so its address stays tentative with no DAD timer.
		Setup: v6TentativeSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "addr show secondary", Slug: "addr_show_sel_secondary",
		Setup: v4SecondarySetup, Floor: 4, Implemented: true,
	},
	{
		Name: "addr show primary", Slug: "addr_show_sel_primary",
		Setup: v4SecondarySetup, Floor: 4, Implemented: true,
	},
	{
		Name: `addr show label "d0:*"`, Slug: "addr_show_sel_label",
		// The driver runs the argv with `sh -c` from the console's directory,
		// so an unquoted d0:* would be a glob there. The shell removes the
		// quotes; Args keeps them, and nothing else reads Args. Headerless:
		// print_linkinfo returns before the header when label is set
		// (ip/ipaddress.c:975).
		Setup: v4LabelSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "addr show group default", Slug: "addr_show_sel_group",
		Setup: mixedLinksSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "addr show master bond0", Slug: "addr_show_sel_master",
		// Six: the get by name that resolves bond0, then the dumps, the link
		// one carrying IFLA_MASTER.
		Setup: mixedLinksSetup, Floor: 6, Implemented: true,
	},
	{
		Name: "-4 addr show master bond0", Slug: "addr_show_sel_v4_master",
		// The same get, but an AF_INET link dump carries no attributes, so
		// master is applied client-side only.
		Setup: mixedLinksSetup, Floor: 6, Implemented: true,
	},
	{
		Name: "addr show nomaster", Slug: "addr_show_sel_nomaster",
		Setup: mixedLinksSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "addr show vrf vrf0", Slug: "addr_show_sel_vrf",
		// Eight: the get by name, name_is_vrf's get, then the dumps.
		Setup: v4VrfSetup, Floor: 8, Implemented: true,
	},
	{
		Name: "addr show vrf d1", Slug: "addr_show_sel_vrf_d1",
		// The same two gets and no dump: d1 is not a vrf, and `ip` exits 255.
		// A read row compares netlink and stdout, both empty here, not the
		// exit status.
		Setup: v4VrfSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "addr show type dummy", Slug: "addr_show_sel_type_dummy",
		Setup: mixedLinksSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "addr show type bond_slave", Slug: "addr_show_sel_type_bond_slave",
		Setup: mixedLinksSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "addr show proto 99", Slug: "addr_show_sel_proto",
		Setup: v4ProtoSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "addr show novf", Slug: "addr_show_sel_novf",
		Setup: mixedLinksSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "addr show d0", Slug: "addr_show_sel_d0",
		// Six, as `addr show dev`: a bare device takes the same path.
		Setup: v4SecondarySetup, Floor: 6, Implemented: true,
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
		Name: "route show table main", Slug: "route_show_table_main",
		// Four, and the request is BYTE-IDENTICAL to `route show`'s — which is
		// the one thing about this command a reader would not guess, because
		// the sibling row above it is not.
		//
		// iproute_list_flush_or_save assigns `filter.tb = RT_TABLE_MAIN`
		// before it parses an argument (ip/iproute.c:1836), and
		// iproute_dump_filter writes RTA_TABLE whenever filter.tb is nonzero
		// (:1726-1730). So a bare `ip route show` ALREADY carries
		// RTA_TABLE = 254 on the wire, and `table main` re-assigns the value
		// it already had. The family promotion is shared for the same reason:
		// `dump_family == AF_UNSPEC && filter.tb` sends both forms as AF_INET
		// (:1998-1999).
		//
		// That is what separates this row from `route show table all`, where
		// `all` means RT_TABLE_UNSPEC: zero, so the attribute is absent
		// ENTIRELY and the promotion does not fire. Three spellings of the
		// table selector, three different requests — 254, nothing, and 255 —
		// and only the middle one was compared before this sweep.
		//
		// The assertion is therefore an identity, and it is live where the
		// offline tests cannot be: goip reaches the same bytes through
		// routeTableID("main") -> unix.RT_TABLE_MAIN and a selector struct,
		// not through a pre-set default, so the two implementations agree by
		// construction on neither side. A goip that treated an explicit
		// `table main` as a client-side filter, or that dropped RTA_TABLE when
		// the value equalled the default, would print the same routes and
		// differ here.
		Floor: 4, Implemented: true,
	},
	{
		Name: "route show table local", Slug: "route_show_table_local",
		// Four, and the only route row whose request differs from `route
		// show`'s in a VALUE rather than in a structure: one byte of one
		// attribute, RTA_TABLE = 255 where every other form sends 254 or
		// omits it.
		//
		// # The row that renders route types past `unicast`
		//
		// Table 255 is where the kernel keeps the routes it creates for every
		// configured address, and they are RTN_LOCAL and RTN_BROADCAST rather
		// than RTN_UNICAST. print_route prints the type token when it is not
		// the default (ip/iproute.c:828), so this listing opens every line
		// with `local` or `broadcast` — and render.routeTypeNames
		// (internal/goip/render/route.go:133-146) has twelve entries of which
		// the compared corpus exercises exactly one.
		//
		// That makes it the only row in the table that reaches those arms
		// without `-d`. The `-d route show` row reaches the SUPPRESSION logic
		// — it unhides `unicast` — which is a different claim: unhiding the
		// default proves the guard works, printing a non-default proves the
		// NAME TABLE is right. A renderer with `local` and `broadcast`
		// transposed passes every existing golden and fails this row on every
		// line.
		//
		// # Why `table default` is not the third row here
		//
		// RT_TABLE_DEFAULT = 253 is an equally real request change, but the
		// parity topology puts no route in table 253, so the stdout half would
		// be empty on both sides — a facet set that matches because neither
		// tool printed anything, which is the vacuity this harness exists to
		// refuse. It is worth a row the moment nltopo::build_clean adds a
		// `table 253` route, and not before.
		Floor: 4, Implemented: true,
	},
	{
		Name: "-4 route show", Slug: "route_show_v4",
		// Four, and byte-identical to `route show` in both halves — the
		// `-4 link show` precedent, reached by a completely different
		// mechanism and therefore worth its own row.
		//
		// There the identity comes from an OVERWRITE: ipaddr_list_link stamps
		// preferred_family with AF_PACKET and discards whatever `-4` set
		// (ip/ipaddress.c:2416). Here nothing is overwritten and `-4` is
		// honored exactly — it is simply honored to the same value the bare
		// form already computes. `dump_family` starts as preferred_family
		// (ip/iproute.c:1821) and is promoted from AF_UNSPEC to AF_INET
		// because filter.tb is nonzero (:1998-1999), so `route show` asks for
		// IPv4 and `-4 route show` asks for IPv4.
		//
		// One identity from a dropped option and one from a redundant option
		// are different failure modes, and a single row cannot hold both.
		//
		// What it can find: `route show table all` is the form where the
		// promotion does NOT fire, so a goip that hardcoded AF_INET for every
		// route dump would pass this row and fail that one, and a goip that
		// hardcoded AF_UNSPEC would do the reverse. The pair is the assertion;
		// neither row is one on its own.
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
	// G1-T01: `route get` (iproute_get), the C1-T02 rows of
	// rtnl-write-corpus.exp with their preconditions. A read: the single get
	// changes nothing, so the three sides share one namespace. Each is the
	// get and its reply, then the GETLINK by index that names the reply's
	// device (4); from/iif resolves d1 by name first (6); the unreachable
	// get is the request and its NLMSG_ERROR, exit 2 and no stdout (2).
	{
		Name: "route get 192.0.2.5", Slug: "route_get",
		// A cloned reply: `uid 0` and the bare `cache ` line.
		Setup: routeGetSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "route get fibmatch 192.0.2.5", Slug: "route_get_fibmatch",
		// RTM_F_FIB_MATCH: the matched FIB entry, 192.0.2.0/24, not a clone.
		Setup: routeGetSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "route get 192.0.2.5 notify", Slug: "route_get_notify",
		// RTM_F_NOTIFY echoed as `notify`, RTCF_NOTIFY as `cache <notify> `.
		Setup: routeGetSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "-6 route get 2001:db8::5", Slug: "route_get_v6",
		// No RTM_F_LOOKUP_TABLE on IPv6, and no cache line.
		Setup: routeGetSetupV6, Floor: 4, Implemented: true,
	},
	{
		Name: "route get 192.0.2.5 from 198.51.100.2 iif d1", Slug: "route_get_from_iif",
		// An input lookup, which needs forwarding: d1 by name, the get with
		// RTA_SRC and RTA_IIF, then d0 by index. No uid on an input lookup.
		Setup: routeGetSetupIif, Floor: 6, Implemented: true,
	},
	{
		Name: "route get 203.0.113.1", Slug: "route_get_unreachable",
		// No route: ENETUNREACH, exit 2.
		Setup: routeGetSetup, Floor: 2, Implemented: true,
	},
	// G1-T02: `neigh get` (ipneigh_get), the C1-T02 rows. Each is the get by
	// name for d0 and the RTM_GETNEIGH get, a request and a reply each (4);
	// the lookup caches d0, so print_neigh sends nothing more.
	{
		Name: "neigh get 192.0.2.2 dev d0", Slug: "neigh_get",
		// A permanent entry on a veth, which does neighbor discovery.
		Setup: neighGetSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "neigh get 192.0.2.9 dev d0", Slug: "neigh_get_missing",
		// No entry: ENOENT, exit 2.
		Setup: neighGetSetupVeth, Floor: 4, Implemented: true,
	},
	// G3-T01: `neigh add`, `change`, `replace` and `del` (ipneigh_modify)
	// and `neigh flush` (do_show_or_flush), the C1-T02 neigh write rows.
	// Each write is ll_init_map's link dump, then the request and its ack;
	// goip sends the dump and writes the request. Floor is 4, as for route
	// restore, so goip's side, at Floor-2, is the dump. Their namespaces pin
	// the veths' MACs and set addrgenmode none, so no link-local, and no
	// multicast entry its MLD would add, varies between the sides. The two
	// adds are one command; the second Name abbreviates `add`, which the
	// bytes do not show, since each Name is one row.
	{
		Name: "neigh add 192.0.2.2 lladdr 02:00:00:00:00:02 dev d0 nud permanent", Slug: "neigh_add",
		Floor: 4, Implemented: true, Write: true, Setup: neighWriteSetupVeth,
	},
	{
		Name: "neigh a 192.0.2.2 lladdr 02:00:00:00:00:02 dev d0 nud permanent", Slug: "neigh_add_existing",
		// EEXIST.
		Floor: 4, Implemented: true, Write: true, Setup: neighWriteSetup,
	},
	{
		Name: "neigh change 192.0.2.2 lladdr 02:00:00:00:00:03 dev d0 nud reachable", Slug: "neigh_change",
		Floor: 4, Implemented: true, Write: true, Setup: neighWriteSetup,
	},
	{
		Name: "neigh change 192.0.2.9 lladdr 02:00:00:00:00:09 dev d0", Slug: "neigh_change_missing",
		// REPLACE without CREATE: ENOENT.
		Floor: 4, Implemented: true, Write: true, Setup: neighWriteSetupVeth,
	},
	{
		Name: "neigh replace 192.0.2.3 lladdr 02:00:00:00:00:03 dev d0 nud stale", Slug: "neigh_replace",
		Floor: 4, Implemented: true, Write: true, Setup: neighWriteSetupVeth,
	},
	{
		Name: "neigh del 192.0.2.2 dev d0", Slug: "neigh_del",
		Floor: 4, Implemented: true, Write: true, Setup: neighWriteSetup,
	},
	// The flush: the link dump, the neighbor dump, the two RTM_DELNEIGH
	// copies of the stale entries in one datagram, unacked, and the dump
	// again; goip writes the copies, its second dump skipping what it
	// copied. ip sends at least eight datagrams and goip at least seven;
	// Floor 6 keeps goip's side at 4. The empty flush deletes nothing, so ip
	// sends the two dumps alone and goip needs no GOIP_WRITE_OUT: a read
	// row, its Name abbreviating `flush`.
	{
		Name: "neigh flush dev d0", Slug: "neigh_flush",
		Floor: 6, Implemented: true, Write: true, Writes: 2, Setup: neighFlushSetup,
	},
	{
		Name: "-s neigh flush dev d0", Slug: "neigh_flush_stats",
		// The round text and the closing line, compared as stdout.
		Floor: 6, Implemented: true, Write: true, Writes: 2, Setup: neighFlushSetup,
	},
	{
		Name: "neigh f dev d0", Slug: "neigh_flush_empty",
		Floor: 4, Implemented: true, Setup: neighWriteSetupVeth,
	},
	{
		Name: "neigh show", Slug: "neigh_show",
		Floor: 4, Implemented: true,
	},
	{
		Name: "-4 neigh show", Slug: "neigh_show_v4",
		// Four, and the first family row this object has ever had — every
		// other object in the table carries at least two.
		//
		// # One byte changes, and it is in the SECOND request, not the first
		//
		// `neigh show` sends two transactions: ll_init_map's link dump, then
		// the neighbor dump. The family reaches only the second of them.
		// ll_init_map's request is hardcoded AF_UNSPEC with its own mask
		// (lib/ll_map.c:395) and takes no family argument at all, which goip
		// mirrors by having req.NeighShowLinkDump accept only a sequence
		// number; the neighbor dump takes filter.family, which
		// do_show_or_flush assigns from preferred_family at ip/ipneigh.c:514
		// and rtnl_neighdump_req puts in ndm_family at :648.
		//
		// The guard there reads `if (!filter.family)`, and it always fires for
		// a show: ipneigh_reset_filter memsets the struct (:480) immediately
		// before (:511). So ndm_family is 0 for the bare form and 2 here, one
		// byte, in the second of two requests — which is a state a comparator
		// that checked only the first request, or only the datagram count,
		// could not see.
		//
		// This is the opposite of the link object, where `-4` is discarded,
		// and of the route object, where `-4` is honored to the value the bare
		// form already had. Here it is honored to a DIFFERENT value, so the
		// two requests must differ and the comparison asserts inequality
		// rather than identity. All three shapes now have a row.
		Floor: 4, Implemented: true,
	},
	{
		Name: "-6 neigh show", Slug: "neigh_show_v6",
		// Four, the other arm, and not a duplicate: `-4` and `-6` are separate
		// assignments in goip's option loop, so a goip that forwarded one and
		// not the other is a state this table can reach and a single row
		// cannot. The same argument the `-6 link show` row makes.
		//
		// It also reaches a different kernel cache: ndm_family AF_INET6 is
		// answered from the IPv6 neighbor table rather than from the ARP
		// cache, so what this row lists is whatever the namespace's
		// link-local addresses have resolved.
		//
		// The three rows PARTITION the bare form's output, which was measured
		// rather than reasoned: `neigh show` printed 575 bytes, `-4 neigh
		// show` 437 and `-6 neigh show` 138, and 437 + 138 = 575. Each family
		// row is therefore a subset of `neigh show`, not a different listing,
		// and what the pair buys is that the split falls in the same place on
		// both sides. A goip that dropped the family would print all 575
		// bytes here and fail two rows at once.
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
		//
		// Gated on G0-T03, on G0-T01's bar: every run since the sweep passed
		// it with zero findings and txns 2/2, and `-4 neigh show`, the same
		// two dumps, read nl=2 then nl=6 on G0-T01's runs and passed both,
		// which is the moved count this row was waiting for.
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
	// G3-T02: `rule add` and `rule del` (iprule_modify), `rule flush`, `rule
	// save` and `rule restore` (iprule_list_flush_or_save, iprule_restore),
	// the C1 rule rows. A rule write resolves nothing first: a device goes
	// by name, so ip sends the write and its ack (2) and goip sends nothing,
	// writing the request. The fourteen selector adds were one capture,
	// rule_add_selectors; each is a row of its own here, in a namespace of
	// its own, so `goto 112` names a rule that is not there, which the kernel
	// accepts.
	{
		Name: "rule add pref 101 from 192.0.2.0/24 table 100", Slug: "rule_add_from",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "rule add pref 102 to 198.51.100.0/24 table 100", Slug: "rule_add_to",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "rule add pref 103 tos 0x10 table 100", Slug: "rule_add_tos",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "rule add pref 104 dscp 10 table 100", Slug: "rule_add_dscp",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "rule add pref 105 fwmark 0x7/0xff table 100", Slug: "rule_add_fwmark",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "rule add pref 106 iif d9 table 100", Slug: "rule_add_iif",
		// d9 does not exist, and need not: the name is what is sent.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "rule add pref 107 oif d9 table 100", Slug: "rule_add_oif",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "rule add pref 108 uidrange 1000-2000 table 100", Slug: "rule_add_uidrange",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "rule add pref 109 ipproto tcp sport 1000-2000 dport 80 table 100", Slug: "rule_add_ports",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "rule add pref 110 not from 203.0.113.0/24 goto 112", Slug: "rule_add_goto",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "rule add pref 111 table main suppress_prefixlength 0", Slug: "rule_add_suppress",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "rule add pref 112 protocol static table 100", Slug: "rule_add_protocol",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "rule add pref 113 l3mdev", Slug: "rule_add_l3mdev",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "-6 rule add pref 114 flowlabel 0x12345 table 100", Slug: "rule_add_flowlabel",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "rule add pref 100 from 192.0.2.0/24 table 100", Slug: "rule_add_existing",
		// EEXIST, exit 2.
		Floor: 2, Implemented: true, Write: true, Setup: ruleExistingSetup,
	},
	{
		Name: "rule del pref 100", Slug: "rule_del",
		Floor: 2, Implemented: true, Write: true, Setup: ruleExistingSetup,
	},
	{
		Name: "rule del pref 4242", Slug: "rule_del_missing",
		// ENOENT, exit 2.
		Floor: 2, Implemented: true, Write: true,
	},
	// The flush: the rule dump, its five replies and DONE in one datagram,
	// then an RTM_DELRULE copy of each reply carrying FRA_PRIORITY, each
	// sent alone and acked, which is every rule but the local one: ip sends
	// ten datagrams, goip the dump's two. save is the dump with the replies
	// written whole after the magic, compared record by record
	// (stdout_dump.go). restore reads rule_save's committed ip.save, which
	// the runner carries in: ll_init_map's link dump, then the five rules,
	// each acked; goip sends the dump and writes the five.
	{
		Name: "rule flush", Slug: "rule_flush",
		Floor: 4, Implemented: true, Write: true, Writes: 4, Setup: ruleFlushSetup,
	},
	{
		Name: "rule save", Slug: "rule_save",
		Floor: 2, Implemented: true, Setup: ruleFlushSetup,
	},
	{
		Name: "rule restore < rule_save.save", Slug: "rule_restore",
		Floor: 4, Implemented: true, Write: true, Writes: 5,
	},

	{
		Name: "nexthop show", Slug: "nexthop_show",
		// Four: the nexthop dump plus the ll_init_map link dump print_rta_ifidx
		// triggers to turn nh_oif into `dev goip0` — the floor the capture
		// driver already uses for this command
		// (nix/microvms/scripts/capture-netlink-dumps.exp).
		//
		// nexthop's first row in this matrix, and its first live grounding.
		// internal/goip's TestNexthopShowMatchesCapturedSidecars diffs goip
		// byte-for-byte against the committed ip_nexthop sidecar offline; runs 10
		// and 11 added the live `ip` a sidecar cannot give, catching a pin that
		// moved a token. Gated there: GOIP_PARITY_PASS, stdout=0, control nl=0,
		// txns 3/3 — `ip` opens three sockets and goip reuses one, but the
		// transaction count matches, which is what the gate reads.
		Floor: 4, Implemented: true,
	},
	// G1-T03: `nexthop get` (ipnh_get_id) and `nexthop bucket` (ipnh_bucket_list,
	// ipnh_bucket_get_id), the C1-T02 rows.
	{
		Name: "nexthop get id 1", Slug: "nexthop_get",
		// The RTM_GETNEXTHOP get, then d0 by index for its `dev` token (4).
		Setup: nexthopGetSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "nexthop get id 9", Slug: "nexthop_get_missing",
		// No such id: ENOENT, exit 2 (2).
		Setup: nexthopGetSetup, Floor: 2, Implemented: true,
	},
	{
		Name: "nexthop bucket show id 20", Slug: "nexthop_bucket_show",
		// The 1024 buckets of a resilient group in one dump; ip's capture held
		// five datagrams, and the floor leaves the kernel's packing room.
		// Each bucket's NHA_RES_BUCKET_IDLE_TIME counts up between the three
		// sides, a reply value the control cancels.
		Setup: nexthopBucketSetup, Floor: 3, Implemented: true,
	},
	{
		Name: "nexthop bucket get id 20 index 0", Slug: "nexthop_bucket_get",
		// One bucket's single get and its reply (2).
		Setup: nexthopBucketSetup, Floor: 2, Implemented: true,
	},
	// G3-T03: `nexthop add`, `replace` and `del` (ipnh_modify) and `nexthop
	// flush` (ipnh_flush), the C1 nexthop rows. `dev` is ll_name_to_index,
	// whose by-name RTM_GETLINK goes ahead of the write: ip sends the get,
	// its reply, the write and its ack (4), goip the get and its reply. A row
	// with no device sends the write alone (2).
	{
		Name: "nexthop add id 1 via 192.0.2.2 dev d0", Slug: "nexthop_add_single",
		Floor: 4, Implemented: true, Write: true, Setup: nexthopD0Setup,
	},
	{
		Name: "nexthop add id 10 group 1,3/2", Slug: "nexthop_add_group",
		Floor: 2, Implemented: true, Write: true, Setup: nexthopGetSetup,
	},
	{
		Name: "nexthop add id 20 group 1/2 type resilient buckets 1024", Slug: "nexthop_add_resilient",
		Floor: 2, Implemented: true, Write: true, Setup: nexthopGetSetup,
	},
	{
		Name: "nexthop add id 1 blackhole", Slug: "nexthop_add_existing",
		// EEXIST, exit 2.
		Floor: 2, Implemented: true, Write: true, Setup: nexthopBlackholeSetup,
	},
	{
		Name: "nexthop replace id 1 via 192.0.2.4 dev d0", Slug: "nexthop_replace",
		Floor: 4, Implemented: true, Write: true, Setup: nexthopGetSetup,
	},
	{
		Name: "nexthop del id 1", Slug: "nexthop_del",
		Floor: 2, Implemented: true, Write: true, Setup: nexthopBlackholeSetup,
	},
	// The flush: the groups dump, then the full dump, each reply's id deleted
	// from a second socket and acked. nexthop_flush's capture held resilient
	// group 20 as well, deleted between the two dumps, but a write row's
	// writes must be one run (nlparity.SplitWrites), so this row flushes the
	// two single nexthops alone: two empty-handed groups replies, then two
	// deletes after the full dump. goip sends the two dumps (4) and writes
	// the deletes. TestNexthopFlushMatchesIp holds goip to the group capture.
	// `nexthop f`, in a namespace with no nexthop, deletes nothing: a read
	// row of the two dumps, `Nothing to flush`.
	{
		Name: "nexthop flush", Slug: "nexthop_flush",
		Floor: 6, Implemented: true, Write: true, Writes: 2, Setup: nexthopGetSetup,
	},
	{
		Name: "nexthop f", Slug: "nexthop_flush_empty",
		Floor: 4, Implemented: true, Setup: []string{"link set lo up"},
	},

	{
		Name: "addrlabel show", Slug: "addrlabel_show",
		// Two, the rule shape rather than the nexthop one: ipaddrlabel_list calls
		// no ll_init_map (ip/ipaddrlabel.c:99-125) because every default entry has
		// ifal_index 0, so print_addrlabel resolves no name and there is no link
		// dump — one request and the multipart reply carrying NLMSG_DONE, the floor
		// the capture driver uses for this command.
		//
		// addrlabel's first row in this matrix, and its first live grounding,
		// exactly as the nexthop rows above: internal/goip's
		// TestAddrLabelShowMatchesCapturedSidecars diffs goip byte-for-byte against
		// the committed ip_addrlabel sidecar offline, and runs 10 and 11 added the
		// live `ip` that notices a pin moving a token. Gated there:
		// GOIP_PARITY_PASS, stdout=0, control nl=0, txns 1/1 — the pristine kind,
		// the vrf bar.
		Floor: 2, Implemented: true,
	},
	{
		Name: "-6 addrlabel show", Slug: "addrlabel_show_v6",
		// Two, and byte-identical to the row above in BOTH halves — the rule/`-4`
		// relationship, mirrored onto the other family.
		//
		// ipaddrlabel_list substitutes AF_INET6 for AF_UNSPEC before it builds
		// anything (ip/ipaddrlabel.c:101-104), the addrlabel table being IPv6-only,
		// so a bare `ip addrlabel show` already asks for AF_INET6 and `-6` changes
		// nothing — not one request byte, not one output character. Hence no `-6`
		// pcap in the corpus: this row asserts the identity instead. It is load
		// bearing the same way `-4 rule show` is — a goip that skipped the
		// substitution would send AF_UNSPEC, be answered, and the request halves
		// would no longer match. Gated with `addrlabel show` on runs 10 and 11,
		// txns 1/1 on both.
		Floor: 2, Implemented: true,
	},
	// G3-T04's addrlabel writes. ipaddrlabel_modify names no device here, so
	// each is the write and its ack alone.
	{
		Name: "addrlabel add prefix 2001:db8::/32 label 99", Slug: "addrlabel_add",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "addrlabel del prefix 2001:db8::/32 label 99", Slug: "addrlabel_del",
		Floor: 2, Implemented: true, Write: true, Setup: addrLabelSetup,
	},
	// The flush: the listing's dump, then an acked RTM_DELADDRLABEL copy of
	// each reply from a socket of its own, the kernel's ten default labels
	// among them, eleven in all. goip sends the dump and writes the eleven.
	{
		Name: "addrlabel flush", Slug: "addrlabel_flush",
		Floor: 4, Implemented: true, Write: true, Writes: 11, Setup: addrLabelSetup,
	},

	{
		Name: "ntable show", Slug: "ntable_show",
		// Four, the neigh shape rather than the addrlabel one: do_ipntable calls
		// ll_init_map (ip/ipntable.c:685) before the RTM_GETNEIGHTBL dump, so the
		// command is a link dump then the table dump — two requests and two
		// NLMSG_DONEs, the floor the capture driver uses for this command and the
		// reason a device-specific parameter set can resolve its NDTPA_IFINDEX.
		//
		// ntable's first row in this matrix, and REPLAY-GROUNDED, not yet
		// live-grounded, as the addrlabel and nexthop rows above: internal/goip's
		// TestNeighTblShowMatchesCapturedSidecars diffs goip byte-for-byte against
		// the committed ip_ntable sidecar across three topologies. Gated on runs 8
		// and 9, the first live grounding of this object: GOIP_PARITY_PASS with
		// stdout=0 and nothing unsuppressed on both, txns 2/2.
		Floor: 4, Implemented: true,
	},
	{
		Name: "-s ntable show", Slug: "ntable_show_stats",
		// Four, and byte-identical to the row above in the REQUEST half: `-s`
		// reaches no part of rtnl_neightbldump_req, it only ungates print_ndtconfig
		// and print_ndtstats (ip/ipntable.c:624,630). So this row's request parity
		// is the same assertion as `ntable show`; the stdout adds the config and
		// stats blocks.
		//
		// One caveat the live comparison must expect: print_ndtconfig renders
		// ndtc_last_flush/ndtc_last_rand as absolute dates from gettimeofday
		// (ntable_strtime_delta, :310-336), so those two tokens are wall-clock, not
		// a function of the bytes, and a live `ip` run minutes after the capture
		// prints different dates. goip reproduces the captured dates offline via an
		// injected clock; a live gate must normalize or ignore the two tokens.
		Floor: 4, Implemented: true,
	},
	// G3-T05's ntable writes. ipntable_modify calls ll_init_map
	// (ip/ipntable.c) whether or not a dev is named, so each is the link dump,
	// then the RTM_SETNEIGHTBL and its ack.
	{
		Name: "ntable change name arp_cache dev d0 queue 64", Slug: "ntable_change",
		Floor: 4, Implemented: true, Write: true, Setup: d0UpSetup,
	},
	{
		Name: "ntable change name nosuch_cache queue 64", Slug: "ntable_change_missing",
		// No such table: ENOENT, exit 2.
		Floor: 4, Implemented: true, Write: true,
	},
	{
		Name: "ntable change name arp_cache thresh1 64", Slug: "ntable_change_thresh_netns",
		// The thresholds are init_net's alone, so outside it neightbl_set
		// (net/core/neighbour.c) refuses them: ENOENT, exit 2.
		Floor: 4, Implemented: true, Write: true,
	},

	{
		Name: "netconf show", Slug: "netconf_show",
		// Four, the ntable shape: do_ipnetconf calls ll_init_map
		// (ip/ipnetconf.c:186) before the netconf dump, so the command is a link
		// dump then the RTM_GETNETCONF dump — two requests and two NLMSG_DONEs. A
		// modern kernel answers the AF_UNSPEC dump with every family in one pass;
		// the old-kernel two-pass fallback is unreachable here (contract-tested in
		// the service layer, not part of this row).
		//
		// REPLAY-GROUNDED, not yet live-grounded, as the ntable rows above:
		// internal/goip's TestNetconfShowMatchesCapturedSidecars diffs goip
		// byte-for-byte against the committed ip_netconf sidecar across three
		// topologies. Gated on runs 8 and 9: GOIP_PARITY_PASS, stdout=0, txns 2/2
		// on both. The `dev` forms below gated later, on G0-T03 — see their rows.
		Floor: 4, Implemented: true,
	},
	{
		Name: "netconf show dev", Slug: "netconf_show_dev",
		NeedsDev: true,
		// Four: ll_name_to_index's RTM_GETLINK by name on the still-empty cache,
		// then the ll_init_map link dump, then the AF_UNSPEC netconf dump, which
		// print_netconf filters to the named ifindex client-side
		// (ip/ipnetconf.c:78-79). Byte-grounded offline against the
		// ip_netconf_dev sidecar, and request for request by internal/goip's
		// TestNetconfDevRequestsMatchIpBytes. Runs 8 and 9 read ip=3 goip=2,
		// when goip resolved the name from its own dump, and the row sat in the
		// permanent set; G0-T03 sent the get by name, its two runs read 3/3,
		// and it gated.
		Floor: 4, Implemented: true,
	},
	{
		Name: "-4 netconf show dev", Slug: "netconf_show_dev4",
		NeedsDev: true,
		// The one genuinely new wire shape in this matrix: with an explicit family
		// AND an ifindex, do_show takes the point get (ip/ipnetconf.c:188-198) — a
		// non-dump RTM_GETNETCONF carrying a NETCONFA_IFINDEX attribute, read with
		// rtnl_talk (NLM_F_REQUEST|ACK, no NLM_F_DUMP), the first
		// attribute-carrying point get here. Still a link dump first, so the floor
		// is the ntable shape. Byte-grounded offline against the ip_netconf_dev4
		// sidecar. Runs 8 and 9: ip=3 goip=2 (the get by name `netconf show
		// dev` lacked too) AND two hygiene orphans, so this row was the one FAIL
		// not a WARN. The orphans were the kernel's ACK, which follows the point
		// get's reply because the request carries NLM_F_ACK; the segmenter
		// closed the transaction on the reply. G0-T03 closed an ACKed point get
		// on its ACK (pkg/nlparity's awaitsAck) and sent the get by name; its
		// two runs read 3/3 with no orphan, and it gated.
		Floor: 4, Implemented: true,
	},

	// The `-j` block.
	//
	// # What these rows claim, and what they do not
	//
	// `-j` provably does not reach the wire. iplink_filter_req's mask depends
	// on filter.vfinfo and show_stats and on nothing else
	// (ip/ipaddress.c:2017-2026, :2060-2068), and every is_json_context() call
	// site in iproute2 is inside a print function. So each row below is
	// byte-identical to its text twin on the request side and inherits the
	// twin's floor, and the request half is an ASSERTION of that rather than a
	// hope: a goip that threaded the output mode into a request would be
	// caught here and nowhere else.
	//
	// The value is on the stdout side, and it is specifically a LIVE `ip`.
	// JSON is already compared exactly offline — TestAddrShowJSONMatchesCaptured-
	// Sidecars and its link sibling diff goip's encoder against committed
	// `ip -j -p` output — but a committed sidecar cannot notice that the
	// nixpkgs iproute2 pin moved and changed a key's spelling. These rows can,
	// because both sides run in one capture window against one binary.
	//
	// # Why they are not gated in the commit that adds them
	//
	// Same bar as everything else here: a command joins gated_commands on its
	// own measured runs, not on the argument that it ought to be quiet. They
	// go in ungated, which also makes GOIP_PARITY_UNGATED_CLEAN count twelve
	// rows instead of two — the check was one gating decision away from its
	// third vacuity. They gated on G0-T01's two runs, which
	// TestQueuedReadRowsGated pins.
	//
	// # The facet work these rows depend on
	//
	// Until this block was written, stdout.go's extraction patterns were
	// text-only, and a `-j` row would have compared essentially nothing while
	// reporting PASS: reStanza, reKeyword, reDev and reStatsHeader match no
	// JSON at all, reCIDR4 cannot see an address whose prefix length is a
	// separate key, and FacetLines is 1 because json.NewEncoder emits one
	// line. Only reMAC survived, by coincidence. See stdout_json.go's
	// jsonFacets, which makes the SAME nine facets and 23 keywords reachable
	// from JSON without adding a locus.
	{
		Name: "-j addr show", Slug: "addr_show_json",
		// Four, as `addr show`. The widest stdout row of the six and the
		// reason the facet work happened at all.
		//
		// iproute2 does not print an address as one token in JSON. `local`
		// and `prefixlen` are separate keys (print_addrinfo,
		// ip/ipaddress.c), so `127.0.0.1/8` exists only after the comparator
		// joins them — which is the single mapping in jsonFacets that a
		// regex could not have done, and the one whose failure is silent:
		// unjoined, FacetCIDRs is empty and empty matches empty.
		//
		// It is also the only row reaching the addr_info nest, and therefore
		// the only one that compares valid_lft and preferred_lft under their
		// JSON spellings, valid_life_time and preferred_life_time.
		Floor: 4, Implemented: true,
	},
	{
		Name: "-j link show", Slug: "link_show_json",
		// Two, as `link show`. Five of the ten renames in jsonKeywordKeys are
		// link keys — operstate for state, linkmode for mode, txqlen for
		// qlen, broadcast for brd, and link_netnsid for link-netnsid — and
		// those spellings are iproute2's choice rather than a
		// transliteration, so they are exactly what a version bump moves. A
		// text-only comparison cannot see any of them.
		//
		// Only ONE of the five is unique to this row, and it was measured
		// rather than assumed: against the committed sidecars, operstate,
		// txqlen and broadcast appear in ip_addr_json as well, because an
		// addr listing carries the link stanza; linkmode appears in
		// ip_link_json alone; and link_netnsid appears in neither, which is
		// the dormant-but-capable case the keyword test classifies. So the
		// claim here is coverage WIDTH, not exclusivity — `-j addr show`
		// would catch a broken operstate too, and that redundancy is wanted
		// on a row whose floor is 2.
		Floor: 2, Implemented: true,
	},
	{
		Name: "-j route show", Slug: "route_show_json",
		// Four, as `route show`, and the row with the most container shapes
		// to walk. Nesting alone is not what singles it out — addr nests
		// addr_info and `-j -s link show` nests stats64 two deep — it is that
		// all three of the shapes below are unique to this row.
		//
		// Three shapes, each unique in the corpus: `metrics` is an array of
		// objects carrying mtu and advmss, where the text form prints them as
		// bare `mtu 1400 advmss 1300` tokens on the route line; `via` is an
		// OBJECT with family and host members rather than the string the text
		// form prints; and `nexthops` is an array whose length is the text
		// form's repeated `nexthop` word. A walker that recursed but recorded
		// only top-level keys would compare the first and miss all three.
		Floor: 4, Implemented: true,
	},
	{
		Name: "-j neigh show", Slug: "neigh_show_json",
		// Four, as `neigh show`. The row that exercises the presence-only rule.
		//
		// print_neigh's flag run emits bare positional words — `router`,
		// `extern_learn`, `extern_valid` — through print_null
		// (ip/ipneigh.c:440-451), and print_null's JSON is the key with a
		// **null** value, not `true`. That is the trap: a mapping written from
		// the text output alone would look for a boolean and find none, and a
		// mapping that read null as absent would drop the flag entirely.
		// jsonFacets treats null and print_bool's true alike, and records
		// either only when the key is in flagTokens — which is what keeps the
		// two formats in agreement, because addr's mngtmpaddr, nodad and
		// noprefixroute are presence-only too and are in neither flagTokens
		// nor the text facet, so they contribute nothing on either side.
		//
		// The flagged neighbors this depends on are in nltopo::build_clean,
		// which the parity harness builds from the same proc as the capture
		// script — so the fixtures and this row see the same entries.
		Floor: 4, Implemented: true,
	},
	{
		Name: "-j rule show", Slug: "rule_show_json",
		// Two, as `rule show`, and the row with the two most surprising
		// mappings in jsonFacets — both forced by what the TEXT extractor
		// already does rather than by what JSON offers.
		//
		// A rule's priority is filed under FacetIfIndexes, because the text
		// form opens each line with `100:` and reStanza is what reads it.
		// Nothing about a rule is an interface index; the facet is where the
		// text extractor puts the number, so JSON parity requires the same
		// filing. And `src`/`srclen` and `dst`/`dstlen` are split key pairs,
		// as addr's are, where the text form prints `from 192.0.2.0/24`.
		//
		// The declared asymmetry is `table`: JSON spells it `table`, text
		// spells it `lookup`, so the keyword facet is populated on the JSON
		// side and empty on the text side. That is pinned by a test row in
		// both directions rather than tolerated.
		Floor: 2, Implemented: true,
	},
	{
		Name: "-j -s link show", Slug: "link_show_stats_json",
		// Two, as `-s link show`, whose floor and whose permanent netlink
		// noise this row inherits — IFLA_STATS and IFLA_STATS64 differ between
		// any two captures and D_control absorbs them, exactly as the text
		// twin records.
		//
		// # The only row that reaches stats64, and the one most able to lie
		//
		// The text form prints an `RX:`/`TX:` heading line and a value line,
		// and FacetStatsHeaders compares the HEADINGS alone: the counters are
		// live and the column widths move with them, so comparing either
		// would report clean while asserting nothing. JSON has no heading
		// line at all — `stats64` is an object of `rx` and `tx`, each an
		// object whose KEYS are the headings and whose values are the
		// counters.
		//
		// So the port has to take the key names and discard the values, and
		// getting that backwards is the one mistake in this whole block that
		// would not announce itself: the row would simply be noisy forever,
		// and a permanently-noisy row has a precedent here to be read as
		// expected rather than as broken. It is not. If this row reports
		// stdout noise, the mapping is wrong and the fix is in jsonFacets.
		Floor: 2, Implemented: true,
	},
	{
		Name: "-j nexthop show", Slug: "nexthop_show_json",
		// Four, as `nexthop show`: `-j` does not reach the wire, so the request
		// is byte-identical to the text twin and the floor is inherited.
		//
		// The group is the one locus this row adds over its text twin's facets.
		// print_nh_group writes `group 1/2` as a single slash-joined token to the
		// text stream (ip/ipnexthop.c:255) and an array of {id[,weight]} objects
		// to the JSON stream, so jsonFacets rejoins the array to that token — the
		// same local+prefixlen synthesis `-j addr show` needs, and the reason a
		// walker that faced the array's inner `weight` keys would fill
		// keyword:weight on the JSON side alone. id/type/buckets/timers are
		// uncompared on both sides, as the text twin's are.
		//
		// Offline-grounded by internal/goip's TestNexthopShowJSONMatchesCaptured-
		// Sidecars; gated on runs 10 and 11 with its text twin, txns 3/3 and
		// `-j` not reaching the wire.
		Floor: 4, Implemented: true,
	},
	{
		Name: "-j addrlabel show", Slug: "addrlabel_show_json",
		// Two, as `addrlabel show`: `-j` does not reach the wire, so the request is
		// byte-identical to the text twin and the floor is inherited.
		//
		// The prefix is the one locus this row's mapping has to reconcile. The text
		// stream glues address and length into `prefix ADDR/LEN` (ip/ipaddrlabel.c:
		// 71-77) while the JSON stream splits them into `address` + `prefixlen`, so
		// jsonFacets synthesizes the text token from the two keys — the same
		// address/prefixlen synthesis `-j addr show` already does. `label` maps to
		// the text `label` token; both sides leave it as a value, not a locus.
		//
		// Offline-grounded by internal/goip's TestAddrLabelShowJSONMatchesCaptured-
		// Sidecars; gated on runs 10 and 11 with its text twin, txns 1/1 and
		// `-j` not reaching the wire.
		Floor: 2, Implemented: true,
	},
	{
		Name: "-j ntable show", Slug: "ntable_show_json",
		// Four, as `ntable show`: `-j` does not reach the wire, so the request is
		// byte-identical to the text twin and the floor is inherited (the bundled
		// link dump is still there).
		//
		// The reconciled loci are the dev names on device-specific sets: the text
		// `dev NAME` token and the JSON `dev` key both land in FacetDevNames, which
		// jsonFacetKeys already maps. Every other ntable token — thresholds, the
		// parameter counters, the config and stats numbers — is a bare value under
		// a named key that is a compared keyword on neither side, so it adds no
		// locus (the measured calibration in stdout_json_test confirms this).
		//
		// Offline-grounded by internal/goip's jsonEquivalent row against the
		// ip_ntable_json sidecar; gated on runs 8 and 9 with its text twin.
		Floor: 4, Implemented: true,
	},
	{
		Name: "-j netconf show", Slug: "netconf_show_json",
		// Four, as `netconf show`: `-j` does not reach the wire, so the request is
		// byte-identical to the text twin and the floor is inherited (the bundled
		// link dump is still there).
		//
		// No locus is reconciled — the measured calibration in stdout_json_test
		// fills zero non-lines loci on both sides, so the agreeing axis is the
		// entry count alone. The device token is the one value that could match,
		// but the text form prints it as a bare positional word with no `dev`
		// keyword (unlike ntable's `dev NAME`), so the text extractor ignores it
		// and the JSON `interface` key is left unmapped to stay symmetric; every
		// other netconf token is a bare value under a key compared on neither side.
		//
		// Offline-grounded by internal/goip's jsonEquivalent row against the
		// ip_netconf_json sidecar; gated on runs 8 and 9 with its text twin.
		Floor: 4, Implemented: true,
	},

	// The stats block — the tenth and last read-only object. Every row carries an
	// explicit `group link` (filter_mask 0x1), which keeps the reply link-only on
	// every topology (a bare multi-group show would print bridge xstats on mesh,
	// a shape goip refuses). The first 12-byte filter_mask-carrying request here.
	{
		Name: "stats show group link", Slug: "stats_show",
		// `ip stats` uniquely does NOT call ll_init_map: it issues the
		// RTM_GETSTATS dump (type 94) first, then names each returned ifindex
		// lazily with one RTM_GETLINK-by-index (ip/ipstats.c:761
		// ll_index_to_name), so its count is 1 + N interfaces (6 on a 5-iface
		// host) and topology-dependent. goip does the same, request for request
		// (internal/goip's TestStatsDumpRequestsMatchIpBytes).
		//
		// internal/goip's TestStatsShowMatchesCapturedSidecars diffs goip
		// byte-for-byte against the committed ip_stats sidecar across three
		// topologies; stdout matches. Runs 8 and 9 read ip=6 goip=2, when goip
		// named the records with one up-front link dump, and the row sat in the
		// permanent set; G0-T02's two runs read 6/6 and it gated. See
		// coverage-status.md.
		Floor: 4, Implemented: true,
	},
	{
		Name: "-s stats show group link", Slug: "stats_show_stats",
		// The request twin of `stats show group link`: `-s` bumps show_stats to 2
		// (the `show` verb's +1 plus the global -s) and selects the extended
		// RX-errors/TX-errors render, but does not reach the wire, so the floor is
		// inherited. Offline-grounded against the ip_stats_s sidecar (counters
		// normalized, the error lines compared exactly).
		Floor: 4, Implemented: true,
	},
	{
		Name: "stats show group link dev", Slug: "stats_show_dev",
		NeedsDev: true,
		// The point get: with a dev, ipstats_show_one issues a non-dump RTM_GETSTATS
		// carrying the ifindex in its 12-byte header (NLM_F_REQUEST alone — no DUMP,
		// no ACK, unlike netconf's point get), read with rtnl_talk. Ahead of it,
		// ll_name_to_index resolves the name with one RTM_GETLINK by name
		// (lib/ll_map.c:354-372). goip used to send a link dump there, which
		// kept the row in the permanent set; it now sends the same lookup, read
		// 2/2 with control nl=0 on two runs, and is gated. Byte-grounded
		// offline against the ip_stats_dev sidecar.
		Floor: 4, Implemented: true,
	},
	{
		Name: "-j stats show group link", Slug: "stats_show_json",
		// Four, as `stats show group link`: `-j` does not reach the wire, so the
		// request is byte-identical to the text twin and the floor is inherited. The
		// short JSON form (ifindex/ifname/group/stats64); the extended `-s` JSON keys
		// are not grounded, so goip refuses `-j -s` rather than emit a short object.
		//
		// Offline-grounded by internal/goip's jsonEquivalent row against the
		// ip_stats_json sidecar; gated with its text twin on G0-T02's two runs.
		Floor: 4, Implemented: true,
	},

	// The xstats block — `ip stats show group xstats` (filter_mask 0x2, no
	// GET_FILTERS nest). The whole group expands to four leaves, in the
	// pointer-sorted golden order bond/802.3ad, bridge/vlan, bridge/mcast,
	// bridge/stp, so every device prints four header stanzas with bodies only on
	// bridges (mesh's br0: vlan + mcast). goip grounds the vlan/mcast bodies,
	// the bond 802.3ad body and the empty stp headers; the stp body is refused.
	// The dev rows are the bond ones below, which bring their own bond.
	{
		Name: "stats show group xstats", Slug: "stats_show_xstats",
		// Four, the stats shape: the RTM_GETSTATS xstats dump, then one
		// RTM_GETLINK get per record, as the link group above. Grounded against
		// the committed ip_stats_xstats sidecar across three topologies. Runs 8
		// and 9 read ip=6 goip=2, the up-front link dump goip sent then;
		// G0-T02's two runs read 6/6 and it gated.
		Floor: 4, Implemented: true,
	},
	{
		Name: "-s stats show group xstats", Slug: "stats_show_xstats_stats",
		// `-s` does not reach the wire and the bridge bodies have no show_stats>1
		// branch, so the render equals the plain form; grounded against
		// ip_stats_xstats_s. Floor inherited.
		Floor: 4, Implemented: true,
	},
	{
		Name: "-j stats show group xstats", Slug: "stats_show_xstats_json",
		// The flat `-j` array, one object per leaf per device; `-j` does not reach
		// the wire, so the request is byte-identical to the text twin. Grounded
		// against ip_stats_xstats_json; gated with its text twin on G0-T02's
		// two runs.
		Floor: 4, Implemented: true,
	},
	// G3-T05's stats write: the by-name RTM_GETLINK that resolves d0, then
	// the RTM_SETSTATS and its ack.
	{
		Name: "stats set dev d0 l3_stats on", Slug: "stats_set_l3",
		Floor: 4, Implemented: true, Write: true, Setup: d0UpSetup,
	},
	{
		Name: "stats show dev bond0 group xstats", Slug: "stats_show_xstats_dev_bond",
		Setup: bondReadSetup,
		// The bond's BOND_XSTATS_3AD counters through the point get, the first
		// xstats dev row. Two transactions, the `stats show group link dev`
		// shape: ll_name_to_index's RTM_GETLINK by name, then one non-dump
		// RTM_GETSTATS with filter_mask 0x2 and no IFLA_STATS_GET_FILTERS nest
		// (ip/ipstats.c:578-585). The reply's ifindex is then cached, so there
		// is no third lookup.
		//
		// bondReadSetup's ports have no carrier, so every counter is zero and
		// stays zero across ip_a, goip and ip_b. This row and the four below
		// read 2/2 with control nl=0 and stdout=0 on their first two live
		// runs, and are gated (TestBondXstatsRowsGated).
		Floor: 4, Implemented: true,
	},
	{
		Name: "stats show dev la1 group xstats_slave", Slug: "stats_show_xstats_slave_dev_bond",
		Setup: bondReadSetup,
		// The per-port copy, filter_mask 0x4: the kernel answers an
		// XSTATS_SLAVE get on an 802.3ad port with LINK_XSTATS_TYPE_BOND
		// (drivers/net/bonding/bond_netlink.c:933-983). Two transactions:
		// unlike `-d link show dev la1`, nothing printed here names the veth
		// peer or the master, so no side lookups.
		Floor: 4, Implemented: true,
	},
	{
		Name: "stats show dev bond0 group xstats subgroup bond suite 802.3ad", Slug: "stats_show_xstats_dev_bond_3ad",
		Setup: bondReadSetup,
		// subgroup and suite are render-only for the xstats groups: the
		// request is byte-identical to the row above, and only the 802.3ad
		// stanza is printed where the plain form prints all four leaves.
		Floor: 4, Implemented: true,
	},
	{
		Name: "stats show dev la1 group xstats_slave subgroup bond suite 802.3ad", Slug: "stats_show_xstats_slave_dev_bond_3ad",
		Setup: bondReadSetup,
		// The render-only filter on the port, the same request as its plain
		// twin above.
		Floor: 4, Implemented: true,
	},
	{
		Name: "-j stats show dev la1 group xstats_slave", Slug: "stats_show_xstats_slave_dev_bond_json",
		Setup: bondReadSetup,
		// The flat `-j` array for the port: one object per leaf, the 802.3ad
		// one carrying the counters. `-j` does not reach the wire, so the
		// request is the text twin's.
		Floor: 4, Implemented: true,
	},

	// `ip link xstats` and `ip link afstats` (G1-T04): the same RTM_GETSTATS
	// dump as `stats show group xstats`, `xstats_slave` and an AF_SPEC group,
	// sent by iplink_ifla_xstats and iplink_afstats, against the C1-T03 reads.
	// linkXstatsSetup is the reads namespace's bridge: br0 and its port v2,
	// whose peer stays down so nothing moves the counters.
	{
		Name: "link xstats type bridge", Slug: "link_xstats_bridge",
		// The master-group dump, then br0 named by one RTM_GETLINK get, the
		// only record carrying the bridge type (4).
		Setup: linkXstatsSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "link xstats type bridge_slave", Slug: "link_xstats_bridge_slave",
		// The port-group dump, then v2 named the same way; v2's stanza adds
		// the stp block (4).
		Setup: linkXstatsSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "link afstats", Slug: "link_afstats",
		// The AF_SPEC dump alone: with no MPLS every nest is empty, nothing
		// prints and nothing is named (2).
		Setup: linkXstatsSetup, Floor: 2, Implemented: true,
	},
	// G3-T06's link property writes. iplink_prop_mod resolves `dev` after
	// its loop, so each is the by-name RTM_GETLINK, then the
	// RTM_NEWLINKPROP or RTM_DELLINKPROP and its ack.
	{
		Name: "link property add dev d0 altname d0alt", Slug: "link_altname_add",
		Floor: 4, Implemented: true, Write: true, Setup: d0UpSetup,
	},
	{
		Name: "link property del dev d0 altname d0alt", Slug: "link_altname_del",
		Floor: 4, Implemented: true, Write: true, Setup: linkAltnameSetup,
	},
	{
		Name: "link property add dev d0 altname lo", Slug: "link_altname_taken",
		// lo holds the name: EEXIST, exit 2.
		Floor: 4, Implemented: true, Write: true, Setup: d0UpSetup,
	},

	// G2-T01: `route save` and `route showdump` (ip/iproute.c:1673-1716,
	// :2390-2405), on route_save's C1-T02 precondition: d0, ifindex 3, and
	// table 100's two routes. save is `route show table 100`'s dump with
	// the replies written whole after the magic; its stdout is that file,
	// which the comparator recognizes and compares record by record
	// (stdout_dump.go). showdump reads route_save's committed ip.save,
	// which the runner carries in and the driver copies beside the captures,
	// and prints it, naming d0 by one RTM_GETLINK get (2). It reads no
	// route, but the get needs ifindex 3 to be d0, as it was in the capture.
	{
		Name: "route save table 100", Slug: "route_save",
		// The dump and its reply, the two routes and the DONE (2).
		Setup: routeSaveSetup, Floor: 2, Implemented: true,
	},
	{
		Name: "route showdump < route_save.save", Slug: "route_showdump",
		Setup: routeSaveSetup, Floor: 2, Implemented: true,
	},
	{
		Name: "-d route showdump < route_save.save", Slug: "route_showdump_details",
		// The type, `proto boot` and the scope on both lines; the routes
		// carry no nhid, so -d sends nothing more.
		Setup: routeSaveSetup, Floor: 2, Implemented: true,
	},
	// G2-T02: `route restore` (iproute_restore), the C1-T02 route_restore
	// row: route_save's file restored on PRE_D0_ADDR, d0 up at ifindex 3
	// with no route in table 100. ip sends ll_init_map's link dump, then the
	// device route and the gateway route, in restore_handler's pass order
	// and not the file's, each answered; goip sends the link dump and writes
	// the two. Floor is 4, as for the address restore rows, because goip's
	// side gets Floor-2 and sends only the dump.
	{
		Name: "route restore < route_save.save", Slug: "route_restore",
		Floor: 4, Implemented: true, Write: true, Writes: 2, Setup: routeGetSetup,
	},
	// G2-T03: `route flush` (iproute_flush), the C1-T02 rows route_flush,
	// route_flush_empty and route_flush_no_table. The first is a write row:
	// ip dumps table 100, sends the two RTM_DELROUTE copies in one datagram,
	// unacked, and dumps again to find nothing; goip sends both dumps and
	// writes the copies, its second dump skipping what it copied. ip's six
	// datagrams set the floor at 5, and goip's side, which sends no batch,
	// gets 3. The other two delete nothing, so ip sends the dump alone and
	// goip needs no GOIP_WRITE_OUT: they are read rows. Their Names
	// abbreviate `table`, which the bytes do not show, since each Name is
	// one row. Neither may run on the shared topology, whose table 100 holds
	// routes `ip_a` would really delete (measured on G2-T03's first run).
	{
		Name: "route flush table 100", Slug: "route_flush",
		Floor: 5, Implemented: true, Write: true, Writes: 2, Setup: routeSaveSetup,
	},
	{
		Name: "-s route flush table 100", Slug: "route_flush_stats",
		// The round text and the closing line, compared as stdout.
		Floor: 5, Implemented: true, Write: true, Writes: 2, Setup: routeSaveSetup,
	},
	{
		Name: "route flush tab 100", Slug: "route_flush_empty",
		// Table 100 added to and emptied: the dump and its DONE (2).
		Floor: 2, Implemented: true, Setup: routeFlushEmptySetup,
	},
	{
		Name: "route flush t 100", Slug: "route_flush_no_table",
		// A namespace of its own with no table 100: the dump answered ENOENT
		// in its DONE, Flush terminated, exit 2 (2). The capture had no
		// precondition; the one step makes the row a Setup row, built in its
		// own namespace, and touches no route table but local.
		Floor: 2, Implemented: true, Setup: routeFlushNoTableSetup,
	},

	// The vrf block — `ip vrf show`, the eleventh read-only object. The request
	// is a filtered link dump: RTM_GETLINK, AF_UNSPEC, one IFLA_LINKINFO nest
	// holding IFLA_INFO_KIND = "vrf" and no IFLA_EXT_MASK. The kernel does not
	// honor the kind filter, so the reply is a full link dump and goip filters
	// kind=="vrf" client-side, exactly as ipvrf_print does. Base carries goipvrf;
	// mesh and tunnel have no VRF (the empty "No VRF has been configured" form).
	// The first request in the corpus that filters a link dump by linkinfo kind.
	{
		Name: "vrf show", Slug: "vrf_show",
		// Two, the link-dump floor: the filtered dump is the sole request, and
		// do_ipvrf runs no ll_init_map (names come from the same reply's
		// IFLA_IFNAME), so there is no side transaction — unlike netconf/stats.
		//
		// internal/goip's TestVrfShowMatchesCapturedSidecars diffs goip
		// byte-for-byte against the committed ip_vrf sidecar across three
		// topologies. Gated on runs 8 and 9: the pristine control: nl=0 stdout=0
		// pair, txns 1/1 on both — the bar `link show` set.
		Floor: 2, Implemented: true,
	},
	{
		Name: "-j vrf show", Slug: "vrf_show_json",
		// Two, as `vrf show`: `-j` does not reach the wire, so the request is
		// byte-identical to the text twin and the floor is inherited. The flat
		// array of {name, table}; the empty topologies render `[]`. Grounded
		// against ip_vrf_json; gated on runs 8 and 9 with its text twin.
		Floor: 2, Implemented: true,
	},
	// The link write rows (link-write-path.md phase 7): the 21 rows of the
	// committed corpus in pkg/xtcpnl/testdata/7_1_4/link-write/write/, with
	// the same preconditions and floors as capture-link-writes.exp. All 21
	// gated on the phase 7 runs, two back to back with identical verdicts:
	// txns matching ip (goip's write file counted as its write) and control
	// nl=0 on the rows with no lookups, nl=2 to 4 on the rest, all of it
	// reply state of a fresh namespace (a new link's random MAC, cacheinfo).
	{
		Name: "link add d0 type dummy", Slug: "link_write_add_dummy",
		// The request and its ACK; add resolves no name.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add v0 type veth peer name v1", Slug: "link_write_add_veth",
		// The peer's own ifinfomsg inside VETH_INFO_PEER.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add bond0 type bond", Slug: "link_write_add_bond_bare",
		// INFO_KIND alone: no arguments, so no INFO_DATA nest.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add bond0 type bond mode 802.3ad lacp_rate fast miimon 100 xmit_hash_policy layer3+4", Slug: "link_write_add_bond_8023ad",
		// Worked example 1.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add bond0 type bond mode 802.3ad lacp_rate fast lacp_active on ad_select bandwidth ad_actor_sys_prio 100 ad_user_port_key 7 ad_actor_system 02:00:00:00:00:0a min_links 1", Slug: "link_write_add_bond_all_lacp",
		// Every 802.3ad option, the u16 priorities and key included.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add bond0 type bond mode active-backup arp_interval 100 arp_ip_target 10.0.0.1,10.0.0.2 arp_validate all", Slug: "link_write_add_bond_ab_arp",
		// The index-numbered ARP_IP_TARGET nest.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add bond0 type bond mode 4", Slug: "link_write_add_bond_numeric_mode",
		// Mode 4 is 802.3ad: the same MODE byte.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link set la1 down master bond0", Slug: "link_write_enslave_down",
		// Worked example 2: master is resolved before dev, two lookups, then the write.
		Floor: 6, Implemented: true, Write: true, Setup: linkWriteSetupPort,
	},
	{
		Name: "link set la1 master bond0", Slug: "link_write_enslave_up",
		// Refused EPERM; compared on the request, as goip cannot know the kernel's answer.
		Floor: 6, Implemented: true, Write: true, Setup: linkWriteSetupPortUp,
	},
	{
		Name: "link set la1 nomaster", Slug: "link_write_nomaster",
		// IFLA_MASTER 0, after one lookup.
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupEnslaved,
	},
	{
		Name: "link set la1 type bond_slave queue_id 1", Slug: "link_write_slave_queue_id",
		// INFO_SLAVE_DATA.
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupEnslaved,
	},
	{
		Name: "link set la1 type bond_slave actor_port_prio 200", Slug: "link_write_slave_actor_port_prio",
		// A u16 port option.
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupEnslaved,
	},
	{
		Name: "link set bond0 type bond lacp_rate slow", Slug: "link_write_set_lacp_rate_up",
		// Refused EBUSY.
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupBondUp,
	},
	{
		Name: "link set bond0 type bond mode balance-rr", Slug: "link_write_set_mode_with_ports",
		// Refused ENOTEMPTY.
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupEnslaved,
	},
	{
		Name: "link set bond0 type bond lacp_rate fast", Slug: "link_write_set_lacp_rate_rr",
		// Refused EACCES.
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupBondRR,
	},
	{
		Name: "link add bond0 type bond arp_interval 100 miimon 100", Slug: "link_write_arp_and_miimon",
		// Refused EINVAL.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link replace bond0 type bond", Slug: "link_write_replace_existing",
		// Refused EOPNOTSUPP, with no extack.
		Floor: 2, Implemented: true, Write: true, Setup: linkWriteSetupBond,
	},
	{
		Name: "link set bond0 up", Slug: "link_write_set_up",
		// Ifi_flags and ifi_change 0x1.
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupBond,
	},
	{
		Name: "link set bond0 down", Slug: "link_write_set_down",
		// Ifi_change 0x1 with the flag clear.
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupBondUp,
	},
	{
		Name: "link set d0 name d1 mtu 9000 alias uplink", Slug: "link_write_rename_mtu_alias",
		// A rename sends IFLA_IFNAME on a set; IFLA_IFALIAS has no NUL.
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link del bond0", Slug: "link_write_del_bond",
		// RTM_DELLINK by index.
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupEnslaved,
	}, // The route write rows (route-write-path.md phase 5): the corpus in
	// pkg/xtcpnl/testdata/7_1_4/link-write/route/, less `nodev`, with the
	// same preconditions and floors as capture-link-writes.exp. A dev row is
	// the d0 lookup and the write (4); a dev-less row is the write alone (2).
	// `nodev` is not a row: goip stops at the lookup with no write file, as
	// ip does, and TestRouteWriteNodevCorpus holds it offline.
	{
		Name: "route replace 192.0.2.50/32 dev d0 congctl lock cubic rto_min 3ms initcwnd 32 initrwnd 32 quickack 1", Slug: "route_write_replace_nfs",
		// The NFS block: five metrics and RTAX_LOCK 0x12000, after the d0 lookup.
		Floor: 4, Implemented: true, Write: true, Setup: routeWriteSetupDev,
	},
	{
		Name: "route replace 198.51.100.0/24 dev d0", Slug: "route_write_replace_dev",
		// The d0 lookup, then DST and OIF.
		Floor: 4, Implemented: true, Write: true, Setup: routeWriteSetupDev,
	},
	{
		Name: "route add 198.51.100.0/24 dev d0", Slug: "route_write_add_dev",
		Floor: 4, Implemented: true, Write: true, Setup: routeWriteSetupDev,
	},
	{
		Name: "route add 198.51.100.0/24 via 192.0.2.254 dev d0", Slug: "route_write_add_via_dev",
		// A gateway keeps scope universe.
		Floor: 4, Implemented: true, Write: true, Setup: routeWriteSetupDev,
	},
	{
		Name: "route add 198.51.100.0/24 via 192.0.2.254", Slug: "route_write_add_via_nodev",
		// No dev, so no lookup: the write alone.
		Floor: 2, Implemented: true, Write: true, Setup: routeWriteSetupDev,
	},
	{
		Name: "route replace default via 192.0.2.254", Slug: "route_write_replace_default",
		// dst_len 0 and no RTA_DST.
		Floor: 2, Implemented: true, Write: true, Setup: routeWriteSetupDev,
	},
	{
		Name: "route change 198.51.100.0/24 dev d0 metric 100", Slug: "route_write_change_metric",
		// ENOENT: the metric is part of the route's identity.
		Floor: 4, Implemented: true, Write: true, Setup: routeWriteSetupRoute,
	},
	{
		Name: "route append 198.51.100.0/24 dev d0 metric 200", Slug: "route_write_append_dev",
		Floor: 4, Implemented: true, Write: true, Setup: routeWriteSetupRoute,
	},
	{
		Name: "route prepend 198.51.100.0/24 dev d0 metric 300", Slug: "route_write_prepend_dev",
		Floor: 4, Implemented: true, Write: true, Setup: routeWriteSetupRoute,
	},
	{
		Name: "route test 198.51.100.0/24 dev d0", Slug: "route_write_test_dev",
		// EEXIST, which is what test is for.
		Floor: 4, Implemented: true, Write: true, Setup: routeWriteSetupRoute,
	},
	{
		Name: "route del 198.51.100.0/24 dev d0", Slug: "route_write_del_dev",
		// RTM_DELROUTE: proto 0, type 0, scope NOWHERE.
		Floor: 4, Implemented: true, Write: true, Setup: routeWriteSetupRoute,
	},
	{
		Name: "route del 198.51.100.0/24", Slug: "route_write_del_nodev",
		Floor: 2, Implemented: true, Write: true, Setup: routeWriteSetupRoute,
	},
	{
		Name: "route del 203.0.113.99/32", Slug: "route_write_del_missing",
		// ESRCH.
		Floor: 2, Implemented: true, Write: true, Setup: routeWriteSetupDev,
	},
	{
		Name: "route add 198.51.100.0/24 dev d0 table 100", Slug: "route_write_add_table_100",
		Floor: 4, Implemented: true, Write: true, Setup: routeWriteSetupDev,
	},
	{
		Name: "route add 198.51.100.0/24 dev d0 table 300", Slug: "route_write_add_table_300",
		// rtm_table 0 and RTA_TABLE 300, at its argv position.
		Floor: 4, Implemented: true, Write: true, Setup: routeWriteSetupDev,
	},
	{
		Name: "route add blackhole 198.51.100.0/24", Slug: "route_write_add_blackhole",
		// No Setup: a fresh namespace has what a blackhole needs.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "route add unreachable 198.51.100.0/24", Slug: "route_write_add_unreachable",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "route add local 192.0.2.77 dev d0", Slug: "route_write_add_local",
		// Table local and scope host, chosen by the type.
		Floor: 4, Implemented: true, Write: true, Setup: routeWriteSetupDev,
	},
	{
		Name: "route add 198.51.100.0/24 dev d0 src 192.0.2.1 proto static scope link metric 50", Slug: "route_write_add_src_proto_scope",
		Floor: 4, Implemented: true, Write: true, Setup: routeWriteSetupDev,
	},
	{
		Name: "route add 198.51.100.0/24 via 203.0.113.1 dev d0 onlink", Slug: "route_write_add_onlink",
		Floor: 4, Implemented: true, Write: true, Setup: routeWriteSetupDev,
	},
	{
		Name: "route add 198.51.100.0/24 via inet6 2001:db8::2 dev d0", Slug: "route_write_add_via_inet6",
		// RTA_VIA: a v6 next hop on a v4 route (RFC 5549).
		Floor: 4, Implemented: true, Write: true, Setup: routeWriteSetupDev6,
	},
	{
		Name: "-6 route replace 2001:db8:1::/64 via 2001:db8::2 dev d0", Slug: "route_write_v6_replace_via",
		Floor: 4, Implemented: true, Write: true, Setup: routeWriteSetupDev6,
	},
	{
		Name: "-6 route del 2001:db8:1::/64 dev d0", Slug: "route_write_v6_del",
		// A v6 delete keeps scope universe.
		Floor: 4, Implemented: true, Write: true, Setup: routeWriteSetupV6Route,
	},
	{
		Name: "route replace 198.51.100.0/24 dev d0 mtu lock 1400 advmss 1360 window 65535 rtt 3ms rttvar 10ms ssthresh 100 cwnd 10 reordering 5 hoplimit 64 initcwnd 10 initrwnd 20 features ecn tcp_usec_ts fastopen_no_cookie 1", Slug: "route_write_metrics_all",
		// Every u32 metric keyword.
		Floor: 4, Implemented: true, Write: true, Setup: routeWriteSetupDev,
	},
	{
		Name: "route replace 198.51.100.0/24 dev d0 rtt 24 rttvar 40 rto_min 200", Slug: "route_write_metrics_raw",
		// Bare numbers are raw; rto_min always locks.
		Floor: 4, Implemented: true, Write: true, Setup: routeWriteSetupDev,
	},
	{
		Name: "route replace 198.51.100.0/24 dev d0 initcwnd 10 initcwnd 20", Slug: "route_write_metric_repeated",
		// Both sub-attributes go out, in order.
		Floor: 4, Implemented: true, Write: true, Setup: routeWriteSetupDev,
	},
	// The address write rows (addr-write-path.md phase 7): the corpus in
	// pkg/xtcpnl/testdata/7_1_4/link-write/addr/, with the preconditions and
	// floors of addr-corpus.exp. Each is the d0 lookup and the write (4); a
	// kernel refusal is still a write row, since the comparator compares the
	// request alone. The five slugs that send no write (nodev, valid_zero,
	// autojoin_unicast, no_dev_keyword, brd_plus_v6) are not rows: goip
	// writes no file and exits 1, and TestAddrWriteNodevCorpus holds them
	// offline.
	{
		Name: "addr add 192.0.2.10/24 dev d0", Slug: "addr_write_add_v4",
		// The plain IPv4 add: LOCAL and ADDRESS both.
		Floor: 4, Implemented: true, Write: true, Setup: addrWriteSetupDev,
	},
	{
		Name: "addr add 10.0.0.1 peer 10.0.0.2/32 dev d0", Slug: "addr_write_add_v4_peer",
		// The peer is IFA_ADDRESS, with no copy of the local.
		Floor: 4, Implemented: true, Write: true, Setup: addrWriteSetupDev,
	},
	{
		Name: "addr add 192.0.2.10/24 brd + dev d0", Slug: "addr_write_add_v4_brd_plus",
		// brd + is computed last: .255.
		Floor: 4, Implemented: true, Write: true, Setup: addrWriteSetupDev,
	},
	{
		Name: "addr add 192.0.2.10/24 brd - dev d0", Slug: "addr_write_add_v4_brd_minus",
		// brd - is the network address: .0.
		Floor: 4, Implemented: true, Write: true, Setup: addrWriteSetupDev,
	},
	{
		Name: "addr add 192.0.2.10/31 brd + dev d0", Slug: "addr_write_add_v4_brd_31",
		// A /31 gets no broadcast.
		Floor: 4, Implemented: true, Write: true, Setup: addrWriteSetupDev,
	},
	{
		Name: "addr add 192.0.2.10/24 brd + label d0:1 dev d0", Slug: "addr_write_add_v4_label",
		// The label goes before the computed broadcast.
		Floor: 4, Implemented: true, Write: true, Setup: addrWriteSetupDev,
	},
	{
		Name: "addr add 192.0.2.10/24 metric 50 dev d0", Slug: "addr_write_add_v4_metric",
		// IFA_RT_PRIORITY.
		Floor: 4, Implemented: true, Write: true, Setup: addrWriteSetupDev,
	},
	{
		Name: "addr add 192.0.2.10/24 pref 7 dev d0", Slug: "addr_write_add_v4_pref_is_metric",
		// pref is metric, not preferred_lft.
		Floor: 4, Implemented: true, Write: true, Setup: addrWriteSetupDev,
	},
	{
		Name: "addr add 192.0.2.10/24 proto kernel_lo dev d0", Slug: "addr_write_add_v4_proto_name",
		// IFA_PROTO by name.
		Floor: 4, Implemented: true, Write: true, Setup: addrWriteSetupDev,
	},
	{
		Name: "addr add 192.0.2.10/24 valid_lft 3600 preferred_lft 1800 dev d0", Slug: "addr_write_add_v4_lifetimes",
		// IFA_CACHEINFO {1800, 3600}: request bytes, so no countdown.
		Floor: 4, Implemented: true, Write: true, Setup: addrWriteSetupDev,
	},
	{
		Name: "addr add 192.0.2.2/24 dev d0", Slug: "addr_write_add_v4_secondary",
		// A secondary is the same request; the kernel marks it.
		Floor: 4, Implemented: true, Write: true, Setup: addrWriteSetupV4,
	},
	{
		Name: "addr add 127.0.0.2/8 dev d0", Slug: "addr_write_add_v4_loopback_scope",
		// 127/8 gets host scope.
		Floor: 4, Implemented: true, Write: true, Setup: addrWriteSetupDev,
	},
	{
		Name: "addr add 224.0.0.251/32 autojoin dev d0", Slug: "addr_write_add_v4_autojoin",
		// autojoin spills to IFA_FLAGS.
		Floor: 4, Implemented: true, Write: true, Setup: addrWriteSetupDev,
	},
	{
		Name: "-6 addr add 2001:db8::5/64 nodad dev d0", Slug: "addr_write_add_v6_nodad",
		// nodad fits the header"s flags byte.
		Floor: 4, Implemented: true, Write: true, Setup: addrWriteSetupDev,
	},
	{
		Name: "-6 addr add 2001:db8::5/64 nodad noprefixroute dev d0", Slug: "addr_write_add_v6_flags_wide",
		// The wide flags: 0x202 in IFA_FLAGS.
		Floor: 4, Implemented: true, Write: true, Setup: addrWriteSetupDev,
	},
	{
		Name: "addr add nodad 2001:db8::5/64 dev d0", Slug: "addr_write_add_v6_nodad_first",
		// A flag before the address warns and is dropped.
		Floor: 4, Implemented: true, Write: true, Setup: addrWriteSetupDev,
	},
	{
		Name: "-6 addr add 2001:db8::5/48 mngtmpaddr dev d0", Slug: "addr_write_add_v6_mngtmpaddr_48",
		// Sent; the kernel refuses mngtmpaddr on a /48.
		Floor: 4, Implemented: true, Write: true, Setup: addrWriteSetupDev,
	},
	{
		Name: "-6 addr add 2001:db8::5/64 nodad optimistic dev d0", Slug: "addr_write_add_v6_nodad_optimistic",
		// Both flags; this kernel clears optimistic and ACKs.
		Floor: 4, Implemented: true, Write: true, Setup: addrWriteSetupDev,
	},
	{
		Name: "addr add 192.0.2.1/24 dev d0", Slug: "addr_write_add_existing",
		// Sent; the kernel answers EEXIST.
		Floor: 4, Implemented: true, Write: true, Setup: addrWriteSetupV4,
	},
	{
		Name: "addr change 192.0.2.10/24 dev d0", Slug: "addr_write_change_missing_creates",
		// change is NLM_F_REPLACE alone.
		Floor: 4, Implemented: true, Write: true, Setup: addrWriteSetupDev,
	},
	{
		Name: "addr replace 192.0.2.1/24 metric 9 dev d0", Slug: "addr_write_replace_existing",
		// replace is CREATE|REPLACE.
		Floor: 4, Implemented: true, Write: true, Setup: addrWriteSetupV4,
	},
	{
		Name: "addr del 192.0.2.1/24 dev d0", Slug: "addr_write_del_v4",
		// RTM_DELADDR keeps scope 0.
		Floor: 4, Implemented: true, Write: true, Setup: addrWriteSetupV4,
	},
	{
		Name: "addr del 192.0.2.1 dev d0", Slug: "addr_write_del_v4_wildcard",
		// The wildcard delete warns and sends no IFA_ADDRESS.
		Floor: 4, Implemented: true, Write: true, Setup: addrWriteSetupV4,
	},
	{
		Name: "addr del 192.0.2.99/24 dev d0", Slug: "addr_write_del_missing",
		// Sent; the kernel answers EADDRNOTAVAIL.
		Floor: 4, Implemented: true, Write: true, Setup: addrWriteSetupDev,
	},
	{
		Name: "addr add 192.0.2.2/24 scope link dev d0", Slug: "addr_write_scope_conflict",
		// Sent; the kernel refuses a secondary with another scope.
		Floor: 4, Implemented: true, Write: true, Setup: addrWriteSetupV4,
	},
	{
		Name: "-6 addr add ff02::fb/128 dev d0", Slug: "addr_write_v6_mcast_noautojoin",
		// Sent; the kernel refuses IPv6 multicast without autojoin.
		Floor: 4, Implemented: true, Write: true, Setup: addrWriteSetupDev,
	},
	{
		Name: "addr add 192.0.2.10/24 label d0:0123456789abc dev d0", Slug: "addr_write_label_16",
		// A 16-byte label is sent; the kernel answers ERANGE.
		Floor: 4, Implemented: true, Write: true, Setup: addrWriteSetupDev,
	},
	// The address flush rows (addr-write-path.md phase 8): the corpus in
	// pkg/xtcpnl/testdata/7_1_4/link-write/addr-flush/, with the
	// preconditions and floors of addr-corpus.exp. ip sends the lookup, the
	// round-1 dump, the DEL batch with no ACK and, unless primary stops it,
	// the round-2 dump; goip sends the same less the batch, which it writes.
	// Writes is each slug's DEL count. Four slugs are not rows, and V051 and
	// V054 hold them offline: flush_nothing writes no file, flush_noargs and
	// flush_packet send nothing, and flush_primary_flipped needs a sysctl
	// that Setup cannot express.
	{
		Name: "addr flush dev d0", Slug: "addr_flush_dev",
		// Four DELs: both IPv4 addresses, 2001:db8::1 and the eui64 link-local.
		Floor: 7, Implemented: true, Write: true, Writes: 4, Setup: addrFlushDevSetup,
	},
	{
		Name: "addr flush dev d0 primary", Slug: "addr_flush_primary",
		// One round: the primary's DEL, and no round-2 dump.
		Floor: 5, Implemented: true, Write: true, Writes: 1, Setup: v4SecondarySetup,
	},
	{
		Name: "addr flush dev d0 secondary", Slug: "addr_flush_secondary",
		Floor: 7, Implemented: true, Write: true, Writes: 1, Setup: v4SecondarySetup,
	},
	{
		Name: "addr flush dev d0 scope link", Slug: "addr_flush_scope_link",
		Floor: 7, Implemented: true, Write: true, Writes: 1, Setup: addrFlushScopeLinkSetup,
	},
	{
		Name: "-6 addr flush dev d0", Slug: "addr_flush_v6",
		// The four IPv6 addresses; addrgenmode none leaves no link-local.
		Floor: 7, Implemented: true, Write: true, Writes: 4, Setup: addrFlushV6Setup,
	},
	{
		Name: "-s addr flush dev d0", Slug: "addr_flush_stats",
		// stdout is the round text, compared like any row's.
		Floor: 7, Implemented: true, Write: true, Writes: 2, Setup: addrFlushStatsSetup,
	},
	{
		// The bare device name: ip's keyword loop reads it as dev
		// (ip/ipaddress.c:2240-2246), so the bytes are flush_many's capture
		// and the Name does not collide with addr_flush_dev's.
		Name: "addr flush d0", Slug: "addr_flush_many",
		// 120 DELs, which ip sends as datagrams of 47, 47 and 26.
		Floor: 7, Implemented: true, Write: true, Writes: 120, Setup: addrFlushManySetup(),
	},

	// The address restore rows (addr-write-path.md phase 9): the corpus in
	// pkg/xtcpnl/testdata/7_1_4/link-write/addr-save/, with the
	// preconditions of addr-corpus.exp. Each reads its own slug's ip.save,
	// which the runner carries into the guest as a store path and the driver
	// copies into the capture directory as <slug>.save; the two files are
	// byte-identical, but the Names must differ. ip sends ll_init_map's link
	// dump, then the 8 saved messages, each answered; goip sends the link
	// dump and writes the 8. Floor is addr-corpus.exp's 4, not ip's 18
	// datagrams, because goip's side gets Floor-2 and sends only the dump.
	// showdump sends nothing, so it has no row: its parity is
	// TestAddrShowdumpMatchesSidecar, offline (addr-write-path.md, "save,
	// showdump and restore").
	{
		Name: "addr restore < restore_fresh.save", Slug: "addr_restore_fresh",
		// lo's 127.0.0.1 and ::1 already exist, so 2 of the 8 are EEXIST.
		Floor: 4, Implemented: true, Write: true, Writes: 8, Setup: addrWriteSetupDev,
	},
	{
		Name: "addr restore < restore_existing.save", Slug: "addr_restore_existing",
		// save_all's addresses are all in place: every write is EEXIST.
		Floor: 4, Implemented: true, Write: true, Writes: 8, Setup: addrRestoreExistingSetup,
	},
})

// The write rows' preconditions, as capture-link-writes.exp spells them:
// PRE_BOND, PRE_PORT and PRE_ENSLAVED there.
var (
	linkWriteSetupBond     = []string{"link add bond0 type bond mode 802.3ad"}
	linkWriteSetupBondUp   = []string{"link add bond0 type bond mode 802.3ad", "link set bond0 up"}
	linkWriteSetupBondRR   = []string{"link add bond0 type bond mode balance-rr"}
	linkWriteSetupDummy    = []string{"link add d0 type dummy"}
	linkWriteSetupPort     = []string{"link add bond0 type bond mode 802.3ad", "link add la1 type veth peer name lb1"}
	linkWriteSetupPortUp   = []string{"link add bond0 type bond mode 802.3ad", "link add la1 type veth peer name lb1", "link set la1 up"}
	linkWriteSetupEnslaved = []string{"link add bond0 type bond mode 802.3ad", "link add la1 type veth peer name lb1", "link set la1 master bond0"}
)

// The route write rows' preconditions: PRE_DEV, PRE_DEV6 and PRE_ROUTE in
// capture-link-writes.exp, and v6_del's own.
var (
	routeWriteSetupDev     = []string{"link add d0 type dummy", "link set d0 up", "addr add 192.0.2.1/24 dev d0"}
	routeWriteSetupDev6    = []string{"link add d0 type dummy", "link set d0 up", "addr add 192.0.2.1/24 dev d0", "-6 addr add 2001:db8::1/64 nodad dev d0"}
	routeWriteSetupRoute   = []string{"link add d0 type dummy", "link set d0 up", "addr add 192.0.2.1/24 dev d0", "route add 198.51.100.0/24 dev d0"}
	routeWriteSetupV6Route = []string{"link add d0 type dummy", "link set d0 up", "addr add 192.0.2.1/24 dev d0", "-6 addr add 2001:db8::1/64 nodad dev d0", "-6 route add 2001:db8:1::/64 dev d0"}
)

// The neigh write rows' preconditions: PRE_VETH, PRE_NEIGH and
// PRE_NEIGH_STALE in rtnl-write-corpus.exp, with the veths' MACs pinned and
// addrgenmode none, so d0 has no link-local and joins no solicited-node
// group: the neighbor dumps hold the setup's entries alone on every side.
var (
	neighWriteSetupVeth = []string{
		"link add d0 address 02:00:00:00:d0:00 type veth peer name d1 address 02:00:00:00:d1:00",
		"link set d0 addrgenmode none", "link set d1 addrgenmode none", "link set d1 up", "link set d0 up",
	}
	neighWriteSetup = append(append([]string{}, neighWriteSetupVeth...),
		"neigh add 192.0.2.2 lladdr 02:00:00:00:00:02 dev d0 nud permanent")
	neighFlushSetup = append(append([]string{}, neighWriteSetupVeth...),
		"neigh add 192.0.2.4 lladdr 02:00:00:00:00:04 dev d0 nud stale",
		neighFlushSecondEntry)
)

// neighFlushSecondEntry is the flush rows' second stale entry, an IPv6 one
// where the capture's is 192.0.2.5. The kernel hashes neighbor entries with
// a per-namespace salt, so two IPv4 entries dump in an order each namespace
// picks for itself, and the first live run had goip's copies in the other
// order to ip_a's; key order is unsuppressible. An AF_UNSPEC dump walks the
// ARP table before the ND table, so one entry of each family dumps in one
// order on every side.
const neighFlushSecondEntry = "neigh add 2001:db8::5 lladdr 02:00:00:00:00:05 dev d0 nud stale"

// The `neigh get` rows' preconditions: PRE_VETH and PRE_NEIGH in
// rtnl-write-corpus.exp. A dummy is IFF_NOARP and holds no neighbor entry.
var (
	neighGetSetupVeth = []string{"link add d0 type veth peer name d1", "link set d1 up", "link set d0 up"}
	neighGetSetup     = []string{
		"link add d0 type veth peer name d1", "link set d1 up", "link set d0 up",
		"neigh add 192.0.2.2 lladdr 02:00:00:00:00:02 dev d0 nud permanent",
	}
)

// The nexthop rows' preconditions: PRE_NH and PRE_NH_RES in
// rtnl-write-corpus.exp, and the two below.
var (
	nexthopGetSetup = []string{
		"link add d0 type dummy", "link set d0 up", "addr add 192.0.2.1/24 dev d0",
		"nexthop add id 1 via 192.0.2.2 dev d0", "nexthop add id 2 via 192.0.2.3 dev d0",
	}
	nexthopBucketSetup = append(append([]string{}, nexthopGetSetup...),
		"nexthop add id 20 group 1/2 type resilient buckets 1024")
	// nexthopD0Setup is PRE_D0_ADDR, nexthop_add_single's, and
	// nexthopBlackholeSetup nexthop_add_existing's and nexthop_del's one step.
	nexthopD0Setup        = nexthopGetSetup[:3:3]
	nexthopBlackholeSetup = []string{"nexthop add id 1 blackhole"}
	// addrLabelSetup is addrlabel_del's and addrlabel_flush's one step.
	addrLabelSetup = []string{"addrlabel add prefix 2001:db8::/32 label 99"}
	// d0UpSetup is PRE_D0, the two steps of ntable_change, stats_set_l3,
	// link_altname_add and link_altname_taken; linkAltnameSetup adds
	// link_altname_del's altname.
	d0UpSetup        = []string{"link add d0 type dummy", "link set d0 up"}
	linkAltnameSetup = append(append([]string{}, d0UpSetup...), "link property add dev d0 altname d0alt")
)

// The `link xstats` and `link afstats` rows' precondition: the bridge of
// rtnl-read-corpus.exp's build list.
var linkXstatsSetup = []string{
	"link add br0 type bridge", "link add v2 type veth peer name v2p",
	"link set v2 master br0", "link set br0 up", "link set v2 up",
}

// The rule rows' preconditions: rule_add_existing's and rule_del's one rule,
// and PRE_RULES in rtnl-write-corpus.exp, rule_flush's and rule_save's.
var (
	ruleExistingSetup = []string{"rule add pref 100 from 192.0.2.0/24 table 100"}
	ruleFlushSetup    = append(append([]string{}, ruleExistingSetup...), "rule add pref 101 fwmark 7 table 101")
)

// routeSaveSetup is PRE_ROUTES in rtnl-write-corpus.exp, route_save's
// topology step for step.
var routeSaveSetup = []string{
	"link add d0 type dummy", "link set d0 up", "addr add 192.0.2.1/24 dev d0",
	"route add 203.0.113.0/24 dev d0 table 100", "route add 198.51.100.0/24 via 192.0.2.2 table 100",
}

// routeFlushEmptySetup is PRE_TABLE_EMPTY in rtnl-write-corpus.exp,
// route_flush_empty's topology step for step: table 100 made and emptied.
var routeFlushEmptySetup = []string{
	"link add d0 type dummy", "link set d0 up", "addr add 192.0.2.1/24 dev d0",
	"route add 203.0.113.0/24 dev d0 table 100", "route del 203.0.113.0/24 dev d0 table 100",
}

// routeFlushNoTableSetup is route_flush_no_table's namespace: the capture
// had no precondition, and a Setup is what gives a read row a namespace of
// its own.
var routeFlushNoTableSetup = []string{"link set lo up"}

// The `route get` rows' preconditions: PRE_D0_ADDR, its v6 sibling, and
// PRE_IIF in rtnl-write-corpus.exp. The sysctl step is setup_step's in
// goip-parity.exp.
var (
	routeGetSetup    = routeWriteSetupDev
	routeGetSetupV6  = []string{"link add d0 type dummy", "link set d0 up", "-6 addr add 2001:db8::1/64 dev d0 nodad"}
	routeGetSetupIif = []string{
		"sysctl net.ipv4.conf.all.forwarding 1",
		"link add d0 type dummy", "link set d0 up", "addr add 192.0.2.1/24 dev d0",
		"link add d1 type dummy", "link set d1 up", "addr add 198.51.100.1/24 dev d1",
	}
)

// The address write rows' preconditions: PRE_ADDR_DEV and PRE_ADDR_V4 in
// addr-corpus.exp, which pin d0's MAC and drop its link-local as the
// addr-show states do.
var (
	addrWriteSetupDev = []string{"link add d0 address 02:00:00:00:d0:00 type dummy", "link set d0 addrgenmode none", "link set d0 up"}
	addrWriteSetupV4  = addrStateSetup("d0", "addr add 192.0.2.1/24 dev d0")
)

// The address flush rows' preconditions: flush_rows in addr-corpus.exp, each
// its slug's committed topology file. flush_dev alone keeps the kernel
// link-local, on purpose.
var (
	addrFlushDevSetup = []string{
		"link add d0 address 02:00:00:00:d0:00 type dummy",
		"link set d0 addrgenmode eui64",
		"link set d0 up",
		"addr add 192.0.2.1/24 dev d0",
		"addr add 198.51.100.1/24 dev d0",
		"-6 addr add 2001:db8::1/64 nodad dev d0",
	}
	addrFlushScopeLinkSetup = append(slices.Clip(addrWriteSetupDev),
		"addr add 169.254.1.1/16 scope link dev d0",
		"addr add 192.0.2.1/24 dev d0")
	addrFlushV6Setup = append(slices.Clip(addrWriteSetupDev),
		"-6 addr add 2001:db8::1/64 nodad dev d0",
		"-6 addr add 2001:db8:1::1/64 home dev d0",
		"-6 addr add 2001:db8:2::1/64 nodad mngtmpaddr noprefixroute dev d0",
		"-6 addr add 2001:db8:3::1/48 nodad dev d0")
	addrFlushStatsSetup = append(slices.Clip(addrWriteSetupDev),
		"addr add 192.0.2.1/24 dev d0",
		"addr add 198.51.100.1/24 dev d0")
)

// addrRestoreExistingSetup is restore_existing's precondition, save_all's
// topology: the two IPv4 addresses, then addrFlushV6Setup's four IPv6 ones.
var addrRestoreExistingSetup = append(slices.Clip(v4SecondarySetup), addrFlushV6Setup[len(addrWriteSetupDev):]...)

// addrFlushManyAddrsCst is flush_many's address count: 192.0.2.1 to .120,
// each a /32, enough for three datagrams.
const addrFlushManyAddrsCst = 120

// addrFlushManySetup is flush_many's precondition.
func addrFlushManySetup() []string {
	steps := slices.Clip(addrWriteSetupDev)
	for i := 1; i <= addrFlushManyAddrsCst; i++ {
		steps = append(steps, fmt.Sprintf("addr add 192.0.2.%d/32 dev d0", i))
	}
	return steps
}

// bondReadSetup is the bond the `-d link show` bond rows read: two ports
// enslaved and the bond up, as capture-link-writes.exp's lacp_no_partner
// builds it, less the step that would make it move. The veth peers lb1 and
// lb2 stay down, so la1 and la2 have no carrier and the 802.3ad machines
// have nothing to do: with carrier and no partner, a port goes from
// defaulted to churned over the next minute, and ip_a, goip and ip_b would
// each read a different moment of it.
var bondReadSetup = []string{
	"link add bond0 type bond mode 802.3ad",
	"link add la1 type veth peer name lb1",
	"link add la2 type veth peer name lb2",
	"link set la1 master bond0",
	"link set la2 master bond0",
	"link set bond0 up",
}

// The addr-show states the selector rows read, each the `ip` steps of its
// committed topology file (pkg/xtcpnl/testdata/7_1_4/link-write/addr-show/
// <state>/topology) in order.
var (
	v4SecondarySetup = []string{
		"link add d0 address 02:00:00:00:d0:00 type dummy",
		"link set d0 addrgenmode none",
		"link set d0 up",
		"addr add 192.0.2.1/24 dev d0",
		"addr add 192.0.2.2/24 dev d0",
	}
	mixedLinksSetup = []string{
		"link add d0 address 02:00:00:00:d0:00 type dummy",
		"link set d0 addrgenmode none",
		"link set d0 up",
		"addr add 192.0.2.1/24 dev d0",
		"link add d1 address 02:00:00:00:d0:01 type dummy",
		"link set d1 addrgenmode none",
		"addr add 198.51.100.1/24 dev d1",
		"link add d2 address 02:00:00:00:d0:02 type dummy",
		"link set d2 addrgenmode none",
		"link set d2 up",
		"link add bond0 address 02:00:00:00:c0:00 type bond",
		"link set bond0 addrgenmode none",
		"link add d3 address 02:00:00:00:d0:03 type dummy",
		"link set d3 addrgenmode none",
		"link set d3 master bond0",
		"link set bond0 up",
		"addr add 203.0.113.1/24 dev d3",
	}
	v4VrfSetup = []string{
		"link add vrf0 address 02:00:00:00:e0:00 type vrf table 10",
		"link set vrf0 addrgenmode none",
		"link set vrf0 up",
		"link add d0 address 02:00:00:00:d0:00 type dummy",
		"link set d0 addrgenmode none",
		"link set d0 master vrf0",
		"link set d0 up",
		"addr add 192.0.2.1/24 dev d0",
		"link add d1 address 02:00:00:00:d0:01 type dummy",
		"link set d1 addrgenmode none",
		"link set d1 up",
		"addr add 198.51.100.1/24 dev d1",
	}
	v4LabelSetup = []string{
		"link add d0 address 02:00:00:00:d0:00 type dummy",
		"link set d0 addrgenmode none",
		"link set d0 up",
		"addr add 192.0.2.1/24 label d0:1 dev d0",
		"addr add 192.0.2.9/24 label d0:9 dev d0",
	}
	v4ProtoSetup = []string{
		"link add d0 address 02:00:00:00:d0:00 type dummy",
		"link set d0 addrgenmode none",
		"link set d0 up",
		"addr add 192.0.2.1/24 proto 99 dev d0",
		"addr add 198.51.100.1/24 proto kernel_lo dev d0",
	}
	v4LifetimesSetup = addrStateSetup("d0",
		"addr add 192.0.2.1/24 valid_lft 3600 preferred_lft 1800 dev d0")
	v4HostScopeSetup = addrStateSetup("d0", "addr add 127.0.0.2/8 dev d0")
	v6PeerSetup      = addrStateSetup("d0", "-6 addr add 2001:db8::1 peer 2001:db8::2/128 nodad dev d0")
	v6TentativeSetup = []string{
		"link add v0 address 02:00:00:00:a0:00 type veth peer name v1 address 02:00:00:00:b0:00",
		"-6 addr add 2001:db8::5/64 dev v0",
	}
	v6StablePrivacySetup = []string{
		"link add d0 address 02:00:00:00:d0:00 type dummy",
		"link set d0 addrgenmode random",
		"link set d0 up",
	}
)

// addrStateSetup is an addr-show state's topology
// (scripts/addr-topology.exp) on a dummy named dev: the fixed MAC, no
// generated link-local, up, then the state's address.
func addrStateSetup(dev, addr string) []string {
	return []string{
		"link add " + dev + " address 02:00:00:00:d0:00 type dummy",
		"link set " + dev + " addrgenmode none",
		"link set " + dev + " up",
		addr,
	}
}

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

// WriteCount is the number of write messages the row sends: Writes, or one
// when it is unset.
func (c Command) WriteCount() int {
	if c.Writes == 0 {
		return 1
	}
	return c.Writes
}

// WriteName is the basename of the request goip wrote to GOIP_WRITE_OUT on a
// write row. Only the goip side has one.
func (c Command) WriteName() string {
	return c.Slug + "." + SideGoip + ".write"
}

// RCName is the basename of one side's exit status on a write row.
func (c Command) RCName(side string) string {
	return c.Slug + "." + side + ".rc"
}

// noSetupCst is String's Setup field for a row with no precondition, since an
// empty field between two tabs is easy to misread in a transcript.
const noSetupCst = "-"

// String renders one table row for `goip-parity commands`, tab-separated so a
// shell can read it with `read -r slug floor impl dev write setup args name`.
//
// setup is noSetupCst or the Setup steps joined with ';'. It is the one
// field before args that holds spaces, which a tab split does not mind.
//
// Tab-separated rather than JSON because the consumer is an expect script
// driving a serial console, where a parser is a liability and `IFS=$'\t'` is
// not. The driver splits on tabs, so the fields that hold spaces (setup,
// args, name) cannot run into each other; TestCommandString pins the count.
func (c Command) String() string {
	setup := noSetupCst
	if len(c.Setup) != 0 {
		setup = strings.Join(c.Setup, ";")
	}
	return strings.Join([]string{
		c.Slug,
		fmt.Sprint(c.Floor),
		yesNo(c.Implemented),
		yesNo(c.NeedsDev),
		yesNo(c.Write),
		setup,
		strings.Join(c.Args, " "),
		c.Name,
	}, "\t")
}

// yesNo is a String() flag field.
func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
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
