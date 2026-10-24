package goip

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// ilaRunArgs runs `goip [-j] ila ARGS`.
func ilaRunArgs(t *testing.T, src Source, json bool, args string) genlRun {
	t.Helper()
	return genlRunArgs(t, runIla, xtcpnl.ILAGenlName, src, json, unix.AF_UNSPEC, args)
}

// TestIlaMatchesIp replays the 15 C2-T06 ila captures, the readback shows
// the write rows captured after them, and other spellings over the same
// captures. As for fou, a run that gets past the lookup sent ip's
// GETFAMILY and dump byte for byte, seq aside; every read exchange must
// have been replayed and no write, the write goip made must be the
// capture's, stdout the capture's output or the JSON given, and the exit
// status ip's, but for a write the kernel refused, which goip does not
// send.
//
// go test ./internal/goip/ -run TestIlaMatchesIp
func TestIlaMatchesIp(t *testing.T) {
	const (
		rb, rbOut = "netlink_generic_ila.pcap", "ip_ila"
		mapping   = "add loc_match 2001:db8:1:2 loc 2001:db8:3:4"
	)
	tests := []struct {
		description string
		row         string
		pcap        string // ip.pcap when empty
		out         string // ip.out when empty
		args        string
		json        bool
		stdout      string // under -j
		exit        int
	}{
		{description: "boundary: show with none", row: "ila_show_empty", args: "show"},
		{description: "positive: add", row: "ila_add", args: mapping},
		{description: "positive: add on a device, its index from ll_link_get", row: "ila_add_dev", args: mapping + " dev d0"},
		{description: "positive: both options", row: "ila_add_opts", args: mapping + " csum-mode adj-transport ident-type luid"},
		{description: "positive: the other two", row: "ila_add_auto",
			args: mapping + " csum-mode neutral-map-auto ident-type use-format"},
		{description: "negative: a mapping that exists, the same request", row: "ila_add_dup", args: mapping},
		{description: "positive: the same match on a device", row: "ila_add_same_match_dev", args: mapping + " dev d0"},
		{description: "corner: no locator, its message and a request with no attribute", row: "ila_add_no_loc",
			args: "add loc_match 2001:db8:1:2"},
		{description: "negative: an unknown word is usage, exit 255, nothing sent", row: "ila_add_unknown_word",
			args: mapping + " bogus", exit: ExitMinusOne},
		{description: "positive: del", row: "ila_del", args: "del loc_match 2001:db8:1:2"},
		{description: "negative: del of none, the same request", row: "ila_del_missing", args: "del loc_match 2001:db8:1:2"},
		{description: "positive: del on a device", row: "ila_del_dev", args: "del loc_match 2001:db8:1:2 dev d0"},
		{description: "negative: del without the device of one that has it", row: "ila_del_nodev_of_dev",
			args: "del loc_match 2001:db8:1:2"},
		{description: "positive: show, two", row: "ila_show", args: "show"},
		{description: "negative: show with an argument, exit 1, no dump", row: "ila_show_args", args: "show 2001:db8:1:2",
			exit: ExitUsage},
		{description: "positive: the readback, a mapping", row: "ila_add", pcap: rb, out: rbOut, args: "show"},
		{description: "positive: the readback, on a device", row: "ila_add_dev", pcap: rb, out: rbOut, args: "show"},
		{description: "positive: the readback, both options", row: "ila_add_opts", pcap: rb, out: rbOut, args: "show"},
		{description: "positive: the readback, the other two", row: "ila_add_auto", pcap: rb, out: rbOut, args: "show"},
		{description: "positive: the readback, two on one match", row: "ila_add_same_match_dev", pcap: rb, out: rbOut,
			args: "show"},
		{description: "corner: the readback, locator 0 to locator 0", row: "ila_add_no_loc", pcap: rb, out: rbOut,
			args: "show"},
		{description: "boundary: the readback, deleted", row: "ila_del", pcap: rb, out: rbOut, args: "show"},
		{description: "boundary: the readback, none to delete", row: "ila_del_missing", pcap: rb, out: rbOut, args: "show"},
		{description: "positive: the readback, the device's deleted", row: "ila_del_dev", pcap: rb, out: rbOut, args: "show"},
		{description: "positive: the readback, the device's left", row: "ila_del_nodev_of_dev", pcap: rb, out: rbOut,
			args: "show"},
		{description: "corner: every word by prefix, loc ahead of loc_match", row: "ila_add",
			args: "a loc_ 2001:db8:1:2 l 2001:db8:3:4"},
		{description: "corner: the words in another order are the same request", row: "ila_add",
			args: "add loc 2001:db8:3:4 loc_match 2001:db8:1:2"},
		{description: "corner: hex words strtoul reads, 0x and capitals", row: "ila_add",
			args: "add loc_match 0x2001:0db8:01:2 loc 2001:DB8:3:4"},
		{description: "corner: the last locator given is sent", row: "ila_add",
			args: "add loc_match 2001:db8:1:2 loc 1:1:1:1 loc 2001:db8:3:4"},
		{description: "corner: d is delete", row: "ila_del", args: "d loc_m 2001:db8:1:2"},
		{description: "corner: l is lst", row: "ila_show", args: "l"},
		{description: "corner: ls is lst alone, not list", row: "ila_show", args: "ls"},
		{description: "corner: list", row: "ila_show", args: "list"},
		{description: "corner: dev if3, which no link answers by name, is index 3", row: "ila_add_dev",
			args: mapping + " dev if3"},
		{description: "positive: -j show, an array of two", row: "ila_show", json: true, args: "show",
			stdout: `[{"locator_match":"2001:db8:1:2","locator":"2001:db8:3:4","interface":"*","csum_mode":"no-action",` +
				`"ident_type":"use-format"},{"locator_match":"2001:db8:5:6","locator":"2001:db8:7:8","interface":"*",` +
				`"csum_mode":"neutral-map","ident_type":"use-format"}]` + "\n"},
		{description: "boundary: -j show with none, an empty array", row: "ila_show_empty", json: true, args: "show",
			stdout: "[]\n"},
		{description: "positive: -j, the device by name", row: "ila_add_dev", pcap: rb, json: true, args: "show",
			stdout: `[{"locator_match":"2001:db8:1:2","locator":"2001:db8:3:4","interface":"d0","csum_mode":"no-action",` +
				`"ident_type":"use-format"}]` + "\n"},
		{description: "negative: -j show with an argument prints nothing", row: "ila_show_args", json: true,
			args: "show 2001:db8:1:2", exit: ExitUsage},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := genlReplay(t, tc.row, tc.pcap)
			r := ilaRunArgs(t, src, tc.json, tc.args)
			if code := ioamExit(r.err); code != tc.exit {
				t.Fatalf("exit %d (%v), want %d", code, r.err, tc.exit)
			}
			want := tc.stdout
			if !tc.json {
				outFile := tc.out
				if outFile == "" {
					outFile = "ip.out"
				}
				want = mustReadFile(t, filepath.Join(tdGenl, tc.row, outFile))
			}
			if r.stdout != want {
				t.Errorf("stdout\n got %q\nwant %q", r.stdout, want)
			}
			ioamCheckExchanges(t, src.ReplaySource, r.writes)
		})
	}
}

