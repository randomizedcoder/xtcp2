package gobridge

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// TestGobridgeBatch runs batches (`-batch -` on stdin unless the args name
// a file) over scripted replies: what each line prints, how failures end
// the batch, and the state one line leaves the next (B28-B32, B36).
//
// go test ./internal/gobridge/ -run TestGobridgeBatch
func TestGobridgeBatch(t *testing.T) {
	t.Parallel()
	hdr := col("port") + "  " + "vlan-id  " + "\n"
	pad := strings.Repeat(" ", 18)
	d1 := func() Msg { return port(4, "d1", spec(vinfo(0, 10))) }
	d2 := func() Msg { return port(5, "d2", spec(vinfo(0, 30))) }
	vlans := func() Reply { return rep(d1(), d2(), done()) }
	links := func() Reply { return rep(port(4, "d1"), port(5, "d2"), done()) }
	link4 := "4: d1: <BROADCAST,UP,LOWER_UP> mtu 1500 master br0 state forwarding priority 32 cost 100 \n"
	link5 := "5: d2: <BROADCAST,UP,LOWER_UP> mtu 1500 master br0 state forwarding priority 32 cost 100 \n"
	missing := filepath.Join(t.TempDir(), "nosuch")
	words := func(n int) string { return "link show" + strings.Repeat(" x", n) + "\n" }
	type want struct {
		stdout, stderr string
		rc, reqs       int
	}
	tests := []struct {
		description string
		args        string
		stdin       string
		replies     []Reply
		expected    want
	}{
		// Positive.
		{"positive: two lines, one ll_init_map", "-batch -", "link show\nvlan show\n",
			[]Reply{cache(), links(), vlans()},
			want{stdout: link4 + link5 + hdr + col("d1") + "  10\n" + col("d2") + "  30\n", reqs: 3}},
		{"positive: comments, a blank line, a continuation", "-batch -", "# ports\n\nlink \\\nshow dev d1 # one\n",
			[]Reply{cache(), links()}, want{stdout: link4, reqs: 2}},
		{"positive: -force with nothing failing", "-force -batch -", "link show\n",
			[]Reply{cache(), links()}, want{stdout: link4 + link5, reqs: 2}},
		{"positive: the last line needs no newline", "-batch -", "link show dev d2",
			[]Reply{cache(), links()}, want{stdout: link5, reqs: 2}},
		// Negative.
		{"negative: a failed line stops the batch", "-batch -", "badobj\nlink show\n", nil,
			want{stderr: "Object \"badobj\" is unknown, try \"bridge help\".\nCommand failed -:1\n", rc: 1}},
		{"negative: -force goes on, rc 1", "-force -batch -", "badobj\nlink show\n", []Reply{cache(), links()},
			want{stdout: link4 + link5, stderr: "Object \"badobj\" is unknown, try \"bridge help\".\nCommand failed -:1\n", rc: 1, reqs: 2}},
		{"negative: exit(1) of a failed dump ends a -force batch (B30)", "-force -batch -", "vni show dev d1\nlink show\n",
			[]Reply{cache(), rep(doneErr(unix.EINVAL, "The device is not a vxlan device"))},
			want{stdout: col("dev") + "  " + col17("vni") + "  group/remote\n",
				stderr: "Error: The device is not a vxlan device.\nDump ternminated\n", rc: 1, reqs: 2}},
		{"negative: an unterminated quote exits before the line runs", "-batch -", "link show dev \"d1\nlink show\n", nil,
			want{stderr: "Unterminated quoted string\n", rc: 1}},
		{"negative: no such batch file", "-batch " + missing, "", nil,
			want{stderr: "Cannot open file \"" + missing + "\" for reading: No such file or directory\n", rc: 1}},
		{"negative: an empty file name is still opened, as freopen(\"\")", "-batch ", "", nil,
			want{stderr: "Cannot open file \"\" for reading: No such file or directory\n", rc: 1}},
		{"negative: a write is refused, ending the batch (gobridge)", "-force -batch -", "vlan add dev d1 vid 5\nlink show\n",
			[]Reply{cache()}, want{stderr: "gobridge: not implemented: bridge vlan add\n", rc: 255, reqs: 1}},
		// Boundary.
		{"boundary: an empty batch", "-batch -", "", nil, want{}},
		{"boundary: 511 words fit", "-batch -", words(509), []Reply{cache(), links()}, want{stdout: link4 + link5, reqs: 2}},
		{"boundary: the 512th word exits 1", "-batch -", words(510), nil,
			want{stderr: "Too many arguments to command\n", rc: 1}},
		{"boundary: Command failed counts physical lines", "-batch -", "\n# c\nlink \\\nshow \\\nbogus\nbadobj\n",
			[]Reply{cache(), links()},
			want{stdout: link4 + link5, stderr: "Object \"badobj\" is unknown, try \"bridge help\".\nCommand failed -:6\n", rc: 1, reqs: 2}},
		// Corner.
		{"corner: B28 vlan's dev filter carries to the next line", "-batch -", "vlan show dev d1\nvlan show\n",
			[]Reply{cache(), vlans(), vlans()},
			want{stdout: hdr + col("d1") + "  10\n" + hdr + col("d1") + "  10\n", reqs: 3}},
		{"corner: B28 each object has its own: mst after vlan dev is unfiltered", "-batch -", "vlan show dev d1\nmst show\n",
			[]Reply{cache(), vlans(), rep(port(4, "d1", spec(at(xtcpnl.IflaBridgeMst|unix.NLA_F_NESTED, mstEntry(0, 3, false)...))),
				port(5, "d2", spec(at(xtcpnl.IflaBridgeMst|unix.NLA_F_NESTED, mstEntry(0, 3, false)...))), done())},
			want{stdout: hdr + col("d1") + "  10\n" + col("port") + "  msti     \n" +
				col("d1") + "  0\n" + pad + "  state forwarding \n" + col("d2") + "  0\n" + pad + "  state forwarding \n", reqs: 3}},
		{"corner: B28 link's master filter carries over", "-batch -", "link show master br0\nlink show\n",
			[]Reply{cache(), links(), rep(port(4, "d1"), link(unix.AF_BRIDGE, 7, up, name("x1"), at(unix.IFLA_MASTER, u32(9)...)), done())},
			want{stdout: link4 + link5 + link4, reqs: 3}},
		{"corner: B29 a second vid line is a duparg exit through -force", "-force -batch -", "vlan show vid 10\nvlan show vid 30\nlink show\n",
			[]Reply{cache(), vlans()},
			want{stdout: hdr + col("d1") + "  10\n", stderr: "Error: duplicate \"vid\": \"30\" is the second value.\n", rc: 255, reqs: 2}},
		{"corner: a failed name clears the filter before nodev", "-force -batch -", "vlan show dev d1\nvlan show dev nosuch\nvlan show\n",
			[]Reply{cache(), vlans(), rep(nlErr(unix.ENODEV)), vlans()},
			want{stdout: hdr + col("d1") + "  10\n" + hdr + col("d1") + "  10\n" + col("d2") + "  30\n",
				stderr: "Cannot find device \"nosuch\"\nCommand failed -:2\n", rc: 1, reqs: 4}},
		{"corner: B4 mst's failed dump is a return, so -force goes on", "-force -batch -", "mst show\nlink show\n",
			[]Reply{cache(), rep(port(4, "d1"), done()), links()},
			want{stdout: col("port") + "  msti     \n" + link4 + link5, stderr: "Dump terminated\nCommand failed -:1\n", rc: 1, reqs: 3}},
		{"corner: B31 a missing continuation ends the batch with rc 0", "-batch -", "link show\nvlan show \\\n",
			[]Reply{cache(), links()}, want{stdout: link4 + link5, stderr: "Missing continuation line\n", reqs: 2}},
		{"corner: B32 words after the file are ignored", "-batch - vlan show", "link show dev d1\n",
			[]Reply{cache(), links()}, want{stdout: link4, reqs: 2}},
		{"corner: B36 -j prints one array per command that gets that far", "-j -force -batch -", "link show dev d1\nmst show dev nosuch\nvlan show dev d2\n",
			[]Reply{cache(), links(), rep(nlErr(unix.ENODEV)), vlans()},
			want{stdout: `[{"ifindex":4,"ifname":"d1","flags":["BROADCAST","UP","LOWER_UP"],"mtu":1500,"master":"br0","state":"forwarding","priority":32,"cost":100}]` +
				"\n" + `[{"ifname":"d2","vlans":[{"vlan":30}]}]` + "\n", stderr: "Cannot find device \"nosuch\"\nCommand failed -:2\n", rc: 1, reqs: 4}},
		{"corner: a NUL ends the line", "-batch -", "link show dev d1\x00 dev d2\n", []Reply{cache(), links()},
			want{stdout: link4, reqs: 2}},
		{"corner: a closing quote ends a word with no space after", "-batch -", "link show dev \"d1\"master br0\n",
			[]Reply{cache(), links()}, want{stdout: link4, reqs: 2}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Parallel()
			src := &fakeSource{replies: tc.replies}
			var out, errb bytes.Buffer
			args := strings.Fields(tc.args)
			if tc.args == "-batch " {
				args = []string{"-batch", ""} // Fields drops the empty name
			}
			rc := run(config{src: src, stdin: strings.NewReader(tc.stdin)}, args, &out, &errb)
			got := want{stdout: out.String(), stderr: errb.String(), rc: rc, reqs: len(src.reqs)}
			if got != tc.expected {
				t.Errorf("bridge %s <<< %q\n got  %#v\n want %#v", tc.args, tc.stdin, got, tc.expected)
			}
		})
	}
}

