//go:build goip_write

package goip

import (
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/internal/goip/req"
	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// TestWsGoipDifferential is O5 over every route, rule and nexthop row the
// workbench recorded. It fails when any row's outcome differs from the
// golden list, or when a space holds no rows.
//
// go test ./internal/goip/ -run TestWsGoipDifferential
func TestWsGoipDifferential(t *testing.T) {
	useLLMap720(t) // the rows ran ip 7.2.0, whose lookups ask for names alone
	useWsO5Netns(t)
	useFakeBpf(t, nil)
	var report []string
	counts := map[string]map[string]int{}
	lookupNotes := map[string]int{}
	padRows, encapPadRows := 0, 0
	t.Cleanup(func() { useWsO5Release("") })
	for _, sp := range wsO5Spaces {
		useWsO5Release(sp)
		ents, err := os.ReadDir(wsO5SpacePath(sp))
		if err != nil {
			t.Fatal(err)
		}
		counts[sp] = map[string]int{}
		for _, e := range ents {
			dir := filepath.Join(wsO5SpacePath(sp), e.Name())
			b, err := os.ReadFile(filepath.Join(dir, "cmd.json"))
			if err != nil {
				t.Fatal(err)
			}
			var row wsRowFile
			if err := json.Unmarshal(b, &row); err != nil {
				t.Fatalf("%s: %v", dir, err)
			}
			if row.Result.Status != "OK" {
				t.Fatalf("%s: status %s, which O1 already fails", row.ID, row.Result.Status)
			}
			// A row whose command is not ip's, nhstats' ping, sends no
			// request of ip's to compare.
			if _, _, args, err := wsO5Argv(wsO5Subst(row.Cmd)); (err == nil && !wsO5Applies(args)) ||
				!strings.HasPrefix(row.Cmd, "ip ") {
				counts[sp]["n/a"]++
				continue
			}
			outcome, detail, lookups, pad, encapPad := wsO5Row(t, dir, row)
			if pad {
				padRows++
			}
			if encapPad {
				encapPadRows++
			}
			counts[sp][outcome]++
			if lookups != "" {
				counts[sp]["lookups"]++
				lookupNotes[lookups]++
			}
			id := row.ID
			if strings.HasPrefix(sp, wsO5IP730Prefix) {
				id = wsO5IP730Prefix + id
			}
			if outcome != o5Same {
				report = append(report, fmt.Sprintf("%s %s rc=%d %s | %s", id, outcome, row.Result.RC, detail, row.Cmd))
			}
			if lookups != "" && !wsO5MaskOnly(lookups) {
				report = append(report, fmt.Sprintf("%s lookups rc=%d %s | %s", id, row.Result.RC, lookups, row.Cmd))
			}
		}
		if len(ents) == 0 {
			t.Errorf("%s holds no rows", sp)
		}
	}
	head := make([]string, 0, len(wsO5Spaces)+len(lookupNotes)+len(report))
	for _, sp := range wsO5Spaces {
		c := counts[sp]
		head = append(head, fmt.Sprintf("# %s: %d same, %d refuses, %d sends, %d differs; lookups differ in %d; not a write %d",
			sp, c[o5Same], c[o5Refuses], c[o5Sends], c[o5Differs], c["lookups"], c["n/a"]))
	}
	head = append(head, fmt.Sprintf("# metrics padding: %d rows: ip sends stack bytes after an RTA_METRICS sub-attribute; zeroed before comparing", padRows))
	head = append(head, fmt.Sprintf("# encap padding: %d rows: ip sends stack bytes in an encap or after its type, on a nexthop, a route or a hop; zeroed before comparing", encapPadRows))
	for _, n := range slices.Sorted(maps.Keys(lookupNotes)) {
		head = append(head, fmt.Sprintf("# lookups: %d rows: %s", lookupNotes[n], n))
	}
	got := strings.Join(append(head, report...), "\n") + "\n"
	if *updateWsO5 {
		if err := os.MkdirAll(filepath.Dir(wsO5Golden), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(wsO5Golden, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(wsO5Golden)
	if err != nil {
		t.Fatalf("%v (run with -update-ws-o5 to write it)", err)
	}
	for _, l := range wsO5GoldenDiff(string(want), got) {
		t.Error(l)
	}
}

// TestWsO5LLMapKnob runs O5 over captured rows under each lookup knob. The
// rows ran ip 7.2.0, so 7.2.0's knob compares their lookups clean, and
// 7.1.0's, which drops RTEXT_FILTER_NAME_ONLY (0x100), is reported; the
// knob is back to the package's after each.
func TestWsO5LLMapKnob(t *testing.T) {
	tests := []struct {
		description string
		row         string // under wsCorpusDir
		knob720     bool
		expected    string // the lookups note
	}{
		{"positive: a link set's lookup under 7.2.0's knob", "linkset/bd-001", true, ""},
		{"positive: a neighbor's dump under 7.2.0's knob", "neigh4/bd-001", true, ""},
		{"negative: 7.1.0's knob drops 0x100 from a lookup", "linkset/bd-001", false, "lookup IFLA_EXT_MASK 0x109, goip 0x9"},
		{"negative: 7.1.0's knob drops 0x100 from a dump", "neigh4/bd-001", false, "dump IFLA_EXT_MASK 0x109, goip 0x1"},
	}
	before := req.LLMapNameOnly
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if tc.knob720 {
				useLLMap720(t)
			}
			dir := filepath.Join(wsCorpusDir, tc.row)
			b, err := os.ReadFile(filepath.Join(dir, "cmd.json"))
			if err != nil {
				t.Fatal(err)
			}
			var row wsRowFile
			if err := json.Unmarshal(b, &row); err != nil {
				t.Fatal(err)
			}
			outcome, _, lookups, _, _ := wsO5Row(t, dir, row)
			if outcome != o5Same || lookups != tc.expected {
				t.Errorf("outcome %s, lookups %q; want same, %q", outcome, lookups, tc.expected)
			}
		})
		if req.LLMapNameOnly != before {
			t.Fatalf("corner: the knob is %v after %q, want %v", req.LLMapNameOnly, tc.description, before)
		}
	}
}

var updateWsO5 = flag.Bool("update-ws-o5", false, "rewrite testdata/ws-differential.txt")

// wsO5Golden lists every row whose outcome is not "same".
const wsO5Golden = "testdata/ws-differential.txt"

// The words the workbench's shell expanded in a row's command, and what O5
// puts in their place: the pid of the row's shell, and the name `ip netns
// identify` gave its namespace. wsO5Netns answers for the paths they make.
const (
	wsO5PidCst        = "4242"
	wsO5NetnsNameCst  = "o5ns"
	wsO5NetnsRunDir   = "/o5/netns"
	wsO5IdentifyShell = "$(ip netns identify $$)"
)

// wsO5Subst is the row's command as its shell ran it, with wsO5PidCst and
// wsO5NetnsNameCst for what the shell expanded.
func wsO5Subst(cmd string) string {
	cmd = strings.ReplaceAll(cmd, wsO5IdentifyShell, wsO5NetnsNameCst)
	return strings.ReplaceAll(cmd, "$$", wsO5PidCst)
}

// useWsO5Netns answers netns_get_fd's opens for the namespaces the rows
// name: wsO5NetnsNameCst under wsO5NetnsRunDir, the row shell's pid, pid 1
// and /proc/self, and /proc/self/stat, a file that isn't one. Each opens
// /dev/null, since O5 zeroes the descriptor; any other path is ENOENT.
func useWsO5Netns(t *testing.T) {
	t.Helper()
	prevOpen, prevDir := netnsOpen, netnsRunDir
	exists := map[string]bool{
		wsO5NetnsRunDir + "/" + wsO5NetnsNameCst: true,
		"/proc/" + wsO5PidCst + "/ns/net":        true,
		"/proc/1/ns/net":                         true,
		"/proc/self/ns/net":                      true,
		"/proc/self/stat":                        true,
	}
	netnsRunDir = wsO5NetnsRunDir
	netnsOpen = func(path string) (int, error) {
		if !exists[path] {
			return -1, unix.ENOENT
		}
		return unix.Open(os.DevNull, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	}
	t.Cleanup(func() { netnsOpen, netnsRunDir = prevOpen, prevDir })
}

// wsO5ZeroNetnsFd zeroes a link write's IFLA_NET_NS_FD value: the
// descriptor ip happened to get, which goip's needn't match.
func wsO5ZeroNetnsFd(msg []byte) []byte {
	if len(msg) < xtcpnl.NlMsgHdrSizeCst+unix.SizeofIfInfomsg {
		return msg
	}
	if t := binary.LittleEndian.Uint16(msg[4:6]); t != unix.RTM_NEWLINK && t != unix.RTM_SETLINK {
		return msg
	}
	out := slices.Clone(msg)
	for off := xtcpnl.NlMsgHdrSizeCst + unix.SizeofIfInfomsg; off+4 <= len(out); {
		l := int(binary.LittleEndian.Uint16(out[off:]))
		if l < 4 || off+l > len(out) {
			break
		}
		if binary.LittleEndian.Uint16(out[off+2:]) == unix.IFLA_NET_NS_FD && l == 8 {
			binary.LittleEndian.PutUint32(out[off+4:], 0)
		}
		off += (l + 3) &^ 3
	}
	return out
}

// wsO5Row runs one row through goip and classifies it.
func wsO5Row(t *testing.T, dir string, row wsRowFile) (outcome, detail, lookups string, pad, encapPad bool) {
	t.Helper()
	family, object, args, err := wsO5Argv(wsO5Subst(row.Cmd))
	if err != nil {
		t.Fatal(err)
	}
	run, ok := wsO5Runners[object]
	if !ok {
		t.Fatalf("%s: no goip runner for %q", row.ID, object)
	}
	pcap := filepath.Join(dir, "ip.pcap")
	ipReqs := captureRequests(t, pcap)
	out := filepath.Join(t.TempDir(), "write.bin")
	t.Setenv(writeOutEnvCst, out)
	src := &ruleReplay{restoreReplay: &restoreReplay{linkReplay: newLinkReplay(t, pcap)}}
	rc, _, stderr := run(&runCtx{src: src, family: family}, args)
	var writes [][]byte
	if _, err := os.Stat(out); err == nil {
		for _, m := range restoreWrites(t, out) {
			w := nlparity.ZeroLwtBpfFDs(wsO5ZeroNetnsFd(wireBytesOf(m)))
			writes = append(writes, w)
		}
	}
	goipErr := ""
	if rc != 0 {
		goipErr = strings.TrimSpace(strings.TrimPrefix(strings.SplitN(stderr, "\n", 2)[0], "goip: "))
	}
	look, dumps, ipWrites := wsO5Split(ipReqs)
	for i := range ipWrites {
		var z bool
		ipWrites[i], z = wsO5ZeroMetricsPad(ipWrites[i])
		pad = pad || z
		ipWrites[i], z = wsO5ZeroEncapPad(ipWrites[i])
		encapPad = encapPad || z
		ipWrites[i] = nlparity.ZeroLwtBpfFDs(wsO5ZeroNetnsFd(ipWrites[i]))
	}
	lookups = wsO5Lookups("lookup", look, src.gets)
	if d := wsO5Lookups("dump", dumps, src.dumpReqs); d != "" {
		lookups = strings.TrimPrefix(lookups+"; "+d, "; ")
	}
	outcome, detail = wsO5Compare(ipWrites, writes, goipErr)
	if outcome == o5Refuses {
		// goip stopped at an argument it does not take, and may not have
		// reached the device whose lookup ip sent.
		lookups = ""
	}
	return outcome, detail, lookups, pad, encapPad
}
