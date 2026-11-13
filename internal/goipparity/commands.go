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
	// it, and the guest's row format is fixed at ten fields.
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

	// Proto is the netlink protocol the row's captures keep, as
	// xtcp2-nlcap's --proto names it: empty for route, NETLINK_ROUTE, or
	// generic. The driver passes it to the capture and the comparator reads
	// the captures as it (nlparity.ProtocolByName).
	Proto string

	// Stdin is the line a write row's three sides read on stdin, nil for
	// none: `ip sr hmac set` reads its secret with getpass, which with no
	// terminal reads stdin, so the driver runs each side as `echo LINE |
	// setsid -w ...`. An empty line is the blank secret, the delete. Letters
	// and digits only, since the driver passes it through the shell
	// unquoted.
	Stdin *string

	// IPStandIn runs `ip` in goip's place, for a generic netlink family goip
	// does not implement yet: the row is captured and compared, `ip` against
	// `ip`, so the harness is exercised end to end before the family's G6
	// task implements it and clears this. Such a row is not Implemented,
	// and it is never gated.
	IPStandIn bool
}

// ProtoGenericCst is Proto for a generic netlink row.
const ProtoGenericCst = "generic"

// ProtoXfrmCst is Proto for a NETLINK_XFRM row.
const ProtoXfrmCst = "xfrm"

// protoRouteCst is the protocol an empty Proto means.
const protoRouteCst = "route"

