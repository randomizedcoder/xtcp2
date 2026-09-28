package render

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// sp is n spaces.
//
// Every expected line below ends in trailing whitespace that belongs to
// iproute2 — eleven characters on a header, one on a value line — and typing
// it as literal spaces at the end of a Go source line is not durable: editors
// strip it on save, `git diff --check` flags it, and the test then fails for a
// reason that has nothing to do with the renderer. Writing it as sp(11) makes
// it survive and makes the count assertable by eye.
func sp(n int) string { return strings.Repeat(" ", n) }

// rxtx builds the twelve counters print_stats64 reads at show_stats == 1, in
// the order its two size_columns calls pass them (ip/ipaddress.c:721-733).
//
// The two arrays are deliberately the same shape as the two value lines of
// the output, because that is where every expected value in this file came
// from: `ip -s link show dev X` prints exactly these twelve numbers and
// nothing else, so the golden is self-describing and the inputs cannot drift
// out of step with it.
func rxtx(rx, tx [6]uint64) xtcpnl.RtnlLinkStats64 {
	return xtcpnl.RtnlLinkStats64{
		RxBytes:        rx[0],
		RxPackets:      rx[1],
		RxErrors:       rx[2],
		RxDropped:      rx[3],
		RxMissedErrors: rx[4],
		Multicast:      rx[5],

		TxBytes:         tx[0],
		TxPackets:       tx[1],
		TxErrors:        tx[2],
		TxDropped:       tx[3],
		TxCarrierErrors: tx[4],
		Collisions:      tx[5],
	}
}

// lines joins four rendered lines the way LinkStatsText emits them: newline
// between, none at the end. The closing newline is print_linkinfo's, not
// print_stats64's — the function's last statement is a print_num
// (ip/ipaddress.c:800-801) and nothing follows it.
func lines(a, b, c, d string) string { return a + "\n" + b + "\n" + c + "\n" + d }

