package gordmaparity

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/nlstrace"
)

// Status is a row's outcome.
type Status string

const (
	StatusPass    Status = "PASS"
	StatusFail    Status = "FAIL"
	StatusSkip    Status = "SKIP"
	StatusMissing Status = "MISSING"
)

// Result is one row's comparison.
type Result struct {
	Command  Command
	Status   Status
	Findings []string // why it failed, one line each
	Hygiene  []string // the captures themselves were not sound
	Missing  []string // the files that were not there
	// Noisy is how many comparisons the two rdma runs disagreed on, and
	// gordma matched one of them.
	Noisy int
}

// Report is a whole directory's comparison.
type Report struct {
	Results []Result
}

// Compared is how many rows were compared, PASS or FAIL.
func (r Report) Compared() int {
	n := 0
	for i := range r.Results {
		if x := &r.Results[i]; x.Status == StatusPass || x.Status == StatusFail {
			n++
		}
	}
	return n
}

// Pass is the verdict: something was compared, and no row failed or is
// missing.
func (r Report) Pass() bool {
	if r.Compared() == 0 {
		return false
	}
	for i := range r.Results {
		if x := &r.Results[i]; x.Status == StatusFail || x.Status == StatusMissing {
			return false
		}
	}
	return true
}

// CompareDir compares every row of the table in dir.
func CompareDir(dir string) Report { return Compare(dir, Commands()) }

// Compare compares cmds in dir.
func Compare(dir string, cmds []Command) Report {
	var r Report
	for _, c := range cmds {
		r.Results = append(r.Results, compareOne(dir, c))
	}
	return r
}

// The netlink message types that end a request's reply.
const (
	nlmsgError = 2
	nlmsgDone  = 3
)

// rcSIGTERM is the exit status of a tool killed by SIGTERM, which is how
// strace-monitor stops a monitor.
const rcSIGTERM = "143"

// devIndexRe is a monitor event's RDMA device index. Each side adds rxe1
// anew, and the kernel never reuses an index, so it differs on every side.
var devIndexRe = regexp.MustCompile(`\bdev \d+\b`)

// side is what one run left behind.
type side struct {
	out, err, rc string
	reqs         [][]byte // seq and pid zeroed
	events       int
}

func compareOne(dir string, c Command) Result {
	res := Result{Command: c}
	if !c.Implemented {
		res.Status = StatusSkip
		if _, err := os.Stat(filepath.Join(dir, c.FileName(SideGordma, "strace"))); err == nil {
			res.Status = StatusFail
			res.Findings = append(res.Findings, "the table says not implemented, but gordma was run")
		}
		return res
	}
	for _, s := range Sides() {
		for _, ext := range []string{"strace", "out", "err", "rc"} {
			res.missing(dir, c.FileName(s, ext))
		}
	}
	for _, ext := range []string{"out", "err", "rc"} {
		res.missing(dir, c.FileName(SideReplay, ext))
	}
	if len(res.Missing) != 0 {
		res.Status = StatusMissing
		return res
	}

	sides := map[string]side{}
	for _, s := range Sides() {
		sd, hyg := readSide(dir, c, s)
		res.Hygiene = append(res.Hygiene, hyg...)
		sides[s] = sd
	}
	a, g, b := sides[SideRdmaA], sides[SideGordma], sides[SideRdmaB]

	// L1: gordma's requests, in order, are rdma's.
	if len(g.reqs) != len(a.reqs) || len(a.reqs) != len(b.reqs) {
		res.control(len(a.reqs) == len(b.reqs), len(g.reqs) == len(a.reqs) || len(g.reqs) == len(b.reqs),
			fmt.Sprintf("L1 requests: gordma sent %d, rdma_a %d, rdma_b %d", len(g.reqs), len(a.reqs), len(b.reqs)))
	} else {
		for i := range a.reqs {
			res.control(bytes.Equal(a.reqs[i], b.reqs[i]),
				bytes.Equal(g.reqs[i], a.reqs[i]) || bytes.Equal(g.reqs[i], b.reqs[i]),
				fmt.Sprintf("L1 request %d differs:\n    gordma %x\n    rdma_a %x", i, g.reqs[i], a.reqs[i]))
		}
	}
	if c.Kind == KindMonitor {
		res.control(a.events == b.events, g.events == a.events || g.events == b.events,
			fmt.Sprintf("L1 events: gordma read %d, rdma_a %d, rdma_b %d", g.events, a.events, b.events))
	}

	// L2: what gordma printed, and its exit status, are rdma's.
	for _, f := range []struct {
		name    string
		a, g, b string
	}{{"stdout", a.out, g.out, b.out}, {"stderr", a.err, g.err, b.err}, {"rc", a.rc, g.rc, b.rc}} {
		res.control(f.a == f.b, f.g == f.a || f.g == f.b,
			fmt.Sprintf("L2 %s differs:\n    gordma %q\n    rdma_a %q", f.name, f.g, f.a))
	}

	// L3: gordma replaying rdma_a's capture prints exactly what rdma_a
	// printed. The replay is deterministic, so there is no control and no
	// masking; only the monitor's SIGTERM status is mapped, since a replayed
	// monitor ends on its own.
	wantRC := strings.TrimSpace(readFile(dir, c.FileName(SideRdmaA, "rc")))
	if c.Kind == KindMonitor && wantRC == rcSIGTERM {
		wantRC = "0"
	}
	for _, f := range []struct{ name, got, want string }{
		{"stdout", readFile(dir, c.FileName(SideReplay, "out")), readFile(dir, c.FileName(SideRdmaA, "out"))},
		{"stderr", readFile(dir, c.FileName(SideReplay, "err")), readFile(dir, c.FileName(SideRdmaA, "err"))},
		{"rc", strings.TrimSpace(readFile(dir, c.FileName(SideReplay, "rc"))), wantRC},
	} {
		if f.got != f.want {
			res.Findings = append(res.Findings, fmt.Sprintf("L3 replay %s differs:\n    replay %q\n    rdma_a %q", f.name, f.got, f.want))
		}
	}

	res.Status = StatusPass
	if len(res.Findings) != 0 || len(res.Hygiene) != 0 {
		res.Status = StatusFail
	}
	return res
}

