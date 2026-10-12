package gordmaparity

import (
	"fmt"
	"io"
	"strings"
)

// Marker is the prefix of every line the host runner may grep for.
const Marker = "GORDMA_PARITY_"

// Render prints the report and returns its verdict. One line per row, its
// findings indented under it, then the run's totals and, last, the one line
// the host runner requires: GORDMA_PARITY_OVERALL_PASS or _FAIL.
func Render(w io.Writer, r Report) bool {
	hygiene, noisy := 0, 0
	for i := range r.Results {
		x := &r.Results[i]
		line := Marker + string(x.Status) + " " + x.Command.Slug
		switch x.Status {
		case StatusSkip:
			line += " (not implemented)"
		case StatusMissing:
			line += ": " + strings.Join(x.Missing, " ")
		case StatusPass, StatusFail:
		}
		fmt.Fprintln(w, line)
		for _, f := range x.Findings {
			fmt.Fprintln(w, "  finding: "+f)
		}
		for _, h := range x.Hygiene {
			fmt.Fprintln(w, "  hygiene: "+h)
		}
		if x.Noisy != 0 {
			fmt.Fprintf(w, "  control-suppressed: %d\n", x.Noisy)
		}
		if len(x.Hygiene) != 0 {
			hygiene++
		}
		noisy += x.Noisy
	}
	if r.Compared() == 0 {
		fmt.Fprintln(w, Marker+"NOTHING_COMPARED")
	}
	fmt.Fprintf(w, "%s%s %d\n", Marker, pick(hygiene == 0, "HYGIENE_PASS", "HYGIENE_FAIL"), hygiene)
	fmt.Fprintf(w, "%s%s %d\n", Marker, pick(noisy == 0, "CONTROL_CLEAN", "CONTROL_NOISY"), noisy)
	pass := r.Pass()
	fmt.Fprintln(w, Marker+pick(pass, "OVERALL_PASS", "OVERALL_FAIL"))
	return pass
}

func pick(c bool, yes, no string) string {
	if c {
		return yes
	}
	return no
}