// TestLinkStatsText checks the whole block against real `ip` output.
//
// # Where these goldens come from, and why that matters
//
// Each row is a verbatim `ip -s link show dev NAME` capture from the pinned
// iproute2 on the development host, on six interfaces chosen for the column
// widths they produce rather than for what they are. The counters fed in were
// read back OFF the golden: the six numbers on each value line are exactly
// the six the renderer consumes, so input and expectation come from one
// observation of one `ip` process and cannot disagree about a counter that
// moved between two invocations.
//
// That makes this a test of the layout rule and not of my reading of it. The
// column arithmetic in size_columns is where this code can plausibly be
// wrong, and a hand-derived expectation would be derived from the same
// misreading as the implementation.
//
// go test ./internal/goip/render/ -run TestLinkStatsText
func TestLinkStatsText(t *testing.T) {
	tests := []struct {
		description string
		s           xtcpnl.RtnlLinkStats64
		want        string
	}{
		{
			// Widths at their floor. Nothing here reaches a header word's
			// length, so cols is still {10,7,6,7,7,7,10,9} and the value
			// lines are almost entirely padding.
			description: "positive: virbr0, every column at its initial width",
			s: rxtx(
				[6]uint64{0, 0, 0, 0, 0, 0},
				[6]uint64{0, 0, 0, 1423, 0, 0}),
			want: lines(
				"    RX:  bytes packets errors dropped  missed   mcast"+sp(11),
				"             0       0      0       0       0       0"+sp(1),
				"    TX:  bytes packets errors dropped carrier collsns"+sp(11),
				"             0       0      0    1423       0       0"+sp(1)),
		},
		{
			// Still at the floor, but with a non-zero in every column that
			// has one, so a padding bug cannot hide behind a field of zeros.
			description: "positive: enp35s0f1np1, floor widths with live counters in five columns",
			s: rxtx(
				[6]uint64{3477972, 43476, 0, 43469, 0, 43476},
				[6]uint64{32075745, 148994, 0, 263, 0, 0}),
			want: lines(
				"    RX:  bytes packets errors dropped  missed   mcast"+sp(11),
				"       3477972   43476      0   43469       0   43476"+sp(1),
				"    TX:  bytes packets errors dropped carrier collsns"+sp(11),
				"      32075745  148994      0     263       0       0"+sp(1)),
		},
		{
			// An 8-digit tx_bytes against a 7-digit rx_bytes: neither
			// reaches 10, so cols[0] does not move and the two value lines
			// are padded differently from each other.
			description: "positive: docker0, asymmetric byte counts that still do not widen anything",
			s: rxtx(
				[6]uint64{6372647, 60184, 0, 0, 0, 2707},
				[6]uint64{78311596, 86458, 0, 2239, 0, 0}),
			want: lines(
				"    RX:  bytes packets errors dropped  missed   mcast"+sp(11),
				"       6372647   60184      0       0       0    2707"+sp(1),
				"    TX:  bytes packets errors dropped carrier collsns"+sp(11),
				"      78311596   86458      0    2239       0       0"+sp(1)),
		},
		{
			// The first widening: 11-digit bytes takes cols[0] from 10 to 11
			// and 8-digit packets takes cols[1] from 7 to 8, which shifts
			// the HEADER too, because the header's first field is cols[0]-4.
			description: "boundary: lo, two columns widened past their header word",
			s: rxtx(
				[6]uint64{67466214495, 58462178, 0, 0, 0, 0},
				[6]uint64{67466214495, 58462178, 0, 0, 0, 0}),
			want: lines(
				"    RX:   bytes  packets errors dropped  missed   mcast"+sp(11),
				"    67466214495 58462178      0       0       0       0"+sp(1),
				"    TX:   bytes  packets errors dropped carrier collsns"+sp(11),
				"    67466214495 58462178      0       0       0       0"+sp(1)),
		},
		{
			// Cross-direction widening, in the one direction the
			// development host can actually produce. rx_bytes is 11 digits
			// and tx_bytes is 10, yet BOTH lines are laid out at 11, so the
			// TX value carries a leading pad it would not have on its own.
			// Likewise cols[2] and cols[5] reach 16 from RX counters and the
			// TX header grows with them.
			//
			// The mirror case — TX widening the RX line — has no real
			// golden: no interface on the host sends more, in any column,
			// than it receives by enough digits to pass the floor. It is
			// covered structurally in TestLinkStatsTextColumnsAreShared
			// instead, which is what caught this row overclaiming.
			description: "boundary: enp1s0, RX values widen the TX line",
			s: rxtx(
				[6]uint64{17628926608, 16110883, 6766306042535503, 0, 0, 2541289174106274},
				[6]uint64{2945431164, 7055710, 2688297874465801, 43, 0, 0}),
			want: lines(
				"    RX:   bytes  packets           errors dropped  missed            mcast"+sp(11),
				"    17628926608 16110883 6766306042535503       0       0 2541289174106274"+sp(1),
				"    TX:   bytes  packets           errors dropped carrier          collsns"+sp(11),
				"     2945431164  7055710 2688297874465801      43       0                0"+sp(1)),
		},
		{
			// 13-digit bytes and 10-digit packets, the widest byte column in
			// the set, confirming the header's cols[0]-4 arithmetic at a
			// second width rather than only at 11.
			description: "boundary: k8stap0, a 13-digit byte column",
			s: rxtx(
				[6]uint64{5286699816631, 1923436352, 0, 0, 0, 0},
				[6]uint64{5197048714699, 4438880578, 0, 246007, 0, 0}),
			want: lines(
				"    RX:     bytes    packets errors dropped  missed   mcast"+sp(11),
				"    5286699816631 1923436352      0       0       0       0"+sp(1),
				"    TX:     bytes    packets errors dropped carrier collsns"+sp(11),
				"    5197048714699 4438880578      0  246007       0       0"+sp(1)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got := LinkStatsText(tt.s)
			if got != tt.want {
				t.Errorf("LinkStatsText mismatch\n got %q\nwant %q", got, tt.want)
			}
		})
	}
}

