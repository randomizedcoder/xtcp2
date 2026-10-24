package goip

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/internal/goip/req"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// TestParseLinkShowArgs pins `ip link show`'s selector loop for the two arms
// goip implements, `type` and `[dev] NAME` (ip/ipaddress.c:2170-2253).
//
// go test ./internal/goip/ -run TestParseLinkShowArgs
func TestParseLinkShowArgs(t *testing.T) {
	type want struct {
		dev                     string
		haveDev                 bool
		kind, slaveKind         string
		haveKind, haveSlaveKind bool
	}
	tests := []struct {
		name, category, description string
		args                        []string
		expected                    want
		wantErr                     error
		wantInErr                   string
	}{
		{
			name: "type", category: "positive", description: "`type vxlan` is the kind",
			args: []string{"type", "vxlan"}, expected: want{kind: "vxlan", haveKind: true},
		},
		{
			name: "slave", category: "positive", description: "`type bond_slave` is the slave kind bond",
			args: []string{"type", "bond_slave"}, expected: want{slaveKind: "bond", haveSlaveKind: true},
		},
		{
			name: "dev_and_type", category: "positive", description: "`dev lo type veth` sets both, in either order",
			args: []string{"type", "veth", "dev", "lo"}, expected: want{dev: "lo", haveDev: true, kind: "veth", haveKind: true},
		},
		{
			name: "bare_and_type", category: "positive", description: "a bare name after `type T` is the device",
			args: []string{"type", "veth", "lo"}, expected: want{dev: "lo", haveDev: true, kind: "veth", haveKind: true},
		},
		{
			name: "type_twice", category: "corner", description: "no duparg: a second kind overwrites the first",
			args: []string{"type", "veth", "type", "dummy"}, expected: want{kind: "dummy", haveKind: true},
		},
		{
			name: "kind_and_slave", category: "corner", description: "a `_slave` word leaves filter.kind alone, so both hold",
			args:     []string{"type", "veth", "type", "bridge_slave"},
			expected: want{kind: "veth", haveKind: true, slaveKind: "bridge", haveSlaveKind: true},
		},
		{
			name: "suffix_only", category: "boundary", description: "`type _slave` is an empty slave kind (soff 0)",
			args: []string{"type", "_slave"}, expected: want{slaveKind: "", haveSlaveKind: true},
		},
		{
			name: "short_word", category: "boundary", description: "a word shorter than `_slave` is a kind",
			args: []string{"type", "slave"}, expected: want{kind: "slave", haveKind: true},
		},
		{
			name: "empty_kind", category: "boundary", description: "`type \"\"` is an empty kind, still set",
			args: []string{"type", ""}, expected: want{kind: "", haveKind: true},
		},
		{
			name: "type_missing", category: "negative", description: "`type` with no value is NEXT_ARG's error",
			args: []string{"type"}, wantErr: ErrInvalidArgument, wantInErr: "Command line is not complete",
		},
		{
			name: "type_abbrev", category: "negative", description: "`typ` is strcmp'd, so it is a device and `vxlan` a second one",
			args: []string{"typ", "vxlan"}, wantErr: ErrInvalidArgument, wantInErr: `"dev" is duplicate, or "vxlan"`,
		},
		{
			name: "other_keyword", category: "negative", description: "`up` is ip's filter, which goip's link show refuses",
			args: []string{"type", "veth", "up"}, wantErr: ErrNotImplemented, wantInErr: `"up"`,
		},
		{
			name: "help", category: "negative", description: "`he` abbreviates help",
			args: []string{"he"}, wantErr: ErrNotImplemented, wantInErr: `"he"`,
		},
		{
			name: "dev_missing", category: "negative", description: "`dev` with no name is NEXT_ARG's error",
			args: []string{"type", "veth", "dev"}, wantErr: ErrInvalidArgument, wantInErr: "Command line is not complete",
		},
	}
	for _, tc := range tests {
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			s, err := parseLinkShowArgs(tc.args)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) || !strings.Contains(err.Error(), tc.wantInErr) {
					t.Fatalf("%s: err = %v, want %v containing %q", tc.description, err, tc.wantErr, tc.wantInErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("%s: %v", tc.description, err)
			}
			got := want{
				dev: s.dev, haveDev: s.haveDev,
				kind: s.filter.kind, haveKind: s.filter.haveKind,
				slaveKind: s.filter.slaveKind, haveSlaveKind: s.filter.haveSlaveKind,
			}
			if got != tc.expected {
				t.Errorf("%s: got %+v, want %+v", tc.description, got, tc.expected)
			}
		})
	}
}

