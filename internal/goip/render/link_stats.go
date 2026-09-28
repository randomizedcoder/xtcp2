package render

import (
	"fmt"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// The `ip -s link show` statistics block.
//
// This file implements print_stats64 (ip/ipaddress.c:624-825) for
// `show_stats == 1` only. `ip -s -s` sets show_stats to 2 and adds an "RX
// errors:" and a "TX errors:" line with seven more counters each
// (:656,697,725,735,763,805); goip rejects `-s -s` at the option loop rather
// than rendering the narrow form, so those branches are deliberately absent
// instead of silently unimplemented. The human-readable (`-h`) and IEC
// (`-iec`) variants of print_num are out of scope for the same reason: goip
// does not accept the options that reach them.
//
// # Column widths are DATA-dependent, and that is the whole difficulty
//
// Every column is widened to fit the widest decimal value that will be
// printed in it (size_columns, ip/ipaddress.c:530-550), starting from the
// width of a header word:
//
//	cols[] = { len("*X errors:"), len("packets"), len("errors"),
//	           len("dropped"), len("heartbt"), len("overrun"),
//	           len("compressed"), len("otherhost") }
//	       = { 10, 7, 6, 7, 7, 7, 10, 9 }
//
// and both the RX and the TX row contribute to the same array, so a large
// tx_bytes widens the RX line too. The practical consequence is that this
// block's whitespace changes when a counter crosses a power of ten — the
// layout is a function of the traffic, not of the interface.
//
// Verified against the pinned `ip`, `ip -s link show lo | cat -A`, with a `|`
// marking column zero because gofmt reflows a doc-comment code block and
// would otherwise eat the four-space indent that is part of the output:
//
//	|    RX:   bytes  packets errors dropped  missed   mcast           $
//	|    67458617346 58458556      0       0       0       0 $
//
// Note the two pieces of trailing whitespace, both of which are iproute2's
// and neither of which is an accident of this transcription. The header's
// last field is `%*s` of "" at width cols[6] when rx_compressed is zero, so
// the line ends in eleven spaces; and print_num writes `"%*<PRIu64> "` — a
// trailing space after every number — so the value line ends in one.
const (
	// The initial column widths, named as iproute2 derives them so the
	// derivation stays checkable rather than appearing as magic numbers.
	colWidthLabel      = len("*X errors:") // 10
	colWidthPackets    = len("packets")    // 7
	colWidthErrors     = len("errors")     // 6
	colWidthDropped    = len("dropped")    // 7
	colWidthHeartbt    = len("heartbt")    // 7
	colWidthOverrun    = len("overrun")    // 7
	colWidthCompressed = len("compressed") // 10
	colWidthOtherhost  = len("otherhost")  // 9
)

// statsCols is the widened column array.
type statsCols [8]int

// initialStatsCols is the starting point, before any value is measured.
func initialStatsCols() statsCols {
	return statsCols{
		colWidthLabel, colWidthPackets, colWidthErrors, colWidthDropped,
		colWidthHeartbt, colWidthOverrun, colWidthCompressed, colWidthOtherhost,
	}
}

// size grows each column to fit its value, mirroring size_columns.
//
// The digit count is iproute2's own loop, `for (len = 1; val > 9; len++, val
// /= 10)`, which yields 1 for zero rather than 0. Using a %d-formatted
// length would agree for every value here, but not obviously so, and the
// zero case is the one that differs from a naive "count the digits".
func (c *statsCols) size(vals ...uint64) {
	for i, v := range vals {
		if i >= len(c) {
			break
		}
		n := 1
		for v > 9 {
			n++
			v /= 10
		}
		if n > c[i] {
			c[i] = n
		}
	}
}

// LinkStatsText renders the two-line RX block and two-line TX block that `ip
// -s` appends to a link stanza, without a leading or trailing newline.
//
// The caller supplies the newline that opens the block, matching
// print_linkinfo's `print_nl(); __print_link_stats(...)` at
// ip/ipaddress.c:1297-1300.
func LinkStatsText(s xtcpnl.RtnlLinkStats64) string {
	cols := initialStatsCols()

	// Both calls, in iproute2's order, and both before anything is printed —
	// the RX header cannot be laid out until the TX values have been
	// measured. The trailing zero64 argument is upstream's, sizing the
	// otherhost column against nothing at show_stats == 1.
	cols.size(s.RxBytes, s.RxPackets, s.RxErrors, s.RxDropped,
		s.RxMissedErrors, s.Multicast, s.RxCompressed, 0)
	cols.size(s.TxBytes, s.TxPackets, s.TxErrors, s.TxDropped,
		s.TxCarrierErrors, s.Collisions, s.TxCompressed, 0)

	var b strings.Builder

	// RX. The last header field is the compressed column, which prints the
	// word only when rx_compressed is non-zero and cols[6] spaces otherwise.
	writeStatsHeader(&b, cols, "RX", "missed", "mcast", s.RxCompressed != 0)
	writeStatsValues(&b, cols, s.RxBytes, s.RxPackets, s.RxErrors,
		s.RxDropped, s.RxMissedErrors, s.Multicast,
		s.RxCompressed, s.RxCompressed != 0)

	b.WriteString("\n")

	writeStatsHeader(&b, cols, "TX", "carrier", "collsns", s.TxCompressed != 0)
	writeStatsValues(&b, cols, s.TxBytes, s.TxPackets, s.TxErrors,
		s.TxDropped, s.TxCarrierErrors, s.Collisions,
		s.TxCompressed, s.TxCompressed != 0)

	return b.String()
}

// writeStatsHeader writes one `    RX: …` or `    TX: …` line and its
// newline.
//
// The first field's width is cols[0]-4, not cols[0], because the header's
// literal prefix `"    RX: "` is four characters longer than the value
// line's `"    "`. That is how the two lines stay aligned while their
// prefixes differ, and it is the one arithmetic detail here worth stating.
func writeStatsHeader(b *strings.Builder, cols statsCols, dir, c4, c5 string, compressed bool) {
	word := ""
	if compressed {
		word = "compressed"
	}
	fmt.Fprintf(b, "    %s: %*s %*s %*s %*s %*s %*s %*s\n",
		dir,
		cols[0]-4, "bytes",
		cols[1], "packets",
		cols[2], "errors",
		cols[3], "dropped",
		cols[4], c4,
		cols[5], c5,
		cols[6], word)
}

// writeStatsValues writes the numeric line, without its newline.
//
// Every number is followed by a space, print_num's own format, so the line
// ends in one. The compressed column is printed only when non-zero, matching
// the header's choice — the two conditions are the same expression upstream
// and must stay the same here or a column heading appears over nothing.
func writeStatsValues(b *strings.Builder, cols statsCols,
	bytes, packets, errors, dropped, c4, c5, compressed uint64, showCompressed bool,
) {
	b.WriteString("    ")
	printNum(b, cols[0], bytes)
	printNum(b, cols[1], packets)
	printNum(b, cols[2], errors)
	printNum(b, cols[3], dropped)
	printNum(b, cols[4], c4)
	printNum(b, cols[5], c5)
	if showCompressed {
		printNum(b, cols[6], compressed)
	}
}

// printNum is print_num (lib/utils.c:2083-2095) on its non-human-readable
// path: right-align in width, then one trailing space.
func printNum(b *strings.Builder, width int, v uint64) {
	fmt.Fprintf(b, "%*d ", width, v)
}

// LinkStatsJSON is the object `ip -s -j link show` nests in each link.
//
// # The JSON branch is not the text branch with quotes around it
//
// print_stats64 splits at `if (is_json_context())` (ip/ipaddress.c:639) and
// the two halves disagree about one counter. The text RX line's fifth column
// is headed "missed" and prints rx_missed_errors (:756); the JSON RX object's
// fifth key is "over_errors" and prints rx_over_errors (:648). They are
// different fields of rtnl_link_stats64, not two names for one, so a renderer
// that fed the text struct straight into the JSON shape would be wrong on any
// interface where the two counters differ — and silently right on the clean
// topology, where both are zero everywhere.
//
// The two directions also do not share a key set: RX carries over_errors and
// multicast where TX carries carrier_errors and collisions. Hence two types
// rather than one parameterized by direction.
type LinkStatsJSON struct {
	RX LinkStatsRxJSON `json:"rx"`
	TX LinkStatsTxJSON `json:"tx"`
}

// LinkStatsRxJSON is the "rx" object, keys in iproute2's emission order
// (ip/ipaddress.c:643-651). Order is cosmetic here — the informational diff
// sorts with `jq -S` — but following the source costs nothing and makes the
// two readable side by side.
type LinkStatsRxJSON struct {
	Bytes   uint64 `json:"bytes"`
	Packets uint64 `json:"packets"`
	Errors  uint64 `json:"errors"`
	Dropped uint64 `json:"dropped"`
	// OverErrors, not missed_errors. See the type comment.
	OverErrors uint64 `json:"over_errors"`
	Multicast  uint64 `json:"multicast"`
	// Compressed is omitempty because iproute2 guards it with
	// `if (s->rx_compressed)` (:652) rather than printing a zero. On a
	// uint64 that guard and omitempty are the same predicate exactly, so
	// this is a transcription and not the usual reflexive omitempty.
	Compressed uint64 `json:"compressed,omitempty"`
}

// LinkStatsTxJSON is the "tx" object (ip/ipaddress.c:682-691).
type LinkStatsTxJSON struct {
	Bytes         uint64 `json:"bytes"`
	Packets       uint64 `json:"packets"`
	Errors        uint64 `json:"errors"`
	Dropped       uint64 `json:"dropped"`
	CarrierErrors uint64 `json:"carrier_errors"`
	Collisions    uint64 `json:"collisions"`
	// Compressed: as on RX, the guard is `if (s->tx_compressed)` (:691).
	Compressed uint64 `json:"compressed,omitempty"`
}

// linkStatsJSONOf projects the decoded counters onto the JSON shape.
func linkStatsJSONOf(s xtcpnl.RtnlLinkStats64) LinkStatsJSON {
	return LinkStatsJSON{
		RX: LinkStatsRxJSON{
			Bytes:      s.RxBytes,
			Packets:    s.RxPackets,
			Errors:     s.RxErrors,
			Dropped:    s.RxDropped,
			OverErrors: s.RxOverErrors,
			Multicast:  s.Multicast,
			Compressed: s.RxCompressed,
		},
		TX: LinkStatsTxJSON{
			Bytes:         s.TxBytes,
			Packets:       s.TxPackets,
			Errors:        s.TxErrors,
			Dropped:       s.TxDropped,
			CarrierErrors: s.TxCarrierErrors,
			Collisions:    s.Collisions,
			Compressed:    s.TxCompressed,
		},
	}
}
