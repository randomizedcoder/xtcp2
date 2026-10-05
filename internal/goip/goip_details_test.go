package goip

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/internal/goip/model"
)

// This file is `ip -d`, end to end: goip's rendering of a committed pcap
// against the `ip -d` sidecar captured from the same topology.
//
// # Why every one of these is a pure win over the reconstruction
//
// Until -d existed, obj_link_test.go had to RECONSTRUCT what plain `ip link
// show` would have printed by cutting the `-d` sidecar at " promiscuity" —
// see plainFromSidecar, which still does that for the 7_1_8 corpus. The `_n`
// sidecars themselves were unreadable, and the capture script said so at
// sidecar_set: "goip does not implement -d, so that form has no counterpart
// to diff against". Eight of them had been committed and unread ever since.
// These tests read them, and that sentence is no longer in the script.
//
// # The one thing that is NOT compared, and why no capture can fix it
//
// nlmon0's promiscuity. It is the interface the pcap is taken on: tcpdump
// opens a packet socket on it and the kernel raises IFF_PROMISC for the
// lifetime of that socket, so a reply recorded INSIDE the capture window says
// 1. The text sidecars come from nlcap's cmd_side, which runs the command with
// no tcpdump (nix/microvms/netlink-capture.nix:425-435), so they say 0.
//
// Capturing the pcap is what sets the bit, so no re-capture reconciles them.
// It is one field on one interface, it is causal, and it was invisible until
// this file existed, because plain `ip link show` prints no promiscuity at
// all. sidecarWithCapturePromisc states the substitution once and REQUIRES it
// to apply, so a corpus that ever stopped needing it fails here rather than
// passing silently.
//
// The parity harness is unaffected, and that is measured rather than argued:
// it runs `ip`, `goip` and `ip` again through nlcap's capio, so all three are
// inside a capture window and all three see 1. Two `-d link show` triples
// reported 1194 bytes of stdout on every side with `control: stdout=0`, so
// the field the offline test has to excuse is the one the live tier compares
// exactly.

// sidecarWithCapturePromisc reads a `-d` sidecar and rewrites nlmon0's
// promiscuity from the value a quiescent interface reports to the value one
// under capture does.
//
// It fails if the substitution finds nothing. That is the point: the rewrite
// is a claim about the corpus, and a silent no-op would let a future capture
// that did not need it pass while still carrying the excuse.
func sidecarWithCapturePromisc(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const quiescent = "link/netlink  promiscuity 0 "
	const capturing = "link/netlink  promiscuity 1 "
	s := string(raw)
	if !strings.Contains(s, quiescent) {
		t.Fatalf("%s: no %q to rewrite; nlmon0's promiscuity is the one field the "+
			"pcap and the sidecar cannot agree on, so its absence means the corpus "+
			"changed shape and this substitution needs re-deriving", path, quiescent)
	}
	return strings.ReplaceAll(s, quiescent, capturing)
}

// sidecarWithoutIoctlQlen removes a `qlen` token whose value did not come off
// the wire.
//
// # A known skew that already has an allowlist entry, not a new one
//
// iproute2 7.1.0's print_queuelen falls back to ioctl(SIOCGIFTXQLEN) when
// IFLA_TXQLEN is absent from the reply, and an AF_INET6 link dump is answered
// by inet6_fill_ifinfo (net/ipv6/addrconf.c), which sends IFLA_IFNAME,
// IFLA_ADDRESS, IFLA_MTU, IFLA_LINK, IFLA_OPERSTATE and IFLA_PROTINFO and
// nothing else. So `ip -6 addr show` prints `qlen 1000` from a syscall while
// the replies in the committed pcap carry no such attribute — measured, on
// netlink_route_getaddr_v6.pcap, whose three RTM_NEWLINK replies hold exactly
// those attribute types.
//
// goip reads netlink and only netlink, so there is nothing for it to have
// missed and nothing for it to print. internal/goipparity/stdout.go:63-68
// already says precisely this, and the parity allowlist already carries the
// `stdout:` entry that keeps `-6 addr show` gateable over it. This helper is
// the offline half of the same accommodation.
//
// Note it is a 7.1.0 behavior specifically: the fallback is gone from
// iproute2 7.2.0, which prints no qlen here at all. The pin is 7.1.0
// (nix/upstream-pins.json), and the goldens were captured with it.
//
// The token is removed WITHOUT its leading space, because that space belongs
// to the preceding `state %s ` — which is why `ip` itself leaves a trailing
// space on the line whenever print_queuelen declines to print.
//
// Fails if there is nothing to remove, for the same reason
// sidecarWithCapturePromisc does.
func sidecarWithoutIoctlQlen(t *testing.T, s string) string {
	t.Helper()
	const token = "qlen 1000"
	if !strings.Contains(s, token) {
		t.Fatalf("no %q to remove; the ioctl fallback is the only reason this "+
			"sidecar has a qlen at all, so its absence means the skew is gone "+
			"and this helper should go with it", token)
	}
	return strings.ReplaceAll(s, token, "")
}

