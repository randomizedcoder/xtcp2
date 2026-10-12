package gordma

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"net/netip"
	"path/filepath"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlstrace"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// corpusSource answers any request one of 7.1.4's captures made with what
// rdma read, whatever order it comes in, and counts the requests.
type corpusSource struct {
	replies map[string][]Msg
	events  []Msg
	sent    int
}

func newCorpusSource(t *testing.T) *corpusSource {
	t.Helper()
	captures, err := filepath.Glob(filepath.Join(corpusRoot, "7_1_4", "rdma", "*.strace"))
	if err != nil {
		t.Fatal(err)
	}
	s := &corpusSource{replies: map[string][]Msg{}}
	for _, c := range captures {
		dgs, err := nlstrace.ParseFile(c)
		if err != nil {
			t.Fatal(err)
		}
		r := newReplay(dgs)
		for _, tx := range r.txs {
			s.replies[string(zeroSeqPID(tx.req))] = tx.msgs
		}
		if len(r.events) != 0 {
			s.events = r.events
		}
	}
	return s
}

func (s *corpusSource) Talk(req []byte) (Reply, error) {
	s.sent++
	msgs, ok := s.replies[string(zeroSeqPID(req))]
	if !ok {
		return Reply{}, fmt.Errorf("%w: no capture made %x", ErrReplay, zeroSeqPID(req))
	}
	return Reply{Msgs: msgs}, nil
}

func (s *corpusSource) Listen(_ uint32, fn func(Msg) bool) (unix.Errno, error) {
	for _, m := range s.events {
		if !fn(m) {
			break
		}
	}
	return 0, nil
}

func runCorpus(t *testing.T, args ...string) (replayResult, int) {
	t.Helper()
	src := newCorpusSource(t)
	var stdout, stderr bytes.Buffer
	rc := run(config{src: src, proc: t.TempDir()}, args, &stdout, &stderr)
	return replayResult{stdout: stdout.String(), stderr: stderr.String(), rc: rc}, src.sent
}

const (
	devLine71  = "0: rxe0: node_type ca protocol roce node_guid 0000:00ff:fe00:5200 sys_image_guid 0000:00ff:fe00:5200 \n"
	devJSON71  = `[{"ifindex":0,"ifname":"rxe0","node_type":"ca","protocol":"roce","node_guid":"0000:00ff:fe00:5200","sys_image_guid":"0000:00ff:fe00:5200"}` + "\n]\n"
	sysLine71  = "netns shared privileged-qkey off monitor on copy-on-fork on\n"
	linkLine71 = "link rxe0/1 state ACTIVE physical_state LINK_UP netdev rx0 \n"
	// resLine71 is res_show's and res_show_busy's line up to its counts:
	// both captures made the same request, and corpusSource keeps one.
	resLine71   = "0: rxe0: pd "
	helpPrefix  = "Usage: rdma [ OPTIONS ] OBJECT { COMMAND | help }\n"
	versionLine = Version + "\n"
)