// TestLinkShowKindFiltered is print_linkinfo's two kind tests over hand-built
// links (ip/ipaddress.c:221-227, :1020-1024).
//
// go test ./internal/goip/ -run TestLinkShowKindFiltered
func TestLinkShowKindFiltered(t *testing.T) {
	veth := xtcpnl.LinkInfo{HasLinkInfo: true, Kind: "veth"}
	port := xtcpnl.LinkInfo{HasLinkInfo: true, Kind: "veth", SlaveKind: "bridge"}
	bare := xtcpnl.LinkInfo{}
	nest := xtcpnl.LinkInfo{HasLinkInfo: true}
	tests := []struct {
		name, category, description string
		args                        []string
		link                        xtcpnl.LinkInfo
		expected                    bool // filtered
	}{
		{"kind_match", "positive", "a veth is kept by `type veth`", []string{"type", "veth"}, veth, false},
		{"slave_match", "positive", "a bridge port is kept by `type bridge_slave`", []string{"type", "bridge_slave"}, port, false},
		{"both_match", "positive", "both tests hold", []string{"type", "veth", "type", "bridge_slave"}, port, false},
		{"kind_miss", "negative", "a veth is dropped by `type dummy`", []string{"type", "dummy"}, veth, true},
		{"slave_miss", "negative", "a veth with no master is dropped by `type bridge_slave`", []string{"type", "bridge_slave"}, veth, true},
		{"one_of_two", "negative", "the kind holds and the slave kind does not", []string{"type", "veth", "type", "bond_slave"}, port, true},
		{"no_linkinfo", "boundary", "a link with no IFLA_LINKINFO never matches, even an empty kind", []string{"type", ""}, bare, true},
		{"empty_nest", "corner", "a nest with no kind compares as \"\", so `type \"\"` keeps it", []string{"type", ""}, nest, false},
		{"no_filter", "corner", "no `type` filters nothing", nil, bare, false},
	}
	for _, tc := range tests {
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			s, err := parseLinkShowArgs(tc.args)
			if err != nil {
				t.Fatalf("%s: %v", tc.description, err)
			}
			if got := s.kindFiltered(&tc.link); got != tc.expected {
				t.Errorf("%s: filtered = %v, want %v", tc.description, got, tc.expected)
			}
		})
	}
}

// TestLinkShowType runs `link show type` over the 7_1_8 dump, which holds
// three bridges, three veths (one a bridge port) and five links of other
// kinds or none. The replay answers the kind-carrying dump with every link,
// as a kernel without the kind's module does, so the client-side test is
// what the output shows.
//
// go test ./internal/goip/ -run TestLinkShowType
func TestLinkShowType(t *testing.T) {
	run := func(t *testing.T, args []string, asJSON bool) (string, *recordingSource) {
		t.Helper()
		src := &recordingSource{inner: openReplay(t, linkDumpPcap)}
		var out bytes.Buffer
		c := &runCtx{src: src, lltab: NewLLTab(), out: &out, errOut: &bytes.Buffer{}, json: asJSON}
		if err := linkShow(c, args); err != nil {
			t.Fatalf("linkShow(%q): %v", args, err)
		}
		return out.String(), src
	}
	plain, _ := run(t, nil, false)
	stanzas := map[string]string{}
	for _, s := range linkStanzas(plain) {
		stanzas[linkStanzaName(s)] = s
	}

	tests := []struct {
		name, category, description string
		args                        []string
		expected                    []string // the printed links, in order
		wantKindOnWire              string   // "" for a dump with no IFLA_LINKINFO
	}{
		{"bridge", "positive", "the three bridges", []string{"type", "bridge"},
			[]string{"virbr0", "docker0", "br-3a5828b2963a"}, "bridge"},
		{"veth", "positive", "the three veths", []string{"type", "veth"},
			[]string{"ve-nfb-vpn", "ve-nordlayepDd-", "veth179a698"}, "veth"},
		{"slave", "positive", "the one bridge port; the slave kind is not sent", []string{"type", "bridge_slave"},
			[]string{"veth179a698"}, ""},
		{"none", "negative", "a kind no link has prints nothing", []string{"type", "dummy"}, nil, "dummy"},
		{"empty", "boundary", "`type \"\"` matches no link here: each nest has a kind", []string{"type", ""}, nil, ""},
		{"kind_and_slave", "corner", "both tests: a veth that is a bridge port", []string{"type", "veth", "type", "bridge_slave"},
			[]string{"veth179a698"}, "veth"},
	}
	for _, tc := range tests {
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			text, src := run(t, tc.args, false)
			var want strings.Builder
			for _, n := range tc.expected {
				want.WriteString(stanzas[n])
			}
			if text != want.String() {
				t.Errorf("%s: text\n%s\nwant the unfiltered show's stanzas\n%s", tc.description, text, want.String())
			}

			if len(src.requests) != 1 {
				t.Fatalf("%s: %d requests, want 1", tc.description, len(src.requests))
			}
			wantReq, err := req.LinkShowDump(req.ExtMaskShow, 0)
			if s, _ := parseLinkShowArgs(tc.args); s.filter.haveKind {
				wantReq, err = req.LinkShowDumpKind(req.ExtMaskShow, s.filter.kind, 0)
			}
			if err != nil {
				t.Fatal(err)
			}
			got := append([]byte(nil), src.requests[0]...)
			copy(got[8:12], make([]byte, 4)) // the sequence number
			if !bytes.Equal(got, wantReq) {
				t.Errorf("%s: request\n got  %x\n want %x", tc.description, got, wantReq)
			}
			if tc.wantKindOnWire != "" && !bytes.Contains(got, []byte(tc.wantKindOnWire)) {
				t.Errorf("%s: request does not carry %q", tc.description, tc.wantKindOnWire)
			}

			js, _ := run(t, tc.args, true)
			var arr []map[string]any
			if err := json.Unmarshal([]byte(js), &arr); err != nil {
				t.Fatalf("%s: JSON %q: %v", tc.description, js, err)
			}
			if len(arr) != wantLinkStanzas {
				t.Errorf("%s: %d JSON objects, want one per reply (%d)", tc.description, len(arr), wantLinkStanzas)
			}
			var names []string
			for _, o := range arr {
				if len(o) == 0 {
					continue
				}
				names = append(names, o["ifname"].(string))
			}
			if strings.Join(names, ",") != strings.Join(tc.expected, ",") {
				t.Errorf("%s: JSON links %q, want %q, the rest `{}`", tc.description, names, tc.expected)
			}
		})
	}
}