// TestLinkShowDetailMatchesSidecar is `goip -d link show` against ip_link_n,
// line for line.
//
// This is the sidecar that has been committed and unread the longest, and it
// is the widest single comparison in the corpus: three links, sixteen tokens
// each, two of them opening continuation lines.
//
// go test ./internal/goip/ -run TestLinkShowDetailMatchesSidecar
func TestLinkShowDetailMatchesSidecar(t *testing.T) {
	tests := []struct {
		description string
		args        []string
		pcap        string
		sidecar     string
	}{
		{
			description: "positive: the clean topology reproduces ip_link_n, detail run included",
			args:        []string{"-d", "link", "show"},
			pcap:        guestDumpsDir + "netlink_route_getlink.pcap",
			sidecar:     guestDumpsDir + "ip_link_n",
		},
		{
			// `-d addr show` is the same stanza minus two things, and the
			// second is the interesting one: no `mode DEFAULT`, because
			// do_link is unset, and no `addrgenmode`, for exactly the same
			// reason — print_af_spec is guarded on the same variable
			// (ip/ipaddress.c:1043, :1185-1186). One flag, two tokens, in
			// different parts of the stanza.
			description: "positive: the addr object reproduces ip_addr_n, which has no addrgenmode anywhere",
			args:        []string{"-d", "addr", "show"},
			pcap:        guestDumpsDir + "netlink_route_getaddr.pcap",
			sidecar:     guestDumpsDir + "ip_addr_n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			want := sidecarWithCapturePromisc(t, tc.sidecar)
			t.Setenv("GOIP_REPLAY", tc.pcap)
			var stdout, stderr bytes.Buffer
			if code := Run(tc.args, &stdout, &stderr); code != ExitOK {
				t.Fatalf("Run(%q) = %d, stderr=%s", tc.args, code, stderr.String())
			}
			assertLinesEqual(t, stdout.String(), want)
		})
	}
}

// TestAddrShowDetailFamilyMatchesSidecars is the -4 and -6 arms, and the pair
// is the strongest evidence in the corpus that the detail run comes from the
// ATTRIBUTES and not from the flag.
//
// Both commands set show_details. Both therefore restore the `link/` line,
// which `ip -4 addr show` and `ip -6 addr show` suppress — the guard at
// ip/ipaddress.c:1060 has a `|| show_details` arm. And then they diverge
// completely: the -4 form prints the whole detail run and the -6 form prints
// not one token of it, because its link dump is AF_INET6 and the kernel
// answers that with inet6_dump_ifinfo's six attributes, none of which is a
// detail attribute.
//
// A renderer that emitted the run because -d was given would pass the -4 row
// and fail the -6 one with fourteen zeros. There is no other place in the
// corpus where that mistake is visible.
//
// Neither sidecar needs the promiscuity substitution: nlmon0 has no address,
// so `addr show` drops it before it reaches the renderer.
//
// go test ./internal/goip/ -run TestAddrShowDetailFamilyMatchesSidecars
func TestAddrShowDetailFamilyMatchesSidecars(t *testing.T) {
	tests := []struct {
		description   string
		args          []string
		pcap          string
		sidecar       string
		dropIoctlQlen bool
	}{
		{
			description: "positive: -d -4 addr show restores the link/ line AND carries the full detail run",
			args:        []string{"-d", "-4", "addr", "show"},
			pcap:        guestDumpsDir + "netlink_route_getaddr_v4.pcap",
			sidecar:     guestDumpsDir + "ip_addr_v4_n",
		},
		{
			description: "boundary: -d -6 addr show restores the link/ line and prints no detail token, because none arrived",
			args:        []string{"-d", "-6", "addr", "show"},
			pcap:        guestDumpsDir + "netlink_route_getaddr_v6.pcap",
			sidecar:     guestDumpsDir + "ip_addr_v6_n",
			// The SAME dump that carries no detail attribute also carries no
			// IFLA_TXQLEN, and 7.1.0 fills that one in from an ioctl. One
			// cause, two missing tokens, and only the qlen has a syscall
			// behind it. See sidecarWithoutIoctlQlen.
			dropIoctlQlen: true,
		},
		{
			// The other two namespaces, and they are here for a reason that
			// the clean row cannot carry: `-d -4 addr show` is REFUSED in
			// both of them, because the AF_INET link dump brings
			// IFLA_LINKINFO and a bridge's or a tunnel's per-kind nest with
			// it (TestDetailRefusedForPerKindData). The v6 form renders in
			// the same namespace, off the same devices, because
			// inet6_dump_ifinfo sends no IFLA_LINKINFO at all.
			//
			// So the two families split on whether goip can render -d AT ALL,
			// within one topology. That is the sharpest statement in the
			// corpus of the fact the -6 row above makes mildly: the detail
			// run is a function of the attributes, and here it decides
			// whether there is any output to compare.
			description:   "corner: -d -6 addr show renders the mesh topology, where -d -4 addr show is refused outright",
			args:          []string{"-d", "-6", "addr", "show"},
			pcap:          guestDumpsDir + "mesh/netlink_route_getaddr_v6.pcap",
			sidecar:       guestDumpsDir + "mesh/ip_addr_v6_n",
			dropIoctlQlen: true,
		},
		{
			// And the tunnel namespace, whose five configured devices put the
			// widest v6 listing in the corpus through this path — ten lines
			// where the clean set has eight.
			description:   "corner: the same for the tunnel topology, whose -d -4 form is refused on tunl0",
			args:          []string{"-d", "-6", "addr", "show"},
			pcap:          guestDumpsDir + "tunnel/netlink_route_getaddr_v6.pcap",
			sidecar:       guestDumpsDir + "tunnel/ip_addr_v6_n",
			dropIoctlQlen: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			raw, err := os.ReadFile(tc.sidecar)
			if err != nil {
				t.Fatal(err)
			}
			want := string(raw)
			if tc.dropIoctlQlen {
				want = sidecarWithoutIoctlQlen(t, want)
			}
			t.Setenv("GOIP_REPLAY", tc.pcap)
			var stdout, stderr bytes.Buffer
			if code := Run(tc.args, &stdout, &stderr); code != ExitOK {
				t.Fatalf("Run(%q) = %d, stderr=%s", tc.args, code, stderr.String())
			}
			assertLinesEqual(t, stdout.String(), want)
		})
	}
}