// TestMakeargs pins lib/utils.c makeargs.
//
// go test ./internal/gobridge/ -run TestMakeargs
func TestMakeargs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		description string
		line        string
		expected    string // the words joined by |, or the error and stderr
	}{
		{"positive: words", "link show dev d1\n", "link|show|dev|d1"},
		{"positive: double quotes", `link show dev "d 1"`, "link|show|dev|d 1"},
		{"positive: single quotes", `link show dev 'd1'`, "link|show|dev|d1"},
		{"negative: unterminated", `link "d1`, "exit 1: Unterminated quoted string\n"},
		{"negative: 512 words", strings.Repeat("x ", 512), "exit 1: Too many arguments to command\n"},
		{"boundary: empty", "", ""},
		{"boundary: only whitespace", " \t\r\n", ""},
		{"boundary: 511 words", strings.TrimSpace(strings.Repeat("x ", 511)), strings.TrimSuffix(strings.Repeat("x|", 511), "|")},
		{"corner: an empty quoted word", `a "" b`, "a||b"},
		{"corner: a quote mid-word is literal", `a"b c"`, `a"b|c"`},
		{"corner: the other quote inside a quote", `"it's"`, "it's"},
		{"corner: text after a closing quote is a new word", `"a"b`, "a|b"},
		{"corner: a quote as the last character", `a "`, "exit 1: Unterminated quoted string\n"},
		{"corner: CR is whitespace", "link\rshow\r\n", "link|show"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Parallel()
			var errb bytes.Buffer
			r := &runner{stderr: &errb}
			argv, err := r.makeargs(tc.line)
			got := strings.Join(argv, "|")
			if err != nil {
				got = err.Error() + ": " + errb.String()
			}
			if got != tc.expected {
				t.Errorf("makeargs(%q) = %q, want %q", tc.line, got, tc.expected)
			}
		})
	}
}

