package listeners

import (
	"errors"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
)

func TestParseSpec(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		category    string
		description string
		in          string
		expected    string // "" = ok; else an error substring
		check       func(*xtcp_config.Listener) bool
	}{
		{"full", "positive", "every key",
			"name=m,serves=health+metrics,address=/run/m.sock,network=unix,mode=0640,unlinkStale=false,auth=raw,tokenFile=/t,hmacKeyFile=/h,adminAuth=hmac,adminTokenFile=/at,adminHmacKeyFile=/ah",
			"", func(l *xtcp_config.Listener) bool {
				return l.GetName() == "m" && len(l.GetServes()) == 2 && l.GetEndpoint().GetUnixSocketMode() == 0o640 &&
					!l.GetEndpoint().GetUnlinkStaleUnixSocket() && l.GetAuth().GetRawTokenFile() == "/t" &&
					l.GetAuth().GetHmacSharedKeyFile() == "/h" && l.GetAdminAuth().GetRawTokenFile() == "/at" &&
					l.GetAdminAuth().GetHmacSharedKeyFile() == "/ah"
			}},
		{"minimal", "positive", "name, serves, address; unlinkStale defaults true; auth inherits",
			"name=m,serves=metrics,address=/run/m.sock", "", func(l *xtcp_config.Listener) bool {
				return l.GetEndpoint().GetUnlinkStaleUnixSocket() && l.GetAuth() == nil
			}},
		{"no_name", "negative", "name is required", "serves=metrics,address=/x", "name= is required", nil},
		{"no_address", "negative", "address is required", "name=m,serves=metrics", "address= is required", nil},
		{"unknown_key", "negative", "an unknown key is refused", "name=m,address=/x,token=s3cret", "unknown key", nil},
		{"unknown_class", "negative", "an unknown class is refused", "name=m,address=/x,serves=metrics+pprof", "unknown endpoint class", nil},
		{"not_kv", "negative", "a field without =", "name=m,address=/x,serves", "not key=value", nil},
		{"dup_key", "corner", "a key given twice", "name=m,name=n,address=/x", "given twice", nil},
		{"bad_mode", "boundary", "a mode past 0777", "name=m,address=/x,mode=01000", "octal permission", nil},
		{"spaces", "corner", "spaces around fields are trimmed", " name=m , serves= metrics ,address=/x ", "", func(l *xtcp_config.Listener) bool {
			return l.GetName() == "m" && len(l.GetServes()) == 1
		}},
	}
	for _, tc := range cases {
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			l, err := ParseSpec(tc.in)
			if tc.expected != "" {
				if err == nil || !strings.Contains(err.Error(), tc.expected) || !errors.Is(err, ErrConfig) {
					t.Fatalf("%s: err = %v, want ErrConfig containing %q", tc.description, err, tc.expected)
				}
				return
			}
			if err != nil {
				t.Fatalf("%s: %v", tc.description, err)
			}
			if tc.check != nil && !tc.check(l) {
				t.Errorf("%s: parsed %v", tc.description, l)
			}
		})
	}
}

func TestParseList(t *testing.T) {
	t.Parallel()
	ls, err := ParseList("name=a,serves=metrics,address=/a;; name=b,serves=health,address=/b;")
	if err != nil || len(ls) != 2 || ls[1].GetName() != "b" {
		t.Fatalf("ParseList = %v, %v", ls, err)
	}
	if _, err := ParseList("name=a,serves=metrics,address=/a;bogus"); err == nil {
		t.Fatal("want an error for a bad entry")
	}
	if ls, err := ParseList(""); err != nil || len(ls) != 0 {
		t.Fatalf("empty = %v, %v", ls, err)
	}
}

func TestParseClassRoundTrip(t *testing.T) {
	t.Parallel()
	for c, n := range classNames {
		got, err := ParseClass(strings.ToUpper(n))
		if err != nil || got != c || ClassName(c) != n {
			t.Errorf("class %v: ParseClass(%q) = %v, %v", c, n, got, err)
		}
	}
	if len(classNames) != len(xtcp_config.EndpointClass_name)-1 {
		t.Errorf("classNames covers %d classes, proto has %d", len(classNames), len(xtcp_config.EndpointClass_name)-1)
	}
}