// control records a comparison: clean is whether the two rdma runs agree,
// and ok whether gordma matched one of them. A mismatch is a finding; an
// agreement only with the run the other disagrees with is noise, counted.
func (r *Result) control(clean, ok bool, finding string) {
	switch {
	case !ok:
		r.Findings = append(r.Findings, finding)
	case !clean:
		r.Noisy++
	}
}

func (r *Result) missing(dir, name string) {
	if _, err := os.Stat(filepath.Join(dir, name)); errors.Is(err, fs.ErrNotExist) {
		r.Missing = append(r.Missing, name)
	}
}

func readFile(dir, name string) string {
	b, err := os.ReadFile(filepath.Join(dir, name)) //nolint:gosec // dir is the run's own capture directory
	if err != nil {
		return fmt.Sprintf("<%v>", err)
	}
	return string(b)
}

// readOutputs reads a captured side's .out, .err and .rc, monitor-normalized:
// the device index masked and the SIGTERM status mapped to 0.
func readOutputs(dir string, c Command, s string) side {
	sd := side{
		out: readFile(dir, c.FileName(s, "out")),
		err: readFile(dir, c.FileName(s, "err")),
		rc:  strings.TrimSpace(readFile(dir, c.FileName(s, "rc"))),
	}
	if c.Kind == KindMonitor {
		sd.out = devIndexRe.ReplaceAllString(sd.out, "dev N")
		if sd.rc == rcSIGTERM {
			sd.rc = "0"
		}
	}
	return sd
}

// readSide is readOutputs plus the side's capture, checked: it parses, and
// every transaction ends in NLMSG_DONE or NLMSG_ERROR.
func readSide(dir string, c Command, s string) (side, []string) {
	sd := readOutputs(dir, c, s)
	name := c.FileName(s, "strace")
	dgs, err := nlstrace.ParseFile(filepath.Join(dir, name))
	if err != nil {
		return sd, []string{fmt.Sprintf("%s: %v", name, err)}
	}
	txns, events := nlstrace.Transactions(dgs)
	sd.events = len(events)
	var hyg []string
	if len(txns) == 0 {
		hyg = append(hyg, name+": no request at all; every rdma run sends at least its device dump")
	}
	for i, tx := range txns {
		sd.reqs = append(sd.reqs, nlstrace.ZeroSeqPID(tx.Req))
		if !ends(tx.Recvs) {
			hyg = append(hyg, fmt.Sprintf("%s: transaction %d on fd %d has no NLMSG_DONE or NLMSG_ERROR", name, i, tx.Fd))
		}
	}
	return sd, hyg
}

// ends reports whether the last message of recvs ends a reply.
func ends(recvs [][]byte) bool {
	if len(recvs) == 0 {
		return false
	}
	msgs, err := nlstrace.Msgs([]nlstrace.Datagram{{B: recvs[len(recvs)-1]}})
	if err != nil || len(msgs) == 0 {
		return false
	}
	t := msgs[len(msgs)-1].Type
	return t == nlmsgDone || t == nlmsgError
}
