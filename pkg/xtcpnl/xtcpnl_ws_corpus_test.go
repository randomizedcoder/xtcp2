package xtcpnl

// The nl_mon capture workbench's corpus: testdata/<kernel>/ws/<row id>/, one
// directory per row of a rows file (nix/microvms/workbench/*.rows.jsonl),
// each holding the row's cmd.json (the row as written, the guest's kernel
// and iproute2, and the batch runner's result), ip.pcap/.rc/.out/.err (the
// command, inside the capture window) and one netlink_<proto>_<name>.pcap
// plus <name>.out per readback. See docs/netlink/coverage-status.md,
// "Workbench and generated rows".
//
// Every row is a table test: its desc and expect come from the rows file,
// and the oracles below hold the capture to them. They are generic on
// purpose. A rows file can hold thousands of generated rows, and none of
// them should need a hand-written test before it can fail.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

// wsRow is a row's cmd.json: the fields of the rows file the oracles read,
// and the batch runner's result.
type wsRow struct {
	ID       string     `json:"id"`
	Desc     string     `json:"desc"`
	Cmd      string     `json:"cmd"`
	Proto    string     `json:"proto"`
	Expect   wsExpect   `json:"expect"`
	Readback []wsRbSpec `json:"readback"`
	// Axes is what a generated row's values mean (tools/nlcapgen
	// matrix.go), which the families' O3 models read.
	Axes map[string]string `json:"axes"`
	Env  struct {
		Kernel   string `json:"kernel"`
		Iproute2 string `json:"iproute2"`
	} `json:"env"`
	Result struct {
		Status string `json:"status"`
		RC     *int   `json:"rc"`
	} `json:"result"`
}

type wsRbSpec struct {
	Name  string `json:"name"`
	Proto string `json:"proto"`
}

// wsExpect is a row's expected outcome. Every field is optional; an absent
// one is not checked. RefusedBy is "none" (rc 0, every request
// acknowledged), "kernel" (the kernel answered with an error), "iproute2"
// (a non-zero rc with no request sent) or "ack-then-fail" (a non-zero rc
// although every request was acknowledged), which is never accepted unless
// a row names it, or expects that non-zero rc: it means a tool reported a
// failure the kernel did not, which is a finding unless the row knew. tc
// does it whenever it refuses an argument after looking its device up,
// since the lookup is an acknowledged request (tdc's negative cases).
type wsExpect struct {
	RC        *int     `json:"rc"`
	Errno     string   `json:"errno"`
	Extack    string   `json:"extack"`
	Sent      *int     `json:"sent"`
	RefusedBy string   `json:"refused_by"`
	Match     *wsMatch `json:"match"`
}

// wsMatch is tdc's verify check: Pattern matches Count times in the
// readback's stdout (non-overlapping, as Python's re.findall counts).
type wsMatch struct {
	Readback string `json:"readback"`
	Pattern  string `json:"pattern"`
	Count    int    `json:"count"`
}

// wsMatchProblem holds a readback's stdout to its row's match.
func wsMatchProblem(m wsMatch, out string) error {
	re, err := regexp.Compile(m.Pattern)
	if err != nil {
		return fmt.Errorf("match pattern: %w", err)
	}
	if n := len(re.FindAllStringIndex(out, -1)); n != m.Count {
		return fmt.Errorf("%q matches %d times in %s.out, want %d", m.Pattern, n, m.Readback, m.Count)
	}
	return nil
}

// wsObserved is what a row's ip.pcap and ip.rc show.
type wsObserved struct {
	rc       int
	requests int
	errno    syscall.Errno
	extack   string
}

const (
	wsRefusedNone     = "none"
	wsRefusedKernel   = "kernel"
	wsRefusedIproute2 = "iproute2"
	wsRefusedAckFail  = "ack-then-fail"
)

// refusedBy classifies an observation; see wsExpect.
func (o wsObserved) refusedBy() string {
	switch {
	case o.errno != 0:
		return wsRefusedKernel
	case o.rc == 0:
		return wsRefusedNone
	case o.requests == 0:
		return wsRefusedIproute2
	default:
		return wsRefusedAckFail
	}
}

