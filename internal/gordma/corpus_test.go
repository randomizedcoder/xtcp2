package gordma

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// corpusRoot is pkg/xtcpnl's testdata, from this package's directory.
const corpusRoot = "../../pkg/xtcpnl/testdata"

// corpusKernels are the kernels with an RDMA corpus, as pkg/xtcpnl's
// rdmaKernels lists them. TestCorpusKernels checks the list against the
// directories, so a new capture cannot be skipped here.
var corpusKernels = []string{"6_1_177", "6_6_144", "6_12_96", "6_18_39", "7_1_4", "7_2_0_rc3"}

func TestCorpusKernels(t *testing.T) {
	dirs, err := filepath.Glob(filepath.Join(corpusRoot, "*", "rdma"))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, d := range dirs {
		got = append(got, filepath.Base(filepath.Dir(d)))
	}
	slices.Sort(got)
	want := slices.Sorted(slices.Values(corpusKernels))
	if !slices.Equal(got, want) {
		t.Fatalf("testdata/*/rdma is %v, corpusKernels %v", got, want)
	}
}

// replayResult is what a replay of one capture produced.
type replayResult struct {
	stdout, stderr string
	rc, pending    int
}

// replay runs gordma with args against capture, with proc as the proc root.
func replay(t *testing.T, capture, proc string, args ...string) replayResult {
	t.Helper()
	src, err := NewReplaySource(capture)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	rc := run(config{src: src, proc: proc}, args, &stdout, &stderr)
	return replayResult{stdout: stdout.String(), stderr: stderr.String(), rc: rc, pending: src.Pending()}
}

// sidecar reads testdata/<k>/rdma/<slug><ext>.
func sidecar(t *testing.T, k, slug, ext string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(corpusRoot, k, "rdma", slug+ext))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

var commRe = regexp.MustCompile(`pid (\d+) comm (\S+)`)

