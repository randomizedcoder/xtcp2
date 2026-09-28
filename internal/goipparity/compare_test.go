package goipparity

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
)

// The guest corpus: the only captures in the repo taken in a namespace with no
// side traffic, which is what makes them usable as both sides of a triple.
//
// A triple built by copying one capture three times is a real clean run, not a
// synthetic one — ip_a, goip and ip_b genuinely hold identical traffic, which
// is exactly what a perfect goip would produce. Every positive row here reads
// captured bytes; the constructed cases are all negative, boundary or corner,
// per the standing rule about where expectations come from.
const (
	tdGuest        = "../../pkg/xtcpnl/testdata/7_1_4/dumps"
	tdGuestLink    = tdGuest + "/netlink_route_getlink.pcap"
	tdGuestLinkOut = tdGuest + "/ip_link_n"
	tdGuestAddr    = tdGuest + "/netlink_route_getaddr.pcap"
	tdGuestAddrOut = tdGuest + "/ip_addr_n"

	// tdEvents is an `ip monitor` capture from the same kernel: multicast
	// notifications and replies belonging to no transaction this capture saw.
	// It is the negative side of the hygiene arm, and real captured bytes
	// rather than constructed ones — which matters, because the thing being
	// asserted is that a capture full of traffic the comparator cannot
	// attribute fails instead of reporting on the part it understood.
	tdEvents = "../../pkg/xtcpnl/testdata/7_1_4/netlink_route_events_link.pcap"
)

// writeTriple lays out one command's six files in dir. A side whose pcap or
// text is the empty string is not written at all, which is how the MISSING
// rows are built.
func writeTriple(t *testing.T, dir string, c Command, pcaps, texts map[string]string) {
	t.Helper()
	for _, side := range Sides() {
		if src := pcaps[side]; src != "" {
			b, err := os.ReadFile(src)
			if err != nil {
				t.Fatalf("reading %s: %v", src, err)
			}
			writeInDir(t, dir, c.CaptureName(side), b)
		}
		if txt, ok := texts[side]; ok {
			writeInDir(t, dir, c.StdoutName(side), []byte(txt))
		}
	}
}

// writeInDir writes one file directly inside dir, and refuses a name that is
// not a bare base name.
//
// The check is the point, not the write. CaptureName and StdoutName are what
// the guest driver and the comparator agree on, and a separator appearing in
// either — a slug that grew a `/`, a side that grew a path — would put the
// file somewhere CompareDir does not look, where it reads as MISSING rather
// than as the naming bug it is. Asserting it here, in the only place the tests
// build a corpus, catches that in every row at once.
//
// The write itself goes through os.Root rather than os.WriteFile on a joined
// path, so the confinement is enforced by the kernel-facing API instead of by
// the check above alone: os.Root resolves every component within dir and
// refuses to escape it. That is also what makes the path untainted for gosec's
// G703, which a filepath.Base sanitization does not, because dir is a
// parameter too.
func writeInDir(t *testing.T, dir, name string, b []byte) {
	t.Helper()
	if base := filepath.Base(name); base != name {
		t.Fatalf("%q is not a bare file name (base is %q); "+
			"CaptureName/StdoutName must not contain a separator", name, base)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatalf("opening %s as a root: %v", dir, err)
	}
	defer func() {
		if err := root.Close(); err != nil {
			t.Errorf("closing root %s: %v", dir, err)
		}
	}()
	f, err := root.Create(name)
	if err != nil {
		t.Fatalf("creating %s in %s: %v", name, dir, err)
	}
	if _, err := f.Write(b); err != nil {
		t.Errorf("writing %s: %v", name, err)
	}
	if err := f.Close(); err != nil {
		t.Errorf("closing %s: %v", name, err)
	}
}

// mustRead is the sidecar text a clean triple uses on all three sides.
func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(b)
}

// sameAll is a side map with the same value on all three sides.
func sameAll(v string) map[string]string {
	return map[string]string{SideIPA: v, SideGoip: v, SideIPB: v}
}

