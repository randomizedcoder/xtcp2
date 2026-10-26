package goipparity

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
)

// KC3's five kinds, each a corpus directory and a slug's middle word.
const (
	vlanKindCst    = "vlan"
	macvlanKindCst = "macvlan"
	macvtapKindCst = "macvtap"
	ipvlanKindCst  = "ipvlan"
	ipvtapKindCst  = "ipvtap"
)

// TestVlanFamilyMatrixRows pins KG2-T05's rows (goip-link-kinds.md,
// "KG2-T05 The VLAN family matrix rows"): one write row per KC3 row that
// sent a request, each with its capture's floor and its topology's steps,
// none of the rows ip refuses before netlink, and the ten read rows.
//
// go test ./internal/goipparity/ -run TestVlanFamilyMatrixRows
func TestVlanFamilyMatrixRows(t *testing.T) {
	kinds := []struct {
		kind    string
		dev     string
		rows    int
		refused int
	}{
		{vlanKindCst, "v0", 21, 7},
		{macvlanKindCst, "m0", 25, 9},
		{macvtapKindCst, "t0", 5, 1},
		{ipvlanKindCst, "i0", 14, 4},
		{ipvtapKindCst, "j0", 3, 1},
	}
	got := map[string][]string{}
	var rows []Command
	for _, c := range Commands() {
		if kind, slug, ok := kindRowKind(c.Slug); ok && kind != bridgeKindCst && kind != bridgeSlaveKindCst {
			got[kind] = append(got[kind], slug)
			rows = append(rows, c)
		}
	}
	t.Run("positive: one write row per request row", func(t *testing.T) {
		for _, tc := range kinds {
			want := bridgeCorpusRows(t, tc.kind, "0", "2")
			if len(want) != tc.rows {
				t.Fatalf("%s: %d corpus rows sent a request, want %d", tc.kind, len(want), tc.rows)
			}
			have := slices.Sorted(slices.Values(got[tc.kind]))
			if !slices.Equal(have, want) {
				t.Errorf("%s rows %v, want the corpus's %v", tc.kind, have, want)
			}
		}
		if len(rows) != 68 {
			t.Errorf("%d rows, want 68", len(rows))
		}
	})
	t.Run("negative: no ip-refused row", func(t *testing.T) {
		n := 0
		for _, tc := range kinds {
			refused := bridgeCorpusRows(t, tc.kind, "1", "255")
			for _, slug := range refused {
				if slices.Contains(got[tc.kind], slug) {
					t.Errorf("%s %s is a matrix row; goip exits non-zero on it", tc.kind, slug)
				}
			}
			if len(refused) != tc.refused {
				t.Errorf("%s: %d ip-refused rows, want %d", tc.kind, len(refused), tc.refused)
			}
			n += len(refused)
		}
		if n != 22 {
			t.Errorf("%d ip-refused rows, want 22", n)
		}
	})
	t.Run("boundary: each floor is the row's ip.pcap record count", func(t *testing.T) {
		for _, c := range rows {
			kind, slug, _ := kindRowKind(c.Slug)
			n := len(pcapRecords(t, mustReadBytes(t, filepath.Join(tdKinds730, kind, slug, "ip.pcap"))))
			if c.Floor != n || !c.Write || !c.Implemented {
				t.Errorf("%s: Floor=%d Write=%v Implemented=%v, want %d, true, true", c.Slug, c.Floor, c.Write, c.Implemented, n)
			}
		}
	})
	t.Run("corner: each setup is the row's topology, step for step", func(t *testing.T) {
		for _, c := range rows {
			kind, slug, _ := kindRowKind(c.Slug)
			if want := bridgeTopology(t, filepath.Join(tdKinds730, kind, slug)); !slices.Equal(c.Setup, want) {
				t.Errorf("%s: Setup %q, want %q", c.Slug, c.Setup, want)
			}
		}
	})
	t.Run("corner: each name is unique", func(t *testing.T) {
		seen := map[string]string{}
		for _, c := range rows {
			if prev, ok := seen[c.Name]; ok {
				t.Errorf("%s and %s share the name %q", prev, c.Slug, c.Name)
			}
			seen[c.Name] = c.Slug
		}
		const busy = "link add link d0 dev i0 type ipvlan"
		if seen[busy] != "link_ipvlan_add_on_busy_lower" {
			t.Errorf("%q is %q, want link_ipvlan_add_on_busy_lower", busy, seen[busy])
		}
	})
	var reads []string
	t.Run("positive: the ten read rows", func(t *testing.T) {
		for _, tc := range kinds {
			for _, j := range []string{"", "-j "} {
				name := "-d " + j + "link show dev " + tc.dev
				reads = append(reads, name)
				c, err := Lookup(name)
				if err != nil {
					t.Fatalf("%q: %v", name, err)
				}
				add := "link add link d0 name " + tc.dev + " type " + tc.kind
				if c.Write || c.Floor != 6 || len(c.Setup) != 2 || c.Setup[0] != linkWriteSetupDummy[0] ||
					!strings.HasPrefix(c.Setup[1], add) {
					t.Errorf("%q: Write=%v Floor=%d Setup=%q, want false, 6, d0 and %q", name, c.Write, c.Floor, c.Setup, add)
				}
			}
		}
		if len(reads) != 10 {
			t.Errorf("%d read rows, want 10", len(reads))
		}
	})
	t.Run("positive: every row is gated", func(t *testing.T) {
		al, err := nlparity.EmbeddedAllowlist()
		if err != nil {
			t.Fatalf("EmbeddedAllowlist: %v", err)
		}
		names := slices.Clone(reads)
		for _, c := range rows {
			names = append(names, c.Name)
		}
		if len(names) != 78 {
			t.Errorf("%d rows, want 78", len(names))
		}
		for _, name := range names {
			if !al.IsGated(name) {
				t.Errorf("%q is not gated", name)
			}
		}
	})
}
