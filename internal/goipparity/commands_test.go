package goipparity

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
)

// The command table is the harness's single source of truth for what gets
// captured, what the capture files are called, and which name the allowlist is
// keyed on. Every test here exists because a drift between those three would
// be silent: the capture loop would write a file nobody reads, or the
// comparator would look up a command the allowlist has never heard of and
// suppress nothing while reporting a pass.
//
// go test ./internal/goipparity/ -run TestCommandTable
func TestCommandTable(t *testing.T) {
	// A slug becomes a path component, so it has to survive being one. The
	// character class is deliberately tighter than the filesystem's: no dot,
	// because the capture name appends `.<side>.pcap` and a dot in the stem
	// would make `cut -d.` in the guest driver disagree with Go's parsing.
	safeSlug := regexp.MustCompile(`^[a-z0-9_]+$`)

	tests := []struct {
		description string
		check       func(t *testing.T)
	}{
		{
			description: "positive: every row has a name, a slug, at least one argument and a floor above zero",
			check: func(t *testing.T) {
				for _, c := range Commands() {
					switch {
					case c.Name == "":
						t.Errorf("a row has no Name: %+v", c)
					case c.Slug == "":
						t.Errorf("%q has no Slug", c.Name)
					case len(c.Args) == 0:
						t.Errorf("%q has no Args", c.Name)
					case c.Floor <= 0:
						t.Errorf("%q has Floor %d, which would accept an empty capture",
							c.Name, c.Floor)
					}
				}
			},
		},
		{
			description: "positive: the slugs are unique, so no two commands write the same capture file",
			check: func(t *testing.T) {
				seen := map[string]string{}
				for _, c := range Commands() {
					if prev, dup := seen[c.Slug]; dup {
						t.Errorf("slug %q is shared by %q and %q; one capture would "+
							"overwrite the other", c.Slug, prev, c.Name)
					}
					seen[c.Slug] = c.Name
				}
			},
		},
		{
			description: "positive: the names are unique, so Lookup cannot be ambiguous",
			check: func(t *testing.T) {
				seen := map[string]bool{}
				for _, c := range Commands() {
					if seen[c.Name] {
						t.Errorf("name %q appears twice", c.Name)
					}
					seen[c.Name] = true
				}
			},
		},
		{
			description: "positive: every slug is a safe path component of [a-z0-9_]",
			check: func(t *testing.T) {
				for _, c := range Commands() {
					if !safeSlug.MatchString(c.Slug) {
						t.Errorf("%q has slug %q, which is not a safe filename stem",
							c.Name, c.Slug)
					}
				}
			},
		},
		{
			description: "positive: the name is the Args joined by spaces, so the allowlist key and the argv cannot drift",
			check: func(t *testing.T) {
				// This holds for the dev-taking command too, because the
				// device lives in NeedsDev rather than in Args: the name
				// stops at `dev` so the allowlist key does not depend on
				// what the topology called the interface.
				for _, c := range Commands() {
					if want := strings.Join(c.Args, " "); c.Name != want {
						t.Errorf("%q has Args %q, which join to %q", c.Name, c.Args, want)
					}
				}
			},
		},
		{
			description: "positive: Lookup finds an implemented command and returns its row",
			check: func(t *testing.T) {
				c, err := Lookup("link show")
				if err != nil {
					t.Fatalf("Lookup(\"link show\"): %v", err)
				}
				if c.Slug != "link_show" || !c.Implemented || c.Floor != 2 {
					t.Errorf("got %+v, want slug link_show, implemented, floor 2", c)
				}
			},
		},
		{
			description: "positive: at least one command is implemented, so the compared branch of the report is reachable from the real table",
			check: func(t *testing.T) {
				// This row used to require an unimplemented command too, so
				// that both branches of the report were exercised by the real
				// table. Every command in the table is implemented as of
				// `route show`, and the honest response is to move the SKIP
				// coverage rather than keep a command unimplemented to feed a
				// test: TestCompareOneUnimplemented and TestRenderSkip drive
				// that branch with a synthetic Command, which is stronger
				// anyway because it does not decay the next time a row is
				// implemented.
				var impl int
				for _, c := range Commands() {
					if c.Implemented {
						impl++
					}
				}
				if impl == 0 {
					t.Error("no command is implemented; the whole compared branch of " +
						"the report is untested")
				}
			},
		},
		{
			description: "negative: Lookup of a name that is not in the table is ErrUnknownCommand",
			check: func(t *testing.T) {
				if _, err := Lookup("bridge show"); !errors.Is(err, ErrUnknownCommand) {
					t.Errorf("err = %v, want ErrUnknownCommand", err)
				}
			},
		},
		{
			description: "negative: Lookup is exact, not prefix — `link` does not resolve to `link show`",
			check: func(t *testing.T) {
				// Prefix matching is right for the CLI (iproute2's matches())
				// and wrong here, because this name is an allowlist key. A
				// prefix-matched allowlist is one whose entries silently
				// widen to cover commands nobody wrote a reason for.
				if _, err := Lookup("link"); !errors.Is(err, ErrUnknownCommand) {
					t.Errorf("err = %v, want ErrUnknownCommand", err)
				}
			},
		},
		{
			description: "negative: Lookup is case-sensitive",
			check: func(t *testing.T) {
				if _, err := Lookup("Link Show"); !errors.Is(err, ErrUnknownCommand) {
					t.Errorf("err = %v, want ErrUnknownCommand", err)
				}
			},
		},
		{
			description: "boundary: Lookup of the empty string is ErrUnknownCommand, not the first row",
			check: func(t *testing.T) {
				if _, err := Lookup(""); !errors.Is(err, ErrUnknownCommand) {
					t.Errorf("err = %v, want ErrUnknownCommand", err)
				}
			},
		},
		{
			description: "boundary: the dev-taking commands are exactly the ones named `... dev`, and only their Argv grows",
			check: func(t *testing.T) {
				// Named as a set rather than counted, because the count is
				// not the property. What matters is that NeedsDev and the
				// trailing `dev` keyword in Name agree in BOTH directions: a
				// row with the keyword and no flag would run `ip addr show
				// dev` with no device, and a row with the flag and no keyword
				// would append a bare name to a command that does not take
				// one. Either way the driver captures something, the
				// comparator finds a triple, and the run looks green.
				want := map[string]bool{
					"link show dev":             true,
					"addr show dev":             true,
					"route show dev":            true,
					"neigh show dev":            true,
					"netconf show dev":          true,
					"-4 netconf show dev":       true,
					"stats show group link dev": true,
				}
				for _, c := range Commands() {
					named := strings.HasSuffix(c.Name, " dev")
					if named != c.NeedsDev {
						t.Errorf("%q: Name ends in %q = %v, NeedsDev = %v; the two must agree",
							c.Name, " dev", named, c.NeedsDev)
					}
					if c.NeedsDev && !want[c.Name] {
						t.Errorf("%q takes a device and is not in the expected set", c.Name)
					}
					delete(want, c.Name)
					if got := len(c.Argv("goip0")); got != len(c.Args)+boolToInt(c.NeedsDev) {
						t.Errorf("%q: Argv is %d long, want %d",
							c.Name, got, len(c.Args)+boolToInt(c.NeedsDev))
					}
				}
				for name := range want {
					t.Errorf("%q is missing from the table", name)
				}
			},
		},
		{
			description: "boundary: Argv on a dev-taking command with an empty device appends an empty element rather than dropping it",
			check: func(t *testing.T) {
				// Dropping it would turn `link show dev ""` into `link show`,
				// a different command that succeeds — so the driver would
				// compare the wrong thing and pass. An empty trailing
				// argument makes `ip` fail loudly instead.
				c, err := Lookup("link show dev")
				if err != nil {
					t.Fatal(err)
				}
				got := c.Argv("")
				if len(got) != 4 || got[3] != "" {
					t.Errorf("Argv(\"\") = %q, want a 4th empty element", got)
				}
			},
		},
		{
			description: "corner: mutating the slice Commands returns does not change the table",
			check: func(t *testing.T) {
				first := Commands()[0].Name
				got := Commands()
				got[0].Name = "clobbered"
				if Commands()[0].Name != first {
					t.Errorf("the table was mutated through the returned slice")
				}
			},
		},
		{
			description: "corner: mutating the slice Argv returns does not change the row's Args",
			check: func(t *testing.T) {
				c, err := Lookup("link show")
				if err != nil {
					t.Fatal(err)
				}
				argv := c.Argv("")
				argv[0] = "clobbered"
				if c.Args[0] != "link" {
					t.Errorf("Args was mutated through Argv's result: %q", c.Args)
				}
			},
		},
		{
			description: "corner: the three sides are distinct and in ip, goip, ip order",
			check: func(t *testing.T) {
				// The order is what makes D_control a measurement across a
				// window containing the goip run rather than beside it. A
				// reordering here would keep every other test green.
				if got := Sides(); len(got) != 3 ||
					got[0] != SideIPA || got[1] != SideGoip || got[2] != SideIPB {
					t.Errorf("Sides() = %q, want [ip_a goip ip_b]", got)
				}
			},
		},
		{
			description: "corner: the capture and stdout names of one command differ across all three sides",
			check: func(t *testing.T) {
				c, err := Lookup("addr show")
				if err != nil {
					t.Fatal(err)
				}
				seen := map[string]bool{}
				for _, s := range Sides() {
					for _, n := range []string{c.CaptureName(s), c.StdoutName(s)} {
						if seen[n] {
							t.Errorf("name %q is produced twice", n)
						}
						seen[n] = true
					}
				}
				if len(seen) != 6 {
					t.Errorf("got %d distinct names for one command, want 6", len(seen))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, tt.check)
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// TestCommandsCoverAllowlist is the drift guard between this table and the
// committed allowlist.
//
// An allowlist entry naming a command the harness never runs suppresses
// nothing, forever, while reading like a decision somebody made — the same
// failure mode as an entry whose locus is prose rather than derived, which
// TestCommittedAllowlistLociAreDerivable pins on the other side. Both
// directions have to hold for the allowlist to mean anything.
//
// go test ./internal/goipparity/ -run TestCommandsCoverAllowlist
func TestCommandsCoverAllowlist(t *testing.T) {
	al, err := nlparity.EmbeddedAllowlist()
	if err != nil {
		t.Fatalf("EmbeddedAllowlist: %v", err)
	}

	known := map[string]bool{}
	for _, c := range Commands() {
		known[c.Name] = true
	}

	tests := []struct {
		description string
		check       func(t *testing.T)
	}{
		{
			description: "positive: every allowlist entry names a command in the table",
			check: func(t *testing.T) {
				for _, e := range al.Entries {
					if !known[e.Command] {
						t.Errorf("allowlist entry for %q at locus %q names no command in "+
							"the table, so it can never match", e.Command, e.Locus)
					}
				}
			},
		},
		{
			description: "positive: every gated command is in the table",
			check: func(t *testing.T) {
				for _, name := range al.GatedCommands {
					if !known[name] {
						t.Errorf("gated_commands names %q, which the harness cannot run",
							name)
					}
				}
			},
		},
		{
			description: "negative: a gated command with no implementation would gate on a skip, and none does",
			check: func(t *testing.T) {
				// Gating a command goip cannot run means the build fails on a
				// skip sentinel, which is worse than not gating it: the red is
				// permanent and unrelated to any regression.
				for _, name := range al.GatedCommands {
					c, lookupErr := Lookup(name)
					if lookupErr != nil {
						continue // reported by the row above
					}
					if !c.Implemented {
						t.Errorf("%q is gated but not implemented", name)
					}
				}
			},
		},
		{
			// The description used to say empty "is the honest state until
			// the live tiers exist". Tier C exists and twenty-three of the
			// table's twenty-four commands are gated, so the row is no longer
			// about emptiness being expected — it is about emptiness still
			// being LEGAL, which matters because this package must not
			// require production gating policy to be non-empty. Which names
			// are gated is settled in pkg/nlparity, where the file lives;
			// what stays here is the log line, deliberately not a failure.
			description: "boundary: gated_commands is allowed to be empty, so this package never depends on production gating policy",
			check: func(t *testing.T) {
				if len(al.GatedCommands) != 0 {
					t.Logf("gated_commands is now %q; this row is a reminder to "+
						"check each one has reasons, not a failure", al.GatedCommands)
				}
			},
		},
		{
			description: "corner: an allowlist command that is never captured is caught even if it is spelled like a real one",
			check: func(t *testing.T) {
				// `link show dev` is in the table; `link show dev goip0` is
				// not, and that is the spelling a careless entry would use.
				if known["link show dev goip0"] {
					t.Error("the table names a command with a device baked in, which " +
						"would make the allowlist key topology-dependent")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, tt.check)
	}
}

// TestCommandString pins the tab-separated wire format the guest driver reads
// with `IFS=$'\t' read`.
//
// go test ./internal/goipparity/ -run TestCommandString
// lookupPtr is Lookup for a table row a test case points at.
func lookupPtr(t *testing.T, name string) *Command {
	t.Helper()
	c, err := Lookup(name)
	if err != nil {
		t.Fatalf("Lookup(%q): %v", name, err)
	}
	return &c
}

func TestCommandString(t *testing.T) {
	tests := []struct {
		description string
		// name is looked up in the real table. cmd, when set, is used
		// instead, for a shape the table no longer holds.
		name       string
		cmd        *Command
		wantFields []string
	}{
		{
			description: "positive: an implemented command with no device renders eight fields",
			name:        "link show",
			wantFields:  []string{"link_show", "2", "yes", "no", "no", "-", "link show", "link show"},
		},
		{
			// Synthetic since `route show` was implemented: the table holds
			// no unimplemented command any more, and the driver still has to
			// read `no` correctly the next time one is added.
			description: "positive: an unimplemented command renders impl=no",
			cmd: &Command{
				Name: "rule show", Slug: "rule_show", Floor: 2,
				Args: []string{"rule", "show"},
			},
			wantFields: []string{"rule_show", "2", "no", "no", "no", "-", "rule show", "rule show"},
		},
		{
			description: "boundary: the implemented dev-taking command renders dev=yes and its Args without a device",
			name:        "link show dev",
			wantFields: []string{
				"link_show_dev", "4", "yes", "yes", "no", "-", "link show dev", "link show dev",
			},
		},
		{
			description: "corner: a command whose name carries a leading dash keeps it, since that dash is an argument",
			name:        "-6 addr show",
			wantFields: []string{
				"addr_show_v6", "4", "yes", "no", "no", "-", "-6 addr show", "-6 addr show",
			},
		},
		{
			description: "positive: a write row renders write=yes and its setup steps joined with ';'",
			cmd:         lookupPtr(t, "link set la1 nomaster"),
			wantFields: []string{
				"link_write_nomaster", "4", "yes", "no", "yes",
				"link add bond0 type bond mode 802.3ad;link add la1 type veth peer name lb1;link set la1 master bond0",
				"link set la1 nomaster", "link set la1 nomaster",
			},
		},
		{
			description: "positive: a stats read row with Setup renders dev=no, write=no and its six setup steps",
			name:        "stats show dev la1 group xstats_slave subgroup bond suite 802.3ad",
			wantFields: []string{
				"stats_show_xstats_slave_dev_bond_3ad", "4", "yes", "no", "no",
				"link add bond0 type bond mode 802.3ad;link add la1 type veth peer name lb1;" +
					"link add la2 type veth peer name lb2;link set la1 master bond0;" +
					"link set la2 master bond0;link set bond0 up",
				"stats show dev la1 group xstats_slave subgroup bond suite 802.3ad",
				"stats show dev la1 group xstats_slave subgroup bond suite 802.3ad",
			},
		},
		{
			description: "boundary: a write row with no precondition renders setup as -",
			cmd:         lookupPtr(t, "link add d0 type dummy"),
			wantFields: []string{
				"link_write_add_dummy", "2", "yes", "no", "yes", "-",
				"link add d0 type dummy", "link add d0 type dummy",
			},
		},
		{
			description: "positive: a read row may carry Setup, and renders write=no beside it",
			cmd:         lookupPtr(t, "-d link show dev bond0"),
			wantFields: []string{
				"link_show_details_bond", "4", "yes", "no", "no",
				"link add bond0 type bond mode 802.3ad;link add la1 type veth peer name lb1;" +
					"link add la2 type veth peer name lb2;link set la1 master bond0;" +
					"link set la2 master bond0;link set bond0 up",
				"-d link show dev bond0", "-d link show dev bond0",
			},
		},
		{
			description: "corner: a route write with a leading -6 keeps it, and its setup carries -6 steps",
			cmd:         lookupPtr(t, "-6 route del 2001:db8:1::/64 dev d0"),
			wantFields: []string{
				"route_write_v6_del", "4", "yes", "no", "yes",
				"link add d0 type dummy;link set d0 up;addr add 192.0.2.1/24 dev d0;" +
					"-6 addr add 2001:db8::1/64 nodad dev d0;-6 route add 2001:db8:1::/64 dev d0",
				"-6 route del 2001:db8:1::/64 dev d0", "-6 route del 2001:db8:1::/64 dev d0",
			},
		},
		{
			description: "corner: a single setup step has no separator",
			cmd:         lookupPtr(t, "link replace bond0 type bond"),
			wantFields: []string{
				"link_write_replace_existing", "2", "yes", "no", "yes",
				"link add bond0 type bond mode 802.3ad",
				"link replace bond0 type bond", "link replace bond0 type bond",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			var c Command
			if tt.cmd != nil {
				c = *tt.cmd
			} else {
				var err error
				c, err = Lookup(tt.name)
				if err != nil {
					t.Fatalf("Lookup(%q): %v", tt.name, err)
				}
			}
			got := strings.Split(c.String(), "\t")
			if len(got) != len(tt.wantFields) {
				t.Fatalf("String() = %q, split to %d fields, want %d",
					c.String(), len(got), len(tt.wantFields))
			}
			for i := range got {
				if got[i] != tt.wantFields[i] {
					t.Errorf("field %d = %q, want %q", i, got[i], tt.wantFields[i])
				}
			}
		})
	}

	t.Run("negative: no field except the last may contain a space, or the shell read would misparse", func(t *testing.T) {
		for _, c := range Commands() {
			f := strings.Split(c.String(), "\t")
			// The last two fields are the joined argv and the name, both of
			// which contain spaces by construction — they are last precisely
			// so a `read -r slug floor impl dev args name` still works. Every
			// leading field has to be space-free for that to hold.
			for i := 0; i < 5; i++ {
				if strings.ContainsAny(f[i], " \t") {
					t.Errorf("%q field %d = %q contains whitespace", c.Name, i, f[i])
				}
			}
		}
	})

	t.Run("corner: no field may contain a tab, or the field count would be wrong", func(t *testing.T) {
		for _, c := range Commands() {
			if n := len(strings.Split(c.String(), "\t")); n != 8 {
				t.Errorf("%q renders %d fields, want 8", c.Name, n)
			}
		}
	})

	t.Run("negative: no setup step may contain ';' or be empty, or the driver's split would change it", func(t *testing.T) {
		for _, c := range Commands() {
			for _, step := range c.Setup {
				if strings.TrimSpace(step) == "" || strings.ContainsAny(step, ";\t") {
					t.Errorf("%q setup step %q", c.Name, step)
				}
			}
		}
	})
}

// TestFamilyTableAndJSONRows pins the twelve rows added for the family and
// table selectors and for the `-j` output mode.
//
// The rest of this file is deliberately property-based — it asserts things true
// of every row, so implementing a command needs no test edit. These rows get
// named assertions anyway, for two reasons. Their FLOORS encode a claim about
// the wire that is not visible from the name: `-0 addr show` sends ONE dump
// where `addr show` sends two, and each `-j` row sends byte-identical requests
// to its text twin. And twelve rows landing ungated at once is what takes
// GOIP_PARITY_UNGATED_CLEAN (compare.go:265-266) from counting two rows to
// counting fourteen, which is the difference between a gate that can fire and
// one that cannot.
//
// go test ./internal/goipparity/ -run TestFamilyTableAndJSONRows
func TestFamilyTableAndJSONRows(t *testing.T) {
	rows := []struct {
		description string
		name        string
		slug        string
		floor       int
		implemented bool
	}{
		{
			description: "positive: -0 addr show reaches the AF_PACKET branch no other CLI input produces, and its floor is 2 because ip/ipaddress.c:2310 skips the address dump entirely",
			name:        "-0 addr show",
			slug:        "addr_show_packet",
			floor:       2,
			implemented: true,
		},
		{
			description: "positive: route show table main is the explicit spelling of the default, so it asserts routeTableID(\"main\") still resolves to 254",
			name:        "route show table main",
			slug:        "route_show_table_main",
			floor:       4,
			implemented: true,
		},
		{
			description: "positive: route show table local carries RTA_TABLE 255 and is the only row rendering route types past unicast",
			name:        "route show table local",
			slug:        "route_show_table_local",
			floor:       4,
			implemented: true,
		},
		{
			description: "positive: -4 route show is byte-identical to route show, by the AF_UNSPEC to AF_INET promotion at ip/iproute.c:1998-1999",
			name:        "-4 route show",
			slug:        "route_show_v4",
			floor:       4,
			implemented: true,
		},
		{
			description: "positive: -4 neigh show is a real request change — ndm_family becomes AF_INET at ip/ipneigh.c:513-514 — and neigh had no family row before",
			name:        "-4 neigh show",
			slug:        "neigh_show_v4",
			floor:       4,
			implemented: true,
		},
		{
			description: "positive: -6 neigh show, the v6 half of the same pair",
			name:        "-6 neigh show",
			slug:        "neigh_show_v6",
			floor:       4,
			implemented: true,
		},
		{
			description: "positive: -j addr show, the row the JSON facets were written for — the local+prefixlen composite and addr_info nesting",
			name:        "-j addr show",
			slug:        "addr_show_json",
			floor:       4,
			implemented: true,
		},
		{
			description: "positive: -j link show, five of the ten jsonKeywordKeys renames are link keys and linkmode is in no other sidecar",
			name:        "-j link show",
			slug:        "link_show_json",
			floor:       2,
			implemented: true,
		},
		{
			description: "positive: -j route show, nested metrics and the via object",
			name:        "-j route show",
			slug:        "route_show_json",
			floor:       4,
			implemented: true,
		},
		{
			description: "positive: -j neigh show, flag tokens as JSON nulls",
			name:        "-j neigh show",
			slug:        "neigh_show_json",
			floor:       4,
			implemented: true,
		},
		{
			description: "positive: -j rule show, priority filed under ifindexes and the src/dst composites",
			name:        "-j rule show",
			slug:        "rule_show_json",
			floor:       2,
			implemented: true,
		},
		{
			description: "positive: -j -s link show, the only row exercising stats64 to statsheaders; two options that combine because each is its own case in goip's option loop",
			name:        "-j -s link show",
			slug:        "link_show_stats_json",
			floor:       2,
			implemented: true,
		},
		{
			description: "positive: -j nexthop show, the group array rejoined to the text form's slash token and nexthop's first row in this matrix",
			name:        "-j nexthop show",
			slug:        "nexthop_show_json",
			floor:       4,
			implemented: true,
		},
		{
			description: "positive: -j addrlabel show, the JSON stream that splits text's glued prefix ADDR/LEN into address+prefixlen and addrlabel's JSON row in this matrix",
			name:        "-j addrlabel show",
			slug:        "addrlabel_show_json",
			floor:       2,
			implemented: true,
		},
		{
			description: "positive: ntable show, the ll_init_map link dump then the RTM_GETNEIGHTBL dump — floor 4, the neigh shape — and ntable's first row in this matrix",
			name:        "ntable show",
			slug:        "ntable_show",
			floor:       4,
			implemented: true,
		},
		{
			description: "positive: -s ntable show, the request twin of ntable show that only ungates the config and stats blocks in print",
			name:        "-s ntable show",
			slug:        "ntable_show_stats",
			floor:       4,
			implemented: true,
		},
		{
			description: "positive: -j ntable show, the JSON stream whose only reconciled locus is the device dev names; ntable's JSON row in this matrix",
			name:        "-j ntable show",
			slug:        "ntable_show_json",
			floor:       4,
			implemented: true,
		},
		{
			description: "positive: netconf show, the ll_init_map link dump then the RTM_GETNETCONF AF_UNSPEC dump — floor 4, the ntable shape — and netconf's first row in this matrix",
			name:        "netconf show",
			slug:        "netconf_show",
			floor:       4,
			implemented: true,
		},
		{
			description: "positive: netconf show dev, the dump filtered to the named ifindex client-side, the dev resolved from the bundled link dump",
			name:        "netconf show dev",
			slug:        "netconf_show_dev",
			floor:       4,
			implemented: true,
		},
		{
			description: "positive: -4 netconf show dev, the attribute-carrying point get (NETCONFA_IFINDEX, no NLM_F_DUMP) taken when a family and an ifindex are both set",
			name:        "-4 netconf show dev",
			slug:        "netconf_show_dev4",
			floor:       4,
			implemented: true,
		},
		{
			description: "positive: -j netconf show, the JSON twin whose only agreeing axis is the entry count; netconf's JSON row in this matrix",
			name:        "-j netconf show",
			slug:        "netconf_show_json",
			floor:       4,
			implemented: true,
		},
		{
			description: "positive: stats show group link, the ll_init_map link dump then the RTM_GETSTATS dump — floor 4, the netconf shape — and stats' first row in this matrix, the last read-only object",
			name:        "stats show group link",
			slug:        "stats_show",
			floor:       4,
			implemented: true,
		},
		{
			description: "positive: -s stats show group link, the request twin selecting the extended RX-errors/TX-errors render off the same bytes",
			name:        "-s stats show group link",
			slug:        "stats_show_stats",
			floor:       4,
			implemented: true,
		},
		{
			description: "positive: stats show group link dev, the non-dump point get carrying the ifindex in the 12-byte header (REQUEST alone, no DUMP, no ACK)",
			name:        "stats show group link dev",
			slug:        "stats_show_dev",
			floor:       4,
			implemented: true,
		},
		{
			description: "positive: -j stats show group link, the short JSON twin (ifindex/ifname/group/stats64); stats' JSON row in this matrix",
			name:        "-j stats show group link",
			slug:        "stats_show_json",
			floor:       4,
			implemented: true,
		},
		{
			description: "positive: stats show group xstats, the whole bridge-xstats group (filter_mask 0x2); four leaves per device, bridge vlan/mcast bodies and empty bond/stp headers",
			name:        "stats show group xstats",
			slug:        "stats_show_xstats",
			floor:       4,
			implemented: true,
		},
		{
			description: "positive: -s stats show group xstats, the extended twin equal to plain (the bridge bodies have no show_stats>1 branch)",
			name:        "-s stats show group xstats",
			slug:        "stats_show_xstats_stats",
			floor:       4,
			implemented: true,
		},
		{
			description: "positive: -j stats show group xstats, the flat JSON array, one object per leaf per device",
			name:        "-j stats show group xstats",
			slug:        "stats_show_xstats_json",
			floor:       4,
			implemented: true,
		},
		{
			description: "positive: vrf show, the filtered link dump (IFLA_LINKINFO/INFO_KIND vrf), floor 2 with no ll_init_map side transaction",
			name:        "vrf show",
			slug:        "vrf_show",
			floor:       2,
			implemented: true,
		},
		{
			description: "positive: -j vrf show, the flat {name,table} array, request byte-identical to the text twin so the floor is inherited",
			name:        "-j vrf show",
			slug:        "vrf_show_json",
			floor:       2,
			implemented: true,
		},
	}

	for _, tt := range rows {
		t.Run(tt.description, func(t *testing.T) {
			c, err := Lookup(tt.name)
			if err != nil {
				t.Fatalf("Lookup(%q): %v", tt.name, err)
			}
			if c.Slug != tt.slug {
				t.Errorf("slug = %q, expected %q", c.Slug, tt.slug)
			}
			if c.Floor != tt.floor {
				t.Errorf("floor = %d, expected %d", c.Floor, tt.floor)
			}
			if c.Implemented != tt.implemented {
				t.Errorf("implemented = %t, expected %t", c.Implemented, tt.implemented)
			}
			// withArgs derives Args from Name, so a leading option only reaches
			// the child process if it is a field of the name. A row named
			// "-j addr show" whose Args lost the "-j" would compare `addr show`
			// against `addr show` and pass while testing nothing.
			//
			// Compared element by element rather than by length and first
			// token: "-j -s link show" carries TWO leading options, and a
			// check on Args[0] alone would not notice the second one going
			// missing.
			if want := strings.Fields(tt.name); !slices.Equal(c.Args, want) {
				t.Errorf("args = %q, expected %q", c.Args, want)
			}
		})
	}

	// Floor equalities, each a claim about the wire rather than about the table.
	twins := []struct {
		description string
		row, twin   string
		equal       bool
	}{
		{
			description: "corner: -0 addr show does NOT share addr show's floor, because the AF_PACKET guard skips the second dump; this is the row that catches the skip being mis-modeled",
			row:         "-0 addr show",
			twin:        "addr show",
			equal:       false,
		},
		{
			description: "corner: route show table main shares route show's floor, both reaching the wire as RTA_TABLE 254, while keeping a distinct slug and name",
			row:         "route show table main",
			twin:        "route show",
			equal:       true,
		},
		{
			description: "corner: -4 route show shares route show's floor, the family option being absorbed by the promotion rather than changing the request",
			row:         "-4 route show",
			twin:        "route show",
			equal:       true,
		},
		{
			description: "positive: -j addr show sends what addr show sends — filt_mask (ip/ipaddress.c:2017-2026) depends only on filter.vfinfo and show_stats, and every is_json_context() hit is inside a print function",
			row:         "-j addr show",
			twin:        "addr show",
			equal:       true,
		},
		{
			description: "positive: -j link show sends what link show sends",
			row:         "-j link show",
			twin:        "link show",
			equal:       true,
		},
		{
			description: "positive: -j route show sends what route show sends",
			row:         "-j route show",
			twin:        "route show",
			equal:       true,
		},
		{
			description: "positive: -j neigh show sends what neigh show sends",
			row:         "-j neigh show",
			twin:        "neigh show",
			equal:       true,
		},
		{
			description: "positive: -j rule show sends what rule show sends",
			row:         "-j rule show",
			twin:        "rule show",
			equal:       true,
		},
		{
			description: "positive: -j -s link show sends what -s link show sends, so adding -j to a stats row changes the rendering and not the request",
			row:         "-j -s link show",
			twin:        "-s link show",
			equal:       true,
		},
		{
			description: "positive: -j nexthop show sends what nexthop show sends — nexthop's text and JSON rows share the dump, differing only in the renderer",
			row:         "-j nexthop show",
			twin:        "nexthop show",
			equal:       true,
		},
		{
			description: "positive: -6 addrlabel show shares addrlabel show's floor, the family option being absorbed by the AF_UNSPEC to AF_INET6 substitution rather than changing the request",
			row:         "-6 addrlabel show",
			twin:        "addrlabel show",
			equal:       true,
		},
		{
			description: "positive: -j addrlabel show sends what addrlabel show sends — addrlabel's text and JSON rows share the dump, differing only in the renderer",
			row:         "-j addrlabel show",
			twin:        "addrlabel show",
			equal:       true,
		},
		{
			description: "positive: -s ntable show shares ntable show's floor, `-s` only ungating the config and stats blocks in print and reaching no part of rtnl_neightbldump_req",
			row:         "-s ntable show",
			twin:        "ntable show",
			equal:       true,
		},
		{
			description: "positive: -j ntable show sends what ntable show sends — ntable's text and JSON rows share the link+table dumps, differing only in the renderer",
			row:         "-j ntable show",
			twin:        "ntable show",
			equal:       true,
		},
		{
			description: "positive: -j vrf show sends what vrf show sends — vrf's text and JSON rows share the one filtered link dump, differing only in the renderer",
			row:         "-j vrf show",
			twin:        "vrf show",
			equal:       true,
		},
	}

	for _, tt := range twins {
		t.Run(tt.description, func(t *testing.T) {
			a, err := Lookup(tt.row)
			if err != nil {
				t.Fatalf("Lookup(%q): %v", tt.row, err)
			}
			b, err := Lookup(tt.twin)
			if err != nil {
				t.Fatalf("Lookup(%q): %v", tt.twin, err)
			}
			if (a.Floor == b.Floor) != tt.equal {
				t.Errorf("floor %d for %q and %d for %q: equal = %t, expected %t",
					a.Floor, tt.row, b.Floor, tt.twin, a.Floor == b.Floor, tt.equal)
			}
			if a.Slug == b.Slug || a.Name == b.Name {
				t.Errorf("%q and %q are not distinct rows: slugs %q/%q",
					tt.row, tt.twin, a.Slug, b.Slug)
			}
		})
	}
}

// TestUngatedSurfaceIsNotVacuous pins the set of matrix rows outside
// gated_commands, because GOIP_PARITY_UNGATED_CLEAN counts exactly those rows
// and a set that shrinks to nothing turns the gate into a tautology.
//
// It has been vacuous twice in this harness's history — both times because a
// gating branch landed every ungated row at once — so the count is asserted as
// a named set rather than left to be noticed on a run that cannot fail.
//
// go test ./internal/goipparity/ -run TestUngatedSurfaceIsNotVacuous
func TestUngatedSurfaceIsNotVacuous(t *testing.T) {
	al, err := nlparity.EmbeddedAllowlist()
	if err != nil {
		t.Fatalf("EmbeddedAllowlist: %v", err)
	}
	gated := make(map[string]bool, len(al.GatedCommands))
	for _, name := range al.GatedCommands {
		gated[name] = true
	}
	ungated := map[string]bool{}
	for _, c := range Commands() {
		if !gated[c.Name] {
			ungated[c.Name] = true
		}
	}

	// The queue is fourteen rows: the twelve the `-j`/family branch added, plus
	// the two -s sweep commands whose noise is unresolved and which pkg/nlparity's
	// held-out negative keeps out of gated_commands on purpose. Each still awaits
	// its own measured-clean live run. The two nexthop and three addrlabel rows
	// left this list on runs 10 and 11, their first live grounding — see
	// TestNexthopAddrlabelGated.
	//
	// The remaining eight are the stats and netconf-dev rows runs 8 and 9 measured
	// DIVERGENT, and they stay ungated for a reason the queue above does not
	// share: not "not yet measured" but "measured, and permanently divergent on
	// the wire". Both sides issue RTM_GETSTATS; the gap is name resolution —
	// goip's one up-front bulk RTM_GETLINK dump versus `ip`'s lazy per-index gets
	// (stats) and redundant per-name get (netconf-dev). That is class
	// DivergenceTransactionCount, which the allowlist cannot suppress, so these
	// eight can never gate — unlike the fourteen above, which each await their own
	// clean live run. TestPermanentWireDivergences pins the eight. The two bond
	// `-d link show` rows left this list on their first two live runs — see
	// TestBondReadRowsGated.
	//
	// Runs 8 and 9 measured nine. `stats show group link dev` left the set, and
	// this list, once goip's dev path sent ip's lookup by name in place of a
	// link dump. It gated on the same two runs as the five bond xstats point
	// gets — see TestBondXstatsRowsGated.
	//
	// `addr show dynamic` is out for a reason of its own: it prints a finite
	// valid_lft, which counts down between ip_a and goip, so it can never
	// gate (addr-write-path.md, "Gating"). TestAddrShowSelectorRowsGated pins
	// it.
	expected := []string{
		"-s addr show", "-s neigh show",
		"-0 addr show", "route show table main", "route show table local",
		"-4 route show", "-4 neigh show", "-6 neigh show",
		"-j addr show", "-j link show", "-j route show", "-j neigh show",
		"-j rule show", "-j -s link show",
		"netconf show dev", "-4 netconf show dev",
		"stats show group link", "-s stats show group link",
		"-j stats show group link",
		"stats show group xstats", "-s stats show group xstats",
		"-j stats show group xstats",
		"addr show dynamic",
	}

	tests := []struct {
		description string
		check       func(t *testing.T)
	}{
		{
			description: "positive: the ungated set is exactly the twenty-three named rows, so UNGATED_CLEAN counts twenty-three rows and not zero",
			check: func(t *testing.T) {
				for _, name := range expected {
					if !ungated[name] {
						t.Errorf("%q is expected to be ungated but is in gated_commands; "+
							"gating it is a separate branch and must edit this list", name)
					}
				}
				for name := range ungated {
					found := false
					for _, e := range expected {
						if e == name {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("%q is ungated and unaccounted for; add it here with a "+
							"reason or gate it", name)
					}
				}
			},
		},
		{
			description: "negative: an empty ungated set would make GOIP_PARITY_UNGATED_CLEAN a tautology, which is the failure this test exists to prevent",
			check: func(t *testing.T) {
				if len(ungated) == 0 {
					t.Error("every matrix row is gated, so UNGATED_CLEAN counts nothing")
				}
			},
		},
		{
			description: "boundary: every ungated row is nonetheless implemented, so UNGATED_CLEAN counts comparisons and not skips",
			check: func(t *testing.T) {
				for _, c := range Commands() {
					if !gated[c.Name] && !c.Implemented {
						t.Errorf("%q is ungated and unimplemented, so it contributes a SKIP "+
							"to a gate that is supposed to count warnings", c.Name)
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, tt.check)
	}
}

// TestObjectGatingStatus pins, per command, the production gating decision runs
// 8 and 9 recorded for ntable/netconf/stats/vrf — the first live grounding of
// those five objects. The earned/gated_commands set-equality pair in
// pkg/nlparity guards the SET both directions; this is the readable per-row
// statement of the flip and of its negative space. The point it makes that a
// bare count cannot: gating is a per-ROW decision, not an object-wide one.
// netconf gated its base and `-j` forms while its two `dev` forms stayed
// ungated, and stats gated nothing, because runs 8 and 9 found those rows
// divergent on the wire — a name-resolution difference, not a message-type one
// (both sides issue RTM_GETSTATS; `ip` adds lazy/redundant RTM_GETLINK gets) —
// even though their stdout matches. TestPermanentWireDivergences records that
// those nine are permanent and unallowlistable (eight since `stats show
// group link dev` resolved its name as ip does and gated). Runs 10 and 11 then gated the
// five nexthop and addrlabel rows (their first live grounding);
// TestNexthopAddrlabelGated pins that flip.
//
// go test ./internal/goipparity/ -run TestObjectGatingStatus
func TestObjectGatingStatus(t *testing.T) {
	al, err := nlparity.EmbeddedAllowlist()
	if err != nil {
		t.Fatalf("EmbeddedAllowlist: %v", err)
	}

	rows := []struct {
		description string
		command     string
		expected    bool // want IsGated
	}{
		{"positive: vrf show gates, the pristine nl=0 pair", "vrf show", true},
		{"positive: ntable show base row gates", "ntable show", true},
		{"positive: netconf show base row gates", "netconf show", true},
		{"positive: the -j twin gates with its text twin (vrf)", "-j vrf show", true},
		{"positive: the -j twin gates with its text twin (ntable)", "-j ntable show", true},
		{"positive: the -j twin gates with its text twin (netconf)", "-j netconf show", true},
		{"positive: the one -s twin that gated, ntable's", "-s ntable show", true},

		{"boundary: vrf show gates although its Floor is 2, so gating is independent of floor", "vrf show", true},
		{"boundary: netconf show dev is NeedsDev in a partly-gated object, yet stays ungated because it diverges", "netconf show dev", false},

		{"negative: stats show group link diverges on name resolution (both sides issue RTM_GETSTATS), so it stays ungated", "stats show group link", false},
		{"negative: -s stats show group link diverges likewise", "-s stats show group link", false},
		{"positive: stats show group link dev gates since it looks the name up as ip does", "stats show group link dev", true},
		{"negative: -j stats show group link diverges likewise", "-j stats show group link", false},
		{"negative: stats show group xstats diverges likewise", "stats show group xstats", false},
		{"negative: -s stats show group xstats diverges likewise", "-s stats show group xstats", false},
		{"negative: -j stats show group xstats diverges likewise", "-j stats show group xstats", false},
		{"negative: -4 netconf show dev is a FAIL on the wire, not merely a WARN", "-4 netconf show dev", false},
		{"negative: -s addr show is a held-out -s sweep row", "-s addr show", false},
		{"negative: -s neigh show is the other held-out -s row", "-s neigh show", false},
		{"positive: nexthop show gates on runs 10 and 11, its first live grounding", "nexthop show", true},
		{"positive: addrlabel show gates on runs 10 and 11, its first live grounding", "addrlabel show", true},
		{"positive: -6 addrlabel show gates with its byte-identical twin", "-6 addrlabel show", true},
		{"positive: the -j twin gates with its text twin (nexthop)", "-j nexthop show", true},
		{"positive: the -j twin gates with its text twin (addrlabel)", "-j addrlabel show", true},

		{"corner: a command not in the matrix is not gated", "bogus show", false},
	}

	for _, r := range rows {
		t.Run(r.description, func(t *testing.T) {
			if got := al.IsGated(r.command); got != r.expected {
				t.Errorf("IsGated(%q) = %v, want %v", r.command, got, r.expected)
			}
		})
	}

	// corner: the gating invariant the rows above sample — every gated command is
	// in the table AND Implemented, so a gate can never fire on a skip sentinel.
	t.Run("corner: every gated command is in the table and Implemented", func(t *testing.T) {
		for _, name := range al.GatedCommands {
			c, lookupErr := Lookup(name)
			if lookupErr != nil {
				t.Errorf("gated %q is not in the table: %v", name, lookupErr)
				continue
			}
			if !c.Implemented {
				t.Errorf("gated %q is not Implemented, so gating it would fire on a skip", name)
			}
		}
	})
}

// TestPermanentWireDivergences pins the eight rows measured as a PERMANENT
// wire divergence — six stats forms and the two netconf-dev forms. Runs 8
// and 9 measured nine; `stats show group link dev` left the set once goip's
// dev path resolved the name with a get by name, as ip does, rather than a
// link dump, and gated (see TestBondXstatsRowsGated). They are not a queue awaiting a cleaner run: both sides issue
// RTM_GETSTATS, and the gap is name resolution (goip's one up-front bulk
// RTM_GETLINK dump versus `ip`'s lazy per-index gets for stats and redundant
// per-name get for netconf-dev). That is class DivergenceTransactionCount,
// which the allowlist cannot suppress, so no run and no entry can gate them.
//
// TestUngatedSurfaceIsNotVacuous pins the whole twenty-three-row ungated SET;
// this marks the eight that are ungated by DESIGN (versus the fourteen awaiting
// their own clean run, and the one finite-lifetime row), so an edit that tries to gate or allowlist one trips
// here with the reason. The last subtest ties the decision to the mechanism —
// a transaction-count divergence is unsuppressible while a value one is not —
// complementing pkg/nlparity's allowlist_test.go, which asserts the same from
// the Suppresses side.
//
// go test ./internal/goipparity/ -run TestPermanentWireDivergences
func TestPermanentWireDivergences(t *testing.T) {
	al, err := nlparity.EmbeddedAllowlist()
	if err != nil {
		t.Fatalf("EmbeddedAllowlist: %v", err)
	}

	// The eight, by command Name — what compare.go passes to IsGated/EntriesFor.
	permanent := []string{
		"stats show group link", "-s stats show group link",
		"-j stats show group link",
		"stats show group xstats", "-s stats show group xstats",
		"-j stats show group xstats",
		"netconf show dev", "-4 netconf show dev",
	}
	inSet := make(map[string]bool, len(permanent))
	for _, n := range permanent {
		inSet[n] = true
	}

	rows := []struct {
		description string
		command     string
		wantInSet   bool
		wantGated   bool
	}{
		{"positive: a stats row is a permanent divergence (stdout correct, only the wire differs)", "stats show group link", true, false},
		{"positive: the xstats base row is a permanent divergence too", "stats show group xstats", true, false},
		{"positive: netconf show dev is a permanent divergence", "netconf show dev", true, false},
		{"boundary: -4 netconf show dev is in the set and is the one FAIL (hygiene orphans), still permanent", "-4 netconf show dev", true, false},
		{"boundary: a row that DID gate on runs 8/9 is not in the set", "netconf show", false, true},
		{"boundary: vrf show gated and is not a permanent divergence", "vrf show", false, true},
		{"negative: stats show group link dev left the set once it looked the name up by name", "stats show group link dev", false, true},
		{"negative: -s addr show is ungated for an unrelated reason (may gate later), not in the set", "-s addr show", false, false},
		{"negative: -s neigh show is ungated for an unrelated reason, not in the permanent set", "-s neigh show", false, false},
		{"corner: a name not in the matrix is neither in the set nor gated", "bogus show", false, false},
	}

	for _, r := range rows {
		t.Run(r.description, func(t *testing.T) {
			if got := inSet[r.command]; got != r.wantInSet {
				t.Errorf("inSet[%q] = %v, want %v", r.command, got, r.wantInSet)
			}
			if got := al.IsGated(r.command); got != r.wantGated {
				t.Errorf("IsGated(%q) = %v, want %v", r.command, got, r.wantGated)
			}
		})
	}

	// negative: each permanent row is ungated AND unallowlisted — it cannot gate
	// and cannot carry an allowlist entry, by design.
	t.Run("negative: each permanent row is ungated and unallowlisted", func(t *testing.T) {
		for _, name := range permanent {
			if al.IsGated(name) {
				t.Errorf("%q is a permanent wire divergence but is gated; it cannot gate "+
					"(DivergenceTransactionCount is unsuppressible) — remove it from gated_commands", name)
			}
			if entries := al.EntriesFor(name); len(entries) != 0 {
				t.Errorf("%q is a permanent wire divergence but carries %d allowlist entr(ies); a "+
					"transaction-count divergence cannot be allowlisted", name, len(entries))
			}
		}
	})

	// positive: each permanent row is in the matrix and Implemented — goip
	// produces correct stdout; only the netlink conversation differs.
	t.Run("positive: each permanent row is in the matrix and Implemented", func(t *testing.T) {
		for _, name := range permanent {
			c, lookupErr := Lookup(name)
			if lookupErr != nil {
				t.Errorf("permanent row %q is not in the table: %v", name, lookupErr)
				continue
			}
			if !c.Implemented {
				t.Errorf("permanent row %q is not Implemented; the divergence is on the wire, "+
					"not in whether goip runs the command", name)
			}
		}
	})

	// corner: the mechanism that makes the eight permanent — a transaction-count
	// divergence is unsuppressible, while a value divergence may be allowlisted.
	t.Run("corner: transaction-count is unsuppressible, value is suppressible", func(t *testing.T) {
		cases := []struct {
			description string
			class       nlparity.DivergenceClass
			expected    bool // want Suppressible
		}{
			{"a transaction-count divergence can never be allowlisted", nlparity.DivergenceTransactionCount, false},
			{"a value divergence is the one class that may be", nlparity.DivergenceValue, true},
			{"a presence divergence cannot be", nlparity.DivergencePresence, false},
			{"a hygiene finding cannot be", nlparity.DivergenceHygiene, false},
		}
		for _, tc := range cases {
			t.Run(tc.description, func(t *testing.T) {
				if got := tc.class.Suppressible(); got != tc.expected {
					t.Errorf("%v.Suppressible() = %v, want %v", tc.class, got, tc.expected)
				}
			})
		}
	})
}

// TestNexthopAddrlabelGated pins the five-row flip runs 10 and 11 recorded — the
// first live grounding of nexthop and addrlabel, two objects that had been
// replay-grounded only (internal/goip's *MatchesCapturedSidecars tests). The live
// runs added what a committed sidecar cannot, an `ip` in the same capture window,
// and both measured all five GOIP_PARITY_PASS with stdout=0, no unsuppressed
// findings, control nl=0, and txns matching ip (nexthop 3/3, addrlabel 1/1). They
// gate with ZERO allowlist entries — the pristine vrf bar, nothing suppressed, so
// nothing needing the only suppressible class (DivergenceValue). The earned/
// gated_commands set-equality pair in pkg/nlparity guards the set; this is the
// readable per-row statement from the command side, as TestObjectGatingStatus is
// for runs 8 and 9.
//
// go test ./internal/goipparity/ -run TestNexthopAddrlabelGated
func TestNexthopAddrlabelGated(t *testing.T) {
	al, err := nlparity.EmbeddedAllowlist()
	if err != nil {
		t.Fatalf("EmbeddedAllowlist: %v", err)
	}
	cohort := []string{
		"nexthop show", "-j nexthop show",
		"addrlabel show", "-6 addrlabel show", "-j addrlabel show",
	}
	inCohort := make(map[string]bool, len(cohort))
	for _, n := range cohort {
		inCohort[n] = true
	}

	rows := []struct {
		description  string
		command      string
		wantInCohort bool
		wantGated    bool
	}{
		{"positive: nexthop's base row gates on its first live run", "nexthop show", true, true},
		{"positive: addrlabel's base row gates on its first live run", "addrlabel show", true, true},
		{"positive: the -j nexthop twin gates with its text twin (no wire)", "-j nexthop show", true, true},
		{"positive: the -j addrlabel twin gates with its text twin (no wire)", "-j addrlabel show", true, true},
		{"boundary: -6 addrlabel show is a byte-identical identity, gates with addrlabel show", "-6 addrlabel show", true, true},
		{"boundary: a permanent-divergence row still does not gate (separates cohorts)", "stats show group link", false, false},
		{"negative: a still-queued -j row is not in this cohort and not gated", "-j addr show", false, false},
		{"negative: a held-out -s row is not in this cohort and not gated", "-s addr show", false, false},
		{"corner: a name not in the matrix is neither in the cohort nor gated", "bogus show", false, false},
	}

	for _, r := range rows {
		t.Run(r.description, func(t *testing.T) {
			if got := inCohort[r.command]; got != r.wantInCohort {
				t.Errorf("inCohort[%q] = %v, want %v", r.command, got, r.wantInCohort)
			}
			if got := al.IsGated(r.command); got != r.wantGated {
				t.Errorf("IsGated(%q) = %v, want %v", r.command, got, r.wantGated)
			}
		})
	}

	// positive: each cohort row is gated, in the matrix, and Implemented — a gate
	// never fires on a skip sentinel.
	t.Run("positive: each cohort row is gated, in the table, and Implemented", func(t *testing.T) {
		for _, name := range cohort {
			if !al.IsGated(name) {
				t.Errorf("%q is a runs-10/11 gated row but IsGated is false; add it to "+
					"gated_commands and the earned list together", name)
			}
			c, lookupErr := Lookup(name)
			if lookupErr != nil {
				t.Errorf("cohort row %q is not in the table: %v", name, lookupErr)
				continue
			}
			if !c.Implemented {
				t.Errorf("cohort row %q is not Implemented; a gate cannot fire on a skip", name)
			}
		}
	})

	// negative: each cohort row gated with ZERO allowlist entries — the runs were
	// pristine (nl=0), so nothing needed suppressing, and only DivergenceValue is
	// suppressible in any case.
	t.Run("negative: each cohort row gated clean, carrying no allowlist entry", func(t *testing.T) {
		for _, name := range cohort {
			if entries := al.EntriesFor(name); len(entries) != 0 {
				t.Errorf("%q gated clean on runs 10/11 but carries %d allowlist entr(ies); "+
					"it needed none", name, len(entries))
			}
		}
	})
}

// TestLinkWriteRowsGated pins the flip the phase 7 runs recorded: the 21 link
// write rows, the first rows of the matrix that change kernel state. Both runs
// reported all 21 GOIP_PARITY_PASS with identical verdicts, txns matching ip
// with goip's GOIP_WRITE_OUT file counted as its write, and control nl=0 on
// the rows with no lookups, nl=2 to 4 on the rest. Like runs 10 and 11 they
// gate with ZERO allowlist entries: the lookup rows' control noise is reply
// state D_control subtracts, not anything suppressed.
//
// go test ./internal/goipparity/ -run TestLinkWriteRowsGated
func TestLinkWriteRowsGated(t *testing.T) {
	al, err := nlparity.EmbeddedAllowlist()
	if err != nil {
		t.Fatalf("EmbeddedAllowlist: %v", err)
	}
	var cohort []Command
	for _, c := range Commands() {
		if c.Write && strings.HasPrefix(c.Slug, linkWriteSlugPrefixCst) {
			cohort = append(cohort, c)
		}
	}

	rows := []struct {
		description string
		command     string
		expected    struct{ write, gated bool }
	}{
		{"positive: an add row, the write alone, is gated", "link add d0 type dummy",
			struct{ write, gated bool }{true, true}},
		{"positive: an enslave row, two lookups and the write, is gated", "link set la1 down master bond0",
			struct{ write, gated bool }{true, true}},
		{"positive: a row the kernel refuses gates on its request", "link set la1 master bond0",
			struct{ write, gated bool }{true, true}},
		{"positive: delete is gated", "link del bond0", struct{ write, gated bool }{true, true}},
		{"boundary: the longest row, every 802.3ad option, is gated",
			"link add bond0 type bond mode 802.3ad lacp_rate fast lacp_active on ad_select bandwidth " +
				"ad_actor_sys_prio 100 ad_user_port_key 7 ad_actor_system 02:00:00:00:00:0a min_links 1",
			struct{ write, gated bool }{true, true}},
		{"negative: a read row of the same object is not a write row", "link show",
			struct{ write, gated bool }{false, true}},
		{"negative: a permanent-divergence row is neither", "stats show group link",
			struct{ write, gated bool }{false, false}},
		{"corner: a write the table does not hold is neither", "link add d9 type dummy",
			struct{ write, gated bool }{false, false}},
	}
	for _, r := range rows {
		t.Run(r.description, func(t *testing.T) {
			var got struct{ write, gated bool }
			if c, lookupErr := Lookup(r.command); lookupErr == nil {
				got.write = c.Write
			}
			got.gated = al.IsGated(r.command)
			if got != r.expected {
				t.Errorf("%q: got %+v, want %+v", r.command, got, r.expected)
			}
		})
	}

	t.Run("boundary: the cohort is the 21 rows of the committed corpus", func(t *testing.T) {
		if len(cohort) != 21 {
			t.Fatalf("%d write rows, want 21", len(cohort))
		}
	})

	t.Run("positive: each write row is gated and Implemented", func(t *testing.T) {
		for _, c := range cohort {
			if !al.IsGated(c.Name) {
				t.Errorf("%q is a phase 7 write row but IsGated is false; add it to "+
					"gated_commands and the earned list together", c.Name)
			}
			if !c.Implemented {
				t.Errorf("write row %q is not Implemented; a gate cannot fire on a skip", c.Name)
			}
		}
	})

	t.Run("negative: each write row gated clean, carrying no allowlist entry", func(t *testing.T) {
		for _, c := range cohort {
			if entries := al.EntriesFor(c.Name); len(entries) != 0 {
				t.Errorf("%q gated clean on the phase 7 runs but carries %d allowlist entr(ies)",
					c.Name, len(entries))
			}
		}
	})
}

// TestRouteWriteRowsGated pins the route write rows (route-write-path.md
// phase 5): the 26 rows of the route corpus that write, each gated on two
// live runs with no allowlist entry. `nodev` is the corpus's 27th row and is
// not one: goip stops at the lookup with no write file, which the matrix's
// write comparison has nothing to compare, so internal/goip's
// TestRouteWriteNodevCorpus holds it offline.
//
// go test ./internal/goipparity/ -run TestRouteWriteRowsGated
func TestRouteWriteRowsGated(t *testing.T) {
	al, err := nlparity.EmbeddedAllowlist()
	if err != nil {
		t.Fatalf("EmbeddedAllowlist: %v", err)
	}
	var cohort []Command
	for _, c := range Commands() {
		if strings.HasPrefix(c.Slug, routeWriteSlugPrefixCst) {
			cohort = append(cohort, c)
		}
	}
	type result struct{ write, setup, gated, cohort bool }

	tests := []struct {
		description string
		command     string
		expected    result
	}{
		{description: "positive: the NFS replace is gated",
			command:  "route replace 192.0.2.50/32 dev d0 congctl lock cubic rto_min 3ms initcwnd 32 initrwnd 32 quickack 1",
			expected: result{write: true, setup: true, gated: true, cohort: true}},
		{description: "positive: a dev-less write with no Setup is gated",
			command:  "route add blackhole 198.51.100.0/24",
			expected: result{write: true, gated: true, cohort: true}},
		{description: "positive: a v6 write is gated",
			command:  "-6 route replace 2001:db8:1::/64 via 2001:db8::2 dev d0",
			expected: result{write: true, setup: true, gated: true, cohort: true}},
		{description: "boundary: a write the kernel refuses gates on its request",
			command:  "route del 203.0.113.99/32",
			expected: result{write: true, setup: true, gated: true, cohort: true}},
		{description: "negative: route show is a read row, not in this cohort",
			command:  "route show",
			expected: result{gated: true}},
		{description: "negative: a link write is the other cohort",
			command:  "link add d0 type dummy",
			expected: result{write: true, gated: true}},
		{description: "corner: nodev is in the corpus but is not a row",
			command:  "route add 198.51.100.0/24 dev nosuch",
			expected: result{}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var got result
			if c, lookupErr := Lookup(tc.command); lookupErr == nil {
				got.write, got.setup = c.Write, len(c.Setup) > 0
				got.cohort = strings.HasPrefix(c.Slug, routeWriteSlugPrefixCst)
			}
			got.gated = al.IsGated(tc.command)
			if got != tc.expected {
				t.Errorf("%q: got %+v, want %+v", tc.command, got, tc.expected)
			}
		})
	}

	t.Run("corner: the cohort is the route corpus less nodev, each a gated, clean, Implemented write", func(t *testing.T) {
		if len(cohort) != 26 {
			t.Fatalf("%d route write rows, want 26", len(cohort))
		}
		for _, c := range cohort {
			if !c.Write || !c.Implemented {
				t.Errorf("%q: Write %v, Implemented %v", c.Name, c.Write, c.Implemented)
			}
			if !al.IsGated(c.Name) {
				t.Errorf("%q is a route write row but IsGated is false; add it to "+
					"gated_commands and the earned list together", c.Name)
			}
			if entries := al.EntriesFor(c.Name); len(entries) != 0 {
				t.Errorf("%q carries %d allowlist entr(ies)", c.Name, len(entries))
			}
		}
	})
}

// TestBondReadRowsGated pins the flip the two runs after phase 7 recorded:
// the bond `-d link show` rows, the first read rows with a Setup. Both runs
// reported both GOIP_PARITY_PASS with stdout=0, txns matching ip (2/2 on the
// master, 4/4 on the port) and control nl=0, since the driver captures all
// three sides in one namespace built from the row's Setup. Like the write rows
// they gate with ZERO allowlist entries.
//
// go test ./internal/goipparity/ -run TestBondReadRowsGated
func TestBondReadRowsGated(t *testing.T) {
	al, err := nlparity.EmbeddedAllowlist()
	if err != nil {
		t.Fatalf("EmbeddedAllowlist: %v", err)
	}
	// The cohort is the `-d link show` read rows with a Setup; the bond
	// xstats rows share bondReadSetup but are pinned on their own.
	var cohort []Command
	for _, c := range Commands() {
		if !c.Write && len(c.Setup) > 0 && strings.HasPrefix(c.Slug, "link_show_details_bond") {
			cohort = append(cohort, c)
		}
	}

	type want struct{ setup, write, gated bool }
	rows := []struct {
		description string
		command     string
		expected    want
	}{
		{"positive: the bond master's detail row has Setup and is gated", "-d link show dev bond0",
			want{true, false, true}},
		{"positive: the bond port's detail row has Setup and is gated", "-d link show dev la1",
			want{true, false, true}},
		{"negative: the dump-wide -d link show needs no Setup", "-d link show",
			want{false, false, true}},
		{"negative: a write row's Setup does not make it a read row", "link set la1 nomaster",
			want{true, true, true}},
		{"boundary: the other port, not in the table, is neither", "-d link show dev la2",
			want{false, false, false}},
		{"corner: a permanent-divergence row is neither", "stats show group link",
			want{false, false, false}},
		{"negative: a stats row with Setup is not in this cohort", "stats show dev la1 group xstats_slave",
			want{true, false, true}},
	}
	for _, r := range rows {
		t.Run(r.description, func(t *testing.T) {
			var got want
			if c, lookupErr := Lookup(r.command); lookupErr == nil {
				got.setup = len(c.Setup) > 0
				got.write = c.Write
			}
			got.gated = al.IsGated(r.command)
			if got != r.expected {
				t.Errorf("%q: got %+v, want %+v", r.command, got, r.expected)
			}
		})
	}

	t.Run("boundary: the cohort is the two bond rows", func(t *testing.T) {
		if len(cohort) != 2 {
			t.Fatalf("%d read rows with Setup, want 2", len(cohort))
		}
	})

	t.Run("positive: each read row with Setup is gated and Implemented", func(t *testing.T) {
		for _, c := range cohort {
			if !al.IsGated(c.Name) {
				t.Errorf("%q is a read row with Setup but IsGated is false; add it to "+
					"gated_commands and the earned list together", c.Name)
			}
			if !c.Implemented {
				t.Errorf("read row %q is not Implemented; a gate cannot fire on a skip", c.Name)
			}
		}
	})

	t.Run("negative: each read row with Setup gated clean, carrying no allowlist entry", func(t *testing.T) {
		for _, c := range cohort {
			if entries := al.EntriesFor(c.Name); len(entries) != 0 {
				t.Errorf("%q gated clean but carries %d allowlist entr(ies)", c.Name, len(entries))
			}
		}
	})
}

// TestBondXstatsRowsGated pins the flip the two runs after the bond `-d link
// show` rows recorded: the five bond xstats point gets, on the same
// bondReadSetup, and `stats show group link dev`, which left the permanent
// set. Both runs reported all six GOIP_PARITY_PASS with stdout=0, control
// nl=0 and txns 2/2: ip's RTM_GETLINK by name, then one non-dump
// RTM_GETSTATS, which goip now sends byte for byte. They gate with ZERO
// allowlist entries.
//
// go test ./internal/goipparity/ -run TestBondXstatsRowsGated
func TestBondXstatsRowsGated(t *testing.T) {
	al, err := nlparity.EmbeddedAllowlist()
	if err != nil {
		t.Fatalf("EmbeddedAllowlist: %v", err)
	}
	var cohort []Command
	for _, c := range Commands() {
		if !c.Write && len(c.Setup) > 0 && strings.HasPrefix(c.Slug, "stats_show_xstats") {
			cohort = append(cohort, c)
		}
	}

	type want struct{ setup, write, gated bool }
	rows := []struct {
		description string
		command     string
		expected    want
	}{
		{"positive: the bond0 xstats point get is gated", "stats show dev bond0 group xstats",
			want{true, false, true}},
		{"positive: the la1 xstats_slave point get is gated", "stats show dev la1 group xstats_slave",
			want{true, false, true}},
		{"positive: the -j twin is gated", "-j stats show dev la1 group xstats_slave",
			want{true, false, true}},
		{"positive: the link group's point get is gated, with no Setup", "stats show group link dev",
			want{false, false, true}},
		{"boundary: the bond0 suite form is gated", "stats show dev bond0 group xstats subgroup bond suite 802.3ad",
			want{true, false, true}},
		{"boundary: the la1 suite form is gated", "stats show dev la1 group xstats_slave subgroup bond suite 802.3ad",
			want{true, false, true}},
		{"negative: the bulk xstats dump stays permanent", "stats show group xstats",
			want{false, false, false}},
		{"negative: the -d link show bond row is the other cohort", "-d link show dev bond0",
			want{true, false, true}},
		{"corner: la2 is not in the table", "stats show dev la2 group xstats_slave",
			want{false, false, false}},
	}
	for _, r := range rows {
		t.Run(r.description, func(t *testing.T) {
			var got want
			if c, lookupErr := Lookup(r.command); lookupErr == nil {
				got.setup = len(c.Setup) > 0
				got.write = c.Write
			}
			got.gated = al.IsGated(r.command)
			if got != r.expected {
				t.Errorf("%q: got %+v, want %+v", r.command, got, r.expected)
			}
		})
	}

	t.Run("boundary: the cohort is the five bond xstats rows", func(t *testing.T) {
		if len(cohort) != 5 {
			t.Fatalf("%d stats read rows with Setup, want 5", len(cohort))
		}
	})

	t.Run("positive: each row in the cohort is gated and Implemented", func(t *testing.T) {
		for _, c := range cohort {
			if !al.IsGated(c.Name) {
				t.Errorf("%q is a bond xstats row but IsGated is false; add it to "+
					"gated_commands and the earned list together", c.Name)
			}
			if !c.Implemented {
				t.Errorf("row %q is not Implemented; a gate cannot fire on a skip", c.Name)
			}
		}
	})

	t.Run("negative: each row gated clean, carrying no allowlist entry", func(t *testing.T) {
		for _, c := range append(cohort, *lookupPtr(t, "stats show group link dev")) {
			if entries := al.EntriesFor(c.Name); len(entries) != 0 {
				t.Errorf("%q gated clean but carries %d allowlist entr(ies)", c.Name, len(entries))
			}
		}
	})
}

// TestSlugsSorted is a small guard on the helper the report uses to print a
// stable command list, because a report whose line order changes run to run
// cannot be diffed.
//
// go test ./internal/goipparity/ -run TestSlugsSorted
func TestSlugsSorted(t *testing.T) {
	tests := []struct {
		description string
		check       func(t *testing.T)
	}{
		{
			description: "positive: the result is sorted",
			check: func(t *testing.T) {
				got := slugsSorted()
				for i := 1; i < len(got); i++ {
					if got[i-1] > got[i] {
						t.Errorf("not sorted at %d: %q then %q", i, got[i-1], got[i])
					}
				}
			},
		},
		{
			description: "positive: the result holds every slug in the table",
			check: func(t *testing.T) {
				if got, want := len(slugsSorted()), len(Commands()); got != want {
					t.Errorf("got %d slugs, want %d", got, want)
				}
			},
		},
		{
			description: "boundary: the table order is not sorted order, so the helper is doing something",
			check: func(t *testing.T) {
				// If the table happened to be alphabetical this test would
				// pass vacuously, and a sort that did nothing would look
				// correct. It is not alphabetical: link_show precedes
				// addr_show, because capture order follows the plan's
				// sequencing, not the alphabet.
				sorted := slugsSorted()
				table := Commands()
				same := true
				for i := range table {
					if table[i].Slug != sorted[i] {
						same = false
						break
					}
				}
				if same {
					t.Error("the table is already in sorted order, so this guard is vacuous")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, tt.check)
	}
}

// TestAddrStateReadRowsGated pins A1-T02's four `addr show dev` rows: each
// an addr-show state on a device named for it, with a Setup, gated clean
// after two live runs and carrying no allowlist entry.
//
// go test ./internal/goipparity/ -run TestAddrStateReadRowsGated
func TestAddrStateReadRowsGated(t *testing.T) {
	al, err := nlparity.EmbeddedAllowlist()
	if err != nil {
		t.Fatalf("EmbeddedAllowlist: %v", err)
	}
	type want struct {
		setup, gated bool
		floor        int
		last         string // the Setup's last step, the state's address
	}
	rows := []struct {
		description string
		command     string
		expected    want
	}{
		{"positive: metric, v4_metric's address", "addr show dev metric0",
			want{true, true, 6, "addr add 192.0.2.1/24 metric 50 dev metric0"}},
		{"positive: a v4 peer, v4_peer's address", "addr show dev peer4",
			want{true, true, 6, "addr add 10.0.0.1 peer 10.0.0.2/32 dev peer4"}},
		{"positive: a v6 peer, v6_peer's address", "addr show dev peer6",
			want{true, true, 6, "-6 addr add 2001:db8::1 peer 2001:db8::2/128 nodad dev peer6"}},
		{"positive: autojoin, v4_autojoin's address", "addr show dev autojoin0",
			want{true, true, 6, "addr add 224.0.0.251/32 autojoin dev autojoin0"}},
		{"negative: the shared-topology addr show dev has no Setup", "addr show dev",
			want{false, true, 6, ""}},
		{"boundary: a state without a row is not in the table", "addr show dev d0",
			want{false, false, 0, ""}},
	}
	for _, r := range rows {
		t.Run(r.description, func(t *testing.T) {
			var got want
			if c, lookupErr := Lookup(r.command); lookupErr == nil {
				got.setup = len(c.Setup) > 0
				got.floor = c.Floor
				if got.setup {
					got.last = c.Setup[len(c.Setup)-1]
				}
				if !c.Implemented {
					t.Errorf("%q is not Implemented; a gate cannot fire on a skip", r.command)
				}
			}
			got.gated = al.IsGated(r.command)
			if got != r.expected {
				t.Errorf("%q: got %+v, want %+v", r.command, got, r.expected)
			}
			if got.setup && len(al.EntriesFor(r.command)) != 0 {
				t.Errorf("%q gated clean but carries an allowlist entry", r.command)
			}
		})
	}
}

// tdAddrShow holds the addr-show states, each with the topology file its
// fixtures were captured on.
const tdAddrShow = "../../pkg/xtcpnl/testdata/7_1_4/link-write/addr-show"

// topologySteps is a state's topology file as Setup steps: each `ip` line
// that ran, with the `ip ` dropped.
func topologySteps(t *testing.T, state string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(tdAddrShow, state, "topology"))
	if err != nil {
		t.Fatalf("reading %s's topology: %v", state, err)
	}
	var steps []string
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(line, "ok ") {
			continue
		}
		if _, step, ok := strings.Cut(line, "] ip "); ok {
			steps = append(steps, step)
		}
	}
	return steps
}

// TestAddrShowSelectorRowsGated pins A2-T03's selector rows: one per selector
// in addr-write-path.md's "Selector and output-mode read rows", each with a
// Setup that is its state's topology step for step, so the matrix and the
// fixtures the unit goldens replay cannot drift apart. Every row is gated
// after two clean live runs except `addr show dynamic`, whose lifetime ticks.
//
// go test ./internal/goipparity/ -run TestAddrShowSelectorRowsGated
func TestAddrShowSelectorRowsGated(t *testing.T) {
	al, err := nlparity.EmbeddedAllowlist()
	if err != nil {
		t.Fatalf("EmbeddedAllowlist: %v", err)
	}
	type want struct {
		implemented, setupMatches, gated, write bool
		floor                                   int
	}
	gated4 := want{implemented: true, setupMatches: true, gated: true, floor: 4}
	gated6 := want{implemented: true, setupMatches: true, gated: true, floor: 6}
	rows := []struct {
		description string
		command     string
		state       string
		expected    want
	}{
		{"positive: to, the link dump stays AF_UNSPEC", "addr show to 192.0.2.0/24", "v4_secondary", gated4},
		{"positive: scope link", "addr show scope link", "v6_stable_privacy", gated4},
		{"positive: scope all", "addr show scope all", "v4_host_scope", gated4},
		{"positive: up", "addr show up", "mixed_links", gated4},
		{"positive: down", "addr show down", "mixed_links", gated4},
		{"corner: permanent reads the lifetime state but prints no finite lifetime", "addr show permanent", "v4_lifetimes", gated4},
		{"corner: dynamic prints a finite lifetime, so it stays ungated", "addr show dynamic", "v4_lifetimes",
			want{implemented: true, setupMatches: true, floor: 4}},
		{"positive: -tentative", "addr show -tentative", "v6_tentative", gated4},
		{"positive: tentative", "addr show tentative", "v6_tentative", gated4},
		{"positive: secondary", "addr show secondary", "v4_secondary", gated4},
		{"positive: primary", "addr show primary", "v4_secondary", gated4},
		{"positive: label, headerless", `addr show label "d0:*"`, "v4_label", gated4},
		{"positive: group", "addr show group default", "mixed_links", gated4},
		{"positive: master, a get by name first", "addr show master bond0", "mixed_links", gated6},
		{"positive: -4 master, the same get and a bare dump", "-4 addr show master bond0", "mixed_links", gated6},
		{"positive: nomaster", "addr show nomaster", "mixed_links", gated4},
		{"positive: vrf, two gets then the dumps", "addr show vrf vrf0", "v4_vrf",
			want{implemented: true, setupMatches: true, gated: true, floor: 8}},
		{"negative: not a vrf is a read row, two gets and no dump", "addr show vrf d1", "v4_vrf", gated4},
		{"positive: type", "addr show type dummy", "mixed_links", gated4},
		{"positive: slave kind", "addr show type bond_slave", "mixed_links", gated4},
		{"positive: proto", "addr show proto 99", "v4_proto", gated4},
		{"positive: novf", "addr show novf", "mixed_links", gated4},
		{"positive: a bare device takes the dev path", "addr show d0", "v4_secondary", gated6},
	}
	lifetimeStates := map[string]bool{"v4_lifetimes": true, "v4_deprecated": true}

	for _, r := range rows {
		t.Run(r.description, func(t *testing.T) {
			c, lookupErr := Lookup(r.command)
			if lookupErr != nil {
				t.Fatalf("Lookup: %v", lookupErr)
			}
			got := want{
				implemented:  c.Implemented,
				setupMatches: slices.Equal(c.Setup, topologySteps(t, r.state)),
				gated:        al.IsGated(r.command),
				write:        c.Write,
				floor:        c.Floor,
			}
			if got != r.expected {
				t.Errorf("%q: got %+v, want %+v", r.command, got, r.expected)
			}
			if n := len(al.EntriesFor(r.command)); n != 0 {
				t.Errorf("%q carries %d allowlist entries; a selector row gates clean or not at all", r.command, n)
			}
		})
	}

	t.Run("negative: no gated row reads a lifetime state, permanent excepted", func(t *testing.T) {
		for _, r := range rows {
			if lifetimeStates[r.state] && al.IsGated(r.command) && r.command != "addr show permanent" {
				t.Errorf("%q reads %s and is gated; its lifetime counts down between the sides", r.command, r.state)
			}
		}
	})

	t.Run("corner: the label glob keeps its shell quotes in Args", func(t *testing.T) {
		c, lookupErr := Lookup(`addr show label "d0:*"`)
		if lookupErr != nil {
			t.Fatalf("Lookup: %v", lookupErr)
		}
		if len(c.Args) != 4 || c.Args[3] != `"d0:*"` {
			t.Errorf("Args = %q, want the glob quoted, because the driver runs argv through sh -c", c.Args)
		}
	})

	t.Run("boundary: the table's selector rows are exactly these", func(t *testing.T) {
		listed := map[string]bool{}
		for _, r := range rows {
			listed[r.command] = true
		}
		var inTable int
		for _, c := range Commands() {
			if !strings.HasPrefix(c.Slug, "addr_show_sel_") {
				continue
			}
			inTable++
			if !listed[c.Name] {
				t.Errorf("selector row %q is not pinned here", c.Name)
			}
		}
		if inTable != len(rows) {
			t.Errorf("%d selector rows in the table, %d pinned here", inTable, len(rows))
		}
	})

	t.Run("corner: a Setup missing a step does not match its topology", func(t *testing.T) {
		short := mixedLinksSetup[:len(mixedLinksSetup)-1]
		if slices.Equal(short, topologySteps(t, "mixed_links")) {
			t.Error("a Setup with its last step dropped still matches mixed_links, so the comparison proves nothing")
		}
	})
}
