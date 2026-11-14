//go:build goip_write

package goipparity

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// A section is the ip object a row exercises: link, addr, route, xfrm and so
// on. `-section` on commands and compare narrows a run to the sections being
// worked on, so a run that only has to say something about `ip link` does not
// spend ~45 minutes capturing all 500-odd rows. A run without it is the full
// gate, and a filtered run says how many rows it left out (SectionLine), so
// the two cannot be mistaken for each other.

// ErrSection is a -section value that names no section, or an empty item.
var ErrSection = errors.New("goipparity: bad -section")

// Section is the row's ip object: the first word of Args that is not an
// option. TestSections pins the set, so a row whose first word abbreviates
// its object (`l s`) or is an option's value fails there instead of forming a
// section of its own.
func (c Command) Section() string {
	for _, a := range c.Args {
		if !strings.HasPrefix(a, "-") {
			return a
		}
	}
	return ""
}

// Sections lists the sections the table has rows in, sorted.
func Sections() []string {
	return sectionsOf(Commands())
}

func sectionsOf(cmds []Command) []string {
	var out []string
	for i := range cmds {
		if s := cmds[i].Section(); !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	slices.Sort(out)
	return out
}

// SelectSections keeps the rows of cmds whose section spec names, in table
// order, which keeps the route rows ahead of the generic ones the driver's
// late setup waits for. spec is a comma-separated list; an empty spec keeps
// every row. A repeated section is harmless. It refuses a section with no
// rows and an empty item, naming the sections there are.
func SelectSections(cmds []Command, spec string) ([]Command, error) {
	if spec == "" {
		return cmds, nil
	}
	known := sectionsOf(cmds)
	want := strings.Split(spec, ",")
	for _, s := range want {
		if !slices.Contains(known, s) {
			return nil, fmt.Errorf("%w: %q is not one of %s", ErrSection, s,
				strings.Join(known, ","))
		}
	}
	out := make([]Command, 0, len(cmds))
	for i := range cmds {
		if slices.Contains(want, cmds[i].Section()) {
			out = append(out, cmds[i])
		}
	}
	return out, nil
}

// SectionLine is the report's note that a run was filtered: the sections and
// how many of the table's rows they hold. Empty for an unfiltered run.
func SectionLine(spec string, selected, total int) string {
	if spec == "" {
		return ""
	}
	return fmt.Sprintf("GOIP_PARITY_SECTION %s (%d of %d rows)", spec, selected, total)
}