// go test ./internal/goipparity/ -run TestCompareDir
func TestCompareDir(t *testing.T) {
	al, err := nlparity.EmbeddedAllowlist()
	if err != nil {
		t.Fatalf("EmbeddedAllowlist: %v", err)
	}
	linkOut := mustRead(t, tdGuestLinkOut)
	addrOut := mustRead(t, tdGuestAddrOut)

	linkShow, err := Lookup("link show")
	if err != nil {
		t.Fatal(err)
	}
	addrShow, err := Lookup("addr show")
	if err != nil {
		t.Fatal(err)
	}
	routeShow, err := Lookup("route show")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		description string
		// setup writes whatever the row needs into a fresh directory.
		setup func(t *testing.T, dir string)
		// want is the expected status of the named command. Commands not
		// named are expected to be SKIP (unimplemented) or MISSING
		// (implemented, no files), which the shared assertions check.
		want map[string]Status
	}{
		{
			description: "positive: a triple of three identical real captures is a clean pass",
			setup: func(t *testing.T, dir string) {
				writeTriple(t, dir, linkShow, sameAll(tdGuestLink), sameAll(linkOut))
			},
			want: map[string]Status{"link show": StatusPass},
		},
		{
			description: "positive: two commands both clean in one directory",
			setup: func(t *testing.T, dir string) {
				writeTriple(t, dir, linkShow, sameAll(tdGuestLink), sameAll(linkOut))
				writeTriple(t, dir, addrShow, sameAll(tdGuestAddr), sameAll(addrOut))
			},
			want: map[string]Status{
				"link show": StatusPass,
				"addr show": StatusPass,
			},
		},
		{
			description: "positive: an unimplemented command with no files is SKIP, not MISSING",
			setup: func(t *testing.T, dir string) {
				writeTriple(t, dir, linkShow, sameAll(tdGuestLink), sameAll(linkOut))
			},
			want: map[string]Status{
				"link show":  StatusPass,
				"route show": StatusSkip,
			},
		},
		{
			description: "negative: an implemented command with no files at all is MISSING and fails",
			setup: func(t *testing.T, _ string) {
				// Nothing written: every implemented command is MISSING, and
				// the run fails rather than reporting a vacuous pass.
			},
			want: map[string]Status{
				"link show": StatusMissing,
				"addr show": StatusMissing,
			},
		},
		{
			description: "negative: a triple missing only the goip pcap is MISSING, naming just that file",
			setup: func(t *testing.T, dir string) {
				writeTriple(t, dir, linkShow, map[string]string{
					SideIPA: tdGuestLink, SideIPB: tdGuestLink,
				}, sameAll(linkOut))
			},
			want: map[string]Status{"link show": StatusMissing},
		},
		{
			description: "negative: a triple missing only the goip stdout is MISSING, because stdout is the hole the netlink tiers cannot see",
			setup: func(t *testing.T, dir string) {
				writeTriple(t, dir, linkShow, sameAll(tdGuestLink), map[string]string{
					SideIPA: linkOut, SideIPB: linkOut,
				})
			},
			want: map[string]Status{"link show": StatusMissing},
		},
		{
			description: "negative: a goip capture for an unimplemented command is a FAIL, since the table and the driver disagree",
			setup: func(t *testing.T, dir string) {
				writeTriple(t, dir, linkShow, sameAll(tdGuestLink), sameAll(linkOut))
				writeTriple(t, dir, routeShow,
					map[string]string{SideGoip: tdGuestLink}, nil)
			},
			want: map[string]Status{
				"link show":  StatusPass,
				"route show": StatusFail,
			},
		},
		{
			description: "negative: a corrupt pcap is a FAIL with the file named, not a silent skip",
			setup: func(t *testing.T, dir string) {
				writeTriple(t, dir, linkShow, sameAll(tdGuestLink), sameAll(linkOut))
				if err := os.WriteFile(
					filepath.Join(dir, linkShow.CaptureName(SideGoip)),
					[]byte("not a pcap at all"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			want: map[string]Status{"link show": StatusFail},
		},
		{
			description: "boundary: an empty goip pcap is a FAIL rather than a clean zero-transaction comparison",
			setup: func(t *testing.T, dir string) {
				writeTriple(t, dir, linkShow, sameAll(tdGuestLink), sameAll(linkOut))
				if err := os.WriteFile(
					filepath.Join(dir, linkShow.CaptureName(SideGoip)),
					nil, 0o600); err != nil {
					t.Fatal(err)
				}
			},
			want: map[string]Status{"link show": StatusFail},
		},
		{
			description: "boundary: an empty goip stdout warns rather than fails on an ungated command, and never reads as PASS",
			setup: func(t *testing.T, dir string) {
				writeTriple(t, dir, addrShow, sameAll(tdGuestAddr), map[string]string{
					SideIPA: addrOut, SideGoip: "", SideIPB: addrOut,
				})
			},
			// `addr show` and not `link show`, because the distinction this row
			// is named for only exists on a command outside gated_commands, and
			// `link show` joined that list once a live Tier C run measured it
			// clean. Written against linkShow this row would have gone on
			// passing while asserting nothing it claims: the status would be
			// FAIL, and "warns rather than fails" would be untested.
			//
			// A goip that rendered nothing must not be labeled PASS, which is
			// why StatusWarn exists. The finding itself is asserted separately
			// below, in TestCompareDirStdoutFindingsAreReported — a row that
			// only checked the status would pass whether or not the comparison
			// happened at all.
			want: map[string]Status{"addr show": StatusWarn},
		},
		{
			// The single most important row in this file. Everything else
			// asserts that a clean run reports clean; this asserts the harness
			// can fail at all, which is the plan's own verification item 4.
			description: "negative: a goip capture that differs from both ip sides on a gated command FAILS and is never PASS",
			setup: func(t *testing.T, dir string) {
				writeTriple(t, dir, linkShow, map[string]string{
					SideIPA: tdGuestLink, SideGoip: tdGuestAddr, SideIPB: tdGuestLink,
				}, sameAll(linkOut))
			},
			// FAIL, not WARN, and that is the whole point of `link show` being
			// in gated_commands: this is the one row that proves a divergence
			// can end a run rather than being filed as advice.
			want: map[string]Status{"link show": StatusFail},
		},
		{
			// The same divergence on an ungated command, so the two rows
			// together pin the gate rather than only one side of it. Without
			// this one, a gated_commands list that had quietly grown to include
			// every command would still be green.
			description: "negative: the same divergence on an ungated command warns instead, and is still never PASS",
			setup: func(t *testing.T, dir string) {
				writeTriple(t, dir, addrShow, map[string]string{
					SideIPA: tdGuestAddr, SideGoip: tdGuestLink, SideIPB: tdGuestAddr,
				}, sameAll(addrOut))
			},
			want: map[string]Status{"addr show": StatusWarn},
		},
		{
			description: "negative: a goip capture full of notifications is a hygiene FAIL, which gating does not soften",
			setup: func(t *testing.T, dir string) {
				writeTriple(t, dir, addrShow, map[string]string{
					SideIPA: tdGuestAddr, SideGoip: tdEvents, SideIPB: tdGuestAddr,
				}, sameAll(addrOut))
			},
			// Deliberately an UNGATED command, which is what gives the row its
			// content: nlparity.Report.Failed fails on hygiene regardless of
			// gating, so FAIL here cannot be explained by gated_commands. A
			// capture the comparator could not attribute says nothing about
			// goip either way, and reporting it as a warning would let a broken
			// capture window pass as a soft finding.
			want: map[string]Status{"addr show": StatusFail},
		},
		{
			description: "corner: a directory that does not exist behaves as an empty one, so every implemented command is MISSING",
			setup:       nil,
			want: map[string]Status{
				"link show": StatusMissing,
				"addr show": StatusMissing,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			dir := t.TempDir()
			if tt.setup == nil {
				dir = filepath.Join(dir, "does-not-exist")
			} else {
				tt.setup(t, dir)
			}

			byName := map[string]Result{}
			for _, r := range CompareDir(dir, al) {
				byName[r.Command.Name] = r
			}
			if len(byName) != len(Commands()) {
				t.Fatalf("got %d results, want one per command (%d)",
					len(byName), len(Commands()))
			}

			for name, want := range tt.want {
				r, ok := byName[name]
				if !ok {
					t.Fatalf("no result for %q", name)
				}
				if r.Status != want {
					t.Errorf("%q status = %s, want %s (err=%v absent=%v)",
						name, r.Status, want, r.Err, r.Absent)
				}
			}

			// Every result the row did not name must still be one of the
			// statuses its command can legitimately have, so a row cannot
			// pass while some other command silently errored.
			for name, r := range byName {
				if _, named := tt.want[name]; named {
					continue
				}
				switch {
				case !r.Command.Implemented && r.Status != StatusSkip:
					t.Errorf("%q is unimplemented but status = %s (err=%v)",
						name, r.Status, r.Err)
				case r.Command.Implemented &&
					r.Status != StatusMissing && r.Status != StatusPass:
					t.Errorf("%q status = %s, expected MISSING or PASS (err=%v)",
						name, r.Status, r.Err)
				}
			}
		})
	}
}

// TestCompareDirDetail asserts the contents of a result rather than only its
// status, because a status is the one thing a comparison that never ran can
// still get right.
//
// go test ./internal/goipparity/ -run TestCompareDirDetail
func TestCompareDirDetail(t *testing.T) {
	al, err := nlparity.EmbeddedAllowlist()
	if err != nil {
		t.Fatalf("EmbeddedAllowlist: %v", err)
	}
	linkOut := mustRead(t, tdGuestLinkOut)
	linkShow, err := Lookup("link show")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		description string
		setup       func(t *testing.T, dir string)
		check       func(t *testing.T, r Result)
	}{
		{
			description: "positive: a clean triple reports equal transaction counts and an empty control",
			setup: func(t *testing.T, dir string) {
				writeTriple(t, dir, linkShow, sameAll(tdGuestLink), sameAll(linkOut))
			},
			check: func(t *testing.T, r Result) {
				if r.Netlink.RefTxns == 0 {
					t.Error("RefTxns is 0; the capture was not segmented")
				}
				if r.Netlink.RefTxns != r.Netlink.SubTxns {
					t.Errorf("txns ip=%d goip=%d, want equal on identical captures",
						r.Netlink.RefTxns, r.Netlink.SubTxns)
				}
				if r.Netlink.ControlSize != 0 {
					t.Errorf("ControlSize = %d, want 0 on two identical reference "+
						"captures", r.Netlink.ControlSize)
				}
				if len(r.Netlink.Findings) != 0 {
					t.Errorf("findings on an identical triple: %v", r.Netlink.Findings)
				}
				if !r.Stdout.Compared {
					t.Error("Stdout.Compared is false on a triple with all three .out files")
				}
			},
		},
		{
			description: "positive: the guest capture is hygienic, which is what makes it usable as a reference",
			setup: func(t *testing.T, dir string) {
				writeTriple(t, dir, linkShow, sameAll(tdGuestLink), sameAll(linkOut))
			},
			check: func(t *testing.T, r Result) {
				if len(r.Netlink.Hygiene) != 0 {
					t.Errorf("hygiene findings on the clean guest capture: %v",
						r.Netlink.Hygiene)
				}
			},
		},
		{
			description: "negative: an empty goip stdout produces unsuppressible structural findings",
			setup: func(t *testing.T, dir string) {
				writeTriple(t, dir, linkShow, sameAll(tdGuestLink), map[string]string{
					SideIPA: linkOut, SideGoip: "", SideIPB: linkOut,
				})
			},
			check: func(t *testing.T, r Result) {
				if len(r.Stdout.Findings) == 0 {
					t.Fatal("no stdout findings for an empty goip rendering; this is " +
						"the case the whole stdout comparison exists for")
				}
				want := map[string]bool{
					"stdout:lines": false, "stdout:ifnames": false,
					"stdout:ifindexes": false,
				}
				for _, d := range r.Stdout.Findings {
					if _, ok := want[d.Locus()]; ok {
						want[d.Locus()] = true
						if d.Class.Suppressible() {
							t.Errorf("%q is suppressible; a goip that rendered "+
								"nothing could then be allowlisted", d.Locus())
						}
					}
				}
				for locus, found := range want {
					if !found {
						t.Errorf("no finding at %q", locus)
					}
				}
			},
		},
		{
			description: "negative: a missing triple names every absent file, not just the first",
			setup:       func(t *testing.T, _ string) {},
			check: func(t *testing.T, r Result) {
				if len(r.Absent) != 6 {
					t.Errorf("Absent = %v, want all six files of the triple", r.Absent)
				}
			},
		},
		{
			description: "boundary: a goip capture identical to ip_a but with ip_b differing puts the difference in the control",
			setup: func(t *testing.T, dir string) {
				// ip_b is a different command's capture, so almost everything
				// diverges between the two references. The control is then
				// large, which is the plan's "distrust this run" sentinel —
				// and the test diff, being empty, stays clean.
				writeTriple(t, dir, linkShow, map[string]string{
					SideIPA: tdGuestLink, SideGoip: tdGuestLink, SideIPB: tdGuestAddr,
				}, sameAll(linkOut))
			},
			check: func(t *testing.T, r Result) {
				if r.Netlink.ControlSize == 0 {
					t.Error("ControlSize = 0 though the two reference captures are " +
						"of different commands")
				}
				if len(r.Netlink.Findings) != 0 {
					t.Errorf("findings though ip_a and goip are byte-identical: %v",
						r.Netlink.Findings)
				}
			},
		},
		{
			description: "corner: a result's Failed tracks its status and nothing else",
			setup: func(t *testing.T, dir string) {
				writeTriple(t, dir, linkShow, sameAll(tdGuestLink), sameAll(linkOut))
			},
			check: func(t *testing.T, r Result) {
				if r.Failed() {
					t.Errorf("a PASS result reports Failed: %+v", r.Status)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			dir := t.TempDir()
			tt.setup(t, dir)
			for _, r := range CompareDir(dir, al) {
				if r.Command.Name == "link show" {
					tt.check(t, r)
					return
				}
			}
			t.Fatal("no result for `link show`")
		})
	}
}

// TestRender pins the sentinel lines, which are the guest driver's interface.
//
// go test ./internal/goipparity/ -run TestRender
func TestRender(t *testing.T) {
	al, err := nlparity.EmbeddedAllowlist()
	if err != nil {
		t.Fatalf("EmbeddedAllowlist: %v", err)
	}
	linkOut := mustRead(t, tdGuestLinkOut)
	addrOut := mustRead(t, tdGuestAddrOut)
	linkShow, _ := Lookup("link show")
	addrShow, _ := Lookup("addr show")

	tests := []struct {
		description string
		setup       func(t *testing.T, dir string)
		wantPass    bool
		wantLines   []string
		wantAbsent  []string
	}{
		{
			description: "positive: every implemented command clean reports all three sentinels green",
			setup: func(t *testing.T, dir string) {
				for _, c := range Commands() {
					if !c.Implemented {
						continue
					}
					src, out := tdGuestAddr, addrOut
					if c.Name == "link show" {
						src, out = tdGuestLink, linkOut
					}
					writeTriple(t, dir, c, sameAll(src), sameAll(out))
				}
			},
			wantPass: true,
			wantLines: []string{
				"GOIP_PARITY_OVERALL_PASS",
				"GOIP_PARITY_CONTROL_CLEAN",
				"GOIP_PARITY_HYGIENE_PASS",
				"GOIP_PARITY_SKIP route_show",
			},
			wantAbsent: []string{"GOIP_PARITY_OVERALL_FAIL", "GOIP_PARITY_MISSING"},
		},
		{
			description: "negative: an empty directory fails and says nothing was compared",
			setup:       func(t *testing.T, _ string) {},
			wantPass:    false,
			wantLines: []string{
				"GOIP_PARITY_NOTHING_COMPARED",
				"GOIP_PARITY_OVERALL_FAIL",
				"GOIP_PARITY_MISSING link_show",
			},
			wantAbsent: []string{"GOIP_PARITY_OVERALL_PASS"},
		},
		{
			description: "negative: one missing command fails the whole run even when another is clean",
			setup: func(t *testing.T, dir string) {
				writeTriple(t, dir, linkShow, sameAll(tdGuestLink), sameAll(linkOut))
			},
			wantPass: false,
			wantLines: []string{
				"GOIP_PARITY_PASS link_show",
				"GOIP_PARITY_MISSING addr_show",
				"GOIP_PARITY_OVERALL_FAIL",
			},
			wantAbsent: []string{"GOIP_PARITY_NOTHING_COMPARED"},
		},
		{
			description: "boundary: a run with a noisy control still passes but says so, since a large control is a warning and not a verdict",
			setup: func(t *testing.T, dir string) {
				writeTriple(t, dir, linkShow, map[string]string{
					SideIPA: tdGuestLink, SideGoip: tdGuestLink, SideIPB: tdGuestAddr,
				}, sameAll(linkOut))
				writeTriple(t, dir, addrShow, sameAll(tdGuestAddr), sameAll(addrOut))
				for _, c := range Commands() {
					if c.Implemented && c.Name != "link show" && c.Name != "addr show" {
						writeTriple(t, dir, c, sameAll(tdGuestAddr), sameAll(addrOut))
					}
				}
			},
			wantPass:   true,
			wantLines:  []string{"GOIP_PARITY_CONTROL_NOISY", "GOIP_PARITY_OVERALL_PASS"},
			wantAbsent: []string{"GOIP_PARITY_CONTROL_CLEAN"},
		},
		{
			description: "negative: a divergent goip on an ungated command renders GOIP_PARITY_WARN with the L1 count finding, and the ungated sentinel counts it",
			setup: func(t *testing.T, dir string) {
				writeTriple(t, dir, addrShow, map[string]string{
					SideIPA: tdGuestAddr, SideGoip: tdGuestLink, SideIPB: tdGuestAddr,
				}, sameAll(addrOut))
			},
			// `addr show`, because GOIP_PARITY_UNGATED_DIVERGENCES only counts
			// findings on commands outside gated_commands - a gated one is
			// counted by the OVERALL sentinel instead. `link show` is gated as
			// of the first clean Tier C run, so this row measures nothing if it
			// is written against it.
			wantPass: false, // link show is missing
			wantLines: []string{
				"GOIP_PARITY_WARN addr_show",
				"finding: L1 transaction-count txn-count: ip=2 goip=1",
				"GOIP_PARITY_UNGATED_DIVERGENCES 1",
			},
			wantAbsent: []string{
				"GOIP_PARITY_PASS addr_show",
				"GOIP_PARITY_UNGATED_CLEAN",
			},
		},
		{
			// The gated counterpart, which is what makes the sentinel pair
			// legible: the same shape of divergence, on a gated command, must
			// reach GOIP_PARITY_FAIL and must NOT be filed under
			// UNGATED_DIVERGENCES.
			description: "negative: the same divergence on a gated command renders GOIP_PARITY_FAIL and is not counted as an ungated divergence",
			setup: func(t *testing.T, dir string) {
				writeTriple(t, dir, linkShow, map[string]string{
					SideIPA: tdGuestLink, SideGoip: tdGuestAddr, SideIPB: tdGuestLink,
				}, sameAll(linkOut))
			},
			wantPass: false,
			wantLines: []string{
				"GOIP_PARITY_FAIL link_show",
				"finding: L1 transaction-count txn-count: ip=1 goip=2",
				"GOIP_PARITY_UNGATED_CLEAN",
			},
			wantAbsent: []string{
				"GOIP_PARITY_PASS link_show",
				"GOIP_PARITY_UNGATED_DIVERGENCES",
			},
		},
		{
			description: "negative: a notification-bearing goip capture renders GOIP_PARITY_HYGIENE_FAIL and names the count",
			setup: func(t *testing.T, dir string) {
				writeTriple(t, dir, linkShow, map[string]string{
					SideIPA: tdGuestLink, SideGoip: tdEvents, SideIPB: tdGuestLink,
				}, sameAll(linkOut))
			},
			wantPass: false,
			wantLines: []string{
				"GOIP_PARITY_FAIL link_show",
				"hygiene:goip:notifications",
				"multicast notifications in the capture window",
				"GOIP_PARITY_HYGIENE_FAIL 2",
			},
			// The level, the class and the locus prefix are all the word
			// `hygiene`, and the report used to print it three times running:
			// Render's own "hygiene: " prefix, then Divergence.String's level,
			// then its class, then the locus. Two survive on purpose — the
			// level, and the locus's own prefix, which is what makes a locus
			// self-describing when it is quoted away from its finding. Three
			// was the defect.
			wantAbsent: []string{
				"hygiene hygiene hygiene",
				"GOIP_PARITY_HYGIENE_PASS",
			},
		},
		{
			description: "negative: a goip that rendered nothing is reported per stdout facet, with the set facets unsuppressed",
			setup: func(t *testing.T, dir string) {
				writeTriple(t, dir, linkShow, sameAll(tdGuestLink), map[string]string{
					SideIPA: linkOut, SideGoip: "", SideIPB: linkOut,
				})
			},
			wantPass: false,
			wantLines: []string{
				// FAIL and not WARN because `link show` is gated, and that is
				// the consequence of gating worth having written down: the
				// qlen entry below has to keep suppressing, or this run goes
				// red on a rendering difference that was accepted on purpose.
				// The row stays on `link show` regardless, because the
				// allow-suppressed assertion is the point of it and that
				// entry exists for this command only.
				"GOIP_PARITY_FAIL link_show",
				"finding: stdout presence stdout:lines:",
				"finding: stdout presence stdout:ifnames:",
				// The one committed stdout allowlist entry for this command,
				// printed rather than hidden. Its locus is spelled the way
				// StdoutLoci derives it, which is what makes it match at all.
				"allow-suppressed: stdout value stdout:keyword:qlen:",
			},
			wantAbsent: []string{"GOIP_PARITY_PASS link_show"},
		},
		{
			description: "corner: the transaction-count sentinel is printed on a clean command, not only on a failing one",
			setup: func(t *testing.T, dir string) {
				writeTriple(t, dir, linkShow, sameAll(tdGuestLink), sameAll(linkOut))
			},
			wantPass:  false, // addr show is missing
			wantLines: []string{"txns: ip=", "goip="},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			dir := t.TempDir()
			tt.setup(t, dir)
			var b bytes.Buffer
			got := Render(&b, CompareDir(dir, al))
			if got != tt.wantPass {
				t.Errorf("Render returned %v, want %v\n%s", got, tt.wantPass, b.String())
			}
			for _, want := range tt.wantLines {
				if !strings.Contains(b.String(), want) {
					t.Errorf("output lacks %q\n%s", want, b.String())
				}
			}
			for _, absent := range tt.wantAbsent {
				if strings.Contains(b.String(), absent) {
					t.Errorf("output contains %q, which it should not\n%s",
						absent, b.String())
				}
			}
		})
	}
}

// TestRenderNilAllowlist records what a nil allowlist does, because the
// in-guest binary loads the embedded one and a test that never exercised nil
// would leave the branch undefined.
//
// go test ./internal/goipparity/ -run TestRenderNilAllowlist
func TestRenderNilAllowlist(t *testing.T) {
	linkOut := mustRead(t, tdGuestLinkOut)
	linkShow, _ := Lookup("link show")

	tests := []struct {
		description string
		al          *nlparity.Allowlist
		check       func(t *testing.T, r Result)
	}{
		{
			description: "positive: a clean triple is still clean with no allowlist, since nothing needed suppressing",
			al:          nil,
			check: func(t *testing.T, r Result) {
				if r.Status != StatusPass {
					t.Errorf("status = %s, want PASS", r.Status)
				}
			},
		},
		{
			description: "boundary: a nil allowlist gates nothing, so it cannot turn a finding into a failure",
			al:          nil,
			check: func(t *testing.T, r Result) {
				if r.Netlink.Gated {
					t.Error("Gated is true with a nil allowlist")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			dir := t.TempDir()
			writeTriple(t, dir, linkShow, sameAll(tdGuestLink), sameAll(linkOut))
			for _, r := range CompareDir(dir, tt.al) {
				if r.Command.Name == "link show" {
					tt.check(t, r)
					return
				}
			}
			t.Fatal("no result for `link show`")
		})
	}
}
