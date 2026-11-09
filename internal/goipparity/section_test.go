package goipparity

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// TestSections pins the table's sections. A new object, or a row whose first
// word abbreviates its object, changes this list and has to say so here.
func TestSections(t *testing.T) {
	want := []string{
		"addr", "addrlabel", "fou", "ila", "ioam", "l2tp", "link", "macsec",
		"mptcp", "mroute", "mrule", "neigh", "netconf", "netns", "nexthop",
		"ntable", "route", "rule", "sr", "stats", "tcp_metrics", "token", "vrf",
		"xfrm",
	}
	if got := Sections(); !slices.Equal(got, want) {
		t.Errorf("Sections() = %v\nwant %v", got, want)
	}
	cmds := Commands()
	for i := range cmds {
		if cmds[i].Section() == "" {
			t.Errorf("%s (%s) has no section", cmds[i].Slug, cmds[i].Name)
		}
	}
}

func TestCommandSection(t *testing.T) {
	tests := []struct {
		description string
		args        []string
		expected    string
	}{
		{"positive: the object is the first word", []string{"link", "show"}, "link"},
		{"positive: options before the object are skipped", []string{"-d", "-j", "link", "show"}, "link"},
		{"positive: a generic netlink object", []string{"macsec", "show"}, "macsec"},
		{"boundary: the object alone", []string{"vrf"}, "vrf"},
		{"boundary: no args", nil, ""},
		{"corner: only options", []string{"-d", "-j"}, ""},
		{"corner: an abbreviated object is its own section", []string{"l", "s"}, "l"},
		{"corner: a later option-like word does not matter", []string{"route", "-6"}, "route"},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := (Command{Args: tt.args}).Section(); got != tt.expected {
				t.Errorf("Section(%q) = %q, want %q", tt.args, got, tt.expected)
			}
		})
	}
}

func TestSelectSections(t *testing.T) {
	rows := []Command{
		{Slug: "link_show", Args: []string{"link", "show"}},
		{Slug: "addr_show", Args: []string{"addr", "show"}},
		{Slug: "link_detail", Args: []string{"-d", "link", "show"}},
		{Slug: "macsec_show", Args: []string{"macsec", "show"}},
	}
	tests := []struct {
		description string
		spec        string
		expected    []string
		wantErr     bool
	}{
		{"positive: one section keeps its rows in table order", "link", []string{"link_show", "link_detail"}, false},
		{"positive: two sections keep table order, not spec order", "macsec,link", []string{"link_show", "link_detail", "macsec_show"}, false},
		{"positive: an empty spec keeps every row", "", []string{"link_show", "addr_show", "link_detail", "macsec_show"}, false},
		{"boundary: every section is every row", "addr,link,macsec", []string{"link_show", "addr_show", "link_detail", "macsec_show"}, false},
		{"corner: a repeated section is harmless", "addr,addr", []string{"addr_show"}, false},
		{"negative: an unknown section", "route", nil, true},
		{"negative: an abbreviation is not its section", "l", nil, true},
		{"negative: an empty item", "link,", nil, true},
		{"negative: a lone comma", ",", nil, true},
		{"negative: case matters", "Link", nil, true},
		{"negative: spaces are not trimmed", "link, addr", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got, err := SelectSections(rows, tt.spec)
			if tt.wantErr {
				if !errors.Is(err, ErrSection) {
					t.Fatalf("SelectSections(%q) err = %v, want ErrSection", tt.spec, err)
				}
				if !strings.Contains(err.Error(), "addr,link,macsec") {
					t.Errorf("error %q does not name the sections there are", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("SelectSections(%q) err = %v", tt.spec, err)
			}
			slugs := make([]string, 0, len(got))
			for i := range got {
				slugs = append(slugs, got[i].Slug)
			}
			if !slices.Equal(slugs, tt.expected) {
				t.Errorf("SelectSections(%q) = %v, want %v", tt.spec, slugs, tt.expected)
			}
		})
	}
}

func TestSectionLine(t *testing.T) {
	tests := []struct {
		description     string
		spec            string
		selected, total int
		expected        string
	}{
		{"positive: a filtered run names its sections and counts", "link", 48, 518, "GOIP_PARITY_SECTION link (48 of 518 rows)"},
		{"positive: the spec is printed as given", "link,addr", 124, 518, "GOIP_PARITY_SECTION link,addr (124 of 518 rows)"},
		{"boundary: an unfiltered run prints nothing", "", 518, 518, ""},
		{"corner: a filter that keeps every row still says so", "addr", 4, 4, "GOIP_PARITY_SECTION addr (4 of 4 rows)"},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := SectionLine(tt.spec, tt.selected, tt.total); got != tt.expected {
				t.Errorf("SectionLine = %q, want %q", got, tt.expected)
			}
		})
	}
}