// TestLinkStatsTextShape covers what no interface on the development host
// could produce, so the expectations are structural rather than verbatim.
//
// A hand-typed golden for these would be derived from the same reading of
// print_stats64 as the implementation, and would pass whether or not that
// reading is right. Counting fields and measuring the trailing run is a
// weaker claim but an independent one.
//
// go test ./internal/goip/render/ -run TestLinkStatsTextShape
func TestLinkStatsTextShape(t *testing.T) {
	// The compressed column is the only conditional field in the block, and
	// its header word and its value are guarded by the same expression
	// upstream (ip/ipaddress.c:751,758 for RX, :791,800 for TX). A renderer
	// that guarded one and not the other prints a heading over nothing, or a
	// number under nothing — both of which these rows catch.
	tests := []struct {
		description string
		s           xtcpnl.RtnlLinkStats64
		// wantRxWord and wantTxWord: does that header end in "compressed"?
		wantRxWord bool
		wantTxWord bool
		// wantRxFields and wantTxFields: numbers on that value line.
		wantRxFields int
		wantTxFields int
	}{
		{
			description:  "positive: no compression anywhere is six fields and no word, as every real row above",
			s:            rxtx([6]uint64{1, 2, 3, 4, 5, 6}, [6]uint64{7, 8, 9, 10, 11, 12}),
			wantRxFields: 6,
			wantTxFields: 6,
		},
		{
			description:  "positive: rx_compressed alone adds the word and the value to RX only",
			s:            withCompressed(rxtx([6]uint64{1, 2, 3, 4, 5, 6}, [6]uint64{7, 8, 9, 10, 11, 12}), 5, 0),
			wantRxWord:   true,
			wantRxFields: 7,
			wantTxFields: 6,
		},
		{
			// The asymmetric case in the other direction. The two guards are
			// separate expressions upstream and a single shared bool here
			// would pass the row above and fail this one.
			description:  "corner: tx_compressed alone adds them to TX only, not to RX",
			s:            withCompressed(rxtx([6]uint64{1, 2, 3, 4, 5, 6}, [6]uint64{7, 8, 9, 10, 11, 12}), 0, 9),
			wantTxWord:   true,
			wantRxFields: 6,
			wantTxFields: 7,
		},
		{
			description:  "positive: both compressed counters set, both lines carry seven",
			s:            withCompressed(rxtx([6]uint64{1, 2, 3, 4, 5, 6}, [6]uint64{7, 8, 9, 10, 11, 12}), 5, 9),
			wantRxWord:   true,
			wantTxWord:   true,
			wantRxFields: 7,
			wantTxFields: 7,
		},
		{
			// Zero is not "absent" for any other column: all six print. This
			// is the control for the four rows above, which would all still
			// pass if the renderer dropped every zero.
			description:  "boundary: all-zero counters still print six fields per line",
			s:            rxtx([6]uint64{}, [6]uint64{}),
			wantRxFields: 6,
			wantTxFields: 6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got := strings.Split(LinkStatsText(tt.s), "\n")
			if len(got) != 4 {
				t.Fatalf("want 4 lines, got %d: %q", len(got), got)
			}
			checkStatsLine(t, "RX", got[0], got[1], tt.wantRxWord, tt.wantRxFields)
			checkStatsLine(t, "TX", got[2], got[3], tt.wantTxWord, tt.wantTxFields)
		})
	}
}

// withCompressed sets the two counters rxtx leaves alone.
func withCompressed(s xtcpnl.RtnlLinkStats64, rx, tx uint64) xtcpnl.RtnlLinkStats64 {
	s.RxCompressed = rx
	s.TxCompressed = tx
	return s
}

// checkStatsLine asserts the header/value pair for one direction.
func checkStatsLine(t *testing.T, dir, header, values string, wantWord bool, wantFields int) {
	t.Helper()

	if !strings.HasPrefix(header, "    "+dir+": ") {
		t.Errorf("%s header does not start with its label: %q", dir, header)
	}
	gotWord := strings.HasSuffix(header, "compressed")
	if gotWord != wantWord {
		t.Errorf("%s header ends in \"compressed\" = %v, want %v: %q",
			dir, gotWord, wantWord, header)
	}
	if !wantWord && !strings.HasSuffix(header, sp(11)) {
		// cols[6] is 10 whenever no compressed counter widened it, and the
		// separator before it is one more space.
		t.Errorf("%s header should end in 11 spaces when nothing is compressed: %q",
			dir, header)
	}

	if !strings.HasSuffix(values, " ") {
		t.Errorf("%s value line must end in print_num's trailing space: %q", dir, values)
	}
	if n := len(strings.Fields(values)); n != wantFields {
		t.Errorf("%s value line has %d numbers, want %d: %q", dir, n, wantFields, values)
	}
}