// TestAddrShowFamilyLinkLineIsDetailGated is the negative control for the row
// above: without -d, the same two commands print no `link/` line at all.
//
// It matters because the -6 row of that test asserts a line with nothing after
// it, and "the line came back empty" and "the line never came back" look the
// same if you only read the detail tokens. This reads the line.
//
// go test ./internal/goip/ -run TestAddrShowFamilyLinkLineIsDetailGated
func TestAddrShowFamilyLinkLineIsDetailGated(t *testing.T) {
	tests := []struct {
		description  string
		args         []string
		pcap         string
		wantLinkLine bool
	}{
		{
			description:  "negative: -6 addr show prints no link/ line, which is what -d restores",
			args:         []string{"-6", "addr", "show"},
			pcap:         guestDumpsDir + "netlink_route_getaddr_v6.pcap",
			wantLinkLine: false,
		},
		{
			description:  "positive: -d -6 addr show prints one",
			args:         []string{"-d", "-6", "addr", "show"},
			pcap:         guestDumpsDir + "netlink_route_getaddr_v6.pcap",
			wantLinkLine: true,
		},
		{
			description:  "negative: -4 addr show prints no link/ line either",
			args:         []string{"-4", "addr", "show"},
			pcap:         guestDumpsDir + "netlink_route_getaddr_v4.pcap",
			wantLinkLine: false,
		},
		{
			// The control that keeps the guard from being read as "-d
			// controls the link/ line": AF_UNSPEC satisfies the first
			// disjunct on its own, so the line is there without -d.
			description:  "control: a bare addr show prints it with no -d at all, because the family is AF_UNSPEC",
			args:         []string{"addr", "show"},
			pcap:         guestDumpsDir + "netlink_route_getaddr.pcap",
			wantLinkLine: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Setenv("GOIP_REPLAY", tc.pcap)
			var stdout, stderr bytes.Buffer
			if code := Run(tc.args, &stdout, &stderr); code != ExitOK {
				t.Fatalf("Run(%q) = %d, stderr=%s", tc.args, code, stderr.String())
			}
			got := strings.Contains(stdout.String(), "\n    link/")
			if got != tc.wantLinkLine {
				t.Errorf("link/ line present = %v, want %v\noutput:\n%s",
					got, tc.wantLinkLine, stdout.String())
			}
		})
	}
}

