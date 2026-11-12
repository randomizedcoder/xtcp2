//go:build goip_write

package goipparity

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
)

// KC4's five kinds, each a corpus directory and a slug's middle word.
const (
	vxlanKindCst   = "vxlan"
	geneveKindCst  = "geneve"
	bareudpKindCst = "bareudp"
	amtKindCst     = "amt"
	gtpKindCst     = "gtp"
)

// udpOverlayKind is one KC4 kind's device, its read rows' setup and
// floor, and its corpus's request and refused row counts.
type udpOverlayKind struct {
	kind    string
	dev     string
	setup   []string
	floor   int
	rows    int
	refused int
}

// TestUdpOverlayMatrixRows pins KG3-T05's rows (goip-link-kinds.md,
// "KG3-T05 The UDP overlay matrix rows"): one write row per KC4 row that
// sent a request, each with its capture's floor and its topology's steps,
// none of the rows ip refuses before netlink, and the 11 read rows.
//
// go test ./internal/goipparity/ -run TestUdpOverlayMatrixRows
func TestUdpOverlayMatrixRows(t *testing.T) {
	kinds := []udpOverlayKind{
		{vxlanKindCst, "vx0", vxlanWriteSetupVx0, 4, 58, 15},
		{geneveKindCst, "gn0", geneveWriteSetupGn0, 4, 35, 14},
		{bareudpKindCst, "b0", bareudpWriteSetupB0, 4, 10, 6},
		{amtKindCst, "a0", amtWriteSetupA0, 6, 13, 7},
		{gtpKindCst, "g0", gtpWriteSetupG0, 4, 11, 8},
	}
	isUDP := func(kind string) bool {
		return slices.ContainsFunc(kinds, func(k udpOverlayKind) bool {
			return k.kind == kind
		})
	}
	got := map[string][]string{}
	var rows []Command
	for _, c := range Commands() {
		if kind, slug, ok := kindRowKind(c.Slug); ok && isUDP(kind) {
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
		if len(rows) != 127 {
			t.Errorf("%d rows, want 127", len(rows))
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
		if n != 50 {
			t.Errorf("%d ip-refused rows, want 50", n)
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
		respelled := []struct {
			name     string
			expected string
		}{
			{"link add dev gn1 type geneve id 1 remote 10.0.0.1", "link_geneve_add_after_external"},
			{"link set dev gn0 type geneve id 1 local 10.0.0.2", "link_geneve_set_local_new"},
		}
		for _, r := range respelled {
			if seen[r.name] != r.expected {
				t.Errorf("%q is %q, want %s", r.name, seen[r.name], r.expected)
			}
		}
	})
	var reads []string
	t.Run("positive: the 11 read rows", func(t *testing.T) {
		names := make([]string, 0, 1+2*len(kinds))
		names = append(names, "-d -d link show dev vx0")
		for _, tc := range kinds {
			names = append(names, "-d link show dev "+tc.dev, "-d -j link show dev "+tc.dev)
		}
		for _, name := range names {
			reads = append(reads, name)
			c, err := Lookup(name)
			if err != nil {
				t.Fatalf("%q: %v", name, err)
			}
			i := slices.IndexFunc(kinds, func(k udpOverlayKind) bool {
				return name[len(name)-len(k.dev)-1:] == " "+k.dev
			})
			if i < 0 {
				t.Fatalf("%q names no kind's device", name)
			}
			tc := kinds[i]
			if c.Write || c.Floor != tc.floor || !slices.Equal(c.Setup, tc.setup) {
				t.Errorf("%q: Write=%v Floor=%d Setup=%q, want false, %d, %q", name, c.Write, c.Floor, c.Setup, tc.floor, tc.setup)
			}
		}
		if len(reads) != 11 {
			t.Errorf("%d read rows, want 11", len(reads))
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
		if len(names) != 138 {
			t.Errorf("%d rows, want 138", len(names))
		}
		for _, name := range names {
			if !al.IsGated(name) {
				t.Errorf("%q is not gated", name)
			}
		}
	})
}