// wsRowProblem holds one observation to its expectation, and to the rules
// every row obeys whatever it expects: a tool that exits 0 must not have
// been refused, and one that exits non-zero after every request was
// acknowledged must say that is what it expected.
func wsRowProblem(exp wsExpect, obs wsObserved) error {
	var errs []error
	if exp.RC != nil && *exp.RC != obs.rc {
		errs = append(errs, fmt.Errorf("rc %d, want %d", obs.rc, *exp.RC))
	}
	if obs.rc == 0 && obs.errno != 0 {
		errs = append(errs, fmt.Errorf("rc 0 although the kernel refused with %s", unix.ErrnoName(obs.errno)))
	}
	got := obs.refusedBy()
	if exp.RefusedBy != "" && exp.RefusedBy != got {
		errs = append(errs, fmt.Errorf("refused by %s, want %s", got, exp.RefusedBy))
	}
	expectedFail := exp.RC != nil && *exp.RC == obs.rc
	if got == wsRefusedAckFail && exp.RefusedBy != wsRefusedAckFail && !expectedFail {
		errs = append(errs, fmt.Errorf("rc %d although every one of %d requests was acknowledged", obs.rc, obs.requests))
	}
	if exp.Errno != "" {
		name := "none"
		if obs.errno != 0 {
			name = unix.ErrnoName(obs.errno)
		}
		if name != exp.Errno {
			errs = append(errs, fmt.Errorf("errno %s, want %s", name, exp.Errno))
		}
	}
	if exp.Extack != "" && !strings.Contains(obs.extack, exp.Extack) {
		errs = append(errs, fmt.Errorf("extack %q, want it to contain %q", obs.extack, exp.Extack))
	}
	if exp.Sent != nil && *exp.Sent != obs.requests {
		errs = append(errs, fmt.Errorf("%d requests sent, want %d", obs.requests, *exp.Sent))
	}
	return errors.Join(errs...)
}

// wsObserve reads a row's command capture: the requests the tool sent, the
// kernel's first refusal and its extack message.
func wsObserve(t *testing.T, msgs []capturedNlMsg, rc int) wsObserved {
	t.Helper()
	obs := wsObserved{rc: rc}
	for _, m := range msgs {
		if m.h.Flags&unix.NLM_F_REQUEST != 0 {
			obs.requests++
		}
	}
	_, obs.errno = rowRequestTypes(t, msgs)
	for _, m := range msgs {
		if m.h.Type != unix.NLMSG_ERROR {
			continue
		}
		e, err := ParseNlMsgErr(m.h.Flags, m.body)
		if err != nil {
			t.Fatal(err)
		}
		if e.Errno != 0 {
			obs.extack = e.Msg
			break
		}
	}
	return obs
}

