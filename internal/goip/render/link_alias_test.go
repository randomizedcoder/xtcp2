package render

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// TestLinkViewAlias checks print_linkinfo's alias line (ip/ipaddress.c:
// 1273-1279): `\n    alias %s` after the detail run, before any altname,
// and "ifalias" in JSON, only through WithAlias (NX7c).
//
// go test ./internal/goip/render/ -run TestLinkViewAlias
func TestLinkViewAlias(t *testing.T) {
	tests := []struct {
		description string
		alias       string
		with        bool // WithAlias, as do_link or -d
		altname     string
		expected    string // the text after the link/ line
		json        string // the JSON's ifalias member, "" for none
	}{
		{description: "positive: an alias", alias: "hello", with: true, expected: "\n    alias hello\n",
			json: `"ifalias":"hello"`},
		{description: "positive: before an altname", alias: "hello", with: true, altname: "a0",
			expected: "\n    alias hello\n    altname a0\n", json: `"ifalias":"hello"`},
		{description: "negative: not asked for, as `ip addr show` without -d", alias: "hello",
			expected: "\n"},
		{description: "boundary: no IFLA_IFALIAS", with: true, expected: "\n"},
		{description: "corner: an alias with spaces prints whole", alias: "a b", with: true,
			expected: "\n    alias a b\n", json: `"ifalias":"a b"`},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			li := xtcpnl.LinkInfo{Index: 3, Name: "d0", IfAlias: tc.alias}
			if tc.altname != "" {
				li.AltNames = []string{tc.altname}
			}
			v := LinkViewOf(li, fakeNames{})
			if tc.with {
				v = v.WithAlias(li)
			}
			text := v.Text()
			if i := strings.Index(text, "\n"); i < 0 || !strings.HasSuffix(text[i+1:], tc.expected) ||
				strings.Contains(text, "alias") != strings.Contains(tc.expected, "alias") {
				t.Errorf("text %q, expected to end %q", text, tc.expected)
			}
			b, err := json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			if has := strings.Contains(string(b), `"ifalias"`); has != (tc.json != "") ||
				tc.json != "" && !strings.Contains(string(b), tc.json) {
				t.Errorf("JSON %s, expected %q", b, tc.json)
			}
		})
	}
}
