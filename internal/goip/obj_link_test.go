package goip

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// The committed capture this whole file is driven from, and its sidecar.
//
// The two came from the same `cap()` invocation in
// nix/capture-netlink-fixtures.nix, which is the property that makes a
// line-by-line expectation legitimate: the pcap holds the RTM_NEWLINK replies
// and the sidecar holds what the pinned `ip` printed from those same replies,
// on the same host, at the same moment.
const (
	linkDumpPcap    = "../../pkg/xtcpnl/testdata/7_1_8/netlink_route_getlink_dump.pcap"
	linkDumpSidecar = "../../pkg/xtcpnl/testdata/7_1_8/ip_link_n"
)

// guestDumpsDir is the in-guest corpus, which unlike 7_1_8 ships MATCHED plain
// and `-d` sidecars from the same command — the ground truth the
// reconstruction rule below is measured against by
// TestReconstructSidecarGroundTruth.
//
// Two corpora live under it: the clean `nlcapc` namespace at the top level and
// the advisory `nlcapm` mesh in mesh/. Both are used, because they exercise
// different clauses — the clean one has a link with no address at all, the mesh
// one has a stanza carrying two detail lines.
const guestDumpsDir = "../../pkg/xtcpnl/testdata/7_1_4/dumps/"

// wantLinkStanzas is how many RTM_NEWLINK replies the capture holds, and so
// how many stanzas any rendering of it must produce.
//
// Named rather than inlined because three tests below assert it from three
// different angles — text stanza count, JSON array length, and index-cache
// size — and a capture re-take that changed the host's link count should fail
// all three at one place.
const wantLinkStanzas = 11

// plainFromSidecar reconstructs what plain `ip link show` printed, from the
// `ip -d link show` sidecar the capture actually shipped.
//
// # Why a reconstruction rather than a plain sidecar
//
// nix/capture-netlink-fixtures.nix captures the *pcap* with a plain `ip` but
// writes its sidecars with `ip -d` (:120-122 vs :128-131) — they are not a
// matched pair, so there is no plain 7_1_8 sidecar to diff a plain render
// against.
//
// That WAS two problems and is now one. goip implements -d, so the `-d`
// attributes are no longer "attributes goip does not decode" and the guest
// corpus's `_n` sidecars are compared directly by
// internal/goip/goip_details_test.go. What remains is the missing plain
// sidecar, which no amount of decoding fixes: the file was never written.
// Hence the reconstruction, and hence its scope — 7_1_8 only.
//
// The in-guest capture does ship matched pairs, and 7_1_4/dumps/ holds them —
// but 7_1_8 does not and will not, since it was taken on a host that no longer
// exists in that state, and it is the corpus with the device breadth these
// tests need (11 links, a bridge, a veth pair, docker0, four altnames). So the
// reconstruction stays for 7_1_8, and what changed is that it is no longer
// self-certified: TestReconstructSidecarGroundTruth runs the same rule over
// the guest corpus and demands byte equality with the plain sidecar `ip`
// actually printed. TestPlainFromSidecar tests the clauses one at a time;
// that test proves the whole rule against output nobody wrote.
//
// # The rule, and why each clause is where the `-d` boundary actually falls
//
//   - A stanza line (`NN: name: <FLAGS> ...`) is kept verbatim. print_linkinfo
//     emits all of it before it ever consults show_details.
//   - A `    link/...` line is cut at " promiscuity". That token is the first
//     thing print_linkinfo prints inside its `if (show_details)` guard
//     (ip/ipaddress.c:1332), so everything before it is plain output and
//     everything from it onwards is not. IFLA_PROMISCUITY is present on all 11
//     links in this capture, which is what makes the cut reliable here; the
//     fallback branch keeps the line whole rather than silently dropping it.
//   - An `    altname X` line is kept verbatim. This is the clause that is easy
//     to get wrong: IFLA_PROP_LIST/IFLA_ALT_IFNAME is printed *outside* the
//     show_details guard (ip/ipaddress.c:1318-1330), so altname lines are plain
//     output even though they look like a detail. 4 of the 11 links carry one.
//   - An `    inet ` / `    inet6 ` line and its `       valid_lft ` follower
//     are kept verbatim. These do not occur in `ip_link_n` at all; they are
//     here because `ip_addr_n` is the same kind of sidecar for the same
//     reason, and print_addrinfo has no show_details branch anywhere in it
//     (ip/ipaddress.c:1500-1707) — an address line is byte-identical between
//     `ip addr show` and `ip -d addr show`. Sharing one reconstruction across
//     both commands is what keeps the two end-to-end tests honest against the
//     same rule.
//   - Everything else is dropped. In `ip_link_n` that is exactly the link-kind
//     detail lines — `bridge`, `bridge_slave`, `veth`, `nlmon` — which
//     print_linkinfo emits from inside the guard via
//     print_linktype/print_opt, and in `ip_addr_n` it is the same four.
//
// The returned slice has no trailing empty element, so len() is the stanza and
// continuation line count.
func plainFromSidecar(t *testing.T, path string) []string {
	t.Helper()
	lines, _ := reconstructSidecar(t, path)
	return lines
}

// sidecarLineNumbers returns the 1-based sidecar line number of each line
// plainFromSidecar keeps, in the same order, so an end-to-end row can cite the
// line its expectation came from.
//
// It shares reconstructSidecar with plainFromSidecar rather than classifying
// the lines again. It used to have its own copy of the switch, and the two
// drifted the moment the address clauses were added — the numbers stayed at
// the link rule's 26 while the lines went to 58 — so the single classification
// is not tidiness, it is the fix.
func sidecarLineNumbers(t *testing.T, path string) []int {
	t.Helper()
	_, nos := reconstructSidecar(t, path)
	return nos
}

// reconstructSidecar is the one implementation of the rule, returning the kept
// lines and their 1-based source line numbers together.
func reconstructSidecar(t *testing.T, path string) ([]string, []int) {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read sidecar %s: %v", path, err)
	}

	var out []string
	var nos []int
	keep := func(line string, n int) {
		out = append(out, line)
		nos = append(nos, n)
	}
	for i, line := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, "    link/"):
			if j := strings.Index(line, " promiscuity"); j >= 0 {
				keep(line[:j], i+1)
			} else {
				keep(line, i+1)
			}
		case strings.HasPrefix(line, "    altname "),
			strings.HasPrefix(line, "    inet "),
			strings.HasPrefix(line, "    inet6 "),
			strings.HasPrefix(line, "       valid_lft "):
			keep(line, i+1)
		case strings.HasPrefix(line, "    "):
			// A link-kind detail line: `-d` only.
		case line == "":
		default:
			// A stanza line. Matching on "not indented" rather than on a
			// leading integer keeps this from silently dropping a stanza if a
			// future capture holds a shape the regexp did not anticipate — it
			// would show up as a diff rather than as a missing line.
			keep(line, i+1)
		}
	}
	return out, nos
}

