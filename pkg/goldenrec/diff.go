package goldenrec

import (
	"fmt"
	"net/netip"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// DefaultKeyColumns group rows for pairing: family, state and ports. The
// namespace is left out on purpose, so a container renamed between two
// captures pairs with itself and shows as one container_name difference.
var DefaultKeyColumns = []string{
	familyColumn, stateColumn,
	"inet_diag_msg_socket_source_port", "inet_diag_msg_socket_destination_port",
}

// Options tune Diff.
type Options struct {
	// Columns, when set, is a regular expression restricting the comparison
	// to matching columns (FP-12 compares only ^container_). Rows still pair
	// by KeyColumns.
	Columns string
	// KeyColumns pair rows for reporting; DefaultKeyColumns when nil.
	KeyColumns []string
	// LossyNetns name the namespaces whose sockets carry deliberate random
	// loss; in their rows the lossyColumns are not compared, the rest are.
	// DefaultLossyNetns when nil; empty for none.
	LossyNetns []string
	// FaultNetns and FaultEgress pick the rows carrying an emulated NIC's
	// injected drops: a row in one of FaultNetns, or whose destination
	// leaves by one of FaultEgress. In them neither the lossyColumns nor the
	// faultOnlyColumns are compared. DefaultFaultNetns and
	// DefaultFaultEgress when nil; empty for none.
	FaultNetns  []string
	FaultEgress []string
	// SeqNetns name the namespaces of the one-way sequence flow; in their
	// rows only the seqColumns are not compared. DefaultSeqNetns when nil;
	// empty for none.
	SeqNetns []string
}

// DefaultLossyNetns are the golden workload's netem pair
// (nix/microvms/scripts/golden-workload.sh). netem drops and reorders at
// random, so which of the lossyColumns a bulk transfer leaves nonzero, and
// the reordering it settles on, differ from run to run. The profile still
// requires them populated.
var DefaultLossyNetns = []string{"golden-bulk-a", "golden-bulk-b"}

// DefaultFaultNetns and DefaultFaultEgress are the golden workload's
// rocm-ernic echo transfers (nix/microvms/golden-ernic.nix): the servers
// in golden-ernic-b and the root namespace's clients out of ionic0. The
// emulator drops fixed frame numbers, but which segments those frames
// carry depends on how data and ACKs interleave, so the counters vary as
// netem's do. The sequence flow from golden-ernic-c (ionic2) to
// golden-ernic-d (ionic3) is one-way: every frame ionic2 sends is data, so
// each drop is one retransmit and its rows are compared.
var (
	DefaultFaultNetns  = []string{"golden-ernic-b"}
	DefaultFaultEgress = []string{"ionic0"}
)

// DefaultSeqNetns are the one-way sequence flow's namespaces
// (golden-workload.sh). Its drops and retransmits are exact, so its rows are
// compared but for the seqColumns.
var DefaultSeqNetns = []string{"golden-ernic-c", "golden-ernic-d"}

// seqColumns are the sequence rows' columns four captures showed differ.
// rwnd_limited counts time the receiver's window held the sender back: the
// server reads into io.Discard as fast as it is scheduled, so whether its
// window ever closes depends on host load (nonzero in two of four).
var seqColumns = map[string]bool{
	rwndLimitedColumn: true,
}

// faultOnlyColumns are, beyond the lossyColumns, the columns five captures
// showed differ on fault rows. Only ACKs are dropped on the servers' path,
// so a server retransmits only after an RTO, in some runs and not others;
// the clients' retransmits are checked in the VM (ernic_loss).
var faultOnlyColumns = map[string]bool{
	"tcp_info_total_retrans": true,
	"tcp_info_bytes_retrans": true,
}

// faultColumn reports whether column c is left out of a fault row's
// comparison.
func faultColumn(c string) bool { return lossyColumns[c] || faultOnlyColumns[c] }

// exemptions returns, for a row, which of its columns Diff leaves out: the
// lossyColumns in a LossyNetns row, the faultColumns in a fault row, the
// seqColumns in a sequence row.
func exemptions(opts Options) func(Row) func(string) bool {
	orDefault := func(v, d []string) []string {
		if v == nil {
			return d
		}
		return v
	}
	lossyNetns := orDefault(opts.LossyNetns, DefaultLossyNetns)
	faultNetns := orDefault(opts.FaultNetns, DefaultFaultNetns)
	faultEgress := orDefault(opts.FaultEgress, DefaultFaultEgress)
	seqNetns := orDefault(opts.SeqNetns, DefaultSeqNetns)
	none := func(string) bool { return false }
	lossy := func(c string) bool { return lossyColumns[c] }
	seq := func(c string) bool { return seqColumns[c] }
	return func(r Row) func(string) bool {
		switch {
		case slices.Contains(faultNetns, r[netnsColumn]) || slices.Contains(faultEgress, r[egressColumn]):
			return faultColumn
		case slices.Contains(lossyNetns, r[netnsColumn]):
			return lossy
		case slices.Contains(seqNetns, r[netnsColumn]):
			return seq
		}
		return none
	}
}

// lossyColumns are the columns seen to differ between captures of the
// netem rows, in strict diffs of five captures. The RTO group moves only
// when a tail loss ends in a retransmission timeout (net/ipv4/tcp_timer.c:
// tcp_write_timeout rehashes, tcp_retransmit_timer counts), which one
// capture in five showed. sk_mem_info_drops moves on a listener when netem
// drops a SYN-ACK: the retransmitted SYN reaches the request socket,
// tcp_check_req resends the SYN-ACK, and tcp_v4_rcv counts the segment as
// a drop on the listener (discard_and_relse). rqueue and the receive
// memory move when a transfer's last echoed bytes, retransmitted late, are
// still unread at the poll (one netem and one ionic row in six captures).
// Every other column of those rows, such as total_retrans or
// bytes_received, is compared.
var lossyColumns = map[string]bool{
	"tcp_info_total_rto":                 true,
	"tcp_info_total_rto_recoveries":      true,
	"tcp_info_total_rto_time":            true,
	"tcp_info_rehash":                    true,
	"tcp_info_dsack_dups":                true,
	"tcp_info_reord_seen":                true,
	rwndLimitedColumn:                    true,
	"tcp_info_probes":                    true,
	"tcp_info_delivery_rate_app_limited": true,
	"inet_diag_msg_retrans":              true,
	"dctcp_info_alpha":                   true,
	"sk_mem_info_drops":                  true,
	"inet_diag_msg_rqueue":               true,
	"sk_mem_info_rmem_alloc":             true,
	"sk_mem_info_fwd_alloc":              true,
	reorderingColumn:                     true,
}

// Difference is one way a candidate differs from the golden capture: a
// column (Golden and Candidate set), or a whole row only one side has
// (OnlyIn set).
type Difference struct {
	Key       string
	Column    string
	Golden    string
	Candidate string
	OnlyIn    string // "golden" or "candidate"
}

func (d Difference) String() string {
	if d.OnlyIn != "" {
		return fmt.Sprintf("row only in %s: %s", d.OnlyIn, d.Key)
	}
	return fmt.Sprintf("%s: %s: golden %q, candidate %q", d.Key, d.Column, d.Golden, d.Candidate)
}

// Diff compares two normalized captures. Values compare by class: an
// ordinal only by whether it is set, an address by its kind (loopback and
// unspecified exactly, else v4 or v6), an anonymous netns as one value; so
// two runs of one workload differ only where an attribute really does.
// Rows pair by key; within a key they match as a multiset.
func Diff(golden, candidate []Row, table Table, opts Options) ([]Difference, error) {
	idx, err := table.index()
	if err != nil {
		return nil, err
	}
	var filter *regexp.Regexp
	if opts.Columns != "" {
		if filter, err = regexp.Compile(opts.Columns); err != nil {
			return nil, fmt.Errorf("goldenrec: columns filter: %w", err)
		}
	}
	keyCols := opts.KeyColumns
	if keyCols == nil {
		keyCols = DefaultKeyColumns
	}
	exempt := exemptions(opts)
	cmp := func(r Row) (key string, cols []string, vals map[string]string) {
		vals = make(map[string]string, len(r))
		skip := exempt(r)
		for c, v := range r {
			if filter != nil && !filter.MatchString(c) {
				continue
			}
			if skip(c) {
				continue
			}
			vals[c] = compareForm(idx[c], v)
			cols = append(cols, c)
		}
		sort.Strings(cols)
		var k []string
		for _, c := range keyCols {
			if v, ok := r[c]; ok {
				k = append(k, c+"="+compareForm(idx[c], v))
			}
		}
		return strings.Join(k, " "), cols, vals
	}

	type entry struct {
		sig  string
		vals map[string]string
	}
	group := func(rows []Row) (map[string][]entry, []string) {
		g := make(map[string][]entry)
		var keys []string
		for _, r := range rows {
			k, cols, vals := cmp(r)
			var sig strings.Builder
			for _, c := range cols {
				sig.WriteString(c + "=" + vals[c] + "\x00")
			}
			if _, ok := g[k]; !ok {
				keys = append(keys, k)
			}
			g[k] = append(g[k], entry{sig.String(), vals})
		}
		return g, keys
	}
	gg, gkeys := group(golden)
	cg, ckeys := group(candidate)
	keys := slices.Concat(gkeys, ckeys)
	sort.Strings(keys)

	var diffs []Difference
	for i, k := range keys {
		if i > 0 && keys[i-1] == k {
			continue
		}
		gl, cl := unmatched(gg[k], cg[k], func(e entry) string { return e.sig })
		// Pair each leftover golden row with the leftover candidate row it
		// differs from least, so a changed attribute reads as one column.
		used := make([]bool, len(cl))
		for _, g := range gl {
			best, bestN := -1, 0
			for ci, c := range cl {
				if used[ci] {
					continue
				}
				if n := len(columnDiffs(k, g.vals, c.vals)); best < 0 || n < bestN {
					best, bestN = ci, n
				}
			}
			if best < 0 {
				diffs = append(diffs, Difference{Key: k, OnlyIn: "golden"})
				continue
			}
			used[best] = true
			diffs = append(diffs, columnDiffs(k, g.vals, cl[best].vals)...)
		}
		for ci := range cl {
			if !used[ci] {
				diffs = append(diffs, Difference{Key: k, OnlyIn: "candidate"})
			}
		}
	}
	return diffs, nil
}

// unmatched removes the entries a and b share (as multisets) and returns the
// rest of each, in their original order.
func unmatched[E any](a, b []E, sig func(E) string) (ra, rb []E) {
	count := make(map[string]int)
	for _, e := range b {
		count[sig(e)]++
	}
	for _, e := range a {
		if count[sig(e)] > 0 {
			count[sig(e)]--
			continue
		}
		ra = append(ra, e)
	}
	seen := make(map[string]int)
	for _, e := range a {
		seen[sig(e)]++
	}
	for _, e := range b {
		if seen[sig(e)] > 0 {
			seen[sig(e)]--
			continue
		}
		rb = append(rb, e)
	}
	return ra, rb
}

// columnDiffs lists the columns where a paired golden and candidate row
// differ.
func columnDiffs(key string, g, c map[string]string) []Difference {
	cols := make(map[string]bool)
	for k := range g {
		cols[k] = true
	}
	for k := range c {
		cols[k] = true
	}
	names := make([]string, 0, len(cols))
	for k := range cols {
		names = append(names, k)
	}
	sort.Strings(names)
	var out []Difference
	for _, col := range names {
		if g[col] != c[col] {
			out = append(out, Difference{Key: key, Column: col, Golden: g[col], Candidate: c[col]})
		}
	}
	return out
}

// compareForm is what Diff compares of a normalized value.
func compareForm(cl Class, v string) string {
	switch cl {
	case Ordinal:
		if v == "0" || v == "" || v == "-" {
			return v
		}
		return "set"
	case Netns:
		if kind, _, ok := strings.Cut(v, ":#"); ok {
			return kind
		}
		return v
	case Address:
		a, err := netip.ParseAddr(v)
		switch {
		case err != nil:
			return v // "" or invalid:N
		case a.IsLoopback() || a.IsUnspecified():
			return v
		case a.Is4():
			return "v4"
		default:
			return "v6"
		}
	default:
		return v
	}
}

// Format renders at most limit differences, one per line, then a count of
// the rest.
func Format(diffs []Difference, limit int) string {
	var b strings.Builder
	for i, d := range diffs {
		if i == limit {
			fmt.Fprintf(&b, "and %d more\n", len(diffs)-limit)
			break
		}
		b.WriteString(d.String())
		b.WriteByte('\n')
	}
	return b.String()
}