// TestStatsColsSize is size_columns in isolation, where the one behavior that
// differs from "count the digits" is visible.
//
// go test ./internal/goip/render/ -run TestStatsColsSize
func TestStatsColsSize(t *testing.T) {
	tests := []struct {
		description string
		// vals are passed to one size call, positionally.
		vals []uint64
		// want is the resulting array.
		want statsCols
	}{
		{
			description: "positive: nothing wide enough leaves every column at its header word",
			vals:        []uint64{1, 2, 3, 4, 5, 6, 7, 8},
			want:        statsCols{10, 7, 6, 7, 7, 7, 10, 9},
		},
		{
			// iproute2's loop is `for (len = 1; val > 9; ...)`, so zero is
			// one digit wide. It cannot matter at these floors, but a Go
			// implementation that counted 0 as zero digits would be a
			// latent difference waiting for a narrower column.
			description: "corner: zero counts as one digit, not none",
			vals:        []uint64{0, 0, 0, 0, 0, 0, 0, 0},
			want:        statsCols{10, 7, 6, 7, 7, 7, 10, 9},
		},
		{
			description: "boundary: 9 and 10 straddle the loop's condition",
			vals:        []uint64{9, 10, 999999, 1000000, 0, 0, 0, 0},
			want:        statsCols{10, 7, 6, 7, 7, 7, 10, 9},
		},
		{
			description: "positive: a value one digit past the floor widens that column and no other",
			vals:        []uint64{0, 12345678, 0, 0, 0, 0, 0, 0},
			want:        statsCols{10, 8, 6, 7, 7, 7, 10, 9},
		},
		{
			description: "boundary: the widest uint64 is twenty digits",
			vals:        []uint64{^uint64(0), 0, 0, 0, 0, 0, 0, 0},
			want:        statsCols{20, 7, 6, 7, 7, 7, 10, 9},
		},
		{
			// A column never shrinks: size_columns only ever takes the max,
			// so a later call with small values must not undo an earlier one.
			description: "negative: a narrow value does not shrink an already-widened column",
			vals:        []uint64{0, 0, 12345678901, 0, 0, 0, 0, 0},
			want:        statsCols{10, 7, 11, 7, 7, 7, 10, 9},
		},
		{
			// Upstream always passes exactly eight. Fewer must leave the
			// tail alone rather than zeroing or panicking.
			description: "corner: fewer than eight values leaves the remaining columns untouched",
			vals:        []uint64{12345678901, 12345678},
			want:        statsCols{11, 8, 6, 7, 7, 7, 10, 9},
		},
		{
			// And more than eight must be ignored rather than writing past
			// the array, which is the only way this function can be unsafe.
			description: "corner: more than eight values are ignored past the eighth",
			vals:        []uint64{0, 0, 0, 0, 0, 0, 0, 0, ^uint64(0), ^uint64(0)},
			want:        statsCols{10, 7, 6, 7, 7, 7, 10, 9},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got := initialStatsCols()
			got.size(tt.vals...)
			if got != tt.want {
				t.Errorf("size(%v) = %v, want %v", tt.vals, got, tt.want)
			}
		})
	}
}

// TestStatsColsSizeAccumulates is the two-call sequence print_stats64
// actually performs, which the single-call table above cannot express.
//
// Both directions contribute, and the row is built so each wins one column:
// RX takes cols[0] to 11 and TX takes cols[1] to 8. A version that dropped
// either call would pass a table where only the other direction was wide.
//
// go test ./internal/goip/render/ -run TestStatsColsSizeAccumulates
func TestStatsColsSizeAccumulates(t *testing.T) {
	got := initialStatsCols()
	got.size(17628926608, 7055710, 0, 0, 0, 0, 0, 0) // RX: widest bytes
	got.size(2945431164, 12345678, 0, 0, 0, 0, 0, 0) // TX: widest packets

	want := statsCols{11, 8, 6, 7, 7, 7, 10, 9}
	if got != want {
		t.Errorf("after RX then TX, cols = %v, want %v", got, want)
	}
}

