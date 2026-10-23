package goipparity

import (
	"encoding/binary"
	"errors"
	"fmt"
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
			description: "negative: a flush or restore row that does not set Writes, or any other row that does, is refused",
			check: func(t *testing.T) {
				for _, c := range Commands() {
					if p := multiWriteRowProblem(c); p != "" {
						t.Errorf("%q: %s", c.Name, p)
					}
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
			description: "positive: an implemented command with no device renders ten fields",
			name:        "link show",
			wantFields:  []string{"link_show", "2", "yes", "no", "no", "route", "-", "-", "link show", "link show"},
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
			wantFields: []string{"rule_show", "2", "no", "no", "no", "route", "-", "-", "rule show", "rule show"},
		},
		{
			description: "boundary: the implemented dev-taking command renders dev=yes and its Args without a device",
			name:        "link show dev",
			wantFields: []string{
				"link_show_dev", "4", "yes", "yes", "no", "route", "-", "-", "link show dev", "link show dev",
			},
		},
		{
			description: "corner: a command whose name carries a leading dash keeps it, since that dash is an argument",
			name:        "-6 addr show",
			wantFields: []string{
				"addr_show_v6", "4", "yes", "no", "no", "route", "-", "-", "-6 addr show", "-6 addr show",
			},
		},
		{
			description: "positive: a write row renders write=yes and its setup steps joined with ';'",
			cmd:         lookupPtr(t, "link set la1 nomaster"),
			wantFields: []string{
				"link_write_nomaster", "4", "yes", "no", "yes", "route",
				"link add bond0 type bond mode 802.3ad;link add la1 type veth peer name lb1;link set la1 master bond0",
				"-", "link set la1 nomaster", "link set la1 nomaster",
			},
		},
		{
			description: "positive: a stats read row with Setup renders dev=no, write=no and its six setup steps",
			name:        "stats show dev la1 group xstats_slave subgroup bond suite 802.3ad",
			wantFields: []string{
				"stats_show_xstats_slave_dev_bond_3ad", "4", "yes", "no", "no", "route",
				"link add bond0 type bond mode 802.3ad;link add la1 type veth peer name lb1;" +
					"link add la2 type veth peer name lb2;link set la1 master bond0;" +
					"link set la2 master bond0;link set bond0 up",
				"-", "stats show dev la1 group xstats_slave subgroup bond suite 802.3ad",
				"stats show dev la1 group xstats_slave subgroup bond suite 802.3ad",
			},
		},
		{
			description: "boundary: a write row with no precondition renders setup as -",
			cmd:         lookupPtr(t, "link add d0 type dummy"),
			wantFields: []string{
				"link_write_add_dummy", "2", "yes", "no", "yes", "route", "-",
				"-", "link add d0 type dummy", "link add d0 type dummy",
			},
		},
		{
			description: "positive: a read row may carry Setup, and renders write=no beside it",
			cmd:         lookupPtr(t, "-d link show dev bond0"),
			wantFields: []string{
				"link_show_details_bond", "4", "yes", "no", "no", "route",
				"link add bond0 type bond mode 802.3ad;link add la1 type veth peer name lb1;" +
					"link add la2 type veth peer name lb2;link set la1 master bond0;" +
					"link set la2 master bond0;link set bond0 up",
				"-", "-d link show dev bond0", "-d link show dev bond0",
			},
		},
		{
			description: "corner: a route write with a leading -6 keeps it, and its setup carries -6 steps",
			cmd:         lookupPtr(t, "-6 route del 2001:db8:1::/64 dev d0"),
			wantFields: []string{
				"route_write_v6_del", "4", "yes", "no", "yes", "route",
				"link add d0 type dummy;link set d0 up;addr add 192.0.2.1/24 dev d0;" +
					"-6 addr add 2001:db8::1/64 nodad dev d0;-6 route add 2001:db8:1::/64 dev d0",
				"-", "-6 route del 2001:db8:1::/64 dev d0", "-6 route del 2001:db8:1::/64 dev d0",
			},
		},
		{
			description: "positive: an ip stand-in genl row renders impl=ip and proto=generic",
			name:        "macsec show",
			wantFields:  []string{"genl_macsec_show", "4", "ip", "no", "no", "generic", "-", "-", "macsec show", "macsec show"},
		},
		{
			description: "positive: a write row with a stdin line renders > and the line",
			name:        "sr hmac set 1 sha256",
			wantFields: []string{
				"sr_hmac_set", "4", "yes", "no", "yes", "generic", "-", ">s3cret",
				"sr hmac set 1 sha256", "sr hmac set 1 sha256",
			},
		},
		{
			description: "boundary: a blank stdin line, the delete, renders > alone",
			name:        "sr hmac se 1 sha256",
			wantFields: []string{
				"sr_hmac_delete", "4", "yes", "no", "yes", "generic", "hmac 1 sha256 s3cret;hmac 2 sha1 0ther", ">",
				"sr hmac se 1 sha256", "sr hmac se 1 sha256",
			},
		},
		{
			description: "corner: a single setup step has no separator",
			cmd:         lookupPtr(t, "link replace bond0 type bond"),
			wantFields: []string{
				"link_write_replace_existing", "2", "yes", "no", "yes", "route",
				"link add bond0 type bond mode 802.3ad",
				"-", "link replace bond0 type bond", "link replace bond0 type bond",
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
			// leading field but setup has to be space-free for that to hold.
			for _, i := range []int{0, 1, 2, 3, 4, 5, 7} {
				if strings.ContainsAny(f[i], " \t") {
					t.Errorf("%q field %d = %q contains whitespace", c.Name, i, f[i])
				}
			}
		}
	})

	t.Run("corner: no field may contain a tab, or the field count would be wrong", func(t *testing.T) {
		for _, c := range Commands() {
			if n := len(strings.Split(c.String(), "\t")); n != 10 {
				t.Errorf("%q renders %d fields, want 10", c.Name, n)
			}
		}
	})

	t.Run("negative: a Stdin is on an implemented write row only, letters and digits, which echo and the shell pass unchanged", func(t *testing.T) {
		n := 0
		for _, c := range Commands() {
			if c.Stdin == nil {
				continue
			}
			n++
			if !c.Write || !c.Implemented || strings.TrimFunc(*c.Stdin, isAlnum) != "" {
				t.Errorf("%q: write %v, implemented %v, stdin %q", c.Name, c.Write, c.Implemented, *c.Stdin)
			}
		}
		// boundary: the six sr hmac set rows, and no other.
		if n != 6 {
			t.Errorf("%d rows have a Stdin, want 6", n)
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
			description: "positive: netconf show dev, the dump filtered to the named ifindex client-side, the dev resolved by ll_name_to_index's get by name ahead of the link dump",
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

	// One row: `addr show dynamic`, out for a reason of its own. It prints a
	// finite valid_lft, which counts down between ip_a and goip, so it can
	// never gate (addr-write-path.md, "Gating"). TestAddrShowSelectorRowsGated
	// pins it, and pkg/nlparity's held-out negative keeps it out of
	// gated_commands.
	//
	// Every other row has left this list:
	//
	//   - the two nexthop and three addrlabel rows on runs 10 and 11, their
	//     first live grounding — see TestNexthopAddrlabelGated;
	//   - the two bond `-d link show` rows on their first two live runs — see
	//     TestBondReadRowsGated;
	//   - `stats show group link dev`, once goip's dev path sent ip's lookup by
	//     name, with the five bond xstats point gets — see
	//     TestBondXstatsRowsGated;
	//   - the twenty-seven address write rows on the two runs after A7-T01 —
	//     see TestAddrWriteRowsGated (A3-T03's four output-mode rows, A8-T03's
	//     seven flush rows and A9-T03's two restore rows never joined it);
	//   - the twelve family, table and `-j` rows of the queue on G0-T01's two
	//     runs — see TestQueuedReadRowsGated;
	//   - the six stats dump rows on G0-T02's, once goip named each record with
	//     `ip`'s get by index — see TestStatsDumpRowsGated;
	//   - the two `-s` sweep rows and the two netconf-dev rows on G0-T03's —
	//     see TestNetconfDevAndSweepRowsGated;
	//   - the six `route get` rows G1-T01 added, on their first two runs —
	//     see TestRouteGetRowsGated;
	//   - the two `neigh get` rows G1-T02 added, likewise — see
	//     TestNeighGetRowsGated;
	//   - the two `nexthop get` and two `nexthop bucket` rows G1-T03 added,
	//     likewise — see TestNexthopGetRowsGated;
	//   - the two `link xstats` rows and `link afstats` G1-T04 added,
	//     likewise — see TestLinkXstatsRowsGated;
	//   - the `route save` row and two `route showdump` rows G2-T01 added,
	//     likewise — see TestRouteSaveRows;
	//   - the `route restore` row G2-T02 added, likewise — see
	//     TestRouteRestoreRow;
	//   - the four `route flush` rows G2-T03 added, likewise — see
	//     TestRouteFlushRows;
	//   - the nine `neigh` write and flush rows G3-T01 added, likewise —
	//     see TestNeighWriteRows;
	//   - the fourteen `tcp_metrics` rows G6-T01 added in place of its
	//     stand-in, likewise — see TestTCPMetricsRows;
	//   - the eleven `sr` rows G6-T02 added in place of its stand-in,
	//     likewise — see TestSrRows;
	//   - the twenty `ioam` rows G6-T03 added in place of its stand-in,
	//     likewise — see TestIoamRows;
	//   - the twenty-eight `mptcp` rows G6-T04 added in place of its
	//     stand-in, likewise — see TestMptcpRows;
	//   - the twenty-four `fou` rows G6-T05 added in place of its stand-in,
	//     likewise — see TestFouRows;
	//   - the eighteen `ila` rows G6-T06 added in place of its stand-in,
	//     likewise — see TestIlaRows;
	//   - the twenty-eight `l2tp` rows G6-T07 added in place of its
	//     stand-in, likewise — see TestL2tpRows.
	//
	// The last four were the permanent set, measured divergent on the wire;
	// TestPermanentWireDivergences records that it is empty.
	//
	// The genl row G5-T03 added and G6 has not yet replaced is
	// ungated by construction: `ip`
	// stands in for goip on them (IPStandIn) until each family's G6 task
	// implements it, and gating `ip` against `ip` would gate nothing about
	// goip. TestGenlStandInRows pins them.
	expected := []string{
		"addr show dynamic",
		"macsec show",
	}

	tests := []struct {
		description string
		check       func(t *testing.T)
	}{
		{
			description: "boundary: the list is two named rows, one held out and one stand-in",
			check: func(t *testing.T) {
				if len(expected) != 2 {
					t.Errorf("got %d named rows, want 2", len(expected))
				}
			},
		},
		{
			description: "positive: the ungated set is exactly the six named rows, so UNGATED_CLEAN counts them and not zero",
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
			description: "boundary: every ungated row is nonetheless compared, implemented or an ip stand-in, so UNGATED_CLEAN counts comparisons and not skips",
			check: func(t *testing.T) {
				for _, c := range Commands() {
					if !gated[c.Name] && !c.Implemented && !c.IPStandIn {
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
// group link dev` resolved its name as ip does and gated, two since G0-T02's
// stats dump did the same and its six rows gated, none since G0-T03's
// netconf-dev rows did). Runs 10 and 11 then gated the
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
		{"boundary: netconf show dev is NeedsDev and gates since G0-T03 resolves its name as ip does", "netconf show dev", true},

		{"positive: stats show group link gates since G0-T02 names each record with ip's get by index", "stats show group link", true},
		{"positive: -s stats show group link gates likewise", "-s stats show group link", true},
		{"positive: stats show group link dev gates since it looks the name up as ip does", "stats show group link dev", true},
		{"positive: -j stats show group link gates likewise", "-j stats show group link", true},
		{"positive: stats show group xstats gates likewise", "stats show group xstats", true},
		{"positive: -s stats show group xstats gates likewise", "-s stats show group xstats", true},
		{"positive: -j stats show group xstats gates likewise", "-j stats show group xstats", true},
		{"positive: -4 netconf show dev, once the one FAIL, gates since G0-T03", "-4 netconf show dev", true},
		{"positive: -s addr show, a held-out -s sweep row until G0-T03, gates", "-s addr show", true},
		{"positive: -s neigh show, the other held-out -s row, gates", "-s neigh show", true},
		{"negative: addr show dynamic stays ungated for its counting-down lifetime", "addr show dynamic", false},
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

// TestPermanentWireDivergences records that the permanent set is EMPTY. Runs 8 and
// 9 measured nine rows divergent on the wire — six stats dumps, `stats show
// group link dev` and the two netconf-dev forms — every one a name-resolution
// difference of class DivergenceTransactionCount, which the allowlist cannot
// suppress, so no run and no entry could gate them. Each left the set by a
// change to goip that sent `ip`'s requests:
//
//   - `stats show group link dev`, a get by name in place of a link dump (see
//     TestBondXstatsRowsGated);
//   - the six stats dumps, ll_index_to_name's get per record in place of an
//     up-front link dump, on G0-T02's runs (see TestStatsDumpRowsGated);
//   - the two netconf-dev rows, ll_name_to_index's get by name ahead of the
//     link dump, on G0-T03's runs, which also needed the segmenter to close an
//     ACKed point get on its ACK (see TestNetconfDevAndSweepRowsGated).
//
// So "permanent" meant "no run and no allowlist entry can gate it", never "no
// change to goip can". The rows below pin each former member as gated and with
// no allowlist entry, so a regression that reopens one surfaces here with the
// history. The last subtest ties it to the mechanism — a transaction-count
// divergence is unsuppressible while a value one is not — complementing
// pkg/nlparity's allowlist_test.go, which asserts the same from the
// Suppresses side.
//
// go test ./internal/goipparity/ -run TestPermanentWireDivergences
func TestPermanentWireDivergences(t *testing.T) {
	al, err := nlparity.EmbeddedAllowlist()
	if err != nil {
		t.Fatalf("EmbeddedAllowlist: %v", err)
	}

	type want struct {
		implemented, gated bool
		entries            int
	}
	gated := want{implemented: true, gated: true}
	rows := []struct {
		description string
		command     string
		expected    want
	}{
		{"positive: stats show group link dev, the first to leave, gates", "stats show group link dev", gated},
		{"positive: stats show group link left on G0-T02 and gates", "stats show group link", gated},
		{"positive: -s stats show group link left on G0-T02 and gates", "-s stats show group link", gated},
		{"positive: -j stats show group link left on G0-T02 and gates", "-j stats show group link", gated},
		{"positive: stats show group xstats left on G0-T02 and gates", "stats show group xstats", gated},
		{"positive: -s stats show group xstats left on G0-T02 and gates", "-s stats show group xstats", gated},
		{"positive: -j stats show group xstats left on G0-T02 and gates", "-j stats show group xstats", gated},
		{"positive: netconf show dev left on G0-T03 and gates", "netconf show dev", gated},
		{"boundary: -4 netconf show dev, once the one FAIL (hygiene orphans), left on G0-T03 and gates", "-4 netconf show dev", gated},
		{"negative: addr show dynamic is ungated by design, for its lifetime and not its wire", "addr show dynamic", want{implemented: true}},
		{"corner: a name not in the matrix is neither", "bogus show", want{}},
	}
	for _, r := range rows {
		t.Run(r.description, func(t *testing.T) {
			var got want
			if c, lookupErr := Lookup(r.command); lookupErr == nil {
				got.implemented = c.Implemented
			}
			got.gated = al.IsGated(r.command)
			got.entries = len(al.EntriesFor(r.command))
			if got != r.expected {
				t.Errorf("%q: got %+v, want %+v", r.command, got, r.expected)
			}
		})
	}

	// corner: the mechanism that made the nine permanent — a transaction-count
	// divergence is unsuppressible, while a value divergence may be allowlisted,
	// so what retired them was a change to goip, never an entry.
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
		{"boundary: a row ungated by design still does not gate (separates cohorts)", "addr show dynamic", false, false},
		{"negative: a -j row G0-T01 gated is gated but not in this cohort", "-j addr show", false, true},
		{"negative: an -s row G0-T03 gated is gated but not in this cohort", "-s addr show", false, true},
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
		{"negative: a row ungated by design is neither", "addr show dynamic",
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
		{"corner: a row ungated by design has its own Setup and is not gated", "addr show dynamic",
			want{true, false, false}},
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
		{"negative: the bulk xstats dump G0-T02 gated has no Setup, so is not in this cohort", "stats show group xstats",
			want{false, false, true}},
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

// topologySteps is a state's topology file under dir as Setup steps: each
// `ip` line that ran, with the `ip ` dropped.
func topologySteps(t *testing.T, dir, state string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, state, "topology"))
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
				setupMatches: slices.Equal(c.Setup, topologySteps(t, tdAddrShow, r.state)),
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
		if slices.Equal(short, topologySteps(t, tdAddrShow, "mixed_links")) {
			t.Error("a Setup with its last step dropped still matches mixed_links, so the comparison proves nothing")
		}
	})
}

// TestAddrOutputModeRowsGated pins A3-T03's output-mode rows: `-br`, `-o`
// and `-br -j` on mixed_links, the state their sidecars were captured on, and
// `-o -6` on v6_peer. Each Setup is its state's topology step for step, and
// each is gated after two clean live runs.
//
// go test ./internal/goipparity/ -run TestAddrOutputModeRowsGated
func TestAddrOutputModeRowsGated(t *testing.T) {
	al, err := nlparity.EmbeddedAllowlist()
	if err != nil {
		t.Fatalf("EmbeddedAllowlist: %v", err)
	}
	type want struct {
		implemented, setupMatches, gated, write bool
		floor                                   int
	}
	gated4 := want{implemented: true, setupMatches: true, gated: true, floor: 4}
	rows := []struct {
		description string
		command     string
		state       string
		expected    want
	}{
		{"positive: brief reads the mixed links", "-br addr show", "mixed_links", gated4},
		{"positive: oneline reads the mixed links", "-o addr show", "mixed_links", gated4},
		{"corner: brief JSON", "-br -j addr show", "mixed_links", gated4},
		{"corner: oneline under -6, a peer address with no DAD", "-o -6 addr show", "v6_peer", gated4},
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
				setupMatches: slices.Equal(c.Setup, topologySteps(t, tdAddrShow, r.state)),
				gated:        al.IsGated(r.command),
				write:        c.Write,
				floor:        c.Floor,
			}
			if got != r.expected {
				t.Errorf("%q: got %+v, want %+v", r.command, got, r.expected)
			}
			if n := len(al.EntriesFor(r.command)); n != 0 {
				t.Errorf("%q carries %d allowlist entries; an output-mode row gates clean or not at all", r.command, n)
			}
		})
	}

	t.Run("negative: no row reads a lifetime state", func(t *testing.T) {
		for _, r := range rows {
			if lifetimeStates[r.state] {
				t.Errorf("%q reads %s; its lifetime counts down between the sides", r.command, r.state)
			}
		}
	})

	t.Run("boundary: the table's output-mode rows are exactly these", func(t *testing.T) {
		listed := map[string]bool{}
		for _, r := range rows {
			listed[r.command] = true
		}
		var inTable int
		for _, c := range Commands() {
			if !strings.HasPrefix(c.Slug, "addr_show_brief") && !strings.HasPrefix(c.Slug, "addr_show_oneline") {
				continue
			}
			inTable++
			if !listed[c.Name] {
				t.Errorf("output-mode row %q is not pinned here", c.Name)
			}
		}
		if inTable != len(rows) {
			t.Errorf("%d output-mode rows in the table, %d pinned here", inTable, len(rows))
		}
	})

	t.Run("corner: v6PeerSetup missing its address step does not match its topology", func(t *testing.T) {
		short := v6PeerSetup[:len(v6PeerSetup)-1]
		if slices.Equal(short, topologySteps(t, tdAddrShow, "v6_peer")) {
			t.Error("a Setup with its address step dropped still matches v6_peer, so the comparison proves nothing")
		}
	})
}

// TestAddrWriteRows pins A7-T01's address write rows (addr-write-path.md
// table 10 row 1): one per slug of the address write corpus that sends a
// write, each with the corpus command as its Name and a Setup that is the
// slug's committed topology step for step. The slugs that send no write are
// not rows.
//
// go test ./internal/goipparity/ -run TestAddrWriteRows
func TestAddrWriteRows(t *testing.T) {
	entries, err := os.ReadDir(tdAddrWrite)
	if err != nil {
		t.Fatal(err)
	}
	writers := map[string]bool{}
	var silent []string
	for _, e := range entries {
		capture, err := nlparity.ParseRouteCaptureFile(filepath.Join(tdAddrWrite, e.Name(), "ip.pcap"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := nlparity.SplitWrites(nlparity.SegmentCapture(capture), 1); err == nil {
			writers[e.Name()] = true
		} else {
			silent = append(silent, e.Name())
		}
	}
	rows := map[string]Command{}
	for _, c := range Commands() {
		if slug, ok := strings.CutPrefix(c.Slug, addrWriteSlugPrefixCst); ok {
			rows[slug] = c
		}
	}

	t.Run("positive: every corpus slug that sends a write has a row, and every row such a slug", func(t *testing.T) {
		for slug := range writers {
			if _, ok := rows[slug]; !ok {
				t.Errorf("corpus slug %s sends a write but has no row", slug)
			}
		}
		for slug := range rows {
			if !writers[slug] {
				t.Errorf("row %s%s has no corpus slug that sends a write", addrWriteSlugPrefixCst, slug)
			}
		}
	})

	t.Run("positive: each row is an implemented write row of floor 4 with a Setup", func(t *testing.T) {
		for slug, c := range rows {
			if !c.Write || !c.Implemented || c.Floor != 4 || len(c.Setup) == 0 {
				t.Errorf("%s: Write %v, Implemented %v, Floor %d, %d Setup steps; want true, true, 4, some",
					slug, c.Write, c.Implemented, c.Floor, len(c.Setup))
			}
		}
	})

	t.Run("positive: each row's Setup is its slug's topology, step for step", func(t *testing.T) {
		for slug, c := range rows {
			if want := topologySteps(t, tdAddrWrite, slug); !slices.Equal(c.Setup, want) {
				t.Errorf("%s: Setup %q, topology %q", slug, c.Setup, want)
			}
		}
	})

	t.Run("positive: a -6 row's captured write is an AF_INET6 request", func(t *testing.T) {
		const afInet6 = 10
		for slug, c := range rows {
			if !strings.HasPrefix(c.Name, "-6 ") {
				continue
			}
			capture, err := nlparity.ParseRouteCaptureFile(filepath.Join(tdAddrWrite, slug, "ip.pcap"))
			if err != nil {
				t.Fatal(err)
			}
			w, err := nlparity.SplitWrites(nlparity.SegmentCapture(capture), 1)
			if err != nil {
				t.Fatal(err)
			}
			if len(w.Writes[0].Body) == 0 || w.Writes[0].Body[0] != afInet6 {
				t.Errorf("%s: the write's ifa_family is not AF_INET6", slug)
			}
		}
	})

	t.Run("negative: the slugs that send no write are not rows", func(t *testing.T) {
		want := []string{"autojoin_unicast", "brd_plus_v6", "no_dev_keyword", "nodev", "valid_zero"}
		if !slices.Equal(silent, want) {
			t.Errorf("silent slugs %q, want %q", silent, want)
		}
		for _, slug := range silent {
			if _, ok := rows[slug]; ok {
				t.Errorf("%s sends no write but is a row", slug)
			}
		}
	})

	t.Run("boundary: the rows and the silent slugs are the corpus, 27 and 5 of 32", func(t *testing.T) {
		if len(rows) != 27 || len(silent) != 5 || len(entries) != 32 {
			t.Errorf("%d rows, %d silent, %d slugs; want 27, 5, 32", len(rows), len(silent), len(entries))
		}
	})

	t.Run("corner: the six slugs with an address already on d0 end their Setup with it, and no other does", func(t *testing.T) {
		const pre = "addr add 192.0.2.1/24 dev d0"
		var withAddr []string
		for slug, c := range rows {
			if c.Setup[len(c.Setup)-1] == pre {
				withAddr = append(withAddr, slug)
			}
		}
		slices.Sort(withAddr)
		want := []string{"add_existing", "add_v4_secondary", "del_v4", "del_v4_wildcard", "replace_existing", "scope_conflict"}
		if !slices.Equal(withAddr, want) {
			t.Errorf("rows ending in %q: %q, want %q", pre, withAddr, want)
		}
	})

	// The Name is the argv the capture ran, less `ip `. No corpus file holds
	// it, so these are copied from addr-corpus.exp's write_rows.
	names := []struct {
		description string
		slug        string
		expected    string
	}{
		{"positive: the plain IPv4 add", "add_v4", "addr add 192.0.2.10/24 dev d0"},
		{"positive: a peer", "add_v4_peer", "addr add 10.0.0.1 peer 10.0.0.2/32 dev d0"},
		{"positive: brd +", "add_v4_brd_plus", "addr add 192.0.2.10/24 brd + dev d0"},
		{"positive: brd -", "add_v4_brd_minus", "addr add 192.0.2.10/24 brd - dev d0"},
		{"boundary: brd + on a /31", "add_v4_brd_31", "addr add 192.0.2.10/31 brd + dev d0"},
		{"positive: a label after brd +", "add_v4_label", "addr add 192.0.2.10/24 brd + label d0:1 dev d0"},
		{"positive: metric", "add_v4_metric", "addr add 192.0.2.10/24 metric 50 dev d0"},
		{"corner: pref is metric", "add_v4_pref_is_metric", "addr add 192.0.2.10/24 pref 7 dev d0"},
		{"positive: proto by name", "add_v4_proto_name", "addr add 192.0.2.10/24 proto kernel_lo dev d0"},
		{"positive: finite lifetimes", "add_v4_lifetimes", "addr add 192.0.2.10/24 valid_lft 3600 preferred_lft 1800 dev d0"},
		{"positive: a secondary", "add_v4_secondary", "addr add 192.0.2.2/24 dev d0"},
		{"boundary: 127/8", "add_v4_loopback_scope", "addr add 127.0.0.2/8 dev d0"},
		{"positive: autojoin", "add_v4_autojoin", "addr add 224.0.0.251/32 autojoin dev d0"},
		{"positive: -6 nodad", "add_v6_nodad", "-6 addr add 2001:db8::5/64 nodad dev d0"},
		{"boundary: the wide flags", "add_v6_flags_wide", "-6 addr add 2001:db8::5/64 nodad noprefixroute dev d0"},
		{"corner: a flag before the address, no -6", "add_v6_nodad_first", "addr add nodad 2001:db8::5/64 dev d0"},
		{"negative: mngtmpaddr on a /48", "add_v6_mngtmpaddr_48", "-6 addr add 2001:db8::5/48 mngtmpaddr dev d0"},
		{"positive: nodad optimistic", "add_v6_nodad_optimistic", "-6 addr add 2001:db8::5/64 nodad optimistic dev d0"},
		{"negative: an existing address", "add_existing", "addr add 192.0.2.1/24 dev d0"},
		{"positive: change", "change_missing_creates", "addr change 192.0.2.10/24 dev d0"},
		{"positive: replace", "replace_existing", "addr replace 192.0.2.1/24 metric 9 dev d0"},
		{"positive: del", "del_v4", "addr del 192.0.2.1/24 dev d0"},
		{"corner: the wildcard delete", "del_v4_wildcard", "addr del 192.0.2.1 dev d0"},
		{"negative: deleting a missing address", "del_missing", "addr del 192.0.2.99/24 dev d0"},
		{"negative: a scope conflict", "scope_conflict", "addr add 192.0.2.2/24 scope link dev d0"},
		{"negative: IPv6 multicast without autojoin", "v6_mcast_noautojoin", "-6 addr add ff02::fb/128 dev d0"},
		{"boundary: a 16-byte label", "label_16", "addr add 192.0.2.10/24 label d0:0123456789abc dev d0"},
	}
	for _, tt := range names {
		t.Run(tt.description, func(t *testing.T) {
			c, ok := rows[tt.slug]
			if !ok {
				t.Fatalf("no row %s%s", addrWriteSlugPrefixCst, tt.slug)
			}
			if c.Name != tt.expected {
				t.Errorf("Name %q, want %q", c.Name, tt.expected)
			}
		})
	}
	t.Run("boundary: the Name table covers every row", func(t *testing.T) {
		if len(names) != len(rows) {
			t.Errorf("%d names, %d rows", len(names), len(rows))
		}
	})
}

// TestAddrWriteRowsGated pins A7-T02's flip (addr-write-path.md table 10
// row 5): the 27 address write rows, each gated on the two live runs after
// A7-T01 with no allowlist entry. The lifetime write gates because its
// request carries the fixed 3600/1800; the design's lifetime rule is about
// readback, which is why `addr show dynamic` does not. The 5 refused slugs of
// the corpus are not rows (TestAddrWriteRows), so they cannot gate.
//
// go test ./internal/goipparity/ -run TestAddrWriteRowsGated
func TestAddrWriteRowsGated(t *testing.T) {
	al, err := nlparity.EmbeddedAllowlist()
	if err != nil {
		t.Fatalf("EmbeddedAllowlist: %v", err)
	}
	var cohort []Command
	for _, c := range Commands() {
		if strings.HasPrefix(c.Slug, addrWriteSlugPrefixCst) {
			cohort = append(cohort, c)
		}
	}
	type result struct{ write, setup, gated, cohort bool }

	tests := []struct {
		description string
		command     string
		expected    result
	}{
		{description: "positive: a plain v4 add is gated",
			command:  "addr add 192.0.2.10/24 dev d0",
			expected: result{write: true, setup: true, gated: true, cohort: true}},
		{description: "positive: a v6 add with flags is gated",
			command:  "-6 addr add 2001:db8::5/64 nodad noprefixroute dev d0",
			expected: result{write: true, setup: true, gated: true, cohort: true}},
		{description: "positive: a delete on the v4 setup is gated",
			command:  "addr del 192.0.2.1/24 dev d0",
			expected: result{write: true, setup: true, gated: true, cohort: true}},
		{description: "boundary: a write the kernel refuses gates on its request",
			command:  "addr add 192.0.2.1/24 dev d0",
			expected: result{write: true, setup: true, gated: true, cohort: true}},
		{description: "corner: the lifetime write gates, because its request holds the fixed 3600/1800",
			command:  "addr add 192.0.2.10/24 valid_lft 3600 preferred_lft 1800 dev d0",
			expected: result{write: true, setup: true, gated: true, cohort: true}},
		{description: "negative: the lifetime readback does not gate",
			command:  "addr show dynamic",
			expected: result{setup: true}},
		{description: "negative: a route write is the other cohort",
			command:  "route add blackhole 198.51.100.0/24",
			expected: result{write: true, gated: true}},
		{description: "corner: nodev is in the corpus but is not a row",
			command:  "addr add 192.0.2.10/24 dev nosuch",
			expected: result{}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var got result
			if c, lookupErr := Lookup(tc.command); lookupErr == nil {
				got.write, got.setup = c.Write, len(c.Setup) > 0
				got.cohort = strings.HasPrefix(c.Slug, addrWriteSlugPrefixCst)
			}
			got.gated = al.IsGated(tc.command)
			if got != tc.expected {
				t.Errorf("%q: got %+v, want %+v", tc.command, got, tc.expected)
			}
		})
	}

	t.Run("boundary: the cohort is the 27 rows of the corpus, less the 5 refused", func(t *testing.T) {
		if len(cohort) != 27 {
			t.Fatalf("%d address write rows, want 27", len(cohort))
		}
	})

	t.Run("positive: each cohort row is a gated, Implemented write", func(t *testing.T) {
		for _, c := range cohort {
			if !c.Write || !c.Implemented {
				t.Errorf("%q: Write %v, Implemented %v", c.Name, c.Write, c.Implemented)
			}
			if !al.IsGated(c.Name) {
				t.Errorf("%q is an address write row but IsGated is false; add it to "+
					"gated_commands and the earned list together", c.Name)
			}
		}
	})

	t.Run("negative: each cohort row gated clean, carrying no allowlist entry", func(t *testing.T) {
		for _, c := range cohort {
			if entries := al.EntriesFor(c.Name); len(entries) != 0 {
				t.Errorf("%q carries %d allowlist entr(ies)", c.Name, len(entries))
			}
		}
	})
}

// TestQueuedReadRowsGated pins G0-T01's flip: the twelve family, table and
// `-j` read rows the JSON facet work added ungated. Both of G0-T01's live runs
// reported each GOIP_PARITY_PASS with zero findings and the same txns each
// time, so they gate with ZERO allowlist entries. Control was the same on both
// runs but for `-4 neigh show`, nl=2 then nl=6: ifindex 3's counters, which
// D_control subtracts. The two `-s` forms they were queued with stay out, for
// G0-T03's decision.
//
// go test ./internal/goipparity/ -run TestQueuedReadRowsGated
func TestQueuedReadRowsGated(t *testing.T) {
	al, err := nlparity.EmbeddedAllowlist()
	if err != nil {
		t.Fatalf("EmbeddedAllowlist: %v", err)
	}
	type want struct {
		implemented, gated bool
		entries            int
	}
	gated := want{implemented: true, gated: true}
	rows := []struct {
		description string
		command     string
		expected    want
	}{
		{"positive: -0 reaches the AF_PACKET branch, one dump", "-0 addr show", gated},
		{"positive: the main table", "route show table main", gated},
		{"positive: the local table, RTA_TABLE 255", "route show table local", gated},
		{"positive: routes under -4", "-4 route show", gated},
		{"positive: neighbors under -4", "-4 neigh show", gated},
		{"positive: neighbors under -6", "-6 neigh show", gated},
		{"positive: addresses as JSON", "-j addr show", gated},
		{"positive: links as JSON", "-j link show", gated},
		{"positive: routes as JSON", "-j route show", gated},
		{"positive: neighbors as JSON", "-j neigh show", gated},
		{"positive: rules as JSON", "-j rule show", gated},
		{"corner: link statistics as JSON, two options at once", "-j -s link show", gated},
		{"boundary: -s addr show, queued with them, gated later on G0-T03's runs", "-s addr show", gated},
		{"boundary: -s neigh show, queued with them, gated later on G0-T03's runs", "-s neigh show", gated},
		{"corner: addr show dynamic stays out by design", "addr show dynamic", want{implemented: true}},
		{"corner: a row not in the matrix is neither", "-j -6 rule show", want{}},
	}
	for _, r := range rows {
		t.Run(r.description, func(t *testing.T) {
			var got want
			if c, lookupErr := Lookup(r.command); lookupErr == nil {
				got.implemented = c.Implemented
			}
			got.gated = al.IsGated(r.command)
			got.entries = len(al.EntriesFor(r.command))
			if got != r.expected {
				t.Errorf("%q: got %+v, want %+v", r.command, got, r.expected)
			}
		})
	}
}

// TestStatsDumpRowsGated pins G0-T02's flip: the six `stats show` dump rows,
// group link and group xstats, each plain, `-s` and `-j`. Runs 8 and 9 read
// them ip=6 goip=2, and they sat in the permanent set, because goip named the
// records with one up-front RTM_GETLINK dump where `ip` calls ll_index_to_name
// per record (ip/ipstats.c:761), one RTM_GETLINK get each. goip now does the
// same, and both of G0-T02's live runs read txns ip=6 goip=6, GOIP_PARITY_PASS
// and zero findings, so they gate with ZERO allowlist entries. The two
// netconf-dev rows they were measured with stay out.
//
// go test ./internal/goipparity/ -run TestStatsDumpRowsGated
func TestStatsDumpRowsGated(t *testing.T) {
	al, err := nlparity.EmbeddedAllowlist()
	if err != nil {
		t.Fatalf("EmbeddedAllowlist: %v", err)
	}
	type want struct {
		implemented, gated bool
		entries            int
	}
	gated := want{implemented: true, gated: true}
	rows := []struct {
		description string
		command     string
		expected    want
	}{
		{"positive: the link group dump", "stats show group link", gated},
		{"positive: the link group under -s", "-s stats show group link", gated},
		{"positive: the link group as JSON", "-j stats show group link", gated},
		{"positive: the xstats group dump", "stats show group xstats", gated},
		{"positive: the xstats group under -s", "-s stats show group xstats", gated},
		{"positive: the xstats group as JSON", "-j stats show group xstats", gated},
		{"boundary: the dev point get gated before G0-T02 and stays gated", "stats show group link dev", gated},
		{"boundary: netconf show dev, measured with them, gated later on G0-T03's runs", "netconf show dev", gated},
		{"negative: addr show dynamic stays out by design", "addr show dynamic", want{implemented: true}},
		{"corner: -j -s stats is refused by goip and is not a matrix row", "-j -s stats show group link", want{}},
	}
	for _, r := range rows {
		t.Run(r.description, func(t *testing.T) {
			var got want
			if c, lookupErr := Lookup(r.command); lookupErr == nil {
				got.implemented = c.Implemented
			}
			got.gated = al.IsGated(r.command)
			got.entries = len(al.EntriesFor(r.command))
			if got != r.expected {
				t.Errorf("%q: got %+v, want %+v", r.command, got, r.expected)
			}
		})
	}
}

// TestNetconfDevAndSweepRowsGated pins G0-T03's flip: the last four rows that
// could gate. The two `-s` sweep rows had been held out while their noise was
// unresolved; every run since passed them with zero findings and txns 2/2, and
// `-4 neigh show`, the same link and neighbor dumps, showed D_control absorbing
// a moved count, so they gate on G0-T01's bar. The two netconf-dev rows read
// ip=3 goip=2 until goip sent ll_name_to_index's get by name ahead of the link
// dump; `-4 netconf show dev` also failed hygiene, because the ACK after its
// point get was an orphan on both sides until the segmenter closed an ACKed
// point get on its ACK. G0-T03's two runs read them 3/3 with no finding and no
// orphan. All four gate with ZERO allowlist entries, leaving `addr show
// dynamic` the one ungated row.
//
// go test ./internal/goipparity/ -run TestNetconfDevAndSweepRowsGated
func TestNetconfDevAndSweepRowsGated(t *testing.T) {
	al, err := nlparity.EmbeddedAllowlist()
	if err != nil {
		t.Fatalf("EmbeddedAllowlist: %v", err)
	}
	type want struct {
		implemented, gated bool
		entries            int
	}
	gated := want{implemented: true, gated: true}
	rows := []struct {
		description string
		command     string
		expected    want
	}{
		{"positive: -s addr show, noisy with variance, gates", "-s addr show", gated},
		{"positive: -s neigh show, steady at nl=2, gates", "-s neigh show", gated},
		{"positive: netconf show dev sends the get by name and gates", "netconf show dev", gated},
		{"corner: -4 netconf show dev, the point get closed on its ACK, gates", "-4 netconf show dev", gated},
		{"boundary: the -s twin gated before G0-T03 stays gated", "-s link show", gated},
		{"boundary: the base netconf row gated on runs 8 and 9 stays gated", "netconf show", gated},
		{"negative: addr show dynamic stays out by design", "addr show dynamic", want{implemented: true}},
		{"corner: a row not in the matrix is neither", "-s -4 netconf show dev", want{}},
	}
	for _, r := range rows {
		t.Run(r.description, func(t *testing.T) {
			var got want
			if c, lookupErr := Lookup(r.command); lookupErr == nil {
				got.implemented = c.Implemented
			}
			got.gated = al.IsGated(r.command)
			got.entries = len(al.EntriesFor(r.command))
			if got != r.expected {
				t.Errorf("%q: got %+v, want %+v", r.command, got, r.expected)
			}
		})
	}
}

// TestRouteGetRowsGated pins G1-T01's six `route get` rows: implemented, with
// their corpus rows' preconditions as Setup, and gated with no allowlist entry
// after two clean runs.
//
// go test ./internal/goipparity/ -run TestRouteGetRowsGated
func TestRouteGetRowsGated(t *testing.T) {
	al, err := nlparity.EmbeddedAllowlist()
	if err != nil {
		t.Fatalf("EmbeddedAllowlist: %v", err)
	}
	type want struct {
		implemented, gated, setup bool
		floor, entries            int
	}
	gated := func(floor int) want { return want{implemented: true, gated: true, setup: true, floor: floor} }
	rows := []struct {
		description string
		command     string
		expected    want
	}{
		{"positive: the plain get, a cloned reply with uid and cache", "route get 192.0.2.5", gated(4)},
		{"positive: fibmatch, the matched FIB entry", "route get fibmatch 192.0.2.5", gated(4)},
		{"positive: notify, echoed in the flags and the cache line", "route get 192.0.2.5 notify", gated(4)},
		{"positive: -6, no lookup-table flag and no cache line", "-6 route get 2001:db8::5", gated(4)},
		{"corner: from/iif, the get by name first, its Setup's sysctl step", "route get 192.0.2.5 from 198.51.100.2 iif d1", gated(6)},
		{"boundary: the unreachable get, the request and its error alone", "route get 203.0.113.1", gated(2)},
		{"negative: a route get form not in the matrix is neither", "route get 192.0.2.6", want{}},
	}
	for _, r := range rows {
		t.Run(r.description, func(t *testing.T) {
			var got want
			if c, lookupErr := Lookup(r.command); lookupErr == nil {
				got.implemented, got.setup, got.floor = c.Implemented, len(c.Setup) > 0, c.Floor
			}
			got.gated = al.IsGated(r.command)
			got.entries = len(al.EntriesFor(r.command))
			if got != r.expected {
				t.Errorf("%q: got %+v, want %+v", r.command, got, r.expected)
			}
		})
	}

	t.Run("corner: only the from/iif row's Setup carries a sysctl step", func(t *testing.T) {
		for _, c := range Commands() {
			n := 0
			for _, step := range c.Setup {
				if strings.HasPrefix(step, "sysctl ") {
					n++
				}
			}
			expected := 0
			if c.Slug == "route_get_from_iif" {
				expected = 1
			}
			if n != expected {
				t.Errorf("%s: %d sysctl steps, want %d", c.Slug, n, expected)
			}
		}
	})
}

// TestNeighGetRowsGated pins G1-T02's two `neigh get` rows: implemented, on a
// veth Setup, floor 4 (the get by name and the get), and gated with no
// allowlist entry after two clean runs.
//
// go test ./internal/goipparity/ -run TestNeighGetRowsGated
func TestNeighGetRowsGated(t *testing.T) {
	al, err := nlparity.EmbeddedAllowlist()
	if err != nil {
		t.Fatalf("EmbeddedAllowlist: %v", err)
	}
	type want struct {
		implemented, gated, veth bool
		floor, entries           int
	}
	gated := want{implemented: true, gated: true, veth: true, floor: 4}
	rows := []struct {
		description string
		command     string
		expected    want
	}{
		{"positive: a permanent entry, printed with its device", "neigh get 192.0.2.2 dev d0", gated},
		{"negative: a missing entry, ENOENT and exit 2, gates the same", "neigh get 192.0.2.9 dev d0", gated},
		{"boundary: the dump the get sits beside stays gated, with no Setup and its one version-skew entry", "neigh show", want{implemented: true, gated: true, floor: 4, entries: 1}},
		{"corner: a neigh get form not in the matrix is neither", "neigh get proxy 192.0.2.2", want{}},
	}
	for _, r := range rows {
		t.Run(r.description, func(t *testing.T) {
			var got want
			if c, lookupErr := Lookup(r.command); lookupErr == nil {
				got.implemented, got.floor = c.Implemented, c.Floor
				got.veth = len(c.Setup) > 0 && strings.Contains(c.Setup[0], "type veth")
			}
			got.gated = al.IsGated(r.command)
			got.entries = len(al.EntriesFor(r.command))
			if got != r.expected {
				t.Errorf("%q: got %+v, want %+v", r.command, got, r.expected)
			}
		})
	}
}

// TestNexthopGetRowsGated pins G1-T03's four rows: implemented, on a dummy
// with nexthops 1 and 2 (and resilient group 20 for the bucket rows), and
// gated with no allowlist entry after two clean runs.
//
// go test ./internal/goipparity/ -run TestNexthopGetRowsGated
func TestNexthopGetRowsGated(t *testing.T) {
	al, err := nlparity.EmbeddedAllowlist()
	if err != nil {
		t.Fatalf("EmbeddedAllowlist: %v", err)
	}
	type want struct {
		implemented, gated, resilient bool
		floor, setup, entries         int
	}
	rows := []struct {
		description string
		command     string
		expected    want
	}{
		{"positive: a nexthop by id, named through d0", "nexthop get id 1",
			want{implemented: true, gated: true, floor: 4, setup: 5}},
		{"negative: a missing id, ENOENT and exit 2, gates the same", "nexthop get id 9",
			want{implemented: true, gated: true, floor: 2, setup: 5}},
		{"positive: the 1024-bucket dump, its floor below ip's five datagrams", "nexthop bucket show id 20",
			want{implemented: true, gated: true, resilient: true, floor: 3, setup: 6}},
		{"positive: one bucket's get", "nexthop bucket get id 20 index 0",
			want{implemented: true, gated: true, resilient: true, floor: 2, setup: 6}},
		{"boundary: the listing the get sits beside stays gated with no Setup", "nexthop show",
			want{implemented: true, gated: true, floor: 4}},
		{"corner: a bucket form not in the matrix is neither", "nexthop bucket show nhid 2", want{}},
	}
	for _, r := range rows {
		t.Run(r.description, func(t *testing.T) {
			var got want
			if c, lookupErr := Lookup(r.command); lookupErr == nil {
				got.implemented, got.floor, got.setup = c.Implemented, c.Floor, len(c.Setup)
				got.resilient = len(c.Setup) > 0 && strings.Contains(c.Setup[len(c.Setup)-1], "type resilient")
			}
			got.gated = al.IsGated(r.command)
			got.entries = len(al.EntriesFor(r.command))
			if got != r.expected {
				t.Errorf("%q: got %+v, want %+v", r.command, got, r.expected)
			}
		})
	}
}

// TestLinkXstatsRowsGated pins G1-T04's three rows: implemented, gated after
// two clean runs with no allowlist entry, each on the reads namespace's
// bridge.
//
// go test ./internal/goipparity/ -run TestLinkXstatsRowsGated
func TestLinkXstatsRowsGated(t *testing.T) {
	al, err := nlparity.EmbeddedAllowlist()
	if err != nil {
		t.Fatalf("EmbeddedAllowlist: %v", err)
	}
	type want struct {
		implemented, gated, bridge bool
		floor, setup, entries      int
	}
	rows := []struct {
		description string
		command     string
		expected    want
	}{
		{"positive: the master group's dump and br0's get", "link xstats type bridge",
			want{implemented: true, gated: true, bridge: true, floor: 4, setup: 5}},
		{"positive: the port group's dump and v2's get", "link xstats type bridge_slave",
			want{implemented: true, gated: true, bridge: true, floor: 4, setup: 5}},
		{"boundary: the AF_SPEC dump alone, nothing to name", "link afstats",
			want{implemented: true, gated: true, bridge: true, floor: 2, setup: 5}},
		{"corner: a selector form not in the matrix is neither", "link xstats type bridge_slave stp", want{}},
	}
	for _, r := range rows {
		t.Run(r.description, func(t *testing.T) {
			var got want
			if c, lookupErr := Lookup(r.command); lookupErr == nil {
				got.implemented, got.floor, got.setup = c.Implemented, c.Floor, len(c.Setup)
				got.bridge = len(c.Setup) > 0 && c.Setup[0] == "link add br0 type bridge"
			}
			got.gated = al.IsGated(r.command)
			got.entries = len(al.EntriesFor(r.command))
			if got != r.expected {
				t.Errorf("%q: got %+v, want %+v", r.command, got, r.expected)
			}
		})
	}
}

// TestRouteSaveRows pins G2-T01's three rows: implemented, on route_save's
// topology step for step, so the save row dumps what the capture saved and
// the showdump rows' d0 is ifindex 3, as the committed file names it; and
// gated after two clean runs with no allowlist entry.
//
// go test ./internal/goipparity/ -run TestRouteSaveRows
func TestRouteSaveRows(t *testing.T) {
	rtnl := filepath.Join("..", "..", "pkg", "xtcpnl", "testdata", "7_1_4", "link-write", "rtnl")
	topo := topologySteps(t, rtnl, "route_save")
	al, err := nlparity.EmbeddedAllowlist()
	if err != nil {
		t.Fatalf("EmbeddedAllowlist: %v", err)
	}
	type want struct {
		implemented, topology, write, gated bool
		floor, entries                      int
	}
	gated := want{implemented: true, topology: true, gated: true, floor: 2}
	rows := []struct {
		description string
		command     string
		expected    want
	}{
		{"positive: save, the dump and its reply", "route save table 100", gated},
		{"positive: showdump of route_save's file, d0 named by one get", "route showdump < route_save.save", gated},
		{"positive: -d showdump, the same get", "-d route showdump < route_save.save", gated},
		{"negative: a save of another table is not in the matrix", "route save table 200", want{}},
	}
	for _, r := range rows {
		t.Run(r.description, func(t *testing.T) {
			var got want
			if c, lookupErr := Lookup(r.command); lookupErr == nil {
				got = want{implemented: c.Implemented, topology: slices.Equal(c.Setup, topo), write: c.Write, floor: c.Floor}
			}
			got.gated = al.IsGated(r.command)
			got.entries = len(al.EntriesFor(r.command))
			if got != r.expected {
				t.Errorf("%q: got %+v, want %+v", r.command, got, r.expected)
			}
		})
	}
}

// TestRouteRestoreRow pins G2-T02's row: a write row of two writes on
// route_restore's topology step for step, reading the file the showdump rows
// read, and gated after two clean runs with no allowlist entry.
//
// go test ./internal/goipparity/ -run TestRouteRestoreRow
func TestRouteRestoreRow(t *testing.T) {
	al, err := nlparity.EmbeddedAllowlist()
	if err != nil {
		t.Fatalf("EmbeddedAllowlist: %v", err)
	}
	topo := topologySteps(t, filepath.Join("..", "..", "pkg", "xtcpnl", "testdata", "7_1_4", "link-write", "rtnl"), "route_restore")
	type want struct {
		implemented, write, topology, gated bool
		floor, writes, entries              int
	}
	rows := []struct {
		description string
		command     string
		expected    want
	}{
		{"positive: restore, the link dump then two writes", "route restore < route_save.save",
			want{implemented: true, write: true, topology: true, gated: true, floor: 4, writes: 2}},
		{"negative: a restore of another file is not in the matrix", "route restore < restore_fresh.save", want{}},
	}
	for _, r := range rows {
		t.Run(r.description, func(t *testing.T) {
			var got want
			if c, lookupErr := Lookup(r.command); lookupErr == nil {
				got = want{implemented: c.Implemented, write: c.Write, topology: slices.Equal(c.Setup, topo),
					floor: c.Floor, writes: c.Writes}
			}
			got.gated = al.IsGated(r.command)
			got.entries = len(al.EntriesFor(r.command))
			if got != r.expected {
				t.Errorf("%q: got %+v, want %+v", r.command, got, r.expected)
			}
		})
	}
}

// TestRouteFlushRows pins G2-T03's four rows: the flush and its -s form,
// write rows of two writes on route_flush's topology; the empty and the
// missing table, read rows on theirs; all gated after two clean runs with no
// allowlist entry.
//
// go test ./internal/goipparity/ -run TestRouteFlushRows
func TestRouteFlushRows(t *testing.T) {
	al, err := nlparity.EmbeddedAllowlist()
	if err != nil {
		t.Fatalf("EmbeddedAllowlist: %v", err)
	}
	rtnl := filepath.Join("..", "..", "pkg", "xtcpnl", "testdata", "7_1_4", "link-write", "rtnl")
	type want struct {
		implemented, write, topology, gated bool
		floor, writes, entries              int
	}
	rows := []struct {
		description string
		command     string
		corpus      string // the slug whose topology Setup must be, "" for none
		expected    want
	}{
		{"positive: the flush, two DELs and a second dump", "route flush table 100", "route_flush",
			want{implemented: true, write: true, topology: true, gated: true, floor: 5, writes: 2}},
		{"positive: -s, the same writes and the round text", "-s route flush table 100", "route_flush",
			want{implemented: true, write: true, topology: true, gated: true, floor: 5, writes: 2}},
		{"boundary: an emptied table, the dump alone, a read row", "route flush tab 100", "route_flush_empty",
			want{implemented: true, topology: true, gated: true, floor: 2}},
		{"negative: a missing table, the dump's error, a read row in a namespace of its own", "route flush t 100", "",
			want{implemented: true, topology: true, gated: true, floor: 2}},
		{"negative: a flush of another table is not in the matrix", "route flush table 200", "", want{}},
	}
	for _, r := range rows {
		t.Run(r.description, func(t *testing.T) {
			var got want
			if c, lookupErr := Lookup(r.command); lookupErr == nil {
				topo := routeFlushNoTableSetup
				if r.corpus != "" {
					topo = topologySteps(t, rtnl, r.corpus)
				}
				got = want{implemented: c.Implemented, write: c.Write, topology: slices.Equal(c.Setup, topo),
					floor: c.Floor, writes: c.Writes}
			}
			got.gated = al.IsGated(r.command)
			got.entries = len(al.EntriesFor(r.command))
			if got != r.expected {
				t.Errorf("%q: got %+v, want %+v", r.command, got, r.expected)
			}
		})
	}
}

// unpinnedNeighSetup is a neigh write row's Setup as the corpus capture ran
// it: the veth pair without its pinned MACs, the addrgenmode steps dropped
// and the flush's IPv6 entry back to the capture's 192.0.2.5, the three
// things the matrix changes so every side dumps the same entries in the same
// order.
func unpinnedNeighSetup(setup []string) []string {
	steps := make([]string, 0, len(setup))
	for _, s := range setup {
		switch {
		case strings.HasSuffix(s, " addrgenmode none"):
			continue
		case s == neighWriteSetupVeth[0]:
			s = "link add d0 type veth peer name d1"
		case s == neighFlushSecondEntry:
			s = "neigh add 192.0.2.5 lladdr 02:00:00:00:00:05 dev d0 nud stale"
		}
		steps = append(steps, s)
	}
	return steps
}

// TestNeighWriteRows pins G3-T01's nine rows: the six write rows and the
// two flush write rows on their capture's precondition, less the pinning,
// and the flush of nothing a read row, every one gated with no allowlist
// entry after two clean live runs.
//
// go test ./internal/goipparity/ -run TestNeighWriteRows
func TestNeighWriteRows(t *testing.T) {
	al, err := nlparity.EmbeddedAllowlist()
	if err != nil {
		t.Fatalf("EmbeddedAllowlist: %v", err)
	}
	rtnl := filepath.Join("..", "..", "pkg", "xtcpnl", "testdata", "7_1_4", "link-write", "rtnl")
	type want struct {
		implemented, write, topology, gated bool
		floor, writes, entries              int
	}
	single := want{implemented: true, write: true, topology: true, gated: true, floor: 4}
	flush := want{implemented: true, write: true, topology: true, gated: true, floor: 6, writes: 2}
	rows := []struct {
		description string
		command     string
		corpus      string // the slug whose topology Setup must be
		expected    want
	}{
		{"positive: add on the bare veth", "neigh add 192.0.2.2 lladdr 02:00:00:00:00:02 dev d0 nud permanent",
			"neigh_add", single},
		{"negative: add of an entry the kernel holds, refused, the same request",
			"neigh a 192.0.2.2 lladdr 02:00:00:00:00:02 dev d0 nud permanent", "neigh_add_existing", single},
		{"positive: change", "neigh change 192.0.2.2 lladdr 02:00:00:00:00:03 dev d0 nud reachable",
			"neigh_change", single},
		{"negative: change of a missing entry", "neigh change 192.0.2.9 lladdr 02:00:00:00:00:09 dev d0",
			"neigh_change_missing", single},
		{"positive: replace", "neigh replace 192.0.2.3 lladdr 02:00:00:00:00:03 dev d0 nud stale",
			"neigh_replace", single},
		{"positive: del", "neigh del 192.0.2.2 dev d0", "neigh_del", single},
		{"positive: flush, two DELs and a second dump", "neigh flush dev d0", "neigh_flush", flush},
		{"positive: -s, the same writes and the round text", "-s neigh flush dev d0", "neigh_flush", flush},
		{"boundary: a flush of nothing, the dumps alone, a read row", "neigh f dev d0", "neigh_flush_empty",
			want{implemented: true, topology: true, gated: true, floor: 4}},
		{"negative: a flush with no dev is not in the matrix", "neigh flush to 192.0.2.0/24", "", want{}},
	}
	for _, r := range rows {
		t.Run(r.description, func(t *testing.T) {
			var got want
			if c, lookupErr := Lookup(r.command); lookupErr == nil {
				got = want{implemented: c.Implemented, write: c.Write,
					topology: slices.Equal(unpinnedNeighSetup(c.Setup), topologySteps(t, rtnl, r.corpus)),
					floor:    c.Floor, writes: c.Writes}
			}
			got.gated = al.IsGated(r.command)
			got.entries = len(al.EntriesFor(r.command))
			if got != r.expected {
				t.Errorf("%q: got %+v, want %+v", r.command, got, r.expected)
			}
		})
	}
}

// TestRuleWriteRows pins G3-T02's twenty rows: the fourteen selector adds,
// one each from rule_add_selectors, the add, del and del of a missing rule,
// the flush and the restore, write rows, and the save a read row, each on
// its capture's precondition, every one gated with no allowlist entry after
// two clean live runs.
//
// go test ./internal/goipparity/ -run TestRuleWriteRows
func TestRuleWriteRows(t *testing.T) {
	al, err := nlparity.EmbeddedAllowlist()
	if err != nil {
		t.Fatalf("EmbeddedAllowlist: %v", err)
	}
	rtnl := filepath.Join("..", "..", "pkg", "xtcpnl", "testdata", "7_1_4", "link-write", "rtnl")
	type want struct {
		implemented, write, topology, gated bool
		floor, writes, entries              int
	}
	single := want{implemented: true, write: true, topology: true, gated: true, floor: 2}
	rows := []struct {
		description string
		command     string
		corpus      string // the slug whose topology Setup must be; "" for none
		expected    want
	}{
		{"positive: from", "rule add pref 101 from 192.0.2.0/24 table 100", "", single},
		{"positive: to", "rule add pref 102 to 198.51.100.0/24 table 100", "", single},
		{"positive: tos", "rule add pref 103 tos 0x10 table 100", "", single},
		{"positive: dscp", "rule add pref 104 dscp 10 table 100", "", single},
		{"positive: fwmark and mask", "rule add pref 105 fwmark 0x7/0xff table 100", "", single},
		{"positive: iif", "rule add pref 106 iif d9 table 100", "", single},
		{"positive: oif", "rule add pref 107 oif d9 table 100", "", single},
		{"positive: uidrange", "rule add pref 108 uidrange 1000-2000 table 100", "", single},
		{"positive: ipproto and ports", "rule add pref 109 ipproto tcp sport 1000-2000 dport 80 table 100", "", single},
		{"positive: not and goto", "rule add pref 110 not from 203.0.113.0/24 goto 112", "", single},
		{"positive: suppress_prefixlength", "rule add pref 111 table main suppress_prefixlength 0", "", single},
		{"positive: protocol", "rule add pref 112 protocol static table 100", "", single},
		{"positive: l3mdev", "rule add pref 113 l3mdev", "", single},
		{"positive: -6 flowlabel", "-6 rule add pref 114 flowlabel 0x12345 table 100", "", single},
		{"negative: add of a rule the kernel holds, refused, the same request",
			"rule add pref 100 from 192.0.2.0/24 table 100", "rule_add_existing", single},
		{"positive: del", "rule del pref 100", "rule_del", single},
		{"negative: del of a missing rule", "rule del pref 4242", "", single},
		{"positive: flush, four DELs each acked", "rule flush", "rule_flush",
			want{implemented: true, write: true, topology: true, gated: true, floor: 4, writes: 4}},
		{"positive: save, a read row", "rule save", "rule_save",
			want{implemented: true, topology: true, gated: true, floor: 2}},
		{"positive: restore, the link dump then five writes", "rule restore < rule_save.save", "",
			want{implemented: true, write: true, topology: true, gated: true, floor: 4, writes: 5}},
		{"negative: a flush with a selector is not in the matrix", "rule flush pref 100", "", want{}},
	}
	for _, r := range rows {
		t.Run(r.description, func(t *testing.T) {
			var got want
			if c, lookupErr := Lookup(r.command); lookupErr == nil {
				var topo []string
				if r.corpus != "" {
					topo = topologySteps(t, rtnl, r.corpus)
				}
				got = want{implemented: c.Implemented, write: c.Write,
					topology: slices.Equal(c.Setup, topo), floor: c.Floor, writes: c.Writes}
			}
			got.gated = al.IsGated(r.command)
			got.entries = len(al.EntriesFor(r.command))
			if got != r.expected {
				t.Errorf("%q: got %+v, want %+v", r.command, got, r.expected)
			}
		})
	}
}

// TestNexthopWriteRows pins G3-T03's eight nexthop rows: each modify row a
// gated write row whose Setup is its capture's topology step for step; the
// flush row a two-DEL write row on the two single nexthops (its capture held
// a resilient group too, see nexthopSinglesFlushDir); and the empty flush a
// read row.
//
// go test ./internal/goipparity/ -run TestNexthopWriteRows
func TestNexthopWriteRows(t *testing.T) {
	al, err := nlparity.EmbeddedAllowlist()
	if err != nil {
		t.Fatalf("EmbeddedAllowlist: %v", err)
	}
	rtnl := filepath.Join("..", "..", "pkg", "xtcpnl", "testdata", "7_1_4", "link-write", "rtnl")
	type want struct {
		implemented, write, topology, gated bool
		floor, writes                       int
	}
	single := want{implemented: true, write: true, topology: true, gated: true, floor: 2}
	routed := want{implemented: true, write: true, topology: true, gated: true, floor: 4}
	rows := []struct {
		description string
		command     string
		corpus      string // the slug whose topology Setup must be
		expected    want
	}{
		{"positive: a single nexthop, the d0 lookup and the add", "nexthop add id 1 via 192.0.2.2 dev d0",
			"nexthop_add_single", routed},
		{"positive: a weighted mpath group", "nexthop add id 10 group 1,3/2", "nexthop_add_group", single},
		{"positive: a resilient group with buckets", "nexthop add id 20 group 1/2 type resilient buckets 1024",
			"nexthop_add_resilient", single},
		{"negative: add of an id the kernel holds, refused, the same request", "nexthop add id 1 blackhole",
			"nexthop_add_existing", single},
		{"positive: replace, the d0 lookup and the write", "nexthop replace id 1 via 192.0.2.4 dev d0",
			"nexthop_replace", routed},
		{"positive: del", "nexthop del id 1", "nexthop_del", single},
		{"corner: flush on the two singles, two DELs; nexthop_flush's topology adds group 20",
			"nexthop flush", "nexthop_get",
			want{implemented: true, write: true, topology: true, gated: true, floor: 6, writes: 2}},
		{"boundary: flush of nothing, a read row", "nexthop f", "",
			want{implemented: true, topology: true, gated: true, floor: 4}},
		{"negative: a flush with a selector is not in the matrix", "nexthop flush dev d0", "", want{}},
	}
	for _, r := range rows {
		t.Run(r.description, func(t *testing.T) {
			var got want
			if c, lookupErr := Lookup(r.command); lookupErr == nil {
				topo := []string{"link set lo up"}
				if r.corpus != "" {
					topo = topologySteps(t, rtnl, r.corpus)
				}
				got = want{implemented: c.Implemented, write: c.Write,
					topology: slices.Equal(c.Setup, topo), floor: c.Floor, writes: c.Writes}
			}
			got.gated = al.IsGated(r.command)
			if got != r.expected {
				t.Errorf("%q: got %+v, want %+v", r.command, got, r.expected)
			}
		})
	}
}

// TestAddrLabelWriteRows pins G3-T04's three addrlabel rows: the add and the
// del, each the write alone, and the flush, eleven copies, each on its
// capture's precondition.
//
// go test ./internal/goipparity/ -run TestAddrLabelWriteRows
func TestAddrLabelWriteRows(t *testing.T) {
	al, err := nlparity.EmbeddedAllowlist()
	if err != nil {
		t.Fatalf("EmbeddedAllowlist: %v", err)
	}
	rtnl := filepath.Join("..", "..", "pkg", "xtcpnl", "testdata", "7_1_4", "link-write", "rtnl")
	type want struct {
		implemented, write, topology, gated bool
		floor, writes                       int
	}
	rows := []struct {
		description string
		command     string
		corpus      string // the slug whose topology Setup must be; "" for none
		expected    want
	}{
		{"positive: add, the write alone, no Setup", "addrlabel add prefix 2001:db8::/32 label 99", "",
			want{implemented: true, write: true, topology: true, gated: true, floor: 2}},
		{"positive: del of the label the Setup added", "addrlabel del prefix 2001:db8::/32 label 99",
			"addrlabel_del", want{implemented: true, write: true, topology: true, gated: true, floor: 2}},
		{"positive: flush, the dump then eleven copies each acked", "addrlabel flush", "addrlabel_flush",
			want{implemented: true, write: true, topology: true, gated: true, floor: 4, writes: 11}},
		{"negative: a flush with an argument is not in the matrix", "addrlabel flush all", "", want{}},
	}
	for _, r := range rows {
		t.Run(r.description, func(t *testing.T) {
			var got want
			if c, lookupErr := Lookup(r.command); lookupErr == nil {
				var topo []string
				if r.corpus != "" {
					topo = topologySteps(t, rtnl, r.corpus)
				}
				got = want{implemented: c.Implemented, write: c.Write,
					topology: slices.Equal(c.Setup, topo), floor: c.Floor, writes: c.Writes}
			}
			got.gated = al.IsGated(r.command)
			if got != r.expected {
				t.Errorf("%q: got %+v, want %+v", r.command, got, r.expected)
			}
		})
	}
}

// TestNTableStatsWriteRows pins G3-T05's four rows: each a gated write row
// whose Setup is its corpus capture's topology.
func TestNTableStatsWriteRows(t *testing.T) {
	al, err := nlparity.EmbeddedAllowlist()
	if err != nil {
		t.Fatalf("EmbeddedAllowlist: %v", err)
	}
	rtnl := filepath.Join("..", "..", "pkg", "xtcpnl", "testdata", "7_1_4", "link-write", "rtnl")
	type want struct {
		implemented, write, topology, gated bool
		floor                               int
	}
	rows := []struct {
		description string
		command     string
		corpus      string // the slug whose topology Setup must be; "" for none
		expected    want
	}{
		{"positive: a device's queue, the link dump then the write", "ntable change name arp_cache dev d0 queue 64",
			"ntable_change", want{implemented: true, write: true, topology: true, gated: true, floor: 4}},
		{"negative: no such table, ENOENT", "ntable change name nosuch_cache queue 64", "",
			want{implemented: true, write: true, topology: true, gated: true, floor: 4}},
		{"corner: a threshold outside init_net, ENOENT", "ntable change name arp_cache thresh1 64", "",
			want{implemented: true, write: true, topology: true, gated: true, floor: 4}},
		{"positive: stats set, the by-name get then the write", "stats set dev d0 l3_stats on",
			"stats_set_l3", want{implemented: true, write: true, topology: true, gated: true, floor: 4}},
		{"negative: the chg abbreviation is not in the matrix", "ntable chg name arp_cache dev d0 queue 64", "", want{}},
		{"boundary: l3_stats off is not in the matrix", "stats set dev d0 l3_stats off", "", want{}},
	}
	for _, r := range rows {
		t.Run(r.description, func(t *testing.T) {
			var got want
			if c, lookupErr := Lookup(r.command); lookupErr == nil {
				var topo []string
				if r.corpus != "" {
					topo = topologySteps(t, rtnl, r.corpus)
				}
				got = want{implemented: c.Implemented, write: c.Write,
					topology: slices.Equal(c.Setup, topo), floor: c.Floor}
			}
			got.gated = al.IsGated(r.command)
			if got != r.expected {
				t.Errorf("%q: got %+v, want %+v", r.command, got, r.expected)
			}
		})
	}
}

// TestLinkPropertyWriteRows pins G3-T06's three rows: each a gated write row
// whose Setup is its corpus capture's topology, link_altname_del's with the
// altname it removes.
//
// go test ./internal/goipparity/ -run TestLinkPropertyWriteRows
func TestLinkPropertyWriteRows(t *testing.T) {
	al, err := nlparity.EmbeddedAllowlist()
	if err != nil {
		t.Fatalf("EmbeddedAllowlist: %v", err)
	}
	rtnl := filepath.Join("..", "..", "pkg", "xtcpnl", "testdata", "7_1_4", "link-write", "rtnl")
	type want struct {
		implemented, write, topology, gated bool
		floor                               int
	}
	gatedWrite := want{implemented: true, write: true, topology: true, gated: true, floor: 4}
	rows := []struct {
		description string
		command     string
		corpus      string
		expected    want
	}{
		{"positive: add an altname, the by-name get then the write", "link property add dev d0 altname d0alt",
			"link_altname_add", gatedWrite},
		{"positive: del the altname its Setup added", "link property del dev d0 altname d0alt",
			"link_altname_del", gatedWrite},
		{"negative: an altname lo holds, EEXIST", "link property add dev d0 altname lo",
			"link_altname_taken", gatedWrite},
		{"corner: the p abbreviation is not in the matrix", "link p add dev d0 altname d0alt", "", want{}},
		{"boundary: two altnames in one request are not in the matrix",
			"link property add dev d0 altname x altname y", "", want{}},
	}
	for _, r := range rows {
		t.Run(r.description, func(t *testing.T) {
			var got want
			if c, lookupErr := Lookup(r.command); lookupErr == nil {
				got = want{implemented: c.Implemented, write: c.Write,
					topology: slices.Equal(c.Setup, topologySteps(t, rtnl, r.corpus)), floor: c.Floor}
			}
			got.gated = al.IsGated(r.command)
			if got != r.expected {
				t.Errorf("%q: got %+v, want %+v", r.command, got, r.expected)
			}
		})
	}
}

// TestMrouteRows pins G4-T01's six rows: implemented, gated after two clean
// runs with no allowlist entry, each on mrouteSetup, which is the reads
// namespace's veths and addresses step for step and then its smcrouted
// configuration as a `smcroute` step.
//
// go test ./internal/goipparity/ -run TestMrouteRows
func TestMrouteRows(t *testing.T) {
	al, err := nlparity.EmbeddedAllowlist()
	if err != nil {
		t.Fatalf("EmbeddedAllowlist: %v", err)
	}
	reads := filepath.Join("..", "..", "pkg", "xtcpnl", "testdata", "7_1_4", "dumps")
	b, err := os.ReadFile(filepath.Join(reads, "reads", "topology"))
	if err != nil {
		t.Fatal(err)
	}
	_, conf, _ := strings.Cut(string(b), "] smcrouted: ")
	conf, _, _ = strings.Cut(conf, "\n")
	topo := slices.Concat(topologySteps(t, reads, "reads")[:8], []string{"smcroute " + conf})
	type want struct {
		implemented, topology, write, gated bool
		floor, entries                      int
	}
	gated := want{implemented: true, topology: true, gated: true, floor: 4}
	rows := []struct {
		description string
		command     string
		expected    want
	}{
		{"positive: the (S,G) entry", "mroute show", gated},
		{"boundary: -6, the IP6MR dump, nothing to print", "-6 mroute show", gated},
		{"positive: iif, filtered client-side", "mroute show iif v0", gated},
		{"positive: table all, no RTA_TABLE", "mroute show table all", gated},
		{"corner: -s, whose Age no facet reads", "-s mroute show", gated},
		{"positive: -j", "-j mroute show", gated},
		{"corner: the bare object is not in the matrix", "mroute", want{}},
		{"corner: the from filter is not in the matrix", "mroute show from 192.0.2.10", want{}},
	}
	for _, r := range rows {
		t.Run(r.description, func(t *testing.T) {
			var got want
			if c, lookupErr := Lookup(r.command); lookupErr == nil {
				got = want{implemented: c.Implemented, write: c.Write,
					topology: slices.Equal(c.Setup, topo), floor: c.Floor}
			}
			got.gated = al.IsGated(r.command)
			got.entries = len(al.EntriesFor(r.command))
			if got != r.expected {
				t.Errorf("%q: got %+v, want %+v", r.command, got, r.expected)
			}
		})
	}
	t.Run("positive: the configuration is the corpus's", func(t *testing.T) {
		if conf != "mroute from v0 source 192.0.2.10 group 239.1.1.1 to v1" {
			t.Errorf("reads topology's smcrouted line is %q", conf)
		}
	})
}

// TestMruleRows pins G4-T02's four `mrule show` rows: no Setup, floor 2,
// gated, and the forms left out.
//
// go test ./internal/goipparity/ -run TestMruleRows
func TestMruleRows(t *testing.T) {
	al, err := nlparity.EmbeddedAllowlist()
	if err != nil {
		t.Fatalf("EmbeddedAllowlist: %v", err)
	}
	type want struct {
		implemented, setup, write, gated bool
		floor, entries                   int
	}
	gated := want{implemented: true, gated: true, floor: 2}
	rows := []struct {
		description string
		command     string
		expected    want
	}{
		{"positive: ipmr's default rule", "mrule show", gated},
		{"positive: -6, ip6mr's", "-6 mrule show", gated},
		{"positive: -d, the kernel protocol", "-d mrule show", gated},
		{"positive: -j", "-j mrule show", gated},
		{"corner: the bare object is not in the matrix", "mrule", want{}},
		{"corner: -0, refused before a request, is not in the matrix", "-0 mrule show", want{}},
		{"negative: rule show is not an mrule row", "rule show",
			want{implemented: true, gated: true, floor: 2}},
	}
	for _, r := range rows {
		t.Run(r.description, func(t *testing.T) {
			var got want
			if c, lookupErr := Lookup(r.command); lookupErr == nil {
				got = want{implemented: c.Implemented, write: c.Write, setup: len(c.Setup) > 0, floor: c.Floor}
			}
			got.gated = al.IsGated(r.command)
			got.entries = len(al.EntriesFor(r.command))
			if got != r.expected {
				t.Errorf("%q: got %+v, want %+v", r.command, got, r.expected)
			}
		})
	}
}

// TestTokenRows pins G4-T03's five `ip token` rows: the reads on
// tokenSetup, the writes on a veth or a dummy, all gated, and the forms
// left out.
//
// go test ./internal/goipparity/ -run TestTokenRows
func TestTokenRows(t *testing.T) {
	al, err := nlparity.EmbeddedAllowlist()
	if err != nil {
		t.Fatalf("EmbeddedAllowlist: %v", err)
	}
	type want struct {
		implemented, write, gated bool
		setup                     string
		floor, entries            int
	}
	read := want{implemented: true, gated: true, setup: "token", floor: 4}
	rows := []struct {
		description string
		command     string
		expected    want
	}{
		{"positive: the listing", "token show", read},
		{"positive: get with dev", "token get dev v0", read},
		{"positive: -j", "-j token show", read},
		{"positive: set on a veth", "token set ::1:2:3:4/64 dev d0",
			want{implemented: true, write: true, gated: true, setup: "veth", floor: 4}},
		{"negative: add on a NOARP dummy, refused by the kernel", "token add ::1:2:3:4/64 dev d0",
			want{implemented: true, write: true, gated: true, setup: "dummy", floor: 4}},
		{"corner: del has no capture, so no row", "token del dev d0", want{}},
		{"corner: the bare object is not in the matrix", "token", want{}},
	}
	setups := map[string][]string{"token": tokenSetup, "veth": tokenVethSetup, "dummy": d0UpSetup}
	for _, r := range rows {
		t.Run(r.description, func(t *testing.T) {
			var got want
			if c, lookupErr := Lookup(r.command); lookupErr == nil {
				got = want{implemented: c.Implemented, write: c.Write, floor: c.Floor}
				for name, s := range setups {
					if slices.Equal(c.Setup, s) {
						got.setup = name
					}
				}
			}
			got.gated = al.IsGated(r.command)
			got.entries = len(al.EntriesFor(r.command))
			if got != r.expected {
				t.Errorf("%q: got %+v, want %+v", r.command, got, r.expected)
			}
		})
	}
	t.Run("boundary: tokenVethSetup gives neither veth a link-local address before it is up", func(t *testing.T) {
		for _, dev := range []string{"d0", "d1"} {
			gen := slices.Index(tokenVethSetup, "link set "+dev+" addrgenmode none")
			up := slices.Index(tokenVethSetup, "link set "+dev+" up")
			if gen < 0 || up < 0 || gen > up {
				t.Errorf("%s: addrgenmode none at %d, up at %d, in %q; want none before up", dev, gen, up, tokenVethSetup)
			}
		}
	})
	t.Run("positive: tokenSetup sets v0's token, the reads corpus's", func(t *testing.T) {
		const step = "token set ::1:2:3:4/64 dev v0"
		topo := topologySteps(t, filepath.Join("..", "..", "pkg", "xtcpnl", "testdata", "7_1_4", "dumps"), "reads")
		if !slices.Contains(tokenSetup, step) || !slices.Contains(topo, step) {
			t.Errorf("tokenSetup %q or the reads topology %q lacks %q", tokenSetup, topo, step)
		}
	})
}

// TestNetnsRows pins G4-T04's netns rows: the list-id reads on netnsSetup,
// the set write on a fresh peer, all gated, each with its one NETNSA_FD
// value entry, and the forms left out.
//
// go test ./internal/goipparity/ -run TestNetnsRows
func TestNetnsRows(t *testing.T) {
	al, err := nlparity.EmbeddedAllowlist()
	if err != nil {
		t.Fatalf("EmbeddedAllowlist: %v", err)
	}
	type want struct {
		implemented, write, gated bool
		setup                     string
		floor, entries            int
	}
	read := want{implemented: true, gated: true, setup: "peers", floor: 4, entries: 1}
	rows := []struct {
		description string
		command     string
		expected    want
	}{
		{"positive: the dump", "netns list-id", read},
		{"positive: -j", "-j netns list-id", read},
		{"positive: a get by nsid", "netns list-id nsid 2", read},
		{"corner: a get with a target, named by the current nsid", "netns list-id nsid 1 target-nsid 2", read},
		{"positive: set on a fresh peer", "netns set c1set 7",
			want{implemented: true, write: true, gated: true, setup: "c1set", floor: 2, entries: 1}},
		{"corner: list is a directory listing, not netlink, so no row", "netns list", want{}},
		{"corner: the bare object is not in the matrix", "netns", want{}},
	}
	setups := map[string][]string{"peers": netnsSetup, "c1set": {"peer c1set"}}
	for _, r := range rows {
		t.Run(r.description, func(t *testing.T) {
			var got want
			if c, lookupErr := Lookup(r.command); lookupErr == nil {
				got = want{implemented: c.Implemented, write: c.Write, floor: c.Floor}
				for name, s := range setups {
					if slices.Equal(c.Setup, s) {
						got.setup = name
					}
				}
			}
			got.gated = al.IsGated(r.command)
			got.entries = len(al.EntriesFor(r.command))
			if got != r.expected {
				t.Errorf("%q: got %+v, want %+v", r.command, got, r.expected)
			}
		})
	}
	suppress := []struct {
		description, command, locus string
		class                       nlparity.DivergenceClass
		expected                    bool
	}{
		{"positive: set's descriptor value is accepted", "netns set c1set 7", "request:type(88):attr(3):write",
			nlparity.DivergenceValue, true},
		{"negative: set's NETNSA_NSID, the value it sets, is not", "netns set c1set 7", "request:type(88):attr(1):write",
			nlparity.DivergenceValue, false},
		{"negative: a missing descriptor is presence, never suppressed", "netns set c1set 7",
			"request:type(88):attr(3):write", nlparity.DivergencePresence, false},
		{"positive: list-id's descriptor value is accepted", "netns list-id", "request:type(90):attr(3):get",
			nlparity.DivergenceValue, true},
		{"negative: list-id's dump target is not", "netns list-id", "request:type(90):attr(4):dump",
			nlparity.DivergenceValue, false},
		{"corner: the entry is per command, so another list-id form is not covered by it", "netns list-id nsid 3",
			"request:type(90):attr(3):get", nlparity.DivergenceValue, false},
	}
	for _, r := range suppress {
		t.Run(r.description, func(t *testing.T) {
			if got := al.Suppresses(r.command, r.locus, r.class); got != r.expected {
				t.Errorf("Suppresses(%q, %q, %s) = %v, want %v", r.command, r.locus, r.class, got, r.expected)
			}
		})
	}
	t.Run("positive: netnsSetup is the reads corpus's two peers", func(t *testing.T) {
		b, err := os.ReadFile(filepath.Join("..", "..", "nix", "microvms", "scripts", "rtnl-read-corpus.exp"))
		if err != nil {
			t.Fatal(err)
		}
		for _, step := range netnsSetup {
			if !strings.Contains(string(b), "{"+step+"}") {
				t.Errorf("rtnl-read-corpus.exp has no {%s}", step)
			}
		}
	})
}

// TestGenlStandInRows pins G5-T03's genl rows, five since G6-T01 to G6-T03
// replaced tcp_metrics', sr's and ioam's: one `show` per family in the C2 order, NETLINK_GENERIC, `ip`
// in goip's place, floor 4, ungated, and with G6's rows after every route
// row, since the driver loads the families' modules before the first of
// them.
//
// go test ./internal/goipparity/ -run TestGenlStandInRows
func TestGenlStandInRows(t *testing.T) {
	al, err := nlparity.EmbeddedAllowlist()
	if err != nil {
		t.Fatalf("EmbeddedAllowlist: %v", err)
	}
	gated := map[string]bool{}
	for _, name := range al.GatedCommands {
		gated[name] = true
	}
	want := []string{
		"macsec show",
	}
	// tcpmRows, srRows, ioamRows, mptcpRows, fouRows, ilaRows and l2tpRows
	// are G6-T01's to G6-T07's rows, the first genl ones, ahead of the
	// stand-in.
	const tcpmRows, srRows, ioamRows, mptcpRows, fouRows, ilaRows, l2tpRows = 14, 11, 20, 28, 24, 18, 28

	cmds := Commands()
	first := len(cmds)
	var got []string
	for i, c := range cmds {
		if c.Protocol() != protoRouteCst && first == len(cmds) {
			first = i
		}
		if c.IPStandIn {
			got = append(got, c.Name)
		}
	}

	tests := []struct {
		description string
		check       func(t *testing.T)
	}{
		{
			description: "positive: the stand-ins are the unimplemented families' shows, in the C2 order",
			check: func(t *testing.T) {
				if !slices.Equal(got, want) {
					t.Errorf("stand-ins %q, want %q", got, want)
				}
			},
		},
		{
			description: "positive: each is generic, floor 4, a read, not implemented, ungated and named genl_",
			check: func(t *testing.T) {
				for _, name := range want {
					c, err := Lookup(name)
					if err != nil {
						t.Fatal(err)
					}
					if c.Proto != ProtoGenericCst || c.Floor != 4 || c.Write || c.Implemented ||
						gated[name] || !strings.HasPrefix(c.Slug, "genl_") || c.NeedsDev || len(c.Setup) != 0 {
						t.Errorf("%q: %+v, gated %v", name, c, gated[name])
					}
				}
			},
		},
		{
			description: "boundary: every row from the first non-route one on is generic, so the modules load after every route row",
			check: func(t *testing.T) {
				for _, c := range cmds[first:] {
					if c.Protocol() != ProtoGenericCst {
						t.Errorf("%q, a %s row, follows the first genl row", c.Name, c.Protocol())
					}
				}
				if n := tcpmRows + srRows + ioamRows + mptcpRows + fouRows + ilaRows + l2tpRows + len(want); len(cmds)-first != n {
					t.Errorf("%d rows from the first genl one, want %d", len(cmds)-first, n)
				}
			},
		},
		{
			description: "negative: no stand-in is implemented and no implemented row is a stand-in or gated as one",
			check: func(t *testing.T) {
				for _, c := range cmds {
					if c.IPStandIn && (c.Implemented || gated[c.Name]) {
						t.Errorf("%q is a stand-in and implemented or gated", c.Name)
					}
				}
			},
		},
		{
			description: "corner: every Proto is empty or one the comparator can read, and never spelled route",
			check: func(t *testing.T) {
				for _, c := range cmds {
					if _, ok := nlparity.ProtocolByName(c.Protocol()); !ok || c.Proto == protoRouteCst {
						t.Errorf("%q has Proto %q", c.Name, c.Proto)
					}
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, tt.check)
	}
}

// isAlnum is an ASCII letter or digit.
func isAlnum(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
}

// tcpmTopology is a tcp_metrics row's Setup as its corpus capture's topology
// file records it: each `ip` step, and each `tcp SERVER CLIENT` step as
// xtcp2-nl-tcp's line, the client to the server's port 7171, closed.
func tcpmTopology(setup []string) []string {
	steps := make([]string, 0, len(setup))
	for _, s := range setup {
		f := strings.Fields(s)
		if len(f) != 3 || f[0] != "tcp" {
			steps = append(steps, "ip "+s)
			continue
		}
		server := f[1]
		if strings.Contains(server, ":") {
			server = "[" + server + "]"
		}
		steps = append(steps, fmt.Sprintf("tcp %s -> %s:7171 closed", f[2], server))
	}
	return steps
}

// corpusTopology is every step a capture's topology file records as done.
func corpusTopology(t *testing.T, dir, state string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, state, "topology"))
	if err != nil {
		t.Fatalf("reading %s's topology: %v", state, err)
	}
	var steps []string
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(line, "ok ") {
			continue
		}
		if _, step, ok := strings.Cut(line, "] "); ok {
			steps = append(steps, step)
		}
	}
	return steps
}

// TestTCPMetricsRows pins G6-T01's fourteen rows: the C2 corpus's tcpm_*
// rows, NETLINK_GENERIC, each on its capture's precondition, connections
// included; the deletes and the flushes that write, write rows of their DEL
// counts; and the shows, the gets and the flush of nothing, read rows. Every
// one is gated with no allowlist entry after two clean live runs.
//
// go test ./internal/goipparity/ -run TestTCPMetricsRows
func TestTCPMetricsRows(t *testing.T) {
	al, err := nlparity.EmbeddedAllowlist()
	if err != nil {
		t.Fatalf("EmbeddedAllowlist: %v", err)
	}
	type want struct {
		implemented, write, topology, gated bool
		proto                               string
		floor, writes, entries              int
	}
	read := want{implemented: true, topology: true, gated: true, proto: ProtoGenericCst, floor: 4}
	del := want{implemented: true, write: true, topology: true, gated: true, proto: ProtoGenericCst, floor: 4}
	flushAll := del
	flushAll.writes = 1
	flush := del
	flush.floor, flush.writes = 6, 2
	rows := []struct {
		description string
		command     string
		corpus      string // the slug whose topology Setup must be
		expected    want
	}{
		{"positive: show, the four entries", "tcp_metrics show", "tcpm_show", read},
		{"boundary: list, the empty table", "tcp_metrics list", "tcpm_show_empty", read},
		{"positive: -6 show, filtered on ip's side", "-6 tcp_metrics show", "tcpm_show_v6", read},
		{"positive: get by address", "tcp_metrics show address 192.0.2.1", "tcpm_get", read},
		{"positive: get by source and address", "tcp_metrics show src 192.0.2.2 address 192.0.2.1", "tcpm_get_src", read},
		{"positive: get by IPv6 address", "tcp_metrics show address 2001:db8::1", "tcpm_get_v6", read},
		{"negative: get of a missing entry, the kernel's ESRCH", "tcp_metrics show address 192.0.2.9",
			"tcpm_get_missing", read},
		{"positive: delete, one acked DEL", "tcp_metrics delete address 192.0.2.1", "tcpm_delete", del},
		{"positive: delete by source", "tcp_metrics delete src 192.0.2.2 address 192.0.2.1", "tcpm_delete_src", del},
		{"negative: delete of a missing entry, refused, the same request", "tcp_metrics delete address 192.0.2.9",
			"tcpm_delete_missing", del},
		{"positive: flush all, one acked DEL with no address", "tcp_metrics flush all", "tcpm_flush_all", flushAll},
		{"positive: flush of a prefix, two DELs and a second dump", "tcp_metrics flush 192.0.2.0/24",
			"tcpm_flush_prefix", flush},
		{"positive: -6 flush, two DELs and a second dump", "-6 tcp_metrics flush", "tcpm_flush_v6", flush},
		{"boundary: a flush of nothing, the dump alone, a read row", "tcp_metrics flush address 192.0.2.0/24",
			"tcpm_flush_empty", read},
		{"negative: a flush of an exact address is not in the matrix", "tcp_metrics flush 192.0.2.1", "", want{}},
	}
	for _, r := range rows {
		t.Run(r.description, func(t *testing.T) {
			var got want
			if c, lookupErr := Lookup(r.command); lookupErr == nil {
				got = want{implemented: c.Implemented, write: c.Write, proto: c.Proto,
					topology: slices.Equal(tcpmTopology(c.Setup), corpusTopology(t, tdGenl, r.corpus)),
					floor:    c.Floor, writes: c.Writes}
			}
			got.gated = al.IsGated(r.command)
			got.entries = len(al.EntriesFor(r.command))
			if got != r.expected {
				t.Errorf("%q: got %+v, want %+v", r.command, got, r.expected)
			}
		})
	}
}

// srTopology is an sr row's Setup as its corpus capture's topology file
// records it: each `hmac KEYID ALGO SECRET` step as xtcp2-nlcap hmac's
// line, the secret left out.
func srTopology(setup []string) []string {
	steps := make([]string, 0, len(setup))
	for _, s := range setup {
		f := strings.Fields(s)
		steps = append(steps, "sr hmac set "+strings.Join(f[1:3], " "))
	}
	return steps
}

// srCorpusTopology is corpusTopology, or none for a capture that started
// from an empty namespace and so has no topology file.
func srCorpusTopology(t *testing.T, slug string) []string {
	t.Helper()
	if _, err := os.Stat(filepath.Join(tdGenl, slug, "topology")); errors.Is(err, os.ErrNotExist) {
		return []string{}
	}
	return corpusTopology(t, tdGenl, slug)
}

// srCorpusStdin is the line a corpus row's SETHMAC was given: its SECRET,
// SEG6_ATTR_SECRET (4), or the blank line when it has none; nil for a row
// with no SETHMAC (SEG6_CMD_SETHMAC, 1).
func srCorpusStdin(t *testing.T, slug string) *string {
	t.Helper()
	capture, err := nlparity.ParseCaptureFile(filepath.Join(tdGenl, slug, "ip.pcap"), 16)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range capture.Datagrams {
		for _, m := range d.Msgs {
			if !nlparity.IsWrite(m) || m.Body[0] != 1 {
				continue
			}
			line := ""
			for b := m.Body[4:]; len(b) >= 4; {
				n := int(binary.LittleEndian.Uint16(b))
				if n < 4 || n > len(b) {
					t.Fatalf("%s: a bad attribute", slug)
				}
				if binary.LittleEndian.Uint16(b[2:]) == 4 {
					line = string(b[4:n])
				}
				b = b[min((n+3)&^3, len(b)):]
			}
			return stdinLine(line)
		}
	}
	return nil
}

// srShowOut is `ip sr hmac show`'s stdout and stderr for the keys a Setup
// made, each secret cut at print_dumphmac's 63 bytes with its warning.
func srShowOut(setup []string) (string, string) {
	var out, errOut strings.Builder
	for _, s := range setup {
		f := strings.Fields(s)
		secret := f[3]
		if len(secret) > 63 {
			fmt.Fprintf(&errOut, "HMAC secret length %d > 63, truncated\n", len(secret))
			secret = secret[:63]
		}
		fmt.Fprintf(&out, "hmac %s algo %s secret %q\n", f[1], f[2], secret)
	}
	return out.String(), errOut.String()
}

// TestSrRows pins G6-T02's eleven rows: the C2 corpus's sr_* rows,
// NETLINK_GENERIC, each on its capture's precondition, the keys it starts
// from made by hmac steps whose secrets the shows print and warn of; the sets, write
// rows reading the secret their capture's SETHMAC carries on stdin; and
// the shows, read rows. Every one is gated with no allowlist entry after
// two clean live runs.
//
// go test ./internal/goipparity/ -run TestSrRows
func TestSrRows(t *testing.T) {
	al, err := nlparity.EmbeddedAllowlist()
	if err != nil {
		t.Fatalf("EmbeddedAllowlist: %v", err)
	}
	type want struct {
		implemented, write, topology, stdin, output, gated bool
		proto                                              string
		floor, entries                                     int
	}
	read := want{implemented: true, topology: true, stdin: true, output: true, gated: true, proto: ProtoGenericCst, floor: 4}
	set := read
	set.write = true
	rows := []struct {
		description string
		command     string
		corpus      string
		expected    want
	}{
		{"boundary: show of no key, the dump topology's", "sr hmac s", "sr_hmac_show_empty", read},
		{"positive: show of PRE_HMAC2's two keys", "sr hmac show", "sr_hmac_show", read},
		{"boundary: show of a 64-byte secret, printed as 63", "sr hm show", "sr_hmac_show_64", read},
		{"positive: tunsrc show, the dump topology's ::", "sr tunsrc show", "sr_tunsrc_show", read},
		{"positive: set, sha256, its secret on stdin", "sr hmac set 1 sha256", "sr_hmac_set", set},
		{"positive: set, sha1", "sr hmac set 2 sha1", "sr_hmac_set_sha1", set},
		{"positive: set over PRE_HMAC1's key", "sr hmac set 1 sha1", "sr_hmac_replace", set},
		{"positive: the delete, a blank line, over PRE_HMAC2", "sr hmac se 1 sha256", "sr_hmac_delete", set},
		{"negative: a delete of no key, refused, the same request", "sr hmac set 9 sha256", "sr_hmac_delete_missing", set},
		{"negative: a 65-byte secret, refused, the same request", "sr hmac set 3 sha256", "sr_hmac_set_long", set},
		{"positive: tunsrc set, no stdin", "sr tunsrc set 2001:db8::1", "sr_tunsrc_set", set},
		{"negative: the corpus's own spelling of the delete is the set's row, not a second one", "sr hmac set 1 sha256",
			"sr_hmac_delete", want{implemented: true, write: true, topology: false, stdin: false, output: true,
				gated: true, proto: ProtoGenericCst, floor: 4}},
	}
	for _, r := range rows {
		t.Run(r.description, func(t *testing.T) {
			var got want
			if c, lookupErr := Lookup(r.command); lookupErr == nil {
				corpusStdin := srCorpusStdin(t, r.corpus)
				out := true
				if strings.HasPrefix(r.corpus, "sr_hmac_show") {
					stdout, stderr := srShowOut(c.Setup)
					out = mustRead(t, filepath.Join(tdGenl, r.corpus, "ip.out")) == stdout &&
						mustRead(t, filepath.Join(tdGenl, r.corpus, "ip.err")) == stderr
				}
				got = want{implemented: c.Implemented, write: c.Write, proto: c.Proto, floor: c.Floor,
					topology: slices.Equal(srTopology(c.Setup), srCorpusTopology(t, r.corpus)),
					stdin:    (c.Stdin == nil) == (corpusStdin == nil) && (c.Stdin == nil || *c.Stdin == *corpusStdin),
					output:   out}
			}
			got.gated = al.IsGated(r.command)
			got.entries = len(al.EntriesFor(r.command))
			if got != r.expected {
				t.Errorf("%q: got %+v, want %+v", r.command, got, r.expected)
			}
		})
	}
}

// TestIoamRows pins G6-T03's twenty rows: the C2 corpus's 18 ioam_* rows
// that are not the monitor's, NETLINK_GENERIC, each on its capture's
// precondition; the adds, deletes and bindings, write rows; the empty
// shows, read rows; and two -j shows on one bound namespace, read rows
// with no corpus row. Every one is gated with no allowlist entry after two
// clean live runs, and no monitor row is in the table.
//
// go test ./internal/goipparity/ -run TestIoamRows
func TestIoamRows(t *testing.T) {
	al, err := nlparity.EmbeddedAllowlist()
	if err != nil {
		t.Fatalf("EmbeddedAllowlist: %v", err)
	}
	type want struct {
		implemented, write, topology, gated bool
		proto                               string
		floor, entries                      int
	}
	read := want{implemented: true, topology: true, gated: true, proto: ProtoGenericCst, floor: 4}
	write := read
	write.write = true
	rows := []struct {
		description string
		command     string
		corpus      string // the slug whose topology Setup must be; "" for ioamJSONSetup
		expected    want
	}{
		{"boundary: namespace show with none", "ioam namespace show", "ioam_ns_show_empty", read},
		{"boundary: schema show with none", "ioam schema show", "ioam_sc_show_empty", read},
		{"positive: -j namespace show, one bound namespace", "-j ioam namespace show", "", read},
		{"positive: -j schema show, its schema", "-j ioam schema show", "", read},
		{"positive: namespace add", "ioam namespace add 1", "ioam_ns_add", write},
		{"positive: with data and wide data", "ioam namespace add 2 data 42 wide 2a", "ioam_ns_add_data", write},
		{"boundary: all ones", "ioam namespace add 3 data 0xffffffff wide ffffffffffffffff", "ioam_ns_add_unavailable", write},
		{"negative: a duplicate, refused, spelled 0x1", "ioam namespace add 0x1", "ioam_ns_add_dup", write},
		{"positive: namespace del", "ioam namespace del 1", "ioam_ns_del", write},
		{"negative: a del of none, refused", "ioam namespace del 9", "ioam_ns_del_missing", write},
		{"positive: schema add", "ioam schema add 1 abcd", "ioam_sc_add", write},
		{"boundary: five bytes", "ioam schema add 2 abcde", "ioam_sc_add_pad", write},
		{"negative: a duplicate schema, refused", "ioam schema add 1 wxyz", "ioam_sc_add_dup", write},
		{"positive: schema del", "ioam schema del 1", "ioam_sc_del", write},
		{"negative: a schema del of none, refused", "ioam schema del 9", "ioam_sc_del_missing", write},
		{"positive: the binding", "ioam namespace set 1 schema 1", "ioam_ns_set_schema", write},
		{"positive: the unbinding", "ioam namespace set 1 schema none", "ioam_ns_set_none", write},
		{"negative: a binding to none, refused", "ioam namespace set 1 schema 9", "ioam_ns_set_missing", write},
		{"corner: a bound schema's del, spelled 0x1", "ioam schema del 0x1", "ioam_sc_del_bound", write},
		{"corner: a bound namespace's del, spelled 01", "ioam namespace del 01", "ioam_ns_del_bound", write},
		{"negative: the monitor is not in the table", "ioam monitor", "", want{}},
	}
	for _, r := range rows {
		t.Run(r.description, func(t *testing.T) {
			var got want
			if c, lookupErr := Lookup(r.command); lookupErr == nil {
				topology := slices.Equal(c.Setup, ioamJSONSetup)
				if r.corpus != "" {
					steps := make([]string, 0, len(c.Setup))
					for _, s := range c.Setup {
						steps = append(steps, "ip "+s)
					}
					topology = slices.Equal(steps, srCorpusTopology(t, r.corpus))
				}
				got = want{implemented: c.Implemented, write: c.Write, proto: c.Proto, floor: c.Floor, topology: topology}
			}
			got.gated = al.IsGated(r.command)
			got.entries = len(al.EntriesFor(r.command))
			if got != r.expected {
				t.Errorf("%q: got %+v, want %+v", r.command, got, r.expected)
			}
		})
	}
}

// TestMptcpRows pins G6-T04's 28 mptcp rows: implemented and gated, generic,
// each on its corpus row's preconditions where it has one, the three -j
// shows on the captured shows', and the port refusal a read of floor 2. The
// monitor is not in the table.
//
// go test ./internal/goipparity/ -run TestMptcpRows
func TestMptcpRows(t *testing.T) {
	al, err := nlparity.EmbeddedAllowlist()
	if err != nil {
		t.Fatalf("EmbeddedAllowlist: %v", err)
	}
	type want struct {
		implemented, write, topology, gated bool
		proto                               string
		floor                               int
	}
	read := want{implemented: true, topology: true, gated: true, proto: ProtoGenericCst, floor: 4}
	write := read
	write.write = true
	refused := read
	refused.floor = 2
	rows := []struct {
		description string
		command     string
		corpus      string // the slug whose topology Setup must be
		expected    want
	}{
		{"boundary: endpoint show with none, spelled s", "mptcp endpoint s", "mptcp_endpoint_show_empty", read},
		{"positive: endpoint show, two", "mptcp endpoint show", "mptcp_endpoint_show", read},
		{"positive: endpoint show id", "mptcp endpoint show id 2", "mptcp_endpoint_get", read},
		{"negative: a GET of none, refused", "mptcp endpoint show id 9", "mptcp_endpoint_get_missing", read},
		{"positive: limits show", "mptcp limits show", "mptcp_limits_show", read},
		{"negative: a port without signal, ip's own refusal", "mptcp endpoint add 192.0.2.2 port 10100 subflow",
			"mptcp_endpoint_add_port_subflow", refused},
		{"positive: -j endpoint show, the two", "-j mptcp endpoint show", "mptcp_endpoint_show", read},
		{"positive: -j endpoint show id", "-j mptcp endpoint show id 2", "mptcp_endpoint_get", read},
		{"corner: -j limits show, text", "-j mptcp limits show", "mptcp_limits_show", read},
		{"positive: endpoint add", "mptcp endpoint add 192.0.2.1 id 1 subflow", "mptcp_endpoint_add", write},
		{"positive: on d0", "mptcp endpoint add 2001:db8::1 dev d0 signal backup", "mptcp_endpoint_add_v6_dev", write},
		{"positive: a port", "mptcp endpoint add 192.0.2.2 port 10100 signal", "mptcp_endpoint_add_port", write},
		{"positive: fullmesh", "mptcp endpoint add 192.0.2.2 subflow fullmesh", "mptcp_endpoint_add_fullmesh", write},
		{"negative: a duplicate address, refused", "mptcp endpoint add 192.0.2.1 subflow", "mptcp_endpoint_add_dup", write},
		{"negative: an id in use, refused", "mptcp endpoint add 192.0.2.2 id 1 subflow", "mptcp_endpoint_add_id_busy", write},
		{"positive: change", "mptcp endpoint change id 1 backup", "mptcp_endpoint_change", write},
		{"positive: change by address", "mptcp endpoint change 192.0.2.1 fullmesh", "mptcp_endpoint_change_addr", write},
		{"boundary: nobackup, no flags", "mptcp endpoint change id 1 nobackup", "mptcp_endpoint_change_nobackup", write},
		{"negative: a change of none, refused", "mptcp endpoint change id 9 backup", "mptcp_endpoint_change_missing", write},
		{"positive: delete", "mptcp endpoint delete id 1", "mptcp_endpoint_delete", write},
		{"negative: a delete of none, refused", "mptcp endpoint delete id 9", "mptcp_endpoint_delete_missing", write},
		{"corner: id 0 with its address", "mptcp endpoint delete id 0 192.0.2.1", "mptcp_endpoint_delete_id0", write},
		{"positive: flush", "mptcp endpoint flush", "mptcp_endpoint_flush", write},
		{"positive: limits set", "mptcp limits set subflows 4 add_addr_accepted 3", "mptcp_limits_set", write},
		{"positive: one limit", "mptcp limits set add_addr_accepted 2", "mptcp_limits_set_one", write},
		{"boundary: none", "mptcp limits set", "mptcp_limits_set_none", write},
		{"boundary: the maxima", "mptcp limits set subflows 8 add_addr_accepted 8", "mptcp_limits_set_max", write},
		{"negative: over them, refused", "mptcp limits set subflows 9", "mptcp_limits_set_over", write},
		{"negative: the monitor is not in the table", "mptcp monitor", "", want{}},
		{"negative: nor is the stand-in it replaced", "mptcp endpoint show ", "", want{}},
	}
	for _, r := range rows {
		t.Run(r.description, func(t *testing.T) {
			var got want
			if c, lookupErr := Lookup(r.command); lookupErr == nil {
				steps := make([]string, 0, len(c.Setup))
				for _, s := range c.Setup {
					steps = append(steps, "ip "+s)
				}
				topology := r.corpus != "" && slices.Equal(steps, srCorpusTopology(t, r.corpus))
				got = want{implemented: c.Implemented, write: c.Write, proto: c.Proto, floor: c.Floor, topology: topology}
			}
			got.gated = al.IsGated(r.command)
			if got != r.expected {
				t.Errorf("%q: got %+v, want %+v", r.command, got, r.expected)
			}
		})
	}
}

// TestFouRows pins G6-T05's fou rows: each on its corpus row's precondition
// (its topology file), and each gated. A row the corpus spells as another
// does is spelled by prefix, the same bytes; -j fou sh is on fou_add_dev's
// precondition and command, the state its readback shows.
//
// go test ./internal/goipparity/ -run TestFouRows
func TestFouRows(t *testing.T) {
	al, err := nlparity.EmbeddedAllowlist()
	if err != nil {
		t.Fatalf("EmbeddedAllowlist: %v", err)
	}
	type want struct {
		implemented, write, topology, gated bool
		proto                               string
		floor                               int
	}
	read := want{implemented: true, topology: true, gated: true, proto: ProtoGenericCst, floor: 4}
	write := read
	write.write = true
	refused := read
	refused.floor = 2
	const devAdd = "ip fou add port 5555 ipproto 4 local 192.0.2.1 dev d0"
	rows := []struct {
		description string
		command     string
		corpus      string // the slug whose topology Setup must be
		then        string // a step Setup holds after it
		expected    want
	}{
		{"boundary: show with none, spelled s", "fou s", "fou_show_empty", "", read},
		{"positive: show, two", "fou show", "fou_show", "", read},
		{"negative: show with an argument, ip's own refusal", "fou show port 5555", "fou_show_args", "", refused},
		{"negative: port 0, likewise", "fou add port 0 gue", "fou_add_port_zero", "", refused},
		{"negative: an unknown device, likewise", "fou add port 5555 gue local 192.0.2.1 dev nosuch",
			"fou_add_unknown_dev", "", refused},
		{"positive: -j show, the two", "-j fou show", "fou_show", "", read},
		{"positive: -j, both addresses", "-j fou sho", "fou_del_local_peer", "", read},
		{"corner: -j, a device, on fou_add_dev's state", "-j fou sh", "fou_add_dev", devAdd, read},
		{"positive: add", "fou add port 5555 ipproto 4", "fou_add", "", write},
		{"positive: gue", "fou add port 5555 gue", "fou_add_gue", "", write},
		{"positive: -6", "fou add port 5555 gue -6", "fou_add_v6", "", write},
		{"positive: both addresses", "fou add port 5555 ipproto 4 local 192.0.2.1 peer 192.0.2.2 peer_port 5556",
			"fou_add_local_peer", "", write},
		{"positive: over IPv6", "fou add port 5555 gue -6 local 2001:db8::1 peer 2001:db8::2 peer_port 5556",
			"fou_add_v6_local_peer", "", write},
		{"positive: a device", "fou add port 5555 ipproto 4 local 192.0.2.1 dev d0", "fou_add_dev", "", write},
		{"negative: a device without local, refused", "fou add port 5555 ipproto 4 dev d0", "fou_add_dev_no_local", "",
			write},
		{"negative: a port in use, refused, spelled a", "fou a port 5555 gue", "fou_add_dup", "", write},
		{"positive: the port over IPv6 too, spelled a", "fou a port 5555 gue -6", "fou_add_same_port_v6", "", write},
		{"corner: no port, no attribute", "fou add ipproto 4", "fou_add_no_port", "", write},
		{"corner: no type, likewise", "fou add port 5555", "fou_add_no_type", "", write},
		{"positive: del", "fou del port 5555", "fou_del", "", write},
		{"negative: del of none, refused, spelled d", "fou d port 5555", "fou_del_missing", "", write},
		{"negative: del in the other family, refused", "fou del port 5555 -6", "fou_del_wrong_family", "", write},
		{"positive: del with the addresses", "fou del port 5555 local 192.0.2.1 peer 192.0.2.2 peer_port 5556",
			"fou_del_local_peer", "", write},
		{"negative: del without them, refused, spelled de", "fou de port 5555", "fou_del_partial", "", write},
	}
	for _, r := range rows {
		t.Run(r.description, func(t *testing.T) {
			var got want
			if c, lookupErr := Lookup(r.command); lookupErr == nil {
				steps := make([]string, 0, len(c.Setup))
				for _, s := range c.Setup {
					steps = append(steps, "ip "+s)
				}
				var corpus []string
				if r.corpus != "" {
					corpus = srCorpusTopology(t, r.corpus)
					if r.then != "" {
						corpus = append(corpus, r.then)
					}
				}
				topology := r.corpus != "" && slices.Equal(steps, corpus)
				got = want{implemented: c.Implemented, write: c.Write, proto: c.Proto, floor: c.Floor, topology: topology}
			}
			got.gated = al.IsGated(r.command)
			if got != r.expected {
				t.Errorf("%q: got %+v, want %+v", r.command, got, r.expected)
			}
		})
	}
}

// TestIlaRows pins G6-T06's ila rows: each on its corpus row's precondition
// (its topology file), and each gated. The show of one locator to match is
// on ila_del_dev's precondition, and -j ila sh on ila_add_opts's and its
// command; the corpus's show of two is replayed, not a row.
//
// go test ./internal/goipparity/ -run TestIlaRows
func TestIlaRows(t *testing.T) {
	al, err := nlparity.EmbeddedAllowlist()
	if err != nil {
		t.Fatalf("EmbeddedAllowlist: %v", err)
	}
	type want struct {
		implemented, write, topology, gated bool
		proto                               string
		floor                               int
	}
	read := want{implemented: true, topology: true, gated: true, proto: ProtoGenericCst, floor: 4}
	write := read
	write.write = true
	refused := read
	refused.floor = 2
	const optsAdd = "ip ila add loc_match 2001:db8:1:2 loc 2001:db8:3:4 csum-mode adj-transport ident-type luid"
	rows := []struct {
		description string
		command     string
		corpus      string // the slug whose topology Setup must be
		then        string // a step Setup holds after it
		expected    want
	}{
		{"boundary: show with none, spelled s", "ila s", "ila_show_empty", "", read},
		{"positive: show, one locator to match with two mappings", "ila show", "ila_del_dev", "", read},
		{"negative: show with an argument, ip's own refusal", "ila show 2001:db8:1:2", "ila_show_args", "", refused},
		{"negative: an unknown word, usage", "ila add loc_match 2001:db8:1:2 loc 2001:db8:3:4 bogus", "ila_add_unknown_word", "", refused},
		{"positive: -j show, the two mappings", "-j ila show", "ila_del_dev", "", read},
		{"positive: -j, the device's mapping", "-j ila sho", "ila_del_nodev_of_dev", "", read},
		{"corner: -j, both options, on ila_add_opts's state", "-j ila sh", "ila_add_opts", optsAdd, read},
		{"positive: add", "ila add loc_match 2001:db8:1:2 loc 2001:db8:3:4", "ila_add", "", write},
		{"positive: on a device", "ila add loc_match 2001:db8:1:2 loc 2001:db8:3:4 dev d0", "ila_add_dev", "", write},
		{"positive: both options", "ila add loc_match 2001:db8:1:2 loc 2001:db8:3:4 csum-mode adj-transport ident-type luid", "ila_add_opts", "", write},
		{"positive: the other two", "ila add loc_match 2001:db8:1:2 loc 2001:db8:3:4 csum-mode neutral-map-auto ident-type use-format", "ila_add_auto", "", write},
		{"negative: a mapping that exists, refused, spelled a", "ila a loc_match 2001:db8:1:2 loc 2001:db8:3:4", "ila_add_dup", "", write},
		{"positive: the same match on a device, spelled a", "ila a loc_match 2001:db8:1:2 loc 2001:db8:3:4 dev d0", "ila_add_same_match_dev", "", write},
		{"corner: no locator, no attribute", "ila add loc_match 2001:db8:1:2", "ila_add_no_loc", "", write},
		{"positive: del", "ila del loc_match 2001:db8:1:2", "ila_del", "", write},
		{"negative: del of none, spelled d", "ila d loc_match 2001:db8:1:2", "ila_del_missing", "", write},
		{"positive: del on a device", "ila del loc_match 2001:db8:1:2 dev d0", "ila_del_dev", "", write},
		{"negative: del without the device of one that has it, spelled de", "ila de loc_match 2001:db8:1:2", "ila_del_nodev_of_dev", "", write},
		{"negative: the show of two locators to match is not in the table", "ila list", "", "", want{}},
	}
	for _, r := range rows {
		t.Run(r.description, func(t *testing.T) {
			var got want
			if c, lookupErr := Lookup(r.command); lookupErr == nil {
				steps := make([]string, 0, len(c.Setup))
				for _, s := range c.Setup {
					steps = append(steps, "ip "+s)
				}
				var corpus []string
				if r.corpus != "" {
					corpus = srCorpusTopology(t, r.corpus)
					if r.then != "" {
						corpus = append(corpus, r.then)
					}
				}
				topology := r.corpus != "" && slices.Equal(steps, corpus)
				got = want{implemented: c.Implemented, write: c.Write, proto: c.Proto, floor: c.Floor, topology: topology}
			}
			got.gated = al.IsGated(r.command)
			if got != r.expected {
				t.Errorf("%q: got %+v, want %+v", r.command, got, r.expected)
			}
		})
	}
}

// TestL2tpRows pins G6-T07's l2tp rows: each on its corpus row's
// precondition (its topology file), and each gated. The -j shows of two
// are on the captured shows' preconditions, and the other two on a write
// row's precondition and its command.
//
// go test ./internal/goipparity/ -run TestL2tpRows
func TestL2tpRows(t *testing.T) {
	al, err := nlparity.EmbeddedAllowlist()
	if err != nil {
		t.Fatalf("EmbeddedAllowlist: %v", err)
	}
	type want struct {
		implemented, write, topology, gated bool
		proto                               string
		floor                               int
	}
	read := want{implemented: true, topology: true, gated: true, proto: ProtoGenericCst, floor: 4}
	write := read
	write.write = true
	refused := read
	refused.floor = 2
	rows := []struct {
		description string
		command     string
		corpus      string // the slug whose topology Setup must be
		then        string // a step Setup holds after it
		expected    want
	}{
		{"boundary: show tunnel with none, spelled s", "l2tp s tunnel", "l2tp_tunnel_show_empty", "", read},
		{"positive: show tunnel, two", "l2tp show tunnel", "l2tp_tunnel_show", "", read},
		{"positive: show tunnel with an id", "l2tp show tunnel tunnel_id 1", "l2tp_tunnel_show_id", "", read},
		{"boundary: show session with none, spelled s", "l2tp s session", "l2tp_session_show_empty", "", read},
		{"positive: show session, two", "l2tp show session", "l2tp_session_show", "", read},
		{"positive: show session with both ids", "l2tp show session tunnel_id 1 session_id 10",
			"l2tp_session_show_id", "", read},
		{"negative: a bare show, usage", "l2tp show", "l2tp_show_bare", "", refused},
		{"negative: no local, missarg",
			"l2tp add tunnel tunnel_id 1 peer_tunnel_id 2 remote 192.0.2.2 udp_sport 5000 udp_dport 6000",
			"l2tp_tunnel_add_no_local", "", refused},
		{"negative: no peer session id, missarg", "l2tp add session tunnel_id 1 session_id 10",
			"l2tp_session_add_no_peer", "", refused},
		{"positive: -j show tunnel, two", "-j l2tp show tunnel", "l2tp_tunnel_show", "", read},
		{"corner: -j, IPv6's checksums, on l2tp_tunnel_add_v6's state", "-j l2tp sho tunnel", "l2tp_tunnel_add_v6",
			"ip " + l2tpTunV6Cst, read},
		{"positive: -j show session, two", "-j l2tp show session", "l2tp_session_show", "", read},
		{"corner: -j, every option, on l2tp_session_add_opts's state", "-j l2tp sho session",
			"l2tp_session_add_opts", "ip " + l2tpSessOptsCst, read},
		{"positive: add a UDP tunnel", l2tpTun1Cst, "l2tp_tunnel_add", "", write},
		{"positive: with the checksum", l2tpTun1Cst + " udp_csum on", "l2tp_tunnel_add_csum", "", write},
		{"positive: over IPv6", l2tpTunV6Cst, "l2tp_tunnel_add_v6", "", write},
		{"positive: IP-encap", l2tpTunIPCst, "l2tp_tunnel_add_ip", "", write},
		{"negative: a tunnel id in use",
			"l2tp add tunnel tunnel_id 1 peer_tunnel_id 2 local 192.0.2.1 remote 192.0.2.2 udp_sport 5001 udp_dport 6001",
			"l2tp_tunnel_add_dup", "", write},
		{"positive: del tunnel", "l2tp del tunnel tunnel_id 1", "l2tp_tunnel_del", "", write},
		{"negative: del of a missing tunnel", "l2tp del tunnel tunnel_id 9", "l2tp_tunnel_del_missing", "", write},
		{"positive: del of a tunnel with a session, spelled d", "l2tp d tunnel tunnel_id 1",
			"l2tp_tunnel_del_sessions", "", write},
		{"positive: add a session", l2tpSess1Cst, "l2tp_session_add", "", write},
		{"positive: every option", l2tpSessOptsCst, "l2tp_session_add_opts", "", write},
		{"negative: a session id in use in a UDP tunnel, accepted, spelled a",
			"l2tp a session tunnel_id 1 session_id 10 peer_session_id 20", "l2tp_session_add_dup", "", write},
		{"negative: in an IP tunnel, refused, spelled ad",
			"l2tp ad session tunnel_id 1 session_id 10 peer_session_id 20", "l2tp_session_add_dup_ip", "", write},
		{"negative: in a missing tunnel, spelled with tid",
			"l2tp add session tid 1 session_id 10 peer_session_id 20", "l2tp_session_add_no_tunnel", "", write},
		{"positive: del session", "l2tp del session tunnel_id 1 session_id 10", "l2tp_session_del", "", write},
		{"negative: del of a missing session", "l2tp del session tunnel_id 1 session_id 99",
			"l2tp_session_del_missing", "", write},
		{"negative: the stand-in's slug is gone", "genl_l2tp_show_tunnel", "", "", want{}},
	}
	for _, r := range rows {
		t.Run(r.description, func(t *testing.T) {
			var got want
			if c, lookupErr := Lookup(r.command); lookupErr == nil {
				steps := make([]string, 0, len(c.Setup))
				for _, s := range c.Setup {
					steps = append(steps, "ip "+s)
				}
				var corpus []string
				if r.corpus != "" {
					corpus = srCorpusTopology(t, r.corpus)
					if r.then != "" {
						corpus = append(corpus, r.then)
					}
				}
				topology := r.corpus != "" && slices.Equal(steps, corpus)
				got = want{implemented: c.Implemented, write: c.Write, proto: c.Proto, floor: c.Floor, topology: topology}
			}
			got.gated = al.IsGated(r.command)
			if got != r.expected {
				t.Errorf("%q: got %+v, want %+v", r.command, got, r.expected)
			}
		})
	}
}