// TestRouteShowDetailMatchesSidecars is `ip -d route show` in its three
// captured forms, compared exactly.
//
// No substitution and no normalization: a route carries nothing that moves
// between two invocations on a quiescent namespace, and nlmon0 has no routes.
// These three are the cleanest end-to-end comparisons -d has.
//
// go test ./internal/goip/ -run TestRouteShowDetailMatchesSidecars
func TestRouteShowDetailMatchesSidecars(t *testing.T) {
	tests := []struct {
		description string
		args        []string
		pcap        string
		sidecar     string
	}{
		{
			// Adds `unicast` to all six lines, and `proto boot scope global`
			// to the five that were defaulting.
			description: "positive: -d route show reproduces ip_route_main_n, type proto and scope unsuppressed",
			args:        []string{"-d", "route", "show"},
			pcap:        guestDumpsDir + "netlink_route_getroute.pcap",
			sidecar:     guestDumpsDir + "ip_route_main_n",
		},
		{
			// The v6 arm, where the restored token is `scope global` on every
			// line — RT_SCOPE_UNIVERSE is what the kernel puts on a v6 route.
			description: "positive: -d -6 route show reproduces ip_route6_n, with scope global on every line",
			args:        []string{"-d", "-6", "route", "show"},
			pcap:        guestDumpsDir + "netlink_route_getroute6.pcap",
			sidecar:     guestDumpsDir + "ip_route6_n",
		},
		{
			// The only form that adds the fourth token. `table` needs
			// filter.tb == 0 as well as -d (ip/iproute.c:903), so `table
			// main` appears here and nowhere else — and the local-table lines
			// that already carried `table local` gain `scope global` instead.
			description: "positive: -d route show table all reproduces ip_route_table_all_n, table token included",
			args:        []string{"-d", "route", "show", "table", "all"},
			pcap:        guestDumpsDir + "netlink_route_getroute_table_all.pcap",
			sidecar:     guestDumpsDir + "ip_route_table_all_n",
		},

		// The same three forms in the other two namespaces. The route object
		// is the only one that reaches every namespace under -d — link and
		// addr are refused in both of these for their per-kind nests — so
		// these six rows are the whole of what -d can be compared against
		// outside the clean topology.
		{
			// br0's routes, and the one thing the clean set has no route for:
			// RTNH_F_LINKDOWN. veth0's peer is left down, so the bridge has
			// no carrier and both of its routes carry the flag, which -d
			// renders alongside the restored type and scope tokens.
			description: "positive: -d route show reproduces mesh/ip_route_main_n, linkdown flag and all",
			args:        []string{"-d", "route", "show"},
			pcap:        guestDumpsDir + "mesh/netlink_route_getroute.pcap",
			sidecar:     guestDumpsDir + "mesh/ip_route_main_n",
		},
		{
			description: "positive: -d -6 route show reproduces mesh/ip_route6_n",
			args:        []string{"-d", "-6", "route", "show"},
			pcap:        guestDumpsDir + "mesh/netlink_route_getroute6.pcap",
			sidecar:     guestDumpsDir + "mesh/ip_route6_n",
		},
		{
			description: "positive: -d route show table all reproduces mesh/ip_route_table_all_n",
			args:        []string{"-d", "route", "show", "table", "all"},
			pcap:        guestDumpsDir + "mesh/netlink_route_getroute_table_all.pcap",
			sidecar:     guestDumpsDir + "mesh/ip_route_table_all_n",
		},
		{
			// Routes on a tunnel device, which is the only place in the
			// corpus they exist. `gre1` has no RTA_GATEWAY on either route
			// and a /25 that is not the device's own prefix, so the detail
			// run lands on a shape the dummy and the bridge both lack.
			description: "positive: -d route show reproduces tunnel/ip_route_main_n, routes on a gre device",
			args:        []string{"-d", "route", "show"},
			pcap:        guestDumpsDir + "tunnel/netlink_route_getroute.pcap",
			sidecar:     guestDumpsDir + "tunnel/ip_route_main_n",
		},
		{
			description: "positive: -d -6 route show reproduces tunnel/ip_route6_n",
			args:        []string{"-d", "-6", "route", "show"},
			pcap:        guestDumpsDir + "tunnel/netlink_route_getroute6.pcap",
			sidecar:     guestDumpsDir + "tunnel/ip_route6_n",
		},
		{
			description: "positive: -d route show table all reproduces tunnel/ip_route_table_all_n, thirteen lines across five tunnel devices",
			args:        []string{"-d", "route", "show", "table", "all"},
			pcap:        guestDumpsDir + "tunnel/netlink_route_getroute_table_all.pcap",
			sidecar:     guestDumpsDir + "tunnel/ip_route_table_all_n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			raw, err := os.ReadFile(tc.sidecar)
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("GOIP_REPLAY", tc.pcap)
			var stdout, stderr bytes.Buffer
			if code := Run(tc.args, &stdout, &stderr); code != ExitOK {
				t.Fatalf("Run(%q) = %d, stderr=%s", tc.args, code, stderr.String())
			}
			assertLinesEqual(t, stdout.String(), string(raw))
		})
	}
}

