package goip

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// NX4 (goip-parity-next.md): every readback of the v730 rows, replayed
// through goip as the release that printed it, 7.2.0 under ws/ and 7.3.0
// under ws-ip730/.

// wsV730Families are NC3-T02's row directories that have readbacks.
var wsV730Families = []string{"v730lladdr", "v730nh", "v730bond", "v730seg6", "v730ovpn"}

// wsReadbackArgv splits a readback's `ip …` into goip's run context and
// its object's arguments.
func wsReadbackArgv(cmd string) (*runCtx, string, []string, error) {
	f := strings.Fields(cmd)
	if len(f) < 2 || f[0] != "ip" {
		return nil, "", nil, fmt.Errorf("not an ip command: %q", cmd)
	}
	c := &runCtx{}
	f = f[1:]
	for len(f) > 0 && strings.HasPrefix(f[0], "-") {
		switch f[0] {
		case "-4":
			c.family = unix.AF_INET
		case "-6":
			c.family = unix.AF_INET6
		case "-j":
			c.json = true
		case "-d":
			c.showDetails++
		case "-s":
			c.showStats++
		case "-f":
			if len(f) < 2 || readFamily(f[1]) == unix.AF_UNSPEC {
				return nil, "", nil, fmt.Errorf("-f without a family in %q", cmd)
			}
			c.family = readFamily(f[1])
			f = f[1:]
		default:
			return nil, "", nil, fmt.Errorf("option %s in %q", f[0], cmd)
		}
		f = f[1:]
	}
	if len(f) == 0 {
		return nil, "", nil, fmt.Errorf("no object in %q", cmd)
	}
	i := objectIndex(f[0])
	if i < 0 {
		return nil, "", nil, fmt.Errorf("unknown object %s in %q", f[0], cmd)
	}
	return c, objects[i].name, f[1:], nil
}

// wsReadbackDiffers is how one readback differs from ip's, or "".
func wsReadbackDiffers(t *testing.T, dir, name, cmd string) string {
	t.Helper()
	c, object, args, err := wsReadbackArgv(cmd)
	if err != nil {
		t.Fatal(err)
	}
	run, ok := wsO5Runners[object]
	if !ok {
		t.Fatalf("%s: no goip runner for %q", cmd, object)
	}
	pcap := filepath.Join(dir, "netlink_route_"+name+".pcap")
	src := &restoreReplay{linkReplay: newLinkReplay(t, pcap)}
	c.src = src
	rc, stdout, _ := run(c, args)
	want, err := os.ReadFile(filepath.Join(dir, name+".out"))
	if err != nil {
		t.Fatal(err)
	}
	reqs := slices.Concat(src.dumpReqs, src.gets)
	slices.SortFunc(reqs, bytes.Compare)
	var why []string
	// An empty .out is a readback ip ran on a device the row never made, and
	// the workbench keeps no readback rc; an empty stdout is the match.
	if rc != ExitOK && len(want) > 0 {
		why = append(why, fmt.Sprintf("exit %d", rc))
	}
	if stdout != string(want) {
		why = append(why, fmt.Sprintf("stdout\n got  %q\n want %q", stdout, want))
	}
	if ip := mplsIPRequests(t, pcap); !slices.EqualFunc(reqs, ip, bytes.Equal) {
		why = append(why, fmt.Sprintf("requests\n goip %x\n ip   %x", reqs, ip))
	}
	return strings.Join(why, "; ")
}

// TestWsV730Readbacks replays each v730 row's readbacks: goip sends ip's
// requests and prints what ip printed, at the release the row ran, except
// the readbacks a row names as differing.
//
// go test ./internal/goip/ -run TestWsV730Readbacks
func TestWsV730Readbacks(t *testing.T) {
	useLLMap720(t) // 7.2.0 and 7.3.0 both ask for names alone
	t.Cleanup(func() { useWsO5Release("") })
	tests := []struct {
		description string
		space       string // as O5 names it
		readbacks   int    // compared
		differs     []string
	}{
		{description: "positive: the lladdr rows at 7.2.0", space: "v730lladdr", readbacks: 11},
		{description: "positive: the lladdr rows at 7.3.0", space: wsO5IP730Prefix + "v730lladdr", readbacks: 11},
		{description: "positive: the nexthop rows at 7.2.0", space: "v730nh", readbacks: 18},
		{description: "positive: the nexthop rows at 7.3.0, fdb with its space and dst_port", space: wsO5IP730Prefix + "v730nh", readbacks: 18},
		{description: "positive: the bond rows at 7.2.0", space: "v730bond", readbacks: 12},
		{description: "positive: the bond rows at 7.3.0, lacp_strict printed", space: wsO5IP730Prefix + "v730bond", readbacks: 12},
		{description: "positive: the seg6 rows at 7.2.0, every route refused", space: "v730seg6", readbacks: 12},
		{description: "positive: the seg6 rows at 7.3.0, the encap and its lookup printed", space: wsO5IP730Prefix + "v730seg6", readbacks: 12},
		{description: "positive: the ovpn rows at 7.2.0, the kind with nothing after it", space: "v730ovpn", readbacks: 8},
		{description: "positive: the ovpn rows at 7.3.0, mode printed", space: wsO5IP730Prefix + "v730ovpn", readbacks: 8},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			useWsO5Release(tc.space)
			ents, err := os.ReadDir(wsO5SpacePath(tc.space))
			if err != nil {
				t.Fatal(err)
			}
			n := 0
			var differs []string
			for _, e := range ents {
				dir := filepath.Join(wsO5SpacePath(tc.space), e.Name())
				b, err := os.ReadFile(filepath.Join(dir, "cmd.json"))
				if err != nil {
					t.Fatal(err)
				}
				var row struct {
					Readback []struct{ Cmd, Name string } `json:"readback"`
				}
				if err := json.Unmarshal(b, &row); err != nil {
					t.Fatal(err)
				}
				for _, r := range row.Readback {
					n++
					id := e.Name() + "/" + r.Name
					d := wsReadbackDiffers(t, dir, r.Name, r.Cmd)
					if d != "" {
						differs = append(differs, id)
					}
					if d != "" && !slices.Contains(tc.differs, id) {
						t.Errorf("%s (%s): %s", id, r.Cmd, d)
					}
				}
			}
			if n != tc.readbacks {
				t.Errorf("%d readbacks, want %d", n, tc.readbacks)
			}
			for _, id := range tc.differs {
				if !slices.Contains(differs, id) {
					t.Errorf("%s matches ip, but the row says it differs", id)
				}
			}
		})
	}
}