// wsRows returns every workbench row directory under testdata, by path.
func wsRows(t testing.TB) []string {
	t.Helper()
	var dirs []string
	roots, err := filepath.Glob(filepath.Join("testdata", "*", "ws"))
	if err != nil {
		t.Fatal(err)
	}
	for _, root := range roots {
		err := filepath.WalkDir(root, func(p string, d os.DirEntry, werr error) error {
			if werr != nil {
				return werr
			}
			if !d.IsDir() && d.Name() == "cmd.json" {
				dirs = append(dirs, filepath.Dir(p))
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	slices.Sort(dirs)
	return dirs
}

func wsLoadRow(t *testing.T, dir string) wsRow {
	t.Helper()
	bs, err := os.ReadFile(filepath.Join(dir, "cmd.json"))
	if err != nil {
		t.Fatal(err)
	}
	var r wsRow
	if err := json.Unmarshal(bs, &r); err != nil {
		t.Fatalf("%s: %v", dir, err)
	}
	return r
}

func wsReadRC(t *testing.T, dir string) int {
	t.Helper()
	bs, err := os.ReadFile(filepath.Join(dir, "ip.rc"))
	if err != nil {
		t.Fatal(err)
	}
	rc, err := strconv.Atoi(strings.TrimSpace(string(bs)))
	if err != nil {
		t.Fatalf("%s/ip.rc: %v", dir, err)
	}
	return rc
}

// TestWsCorpusRows runs every committed workbench row through the oracles:
// the row was captured whole (status OK, every file there), its readbacks
// parse, and its rc, refusal, errno, extack and request count are what its
// rows file said.
func TestWsCorpusRows(t *testing.T) {
	dirs := wsRows(t)
	if len(dirs) == 0 {
		t.Fatal("no workbench rows under testdata/*/ws")
	}
	prefixes := []string{"positive:", "negative:", "boundary:", "corner:"}
	for _, dir := range dirs {
		row := wsLoadRow(t, dir)
		t.Run(row.Env.Kernel+"/"+row.ID, func(t *testing.T) {
			if !strings.HasSuffix(filepath.ToSlash(dir), "/ws/"+row.ID) {
				t.Fatalf("%s holds row %s", dir, row.ID)
			}
			if !slices.ContainsFunc(prefixes, func(p string) bool { return strings.HasPrefix(row.Desc, p) }) {
				t.Errorf("desc %q has no positive:/negative:/boundary:/corner: class", row.Desc)
			}
			if row.Result.Status != "OK" {
				t.Fatalf("committed with status %s", row.Result.Status)
			}
			rc := wsReadRC(t, dir)
			if row.Result.RC == nil || *row.Result.RC != rc {
				t.Errorf("cmd.json result rc %v, ip.rc %d", row.Result.RC, rc)
			}
			obs := wsObserve(t, pcapNlMsgs(t, filepath.Join(dir, "ip.pcap")), rc)
			if err := wsRowProblem(row.Expect, obs); err != nil {
				t.Errorf("%s\n  cmd: %s", err, row.Cmd)
			}
			for _, rb := range row.Readback {
				proto := rb.Proto
				if proto == "" {
					proto = "route"
				}
				pcapNlMsgs(t, filepath.Join(dir, "netlink_"+proto+"_"+rb.Name+".pcap"))
				if _, err := os.Stat(filepath.Join(dir, rb.Name+".out")); err != nil {
					t.Error(err)
				}
			}
			if m := row.Expect.Match; m != nil {
				out, err := os.ReadFile(filepath.Join(dir, m.Readback+".out"))
				if err != nil {
					t.Fatal(err)
				}
				if err := wsMatchProblem(*m, string(out)); err != nil {
					t.Error(err)
				}
			}
		})
	}
}

// TestWsRowProblem holds the oracle itself to hand-built observations, so
// that a corpus row passing means the oracle looked, not that it could not
// fail.
func TestWsRowProblem(t *testing.T) {
	zero, one, two := 0, 1, 2
	cases := []struct {
		description string
		exp         wsExpect
		obs         wsObserved
		expected    string // "" for no problem, else a substring of it
	}{
		{"positive: an acknowledged write", wsExpect{RC: &zero, Sent: &one, RefusedBy: wsRefusedNone},
			wsObserved{rc: 0, requests: 1}, ""},
		{"positive: a kernel refusal with its errno and extack", wsExpect{RC: &two, Errno: "EEXIST", Extack: "exists", RefusedBy: wsRefusedKernel},
			wsObserved{rc: 2, requests: 1, errno: unix.EEXIST, extack: "Link already exists"}, ""},
		{"positive: iproute2 refuses before sending", wsExpect{RC: &one, Sent: &zero, RefusedBy: wsRefusedIproute2},
			wsObserved{rc: 1}, ""},
		{"negative: the rc differs", wsExpect{RC: &zero}, wsObserved{rc: 2, errno: unix.EINVAL}, "rc 2, want 0"},
		{"negative: rc 0 despite a kernel refusal", wsExpect{}, wsObserved{rc: 0, requests: 1, errno: unix.EINVAL}, "rc 0 although the kernel refused with EINVAL"},
		{"negative: a refused command that sent a request", wsExpect{Sent: &zero, RefusedBy: wsRefusedIproute2},
			wsObserved{rc: 1, requests: 1, errno: unix.EINVAL}, "refused by kernel, want iproute2"},
		{"negative: a non-zero rc after every request was acknowledged", wsExpect{},
			wsObserved{rc: 1, requests: 2}, "rc 1 although every one of 2 requests was acknowledged"},
		{"negative: the wrong errno", wsExpect{Errno: "EEXIST"}, wsObserved{rc: 2, requests: 1, errno: unix.ENODEV}, "errno ENODEV, want EEXIST"},
		{"negative: an errno expected, none came", wsExpect{Errno: "EEXIST"}, wsObserved{rc: 0, requests: 1}, "errno none, want EEXIST"},
		{"negative: the extack is missing", wsExpect{Extack: "policy"}, wsObserved{rc: 2, requests: 1, errno: unix.EINVAL}, `extack "", want it to contain "policy"`},
		{"negative: the tool sent twice", wsExpect{Sent: &one}, wsObserved{rc: 0, requests: 2}, "2 requests sent, want 1"},
		{"boundary: nothing expected, a clean ack", wsExpect{}, wsObserved{rc: 0, requests: 1}, ""},
		{"boundary: nothing expected, nothing sent, rc 0", wsExpect{}, wsObserved{rc: 0}, ""},
		{"corner: ack-then-fail, named by the row", wsExpect{RefusedBy: wsRefusedAckFail}, wsObserved{rc: 1, requests: 1}, ""},
		{"corner: ack-then-fail with the rc the row expects (a lookup, then a refused argument)", wsExpect{RC: &one},
			wsObserved{rc: 1, requests: 1}, ""},
		{"negative: ack-then-fail with another non-zero rc", wsExpect{RC: &two},
			wsObserved{rc: 1, requests: 1}, "rc 1 although every one of 1 requests was acknowledged"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			err := wsRowProblem(tc.exp, tc.obs)
			switch {
			case tc.expected == "" && err != nil:
				t.Fatalf("unexpected problem: %v", err)
			case tc.expected != "" && err == nil:
				t.Fatalf("no problem, want %q", tc.expected)
			case tc.expected != "" && !strings.Contains(err.Error(), tc.expected):
				t.Fatalf("problem %q, want it to contain %q", err, tc.expected)
			}
		})
	}
}

// TestWsMatchProblem holds the match oracle to hand-built output.
func TestWsMatchProblem(t *testing.T) {
	out := "qdisc fq 1: root refcnt 2 limit 10000p\nqdisc fq 2: parent 1:1 limit 10000p\n"
	cases := []struct {
		description string
		m           wsMatch
		expected    string // "" for no problem, else a substring of it
	}{
		{"positive: the count matches", wsMatch{Readback: "verify", Pattern: "qdisc fq [0-9]+:", Count: 2}, ""},
		{"negative: one match too few", wsMatch{Readback: "verify", Pattern: "root refcnt", Count: 2}, "matches 1 times in verify.out, want 2"},
		{"boundary: count 0, absent", wsMatch{Readback: "verify", Pattern: "htb", Count: 0}, ""},
		{"negative: count 0, present", wsMatch{Readback: "verify", Pattern: "limit", Count: 0}, "matches 2 times"},
		{"corner: matches do not overlap", wsMatch{Readback: "verify", Pattern: "0+", Count: 2}, ""},
		{"negative: a pattern that does not compile", wsMatch{Readback: "verify", Pattern: "(", Count: 0}, "match pattern"},
		{"corner: (?s) lets .* cross lines, as tdc's re.DOTALL does", wsMatch{Readback: "verify", Pattern: "(?sm)root.*parent", Count: 1}, ""},
		{"negative: without (?s), .* stops at the line's end", wsMatch{Readback: "verify", Pattern: "root.*parent", Count: 1}, "matches 0 times"},
		{"corner: (?m) anchors ^ at each line, as re.MULTILINE does", wsMatch{Readback: "verify", Pattern: "(?sm)^qdisc", Count: 2}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			err := wsMatchProblem(tc.m, out)
			switch {
			case tc.expected == "" && err != nil:
				t.Fatalf("unexpected problem: %v", err)
			case tc.expected != "" && err == nil:
				t.Fatalf("no problem, want %q", tc.expected)
			case tc.expected != "" && !strings.Contains(err.Error(), tc.expected):
				t.Fatalf("problem %q, want it to contain %q", err, tc.expected)
			}
		})
	}
}

// TestWsSelftestTableMax is the semantic check on the selftest's boundary
// table: a table id that does not fit rtm_table's byte travels in
// RTA_TABLE. The two directions disagree about the header, and both are
// pinned: iproute2 sends RT_TABLE_UNSPEC (ip/iproute.c, iproute_modify),
// and the kernel answers RT_TABLE_COMPAT (net/ipv6/route.c, rt6_fill_node).
func TestWsSelftestTableMax(t *testing.T) {
	dirs := slices.DeleteFunc(wsRows(t), func(d string) bool {
		return !strings.HasSuffix(filepath.ToSlash(d), "/ws/selftest/route6-table-max")
	})
	if len(dirs) == 0 {
		t.Fatal("no selftest/route6-table-max row")
	}
	cases := []struct {
		description string
		file        string
		typ         uint16
		request     bool
		expected    string
	}{
		{"positive: the request carries UNSPEC and RTA_TABLE", "ip.pcap", unix.RTM_NEWROUTE, true, "hdr 0 table 4294967295"},
		{"positive: the dump answers with COMPAT and RTA_TABLE", "netlink_route_getroute.pcap", unix.RTM_NEWROUTE, false, "hdr 252 table 4294967295"},
	}
	for _, dir := range dirs {
		for _, tc := range cases {
			t.Run(filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(dir))))+"/"+tc.description, func(t *testing.T) {
				var got []string
				for _, m := range pcapNlMsgs(t, filepath.Join(dir, tc.file)) {
					if m.h.Type != tc.typ || (m.h.Flags&unix.NLM_F_REQUEST != 0) != tc.request {
						continue
					}
					r, err := ParseNewRoute(m.body)
					if err != nil {
						t.Fatal(err)
					}
					got = append(got, fmt.Sprintf("hdr %d table %d", m.body[4], r.Table))
				}
				if len(got) != 1 || got[0] != tc.expected {
					t.Fatalf("got %q, want [%s]", got, tc.expected)
				}
			})
		}
	}
}
