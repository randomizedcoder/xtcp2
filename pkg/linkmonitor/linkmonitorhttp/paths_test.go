package linkmonitorhttp

import (
	"strings"
	"testing"
)

func TestPathsValidate(t *testing.T) {
	reserved := []string{"/metrics", "/healthz", "/readyz", "/debug/pprof/"}
	base := func(edit func(*Paths)) Paths {
		p := Paths{CategoryPrefix: "/metrics/", Categories: []string{"link", "rdma"}, AllPath: "/metrics-all", InterfacePaths: true, Reserved: reserved}
		if edit != nil {
			edit(&p)
		}
		return p
	}
	tests := []struct {
		name, category, description, expectedOutcome string
		paths                                        Paths
		wantErr                                      string
	}{
		{"default_layout", "positive", "go-link-monitor's layout beside xtcp2's reserved paths", "valid", base(nil), ""},
		{"all_path_under_prefix", "positive", "an all-metrics path under the category prefix that is no category", "valid", base(func(p *Paths) { p.AllPath = "/metrics/all" }), ""},
		{"ip_paths_clear", "positive", "/ip enabled with no path under it", "valid", base(func(p *Paths) { p.IPPaths = true }), ""},
		{"unknown_category", "negative", "a category the collector does not know", "category error", base(func(p *Paths) { p.Categories = []string{"bogus"} }), "bogus"},
		{"all_path_reserved", "negative", "the all-metrics path is promPath", "collision", base(func(p *Paths) { p.AllPath = "/metrics" }), `collides with "/metrics"`},
		{"all_path_health", "negative", "the all-metrics path is a health path", "collision", base(func(p *Paths) { p.AllPath = "/healthz" }), `collides with "/healthz"`},
		{"all_path_under_subtree", "negative", "the all-metrics path is under a reserved subtree", "collision", base(func(p *Paths) { p.AllPath = "/debug/pprof/heap" }), `collides with "/debug/pprof/"`},
		{"all_path_category", "negative", "the all-metrics path is a category path", "collision", base(func(p *Paths) { p.AllPath = "/metrics/rdma" }), "category path"},
		{"all_path_ip", "negative", "the all-metrics path is under /i with /ip enabled", "collision", base(func(p *Paths) { p.AllPath, p.IPPaths = "/i/all", true }), "/i paths"},
		{"reserved_ip", "negative", "a reserved path is /ip with /ip enabled", "collision", base(func(p *Paths) { p.Reserved = []string{"/ip"}; p.IPPaths = true }), "/ip paths"},
		{"relative_all_path", "negative", "an all-metrics path without a leading slash", "pattern error", base(func(p *Paths) { p.AllPath = "metrics-all" }), "exact absolute path"},
		{"bad_prefix", "negative", "a category prefix without its trailing slash", "prefix error", base(func(p *Paths) { p.CategoryPrefix = "/metrics" }), "category prefix"},
		{"subtree_root", "boundary", "the all-metrics path is a reserved subtree's root", "collision", base(func(p *Paths) { p.AllPath = "/debug/pprof" }), `collides with "/debug/pprof/"`},
		{"trailing_slash", "boundary", "an all-metrics path with a trailing slash", "pattern error", base(func(p *Paths) { p.AllPath = "/metrics-all/" }), "exact absolute path"},
		{"no_all_path", "boundary", "no all-metrics path and no categories", "valid", base(func(p *Paths) { p.AllPath, p.Categories = "", nil }), ""},
		{"ip_off_ip_path", "boundary", "an all-metrics path under /ip with /ip disabled", "valid", base(func(p *Paths) { p.AllPath = "/ip/all" }), ""},
		{"under_interface_view", "corner", "the all-metrics path under a category's interface paths", "shadowed", base(func(p *Paths) { p.AllPath = "/metrics/link/all" }), "interface paths"},
		{"under_category_no_views", "corner", "the same path without interface paths", "valid", base(func(p *Paths) { p.AllPath, p.InterfacePaths = "/metrics/link/all", false }), ""},
		{"brace_pattern", "corner", "an all-metrics path a ServeMux reads as a wildcard", "pattern error", base(func(p *Paths) { p.AllPath = "/m/{x}" }), "exact absolute path"},
		{"lookalike_prefix", "corner", "an all-metrics path sharing a reserved path's prefix", "valid", base(func(p *Paths) { p.AllPath = "/metricsall" }), ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			err := tc.paths.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate() = %v, want %q", err, tc.wantErr)
			}
		})
	}
}