// TestRdmaArgs runs the option and object parsing against 7.1.4's
// replies. sent counts requests, rd_init's dump included.
func TestRdmaArgs(t *testing.T) {
	tests := []struct {
		description string
		args        []string
		expected    replayResult // stdout and stderr are matched as prefixes
		sent        int
	}{
		{description: "positive: an option before the object", args: []string{"-j", "dev", "show"},
			expected: replayResult{stdout: devJSON71}, sent: 2},
		{description: "positive: an option after the object, by permutation", args: []string{"dev", "show", "-j"},
			expected: replayResult{stdout: devJSON71}, sent: 2},
		{description: "positive: a long option", args: []string{"--json", "dev"},
			expected: replayResult{stdout: devJSON71}, sent: 2},
		{description: "positive: prefixes, d s", args: []string{"d", "s"}, expected: replayResult{stdout: devLine71}, sent: 2},
		{description: "positive: prefixes, l l", args: []string{"l", "l"}, expected: replayResult{stdout: linkLine71}, sent: 2},
		{description: "positive: prefixes, r", args: []string{"r"}, expected: replayResult{stdout: resLine71}, sent: 2},
		{description: "positive: prefixes, stat show link", args: []string{"stat", "show", "link", "rxe0/1"},
			expected: replayResult{stdout: "link rxe0/1 \n    sent_pkts 0 "}, sent: 2},
		{description: "negative: an unknown object is reported, rc 0", args: []string{"foo"},
			expected: replayResult{stderr: "Unknown object 'foo'.\n"}, sent: 1},
		{description: "negative: an unknown dev command", args: []string{"dev", "frob"},
			expected: replayResult{stderr: "Unknown dev command 'frob'.\n"}, sent: 1},
		{description: "negative: an unknown option prints help, rc 1", args: []string{"-x", "dev"},
			expected: replayResult{stdout: helpPrefix, stderr: "Unknown option.\n", rc: 1}},
		{description: "negative: -b is refused", args: []string{"-b", "batch.txt"},
			expected: replayResult{stderr: "gordma: -b (batch) is not implemented\n", rc: 1}},
		{description: "negative: -b without its file", args: []string{"-b"},
			expected: replayResult{stderr: "-b option requires an argument\n", rc: 1}},
		{description: "negative: a write verb is refused after rd_init", args: []string{"link", "add", "x", "type", "rxe", "netdev", "y"},
			expected: replayResult{stderr: "gordma: link add is not implemented\n", rc: 1}, sent: 1},
		{description: "negative: -dd is refused", args: []string{"-d", "-d", "res", "show", "qp"},
			expected: replayResult{stderr: "gordma: -dd (driver details) is not implemented\n", rc: 1}},
		{description: "boundary: no arguments print help after rd_init", args: nil,
			expected: replayResult{stdout: helpPrefix}, sent: 1},
		{description: "boundary: -V prints the version and sends nothing", args: []string{"-V", "dev"},
			expected: replayResult{stdout: versionLine}},
		{description: "boundary: a long option prefix, --vers", args: []string{"--vers"},
			expected: replayResult{stdout: versionLine}},
		{description: "boundary: -help is -h, for cli-help-smoke", args: []string{"-help"},
			expected: replayResult{stdout: helpPrefix}},
		{description: "corner: s is system, which rdma lists before statistic", args: []string{"s"},
			expected: replayResult{stdout: sysLine71}, sent: 2},
		{description: "corner: -j does not reach system show", args: []string{"-j", "system", "show"},
			expected: replayResult{stdout: sysLine71}, sent: 2},
		{description: "corner: -- ends the options", args: []string{"--", "-j"},
			expected: replayResult{stderr: "Unknown object '-j'.\n"}, sent: 1},
		{description: "corner: a long option given a value it does not take", args: []string{"--json=1"},
			expected: replayResult{stdout: helpPrefix, stderr: "Unknown option.\n", rc: 1}},
		{description: "corner: res show DEV is that device's summary", args: []string{"res", "show", "rxe0"},
			expected: replayResult{stdout: resLine71}, sent: 2},
		{description: "corner: a filter without its value", args: []string{"res", "show", "qp", "lqpn"},
			expected: replayResult{stderr: "No filter data was supplied to filter option lqpn\n", rc: 1}, sent: 1},
		{description: "corner: an unsupported filter", args: []string{"res", "show", "cq", "lqpn", "1"},
			expected: replayResult{stderr: "Unsupported filter option: lqpn\n", rc: 1}, sent: 1},
		{description: "corner: a numeric filter given letters", args: []string{"res", "show", "cq", "cqn", "x"},
			expected: replayResult{stderr: "cqn filter accepts \"1234567890,-\" characters only\n", rc: 1}, sent: 1},
		{description: "corner: monitor prints the captured events", args: []string{"monitor"},
			expected: replayResult{stdout: "[REGISTER]\tdev 1 rxe1\n[NETDEV_ATTACH]\tdev 1 rxe1 port 1 netdev 3 rx1\n"}, sent: 2},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, sent := runCorpus(t, tc.args...)
			if got.rc != tc.expected.rc || sent != tc.sent ||
				!strings.HasPrefix(got.stdout, tc.expected.stdout) || !strings.HasPrefix(got.stderr, tc.expected.stderr) ||
				(tc.expected.stderr == "" && got.stderr != "") {
				t.Fatalf("got %#v, sent %d\nwant %#v, sent %d", got, sent, tc.expected, tc.sent)
			}
		})
	}
}

