package goipparity

import (
	"errors"
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
					"link show dev":       true,
					"addr show dev":       true,
					"route show dev":      true,
					"neigh show dev":      true,
					"netconf show dev":    true,
					"-4 netconf show dev": true,
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
			description: "positive: an implemented command with no device renders six fields",
			name:        "link show",
			wantFields:  []string{"link_show", "2", "yes", "no", "link show", "link show"},
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
			wantFields: []string{"rule_show", "2", "no", "no", "rule show", "rule show"},
		},
		{
			description: "boundary: the implemented dev-taking command renders dev=yes and its Args without a device",
			name:        "link show dev",
			wantFields: []string{
				"link_show_dev", "4", "yes", "yes", "link show dev", "link show dev",
			},
		},
		{
			description: "corner: a command whose name carries a leading dash keeps it, since that dash is an argument",
			name:        "-6 addr show",
			wantFields: []string{
				"addr_show_v6", "4", "yes", "no", "-6 addr show", "-6 addr show",
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
			for i := 0; i < 4; i++ {
				if strings.ContainsAny(f[i], " \t") {
					t.Errorf("%q field %d = %q contains whitespace", c.Name, i, f[i])
				}
			}
		}
	})

	t.Run("corner: no field may contain a tab, or the field count would be wrong", func(t *testing.T) {
		for _, c := range Commands() {
			if n := len(strings.Split(c.String(), "\t")); n != 6 {
				t.Errorf("%q renders %d fields, want 6", c.Name, n)
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

	// The twelve rows the `-j`/family branch added, plus the two -s sweep
	// commands whose noise is unresolved and which pkg/nlparity's held-out
	// negative keeps out of gated_commands on purpose, plus the two nexthop rows,
	// the three addrlabel rows (show, -6 show, -j show), the three ntable rows
	// (show, -s show, -j show) and the four netconf rows (show, show dev, -4 show
	// dev, -j show) each object's branch adds as its first matrix entries. Gating
	// any of these is a separate branch, after a measured-clean live run, and that
	// branch edits this list.
	expected := []string{
		"-s addr show", "-s neigh show",
		"-0 addr show", "route show table main", "route show table local",
		"-4 route show", "-4 neigh show", "-6 neigh show",
		"-j addr show", "-j link show", "-j route show", "-j neigh show",
		"-j rule show", "-j -s link show",
		"nexthop show", "-j nexthop show",
		"addrlabel show", "-6 addrlabel show", "-j addrlabel show",
		"ntable show", "-s ntable show", "-j ntable show",
		"netconf show", "netconf show dev", "-4 netconf show dev", "-j netconf show",
	}

	tests := []struct {
		description string
		check       func(t *testing.T)
	}{
		{
			description: "positive: the ungated set is exactly the twenty-six named rows, so UNGATED_CLEAN counts twenty-six rows and not zero",
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