// TestIlaArgs runs ila_parse_opt's refusals and do_ipila's over
// ila_show_empty's capture, whose lookup each one makes: the exit status,
// a fragment of the error or of stderr, and the attributes of what was
// written, "-" for nothing. A value ila_parse_opt refuses, and a check it
// fails, still write, with no attribute.
//
// go test ./internal/goip/ -run TestIlaArgs
func TestIlaArgs(t *testing.T) {
	const (
		match = "0c000300" + "20010db800010002"
		loc   = "0c000100" + "20010db800030004"
	)
	tests := []struct {
		description string
		args        string
		exit        int
		text        string // in the error, or on stderr for a write
		attrs       string // the written request's attributes in hex, "-" for none written
	}{
		{"negative: add with no locator to match", "add loc 2001:db8:3:4", 0, "ila: missing locator0match\n", ""},
		{"negative: add with neither, the locator first", "add", 0, "ila: missing locator\n", ""},
		{"negative: a bad locator", "add loc_match 2001:db8:1:2 loc bogus", 0, "Bad locator: bogus\n", ""},
		{"negative: a bad locator to match", "del loc_match 1:2:3:4:5", 0, "Bad locator to match: 1:2:3:4:5\n", ""},
		{"negative: a checksum mode by prefix is not one", "add loc_match 1:2:3:4 loc 1:2:3:4 csum-mode adj", 0,
			"Bad csum-mode: adj\n", ""},
		{"corner: iid, compiled out of ip", "add loc_match 1:2:3:4 loc 1:2:3:4 ident-type iid", 0, "Bad ident-type: iid\n",
			""},
		{"negative: an unknown device", "add loc_match 1:2:3:4 loc 1:2:3:4 dev nosuch", 0, "No such interface: nosuch\n", ""},
		{"corner: a refused value ends the loop ahead of an unknown word", "add loc bogus foo", 0, "Bad locator: bogus\n", ""},
		{"positive: del needs no locator, and sends nothing it was not given", "del", 0, "", ""},
		{"positive: del with both locators", "del loc_match 2001:db8:1:2 loc 2001:db8:3:4", 0, "", match + loc},
		{"positive: every attribute in the parse's order, whatever the words'",
			"add csum-mode no-action ident-type luid dev d0 loc 2001:db8:3:4 loc_match 2001:db8:1:2", 0, "",
			match + loc + "08000400" + "03000000" + "05000700" + "02000000" + "05000800" + "01000000"},
		{"negative: an unknown word ahead of a bad value is usage", "add foo loc bogus", ExitMinusOne, "Usage: ip ila", "-"},
		{"negative: loc with no value", "add loc", ExitMinusOne, "not complete", "-"},
		{"negative: no verb is usage", "", ExitMinusOne, "Usage: ip ila", "-"},
		{"negative: help", "help", ExitMinusOne, "Usage: ip ila", "-"},
		{"corner: h is help", "h", ExitMinusOne, "Usage: ip ila", "-"},
		{"negative: flush, which ip does not have", "flush", ExitMinusOne, `Command "flush" is unknown`, "-"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			r := ilaRunArgs(t, genlReplay(t, "ila_show_empty", ""), false, tc.args)
			if code := ioamExit(r.err); code != tc.exit {
				t.Fatalf("exit %d (%v), want %d", code, r.err, tc.exit)
			}
			if r.err == nil && r.stderr != tc.text {
				t.Errorf("stderr %q, want %q", r.stderr, tc.text)
			}
			if r.err != nil && !strings.Contains(r.err.Error(), tc.text) {
				t.Errorf("%q does not hold %q", r.err.Error(), tc.text)
			}
			switch {
			case tc.attrs == "-" && len(r.writes) != 0:
				t.Errorf("%d writes, want none", len(r.writes))
			case tc.attrs != "-" && len(r.writes) != 1:
				t.Fatalf("%d writes, want one", len(r.writes))
			case tc.attrs != "-":
				if got := hexOf(r.writes[0].Body[4:]); got != tc.attrs {
					t.Errorf("attrs\n got %s\nwant %s", got, tc.attrs)
				}
			}
		})
	}
}