// testRD is an rd that prints into a buffer.
func testRD(json, pretty bool) (*rd, *bytes.Buffer) {
	var b bytes.Buffer
	return &rd{p: &printer{w: bufio.NewWriter(&b), sl: "\n", json: json, pretty: pretty}, stderr: &b, procRoot: "/nonexistent"}, &b
}

func flushed(t *testing.T, r *rd, b *bytes.Buffer) string {
	t.Helper()
	if err := r.p.w.Flush(); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

var (
	devRxe0 = xtcpnl.RdmaDev{DevIndex: 0, HasDevIndex: true, DevName: "rxe0", HasDevName: true,
		NodeType: 1, HasNodeType: true, DevProtocol: "roce", HasDevProtocol: true,
		NodeGUID: 0x000000fffe005200, HasNodeGUID: true, SysImageGUID: 0x000000fffe005200, HasSysImageGUID: true}
	portRxe0 = xtcpnl.RdmaPort{
		RdmaHandle: xtcpnl.RdmaHandle{DevName: "rxe0", HasDevName: true, HasDevIndex: true, PortIndex: 1, HasPortIndex: true},
		RdmaNetdev: xtcpnl.RdmaNetdev{NdevIndex: 4, HasNdevIndex: true, NdevName: "rx0", HasNdevName: true},
		State:      4, HasState: true, PhysState: 5, HasPhysState: true}
	kernelQP = xtcpnl.RdmaResQP{PortIndex: 1, HasPortIndex: true, LQPN: 1, HasLQPN: true, SQPSN: 0, HasSQPSN: true,
		Type: 1, HasType: true, State: 3, HasState: true,
		RdmaResOwner: xtcpnl.RdmaResOwner{KernName: "ib_core", HasKernName: true}}
)

// TestRenderText pins the text renderers against hand-built xtcpnl
// values.
func TestRenderText(t *testing.T) {
	with := func(d xtcpnl.RdmaDev, f func(*xtcpnl.RdmaDev)) xtcpnl.RdmaDev { f(&d); return d }
	withPort := func(p xtcpnl.RdmaPort, f func(*xtcpnl.RdmaPort)) xtcpnl.RdmaPort { f(&p); return p }
	withQP := func(q xtcpnl.RdmaResQP, f func(*xtcpnl.RdmaResQP)) xtcpnl.RdmaResQP { f(&q); return q }
	tests := []struct {
		description string
		details     bool
		render      func(r *rd) error
		expected    string
		expectedErr error
	}{
		{description: "positive: dev", render: func(r *rd) error { return r.devLine(devRxe0) }, expected: devLine71},
		{description: "positive: link", render: func(r *rd) error { return r.linkLine(portRxe0) }, expected: linkLine71},
		{description: "positive: sys", render: func(r *rd) error {
			r.sysLine(xtcpnl.RdmaSys{NetnsMode: 1, HasNetnsMode: true, HasPrivilegedQKeyMode: true, MonitorMode: 1,
				HasMonitorMode: true, CopyOnFork: 1, HasCopyOnFork: true})
			return nil
		}, expected: sysLine71},
		{description: "positive: a kernel QP", render: func(r *rd) error { r.portIdx = 1; return r.qpLine("rxe0", 0, kernelQP) },
			expected: "link rxe0/1 lqpn 1 type GSI state RTS sq-psn 0 comm [ib_core] \n"},
		{description: "positive: a CM ID with AF_INET addresses", render: func(r *rd) error {
			r.portIdx = 1
			return r.cmidLine("rxe0", 0, xtcpnl.RdmaResCMID{PortIndex: 1, HasPortIndex: true, PS: rdmaPSTCP, HasPS: true,
				State: 8, HasState: true, HasCMIDN: true,
				Src: xtcpnl.RdmaSockaddr{Family: unix.AF_INET, Port: 7174, Addr: netip.MustParseAddr("10.99.0.1")}, HasSrc: true,
				Dst: xtcpnl.RdmaSockaddr{Family: unix.AF_INET, Addr: netip.MustParseAddr("0.0.0.0")}, HasDst: true})
		}, expected: "link rxe0/1 cm-idn 0 state LISTEN ps TCP src-addr 10.99.0.1:7174 dst-addr 0.0.0.0:0 \n"},
		{description: "negative: a link without a netdev has no netdev field",
			render: func(r *rd) error {
				return r.linkLine(withPort(portRxe0, func(p *xtcpnl.RdmaPort) { p.RdmaNetdev = xtcpnl.RdmaNetdev{} }))
			}, expected: "link rxe0/1 state ACTIVE physical_state LINK_UP \n"},
		{description: "negative: a dev without fw has no fw field; with one, after protocol",
			render: func(r *rd) error {
				return r.devLine(with(devRxe0, func(d *xtcpnl.RdmaDev) { d.FwVersion, d.HasFwVersion = "1.0", true }))
			}, expected: "0: rxe0: node_type ca protocol roce fw 1.0 node_guid 0000:00ff:fe00:5200 sys_image_guid 0000:00ff:fe00:5200 \n"},
		{description: "negative: a dev without its name is not printed",
			render:   func(r *rd) error { return r.devLine(with(devRxe0, func(d *xtcpnl.RdmaDev) { d.HasDevName = false })) },
			expected: "", expectedErr: errCB},
		{description: "negative: a CM ID of another family is not printed", render: func(r *rd) error {
			return r.cmidLine("rxe0", 0, xtcpnl.RdmaResCMID{HasPS: true, HasState: true,
				Src: xtcpnl.RdmaSockaddr{Family: 17}, HasSrc: true})
		}, expected: ""},
		{description: "boundary: GUID word order", render: func(r *rd) error {
			return r.devLine(xtcpnl.RdmaDev{HasDevIndex: true, DevName: "d", HasDevName: true,
				NodeGUID: 0x1122334455667788, HasNodeGUID: true})
		}, expected: "0: d: node_guid 1122:3344:5566:7788 \n"},
		{description: "boundary: a stat with no counters", render: func(r *rd) error {
			return r.statLine(xtcpnl.RdmaStat{RdmaHandle: portRxe0.RdmaHandle})
		}, expected: "link rxe0/1 \n"},
		{description: "boundary: an SRQ's QPs, a lone one and a range", render: func(r *rd) error {
			return r.srqLine("rxe0", 0, xtcpnl.RdmaResSRQ{HasSRQN: true, QPs: []xtcpnl.RdmaQPRange{{Min: 5, Max: 5}, {Min: 7, Max: 22}}})
		}, expected: "dev rxe0 srqn 0 type BASIC lqpn 5,7-22 \n"},
		{description: "boundary: -d adds adaptive-moderation and the caps",
			details: true, render: func(r *rd) error {
				return r.devLine(with(devRxe0, func(d *xtcpnl.RdmaDev) {
					d.HasDIM, d.CapFlags, d.HasCapFlags = true, 1<<0|1<<16|1<<32, true
				}))
			}, expected: strings.TrimSuffix(devLine71, "\n") + "adaptive-moderation off \n    caps: <RESIZE_MAX_WR, UNKNOWN, SG_GAPS_REG>\n"},
		{description: "corner: an unknown port state", render: func(r *rd) error {
			return r.linkLine(withPort(portRxe0, func(p *xtcpnl.RdmaPort) { p.State = 9 }))
		}, expected: "link rxe0/1 state UNKNOWN physical_state LINK_UP netdev rx0 \n"},
		{description: "corner: a user owner whose comm cannot be read has none", render: func(r *rd) error {
			r.portIdx = 1
			return r.qpLine("rxe0", 0, withQP(kernelQP, func(q *xtcpnl.RdmaResQP) {
				q.RdmaResOwner = xtcpnl.RdmaResOwner{PID: 100, HasPID: true}
			}))
		}, expected: "link rxe0/1 lqpn 1 type GSI state RTS sq-psn 0 pid 100 \n"},
		{description: "corner: a QP of another port is skipped", render: func(r *rd) error { return r.qpLine("rxe0", 0, kernelQP) },
			expected: ""},
		{description: "corner: a key prints the low 32 bits, as print_hex does", render: func(r *rd) error {
			return r.mrLine("rxe0", 0, xtcpnl.RdmaResMR{IOVA: 0x1_0000_0abc, HasIOVA: true, MRLen: 4096, HasMRLen: true})
		}, expected: "dev rxe0 iova 0xabc mrlen 4096 \n"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			r, b := testRD(false, false)
			r.showDetails = tc.details
			err := tc.render(r)
			if got := flushed(t, r, b); got != tc.expected {
				t.Fatalf("got %q, %v\nwant %q", got, err, tc.expected)
			}
			if !errors.Is(err, tc.expectedErr) {
				t.Fatalf("err %v, want %v", err, tc.expectedErr)
			}
		})
	}
}

