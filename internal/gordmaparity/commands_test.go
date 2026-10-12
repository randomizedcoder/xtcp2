package gordmaparity

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

const corpusDir = "../../pkg/xtcpnl/testdata/7_1_4/rdma"

// expRows is capture-rdma.exp's rows, in order: slug -> args, the `rdma `
// stripped and $cqn written as the placeholder; the monitor row is
// strace-monitor's, whose tool runs `rdma monitor`.
func expRows(t *testing.T) ([]string, map[string]string) {
	t.Helper()
	b, err := os.ReadFile("../../nix/microvms/scripts/capture-rdma.exp")
	if err != nil {
		t.Fatal(err)
	}
	rowRe := regexp.MustCompile(`(?m)^\s*row (\S+)\s+"rdma ([^"]*)"\s*$`)
	monRe := regexp.MustCompile(`(?m)strace-monitor \$NS \$SUB (\S+)`)
	var order []string
	args := map[string]string{}
	for _, m := range rowRe.FindAllStringSubmatch(string(b), -1) {
		order = append(order, m[1])
		args[m[1]] = strings.ReplaceAll(m[2], "$cqn", CQNPlaceholder)
	}
	for _, m := range monRe.FindAllStringSubmatch(string(b), -1) {
		order = append(order, m[1])
		args[m[1]] = "monitor"
	}
	return order, args
}

func TestParityCommands(t *testing.T) {
	order, args := expRows(t)
	captures, err := filepath.Glob(filepath.Join(corpusDir, "*.strace"))
	if err != nil {
		t.Fatal(err)
	}
	var slugs, corpus []string
	for _, c := range Commands() {
		slugs = append(slugs, c.Slug)
	}
	for _, c := range captures {
		corpus = append(corpus, strings.TrimSuffix(filepath.Base(c), ".strace"))
	}
	tests := []struct {
		description string
		check       func() []string // the violations
		expected    int
	}{
		{description: "positive: every corpus slug has a row, and no other", check: func() []string {
			a, b := slices.Sorted(slices.Values(slugs)), slices.Sorted(slices.Values(corpus))
			if !slices.Equal(a, b) {
				return []string{strings.Join(a, " "), strings.Join(b, " ")}
			}
			return nil
		}},
		{description: "positive: the rows and their order are capture-rdma.exp's", check: func() []string {
			if !slices.Equal(slugs, order) {
				return []string{strings.Join(slugs, " "), strings.Join(order, " ")}
			}
			return nil
		}},
		{description: "positive: each row's args are capture-rdma.exp's", check: func() []string {
			var bad []string
			for _, c := range Commands() {
				if got := strings.Join(c.Args, " "); got != args[c.Slug] {
					bad = append(bad, c.Slug+": "+got+" vs "+args[c.Slug])
				}
			}
			return bad
		}},
		{description: "negative: only res_qp_raw is not implemented", check: func() []string {
			var bad []string
			for _, c := range Commands() {
				if c.Implemented == (c.Slug == "res_qp_raw") {
					bad = append(bad, c.Slug)
				}
			}
			return bad
		}},
		{description: "boundary: every line has exactly 5 tab-separated fields", check: func() []string {
			var bad []string
			for _, c := range Commands() {
				if n := len(strings.Split(c.String(), "\t")); n != 5 {
					bad = append(bad, c.String())
				}
			}
			return bad
		}},
		{description: "boundary: the table has the corpus's 27 rows", check: func() []string {
			if len(Commands()) != 27 {
				return slugs
			}
			return nil
		}},
		{description: "corner: the busy rows are one run, from res_show_busy to res_cq_cqn", check: func() []string {
			var phases []string
			for _, c := range Commands() {
				if len(phases) == 0 || phases[len(phases)-1] != string(c.Phase) {
					phases = append(phases, string(c.Phase))
				}
			}
			first := slices.IndexFunc(Commands(), func(c Command) bool { return c.Phase == PhaseBusy })
			if !slices.Equal(phases, []string{"idle", "busy", "idle"}) || first < 0 || commands[first].Slug != "res_show_busy" {
				return phases
			}
			return nil
		}},
		{description: "corner: the placeholder is res_cq_cqn's alone, and the monitor is the one monitor row", check: func() []string {
			var bad []string
			for _, c := range Commands() {
				if slices.Contains(c.Args, CQNPlaceholder) != (c.Slug == "res_cq_cqn") ||
					(c.Kind == KindMonitor) != (c.Slug == "monitor_add") {
					bad = append(bad, c.Slug)
				}
			}
			return bad
		}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := tc.check(); len(got) != tc.expected {
				t.Fatalf("%d violations, want %d: %v", len(got), tc.expected, got)
			}
		})
	}
}
