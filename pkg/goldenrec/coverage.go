package goldenrec

import (
	"slices"
	"strings"
)

// manifestRef prefixes a Coverage value that stands for a list in the
// fixture's manifest.json: "@manifest.cong_available" is every congestion
// algorithm the capture's kernel offered, so the requirement follows the
// kernel rather than a list kept by hand.
const manifestRef = "@manifest."

// Coverage is what a golden fixture must exercise, so a workload change
// can't quietly stop populating a column. It is checked against the
// fixture's Profile.
type Coverage struct {
	// Populated columns must be nonzero in at least one row.
	Populated []string `json:"populated,omitempty"`
	// Values maps a listed column (see profileListed) to values it must
	// take. An entry "@manifest.<key>" expands to the manifest's list.
	Values map[string][]string `json:"values,omitempty"`
	// Families are inet_diag_msg_family values that must have rows.
	Families []string `json:"families,omitempty"`
	// Kinds are namespace kinds (see Profile.NetnsKinds) that must have rows.
	Kinds []string `json:"kinds,omitempty"`
}

// CheckCoverage lists, sorted, how p falls short of c. refs holds the
// manifest's lists by key, for "@manifest.<key>" values.
func CheckCoverage(p Profile, c Coverage, refs map[string][]string) []string {
	var out []string
	add := func(s string) { out = append(out, "coverage: "+s) }
	for _, name := range c.Populated {
		col, ok := p.Columns[name]
		switch {
		case !ok:
			add(name + " is not a column")
		case col.Nonzero == 0:
			add(name + " never populated")
		}
	}
	for name, want := range c.Values {
		col, ok := p.Columns[name]
		switch {
		case !ok:
			add(name + " is not a column")
			continue
		case !profileListed[name]:
			add(name + " values are not listed")
			continue
		}
		for _, v := range expandRefs(want, refs, add) {
			if !slices.Contains(col.Values, v) {
				add(name + "=" + v + " missing")
			}
		}
	}
	for _, f := range c.Families {
		if p.Families[f] == 0 {
			add("family " + f + " missing")
		}
	}
	for _, k := range c.Kinds {
		if p.NetnsKinds[k] == 0 {
			add("kind " + k + " missing")
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// expandRefs replaces each "@manifest.<key>" in want with refs[key],
// reporting a key the manifest lacks or lists empty.
func expandRefs(want []string, refs map[string][]string, add func(string)) []string {
	out := make([]string, 0, len(want))
	for _, v := range want {
		key, ok := strings.CutPrefix(v, manifestRef)
		if !ok {
			out = append(out, v)
			continue
		}
		list, ok := refs[key]
		switch {
		case !ok:
			add("manifest has no " + key)
		case len(list) == 0:
			add("manifest " + key + " is empty")
		}
		out = append(out, list...)
	}
	return out
}
