package goip

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// iproute2Verbs is a hand transcription of each do_ip* function's verb tests
// in iproute2 7.1.0, in source order. A trailing "=" marks a strcmp test,
// which does not abbreviate. It is kept separate from verbs.go on purpose:
// the chains there are built from the write tables, and this is what they
// must add up to.
var iproute2Verbs = map[string][]string{
	// ip/iplink.c:1515-1546
	"link": {"add", "set", "change", "replace", "delete", "show", "lst", "list", "xstats", "afstats", "property", "help"},
	// ip/ipaddress.c:2692-2712
	"address": {"add", "change", "chg=", "replace", "delete", "list", "show", "lst", "flush", "save", "showdump", "restore", "help"},
	// ip/iproute.c:2422-2456
	"route": {"add", "change", "chg=", "replace", "prepend", "append", "test", "delete", "list", "show", "lst", "get", "flush", "save", "restore", "showdump", "help"},
	// ip/ipneigh.c:747-764
	"neighbor": {"add", "change", "chg=", "replace", "delete", "get", "show", "lst", "list", "flush", "help"},
	// ip/iprule.c:1240-1254
	"rule": {"list", "lst", "show", "save", "restore", "add", "delete", "flush", "help"},
	// ip/ipntable.c:688-697
	"ntable": {"change", "chg", "show", "lst", "list", "help"},
	// ip/ipnetconf.c:236-240
	"netconf": {"show", "lst", "list", "help"},
	// ip/ipnexthop.c:1442-1465
	"nexthop": {"add", "replace", "delete", "list", "show", "lst", "get", "flush", "bucket", "help"},
	// ip/ipstats.c:1321-1330
	"stats": {"help=", "show=", "set="},
	// ip/ipaddrlabel.c:248-258
	"addrlabel": {"list", "lst", "show", "add", "delete", "flush", "help"},
	// ip/ipvrf.c:633-647
	"vrf": {"identify", "pids", "exec", "show", "lst", "list", "help"},
}

// objectVerbs is the chain the object table holds for name.
func objectVerbs(t *testing.T, name string) []verb {
	t.Helper()
	for i := range objects {
		if objects[i].name == name {
			return objects[i].verbs
		}
	}
	t.Fatalf("no object %q", name)
	return nil
}