// TestNeighShowIgnoresDetails asserts that -d changes nothing for the neigh
// object, which is a real row and not a triviality.
//
// ip/ipneigh.c does not contain the identifier show_details, so `ip neigh
// show` is the only command goip implements whose output -d leaves untouched —
// and the committed pair says so with no diff at all, ip_neigh and ip_neigh_n
// being the same file.
//
// That last sentence used to be prose. Both sidecars are now READ and
// compared to each other, so "the pair is identical" is a claim about the
// bytes `ip` wrote rather than about what someone saw once: a future capture
// against an iproute2 that grew a show_details branch in ipneigh.c fails here
// on the sidecars alone, before goip is consulted.
//
// The reason the goip half needs asserting is that "-d changed nothing" and
// "-d was dropped on the floor" are indistinguishable HERE and distinguishable
// nowhere else in one command. Pairing it with the route and link tests, which
// fail if -d is dropped, is what makes this an assertion about ipneigh.c
// rather than about the plumbing.
//
// # The empty rows are the point of the mesh namespace
//
// build_mesh adds no neighbors, so mesh/ip_neigh and mesh/ip_neigh_n are both
// zero bytes. wantEmpty makes that the expectation instead of a skipped row:
// the pcap holds a real request and a real NLMSG_DONE, so "goip printed
// nothing" is being distinguished from "goip failed to ask", which an absent
// row would not distinguish at all.
//
// go test ./internal/goip/ -run TestNeighShowIgnoresDetails
func TestNeighShowIgnoresDetails(t *testing.T) {
	tests := []struct {
		description string
		plain       []string
		detailed    []string
		pcap        string
		// plainSidecar and detailSidecar are `ip`'s own two renderings. They
		// are asserted equal to each other AND equal to goip's output, which
		// is three comparisons from two files.
		plainSidecar  string
		detailSidecar string
		// wantEmpty inverts the non-vacuity guard for the namespaces whose
		// neighbor table is empty by construction.
		wantEmpty bool
	}{
		{
			description:   "negative: -d neigh show is byte-identical to neigh show",
			plain:         []string{"neigh", "show"},
			detailed:      []string{"-d", "neigh", "show"},
			pcap:          guestDumpsDir + "netlink_route_getneigh.pcap",
			plainSidecar:  guestDumpsDir + "ip_neigh",
			detailSidecar: guestDumpsDir + "ip_neigh_n",
		},
		{
			// The selector composes with -d the same way, i.e. not at all.
			description: "negative: -d neigh show dev is byte-identical too",
			plain:       []string{"neigh", "show", "dev", "goip0"},
			detailed:    []string{"-d", "neigh", "show", "dev", "goip0"},
			pcap:        guestDumpsDir + "netlink_route_getneigh_dev.pcap",
		},
		{
			// And on the proxy table, whose entries carry ndm_state 0 and no
			// lladdr — a different print_neigh path with, again, no
			// show_details in it.
			description: "negative: -d neigh show proxy is byte-identical too",
			plain:       []string{"neigh", "show", "proxy"},
			detailed:    []string{"-d", "neigh", "show", "proxy"},
			pcap:        guestDumpsDir + "netlink_route_getneigh_proxy.pcap",
		},
		{
			// The mesh namespace, where the answer is an empty listing from a
			// non-empty transaction. Both sidecars are zero bytes, which is
			// also the only way `ip` can say "-d changed nothing" about
			// nothing.
			description:   "boundary: the mesh table is empty by construction, and -d leaves that unchanged too",
			plain:         []string{"neigh", "show"},
			detailed:      []string{"-d", "neigh", "show"},
			pcap:          guestDumpsDir + "mesh/netlink_route_getneigh.pcap",
			plainSidecar:  guestDumpsDir + "mesh/ip_neigh",
			detailSidecar: guestDumpsDir + "mesh/ip_neigh_n",
			wantEmpty:     true,
		},
		{
			// The tunnel namespace, and the row that would catch a -d
			// implementation that reached the neigh renderer at all: these
			// are the two entries whose lladdr comes through ll_addr_n2a's
			// special cases, so a -d branch that re-derived the L2 format
			// would regress them to colon-hex here while the clean row, all
			// of whose neighbors sit on an ARPHRD_ETHER dummy, still passed.
			description:   "corner: -d leaves the tunnel namespace's two type-driven lladdrs alone",
			plain:         []string{"neigh", "show"},
			detailed:      []string{"-d", "neigh", "show"},
			pcap:          guestDumpsDir + "tunnel/netlink_route_getneigh.pcap",
			plainSidecar:  guestDumpsDir + "tunnel/ip_neigh",
			detailSidecar: guestDumpsDir + "tunnel/ip_neigh_n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			run := func(args []string) string {
				t.Helper()
				t.Setenv("GOIP_REPLAY", tc.pcap)
				var stdout, stderr bytes.Buffer
				if code := Run(args, &stdout, &stderr); code != ExitOK {
					t.Fatalf("Run(%q) = %d, stderr=%s", args, code, stderr.String())
				}
				return stdout.String()
			}
			plain, detailed := run(tc.plain), run(tc.detailed)
			if plain != detailed {
				t.Errorf("-d changed the output\n plain: %q\ndetail: %q", plain, detailed)
			}
			if (plain == "") != tc.wantEmpty {
				t.Fatalf("output empty = %v, want %v; a row that compares two "+
					"empty strings by accident compares nothing", plain == "", tc.wantEmpty)
			}

			if tc.plainSidecar == "" {
				return
			}
			wantPlain, err := os.ReadFile(tc.plainSidecar)
			if err != nil {
				t.Fatal(err)
			}
			wantDetail, err := os.ReadFile(tc.detailSidecar)
			if err != nil {
				t.Fatal(err)
			}
			// Byte-exact, and it can be: the two sidecars are two separate
			// `ip` invocations, so this also pins that the neighbor hash
			// order is stable within a boot for an unchanging table. All
			// three namespaces' pairs are identical today.
			if !bytes.Equal(wantPlain, wantDetail) {
				t.Errorf("%s and %s differ, so `ip` itself renders -d differently "+
					"and this whole test is measuring the wrong thing\n plain: %q\ndetail: %q",
					tc.plainSidecar, tc.detailSidecar, wantPlain, wantDetail)
			}

			// Line multiset, not bytes, and only against goip. goip sorts
			// neighbors where `ip` prints hash-bucket order — deliberate and
			// documented at internal/goip/model/model.go:25, pre-existing,
			// and the reason every other table in obj_neigh_test.go that
			// compares a multi-entry listing is `unordered` too. The clean
			// namespace is the only one of the three with enough entries for
			// the two orders to disagree.
			assertSameLines(t, plain, string(wantPlain))
		})
	}
}