// TestLinkStatsTextColumnsAreShared states cross-direction coupling as a
// property, which is what lets it cover the case no host interface produces.
//
// size_columns is called twice into ONE cols array (ip/ipaddress.c:721,731),
// so every column is as wide as the widest value either direction puts in
// it. The observable consequence needs no format string to express: when
// neither direction has a compressed counter, the RX and TX value lines are
// the same length, and so are the two headers. A per-direction layout makes
// them differ the moment one direction is wider.
//
// This is the assertion that failed the `vice versa` claim in
// TestLinkStatsText's enp1s0 row. Every real golden there happens to be
// RX-dominant in every column, so removing the TX size call entirely left
// all six passing.
//
// go test ./internal/goip/render/ -run TestLinkStatsTextColumnsAreShared
func TestLinkStatsTextColumnsAreShared(t *testing.T) {
	tests := []struct {
		description string
		s           xtcpnl.RtnlLinkStats64
	}{
		{
			// The case with no real golden: TX wider in the bytes column,
			// past the floor of 10, so cols[0] can only come from TX.
			description: "boundary: tx_bytes is wider than rx_bytes and past the floor",
			s: rxtx(
				[6]uint64{12345, 1, 0, 0, 0, 0},
				[6]uint64{123456789012, 1, 0, 0, 0, 0}),
		},
		{
			description: "boundary: tx_packets is wider than rx_packets and past the floor",
			s: rxtx(
				[6]uint64{0, 12, 0, 0, 0, 0},
				[6]uint64{0, 123456789, 0, 0, 0, 0}),
		},
		{
			// The mirror, so a bug that swapped the two calls is caught too.
			description: "boundary: rx_bytes is the wider one instead",
			s: rxtx(
				[6]uint64{123456789012, 1, 0, 0, 0, 0},
				[6]uint64{12345, 1, 0, 0, 0, 0}),
		},
		{
			// Column 5 holds multicast on RX and collisions on TX — two
			// unrelated counters sharing one width.
			description: "corner: multicast and collisions share column five despite being unrelated",
			s: rxtx(
				[6]uint64{0, 0, 0, 0, 0, 1},
				[6]uint64{0, 0, 0, 0, 0, 123456789}),
		},
		{
			// The control: with both directions equal the lines match
			// trivially, so the rows above are the ones doing the work.
			description: "positive: equal counters match trivially, which is why the rows above are not enough alone",
			s: rxtx(
				[6]uint64{123456789012, 1, 0, 0, 0, 0},
				[6]uint64{123456789012, 1, 0, 0, 0, 0}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got := strings.Split(LinkStatsText(tt.s), "\n")
			if len(got) != 4 {
				t.Fatalf("want 4 lines, got %d", len(got))
			}
			if len(got[0]) != len(got[2]) {
				t.Errorf("headers differ in length (%d vs %d), so the columns are not shared:\n%q\n%q",
					len(got[0]), len(got[2]), got[0], got[2])
			}
			if len(got[1]) != len(got[3]) {
				t.Errorf("value lines differ in length (%d vs %d), so the columns are not shared:\n%q\n%q",
					len(got[1]), len(got[3]), got[1], got[3])
			}
		})
	}
}

