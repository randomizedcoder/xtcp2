package goiphttp

import (
	"errors"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/internal/goip"
	"golang.org/x/sys/unix"
)

func objectAt(t *testing.T, path string) *object {
	t.Helper()
	for _, o := range objects {
		if o.path == path {
			return o
		}
	}
	t.Fatalf("no object at %q", path)
	return nil
}

// TestParams is the query-string allowlist, without a server.
//
// go test ./internal/goip/goiphttp/ -run TestParams
func TestParams(t *testing.T) {
	tests := []struct {
		name        string
		description string
		path        string
		query       string
		pathDev     string
		want        goip.ShowRequest // checked when wantErr is false
		wantText    bool
		wantErr     bool
	}{
		{name: "addr_bare", description: "positive: no parameters is ip -j address show",
			path: "addr", want: goip.ShowRequest{Object: "address", JSON: true}},
		{name: "addr_dev_family", description: "positive: dev and family=inet6",
			path: "addr", query: "dev=eth0&family=inet6",
			want: goip.ShowRequest{Object: "address", Family: unix.AF_INET6, JSON: true, Selectors: []string{"dev", "eth0"}}},
		{name: "addr_family_link", description: "positive: family=link is -0 on addr",
			path: "addr", query: "family=link", want: goip.ShowRequest{Object: "address", Family: unix.AF_PACKET, JSON: true}},
		{name: "route_table_all", description: "positive: table=all",
			path: "route", query: "table=all", want: goip.ShowRequest{Object: "route", JSON: true, Selectors: []string{"table", "all"}}},
		{name: "link_text_stats_details", description: "positive: text with -s -d",
			path: "link", query: "format=text&stats=1&details=true",
			want: goip.ShowRequest{Object: "link", Stats: true, Details: true}, wantText: true},
		{name: "neigh_proxy", description: "positive: a flag selector emits its keyword alone",
			path: "neigh", query: "proxy=1", want: goip.ShowRequest{Object: "neighbor", JSON: true, Selectors: []string{"proxy"}}},
		{name: "path_dev", description: "positive: /ip/addr/{interface} is dev <interface>",
			path: "addr", pathDev: "eth0", want: goip.ShowRequest{Object: "address", JSON: true, Selectors: []string{"dev", "eth0"}}},
		{name: "stats_order", description: "positive: selectors follow registry order, not query order",
			path: "stats", query: "suite=x&subgroup=y&group=link&dev=eth0",
			want: goip.ShowRequest{Object: "stats", JSON: true, Selectors: []string{"dev", "eth0", "group", "link", "subgroup", "y", "suite", "x"}}},

		{name: "unknown_param", description: "negative: an unknown parameter is refused, not ignored",
			path: "addr", query: "foo=1", wantErr: true},
		{name: "repeated_param", description: "negative: a repeated parameter is refused",
			path: "addr", query: "dev=a&dev=b", wantErr: true},
		{name: "param_wrong_object", description: "negative: table belongs to route, not link",
			path: "link", query: "table=main", wantErr: true},
		{name: "family_link_on_route", description: "negative: family=link only means something on addr",
			path: "route", query: "family=link", wantErr: true},
		{name: "family_bad", description: "negative: family=4 is not a spelling the paths accept",
			path: "route", query: "family=4", wantErr: true},
		{name: "format_bad", description: "negative: format=yaml",
			path: "addr", query: "format=yaml", wantErr: true},
		{name: "dev_twice", description: "negative: dev in both path and query",
			path: "addr", query: "dev=eth0", pathDev: "eth1", wantErr: true},
		{name: "vrf_any_param", description: "negative: vrf takes nothing",
			path: "vrf", query: "family=inet", wantErr: true},
		{name: "nexthop_id_exclusive", description: "negative: nexthop id cannot be combined with dev",
			path: "nexthop", query: "id=1&dev=eth0", wantErr: true},

		{name: "dev_15", description: "boundary: a 15-byte name is accepted",
			path: "link", query: "dev=abcdefghijklmno",
			want: goip.ShowRequest{Object: "link", JSON: true, Selectors: []string{"dev", "abcdefghijklmno"}}},
		{name: "dev_16", description: "boundary: 16 bytes exceeds IFNAMSIZ-1",
			path: "link", query: "dev=abcdefghijklmnop", wantErr: true},
		{name: "dev_empty", description: "boundary: an empty dev is refused",
			path: "link", query: "dev=", wantErr: true},
		{name: "table_max", description: "boundary: 2^32-1 is a table",
			path: "route", query: "table=4294967295",
			want: goip.ShowRequest{Object: "route", JSON: true, Selectors: []string{"table", "4294967295"}}},
		{name: "table_over", description: "boundary: 2^32 is not",
			path: "route", query: "table=4294967296", wantErr: true},
		{name: "table_leading_zero", description: "boundary: a number is re-spelled canonically",
			path: "route", query: "table=0254",
			want: goip.ShowRequest{Object: "route", JSON: true, Selectors: []string{"table", "254"}}},
		{name: "protocol_255", description: "boundary: protocol is 8 bits",
			path: "nexthop", query: "protocol=255",
			want: goip.ShowRequest{Object: "nexthop", JSON: true, Selectors: []string{"protocol", "255"}}},
		{name: "protocol_256", description: "boundary: 256 is out of range",
			path: "nexthop", query: "protocol=256", wantErr: true},
		{name: "bool_zero", description: "boundary: stats=0 is refused, not read as off",
			path: "addr", query: "stats=0", wantErr: true},
		{name: "bool_empty", description: "boundary: stats= is refused",
			path: "addr", query: "stats=", wantErr: true},

		{name: "dev_dotdot", description: "corner: dev=.. is refused",
			path: "addr", query: "dev=..", wantErr: true},
		{name: "dev_slash", description: "corner: dev with / is refused",
			path: "addr", query: "dev=a%2Fb", wantErr: true},
		{name: "dev_keyword", description: "corner: dev=show is a value after dev, never a keyword",
			path: "addr", query: "dev=show",
			want: goip.ShowRequest{Object: "address", JSON: true, Selectors: []string{"dev", "show"}}},
		{name: "dev_nul", description: "corner: NUL in a name",
			path: "addr", query: "dev=a%00b", wantErr: true},
		{name: "dev_space", description: "corner: space in a name",
			path: "addr", query: "dev=a%20b", wantErr: true},
		{name: "token_upper", description: "corner: stats group must be lower-case",
			path: "stats", query: "group=LINK", wantErr: true},
		{name: "table_name_negative", description: "corner: table=-1",
			path: "route", query: "table=-1", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			q, err := url.ParseQuery(tc.query)
			if err != nil {
				t.Fatal(err)
			}
			got, err := objectAt(t, tc.path).parse(q, tc.pathDev)
			if tc.wantErr {
				if !errors.Is(err, errParam) {
					t.Fatalf("%s: err = %v, want errParam", tc.description, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("%s: %v", tc.description, err)
			}
			if got.text != tc.wantText {
				t.Errorf("%s: text = %v, want %v", tc.description, got.text, tc.wantText)
			}
			if !slices.Equal(got.show.Argv(), tc.want.Argv()) {
				t.Errorf("%s: argv = %q, want %q", tc.description, got.show.Argv(), tc.want.Argv())
			}
		})
	}
}

// TestRegistry guards the registry against goip: every path names a goip
// show object and every goip show object has a path.
func TestRegistry(t *testing.T) {
	t.Run("covers_show_objects", func(t *testing.T) {
		// positive: the two sets are equal.
		got := make([]string, 0, len(objects))
		for _, o := range objects {
			got = append(got, o.object)
		}
		slices.Sort(got)
		if want := goip.ShowObjects(); !slices.Equal(got, want) {
			t.Fatalf("registry objects %q, goip.ShowObjects %q", got, want)
		}
	})
	t.Run("no_duplicate_paths_or_params", func(t *testing.T) {
		// negative: a duplicate would make one entry unreachable.
		seen := map[string]bool{}
		for _, o := range objects {
			if seen[o.path] {
				t.Errorf("duplicate path %q", o.path)
			}
			seen[o.path] = true
			if len(o.byName) != len(o.params) {
				t.Errorf("%s: duplicate parameter name", o.path)
			}
			if _, ok := o.byName[formatParam]; ok {
				t.Errorf("%s: format is global and must not be an object parameter", o.path)
			}
		}
	})
	t.Run("dev_path_has_dev", func(t *testing.T) {
		// boundary: devPath without a dev parameter would 400 every request.
		for _, o := range objects {
			if _, ok := o.byName["dev"]; ok != o.devPath {
				t.Errorf("%s: devPath %v but dev param %v", o.path, o.devPath, ok)
			}
		}
	})
	t.Run("paths_are_url_safe", func(t *testing.T) {
		// corner: every path segment is a plain lower-case word.
		for _, o := range objects {
			if o.path == "" || strings.Trim(o.path, "abcdefghijklmnopqrstuvwxyz") != "" {
				t.Errorf("path %q is not a lower-case word", o.path)
			}
		}
	})
}
