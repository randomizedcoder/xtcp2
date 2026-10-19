package goip

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// TestResolveShow covers the exported resolver row by row.
//
// go test ./internal/goip/ -run TestResolveShow
func TestResolveShow(t *testing.T) {
	tests := []struct {
		name        string
		category    string
		description string
		// inputs are one or more command lines that must all give expected.
		inputs [][]string
		// expected: the full command line and the selectors, or an error
		// and a substring of its message.
		wantArgv      string
		wantSelectors []string
		wantErr       error
		wantMsg       string
	}{
		{
			name: "a_s", category: "positive",
			description: "a s: address precedes every object, show precedes save",
			inputs:      [][]string{{"a", "s"}}, wantArgv: "address show",
		},
		{
			name: "ad_s", category: "positive",
			description: "ad s: still address, not addrlabel, which comes second",
			inputs:      [][]string{{"ad", "s"}}, wantArgv: "address show",
		},
		{
			name: "addrl", category: "positive",
			description: "addrl: addrlabel, with the default verb",
			inputs:      [][]string{{"addrl"}}, wantArgv: "addrlabel show",
		},
		{
			name: "r", category: "positive",
			description: "r: route precedes rule",
			inputs:      [][]string{{"r"}}, wantArgv: "route show",
		},
		{
			name: "n", category: "positive",
			description: "n: neighbor precedes netns, netconf and nexthop",
			inputs:      [][]string{{"n"}}, wantArgv: "neighbor show",
		},
		{
			name: "options_long_short", category: "positive",
			description: "the same options abbreviated and spelled in full, in either order, give one request",
			inputs: [][]string{
				{"-stat", "-de", "-j", "a"},
				{"-s", "-details", "-json", "address"},
				{"--json", "-statistics", "-d", "-d", "address"},
			},
			wantArgv: "-stats -details -json address show",
		},
		{
			name: "default_verb", category: "positive",
			description: "link: no verb is show, as in ip",
			inputs:      [][]string{{"link"}}, wantArgv: "link show",
		},
		{
			name: "family_last_wins", category: "positive",
			description: "-4 -6: ip overwrites preferred_family, so the last one counts",
			inputs:      [][]string{{"-4", "-6", "r"}, {"-6", "--", "r"}}, wantArgv: "-6 route show",
		},
		{
			name: "list_spelling_kept", category: "positive",
			description: "a l: list is a show verb and keeps its own name",
			inputs:      [][]string{{"a", "l"}}, wantArgv: "address list",
		},
		{
			name: "l_s_write", category: "negative",
			description: "l s: link tests set before show",
			inputs:      [][]string{{"l", "s"}}, wantErr: ErrWriteVerb, wantMsg: "ip link set",
		},
		{
			name: "a_f_write", category: "negative",
			description: "a f: flush",
			inputs:      [][]string{{"a", "f"}}, wantErr: ErrWriteVerb, wantMsg: "ip address flush",
		},
		{
			name: "a_sa_read", category: "negative",
			description: "a sa: save reads but is not the listing, so it is not served (not a write)",
			inputs:      [][]string{{"a", "sa"}}, wantErr: ErrNotImplemented, wantMsg: "ip address save",
		},
		{
			name: "netns_refused", category: "negative",
			description: "-n is -netns, which would enter another namespace",
			inputs:      [][]string{{"-n", "x", "link"}, {"-netns", "x", "link"}, {"-ne", "x", "link"}, {"-n"}},
			wantErr:     ErrRefusedOption, wantMsg: "-netns",
		},
		{
			name: "batch_refused", category: "negative",
			description: "-b is -batch, as ip tests it before -brief",
			inputs:      [][]string{{"-b", "f"}, {"-batch", "f"}, {"-ba", "f", "link"}},
			wantErr:     ErrRefusedOption, wantMsg: "-batch",
		},
		{
			name: "force_refused", category: "negative",
			description: "-force; -f alone is -family",
			inputs:      [][]string{{"-force", "link"}, {"-fo", "link"}},
			wantErr:     ErrRefusedOption, wantMsg: "-force",
		},
		{
			name: "loops_help_rcvbuf_refused", category: "negative",
			description: "-loops, -help (and goip's -h) and -rcvbuf, which Run or ip take but a read-only path does not",
			inputs: [][]string{
				{"-l", "1", "link"}, {"-loops", "1", "link"}, {"-h"}, {"-help", "link"}, {"-rc", "1", "link"},
			},
			wantErr: ErrRefusedOption,
		},
		{
			name: "family_not_implemented", category: "negative",
			description: "-f is ip's -family, which goip does not implement",
			inputs:      [][]string{{"-f", "inet", "link"}, {"-pretty", "link"}},
			wantErr:     ErrNotImplemented,
		},
		{
			name: "unknown_option", category: "negative",
			description: "-zz matches no option of ip's",
			inputs:      [][]string{{"-zz", "link"}, {"---json", "link"}},
			wantErr:     ErrInvalidArgument, wantMsg: "is unknown",
		},
		{
			name: "unknown_object", category: "negative",
			description: "zz is no object",
			inputs:      [][]string{{"zz"}}, wantErr: ErrUnknownObject,
		},
		{
			name: "unimplemented_object", category: "negative",
			description: "m is maddress, which goip does not implement",
			inputs:      [][]string{{"m"}}, wantErr: ErrNotImplemented, wantMsg: "maddress",
		},
		{
			name: "unknown_verb", category: "negative",
			description: "link eth0: no verb matches; ip says the command is unknown",
			inputs:      [][]string{{"link", "eth0"}}, wantErr: ErrInvalidArgument, wantMsg: `"eth0"`,
		},
		{
			name: "resolve_brief", category: "positive",
			description: "-stat -br a and -s -brief address: one request, with Brief (IA1-T02)",
			inputs:      [][]string{{"-stat", "-br", "a"}, {"-s", "-brief", "address"}},
			wantArgv:    "-stats -brief address show",
		},
		{
			name: "stats_two", category: "boundary",
			description: "-s -s is refused with Run's message",
			inputs:      [][]string{{"-s", "-s", "link"}}, wantErr: ErrInvalidArgument, wantMsg: "`-s -s`",
		},
		{
			name: "no_object", category: "boundary",
			description: "options and nothing else",
			inputs:      [][]string{{}, {"-j"}, {"--"}}, wantErr: ErrInvalidArgument, wantMsg: "no object",
		},
		{
			name: "object_longer_than_keyword", category: "boundary",
			description: "addressx: matches() refuses a longer argument",
			inputs:      [][]string{{"addressx"}}, wantErr: ErrUnknownObject,
		},
		{
			name: "stats_verb_exact", category: "boundary",
			description: "stats sh: ip compares stats verbs with strcmp",
			inputs:      [][]string{{"stats", "sh"}}, wantErr: ErrInvalidArgument,
		},
		{
			name: "empty_token_object", category: "corner",
			description: `"" as the object: goip refuses it, and the tokenizer never produces one`,
			inputs:      [][]string{{""}, {"-j", ""}}, wantErr: ErrUnknownObject,
		},
		{
			name: "case_sensitive", category: "corner",
			description: "A S and Link: matches() is memcmp",
			inputs:      [][]string{{"A", "S"}, {"Link"}}, wantErr: ErrUnknownObject,
		},
		{
			name: "dev_not_abbreviated", category: "corner",
			description: "route show d lo: the resolver passes d through for route's parser, which compares dev with strcmp",
			inputs:      [][]string{{"route", "show", "d", "lo"}}, wantArgv: "route show d lo", wantSelectors: []string{"d", "lo"},
		},
		{
			name: "selectors_untouched", category: "corner",
			description: "a s up scope global: the selectors are not resolved",
			inputs:      [][]string{{"a", "s", "up", "scope", "global"}}, wantArgv: "address show up scope global",
			wantSelectors: []string{"up", "scope", "global"},
		},
		{
			name: "alias_object", category: "corner",
			description: "ntbl keeps its spelling, and the request uses the canonical ntable",
			inputs:      [][]string{{"ntbl", "lst"}}, wantArgv: "ntbl lst",
		},
	}
	for _, tt := range tests {
		t.Run(tt.category+"/"+tt.name, func(t *testing.T) {
			var first ShowRequest
			for i, in := range tt.inputs {
				r, res, err := ResolveShow(in)
				if tt.wantErr != nil {
					if !errors.Is(err, tt.wantErr) || !strings.Contains(err.Error(), tt.wantMsg) {
						t.Fatalf("%s: ResolveShow(%q) err %v, want %v containing %q", tt.description, in, err, tt.wantErr, tt.wantMsg)
					}
					continue
				}
				if err != nil {
					t.Fatalf("%s: ResolveShow(%q): %v", tt.description, in, err)
				}
				if got := strings.Join(res.Argv(), " "); got != tt.wantArgv {
					t.Errorf("%s: ResolveShow(%q) argv %q, want %q", tt.description, in, got, tt.wantArgv)
				}
				if tt.wantSelectors != nil && !slices.Equal(r.Selectors, tt.wantSelectors) {
					t.Errorf("%s: selectors %q, want %q", tt.description, r.Selectors, tt.wantSelectors)
				}
				if !slices.Contains(showObjects, r.Object) {
					t.Errorf("%s: request object %q is not one Show serves", tt.description, r.Object)
				}
				if i == 0 {
					first = r
				} else if !reflect.DeepEqual(r, first) {
					t.Errorf("%s: ResolveShow(%q) = %+v, want %+v as for %q", tt.description, in, r, first, tt.inputs[0])
				}
			}
		})
	}
}