// TestLinkShowDevType is `link show dev NAME type T`: the same two gets as
// `dev NAME`, then print_linkinfo's kind test on the reply.
//
// go test ./internal/goip/ -run TestLinkShowDevType
func TestLinkShowDevType(t *testing.T) {
	run := func(args ...string) (int, string) {
		t.Setenv("GOIP_REPLAY", guestDumpsDir+"netlink_route_getlink_dev.pcap")
		var stdout, stderr bytes.Buffer
		code := Run(args, &stdout, &stderr)
		return code, stdout.String() + stderr.String()
	}
	_, dev := run("link", "show", "dev", "goip0")
	tests := []struct {
		name, category, description string
		args                        []string
		expected                    string // "dev" is the `dev goip0` output
	}{
		{"match", "positive", "goip0 is a dummy", []string{"link", "show", "dev", "goip0", "type", "dummy"}, "dev"},
		{"bare_match", "positive", "with the bare name, type first", []string{"link", "show", "type", "dummy", "goip0"}, "dev"},
		{"miss", "negative", "a veth filter prints nothing", []string{"link", "show", "dev", "goip0", "type", "veth"}, ""},
		{"miss_json", "corner", "under -j the dropped link is still an object", []string{"-j", "link", "show", "goip0", "type", "veth"}, "[{}]\n"},
		{"slave_miss", "boundary", "goip0 has no master, so no slave kind", []string{"link", "show", "goip0", "type", "dummy_slave"}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			code, out := run(tc.args...)
			want := tc.expected
			if want == "dev" {
				want = dev
			}
			if code != ExitOK || out != want {
				t.Errorf("%s: Run(%q) = %d %q, want 0 %q", tc.description, tc.args, code, out, want)
			}
		})
	}
}

// linkStanzas splits `link show` text into stanzas, each from its
// `N: name:` line up to the next.
func linkStanzas(text string) []string {
	var out []string
	for _, line := range strings.SplitAfter(text, "\n") {
		if line == "" {
			continue
		}
		if line[0] >= '0' && line[0] <= '9' || len(out) == 0 {
			out = append(out, line)
			continue
		}
		out[len(out)-1] += line
	}
	return out
}

// linkStanzaName is the stanza's link name, without an `@peer` suffix.
func linkStanzaName(stanza string) string {
	_, rest, _ := strings.Cut(stanza, ": ")
	name, _, _ := strings.Cut(rest, ":")
	name, _, _ = strings.Cut(name, "@")
	return name
}
