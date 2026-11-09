package goipparity

import (
	"slices"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
)

// inetSlugPrefixCst marks KG0-T02's `link set dev d0 inet …` rows.
const inetSlugPrefixCst = "link_inet_"

// TestInetMatrixRows pins KG0-T02's rows (goip-link-kinds.md, "KG0-T02 The
// inet matrix rows"): KC1-T01's 11 rows the kernel answers, in its order,
// each the d0 lookup and the write, and none of the 7 ip refuses before
// netlink, since a write row needs goip to exit 0.
//
// go test ./internal/goipparity/ -run TestInetMatrixRows
func TestInetMatrixRows(t *testing.T) {
	var rows []Command
	for _, c := range Commands() {
		if strings.HasPrefix(c.Slug, inetSlugPrefixCst) {
			rows = append(rows, c)
		}
	}
	slugs := make([]string, 0, len(rows))
	for _, c := range rows {
		slugs = append(slugs, strings.TrimPrefix(c.Slug, inetSlugPrefixCst))
	}
	t.Run("positive: the rows are KC1's kernel-answered rows, in order", func(t *testing.T) {
		want := []string{"forwarding_on", "rp_filter_max", "rp_filter_over", "tag_hex", "arp_ignore_octal",
			"medium_id_neg1", "medium_id_neg2", "several", "every_param", "repeated", "then_mtu"}
		if !slices.Equal(slugs, want) {
			t.Errorf("slugs %v, want %v", slugs, want)
		}
	})
	t.Run("negative: no ip-refused row is a matrix row", func(t *testing.T) {
		for _, refused := range []string{"then_garbage", "bad_on_off", "bad_number", "no_param", "unknown_param",
			"abbreviated", "no_value"} {
			if slices.Contains(slugs, refused) {
				t.Errorf("%s is a matrix row; goip exits non-zero on it", refused)
			}
		}
	})
	t.Run("boundary: each row is an implemented write with floor 4", func(t *testing.T) {
		for _, c := range rows {
			if c.Floor != 4 || !c.Write || !c.Implemented {
				t.Errorf("%s: Floor=%d Write=%v Implemented=%v, want 4, true, true", c.Slug, c.Floor, c.Write, c.Implemented)
			}
		}
	})
	t.Run("positive: every row is gated (V135, V136)", func(t *testing.T) {
		al, err := nlparity.EmbeddedAllowlist()
		if err != nil {
			t.Fatalf("EmbeddedAllowlist: %v", err)
		}
		for _, c := range rows {
			if !al.IsGated(c.Name) {
				t.Errorf("%s (%q) is not gated", c.Slug, c.Name)
			}
		}
	})
	t.Run("corner: every row sets up d0 alone", func(t *testing.T) {
		for _, c := range rows {
			if !slices.Equal(c.Setup, linkWriteSetupDummy) {
				t.Errorf("%s: Setup %v, want %v", c.Slug, c.Setup, linkWriteSetupDummy)
			}
		}
	})
}