// TestDetailRefusedForPerKindData drives the refusal against the two corpora
// that trigger it.
//
// # What is being defended
//
// print_linktype prints the kind token and then print_opt's whole output ON
// THE SAME LINE, so a renderer that stopped at the kind would emit a line that
// says something different from `ip`'s rather than a shorter one. The tunnel
// sidecar shows the size of the gap directly — tunnel/ip_link_n:3 reads
//
//	ipip any remote any local any ttl inherit nopmtudisc numtxqueues 1 …
//
// and everything between "ipip" and "numtxqueues" comes out of a print_opt
// goip does not have. Emitting "ipip numtxqueues 1" would reach the parity
// harness as a rendering bug with nothing to say a subsystem was absent.
//
// # What it costs
//
// The refusal is per-COMMAND, not per-link: one bridge in a namespace means
// `-d link show` prints nothing there. That is deliberate and it follows the
// `-s -s` precedent — goip is a coverage test for pkg/xtcpnl, so "I cannot
// reproduce this output" is a more useful answer than most of it.
//
// # The sidecars are read, not cited
//
// The four `_n` sidecars for the refused commands are the only committed
// evidence of what goip is declining to render, and wantSidecarTokens asserts
// the tokens are there. That direction matters: the refusal is justified by
// the claim "`ip` prints per-kind data here", and if an iproute2 ever stopped
// printing it the refusal would be returning ExitUsage for output goip could
// in fact reproduce. The sidecar is the only thing that can notice.
//
// go test ./internal/goip/ -run TestDetailRefusedForPerKindData
func TestDetailRefusedForPerKindData(t *testing.T) {
	tests := []struct {
		description string
		args        []string
		pcap        string
		wantCode    int
		wantErr     bool
		wantStderr  []string
		// sidecar is `ip`'s rendering of the refused command, and
		// wantSidecarTokens the per-kind tokens it must carry — the output
		// goip is declining to approximate.
		sidecar           string
		wantSidecarTokens []string
	}{
		{
			// br0's IFLA_INFO_DATA is 800-odd bytes of bridge parameters that
			// `ip` prints in full.
			description: "negative: -d link show refuses the mesh topology, naming the bridge and its kind",
			args:        []string{"-d", "link", "show"},
			pcap:        guestDumpsDir + "mesh/netlink_route_getlink.pcap",
			wantCode:    ExitUsage,
			wantErr:     true,
			wantStderr:  []string{"br0", `"bridge"`, "IFLA_INFO_DATA"},
			// mesh/ip_link_n is not among the unread fixtures — the link
			// table reads it — so the tokens are asserted here without a
			// second reader being claimed for it.
			sidecar: guestDumpsDir + "mesh/ip_link_n",
			// bridge_id and designated_root are per-boot values; the token
			// names are not, and the names are what print_opt emits.
			wantSidecarTokens: []string{"bridge forward_delay", "bridge_id", "designated_root"},
		},
		{
			// And it refuses on tunl0, the FIRST link with data rather than
			// the first link overall, which is what says the check walks the
			// dump.
			description: "negative: -d link show refuses the tunnel topology, naming the ipip fallback device",
			args:        []string{"-d", "link", "show"},
			pcap:        guestDumpsDir + "tunnel/netlink_route_getlink.pcap",
			wantCode:    ExitUsage,
			wantErr:     true,
			wantStderr:  []string{"tunl0", `"ipip"`},
			sidecar:     guestDumpsDir + "tunnel/ip_link_n",
			// The widest print_opt run in the corpus, and the one quoted in
			// this test's own doc comment: everything between "ipip" and
			// "numtxqueues" on tunl0's line.
			wantSidecarTokens: []string{
				"ipip any remote any local any ttl inherit nopmtudisc",
				"gre remote 198.51.100.3 local 192.0.2.3 ttl inherit",
			},
		},
		{
			// The addr object reaches the same check, because it renders the
			// same link stanza through the same detail view.
			description: "negative: -d addr show refuses the mesh topology for the same reason",
			args:        []string{"-d", "addr", "show"},
			pcap:        guestDumpsDir + "mesh/netlink_route_getaddr.pcap",
			wantCode:    ExitUsage,
			wantErr:     true,
			wantStderr:  []string{"IFLA_INFO_DATA"},
		},
		{
			// Same for the tunnel namespace's addr dump, and this is the row
			// that makes tunnel/ip_addr_n evidence: 51 lines, fourteen
			// devices, every one of them carrying a print_opt run.
			description:       "negative: -d addr show refuses the tunnel topology, and tunnel/ip_addr_n is what it declines to print",
			args:              []string{"-d", "addr", "show"},
			pcap:              guestDumpsDir + "tunnel/netlink_route_getaddr.pcap",
			wantCode:          ExitUsage,
			wantErr:           true,
			wantStderr:        []string{"IFLA_INFO_DATA", `"ipip"`},
			sidecar:           guestDumpsDir + "tunnel/ip_addr_n",
			wantSidecarTokens: []string{"ipip any remote any local any ttl inherit"},
		},
		{
			// The -4 arm, in both namespaces. It is refused where the -6 arm
			// of the SAME command renders in full
			// (TestAddrShowDetailFamilyMatchesSidecars), because
			// inet6_dump_ifinfo sends no IFLA_LINKINFO — so these two rows
			// and those two are one fact stated from both sides: the refusal
			// tracks the attributes on the wire, not the option.
			description:       "corner: -d -4 addr show is refused in the mesh namespace where -d -6 addr show renders",
			args:              []string{"-d", "-4", "addr", "show"},
			pcap:              guestDumpsDir + "mesh/netlink_route_getaddr_v4.pcap",
			wantCode:          ExitUsage,
			wantErr:           true,
			wantStderr:        []string{"IFLA_INFO_DATA", `"bridge"`},
			sidecar:           guestDumpsDir + "mesh/ip_addr_v4_n",
			wantSidecarTokens: []string{"bridge forward_delay"},
		},
		{
			description:       "corner: and refused in the tunnel namespace, for the same reason on a different kind",
			args:              []string{"-d", "-4", "addr", "show"},
			pcap:              guestDumpsDir + "tunnel/netlink_route_getaddr_v4.pcap",
			wantCode:          ExitUsage,
			wantErr:           true,
			wantStderr:        []string{"IFLA_INFO_DATA"},
			sidecar:           guestDumpsDir + "tunnel/ip_addr_v4_n",
			wantSidecarTokens: []string{"gre remote 198.51.100.3 local 192.0.2.3"},
		},
		{
			// THE control. Without -d the same pcap renders fine, because
			// nothing in a plain stanza comes from the nest — so the refusal
			// is scoped to the option and has not quietly broken `link show`.
			description: "control: the same mesh pcap renders fine without -d",
			args:        []string{"link", "show"},
			pcap:        guestDumpsDir + "mesh/netlink_route_getlink.pcap",
			wantCode:    ExitOK,
			wantErr:     false,
		},
		{
			// The other control, and the one that keeps the refusal from
			// being read as "-d is unimplemented for link": the clean
			// topology carries no per-kind data and renders in full.
			description: "control: the clean topology carries no per-kind data, so -d succeeds there",
			args:        []string{"-d", "link", "show"},
			pcap:        guestDumpsDir + "netlink_route_getlink.pcap",
			wantCode:    ExitOK,
			wantErr:     false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Setenv("GOIP_REPLAY", tc.pcap)
			var stdout, stderr bytes.Buffer
			code := Run(tc.args, &stdout, &stderr)
			if code != tc.wantCode {
				t.Fatalf("Run(%q) = %d, want %d\nstderr: %s", tc.args, code, tc.wantCode, stderr.String())
			}

			if tc.sidecar != "" {
				raw, err := os.ReadFile(tc.sidecar)
				if err != nil {
					t.Fatal(err)
				}
				for _, want := range tc.wantSidecarTokens {
					if !strings.Contains(string(raw), want) {
						t.Errorf("%s does not contain %q; the refusal is justified by "+
							"`ip` printing per-kind data here, and this sidecar is the "+
							"only evidence that it does", tc.sidecar, want)
					}
				}
			}

			if !tc.wantErr {
				if stdout.Len() == 0 {
					t.Errorf("no output; a control row is meant to render something")
				}
				return
			}
			if stdout.Len() != 0 {
				t.Errorf("refused and still wrote %d bytes of stdout; a partial "+
					"render is the thing the refusal exists to prevent:\n%s",
					stdout.Len(), stdout.String())
			}
			for _, want := range tc.wantStderr {
				if !strings.Contains(stderr.String(), want) {
					t.Errorf("stderr does not mention %s:\n%s", want, stderr.String())
				}
			}
		})
	}
}

