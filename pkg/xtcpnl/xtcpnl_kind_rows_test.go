package xtcpnl

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// KC7-T01: every row of the kind corpus in the 7.3.0 link-write set carries
// its status and request, and its readbacks exactly when the request reached
// the kernel (docs/netlink/goip-link-kinds.md, "KC7-T01 The 7.3.0 link-write
// corpus"). A row ip accepts (0) or the kernel refuses (2) is read back; one
// ip refuses before netlink (1, 255) never reaches the readbacks.

// kindRowReadbacks are the readback pcaps link-write-readback.exp writes.
var kindRowReadbacks = []string{
	"netlink_route_getlink.pcap",
	"netlink_route_getlink_detail.pcap",
	"netlink_route_getlink_detail_json.pcap",
}

// kindRowProblems checks every kinds/<kind>/<slug>/ directory in fsys.
func kindRowProblems(fsys fs.FS) []string {
	var out []string
	kinds, err := fs.ReadDir(fsys, "kinds")
	if err != nil {
		return []string{"kinds: " + err.Error()}
	}
	for _, k := range kinds {
		rows, err := fs.ReadDir(fsys, path.Join("kinds", k.Name()))
		if err != nil {
			out = append(out, k.Name()+": "+err.Error())
			continue
		}
		for _, r := range rows {
			out = append(out, kindRowProblem(fsys, path.Join("kinds", k.Name(), r.Name()))...)
		}
	}
	slices.Sort(out)
	return out
}

// kindRowProblem checks one row directory.
func kindRowProblem(fsys fs.FS, dir string) []string {
	if _, err := fs.Stat(fsys, path.Join(dir, "ip.pcap")); err != nil {
		return []string{dir + ": no ip.pcap"}
	}
	raw, err := fs.ReadFile(fsys, path.Join(dir, "ip.rc"))
	if err != nil {
		return []string{dir + ": no ip.rc"}
	}
	rc := strings.TrimSpace(string(raw))
	var want bool
	switch rc {
	case "0", "2":
		want = true
	case "1", "255":
	default:
		return []string{dir + ": ip.rc is " + rc}
	}
	var out []string
	for _, rb := range kindRowReadbacks {
		_, err := fs.Stat(fsys, path.Join(dir, rb))
		switch {
		case want && err != nil:
			out = append(out, dir+": rc "+rc+" and no "+rb)
		case !want && err == nil:
			out = append(out, dir+": rc "+rc+" and a "+rb)
		}
	}
	return out
}

func TestKindRowsComplete(t *testing.T) {
	// row returns the files of one row with status rc and the readbacks.
	row := func(dir, rc string, readbacks ...string) fstest.MapFS {
		m := fstest.MapFS{
			dir + "/ip.pcap": {Data: []byte{0}},
			dir + "/ip.rc":   {Data: []byte(rc + "\n")},
		}
		for _, rb := range readbacks {
			m[dir+"/"+rb] = &fstest.MapFile{Data: []byte{0}}
		}
		return m
	}
	all := kindRowReadbacks
	const d = "kinds/vcan/set_mtu_72"

	tests := []struct {
		description string
		fsys        fs.FS
		expected    []string
	}{
		{
			description: "positive: the committed 7.3.0 kind corpus",
			fsys:        os.DirFS(tdLinkWrite_7_3_0),
			expected:    nil,
		},
		{
			description: "positive: an accepted row with its three readbacks",
			fsys:        row(d, "0", all...),
			expected:    nil,
		},
		{
			description: "negative: a row with no status",
			fsys:        fstest.MapFS{d + "/ip.pcap": {Data: []byte{0}}},
			expected:    []string{d + ": no ip.rc"},
		},
		{
			description: "negative: a row with no request",
			fsys:        fstest.MapFS{d + "/ip.rc": {Data: []byte("0\n")}},
			expected:    []string{d + ": no ip.pcap"},
		},
		{
			description: "boundary: a kernel refusal is read back too",
			fsys:        row(d, "2", all...),
			expected:    nil,
		},
		{
			description: "boundary: an ip refusal has no readback",
			fsys:        row(d, "255"),
			expected:    nil,
		},
		{
			description: "corner: an accepted row missing one readback",
			fsys:        row(d, "0", all[:2]...),
			expected:    []string{d + ": rc 0 and no netlink_route_getlink_detail_json.pcap"},
		},
		{
			description: "corner: an ip refusal that was read back",
			fsys:        row(d, "1", all[0]),
			expected:    []string{d + ": rc 1 and a netlink_route_getlink.pcap"},
		},
		{
			description: "corner: a status the corpus never plans",
			fsys:        row(d, "3"),
			expected:    []string{d + ": ip.rc is 3"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got := kindRowProblems(tt.fsys)
			if !slices.Equal(got, tt.expected) {
				t.Errorf("got %q, want %q", got, tt.expected)
			}
		})
	}
}

// TestCorpusAllSkipsLinkWrite730 pins corpusAll's one exclusion: the corpus
// tests' tallies read the 7.1.4 link-write set and never its 7.3.0 recapture.
func TestCorpusAllSkipsLinkWrite730(t *testing.T) {
	// under counts the corpus messages whose pcap is under root.
	under := func(root string) int {
		n := 0
		for _, c := range corpusAll(t) {
			if strings.HasPrefix(c.path, filepath.Clean(root)+"/") {
				n++
			}
		}
		return n
	}
	tests := []struct {
		description string
		root        string
		expected    bool
	}{
		{description: "positive: the 7.1.4 link-write set is read", root: tdLinkWrite_7_1_4, expected: true},
		{description: "negative: the 7.3.0 link-write set is not", root: tdLinkWrite_7_3_0, expected: false},
		{description: "boundary: the 7.3.0 dump set beside it still is", root: tdDumps_7_3_0, expected: true},
		{description: "corner: the 7.3.0 kind corpus, inside the skipped set, is not", root: tdLinkWrite_7_3_0 + "/kinds", expected: false},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := under(tt.root) > 0; got != tt.expected {
				t.Errorf("messages under %s: got %v, want %v", tt.root, got, tt.expected)
			}
		})
	}
}