// TestGetcmdline pins lib/utils.c getcmdline over a whole input: each line
// as read, with its line number.
//
// go test ./internal/gobridge/ -run TestGetcmdline
func TestGetcmdline(t *testing.T) {
	t.Parallel()
	tests := []struct {
		description string
		input       string
		expected    string // "N:line" for each, then any stderr
	}{
		{"positive: two lines", "a\nb\n", "1:a\n|2:b\n|"},
		{"positive: a comment is cut", "a # c\n", "1:a |"},
		{"positive: a continuation joins", "a \\\nb\n", "2:a b\n|"},
		{"negative: a missing continuation", "a \\\n", "Missing continuation line\n"},
		{"boundary: empty input", "", ""},
		{"boundary: a last line without newline", "a", "1:a|"},
		{"boundary: a blank line", "\n", "1:\n|"},
		{"corner: a comment hides the backslash", "a # \\\nb\n", "1:a |2:b\n|"},
		{"corner: the next line's comment is cut too", "a \\\nb # c\nd\n", "2:a b |3:d\n|"},
		{"corner: two continuations", "a\\\nb\\\nc\n", "3:abc\n|"},
		{"corner: backslash CR LF is no continuation", "a\\\r\nb\n", "1:a\\\r\n|2:b\n|"},
		{"corner: a NUL ends the line", "a\x00b\n", "1:a|"},
		{"corner: a NUL hides the continuation", "a\x00\\\nb\n", "1:a|2:b\n|"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Parallel()
			var errb bytes.Buffer
			r := &runner{stderr: &errb}
			br := bufio.NewReader(strings.NewReader(tc.input))
			var b strings.Builder
			n := 0
			for {
				line, ok := r.getcmdline(br, &n)
				if !ok {
					break
				}
				fmt.Fprintf(&b, "%d:%s|", n, line)
			}
			if got := b.String() + errb.String(); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

// TestGobridgeOptions pins -family, -4, -6 and -netns. -netns opens names
// under a temporary NETNS_RUN_DIR; switching for real needs root and a
// namespace, which the live run checks (6X-L). Not parallel: it sets
// netnsRunDir, which the parallel tests never read.
//
// go test ./internal/gobridge/ -run TestGobridgeOptions
func TestGobridgeOptions(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plain"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	old := netnsRunDir
	netnsRunDir = dir
	t.Cleanup(func() { netnsRunDir = old })
	usage := "Usage: bridge [ OPTIONS ] OBJECT { COMMAND | help }\n"
	type want struct {
		stdout, stderr string
		rc, reqs       int
	}
	link4 := "4: d1: <BROADCAST,UP,LOWER_UP> mtu 1500 master br0 state forwarding priority 32 cost 100 \n"
	tests := []struct {
		description string
		args        string
		replies     []Reply
		expected    want
	}{
		// Positive.
		{"positive: -family inet changes nothing", "-family inet link show", []Reply{cache(), rep(port(4, "d1"), done())},
			want{stdout: link4, reqs: 2}},
		{"positive: -family inet6", "-family inet6 link show", []Reply{cache(), rep(port(4, "d1"), done())},
			want{stdout: link4, reqs: 2}},
		{"positive: -4", "-4 link show", []Reply{cache(), rep(port(4, "d1"), done())}, want{stdout: link4, reqs: 2}},
		{"positive: --6 is -6", "--6 link show", []Reply{cache(), rep(port(4, "d1"), done())}, want{stdout: link4, reqs: 2}},
		{"positive: -V prints bridge's version line, no iproute2- (B37)", "-V", nil, want{stdout: "bridge utility, 7.2.0\n"}},
		{"positive: -Version exits 0 before any request", "-Version link show", nil, want{stdout: "bridge utility, 7.2.0\n"}},
		// Negative.
		{"negative: an unknown family", "-family ipx link show", nil,
			want{stderr: "Error: argument \"ipx\" is wrong: invalid protocol family\n", rc: 255}},
		{"negative: -family with no value", "-family", nil, want{stderr: usage, rc: 255}},
		{"negative: -family help", "-family help", nil, want{stderr: usage, rc: 255}},
		{"negative: no such namespace", "-netns nosuch link show", nil,
			want{stderr: "Cannot open network namespace \"nosuch\": No such file or directory\n", rc: 255}},
		{"negative: a file that isn't a namespace", "-netns plain link show", nil,
			want{stderr: "setting the network namespace \"plain\" failed: Invalid argument\n", rc: 255}},
		{"negative: -batch with no file", "-batch", nil, want{stderr: usage, rc: 255}},
		// Boundary.
		{"boundary: -4 with no object prints the usage", "-4", nil, want{stderr: usage, rc: 255}},
		// Corner.
		{"corner: B33 -netns with no name opens (null)", "-netns", nil,
			want{stderr: "Cannot open network namespace \"(null)\": No such file or directory\n", rc: 255}},
		{"corner: -f is -family, tested before -force", "-f link show", nil,
			want{stderr: "Error: argument \"link\" is wrong: invalid protocol family\n", rc: 255}},
		{"corner: -n is -netns", "-n nosuch link", nil,
			want{stderr: "Cannot open network namespace \"nosuch\": No such file or directory\n", rc: 255}},
		{"corner: -4 needs the exact word", "-46 link show", nil,
			want{stderr: "Option \"-46\" is unknown, try \"bridge help\".\n", rc: 255}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := &fakeSource{replies: tc.replies}
			var out, errb bytes.Buffer
			rc := run(config{src: src}, strings.Fields(tc.args), &out, &errb)
			got := want{stdout: out.String(), stderr: errb.String(), rc: rc, reqs: len(src.reqs)}
			if strings.HasPrefix(tc.expected.stderr, "Usage:") && strings.HasPrefix(got.stderr, tc.expected.stderr) {
				got.stderr = tc.expected.stderr
			}
			if got != tc.expected {
				t.Errorf("bridge %s\n got  %#v\n want %#v", tc.args, got, tc.expected)
			}
		})
	}
}

// TestRouterPortStats pins br_print_router_port_stats under -s on crafted
// router ports: the timer's aligned text (B34), the type, and short
// attributes (B35).
//
// go test ./internal/gobridge/ -run TestRouterPortStats
func TestRouterPortStats(t *testing.T) {
	t.Parallel()
	hdr := col("port") + "  " + "vlan-id  " + "\n"
	pad := strings.Repeat(" ", 20)
	glob := func(ports ...[]byte) []Reply {
		return []Reply{cache(), rep(vlanMsg(3, at(xtcpnl.BridgeVlandbGlobalOptions, cat(
			at(xtcpnl.BridgeVlandbGoptsID, u16(10)...),
			at(goptsRouterPorts|unix.NLA_F_NESTED, at(xtcpnl.MdbaRouter, cat(ports...)...)...))...)), done())}
	}
	rp := func(ifindex uint32, attrs ...[]byte) []byte {
		return at(xtcpnl.MdbaRouterPort, append(u32(ifindex), cat(attrs...)...)...)
	}
	timer := func(v uint32) []byte { return at(xtcpnl.MdbaRouterPattrTimer, u32(v)...) }
	typ := func(v uint8) []byte { return at(xtcpnl.MdbaRouterPattrType, v) }
	type want struct {
		stdout, stderr string
		rc             int
	}
	tests := []struct {
		description string
		args        string
		replies     []Reply
		expected    want
	}{
		{"positive: two ports, timer and type", "-s vlan global show", glob(rp(5, timer(25487), typ(2)), rp(4, timer(0), typ(1))),
			want{stdout: hdr + col("br0") + "  10\n" + pad + "\n" + pad + "router ports: d2   254.87 permanent\n" +
				pad + "              d1     0.00 temp\n"}},
		{"positive: -j -s, the timer keeps its padding (B34)", "-j -s vlan global show", glob(rp(4, timer(5), typ(3))),
			want{stdout: `[{"ifname":"br0","vlans":[{"vlan":10,"router_ports":[{"port":"d1","timer":"   0.05","type":"temp"}]}]}]` + "\n"}},
		{"positive: without -s, ports only", "vlan global show", glob(rp(5, timer(1), typ(2)), rp(4)),
			want{stdout: hdr + col("br0") + "  10\n" + pad + "\n" + pad + "router ports: d2 d1 \n"}},
		{"negative: a 2-byte timer is read past (B35)", "-s vlan global show", glob(rp(4, at(xtcpnl.MdbaRouterPattrTimer, 1, 0))),
			want{stdout: "", stderr: "gobridge: MDBA_ROUTER_PATTR_TIMER is short; bridge reads past it (B35)\n", rc: 1}},
		{"negative: an empty type is read past (B35)", "-s vlan global show", glob(rp(4, at(xtcpnl.MdbaRouterPattrType))),
			want{stdout: "", stderr: "gobridge: MDBA_ROUTER_PATTR_TYPE is short; bridge reads past it (B35)\n", rc: 1}},
		{"boundary: the largest timer", "-s vlan global show", glob(rp(4, timer(0xffffffff))),
			want{stdout: hdr + col("br0") + "  10\n" + pad + "\n" + pad + "router ports: d1  42949672.95\n"}},
		{"boundary: a port with neither attribute", "-s vlan global show", glob(rp(4)),
			want{stdout: hdr + col("br0") + "  10\n" + pad + "\n" + pad + "router ports: d1 \n"}},
		{"corner: type 0 (disabled) prints permanent", "-s vlan global show", glob(rp(4, typ(0))),
			want{stdout: hdr + col("br0") + "  10\n" + pad + "\n" + pad + "router ports: d1  permanent\n"}},
		{"corner: an unknown type prints permanent", "-s vlan global show", glob(rp(4, typ(9))),
			want{stdout: hdr + col("br0") + "  10\n" + pad + "\n" + pad + "router ports: d1  permanent\n"}},
		{"corner: a flagged attribute is skipped (parse_rtattr, no mask)", "-s vlan global show",
			glob(rp(4, at(xtcpnl.MdbaRouterPattrType|unix.NLA_F_NESTED, 3))),
			want{stdout: hdr + col("br0") + "  10\n" + pad + "\n" + pad + "router ports: d1 \n"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Parallel()
			src := &fakeSource{replies: tc.replies}
			var out, errb bytes.Buffer
			rc := run(config{src: src}, strings.Fields(tc.args), &out, &errb)
			got := want{stdout: out.String(), stderr: errb.String(), rc: rc}
			if got.rc != 0 {
				got.stdout = "" // bridge's stdout is lost to the refusal's point; only rc and stderr are pinned
			}
			if got != tc.expected {
				t.Errorf("bridge %s\n got  %#v\n want %#v", tc.args, got, tc.expected)
			}
		})
	}
}