// TestRunDetailsOption covers the `-d` spelling itself, which is
// matches(opt, "-details") — an unanchored prefix, like every other iproute2
// option.
//
// go test ./internal/goip/ -run TestRunDetailsOption
func TestRunDetailsOption(t *testing.T) {
	tests := []struct {
		description string
		args        []string
		wantCode    int
		wantDetail  bool
		wantStderr  string
	}{
		{
			description: "positive: -d is the spelling every sidecar in the corpus was captured with",
			args:        []string{"-d", "link", "show"},
			wantCode:    ExitOK,
			wantDetail:  true,
		},
		{
			// matches() is a prefix test against "-details", so every
			// truncation of it works. No option earlier in iproute2's chain
			// begins with -d, which is why the one-letter form is
			// unambiguous — a property of the chain's ORDER, not of the
			// pattern.
			description: "positive: -det is the same option, because matches() is an unanchored prefix",
			args:        []string{"-det", "link", "show"},
			wantCode:    ExitOK,
			wantDetail:  true,
		},
		{
			description: "positive: the full -details works too",
			args:        []string{"-details", "link", "show"},
			wantCode:    ExitOK,
			wantDetail:  true,
		},
		{
			// One leading dash comes off when the second character is also a
			// dash (ip/ip.c:198-199), so the GNU-looking spelling is the same
			// option. See the block in Run, and goip_dash_option_test.go.
			description: "positive: --details is -details with one dash stripped",
			args:        []string{"--details", "link", "show"},
			wantCode:    ExitOK,
			wantDetail:  true,
		},
		{
			// matches() fails when the argument is LONGER than the pattern,
			// because the length test comes first — so this is not a prefix
			// of anything and there is no other option it could be.
			description: "negative: -detailsx is longer than the pattern and so matches nothing",
			args:        []string{"-detailsx", "link", "show"},
			wantCode:    ExitUsage,
			wantStderr:  "is unknown",
		},
		{
			// show_details is only ever tested for truth — nine sites, all
			// `show_details` or `show_details > 0`, none for a second one.
			// So -d -d is -d, which is the OPPOSITE of -s -s, and the
			// contrast is why both are tested.
			description: "corner: -d -d is accepted and means the same as -d, unlike -s -s",
			args:        []string{"-d", "-d", "link", "show"},
			wantCode:    ExitOK,
			wantDetail:  true,
		},
		{
			// Composes with -s, which is worth a row because both are
			// counted ints on runCtx and an object reads them separately.
			description: "positive: -d composes with -s, and both runs appear",
			args:        []string{"-d", "-s", "link", "show"},
			wantCode:    ExitOK,
			wantDetail:  true,
		},
		{
			description: "negative: without it, no detail token appears",
			args:        []string{"link", "show"},
			wantCode:    ExitOK,
			wantDetail:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Setenv("GOIP_REPLAY", guestDumpsDir+"netlink_route_getlink.pcap")
			var stdout, stderr bytes.Buffer
			code := Run(tc.args, &stdout, &stderr)
			if code != tc.wantCode {
				t.Fatalf("Run(%q) = %d, want %d\nstderr: %s", tc.args, code, tc.wantCode, stderr.String())
			}
			if tc.wantStderr != "" && !strings.Contains(stderr.String(), tc.wantStderr) {
				t.Errorf("stderr does not contain %q:\n%s", tc.wantStderr, stderr.String())
			}
			if tc.wantCode != ExitOK {
				return
			}
			if got := strings.Contains(stdout.String(), "promiscuity"); got != tc.wantDetail {
				t.Errorf("detail run present = %v, want %v\noutput:\n%s",
					got, tc.wantDetail, stdout.String())
			}
		})
	}
}