// TestRenderJSON pins the JSON renderers. The empty and two-device rows
// are what jsonw produces, which no capture shows: rxe0 is the corpus's
// only device.
func TestRenderJSON(t *testing.T) {
	devs := func(n int) func(r *rd) {
		return func(r *rd) {
			r.p.newJSONObj()
			for range n {
				if err := r.devLine(devRxe0); err != nil {
					t.Fatal(err)
				}
			}
			r.p.deleteJSONObj()
		}
	}
	one := strings.TrimSuffix(strings.TrimPrefix(devJSON71, "["), "\n]\n")
	tests := []struct {
		description string
		json        bool
		pretty      bool
		render      func(r *rd)
		expected    string
	}{
		{description: "positive: one device, as the capture", json: true, render: devs(1), expected: devJSON71},
		{description: "positive: a link has netdev_index without -d", json: true, render: func(r *rd) {
			r.p.newJSONObj()
			if err := r.linkLine(portRxe0); err != nil {
				t.Fatal(err)
			}
			r.p.deleteJSONObj()
		}, expected: `[{"ifindex":0,"ifname":"rxe0","port":1,"state":"ACTIVE","physical_state":"LINK_UP","netdev":"rx0","netdev_index":4}` + "\n]\n"},
		{description: "negative: without -j the writer never opens", render: devs(1), expected: devLine71},
		{description: "boundary: no devices", json: true, render: devs(0), expected: "[]\n"},
		{description: "corner: two devices", json: true, render: devs(2), expected: "[" + one + "\n," + one + "\n]\n"},
		{description: "corner: -p indents", json: true, pretty: true, render: func(r *rd) {
			r.p.newJSONObj()
			r.p.openObject()
			r.p.printUint(printAny, "ifindex", "", 0)
			r.p.printOnOff(printAny, "monitor", "", true)
			r.p.closeObject()
			r.p.newline()
			r.p.deleteJSONObj()
		}, expected: "[ {\n        \"ifindex\": 0,\n        \"monitor\": true\n    }\n ]\n"},
		{description: "corner: strings are escaped as jsonw_puts does", json: true, render: func(r *rd) {
			r.p.newJSONObj()
			r.p.printString(printAny, "", "", "a\"b\\c\t\x01\x7f\xe9")
			r.p.deleteJSONObj()
		}, expected: `["a\"b\\c\t\u0001\u007f` + "\xe9\"]\n"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			r, b := testRD(tc.json, tc.pretty)
			tc.render(r)
			if got := flushed(t, r, b); got != tc.expected {
				t.Fatalf("got %q\nwant %q", got, tc.expected)
			}
		})
	}
}