// TestResolveShowRequest checks the ShowRequest ResolveShow builds against
// the fields Show reads.
//
// go test ./internal/goip/ -run TestResolveShowRequest
func TestResolveShowRequest(t *testing.T) {
	tests := []struct {
		name        string
		category    string
		description string
		in          []string
		expected    ShowRequest
	}{
		{
			name: "every_field", category: "positive",
			description: "-0 -s -d -j n s dev lo",
			in:          []string{"-0", "-s", "-d", "-j", "n", "s", "dev", "lo"},
			expected: ShowRequest{
				Object: "neighbor", Family: unix.AF_PACKET, JSON: true, Stats: 1, Details: true,
				Selectors: []string{"dev", "lo"},
			},
		},
		{
			name: "output_modes", category: "positive",
			description: "-br -o a: both output modes reach the request",
			in:          []string{"-br", "-o", "a"},
			expected:    ShowRequest{Object: "address", Brief: true, Oneline: true, Selectors: []string{}},
		},
		{
			name: "bare", category: "boundary",
			description: "ntbl: the alias maps to ntable, no options, no selectors",
			in:          []string{"ntbl"},
			expected:    ShowRequest{Object: "ntable", Selectors: []string{}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.category+"/"+tt.name, func(t *testing.T) {
			r, _, err := ResolveShow(tt.in)
			if err != nil || !reflect.DeepEqual(r, tt.expected) {
				t.Fatalf("%s: (%+v, %v), want %+v", tt.description, r, err, tt.expected)
			}
			if got, want := r.Argv(), tt.expected.Argv(); !slices.Equal(got, want) {
				t.Fatalf("%s: Argv %q, want %q", tt.description, got, want)
			}
		})
	}
}

// TestResolvedPath checks the canonical path.
//
// go test ./internal/goip/ -run TestResolvedPath
func TestResolvedPath(t *testing.T) {
	tests := []struct {
		name        string
		category    string
		description string
		in          []string
		expected    string
	}{
		{
			name: "short", category: "positive",
			description: "-j a s dev lo",
			in:          []string{"-j", "a", "s", "dev", "lo"}, expected: "/ip/-json/address/show/dev/lo",
		},
		{
			name: "prefix_escaped", category: "corner",
			description: "a prefix selector stays one segment",
			in:          []string{"r", "s", "10.0.0.0/8"}, expected: "/ip/route/show/10.0.0.0%2F8",
		},
		{
			name: "percent_escaped", category: "corner",
			description: "a literal % is escaped, so the path decodes back to the token",
			in:          []string{"l", "sh", "dev", "a%b"}, expected: "/ip/link/show/dev/a%25b",
		},
	}
	for _, tt := range tests {
		t.Run(tt.category+"/"+tt.name, func(t *testing.T) {
			_, res, err := ResolveShow(tt.in)
			if err != nil || res.Path() != tt.expected {
				t.Fatalf("%s: (%q, %v), want %q", tt.description, res.Path(), err, tt.expected)
			}
		})
	}
}

// TestShortest checks Shortest against the tables.
//
// go test ./internal/goip/ -run TestShortest
func TestShortest(t *testing.T) {
	tests := []struct {
		name        string
		category    string
		description string
		key         string
		expected    string
	}{
		{name: "address", category: "positive", description: "a is address", key: "address", expected: "a"},
		{name: "addrlabel", category: "positive", description: "addrl: addr is still address", key: "addrlabel", expected: "addrl"},
		{name: "route", category: "positive", description: "r is route", key: "route", expected: "r"},
		{name: "rule", category: "positive", description: "ru: r is route", key: "rule", expected: "ru"},
		{name: "link_show", category: "positive", description: "sh: s is set", key: "link show", expected: "sh"},
		{name: "address_show", category: "positive", description: "s: address tests show before save", key: "address show", expected: "s"},
		{name: "netconf", category: "boundary", description: "netc: net is netns", key: "netconf", expected: "netc"},
		{name: "stats_show", category: "corner", description: "show: stats compares with strcmp", key: "stats show", expected: "show"},
		{name: "ntbl", category: "corner", description: "ntb: nt is ntable, tested first", key: "ntbl", expected: "ntb"},
	}
	got := Shortest()
	for _, tt := range tests {
		t.Run(tt.category+"/"+tt.name, func(t *testing.T) {
			if got[tt.key] != tt.expected {
				t.Fatalf("%s: Shortest()[%q] = %q, want %q", tt.description, tt.key, got[tt.key], tt.expected)
			}
		})
	}

	t.Run("boundary/shortest_is_shortest", func(t *testing.T) {
		// For every keyword, its spelling resolves to it and one character
		// less does not.
		for key, short := range got {
			obj, verb, isVerb := strings.Cut(key, " ")
			argv := func(tok string) []string {
				if isVerb {
					return []string{obj, tok}
				}
				return []string{tok}
			}
			_, res, err := ResolveShow(argv(short))
			if err != nil || (isVerb && res.Verb != verb) || (!isVerb && res.Object != obj) {
				t.Errorf("%s: %q gives (%+v, %v)", key, short, res, err)
			}
			if len(short) < 2 {
				continue
			}
			_, res, err = ResolveShow(argv(short[:len(short)-1]))
			if err == nil && ((isVerb && res.Verb == verb) || (!isVerb && res.Object == obj)) {
				t.Errorf("%s: %q, one shorter than %q, still resolves to it", key, short[:len(short)-1], short)
			}
		}
	})

	t.Run("positive/covers_every_object", func(t *testing.T) {
		for _, o := range objects {
			if _, ok := got[o.name]; ok != (o.run != nil) {
				t.Errorf("%s: in Shortest %v, implemented %v", o.name, ok, o.run != nil)
			}
		}
	})

	t.Run("corner/copy", func(t *testing.T) {
		got["address"] = "zz"
		if Shortest()["address"] != "a" {
			t.Fatal("Shortest's map is shared")
		}
	})
}

// TestResolvePrefixProperty is the exhaustive check: every prefix of every
// object, and every prefix of every verb of every implemented object,
// resolves to what walking the tables by hand gives, and every non-show
// result is an error.
//
// go test ./internal/goip/ -run TestResolvePrefixProperty
func TestResolvePrefixProperty(t *testing.T) {
	// byHand is a deliberately separate first-match walk, so the property
	// does not test resolveVerb with itself.
	byHand := func(chain []verb, tok string) (verb, bool) {
		for _, v := range chain {
			if v.exact && tok == v.name || !v.exact && strings.HasPrefix(v.name, tok) {
				return v, true
			}
		}
		return verb{}, false
	}
	t.Run("corner/objects", func(t *testing.T) {
		for _, o := range objects {
			for n := 1; n <= len(o.name); n++ {
				p := o.name[:n]
				var want object
				for _, c := range objects {
					if strings.HasPrefix(c.name, p) {
						want = c
						break
					}
				}
				_, res, err := ResolveShow([]string{p})
				switch {
				case want.run == nil:
					if !errors.Is(err, ErrNotImplemented) || !strings.Contains(err.Error(), want.name) {
						t.Errorf("%q: err %v, want ErrNotImplemented naming %s", p, err, want.name)
					}
				case err != nil || res.Object != want.name:
					t.Errorf("%q: (%q, %v), want %s", p, res.Object, err, want.name)
				}
			}
		}
	})
	t.Run("corner/verbs", func(t *testing.T) {
		checked := 0
		for _, o := range objects {
			for _, v := range o.verbs {
				for n := 1; n <= len(v.name); n++ {
					p := v.name[:n]
					want, ok := byHand(o.verbs, p)
					_, res, err := ResolveShow([]string{o.name, p})
					checked++
					switch {
					case !ok:
						if !errors.Is(err, ErrInvalidArgument) {
							t.Errorf("%s %q: err %v, want an unknown command", o.name, p, err)
						}
					case want.kind == verbShow:
						if err != nil || res.Verb != want.name {
							t.Errorf("%s %q: (%q, %v), want %s", o.name, p, res.Verb, err, want.name)
						}
					case want.kind == verbWrite:
						if !errors.Is(err, ErrWriteVerb) || !strings.Contains(err.Error(), "ip "+o.name+" "+want.name+":") {
							t.Errorf("%s %q: err %v, want ErrWriteVerb naming %s", o.name, p, err, want.name)
						}
					default:
						if !errors.Is(err, ErrNotImplemented) || !strings.Contains(err.Error(), "ip "+o.name+" "+want.name+":") {
							t.Errorf("%s %q: err %v, want ErrNotImplemented naming %s", o.name, p, err, want.name)
						}
					}
				}
			}
		}
		if checked < 500 {
			t.Fatalf("only %d prefixes checked", checked)
		}
	})
	t.Run("corner/options", func(t *testing.T) {
		// Every prefix of every option resolves to ip's first match, and
		// only the display options are served.
		for _, row := range ipOptions {
			for n := 2; n <= len(row.name); n++ {
				p := row.name[:n]
				if row.exact && p != row.name {
					continue
				}
				var want *ipOption
				for i := range ipOptions {
					c := &ipOptions[i]
					if c.exact && p == c.name || !c.exact && strings.HasPrefix(c.name, p) {
						want = c
						break
					}
				}
				if p == "-h" {
					want = helpOption
				}
				in := []string{p}
				if want.arg != "" {
					in = append(in, "1")
				}
				in = append(in, "link")
				_, res, err := ResolveShow(in)
				switch {
				case want.refused:
					if !errors.Is(err, ErrRefusedOption) || !strings.Contains(err.Error(), want.name) {
						t.Errorf("%q: err %v, want ErrRefusedOption naming %s", p, err, want.name)
					}
				case want.set == nil:
					if !errors.Is(err, ErrNotImplemented) {
						t.Errorf("%q: err %v, want ErrNotImplemented", p, err)
					}
				case err != nil || len(res.Options) != 1:
					t.Errorf("%q: (%q, %v), want one option", p, res.Options, err)
				}
			}
		}
	})
}
