package main

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// The grammar matrix (coverage-status.md, "Grammar matrix (W2)"). A space
// is one command's arguments as axes, each a list of values. Rows come
// from four strategies, in this order, with repeats dropped:
//
//   - each-choice: every value of every axis appears in some row;
//   - pairwise: every pair of values across two axes appears in some row,
//     by a deterministic greedy (AETG-like) fill;
//   - boundary: rows built from the values marked boundary;
//   - random: rows drawn from a recorded seed, the fuzz-like part.
//
// Exclusions name pairs that cannot share a positive row. An exclusion with
// an expectation becomes a negative row of its own instead of being
// dropped, as do bad values (one argument out of range). A value's facts
// are what it means, independent of how the command spells it; they are
// written into the row's axes for the corpus test's model (O3) to read.

// value is one choice on an axis.
type value struct {
	Name     string            // unique on its axis; used in ids, descs and exclusions
	Arg      string            // the command's spelling; "" adds nothing
	Facts    map[string]string // merged into the row's axes
	Pre      []string          // setup this value needs, run before the command
	Boundary bool              // the value sits on a range's edge
	Corner   bool              // an unusual spelling or a rarely taken path
}

type axis struct {
	Name   string
	Values []value
}

// exclusion keeps A and B ("axis=value") out of every positive row. With
// Expect set, the pair is written as one negative row expecting it.
type exclusion struct {
	A, B   string
	Expect *expect
	Why    string
}

// badValue is a value outside the axis's valid set, written as one
// negative row: a positive row with this one axis changed.
type badValue struct {
	Axis   string
	Value  value
	Expect expect
	Why    string
}

// space is one command's matrix. Build turns a full assignment (one value
// per axis, in axis order) into a row with its id and desc still unset.
type space struct {
	Name  string
	Axes  []axis
	Excl  []exclusion
	Bad   []badValue
	Build func(vals []value) row
	// Derive returns more negative rows made from the positive ones (a
	// duplicate add, a delete of something absent); their ids are set here.
	Derive func(pos []row) []row
	// NegBase, when set, picks the positive rows a negative may be made
	// from (a delete's setup adds the route, so it can't carry a bad one).
	NegBase func(vals []value) bool
	// Judge, when set, predicts the refusal of a full assignment that no
	// single pair explains (one that turns on three axes or more), given
	// each axis's value name; nil means it should succeed. A judged row
	// stays in the matrix as a negative with that expectation.
	Judge func(pick map[string]string) (*expect, string)
}

// matrixOpts picks the strategies' extent.
type matrixOpts struct {
	Seed   uint64
	Random int
}

// pairKey is one value pair: axis i's value vi with axis j's value vj, i < j.
type pairKey struct{ i, vi, j, vj int }

// compiled is a space with its exclusions resolved to indexes.
type compiled struct {
	sp   *space
	excl map[pairKey]bool
}

func (sp *space) lookup(ref string) (ai, vi int, err error) {
	name, val, ok := strings.Cut(ref, "=")
	if !ok {
		return 0, 0, fmt.Errorf("%s: exclusion %q is not axis=value", sp.Name, ref)
	}
	for i, a := range sp.Axes {
		if a.Name != name {
			continue
		}
		for j := range a.Values {
			if a.Values[j].Name == val {
				return i, j, nil
			}
		}
		return 0, 0, fmt.Errorf("%s: axis %s has no value %q", sp.Name, name, val)
	}
	return 0, 0, fmt.Errorf("%s: no axis %q", sp.Name, name)
}

func (sp *space) pair(e exclusion) (pairKey, error) {
	ai, vi, err := sp.lookup(e.A)
	if err != nil {
		return pairKey{}, err
	}
	aj, vj, err := sp.lookup(e.B)
	if err != nil {
		return pairKey{}, err
	}
	if ai == aj {
		return pairKey{}, fmt.Errorf("%s: exclusion %s, %s names one axis twice", sp.Name, e.A, e.B)
	}
	if ai > aj {
		ai, vi, aj, vj = aj, vj, ai, vi
	}
	return pairKey{ai, vi, aj, vj}, nil
}