// TestLinkStatsJSONOf pins the two ways `ip -j`'s counter object differs
// from the text block, both of which are easy to get wrong by transcribing
// one branch of print_stats64 and assuming the other matches.
//
// go test ./internal/goip/render/ -run TestLinkStatsJSONOf
func TestLinkStatsJSONOf(t *testing.T) {
	tests := []struct {
		description string
		s           xtcpnl.RtnlLinkStats64
		// wantKeys are keys the marshaled "rx" object must contain.
		wantKeys []string
		// wantAbsent are keys it must not.
		wantAbsent []string
		// wantRxOver is the value under "over_errors".
		wantRxOver uint64
	}{
		{
			// The whole point of the type. The text line's fifth column is
			// rx_missed_errors; the JSON's fifth key is over_errors, a
			// different field. Only a fixture where they differ can tell a
			// correct projection from one that reuses the text field.
			description: "positive: over_errors carries rx_over_errors, NOT the rx_missed_errors the text prints",
			s: func() xtcpnl.RtnlLinkStats64 {
				s := rxtx([6]uint64{1, 2, 3, 4, 500, 6}, [6]uint64{7, 8, 9, 10, 11, 12})
				s.RxOverErrors = 700
				return s
			}(),
			wantKeys:   []string{"bytes", "packets", "errors", "dropped", "over_errors", "multicast"},
			wantAbsent: []string{"missed", "missed_errors", "compressed"},
			wantRxOver: 700,
		},
		{
			// omitempty here is a transcription of `if (s->rx_compressed)`
			// (ip/ipaddress.c:652), not a default. A zero must produce no
			// key at all rather than `"compressed": 0`.
			description: "negative: a zero compressed counter omits the key, matching iproute2's guard",
			s:           rxtx([6]uint64{1, 2, 3, 4, 5, 6}, [6]uint64{7, 8, 9, 10, 11, 12}),
			wantKeys:    []string{"bytes", "over_errors"},
			wantAbsent:  []string{"compressed"},
		},
		{
			description: "positive: a non-zero compressed counter emits the key",
			s:           withCompressed(rxtx([6]uint64{1, 2, 3, 4, 5, 6}, [6]uint64{7, 8, 9, 10, 11, 12}), 5, 0),
			wantKeys:    []string{"compressed"},
		},
		{
			// Every other counter is unconditional, so an all-zero link
			// still emits the full six keys. Without this row the two
			// omitempty rows above would also pass on a struct that had
			// omitempty everywhere.
			description: "boundary: all-zero counters still emit every unconditional key",
			s:           rxtx([6]uint64{}, [6]uint64{}),
			wantKeys:    []string{"bytes", "packets", "errors", "dropped", "over_errors", "multicast"},
			wantAbsent:  []string{"compressed"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			b, err := json.Marshal(linkStatsJSONOf(tt.s))
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			var got struct {
				RX map[string]uint64 `json:"rx"`
				TX map[string]uint64 `json:"tx"`
			}
			if uerr := json.Unmarshal(b, &got); uerr != nil {
				t.Fatalf("Unmarshal: %v", uerr)
			}
			for _, k := range tt.wantKeys {
				if _, ok := got.RX[k]; !ok {
					t.Errorf("rx is missing key %q: %s", k, b)
				}
			}
			for _, k := range tt.wantAbsent {
				if _, ok := got.RX[k]; ok {
					t.Errorf("rx has key %q that iproute2 would not emit: %s", k, b)
				}
			}
			if tt.wantRxOver != 0 && got.RX["over_errors"] != tt.wantRxOver {
				t.Errorf("rx.over_errors = %d, want %d", got.RX["over_errors"], tt.wantRxOver)
			}
		})
	}
}

// TestLinkStatsJSONTxKeys is the TX half, whose key set is not the RX one.
//
// go test ./internal/goip/render/ -run TestLinkStatsJSONTxKeys
func TestLinkStatsJSONTxKeys(t *testing.T) {
	b, err := json.Marshal(linkStatsJSONOf(
		rxtx([6]uint64{1, 2, 3, 4, 5, 6}, [6]uint64{7, 8, 9, 10, 11, 12})))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got struct {
		TX map[string]uint64 `json:"tx"`
	}
	if uerr := json.Unmarshal(b, &got); uerr != nil {
		t.Fatalf("Unmarshal: %v", uerr)
	}

	// carrier_errors and collisions where RX has over_errors and multicast,
	// so the two directions cannot share a type.
	want := map[string]uint64{
		"bytes": 7, "packets": 8, "errors": 9, "dropped": 10,
		"carrier_errors": 11, "collisions": 12,
	}
	for k, v := range want {
		if got.TX[k] != v {
			t.Errorf("tx[%q] = %d, want %d: %s", k, got.TX[k], v, b)
		}
	}
	for _, k := range []string{"over_errors", "multicast", "missed_errors"} {
		if _, ok := got.TX[k]; ok {
			t.Errorf("tx has RX-only key %q: %s", k, b)
		}
	}
}

