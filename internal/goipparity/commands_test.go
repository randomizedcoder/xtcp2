package goipparity

import (
	"errors"
	"regexp"
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
			description: "boundary: exactly one command needs a device, and it is the only one whose Argv grows",
			check: func(t *testing.T) {
				var needy []string
				for _, c := range Commands() {
					if c.NeedsDev {
						needy = append(needy, c.Name)
					}
					if got := len(c.Argv("goip0")); got != len(c.Args)+boolToInt(c.NeedsDev) {
						t.Errorf("%q: Argv is %d long, want %d",
							c.Name, got, len(c.Args)+boolToInt(c.NeedsDev))
					}
				}
				if len(needy) != 1 || needy[0] != "link show dev" {
					t.Errorf("dev-taking commands = %q, want exactly [link show dev]", needy)
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
			description: "boundary: gated_commands is allowed to be empty, and is the honest state until the live tiers exist",
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
				"link_show_dev", "2", "yes", "yes", "link show dev", "link show dev",
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