// TestPlainFromSidecar tests the reconstruction itself.
//
// Without this table the end-to-end test below would be comparing goip against
// a transform of my own invention: a reconstruction that dropped a line goip
// also does not print would let a missing feature pass as parity. Each row
// here is one clause of the rule, cited to the sidecar line it came from.
//
// go test ./internal/goip/ -run TestPlainFromSidecar
func TestPlainFromSidecar(t *testing.T) {
	tests := []struct {
		description string
		sidecar     string
		in          string
		want        []string
	}{
		{
			description: "positive: a stanza line is kept verbatim, trailing space and all",
			sidecar:     "ip_link_n:15",
			in:          "8: docker0: <NO-CARRIER,BROADCAST,MULTICAST,UP> mtu 1500 qdisc noqueue state DOWN mode DEFAULT group default ",
			want:        []string{"8: docker0: <NO-CARRIER,BROADCAST,MULTICAST,UP> mtu 1500 qdisc noqueue state DOWN mode DEFAULT group default "},
		},
		{
			description: "positive: a link/ line is cut at ` promiscuity`, keeping the MAC and brd",
			sidecar:     "ip_link_n:2",
			in:          "    link/loopback 00:00:00:00:00:00 brd 00:00:00:00:00:00 promiscuity 0 allmulti 0 minmtu 0",
			want:        []string{"    link/loopback 00:00:00:00:00:00 brd 00:00:00:00:00:00"},
		},
		{
			// **The clause that looks like a bug and is not.** altname is
			// printed outside print_linkinfo's show_details guard, so it
			// survives into plain output even though it is indented like a
			// detail line.
			description: "positive: an altname line survives, because IFLA_PROP_LIST is not an `ip -d` detail",
			sidecar:     "ip_link_n:5",
			in:          "    altname enxe04f43e628ef",
			want:        []string{"    altname enxe04f43e628ef"},
		},
		{
			// **The no-MAC line, and the reason the cut is on " promiscuity"
			// rather than on "promiscuity".** The sidecar has two spaces here:
			// one closing `"    link/%s "` and one opening the detail. Cutting
			// on the leading-space form leaves exactly the one trailing space
			// plain output has.
			description: "boundary: a link/ line with no address keeps its single trailing space",
			sidecar:     "ip_link_n:33",
			in:          "    link/netlink  promiscuity 0 allmulti 0 minmtu 16 maxmtu 0 ",
			want:        []string{"    link/netlink "},
		},
		{
			description: "boundary: link-netnsid precedes promiscuity, so it survives the cut",
			sidecar:     "ip_link_n:22",
			in:          "    link/ether 6e:05:d5:51:50:25 brd ff:ff:ff:ff:ff:ff link-netnsid 1 promiscuity 0 allmulti 0",
			want:        []string{"    link/ether 6e:05:d5:51:50:25 brd ff:ff:ff:ff:ff:ff link-netnsid 1"},
		},
		{
			// The four address clauses. They never fire on ip_link_n, which
			// holds no address lines at all; they are cited to ip_addr_n
			// because obj_addr_test.go drives the same function over that
			// sidecar. print_addrinfo has no show_details branch, so these
			// lines are already plain and the rule's job is to not lose them.
			description: "positive: an inet line is kept verbatim, label and all",
			sidecar:     "ip_addr_n:10",
			in:          "    inet 172.16.50.219/24 brd 172.16.50.255 scope global dynamic noprefixroute enp1s0",
			want:        []string{"    inet 172.16.50.219/24 brd 172.16.50.255 scope global dynamic noprefixroute enp1s0"},
		},
		{
			// A v6 line has no IFA_LABEL, so print_addrinfo's last field is a
			// flag token and its trailing space is the line's last character.
			// The four-space prefix makes this look like a detail line, which
			// is why it needs its own clause rather than falling through.
			description: "boundary: an inet6 line keeps the trailing space its missing label leaves",
			sidecar:     "ip_addr_n:18",
			in:          "    inet6 fe80::b5c8:b23e:9a98:a37c/64 scope link noprefixroute ",
			want:        []string{"    inet6 fe80::b5c8:b23e:9a98:a37c/64 scope link noprefixroute "},
		},
		{
			// Seven spaces, not four — print_addrinfo indents the lifetime
			// continuation deeper than print_linkinfo indents its own. The
			// clause has to precede the generic four-space drop, and this row
			// is what fails if it is reordered.
			description: "boundary: a valid_lft line is kept despite its seven-space indent",
			sidecar:     "ip_addr_n:11",
			in:          "       valid_lft 47871sec preferred_lft 47871sec",
			want:        []string{"       valid_lft 47871sec preferred_lft 47871sec"},
		},
		{
			// The `-d` reconstruction must not confuse an address family
			// keyword with the link-kind lines below. "inet" as a bare kind
			// line does not exist, but "nlmon" does, and a prefix rule keyed
			// on "inet" without the trailing space would also match a
			// hypothetical "inetfoo" detail line.
			description: "corner: an indented `inet`-like token with no trailing space is not an address line",
			in:          "    inetfoo 1 2 3",
			want:        nil,
		},
		{
			description: "negative: a bridge detail line is dropped",
			sidecar:     "ip_link_n:20",
			in:          "    bridge forward_delay 1500 hello_time 200 max_age 2000",
			want:        nil,
		},
		{
			description: "negative: a bridge_slave detail line is dropped",
			sidecar:     "ip_link_n:31",
			in:          "    bridge_slave state forwarding priority 32 cost 2",
			want:        nil,
		},
		{
			description: "negative: a bare `veth` kind line is dropped",
			sidecar:     "ip_link_n:30",
			in:          "    veth ",
			want:        nil,
		},
		{
			description: "boundary: an empty line is dropped rather than kept as an empty stanza",
			in:          "",
			want:        nil,
		},
		{
			// A link with no IFLA_PROMISCUITY would produce this. The rule
			// keeps the whole line instead of dropping it, so the failure mode
			// is a visible diff rather than a silently shortened expectation.
			description: "corner: a link/ line with no ` promiscuity` is kept whole, not dropped",
			in:          "    link/ether 02:00:00:00:00:01 brd ff:ff:ff:ff:ff:ff",
			want:        []string{"    link/ether 02:00:00:00:00:01 brd ff:ff:ff:ff:ff:ff"},
		},
		{
			// print_linkinfo would never emit this, but the rule has to be
			// total over its input, and "indented and unrecognized" is the
			// `-d` case.
			description: "corner: an indented line matching no clause is treated as a detail and dropped",
			in:          "    something_unknown 1 2 3",
			want:        nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			path := writeTempSidecar(t, tc.in)
			got := plainFromSidecar(t, path)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d lines %q, want %d %q", len(got), got, len(tc.want), tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("line %d = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// writeTempSidecar writes one line to a file so plainFromSidecar can be
// exercised on synthetic input through exactly the code path the real sidecar
// takes, rather than through an extracted inner helper that the real run would
// not use.
func writeTempSidecar(t *testing.T, line string) string {
	t.Helper()
	path := t.TempDir() + "/sidecar"
	if err := os.WriteFile(path, []byte(line+"\n"), 0o600); err != nil {
		t.Fatalf("write temp sidecar: %v", err)
	}
	return path
}

// TestReconstructSidecarGroundTruth measures the reconstruction rule against
// output `ip` actually printed, rather than against my reading of its source.
//
// # What this test is for, and why it could not be written until now
//
// TestPlainFromSidecar above tests the rule one clause at a time, on lines I
// chose, with expectations I wrote. That is a real test of each clause and no
// test at all of the *set* of clauses: a rule missing an entire category of
// line passes it, because the missing category has no row. The failure mode is
// not hypothetical — it is precisely what the two `negative` rows below catch.
//
// The in-guest capture closed the gap, because it writes both forms of every
// sidecar from the same command in the same namespace at the same moment
// (nix/microvms/scripts/capture-netlink-dumps.exp). So `-d` is the rule's
// input, plain is the answer, and nobody involved in writing this test chose
// either. Byte equality is then the whole assertion.
//
// # The negative rows are the finding, not decoration
//
// The rule is WRONG for the `-4`/`-6` forms, and this table is how that became
// known. `ip/ipaddress.c:1061` guards the entire `    link/...` line with
//
//	if (!filter.family || filter.family == AF_PACKET || show_details)
//
// so under a family filter that line is not a plain line with a `-d` tail — it
// is `-d`-only in its entirety, and the rule's " promiscuity" cut keeps a line
// that plain output does not have at all. (`ip -6 -d addr show` makes this
// visible a second way: its `link/ether` line carries neither `brd` nor
// `promiscuity`, because those sit behind the same family condition, so there
// is no cut point to find even in principle.) Two rows pin that, with the first
// divergent line named, so the limit is recorded where the rule is rather than
// in a document beside it.
//
// This is also why the reconstruction is not deleted here. It stays for 7_1_8,
// whose AF_UNSPEC `link show` / `addr show` sidecars are exactly the forms the
// four positive rows prove it handles, and the two consumers
// (TestLinkShowMatchesCapturedOutput, TestAddrShowMatchesCapturedOutput) drive
// only those. Any future consumer on a family-filtered sidecar has a failing
// row waiting for it.
//
// go test ./internal/goip/ -run TestReconstructSidecarGroundTruth
func TestReconstructSidecarGroundTruth(t *testing.T) {
	tests := []struct {
		description string
		// detail is the `ip -d` sidecar the rule transforms; plain is the
		// sidecar the same command produced without `-d`, and the answer.
		detail string
		plain  string
		// wantEqual is whether the reconstruction must reproduce plain byte
		// for byte.
		wantEqual bool
		// wantFirstDiff{Got,Want} are asserted only when wantEqual is false:
		// the first line where the two disagree. Naming it makes the row a
		// statement about where the rule's limit falls, rather than the far
		// weaker claim that something somewhere differs.
		wantFirstDiffGot  string
		wantFirstDiffWant string
		// wantLineNos, when non-nil, is the exact 1-based line number of each
		// kept line in the DETAIL file. Asserted on one row, because it is the
		// only place the citation's frame of reference is pinned: the numbers
		// have to index the `-d` file even though the lines equal the plain
		// one, and nothing else in this table would notice if they came back
		// numbered against the wrong file.
		wantLineNos []int
		// check is an extra pointed assertion on the reconstruction, for the
		// rows whose value is in one specific line rather than in the whole
		// file. It runs whether or not the equality assertion passed, so a
		// whole-file failure does not hide which clause broke.
		check func(t *testing.T, got []string)
	}{
		{
			// The clean nlcapc namespace: lo, nlmon0, goip0. Three stanzas,
			// three link/ lines, two link-kind detail lines to drop.
			description: "positive: `ip -d link show` reconstructs plain `ip link show`, clean namespace",
			detail:      guestDumpsDir + "ip_link_n",
			plain:       guestDumpsDir + "ip_link",
			wantEqual:   true,
		},
		{
			// The address clauses against ground truth for the first time.
			// print_addrinfo has no show_details branch, so every inet/inet6
			// and valid_lft line must survive untouched — including the
			// trailing space a v6 line's missing IFA_LABEL leaves.
			description: "positive: `ip -d addr show` reconstructs plain `ip addr show`, clean namespace",
			detail:      guestDumpsDir + "ip_addr_n",
			plain:       guestDumpsDir + "ip_addr",
			wantEqual:   true,
		},
		{
			// The mesh corpus is not a duplicate of the clean one: br0 carries
			// a ~2 KiB `bridge` detail line, and veth0 carries TWO detail
			// lines under one stanza (`veth ` then `bridge_slave …`). A rule
			// that dropped only the first detail line per stanza passes every
			// clean-namespace row and fails here.
			description: "positive: `ip -d link show` reconstructs plain, mesh namespace with two detail lines on one stanza",
			detail:      guestDumpsDir + "mesh/ip_link_n",
			plain:       guestDumpsDir + "mesh/ip_link",
			wantEqual:   true,
			check: func(t *testing.T, got []string) {
				for i, line := range got {
					for _, kind := range []string{"    bridge", "    veth", "    nlmon", "    dummy"} {
						if strings.HasPrefix(line, kind) {
							t.Errorf("line %d = %q kept, but %q is a link-kind detail line", i, line, kind)
						}
					}
				}
			},
		},
		{
			description: "positive: `ip -d addr show` reconstructs plain, mesh namespace",
			detail:      guestDumpsDir + "mesh/ip_addr_n",
			plain:       guestDumpsDir + "mesh/ip_addr",
			wantEqual:   true,
		},
		{
			// **The finding.** See the doc comment: under `-4` the link/ line
			// is `-d`-only in full, so the rule keeps a line plain output
			// never had, and everything after it is offset by one.
			description: "negative: the rule does NOT reconstruct `ip -4 addr show`, whose link/ line is `-d`-only (ip/ipaddress.c:1061)",
			detail:      guestDumpsDir + "ip_addr_v4_n",
			plain:       guestDumpsDir + "ip_addr_v4",
			wantEqual:   false,
			// The cut fires and produces a well-formed plain-looking line. It
			// is still wrong, which is the point: a rule can be locally
			// correct on every clause and globally wrong about the set.
			wantFirstDiffGot:  "    link/loopback 00:00:00:00:00:00 brd 00:00:00:00:00:00",
			wantFirstDiffWant: "    inet 127.0.0.1/8 scope host lo",
		},
		{
			// The `-6` form fails the same way for the same reason, and shows
			// the second half of it: `ip -6 -d` prints `link/ether <mac>` with
			// no brd and no promiscuity, so the line survives the cut whole
			// via the fallback branch. There is no token to cut on, so no
			// amount of tuning the cut fixes this family.
			description:       "negative: the rule does NOT reconstruct `ip -6 addr show`, whose `-d` link/ line has no ` promiscuity` to cut at",
			detail:            guestDumpsDir + "ip_addr_v6_n",
			plain:             guestDumpsDir + "ip_addr_v6",
			wantEqual:         false,
			wantFirstDiffGot:  "    link/loopback 00:00:00:00:00:00",
			wantFirstDiffWant: "    inet6 ::1/128 scope host proto kernel_lo ",
		},
		{
			// A projection applied twice must equal itself applied once. This
			// is the boundary case the four positive rows cannot see, because
			// they never feed the rule its own output: if any clause mangled a
			// line it was supposed to keep verbatim, the second pass would
			// mangle it again and the two would differ.
			description: "boundary: the rule is idempotent — reconstructing an already-plain sidecar is the identity",
			detail:      guestDumpsDir + "ip_link",
			plain:       guestDumpsDir + "ip_link",
			wantEqual:   true,
		},
		{
			// Idempotence on the address forms too, where it is the stronger
			// statement: the seven-space valid_lft indent and the four-space
			// inet6 indent both look like detail lines, so this is the row
			// that fails if the address clauses are ever reordered after the
			// generic four-space drop.
			description: "boundary: idempotent on the mesh address sidecar, whose deepest indent is seven spaces",
			detail:      guestDumpsDir + "mesh/ip_addr",
			plain:       guestDumpsDir + "mesh/ip_addr",
			wantEqual:   true,
		},
		{
			// nlmon0 has no IFLA_ADDRESS, so `ip` prints `"    link/%s "` and
			// stops — the plain line's last character is the space that closed
			// the format string, and the `-d` line has that space followed by
			// the one that opens " promiscuity". Cutting on the leading-space
			// form is what leaves exactly one. The positive row above already
			// covers this by byte equality; this row exists so the failure
			// names the line instead of printing a whole-file diff.
			description: "corner: a link with no address keeps exactly one trailing space after `link/netlink`",
			detail:      guestDumpsDir + "ip_link_n",
			plain:       guestDumpsDir + "ip_link",
			wantEqual:   true,
			check: func(t *testing.T, got []string) {
				const want = "    link/netlink "
				var found bool
				for _, line := range got {
					if strings.HasPrefix(line, "    link/netlink") {
						found = true
						if line != want {
							t.Errorf("nlmon0 link line = %q, want %q", line, want)
						}
					}
				}
				if !found {
					t.Error("no `    link/netlink` line in the reconstruction")
				}
			},
		},
		{
			// The line numbers travel with the lines, and an end-to-end row
			// cites them. If they were ever computed by a second walk they
			// could drift out of order or off the end of the file — which has
			// happened once already, and is why reconstructSidecar returns
			// both together.
			//
			// The mesh link sidecar is the sharpest case for this: its 15 `-d`
			// lines reduce to 10, and the five dropped ones (5, 8, 11, 14, 15)
			// are spread through the file rather than bunched at the end, so a
			// citation numbered against the 10-line plain file would be wrong
			// from its second stanza onwards and still look plausible.
			description: "corner: line numbers index the 15-line `-d` file, not the 10-line plain one",
			detail:      guestDumpsDir + "mesh/ip_link_n",
			plain:       guestDumpsDir + "mesh/ip_link",
			wantEqual:   true,
			wantLineNos: []int{1, 2, 3, 4, 6, 7, 9, 10, 12, 13},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, nos := reconstructSidecar(t, tc.detail)
			want := readSidecarLines(t, tc.plain)

			if len(got) != len(nos) {
				t.Fatalf("reconstruction returned %d lines but %d line numbers", len(got), len(nos))
			}
			// Asserted on every row rather than only on the row that names it,
			// because it costs nothing and the invariant is not row-specific.
			detailLines := len(readSidecarLines(t, tc.detail))
			for i := range nos {
				if nos[i] < 1 || nos[i] > detailLines {
					t.Errorf("line number %d = %d, outside 1..%d", i, nos[i], detailLines)
				}
				if i > 0 && nos[i] <= nos[i-1] {
					t.Errorf("line number %d = %d, not greater than the previous %d", i, nos[i], nos[i-1])
				}
			}

			if tc.wantLineNos != nil {
				if len(nos) != len(tc.wantLineNos) {
					t.Errorf("kept %d lines %v, want %d %v", len(nos), nos, len(tc.wantLineNos), tc.wantLineNos)
				} else {
					for i := range nos {
						if nos[i] != tc.wantLineNos[i] {
							t.Errorf("kept line %d came from %s:%d, want :%d (%q)",
								i, tc.detail, nos[i], tc.wantLineNos[i], got[i])
						}
					}
				}
			}

			if tc.check != nil {
				tc.check(t, got)
			}

			gotDiff, wantDiff, differs := firstDiff(got, want)
			switch {
			case tc.wantEqual && differs:
				t.Errorf("reconstruction of %s does not reproduce %s: first difference\n got: %q\nwant: %q\n(%d lines vs %d)",
					tc.detail, tc.plain, gotDiff, wantDiff, len(got), len(want))
			case !tc.wantEqual && !differs:
				t.Errorf("reconstruction of %s unexpectedly reproduces %s; the row asserting the rule's limit is now stale",
					tc.detail, tc.plain)
			case !tc.wantEqual:
				if gotDiff != tc.wantFirstDiffGot {
					t.Errorf("first divergent reconstructed line = %q, want %q", gotDiff, tc.wantFirstDiffGot)
				}
				if wantDiff != tc.wantFirstDiffWant {
					t.Errorf("first divergent plain line = %q, want %q", wantDiff, tc.wantFirstDiffWant)
				}
			}
		})
	}
}

// readSidecarLines splits a sidecar exactly the way reconstructSidecar does, so
// the comparison cannot be an artifact of two different ideas about the
// trailing newline.
func readSidecarLines(t *testing.T, path string) []string {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read sidecar %s: %v", path, err)
	}
	trimmed := strings.TrimRight(string(raw), "\n")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

// firstDiff returns the first line at which two slices disagree, and whether
// they disagree at all. A slice running out counts as a difference, reported as
// an empty string on that side, so a truncation is located rather than just
// counted.
func firstDiff(got, want []string) (string, string, bool) {
	at := func(s []string, i int) string {
		if i < len(s) {
			return s[i]
		}
		return ""
	}
	n := max(len(got), len(want))
	for i := range n {
		if at(got, i) != at(want, i) {
			return at(got, i), at(want, i), true
		}
	}
	return "", "", false
}

// TestLinkShowMatchesCapturedOutput is §8.7's headline assertion: every line
// `ip` printed, reproduced from the same replies, with no socket and no root.
//
// # The table is derived from the fixture, not transcribed from it
//
// The rows are the reconstructed sidecar lines themselves, one subtest each,
// named with the `ip_link_n` line they came from. That is deliberate and it is
// the stronger form: a hand-written expectation can be quietly edited to match
// whatever the code does today, whereas these rows cannot be edited at all
// without editing the committed capture. It also means a new attribute landing
// in the renderer fails at the one stanza it changed rather than in a wall of
// diff.
//
// The stanza count is asserted separately from the lines, because "goip
// rendered 10 of 11 links, each perfectly" and "goip rendered 11 links, one
// wrongly" are different bugs and a single whole-output comparison reports
// them identically.
//
// go test ./internal/goip/ -run TestLinkShowMatchesCapturedOutput
func TestLinkShowMatchesCapturedOutput(t *testing.T) {
	want := plainFromSidecar(t, linkDumpSidecar)

	var stdout, stderr bytes.Buffer
	t.Setenv("GOIP_REPLAY", linkDumpPcap)
	if code := Run([]string{"link", "show"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("Run = %d, want %d; stderr: %s", code, ExitOK, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr not empty: %q", stderr.String())
	}

	got := strings.Split(strings.TrimRight(stdout.String(), "\n"), "\n")

	t.Run("positive: the output has one stanza per captured RTM_NEWLINK reply", func(t *testing.T) {
		stanzas := 0
		for _, line := range got {
			if !strings.HasPrefix(line, " ") {
				stanzas++
			}
		}
		if stanzas != wantLinkStanzas {
			t.Errorf("rendered %d stanzas, want %d", stanzas, wantLinkStanzas)
		}
	})

	t.Run("positive: the output has the same line count as the reconstructed sidecar", func(t *testing.T) {
		if len(got) != len(want) {
			t.Errorf("rendered %d lines, want %d", len(got), len(want))
		}
	})

	// Sidecar line numbers are recovered by walking the same clauses
	// plainFromSidecar uses, so each row can cite the line it came from — the
	// convention xtcpnl_rtnetlink_realfixtures_test.go:26-40 set.
	lineNos := sidecarLineNumbers(t, linkDumpSidecar)

	for i, wantLine := range want {
		cite := "unknown"
		if i < len(lineNos) {
			cite = fmt.Sprintf("ip_link_n:%d", lineNos[i])
		}
		// The class prefix is "positive" for all of these: every row is a real
		// captured reply rendered against real captured output. The negative,
		// boundary and corner classes for this renderer live in
		// render/link_test.go, which can construct the shapes this host does
		// not have.
		t.Run(fmt.Sprintf("positive: output line %d matches %s", i+1, cite), func(t *testing.T) {
			if i >= len(got) {
				t.Fatalf("missing output line; want %q", wantLine)
			}
			if got[i] != wantLine {
				t.Errorf("line %d mismatch\n got: %q\nwant: %q", i+1, got[i], wantLine)
			}
		})
	}
}

// stubSource is a Source that returns canned bodies, for the shapes a real
// capture cannot provide — an empty dump, and a failing one.
type stubSource struct {
	bodies [][]byte
	err    error
}

func (s stubSource) Dump(_ []byte, _ uint16) ([][]byte, error) {
	return s.bodies, s.err
}

var errStubDump = errors.New("stub dump failure")

// TestLinkShowSourceOutcomes covers what linkShow does with reply sets a
// committed capture cannot hold.
//
// The empty-dump row is the plan's §8.7 "zero links → empty output, exit 0",
// and it needs a stub rather than a pcap: ReplaySource treats "no recorded
// reply of this type" as ErrNoReplay, i.e. a missing fixture, because for a
// replay that is what it means. A live socket answering a dump with zero links
// is a different thing — an empty answer, not a broken one — and this is where
// that distinction is pinned.
//
// go test ./internal/goip/ -run TestLinkShowSourceOutcomes
func TestLinkShowSourceOutcomes(t *testing.T) {
	realBodies := capturedLinkBodies(t)

	tests := []struct {
		description string
		src         Source
		wantOut     string
		wantErr     error
	}{
		{
			description: "positive: the captured replies render the full listing",
			src:         stubSource{bodies: realBodies},
			// The content is asserted line by line by the test above; here the
			// only claim is that a non-empty dump produces non-empty output,
			// which is what makes the empty row below meaningful.
			wantOut: "1: lo: ",
		},
		{
			description: "boundary: a dump with zero replies renders nothing and succeeds",
			src:         stubSource{bodies: nil},
			wantOut:     "",
		},
		{
			description: "boundary: a dump with one reply renders exactly one stanza",
			src:         stubSource{bodies: realBodies[:1]},
			wantOut:     "1: lo: ",
		},
		{
			description: "negative: a source error propagates and is not rendered as an empty listing",
			src:         stubSource{err: errStubDump},
			wantErr:     errStubDump,
		},
		{
			// A body too short to hold an ifinfomsg. The decoder must report
			// it rather than render a partial stanza, because a half-rendered
			// line is indistinguishable from a real one in a parity diff.
			description: "corner: an undecodable reply body is an error, not a partial stanza",
			src:         stubSource{bodies: [][]byte{{0x00, 0x11}}},
			wantErr:     nil, // any non-nil error; checked below
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var out bytes.Buffer
			c := &runCtx{src: tc.src, lltab: NewLLTab(), out: &out, errOut: &out}
			err := linkShow(c, nil)

			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if strings.HasPrefix(tc.description, "corner:") {
				if err == nil {
					t.Fatalf("err = nil, want a decode error; output was %q", out.String())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantOut == "" {
				if out.Len() != 0 {
					t.Errorf("output = %q, want empty", out.String())
				}
				return
			}
			if !strings.HasPrefix(out.String(), tc.wantOut) {
				t.Errorf("output = %q, want prefix %q", out.String(), tc.wantOut)
			}
		})
	}
}

// capturedLinkBodies pulls the RTM_NEWLINK reply bodies out of the committed
// capture, so the stub rows above are driven by real replies too — only the
// *set* is synthetic, never the bytes.
func capturedLinkBodies(t *testing.T) [][]byte {
	t.Helper()

	src, err := OpenReplay(linkDumpPcap)
	if err != nil {
		t.Fatalf("open replay: %v", err)
	}
	bodies, err := src.Dump(nil, 16) // RTM_NEWLINK
	if err != nil {
		t.Fatalf("replay dump: %v", err)
	}
	if len(bodies) != wantLinkStanzas {
		t.Fatalf("capture holds %d RTM_NEWLINK replies, want %d", len(bodies), wantLinkStanzas)
	}
	return bodies
}

// TestRunLinkArgs covers the CLI surface around the renderer: the verbs, the
// abbreviations, and the arguments goip refuses rather than ignores.
//
// go test ./internal/goip/ -run TestRunLinkArgs
func TestRunLinkArgs(t *testing.T) {
	tests := []struct {
		description string
		args        []string
		wantCode    int
		// wantStdoutPrefix is checked only when non-empty.
		wantStdoutPrefix string
		wantStderrSubstr string
	}{
		{
			description: "positive: `link show` renders",
			args:        []string{"link", "show"}, wantCode: ExitOK,
			wantStdoutPrefix: "1: lo: ",
		},
		{
			// do_iplink falls through to ipaddr_list_link when argc is 0, so a
			// bare object is a show. Getting this wrong would make `goip link`
			// a usage error where `ip link` lists.
			description: "positive: a bare `link` is a show, matching do_iplink's argc==0 fallthrough",
			args:        []string{"link"}, wantCode: ExitOK,
			wantStdoutPrefix: "1: lo: ",
		},
		{
			description: "positive: `l s` abbreviates both the object and the verb",
			args:        []string{"l", "s"}, wantCode: ExitOK,
			wantStdoutPrefix: "1: lo: ",
		},
		{
			// All three verbs are in iplink.c's matches() chain, and "lst" is
			// the one nobody remembers.
			description: "positive: `link lst` is a show",
			args:        []string{"link", "lst"}, wantCode: ExitOK,
			wantStdoutPrefix: "1: lo: ",
		},
		{
			description: "positive: `link list` is a show",
			args:        []string{"link", "list"}, wantCode: ExitOK,
			wantStdoutPrefix: "1: lo: ",
		},
		{
			// `link show` overrides preferred_family to AF_PACKET, so -4 must
			// not change the listing. The request-side half of this claim is
			// asserted byte-for-byte by req's Tier A table; here it is the
			// user-visible consequence.
			description: "positive: -4 does not change `link show`, because ipaddr_list_link forces AF_PACKET",
			args:        []string{"-4", "link", "show"}, wantCode: ExitOK,
			wantStdoutPrefix: "1: lo: ",
		},
		{
			// Replay implements a single-get as dump plus exact filtering. The
			// live source uses RTM_GETLINK without NLM_F_DUMP.
			description: "positive: `link show dev lo` selects exactly one link",
			args:        []string{"link", "show", "dev", "lo"}, wantCode: ExitOK,
			wantStdoutPrefix: "1: lo: ",
		},
		{
			description: "negative: an unknown link verb is refused",
			args:        []string{"link", "frobnicate"}, wantCode: ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			description: "negative: an unknown object is a usage error naming the object",
			args:        []string{"zzz", "show"}, wantCode: ExitUsage,
			wantStderrSubstr: `Object "zzz" is unknown`,
		},
		{
			description: "negative: no arguments at all prints usage and fails",
			args:        []string{}, wantCode: ExitUsage,
			wantStderrSubstr: "Usage: goip",
		},
		{
			description: "boundary: -h prints usage to stdout and succeeds",
			args:        []string{"-h"}, wantCode: ExitOK,
			wantStdoutPrefix: "Usage: goip",
		},
		{
			description: "negative: an unknown option is refused before any object lookup",
			args:        []string{"-z", "link", "show"}, wantCode: ExitUsage,
			wantStderrSubstr: `Option "-z" is unknown`,
		},
		{
			// Options come before the object in iproute2, so this is an object
			// named "-4", which is not an object. Go's flag package would
			// accept it, which is precisely why the parsing is hand-written.
			description: "corner: an option after the object is not an option",
			args:        []string{"link", "show", "-4"}, wantCode: ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			// `link` is reached by "l" before "l2tp" is considered, so no
			// argument can ever name l2tp. Asserting it here rather than only
			// in the dispatch table keeps the claim attached to a real run.
			description: "corner: `l2tp` is unreachable, because `link` matches its prefix first",
			args:        []string{"l2tp", "show"}, wantCode: ExitUsage,
			wantStderrSubstr: "not implemented",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Setenv("GOIP_REPLAY", linkDumpPcap)
			var stdout, stderr bytes.Buffer
			code := Run(tc.args, &stdout, &stderr)
			if code != tc.wantCode {
				t.Errorf("Run(%q) = %d, want %d; stderr: %s", tc.args, code, tc.wantCode, stderr.String())
			}
			if tc.wantStdoutPrefix != "" && !strings.HasPrefix(stdout.String(), tc.wantStdoutPrefix) {
				t.Errorf("stdout = %q, want prefix %q", stdout.String(), tc.wantStdoutPrefix)
			}
			if tc.wantStderrSubstr != "" && !strings.Contains(stderr.String(), tc.wantStderrSubstr) {
				t.Errorf("stderr = %q, want to contain %q", stderr.String(), tc.wantStderrSubstr)
			}
		})
	}
}

// TestLinkShowJSONMatchesCapturedSidecars compares `goip -json link show`
// against the `ip -j -p link show` sidecar captured from the same guest, in
// the same namespace, at the same moment as the pcap it replays.
//
// # Why this did not exist until now, and what it found
//
// dumps/ip_link_json and dumps/mesh/ip_link_json have been committed since
// the in-guest capture was written (capture-netlink-dumps.exp:206) and
// nothing read either of them, while the identical pattern was already wired
// up for route and neigh. They were free ground truth sitting unused, and
// wiring the ADDRESS half of the same pair immediately failed: `ip` emits one
// boolean key per ifa flag and goip was emitting a "flags" array. See
// TestAddrShowJSONMatchesCapturedSidecars and render/addr.go's MarshalJSON.
// The link half, below, passed unchanged — which is the outcome that makes
// the address finding worth trusting rather than a sign the comparison is
// weak.
//
// # The stats rows, and why their counters are zeroed first
//
// ip_link_stats_json is `ip -j -p -s link show`, and its counters are LIVE:
// the sidecar runs a moment after the pcap is captured, on a guest whose
// nlmon interface is carrying the capture itself, so rx_packets has moved on
// by the time `ip` prints. Measured, not assumed — on the clean topology the
// sidecar says 153 packets and the replay renders 77. Comparing the numbers
// would test the scheduler. Comparing everything else tests what the fixture
// can actually pin: that `stats64` is the key `ip` chose, that the nesting is
// rx/tx objects, and that the member set matches exactly.
//
// The values are not left untested, they are tested where they can be: the
// decode against the same pcap is pkg/xtcpnl's TestLinkStatsRealFixtures, and
// the rendering of given counters is render/link_stats_test.go. What only
// this test can reach is the key names, and those are exactly what a sidecar
// captured seconds late still states correctly.
//
// go test ./internal/goip/ -run TestLinkShowJSONMatchesCapturedSidecars
func TestLinkShowJSONMatchesCapturedSidecars(t *testing.T) {
	tests := []struct {
		description string
		pcap        string
		args        []string
		sidecar     string
		// zeroCounters blanks every number under the stats/stats64 keys in
		// both documents before comparing, leaving the key set and the
		// nesting asserted and the live values not.
		zeroCounters bool
	}{
		{
			description: "positive: the clean topology reproduces ip_link_json key for key",
			pcap:        guestDumpsDir + "netlink_route_getlink.pcap",
			args:        []string{"-json", "link", "show"},
			sidecar:     "ip_link_json",
		},
		{
			// The mesh namespace is where IFLA_MASTER, a local IFLA_LINK pair
			// and IFLA_INFO_KIND live, so it is not a repeat of the row above
			// even though the assertion is the same shape.
			description: "positive: the mesh topology reproduces mesh/ip_link_json, bridge and veth included",
			pcap:        guestDumpsDir + "mesh/netlink_route_getlink.pcap",
			args:        []string{"-json", "link", "show"},
			sidecar:     "mesh/ip_link_json",
		},
		{
			// The tunnel namespace is the only source of two JSON shapes
			// neither row above can produce:
			//
			//   "link": null       IFLA_LINK present with value 0, which is
			//                      print_null at lib/utils.c:1333-1334 and
			//                      not an absent key. Every device here has
			//                      it, because a tunnel sits on nothing.
			//   "address": an IP   ll_addr_n2a renders a 4-byte address on
			//                      ARPHRD_TUNNEL/SIT/IPGRE and a 16-byte one
			//                      on ARPHRD_TUNNEL6/IP6GRE as an IP, not as
			//                      colon-hex (lib/ll_addr.c:32-38).
			//
			// The permaddr keys on the v6 devices are random per boot, and
			// they still compare exactly here: the pcap and the sidecar come
			// from the SAME boot of the SAME capture, so they agree with each
			// other even though neither survives a re-capture unchanged. That
			// is a property of this pair, not of the value — see
			// pkg/xtcpnl's tdDumpsTunnel_7_1_4.
			description: "positive: the tunnel topology reproduces tunnel/ip_link_json, null link and IP-valued addresses included",
			pcap:        guestDumpsDir + "tunnel/netlink_route_getlink.pcap",
			args:        []string{"-json", "link", "show"},
			sidecar:     "tunnel/ip_link_json",
		},
		{
			description:  "positive: `-s` adds the stats64 object, with ip's key names and nesting",
			pcap:         guestDumpsDir + "netlink_route_getlink_stats.pcap",
			args:         []string{"-json", "-s", "link", "show"},
			sidecar:      "ip_link_stats_json",
			zeroCounters: true,
		},
		{
			description:  "positive: the same on the mesh topology, where four more links carry counters",
			pcap:         guestDumpsDir + "mesh/netlink_route_getlink_stats.pcap",
			args:         []string{"-json", "-s", "link", "show"},
			sidecar:      "mesh/ip_link_stats_json",
			zeroCounters: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			want, err := os.ReadFile(guestDumpsDir + tc.sidecar)
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("GOIP_REPLAY", tc.pcap)
			var stdout, stderr bytes.Buffer
			if code := Run(tc.args, &stdout, &stderr); code != ExitOK {
				t.Fatalf("Run(%q) = %d, stderr=%s", tc.args, code, stderr.String())
			}
			got := stdout.Bytes()
			if tc.zeroCounters {
				got, want = zeroLinkStats(t, got), zeroLinkStats(t, want)
			}
			assertJSONEntriesEqual(t, got, want, false)
		})
	}
}

// TestLinkShowTextMatchesCapturedSidecars diffs `goip link show`'s TEXT output
// against the plain `ip link show` sidecar from the same capture, line by line.
//
// # Why a text test when the JSON one already exists
//
// Two of the three things this increment added are invisible to JSON:
//
//   - `@NONE`. In JSON the IFLA_LINK = 0 arm is `"link": null`; the literal
//     token only exists in the text form, where print_name_and_link splices
//     it into the ifname (lib/utils.c:1336-1341). The JSON row can assert
//     that the arm was taken; only this one can assert what it prints.
//   - The ` permaddr …` prefix. print_string(PRINT_FP, NULL, " permaddr ",
//     NULL) at ip/ipaddress.c:1101 is FP-only — the JSON half is a separate
//     print_color_string with no leading space. Its POSITION on the line,
//     after brd/peer and inside the same `if` that opens the link/ line, is a
//     text-only fact.
//
// # Why the clean and mesh rows are here too
//
// They are controls, not coverage. If the tunnel row is the only row and it
// fails, the failure is ambiguous between "the tunnel work is wrong" and
// "goip's plain text form has always differed from `ip`'s". With all three,
// a tunnel-only failure is attributable and a three-way failure is a finding
// about something else.
//
// go test ./internal/goip/ -run TestLinkShowTextMatchesCapturedSidecars
func TestLinkShowTextMatchesCapturedSidecars(t *testing.T) {
	tests := []struct {
		description string
		pcap        string
		sidecar     string
	}{
		{
			description: "control: the clean topology reproduces ip_link line for line",
			pcap:        guestDumpsDir + "netlink_route_getlink.pcap",
			sidecar:     "ip_link",
		},
		{
			description: "control: the mesh topology reproduces mesh/ip_link line for line",
			pcap:        guestDumpsDir + "mesh/netlink_route_getlink.pcap",
			sidecar:     "mesh/ip_link",
		},
		{
			description: "positive: the tunnel topology reproduces tunnel/ip_link, @NONE and permaddr included",
			pcap:        guestDumpsDir + "tunnel/netlink_route_getlink.pcap",
			sidecar:     "tunnel/ip_link",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			raw, err := os.ReadFile(guestDumpsDir + tc.sidecar)
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("GOIP_REPLAY", tc.pcap)
			var stdout, stderr bytes.Buffer
			args := []string{"link", "show"}
			if code := Run(args, &stdout, &stderr); code != ExitOK {
				t.Fatalf("Run(%q) = %d, stderr=%s", args, code, stderr.String())
			}
			assertLinesEqual(t, stdout.String(), string(raw))
		})
	}
}

// TestLinkShowStatsTextMatchesCapturedSidecars diffs `goip -s link show`'s TEXT
// output against the plain `ip -s link show` sidecar, with the counter values
// normalized away and everything else compared exactly.
//
// # Why this is not already covered
//
// The `-s` render is compared byte-for-byte inside the parity microVM, where
// `ip` and `goip` run milliseconds apart in one boot. That comparison is
// stronger than this one and it needs /dev/kvm, so on a plain `go test` — and
// therefore in every CI path that is not the KVM integration run — nothing
// checks print_stats64's transcription at all. The JSON sibling
// (TestLinkShowJSONMatchesCapturedSidecars, zeroCounters) covers the key names
// and nesting; it cannot cover a column header or a field order, because JSON
// has neither.
//
// # Why normalization is required, and what survives it
//
// The sidecar and the pcap come from two different invocations of the capture
// driver, so lo and goip0 moved traffic in between: the committed goldens and
// a replay of the committed pcap differ on exactly two lines out of thirty-two,
// and only in the counter values (65796/149 against 31596/77). Everything else
// — every link header, every link/ line, and both
//
//	RX:  bytes packets errors dropped  missed   mcast
//	TX:  bytes packets errors dropped carrier collsns
//
// header rows, trailing spaces included — is already identical. Those two rows
// are format-string constants transcribed from ip/ipaddress.c, and they are the
// thing worth pinning; the counters are not, and cannot be.
//
// So normalizeLinkStatsText rewrites ONLY the value line that follows an RX:/TX:
// header, and only into a field count. A column header that lost a column, a
// swapped field order, a dropped stanza, a wrong indent, or a missing trailing
// space all still fail. A counter that advanced does not.
//
// go test ./internal/goip/ -run TestLinkShowStatsTextMatchesCapturedSidecars
func TestLinkShowStatsTextMatchesCapturedSidecars(t *testing.T) {
	tests := []struct {
		description string
		pcap        string
		sidecar     string
	}{
		{
			description: "control: the clean topology reproduces ip_link_stats, headers and stanza layout",
			pcap:        guestDumpsDir + "netlink_route_getlink_stats.pcap",
			sidecar:     "ip_link_stats",
		},
		{
			description: "control: the mesh topology reproduces mesh/ip_link_stats across its extra links",
			pcap:        guestDumpsDir + "mesh/netlink_route_getlink_stats.pcap",
			sidecar:     "mesh/ip_link_stats",
		},
		{
			// The stats stanza has to sit under a header line carrying @NONE
			// and, on the v6 devices, a permaddr token. This is the only row
			// where the two renders meet.
			description: "positive: the tunnel topology reproduces tunnel/ip_link_stats under @NONE headers",
			pcap:        guestDumpsDir + "tunnel/netlink_route_getlink_stats.pcap",
			sidecar:     "tunnel/ip_link_stats",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			raw, err := os.ReadFile(guestDumpsDir + tc.sidecar)
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("GOIP_REPLAY", tc.pcap)
			var stdout, stderr bytes.Buffer
			args := []string{"-s", "link", "show"}
			if code := Run(args, &stdout, &stderr); code != ExitOK {
				t.Fatalf("Run(%q) = %d, stderr=%s", args, code, stderr.String())
			}
			assertLinesEqual(t,
				normalizeLinkStatsText(t, stdout.String()),
				normalizeLinkStatsText(t, string(raw)))
		})
	}
}

// normalizeLinkStatsText replaces the counter values under each RX:/TX: header
// with their field count, leaving every other line byte-for-byte.
//
// It asserts on the way through that the line it is about to erase really was
// all digits. Without that, a render which printed a word where a number
// belongs would be normalized into agreement — the normalizer would be hiding
// the bug it exists to tolerate.
func normalizeLinkStatsText(t *testing.T, s string) string {
	t.Helper()

	lines := strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	inStats := false
	for i, line := range lines {
		if !inStats {
			trimmed := strings.TrimSpace(line)
			inStats = strings.HasPrefix(trimmed, "RX:") || strings.HasPrefix(trimmed, "TX:")
			continue
		}
		inStats = false

		fields := strings.Fields(line)
		for _, f := range fields {
			if strings.TrimLeft(f, "0123456789") != "" {
				t.Fatalf("line %d: %q is not a counter, and this line follows an RX:/TX: header; "+
					"normalizing it would hide a render bug", i+1, f)
			}
		}
		lines[i] = "<" + strconv.Itoa(len(fields)) + " counters>"
	}
	return strings.Join(lines, "\n") + "\n"
}

// TestNormalizeLinkStatsText checks the normalizer against the golden it is
// applied to, because a normalizer is the one helper whose failure mode is
// silent: if it erased too much, every row of
// TestLinkShowStatsTextMatchesCapturedSidecars would still pass and would mean
// nothing.
//
// Each row mutates the real dumps/ip_link_stats and states whether the
// mutation must survive normalization. The counter row is the only `false`,
// and it is the entire reason the helper exists.
//
// go test ./internal/goip/ -run TestNormalizeLinkStatsText
func TestNormalizeLinkStatsText(t *testing.T) {
	raw, err := os.ReadFile(guestDumpsDir + "ip_link_stats")
	if err != nil {
		t.Fatal(err)
	}
	counterFrom, counterTo := advancedCounterLine(t, string(raw))

	tests := []struct {
		description string
		from, to    string
		wantDetect  bool
	}{
		{
			// The only row whose mutation is DERIVED from the golden rather
			// than written out, because it is the only one about counter
			// VALUES rather than about the format around them. It used to
			// carry a literal `         65796     149`, which meant
			// "whatever nlmon0 had received when this golden was captured" —
			// so re-capturing ip_link_stats made the mutation stop applying
			// and the row failed with the shape complaint below, which says
			// nothing about counters.
			//
			// advancedCounterLine picks a real counter line and rotates its
			// digits, which is the mutation this row always meant: same
			// columns, same widths, same field count, different numbers.
			description: "negative: a counter that advanced between the sidecar and the pcap is tolerated",
			from:        counterFrom,
			to:          counterTo,
			wantDetect:  false,
		},
		{
			description: "positive: a dropped column in the RX header is detected",
			from:        "errors dropped  missed",
			to:          "errors  missed",
			wantDetect:  true,
		},
		{
			description: "positive: RX and TX transposed is detected",
			from:        "    RX:  bytes",
			to:          "    TX:  bytes",
			wantDetect:  true,
		},
		{
			// print_stats64 pads the header to a fixed width, so the run of
			// spaces after `mcast` is part of the format string and not
			// incidental. Nothing else in the suite would notice it going.
			description: "boundary: the trailing padding on a header line is detected",
			from:        "  mcast           \n",
			to:          "  mcast\n",
			wantDetect:  true,
		},
		{
			description: "boundary: a one-space indent change on a link/ line is detected",
			from:        "    link/loopback",
			to:          "   link/loopback",
			wantDetect:  true,
		},
	}

	base := normalizeLinkStatsText(t, string(raw))

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			mutated := strings.Replace(string(raw), tc.from, tc.to, 1)
			if mutated == string(raw) {
				t.Fatalf("mutation %q -> %q did not apply; the golden has changed shape "+
					"and this row no longer tests what it says", tc.from, tc.to)
			}
			if got := normalizeLinkStatsText(t, mutated) != base; got != tc.wantDetect {
				t.Errorf("detected = %v, want %v", got, tc.wantDetect)
			}
		})
	}
}

