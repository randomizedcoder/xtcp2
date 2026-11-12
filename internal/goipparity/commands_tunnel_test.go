//go:build goip_write

package goipparity

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
)

// KC5's twelve kinds, each a corpus directory and a slug's middle word.
const (
	greKindCst       = "gre"
	gretapKindCst    = "gretap"
	erspanKindCst    = "erspan"
	ip6greKindCst    = "ip6gre"
	ip6gretapKindCst = "ip6gretap"
	ip6erspanKindCst = "ip6erspan"
	ipipKindCst      = "ipip"
	sitKindCst       = "sit"
	ip6tnlKindCst    = "ip6tnl"
	vtiKindCst       = "vti"
	vti6KindCst      = "vti6"
	xfrmKindCst      = ProtoXfrmCst
)

// TestIpTunnelMatrixRows pins KG4-T09's rows (goip-link-kinds.md,
// "KG4-T09 The IP tunnel matrix rows"): one write row per KC5 row that
// sent a request, each with its capture's floor and its topology's steps,
// none of the rows ip refuses before netlink, and the 24 read rows.
//
// go test ./internal/goipparity/ -run TestIpTunnelMatrixRows
func TestIpTunnelMatrixRows(t *testing.T) {
	kinds := []udpOverlayKind{
		{greKindCst, "gr0", greWriteSetupGr0, 4, 31, 14},
		{gretapKindCst, "gt0", gretapWriteSetupGt0, 4, 10, 1},
		{erspanKindCst, "es0", erspanWriteSetupEs0, 4, 19, 6},
		{ip6greKindCst, "g6", ip6greWriteSetupG6, 4, 26, 10},
		{ip6gretapKindCst, "gt6", ip6gretapWriteSetupGt6, 4, 9, 0},
		{ip6erspanKindCst, "es6", ip6erspanWriteSetupEs6, 4, 15, 4},
		{ipipKindCst, "ip0", ipipWriteSetupIp0, 4, 34, 13},
		{sitKindCst, "st0", sitWriteSetupSt0, 4, 26, 11},
		{ip6tnlKindCst, "t6", ip6tnlWriteSetupT6, 4, 36, 13},
		{vtiKindCst, "vt0", vtiReadSetupVt0, 6, 16, 7},
		{vti6KindCst, "v6", vti6ReadSetupV6, 6, 9, 3},
		{xfrmKindCst, "xf0", xfrmWriteSetupD0Xf0Dev, 6, 17, 6},
	}
	isTunnel := func(kind string) bool {
		return slices.ContainsFunc(kinds, func(k udpOverlayKind) bool {
			return k.kind == kind
		})
	}
	got := map[string][]string{}
	var rows []Command
	for _, c := range Commands() {
		if kind, slug, ok := kindRowKind(c.Slug); ok && isTunnel(kind) {
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
		if len(rows) != 248 {
			t.Errorf("%d rows, want 248", len(rows))
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
		if n != 88 {
			t.Errorf("%d ip-refused rows, want 88", n)
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
	t.Run("corner: a set's floor counts the prefetch", func(t *testing.T) {
		for _, tc := range []struct {
			slug     string
			expected int
		}{
			{"link_gre_set_changeable", 6},
			{"link_ipip_set_dev", 8},
			{"link_gre_set_dev", 8},
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
			{"link add dev gt6 type ip6gretap remote 2001:db8::1", "link_ip6gretap_add_beside_ip6gre"},
			{"link set dev t6 type ip6tnl remote 2001:db8::2", "link_ip6tnl_set_collide"},
			{"link set dev xf0 type xfrm if_id 2", "link_xfrm_set_drop_dev"},
		}
		for _, r := range respelled {
			if seen[r.name] != r.expected {
				t.Errorf("%q is %q, want %s", r.name, seen[r.name], r.expected)
			}
		}
	})
	var reads []string
	t.Run("positive: the 24 read rows", func(t *testing.T) {
		for _, tc := range kinds {
			for _, name := range []string{"-d link show dev " + tc.dev, "-d -j link show dev " + tc.dev} {
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
		if len(reads) != 24 {
			t.Errorf("%d read rows, want 24", len(reads))
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
		if len(names) != 272 {
			t.Errorf("%d rows, want 272", len(names))
		}
		for _, name := range names {
			if !al.IsGated(name) {
				t.Errorf("%q is not gated", name)
			}
		}
	})
}