// TestIlaAddr64 is get_addr64: up to four hex words of at most 0xffff.
//
// go test ./internal/goip/ -run TestIlaAddr64
func TestIlaAddr64(t *testing.T) {
	tests := []struct {
		description string
		in          string
		expected    uint64
		ok          bool
	}{
		{description: "positive: four words", in: "2001:db8:1:2", expected: 0x20010db800010002, ok: true},
		{description: "boundary: every word 0xffff", in: "ffff:ffff:ffff:ffff", expected: 0xffffffffffffffff, ok: true},
		{description: "corner: one word, the rest 0", in: "1", expected: 0x0001000000000000, ok: true},
		{description: "corner: 0x, as strtoul base 16 takes it", in: "0x10:0:0:0", expected: 0x0010000000000000, ok: true},
		{description: "corner: leading space and -0, as strtoul takes them", in: " 1:-0:2:3", expected: 0x0001000000020003,
			ok: true},
		{description: "boundary: a word of 0x10000", in: "10000"},
		{description: "negative: a fifth word", in: "1:2:3:4:5"},
		{description: "negative: what follows the last word", in: "1:2:3:4x"},
		{description: "negative: empty", in: ""},
		{description: "negative: an empty word", in: "1::2"},
		{description: "negative: a trailing colon", in: "1:"},
		{description: "negative: -1, ULONG_MAX", in: "-1"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, ok := ilaAddr64(tc.in)
			if ok != tc.ok || got != tc.expected {
				t.Errorf("ilaAddr64(%q) = %#x, %v, want %#x, %v", tc.in, got, ok, tc.expected, tc.ok)
			}
		})
	}
}