// fakeProc is a proc root with a <pid>/comm for each "pid N comm X" in k's
// sidecars: rdma read those names from the capture VM's /proc, the one
// input of its output that is not on the wire.
func fakeProc(t *testing.T, k string) string {
	t.Helper()
	outs, err := filepath.Glob(filepath.Join(corpusRoot, k, "rdma", "*.out"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	// os.Root keeps the writes inside dir whatever a sidecar holds.
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	}()
	for _, o := range outs {
		b, err := os.ReadFile(o)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range commRe.FindAllStringSubmatch(string(b), -1) {
			if err := root.Mkdir(m[1], 0o750); err != nil && !errors.Is(err, os.ErrExist) {
				t.Fatal(err)
			}
			if err := root.WriteFile(m[1]+"/comm", []byte(m[2]+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	return dir
}

var userCQRe = regexp.MustCompile(`(?m)^dev \S+ cqn (\d+) .* pid \d+ `)

// userCQN is the capture's `nlcap rdma-user-cqn`: res_cq_user's first
// user-owned CQ, which res_cq_cqn asked for.
func userCQN(t *testing.T, k string) string {
	t.Helper()
	m := userCQRe.FindStringSubmatch(sidecar(t, k, "res_cq_user", ".out"))
	if m == nil {
		t.Fatalf("%s: no user CQ in res_cq_user.out", k)
	}
	return m[1]
}

// TestGordmaMatchesCapturedSidecars replays every corpus capture of every
// kernel through gordma with the arguments rdma was run with
// (nix/microvms/scripts/capture-rdma.exp). Replay itself checks that each
// request is rdma's, byte for byte; every recorded request must be asked
// for; and stdout, stderr and the exit code must be rdma's .out, .err and
// .rc, except where expected says otherwise.
func TestGordmaMatchesCapturedSidecars(t *testing.T) {
	const rawRefused = "gordma: -r (the _RAW resource dumps) is not implemented\n"
	type deviation struct {
		stderr  string // replaces .err when set
		rc      int    // replaces .rc when set
		pending int
	}
	tests := []struct {
		description string
		slug        string
		args        []string
		expected    *deviation // nil: exactly the sidecars
	}{
		{description: "positive: dev show", slug: "dev_show", args: []string{"dev", "show"}},
		{description: "positive: dev show of one device", slug: "dev_show_one", args: []string{"dev", "show", "rxe0"}},
		{description: "positive: dev show as JSON", slug: "dev_show_json", args: []string{"-j", "dev", "show"}},
		{description: "positive: link show", slug: "link_show", args: []string{"link", "show"}},
		{description: "positive: link show of one port", slug: "link_show_one", args: []string{"link", "show", "rxe0/1"}},
		{description: "positive: res summary", slug: "res_show", args: []string{"res", "show"}},
		{description: "positive: res summary with the holders running", slug: "res_show_busy", args: []string{"res", "show"}},
		{description: "positive: statistic of a port", slug: "stat_show", args: []string{"statistic", "show", "link", "rxe0/1"}},
		{description: "positive: system show", slug: "sys_show", args: []string{"system", "show"}},
		{description: "positive: the kernel's QPs, one dump per port", slug: "res_qp", args: []string{"res", "show", "qp"}},
		{description: "positive: the kernel's CQ", slug: "res_cq", args: []string{"res", "show", "cq"}},
		{description: "positive: the kernel's PD", slug: "res_pd", args: []string{"res", "show", "pd"}},
		{description: "positive: the QPs as JSON", slug: "res_qp_json", args: []string{"-j", "res", "show", "qp"}},
		{description: "positive: user QPs", slug: "res_qp_user", args: []string{"res", "show", "qp"}},
		{description: "positive: user CQs", slug: "res_cq_user", args: []string{"res", "show", "cq"}},
		{description: "positive: an MR", slug: "res_mr", args: []string{"res", "show", "mr"}},
		{description: "positive: user PDs", slug: "res_pd_user", args: []string{"res", "show", "pd"}},
		{description: "positive: contexts", slug: "res_ctx", args: []string{"res", "show", "ctx"}},
		{description: "positive: an SRQ and its QP range", slug: "res_srq", args: []string{"res", "show", "srq"}},
		{description: "positive: a listening CM ID", slug: "res_cm_id", args: []string{"res", "show", "cm_id"}},
		{description: "positive: a CQ by id, its doit", slug: "res_cq_cqn", args: []string{"res", "show", "cq", "cqn", "<cqn>"}},
		{description: "positive: monitor, or the refusal of a kernel without MONITOR_MODE", slug: "monitor_add",
			args: []string{"monitor"}},
		{description: "negative: an unknown device", slug: "dev_missing", args: []string{"dev", "show", "nosuch"}},
		{description: "negative: a port out of range", slug: "link_missing", args: []string{"link", "show", "rxe0/9"}},
		{description: "negative: -r, refused before the _RAW dump rdma sent", slug: "res_qp_raw",
			args: []string{"-r", "res", "show", "qp"}, expected: &deviation{stderr: rawRefused, rc: 1, pending: 1}},
		{description: "boundary: an empty MR dump", slug: "res_mr_empty", args: []string{"res", "show", "mr"}},
		{description: "corner: an lqpn doit refused, then the port's dump", slug: "res_qp_lqpn",
			args: []string{"res", "show", "qp", "link", "rxe0/1", "lqpn", "1"}},
	}
	for _, k := range corpusKernels {
		t.Run(k, func(t *testing.T) {
			proc := fakeProc(t, k)
			covered := map[string]bool{}
			for _, tc := range tests {
				covered[tc.slug] = true
				t.Run(tc.description, func(t *testing.T) {
					args := slices.Clone(tc.args)
					for i, a := range args {
						if a == "<cqn>" {
							args[i] = userCQN(t, k)
						}
					}
					got := replay(t, filepath.Join(corpusRoot, k, "rdma", tc.slug+".strace"), proc, args...)
					wantRC, err := strconv.Atoi(strings.TrimSpace(sidecar(t, k, tc.slug, ".rc")))
					if err != nil {
						t.Fatal(err)
					}
					if wantRC == 143 {
						// rdma monitor was stopped by SIGTERM; the replay's
						// event stream just ends.
						wantRC = 0
					}
					want := replayResult{stdout: sidecar(t, k, tc.slug, ".out"), stderr: sidecar(t, k, tc.slug, ".err"), rc: wantRC}
					if d := tc.expected; d != nil {
						want.stderr, want.rc, want.pending = d.stderr, d.rc, d.pending
					}
					if got != want {
						t.Fatalf("gordma %s\n got %#v\nwant %#v", strings.Join(args, " "), got, want)
					}
					if slices.Contains(args, "-j") && !json.Valid([]byte(got.stdout)) {
						t.Fatalf("not JSON: %q", got.stdout)
					}
				})
			}
			t.Run("boundary: every capture has a row", func(t *testing.T) {
				captures, err := filepath.Glob(filepath.Join(corpusRoot, k, "rdma", "*.strace"))
				if err != nil {
					t.Fatal(err)
				}
				for _, c := range captures {
					if slug := strings.TrimSuffix(filepath.Base(c), ".strace"); !covered[slug] {
						t.Errorf("%s has no row", slug)
					}
				}
				if len(captures) != len(tests) {
					t.Errorf("%d captures, %d rows", len(captures), len(tests))
				}
			})
		})
	}
}

// TestGordmaReplayGuards pins that a replay fails, rc 2, when gordma asks
// for something rdma did not.
func TestGordmaReplayGuards(t *testing.T) {
	dir := filepath.Join(corpusRoot, "7_1_4", "rdma")
	tests := []struct {
		description string
		slug        string
		args        []string
		expected    replayResult // stderr is matched as a prefix
	}{
		{description: "positive: the capture's own command replays clean", slug: "dev_show",
			args: []string{"dev", "show"}, expected: replayResult{stdout: "0: rxe0: ", rc: 0}},
		{description: "negative: another command's request differs", slug: "dev_show",
			args: []string{"link", "show"}, expected: replayResult{stderr: "gordma: replay: request 1 differs", rc: 2, pending: 1}},
		{description: "negative: more requests than recorded", slug: "dev_missing",
			args: []string{"dev", "show"}, expected: replayResult{stderr: "gordma: replay: request 1 is past the 1", rc: 2}},
		{description: "boundary: an unknown object sends only rd_init's dump", slug: "dev_missing",
			args: []string{"nosuch"}, expected: replayResult{stderr: "Unknown object 'nosuch'.\n", rc: 0}},
		{description: "corner: an id filter's doit that rdma did not try", slug: "res_qp",
			args: []string{"res", "show", "qp", "lqpn", "1"}, expected: replayResult{stderr: "gordma: replay: request 1 differs", rc: 2, pending: 2}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := replay(t, filepath.Join(dir, tc.slug+".strace"), t.TempDir(), tc.args...)
			if got.rc != tc.expected.rc || got.pending != tc.expected.pending ||
				!strings.HasPrefix(got.stderr, tc.expected.stderr) || !strings.HasPrefix(got.stdout, tc.expected.stdout) {
				t.Fatalf("got %#v\nwant %#v", got, tc.expected)
			}
		})
	}
	t.Run("negative: a capture that does not exist", func(t *testing.T) {
		_, err := NewReplaySource(filepath.Join(dir, "nosuch.strace"))
		if !errors.Is(err, ErrReplay) {
			t.Fatalf("err %v, want ErrReplay", err)
		}
	})
}