// TestRunSection drives -section through Run, on both subcommands.
func TestRunSection(t *testing.T) {
	linkShow, err := Lookup("link show")
	if err != nil {
		t.Fatal(err)
	}
	linkRows, err := SelectSections(Commands(), "link")
	if err != nil {
		t.Fatal(err)
	}
	linkLine := SectionLine("link", len(linkRows), len(Commands()))

	tests := []struct {
		description string
		args        []string
		useDir      bool
		wantCode    int
		wantLines   int // -1: unchecked
		wantStdout  []string
		absent      []string
		wantStderr  []string
	}{
		{
			description: "positive: commands -section link prints the link rows alone",
			args:        []string{"commands", "-section", "link"},
			wantCode:    ExitOK,
			wantLines:   len(linkRows),
			wantStdout:  []string{"link_show\t2\t"},
			absent:      []string{"addr_show\t", "macsec_show_empty\t"},
		},
		{
			description: "positive: compare -section link says it was filtered and leaves other sections out",
			args:        []string{"compare", "-section", "link"},
			useDir:      true,
			wantCode:    ExitFailure, // the other link rows are missing
			wantLines:   -1,
			wantStdout:  []string{linkLine, "GOIP_PARITY_PASS link_show"},
			absent:      []string{"GOIP_PARITY_MISSING addr_show", "macsec_show_empty"},
		},
		{
			description: "boundary: compare without -section prints no section line",
			args:        []string{"compare"},
			useDir:      true,
			wantCode:    ExitFailure,
			wantLines:   -1,
			wantStdout:  []string{"GOIP_PARITY_MISSING addr_show"},
			absent:      []string{"GOIP_PARITY_SECTION"},
		},
		{
			description: "boundary: commands -section with every section prints every row",
			args:        []string{"commands", "-section", strings.Join(Sections(), ",")},
			wantCode:    ExitOK,
			wantLines:   len(Commands()),
		},
		{
			description: "negative: commands with an unknown section is a usage error",
			args:        []string{"commands", "-section", "lnk"},
			wantCode:    ExitUsage,
			wantLines:   0,
			wantStderr:  []string{`"lnk" is not one of`, "link"},
		},
		{
			description: "negative: compare with an unknown section is a usage error, not an empty comparison",
			args:        []string{"compare", "-section", "lnk"},
			useDir:      true,
			wantCode:    ExitUsage,
			wantLines:   0,
			wantStderr:  []string{`"lnk" is not one of`},
		},
		{
			description: "corner: commands -section with an empty item is a usage error",
			args:        []string{"commands", "-section", "link,"},
			wantCode:    ExitUsage,
			wantLines:   0,
			wantStderr:  []string{`"" is not one of`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			args := tt.args
			if tt.useDir {
				dir := t.TempDir()
				writeTriple(t, dir, linkShow, sameAll(tdGuestLink), sameAll(mustRead(t, tdGuestLinkOut)))
				args = append(append([]string{}, tt.args...), "-dir", dir)
			}
			var stdout, stderr bytes.Buffer
			if got := Run(args, &stdout, &stderr); got != tt.wantCode {
				t.Errorf("Run(%v) = %d, want %d\nstderr:\n%s", args, got, tt.wantCode, stderr.String())
			}
			out := stdout.String()
			if tt.wantLines >= 0 {
				if n := strings.Count(out, "\n"); n != tt.wantLines {
					t.Errorf("stdout has %d lines, want %d", n, tt.wantLines)
				}
			}
			for _, w := range tt.wantStdout {
				if !strings.Contains(out, w) {
					t.Errorf("stdout lacks %q", w)
				}
			}
			for _, a := range tt.absent {
				if strings.Contains(out, a) {
					t.Errorf("stdout has %q", a)
				}
			}
			for _, w := range tt.wantStderr {
				if !strings.Contains(stderr.String(), w) {
					t.Errorf("stderr lacks %q\n%s", w, stderr.String())
				}
			}
		})
	}
}

func BenchmarkSelectSections(b *testing.B) {
	cmds := Commands()
	for _, spec := range []string{"link", "link,addr,route"} {
		b.Run(fmt.Sprintf("spec=%s", spec), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := SelectSections(cmds, spec); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
