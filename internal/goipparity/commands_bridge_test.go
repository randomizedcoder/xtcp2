package goipparity

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
)

const (
	bridgeKindCst      = "bridge"
	bridgeSlaveKindCst = "bridge_slave"
	// bridgeTopologyPrefixCst is the start of a topology line the step ran
	// cleanly on, before the step's argv.
	bridgeTopologyPrefixCst = "ok    [lacpa] ip "
)

// kindRowKinds are the corpus kinds with write rows, each row's slug
// `link_<kind>_<row>` (KG1-T05, KG2-T05, KG3-T05, KG4-T09, KG5-T07). A
// port kind's prefix starts with its master's (bridge_slave, vrf_slave,
// team_slave), so it is tried first.
var kindRowKinds = []string{
	bridgeSlaveKindCst, bridgeKindCst,
	vlanKindCst, macvlanKindCst, macvtapKindCst, ipvlanKindCst, ipvtapKindCst,
	vxlanKindCst, geneveKindCst, bareudpKindCst, amtKindCst, gtpKindCst,
	greKindCst, gretapKindCst, erspanKindCst, ip6greKindCst, ip6gretapKindCst, ip6erspanKindCst,
	ipipKindCst, sitKindCst, ip6tnlKindCst, vtiKindCst, vti6KindCst, xfrmKindCst,
	hsrKindCst, netkitKindCst, vrfSlaveKindCst, vrfKindCst, vxcanKindCst, batadvKindCst,
	ifbKindCst, nlmonKindCst, vcanKindCst, teamSlaveKindCst, teamKindCst,
}

// kindRowKind is a kind write row's corpus kind and slug.
func kindRowKind(slug string) (kind, row string, ok bool) {
	for _, kind := range kindRowKinds {
		if row, ok = strings.CutPrefix(slug, "link_"+kind+"_"); ok {
			return kind, row, true
		}
	}
	return "", "", false
}

// bridgeRowKind is kindRowKind for KG1-T05's two kinds alone.
func bridgeRowKind(slug string) (kind, row string, ok bool) {
	kind, row, ok = kindRowKind(slug)
	if kind != bridgeKindCst && kind != bridgeSlaveKindCst {
		return "", "", false
	}
	return kind, row, ok
}

// bridgeCorpusRows is each KC2 row of kind whose ip.rc is one of rcs.
func bridgeCorpusRows(t *testing.T, kind string, rcs ...string) []string {
	t.Helper()
	ents, err := os.ReadDir(filepath.Join(tdKinds730, kind))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range ents {
		rc := strings.TrimSpace(mustRead(t, filepath.Join(tdKinds730, kind, e.Name(), "ip.rc")))
		if slices.Contains(rcs, rc) {
			out = append(out, e.Name())
		}
	}
	return out
}

// bridgeTopology is a row's topology file as Setup steps, nil for none.
func bridgeTopology(t *testing.T, dir string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "topology"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var steps []string
	for _, line := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
		step, ok := strings.CutPrefix(line, bridgeTopologyPrefixCst)
		if !ok {
			t.Fatalf("%s: topology line %q", dir, line)
		}
		steps = append(steps, step)
	}
	return steps
}

// TestBridgeMatrixRows pins KG1-T05's rows (goip-link-kinds.md, "KG1-T05
// The bridge matrix rows"): one write row per KC2 row that sent a request,
// each with its capture's floor and its topology's steps, none of the rows
// ip refuses before netlink, and the four read rows.
//
// go test ./internal/goipparity/ -run TestBridgeMatrixRows
func TestBridgeMatrixRows(t *testing.T) {
	got := map[string][]string{}
	var rows []Command
	for _, c := range Commands() {
		if kind, slug, ok := bridgeRowKind(c.Slug); ok {
			got[kind] = append(got[kind], slug)
			rows = append(rows, c)
		}
	}
	t.Run("positive: one write row per request row", func(t *testing.T) {
		for _, tc := range []struct {
			kind string
			want int
		}{{bridgeKindCst, 55}, {bridgeSlaveKindCst, 37}} {
			want := bridgeCorpusRows(t, tc.kind, "0", "2")
			if len(want) != tc.want {
				t.Fatalf("%s: %d corpus rows sent a request, want %d", tc.kind, len(want), tc.want)
			}
			have := slices.Sorted(slices.Values(got[tc.kind]))
			if !slices.Equal(have, want) {
				t.Errorf("%s rows %v, want the corpus's %v", tc.kind, have, want)
			}
		}
		if len(rows) != 92 {
			t.Errorf("%d rows, want 92", len(rows))
		}
	})
	t.Run("negative: no ip-refused row", func(t *testing.T) {
		for _, kind := range []string{bridgeKindCst, bridgeSlaveKindCst} {
			refused := bridgeCorpusRows(t, kind, "1", "255")
			for _, slug := range refused {
				if slices.Contains(got[kind], slug) {
					t.Errorf("%s %s is a matrix row; goip exits non-zero on it", kind, slug)
				}
			}
			if kind == bridgeKindCst && len(refused) != 5 || kind == bridgeSlaveKindCst && len(refused) != 5 {
				t.Errorf("%s: %d ip-refused rows, want 5", kind, len(refused))
			}
		}
	})
	t.Run("boundary: each floor is the row's ip.pcap record count", func(t *testing.T) {
		for _, c := range rows {
			kind, slug, _ := bridgeRowKind(c.Slug)
			n := len(pcapRecords(t, mustReadBytes(t, filepath.Join(tdKinds730, kind, slug, "ip.pcap"))))
			if c.Floor != n || !c.Write || !c.Implemented {
				t.Errorf("%s: Floor=%d Write=%v Implemented=%v, want %d, true, true", c.Slug, c.Floor, c.Write, c.Implemented, n)
			}
		}
	})
	t.Run("corner: each setup is the row's topology, step for step", func(t *testing.T) {
		for _, c := range rows {
			kind, slug, _ := bridgeRowKind(c.Slug)
			if want := bridgeTopology(t, filepath.Join(tdKinds730, kind, slug)); !slices.Equal(c.Setup, want) {
				t.Errorf("%s: Setup %q, want %q", c.Slug, c.Setup, want)
			}
		}
	})
	reads := []struct {
		name  string
		floor int
	}{
		{"-d link show dev br0", 4}, {"-d -j link show dev br0", 4},
		{"-d link show dev p0", 6}, {"-d -j link show dev p0", 6},
	}
	t.Run("positive: the four read rows share the ports' setup", func(t *testing.T) {
		for _, tc := range reads {
			c, err := Lookup(tc.name)
			if err != nil {
				t.Fatalf("%q: %v", tc.name, err)
			}
			if c.Write || c.Floor != tc.floor || !slices.Equal(c.Setup, bridgeWriteSetupPorts) {
				t.Errorf("%q: Write=%v Floor=%d Setup=%q, want false, %d, PRE_PORTS", tc.name, c.Write, c.Floor, c.Setup, tc.floor)
			}
		}
	})
	t.Run("positive: every row is gated (V154, V155)", func(t *testing.T) {
		al, err := nlparity.EmbeddedAllowlist()
		if err != nil {
			t.Fatalf("EmbeddedAllowlist: %v", err)
		}
		names := make([]string, 0, len(reads)+len(rows))
		for _, tc := range reads {
			names = append(names, tc.name)
		}
		for _, c := range rows {
			names = append(names, c.Name)
		}
		for _, name := range names {
			if !al.IsGated(name) {
				t.Errorf("%q is not gated", name)
			}
		}
	})
}