// ilaBody is a GET answer whose attributes put builds.
func ilaBody(put func(b *xtcpnl.AttrBuilder)) []byte {
	b := xtcpnl.NewAttrBuilder(make([]byte, 128))
	put(&b)
	return append([]byte{xtcpnl.ILACmdGet, xtcpnl.ILAGenlVersion, 0, 0}, b.Bytes()...)
}

// TestIlaShow runs show over a fake family for what the captures do not
// hold: a refused dump, an answer the parse refuses, and an index no link
// answers.
//
// go test ./internal/goip/ -run TestIlaShow
func TestIlaShow(t *testing.T) {
	one := ilaBody(func(b *xtcpnl.AttrBuilder) {
		mustNil(b.PutBytes(xtcpnl.ILAAttrLocatorMatch, []byte{0, 1, 0, 2, 0, 3, 0, 4}))
	})
	dev := ilaBody(func(b *xtcpnl.AttrBuilder) { mustNil(b.PutS32(xtcpnl.ILAAttrIfindex, 7)) })
	unknown := ilaBody(func(b *xtcpnl.AttrBuilder) { mustNil(b.PutS32(xtcpnl.ILAAttrIfindex, 9)) })
	const rest = "-                   -          -          -\n"
	tests := []struct {
		description string
		src         *genlFake
		json        bool
		stdout      string
		exit        int
	}{
		{description: "negative: a refused dump prints nothing, exit 255", src: &genlFake{dumpErr: unix.EPERM},
			exit: ExitMinusOne},
		{description: "negative: under -j it closes the empty array", src: &genlFake{dumpErr: unix.EPERM}, json: true,
			stdout: "[]\n", exit: ExitMinusOne},
		{description: "negative: an answer shorter than its genlmsghdr ends the dump after the ones before",
			src: &genlFake{dump: [][]byte{one, {xtcpnl.ILACmdGet}}}, stdout: "1:2:3:4             " + rest,
			exit: ExitMinusOne},
		{description: "negative: under -j the array holds them, closed",
			src: &genlFake{dump: [][]byte{one, {xtcpnl.ILACmdGet}}}, json: true,
			stdout: `[{"locator_match":"1:2:3:4","locator":"-"}]` + "\n", exit: ExitMinusOne},
		{description: "positive: a device by its index, ll_link_get's name",
			src:    &genlFake{dump: [][]byte{dev}},
			stdout: "-                   -                   eth7            -          -\n"},
		{description: "corner: an index no link answers is ifN",
			src:    &genlFake{dump: [][]byte{unknown}},
			stdout: "-                   -                   if9             -          -\n"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			tc.src.name = xtcpnl.ILAGenlName
			r := ilaRunArgs(t, tc.src, tc.json, "show")
			if code := ioamExit(r.err); code != tc.exit {
				t.Errorf("exit %d (%v), want %d", code, r.err, tc.exit)
			}
			if r.stdout != tc.stdout {
				t.Errorf("stdout\n got %q\nwant %q", r.stdout, tc.stdout)
			}
			if len(r.writes) != 0 {
				t.Errorf("%d writes, want none", len(r.writes))
			}
		})
	}
}

// TestIlaExitCodes pins the status each of ila's errors exits with.
//
// go test ./internal/goip/ -run TestIlaExitCodes
func TestIlaExitCodes(t *testing.T) {
	tests := []struct {
		description string
		err         error
		expected    int
	}{
		{description: "positive: usage's exit(-1)", err: ErrIlaUsage, expected: 255},
		{description: "positive: show's arguments, exit 1", err: ErrIlaShowArgs, expected: 1},
		{description: "positive: Dump terminated, do_list's 1 negated", err: ErrIlaDumpTerminated, expected: 255},
		{description: "corner: NEXT_ARG's incomplete_command wrapped as usage is 255, not its 1",
			err: fmt.Errorf("%w: %w", ErrIlaUsage, incompleteErr()), expected: 255},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := exitCodeOf(tc.err); got != tc.expected {
				t.Errorf("exitCodeOf(%v) = %d, want %d", tc.err, got, tc.expected)
			}
		})
	}
}