// advancedCounterLine returns a counter line from an `ip -s link show` dump
// and the same line with every digit rotated, as a (from, to) pair for
// strings.Replace.
//
// It stands in for a hand-written counter literal, which is unwritable
// without pinning it to one capture: the values are whatever the interface
// had transferred at that instant.
//
// Two things make the returned pair a fair test of the normalizer. The
// rotation is character-for-character, so column widths, the field count and
// the trailing space all survive — the mutation differs from the original in
// digits and nothing else, which is exactly the difference the normalizer is
// supposed to tolerate. And the line chosen has at least one NON-ZERO field:
// an all-zero line is both the commonest in the dump and the weakest choice,
// since `0` rotating to `1` is a change a reader has to squint at, and the
// first such line in the file is ambiguous about which interface it came
// from.
func advancedCounterLine(t *testing.T, s string) (string, string) {
	t.Helper()

	lines := strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	rotate := func(r rune) rune {
		if r >= '0' && r <= '9' {
			return '0' + (r-'0'+1)%10
		}
		return r
	}
	inStats := false
	for _, line := range lines {
		if !inStats {
			trimmed := strings.TrimSpace(line)
			inStats = strings.HasPrefix(trimmed, "RX:") || strings.HasPrefix(trimmed, "TX:")
			continue
		}
		inStats = false

		nonZero := false
		for _, f := range strings.Fields(line) {
			if strings.TrimLeft(f, "0123456789") != "" {
				// Not a counter line after all. normalizeLinkStatsText
				// fails loudly on this; here it only means "keep looking",
				// because that test's job is to report it and this
				// helper's is to find an input for it.
				nonZero = false
				break
			}
			if strings.Trim(f, "0") != "" {
				nonZero = true
			}
		}
		if nonZero {
			return line, strings.Map(rotate, line)
		}
	}
	t.Fatalf("no counter line with a non-zero field in ip_link_stats; " +
		"either the dump changed shape or the capture was taken on an idle guest")
	return "", ""
}