// TestVerbChains checks resolveVerb over the chains, and the chains against
// iproute2.
//
// go test ./internal/goip/ -run TestVerbChains
func TestVerbChains(t *testing.T) {
	tests := []struct {
		name        string
		category    string
		description string
		object      string
		token       string
		// expected: the verb it resolves to and that verb's kind, or
		// wantNone for no match.
		wantName string
		wantKind verbKind
		wantNone bool
	}{
		{
			name: "neigh_s_is_show", category: "positive",
			description: "ip neigh s: nothing before show starts with s",
			object:      "neighbor", token: "s", wantName: "show", wantKind: verbShow,
		},
		{
			name: "addr_s_is_show", category: "positive",
			description: "ip address s: address tests show before save",
			object:      "address", token: "s", wantName: "show", wantKind: verbShow,
		},
		{
			name: "rule_l_is_list", category: "positive",
			description: "ip rule l: list is rule's first verb",
			object:      "rule", token: "l", wantName: "list", wantKind: verbShow,
		},
		{
			name: "neigh_d_is_delete", category: "negative",
			description: "ip neigh d: delete precedes every read",
			object:      "neighbor", token: "d", wantName: "delete", wantKind: verbWrite,
		},
		{
			name: "rule_f_is_flush", category: "negative",
			description: "ip rule f: flush, not a listing",
			object:      "rule", token: "f", wantName: "flush", wantKind: verbWrite,
		},
		{
			name: "link_s_is_set", category: "negative",
			description: "ip link s: do_iplink tests set before show",
			object:      "link", token: "s", wantName: "set", wantKind: verbWrite,
		},
		{
			name: "addr_sa_is_save", category: "negative",
			description: "ip address sa: save, which reads but is not the listing",
			object:      "address", token: "sa", wantName: "save", wantKind: verbOther,
		},
		{
			name: "route_g_is_get", category: "negative",
			description: "ip route g: get, a single lookup and not the listing",
			object:      "route", token: "g", wantName: "get", wantKind: verbOther,
		},
		{
			name: "verb_one_longer", category: "boundary",
			description: "showx: matches() refuses an argument longer than the keyword",
			object:      "neighbor", token: "showx", wantNone: true,
		},
		{
			name: "chg_exact_address", category: "boundary",
			description: "ip address ch is change; chg is compared with strcmp and is its own verb",
			object:      "address", token: "ch", wantName: "change", wantKind: verbWrite,
		},
		{
			name: "chg_prefix_ntable", category: "corner",
			description: "ntable matches chg with matches(), so cha is change and chg is chg",
			object:      "ntable", token: "chg", wantName: "chg", wantKind: verbWrite,
		},
		{
			name: "stats_sh_not_show", category: "corner",
			description: "ip stats sh: stats compares with strcmp, so nothing abbreviates",
			object:      "stats", token: "sh", wantNone: true,
		},
		{
			name: "empty_verb", category: "corner",
			description: "goip's matchesPrefix refuses \"\", where iproute2 would take the first verb; never show",
			object:      "link", token: "", wantNone: true,
		},
		{
			name: "case_sensitive", category: "corner",
			description: "ip link SHOW: matches() is memcmp, so case matters",
			object:      "link", token: "SHOW", wantNone: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.category+"/"+tt.name, func(t *testing.T) {
			v, ok := resolveVerb(objectVerbs(t, tt.object), tt.token)
			if tt.wantNone {
				if ok {
					t.Fatalf("%s: %s %q resolved to %q, want no match", tt.description, tt.object, tt.token, v.name)
				}
				return
			}
			if !ok || v.name != tt.wantName || v.kind != tt.wantKind {
				t.Fatalf("%s: %s %q = (%q, kind %d, %v), want (%q, kind %d)",
					tt.description, tt.object, tt.token, v.name, v.kind, ok, tt.wantName, tt.wantKind)
			}
		})
	}

	t.Run("boundary/verb_exact_length", func(t *testing.T) {
		// Every verb spelled in full resolves to itself: no earlier verb
		// shadows it.
		for _, o := range objects {
			for _, want := range o.verbs {
				if v, ok := resolveVerb(o.verbs, want.name); !ok || v.name != want.name {
					t.Errorf("%s %q resolved to %q (%v)", o.name, want.name, v.name, ok)
				}
			}
		}
	})

	t.Run("corner/chain_order_citation", func(t *testing.T) {
		// Every implemented object's chain equals its transcription, in
		// order, with the same strcmp marks.
		for _, o := range objects {
			if o.run == nil {
				if o.verbs != nil {
					t.Errorf("%s is unimplemented but has a verb chain", o.name)
				}
				continue
			}
			// An alias row (the British neighbor spelling, ntbl) shares the
			// chain of the row it aliases; look it up under that row's name.
			key := o.name
			for _, first := range objects {
				if first.verbs != nil && &first.verbs[0] == &o.verbs[0] {
					key = first.name
					break
				}
			}
			want, ok := iproute2Verbs[key]
			if !ok {
				t.Errorf("%s has no transcription", o.name)
				continue
			}
			got := make([]string, len(o.verbs))
			for i, v := range o.verbs {
				got[i] = v.name
				if v.exact {
					got[i] += "="
				}
			}
			if !slices.Equal(got, want) {
				t.Errorf("%s chain\n got %q\nwant %q", o.name, got, want)
			}
		}
	})

	t.Run("positive/every_chain_lists", func(t *testing.T) {
		// Each implemented object has show, list and lst, and only those
		// are verbShow.
		for _, o := range objects {
			if o.run == nil {
				continue
			}
			var shows []string
			for _, v := range o.verbs {
				if v.kind == verbShow {
					shows = append(shows, v.name)
				}
			}
			slices.Sort(shows)
			want := []string{listVerbCst, lstVerbCst, showVerbCst}
			if o.name == "stats" {
				want = []string{showVerbCst}
			}
			if !slices.Equal(shows, want) {
				t.Errorf("%s show verbs %q, want %q", o.name, shows, want)
			}
		}
	})
}

// TestShowArgs checks the shared listing gate the read-only objects use.
//
// go test ./internal/goip/ -run TestShowArgs
func TestShowArgs(t *testing.T) {
	tests := []struct {
		name        string
		category    string
		description string
		args        []string
		wantRest    []string
		wantErr     string // substring; "" for success
	}{
		{
			name: "show_selectors", category: "positive",
			description: "neigh s dev lo: the selectors follow the verb",
			args:        []string{"s", "dev", "lo"}, wantRest: []string{"dev", "lo"},
		},
		{
			name: "no_verb", category: "boundary",
			description: "a bare object lists",
			args:        []string{}, wantRest: []string{},
		},
		{
			name: "write_named", category: "negative",
			description: "neigh d: refused naming the resolved verb, delete",
			args:        []string{"d"}, wantErr: `neigh "delete"`,
		},
		{
			name: "unknown_named", category: "negative",
			description: "neigh zz: no verb matches, refused naming the token",
			args:        []string{"zz"}, wantErr: `neigh "zz"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.category+"/"+tt.name, func(t *testing.T) {
			rest, err := showArgs("neigh", neighVerbs, tt.args)
			if tt.wantErr != "" {
				if !errors.Is(err, ErrNotImplemented) || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("%s: err %v, want ErrNotImplemented containing %q", tt.description, err, tt.wantErr)
				}
				return
			}
			if err != nil || !slices.Equal(rest, tt.wantRest) {
				t.Fatalf("%s: (%q, %v), want %q", tt.description, rest, err, tt.wantRest)
			}
		})
	}
}
