package goipparity

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
)

// KC6's eleven kinds, each a corpus directory and a slug's middle word.
const (
	hsrKindCst       = "hsr"
	netkitKindCst    = "netkit"
	vrfKindCst       = "vrf"
	vrfSlaveKindCst  = "vrf_slave"
	vxcanKindCst     = "vxcan"
	batadvKindCst    = "batadv"
	ifbKindCst       = "ifb"
	nlmonKindCst     = "nlmon"
	vcanKindCst      = "vcan"
	teamKindCst      = "team"
	teamSlaveKindCst = "team_slave"
)

// remainingKind is one KC6 kind's expected rows: its read rows' show
// words, setup and floor, and how many corpus rows sent a request and how
// many ip refused.
type remainingKind struct {
	kind    string
	show    string
	setup   []string
	floor   int
	rows    int
	refused int
}

// TestRemainingKindsMatrixRows pins KG5-T07's rows (goip-link-kinds.md,
// "KG5-T07 The remaining kinds' matrix rows"): one write row per KC6 row
// that sent a request, each with its capture's floor and its topology's
// steps, none of the rows ip refuses before netlink, and the 22 read rows.
//
// go test ./internal/goipparity/ -run TestRemainingKindsMatrixRows
func TestRemainingKindsMatrixRows(t *testing.T) {
	kinds := []remainingKind{
		{hsrKindCst, "link show dev hs0", hsrWriteSetupHs0, 8, 20, 9},
		{netkitKindCst, "link show dev nt0", netkitReadSetupNt0, 6, 27, 16},
		{vrfKindCst, "link show dev vr0", vrfWriteSetupVr0, 4, 12, 6},
		{vrfSlaveKindCst, "link show dev d0", vrfSlaveWriteSetupD0Enslaved, 6, 7, 3},
		{vxcanKindCst, "link show dev vc0", vxcanWriteSetupVc0, 6, 9, 9},
		{batadvKindCst, "link show dev bat0", batadvReadSetupBat0, 4, 12, 5},
		{ifbKindCst, "link show dev ib0", ifbWriteSetupIb0, 4, 5, 4},
		{nlmonKindCst, "link show dev nm0", nlmonWriteSetupNm0, 4, 5, 1},
		{vcanKindCst, "link show dev vn0", vcanWriteSetupVn0, 4, 11, 2},
		{teamKindCst, "link show dev tm0", teamWriteSetupTm0, 4, 11, 3},
		{teamSlaveKindCst, "link show d0", teamSlaveReadSetupD0, 6, 3, 1},
	}
	isRemaining := func(kind string) bool {
		return slices.ContainsFunc(kinds, func(k remainingKind) bool { return k.kind == kind })
	}
	got := map[string][]string{}
	var rows []Command
	for _, c := range Commands() {
		if kind, slug, ok := kindRowKind(c.Slug); ok && isRemaining(kind) {
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
		if len(rows) != 122 {
			t.Errorf("%d rows, want 122", len(rows))
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
		if n != 59 {
			t.Errorf("%d ip-refused rows, want 59", n)
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
	t.Run("corner: the floors that count lookups", func(t *testing.T) {
		for _, tc := range []struct {
			slug     string
			expected int
		}{
			{"link_vrf_slave_set_vrf", 8},
			{"link_hsr_add_interlink", 8},
			{"link_netkit_set_peer_name", 6},
		} {
			i := slices.IndexFunc(rows, func(c Command) bool { return c.Slug == tc.slug })
			if i < 0 {
				t.Fatalf("%s: no row", tc.slug)
			}
			if rows[i].Floor != tc.expected {
				t.Errorf("%s: Floor=%d, want %d", tc.slug, rows[i].Floor, tc.expected)
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
		for _, c := range Commands() {
			if prev, ok := seen[c.Name]; ok {
				t.Errorf("%s and %s share the name %q", prev, c.Slug, c.Name)
			}
			seen[c.Name] = c.Slug
		}
		respelled := []struct {
			name     string
			expected string
		}{
			{"link add dev bat0 type batadv", "link_batadv_add_duplicate"},
			{"link add dev vr0 type vrf table 10", "link_vrf_add_shared_table"},
			{"link set dev d0 master tm0", "link_team_set_enslave_again"},
			{"link set dev d0 nomaster", "link_vrf_slave_set_nomaster"},
			{"link set dev d0 type team_slave", "link_team_slave_set_unenslaved"},
			{"link set dev vn0 mtu 72", "link_vcan_set_mtu_up"},
		}
		for _, r := range respelled {
			if seen[r.name] != r.expected {
				t.Errorf("%q is %q, want %s", r.name, seen[r.name], r.expected)
			}
		}
	})
	var reads []string
	t.Run("positive: the 22 read rows", func(t *testing.T) {
		for _, tc := range kinds {
			for _, name := range []string{"-d " + tc.show, "-d -j " + tc.show} {
				reads = append(reads, name)
				c, err := Lookup(name)
				if err != nil {
					t.Fatalf("%q: %v", name, err)
				}
				if c.Write || c.Floor != tc.floor || !slices.Equal(c.Setup, tc.setup) {
					t.Errorf("%q: Write=%v Floor=%d Setup=%q, want false, %d, %q", name, c.Write, c.Floor, c.Setup, tc.floor, tc.setup)
				}
			}
		}
		if len(reads) != 22 {
			t.Errorf("%d read rows, want 22", len(reads))
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
		if len(names) != 144 {
			t.Errorf("%d rows, want 144", len(names))
		}
		for _, name := range names {
			if !al.IsGated(name) {
				t.Errorf("%q is not gated", name)
			}
		}
	})
}