// TestReplaySource pins the replay's matching.
func TestReplaySource(t *testing.T) {
	capture := filepath.Join(corpusRoot, "7_1_4", "rdma", "dev_show.strace")
	dump := xtcpnl.BuildRdmaDevDumpRequest(0)
	tests := []struct {
		description string
		capture     string
		reqs        [][]byte
		expected    []int // replies per Talk; -1 for ErrReplay
	}{
		{description: "positive: rdma's requests, in order", capture: capture,
			reqs: [][]byte{dump, xtcpnl.BuildRdmaDevGetRequest(0, 0)}, expected: []int{2, 2}},
		{description: "negative: a changed DEV_INDEX", capture: capture,
			reqs: [][]byte{dump, xtcpnl.BuildRdmaDevGetRequest(1, 0)}, expected: []int{2, -1}},
		{description: "negative: one request more than recorded", capture: capture,
			reqs: [][]byte{dump, xtcpnl.BuildRdmaDevGetRequest(0, 0), dump}, expected: []int{2, 2, -1}},
		{description: "boundary: an empty dump is its NLMSG_DONE alone",
			capture:  filepath.Join(corpusRoot, "7_1_4", "rdma", "res_mr_empty.strace"),
			reqs:     [][]byte{dump, mustBuild(xtcpnl.BuildRdmaResDumpRequest(xtcpnl.RdmaNldevCmdResMRGet, 0, 0, 0))},
			expected: []int{2, 1}},
		{description: "corner: seq and pid are not compared", capture: capture,
			reqs: [][]byte{withPID(xtcpnl.BuildRdmaDevDumpRequest(99), 7)}, expected: []int{2}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			s, err := NewReplaySource(tc.capture)
			if err != nil {
				t.Fatal(err)
			}
			for i, req := range tc.reqs {
				rep, err := s.Talk(req)
				switch {
				case tc.expected[i] < 0:
					if !errors.Is(err, ErrReplay) || !strings.Contains(err.Error(), fmt.Sprintf("request %d ", i)) {
						t.Fatalf("Talk %d: err %v, want ErrReplay naming request %d", i, err, i)
					}
				case err != nil || len(rep.Msgs) != tc.expected[i]:
					t.Fatalf("Talk %d: %d replies, %v; want %d", i, len(rep.Msgs), err, tc.expected[i])
				}
			}
		})
	}
}