// assertLinesEqual reports the FIRST differing line and the counts, rather
// than dumping two whole documents.
//
// A stanza-per-link diff is long enough that a %q of both sides buries the one
// line that moved, and these sidecars run to fourteen links.
func assertLinesEqual(t *testing.T, got, want string) {
	t.Helper()
	gotLines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	wantLines := strings.Split(strings.TrimSuffix(want, "\n"), "\n")

	n := len(gotLines)
	if len(wantLines) < n {
		n = len(wantLines)
	}
	for i := range n {
		if gotLines[i] != wantLines[i] {
			t.Fatalf("line %d differs\n got: %q\nwant: %q", i+1, gotLines[i], wantLines[i])
		}
	}
	if len(gotLines) != len(wantLines) {
		t.Fatalf("line count = %d, want %d; first %d lines agree",
			len(gotLines), len(wantLines), n)
	}
}

// zeroLinkStats replaces every number reachable under a "stats" or "stats64"
// key with 0, so two documents captured seconds apart compare on shape alone.
//
// It rewrites rather than deletes deliberately: deleting would make a MISSING
// counter indistinguishable from a matching one, and the member set is the
// half of the stats object this fixture can actually certify.
func zeroLinkStats(t *testing.T, raw []byte) []byte {
	t.Helper()

	var entries []map[string]any
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatalf("zeroLinkStats: %v (%s)", err, raw)
	}
	var zero func(v any) any
	zero = func(v any) any {
		switch x := v.(type) {
		case map[string]any:
			for k, e := range x {
				x[k] = zero(e)
			}
			return x
		case float64:
			return float64(0)
		default:
			return v
		}
	}
	for _, e := range entries {
		for _, k := range []string{"stats", "stats64"} {
			if s, ok := e[k]; ok {
				e[k] = zero(s)
			}
		}
	}
	out, err := json.Marshal(entries)
	if err != nil {
		t.Fatalf("zeroLinkStats: %v", err)
	}
	return out
}

