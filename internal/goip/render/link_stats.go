package render

import (
	"fmt"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// The `ip -s link show` / `ip stats show group link` statistics block.
//
// This file implements print_stats64 (ip/ipaddress.c:624-825). The short form
// (`show_stats == 1`) is what `ip -s link`/`ip -s addr` render; `LinkStatsText`
// is that form. The extended form (`show_stats > 1`) adds an "RX errors:" and a
// "TX errors:" line (:763-784,:805-822) and is reached by `ip -s stats show`
// (the `show` verb bumps show_stats to 2); `linkStatsText(s, true)` renders it.
// `ip -s -s link` would reach the same extended form but goip rejects `-s -s` at
// the option loop, so the link/addr callers only ever produce the short form.
// The human-readable (`-h`) and IEC (`-iec`) variants of print_num are out of
// scope: goip does not accept the options that reach them.
//
// `ip stats` calls print_stats64 with carrier_changes NULL (ip/ipstats.c:167),
// so the extended TX "transns" column is always absent here — the one extended
// field that depends on a separate link attribute.
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
	return linkStatsText(s, false)
}

// linkStatsText is print_stats64's text branch (ip/ipaddress.c:718-823). extended
// is `show_stats > 1`: it widens every column over the error counters too and
// appends the "RX errors:"/"TX errors:" lines. The block carries no leading or
// trailing newline; the caller supplies the one that opens it.
func linkStatsText(s xtcpnl.RtnlLinkStats64, extended bool) string {
	cols := initialStatsCols()

	// All size_columns calls, in iproute2's order, before anything is printed —
	// the RX header cannot be laid out until the TX values have been measured.
	// The trailing zero64 is upstream's. Under extended the two error rows widen
	// the shared columns too (:725-743); carrier_changes is NULL for stats, so
	// its sizing value is 0.
	cols.size(s.RxBytes, s.RxPackets, s.RxErrors, s.RxDropped,
		s.RxMissedErrors, s.Multicast, s.RxCompressed, 0)
	if extended {
		cols.size(0, s.RxLengthErrors, s.RxCrcErrors, s.RxFrameErrors,
			s.RxFifoErrors, s.RxOverErrors, s.RxNohandler, s.RxOtherhostDropped)
	}
	cols.size(s.TxBytes, s.TxPackets, s.TxErrors, s.TxDropped,
		s.TxCarrierErrors, s.Collisions, s.TxCompressed, 0)
	if extended {
		cols.size(0, 0, s.TxAbortedErrors, s.TxFifoErrors,
			s.TxWindowErrors, s.TxHeartbeatErrors, 0, 0)
	}

	var b strings.Builder

	// RX. The last header field is the compressed column, which prints the
	// word only when rx_compressed is non-zero and cols[6] spaces otherwise.
	writeStatsHeader(&b, cols, "RX", "missed", "mcast", s.RxCompressed != 0)
	writeStatsValues(&b, cols, s.RxBytes, s.RxPackets, s.RxErrors,
		s.RxDropped, s.RxMissedErrors, s.Multicast,
		s.RxCompressed, s.RxCompressed != 0)
	if extended {
		b.WriteString("\n")
		writeRxErrorsHeader(&b, cols, s)
		writeRxErrorsValues(&b, cols, s)
	}

	b.WriteString("\n")

	writeStatsHeader(&b, cols, "TX", "carrier", "collsns", s.TxCompressed != 0)
	writeStatsValues(&b, cols, s.TxBytes, s.TxPackets, s.TxErrors,
		s.TxDropped, s.TxCarrierErrors, s.Collisions,
		s.TxCompressed, s.TxCompressed != 0)
	if extended {
		b.WriteString("\n")
		writeTxErrorsHeader(&b, cols)
		writeTxErrorsValues(&b, cols, s)
	}

	return b.String()
}

// writeRxErrorsHeader writes the `    RX errors: …` line (ip/ipaddress.c:765-773).
// The nohandler/otherhost columns appear only when their counter is non-zero,
// each carrying its own leading space inside the field (width cols[n]+1).
func writeRxErrorsHeader(b *strings.Builder, cols statsCols, s xtcpnl.RtnlLinkStats64) {
	nohandler, otherhost := "", ""
	nw, ow := 0, 0
	if s.RxNohandler != 0 {
		nohandler, nw = " nohandler", cols[6]+1
	}
	if s.RxOtherhostDropped != 0 {
		otherhost, ow = " otherhost", cols[7]+1
	}
	fmt.Fprintf(b, "    RX errors:%*s %*s %*s %*s %*s %*s%*s%*s\n",
		cols[0]-10, "",
		cols[1], "length",
		cols[2], "crc",
		cols[3], "frame",
		cols[4], "fifo",
		cols[5], "overrun",
		nw, nohandler,
		ow, otherhost)
}

// writeRxErrorsValues writes the RX error counters (ip/ipaddress.c:774-783). The
// line opens with a cols[0]+5 blank — the error rows carry no value in the label
// column — and the nohandler/otherhost values track the header's guards.
func writeRxErrorsValues(b *strings.Builder, cols statsCols, s xtcpnl.RtnlLinkStats64) {
	fmt.Fprintf(b, "%*s", cols[0]+5, "")
	printNum(b, cols[1], s.RxLengthErrors)
	printNum(b, cols[2], s.RxCrcErrors)
	printNum(b, cols[3], s.RxFrameErrors)
	printNum(b, cols[4], s.RxFifoErrors)
	printNum(b, cols[5], s.RxOverErrors)
	if s.RxNohandler != 0 {
		printNum(b, cols[6], s.RxNohandler)
	}
	if s.RxOtherhostDropped != 0 {
		printNum(b, cols[7], s.RxOtherhostDropped)
	}
}

// writeTxErrorsHeader writes the `    TX errors: …` line (ip/ipaddress.c:807-812).
// carrier_changes is NULL for `ip stats`, so the transns column header is an
// empty cols[5]-wide field and no transns value follows.
func writeTxErrorsHeader(b *strings.Builder, cols statsCols) {
	fmt.Fprintf(b, "    TX errors:%*s %*s %*s %*s %*s %*s\n",
		cols[0]-10, "",
		cols[1], "aborted",
		cols[2], "fifo",
		cols[3], "window",
		cols[4], "heartbt",
		cols[5], "")
}

// writeTxErrorsValues writes the TX error counters (ip/ipaddress.c:814-821),
// opening with the same cols[0]+5 blank as the RX error line.
func writeTxErrorsValues(b *strings.Builder, cols statsCols, s xtcpnl.RtnlLinkStats64) {
	fmt.Fprintf(b, "%*s", cols[0]+5, "")
	printNum(b, cols[1], s.TxAbortedErrors)
	printNum(b, cols[2], s.TxFifoErrors)
	printNum(b, cols[3], s.TxWindowErrors)
	printNum(b, cols[4], s.TxHeartbeatErrors)
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