func mustBuild(b []byte, err error) []byte {
	if err != nil {
		panic(err)
	}
	return b
}

func withPID(b []byte, pid byte) []byte {
	b[12] = pid
	return b
}

// TestFilters pins rdma's filter matching.
func TestFilters(t *testing.T) {
	tests := []struct {
		description string
		filters     []filterEntry
		check       func(r *rd) bool
		expected    bool
	}{
		{description: "positive: a number in a list", filters: []filterEntry{{key: "lqpn", value: "3,17"}},
			check: func(r *rd) bool { return r.isFiltered("lqpn", 17, true) }, expected: false},
		{description: "positive: a key prefix names the filter", filters: []filterEntry{{key: "lq", value: "17"}},
			check: func(r *rd) bool { return r.isFiltered("lqpn", 18, true) }, expected: true},
		{description: "positive: a string list, case-insensitive", filters: []filterEntry{{key: "type", value: "ud,rc"}},
			check: func(r *rd) bool { return r.isStringFiltered("type", "RC") }, expected: false},
		{description: "negative: a number not in the list", filters: []filterEntry{{key: "lqpn", value: "3,17"}},
			check: func(r *rd) bool { return r.isFiltered("lqpn", 4, true) }, expected: true},
		{description: "negative: an absent attribute the user filters on", filters: []filterEntry{{key: "pid", value: "1"}},
			check: func(r *rd) bool { return r.isFiltered("pid", 0, false) }, expected: true},
		{description: "boundary: a range's ends match, as numbers of the list", filters: []filterEntry{{key: "lqpn", value: "3-5"}},
			check: func(r *rd) bool {
				return !r.isFiltered("lqpn", 3, true) && !r.isFiltered("lqpn", 5, true) && r.isFiltered("lqpn", 6, true)
			}, expected: true},
		{description: "boundary: no filters filter nothing",
			check: func(r *rd) bool { return r.isFiltered("lqpn", 4, false) || r.isStringFiltered("type", "RC") }, expected: false},
		{description: "corner: a string filter checks the default of an absent attribute",
			filters: []filterEntry{{key: "path-mig-state", value: "migrated"}},
			check:   func(r *rd) bool { return r.isStringFiltered("path-mig-state", pathMigStr(0)) }, expected: false},
		{description: "corner: an empty list token matches nothing", filters: []filterEntry{{key: "type", value: ",,"}},
			check: func(r *rd) bool { return r.isStringFiltered("type", "") }, expected: true},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			r := &rd{filters: tc.filters}
			if got := tc.check(r); got != tc.expected {
				t.Fatalf("got %v, want %v", got, tc.expected)
			}
		})
	}
}