// TestLinkViewWithStats covers the wiring: which of the three stats fields
// WithStats sets, and therefore whether a block is rendered and under which
// JSON key.
//
// go test ./internal/goip/render/ -run TestLinkViewWithStats
func TestLinkViewWithStats(t *testing.T) {
	full := rxtx([6]uint64{1, 2, 3, 4, 5, 6}, [6]uint64{7, 8, 9, 10, 11, 12})

	tests := []struct {
		description string
		li          xtcpnl.LinkInfo
		// wantText is whether Text() carries an "RX:" line.
		wantText bool
		// wantKey is the JSON key expected, or "" for neither.
		wantKey string
	}{
		{
			// The name records which attribute the kernel sent
			// (ip/ipaddress.c:836-837); the contents are the same either way.
			description: "positive: IFLA_STATS64 renders text and the stats64 key",
			li:          xtcpnl.LinkInfo{Index: 1, Name: "lo", Stats: &full, StatsIs64: true},
			wantText:    true,
			wantKey:     "stats64",
		},
		{
			description: "positive: a widened IFLA_STATS renders the same text under the stats key",
			li:          xtcpnl.LinkInfo{Index: 1, Name: "lo", Stats: &full, StatsIs64: false},
			wantText:    true,
			wantKey:     "stats",
		},
		{
			// `-s` asked, kernel sent nothing. __print_link_stats returns on
			// get_rtnl_link_stats_rta's -1 (ip/ipaddress.c:832-834), so the
			// answer is no block rather than a block of zeros.
			description: "negative: no attributes at all leaves every field clear, so nothing is printed",
			li:          xtcpnl.LinkInfo{Index: 1, Name: "lo"},
			wantText:    false,
			wantKey:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			v := LinkViewOf(tt.li, fakeNames{}).WithStats(tt.li)

			if got := strings.Contains(v.Text(), "    RX: "); got != tt.wantText {
				t.Errorf("Text() has an RX line = %v, want %v:\n%s", got, tt.wantText, v.Text())
			}

			b, err := json.Marshal(v)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			var obj map[string]json.RawMessage
			if uerr := json.Unmarshal(b, &obj); uerr != nil {
				t.Fatalf("Unmarshal: %v", uerr)
			}
			for _, k := range []string{"stats", "stats64"} {
				_, present := obj[k]
				if present != (k == tt.wantKey) {
					t.Errorf("JSON key %q present = %v, want %v: %s",
						k, present, k == tt.wantKey, b)
				}
			}
		})
	}
}

// TestLinkViewOfLeavesStatsClear is the other half of the WithStats
// contract, and the reason it is a separate step.
//
// A reply can carry IFLA_STATS64 without anyone having asked: `ip -4 addr
// show`'s link dump sends no IFLA_EXT_MASK at all, so the kernel attaches the
// counters and iproute2 prints none of them, because `do_link && show_stats`
// is false. A LinkViewOf that copied the field would print a stats block for
// `goip -4 addr show`.
//
// go test ./internal/goip/render/ -run TestLinkViewOfLeavesStatsClear
func TestLinkViewOfLeavesStatsClear(t *testing.T) {
	full := rxtx([6]uint64{1, 2, 3, 4, 5, 6}, [6]uint64{7, 8, 9, 10, 11, 12})
	li := xtcpnl.LinkInfo{Index: 1, Name: "lo", Stats: &full, StatsIs64: true}

	v := LinkViewOf(li, fakeNames{})
	if v.Stats != nil || v.JSONStats64 != nil || v.JSONStats != nil {
		t.Fatalf("LinkViewOf set a stats field without being asked: %+v", v)
	}
	if strings.Contains(v.Text(), "RX:") {
		t.Errorf("unrequested counters were rendered:\n%s", v.Text())
	}
}

// TestLinkViewTextStatsBeforeAltnames pins the one ordering decision in the
// block's placement.
//
// print_linkinfo emits the stats at ip/ipaddress.c:1297-1300 and the
// IFLA_PROP_LIST altnames at :1322-1327 — stats first. Nothing in the clean
// parity topology has an altname, so the harness cannot catch this; four of
// the eleven links in the committed 7_1_8 dump do.
//
// go test ./internal/goip/render/ -run TestLinkViewTextStatsBeforeAltnames
func TestLinkViewTextStatsBeforeAltnames(t *testing.T) {
	full := rxtx([6]uint64{1, 2, 3, 4, 5, 6}, [6]uint64{7, 8, 9, 10, 11, 12})
	li := xtcpnl.LinkInfo{
		Index:     2,
		Name:      "enp1s0",
		AltNames:  []string{"enxe04f43e628ef"},
		Stats:     &full,
		StatsIs64: true,
	}

	got := LinkViewOf(li, fakeNames{}).WithStats(li).Text()

	rx := strings.Index(got, "    RX: ")
	alt := strings.Index(got, "    altname ")
	switch {
	case rx < 0:
		t.Fatalf("no stats block:\n%s", got)
	case alt < 0:
		t.Fatalf("no altname line:\n%s", got)
	case rx > alt:
		t.Errorf("stats block follows the altname, but print_linkinfo emits it first:\n%s", got)
	}

	if !strings.HasSuffix(got, "\n") || strings.HasSuffix(got, "\n\n") {
		t.Errorf("stanza must end in exactly one newline: %q", got[len(got)-4:])
	}
}