// TestRunLinkShowJSON asserts the -json form over the 7_1_8 capture.
//
// # What this asserts and what it deliberately does not
//
// 7_1_8 has no `ip -j link show` sidecar and never will — it was taken on a
// host that no longer exists in that state — so this test cannot compare
// against `ip`'s JSON. What it asserts without one is everything that does not
// need it: that the output parses, that it holds one object per captured
// reply, and that the values agree with the text form goip printed from the
// same bytes. It is worth having on this corpus in particular, because 7_1_8
// is the one with the device breadth: 11 links, a bridge, a veth pair,
// docker0, four altnames.
//
// The direct comparison this comment used to describe as missing now exists
// against the OTHER corpus: TestLinkShowJSONMatchesCapturedSidecars replays
// the in-guest 7_1_4 dumps against the `ip -j -p` sidecars captured beside
// them. Neither test subsumes the other — that one has ground truth on a
// five-link topology, this one has the devices.
//
// go test ./internal/goip/ -run TestRunLinkShowJSON
func TestRunLinkShowJSON(t *testing.T) {
	t.Setenv("GOIP_REPLAY", linkDumpPcap)

	var textOut, stderr bytes.Buffer
	if code := Run([]string{"link", "show"}, &textOut, &stderr); code != ExitOK {
		t.Fatalf("text Run = %d; stderr: %s", code, stderr.String())
	}

	var jsonOut bytes.Buffer
	stderr.Reset()
	if code := Run([]string{"-j", "link", "show"}, &jsonOut, &stderr); code != ExitOK {
		t.Fatalf("json Run = %d; stderr: %s", code, stderr.String())
	}

	var views []map[string]any
	if err := json.Unmarshal(jsonOut.Bytes(), &views); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, jsonOut.String())
	}

	tests := []struct {
		description string
		check       func(t *testing.T)
	}{
		{
			description: "positive: the JSON array holds one object per captured reply",
			check: func(t *testing.T) {
				if len(views) != wantLinkStanzas {
					t.Errorf("%d objects, want %d", len(views), wantLinkStanzas)
				}
			},
		},
		{
			description: "positive: every ifname in the JSON appears in the text form",
			check: func(t *testing.T) {
				for i, v := range views {
					name, ok := v["ifname"].(string)
					if !ok {
						t.Errorf("object %d has no string ifname: %v", i, v["ifname"])
						continue
					}
					// The text form prints `NN: name@peer:` or `NN: name:`, so
					// the name is always followed by one of those two bytes.
					if !strings.Contains(textOut.String(), " "+name+":") &&
						!strings.Contains(textOut.String(), " "+name+"@") {
						t.Errorf("ifname %q is in the JSON but not in the text output", name)
					}
				}
			},
		},
		{
			description: "positive: the objects are in the same order as the text stanzas",
			check: func(t *testing.T) {
				rest := textOut.String()
				for i, v := range views {
					name, _ := v["ifname"].(string)
					idx := strings.Index(rest, " "+name)
					if idx < 0 {
						t.Fatalf("object %d %q out of order or missing", i, name)
					}
					rest = rest[idx+len(name):]
				}
			},
		},
		{
			// The renderer suppresses `qlen 0` in text (see RenderQlenZero) but
			// `ip -j` emits txqlen unconditionally, so the two forms genuinely
			// disagree here and the JSON is the one that keeps the value. This
			// row is what stops someone "fixing" the inconsistency by dropping
			// the field.
			description: "boundary: a link whose text form suppresses qlen still carries txqlen in JSON",
			check: func(t *testing.T) {
				found := false
				for _, v := range views {
					if v["ifname"] == "docker0" {
						found = true
						if _, ok := v["txqlen"]; !ok {
							t.Errorf("docker0 has no txqlen in JSON, though its text stanza ends at `group default `")
						}
					}
				}
				if !found {
					t.Error("docker0 is missing from the JSON, so this row asserted nothing")
				}
			},
		},
		{
			// 4 of the 11 links carry an altname, and it is a slice rather
			// than a scalar. A renderer that flattened it to the first entry
			// would still pass the text diff on this capture, because no link
			// here has two.
			description: "boundary: altnames are a JSON array, on exactly the links the sidecar shows one for",
			check: func(t *testing.T) {
				const wantWithAltnames = 4
				n := 0
				for _, v := range views {
					if a, ok := v["altnames"]; ok && a != nil {
						arr, isArr := a.([]any)
						if !isArr {
							t.Errorf("altnames is %T, want an array", a)
							continue
						}
						if len(arr) > 0 {
							n++
						}
					}
				}
				if n != wantWithAltnames {
					t.Errorf("%d links carry altnames, want %d", n, wantWithAltnames)
				}
			},
		},
		{
			description: "negative: no object carries an empty ifname",
			check: func(t *testing.T) {
				for i, v := range views {
					if v["ifname"] == "" {
						t.Errorf("object %d has an empty ifname", i)
					}
				}
			},
		},
		{
			// `ip -j` prints flags as an array of the same tokens the text
			// form puts between the angle brackets, not as a number. A numeric
			// flags field would be a silent schema divergence that no text
			// diff could see.
			description: "corner: flags is an array of tokens, not the raw number",
			check: func(t *testing.T) {
				for i, v := range views {
					arr, ok := v["flags"].([]any)
					if !ok {
						t.Fatalf("object %d flags is %T, want an array", i, v["flags"])
					}
					for _, tok := range arr {
						if _, isStr := tok.(string); !isStr {
							t.Errorf("object %d has a non-string flag %v", i, tok)
						}
					}
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, tc.check)
	}
}

// ---- `link show dev NAME`: the two by-name single-gets -----------------------

// linkReplay is a ReplaySource that also implements TalkSource, answering
// by-NAME single-gets and recording each request verbatim.
//
// routeReplay (obj_route_test.go) is the by-index sibling and cannot be reused
// here. `ip link show dev NAME` selects on IFLA_IFNAME and leaves ifi_index at
// 0, so an index-keyed responder would try to answer both requests with
// whichever link has index 0 — that is, none of them.
//
// Requests are kept whole rather than reduced to the name they carry, because
// for this command the bytes are the assertion. Both requests name the same
// interface and carry the same two attributes; what tells them apart is
// ifi_family and the order those attributes appear in, and nothing short of
// the request itself shows that.
type linkReplay struct {
	inner *ReplaySource

	// dumps counts Dump calls, which for this command must stay at zero, and
	// gets records every single-get in the order it was sent.
	dumps int
	gets  [][]byte

	// failGet is the 1-based index of a single-get the replayed kernel refuses
	// to answer; 0 answers all of them. It is an ordinal rather than a name
	// because both requests name the same interface, so failing "lo" would
	// fail both and could not distinguish the two error paths.
	failGet int
}

func newLinkReplay(t *testing.T, path string) *linkReplay {
	t.Helper()
	s, err := OpenReplay(path)
	if err != nil {
		t.Fatalf("OpenReplay(%s): %v", path, err)
	}
	return &linkReplay{inner: s}
}

func (s *linkReplay) Dump(request []byte, msgType uint16) ([][]byte, error) {
	s.dumps++
	return s.inner.Dump(request, msgType)
}

func (s *linkReplay) Talk(request []byte, msgType uint16) ([]byte, error) {
	s.gets = append(s.gets, zeroSeqPidCopy(request))
	if s.failGet == len(s.gets) {
		return nil, fmt.Errorf("%w: single-get %d refused", ErrNoReplay, s.failGet)
	}

	// A `link show dev` run sends by-name gets for the selector and by-index
	// gets for whatever the reply points at, so this answers both. Which one
	// a request is, is decided the way the kernel decides it: IFLA_IFNAME
	// wins if it is there, otherwise ifi_index is the selector.
	sel := selectorOf(request)
	for _, m := range s.inner.cap.Msgs() {
		if m.IsRequest() || m.Hdr.Type != msgType {
			continue
		}
		li, perr := xtcpnl.ParseNewLink(m.Body)
		if perr != nil {
			continue
		}
		if (sel.name != "" && li.Name == sel.name) || (sel.name == "" && li.Index == sel.index) {
			return xtcpnl.CopyBytes(m.Body), nil
		}
	}
	return nil, fmt.Errorf("%w: RTM_GETLINK %s", ErrNoReplay, sel)
}

// zeroSeqPidCopy returns the request with nlmsg_seq and nlmsg_pid cleared, so
// a recorded request can be compared without depending on how many requests
// happened to precede it.
func zeroSeqPidCopy(request []byte) []byte {
	out := xtcpnl.CopyBytes(request)
	for i := 8; i < 16 && i < len(out); i++ {
		out[i] = 0
	}
	return out
}

// getShape is the part of a single-get this test asserts.
//
// Deliberately not the whole request: comparing against bytes rebuilt from
// req.LinkShowByName and req.LinkShowDev would only prove those two functions
// equal themselves. Their byte-level correctness is pinned against captured
// iproute2 traffic in pkg/xtcpnl; what is under test here is the wiring — that
// `link show dev` sends one of each, in this order, plus whatever side-gets
// the reply's own indexes require, and no dump.
//
// name and index are both here because exactly one of them is the selector,
// and which one it is distinguishes the two by-name requests from the
// by-index side-gets print_linkinfo issues for IFLA_MASTER and IFLA_LINK.
type getShape struct {
	flags     uint16
	family    uint8
	firstAttr uint16
	name      string
	index     int32
}

// String makes a mismatched row's failure message readable, and gives
// linkReplay a way to say what it could not answer.
func (g getShape) String() string {
	if g.name != "" {
		return fmt.Sprintf("name %q", g.name)
	}
	return fmt.Sprintf("index %d", g.index)
}

// selectorOf reads the fields of an RTM_GETLINK request that getShape keeps.
//
// A malformed request cannot happen here — every one of them came from a
// builder in this repo moments earlier — so an unwalkable attribute list
// yields the zero name rather than an error the caller would have to thread
// through a table.
func selectorOf(request []byte) getShape {
	g := getShape{
		flags:  binary.LittleEndian.Uint16(request[6:8]),
		family: request[xtcpnl.NlMsgHdrSizeCst],
		index:  int32(binary.LittleEndian.Uint32(request[xtcpnl.NlMsgHdrSizeCst+4:])),
	}
	attrs := request[xtcpnl.NlMsgHdrSizeCst+xtcpnl.IfInfomsgSizeCst:]
	if len(attrs) >= 4 {
		g.firstAttr = binary.LittleEndian.Uint16(attrs[2:4])
	}
	_ = xtcpnl.WalkRTAttrs(attrs, func(atype uint16, val []byte) {
		if atype == uint16(unix.IFLA_IFNAME) {
			g.name = strings.TrimRight(string(val), "\x00")
		}
	})
	return g
}

// runLinkWith drives runLink over a source directly, bypassing Run so the test
// can supply a TalkSource and read the counters back afterwards.
func runLinkWith(t *testing.T, src Source, family uint8, args []string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	c := &runCtx{
		src:    src,
		lltab:  NewLLTab(),
		out:    &out,
		errOut: io.Discard,
		family: family,
	}
	err := runLink(c, args)
	return out.String(), err
}

// TestLinkShowDevTransactionShape is the assertion stdout cannot make: that
// `ip link show dev NAME` is two by-name single-gets and no dump, and that the
// two are not the same request sent twice.
//
// iproute2 asks about the interface twice. `dev NAME` is parsed into
// filter.ifindex by ll_name_to_index (ip/ipaddress.c:2254), which issues
// ll_link_get(name, 0) on a throwaway socket (lib/ll_map.c:264) purely to
// learn the index; then iplink_get re-fetches the same link, by name again, on
// the main socket (ip/ipaddress.c:2293, ip/iplink.c:1497-1515), and that second
// reply is the one print_linkinfo renders. The two differ in ifi_family and in
// the order they place IFLA_IFNAME and IFLA_EXT_MASK, so pkg/nlparity's
// full-byte request comparison rejects either one standing in for the other.
//
// The shape this replaced — dump, resolve the name locally, get by index —
// printed byte-identical output and kept the transaction count at two, so
// nothing but a request-level assertion could see it was wrong. That is what
// the wantDumps column is for.
//
// go test ./internal/goip/ -run TestLinkShowDevTransactionShape
func TestLinkShowDevTransactionShape(t *testing.T) {
	// The two shapes, named once. Every positive row expects exactly these,
	// in this order, differing only in the interface named.
	llLinkGet := func(name string) getShape {
		return getShape{
			flags:     uint16(unix.NLM_F_REQUEST),
			family:    unix.AF_UNSPEC,
			firstAttr: uint16(unix.IFLA_EXT_MASK),
			name:      name,
		}
	}
	iplinkGet := func(name string) getShape {
		return getShape{
			flags:     uint16(unix.NLM_F_REQUEST),
			family:    unix.AF_PACKET,
			firstAttr: uint16(unix.IFLA_IFNAME),
			name:      name,
		}
	}
	// The third shape: ll_index_to_name's by-index get, which print_linkinfo
	// issues for IFLA_MASTER and for a netnsid-less IFLA_LINK. It is the same
	// request `route show` sends, and it names an index, not a name.
	byIndexGet := func(index int32) getShape {
		return getShape{
			flags:     uint16(unix.NLM_F_REQUEST),
			family:    unix.AF_UNSPEC,
			firstAttr: uint16(unix.IFLA_EXT_MASK),
			index:     index,
		}
	}

	tests := []struct {
		description      string
		family           uint8
		args             []string
		failGet          int
		wantDumps        int
		wantGets         []getShape
		wantStdoutPrefix string
		wantErr          error
	}{
		{
			description: "positive: `link show dev lo` sends two by-name single-gets and no dump",
			family:      unix.AF_PACKET,
			args:        []string{"show", "dev", "lo"},
			wantDumps:   0,
			wantGets: []getShape{
				llLinkGet("lo"),
				iplinkGet("lo"),
			},
			wantStdoutPrefix: "1: lo: ",
		},
		{
			// A second device, so a row cannot pass by coincidence of `lo`
			// being index 1 and first in the capture.
			description: "positive: the same two requests are sent for a device that is not lo",
			family:      unix.AF_PACKET,
			args:        []string{"show", "dev", "docker0"},
			wantDumps:   0,
			wantGets: []getShape{
				llLinkGet("docker0"),
				iplinkGet("docker0"),
			},
			wantStdoutPrefix: "8: docker0: ",
		},
		{
			// 15 characters is the longest IFNAMSIZ allows, and it is the
			// length at which a builder that forgot the NUL terminator would
			// overflow the attribute rather than merely mis-size it.
			//
			// This veth also pins the cheap peer suffix. Its IFLA_LINK names
			// index 2, but IFLA_LINK_NETNSID is present, so print_name_and_link
			// takes the ll_idx_n2a branch — an unconditional "if%u" that
			// consults nothing and asks nothing (lib/ll_map.c). Hence `@if2`
			// and hence still two requests. The committed ip_link_n shows `ip`
			// printing `@if2` from a FULL cache, which is the evidence that
			// the branch really is index-blind rather than merely unresolved.
			description: "boundary: a 15-character device name round-trips, and its netnsid peer stays if%u",
			family:      unix.AF_PACKET,
			args:        []string{"show", "dev", "ve-nordlayepDd-"},
			wantDumps:   0,
			wantGets: []getShape{
				llLinkGet("ve-nordlayepDd-"),
				iplinkGet("ve-nordlayepDd-"),
			},
			wantStdoutPrefix: "59: ve-nordlayepDd-@if2: ",
		},
		{
			// IFLA_MASTER is resolved by ll_index_to_name (ip/ipaddress.c:1037),
			// which does hit the kernel on a cache miss — and after two by-name
			// gets the cache holds exactly one link, so it misses. Three
			// requests, the third by index, and only then does the bridge get
			// a name instead of `if9`.
			//
			// This is the row that would have caught the earlier
			// dump-then-get-by-index shape from the other side: a dump fills
			// the cache with all eleven links, so the master resolves for free
			// and the third request is never sent.
			description: "positive: a link with a master sends a third, by-index get for the bridge",
			family:      unix.AF_PACKET,
			args:        []string{"show", "dev", "veth179a698"},
			wantDumps:   0,
			wantGets: []getShape{
				llLinkGet("veth179a698"),
				iplinkGet("veth179a698"),
				byIndexGet(9),
			},
			wantStdoutPrefix: "60: veth179a698@if2: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 qdisc noqueue master br-3a5828b2963a ",
		},
		{
			description: "boundary: `link lst dev lo` abbreviates to the same two requests",
			family:      unix.AF_PACKET,
			args:        []string{"lst", "dev", "lo"},
			wantDumps:   0,
			wantGets: []getShape{
				llLinkGet("lo"),
				iplinkGet("lo"),
			},
			wantStdoutPrefix: "1: lo: ",
		},
		{
			// ipaddr_list_link assigns preferred_family = AF_PACKET at
			// ip/ipaddress.c:2416, BEFORE parsing arguments, so `-4` never
			// reaches the header. A goip that threaded c.family through would
			// send AF_INET here and diverge from `ip` at L2 while printing the
			// same stanza.
			description: "corner: `-4 link show dev lo` still sends AF_PACKET, because -4 is overridden",
			family:      unix.AF_INET,
			args:        []string{"show", "dev", "lo"},
			wantDumps:   0,
			wantGets: []getShape{
				llLinkGet("lo"),
				iplinkGet("lo"),
			},
			wantStdoutPrefix: "1: lo: ",
		},
		{
			// The name never resolves, so the first get fails and the second
			// is never sent. `ip` behaves the same way: ll_name_to_index
			// returning 0 is "Cannot find device", and iplink_get is not
			// reached.
			description: "negative: an unknown device fails on the first get, and the second is not sent",
			family:      unix.AF_PACKET,
			args:        []string{"show", "dev", "nosuchdev0"},
			wantDumps:   0,
			wantGets:    []getShape{llLinkGet("nosuchdev0")},
			wantErr:     ErrNoReplay,
		},
		{
			// A failure on the SECOND get must surface too, and must not be
			// papered over with a dump-and-filter fallback — that fallback is
			// the divergence this whole path exists to remove.
			description: "negative: a failure on the second get is an error, not a fall back to a dump",
			family:      unix.AF_PACKET,
			args:        []string{"show", "dev", "lo"},
			failGet:     2,
			wantDumps:   0,
			wantGets: []getShape{
				llLinkGet("lo"),
				iplinkGet("lo"),
			},
			wantErr: ErrNoReplay,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := newLinkReplay(t, linkDumpPcap)
			src.failGet = tc.failGet

			got, err := runLinkWith(t, src, tc.family, tc.args)

			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
			} else if err != nil {
				t.Fatalf("runLink(%q): %v", tc.args, err)
			}

			if src.dumps != tc.wantDumps {
				t.Errorf("dumps = %d, want %d", src.dumps, tc.wantDumps)
			}
			if len(src.gets) != len(tc.wantGets) {
				t.Fatalf("single-gets = %d, want %d", len(src.gets), len(tc.wantGets))
			}
			for i := range src.gets {
				if shape := selectorOf(src.gets[i]); shape != tc.wantGets[i] {
					t.Errorf("single-get %d = %+v, want %+v", i+1, shape, tc.wantGets[i])
				}
			}
			if tc.wantStdoutPrefix != "" && !strings.HasPrefix(got, tc.wantStdoutPrefix) {
				t.Errorf("stdout = %q, want prefix %q", got, tc.wantStdoutPrefix)
			}
		})
	}
}