// TestCheckRouteDetailSupported covers the refusal `-d` needs on the route
// object, which is a different KIND of gap from the link one.
//
// The link refusal is about rendering. This one is about TRAFFIC: under -d,
// print_route follows RTA_NH_ID into print_cache_nexthop_id, which calls
// ipnh_cache_add and sends a live RTM_GETNEXTHOP (ip/iproute.c:1002-1004,
// ip/ipnexthop.c:784-798). So -d on such a route turns one dump into a dump
// plus a single-get per distinct nexthop id, and a goip that rendered the
// routes and skipped the block would diverge on the wire — the harness's
// highest-value assertion — as well as on stdout.
//
// It is driven directly rather than through Run because no topology the
// capture builds creates a nexthop object, so no committed pcap can reach it.
// That is the same reason the check is three lines rather than an assumption.
//
// go test ./internal/goip/ -run TestCheckRouteDetailSupported
func TestCheckRouteDetailSupported(t *testing.T) {
	tests := []struct {
		description string
		details     bool
		nhIDs       []uint32
		wantErr     bool
		wantStderr  string
	}{
		{
			description: "negative: -d over a route carrying RTA_NH_ID is refused, and the id is named",
			details:     true,
			nhIDs:       []uint32{0, 17, 0},
			wantErr:     true,
			wantStderr:  "nhid 17",
		},
		{
			// Without -d the same route is fine: print_route's `nhid %u`
			// token sits OUTSIDE the guard (ip/iproute.c:859-861) and sends
			// nothing, so goip renders it as it always has. The refusal is
			// conditioned on the option, not on the attribute, and this row
			// is what says so.
			description: "control: the same route without -d is not refused, because only the -d block is a transaction",
			details:     false,
			nhIDs:       []uint32{0, 17, 0},
			wantErr:     false,
		},
		{
			// The committed corpus, in effect: every route in it has NhID 0.
			description: "control: -d over routes with no RTA_NH_ID is fine, which is every route in the corpus",
			details:     true,
			nhIDs:       []uint32{0, 0, 0},
			wantErr:     false,
		},
		{
			description: "boundary: an empty route set is not refused",
			details:     true,
			nhIDs:       nil,
			wantErr:     false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			c := &runCtx{}
			if tc.details {
				c.showDetails = 1
			}
			routes := make([]model.Route, 0, len(tc.nhIDs))
			for _, id := range tc.nhIDs {
				routes = append(routes, model.Route{NhID: id})
			}
			err := checkRouteDetailSupported(c, routes)
			if tc.wantErr {
				if err == nil {
					t.Fatal("no error, want one")
				}
				if !errors.Is(err, ErrNotImplemented) {
					t.Errorf("error is not ErrNotImplemented: %v", err)
				}
				if !strings.Contains(err.Error(), tc.wantStderr) {
					t.Errorf("error does not mention %q: %v", tc.wantStderr, err)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