// TestPortFromArg is get_port_from_argv's table, from rdma/utils.c.
func TestPortFromArg(t *testing.T) {
	type res struct {
		port         uint32
		dumpAll, bad bool
	}
	tests := []struct {
		description string
		arg         string
		strict      bool
		expected    res
	}{
		{description: "positive: dev/1", arg: "mlx5_1/1", expected: res{port: 1}},
		{description: "positive: a bare dev dumps every port", arg: "mlx5_1", expected: res{dumpAll: true}},
		{description: "negative: dev/0 is malformed", arg: "mlx5_1/0", expected: res{bad: true}},
		{description: "negative: strict needs a port", arg: "mlx5_1", strict: true, expected: res{bad: true}},
		{description: "boundary: dev/ dumps every port", arg: "mlx5_1/", expected: res{dumpAll: true}},
		{description: "boundary: a port past int saturates to 0xffffffff", arg: "d/99999999999999999999",
			expected: res{port: 0xffffffff}},
		{description: "corner: dev/- is no port, and not a dump", arg: "mlx5_1/-", expected: res{}},
		{description: "corner: dev/- under strict is malformed", arg: "mlx5_1/-", strict: true, expected: res{bad: true}},
		{description: "corner: atoi stops at the first non-digit", arg: "d/2x", expected: res{port: 2}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			p, d, b := portFromArg(tc.arg, tc.strict)
			if got := (res{p, d, b}); got != tc.expected {
				t.Fatalf("got %+v, want %+v", got, tc.expected)
			}
		})
	}
}

// TestNtop6 pins glibc's inet_ntop6 rules.
func TestNtop6(t *testing.T) {
	tests := []struct {
		description string
		addr        string
		expected    string
	}{
		{description: "positive: loopback", addr: "::1", expected: "::1"},
		{description: "positive: a global address", addr: "2001:db8::1", expected: "2001:db8::1"},
		{description: "negative: a lone zero word is not compressed", addr: "2001:db8:0:1:1:1:1:1", expected: "2001:db8:0:1:1:1:1:1"},
		{description: "boundary: the unspecified address", addr: "::", expected: "::"},
		{description: "corner: v4-mapped ends dotted", addr: "::ffff:10.99.0.1", expected: "::ffff:10.99.0.1"},
		{description: "corner: v4-compatible ends dotted, unlike netip", addr: "::10.99.0.1", expected: "::10.99.0.1"},
		{description: "corner: the first of two equal runs", addr: "1:0:0:1:0:0:1:1", expected: "1::1:0:0:1:1"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := ntop6(netip.MustParseAddr(tc.addr).As16()); got != tc.expected {
				t.Fatalf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

// TestStrerror pins the perror wording.
func TestStrerror(t *testing.T) {
	tests := []struct {
		description string
		errno       unix.Errno
		expected    string
	}{
		{description: "positive: EINVAL, res_qp_raw's", errno: unix.EINVAL, expected: "Invalid argument"},
		{description: "positive: ENOENT", errno: unix.ENOENT, expected: "No such file or directory"},
		{description: "negative: EOPNOTSUPP", errno: unix.EOPNOTSUPP, expected: "Operation not supported"},
		{description: "boundary: 0 is glibc's Success", errno: 0, expected: "Success"},
		{description: "corner: EPROTO, a sequence mismatch", errno: unix.EPROTO, expected: "Protocol error"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := strerror(tc.errno); got != tc.expected {
				t.Fatalf("got %q, want %q", got, tc.expected)
			}
		})
	}
}