// Protocol is the row's protocol name, route when Proto is empty.
func (c Command) Protocol() string {
	if c.Proto == "" {
		return protoRouteCst
	}
	return c.Proto
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
	// NX4c: `maddress show` (ip/ipmaddr.c, iproute2 7.3.0), the
	// ws-ip730/v730maddr rows. The link-layer groups come from
	// /proc/net/dev_mcast, which both tools read in the shared namespace;
	// the inet and inet6 groups from one RTM_GETMULTICAST dump each. `dev
	// nosuch` is the by-name get alone, and prints nothing.
	{
		Name: "maddress show", Slug: "maddr_show",
		Setup: maddrReadSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "-4 maddress show", Slug: "maddr_show_v4",
		Setup: maddrReadSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "-6 maddress show", Slug: "maddr_show_v6",
		Setup: maddrReadSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "-j maddress show", Slug: "maddr_show_json",
		Setup: maddrReadSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "maddress show dev d0", Slug: "maddr_show_dev",
		Setup: maddrReadSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "maddress show dev nosuch", Slug: "maddr_show_nosuch",
		Setup: maddrReadSetup, Floor: 2, Implemented: true,
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
		// Four: the nexthop dump plus the ll_link_get single-get print_rta_ifidx
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
	// NX9-T03: the lwtenc readbacks, on routes and nexthops ip installs
	// (lwtencReadSetup), the bpf ones included, since ip loads the object.
	{
		Name: "-6 route show table main", Slug: "lwtenc_route6_show",
		// The dump plus d0 by index (4): rpl, ioam6 and an rpl hop.
		Setup: lwtencReadSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "-j -6 route show table main", Slug: "lwtenc_route6_show_json",
		// The same requests; encap objects in JSON.
		Setup: lwtencReadSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "-d -6 route show table main", Slug: "lwtenc_route6_show_detail",
		// The same requests; -d's extra tokens around the encaps.
		Setup: lwtencReadSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "-4 route show table main", Slug: "lwtenc_route4_show",
		// The bpf route: three progs named `lwt_bpf.o:[sec]`, headroom never dumped.
		Setup: lwtencReadSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "-j -4 route show table main", Slug: "lwtenc_route4_show_json",
		// The same; a prog's name in JSON.
		Setup: lwtencReadSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "-6 nexthop show", Slug: "lwtenc_nexthop6_show",
		// rpl, ioam6 and bpf on nexthops 81 to 83.
		Setup: lwtencReadSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "-j -6 nexthop show", Slug: "lwtenc_nexthop6_show_json",
		// The same in JSON.
		Setup: lwtencReadSetup, Floor: 4, Implemented: true,
	},
	// NX8 (goip-parity-next.md): `nexthop show -s` and `-s -s`, the nhstats
	// readback forms. The dump and get carry NHA_OP_FLAGS (1 for -s, 3 for
	// -s -s); a group prints its `stats:` block, the hw_stats group its
	// `hw_stats on used off`, and the seg6local nexthop its counters. No row
	// sends traffic, so every counter is 0 on all three sides.
	{
		Name: "-s nexthop show", Slug: "nexthop_show_stats",
		// The dump plus d0 by index, as nexthop_show (4).
		Setup: nexthopStatsSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "-s -s nexthop show", Slug: "nexthop_show_stats2",
		Setup: nexthopStatsSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "-j -s nexthop show", Slug: "nexthop_show_stats_json",
		Setup: nexthopStatsSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "-j -s -s nexthop show", Slug: "nexthop_show_stats2_json",
		Setup: nexthopStatsSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "-s nexthop get id 10", Slug: "nexthop_get_stats",
		// A group has no dev: the get and its reply (2).
		Setup: nexthopStatsSetup, Floor: 2, Implemented: true,
	},
	{
		Name: "-s nexthop show id 12", Slug: "nexthop_show_id_stats",
		// show id is the same get as get id (2).
		Setup: nexthopStatsSetup, Floor: 2, Implemented: true,
	},
	{
		Name: "-d nexthop show id 12", Slug: "nexthop_show_id_detail",
		// -d prints hw_stats without NHA_OP_FLAGS' stats (2).
		Setup: nexthopStatsSetup, Floor: 2, Implemented: true,
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
	// The GW nexthop and rule rows (goip-write-keywords.md, "GW5-T03 the
	// nexthop and rule rows"): as the GW route rows, for GW3's hw_stats and
	// GW4's rule realms. Every encap mpls row runs sysctl, so none is here.
	{
		Name: "-4 nexthop add id 10 dev d0 hw_stats on", Slug: "gw_nh4_ng_005",
		// nh4/ng-005: hw_stats_single_on (rc 2).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "nexthop add id 1 group 21/26 type resilient buckets 2 hw_stats on", Slug: "gw_nhgrp_pw_002",
		// nhgrp/pw-002: hw_stats_group_on (rc 0).
		Floor: 2, Implemented: true, Write: true, Setup: gwWsSetupNhGrp,
	},
	{
		Name: "nexthop replace group 21/22 type mpath hw_stats on protocol static", Slug: "gw_nhgrp_ec_002",
		// nhgrp/ec-002: hw_stats_group_on (rc 2).
		Floor: 2, Implemented: true, Write: true, Setup: gwWsSetupNhGrp,
	},
	{
		Name: "nexthop add group 21/22/23 type resilient buckets 8 idle_timer 10 unbalanced_timer 20 hw_stats off protocol static", Slug: "gw_nhgrp_pw_001",
		// nhgrp/pw-001: hw_stats_group_off (rc 0).
		Floor: 2, Implemented: true, Write: true, Setup: gwWsSetupNhGrp,
	},
	{
		Name: "nexthop replace id 0 group 21/22/23 type resilient buckets 2 hw_stats off protocol static", Slug: "gw_nhgrp_pw_008",
		// nhgrp/pw-008: hw_stats_group_off (rc 2).
		Floor: 2, Implemented: true, Write: true, Setup: gwWsSetupNhGrp,
	},
	{
		Name: "-4 rule del not from 198.51.100.0/24 to 203.0.113.0/24 pref 100 fwmark 0x10 iif d0 oif d0 tos 0x10 ipproto tcp sport 80 dport 80 uidrange 1000-2000 suppress_prefixlength 0 protocol static realms 10 lookup main", Slug: "gw_rule4_ec_002",
		// rule4/ec-002: rule_realms_to (rc 0).
		Floor: 2, Implemented: true, Write: true, Setup: append(slices.Clone(gwWsSetupD0), "-4 rule add not from 198.51.100.0/24 to 203.0.113.0/24 pref 100 fwmark 0x10 iif d0 oif d0 tos 0x10 ipproto tcp sport 80 dport 80 uidrange 1000-2000 suppress_prefixlength 0 protocol static realms 10 lookup main"),
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
		Name: "nexthop show dev goip0", Slug: "nexthop_show_dev",
		// Four, the bare dump's: nh_dump_filter (ip/ipnexthop.c:70) resolves
		// the name with ll_link_get before the NHA_OIF-filtered dump, and
		// print_rta_ifidx renders `dev goip0` from the reply. The device is
		// in the argv rather than NeedsDev because the clean topology the
		// driver builds puts ids 1 and 2 on goip0 by name.
		//
		// Since iproute2 7.2.0 the name lookup is an ll_link_get by name
		// with IFLA_EXT_MASK 0x109, not an ll_init_map dump; the 7_3_0
		// corpus grounds it offline (TestCorpus730MatchesSidecarsAt720,
		// req.TestTierALLRequests730's link_get_by_name row). Gated on two
		// live runs at 7.2.0: PASS, stdout=0, control nl=0, txns 2/2 — the
		// get and the dump.
		Floor: 4, Implemented: true,
	},
	{
		Name: "nexthop show master goipvrf", Slug: "nexthop_show_master",
		// Four: the same ll_link_get for the master's name, then an
		// NHA_MASTER dump that returns id 7, on the VRF-enslaved goipv,
		// then a by-index get to print its `dev`. Gated with the dev row:
		// txns 3/3, control nl=0 on both runs.
		Floor: 4, Implemented: true,
	},
	{
		Name: "nexthop show vrf goipvrf", Slug: "nexthop_show_vrf",
		// Four, plus one transaction over `master`: name_is_vrf
		// (ip/iplink_vrf.c:170) gets the link first to confirm its kind, a
		// get that carries no NAME_ONLY, then the same ll_link_get,
		// NHA_MASTER dump and by-index get as `master`, and the same id 7.
		// Gated with the dev row: txns 4/4, control nl=0 on both runs.
		Floor: 4, Implemented: true,
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

	// The stats block — the tenth and last read-only object. These first rows
	// carry an explicit `group link` (filter_mask 0x1), from when goip grounded
	// that group alone; NX5a's rows after the xstats block select the rest. The
	// first 12-byte filter_mask-carrying request here.
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
		// short JSON form (ifindex/ifname/group/stats64); `-j -s stats show group
		// link`, below, is the extended one.
		//
		// Offline-grounded by internal/goip's jsonEquivalent row against the
		// ip_stats_json sidecar; gated with its text twin on G0-T02's two runs.
		Floor: 4, Implemented: true,
	},

	// The xstats block — `ip stats show group xstats` (filter_mask 0x2, no
	// GET_FILTERS nest). The whole group expands to four leaves, in the
	// pointer-sorted golden order bond/802.3ad, bridge/vlan, bridge/mcast,
	// bridge/stp, so every device prints four header stanzas with bodies only on
	// bridges (mesh's br0: vlan + mcast). The dev rows are the bond ones below,
	// which bring their own bond.
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
	// NX5a: every group, as ipstats.c selects and prints them (the NC4
	// `stats` corpus rows, internal/goip's TestWsStatsCorpus). A selection
	// is render-only but for the filter it packs: the mask, and an offload
	// sub-mask in an IFLA_STATS_GET_FILTERS nest when it narrows the group.
	// Every dump is the stats shape above, the RTM_GETSTATS dump then one
	// RTM_GETLINK per device; every dev row the by-name get then the point
	// get.
	{
		Name: "stats", Slug: "stats_bare",
		// do_ipstats with no verb: ipstats_show at the -s level given, 0,
		// so link's short block; filter_mask 0x1f.
		Floor: 4, Implemented: true,
	},
	{
		Name: "stats show", Slug: "stats_show_all",
		// All thirteen leaves per device, in descriptor-address order.
		Floor: 4, Implemented: true,
	},
	{
		Name: "-s stats show", Slug: "stats_show_all_stats",
		Floor: 4, Implemented: true,
	},
	{
		Name: "-j stats show", Slug: "stats_show_all_json",
		Floor: 4, Implemented: true,
	},
	{
		Name: "-j -s stats show", Slug: "stats_show_all_json_stats",
		Floor: 4, Implemented: true,
	},
	{
		Name: "-j -s stats show group link", Slug: "stats_show_json_stats",
		// print_stats64's extended JSON keys.
		Floor: 4, Implemented: true,
	},
	{
		Name: "stats show group offload", Slug: "stats_show_offload",
		// All three subgroups make the full sub-mask, so no nest.
		Floor: 4, Implemented: true,
	},
	{
		Name: "stats show group offload subgroup l3_stats", Slug: "stats_show_offload_l3",
		// l3_stats packs hw_stats_info too: the nest's {4: 0x6}.
		Floor: 4, Implemented: true,
	},
	{
		Name: "stats show group offload subgroup cpu_hit subgroup l3_stats", Slug: "stats_show_offload_two",
		// 0x1 | 0x6 is the full sub-mask, which is no nest.
		Floor: 4, Implemented: true,
	},
	{
		Name: "stats show group afstats", Slug: "stats_show_afstats",
		// AF_MPLS's counters where mpls_router has given a device one, the
		// header alone elsewhere.
		Floor: 4, Implemented: true,
	},
	{
		Name: "stats show group link group afstats", Slug: "stats_show_two_groups",
		// Two groups, printed afstats first: sorted, not in argument order.
		Floor: 4, Implemented: true,
	},
	{
		Name: "stats show group xstats_slave", Slug: "stats_show_xstats_slave",
		Floor: 4, Implemented: true,
	},
	{
		Name: "stats show dev d0", Slug: "stats_show_dev_l3",
		Setup: statsL3Setup,
		// Every leaf of one device, l3_stats on: the request flag shows on
		// the header and the hw stats64 block under it.
		Floor: 4, Implemented: true,
	},
	{
		Name: "stats show dev d0 group offload subgroup l3_stats", Slug: "stats_show_dev_l3_only",
		Setup: statsL3Setup,
		Floor: 4, Implemented: true,
	},
	{
		Name: "-j stats show dev d0 group offload", Slug: "stats_show_dev_offload_json",
		Setup: statsL3Setup,
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
	// NX5b's `link afstats` forms: with no MPLS in the matrix by here, the
	// rows hold `-j`'s empty array and the `dev` lookups, both ways.
	{
		Name: "-j link afstats", Slug: "link_afstats_json",
		// new_json_obj's `[]`, around no device.
		Setup: linkXstatsSetup, Floor: 2, Implemented: true,
	},
	{
		Name: "link afstats dev v2", Slug: "link_afstats_dev",
		// The by-name get, then the same dump, filtered by index.
		Setup: linkXstatsSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "link afstats dev nosuch", Slug: "link_afstats_nosuch",
		// The get request's ENODEV: ip's "does not exist" on stderr, no dump, and
		// exit 0, since do_iplink drops the return.
		Setup: linkXstatsSetup, Floor: 2, Implemented: true,
	},
	// NX5c's `link xstats` forms, onto the Printer: bond's and bond_slave's
	// 802.3ad nests, and bridge's filters.
	{
		Name: "link xstats type bond", Slug: "link_xstats_bond",
		// The master-group dump, then bond0 named.
		Setup: bondReadSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "link xstats type bond_slave", Slug: "link_xstats_bond_slave",
		// The port-group dump, then la1 and la2 named.
		Setup: bondReadSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "-j link xstats type bond_slave", Slug: "link_xstats_bond_slave_json",
		// One object per port, its 802.3ad object inside.
		Setup: bondReadSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "link xstats type bond 802.3ad", Slug: "link_xstats_bond_3ad",
		// The selector changes nothing on the wire.
		Setup: bondReadSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "link xstats type bond_slave dev la1", Slug: "link_xstats_bond_slave_dev",
		// The by-name get, then the dump, filtered by index.
		Setup: bondReadSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "-j link xstats type bridge", Slug: "link_xstats_bridge_json",
		// br0's multicast object.
		Setup: linkXstatsSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "link xstats type bridge_slave stp", Slug: "link_xstats_bridge_slave_stp",
		// v2's stp block alone.
		Setup: linkXstatsSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "link xstats type bridge_slave dev nosuch", Slug: "link_xstats_bridge_slave_nosuch",
		// The get request's ENODEV is nodev: exit 255, no dump.
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

	// G4-T01: `ip mroute show` (ip/ipmroute.c:232-325) on mrouteSetup, the
	// reads namespace's veths and smcrouted's one (S,G) entry. Each row is
	// ll_init_map's link dump, then the RTM_GETROUTE dump of
	// RTNL_FAMILY_IPMR, which carries RTA_TABLE 253 unless `table all`, or of
	// RTNL_FAMILY_IP6MR under -6, which carries none and finds nothing (4).
	// iif, the table and -s, -j filter or print client-side and reach no
	// request. The -s line's Age ticks; no stdout facet reads it.
	{
		Name: "mroute show", Slug: "mroute_show",
		Setup: mrouteSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "-6 mroute show", Slug: "mroute_show_v6",
		Setup: mrouteSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "mroute show iif v0", Slug: "mroute_show_iif",
		Setup: mrouteSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "mroute show table all", Slug: "mroute_show_table_all",
		Setup: mrouteSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "-s mroute show", Slug: "mroute_show_stats",
		Setup: mrouteSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "-j mroute show", Slug: "mroute_show_json",
		Setup: mrouteSetup, Floor: 4, Implemented: true,
	},

	// G4-T02: `ip mrule show`, do_multirule (ip/iprule.c:1262-1283), which
	// is `ip rule show` with preferred_family swapped for
	// RTNL_FAMILY_IPMR, or RTNL_FAMILY_IP6MR under -6. One RTM_GETRULE dump
	// and its NLMSG_DONE (2); every namespace holds ipmr's and ip6mr's
	// default rule, so no Setup. -d and -j reach no request byte.
	{
		Name: "mrule show", Slug: "mrule_show",
		Floor: 2, Implemented: true,
	},
	{
		Name: "-6 mrule show", Slug: "mrule_show_v6",
		Floor: 2, Implemented: true,
	},
	{
		Name: "-d mrule show", Slug: "mrule_show_details",
		Floor: 2, Implemented: true,
	},
	{
		Name: "-j mrule show", Slug: "mrule_show_json",
		Floor: 2, Implemented: true,
	},

	// G4-T03: `ip token` (ip/iptoken.c). Every row is ll_init_map's link
	// dump first. The reads, on tokenSetup, then dump AF_INET6 links and
	// print v0's ::1:2:3:4 and v1's ::, skipping lo (4). The writes, on a
	// veth or, refused with EINVAL, on a NOARP dummy, are one RTM_SETLINK
	// carrying IFLA_AF_SPEC { AF_INET6 { IFLA_INET6_TOKEN } } and its ack,
	// each on the C1-T02 capture of its slug; `token del`, which has none,
	// is left to the unit tests.
	{
		Name: "token show", Slug: "token_show",
		Setup: tokenSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "token get dev v0", Slug: "token_get_dev",
		Setup: tokenSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "-j token show", Slug: "token_show_json",
		Setup: tokenSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "token set ::1:2:3:4/64 dev d0", Slug: "token_set",
		Floor: 4, Implemented: true, Write: true, Setup: tokenVethSetup,
	},
	{
		Name: "token add ::1:2:3:4/64 dev d0", Slug: "token_set_noarp",
		// A dummy is IFF_NOARP: EINVAL, exit 2.
		Floor: 4, Implemented: true, Write: true, Setup: d0UpSetup,
	},

	// G4-T04: `ip netns list-id` and `ip netns set` (ip/ipnetns.c). list-id
	// is netns_map_init's RTM_GETNSID for each file under /run/netns, by a
	// descriptor of it, then the get by nsid or the dump; on netnsSetup,
	// whose peers nlcaprp1 and nlcaprp2 hold nsids 1 and 2 in the row's
	// namespace, the names, two files at least, set the floor at 4. The
	// descriptor is each process's own, so NETNSA_FD is an accepted value
	// divergence. set is one RTM_NEWNSID, FD then NSID, and its ack, on the
	// C1-T02 capture of its slug, its peer fresh for each side.
	{
		Name: "netns list-id", Slug: "netns_list_id",
		Setup: netnsSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "-j netns list-id", Slug: "netns_list_id_json",
		Setup: netnsSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "netns list-id nsid 2", Slug: "netns_list_id_nsid",
		Setup: netnsSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "netns list-id nsid 1 target-nsid 2", Slug: "netns_list_id_target",
		// nlcaprp2's nsid for nlcaprp1, unassigned, and nlcaprp1's in the
		// row's namespace, the current nsid, that names it.
		Setup: netnsSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "netns set c1set 7", Slug: "netns_set",
		Floor: 2, Implemented: true, Write: true, Setup: []string{"peer c1set"},
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
	{
		Name: "vrf show goipvrf", Slug: "vrf_show_name",
		// Two, the single-get floor: ipvrf_show's argc==1 branch (ip/ipvrf.c
		// :590-600) is ipvrf_get_table's one RTM_GETLINK get by name
		// (iplink_vrf.c:102-166) — the same 44 bytes name_is_vrf sends, no
		// ll_init_map, no IFLA_EXT_MASK — answered by the vrf link itself, and
		// both sides print `goipvrf 100`. Grounded over both corpora's
		// netlink_route_getnexthop_vrf.pcap (VS0) and VS3's
		// netlink_route_getvrf_name.pcap; the object's tests are VS2's
		// TestVrfShowName and TestVrfShowNameReplay.
		Floor: 2, Implemented: true,
	},
	{
		Name: "vrf show default", Slug: "vrf_show_default",
		// Two, the ENODEV arm: no namespace builds a vrf named default, so the
		// kernel answers the get with NLMSG_ERROR ENODEV and iplink_vrf.c
		// :136-142 resolves it to the main table — both sides print `default
		// 254`, ip without sending anything else. The errno reply is the
		// transaction's whole second half, so the floor is the get's own.
		Floor: 2, Implemented: true,
	},
	{
		Name: "vrf show goip0", Slug: "vrf_show_novrf",
		// Two, the kind-check arm: goip0 exists (the clean dummy), so the
		// kernel answers the get with its own link message, kind dummy, and
		// ipvrf_get_table's kind != "vrf" returns a zero table — the Invalid
		// VRF both sides print on stderr and exit 1. Read rows drop stderr,
		// so what the comparator holds is the shared 44-byte get and the
		// silence on stdout, the reply-driven refusal the ENODEV rows cannot
		// ground.
		Floor: 2, Implemented: true,
	},
	{
		Name: "-4 vrf show goipvrf", Slug: "vrf_show_name_v4",
		// The parked NAME get's family forms (vrf-family-forms.md): -4/-6/-0
		// changes only the request's ifi_family byte (iplink_vrf.c:110-113) —
		// the kernel resolves the name without consulting the family
		// (rtnetlink.c:4346-4355), so the answer and stdout are the plain
		// row's own, `goipvrf 100`, and the floor with it. VF0's captures
		// ground the bytes; VF1 deleted the guard.
		Floor: 2, Implemented: true,
	},
	{
		Name: "-6 vrf show goipvrf", Slug: "vrf_show_name_v6",
		// The AF_INET6 twin: the same get with the v6 family byte, the same
		// answer, the same floor.
		Floor: 2, Implemented: true,
	},
	{
		Name: "-0 vrf show goipvrf", Slug: "vrf_show_name_0",
		// The AF_PACKET corner: the family byte no other vrf request carries;
		// the kernel answers it exactly as the others.
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
	},
	// The GW link rows (goip-write-keywords.md, "GW5-T02 the link rows"):
	// as the GW route rows, for the link keyword forms GW2 built.
	{
		Name: "link set dev d0 up mtu 1400 txqueuelen 500 group 5 alias hello address 02:00:00:00:00:aa multicast on gso_max_size 32768 mode dormant name d9", Slug: "gw_linkset_ec_002",
		// linkset/ec-002: address (rc 0), gso_max_size (rc 0), mode (rc 0).
		Floor: 6, Implemented: true, Write: true, Setup: gwWsLinkD0,
	},
	{
		Name: "link set dev d0 address 01:00:00:00:00:01", Slug: "gw_linkset_ng_006",
		// linkset/ng-006: address (rc 2).
		Floor: 6, Implemented: true, Write: true, Setup: gwWsLinkD0,
	},
	{
		Name: "link set dev d0 mtu 0 txqlen 0 group 2147483647 alias aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa gso_max_segs 65535", Slug: "gw_linkset_bd_001",
		// linkset/bd-001: group_dev (rc 0), gso_max_segs (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsLinkD0,
	},
	{
		Name: "link set dev d0 mtu 65535 qlen 32767 group 2147483647 alias aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa gro_max_size 524280", Slug: "gw_linkset_bd_002",
		// linkset/bd-002: gro_max_size (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsLinkD0,
	},
	{
		Name: "link set dev d0 gro_max_size 524281", Slug: "gw_linkset_ng_009",
		// linkset/ng-009: gro_max_size (rc 2).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsLinkD0,
	},
	{
		Name: "link set dev d0 dynamic on gso_ipv4_max_size 32768", Slug: "gw_linkset_ec_007",
		// linkset/ec-007: gso_ipv4_max_size (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsLinkD0,
	},
	{
		Name: "link set dev d0 trailers off gro_ipv4_max_size 32768", Slug: "gw_linkset_ec_008",
		// linkset/ec-008: gro_ipv4_max_size (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsLinkD0,
	},
	{
		Name: "link set dev d0 master br0 addrgenmode eui64 xdpgeneric none", Slug: "gw_linkattr_pw_005",
		// linkattr/pw-005: agm_eui64 (rc 0), xdpgeneric (rc 0).
		Floor: 6, Implemented: true, Write: true, Setup: gwWsLinkBrVrf,
	},
	{
		Name: "link set dev d0 vrf v0 addrgenmode none carrier on xdpdrv off", Slug: "gw_linkattr_pw_010",
		// linkattr/pw-010: agm_none (rc 0).
		Floor: 8, Implemented: true, Write: true, Setup: gwWsLinkBrVrf,
	},
	{
		Name: "link set dev d0 addrgenmode stable_secret", Slug: "gw_linkattr_ng_004",
		// linkattr/ng-004: agm_stable_secret (rc 2).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsLinkBrVrf,
	},
	{
		Name: "link set dev d0 nomaster addrgenmode random xdpdrv off", Slug: "gw_linkattr_ec_004",
		// linkattr/ec-004: agm_random (rc 0), xdpdrv (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsLinkBrVrf,
	},
	{
		Name: "link set dev m0 protodown on protodown_reason 0 on up", Slug: "gw_linkpd_ec_002",
		// linkpd/ec-002: protodown (rc 0), protodown_reason (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsLinkMacvlan,
	},
	{
		Name: "link set dev d0 protodown on", Slug: "gw_linkattr_ng_009",
		// linkattr/ng-009: protodown (rc 2).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsLinkBrVrf,
	},
	{
		Name: "link set dev m0 protodown off protodown_reason 0 on up", Slug: "gw_linkpd_pw_008",
		// linkpd/pw-008: protodown_reason (rc 2).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsLinkMacvlan,
	},
	{
		Name: "link set dev d0 mtu 65535 qlen 32767 group 2147483647 broadcast ff:ff:ff:ff:ff:fe promisc on gro_max_size 32768 state dormant", Slug: "gw_linkset_ec_004",
		// linkset/ec-004: broadcast (rc 0), state (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsLinkD0,
	},
	{
		Name: "link set dev d0 master br0 addrgenmode random carrier off xdp off", Slug: "gw_linkattr_pw_007",
		// linkattr/pw-007: carrier (rc 0), xdp (rc 0).
		Floor: 6, Implemented: true, Write: true, Setup: gwWsLinkBrVrf,
	},
	{
		Name: "link set dev d0 link-netnsid 5", Slug: "gw_linkattr_ng_008",
		// linkattr/ng-008: link_netnsid (rc 2).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsLinkBrVrf,
	},
	{
		Name: "link set dev d0 parentdev br0", Slug: "gw_linkattr_parentdev",
		// linkattr/parentdev: parentdev (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsLinkBrVrf,
	},
	// NX2 (goip-parity-next.md): `link set … netns`, IFLA_NET_NS_FD in the
	// keyword's place, the descriptor of the namespace netns_get_fd opened.
	// Each side meets a new peer c1mv; ip moves d0 into it, goip writes the
	// request. Each row has the shape of a 7.3.1 linkns capture, its corpus
	// row offline. The pid forms are not rows: the VM has no pid to name but
	// init's, and moving d0 there would leave it in the root; O5 holds them,
	// with index on a set (linkns, and linkattr's 26 netns rows). The
	// descriptor's number is the process's next free slot, so each row has
	// an accepted-divergence entry.
	{
		Name: "link set dev d0 netns c1mv", Slug: "link_netns_name",
		// By name, under NETNS_RUN_DIR (linkns/ec-002): the lookup and the
		// write.
		Floor: 4, Implemented: true, Write: true, Setup: linkNetnsSetup,
	},
	{
		Name: "link set dev d0 netns /proc/self/ns/net", Slug: "link_netns_self",
		// By path, the sender's own namespace (linkns/ec-003): the kernel's
		// move is a no-op.
		Floor: 4, Implemented: true, Write: true, Setup: linkNetnsSetup,
	},
	{
		Name: "link add d9 netns c1mv index 100 type dummy", Slug: "link_netns_add_index",
		// On an add, index is ifi_index and there's no lookup
		// (linkns/add-index).
		Floor: 2, Implemented: true, Write: true, Setup: linkNetnsSetup,
	},
	// The inet rows (goip-link-kinds.md, KG0-T02): KC1-T01's 11 rows the
	// kernel answers, in its order, from pkg/xtcpnl/testdata/7_3_0/link-write/
	// rtnl/inet_*. Each is the d0 lookup and the write (4). The 7 rows ip
	// refuses before netlink are not rows, since a write row needs goip to
	// exit 0; TestLinkSetInetRefusals holds them offline.
	{
		Name: "link set dev d0 inet forwarding on", Slug: "link_inet_forwarding_on",
		// One on/off param: IFLA_AF_SPEC, AF_INET, IFLA_INET_CONF|NLA_F_NESTED.
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link set dev d0 inet rp_filter 2", Slug: "link_inet_rp_filter_max",
		// A numeric param at the top of inet_devconf_policy's range.
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link set dev d0 inet rp_filter 3", Slug: "link_inet_rp_filter_over",
		// One past it: refused EINVAL, compared on the request.
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link set dev d0 inet tag 0x10", Slug: "link_inet_tag_hex",
		// get_integer base 0: hex.
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link set dev d0 inet arp_ignore 010", Slug: "link_inet_arp_ignore_octal",
		// Base 0: a leading 0 is octal, so 8.
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link set dev d0 inet medium_id -1", Slug: "link_inet_medium_id_neg1",
		// The one signed param, sent as 0xffffffff.
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link set dev d0 inet medium_id -2", Slug: "link_inet_medium_id_neg2",
		// One under its minimum: refused EINVAL, compared on the request.
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link set dev d0 inet forwarding on proxy_arp on rp_filter 1 arp_ignore 2", Slug: "link_inet_several",
		// Several params in one nest, in argument order.
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link set dev d0 inet forwarding on proxy_arp on accept_redirects on secure_redirects on send_redirects on shared_media on rp_filter 1 accept_source_route on bootp_relay on log_martians on tag 7 arp_filter on medium_id 1 disable_xfrm on disable_policy on force_igmp_version 2 arp_announce 1 arp_ignore 1 promote_secondaries on arp_accept 1 arp_notify on accept_local on src_vmark on proxy_arp_pvlan on route_localnet on bc_forwarding on igmpv2_unsolicited_report_interval 1000 igmpv3_unsolicited_report_interval 1000 ignore_routes_with_linkdown on drop_unicast_in_l2_multicast on drop_gratuitous_arp on arp_evict_nocarrier on", Slug: "link_inet_every_param",
		// All 32 params, in iplink_parse_inet's order.
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link set dev d0 inet forwarding on forwarding off", Slug: "link_inet_repeated",
		// No repeat check: both attributes.
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link set dev d0 inet forwarding on mtu 1400", Slug: "link_inet_then_mtu",
		// The push-back: IFLA_AF_SPEC, then IFLA_MTU, in one request.
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	// The bridge rows (goip-link-kinds.md, KG1-T05): KC2's rows that sent a
	// request, in kind-write-corpus.exp's order, from pkg/xtcpnl/testdata/
	// 7_3_0/link-write/kinds/{bridge,bridge_slave}/. Each Floor is the row's
	// ip.pcap record count: an add is the write (2), a set the lookup and the
	// write (4). Each Setup is the row's topology file. The 10 rows ip
	// refuses before netlink are not rows, since a write row needs goip to
	// exit 0; TestLinkBridgeRefusals and TestLinkBridgeSlaveRefusals hold
	// them offline.
	{
		Name: "link add br0 type bridge", Slug: "link_bridge_add_default",
		// No options: IFLA_INFO_KIND alone.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add br0 type bridge forward_delay 400", Slug: "link_bridge_add_forward_delay",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add br0 type bridge hello_time 100", Slug: "link_bridge_add_hello_time",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add br0 type bridge max_age 1000", Slug: "link_bridge_add_max_age",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add br0 type bridge " + bridgeAgeingTimeCst + " 15000", Slug: "link_bridge_add_" + bridgeAgeingTimeCst,
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add br0 type bridge stp_state 1", Slug: "link_bridge_add_stp_state",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add br0 type bridge priority 100", Slug: "link_bridge_add_priority",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add br0 type bridge vlan_filtering 1", Slug: "link_bridge_add_vlan_filtering",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add br0 type bridge vlan_protocol 802.1ad", Slug: "link_bridge_add_vlan_protocol",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add br0 type bridge group_fwd_mask 8", Slug: "link_bridge_add_group_fwd_mask",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add br0 type bridge group_address 01:80:c2:00:00:0e", Slug: "link_bridge_add_group_address",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add br0 type bridge no_linklocal_learn 1", Slug: "link_bridge_add_no_linklocal_learn",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add br0 type bridge mst_enabled 1", Slug: "link_bridge_add_mst_enabled",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add br0 type bridge fdb_max_learned 100", Slug: "link_bridge_add_fdb_max_learned",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add br0 type bridge vlan_default_pvid 10", Slug: "link_bridge_add_vlan_default_pvid",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add br0 type bridge vlan_stats_enabled 1", Slug: "link_bridge_add_vlan_stats_enabled",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add br0 type bridge vlan_stats_per_port 1", Slug: "link_bridge_add_vlan_stats_per_port",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add br0 type bridge mcast_router 2", Slug: "link_bridge_add_mcast_router",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add br0 type bridge mcast_snooping 0", Slug: "link_bridge_add_mcast_snooping",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add br0 type bridge vlan_filtering 1 mcast_vlan_snooping 1", Slug: "link_bridge_add_mcast_vlan_snooping",
		// Needs vlan_filtering, which br_changelink applies first.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add br0 type bridge mcast_query_use_ifaddr 1", Slug: "link_bridge_add_mcast_query_use_ifaddr",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add br0 type bridge mcast_querier 1", Slug: "link_bridge_add_mcast_querier",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add br0 type bridge mcast_hash_elasticity 8", Slug: "link_bridge_add_mcast_hash_elasticity",
		// Accepted and ignored: the kernel keeps 16.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add br0 type bridge mcast_hash_max 1024", Slug: "link_bridge_add_mcast_hash_max",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add br0 type bridge mcast_last_member_count 3", Slug: "link_bridge_add_mcast_last_member_count",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add br0 type bridge mcast_startup_query_count 3", Slug: "link_bridge_add_mcast_startup_query_count",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add br0 type bridge mcast_last_member_interval 200", Slug: "link_bridge_add_mcast_last_member_interval",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add br0 type bridge mcast_membership_interval 30000", Slug: "link_bridge_add_mcast_membership_interval",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add br0 type bridge mcast_querier_interval 30000", Slug: "link_bridge_add_mcast_querier_interval",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add br0 type bridge mcast_query_interval 15000", Slug: "link_bridge_add_mcast_query_interval",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add br0 type bridge mcast_query_response_interval 500", Slug: "link_bridge_add_mcast_query_response_interval",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add br0 type bridge mcast_startup_query_interval 1000", Slug: "link_bridge_add_mcast_startup_query_interval",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add br0 type bridge mcast_stats_enabled 1", Slug: "link_bridge_add_mcast_stats_enabled",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add br0 type bridge mcast_igmp_version 3", Slug: "link_bridge_add_mcast_igmp_version",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add br0 type bridge mcast_mld_version 2", Slug: "link_bridge_add_mcast_mld_version",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add br0 type bridge nf_call_iptables 1", Slug: "link_bridge_add_nf_call_iptables",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add br0 type bridge nf_call_ip6tables 1", Slug: "link_bridge_add_nf_call_ip6tables",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add br0 type bridge nf_call_arptables 1", Slug: "link_bridge_add_nf_call_arptables",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add br0 type bridge mdb_offload_fail_notification 1", Slug: "link_bridge_add_mdb_offload_fail_notification",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add br0 type bridge fdb_local_vlan_0 1", Slug: "link_bridge_add_fdb_local_vlan_0",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add br0 type bridge fdb_flush", Slug: "link_bridge_add_fdb_flush",
		// The flag: an empty IFLA_BR_FDB_FLUSH.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add br0 type bridge stp_mode auto", Slug: "link_bridge_add_stp_mode_auto",
		// 7.2.0's stp_mode names.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add br0 type bridge stp_mode user", Slug: "link_bridge_add_stp_mode_user",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add br0 type bridge stp_mode kernel", Slug: "link_bridge_add_stp_mode_kernel",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add br0 type bridge mst_enabled 1 mdb_offload_fail_notification 1 fdb_local_vlan_0 1", Slug: "link_bridge_add_boolopts",
		// Three boolopts folded into one IFLA_BR_MULTI_BOOLOPT.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add br0 type bridge stp_state 1 priority 100 vlan_filtering 1 mcast_snooping 0", Slug: "link_bridge_add_several",
		// Several options, in the argument order.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link set br0 type bridge " + bridgeAgeingTimeCst + " 15000", Slug: "link_bridge_set_" + bridgeAgeingTimeCst,
		// The change path: the br0 lookup and the write (4).
		Floor: 4, Implemented: true, Write: true, Setup: bridgeWriteSetupBr0,
	},
	{
		Name: "link set br0 type bridge vlan_filtering 1", Slug: "link_bridge_set_vlan_filtering",
		Floor: 4, Implemented: true, Write: true, Setup: bridgeWriteSetupBr0,
	},
	{
		Name: "link set br0 type bridge stp_mode user", Slug: "link_bridge_set_stp_mode",
		Floor: 4, Implemented: true, Write: true, Setup: bridgeWriteSetupBr0,
	},
	{
		Name: "link set dev br0 type bridge stp_mode user", Slug: "link_bridge_set_stp_mode_busy",
		// Refused with STP on (an extack), compared on the request. Spelled
		// with `dev`, which sends the same request, since set_stp_mode has the
		// plain spelling and Name is the allowlist key.
		Floor: 4, Implemented: true, Write: true, Setup: bridgeWriteSetupBr0Stp,
	},
	{
		Name: "link add br0 type bridge forward_d 400", Slug: "link_bridge_add_abbrev",
		// A matches()-style keyword by prefix.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add br0 type bridge group_address 01:80:c2:00:00:01", Slug: "link_bridge_add_group_address_bad",
		// EINVAL, a reserved group address, compared on the request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add br0 type bridge group_fwd_mask 1", Slug: "link_bridge_add_group_fwd_mask_bad",
		// EINVAL (the STP bit), compared on the request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add br0 type bridge mcast_igmp_version 4", Slug: "link_bridge_add_igmp_version_bad",
		// EINVAL, compared on the request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add br0 type bridge mcast_vlan_snooping 1", Slug: "link_bridge_add_mcast_vlan_snooping_no_vlan",
		// Refused without vlan_filtering (an extack), compared on the request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link set dev p0 type bridge_slave state 4", Slug: "link_bridge_slave_set_state",
		// Needs p0 up (br_set_port_state).
		Floor: 4, Implemented: true, Write: true, Setup: bridgeWriteSetupPortsUp,
	},
	{
		Name: "link set dev p0 type bridge_slave priority 7", Slug: "link_bridge_slave_set_priority",
		Floor: 4, Implemented: true, Write: true, Setup: bridgeWriteSetupPorts,
	},
	{
		Name: "link set dev p0 type bridge_slave cost 10", Slug: "link_bridge_slave_set_cost",
		Floor: 4, Implemented: true, Write: true, Setup: bridgeWriteSetupPorts,
	},
	{
		Name: "link set dev p0 type bridge_slave hairpin on", Slug: "link_bridge_slave_set_hairpin",
		Floor: 4, Implemented: true, Write: true, Setup: bridgeWriteSetupPorts,
	},
	{
		Name: "link set dev p0 type bridge_slave guard on", Slug: "link_bridge_slave_set_guard",
		Floor: 4, Implemented: true, Write: true, Setup: bridgeWriteSetupPorts,
	},
	{
		Name: "link set dev p0 type bridge_slave root_block on", Slug: "link_bridge_slave_set_root_block",
		Floor: 4, Implemented: true, Write: true, Setup: bridgeWriteSetupPorts,
	},
	{
		Name: "link set dev p0 type bridge_slave fastleave on", Slug: "link_bridge_slave_set_fastleave",
		Floor: 4, Implemented: true, Write: true, Setup: bridgeWriteSetupPorts,
	},
	{
		Name: "link set dev p0 type bridge_slave learning off", Slug: "link_bridge_slave_set_learning",
		Floor: 4, Implemented: true, Write: true, Setup: bridgeWriteSetupPorts,
	},
	{
		Name: "link set dev p0 type bridge_slave flood off", Slug: "link_bridge_slave_set_flood",
		Floor: 4, Implemented: true, Write: true, Setup: bridgeWriteSetupPorts,
	},
	{
		Name: "link set dev p0 type bridge_slave mcast_flood off", Slug: "link_bridge_slave_set_mcast_flood",
		Floor: 4, Implemented: true, Write: true, Setup: bridgeWriteSetupPorts,
	},
	{
		Name: "link set dev p0 type bridge_slave bcast_flood off", Slug: "link_bridge_slave_set_bcast_flood",
		Floor: 4, Implemented: true, Write: true, Setup: bridgeWriteSetupPorts,
	},
	{
		Name: "link set dev p0 type bridge_slave mcast_to_unicast on", Slug: "link_bridge_slave_set_mcast_to_unicast",
		Floor: 4, Implemented: true, Write: true, Setup: bridgeWriteSetupPorts,
	},
	{
		Name: "link set dev p0 type bridge_slave proxy_arp on", Slug: "link_bridge_slave_set_proxy_arp",
		Floor: 4, Implemented: true, Write: true, Setup: bridgeWriteSetupPorts,
	},
	{
		Name: "link set dev p0 type bridge_slave proxy_arp_wifi on", Slug: "link_bridge_slave_set_proxy_arp_wifi",
		Floor: 4, Implemented: true, Write: true, Setup: bridgeWriteSetupPorts,
	},
	{
		Name: "link set dev p0 type bridge_slave mcast_router 2", Slug: "link_bridge_slave_set_mcast_router",
		Floor: 4, Implemented: true, Write: true, Setup: bridgeWriteSetupPorts,
	},
	{
		Name: "link set dev p0 type bridge_slave mcast_fast_leave on", Slug: "link_bridge_slave_set_mcast_fast_leave",
		Floor: 4, Implemented: true, Write: true, Setup: bridgeWriteSetupPorts,
	},
	{
		Name: "link set dev p0 type bridge_slave neigh_suppress on", Slug: "link_bridge_slave_set_neigh_suppress",
		Floor: 4, Implemented: true, Write: true, Setup: bridgeWriteSetupPorts,
	},
	{
		Name: "link set dev p0 type bridge_slave neigh_vlan_suppress on", Slug: "link_bridge_slave_set_neigh_vlan_suppress",
		Floor: 4, Implemented: true, Write: true, Setup: bridgeWriteSetupPorts,
	},
	{
		Name: "link set dev p0 type bridge_slave group_fwd_mask 8", Slug: "link_bridge_slave_set_group_fwd_mask",
		Floor: 4, Implemented: true, Write: true, Setup: bridgeWriteSetupPorts,
	},
	{
		Name: "link set dev p0 type bridge_slave vlan_tunnel on", Slug: "link_bridge_slave_set_vlan_tunnel",
		Floor: 4, Implemented: true, Write: true, Setup: bridgeWriteSetupPorts,
	},
	{
		Name: "link set dev p0 type bridge_slave isolated on", Slug: "link_bridge_slave_set_isolated",
		Floor: 4, Implemented: true, Write: true, Setup: bridgeWriteSetupPorts,
	},
	{
		Name: "link set dev p0 type bridge_slave locked on", Slug: "link_bridge_slave_set_locked",
		Floor: 4, Implemented: true, Write: true, Setup: bridgeWriteSetupPorts,
	},
	{
		Name: "link set dev p0 type bridge_slave locked on mab on", Slug: "link_bridge_slave_set_mab",
		// Needs locked on, so the row sets both.
		Floor: 4, Implemented: true, Write: true, Setup: bridgeWriteSetupPorts,
	},
	{
		Name: "link set dev p0 type bridge_slave backup_nhid 5", Slug: "link_bridge_slave_set_backup_nhid",
		Floor: 4, Implemented: true, Write: true, Setup: bridgeWriteSetupPorts,
	},
	{
		Name: "link set dev p0 type bridge_slave fdb_flush", Slug: "link_bridge_slave_set_fdb_flush",
		// The flag: an empty IFLA_BRPORT_FLUSH.
		Floor: 4, Implemented: true, Write: true, Setup: bridgeWriteSetupPorts,
	},
	{
		Name: "link set dev p0 type bridge_slave nobackup_port", Slug: "link_bridge_slave_set_nobackup_port",
		// IFLA_BRPORT_BACKUP_PORT 0, after a backup port is set.
		Floor: 4, Implemented: true, Write: true, Setup: bridgeWriteSetupPortsBackup,
	},
	{
		Name: "link set dev p0 type bridge_slave neigh_forward_grat on", Slug: "link_bridge_slave_set_neigh_forward_grat",
		// 7.2.0's port option.
		Floor: 4, Implemented: true, Write: true, Setup: bridgeWriteSetupPorts,
	},
	{
		Name: "link set dev p0 type bridge_slave backup_port p1", Slug: "link_bridge_slave_set_backup_port",
		// The p0 lookup, p1 resolved by name, and the write (6).
		Floor: 6, Implemented: true, Write: true, Setup: bridgeWriteSetupPorts,
	},
	{
		Name: "link set dev p0 type bridge_slave cost 10 priority 7 learning off", Slug: "link_bridge_slave_set_several",
		// Several options, in the argument order.
		Floor: 4, Implemented: true, Write: true, Setup: bridgeWriteSetupPorts,
	},
	{
		Name: "link set dev p0 type bridge_slave co 10", Slug: "link_bridge_slave_set_abbrev",
		// A matches()-style keyword by prefix.
		Floor: 4, Implemented: true, Write: true, Setup: bridgeWriteSetupPorts,
	},
	{
		Name: "link set dev p0 type bridge_slave state 5", Slug: "link_bridge_slave_set_state_bad",
		// EINVAL past BR_STATE_BLOCKING, compared on the request.
		Floor: 4, Implemented: true, Write: true, Setup: bridgeWriteSetupPorts,
	},
	{
		Name: "link set dev p0 type bridge_slave priority 64", Slug: "link_bridge_slave_set_priority_over",
		// ERANGE past 63, compared on the request.
		Floor: 4, Implemented: true, Write: true, Setup: bridgeWriteSetupPorts,
	},
	{
		Name: "link set dev p0 type bridge_slave cost 0", Slug: "link_bridge_slave_set_cost_zero",
		// ERANGE, compared on the request.
		Floor: 4, Implemented: true, Write: true, Setup: bridgeWriteSetupPorts,
	},
	{
		Name: "link set dev p0 type bridge_slave group_fwd_mask 2", Slug: "link_bridge_slave_set_group_fwd_mask_bad",
		// EINVAL, a restricted bit, compared on the request.
		Floor: 4, Implemented: true, Write: true, Setup: bridgeWriteSetupPorts,
	},
	{
		Name: "link set dev p0 type bridge_slave mab on", Slug: "link_bridge_slave_set_mab_unlocked",
		// Refused without locked (an extack), compared on the request.
		Floor: 4, Implemented: true, Write: true, Setup: bridgeWriteSetupPorts,
	},
	{
		Name: "link set dev p0 type bridge_slave backup_port p0", Slug: "link_bridge_slave_set_backup_port_self",
		// EINVAL, its own backup, compared on the request.
		Floor: 4, Implemented: true, Write: true, Setup: bridgeWriteSetupPorts,
	},
	{
		Name: "link set p0 type bridge_slave cost 10", Slug: "link_bridge_slave_set_not_enslaved",
		// EOPNOTSUPP on a port of no bridge, compared on the request. Spelled
		// without `dev`, which sends the same request, since set_cost has the
		// plain spelling and Name is the allowlist key.
		Floor: 4, Implemented: true, Write: true, Setup: bridgeWriteSetupP0,
	},
	// The VLAN family's write rows (goip-link-kinds.md, KG2-T05): KC3's
	// vlan, macvlan, macvtap, ipvlan and ipvtap rows that sent a request, in
	// kind-write-corpus.exp's order, from pkg/xtcpnl/testdata/7_3_0/
	// link-write/kinds/<kind>/. Each Floor is the row's ip.pcap record
	// count: the lower's lookup and the write (4), or the write alone for a
	// row with no `link` (2); a set is the device's lookup and the write.
	// Each Setup is the row's topology file. The 22 rows ip refuses before
	// netlink are not rows; the kinds' refusal tests hold them offline.
	{
		Name: "link add link d0 name v0 type vlan id 10", Slug: "link_vlan_add_default",
		// The d0 lookup for `link`, then the write (4).
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add link d0 name v0 type vlan id 10 protocol 802.1ad", Slug: "link_vlan_add_protocol",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add link d0 name v0 type vlan id 10 reorder_hdr off", Slug: "link_vlan_add_reorder_hdr",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add link d0 name v0 type vlan id 10 gvrp on", Slug: "link_vlan_add_gvrp",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add link d0 name v0 type vlan id 10 mvrp on", Slug: "link_vlan_add_mvrp",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add link d0 name v0 type vlan id 10 loose_binding on", Slug: "link_vlan_add_loose_binding",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add link d0 name v0 type vlan id 10 bridge_binding on", Slug: "link_vlan_add_bridge_binding",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add link d0 name v0 type vlan id 10 ingress-qos-map 1:2 3:4", Slug: "link_vlan_add_ingress_qos_map",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add link d0 name v0 type vlan id 10 egress-qos-map 2:1 4:3", Slug: "link_vlan_add_egress_qos_map",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add link d0 name v0 type vlan id 10 protocol 802.1ad gvrp on mvrp on", Slug: "link_vlan_add_several",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add link d0 name v0 type vlan id 4094", Slug: "link_vlan_add_id_max",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add link d0 name v0 type vlan i 10", Slug: "link_vlan_add_abbrev",
		// `i` is `id`: matches() takes a prefix.
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link set v0 type vlan loose_binding on", Slug: "link_vlan_set_flags",
		Floor: 4, Implemented: true, Write: true, Setup: vlanWriteSetupV0,
	},
	{
		Name: "link set v0 type vlan egress-qos-map 1:5", Slug: "link_vlan_set_egress_qos_map",
		Floor: 4, Implemented: true, Write: true, Setup: vlanWriteSetupV0,
	},
	{
		Name: "link set v0 type vlan id 20", Slug: "link_vlan_set_id",
		// vlan_changelink ignores the id: accepted, nothing changes.
		Floor: 4, Implemented: true, Write: true, Setup: vlanWriteSetupV0,
	},
	{
		Name: "link add link d0 name v0 type vlan", Slug: "link_vlan_add_no_id",
		// "VLAN properties not specified" (an extack), compared on the request.
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add name v0 type vlan id 10", Slug: "link_vlan_add_no_link",
		// "link not specified" (an extack): no lookup, the write alone (2).
		Floor: 2, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add link d0 name v0 type vlan id 4095", Slug: "link_vlan_add_id_over",
		// "Invalid VLAN id" (an extack), compared on the request.
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add link d0 name v0 type vlan id 10 protocol ip", Slug: "link_vlan_add_protocol_unsupported",
		// "Invalid VLAN protocol" (an extack), compared on the request.
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add link d0 name v1 type vlan id 10", Slug: "link_vlan_add_duplicate_id",
		// "VLAN device already exists" (an extack): v1 on v0's id.
		Floor: 4, Implemented: true, Write: true, Setup: vlanWriteSetupV0,
	},
	{
		Name: "link add link lo name v0 type vlan id 10", Slug: "link_vlan_add_on_loopback",
		// "VLANs not supported on device" (an extack): lo is the lower.
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add link d0 name m0 type macvlan", Slug: "link_macvlan_add_default",
		// No options: IFLA_INFO_KIND alone, after the d0 lookup (4).
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add link d0 name m0 type macvlan mode private", Slug: "link_macvlan_add_mode_private",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add link d0 name m0 type macvlan mode vepa", Slug: "link_macvlan_add_mode_vepa",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add link d0 name m0 type macvlan mode bridge", Slug: "link_macvlan_add_mode_bridge",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add link d0 name m0 type macvlan mode passthru", Slug: "link_macvlan_add_mode_passthru",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add link d0 name m0 type macvlan mode source", Slug: "link_macvlan_add_mode_source",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add link d0 name m0 type macvlan mode bridge flag nodst", Slug: "link_macvlan_add_flag_nodst",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add link d0 name m0 type macvlan mode bridge nodst", Slug: "link_macvlan_add_nodst",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add link d0 name m0 type macvlan mode bridge flag null", Slug: "link_macvlan_add_flag_null",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add link d0 name m0 type macvlan mode passthru nopromisc", Slug: "link_macvlan_add_passthru_nopromisc",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add link d0 name m0 type macvlan bcqueuelen 2000", Slug: "link_macvlan_add_bcqueuelen",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add link d0 name m0 type macvlan bclim 10", Slug: "link_macvlan_add_bclim",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add link d0 name m0 type macvlan bclim -1", Slug: "link_macvlan_add_bclim_negative",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add link d0 name m0 type macvlan mode source macaddr add 02:00:00:00:00:01", Slug: "link_macvlan_add_macaddr_add",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add link d0 name m0 type macvlan mode source macaddr set 02:00:00:00:00:01 02:00:00:00:00:02", Slug: "link_macvlan_add_macaddr_set",
		// The set reads both addresses; the word it stops on is pushed back.
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add link d0 name m0 type macvlan mo bridge", Slug: "link_macvlan_add_abbrev",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link set m0 type macvlan mode private", Slug: "link_macvlan_set_mode",
		Floor: 4, Implemented: true, Write: true, Setup: macvlanWriteSetupM0,
	},
	{
		Name: "link set m0 type macvlan macaddr del 02:00:00:00:00:01", Slug: "link_macvlan_set_macaddr_del",
		Floor: 4, Implemented: true, Write: true, Setup: macvlanWriteSetupM0Source,
	},
	{
		Name: "link set m0 type macvlan macaddr flush", Slug: "link_macvlan_set_macaddr_flush",
		Floor: 4, Implemented: true, Write: true, Setup: macvlanWriteSetupM0Source,
	},
	{
		Name: "link set m0 type macvlan bcqueuelen 500", Slug: "link_macvlan_set_bcqueuelen",
		Floor: 4, Implemented: true, Write: true, Setup: macvlanWriteSetupM0,
	},
	{
		Name: "link set m0 type macvlan mode passthru", Slug: "link_macvlan_set_passthru",
		// EINVAL: passthru is not set by a change, compared on the request.
		Floor: 4, Implemented: true, Write: true, Setup: macvlanWriteSetupM0,
	},
	{
		Name: "link add link d0 name m0 type macvlan mode bridge macaddr add 02:00:00:00:00:01", Slug: "link_macvlan_add_macaddr_not_source",
		// EINVAL: source addresses outside source mode, compared on the request.
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add link d0 name m0 type macvlan mode source macaddr add 01:00:5e:00:00:01", Slug: "link_macvlan_add_macaddr_multicast",
		// EADDRNOTAVAIL: a multicast source address, compared on the request.
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add link d0 name m1 type macvlan mode passthru", Slug: "link_macvlan_add_second_passthru",
		// EINVAL: one passthru per lower; m1 on m0's d0.
		Floor: 4, Implemented: true, Write: true, Setup: macvlanWriteSetupM0Passthru,
	},
	{
		Name: "link add link lo name m0 type macvlan", Slug: "link_macvlan_add_on_loopback",
		// EINVAL: lo is the lower, compared on the request.
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add link d0 name t0 type macvtap", Slug: "link_macvtap_add_default",
		// macvlan_parse_opt under its own kind name.
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add link d0 name t0 type macvtap mode bridge", Slug: "link_macvtap_add_mode_bridge",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add link d0 name t0 type macvtap mode passthru", Slug: "link_macvtap_add_mode_passthru",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add link d0 name t0 type macvtap bcqueuelen 2000", Slug: "link_macvtap_add_bcqueuelen",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link set t0 type macvtap mode private", Slug: "link_macvtap_set_mode",
		Floor: 4, Implemented: true, Write: true, Setup: macvtapWriteSetupT0,
	},
	{
		Name: "link add link d0 name i0 type ipvlan", Slug: "link_ipvlan_add_default",
		// No options: no IFLA_INFO_DATA at all.
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add link d0 name i0 type ipvlan mode l2", Slug: "link_ipvlan_add_mode_l2",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add link d0 name i0 type ipvlan mode l3", Slug: "link_ipvlan_add_mode_l3",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add link d0 name i0 type ipvlan mode l3s", Slug: "link_ipvlan_add_mode_l3s",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add link d0 name i0 type ipvlan private", Slug: "link_ipvlan_add_private",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add link d0 name i0 type ipvlan vepa", Slug: "link_ipvlan_add_vepa",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add link d0 name i0 type ipvlan bridge", Slug: "link_ipvlan_add_bridge",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add link d0 name i0 type ipvlan mode l2 vepa", Slug: "link_ipvlan_add_mode_and_flag",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add link d0 name i0 type ipvlan mo l2", Slug: "link_ipvlan_add_abbrev",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link set i0 type ipvlan mode l2", Slug: "link_ipvlan_set_mode",
		// IFLA_IPVLAN_FLAGS 0 with the mode: the set clears private.
		Floor: 4, Implemented: true, Write: true, Setup: ipvlanWriteSetupI0Private,
	},
	{
		Name: "link set i0 type ipvlan vepa", Slug: "link_ipvlan_set_flag",
		Floor: 4, Implemented: true, Write: true, Setup: ipvlanWriteSetupI0,
	},
	{
		Name: "link add name i0 type ipvlan", Slug: "link_ipvlan_add_no_link",
		// EINVAL: no lower, so no lookup, the write alone (2).
		Floor: 2, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add link lo name i0 type ipvlan", Slug: "link_ipvlan_add_on_loopback",
		// EINVAL: lo is not Ethernet, compared on the request.
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add link d0 dev i0 type ipvlan", Slug: "link_ipvlan_add_on_busy_lower",
		// EBUSY: m0 holds d0's rx handler. Spelled with `dev`, which sends the
		// same request, since add_default has the `name` spelling and Name is the
		// allowlist key.
		Floor: 4, Implemented: true, Write: true, Setup: macvlanWriteSetupM0,
	},
	{
		Name: "link add link d0 name j0 type ipvtap", Slug: "link_ipvtap_add_default",
		// ipvlan_parse_opt under its own kind name.
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add link d0 name j0 type ipvtap mode l2", Slug: "link_ipvtap_add_mode_l2",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link set j0 type ipvtap mode l2", Slug: "link_ipvtap_set_mode",
		Floor: 4, Implemented: true, Write: true, Setup: ipvtapWriteSetupJ0,
	},
	// The UDP overlays' write rows (goip-link-kinds.md, KG3-T05): KC4's
	// vxlan, geneve, bareudp, amt and gtp rows that sent a request, in
	// kind-write-corpus.exp's order, from pkg/xtcpnl/testdata/7_3_0/
	// link-write/kinds/<kind>/. Each Floor is the row's ip.pcap record
	// count: the write alone (2), a lower's or the device's lookup and the
	// write (4), or both lookups and the write (6). Each Setup is the row's
	// topology file. The 41 rows the kernel refuses compare on the request;
	// the 50 rows ip refuses before netlink are not rows, and the kinds'
	// refusal tests hold them offline.
	{
		Name: "link add vx0 type vxlan id 42", Slug: "link_vxlan_add_default",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vx0 type vxlan id 42 dstport 4789", Slug: "link_vxlan_add_dstport",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vx0 type vxlan id 42 dstport 0", Slug: "link_vxlan_add_dstport_zero",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vx0 type vxlan id 0 dstport 4789", Slug: "link_vxlan_add_id_zero",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vx0 type vxlan id 16777215 dstport 4789", Slug: "link_vxlan_add_id_max",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vx0 type vxlan v 42 dstport 4789", Slug: "link_vxlan_add_vni_abbrev",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vx0 type vxlan id 42 dstport 4789 remote 10.0.0.1", Slug: "link_vxlan_add_remote",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vx0 type vxlan id 42 dstport 4789 remote 2001:db8::1", Slug: "link_vxlan_add_remote6",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vx0 type vxlan id 42 dstport 4789 group 239.1.1.1 dev d0", Slug: "link_vxlan_add_group",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add vx0 type vxlan id 42 dstport 4789 group ff05::1 dev d0", Slug: "link_vxlan_add_group6",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add vx0 type vxlan id 42 dstport 4789 local 10.0.0.1", Slug: "link_vxlan_add_local",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vx0 type vxlan id 42 dstport 4789 local fe80::1 dev d0", Slug: "link_vxlan_add_local6_link_local",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add vx0 type vxlan id 42 dstport 4789 ttl 64", Slug: "link_vxlan_add_ttl",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vx0 type vxlan id 42 dstport 4789 ttl auto", Slug: "link_vxlan_add_ttl_auto",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vx0 type vxlan id 42 dstport 4789 ttl inherit", Slug: "link_vxlan_add_ttl_inherit",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vx0 type vxlan id 42 dstport 4789 tos 10", Slug: "link_vxlan_add_tos",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vx0 type vxlan id 42 dstport 4789 tos inherit", Slug: "link_vxlan_add_tos_inherit",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vx0 type vxlan id 42 dstport 4789 df set", Slug: "link_vxlan_add_df_set",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vx0 type vxlan id 42 dstport 4789 df unset", Slug: "link_vxlan_add_df_unset",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vx0 type vxlan id 42 dstport 4789 df inherit", Slug: "link_vxlan_add_df_inherit",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vx0 type vxlan id 42 dstport 4789 remote 2001:db8::1 flowlabel 0x12345", Slug: "link_vxlan_add_flowlabel",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vx0 type vxlan id 42 dstport 4789 " + vxlanAgeingCst + " 600", Slug: "link_vxlan_add_" + vxlanAgeingCst,
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vx0 type vxlan id 42 dstport 4789 " + vxlanAgeingCst + " none", Slug: "link_vxlan_add_" + vxlanAgeingCst + "_none",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vx0 type vxlan id 42 dstport 4789 maxaddress 100", Slug: "link_vxlan_add_maxaddress",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vx0 type vxlan id 42 dstport 4789 srcport 1000 2000", Slug: "link_vxlan_add_srcport",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vx0 type vxlan id 42 dstport 4789 proxy", Slug: "link_vxlan_add_proxy",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vx0 type vxlan id 42 dstport 4789 rsc", Slug: "link_vxlan_add_rsc",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vx0 type vxlan id 42 dstport 4789 l2miss", Slug: "link_vxlan_add_l2miss",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vx0 type vxlan id 42 dstport 4789 l3miss", Slug: "link_vxlan_add_l3miss",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vx0 type vxlan id 42 dstport 4789 noudpcsum", Slug: "link_vxlan_add_noudpcsum",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vx0 type vxlan id 42 dstport 4789 udp6zerocsumtx", Slug: "link_vxlan_add_udp6zerocsumtx",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vx0 type vxlan id 42 dstport 4789 udp6zerocsumrx", Slug: "link_vxlan_add_udp6zerocsumrx",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vx0 type vxlan id 42 dstport 4789 remcsumtx", Slug: "link_vxlan_add_remcsumtx",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vx0 type vxlan id 42 dstport 4789 remcsumrx", Slug: "link_vxlan_add_remcsumrx",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vx0 type vxlan id 42 dstport 4789 nolearning", Slug: "link_vxlan_add_nolearning",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vx0 type vxlan id 42 dstport 4789 nolocalbypass", Slug: "link_vxlan_add_nolocalbypass",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vx0 type vxlan id 42 dstport 4789 gbp", Slug: "link_vxlan_add_gbp",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vx0 type vxlan id 42 dstport 4789 mcroute", Slug: "link_vxlan_add_mcroute",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vx0 type vxlan external", Slug: "link_vxlan_add_external",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vx0 type vxlan external vnifilter dstport 4789", Slug: "link_vxlan_add_vnifilter",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vx0 type vxlan external gpe", Slug: "link_vxlan_add_gpe",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vx0 type vxlan id 42 dstport 4789 reserved_bits 0", Slug: "link_vxlan_add_reserved_bits",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vx0 type vxlan id 42 dstport 4789 no", Slug: "link_vxlan_add_no_abbrev",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link set vx0 type vxlan ttl 32 tos inherit df set " + vxlanAgeingCst + " 600 nolearning remote 10.0.0.2", Slug: "link_vxlan_set_changeable",
		Floor: 4, Implemented: true, Write: true, Setup: vxlanWriteSetupVx0,
	},
	{
		Name: "link set vx0 type vxlan dev d0", Slug: "link_vxlan_set_dev",
		Floor: 6, Implemented: true, Write: true, Setup: vxlanWriteSetupD0Vx0,
	},
	{
		Name: "link set vx0 type vxlan id 42", Slug: "link_vxlan_set_id_same",
		Floor: 4, Implemented: true, Write: true, Setup: vxlanWriteSetupVx0,
	},
	{
		Name: "link add vx0 type vxlan", Slug: "link_vxlan_add_no_option",
		// "Required attributes not provided to perform the operation", compared on
		// the request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vx1 type vxlan id 42 dstport 4789", Slug: "link_vxlan_add_duplicate_vni",
		// "A VXLAN device with the specified VNI already exists", compared on the
		// request.
		Floor: 2, Implemented: true, Write: true, Setup: vxlanWriteSetupVx0,
	},
	{
		Name: "link add vx0 type vxlan id 42 local fe80::1", Slug: "link_vxlan_add_link_local_no_dev",
		// "Local interface required for link-local local/remote addresses",
		// compared on the request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vx0 type vxlan id 42 local fe80::1 remote 2001:db8::1", Slug: "link_vxlan_add_scope_mix",
		// "Invalid combination of local and remote address scopes", compared on
		// the request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vx0 type vxlan id 42 flowlabel 5", Slug: "link_vxlan_add_flowlabel_v4",
		// "Label attribute only applies to IPv6 VXLAN devices", compared on the
		// request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vx0 type vxlan id 42 srcport 2000 1000", Slug: "link_vxlan_add_srcport_inverted",
		// "Invalid source port range", compared on the request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vx0 type vxlan id 42 gpe", Slug: "link_vxlan_add_gpe_no_external",
		// "VXLAN GPE does not support this combination of attributes", compared on
		// the request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vx0 type vxlan id 42 reserved_bits 0x0800000000000000", Slug: "link_vxlan_add_reserved_bits_overlap",
		// "Used bits 0x08000000ffffff00 cannot overlap reserved bits
		// 0x0800000000000000", compared on the request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link set vx0 type vxlan id 43", Slug: "link_vxlan_set_id",
		// "Cannot change VNI", compared on the request.
		Floor: 4, Implemented: true, Write: true, Setup: vxlanWriteSetupVx0,
	},
	{
		Name: "link set vx0 type vxlan dstport 4789", Slug: "link_vxlan_set_dstport",
		// "Cannot change port", compared on the request.
		Floor: 4, Implemented: true, Write: true, Setup: vxlanWriteSetupVx0,
	},
	{
		Name: "link set vx0 type vxlan proxy", Slug: "link_vxlan_set_proxy",
		// "vxlan: cannot change PROXY flag", compared on the request.
		Floor: 4, Implemented: true, Write: true, Setup: vxlanWriteSetupVx0,
	},
	{
		Name: "link set vx0 type vxlan remote 2001:db8::1", Slug: "link_vxlan_set_family",
		// "New group address family does not match old group", compared on the
		// request.
		Floor: 4, Implemented: true, Write: true, Setup: vxlanWriteSetupVx0,
	},
	{
		Name: "link add gn0 type geneve id 1 remote 10.0.0.1", Slug: "link_geneve_add_default",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add gn0 type geneve id 1 remote 2001:db8::1", Slug: "link_geneve_add_remote6",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add gn0 type geneve external", Slug: "link_geneve_add_external",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add gn0 type geneve id 16777215 remote 10.0.0.1", Slug: "link_geneve_add_id_max",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add gn0 type geneve i 1 r 10.0.0.1", Slug: "link_geneve_add_abbrev",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add gn0 type geneve id 1 remote 10.0.0.1 local 10.0.0.2", Slug: "link_geneve_add_local",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add gn0 type geneve id 1 remote 2001:db8::1 local 2001:db8::2", Slug: "link_geneve_add_local6",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add gn0 type geneve external local 192.168.0.1", Slug: "link_geneve_add_external_local",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add gn0 type geneve external local 2001:db8::1", Slug: "link_geneve_add_external_local6",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add gn0 type geneve id 1 remote 10.0.0.1 ttl 64", Slug: "link_geneve_add_ttl",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add gn0 type geneve id 1 remote 10.0.0.1 ttl inherit", Slug: "link_geneve_add_ttl_inherit",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add gn0 type geneve id 1 remote 10.0.0.1 tos 10", Slug: "link_geneve_add_tos",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add gn0 type geneve id 1 remote 10.0.0.1 df set", Slug: "link_geneve_add_df",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add gn0 type geneve id 1 remote 10.0.0.1 dstport 4789", Slug: "link_geneve_add_dstport",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add gn0 type geneve id 1 remote 10.0.0.1 dstport 0", Slug: "link_geneve_add_dstport_zero",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add gn0 type geneve id 1 remote 2001:db8::1 flowlabel 0xfffff", Slug: "link_geneve_add_flowlabel",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add gn0 type geneve id 1 remote 2001:db8::1 udp6zerocsumtx udp6zerocsumrx", Slug: "link_geneve_add_zero_csum6",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add gn0 type geneve id 1 remote 10.0.0.1 udpcsum udp6zerocsumtx", Slug: "link_geneve_add_csum_cancel",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add gn0 type geneve id 1 remote 10.0.0.1 innerprotoinherit", Slug: "link_geneve_add_innerprotoinherit",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link set gn0 type geneve id 1 ttl 5 tos 10 df set", Slug: "link_geneve_set_changeable",
		Floor: 4, Implemented: true, Write: true, Setup: geneveWriteSetupGn0,
	},
	{
		Name: "link set gn0 type geneve id 1 remote 10.0.0.9", Slug: "link_geneve_set_remote",
		Floor: 4, Implemented: true, Write: true, Setup: geneveWriteSetupGn0,
	},
	{
		Name: "link set gn0 type geneve id 1 local 10.0.0.2", Slug: "link_geneve_set_local_same",
		Floor: 4, Implemented: true, Write: true, Setup: geneveWriteSetupGn0Local,
	},
	{
		Name: "link add gn0 type geneve", Slug: "link_geneve_add_no_option",
		// "Not enough attributes provided to perform the operation", compared on
		// the request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add gn0 type geneve id 1 remote fe80::1", Slug: "link_geneve_add_remote6_link_local",
		// "Remote IPv6 address cannot be link-local", compared on the request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add gn0 type geneve id 1 remote 2001:db8::1 local fe80::2", Slug: "link_geneve_add_local6_link_local",
		// "Local IPv6 address cannot be link-local", compared on the request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add gn0 type geneve id 1 remote 10.0.0.1 local 2001:db8::2", Slug: "link_geneve_add_local_family_mix",
		// "Cannot specify both IPv4/IPv6 Remote/Local addresses", compared on the
		// request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add gn0 type geneve id 1 remote 10.0.0.1 label 5", Slug: "link_geneve_add_flowlabel_v4",
		// "Label attribute only applies for IPv6 Geneve devices", compared on the
		// request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add gn0 type geneve external udpcsum", Slug: "link_geneve_add_external_udpcsum",
		// "Device is externally controlled, so attributes (VNI, Port, and so on)
		// must not be specified", compared on the request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add gn1 type geneve id 1 remote 10.0.0.1", Slug: "link_geneve_add_duplicate",
		// "Device or resource busy", compared on the request.
		Floor: 2, Implemented: true, Write: true, Setup: geneveWriteSetupGn0,
	},
	{
		Name: "link add dev gn1 type geneve id 1 remote 10.0.0.1", Slug: "link_geneve_add_after_external",
		// "There already exists an externally controlled device on this
		// destination port and the source address", compared on the request.
		// Spelled with iplink_parse's optional `dev`, which sends the same
		// request, since add_duplicate has the plain spelling and Name is the
		// allowlist key.
		Floor: 2, Implemented: true, Write: true, Setup: geneveWriteSetupGn0External,
	},
	{
		Name: "link set gn0 type geneve id 2", Slug: "link_geneve_set_id",
		// "Changing VNI, Port, endpoint IP address family, external,
		// inner_proto_inherit, gro_hint and UDP checksum attributes are not
		// supported", compared on the request.
		Floor: 4, Implemented: true, Write: true, Setup: geneveWriteSetupGn0,
	},
	{
		Name: "link set gn0 type geneve id 1 dstport 6081", Slug: "link_geneve_set_dstport",
		// "Changing VNI, Port, endpoint IP address family, external,
		// inner_proto_inherit, gro_hint and UDP checksum attributes are not
		// supported", compared on the request.
		Floor: 4, Implemented: true, Write: true, Setup: geneveWriteSetupGn0,
	},
	{
		Name: "link set gn0 type geneve id 1 remote 2001:db8::1", Slug: "link_geneve_set_family",
		// "Changing VNI, Port, endpoint IP address family, external,
		// inner_proto_inherit, gro_hint and UDP checksum attributes are not
		// supported", compared on the request.
		Floor: 4, Implemented: true, Write: true, Setup: geneveWriteSetupGn0,
	},
	{
		Name: "link set dev gn0 type geneve id 1 local 10.0.0.2", Slug: "link_geneve_set_local_new",
		// "Changing VNI, Port, endpoint IP address family, external,
		// inner_proto_inherit, gro_hint and UDP checksum attributes are not
		// supported", compared on the request. Spelled with `dev` for
		// set_local_same's plain spelling, as add_after_external is.
		Floor: 4, Implemented: true, Write: true, Setup: geneveWriteSetupGn0,
	},
	{
		Name: "link set gn0 type geneve id 0", Slug: "link_geneve_set_on_external",
		// "Operation not supported", compared on the request.
		Floor: 4, Implemented: true, Write: true, Setup: geneveWriteSetupGn0External,
	},
	{
		Name: "link add b0 type bareudp dstport 6635 ethertype mpls_uc", Slug: "link_bareudp_add_default",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add b0 type bareudp dstport 6635 ethertype ipv4", Slug: "link_bareudp_add_ethertype_ipv4",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add b0 type bareudp dstport 6635 ethertype 0x1234", Slug: "link_bareudp_add_ethertype_number",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add b0 type bareudp dstport 6635 ethertype ip srcportmin 1000", Slug: "link_bareudp_add_srcportmin",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add b0 type bareudp dstport 6635 ethertype mpls_uc multiproto", Slug: "link_bareudp_add_multiproto",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add b0 type bareudp d 6635 e ip s 1 m", Slug: "link_bareudp_add_abbrev",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add b0 type bareudp", Slug: "link_bareudp_add_no_option",
		// "Not enough attributes provided to perform the operation", compared on
		// the request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add b0 type bareudp dstport 6635 ethertype ipv6 multiproto", Slug: "link_bareudp_add_multiproto_ipv6",
		// "Cannot set multiproto mode for this ethertype (only IPv4 and unicast
		// MPLS are supported)", compared on the request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add b1 type bareudp dstport 6635 ethertype mpls_uc", Slug: "link_bareudp_add_port_busy",
		// "Another bareudp device using the same port already exists", compared on
		// the request.
		Floor: 2, Implemented: true, Write: true, Setup: bareudpWriteSetupB0,
	},
	{
		Name: "link set b0 type bareudp dstport 6635 ethertype ip", Slug: "link_bareudp_set_same",
		// "Operation not supported", compared on the request.
		Floor: 4, Implemented: true, Write: true, Setup: bareudpWriteSetupB0,
	},
	{
		Name: "link add a0 type amt mode gateway dev d0 local 10.0.0.1 discovery 10.0.0.2", Slug: "link_amt_add_gateway",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add a0 type amt mode relay dev d0 local 10.0.0.1", Slug: "link_amt_add_relay",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add a0 type amt mode gateway dev d0 local 10.0.0.1 discovery 10.0.0.2 relay_port 3000 gateway_port 3001 max_tunnels 10", Slug: "link_amt_add_ports",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add a0 type amt mode relay dev d0 local 10.0.0.1 max_tunnels 0", Slug: "link_amt_add_max_tunnels_zero",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add a0 type amt mode gateway mode relay dev d0 local 10.0.0.1", Slug: "link_amt_add_mode_twice",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add a0 type amt", Slug: "link_amt_add_no_option",
		// "Invalid argument", compared on the request.
		Floor: 2, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add a0 type amt mode relay local 10.0.0.1", Slug: "link_amt_add_no_link",
		// "Link attribute is required", compared on the request.
		Floor: 2, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add a0 type amt mode gateway dev d0 local 10.0.0.1", Slug: "link_amt_add_gateway_no_discovery",
		// "Discovery attribute is required", compared on the request.
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add a0 type amt mode relay dev lo local 10.0.0.1", Slug: "link_amt_add_on_loopback",
		// "Invalid stream device type", compared on the request.
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add a0 type amt mode relay dev d0 local 127.0.0.1", Slug: "link_amt_add_local_loopback",
		// "Invalid Local address", compared on the request.
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add a0 type amt mode relay dev d0 local 10.0.0.1 relay_port 0", Slug: "link_amt_add_relay_port_zero",
		// "relay port must not be 0", compared on the request.
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add a0 type amt mode gateway dev d0 local 10.0.0.1 discovery 224.0.0.1", Slug: "link_amt_add_discovery_multicast",
		// "discovery must be unicast", compared on the request.
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link set a0 type amt mode relay dev d0 local 10.0.0.1", Slug: "link_amt_set_same",
		// "Operation not supported", compared on the request.
		Floor: 6, Implemented: true, Write: true, Setup: amtWriteSetupA0,
	},
	{
		Name: "link add g0 type gtp role ggsn", Slug: "link_gtp_add_ggsn",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add g0 type gtp role sgsn", Slug: "link_gtp_add_sgsn",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add g0 type gtp role ggsn hsize 4096", Slug: "link_gtp_add_hsize",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add g0 type gtp role ggsn hsize 131071", Slug: "link_gtp_add_hsize_max",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add g0 type gtp role ggsn hsize 0", Slug: "link_gtp_add_hsize_zero",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add g0 type gtp role ggsn restart_count 255", Slug: "link_gtp_add_restart_count",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add g0 type gtp role ggsn foo", Slug: "link_gtp_add_unknown_skipped",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add g0 type gtp", Slug: "link_gtp_add_no_option",
		// "Invalid argument", compared on the request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add g1 type gtp role sgsn", Slug: "link_gtp_add_second",
		// "Address already in use", compared on the request.
		Floor: 2, Implemented: true, Write: true, Setup: gtpWriteSetupG0,
	},
	{
		Name: "link set g0 type gtp role ggsn", Slug: "link_gtp_set_same",
		// "Operation not supported", compared on the request.
		Floor: 4, Implemented: true, Write: true, Setup: gtpWriteSetupG0,
	},
	{
		Name: "link set g0 type gtp", Slug: "link_gtp_set_no_option",
		// "Invalid argument", compared on the request.
		Floor: 4, Implemented: true, Write: true, Setup: gtpWriteSetupG0,
	},
	// The IP tunnels' write rows (goip-link-kinds.md, KG4-T09): KC5's gre,
	// gretap, erspan, ip6gre, ip6gretap, ip6erspan, ipip, sit, ip6tnl, vti,
	// vti6 and xfrm rows that sent a request, in kind-write-corpus.exp's
	// order, from pkg/xtcpnl/testdata/7_3_0/link-write/kinds/<kind>/. Each
	// Floor is the row's ip.pcap record count, a set's counting the
	// prefetch, and each Setup is the row's topology file. The 66 rows the
	// kernel refuses compare on the request; the 88 rows ip refuses before
	// netlink are not rows, and the kinds' refusal tests hold them offline.
	{
		Name: "link add gr0 type gre remote 10.0.0.1", Slug: "link_gre_add_default",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add gr0 type gre", Slug: "link_gre_add_no_option",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add gr0 type gre remote any local 10.0.0.2", Slug: "link_gre_add_local",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add gr0 type gre remote 10.0.0.1 dev d0", Slug: "link_gre_add_dev",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add gr0 type gre remote 10.0.0.1 key 42", Slug: "link_gre_add_key",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add gr0 type gre remote 10.0.0.1 ikey 1.2.3.4 okey 5", Slug: "link_gre_add_key_split",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add gr0 type gre remote 10.0.0.1 seq csum noocsum", Slug: "link_gre_add_seq_csum",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add gr0 type gre remote 10.0.0.1 hoplimit 255", Slug: "link_gre_add_ttl_max",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add gr0 type gre remote 10.0.0.1 tclass ff", Slug: "link_gre_add_tos_max",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add gr0 type gre remote 10.0.0.1 tos inherit", Slug: "link_gre_add_tos_inherit",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add gr0 type gre remote 10.0.0.1 nopmtudisc ignore-df", Slug: "link_gre_add_nopmtudisc_ignore_df",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add gr0 type gre remote 10.0.0.1 fwmark 0x10", Slug: "link_gre_add_fwmark",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add gr0 type gre remote 10.0.0.1 encap none", Slug: "link_gre_add_encap_none",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add gr0 type gre remote 10.0.0.1 encap fou encap-sport auto encap-dport 5555 encap-csum noencap-csum encap-udp6-csum encap-remcsum", Slug: "link_gre_add_encap_fou",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add gr0 type gre external", Slug: "link_gre_add_external",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add gr0 type gre external encap fou", Slug: "link_gre_add_external_encap",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add gr0 type gre remote 239.1.1.1 local 10.0.0.1", Slug: "link_gre_add_multicast",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add gr0 type gre r 10.0.0.1 l 10.0.0.2 t 64 k 5 s c", Slug: "link_gre_add_abbrev",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add gr0 type gre remote 10.0.0.1 key 5 no", Slug: "link_gre_add_no_abbrev",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add gr1 type gre remote 10.0.0.1 key 5", Slug: "link_gre_add_second_keyed",
		Floor: 2, Implemented: true, Write: true, Setup: greWriteSetupGr0,
	},
	{
		Name: "link set gr0 type gre ttl 32 tos 10 key 5 seq csum fwmark 7 local 10.0.0.2 remote 10.0.0.2", Slug: "link_gre_set_changeable",
		Floor: 6, Implemented: true, Write: true, Setup: greWriteSetupGr0,
	},
	{
		Name: "link set gr0 type gre dev d0", Slug: "link_gre_set_dev",
		Floor: 8, Implemented: true, Write: true, Setup: greWriteSetupD0Gr0,
	},
	{
		Name: "link set gr0 type gre", Slug: "link_gre_set_no_option",
		Floor: 4, Implemented: true, Write: true, Setup: greWriteSetupGr0,
	},
	{
		Name: "link set gr0 type gre ttl 64", Slug: "link_gre_set_on_external",
		Floor: 6, Implemented: true, Write: true, Setup: greWriteSetupGr0External,
	},
	{
		Name: "link add gr0 type gre remote 10.0.0.1 ignore-df", Slug: "link_gre_add_ignore_df",
		// "Invalid argument", compared on the request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add gr1 type gre remote 10.0.0.1", Slug: "link_gre_add_duplicate",
		// "File exists", compared on the request.
		Floor: 2, Implemented: true, Write: true, Setup: greWriteSetupGr0,
	},
	{
		Name: "link add gr1 type gre external", Slug: "link_gre_add_second_external",
		// "File exists", compared on the request.
		Floor: 2, Implemented: true, Write: true, Setup: greWriteSetupGr0External,
	},
	{
		Name: "link set gr0 type gre remote any", Slug: "link_gre_set_remote_any",
		// "Invalid argument", compared on the request.
		Floor: 6, Implemented: true, Write: true, Setup: greWriteSetupGr0,
	},
	{
		Name: "link set gr1 type gre remote 10.0.0.1", Slug: "link_gre_set_collide",
		// "File exists", compared on the request.
		Floor: 6, Implemented: true, Write: true, Setup: greWriteSetupGr0Gr1,
	},
	{
		Name: "link set gr0 type gre external", Slug: "link_gre_set_external",
		// "Enabling collect_md on an existing device is not supported", compared
		// on the request.
		Floor: 6, Implemented: true, Write: true, Setup: greWriteSetupGr0,
	},
	{
		Name: "link set gr0 type gre pmtudisc noignore-df", Slug: "link_gre_set_pmtudisc_ignore_df",
		// "Invalid argument", compared on the request.
		Floor: 6, Implemented: true, Write: true, Setup: greWriteSetupGr0NoPMTUDisc,
	},
	{
		Name: "link add gt0 type gretap remote 10.0.0.1", Slug: "link_gretap_add_default",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add gt0 type gretap", Slug: "link_gretap_add_no_option",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add gt0 type gretap external", Slug: "link_gretap_add_external",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add gt0 type gretap remote 239.1.1.1 local 10.0.0.1", Slug: "link_gretap_add_multicast",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add gt0 address 02:00:00:00:00:01 type gretap remote 10.0.0.1", Slug: "link_gretap_add_address",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link set gt0 type gretap remote any", Slug: "link_gretap_set_remote_any",
		Floor: 6, Implemented: true, Write: true, Setup: gretapWriteSetupGt0,
	},
	{
		Name: "link add gt0 address 01:00:5e:00:00:01 type gretap remote 10.0.0.1", Slug: "link_gretap_add_address_multicast",
		// "Cannot assign requested address", compared on the request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add gt1 type gretap external", Slug: "link_gretap_add_second_external",
		// "File exists", compared on the request.
		Floor: 2, Implemented: true, Write: true, Setup: gretapWriteSetupGt0External,
	},
	{
		Name: "link set gt0 type gretap external", Slug: "link_gretap_set_external",
		// "Enabling collect_md on an existing device is not supported", compared
		// on the request.
		Floor: 6, Implemented: true, Write: true, Setup: gretapWriteSetupGt0,
	},
	{
		Name: "link set gr0 type gretap ttl 5", Slug: "link_gretap_set_on_gre",
		// "Operation not supported", compared on the request.
		Floor: 6, Implemented: true, Write: true, Setup: greWriteSetupGr0,
	},
	{
		Name: "link add es0 type erspan remote 10.0.0.1 seq key 10", Slug: "link_erspan_add_default",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add es0 type erspan", Slug: "link_erspan_add_no_option",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add es0 type erspan remote 10.0.0.1 seq key 10 erspan 123", Slug: "link_erspan_add_index",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add es0 type erspan remote 10.0.0.1 seq key 10 erspan 1048575", Slug: "link_erspan_add_index_max",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add es0 type erspan remote 10.0.0.1 iseq okey 10", Slug: "link_erspan_add_flags_split",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add es0 type erspan remote 10.0.0.1 seq key 1023", Slug: "link_erspan_add_key_max",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add es0 type erspan remote 10.0.0.1 erspan_ver 0 csum", Slug: "link_erspan_add_ver0",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add es0 type erspan remote 10.0.0.1 seq key 10 erspan_ver 2 erspan_dir egress erspan_hwid 7", Slug: "link_erspan_add_ver2",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add es0 type erspan remote 10.0.0.1 seq key 10 erspan_ver 2 erspan_hwid 0x3f", Slug: "link_erspan_add_hwid_max",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add es0 type erspan remote 10.0.0.1 seq key 10 erspan_ver 2 erspan_dir e", Slug: "link_erspan_add_dir_abbrev",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add es0 type erspan remote 10.0.0.1 seq key 10 erspan_dir egress erspan_hwid 7", Slug: "link_erspan_add_dir_ver1",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add es0 type erspan external", Slug: "link_erspan_add_external",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link set es0 type erspan erspan 200", Slug: "link_erspan_set_index",
		Floor: 6, Implemented: true, Write: true, Setup: erspanWriteSetupEs0,
	},
	{
		Name: "link set es0 type erspan erspan_ver 0 noseq", Slug: "link_erspan_set_ver0_noseq",
		Floor: 6, Implemented: true, Write: true, Setup: erspanWriteSetupEs0,
	},
	{
		Name: "link add es0 type erspan remote 10.0.0.1", Slug: "link_erspan_add_remote_only",
		// "Invalid argument", compared on the request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add es0 type erspan remote 10.0.0.1 seq key 10 csum", Slug: "link_erspan_add_csum",
		// "Invalid argument", compared on the request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add es0 type erspan remote 10.0.0.1 seq key 1024", Slug: "link_erspan_add_key_over",
		// "Invalid argument", compared on the request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add es0 type erspan remote 10.0.0.1 seq key 10 erspan_ver 2 erspan_hwid 0x40", Slug: "link_erspan_add_hwid_over",
		// "Invalid argument", compared on the request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link set es0 type erspan noseq", Slug: "link_erspan_set_noseq",
		// "Invalid argument", compared on the request.
		Floor: 6, Implemented: true, Write: true, Setup: erspanWriteSetupEs0,
	},
	{
		Name: "link add g6 type ip6gre remote 2001:db8::1", Slug: "link_ip6gre_add_default",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add g6 type ip6gre", Slug: "link_ip6gre_add_no_option",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add g6 type ip6gre remote 2001:db8::1 dev d0", Slug: "link_ip6gre_add_dev",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add g6 type ip6gre remote 2001:db8::1 ttl inherit", Slug: "link_ip6gre_add_ttl_inherit",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add g6 type ip6gre remote 2001:db8::1 encaplimit none", Slug: "link_ip6gre_add_encaplimit_none",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add g6 type ip6gre remote 2001:db8::1 hoplimit 255 tclass ff flowlabel 0xfffff", Slug: "link_ip6gre_add_tops",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add g6 type ip6gre remote 2001:db8::1 tclass inherit flowlabel inherit fwmark inherit dscp inherit allow-localremote", Slug: "link_ip6gre_add_flags",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add g6 type ip6gre remote 2001:db8::1 fwmark 0x10", Slug: "link_ip6gre_add_fwmark",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add g6 type ip6gre remote 2001:db8::1 key 42 seq csum", Slug: "link_ip6gre_add_key",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add g6 type ip6gre remote 2001:db8::1 ikey 1.2.3.4 okey 5 iseq ocsum", Slug: "link_ip6gre_add_ikey_okey",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add g6 type ip6gre remote 2001:db8::1 key 42 seq csum nokey noseq nocsum", Slug: "link_ip6gre_add_no_forms",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add g6 type ip6gre r 2001:db8::1 l 2001:db8::2 h 10 tc 20 k 5", Slug: "link_ip6gre_add_abbrev",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add g6 type ip6gre remote 2001:db8::1 hoplimit 10 hoplimit 20", Slug: "link_ip6gre_add_repeat",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add g6 type ip6gre remote 2001:db8::1 encap none encap-sport 5555 encap-dport 6666 encap-csum encap-remcsum noencap-udp6-csum", Slug: "link_ip6gre_add_encap_none",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add g6 type ip6gre external", Slug: "link_ip6gre_add_external",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add g6 type ip6gre remote 2001:db8::1 erspan_ver 0", Slug: "link_ip6gre_add_erspan_ver_zero",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add g7 type ip6gre remote 2001:db8::1 key 1", Slug: "link_ip6gre_add_second_key",
		Floor: 2, Implemented: true, Write: true, Setup: ip6greWriteSetupG6,
	},
	{
		Name: "link set g6 type ip6gre hoplimit 10 tclass 20 flowlabel 0x1 encaplimit 2 key 5 local 2001:db8::2", Slug: "link_ip6gre_set_changeable",
		Floor: 6, Implemented: true, Write: true, Setup: ip6greWriteSetupG6,
	},
	{
		Name: "link set g6 type ip6gre", Slug: "link_ip6gre_set_no_option",
		Floor: 4, Implemented: true, Write: true, Setup: ip6greWriteSetupG6,
	},
	{
		Name: "link set g6 type ip6gre external", Slug: "link_ip6gre_set_external_ignored",
		Floor: 6, Implemented: true, Write: true, Setup: ip6greWriteSetupG6,
	},
	{
		Name: "link set g6 type ip6gre hoplimit 10", Slug: "link_ip6gre_set_encap_sport_swap",
		Floor: 6, Implemented: true, Write: true, Setup: ip6greWriteSetupG6EncapSport,
	},
	{
		Name: "link add g7 type ip6gre remote 2001:db8::1", Slug: "link_ip6gre_add_duplicate",
		// "File exists", compared on the request.
		Floor: 2, Implemented: true, Write: true, Setup: ip6greWriteSetupG6,
	},
	{
		Name: "link add g7 type ip6gre external", Slug: "link_ip6gre_add_external_twice",
		// "File exists", compared on the request.
		Floor: 2, Implemented: true, Write: true, Setup: ip6greWriteSetupG6External,
	},
	{
		Name: "link set g7 type ip6gre remote 2001:db8::1", Slug: "link_ip6gre_set_collide",
		// "File exists", compared on the request.
		Floor: 6, Implemented: true, Write: true, Setup: ip6greWriteSetupG6G7,
	},
	{
		Name: "link add g6 type ip6gre remote 2001:db8::1 encap fou encap-dport 5555", Slug: "link_ip6gre_add_encap_fou",
		// "Invalid argument", compared on the request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add g6 type ip6gre remote 2001:db8::1 encap gue encap-dport 5555", Slug: "link_ip6gre_add_encap_gue",
		// "Invalid argument", compared on the request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add gt6 type ip6gretap remote 2001:db8::1", Slug: "link_ip6gretap_add_default",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add gt6 type ip6gretap", Slug: "link_ip6gretap_add_no_option",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add gt6 type ip6gretap remote ::", Slug: "link_ip6gretap_add_remote_any",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add dev gt6 type ip6gretap remote 2001:db8::1", Slug: "link_ip6gretap_add_beside_ip6gre",
		// Spelled with iplink_parse's optional `dev`, which sends the same
		// request, since add_default has the plain spelling and Name is the
		// allowlist key.
		Floor: 2, Implemented: true, Write: true, Setup: ip6greWriteSetupG6,
	},
	{
		Name: "link set gt6 type ip6gretap hoplimit 10 key 5", Slug: "link_ip6gretap_set_changeable",
		Floor: 6, Implemented: true, Write: true, Setup: ip6gretapWriteSetupGt6,
	},
	{
		Name: "link set gt7 type ip6gretap remote 2001:db8::1", Slug: "link_ip6gretap_set_collide_unchecked",
		Floor: 6, Implemented: true, Write: true, Setup: ip6gretapWriteSetupGt6Gt7,
	},
	{
		Name: "link add gt7 type ip6gretap remote 2001:db8::1", Slug: "link_ip6gretap_add_duplicate",
		// "File exists", compared on the request.
		Floor: 2, Implemented: true, Write: true, Setup: ip6gretapWriteSetupGt6,
	},
	{
		Name: "link add gt6 type ip6gretap external", Slug: "link_ip6gretap_add_external_beside_ip6gre",
		// "File exists", compared on the request.
		Floor: 2, Implemented: true, Write: true, Setup: ip6greWriteSetupG6External,
	},
	{
		Name: "link set gt6 type ip6gre hoplimit 5", Slug: "link_ip6gretap_set_kind_mismatch",
		// "Operation not supported", compared on the request.
		Floor: 6, Implemented: true, Write: true, Setup: ip6gretapWriteSetupGt6,
	},
	{
		Name: "link add es6 type ip6erspan remote 2001:db8::1 key 10 seq", Slug: "link_ip6erspan_add_default",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add es6 type ip6erspan", Slug: "link_ip6erspan_add_no_option",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add es6 type ip6erspan external", Slug: "link_ip6erspan_add_external",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add es6 type ip6erspan remote 2001:db8::1 key 10 seq erspan 1048575", Slug: "link_ip6erspan_add_index_max",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add es6 type ip6erspan remote 2001:db8::1 key 10 seq erspan_ver 2 erspan_dir e erspan_hwid 63", Slug: "link_ip6erspan_add_v2",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add es6 type ip6erspan remote 2001:db8::1 key 10 seq erspan_hwid 64", Slug: "link_ip6erspan_add_hwid_v1",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add es6 type ip6erspan remote 2001:db8::1 key 1023 seq", Slug: "link_ip6erspan_add_key_max",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add es6 type ip6erspan remote 2001:db8::1 ikey 10 oseq", Slug: "link_ip6erspan_add_split_flags",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link set es6 type ip6erspan erspan_ver 2 erspan_dir egress erspan_hwid 63", Slug: "link_ip6erspan_set_v2",
		Floor: 6, Implemented: true, Write: true, Setup: ip6erspanWriteSetupEs6,
	},
	{
		Name: "link add es6 type ip6erspan remote 2001:db8::1 key 10", Slug: "link_ip6erspan_add_no_seq",
		// "Invalid argument", compared on the request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add es6 type ip6erspan remote 2001:db8::1 key 10 seq csum", Slug: "link_ip6erspan_add_csum",
		// "Invalid argument", compared on the request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add es6 type ip6erspan remote 2001:db8::1 key 1024 seq", Slug: "link_ip6erspan_add_key_over",
		// "Invalid argument", compared on the request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add es6 type ip6erspan remote 2001:db8::1 key 10 seq erspan_ver 0", Slug: "link_ip6erspan_add_ver_zero",
		// "Invalid argument", compared on the request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add es6 type ip6erspan remote 2001:db8::1 key 10 seq erspan_ver 2 erspan_hwid 64", Slug: "link_ip6erspan_add_hwid_over",
		// "Invalid argument", compared on the request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link set es6 type ip6erspan noseq", Slug: "link_ip6erspan_set_noseq",
		// "Invalid argument", compared on the request.
		Floor: 6, Implemented: true, Write: true, Setup: ip6erspanWriteSetupEs6,
	},
	{
		Name: "link add ip0 type ipip remote 10.0.0.1", Slug: "link_ipip_add_default",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add ip0 type ipip", Slug: "link_ipip_add_no_option",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add ip0 type ipip remote 10.0.0.1 mode ipip", Slug: "link_ipip_add_mode_ipip",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add ip0 type ipip remote 10.0.0.1 mode mplsip", Slug: "link_ipip_add_mode_mplsip",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add ip0 type ipip remote 10.0.0.1 mode any", Slug: "link_ipip_add_mode_any",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add ip0 type ipip remote 10.0.0.1 mode ip4ip4", Slug: "link_ipip_add_mode_alias",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add ip0 type ipip remote 10.0.0.1 local 10.0.0.2", Slug: "link_ipip_add_local",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add ip0 type ipip remote 10.0.0.1 dev d0", Slug: "link_ipip_add_dev",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add ip0 type ipip remote 10.0.0.1 ttl 255", Slug: "link_ipip_add_ttl",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add ip0 type ipip remote 10.0.0.1 ttl inherit", Slug: "link_ipip_add_ttl_inherit",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add ip0 type ipip remote 10.0.0.1 tos 10", Slug: "link_ipip_add_tos",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add ip0 type ipip remote 10.0.0.1 tos inherit", Slug: "link_ipip_add_tos_inherit",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add ip0 type ipip remote 10.0.0.1 nopmtudisc", Slug: "link_ipip_add_nopmtudisc",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add ip0 type ipip remote 10.0.0.1 fwmark 0xffffffff", Slug: "link_ipip_add_fwmark",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add ip0 type ipip external", Slug: "link_ipip_add_external",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add ip0 type ipip remote 10.0.0.1 encap fou encap-sport auto encap-dport 5555", Slug: "link_ipip_add_encap_fou",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add ip0 type ipip remote 10.0.0.1 encap gue encap-sport 1000 encap-dport 6000 encap-csum encap-udp6-csum encap-remcsum", Slug: "link_ipip_add_encap_gue",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add ip0 type ipip remote 10.0.0.1 encap fou encap-csum noencap noencap-csum", Slug: "link_ipip_add_noencap",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add ip0 type ipip remote 10.0.0.1 6rd-prefix 2001:db8::/32", Slug: "link_ipip_add_6rd_ignored",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add ip0 type ipip remote 239.1.1.1", Slug: "link_ipip_add_remote_multicast",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link set ip0 type ipip hlim 64 dsf 10 fwmark 5 local 10.0.0.2", Slug: "link_ipip_set_changeable",
		Floor: 6, Implemented: true, Write: true, Setup: ipipWriteSetupIp0,
	},
	{
		Name: "link set ip0 type ipip d d0", Slug: "link_ipip_set_dev",
		Floor: 8, Implemented: true, Write: true, Setup: ipipWriteSetupD0Ip0,
	},
	{
		Name: "link set ip0 type ipip mode mplsip", Slug: "link_ipip_set_mode",
		Floor: 6, Implemented: true, Write: true, Setup: ipipWriteSetupIp0,
	},
	{
		Name: "link set ip0 type ipip encap fou encap-dport 5555", Slug: "link_ipip_set_encap",
		Floor: 6, Implemented: true, Write: true, Setup: ipipWriteSetupIp0,
	},
	{
		Name: "link set ip0 type ipip nopmtudisc", Slug: "link_ipip_set_nopmtudisc",
		Floor: 6, Implemented: true, Write: true, Setup: ipipWriteSetupIp0,
	},
	{
		Name: "link set ip0 type ipip", Slug: "link_ipip_set_no_option",
		Floor: 4, Implemented: true, Write: true, Setup: ipipWriteSetupIp0,
	},
	{
		Name: "link add ip1 type ipip remote 10.0.0.1", Slug: "link_ipip_add_duplicate",
		// "File exists", compared on the request.
		Floor: 2, Implemented: true, Write: true, Setup: ipipWriteSetupIp0,
	},
	{
		Name: "link add ip1 type ipip external", Slug: "link_ipip_add_external_second",
		// "File exists", compared on the request.
		Floor: 2, Implemented: true, Write: true, Setup: ipipWriteSetupIp0External,
	},
	{
		Name: "link set ip0 type ipip remote any", Slug: "link_ipip_set_remote_any",
		// "Invalid argument", compared on the request.
		Floor: 6, Implemented: true, Write: true, Setup: ipipWriteSetupIp0,
	},
	{
		Name: "link set ip0 type ipip remote 10.0.0.1", Slug: "link_ipip_set_remote_on_wildcard",
		// "Invalid argument", compared on the request.
		Floor: 6, Implemented: true, Write: true, Setup: ipipWriteSetupIp0Bare,
	},
	{
		Name: "link set ip0 type ipip remote 239.1.1.1", Slug: "link_ipip_set_remote_multicast",
		// "Invalid argument", compared on the request.
		Floor: 6, Implemented: true, Write: true, Setup: ipipWriteSetupIp0,
	},
	{
		Name: "link set ip0 type ipip remote 10.0.0.2", Slug: "link_ipip_set_collide",
		// "File exists", compared on the request.
		Floor: 6, Implemented: true, Write: true, Setup: ipipWriteSetupIp0Ip1,
	},
	{
		Name: "link set ip0 type ipip ttl 64", Slug: "link_ipip_set_on_external",
		// "Invalid argument", compared on the request.
		Floor: 6, Implemented: true, Write: true, Setup: ipipWriteSetupIp0External,
	},
	{
		Name: "link set ip0 type ipip external", Slug: "link_ipip_set_external",
		// "Invalid argument", compared on the request.
		Floor: 6, Implemented: true, Write: true, Setup: ipipWriteSetupIp0,
	},
	{
		Name: "link add st0 type sit remote 10.0.0.1", Slug: "link_sit_add_default",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add st0 type sit", Slug: "link_sit_add_no_option",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add st0 type sit remote 10.0.0.1 mode ip6ip", Slug: "link_sit_add_mode_ip6ip",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add st0 type sit remote 10.0.0.1 mode ipip", Slug: "link_sit_add_mode_ipip",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add st0 type sit remote 10.0.0.1 mode mplsip", Slug: "link_sit_add_mode_mplsip",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add st0 type sit remote 10.0.0.1 mode any", Slug: "link_sit_add_mode_any",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add st0 type sit remote 10.0.0.1 mode ipv6/ipv4", Slug: "link_sit_add_mode_alias",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add st0 type sit remote 10.0.0.1 local 10.0.0.2 dev d0", Slug: "link_sit_add_local_dev",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add st0 type sit remote 10.0.0.1 ttl 64 tos inherit", Slug: "link_sit_add_ttl_tos",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add st0 type sit remote 10.0.0.1 nopmtudisc", Slug: "link_sit_add_nopmtudisc",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add st0 type sit isatap", Slug: "link_sit_add_isatap",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add st0 type sit remote 10.0.0.1 fwmark 7", Slug: "link_sit_add_fwmark",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add st0 type sit remote 10.0.0.1 6rd-prefix 2001:db8::/32 6rd-relay_prefix 10.0.0.0/8", Slug: "link_sit_add_6rd_prefix",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add st0 type sit remote 10.0.0.1 6rd-prefix 2001:db8::/48 6rd-relay_prefix 10.0.0.0/8", Slug: "link_sit_add_6rd_overlong",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add st0 type sit external", Slug: "link_sit_add_external",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add st0 type sit remote 10.0.0.1 encap fou encap-sport 1000 encap-dport 5555", Slug: "link_sit_add_encap_fou",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link set st0 type sit ttl 64 tos 10 local 10.0.0.2", Slug: "link_sit_set_changeable",
		Floor: 6, Implemented: true, Write: true, Setup: sitWriteSetupSt0,
	},
	{
		Name: "link set st0 type sit mode ipip", Slug: "link_sit_set_mode",
		Floor: 6, Implemented: true, Write: true, Setup: sitWriteSetupSt0,
	},
	{
		Name: "link set st0 type sit isatap", Slug: "link_sit_set_isatap",
		Floor: 6, Implemented: true, Write: true, Setup: sitWriteSetupSt0,
	},
	{
		Name: "link set st0 type sit", Slug: "link_sit_set_no_option",
		Floor: 4, Implemented: true, Write: true, Setup: sitWriteSetupSt0,
	},
	{
		Name: "link add st1 type sit remote 10.0.0.1", Slug: "link_sit_add_duplicate",
		// "File exists", compared on the request.
		Floor: 2, Implemented: true, Write: true, Setup: sitWriteSetupSt0,
	},
	{
		Name: "link add st1 type sit", Slug: "link_sit_add_duplicate_wildcard",
		// "File exists", compared on the request.
		Floor: 2, Implemented: true, Write: true, Setup: sitWriteSetupSt0Bare,
	},
	{
		Name: "link set st0 type sit remote any", Slug: "link_sit_set_remote_any",
		// "Invalid argument", compared on the request.
		Floor: 6, Implemented: true, Write: true, Setup: sitWriteSetupSt0,
	},
	{
		Name: "link set st0 type sit remote 10.0.0.1", Slug: "link_sit_set_remote_on_inherit",
		// "Invalid argument", compared on the request.
		Floor: 6, Implemented: true, Write: true, Setup: sitWriteSetupSt0Inherit,
	},
	{
		Name: "link set st0 type sit remote 10.0.0.2", Slug: "link_sit_set_collide",
		// "File exists", compared on the request.
		Floor: 6, Implemented: true, Write: true, Setup: sitWriteSetupSt0St1,
	},
	{
		Name: "link set st0 type sit external", Slug: "link_sit_set_external",
		// "Invalid argument", compared on the request.
		Floor: 6, Implemented: true, Write: true, Setup: sitWriteSetupSt0,
	},
	{
		Name: "link add t6 type ip6tnl remote 2001:db8::1", Slug: "link_ip6tnl_add_default",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add t6 type ip6tnl", Slug: "link_ip6tnl_add_no_option",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add t6 type ip6tnl remote 2001:db8::1 mode ip6ip6", Slug: "link_ip6tnl_add_mode_ip6ip6",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add t6 type ip6tnl remote 2001:db8::1 mode ipip6", Slug: "link_ip6tnl_add_mode_ipip6",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add t6 type ip6tnl remote 2001:db8::1 mode any", Slug: "link_ip6tnl_add_mode_any",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add t6 type ip6tnl remote 2001:db8::1 mode ip4ip6", Slug: "link_ip6tnl_add_mode_alias",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add t6 type ip6tnl remote 2001:db8::1 local 2001:db8::2", Slug: "link_ip6tnl_add_local",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add t6 type ip6tnl remote 2001:db8::1 dev d0", Slug: "link_ip6tnl_add_dev",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add t6 type ip6tnl remote 2001:db8::1 hoplimit 255", Slug: "link_ip6tnl_add_hoplimit",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add t6 type ip6tnl remote 2001:db8::1 hoplimit inherit", Slug: "link_ip6tnl_add_hoplimit_inherit",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add t6 type ip6tnl remote 2001:db8::1 hoplimit 10 ttl 20", Slug: "link_ip6tnl_add_hoplimit_twice",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add t6 type ip6tnl remote 2001:db8::1 encaplimit 0", Slug: "link_ip6tnl_add_encaplimit",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add t6 type ip6tnl remote 2001:db8::1 encaplimit none", Slug: "link_ip6tnl_add_encaplimit_none",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add t6 type ip6tnl remote 2001:db8::1 tclass ff", Slug: "link_ip6tnl_add_tclass",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add t6 type ip6tnl remote 2001:db8::1 tclass inherit", Slug: "link_ip6tnl_add_tclass_inherit",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add t6 type ip6tnl remote 2001:db8::1 dsf 10", Slug: "link_ip6tnl_add_dsfield_abbrev",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add t6 type ip6tnl remote 2001:db8::1 flowlabel 0xfffff", Slug: "link_ip6tnl_add_flowlabel",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add t6 type ip6tnl remote 2001:db8::1 fl inherit", Slug: "link_ip6tnl_add_flowlabel_inherit",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add t6 type ip6tnl remote 2001:db8::1 dscp inherit", Slug: "link_ip6tnl_add_dscp_inherit",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add t6 type ip6tnl remote 2001:db8::1 fwmark 0xffffffff", Slug: "link_ip6tnl_add_fwmark",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add t6 type ip6tnl remote 2001:db8::1 fwmark inherit", Slug: "link_ip6tnl_add_fwmark_inherit",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add t6 type ip6tnl remote 2001:db8::1 allow-localremote", Slug: "link_ip6tnl_add_allow_localremote",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add t6 type ip6tnl external", Slug: "link_ip6tnl_add_external",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add t6 type ip6tnl remote 2001:db8::1 noencap encap-sport 1000 encap-dport 5555 encap-csum noencap-udp6-csum encap-remcsum", Slug: "link_ip6tnl_add_noencap",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link set t6 type ip6tnl hlim 32 encaplimit none tclass inherit flowlabel 0x12345 dscp inherit", Slug: "link_ip6tnl_set_changeable",
		Floor: 6, Implemented: true, Write: true, Setup: ip6tnlWriteSetupT6,
	},
	{
		Name: "link set t6 type ip6tnl mode ipip6", Slug: "link_ip6tnl_set_mode",
		Floor: 6, Implemented: true, Write: true, Setup: ip6tnlWriteSetupT6,
	},
	{
		Name: "link set t6 type ip6tnl remote 2001:db8::2", Slug: "link_ip6tnl_set_remote",
		Floor: 6, Implemented: true, Write: true, Setup: ip6tnlWriteSetupT6,
	},
	{
		Name: "link set t6 type ip6tnl tclass 0x20", Slug: "link_ip6tnl_set_keeps_prefetch",
		Floor: 6, Implemented: true, Write: true, Setup: ip6tnlWriteSetupT6Hoplimit,
	},
	{
		Name: "link set t6 type ip6tnl", Slug: "link_ip6tnl_set_no_option",
		Floor: 4, Implemented: true, Write: true, Setup: ip6tnlWriteSetupT6,
	},
	{
		Name: "link add t6 type ip6tnl remote 2001:db8::1 encap fou encap-dport 5555", Slug: "link_ip6tnl_add_encap_fou",
		// "Invalid argument", compared on the request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add t7 type ip6tnl remote 2001:db8::1", Slug: "link_ip6tnl_add_duplicate",
		// "File exists", compared on the request.
		Floor: 2, Implemented: true, Write: true, Setup: ip6tnlWriteSetupT6,
	},
	{
		Name: "link add t7 type ip6tnl", Slug: "link_ip6tnl_add_duplicate_wildcard",
		// "File exists", compared on the request.
		Floor: 2, Implemented: true, Write: true, Setup: ip6tnlWriteSetupT6Bare,
	},
	{
		Name: "link add t7 type ip6tnl external", Slug: "link_ip6tnl_add_external_second",
		// "File exists", compared on the request.
		Floor: 2, Implemented: true, Write: true, Setup: ip6tnlWriteSetupT6External,
	},
	{
		Name: "link set dev t6 type ip6tnl remote 2001:db8::2", Slug: "link_ip6tnl_set_collide",
		// "File exists", compared on the request. Spelled with `dev`, as
		// ip6gretap's add_beside_ip6gre is, since set_remote has the plain
		// spelling.
		Floor: 6, Implemented: true, Write: true, Setup: ip6tnlWriteSetupT6T7,
	},
	{
		Name: "link set t6 type ip6tnl hoplimit 5", Slug: "link_ip6tnl_set_on_external",
		// "Invalid argument", compared on the request.
		Floor: 6, Implemented: true, Write: true, Setup: ip6tnlWriteSetupT6External,
	},
	{
		Name: "link set t6 type ip6tnl external", Slug: "link_ip6tnl_set_external",
		// "Invalid argument", compared on the request.
		Floor: 6, Implemented: true, Write: true, Setup: ip6tnlWriteSetupT6,
	},
	{
		Name: "link add vt0 type vti remote 10.0.0.1 local 10.0.0.2", Slug: "link_vti_add_default",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vt0 type vti", Slug: "link_vti_add_no_option",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vt0 type vti remote 10.0.0.1 ikey 1.2.3.4 okey 0x10", Slug: "link_vti_add_ikey_okey",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vt0 type vti remote 10.0.0.1 key 4294967295", Slug: "link_vti_add_key_max",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vt0 type vti remote 10.0.0.1 key 5 ikey 6", Slug: "link_vti_add_key_last_wins",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vt0 type vti remote 10.0.0.1 dev d0", Slug: "link_vti_add_dev",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add vt0 type vti remote 10.0.0.1 fwmark 0x10", Slug: "link_vti_add_fwmark",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vt0 type vti r 10.0.0.1 l 10.0.0.2 i 1 o 2 d d0", Slug: "link_vti_add_abbrev",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add vt1 type vti remote 10.0.0.1 dev d0", Slug: "link_vti_add_second_other_dev",
		Floor: 4, Implemented: true, Write: true, Setup: vtiWriteSetupD0Vt0,
	},
	{
		Name: "link set vt0 type vti fwmark 5", Slug: "link_vti_set_fwmark",
		Floor: 6, Implemented: true, Write: true, Setup: vtiWriteSetupVt0,
	},
	{
		Name: "link set vt0 type vti", Slug: "link_vti_set_no_option",
		Floor: 4, Implemented: true, Write: true, Setup: vtiWriteSetupVt0,
	},
	{
		Name: "link add vt1 type vti remote 10.0.0.1 key 7", Slug: "link_vti_add_duplicate_other_key",
		// "File exists", compared on the request.
		Floor: 2, Implemented: true, Write: true, Setup: vtiWriteSetupVt0,
	},
	{
		Name: "link add vt1 type vti", Slug: "link_vti_add_duplicate_any",
		// "File exists", compared on the request.
		Floor: 2, Implemented: true, Write: true, Setup: vtiWriteSetupVt0Bare,
	},
	{
		Name: "link set vt0 type vti remote 10.0.0.1", Slug: "link_vti_set_remote_on_any",
		// "Invalid argument", compared on the request.
		Floor: 6, Implemented: true, Write: true, Setup: vtiWriteSetupVt0Bare,
	},
	{
		Name: "link set vt0 type vti remote any", Slug: "link_vti_set_remote_any",
		// "Invalid argument", compared on the request.
		Floor: 6, Implemented: true, Write: true, Setup: vtiWriteSetupVt0,
	},
	{
		Name: "link set vt1 type vti remote 10.0.0.1", Slug: "link_vti_set_collide",
		// "File exists", compared on the request.
		Floor: 6, Implemented: true, Write: true, Setup: vtiWriteSetupVt0Vt1,
	},
	{
		Name: "link add v6 type vti6 remote 2001:db8::1 local 2001:db8::2", Slug: "link_vti6_add_default",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add v6 type vti6", Slug: "link_vti6_add_no_option",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add v6 type vti6 remote 2001:db8::1 key 5 dev d0 fwmark 0x10", Slug: "link_vti6_add_options",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link set v6 type vti6 fwmark 5", Slug: "link_vti6_set_fwmark",
		Floor: 6, Implemented: true, Write: true, Setup: vti6WriteSetupV6,
	},
	{
		Name: "link set v6 type vti6", Slug: "link_vti6_set_no_option",
		Floor: 4, Implemented: true, Write: true, Setup: vti6WriteSetupV6,
	},
	{
		Name: "link set v6 type vti6 remote 2001:db8::1", Slug: "link_vti6_set_remote_on_any",
		Floor: 6, Implemented: true, Write: true, Setup: vti6WriteSetupV6Bare,
	},
	{
		Name: "link set v6 type vti6 remote any", Slug: "link_vti6_set_remote_any",
		Floor: 6, Implemented: true, Write: true, Setup: vti6WriteSetupV6,
	},
	{
		Name: "link add v7 type vti6 remote 2001:db8::1 dev d0", Slug: "link_vti6_add_duplicate_other_dev",
		// "File exists", compared on the request.
		Floor: 4, Implemented: true, Write: true, Setup: vti6WriteSetupD0V6,
	},
	{
		Name: "link set v7 type vti6 remote 2001:db8::1", Slug: "link_vti6_set_collide",
		// "File exists", compared on the request.
		Floor: 6, Implemented: true, Write: true, Setup: vti6WriteSetupV6V7,
	},
	{
		Name: "link add xf0 type xfrm if_id 1", Slug: "link_xfrm_add_default",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add xf0 type xfrm dev d0 if_id 1", Slug: "link_xfrm_add_dev",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add xf0 type xfrm if_id 0xffffffff", Slug: "link_xfrm_add_if_id_max",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add xf0 type xfrm external", Slug: "link_xfrm_add_external",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add xf0 type xfrm d d0 i 5", Slug: "link_xfrm_add_abbrev",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	{
		Name: "link add xf0 type xfrm if_id 1 if_id 2", Slug: "link_xfrm_add_if_id_twice",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link set xf0 type xfrm if_id 2", Slug: "link_xfrm_set_if_id",
		Floor: 4, Implemented: true, Write: true, Setup: xfrmWriteSetupXf0,
	},
	{
		Name: "link set xf0 type xfrm dev d0 if_id 2", Slug: "link_xfrm_set_if_id_dev",
		Floor: 6, Implemented: true, Write: true, Setup: xfrmWriteSetupD0Xf0Dev,
	},
	{
		Name: "link set xf0 type xfrm", Slug: "link_xfrm_set_no_option",
		Floor: 4, Implemented: true, Write: true, Setup: xfrmWriteSetupXf0,
	},
	{
		Name: "link add xf0 type xfrm", Slug: "link_xfrm_add_no_option",
		// "if_id must be non zero", compared on the request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add xf1 type xfrm dev d0 if_id 1", Slug: "link_xfrm_add_duplicate_other_dev",
		// "File exists", compared on the request.
		Floor: 4, Implemented: true, Write: true, Setup: xfrmWriteSetupD0Xf0,
	},
	{
		Name: "link add xf1 type xfrm external", Slug: "link_xfrm_add_second_external",
		// "File exists", compared on the request.
		Floor: 2, Implemented: true, Write: true, Setup: xfrmWriteSetupXf0External,
	},
	{
		Name: "link set dev xf0 type xfrm if_id 2", Slug: "link_xfrm_set_drop_dev",
		// "Invalid argument", compared on the request. Spelled with `dev`, as
		// ip6gretap's add_beside_ip6gre is, since set_if_id has the plain
		// spelling.
		Floor: 4, Implemented: true, Write: true, Setup: xfrmWriteSetupD0Xf0Dev,
	},
	{
		Name: "link set xf0 type xfrm dev d0 if_id 1", Slug: "link_xfrm_set_add_dev",
		// "Invalid argument", compared on the request.
		Floor: 6, Implemented: true, Write: true, Setup: xfrmWriteSetupD0Xf0,
	},
	{
		Name: "link set xf1 type xfrm if_id 1", Slug: "link_xfrm_set_collide",
		// "File exists", compared on the request.
		Floor: 4, Implemented: true, Write: true, Setup: xfrmWriteSetupXf0Xf1,
	},
	{
		Name: "link set xf0 type xfrm external", Slug: "link_xfrm_set_external",
		// "if_id must be non zero", compared on the request.
		Floor: 4, Implemented: true, Write: true, Setup: xfrmWriteSetupXf0,
	},
	{
		Name: "link set xf0 type xfrm if_id 5", Slug: "link_xfrm_set_on_external",
		// "collect_md can't be changed", compared on the request.
		Floor: 4, Implemented: true, Write: true, Setup: xfrmWriteSetupXf0External,
	},
	// The remaining kinds' write rows (goip-link-kinds.md, KG5-T07): KC6's
	// hsr, netkit, vrf, vrf_slave, vxcan, batadv, ifb, nlmon, vcan, team and
	// team_slave rows that sent a request, in kind-write-corpus.exp's order,
	// from pkg/xtcpnl/testdata/7_3_0/link-write/kinds/<kind>/. Each Floor is
	// the row's ip.pcap record count, and each Setup is the row's topology
	// file. The 46 rows the kernel refuses compare on the request; the 59
	// rows ip refuses before netlink are not rows, and the kinds' refusal
	// tests hold them offline. Six rows that share an argv with another take
	// iplink_parse's optional dev.
	{
		Name: "link add hs0 type hsr slave1 d0 slave2 d1", Slug: "link_hsr_add_default",
		Floor: 6, Implemented: true, Write: true, Setup: hsrWriteSetupD0D1,
	},
	{
		Name: "link add hs0 type hsr slave1 d0 slave2 d1 supervision 42", Slug: "link_hsr_add_supervision",
		Floor: 6, Implemented: true, Write: true, Setup: hsrWriteSetupD0D1,
	},
	{
		Name: "link add hs0 type hsr slave1 d0 slave2 d1 supervision 255", Slug: "link_hsr_add_supervision_max",
		Floor: 6, Implemented: true, Write: true, Setup: hsrWriteSetupD0D1,
	},
	{
		Name: "link add hs0 type hsr slave1 d0 slave2 d1 supervision 0xff", Slug: "link_hsr_add_supervision_hex",
		Floor: 6, Implemented: true, Write: true, Setup: hsrWriteSetupD0D1,
	},
	{
		Name: "link add hs0 type hsr slave1 d0 slave2 d1 supervision 1 supervision 2", Slug: "link_hsr_add_supervision_twice",
		Floor: 6, Implemented: true, Write: true, Setup: hsrWriteSetupD0D1,
	},
	{
		Name: "link add hs0 type hsr slave1 d0 slave2 d1 version 1", Slug: "link_hsr_add_version_1",
		Floor: 6, Implemented: true, Write: true, Setup: hsrWriteSetupD0D1,
	},
	{
		Name: "link add hs0 type hsr slave1 d0 slave2 d1 proto 1", Slug: "link_hsr_add_proto_prp",
		Floor: 6, Implemented: true, Write: true, Setup: hsrWriteSetupD0D1,
	},
	{
		Name: "link add hs0 type hsr slave1 d0 slave2 d1 interlink d2", Slug: "link_hsr_add_interlink",
		Floor: 8, Implemented: true, Write: true, Setup: hsrWriteSetupD0D1D2,
	},
	{
		Name: "link add hs0 type hsr sl d0 slave2 d1 s 7 v 1 p 0", Slug: "link_hsr_add_abbrev",
		Floor: 6, Implemented: true, Write: true, Setup: hsrWriteSetupD0D1,
	},
	{
		Name: "link add hs0 type hsr", Slug: "link_hsr_add_no_option",
		// "No slave devices specified", compared on the request.
		Floor: 2, Implemented: true, Write: true, Setup: hsrWriteSetupD0D1,
	},
	{
		Name: "link add hs0 type hsr slave1 d0", Slug: "link_hsr_add_slave1_only",
		// "Slave2 device not specified", compared on the request.
		Floor: 4, Implemented: true, Write: true, Setup: hsrWriteSetupD0D1,
	},
	{
		Name: "link add hs0 type hsr slave2 d1", Slug: "link_hsr_add_slave2_only",
		// "Slave1 device not specified", compared on the request.
		Floor: 4, Implemented: true, Write: true, Setup: hsrWriteSetupD0D1,
	},
	{
		Name: "link add hs0 type hsr slave1 d0 slave2 d0", Slug: "link_hsr_add_same_slaves",
		// "Slave1 and Slave2 are same", compared on the request.
		Floor: 4, Implemented: true, Write: true, Setup: hsrWriteSetupD0D1,
	},
	{
		Name: "link add hs0 type hsr slave1 d0 slave2 d1 version 2", Slug: "link_hsr_add_version_2",
		// "Only HSR version 0/1 supported", compared on the request.
		Floor: 6, Implemented: true, Write: true, Setup: hsrWriteSetupD0D1,
	},
	{
		Name: "link add hs0 type hsr slave1 d0 slave2 d1 proto 2", Slug: "link_hsr_add_proto_2",
		// "Unsupported protocol", compared on the request.
		Floor: 6, Implemented: true, Write: true, Setup: hsrWriteSetupD0D1,
	},
	{
		Name: "link add hs0 type hsr slave1 d0 slave2 d1 proto 1 version 0", Slug: "link_hsr_add_prp_version",
		// "PRP version unsupported", compared on the request.
		Floor: 6, Implemented: true, Write: true, Setup: hsrWriteSetupD0D1,
	},
	{
		Name: "link add hs0 type hsr slave1 d0 slave2 d1 interlink d0", Slug: "link_hsr_add_interlink_slave",
		// "Interlink and Slave1 are the same", compared on the request.
		Floor: 6, Implemented: true, Write: true, Setup: hsrWriteSetupD0D1,
	},
	{
		Name: "link add hs0 type hsr slave1 lo slave2 d1", Slug: "link_hsr_add_slave_lo",
		// "Cannot use loopback or non-ethernet device as HSR slave", compared on the request.
		Floor: 6, Implemented: true, Write: true, Setup: hsrWriteSetupD0D1,
	},
	{
		Name: "link set hs0 type hsr supervision 5", Slug: "link_hsr_set_option",
		// "Operation not supported", compared on the request.
		Floor: 4, Implemented: true, Write: true, Setup: hsrWriteSetupHs0,
	},
	{
		Name: "link set hs0 type hsr", Slug: "link_hsr_set_no_option",
		Floor: 4, Implemented: true, Write: true, Setup: hsrWriteSetupHs0,
	},
	{
		Name: "link add nt0 type netkit", Slug: "link_netkit_add_no_option",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add nt0 type netkit peer", Slug: "link_netkit_add_peer_only",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add nt0 type netkit mode l2", Slug: "link_netkit_add_mode_l2",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add nt0 type netkit mode l3", Slug: "link_netkit_add_mode_l3",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add nt0 type netkit blackhole", Slug: "link_netkit_add_blackhole",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add nt0 type netkit forward blackhole", Slug: "link_netkit_add_policy_twice",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add nt0 type netkit scrub none", Slug: "link_netkit_add_scrub_none",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add nt0 type netkit peer blackhole", Slug: "link_netkit_add_peer_blackhole",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add nt0 type netkit peer scrub none", Slug: "link_netkit_add_peer_scrub",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add nt0 type netkit peer name nt1", Slug: "link_netkit_add_peer_name",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add nt0 type netkit peer nt1", Slug: "link_netkit_add_peer_bare_name",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add nt0 type netkit peer black", Slug: "link_netkit_add_peer_word",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add nt0 type netkit peer mode l2", Slug: "link_netkit_add_peer_mode",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add nt0 type netkit peer up", Slug: "link_netkit_add_peer_up",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add nt0 address 02:00:00:00:00:01 type netkit mode l2", Slug: "link_netkit_add_l2_address",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add nt0 type netkit peer name nt0", Slug: "link_netkit_add_peer_samename",
		// "File exists", compared on the request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add nt0 address 02:00:00:00:00:01 type netkit", Slug: "link_netkit_add_l3_address",
		// "Operation not supported", compared on the request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add nt0 type netkit peer address 02:00:00:00:00:01", Slug: "link_netkit_add_l3_peer_address",
		// "Operation not supported", compared on the request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add nt0 address 01:00:00:00:00:01 type netkit mode l2", Slug: "link_netkit_add_mcast_address",
		// "Cannot assign requested address", compared on the request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link set nt0 type netkit blackhole", Slug: "link_netkit_set_blackhole",
		Floor: 4, Implemented: true, Write: true, Setup: netkitWriteSetupNt0,
	},
	{
		Name: "link set nt0 type netkit peer blackhole", Slug: "link_netkit_set_peer_policy",
		Floor: 4, Implemented: true, Write: true, Setup: netkitWriteSetupNt0,
	},
	{
		Name: "link set nt0 type netkit", Slug: "link_netkit_set_no_option",
		Floor: 4, Implemented: true, Write: true, Setup: netkitWriteSetupNt0,
	},
	{
		Name: "link set nt0 type netkit peer", Slug: "link_netkit_set_peer_only",
		Floor: 4, Implemented: true, Write: true, Setup: netkitWriteSetupNt0,
	},
	{
		Name: "link set nt0 type netkit mode l3", Slug: "link_netkit_set_mode",
		// "netkit link operating mode cannot be changed after device creation", compared on the request.
		Floor: 4, Implemented: true, Write: true, Setup: netkitWriteSetupNt0,
	},
	{
		Name: "link set nt0 type netkit scrub none", Slug: "link_netkit_set_scrub",
		// "netkit link scrubbing cannot be changed after device creation", compared on the request.
		Floor: 4, Implemented: true, Write: true, Setup: netkitWriteSetupNt0,
	},
	{
		Name: "link set nt0 type netkit peer name nt1", Slug: "link_netkit_set_peer_name",
		// "netkit link peer info cannot be changed after device creation", compared on the request.
		Floor: 6, Implemented: true, Write: true, Setup: netkitWriteSetupNt0Nt1,
	},
	{
		Name: "link set nt1 type netkit blackhole", Slug: "link_netkit_set_on_peer",
		// "netkit link settings can be changed only through the primary device", compared on the request.
		Floor: 4, Implemented: true, Write: true, Setup: netkitWriteSetupNt0Nt1,
	},
	{
		Name: "link add vr0 type vrf table 10", Slug: "link_vrf_add_table",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vr0 type vrf table 4294967295", Slug: "link_vrf_add_table_max",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vr0 type vrf table 0x10", Slug: "link_vrf_add_table_hex",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vr0 type vrf table main", Slug: "link_vrf_add_table_main",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vr0 type vrf table 10 table 20", Slug: "link_vrf_add_table_twice",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vr0 type vrf t 10", Slug: "link_vrf_add_abbrev",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add dev vr0 type vrf table 10", Slug: "link_vrf_add_shared_table",
		Floor: 2, Implemented: true, Write: true, Setup: vrfWriteSetupVr1,
	},
	{
		Name: "link add vr0 type vrf", Slug: "link_vrf_add_no_option",
		// "VRF table id is missing", compared on the request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vr0 type vrf table 0", Slug: "link_vrf_add_table_zero",
		// "Invalid VRF table id", compared on the request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vr0 address 01:00:00:00:00:01 type vrf table 10", Slug: "link_vrf_add_mcast_address",
		// "Invalid hardware address", compared on the request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link set vr0 type vrf table 20", Slug: "link_vrf_set_table",
		// "Operation not supported", compared on the request.
		Floor: 4, Implemented: true, Write: true, Setup: vrfWriteSetupVr0,
	},
	{
		Name: "link set vr0 type vrf", Slug: "link_vrf_set_no_option",
		Floor: 4, Implemented: true, Write: true, Setup: vrfWriteSetupVr0,
	},
	{
		Name: "link set d0 master vr0", Slug: "link_vrf_slave_set_master",
		Floor: 6, Implemented: true, Write: true, Setup: vrfSlaveWriteSetupVr0D0,
	},
	{
		Name: "link set d0 vrf vr0", Slug: "link_vrf_slave_set_vrf",
		Floor: 8, Implemented: true, Write: true, Setup: vrfSlaveWriteSetupVr0D0,
	},
	{
		Name: "link set d0 type vrf_slave", Slug: "link_vrf_slave_set_type",
		Floor: 4, Implemented: true, Write: true, Setup: vrfSlaveWriteSetupD0Enslaved,
	},
	{
		Name: "link set dev d0 nomaster", Slug: "link_vrf_slave_set_nomaster",
		Floor: 4, Implemented: true, Write: true, Setup: vrfSlaveWriteSetupD0Enslaved,
	},
	{
		Name: "link set lo master vr0", Slug: "link_vrf_slave_set_master_lo",
		// "Can not enslave loopback device to a VRF", compared on the request.
		Floor: 6, Implemented: true, Write: true, Setup: vrfWriteSetupVr0,
	},
	{
		Name: "link set vr1 master vr0", Slug: "link_vrf_slave_set_vrf_in_vrf",
		// "Can not enslave an L3 master device to a VRF", compared on the request.
		Floor: 6, Implemented: true, Write: true, Setup: vrfSlaveWriteSetupVr0Vr1,
	},
	{
		Name: "link add vs0 type vrf_slave", Slug: "link_vrf_slave_add",
		// "Unknown device type", compared on the request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vc0 type vxcan", Slug: "link_vxcan_add_no_option",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vc0 type vxcan peer", Slug: "link_vxcan_add_peer",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vc0 type vxcan peer name vc1", Slug: "link_vxcan_add_peer_name",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vc0 type vxcan peer vc1", Slug: "link_vxcan_add_peer_bare_name",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vc0 up type vxcan", Slug: "link_vxcan_add_up",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vc0 type vxcan peer name vc0", Slug: "link_vxcan_add_peer_samename",
		// "File exists", compared on the request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vc0 type vxcan peer up", Slug: "link_vxcan_add_peer_up",
		// "Transport endpoint is not connected", compared on the request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link set vc0 type vxcan peer name vc1", Slug: "link_vxcan_set_peer",
		// "Operation not supported", compared on the request.
		Floor: 6, Implemented: true, Write: true, Setup: vxcanWriteSetupVc0,
	},
	{
		Name: "link set vc0 type vxcan", Slug: "link_vxcan_set_no_option",
		Floor: 4, Implemented: true, Write: true, Setup: vxcanWriteSetupVc0,
	},
	{
		Name: "link add bat0 type batadv", Slug: "link_batadv_add_no_option",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add bat0 type batadv ra BATMAN_V", Slug: "link_batadv_add_ra_v",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add bat0 type batadv ra BATMAN_IV", Slug: "link_batadv_add_ra_iv",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add bat0 type batadv r BATMAN_V", Slug: "link_batadv_add_abbrev",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add bat0 type batadv ra BATMAN_IV ra BATMAN_V", Slug: "link_batadv_add_ra_twice",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add dev bat0 type batadv", Slug: "link_batadv_add_duplicate",
		// "File exists", compared on the request.
		Floor: 2, Implemented: true, Write: true, Setup: batadvWriteSetupBat0,
	},
	{
		Name: "link add bat0 type batadv ra BOGUS", Slug: "link_batadv_add_ra_bogus",
		// "Invalid argument", compared on the request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add bat0 type batadv ra batman_v", Slug: "link_batadv_add_ra_lowercase",
		// "Invalid argument", compared on the request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add bat0 mtu 67 type batadv", Slug: "link_batadv_add_mtu_low",
		// "mtu less than device minimum", compared on the request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link set bat0 type batadv ra BATMAN_V", Slug: "link_batadv_set_ra",
		// "Operation not supported", compared on the request.
		Floor: 4, Implemented: true, Write: true, Setup: batadvWriteSetupBat0,
	},
	{
		Name: "link set bat0 type batadv ra BOGUS", Slug: "link_batadv_set_ra_bogus",
		// "Invalid argument", compared on the request.
		Floor: 4, Implemented: true, Write: true, Setup: batadvWriteSetupBat0,
	},
	{
		Name: "link set bat0 type batadv", Slug: "link_batadv_set_no_option",
		Floor: 4, Implemented: true, Write: true, Setup: batadvWriteSetupBat0,
	},
	{
		Name: "link add ib0 type ifb", Slug: "link_ifb_add_no_option",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add ib0 numtxqueues 4 type ifb", Slug: "link_ifb_add_numtxqueues",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link set ib0 type ifb", Slug: "link_ifb_set_no_option",
		Floor: 4, Implemented: true, Write: true, Setup: ifbWriteSetupIb0,
	},
	{
		Name: "link set ib0 type vcan", Slug: "link_ifb_set_kind_mismatch",
		Floor: 4, Implemented: true, Write: true, Setup: ifbWriteSetupIb0,
	},
	{
		Name: "link add ib0 address 01:00:5e:00:00:01 type ifb", Slug: "link_ifb_add_mcast_address",
		// "Cannot assign requested address", compared on the request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add nm0 type nlmon", Slug: "link_nlmon_add_no_option",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add nm0 mtu 16 type nlmon", Slug: "link_nlmon_add_mtu_min",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add nm0 address 00:11:22:33:44:55 type nlmon", Slug: "link_nlmon_add_address",
		// "Invalid argument", compared on the request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add nm0 mtu 15 type nlmon", Slug: "link_nlmon_add_mtu_below",
		// "mtu less than device minimum", compared on the request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link set nm0 type nlmon", Slug: "link_nlmon_set_no_option",
		Floor: 4, Implemented: true, Write: true, Setup: nlmonWriteSetupNm0,
	},
	{
		Name: "link add vn0 type vcan", Slug: "link_vcan_add_no_option",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vn0 mtu 16 type vcan", Slug: "link_vcan_add_mtu_16",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link add vn0 mtu 100 type vcan", Slug: "link_vcan_add_mtu_100",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link set vn0 mtu 72", Slug: "link_vcan_set_mtu_72",
		Floor: 4, Implemented: true, Write: true, Setup: vcanWriteSetupVn0,
	},
	{
		Name: "link set vn0 mtu 76", Slug: "link_vcan_set_mtu_76",
		Floor: 4, Implemented: true, Write: true, Setup: vcanWriteSetupVn0,
	},
	{
		Name: "link set vn0 mtu 2060", Slug: "link_vcan_set_mtu_same",
		Floor: 4, Implemented: true, Write: true, Setup: vcanWriteSetupVn0,
	},
	{
		Name: "link set vn0 up mtu 72", Slug: "link_vcan_set_up_and_mtu",
		Floor: 4, Implemented: true, Write: true, Setup: vcanWriteSetupVn0,
	},
	{
		Name: "link set vn0 type vcan", Slug: "link_vcan_set_no_option",
		Floor: 4, Implemented: true, Write: true, Setup: vcanWriteSetupVn0,
	},
	{
		Name: "link set vn0 mtu 75", Slug: "link_vcan_set_mtu_75",
		// "Invalid argument", compared on the request.
		Floor: 4, Implemented: true, Write: true, Setup: vcanWriteSetupVn0,
	},
	{
		Name: "link set vn0 mtu 2061", Slug: "link_vcan_set_mtu_2061",
		// "Invalid argument", compared on the request.
		Floor: 4, Implemented: true, Write: true, Setup: vcanWriteSetupVn0,
	},
	{
		Name: "link set dev vn0 mtu 72", Slug: "link_vcan_set_mtu_up",
		// "Device or resource busy", compared on the request.
		Floor: 4, Implemented: true, Write: true, Setup: vcanWriteSetupVn0Up,
	},
	{
		Name: "link add tm0 type team", Slug: "link_team_add_no_option",
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link set tm0 type team", Slug: "link_team_set_no_option",
		Floor: 4, Implemented: true, Write: true, Setup: teamWriteSetupTm0,
	},
	{
		Name: "link set d0 master tm0", Slug: "link_team_set_enslave",
		Floor: 6, Implemented: true, Write: true, Setup: teamWriteSetupTm0D0,
	},
	{
		Name: "link add d0 master tm0 type dummy", Slug: "link_team_add_enslaved",
		Floor: 4, Implemented: true, Write: true, Setup: teamWriteSetupTm0,
	},
	{
		Name: "link set dev d0 master tm0", Slug: "link_team_set_enslave_again",
		Floor: 6, Implemented: true, Write: true, Setup: teamWriteSetupD0Enslaved,
	},
	{
		Name: "link set d0 nomaster", Slug: "link_team_set_nomaster",
		Floor: 4, Implemented: true, Write: true, Setup: teamWriteSetupD0Enslaved,
	},
	{
		Name: "link add tm0 address 01:00:5e:00:00:01 type team", Slug: "link_team_add_mcast_address",
		// "Cannot assign requested address", compared on the request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link set d0 up master tm0", Slug: "link_team_set_enslave_up",
		// "Device is up. Set it down before adding it as a team port", compared on the request.
		Floor: 6, Implemented: true, Write: true, Setup: teamWriteSetupTm0D0,
	},
	{
		Name: "link add d0 up master tm0 type dummy", Slug: "link_team_add_enslaved_up",
		// "Device is up. Set it down before adding it as a team port", compared on the request.
		Floor: 4, Implemented: true, Write: true, Setup: teamWriteSetupTm0,
	},
	{
		Name: "link set lo master tm0", Slug: "link_team_set_enslave_lo",
		// "Loopback device can't be added as a team port", compared on the request.
		Floor: 6, Implemented: true, Write: true, Setup: teamWriteSetupTm0,
	},
	{
		Name: "link set tm0 master tm0", Slug: "link_team_set_enslave_self",
		// "Cannot enslave team device to itself", compared on the request.
		Floor: 4, Implemented: true, Write: true, Setup: teamWriteSetupTm0,
	},
	{
		Name: "link add ts0 type team_slave", Slug: "link_team_slave_add",
		// "Unknown device type", compared on the request.
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "link set d0 type team_slave", Slug: "link_team_slave_set_no_option",
		Floor: 4, Implemented: true, Write: true, Setup: teamWriteSetupD0Enslaved,
	},
	{
		Name: "link set dev d0 type team_slave", Slug: "link_team_slave_set_unenslaved",
		Floor: 4, Implemented: true, Write: true, Setup: linkWriteSetupDummy,
	},
	// The bridge read rows: br0's IFLA_INFO_DATA and p0's
	// IFLA_INFO_SLAVE_DATA under -d, text and JSON, on one bridge with two
	// ports. The stdout comparison is the facets, so the timers move freely;
	// TestLinkShowDetailBridgeMatchesSidecar and its bridge_slave twin are
	// the text and JSON oracle.
	{
		Name: "-d link show dev br0", Slug: "link_show_details_bridge",
		// `link show dev`'s two gets (4).
		Setup: bridgeReadSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "-d -j link show dev br0", Slug: "link_show_details_bridge_json",
		Setup: bridgeReadSetup, Floor: 4, Implemented: true,
	},
	{
		Name: "-d link show dev p0", Slug: "link_show_details_bridge_slave",
		// The two gets, then br0's to name the master (6).
		Setup: bridgeReadSetup, Floor: 6, Implemented: true,
	},
	{
		Name: "-d -j link show dev p0", Slug: "link_show_details_bridge_slave_json",
		Setup: bridgeReadSetup, Floor: 6, Implemented: true,
	},
	// The VLAN family's read rows: each kind's IFLA_INFO_DATA under -d,
	// text and JSON, on d0 and the device's add. Each is the device's two
	// gets, then d0's to name the `@d0` (6);
	// TestLinkShowDetailVlanFamilyMatchesSidecar is the text and JSON oracle.
	{
		Name: "-d link show dev v0", Slug: "link_show_details_vlan",
		Setup: vlanWriteSetupV0, Floor: 6, Implemented: true,
	},
	{
		Name: "-d -j link show dev v0", Slug: "link_show_details_vlan_json",
		Setup: vlanWriteSetupV0, Floor: 6, Implemented: true,
	},
	{
		Name: "-d link show dev m0", Slug: "link_show_details_macvlan",
		Setup: macvlanWriteSetupM0, Floor: 6, Implemented: true,
	},
	{
		Name: "-d -j link show dev m0", Slug: "link_show_details_macvlan_json",
		Setup: macvlanWriteSetupM0, Floor: 6, Implemented: true,
	},
	{
		Name: "-d link show dev t0", Slug: "link_show_details_macvtap",
		Setup: macvtapWriteSetupT0, Floor: 6, Implemented: true,
	},
	{
		Name: "-d -j link show dev t0", Slug: "link_show_details_macvtap_json",
		Setup: macvtapWriteSetupT0, Floor: 6, Implemented: true,
	},
	{
		Name: "-d link show dev i0", Slug: "link_show_details_ipvlan",
		Setup: ipvlanWriteSetupI0, Floor: 6, Implemented: true,
	},
	{
		Name: "-d -j link show dev i0", Slug: "link_show_details_ipvlan_json",
		Setup: ipvlanWriteSetupI0, Floor: 6, Implemented: true,
	},
	{
		Name: "-d link show dev j0", Slug: "link_show_details_ipvtap",
		Setup: ipvtapWriteSetupJ0, Floor: 6, Implemented: true,
	},
	{
		Name: "-d -j link show dev j0", Slug: "link_show_details_ipvtap_json",
		Setup: ipvtapWriteSetupJ0, Floor: 6, Implemented: true,
	},
	// The UDP overlays' read rows: each kind's IFLA_INFO_DATA under -d,
	// text and JSON, on the device's add. Each is the device's two gets
	// (4), and a0's is d0's get too, for amt's `dev` (6); vx0 has no lower.
	// TestLinkShowDetailUdpOverlayMatchesSidecar is the text and JSON
	// oracle. `-d -d` is vx0's every bool, at its default or not.
	{
		Name: "-d link show dev vx0", Slug: "link_show_details_vxlan",
		Setup: vxlanWriteSetupVx0, Floor: 4, Implemented: true,
	},
	{
		Name: "-d -j link show dev vx0", Slug: "link_show_details_vxlan_json",
		Setup: vxlanWriteSetupVx0, Floor: 4, Implemented: true,
	},
	{
		Name: "-d link show dev gn0", Slug: "link_show_details_geneve",
		Setup: geneveWriteSetupGn0, Floor: 4, Implemented: true,
	},
	{
		Name: "-d -j link show dev gn0", Slug: "link_show_details_geneve_json",
		Setup: geneveWriteSetupGn0, Floor: 4, Implemented: true,
	},
	{
		Name: "-d link show dev b0", Slug: "link_show_details_bareudp",
		Setup: bareudpWriteSetupB0, Floor: 4, Implemented: true,
	},
	{
		Name: "-d -j link show dev b0", Slug: "link_show_details_bareudp_json",
		Setup: bareudpWriteSetupB0, Floor: 4, Implemented: true,
	},
	{
		Name: "-d link show dev a0", Slug: "link_show_details_amt",
		Setup: amtWriteSetupA0, Floor: 6, Implemented: true,
	},
	{
		Name: "-d -j link show dev a0", Slug: "link_show_details_amt_json",
		Setup: amtWriteSetupA0, Floor: 6, Implemented: true,
	},
	{
		Name: "-d link show dev g0", Slug: "link_show_details_gtp",
		Setup: gtpWriteSetupG0, Floor: 4, Implemented: true,
	},
	{
		Name: "-d -j link show dev g0", Slug: "link_show_details_gtp_json",
		Setup: gtpWriteSetupG0, Floor: 4, Implemented: true,
	},
	{
		Name: "-d -d link show dev vx0", Slug: "link_show_details_vxlan_twice",
		Setup: vxlanWriteSetupVx0, Floor: 4, Implemented: true,
	},
	// The IP tunnels' read rows: each kind's IFLA_INFO_DATA under -d, text
	// and JSON. Each is the device's two gets (4), and vt0's, v6's and
	// xf0's are d0's get too, for their `dev d0` (6).
	// TestLinkShowDetailIptunMatchesSidecar and
	// TestLinkShowDetailGreMatchesSidecar are the text and JSON oracles.
	{
		Name: "-d link show dev gr0", Slug: "link_show_details_gre",
		Setup: greWriteSetupGr0, Floor: 4, Implemented: true,
	},
	{
		Name: "-d -j link show dev gr0", Slug: "link_show_details_gre_json",
		Setup: greWriteSetupGr0, Floor: 4, Implemented: true,
	},
	{
		Name: "-d link show dev gt0", Slug: "link_show_details_gretap",
		Setup: gretapWriteSetupGt0, Floor: 4, Implemented: true,
	},
	{
		Name: "-d -j link show dev gt0", Slug: "link_show_details_gretap_json",
		Setup: gretapWriteSetupGt0, Floor: 4, Implemented: true,
	},
	{
		Name: "-d link show dev es0", Slug: "link_show_details_erspan",
		Setup: erspanWriteSetupEs0, Floor: 4, Implemented: true,
	},
	{
		Name: "-d -j link show dev es0", Slug: "link_show_details_erspan_json",
		Setup: erspanWriteSetupEs0, Floor: 4, Implemented: true,
	},
	{
		Name: "-d link show dev g6", Slug: "link_show_details_ip6gre",
		Setup: ip6greWriteSetupG6, Floor: 4, Implemented: true,
	},
	{
		Name: "-d -j link show dev g6", Slug: "link_show_details_ip6gre_json",
		Setup: ip6greWriteSetupG6, Floor: 4, Implemented: true,
	},
	{
		Name: "-d link show dev gt6", Slug: "link_show_details_ip6gretap",
		Setup: ip6gretapWriteSetupGt6, Floor: 4, Implemented: true,
	},
	{
		Name: "-d -j link show dev gt6", Slug: "link_show_details_ip6gretap_json",
		Setup: ip6gretapWriteSetupGt6, Floor: 4, Implemented: true,
	},
	{
		Name: "-d link show dev es6", Slug: "link_show_details_ip6erspan",
		Setup: ip6erspanWriteSetupEs6, Floor: 4, Implemented: true,
	},
	{
		Name: "-d -j link show dev es6", Slug: "link_show_details_ip6erspan_json",
		Setup: ip6erspanWriteSetupEs6, Floor: 4, Implemented: true,
	},
	{
		Name: "-d link show dev ip0", Slug: "link_show_details_ipip",
		Setup: ipipWriteSetupIp0, Floor: 4, Implemented: true,
	},
	{
		Name: "-d -j link show dev ip0", Slug: "link_show_details_ipip_json",
		Setup: ipipWriteSetupIp0, Floor: 4, Implemented: true,
	},
	{
		Name: "-d link show dev st0", Slug: "link_show_details_sit",
		Setup: sitWriteSetupSt0, Floor: 4, Implemented: true,
	},
	{
		Name: "-d -j link show dev st0", Slug: "link_show_details_sit_json",
		Setup: sitWriteSetupSt0, Floor: 4, Implemented: true,
	},
	{
		Name: "-d link show dev t6", Slug: "link_show_details_ip6tnl",
		Setup: ip6tnlWriteSetupT6, Floor: 4, Implemented: true,
	},
	{
		Name: "-d -j link show dev t6", Slug: "link_show_details_ip6tnl_json",
		Setup: ip6tnlWriteSetupT6, Floor: 4, Implemented: true,
	},
	{
		Name: "-d link show dev vt0", Slug: "link_show_details_vti",
		Setup: vtiReadSetupVt0, Floor: 6, Implemented: true,
	},
	{
		Name: "-d -j link show dev vt0", Slug: "link_show_details_vti_json",
		Setup: vtiReadSetupVt0, Floor: 6, Implemented: true,
	},
	{
		Name: "-d link show dev v6", Slug: "link_show_details_vti6",
		Setup: vti6ReadSetupV6, Floor: 6, Implemented: true,
	},
	{
		Name: "-d -j link show dev v6", Slug: "link_show_details_vti6_json",
		Setup: vti6ReadSetupV6, Floor: 6, Implemented: true,
	},
	{
		Name: "-d link show dev xf0", Slug: "link_show_details_xfrm",
		Setup: xfrmWriteSetupD0Xf0Dev, Floor: 6, Implemented: true,
	},
	{
		Name: "-d -j link show dev xf0", Slug: "link_show_details_xfrm_json",
		Setup: xfrmWriteSetupD0Xf0Dev, Floor: 6, Implemented: true,
	},
	// The remaining kinds' -d read rows (KG5-T07): each device as one
	// accepted KC6 row left it, its Floor the row's getlink_detail record
	// count. team_slave's d0 is shown without `dev`, since vrf_slave's read
	// rows already name `dev d0`. TestLinkShowDetailHsrNetkitMatchesSidecar
	// and the kinds' replays are the offline oracles.
	{
		Name: "-d link show dev hs0", Slug: "link_show_details_hsr",
		Setup: hsrWriteSetupHs0, Floor: 8, Implemented: true,
	},
	{
		Name: "-d -j link show dev hs0", Slug: "link_show_details_hsr_json",
		Setup: hsrWriteSetupHs0, Floor: 8, Implemented: true,
	},
	{
		Name: "-d link show dev nt0", Slug: "link_show_details_netkit",
		Setup: netkitReadSetupNt0, Floor: 6, Implemented: true,
	},
	{
		Name: "-d -j link show dev nt0", Slug: "link_show_details_netkit_json",
		Setup: netkitReadSetupNt0, Floor: 6, Implemented: true,
	},
	{
		Name: "-d link show dev vr0", Slug: "link_show_details_vrf",
		Setup: vrfWriteSetupVr0, Floor: 4, Implemented: true,
	},
	{
		Name: "-d -j link show dev vr0", Slug: "link_show_details_vrf_json",
		Setup: vrfWriteSetupVr0, Floor: 4, Implemented: true,
	},
	{
		Name: "-d link show dev d0", Slug: "link_show_details_vrf_slave",
		Setup: vrfSlaveWriteSetupD0Enslaved, Floor: 6, Implemented: true,
	},
	{
		Name: "-d -j link show dev d0", Slug: "link_show_details_vrf_slave_json",
		Setup: vrfSlaveWriteSetupD0Enslaved, Floor: 6, Implemented: true,
	},
	{
		Name: "-d link show dev vc0", Slug: "link_show_details_vxcan",
		Setup: vxcanWriteSetupVc0, Floor: 6, Implemented: true,
	},
	{
		Name: "-d -j link show dev vc0", Slug: "link_show_details_vxcan_json",
		Setup: vxcanWriteSetupVc0, Floor: 6, Implemented: true,
	},
	{
		Name: "-d link show dev bat0", Slug: "link_show_details_batadv",
		Setup: batadvReadSetupBat0, Floor: 4, Implemented: true,
	},
	{
		Name: "-d -j link show dev bat0", Slug: "link_show_details_batadv_json",
		Setup: batadvReadSetupBat0, Floor: 4, Implemented: true,
	},
	{
		Name: "-d link show dev ib0", Slug: "link_show_details_ifb",
		Setup: ifbWriteSetupIb0, Floor: 4, Implemented: true,
	},
	{
		Name: "-d -j link show dev ib0", Slug: "link_show_details_ifb_json",
		Setup: ifbWriteSetupIb0, Floor: 4, Implemented: true,
	},
	{
		Name: "-d link show dev nm0", Slug: "link_show_details_nlmon",
		Setup: nlmonWriteSetupNm0, Floor: 4, Implemented: true,
	},
	{
		Name: "-d -j link show dev nm0", Slug: "link_show_details_nlmon_json",
		Setup: nlmonWriteSetupNm0, Floor: 4, Implemented: true,
	},
	{
		Name: "-d link show dev vn0", Slug: "link_show_details_vcan",
		Setup: vcanWriteSetupVn0, Floor: 4, Implemented: true,
	},
	{
		Name: "-d -j link show dev vn0", Slug: "link_show_details_vcan_json",
		Setup: vcanWriteSetupVn0, Floor: 4, Implemented: true,
	},
	{
		Name: "-d link show dev tm0", Slug: "link_show_details_team",
		Setup: teamWriteSetupTm0, Floor: 4, Implemented: true,
	},
	{
		Name: "-d -j link show dev tm0", Slug: "link_show_details_team_json",
		Setup: teamWriteSetupTm0, Floor: 4, Implemented: true,
	},
	{
		Name: "-d link show d0", Slug: "link_show_details_team_slave",
		Setup: teamSlaveReadSetupD0, Floor: 6, Implemented: true,
	},
	{
		Name: "-d -j link show d0", Slug: "link_show_details_team_slave_json",
		Setup: teamSlaveReadSetupD0, Floor: 6, Implemented: true,
	},
	// The route write rows (route-write-path.md phase 5): the corpus in
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
	// The GW route rows (goip-write-keywords.md, "GW5-T01 the route rows"):
	// for each route keyword form GW1 built, the first workbench row by id
	// in pkg/xtcpnl/testdata/7_3_0/ws/ that O5 finds the same, once with ip
	// rc 0 and once with rc 2, its pre as Setup and its ip.pcap record count
	// as Floor. TestGwRouteMatrixRows derives each from the corpus.
	{
		Name: "-4 route prepend unicast 0.0.0.0/0 table 255 proto 255 scope global nhid 10", Slug: "gw_route4_pw_023",
		// route4/pw-023: nhid_v4_unicast (rc 0).
		Floor: 2, Implemented: true, Write: true, Setup: gwWsSetupNh4,
	},
	{
		Name: "-4 route add blackhole 198.51.100.7/32 table 4294967295 nhid 10", Slug: "gw_route4_ec_006",
		// route4/ec-006: nhid_v4_blackhole (rc 0).
		Floor: 2, Implemented: true, Write: true, Setup: gwWsSetupNh4,
	},
	{
		Name: "-6 route add blackhole 2001:db8:2::7/128 table 4294967295 nhid 10", Slug: "gw_route6_ec_006",
		// route6/ec-006: nhid_v6 (rc 0).
		Floor: 2, Implemented: true, Write: true, Setup: gwWsSetupNh6,
	},
	{
		Name: "-6 route del prohibit 8000::/1 table main metric 1024 pref low mtu lock 1400 nhid 10", Slug: "gw_route6_gone_3",
		// route6/gone-3: nhid_v6 (rc 2), pref_low (rc 2).
		Floor: 2, Implemented: true, Write: true, Setup: gwWsSetupNh6,
	},
	{
		Name: "-6 route replace unicast default table main metric 0 proto static pref low expires 60 mtu 1280 src 2001:db8:1::10 via 2001:db8:1::1 dev d0", Slug: "gw_route6_ec_002",
		// route6/ec-002: pref_low (rc 0), expires (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route append local ::/0 table 100 metric 1024 proto 255 pref medium mtu lock 1400 via 2001:db8:1::1", Slug: "gw_route6_ec_003",
		// route6/ec-003: pref_medium (rc 0).
		Floor: 2, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route del unreachable ::/0 preference 7 proto 0 pref medium advmss 1300 via 2001:db8:1::1", Slug: "gw_route6_pw_030",
		// route6/pw-030: pref_medium (rc 2).
		Floor: 2, Implemented: true, Write: true, Setup: append(slices.Clone(gwWsSetupD0), "-6 route add unreachable ::/0 preference 7 proto 0 pref medium advmss 1300 via 2001:db8:1::1"),
	},
	{
		Name: "-6 route prepend anycast 8000::/1 table 255 metric 4294967295 proto 0 pref high advmss 1300 via 2001:db8:3::1 dev d0 onlink", Slug: "gw_route6_ec_004",
		// route6/ec-004: pref_high (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route del blackhole default table 256 metric 4294967295 proto 255 pref low expires 60 dev d0", Slug: "gw_route6_pw_029",
		// route6/pw-029: expires (rc 2).
		Floor: 4, Implemented: true, Write: true, Setup: append(slices.Clone(gwWsSetupD0), "-6 route add blackhole default table 256 metric 4294967295 proto 255 pref low expires 60 dev d0"),
	},
	{
		Name: "-4 route add multicast 198.51.100.6/31 table 256 preference 7 scope global mtu lock 1400 src 192.0.2.10 nexthop via 192.0.2.1 dev d0 weight 1 nexthop via 192.0.2.2 dev d0 weight 256", Slug: "gw_route4_pw_004",
		// route4/pw-004: mp_two_hops (rc 0), mp_weights_1_256 (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-4 route del multicast 198.51.100.6/31 table 256 preference 7 nexthop via 192.0.2.1 dev d0 weight 1 nexthop via 192.0.2.2 dev d0 weight 256", Slug: "gw_route4_ec_005",
		// route4/ec-005: mp_two_hops (rc 2), mp_weights_1_256 (rc 2).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
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

	// The kwlive rows (goip-live-forms.md, LF3-T01): as the GW rows, for the
	// keyword forms GW5 found no row for, from the workbench's kwlive
	// family, whose setups are ip alone. They are the last route rows: the
	// kwnh rows' encap mpls autoloads mpls_router and mpls_iptunnel, which
	// add AF_MPLS to netconf and afstats replies, and every row after them
	// is generic netlink or XFRM.
	{
		Name: "-4 route add 198.51.100.0/24 dev d0 tos 0x10", Slug: "gw_kwroute4_ec_001",
		// kwroute4/ec-001: tos (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-4 route add 198.51.100.0/24 dev d0 tos 0x01", Slug: "gw_kwroute4_ng_001",
		// kwroute4/ng-001: tos-ecn (rc 2).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-4 route add 198.51.100.0/24 dev d0 dsfield 0x10", Slug: "gw_kwroute4_ec_002",
		// kwroute4/ec-002: dsfield (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:2::/64 dev d0 dsfield 0x10", Slug: "gw_kwroute6_ng_001",
		// kwroute6/ng-001: dsfield (rc 2).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-4 route add 198.51.100.0/24 dev d0 realms 5", Slug: "gw_kwroute4_ec_003",
		// kwroute4/ec-003: realms-to (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-4 route add 198.51.100.0/24 dev d0 realms 1/2", Slug: "gw_kwroute4_ec_004",
		// kwroute4/ec-004: realms-from-to (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-4 route add 198.51.100.0/24 dev d0 vrf vrf0", Slug: "gw_kwroute4_ec_005",
		// kwroute4/ec-005: vrf-100 (rc 0).
		Floor: 6, Implemented: true, Write: true, Setup: gwWsSetupVrf,
	},
	{
		Name: "-4 route add 198.51.100.0/24 dev d0 vrf vrf1", Slug: "gw_kwroute4_ec_006",
		// kwroute4/ec-006: vrf-1111 (rc 0).
		Floor: 6, Implemented: true, Write: true, Setup: gwWsSetupVrf,
	},
	{
		Name: "-4 route add 198.51.100.0/24 nexthop via 203.0.113.1 dev d0 onlink nexthop via 192.0.2.1 dev d0", Slug: "gw_kwroute4_ec_007",
		// kwroute4/ec-007: hop-onlink (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-4 route add 198.51.100.0/24 nexthop via inet6 2001:db8:1::1 dev d0 nexthop via 192.0.2.1 dev d0", Slug: "gw_kwroute4_ec_008",
		// kwroute4/ec-008: hop-via6 (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-4 route add 198.51.100.0/24 nexthop via 192.0.2.1 dev d0 realms 3 nexthop via 192.0.2.2 dev d0", Slug: "gw_kwroute4_ec_009",
		// kwroute4/ec-009: hop-realms (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:2::/64 dev d0 pref 1", Slug: "gw_kwroute6_ec_001",
		// kwroute6/ec-001: pref-1 (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:2::/64 dev d0 pref 2", Slug: "gw_kwroute6_dup_2",
		// kwroute6/dup-2: pref-2 (rc 2).
		Floor: 4, Implemented: true, Write: true, Setup: append(slices.Clone(gwWsSetupD0), "-6 route add 2001:db8:2::/64 dev d0 pref 2"),
	},
	{
		Name: "link set group 5 mtu 1400", Slug: "gw_kwlink_ec_001",
		// kwlink/ec-001: group (rc 0).
		Floor: 2, Implemented: true, Write: true, Setup: gwWsKwLink,
	},
	{
		Name: "link set d0 xdpoffload off", Slug: "gw_kwlink_ec_003",
		// kwlink/ec-003: xdpoffload (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsKwLink,
	},
	{
		Name: "link add d7 index 77 type dummy", Slug: "gw_kwlink_ec_004",
		// kwlink/ec-004: index (rc 0).
		Floor: 2, Implemented: true, Write: true, Setup: gwWsKwLink,
	},
	{
		Name: "link add d8 index 1 type dummy", Slug: "gw_kwlink_ng_001",
		// kwlink/ng-001: index-used (rc 2).
		Floor: 2, Implemented: true, Write: true, Setup: gwWsKwLink,
	},
	{
		Name: "-4 rule add pref 200 realms 1/2 table 100", Slug: "gw_kwrule_ec_001",
		// kwrule/ec-001: realms-from-to (rc 0).
		Floor: 2, Implemented: true, Write: true,
	},
	{
		Name: "nexthop add id 55 via 192.0.2.1 dev d0 hw_stats off", Slug: "gw_kwnh_ng_001",
		// kwnh/ng-001: hw-stats-off (rc 2).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "nexthop add id 51 encap mpls 101 via 192.0.2.1 dev d0", Slug: "gw_kwnh_ec_001",
		// kwnh/ec-001: mpls (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "nexthop add id 52 encap mpls 101/102 via 192.0.2.1 dev d0", Slug: "gw_kwnh_ec_002",
		// kwnh/ec-002: mpls-two (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "nexthop add id 53 encap mpls 101 ttl 10 via 192.0.2.1 dev d0", Slug: "gw_kwnh_ec_003",
		// kwnh/ec-003: mpls-ttl (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "nexthop add id 54 encap mpls 101 via 2001:db8:1::1 dev d0", Slug: "gw_kwnh_ec_004",
		// kwnh/ec-004: mpls-v6 (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},

	// The lwt rows (goip-lwt-encap.md, LE4-T01): as the kwlive rows, for
	// the encap forms, from the workbench's lwt family, whose setups are
	// GW5's d0. They follow the kwnh rows, whose encap mpls has autoloaded
	// mpls_router and mpls_iptunnel by then. The four rc 255 forms are
	// refusals goip exits 1 for, so they are O5's alone.
	{
		Name: "-4 route add 198.51.100.0/24 encap mpls 100 via 192.0.2.1 dev d0", Slug: "gw_lwtroute4_ec_001",
		// lwtroute4/ec-001: mpls (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-4 route add 198.51.100.0/24 encap mpls 3 via 192.0.2.1 dev d0", Slug: "gw_lwtroute4_ng_001",
		// lwtroute4/ng-001: mpls-implicit-null (rc 2).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-4 route add 198.51.100.0/24 encap mpls 100/200 ttl 10 via 192.0.2.1 dev d0", Slug: "gw_lwtroute4_ec_002",
		// lwtroute4/ec-002: mpls-stack-ttl (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-4 route add 198.51.100.0/24 encap mpls 100 ttl 0 via 192.0.2.1 dev d0", Slug: "gw_lwtroute4_ec_003",
		// lwtroute4/ec-003: mpls-ttl-0 (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-4 route add 198.51.100.0/24 encap ip id 1 dst 203.0.113.5 dev d0", Slug: "gw_lwtroute4_ec_004",
		// lwtroute4/ec-004: ip (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-4 route add 198.51.100.0/24 encap ip id 1 dst 203.0.113.5 src 192.0.2.10 tos 0x10 ttl 64 key csum seq dev d0", Slug: "gw_lwtroute4_ec_005",
		// lwtroute4/ec-005: ip-every (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-4 route add 198.51.100.0/24 encap ip id 1 dst 203.0.113.5 geneve_opts 0x0102:0x80:00112233 dev d0", Slug: "gw_lwtroute4_ec_006",
		// lwtroute4/ec-006: ip-geneve (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-4 route add 198.51.100.0/24 encap ip id 1 dst 203.0.113.5 geneve_opts 0x0102:0x80:001122 dev d0", Slug: "gw_lwtroute4_ng_002",
		// lwtroute4/ng-002: geneve-data-3 (rc 2).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-4 route add 198.51.100.0/24 encap ip id 1 dst 203.0.113.5 geneve_opts 0102:80:00112233 dev d0", Slug: "gw_lwtroute4_ec_007",
		// lwtroute4/ec-007: ip-geneve-base0 (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-4 route add 198.51.100.0/24 encap ip id 1 dst 203.0.113.5 geneve_opts 0x0102:0x80:00112233,0x0102:0x81:44556677 dev d0", Slug: "gw_lwtroute4_ec_008",
		// lwtroute4/ec-008: ip-geneve-two (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-4 route add 198.51.100.0/24 encap ip id 1 dst 203.0.113.5 vxlan_opts 0x800 dev d0", Slug: "gw_lwtroute4_ec_009",
		// lwtroute4/ec-009: ip-vxlan (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-4 route add 198.51.100.0/24 encap ip id 1 dst 203.0.113.5 erspan_opts 1:0x1234:: dev d0", Slug: "gw_lwtroute4_ec_010",
		// lwtroute4/ec-010: ip-erspan (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:2::/64 encap ip6 id 1 dst 2001:db8:9::1 dev d0", Slug: "gw_lwtroute6_ec_001",
		// lwtroute6/ec-001: ip6 (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:2::/64 encap ip6 id 1 dst 2001:db8:9::1 src 2001:db8:1::10 tc 0x10 hoplimit 64 key csum seq dev d0", Slug: "gw_lwtroute6_ec_002",
		// lwtroute6/ec-002: ip6-every (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:2::/64 encap ip6 id 1 dst 2001:db8:9::1 geneve_opts 0x0102:0x80:00112233 dev d0", Slug: "gw_lwtroute6_ec_003",
		// lwtroute6/ec-003: ip6-geneve (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:2::/64 encap mpls 100 via 2001:db8:1::1 dev d0", Slug: "gw_lwtroute6_ec_004",
		// lwtroute6/ec-004: mpls (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:2::/64 encap ip id 1 dst 203.0.113.5 dev d0", Slug: "gw_lwtroute6_ec_005",
		// lwtroute6/ec-005: ip-on-v6 (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-4 route add 198.51.100.0/24 nexthop encap mpls 100 via 192.0.2.1 dev d0 nexthop via 192.0.2.2 dev d0", Slug: "gw_lwthop_ec_001",
		// lwthop/ec-001: hop-mpls (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-4 route add 198.51.100.0/24 nexthop encap ip id 1 dst 203.0.113.5 via 192.0.2.1 dev d0 nexthop encap ip id 2 dst 203.0.113.6 via 192.0.2.2 dev d0", Slug: "gw_lwthop_ec_002",
		// lwthop/ec-002: hops-ip (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:2::/64 nexthop encap ip6 dst 2001:db8:9::1 via 2001:db8:1::1 dev d0 nexthop via 2001:db8:1::2 dev d0", Slug: "gw_lwthop_ec_003",
		// lwthop/ec-003: hop-ip6 (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "nexthop add id 61 encap ip id 1 dst 203.0.113.5 via 192.0.2.1 dev d0", Slug: "gw_lwtnh_ec_001",
		// lwtnh/ec-001: ip (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "nexthop add id 62 encap ip6 dst 2001:db8:9::1 via 2001:db8:1::1 dev d0", Slug: "gw_lwtnh_ec_002",
		// lwtnh/ec-002: ip6 (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "nexthop add id 63 encap ip id 1 dst 203.0.113.5 geneve_opts 0x0102:0x80:00112233 via 192.0.2.1 dev d0", Slug: "gw_lwtnh_ec_003",
		// lwtnh/ec-003: ip-geneve (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},

	// The lwtx rows (goip-lwt-srv6.md, LX4-T01): as the lwt rows, for the
	// seg6, seg6local, ila, xfrm, ioam6 and rpl forms, from the workbench's
	// lwtx family, whose setups are GW5's d0. ioam6 and rpl are refused by
	// a guest kernel without them, after ip and goip send the same request,
	// so they are rc 2 rows. The twelve rc 255 forms and ila's Bad locator
	// (rc 1) are refusals goip exits 1 for, so they are O5's alone.
	{
		Name: "-6 route add 2001:db8:2::/64 encap seg6 mode encap segs 2001:db8:9::1 dev d0", Slug: "gw_seg6route6_ec_001",
		// seg6route6/ec-001: encap (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:2::/64 encap seg6 mode encap segs 2001:db8:9::1,2001:db8:9::2,2001:db8:9::3 dev d0", Slug: "gw_seg6route6_ec_002",
		// seg6route6/ec-002: encap-three (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:2::/64 encap seg6 mode inline segs 2001:db8:9::1 dev d0", Slug: "gw_seg6route6_ec_003",
		// seg6route6/ec-003: inline (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:2::/64 encap seg6 mode l2encap segs 2001:db8:9::1 dev d0", Slug: "gw_seg6route6_ec_004",
		// seg6route6/ec-004: l2encap (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:2::/64 encap seg6 mode encap.red segs 2001:db8:9::1,2001:db8:9::2,2001:db8:9::3 dev d0", Slug: "gw_seg6route6_ec_005",
		// seg6route6/ec-005: encap-red (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:2::/64 encap seg6 mode l2encap.red segs 2001:db8:9::1 dev d0", Slug: "gw_seg6route6_ec_006",
		// seg6route6/ec-006: l2encap-red (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:2::/64 encap seg6 mode encap segs 2001:db8:9::1 hmac 7 dev d0", Slug: "gw_seg6route6_ec_007",
		// seg6route6/ec-007: hmac (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:2::/64 encap seg6 mode encap tunsrc 2001:db8:1::10 segs 2001:db8:9::1 dev d0", Slug: "gw_seg6route6_ec_008",
		// seg6route6/ec-008: tunsrc (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:2::/64 encap seg6 mode encap segs 2001:db8:9::1 hmac x dev d0", Slug: "gw_seg6route6_ec_009",
		// seg6route6/ec-009: hmac-x (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:2::/64 encap seg6 hmac 1 dev d0", Slug: "gw_seg6route6_ng_001",
		// seg6route6/ng-001: no-mode (rc 2).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-4 route add 198.51.100.0/24 encap seg6 mode encap segs 2001:db8:9::1 dev d0", Slug: "gw_seg6route4_ec_001",
		// seg6route4/ec-001: encap (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-4 route add 198.51.100.0/24 encap seg6 mode inline segs 2001:db8:9::1 dev d0", Slug: "gw_seg6route4_ng_001",
		// seg6route4/ng-001: inline (rc 2).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:5::/64 encap seg6local action End dev d0", Slug: "gw_seg6local_ec_001",
		// seg6local/ec-001: end (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:5::/64 encap seg6local action End count dev d0", Slug: "gw_seg6local_ec_002",
		// seg6local/ec-002: end-count (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:5::/64 encap seg6local action End flavors next-csid lblen 32 nflen 16 dev d0", Slug: "gw_seg6local_ec_003",
		// seg6local/ec-003: end-csid (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:5::/64 encap seg6local action End flavors psp dev d0", Slug: "gw_seg6local_ec_004",
		// seg6local/ec-004: end-psp (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:5::/64 encap seg6local action End flavors usp dev d0", Slug: "gw_seg6local_ng_001",
		// seg6local/ng-001: end-usp (rc 2).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:5::/64 encap seg6local action End.X nh6 2001:db8:1::1 oif d0 dev d0", Slug: "gw_seg6local_ec_005",
		// seg6local/ec-005: end-x (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:5::/64 encap seg6local action End.X dev d0", Slug: "gw_seg6local_ng_002",
		// seg6local/ng-002: end-x-bare (rc 2).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:5::/64 encap seg6local action End.T table 100 dev d0", Slug: "gw_seg6local_ec_006",
		// seg6local/ec-006: end-t (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:5::/64 encap seg6local action End.DX2 oif d0 dev d0", Slug: "gw_seg6local_ec_007",
		// seg6local/ec-007: end-dx2 (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:5::/64 encap seg6local action End.DX6 nh6 2001:db8:1::1 dev d0", Slug: "gw_seg6local_ec_008",
		// seg6local/ec-008: end-dx6 (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:5::/64 encap seg6local action End.DX4 nh4 192.0.2.1 dev d0", Slug: "gw_seg6local_ec_009",
		// seg6local/ec-009: end-dx4 (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:5::/64 encap seg6local action End.DT6 table 100 dev d0", Slug: "gw_seg6local_ec_010",
		// seg6local/ec-010: end-dt6 (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:5::/64 encap seg6local action End.DT4 vrftable 100 dev d0", Slug: "gw_seg6local_ng_003",
		// seg6local/ng-003: end-dt4 (rc 2).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:5::/64 encap seg6local action End.B6 srh segs 2001:db8:9::1,2001:db8:9::2,2001:db8:9::3 dev d0", Slug: "gw_seg6local_ec_011",
		// seg6local/ec-011: end-b6 (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:5::/64 encap seg6local action End.B6.Encaps srh segs 2001:db8:9::1 hmac 7 dev d0", Slug: "gw_seg6local_ec_012",
		// seg6local/ec-012: end-b6-encaps (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:0:1::/64 encap ila 2001:0:0:2 ident-type luid dev d0", Slug: "gw_ila_ec_001",
		// ila/ec-001: luid (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:0:1::/64 encap ila 2001:0:0:2 dev d0", Slug: "gw_ila_ng_001",
		// ila/ng-001: use-format (rc 2).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:0:1::/64 encap ila 2001:0:0:2 csum-mode no-action ident-type luid hook-type output dev d0", Slug: "gw_ila_ec_002",
		// ila/ec-002: every (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:0:1::/64 encap ila 2001:0:0:2 csum-mode adj-transport ident-type luid dev d0", Slug: "gw_ila_ec_003",
		// ila/ec-003: adj-transport (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-4 route add 198.51.100.0/24 encap ila 2001:0:0:2 ident-type luid dev d0", Slug: "gw_ila_ng_002",
		// ila/ng-002: ipv4 (rc 2).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:2::/64 dev d0 encap xfrm if_id 1", Slug: "gw_xfrm_ec_001",
		// xfrm/ec-001: if-id (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:2::/64 encap xfrm if_id 1 dev d0", Slug: "gw_xfrm_ng_001",
		// xfrm/ng-001: swallowed (rc 2).
		Floor: 2, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:2::/64 dev d0 encap xfrm if_id 1 link_dev d0", Slug: "gw_xfrm_ec_002",
		// xfrm/ec-002: link-dev (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-4 route add 198.51.100.0/24 dev d0 encap xfrm if_id 7", Slug: "gw_xfrm_ec_003",
		// xfrm/ec-003: ipv4 (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:2::/64 encap ioam6 trace prealloc type 0x800000 ns 1 size 12 dev d0", Slug: "gw_lwtxreq_ec_001",
		// lwtxreq/ec-001: ioam6-inline, rc 2 (ENOTSUP) in that corpus's stock
		// kernel; rc 0 on the parity guest's (NX9-T01), as ioam6route/ec-001.
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:2::/64 encap ioam6 freq 1/2 mode encap tunsrc 2001:db8:1::10 tundst 2001:db8:9::1 trace prealloc type 0x800000 ns 1 size 12 dev d0", Slug: "gw_lwtxreq_ec_002",
		// lwtxreq/ec-002: ioam6-encap, rc 2 there; rc 0 here, as ioam6route/ec-002.
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:2::/64 encap rpl segs 2001:db8:9::1,2001:db8:9::2 dev d0", Slug: "gw_lwtxreq_ec_006",
		// lwtxreq/ec-006: rpl, rc 2 there; rc 0 here (NX9-T01).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	// NX9-T03 (goip-parity-next.md): the lwtenc corpus's rpl and ioam6
	// writes, which install on the parity guest's kernel (NX9-T01). The bpf
	// writes follow (NX10b).
	{
		Name: "-6 route add 2001:db8:2::/64 encap rpl segs 2001:db8:9::1 dev d0", Slug: "gw_rplroute_ec_001",
		// rplroute/ec-001: one seg (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:2::/64 encap rpl segs 2001:db8:9::1,2001:db8:9::2,2001:db8:9::3 dev d0", Slug: "gw_rplroute_ec_002",
		// rplroute/ec-002: three segs (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:2::/64 encap rpl dev d0", Slug: "gw_rplroute_ec_003",
		// rplroute/ec-003: no segs, an empty SRH (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:2::/64 encap ioam6 mode encap tundst 2001:db8:9::1 trace prealloc type 0x800000 ns 1 size 12 dev d0", Slug: "gw_ioam6route_ec_003",
		// ioam6route/ec-003: encap mode, no tunsrc (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:2::/64 encap ioam6 mode auto tundst 2001:db8:9::1 trace prealloc type 0x800000 ns 1 size 12 dev d0", Slug: "gw_ioam6route_ec_004",
		// ioam6route/ec-004: auto mode (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:2::/64 encap ioam6 freq 1000000/1000000 trace prealloc type 0x800000 ns 1 size 12 dev d0", Slug: "gw_ioam6route_ec_006",
		// ioam6route/ec-006: the largest freq (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:2::/64 encap ioam6 trace prealloc type 0x800000 ns 1 size 4 dev d0", Slug: "gw_ioam6route_ec_007",
		// ioam6route/ec-007: the smallest size (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:2::/64 encap ioam6 trace prealloc type 0x800000 ns 1 size 244 dev d0", Slug: "gw_ioam6route_ec_008",
		// ioam6route/ec-008: the largest size (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:2::/64 encap ioam6 trace prealloc type 0x800000 ns 65535 size 12 dev d0", Slug: "gw_ioam6route_ec_010",
		// ioam6route/ec-010: the largest ns (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 nexthop add id 81 encap rpl segs 2001:db8:9::1 dev d0", Slug: "gw_lwtencnh_ec_001",
		// lwtencnh/ec-001: rpl on a nexthop (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 nexthop add id 82 encap ioam6 trace prealloc type 0x800000 ns 1 size 12 dev d0", Slug: "gw_lwtencnh_ec_002",
		// lwtencnh/ec-002: ioam6 on a nexthop (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:2::/64 nexthop via 2001:db8:1::1 dev d0 encap rpl segs 2001:db8:9::1 nexthop via 2001:db8:1::2 dev d0", Slug: "gw_lwtenchop_ec_001",
		// lwtenchop/ec-001: rpl on a multipath hop (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:2::/64 nexthop via 2001:db8:1::1 encap ioam6 trace prealloc type 0x800000 ns 1 size 12 dev d0 nexthop via 2001:db8:1::2 dev d0", Slug: "gw_lwtenchop_ec_002",
		// lwtenchop/ec-002: ioam6 on a multipath hop (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	// NX10b (goip-parity-next.md): the lwtenc corpus's bpf writes, which goip
	// loads with cilium/ebpf (NX10a). The parity guest links the object at
	// /run/nlcap-ws/lwt_bpf.o; nlparity zeroes LWT_BPF_PROG_FD, the two
	// processes' own descriptors. ip's refusals before sending (rc 255) are
	// not rows.
	{
		Name: "-4 route add 198.51.100.0/24 encap bpf in obj /run/nlcap-ws/lwt_bpf.o sec lwt_in dev d0", Slug: "gw_bpfroute_ec_001",
		// bpfroute/ec-001: an in prog (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-4 route add 198.51.100.0/24 encap bpf out obj /run/nlcap-ws/lwt_bpf.o sec lwt_out dev d0", Slug: "gw_bpfroute_ec_002",
		// bpfroute/ec-002: an out prog (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-4 route add 198.51.100.0/24 encap bpf xmit obj /run/nlcap-ws/lwt_bpf.o sec lwt_xmit dev d0", Slug: "gw_bpfroute_ec_003",
		// bpfroute/ec-003: an xmit prog (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-4 route add 198.51.100.0/24 encap bpf in obj /run/nlcap-ws/lwt_bpf.o sec lwt_in out obj /run/nlcap-ws/lwt_bpf.o sec lwt_out xmit obj /run/nlcap-ws/lwt_bpf.o sec lwt_xmit headroom 16 dev d0", Slug: "gw_bpfroute_ec_004",
		// bpfroute/ec-004: all three and a headroom (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-4 route add 198.51.100.0/24 encap bpf xmit obj /run/nlcap-ws/lwt_bpf.o sec lwt_xmit headroom 1 dev d0", Slug: "gw_bpfroute_ec_005",
		// bpfroute/ec-005: the smallest headroom (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-4 route add 198.51.100.0/24 encap bpf xmit obj /run/nlcap-ws/lwt_bpf.o sec lwt_xmit headroom 256 dev d0", Slug: "gw_bpfroute_ec_006",
		// bpfroute/ec-006: the largest headroom the kernel takes (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-4 route add 198.51.100.0/24 encap bpf in obj /run/nlcap-ws/lwt_bpf.o sec lwt_xmit dev d0", Slug: "gw_bpfroute_ec_007",
		// bpfroute/ec-007: lwt_xmit's prog on the in hook, retyped (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-4 route add 198.51.100.0/24 encap bpf in obj /run/nlcap-ws/lwt_bpf.o sec lwt_in in obj /run/nlcap-ws/lwt_bpf.o sec lwt_out dev d0", Slug: "gw_bpfroute_ec_008",
		// bpfroute/ec-008: a repeated in, both sent; the kernel keeps the last (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-4 route add 198.51.100.0/24 encap bpf xmit obj /run/nlcap-ws/lwt_bpf.o sec lwt_xmit headroom 8 headroom 9 dev d0", Slug: "gw_bpfroute_ec_009",
		// bpfroute/ec-009: two headrooms, the first sent (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-4 route add 198.51.100.0/24 encap bpf xmit obj /run/nlcap-ws/lwt_bpf.o sec lwt_xmit headroom 257 dev d0", Slug: "gw_bpfroute_ng_002",
		// bpfroute/ng-002: a headroom past LWT_BPF_MAX_HEADROOM, ERANGE (rc 2).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-4 route add 198.51.100.0/24 encap bpf headroom 16 dev d0", Slug: "gw_bpfroute_ng_003",
		// bpfroute/ng-003: a headroom and no prog, EINVAL (rc 2).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 nexthop add id 83 encap bpf in obj /run/nlcap-ws/lwt_bpf.o sec lwt_in dev d0", Slug: "gw_lwtencnh_ec_003",
		// lwtencnh/ec-003: bpf on a nexthop (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 nexthop add id 84 encap bpf xmit obj /run/nlcap-ws/lwt_bpf.o sec lwt_xmit headroom 32 dev d0", Slug: "gw_lwtencnh_ec_004",
		// lwtencnh/ec-004: bpf xmit and a headroom on a nexthop (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:2::/64 nexthop via 2001:db8:1::1 dev d0 encap bpf in obj /run/nlcap-ws/lwt_bpf.o sec lwt_in nexthop via 2001:db8:1::2 dev d0", Slug: "gw_lwtenchop_ec_003",
		// lwtenchop/ec-003: bpf on a multipath hop (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 route add 2001:db8:2::/64 nexthop via 2001:db8:1::1 dev d0 encap seg6 mode encap segs 2001:db8:9::1 nexthop via 2001:db8:1::2 dev d0", Slug: "gw_lwtxhop_ec_001",
		// lwtxhop/ec-001: hop6 (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-4 route add 198.51.100.0/24 nexthop via 192.0.2.1 dev d0 encap seg6 mode encap segs 2001:db8:9::1 nexthop via 192.0.2.2 dev d0", Slug: "gw_lwtxhop_ec_002",
		// lwtxhop/ec-002: hop4 (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 nexthop add id 71 encap seg6 mode encap segs 2001:db8:9::1 dev d0", Slug: "gw_lwtxnh_ec_001",
		// lwtxnh/ec-001: seg6 (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},
	{
		Name: "-6 nexthop add id 72 encap seg6local action End dev d0", Slug: "gw_lwtxnh_ec_002",
		// lwtxnh/ec-002: seg6local (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: gwWsSetupD0,
	},

	// NX3's MPLS route writes (goip-parity-next.md), each a 7_3_1/ws/mpls
	// row's command, its -f mpls spelled -M so that the row stays in the
	// route section: the same preferred_family, the same bytes. They come
	// last among the route rows because mplsRouteSetup loads mpls_router,
	// which is kernel-wide: every device gains an AF_MPLS netconf entry and
	// afstats, which no row after them reads.
	{
		Name: "-M route add 16 as 200 ttl-propagate enabled via inet6 2001:db8::2 dev d0", Slug: "mpls_route_pw_001",
		// mpls/pw-001: a swap, ttl-propagate before the via (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: mplsRouteSetup,
	},
	{
		Name: "-M route add 100 as to 200 dev d0", Slug: "mpls_route_ec_005",
		// mpls/ec-005: as to (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: mplsRouteSetup,
	},
	{
		Name: "-M route add 100 as 200/300/400 ttl-propagate enabled nexthop via inet 10.0.0.2 dev d0 nexthop via inet6 2001:db8::2 dev d0",
		Slug: "mpls_route_pw_018",
		// mpls/pw-018: a three-label stack over two hops, one lookup (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: mplsRouteSetup,
	},
	{
		Name: "-M route add 100 as 100/101/102/103/104/105/106/107/108/109/110/111/112/113/114/115/116/117/118/119/120/121/122/123/124/125/126/127/128/129 " +
			"nexthop as 300 via inet 10.0.0.2 dev d0 nexthop as 400 via inet 10.0.0.3 dev d0",
		Slug: "mpls_route_ec_007",
		// mpls/ec-007: the kernel's 30-label stack, and each hop's as (rc 0).
		Floor: 4, Implemented: true, Write: true, Setup: mplsRouteSetup,
	},
	{
		Name: "-M route add 15 via inet 10.0.0.2 dev d0", Slug: "mpls_route_ng_001",
		// mpls/ng-001: a reserved label, sent and refused (rc 2).
		Floor: 4, Implemented: true, Write: true, Setup: mplsRouteSetup,
	},
	{
		Name: "-M route del 100", Slug: "mpls_route_del_absent",
		// mpls/del-absent: unicast, scope universe, no lookup; the kernel
		// acks a delete of a label that has no route (rc 0).
		Floor: 2, Implemented: true, Write: true, Setup: mplsRouteSetup,
	},
	{
		Name: "route add 10.1.0.0/24 as 200 via 10.0.0.2", Slug: "mpls_route_inet_as",
		// mpls/inet-as: an IPv4 RTA_NEWDST, which the kernel ignores (rc 0).
		Floor: 2, Implemented: true, Write: true, Setup: mplsRouteSetup,
	},
	// NX3b's MPLS reads, after the writes for the same reason: the dump of
	// mpls/show's three routes, a swap, a multipath and a ttl-propagate, as
	// text and JSON, then a get of the one route mpls/get adds, bare and
	// with as.
	{
		Name: "-M route show", Slug: "mpls_route_show",
		// The dump, its three routes and the done, and d0's lookup by index.
		Floor: 5, Implemented: true, Setup: mplsShowSetup,
	},
	{
		Name: "-j -M route show", Slug: "mpls_route_show_json",
		Floor: 5, Implemented: true, Setup: mplsShowSetup,
	},
	{
		Name: "-M route get 100", Slug: "mpls_route_get",
		// The get, its reply, and d0's lookup by index.
		Floor: 4, Implemented: true, Setup: mplsGetSetup,
	},
	{
		Name: "-M route get 100 as 200", Slug: "mpls_route_get_as",
		Floor: 4, Implemented: true, Setup: mplsGetSetup,
	},

	// G6-T01: tcp_metrics, the C2 corpus's 14 tcpm_* rows on its
	// preconditions (genl-write-corpus.exp). The connections' entries carry
	// each side's own RTTs and ages, which nlparity normalizes, and a dump's
	// order is its namespace's hash, so the replies and a flush's DELs are
	// paired by address and source. delete, and flush of all or of an exact
	// address, send one DEL; a prefix flush sends a dump, a batch of DELs
	// and a second dump. A flush that finds nothing writes nothing, so
	// tcpm_flush_empty is a read row. Two rows' corpus commands are another
	// row's, so they are spelled with ip's other keyword: `list` for show,
	// `address` before the prefix, the same bytes.
	{Name: "tcp_metrics show", Slug: "tcpm_show", Floor: 4, Implemented: true, Proto: ProtoGenericCst,
		Setup: tcpmSetup},
	{Name: "tcp_metrics list", Slug: "tcpm_show_empty", Floor: 4, Implemented: true, Proto: ProtoGenericCst,
		Setup: tcpmAddrsSetup},
	{Name: "-6 tcp_metrics show", Slug: "tcpm_show_v6", Floor: 4, Implemented: true, Proto: ProtoGenericCst,
		Setup: tcpmSetup},
	{Name: "tcp_metrics show address 192.0.2.1", Slug: "tcpm_get", Floor: 4, Implemented: true,
		Proto: ProtoGenericCst, Setup: tcpmSetup},
	{Name: "tcp_metrics show src 192.0.2.2 address 192.0.2.1", Slug: "tcpm_get_src", Floor: 4, Implemented: true,
		Proto: ProtoGenericCst, Setup: tcpmSetup},
	{Name: "tcp_metrics show address 2001:db8::1", Slug: "tcpm_get_v6", Floor: 4, Implemented: true,
		Proto: ProtoGenericCst, Setup: tcpmSetup},
	{Name: "tcp_metrics show address 192.0.2.9", Slug: "tcpm_get_missing", Floor: 4, Implemented: true,
		Proto: ProtoGenericCst, Setup: tcpmSetup},
	{Name: "tcp_metrics delete address 192.0.2.1", Slug: "tcpm_delete", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst, Setup: tcpmSetup},
	{Name: "tcp_metrics delete src 192.0.2.2 address 192.0.2.1", Slug: "tcpm_delete_src", Floor: 4,
		Implemented: true, Write: true, Proto: ProtoGenericCst, Setup: tcpmSetup},
	{Name: "tcp_metrics delete address 192.0.2.9", Slug: "tcpm_delete_missing", Floor: 4, Implemented: true,
		Write: true, Proto: ProtoGenericCst, Setup: tcpmSetup},
	{Name: "tcp_metrics flush all", Slug: "tcpm_flush_all", Floor: 4, Implemented: true, Write: true, Writes: 1,
		Proto: ProtoGenericCst, Setup: tcpmSetup},
	{Name: "tcp_metrics flush 192.0.2.0/24", Slug: "tcpm_flush_prefix", Floor: 6, Implemented: true, Write: true,
		Writes: 2, Proto: ProtoGenericCst, Setup: tcpmSetup},
	{Name: "-6 tcp_metrics flush", Slug: "tcpm_flush_v6", Floor: 6, Implemented: true, Write: true, Writes: 2,
		Proto: ProtoGenericCst, Setup: tcpmSetup},
	{Name: "tcp_metrics flush address 192.0.2.0/24", Slug: "tcpm_flush_empty", Floor: 4, Implemented: true,
		Proto: ProtoGenericCst, Setup: tcpmAddrsSetup},

	// G6-T02: sr, the C2 corpus's 11 sr_* rows on its preconditions
	// (genl-write-corpus.exp). The keys a row starts from are made by an
	// `hmac KEYID ALGO SECRET` step, and a set reads its secret on stdin, as
	// the corpus's did, so a delete is a blank line. The reads with no
	// precondition read the dump topology, which has no key and no tunnel
	// source. Three rows' corpus commands are another row's, so they are
	// spelled with ip's abbreviations, the same bytes: `s` and `hm` for the
	// shows of no key and of the 64-byte secret, `se` for the delete.
	{Name: "sr hmac s", Slug: "sr_hmac_show_empty", Floor: 4, Implemented: true, Proto: ProtoGenericCst},
	{Name: "sr hmac show", Slug: "sr_hmac_show", Floor: 4, Implemented: true, Proto: ProtoGenericCst,
		Setup: srHmacTwoSetup},
	{Name: "sr hm show", Slug: "sr_hmac_show_64", Floor: 4, Implemented: true, Proto: ProtoGenericCst,
		Setup: []string{"hmac 4 sha256 " + strings.Repeat("y", 64)}},
	{Name: "sr tunsrc show", Slug: "sr_tunsrc_show", Floor: 4, Implemented: true, Proto: ProtoGenericCst},
	{Name: "sr hmac set 1 sha256", Slug: "sr_hmac_set", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst, Stdin: stdinLine("s3cret")},
	{Name: "sr hmac set 2 sha1", Slug: "sr_hmac_set_sha1", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst, Stdin: stdinLine("0ther")},
	{Name: "sr hmac set 1 sha1", Slug: "sr_hmac_replace", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst, Setup: srHmacTwoSetup[:1], Stdin: stdinLine("n3w")},
	{Name: "sr hmac se 1 sha256", Slug: "sr_hmac_delete", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst, Setup: srHmacTwoSetup, Stdin: stdinLine("")},
	{Name: "sr hmac set 9 sha256", Slug: "sr_hmac_delete_missing", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst, Stdin: stdinLine("")},
	{Name: "sr hmac set 3 sha256", Slug: "sr_hmac_set_long", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst, Stdin: stdinLine(strings.Repeat("x", 65))},
	{Name: "sr tunsrc set 2001:db8::1", Slug: "sr_tunsrc_set", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst},

	// G6-T03: ioam, the C2 corpus's 18 ioam_* rows that are not the
	// monitor's, on its preconditions (genl-write-corpus.exp), and two -j
	// shows of one bound namespace with data and wide data and its schema.
	// Every keyword is strcmp's, so the three rows whose corpus command is
	// another row's spell the id another way, base 0, the same bytes: 0x1
	// for the duplicate add and the bound schema's del, 01 for the bound
	// namespace's del. The kernel walks its namespaces and schemas in an
	// rhashtable seeded per namespace, so a show of more than one could
	// differ in order between the two sides; each -j show has one. The
	// monitor's three rows are not here: the driver runs no monitor.
	{Name: "ioam namespace show", Slug: "ioam_ns_show_empty", Floor: 4, Implemented: true, Proto: ProtoGenericCst},
	{Name: "ioam schema show", Slug: "ioam_sc_show_empty", Floor: 4, Implemented: true, Proto: ProtoGenericCst},
	{Name: "-j ioam namespace show", Slug: "ioam_ns_show_json", Floor: 4, Implemented: true, Proto: ProtoGenericCst,
		Setup: ioamJSONSetup},
	{Name: "-j ioam schema show", Slug: "ioam_sc_show_json", Floor: 4, Implemented: true, Proto: ProtoGenericCst,
		Setup: ioamJSONSetup},
	{Name: "ioam namespace add 1", Slug: "ioam_ns_add", Floor: 4, Implemented: true, Write: true, Proto: ProtoGenericCst},
	{Name: "ioam namespace add 2 data 42 wide 2a", Slug: "ioam_ns_add_data", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst},
	{Name: "ioam namespace add 3 data 0xffffffff wide ffffffffffffffff", Slug: "ioam_ns_add_unavailable", Floor: 4,
		Implemented: true, Write: true, Proto: ProtoGenericCst},
	{Name: "ioam namespace add 0x1", Slug: "ioam_ns_add_dup", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst, Setup: ioamNs1Setup},
	{Name: "ioam namespace del 1", Slug: "ioam_ns_del", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst, Setup: ioamNs1Setup},
	{Name: "ioam namespace del 9", Slug: "ioam_ns_del_missing", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst},
	{Name: "ioam schema add 1 abcd", Slug: "ioam_sc_add", Floor: 4, Implemented: true, Write: true, Proto: ProtoGenericCst},
	{Name: "ioam schema add 2 abcde", Slug: "ioam_sc_add_pad", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst},
	{Name: "ioam schema add 1 wxyz", Slug: "ioam_sc_add_dup", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst, Setup: ioamBoundSetup[1:2]},
	{Name: "ioam schema del 1", Slug: "ioam_sc_del", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst, Setup: ioamBoundSetup[1:2]},
	{Name: "ioam schema del 9", Slug: "ioam_sc_del_missing", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst},
	{Name: "ioam namespace set 1 schema 1", Slug: "ioam_ns_set_schema", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst, Setup: ioamBoundSetup[:2]},
	{Name: "ioam namespace set 1 schema none", Slug: "ioam_ns_set_none", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst, Setup: ioamBoundSetup},
	{Name: "ioam namespace set 1 schema 9", Slug: "ioam_ns_set_missing", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst, Setup: ioamNs1Setup},
	{Name: "ioam schema del 0x1", Slug: "ioam_sc_del_bound", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst, Setup: ioamBoundSetup},
	{Name: "ioam namespace del 01", Slug: "ioam_ns_del_bound", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst, Setup: ioamBoundSetup},

	// G6-T04: mptcp, the C2 corpus's 25 mptcp_* rows that are not the
	// monitor's, on its preconditions (genl-write-corpus.exp), and three -j
	// shows on the captured ones'. The empty show's corpus command is the
	// populated one's, so it is spelled `s`, the same bytes. A port without
	// signal is refused after the lookup, so its row is a read of floor 2
	// that exits 255 on every side. The kernel keeps its endpoints in a
	// list in the order they were added, so a show of two is in one order
	// on every side. The monitor's two rows are not here: the driver runs
	// no monitor.
	{Name: "mptcp endpoint s", Slug: "mptcp_endpoint_show_empty", Floor: 4, Implemented: true, Proto: ProtoGenericCst},
	{Name: "mptcp endpoint show", Slug: "mptcp_endpoint_show", Floor: 4, Implemented: true, Proto: ProtoGenericCst,
		Setup: mptcpEp2Setup},
	{Name: "mptcp endpoint show id 2", Slug: "mptcp_endpoint_get", Floor: 4, Implemented: true, Proto: ProtoGenericCst,
		Setup: mptcpEp2Setup},
	{Name: "mptcp endpoint show id 9", Slug: "mptcp_endpoint_get_missing", Floor: 4, Implemented: true,
		Proto: ProtoGenericCst, Setup: tcpmAddrsSetup},
	{Name: "mptcp limits show", Slug: "mptcp_limits_show", Floor: 4, Implemented: true, Proto: ProtoGenericCst},
	{Name: "mptcp endpoint add 192.0.2.2 port 10100 subflow", Slug: "mptcp_endpoint_add_port_subflow", Floor: 2,
		Implemented: true, Proto: ProtoGenericCst, Setup: tcpmAddrsSetup},
	{Name: "-j mptcp endpoint show", Slug: "mptcp_endpoint_show_json", Floor: 4, Implemented: true,
		Proto: ProtoGenericCst, Setup: mptcpEp2Setup},
	{Name: "-j mptcp endpoint show id 2", Slug: "mptcp_endpoint_get_json", Floor: 4, Implemented: true,
		Proto: ProtoGenericCst, Setup: mptcpEp2Setup},
	{Name: "-j mptcp limits show", Slug: "mptcp_limits_show_json", Floor: 4, Implemented: true, Proto: ProtoGenericCst},
	{Name: "mptcp endpoint add 192.0.2.1 id 1 subflow", Slug: "mptcp_endpoint_add", Floor: 4, Implemented: true,
		Write: true, Proto: ProtoGenericCst, Setup: tcpmAddrsSetup},
	{Name: "mptcp endpoint add 2001:db8::1 dev d0 signal backup", Slug: "mptcp_endpoint_add_v6_dev", Floor: 4,
		Implemented: true, Write: true, Proto: ProtoGenericCst, Setup: tcpmAddrsSetup},
	{Name: "mptcp endpoint add 192.0.2.2 port 10100 signal", Slug: "mptcp_endpoint_add_port", Floor: 4,
		Implemented: true, Write: true, Proto: ProtoGenericCst, Setup: tcpmAddrsSetup},
	{Name: "mptcp endpoint add 192.0.2.2 subflow fullmesh", Slug: "mptcp_endpoint_add_fullmesh", Floor: 4,
		Implemented: true, Write: true, Proto: ProtoGenericCst, Setup: tcpmAddrsSetup},
	{Name: "mptcp endpoint add 192.0.2.1 subflow", Slug: "mptcp_endpoint_add_dup", Floor: 4, Implemented: true,
		Write: true, Proto: ProtoGenericCst, Setup: mptcpEp1Setup},
	{Name: "mptcp endpoint add 192.0.2.2 id 1 subflow", Slug: "mptcp_endpoint_add_id_busy", Floor: 4,
		Implemented: true, Write: true, Proto: ProtoGenericCst, Setup: mptcpEp1Setup},
	{Name: "mptcp endpoint change id 1 backup", Slug: "mptcp_endpoint_change", Floor: 4, Implemented: true,
		Write: true, Proto: ProtoGenericCst, Setup: mptcpEp1Setup},
	{Name: "mptcp endpoint change 192.0.2.1 fullmesh", Slug: "mptcp_endpoint_change_addr", Floor: 4,
		Implemented: true, Write: true, Proto: ProtoGenericCst, Setup: mptcpEp1Setup},
	{Name: "mptcp endpoint change id 1 nobackup", Slug: "mptcp_endpoint_change_nobackup", Floor: 4,
		Implemented: true, Write: true, Proto: ProtoGenericCst, Setup: mptcpEp1BackupSetup},
	{Name: "mptcp endpoint change id 9 backup", Slug: "mptcp_endpoint_change_missing", Floor: 4,
		Implemented: true, Write: true, Proto: ProtoGenericCst, Setup: tcpmAddrsSetup},
	{Name: "mptcp endpoint delete id 1", Slug: "mptcp_endpoint_delete", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst, Setup: mptcpEp2Setup},
	{Name: "mptcp endpoint delete id 9", Slug: "mptcp_endpoint_delete_missing", Floor: 4, Implemented: true,
		Write: true, Proto: ProtoGenericCst, Setup: tcpmAddrsSetup},
	{Name: "mptcp endpoint delete id 0 192.0.2.1", Slug: "mptcp_endpoint_delete_id0", Floor: 4, Implemented: true,
		Write: true, Proto: ProtoGenericCst, Setup: mptcpEp1Setup},
	{Name: "mptcp endpoint flush", Slug: "mptcp_endpoint_flush", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst, Setup: mptcpEp2Setup},
	{Name: "mptcp limits set subflows 4 add_addr_accepted 3", Slug: "mptcp_limits_set", Floor: 4, Implemented: true,
		Write: true, Proto: ProtoGenericCst},
	{Name: "mptcp limits set add_addr_accepted 2", Slug: "mptcp_limits_set_one", Floor: 4, Implemented: true,
		Write: true, Proto: ProtoGenericCst},
	{Name: "mptcp limits set", Slug: "mptcp_limits_set_none", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst},
	{Name: "mptcp limits set subflows 8 add_addr_accepted 8", Slug: "mptcp_limits_set_max", Floor: 4,
		Implemented: true, Write: true, Proto: ProtoGenericCst},
	{Name: "mptcp limits set subflows 9", Slug: "mptcp_limits_set_over", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst},

	// G6-T05: fou, the C2 corpus's 21 fou_* rows on their preconditions
	// (genl-write-corpus.exp), and three -j shows: two on a captured one's
	// state, and one on fou_add_dev's, its readback, whose device prints by
	// name. The corpus spells several commands alike on different
	// preconditions; a row's name is its command, so those are spelled by
	// prefix here, the same bytes. Port 0, an unknown device and show with
	// an argument are refused after the lookup, so each is a read of floor
	// 2. The kernel keeps its ports in a list, the newest first, so a show
	// of two is in one order on every side.
	{Name: "fou s", Slug: "fou_show_empty", Floor: 4, Implemented: true, Proto: ProtoGenericCst},
	{Name: "fou show", Slug: "fou_show", Floor: 4, Implemented: true, Proto: ProtoGenericCst, Setup: fou2Setup},
	{Name: "fou show port 5555", Slug: "fou_show_args", Floor: 2, Implemented: true, Proto: ProtoGenericCst},
	{Name: "fou add port 0 gue", Slug: "fou_add_port_zero", Floor: 2, Implemented: true, Proto: ProtoGenericCst},
	{Name: "fou add port 5555 gue local 192.0.2.1 dev nosuch", Slug: "fou_add_unknown_dev", Floor: 2,
		Implemented: true, Proto: ProtoGenericCst, Setup: tcpmAddrsSetup},
	{Name: "-j fou show", Slug: "fou_show_json", Floor: 4, Implemented: true, Proto: ProtoGenericCst,
		Setup: fou2Setup},
	{Name: "-j fou sho", Slug: "fou_show_peer_json", Floor: 4, Implemented: true, Proto: ProtoGenericCst,
		Setup: fouPeerSetup},
	{Name: "-j fou sh", Slug: "fou_show_dev_json", Floor: 4, Implemented: true, Proto: ProtoGenericCst,
		Setup: fouDevSetup},
	{Name: "fou add port 5555 ipproto 4", Slug: "fou_add", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst},
	{Name: "fou add port 5555 gue", Slug: "fou_add_gue", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst},
	{Name: "fou add port 5555 gue -6", Slug: "fou_add_v6", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst},
	{Name: "fou add port 5555 ipproto 4 local 192.0.2.1 peer 192.0.2.2 peer_port 5556", Slug: "fou_add_local_peer",
		Floor: 4, Implemented: true, Write: true, Proto: ProtoGenericCst, Setup: tcpmAddrsSetup},
	{Name: "fou add port 5555 gue -6 local 2001:db8::1 peer 2001:db8::2 peer_port 5556",
		Slug: "fou_add_v6_local_peer", Floor: 4, Implemented: true, Write: true, Proto: ProtoGenericCst,
		Setup: tcpmAddrsSetup},
	{Name: "fou add port 5555 ipproto 4 local 192.0.2.1 dev d0", Slug: "fou_add_dev", Floor: 4, Implemented: true,
		Write: true, Proto: ProtoGenericCst, Setup: tcpmAddrsSetup},
	{Name: "fou add port 5555 ipproto 4 dev d0", Slug: "fou_add_dev_no_local", Floor: 4, Implemented: true,
		Write: true, Proto: ProtoGenericCst, Setup: tcpmAddrsSetup},
	{Name: "fou a port 5555 gue", Slug: "fou_add_dup", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst, Setup: fou1Setup},
	{Name: "fou a port 5555 gue -6", Slug: "fou_add_same_port_v6", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst, Setup: fou1Setup},
	{Name: "fou add ipproto 4", Slug: "fou_add_no_port", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst},
	{Name: "fou add port 5555", Slug: "fou_add_no_type", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst},
	{Name: "fou del port 5555", Slug: "fou_del", Floor: 4, Implemented: true, Write: true, Proto: ProtoGenericCst,
		Setup: fou1Setup},
	{Name: "fou d port 5555", Slug: "fou_del_missing", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst},
	{Name: "fou del port 5555 -6", Slug: "fou_del_wrong_family", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst, Setup: fou1Setup},
	{Name: "fou del port 5555 local 192.0.2.1 peer 192.0.2.2 peer_port 5556", Slug: "fou_del_local_peer", Floor: 4,
		Implemented: true, Write: true, Proto: ProtoGenericCst, Setup: fouPeerSetup},
	{Name: "fou de port 5555", Slug: "fou_del_partial", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst, Setup: fouPeerSetup},

	// G6-T06: ila, the C2 corpus's 15 ila_* rows but its show of two, on
	// their preconditions (genl-write-corpus.exp), a show of one locator
	// to match with two mappings, and three -j shows. The kernel keeps
	// its mappings in an rhashtable seeded per namespace, so a show of two
	// locators to match can differ in order between the sides; one
	// locator's mappings are a list in the kernel's order, the device's
	// first, so that show is in one order on every side. The show of two
	// is replayed. Commands the corpus spells alike are spelled by prefix,
	// the same bytes. An unknown word and show with an argument are
	// refused after the lookup, so each is a read of floor 2.
	{Name: "ila s", Slug: "ila_show_empty", Floor: 4, Implemented: true,
		Proto: ProtoGenericCst},
	{Name: "ila show", Slug: "ila_show_both", Floor: 4, Implemented: true,
		Proto: ProtoGenericCst, Setup: ilaBothSetup},
	{Name: "ila show 2001:db8:1:2", Slug: "ila_show_args", Floor: 2, Implemented: true,
		Proto: ProtoGenericCst},
	{Name: "ila add loc_match 2001:db8:1:2 loc 2001:db8:3:4 bogus", Slug: "ila_add_unknown_word", Floor: 2, Implemented: true,
		Proto: ProtoGenericCst},
	{Name: "-j ila show", Slug: "ila_show_both_json", Floor: 4, Implemented: true,
		Proto: ProtoGenericCst, Setup: ilaBothSetup},
	{Name: "-j ila sho", Slug: "ila_show_dev_json", Floor: 4, Implemented: true,
		Proto: ProtoGenericCst, Setup: ilaDevSetup},
	{Name: "-j ila sh", Slug: "ila_show_opts_json", Floor: 4, Implemented: true,
		Proto: ProtoGenericCst, Setup: ilaOptsSetup},
	{Name: "ila add loc_match 2001:db8:1:2 loc 2001:db8:3:4", Slug: "ila_add", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst},
	{Name: "ila add loc_match 2001:db8:1:2 loc 2001:db8:3:4 dev d0", Slug: "ila_add_dev", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst, Setup: tcpmAddrsSetup},
	{Name: "ila add loc_match 2001:db8:1:2 loc 2001:db8:3:4 csum-mode adj-transport ident-type luid", Slug: "ila_add_opts", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst},
	{Name: "ila add loc_match 2001:db8:1:2 loc 2001:db8:3:4 csum-mode neutral-map-auto ident-type use-format", Slug: "ila_add_auto", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst},
	{Name: "ila a loc_match 2001:db8:1:2 loc 2001:db8:3:4", Slug: "ila_add_dup", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst, Setup: ila1Setup},
	{Name: "ila a loc_match 2001:db8:1:2 loc 2001:db8:3:4 dev d0", Slug: "ila_add_same_match_dev", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst, Setup: ilaAddrsSetup},
	{Name: "ila add loc_match 2001:db8:1:2", Slug: "ila_add_no_loc", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst},
	{Name: "ila del loc_match 2001:db8:1:2", Slug: "ila_del", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst, Setup: ila1Setup},
	{Name: "ila d loc_match 2001:db8:1:2", Slug: "ila_del_missing", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst},
	{Name: "ila del loc_match 2001:db8:1:2 dev d0", Slug: "ila_del_dev", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst, Setup: ilaBothSetup},
	{Name: "ila de loc_match 2001:db8:1:2", Slug: "ila_del_nodev_of_dev", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst, Setup: ilaDevSetup},

	// G6-T07: l2tp, the C2 corpus's 24 l2tp_* rows on their preconditions
	// (genl-write-corpus.exp), and four -j shows: two on a captured show's
	// state, and two on a write row's, its readback. The kernel keeps its
	// tunnels and each tunnel's sessions by id, and names a session's
	// l2tpeth device from a per-namespace count, so a show of two is in one
	// order, with the same names, on every side. Commands the corpus spells
	// alike are spelled by prefix, or with tid for tunnel_id, the same
	// bytes. A bare show and the missing arguments are refused after the
	// lookup, so each is a read of floor 2.
	{Name: "l2tp s tunnel", Slug: "l2tp_tunnel_show_empty", Floor: 4, Implemented: true, Proto: ProtoGenericCst},
	{Name: "l2tp show tunnel", Slug: "l2tp_tunnel_show", Floor: 4, Implemented: true, Proto: ProtoGenericCst,
		Setup: l2tpTun2Setup},
	{Name: "l2tp show tunnel tunnel_id 1", Slug: "l2tp_tunnel_show_id", Floor: 4, Implemented: true,
		Proto: ProtoGenericCst, Setup: l2tpTun2Setup},
	{Name: "l2tp s session", Slug: "l2tp_session_show_empty", Floor: 4, Implemented: true, Proto: ProtoGenericCst,
		Setup: l2tpTunSetup},
	{Name: "l2tp show session", Slug: "l2tp_session_show", Floor: 4, Implemented: true, Proto: ProtoGenericCst,
		Setup: l2tpSess2Setup},
	{Name: "l2tp show session tunnel_id 1 session_id 10", Slug: "l2tp_session_show_id", Floor: 4,
		Implemented: true, Proto: ProtoGenericCst, Setup: l2tpSess2Setup},
	{Name: "l2tp show", Slug: "l2tp_show_bare", Floor: 2, Implemented: true, Proto: ProtoGenericCst},
	{Name: "l2tp add tunnel tunnel_id 1 peer_tunnel_id 2 remote 192.0.2.2 udp_sport 5000 udp_dport 6000",
		Slug: "l2tp_tunnel_add_no_local", Floor: 2, Implemented: true, Proto: ProtoGenericCst, Setup: tcpmAddrsSetup},
	{Name: "l2tp add session tunnel_id 1 session_id 10", Slug: "l2tp_session_add_no_peer", Floor: 2,
		Implemented: true, Proto: ProtoGenericCst, Setup: l2tpTunSetup},
	{Name: "-j l2tp show tunnel", Slug: "l2tp_tunnel_show_json", Floor: 4, Implemented: true,
		Proto: ProtoGenericCst, Setup: l2tpTun2Setup},
	{Name: "-j l2tp sho tunnel", Slug: "l2tp_tunnel_show_v6_json", Floor: 4, Implemented: true,
		Proto: ProtoGenericCst, Setup: l2tpTunV6Setup},
	{Name: "-j l2tp show session", Slug: "l2tp_session_show_json", Floor: 4, Implemented: true,
		Proto: ProtoGenericCst, Setup: l2tpSess2Setup},
	{Name: "-j l2tp sho session", Slug: "l2tp_session_show_opts_json", Floor: 4, Implemented: true,
		Proto: ProtoGenericCst, Setup: l2tpSessOptsSetup},
	{Name: l2tpTun1Cst, Slug: "l2tp_tunnel_add", Floor: 4, Implemented: true, Write: true, Proto: ProtoGenericCst,
		Setup: tcpmAddrsSetup},
	{Name: l2tpTun1Cst + " udp_csum on", Slug: "l2tp_tunnel_add_csum", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst, Setup: tcpmAddrsSetup},
	{Name: l2tpTunV6Cst, Slug: "l2tp_tunnel_add_v6", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst, Setup: tcpmAddrsSetup},
	{Name: l2tpTunIPCst, Slug: "l2tp_tunnel_add_ip", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst, Setup: tcpmAddrsSetup},
	{Name: "l2tp add tunnel tunnel_id 1 peer_tunnel_id 2 local 192.0.2.1 remote 192.0.2.2 udp_sport 5001 udp_dport 6001",
		Slug: "l2tp_tunnel_add_dup", Floor: 4, Implemented: true, Write: true, Proto: ProtoGenericCst,
		Setup: l2tpTunSetup},
	{Name: "l2tp del tunnel tunnel_id 1", Slug: "l2tp_tunnel_del", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst, Setup: l2tpTunSetup},
	{Name: "l2tp del tunnel tunnel_id 9", Slug: "l2tp_tunnel_del_missing", Floor: 4, Implemented: true,
		Write: true, Proto: ProtoGenericCst, Setup: tcpmAddrsSetup},
	{Name: "l2tp d tunnel tunnel_id 1", Slug: "l2tp_tunnel_del_sessions", Floor: 4, Implemented: true,
		Write: true, Proto: ProtoGenericCst, Setup: l2tpSessSetup},
	{Name: l2tpSess1Cst, Slug: "l2tp_session_add", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst, Setup: l2tpTunSetup},
	{Name: l2tpSessOptsCst, Slug: "l2tp_session_add_opts", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst, Setup: l2tpTunSetup},
	{Name: "l2tp a session tunnel_id 1 session_id 10 peer_session_id 20", Slug: "l2tp_session_add_dup", Floor: 4,
		Implemented: true, Write: true, Proto: ProtoGenericCst, Setup: l2tpSessSetup},
	{Name: "l2tp ad session tunnel_id 1 session_id 10 peer_session_id 20", Slug: "l2tp_session_add_dup_ip",
		Floor: 4, Implemented: true, Write: true, Proto: ProtoGenericCst, Setup: l2tpSessIPSetup},
	{Name: "l2tp add session tid 1 session_id 10 peer_session_id 20", Slug: "l2tp_session_add_no_tunnel",
		Floor: 4, Implemented: true, Write: true, Proto: ProtoGenericCst, Setup: tcpmAddrsSetup},
	{Name: "l2tp del session tunnel_id 1 session_id 10", Slug: "l2tp_session_del", Floor: 4, Implemented: true,
		Write: true, Proto: ProtoGenericCst, Setup: l2tpSessSetup},
	{Name: "l2tp del session tunnel_id 1 session_id 99", Slug: "l2tp_session_del_missing", Floor: 4,
		Implemented: true, Write: true, Proto: ProtoGenericCst, Setup: l2tpTunSetup},

	// G6-T08: macsec, the C2 corpus's 32 macsec_* rows on their
	// preconditions (genl-write-corpus.exp), four -j shows and three -s
	// shows, each on a captured show's state or a write row's, its
	// readback. A show is always a dump of every SecY, in ifindex order,
	// which `show DEV` filters, and the SecYs are made down, so their
	// counters stay zero. Commands the corpus spells alike are spelled by
	// prefix. A missing device and the SAs ip refuses itself are refused
	// after the lookup, so each is a read of floor 2. These rows replace
	// the last of G5-T03's stand-ins, one `show` per generic netlink family
	// with `ip` in goip's place, ungated; none remains. They come last: the
	// driver runs nltopo::late_setup, which loads the families' modules,
	// before the first genl row.
	{Name: "macsec show", Slug: "macsec_show_empty", Floor: 4, Implemented: true, Proto: ProtoGenericCst},
	{Name: "macsec s", Slug: "macsec_show", Floor: 4, Implemented: true, Proto: ProtoGenericCst,
		Setup: macsec2Setup},
	{Name: "macsec show macsec1", Slug: "macsec_show_dev", Floor: 4, Implemented: true, Proto: ProtoGenericCst,
		Setup: macsec2Setup},
	{Name: "macsec show nodev0", Slug: "macsec_show_unknown_dev", Floor: 2, Implemented: true,
		Proto: ProtoGenericCst},
	{Name: "macsec add macsec0 tx sa 1 pn 1", Slug: "macsec_txsa_add_no_key", Floor: 2, Implemented: true,
		Proto: ProtoGenericCst, Setup: macsecSetup},
	{Name: "macsec add macsec0 tx sa 1 " + macsecKey1Cst, Slug: "macsec_txsa_add_no_pn", Floor: 2,
		Implemented: true, Proto: ProtoGenericCst, Setup: macsecSetup},
	{Name: "macsec add macsec0 tx sa 4 pn 1 " + macsecKey1Cst, Slug: "macsec_txsa_add_an4", Floor: 2,
		Implemented: true, Proto: ProtoGenericCst, Setup: macsecSetup},
	{Name: "macsec set macsec0 tx sa 0 " + macsecKey1Cst, Slug: "macsec_txsa_set_key", Floor: 2,
		Implemented: true, Proto: ProtoGenericCst, Setup: macsecTxsaSetup},
	{Name: "-j macsec show", Slug: "macsec_show_json", Floor: 4, Implemented: true, Proto: ProtoGenericCst,
		Setup: macsec2Setup},
	{Name: "-j macsec sho", Slug: "macsec_show_txsa_json", Floor: 4, Implemented: true, Proto: ProtoGenericCst,
		Setup: macsecTxsaSetup},
	{Name: "-j macsec sh", Slug: "macsec_show_rxsa_json", Floor: 4, Implemented: true, Proto: ProtoGenericCst,
		Setup: macsecRxsaSetup},
	{Name: "-j macsec show macsec0", Slug: "macsec_show_xpn_json", Floor: 4, Implemented: true,
		Proto: ProtoGenericCst, Setup: macsecXpnTxsaSetup},
	{Name: "-s macsec show", Slug: "macsec_show_stats", Floor: 4, Implemented: true, Proto: ProtoGenericCst,
		Setup: macsec2Setup},
	{Name: "-s macsec sho", Slug: "macsec_show_rxsa_stats", Floor: 4, Implemented: true, Proto: ProtoGenericCst,
		Setup: macsecRxsaSetup},
	{Name: "-s -j macsec show", Slug: "macsec_show_txsa_stats_json", Floor: 4, Implemented: true,
		Proto: ProtoGenericCst, Setup: macsecTxsaSetup},
	{Name: macsecTxsaCst, Slug: "macsec_txsa_add", Floor: 4, Implemented: true, Write: true, Proto: ProtoGenericCst,
		Setup: macsecSetup},
	{Name: "macsec add macsec0 tx sa 3 pn 100 off " + macsecKey1Cst, Slug: "macsec_txsa_add_off", Floor: 4,
		Implemented: true, Write: true, Proto: ProtoGenericCst, Setup: macsecSetup},
	{Name: "macsec a macsec0 tx sa 0 pn 1 on " + macsecKey1Cst, Slug: "macsec_txsa_add_dup", Floor: 4,
		Implemented: true, Write: true, Proto: ProtoGenericCst, Setup: macsecTxsaSetup},
	{Name: "macsec add macsec0 tx sa 1 pn 1 key 01000000000000000000000000000000 " +
		"8181818181818181818181818181818181818181818181818181818181818181", Slug: "macsec_txsa_add_long_key",
		Floor: 4, Implemented: true, Write: true, Proto: ProtoGenericCst, Setup: macsecSetup},
	{Name: "macsec add d0 tx sa 0 pn 1 " + macsecKey1Cst, Slug: "macsec_txsa_add_not_macsec", Floor: 4,
		Implemented: true, Write: true, Proto: ProtoGenericCst, Setup: tcpmAddrsSetup},
	{Name: "macsec set macsec0 tx sa 0 pn 50 off", Slug: "macsec_txsa_set", Floor: 4, Implemented: true,
		Write: true, Proto: ProtoGenericCst, Setup: macsecTxsaSetup},
	{Name: "macsec d macsec0 tx sa 0", Slug: "macsec_txsa_del_active", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst, Setup: macsecTxsaSetup},
	{Name: "macsec del macsec0 tx sa 0", Slug: "macsec_txsa_del", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst, Setup: macsecTxsaOffSetup},
	{Name: "macsec del macsec0 tx sa 2", Slug: "macsec_txsa_del_missing", Floor: 4, Implemented: true,
		Write: true, Proto: ProtoGenericCst, Setup: macsecSetup},
	{Name: macsecRxscCst, Slug: "macsec_rxsc_add", Floor: 4, Implemented: true, Write: true, Proto: ProtoGenericCst,
		Setup: macsecSetup},
	{Name: "macsec add macsec0 rx port 1 address 02:00:00:00:00:01", Slug: "macsec_rxsc_add_port", Floor: 4,
		Implemented: true, Write: true, Proto: ProtoGenericCst, Setup: macsecSetup},
	{Name: "macsec a macsec0 rx sci " + macsecSciCst + " on", Slug: "macsec_rxsc_add_dup", Floor: 4,
		Implemented: true, Write: true, Proto: ProtoGenericCst, Setup: macsecRxscSetup},
	{Name: "macsec set macsec0 rx sci " + macsecSciCst + " off", Slug: "macsec_rxsc_set", Floor: 4,
		Implemented: true, Write: true, Proto: ProtoGenericCst, Setup: macsecRxscSetup},
	{Name: "macsec del macsec0 rx sci " + macsecSciCst, Slug: "macsec_rxsc_del", Floor: 4, Implemented: true,
		Write: true, Proto: ProtoGenericCst, Setup: macsecRxscSetup},
	{Name: "macsec d macsec0 rx sci " + macsecSciCst, Slug: "macsec_rxsc_del_missing", Floor: 4,
		Implemented: true, Write: true, Proto: ProtoGenericCst, Setup: macsecSetup},
	{Name: macsecRxsaCst, Slug: "macsec_rxsa_add", Floor: 4, Implemented: true, Write: true, Proto: ProtoGenericCst,
		Setup: macsecRxscSetup},
	{Name: "macsec a macsec0 rx sci " + macsecSciCst + " sa 0 pn 1 on " + macsecKey2Cst, Slug: "macsec_rxsa_add_no_sc",
		Floor: 4, Implemented: true, Write: true, Proto: ProtoGenericCst, Setup: macsecSetup},
	{Name: "macsec set macsec0 rx sci " + macsecSciCst + " sa 0 pn 60 off", Slug: "macsec_rxsa_set", Floor: 4,
		Implemented: true, Write: true, Proto: ProtoGenericCst, Setup: macsecRxsaSetup},
	{Name: "macsec d macsec0 rx sci " + macsecSciCst + " sa 0", Slug: "macsec_rxsa_del_active", Floor: 4,
		Implemented: true, Write: true, Proto: ProtoGenericCst, Setup: macsecRxsaSetup},
	{Name: "macsec del macsec0 rx sci " + macsecSciCst + " sa 0", Slug: "macsec_rxsa_del", Floor: 4,
		Implemented: true, Write: true, Proto: ProtoGenericCst, Setup: macsecRxsaOffSetup},
	{Name: macsecXpnTxsaCst, Slug: "macsec_xpn_txsa_add", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst, Setup: macsecXpnSetup},
	{Name: "macsec add macsec0 tx sa 0 pn 1 salt 0102030405060708090a0b0c ssci 1 " + macsecKey1Cst,
		Slug: "macsec_xpn_txsa_pn32", Floor: 4, Implemented: true, Write: true, Proto: ProtoGenericCst,
		Setup: macsecXpnSetup},
	{Name: "macsec offload macsec0 off", Slug: "macsec_offload_off", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst, Setup: macsecSetup},
	{Name: "macsec offload macsec0 mac", Slug: "macsec_offload_mac", Floor: 4, Implemented: true, Write: true,
		Proto: ProtoGenericCst, Setup: macsecSetup},

	// G7-T01: the XFRM state reads, the C3 corpus's nine state_list*,
	// state_get* and state_count* rows on their preconditions
	// (xfrm-write-corpus.exp), each slug the corpus's with xfrm_ ahead of
	// it. The kernel lists its SAs newest first, the order they were added,
	// and an SA's clocks are zeroed before comparison, so its two SAs come
	// back in one order on every side. Commands the corpus spells alike run
	// as show, g and c, the same bytes. They follow the genl rows, after
	// late setup has loaded xfrm_user.
	{Name: "xfrm state list", Slug: "xfrm_state_list_empty", Floor: 2, Implemented: true, Proto: ProtoXfrmCst},
	{Name: "xfrm state show", Slug: "xfrm_state_list", Floor: 2, Implemented: true, Proto: ProtoXfrmCst,
		Setup: xfrmTwoSetup},
	{Name: "xfrm state list dst 192.0.2.2", Slug: "xfrm_state_list_dst", Floor: 2, Implemented: true,
		Proto: ProtoXfrmCst, Setup: xfrmTwoSetup},
	{Name: "xfrm state list nokeys", Slug: "xfrm_state_list_nokeys", Floor: 2, Implemented: true,
		Proto: ProtoXfrmCst, Setup: xfrmTwoSetup},
	{Name: "xfrm state get " + xfrmSA1IDCst, Slug: "xfrm_state_get", Floor: 2, Implemented: true,
		Proto: ProtoXfrmCst, Setup: xfrmSA1Setup},
	{Name: "xfrm state g " + xfrmSA1IDCst, Slug: "xfrm_state_get_missing", Floor: 2, Implemented: true,
		Proto: ProtoXfrmCst},
	{Name: "xfrm state count", Slug: "xfrm_state_count", Floor: 2, Implemented: true, Proto: ProtoXfrmCst,
		Setup: xfrmTwoSetup},
	{Name: "xfrm state c", Slug: "xfrm_state_count_empty", Floor: 2, Implemented: true, Proto: ProtoXfrmCst},
	{Name: "-s xfrm state count", Slug: "xfrm_state_count_stats", Floor: 2, Implemented: true,
		Proto: ProtoXfrmCst, Setup: xfrmTwoSetup},

	// G7-T02: the XFRM state writes, the C3 corpus's 17 other state_* rows
	// but allocspi's: goip writes its ALLOCSPI and has no larval SA to
	// print, where ip prints the kernel's answer. add_no_algo and
	// allocspi_min_gt_max, refused before a request, capture nothing to
	// compare (Floor 0), and TestXfrmStateWritesMatchIp checks them.
	// Commands the corpus spells alike run as a, u, d and delall, the same
	// bytes. deleteall's two DELSAs go in one datagram, without NLM_F_ACK,
	// between its dumps; with none to delete, it is a read.
	{Name: xfrmSA1Cst, Slug: "xfrm_state_add", Floor: 2, Implemented: true, Write: true, Proto: ProtoXfrmCst},
	{Name: xfrmSA2Cst, Slug: "xfrm_state_add_v6_tunnel", Floor: 2, Implemented: true, Write: true,
		Proto: ProtoXfrmCst},
	{Name: "xfrm state add src 192.0.2.1 dst 192.0.2.3 proto esp spi 0x102 reqid 3 mode transport aead " +
		`"rfc4106(gcm(aes))" 0x3333333333333333333333333333333333333333 128`, Slug: "xfrm_state_add_aead", Floor: 2,
		Implemented: true, Write: true, Proto: ProtoXfrmCst},
	{Name: "xfrm state add src 192.0.2.1 dst 192.0.2.4 proto esp spi 0x103 mode transport enc aes " + xfrmEncKeyCst,
		Slug: "xfrm_state_add_enc_only", Floor: 2, Implemented: true, Write: true, Proto: ProtoXfrmCst},
	{Name: "xfrm state a " + xfrmSA1IDCst + " reqid 1 mode transport auth sha256 " + xfrmAuthKeyCst + " enc aes " +
		xfrmEncKeyCst, Slug: "xfrm_state_add_dup", Floor: 2, Implemented: true, Write: true, Proto: ProtoXfrmCst,
		Setup: xfrmSA1Setup},
	{Name: "xfrm state add " + xfrmSA1IDCst + " mode transport enc nosuchalg " + xfrmEncKeyCst,
		Slug: "xfrm_state_add_unknown_alg", Floor: 2, Implemented: true, Write: true, Proto: ProtoXfrmCst},
	{Name: "xfrm state add " + xfrmSA1IDCst + " mode transport enc aes 0x222222222222222222222222222222",
		Slug: "xfrm_state_add_short_key", Floor: 2, Implemented: true, Write: true, Proto: ProtoXfrmCst},
	{Name: "xfrm state update " + xfrmSA1IDCst + " reqid 1" + xfrmAlgosCst + " limit time-hard 3600",
		Slug: "xfrm_state_update", Floor: 2, Implemented: true, Write: true, Proto: ProtoXfrmCst,
		Setup: xfrmSA1Setup},
	{Name: "xfrm state update src 192.0.2.1 dst 192.0.2.2 proto esp spi 0x200" + xfrmAlgosCst,
		Slug: "xfrm_state_update_larval", Floor: 2, Implemented: true, Write: true, Proto: ProtoXfrmCst,
		Setup: []string{xfrmAllocspiCst}},
	{Name: "xfrm state u " + xfrmSA1IDCst + " reqid 1" + xfrmAlgosCst, Slug: "xfrm_state_update_missing",
		Floor: 2, Implemented: true, Write: true, Proto: ProtoXfrmCst},
	{Name: "xfrm state delete " + xfrmSA1IDCst, Slug: "xfrm_state_delete", Floor: 2, Implemented: true,
		Write: true, Proto: ProtoXfrmCst, Setup: xfrmSA1Setup},
	{Name: "xfrm state d " + xfrmSA1IDCst, Slug: "xfrm_state_delete_missing", Floor: 2, Implemented: true,
		Write: true, Proto: ProtoXfrmCst},
	{Name: "xfrm state deleteall", Slug: "xfrm_state_deleteall", Floor: 5, Implemented: true, Write: true,
		Writes: 2, Proto: ProtoXfrmCst, Setup: xfrmTwoSetup},
	{Name: "xfrm state delall", Slug: "xfrm_state_deleteall_empty", Floor: 2, Implemented: true,
		Proto: ProtoXfrmCst},
	{Name: "xfrm state flush", Slug: "xfrm_state_flush", Floor: 2, Implemented: true, Write: true, Writes: 1,
		Proto: ProtoXfrmCst, Setup: xfrmTwoSetup},
	{Name: "xfrm state flush proto ah", Slug: "xfrm_state_flush_proto_ah", Floor: 2, Implemented: true,
		Write: true, Writes: 1, Proto: ProtoXfrmCst, Setup: xfrmTwoSetup},
	{Name: "xfrm state allocspi src 192.0.2.1 dst 192.0.2.2 proto esp min 0x100 max 0x100",
		Slug: "xfrm_state_allocspi_taken", Floor: 2, Implemented: true, Write: true, Proto: ProtoXfrmCst,
		Setup: xfrmSA1Setup},

	// G7-T03: the XFRM policy reads, the C3 corpus's twelve policy_list*,
	// policy_get*, policy_count* and policy_getdefault rows on their
	// preconditions but the two refused before a request, get_no_sel and
	// get_sel_and_index, which capture nothing to compare (Floor 0), and
	// TestXfrmPolicyMatchesIp checks them. A policy's clocks are zeroed
	// before comparison, and P1, each side's namespace's first out policy,
	// is index 1 on every side. Commands the corpus spells alike run as
	// show, g, c and getd, the same bytes.
	{Name: "xfrm policy list", Slug: "xfrm_policy_list_empty", Floor: 2, Implemented: true, Proto: ProtoXfrmCst},
	{Name: "xfrm policy show", Slug: "xfrm_policy_list", Floor: 2, Implemented: true, Proto: ProtoXfrmCst,
		Setup: xfrmPTwoSetup},
	{Name: "xfrm policy list dir in", Slug: "xfrm_policy_list_dir_in", Floor: 2, Implemented: true,
		Proto: ProtoXfrmCst, Setup: xfrmPTwoSetup},
	{Name: "xfrm policy get " + xfrmP1SelCst, Slug: "xfrm_policy_get", Floor: 2, Implemented: true,
		Proto: ProtoXfrmCst, Setup: xfrmP1Setup},
	{Name: "xfrm policy get index 1 dir out", Slug: "xfrm_policy_get_index", Floor: 2, Implemented: true,
		Proto: ProtoXfrmCst, Setup: xfrmP1Setup},
	{Name: "xfrm policy g " + xfrmP1SelCst, Slug: "xfrm_policy_get_missing", Floor: 2, Implemented: true,
		Proto: ProtoXfrmCst},
	{Name: "xfrm policy count", Slug: "xfrm_policy_count", Floor: 2, Implemented: true, Proto: ProtoXfrmCst,
		Setup: xfrmPTwoSetup},
	{Name: "xfrm policy c", Slug: "xfrm_policy_count_empty", Floor: 2, Implemented: true, Proto: ProtoXfrmCst},
	{Name: "-s -s xfrm policy count", Slug: "xfrm_policy_count_stats", Floor: 2, Implemented: true,
		Proto: ProtoXfrmCst, Setup: xfrmPTwoSetup},
	{Name: "xfrm policy getd", Slug: "xfrm_policy_getdefault", Floor: 2, Implemented: true, Proto: ProtoXfrmCst},

	// G7-T04: the XFRM policy writes, the C3 corpus's 17 other policy_*
	// rows but the four refused before a request, add_no_dir, add_bad_dir,
	// set_hthresh_bad and setdefault_bad, which capture nothing to compare
	// (Floor 0), and TestXfrmPolicyWritesMatchIp checks them. Commands the
	// corpus spells alike run as a, d, delall and f, the same bytes.
	// deleteall's two DELPOLICYs go in one datagram, without NLM_F_ACK,
	// between its dumps; with none to delete, it is a read. setdefault and
	// set change only the namespace each side writes in.
	{Name: xfrmP1Cst, Slug: "xfrm_policy_add", Floor: 2, Implemented: true, Write: true, Proto: ProtoXfrmCst},
	{Name: xfrmP2Cst, Slug: "xfrm_policy_add_v6_block", Floor: 2, Implemented: true, Write: true,
		Proto: ProtoXfrmCst},
	{Name: "xfrm policy a " + xfrmP1SelCst + " " + xfrmP1TmplCst, Slug: "xfrm_policy_add_dup", Floor: 2,
		Implemented: true, Write: true, Proto: ProtoXfrmCst, Setup: xfrmP1Setup},
	{Name: "xfrm policy update " + xfrmP1SelCst + " priority 5 " + xfrmP1TmplCst, Slug: "xfrm_policy_update",
		Floor: 2, Implemented: true, Write: true, Proto: ProtoXfrmCst, Setup: xfrmP1Setup},
	{Name: "xfrm policy update " + xfrmP1SelCst + " " + xfrmP1TmplCst, Slug: "xfrm_policy_update_new", Floor: 2,
		Implemented: true, Write: true, Proto: ProtoXfrmCst},
	{Name: "xfrm policy delete " + xfrmP1SelCst, Slug: "xfrm_policy_delete", Floor: 2, Implemented: true,
		Write: true, Proto: ProtoXfrmCst, Setup: xfrmP1Setup},
	{Name: "xfrm policy d " + xfrmP1SelCst, Slug: "xfrm_policy_delete_missing", Floor: 2, Implemented: true,
		Write: true, Proto: ProtoXfrmCst},
	{Name: "xfrm policy deleteall", Slug: "xfrm_policy_deleteall", Floor: 5, Implemented: true, Write: true,
		Writes: 2, Proto: ProtoXfrmCst, Setup: xfrmPTwoSetup},
	{Name: "xfrm policy delall", Slug: "xfrm_policy_deleteall_empty", Floor: 2, Implemented: true,
		Proto: ProtoXfrmCst},
	{Name: "xfrm policy flush", Slug: "xfrm_policy_flush", Floor: 2, Implemented: true, Write: true, Writes: 1,
		Proto: ProtoXfrmCst, Setup: xfrmPTwoSetup},
	{Name: "xfrm policy f", Slug: "xfrm_policy_flush_empty", Floor: 2, Implemented: true, Write: true, Writes: 1,
		Proto: ProtoXfrmCst},
	{Name: "xfrm policy set hthresh4 24 16 hthresh6 64 48", Slug: "xfrm_policy_set_hthresh", Floor: 2,
		Implemented: true, Write: true, Proto: ProtoXfrmCst},
	{Name: "xfrm policy setdefault in block fwd block out accept", Slug: "xfrm_policy_setdefault", Floor: 2,
		Implemented: true, Write: true, Proto: ProtoXfrmCst},
})

// tcpmAddrsSetup is genl-write-corpus.exp's PRE_ADDRS: d0, up, with an IPv4
// and an IPv6 pair on it, whose traffic the namespace's lo carries.
var tcpmAddrsSetup = []string{
	"link add d0 type dummy",
	"link set d0 up",
	"addr add 192.0.2.1/24 dev d0",
	"addr add 192.0.2.2/24 dev d0",
	"-6 addr add 2001:db8::1/64 dev d0 nodad",
	"-6 addr add 2001:db8::2/64 dev d0 nodad",
}

// tcpmSetup is PRE_TCPM: PRE_ADDRS and a connection over each pair, through
// setup_step's `tcp SERVER CLIENT` form, which leaves four entries, one per
// end of each.
var tcpmSetup = append(slices.Clone(tcpmAddrsSetup),
	"tcp 192.0.2.1 192.0.2.2",
	"tcp 2001:db8::1 2001:db8::2",
)

// srHmacTwoSetup is genl-write-corpus.exp's PRE_HMAC2: key 1, sha256, and
// key 2, sha1, through setup_step's `hmac KEYID ALGO SECRET` form; its
// first step is PRE_HMAC1.
var srHmacTwoSetup = []string{
	"hmac 1 sha256 s3cret",
	"hmac 2 sha1 0ther",
}

// ioamNs1Setup is genl-write-corpus.exp's PRE_NS1: namespace 1.
var ioamNs1Setup = []string{"ioam namespace add 1"}

// ioamBoundSetup is PRE_BOUND: namespace 1, schema 1 with four bytes of
// data, and the one bound to the other. Its first two steps are
// PRE_NS1_SC1, and its second PRE_SC1.
var ioamBoundSetup = []string{
	"ioam namespace add 1",
	"ioam schema add 1 abcd",
	"ioam namespace set 1 schema 1",
}

// ioamJSONSetup is the -j shows' one namespace, with data and wide data,
// bound to its one schema.
var ioamJSONSetup = []string{
	"ioam namespace add 2 data 42 wide 2a",
	"ioam schema add 1 abcd",
	"ioam namespace set 2 schema 1",
}

// mptcpEp1Setup is genl-write-corpus.exp's PRE_EP1: PRE_ADDRS and a
// subflow endpoint, id 1.
var mptcpEp1Setup = append(slices.Clone(tcpmAddrsSetup), "mptcp endpoint add 192.0.2.1 id 1 subflow")

// mptcpEp1BackupSetup is PRE_EP1_BACKUP: the same endpoint, backup.
var mptcpEp1BackupSetup = append(slices.Clone(tcpmAddrsSetup), "mptcp endpoint add 192.0.2.1 id 1 subflow backup")

// mptcpEp2Setup is PRE_EP2: PRE_EP1 and a signal endpoint on d0, id 2.
var mptcpEp2Setup = append(slices.Clone(mptcpEp1Setup), "mptcp endpoint add 2001:db8::1 id 2 dev d0 signal")

// fou1Setup is genl-write-corpus.exp's PRE_FOU: one direct port.
var fou1Setup = []string{"fou add port 5555 ipproto 4"}

// fou2Setup is PRE_FOU2: that port and a gue one over IPv6.
var fou2Setup = []string{"fou add port 5555 ipproto 4", "fou add port 5556 gue -6"}

// fouPeerSetup is PRE_FOU_PEER: PRE_ADDRS and a port with both addresses.
var fouPeerSetup = append(slices.Clone(tcpmAddrsSetup),
	"fou add port 5555 ipproto 4 local 192.0.2.1 peer 192.0.2.2 peer_port 5556")

// fouDevSetup is fou_add_dev's precondition and command, the state its
// readback shows: PRE_ADDRS and a port bound to d0.
var fouDevSetup = append(slices.Clone(tcpmAddrsSetup), "fou add port 5555 ipproto 4 local 192.0.2.1 dev d0")

// ila1Setup is genl-write-corpus.exp's PRE_ILA: one mapping.
var ila1Setup = []string{"ila add loc_match 2001:db8:1:2 loc 2001:db8:3:4"}

// ilaAddrsSetup is PRE_ILA_ADDRS: PRE_ADDRS and that mapping.
var ilaAddrsSetup = append(slices.Clone(tcpmAddrsSetup), "ila add loc_match 2001:db8:1:2 loc 2001:db8:3:4")

// ilaDevSetup is PRE_ILA_DEV: PRE_ADDRS and the mapping on d0.
var ilaDevSetup = append(slices.Clone(tcpmAddrsSetup), "ila add loc_match 2001:db8:1:2 loc 2001:db8:3:4 dev d0")

// ilaBothSetup is PRE_ILA_BOTH: PRE_ADDRS and the mapping, without and
// with d0, one locator to match.
var ilaBothSetup = append(slices.Clone(ilaAddrsSetup), "ila add loc_match 2001:db8:1:2 loc 2001:db8:3:4 dev d0")

// ilaOptsSetup is ila_add_opts's command, the state its readback shows.
var ilaOptsSetup = []string{"ila add loc_match 2001:db8:1:2 loc 2001:db8:3:4 csum-mode adj-transport ident-type luid"}

// The l2tp commands genl-write-corpus.exp's rows and preconditions share:
// L2TP_TUN1 and L2TP_SESS1, the IPv6 and IP-encap tunnels, and the session
// with every option.
const (
	l2tpTun1Cst  = "l2tp add tunnel tunnel_id 1 peer_tunnel_id 2 local 192.0.2.1 remote 192.0.2.2 udp_sport 5000 udp_dport 6000"
	l2tpTunV6Cst = "l2tp add tunnel tunnel_id 1 peer_tunnel_id 2 local 2001:db8::1 remote 2001:db8::2 " +
		"udp_sport 5000 udp_dport 6000 udp6_csum_rx off"
	l2tpTunIPCst    = "l2tp add tunnel tunnel_id 1 peer_tunnel_id 2 encap ip local 192.0.2.1 remote 192.0.2.2"
	l2tpSess1Cst    = "l2tp add session tunnel_id 1 session_id 10 peer_session_id 20"
	l2tpSessOptsCst = "l2tp add session name l2tp9 tunnel_id 1 session_id 10 peer_session_id 20 " +
		"cookie 0102030405060708 peer_cookie 0a0b0c0d seq both l2spec_type none"
)

// l2tpTunSetup is genl-write-corpus.exp's PRE_TUN: PRE_ADDRS and a UDP
// tunnel.
var l2tpTunSetup = append(slices.Clone(tcpmAddrsSetup), l2tpTun1Cst)

// l2tpTun2Setup is PRE_TUN2: PRE_TUN and a second tunnel.
var l2tpTun2Setup = append(slices.Clone(l2tpTunSetup),
	"l2tp add tunnel tunnel_id 3 peer_tunnel_id 4 local 192.0.2.1 remote 192.0.2.2 udp_sport 5001 udp_dport 6001")

// l2tpSessSetup is PRE_SESS: PRE_TUN and a session.
var l2tpSessSetup = append(slices.Clone(l2tpTunSetup), l2tpSess1Cst)

// l2tpSess2Setup is PRE_SESS2: PRE_SESS and a second session.
var l2tpSess2Setup = append(slices.Clone(l2tpSessSetup),
	"l2tp add session tunnel_id 1 session_id 11 peer_session_id 21")

// l2tpSessIPSetup is PRE_SESS_IP: PRE_ADDRS, an IP-encap tunnel and the
// session.
var l2tpSessIPSetup = append(slices.Clone(tcpmAddrsSetup), l2tpTunIPCst, l2tpSess1Cst)

// l2tpTunV6Setup is l2tp_tunnel_add_v6's precondition and command, the
// state its readback shows.
var l2tpTunV6Setup = append(slices.Clone(tcpmAddrsSetup), l2tpTunV6Cst)

// l2tpSessOptsSetup is l2tp_session_add_opts's precondition and command,
// likewise.
var l2tpSessOptsSetup = append(slices.Clone(l2tpTunSetup), l2tpSessOptsCst)

// The macsec commands genl-write-corpus.exp's rows and preconditions
// share: MACSEC_LINK, the two keys, the receive channel's SCI,
// MACSEC_TXSA, MACSEC_RXSC, MACSEC_RXSA and the XPN SA.
const (
	macsecLinkCst    = "link add link d0 macsec0 type macsec sci 0102030405060001"
	macsecKey1Cst    = "key 01000000000000000000000000000000 81818181818181818181818181818181"
	macsecKey2Cst    = "key 02000000000000000000000000000000 82828282828282828282828282828282"
	macsecSciCst     = "0a0b0c0d0e0f0001"
	macsecTxsaCst    = "macsec add macsec0 tx sa 0 pn 1 on " + macsecKey1Cst
	macsecRxscCst    = "macsec add macsec0 rx sci " + macsecSciCst + " on"
	macsecRxsaCst    = "macsec add macsec0 rx sci " + macsecSciCst + " sa 0 pn 1 on " + macsecKey2Cst
	macsecXpnTxsaCst = "macsec add macsec0 tx sa 0 xpn 1 salt 0102030405060708090a0b0c ssci 1 on " + macsecKey1Cst
)

// macsecSetup is genl-write-corpus.exp's PRE_MACSEC: PRE_ADDRS and
// macsec0 over d0.
var macsecSetup = append(slices.Clone(tcpmAddrsSetup), macsecLinkCst)

// macsec2Setup is PRE_MACSEC2: PRE_MACSEC and macsec1.
var macsec2Setup = append(slices.Clone(macsecSetup), "link add link d0 macsec1 type macsec sci 0102030405060002")

// macsecXpnSetup is PRE_MACSEC_XPN: PRE_ADDRS and an XPN macsec0.
var macsecXpnSetup = append(slices.Clone(tcpmAddrsSetup), macsecLinkCst+" cipher gcm-aes-xpn-128")

// macsecTxsaSetup is PRE_TXSA: PRE_MACSEC and an active transmit SA.
var macsecTxsaSetup = append(slices.Clone(macsecSetup), macsecTxsaCst)

// macsecTxsaOffSetup is PRE_TXSA_OFF: PRE_MACSEC and the SA inactive.
var macsecTxsaOffSetup = append(slices.Clone(macsecSetup), "macsec add macsec0 tx sa 0 pn 1 off "+macsecKey1Cst)

// macsecRxscSetup is PRE_RXSC: PRE_MACSEC and a receive channel.
var macsecRxscSetup = append(slices.Clone(macsecSetup), macsecRxscCst)

// macsecRxsaSetup is PRE_RXSA: PRE_RXSC and an active receive SA.
var macsecRxsaSetup = append(slices.Clone(macsecRxscSetup), macsecRxsaCst)

// macsecRxsaOffSetup is PRE_RXSA_OFF: PRE_RXSC and the SA inactive.
var macsecRxsaOffSetup = append(slices.Clone(macsecRxscSetup),
	"macsec add macsec0 rx sci "+macsecSciCst+" sa 0 pn 1 off "+macsecKey2Cst)

// macsecXpnTxsaSetup is macsec_xpn_txsa_add's precondition and command,
// the state its readback shows.
var macsecXpnTxsaSetup = append(slices.Clone(macsecXpnSetup), macsecXpnTxsaCst)

// The XFRM SAs xfrm-write-corpus.exp's rows and preconditions share: SA
// 1's id, SA 1, ESP in transport mode with hmac(sha256) and cbc(aes), and
// SA 2, IPv6 ESP in tunnel mode with a truncated hmac(sha256).
const (
	xfrmAuthKeyCst = "0x1111111111111111111111111111111111111111111111111111111111111111"
	xfrmEncKeyCst  = "0x22222222222222222222222222222222"
	xfrmSA1IDCst   = "src 192.0.2.1 dst 192.0.2.2 proto esp spi 0x100"
	xfrmSA1Cst     = "xfrm state add " + xfrmSA1IDCst + " reqid 1 mode transport auth sha256 " + xfrmAuthKeyCst +
		" enc aes " + xfrmEncKeyCst
	xfrmSA2Cst = "xfrm state add src 2001:db8::1 dst 2001:db8::2 proto esp spi 0x101 reqid 2 mode tunnel " +
		"auth-trunc sha256 " + xfrmAuthKeyCst + " 128 enc aes " + xfrmEncKeyCst
	// xfrmAlgosCst is SA 1's mode and algorithms, which its updates send
	// again.
	xfrmAlgosCst = " mode transport auth sha256 " + xfrmAuthKeyCst + " enc aes " + xfrmEncKeyCst
	// xfrmAllocspiCst is the corpus's ALLOCSPI: one larval SA, SPI 0x200.
	xfrmAllocspiCst = "xfrm state allocspi src 192.0.2.1 dst 192.0.2.2 proto esp min 0x200 max 0x200"
)

// xfrmSA1Setup is xfrm-write-corpus.exp's PRE_SA1, and xfrmTwoSetup its
// PRE_TWO.
var (
	xfrmSA1Setup = []string{xfrmSA1Cst}
	xfrmTwoSetup = []string{xfrmSA1Cst, xfrmSA2Cst}
)

// The policies xfrm-write-corpus.exp's rows and preconditions share:
// policy 1, out from 192.0.2.1 to 192.0.2.2 requiring SA 1's ESP in
// transport mode, reqid 1, and policy 2, IPv6 TCP to port 443 inbound,
// blocked, at priority 10.
const (
	xfrmP1SelCst  = "src 192.0.2.1/32 dst 192.0.2.2/32 dir out"
	xfrmP1TmplCst = "tmpl src 192.0.2.1 dst 192.0.2.2 proto esp reqid 1 mode transport"
	xfrmP1Cst     = "xfrm policy add " + xfrmP1SelCst + " " + xfrmP1TmplCst
	xfrmP2Cst     = "xfrm policy add src 2001:db8::1/128 dst 2001:db8::2/128 proto tcp dport 443 dir in " +
		"priority 10 action block"
)

// xfrmP1Setup is xfrm-write-corpus.exp's PRE_P1, and xfrmPTwoSetup its
// PRE_PTWO.
var (
	xfrmP1Setup   = []string{xfrmP1Cst}
	xfrmPTwoSetup = []string{xfrmP1Cst, xfrmP2Cst}
)

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

// bridgeAgeingTimeCst is the bridge's British-spelled timer keyword, split as
// xtcpnl.BridgeKeywordAgeingTime is, since misspell holds strings to US
// spelling and the keyword is iproute2's.
const bridgeAgeingTimeCst = "age" + "ing_time"

// vxlanAgeingCst is vxlan's British-spelled timer keyword, split as
// xtcpnl.VxlanKeywordAgeing is, for bridgeAgeingTimeCst's reason.
const vxlanAgeingCst = "age" + "ing"

// The bridge write rows' preconditions, kind-write-corpus.exp's PRE_BR,
// PRE_BR_STP and PRE_PORTS, and the steps the state, nobackup_port and
// not_enslaved rows add or take away.
var (
	bridgeWriteSetupBr0         = []string{"link add br0 type bridge"}
	bridgeWriteSetupBr0Stp      = []string{"link add br0 type bridge stp_state 1"}
	bridgeWriteSetupPorts       = []string{"link add br0 type bridge", "link add p0 type dummy", "link add p1 type dummy", "link set p0 master br0", "link set p1 master br0"}
	bridgeWriteSetupPortsUp     = append(slices.Clone(bridgeWriteSetupPorts), "link set p0 addrgenmode none", "link set p0 up")
	bridgeWriteSetupPortsBackup = append(slices.Clone(bridgeWriteSetupPorts), "link set dev p0 type bridge_slave backup_port p1")
	bridgeWriteSetupP0          = []string{"link add p0 type dummy"}
)

// The VLAN family's preconditions past PRE_D0 (linkWriteSetupDummy):
// kind-write-corpus.exp's PRE_V0, PRE_M0, PRE_M0_SOURCE, PRE_M0_PASSTHRU,
// PRE_T0, PRE_I0, PRE_I0_PRIVATE and PRE_J0, each after d0.
var (
	vlanWriteSetupV0            = []string{"link add d0 type dummy", "link add link d0 name v0 type vlan id 10"}
	macvlanWriteSetupM0         = []string{"link add d0 type dummy", "link add link d0 name m0 type macvlan mode bridge"}
	macvlanWriteSetupM0Source   = []string{"link add d0 type dummy", "link add link d0 name m0 type macvlan mode source macaddr add 02:00:00:00:00:01"}
	macvlanWriteSetupM0Passthru = []string{"link add d0 type dummy", "link add link d0 name m0 type macvlan mode passthru"}
	macvtapWriteSetupT0         = []string{"link add d0 type dummy", "link add link d0 name t0 type macvtap mode bridge"}
	ipvlanWriteSetupI0          = []string{"link add d0 type dummy", "link add link d0 name i0 type ipvlan"}
	ipvlanWriteSetupI0Private   = []string{"link add d0 type dummy", "link add link d0 name i0 type ipvlan private"}
	ipvtapWriteSetupJ0          = []string{"link add d0 type dummy", "link add link d0 name j0 type ipvtap"}
)

// The UDP overlays' preconditions (kind-write-corpus.exp): PRE_VX0,
// PRE_VX0 after d0, PRE_GN0, PRE_GN0_LOCAL, PRE_GN0_EXTERNAL, PRE_B0,
// amt's a0 after d0, and PRE_G0.
var (
	vxlanWriteSetupVx0          = []string{"link add vx0 type vxlan id 42 dstport 4789"}
	vxlanWriteSetupD0Vx0        = []string{"link add d0 type dummy", "link add vx0 type vxlan id 42 dstport 4789"}
	geneveWriteSetupGn0         = []string{"link add gn0 type geneve id 1 remote 10.0.0.1"}
	geneveWriteSetupGn0Local    = []string{"link add gn0 type geneve id 1 remote 10.0.0.1 local 10.0.0.2"}
	geneveWriteSetupGn0External = []string{"link add gn0 type geneve external"}
	bareudpWriteSetupB0         = []string{"link add b0 type bareudp dstport 6635 ethertype ip"}
	amtWriteSetupA0             = []string{"link add d0 type dummy", "link add a0 type amt mode relay dev d0 local 10.0.0.1"}
	gtpWriteSetupG0             = []string{"link add g0 type gtp role ggsn"}
)

// The IP tunnels' preconditions (kind-write-corpus.exp's PRE_* for the
// twelve kinds), and the vti read rows' devices, keyed and on d0.
var (
	greWriteSetupGr0             = []string{"link add gr0 type gre remote 10.0.0.1"}
	greWriteSetupGr0External     = []string{"link add gr0 type gre external"}
	greWriteSetupGr0NoPMTUDisc   = []string{"link add gr0 type gre remote 10.0.0.1 nopmtudisc ignore-df"}
	greWriteSetupD0Gr0           = []string{"link add d0 type dummy", "link add gr0 type gre remote 10.0.0.1"}
	greWriteSetupGr0Gr1          = []string{"link add gr0 type gre remote 10.0.0.1", "link add gr1 type gre remote 10.0.0.2"}
	gretapWriteSetupGt0          = []string{"link add gt0 type gretap remote 10.0.0.1"}
	gretapWriteSetupGt0External  = []string{"link add gt0 type gretap external"}
	erspanWriteSetupEs0          = []string{"link add es0 type erspan remote 10.0.0.1 seq key 10"}
	ip6greWriteSetupG6           = []string{"link add g6 type ip6gre remote 2001:db8::1"}
	ip6greWriteSetupG6External   = []string{"link add g6 type ip6gre external"}
	ip6greWriteSetupG6EncapSport = []string{"link add g6 type ip6gre remote 2001:db8::1 encap-sport 4660"}
	ip6greWriteSetupG6G7         = []string{"link add g6 type ip6gre remote 2001:db8::1", "link add g7 type ip6gre remote 2001:db8::2"}
	ip6gretapWriteSetupGt6       = []string{"link add gt6 type ip6gretap remote 2001:db8::1"}
	ip6gretapWriteSetupGt6Gt7    = []string{"link add gt6 type ip6gretap remote 2001:db8::1", "link add gt7 type ip6gretap remote 2001:db8::2"}
	ip6erspanWriteSetupEs6       = []string{"link add es6 type ip6erspan remote 2001:db8::1 key 10 seq"}
	ipipWriteSetupIp0            = []string{"link add ip0 type ipip remote 10.0.0.1"}
	ipipWriteSetupIp0Bare        = []string{"link add ip0 type ipip"}
	ipipWriteSetupIp0External    = []string{"link add ip0 type ipip external"}
	ipipWriteSetupD0Ip0          = []string{"link add d0 type dummy", "link add ip0 type ipip remote 10.0.0.1"}
	ipipWriteSetupIp0Ip1         = []string{"link add ip0 type ipip remote 10.0.0.1", "link add ip1 type ipip remote 10.0.0.2"}
	sitWriteSetupSt0             = []string{"link add st0 type sit remote 10.0.0.1"}
	sitWriteSetupSt0Bare         = []string{"link add st0 type sit"}
	sitWriteSetupSt0Inherit      = []string{"link add st0 type sit ttl inherit"}
	sitWriteSetupSt0St1          = []string{"link add st0 type sit remote 10.0.0.1", "link add st1 type sit remote 10.0.0.2"}
	ip6tnlWriteSetupT6           = []string{"link add t6 type ip6tnl remote 2001:db8::1"}
	ip6tnlWriteSetupT6Bare       = []string{"link add t6 type ip6tnl"}
	ip6tnlWriteSetupT6External   = []string{"link add t6 type ip6tnl external"}
	ip6tnlWriteSetupT6Hoplimit   = []string{"link add t6 type ip6tnl remote 2001:db8::1 hoplimit 10"}
	ip6tnlWriteSetupT6T7         = []string{"link add t6 type ip6tnl remote 2001:db8::1", "link add t7 type ip6tnl remote 2001:db8::2"}
	vtiWriteSetupVt0             = []string{"link add vt0 type vti remote 10.0.0.1"}
	vtiWriteSetupVt0Bare         = []string{"link add vt0 type vti"}
	vtiWriteSetupD0Vt0           = []string{"link add d0 type dummy", "link add vt0 type vti remote 10.0.0.1"}
	vtiWriteSetupVt0Vt1          = []string{"link add vt0 type vti remote 10.0.0.1", "link add vt1 type vti remote 10.0.0.2"}
	vti6WriteSetupV6             = []string{"link add v6 type vti6 remote 2001:db8::1"}
	vti6WriteSetupV6Bare         = []string{"link add v6 type vti6"}
	vti6WriteSetupD0V6           = []string{"link add d0 type dummy", "link add v6 type vti6 remote 2001:db8::1"}
	vti6WriteSetupV6V7           = []string{"link add v6 type vti6 remote 2001:db8::1", "link add v7 type vti6 remote 2001:db8::2"}
	xfrmWriteSetupXf0            = []string{"link add xf0 type xfrm if_id 1"}
	xfrmWriteSetupXf0External    = []string{"link add xf0 type xfrm external"}
	xfrmWriteSetupD0Xf0          = []string{"link add d0 type dummy", "link add xf0 type xfrm if_id 1"}
	xfrmWriteSetupD0Xf0Dev       = []string{"link add d0 type dummy", "link add xf0 type xfrm dev d0 if_id 1"}
	xfrmWriteSetupXf0Xf1         = []string{"link add xf0 type xfrm if_id 1", "link add xf1 type xfrm if_id 2"}
	vtiReadSetupVt0              = []string{"link add d0 type dummy", "link add vt0 type vti remote 10.0.0.1 local 10.0.0.2 ikey 1 okey 2 dev d0"}
	vti6ReadSetupV6              = []string{"link add d0 type dummy", "link add v6 type vti6 remote 2001:db8::1 key 5 dev d0 fwmark 0x10"}
	hsrWriteSetupD0D1            = []string{"link add d0 type dummy", "link add d1 type dummy"}
	hsrWriteSetupD0D1D2          = []string{"link add d0 type dummy", "link add d1 type dummy", "link add d2 type dummy"}
	hsrWriteSetupHs0             = []string{"link add d0 type dummy", "link add d1 type dummy", "link add hs0 type hsr slave1 d0 slave2 d1"}
	netkitWriteSetupNt0          = []string{"link add nt0 type netkit"}
	netkitWriteSetupNt0Nt1       = []string{"link add nt0 type netkit peer name nt1"}
	vrfWriteSetupVr1             = []string{"link add vr1 type vrf table 10"}
	vrfWriteSetupVr0             = []string{"link add vr0 type vrf table 10"}
	vrfSlaveWriteSetupVr0D0      = []string{"link add vr0 type vrf table 10", "link add d0 type dummy"}
	vrfSlaveWriteSetupD0Enslaved = []string{"link add vr0 type vrf table 10", "link add d0 type dummy", "link set d0 master vr0"}
	vrfSlaveWriteSetupVr0Vr1     = []string{"link add vr0 type vrf table 10", "link add vr1 type vrf table 20"}
	vxcanWriteSetupVc0           = []string{"link add vc0 type vxcan peer name vc1"}
	batadvWriteSetupBat0         = []string{"link add bat0 type batadv"}
	ifbWriteSetupIb0             = []string{"link add ib0 type ifb"}
	nlmonWriteSetupNm0           = []string{"link add nm0 type nlmon"}
	vcanWriteSetupVn0            = []string{"link add vn0 type vcan"}
	vcanWriteSetupVn0Up          = []string{"link add vn0 type vcan", "link set vn0 up"}
	teamWriteSetupTm0            = []string{"link add tm0 type team"}
	teamWriteSetupTm0D0          = []string{"link add tm0 type team", "link add d0 type dummy"}
	teamWriteSetupD0Enslaved     = []string{"link add tm0 type team", "link add d0 type dummy", "link set d0 master tm0"}
	netkitReadSetupNt0           = []string{"link add nt0 type netkit blackhole"}
	batadvReadSetupBat0          = []string{"link add bat0 type batadv ra BATMAN_V"}
	teamSlaveReadSetupD0         = []string{"link add tm0 type team", "link add d0 type dummy", "link set d0 master tm0", "link set d0 type team_slave"}
)

// bridgeReadSetup is the bridge read rows' namespace, PRE_PORTS: br0 and
// its ports p0 and p1, dummies, none up.
var bridgeReadSetup = bridgeWriteSetupPorts

// The GW link rows' preconditions: nlcapgen's d0, with the bridge and vrf
// a master needs, or with a macvlan, whose protodown a dummy refuses.
var (
	gwWsLinkD0      = []string{"link add d0 type dummy", "link set d0 addrgenmode none"}
	gwWsLinkBrVrf   = append(slices.Clone(gwWsLinkD0), "link add br0 type bridge", "link add v0 type vrf table 10")
	gwWsLinkMacvlan = append(slices.Clone(gwWsLinkD0), "link add m0 link d0 type macvlan mode bridge")
)

// The GW rows' preconditions: the workbench's d0 (nlcapgen's routeSetup), and
// with nexthop 10 by a v4 or a v6 gateway.
var (
	gwWsSetupD0 = []string{"link add d0 type dummy", "link set d0 addrgenmode none", "link set d0 up",
		"addr add 192.0.2.10/24 dev d0", "-6 addr add 2001:db8:1::10/64 dev d0 nodad"}
	// The kwlive vrf rows' VRFs: tables 100 and 1111, either side of 256.
	gwWsSetupVrf = append(slices.Clone(gwWsSetupD0), "link add vrf0 type vrf table 100", "link set vrf0 up",
		"link add vrf1 type vrf table 1111", "link set vrf1 up")
	// The kwlink rows' two dummies in group 5, for the group-wide set.
	gwWsKwLink   = []string{"link add d0 type dummy", "link set d0 group 5", "link add d1 type dummy", "link set d1 group 5"}
	gwWsSetupNh4 = append(slices.Clone(gwWsSetupD0), "nexthop add id 10 via 192.0.2.1 dev d0")
	gwWsSetupNh6 = append(slices.Clone(gwWsSetupD0), "-6 nexthop add id 10 via 2001:db8:1::1 dev d0")
	// lwtencReadSetup is the lwtenc corpus's positive forms side by side for
	// the NX9 readback rows: rpl, two ioam6 modes and an rpl hop on IPv6
	// routes, bpf on an IPv4 route, and rpl, ioam6 and bpf on nexthops. The
	// bpf object is the parity guest's /run/nlcap-ws/lwt_bpf.o (mkVm.nix).
	lwtencReadSetup = append(slices.Clone(gwWsSetupD0),
		"-6 route add 2001:db8:2::/64 encap rpl segs 2001:db8:9::1,2001:db8:9::2,2001:db8:9::3 dev d0",
		"-6 route add 2001:db8:3::/64 encap ioam6 freq 1/2 mode encap tunsrc 2001:db8:1::10 tundst 2001:db8:9::1 trace prealloc type 0x800000 ns 1 size 12 dev d0",
		"-6 route add 2001:db8:4::/64 encap ioam6 mode auto tundst 2001:db8:9::1 trace prealloc type 0x800000 ns 1 size 12 dev d0",
		"-6 route add 2001:db8:5::/64 nexthop via 2001:db8:1::1 dev d0 encap rpl segs 2001:db8:9::1 nexthop via 2001:db8:1::2 dev d0",
		"-4 route add 198.51.100.0/24 encap bpf in obj /run/nlcap-ws/lwt_bpf.o sec lwt_in out obj /run/nlcap-ws/lwt_bpf.o sec lwt_out xmit obj /run/nlcap-ws/lwt_bpf.o sec lwt_xmit headroom 16 dev d0",
		"-6 nexthop add id 81 encap rpl segs 2001:db8:9::1 dev d0",
		"-6 nexthop add id 82 encap ioam6 trace prealloc type 0x800000 ns 1 size 12 dev d0",
		"-6 nexthop add id 83 encap bpf in obj /run/nlcap-ws/lwt_bpf.o sec lwt_in dev d0")
	// The groups' members: nlcapgen's nhMembers.
	gwWsSetupNhGrp = append(slices.Clone(gwWsSetupD0), "nexthop add id 21 via 192.0.2.1 dev d0",
		"nexthop add id 22 via 192.0.2.2 dev d0", "nexthop add id 23 dev d0", "-6 nexthop add id 26 via 2001:db8:1::1 dev d0",
		"nexthop add id 29 blackhole", "nexthop add id 31 via 192.0.2.1 fdb", "nexthop add id 32 via 192.0.2.2 fdb")
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
	// nexthopStatsSetup is the nhstats corpus' groups (ec-001, ec-003) and
	// a seg6local nexthop with counters, for the -s rows.
	nexthopStatsSetup = append(append([]string{}, nexthopGetSetup...),
		"nexthop add id 10 group 1/2", "nexthop add id 12 group 1/2 hw_stats on",
		"-6 nexthop add id 30 encap seg6local action End count dev d0")
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

// mrouteSetup is the `ip mroute show` rows' precondition: the veths and
// addresses of rtnl-read-corpus.exp's build list, then MROUTE_CONF, which
// setup_step's `smcroute` form hands to xtcp2-nlcap mroute-start.
// addrgenmode none leaves the veths no link-local: one finishing DAD inside
// a capture window is an RTM_NEWADDR, and smcrouted answers it with a link
// dump of its own, both in that side's capture. settle_links, which waits
// out linkwatch, put the -6, iif, table all and -j rows' ip_a there.
var mrouteSetup = []string{
	"link add v0 type veth peer name v0p", "link add v1 type veth peer name v1p",
	"link set v0 addrgenmode none", "link set v0p addrgenmode none",
	"link set v1 addrgenmode none", "link set v1p addrgenmode none",
	"link set v0p up", "link set v1p up", "link set v0 up", "link set v1 up",
	"addr add 192.0.2.1/24 dev v0", "addr add 198.51.100.1/24 dev v1",
	"smcroute mroute from v0 source 192.0.2.10 group 239.1.1.1 to v1",
}

// The `ip token` rows' preconditions: the reads veths with v0's token, as
// rtnl-read-corpus.exp's build list sets it; and rtnl-write-corpus.exp's
// PRE_VETH, a veth d0 whose peer is up, with addrgenmode none on both, as
// neighWriteSetupVeth has. ip token set dumps the links before it writes,
// and a link-local address would race the dump: DAD, MLD and the router
// solicitation once d0 is ready change its counters and IF_RS_SENT, which
// one side saw and the other did not.
var (
	tokenSetup = []string{
		"link add v0 type veth peer name v0p", "link add v1 type veth peer name v1p",
		"link set v0p up", "link set v1p up", "link set v0 up", "link set v1 up",
		"token set ::1:2:3:4/64 dev v0",
	}
	tokenVethSetup = []string{
		"link add d0 type veth peer name d1",
		"link set d0 addrgenmode none", "link set d1 addrgenmode none", "link set d1 up", "link set d0 up",
	}
)

// linkNetnsSetup is the netns rows' precondition: the workbench's d0 and
// the peer c1mv it moves into.
var linkNetnsSetup = append(slices.Clone(gwWsLinkD0), "peer c1mv")

// mplsRouteSetup is the mpls rows' pre (nlcapgen's matrix_mpls.go):
// mpls_router, d0 up with an IPv4 and an IPv6 address, 1000 platform
// labels, and MPLS input on d0.
var mplsRouteSetup = []string{
	"modules mpls_router",
	"link add d0 type dummy", "link set d0 addrgenmode none", "link set d0 up",
	"addr add 10.0.0.1/24 dev d0", "-6 addr add 2001:db8::1/64 dev d0 nodad",
	"sysctl net.mpls.platform_labels 1000", "sysctl net.mpls.conf.d0.input 1",
}

// mplsShowSetup and mplsGetSetup are the pre steps of the mpls rows named
// show and get, after mplsRouteSetup's.
var (
	mplsShowSetup = append(slices.Clone(mplsRouteSetup),
		"-f mpls route add 100 as 200/300 via inet 10.0.0.2 dev d0",
		"-f mpls route add 101 nexthop as 200 via inet 10.0.0.2 dev d0 nexthop via inet6 2001:db8::2 dev d0",
		"-f mpls route add 102 dev d0 ttl-propagate disabled")
	mplsGetSetup = append(slices.Clone(mplsRouteSetup), "-f mpls route add 100 via inet 10.0.0.2 dev d0")
)

// netnsSetup is the netns reads' precondition, rtnl-read-corpus.exp's two
// peers: nlcaprp1 and nlcaprp2, made from the root, nsids 1 and 2 in the
// row's namespace.
var netnsSetup = []string{"peer nlcaprp1 1", "peer nlcaprp2 2"}

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
// maddrReadSetup is the v730maddr rows' pre: a dummy, with no link-local
// address to race DAD, that has joined one group of each kind.
var maddrReadSetup = []string{
	"link add d0 type dummy",
	"link set d0 addrgenmode none",
	"link set d0 up",
	"maddr add 33:33:00:00:00:99 dev d0",
	"addr add 239.1.1.1/32 dev d0 autojoin",
	"-6 addr add ff05::1234/128 dev d0 autojoin",
}

// statsL3Setup is the stats corpus's l3 topology: a dummy with its L3
// offload stats requested.
var statsL3Setup = []string{
	"link add d0 type dummy",
	"link set d0 addrgenmode none",
	"link set d0 up",
	"stats set dev d0 l3_stats on",
}

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
	for i := range commands {
		if commands[i].Name == name {
			return commands[i], nil
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

// ipStandInCst is String's impl field for an IPStandIn row.
const ipStandInCst = "ip"

// noSetupCst is String's Setup field for a row with no precondition, since an
// empty field between two tabs is easy to misread in a transcript.
const noSetupCst = "-"

// String renders one table row for `goip-parity commands`, tab-separated so a
// shell can read it with `read -r slug floor impl dev write proto setup args
// name`.
//
// impl is yes, no, or ip for an IPStandIn row, whose goip side runs `ip`.
// proto is Protocol(). setup is noSetupCst or the Setup steps joined with
// ';'. It is the one field before args that holds spaces, which a tab split
// does not mind. stdin is noSetupCst for no Stdin, or '>' and the line, so
// the blank line is '>' alone.
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
	impl := yesNo(c.Implemented)
	if c.IPStandIn {
		impl = ipStandInCst
	}
	stdin := noSetupCst
	if c.Stdin != nil {
		stdin = stdinMarkCst + *c.Stdin
	}
	return strings.Join([]string{
		c.Slug,
		fmt.Sprint(c.Floor),
		impl,
		yesNo(c.NeedsDev),
		yesNo(c.Write),
		c.Protocol(),
		setup,
		stdin,
		strings.Join(c.Args, " "),
		c.Name,
	}, "\t")
}

// stdinMarkCst begins String()'s stdin field when the row has a Stdin.
const stdinMarkCst = ">"

// stdinLine is a Stdin.
func stdinLine(line string) *string { return &line }

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
	for i := range commands {
		out = append(out, commands[i].Slug)
	}
	sort.Strings(out)
	return out
}