func compile(sp *space) (*compiled, error) {
	if len(sp.Axes) == 0 {
		return nil, fmt.Errorf("%s: no axes", sp.Name)
	}
	seen := map[string]bool{}
	for _, a := range sp.Axes {
		if len(a.Values) == 0 || seen[a.Name] {
			return nil, fmt.Errorf("%s: axis %q is empty or repeated", sp.Name, a.Name)
		}
		seen[a.Name] = true
	}
	c := &compiled{sp: sp, excl: map[pairKey]bool{}}
	for _, e := range sp.Excl {
		k, err := sp.pair(e)
		if err != nil {
			return nil, err
		}
		c.excl[k] = true
	}
	for i := range sp.Bad {
		if !seen[sp.Bad[i].Axis] {
			return nil, fmt.Errorf("%s: bad value for no axis %q", sp.Name, sp.Bad[i].Axis)
		}
	}
	return c, nil
}

// fits reports whether axis i's value v can join the partial assignment
// a (-1 marks an unset axis).
func (c *compiled) fits(a []int, i, v int) bool {
	for j, vj := range a {
		if vj < 0 || j == i {
			continue
		}
		k := pairKey{i, v, j, vj}
		if j < i {
			k = pairKey{j, vj, i, v}
		}
		if c.excl[k] {
			return false
		}
	}
	return true
}

func unset(n int) []int {
	a := make([]int, n)
	for i := range a {
		a[i] = -1
	}
	return a
}

// fill sets every unset axis, in order, to the fitting value that scores
// highest (ties to the lowest index). It fails if some axis has no value
// that fits.
func (c *compiled) fill(a []int, score func(a []int, i, v int) int) bool {
	for i := range a {
		if a[i] >= 0 {
			continue
		}
		best, bestScore := -1, -1
		for v := range c.sp.Axes[i].Values {
			if !c.fits(a, i, v) {
				continue
			}
			if s := score(a, i, v); s > bestScore {
				best, bestScore = v, s
			}
		}
		if best < 0 {
			return false
		}
		a[i] = best
	}
	return true
}

func noScore([]int, int, int) int { return 0 }

// eachChoice returns rows in which every value that fits some row appears.
func (c *compiled) eachChoice() [][]int {
	axes := c.sp.Axes
	covered := make([][]bool, len(axes))
	for i := range axes {
		covered[i] = make([]bool, len(axes[i].Values))
	}
	var rows [][]int
	for i := range axes {
		for v := range axes[i].Values {
			if covered[i][v] {
				continue
			}
			a := unset(len(axes))
			a[i] = v
			ok := c.fill(a, func(_ []int, j, w int) int {
				if covered[j][w] {
					return 0
				}
				return 1
			})
			if !ok {
				continue
			}
			for j, w := range a {
				covered[j][w] = true
			}
			rows = append(rows, a)
		}
	}
	return rows
}

// pairs lists every value pair no exclusion forbids, in a fixed order.
func (c *compiled) pairs() []pairKey {
	axes := c.sp.Axes
	var ps []pairKey
	for i := range axes {
		for j := i + 1; j < len(axes); j++ {
			for vi := range axes[i].Values {
				for vj := range axes[j].Values {
					if k := (pairKey{i, vi, j, vj}); !c.excl[k] {
						ps = append(ps, k)
					}
				}
			}
		}
	}
	return ps
}

// pairwise returns rows covering every pair that some row can hold. Each
// row starts from the first uncovered pair and fills the other axes with
// the values that cover the most uncovered pairs. A pair no row can hold
// (its axes force an excluded third value) is counted in infeasible.
func (c *compiled) pairwise() (rows [][]int, infeasible int) {
	todo := map[pairKey]bool{}
	order := c.pairs()
	for _, k := range order {
		todo[k] = true
	}
	gain := func(a []int, i, v int) int {
		n := 0
		for j, vj := range a {
			if vj < 0 || j == i {
				continue
			}
			k := pairKey{i, v, j, vj}
			if j < i {
				k = pairKey{j, vj, i, v}
			}
			if todo[k] {
				n++
			}
		}
		return n
	}
	for _, k := range order {
		if !todo[k] {
			continue
		}
		a := unset(len(c.sp.Axes))
		a[k.i], a[k.j] = k.vi, k.vj
		if !c.fill(a, gain) {
			delete(todo, k)
			infeasible++
			continue
		}
		for i := range a {
			for j := i + 1; j < len(a); j++ {
				delete(todo, pairKey{i, a[i], j, a[j]})
			}
		}
		rows = append(rows, a)
	}
	return rows, infeasible
}

// boundaryRows returns rows of boundary values: row n takes each axis's
// n-th boundary value (cycling), for as many rows as the axis with the
// most boundary values has. An axis with none, or whose boundary value
// doesn't fit, is filled as each-choice fills it.
func (c *compiled) boundaryRows() [][]int {
	axes := c.sp.Axes
	idx := make([][]int, len(axes))
	most := 0
	for i := range axes {
		for v := range axes[i].Values {
			if axes[i].Values[v].Boundary {
				idx[i] = append(idx[i], v)
			}
		}
		most = max(most, len(idx[i]))
	}
	var rows [][]int
	for n := range most {
		a := unset(len(axes))
		for i := range axes {
			if len(idx[i]) == 0 {
				continue
			}
			if v := idx[i][n%len(idx[i])]; c.fits(a, i, v) {
				a[i] = v
			}
		}
		if c.fill(a, noScore) {
			rows = append(rows, a)
		}
	}
	return rows
}

// splitmix is SplitMix64 (Steele, Lea and Flood, "Fast splittable
// pseudorandom number generators", OOPSLA 2014): a seeded generator whose
// sequence is fixed by its definition, not by a library's version, so a
// recorded seed regenerates the same rows anywhere.
type splitmix struct{ s uint64 }

func (r *splitmix) next() uint64 {
	r.s += 0x9e3779b97f4a7c15
	z := r.s
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

// pick returns an index into a list of n items (n > 0) by walking the list
// until the draw, reduced below n, is reached; no conversion can overflow.
func (r *splitmix) pick(n int) int {
	draw := r.next()
	for i := range n {
		if draw%uint64(n-i) == 0 {
			return i
		}
		draw /= 2
	}
	return n - 1
}

// randomRows draws n rows from seed. A draw that breaks an exclusion is
// redrawn, up to a bound, so a space that excludes nearly everything ends
// rather than spins.
func (c *compiled) randomRows(seed uint64, n int) [][]int {
	r := &splitmix{s: seed}
	axes := c.sp.Axes
	var rows [][]int
	for tries := 0; len(rows) < n && tries < 100*n; tries++ {
		a := unset(len(axes))
		ok := true
		for i := range axes {
			v := r.pick(len(axes[i].Values))
			if !c.fits(a, i, v) {
				ok = false
				break
			}
			a[i] = v
		}
		if ok {
			rows = append(rows, a)
		}
	}
	return rows
}

func (c *compiled) values(a []int) []value {
	vals := make([]value, len(a))
	for i, v := range a {
		vals[i] = c.sp.Axes[i].Values[v]
	}
	return vals
}

// rowDesc names a row by its values, with the prefix its kind earns.
func rowDesc(kind string, vals []value, axes []axis) string {
	parts := make([]string, len(vals))
	boundary, corner := false, false
	for i, v := range vals {
		parts[i] = axes[i].Name + "=" + v.Name
		boundary = boundary || v.Boundary
		corner = corner || v.Corner
	}
	prefix := "positive:"
	switch {
	case kind != "":
		prefix = kind
	case corner:
		prefix = "corner:"
	case boundary:
		prefix = "boundary:"
	}
	return prefix + " " + strings.Join(parts, " ")
}

// strategy is one group of rows and the id tag its rows carry.
type strategy struct {
	tag  string
	rows [][]int
}

// generate runs every strategy and the negatives, and returns the rows.
func generate(sp *space, opts matrixOpts) ([]row, error) {
	c, err := compile(sp)
	if err != nil {
		return nil, err
	}
	pw, _ := c.pairwise()
	groups := []strategy{
		{"ec", c.eachChoice()},
		{"pw", pw},
		{"bd", c.boundaryRows()},
		{fmt.Sprintf("rn%d", opts.Seed), c.randomRows(opts.Seed, opts.Random)},
	}
	seen := map[string]bool{}
	var pos []row
	var posVals [][]int
	for _, g := range groups {
		n := 0
		for _, a := range g.rows {
			key := fmt.Sprint(a)
			if seen[key] {
				continue
			}
			seen[key] = true
			n++
			vals := c.values(a)
			r := sp.Build(vals)
			r.ID = fmt.Sprintf("%s/%s-%03d", sp.Name, g.tag, n)
			r.Desc = rowDesc("", vals, sp.Axes)
			if exp, why := c.judge(a); exp != nil {
				r.Desc = rowDesc("negative:", vals, sp.Axes) + ": " + why
				r.Expect = *exp
			} else {
				posVals = append(posVals, a)
			}
			pos = append(pos, r)
		}
	}
	if len(pos) == 0 {
		return nil, fmt.Errorf("%s: no rows: the exclusions leave no valid assignment", sp.Name)
	}
	neg, err := c.negatives(posVals)
	if err != nil {
		return nil, err
	}
	if sp.Derive != nil {
		neg = append(neg, sp.Derive(pos)...)
	}
	return append(pos, neg...), nil
}

// negatives writes a row for each exclusion that carries an expectation
// and for each bad value, each made from the first positive row that can
// take the change.
func (c *compiled) negatives(all [][]int) ([]row, error) {
	sp := c.sp
	pos := all
	if sp.NegBase != nil {
		pos = nil
		for _, a := range all {
			if sp.NegBase(c.values(a)) {
				pos = append(pos, a)
			}
		}
		if len(pos) == 0 {
			return nil, fmt.Errorf("%s: no positive row is a base for negatives", sp.Name)
		}
	}
	var out []row
	add := func(vals []value, exp expect, why string) {
		r := sp.Build(vals)
		r.ID = fmt.Sprintf("%s/ng-%03d", sp.Name, len(out)+1)
		r.Desc = rowDesc("negative:", vals, sp.Axes) + ": " + why
		r.Expect = exp
		out = append(out, r)
	}
	for _, e := range sp.Excl {
		if e.Expect == nil {
			continue
		}
		k, err := sp.pair(e)
		if err != nil {
			return nil, err
		}
		base := c.withPair(pos, k)
		if base == nil {
			return nil, fmt.Errorf("%s: no row can hold the excluded pair %s, %s", sp.Name, e.A, e.B)
		}
		add(c.values(base), *e.Expect, e.Why)
	}
	for i := range sp.Bad {
		b := &sp.Bad[i]
		ai := slices.IndexFunc(sp.Axes, func(a axis) bool { return a.Name == b.Axis })
		vals := c.values(pos[0])
		vals[ai] = b.Value
		add(vals, b.Expect, b.Why)
	}
	return out, nil
}

// judge runs the space's Judge, if any, on assignment a.
func (c *compiled) judge(a []int) (*expect, string) {
	if c.sp.Judge == nil {
		return nil, ""
	}
	return c.sp.Judge(pickOf(c.sp.Axes, c.values(a)))
}

// pickOf maps each axis name to its value's name.
func pickOf(axes []axis, vals []value) map[string]string {
	p := make(map[string]string, len(axes))
	for i := range axes {
		p[axes[i].Name] = vals[i].Name
	}
	return p
}

// assign returns the values names picks by axis, and each other axis's
// first value.
func (sp *space) assign(names map[string]string) []value {
	vals := make([]value, len(sp.Axes))
	for i := range sp.Axes {
		vals[i] = sp.Axes[i].Values[0]
		for _, val := range sp.Axes[i].Values {
			if val.Name == names[sp.Axes[i].Name] {
				vals[i] = val
			}
		}
	}
	return vals
}

// withPair returns a copy of the first positive row that, with pair k
// forced in, breaks no exclusion but k itself and that Judge passes, so
// the pair's refusal is the only one the row can meet.
func (c *compiled) withPair(pos [][]int, k pairKey) []int {
	for _, p := range pos {
		a := slices.Clone(p)
		a[k.i], a[k.j] = k.vi, k.vj
		exp, _ := c.judge(a)
		ok := exp == nil
		for i := range a {
			for j := i + 1; j < len(a); j++ {
				if pk := (pairKey{i, a[i], j, a[j]}); pk != k && c.excl[pk] {
					ok = false
				}
			}
		}
		if ok {
			return a
		}
	}
	return nil
}

// mergeFacts unions the values' facts; a later value wins a shared key.
func mergeFacts(vals []value) map[string]string {
	f := map[string]string{}
	for _, v := range vals {
		maps.Copy(f, v.Facts)
	}
	return f
}

// joinArgs joins the non-empty spellings with single spaces.
func joinArgs(parts ...string) string {
	var b []string
	for _, p := range parts {
		if p != "" {
			b = append(b, p)
		}
	}
	return strings.Join(b, " ")
}
