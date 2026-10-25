package xtcpnl

import (
	"bytes"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// The vxcan encoder's tests, against the vxcan rows of the 7.3.0 kind
// corpus (docs/netlink/goip-link-kinds.md, "KP5-T03 The vrf, vrf_slave and
// vxcan parsers").

var vxcanKind = greRow("vxcan")

// vxcanCorpusWords are the option words after `type vxcan` of every row
// that sent a request (kind-write-corpus.exp), keyed by slug: 6 accepted
// and 3 refused by the kernel.
func vxcanCorpusWords() map[string]string {
	return map[string]string{
		"add_no_option":      "",
		"add_peer":           "peer",
		"add_peer_name":      "peer name vc1",
		"add_peer_bare_name": "peer vc1",
		"add_up":             "",
		"add_peer_samename":  "peer name vc0",
		"add_peer_up":        "peer up",
		"set_peer":           "peer name vc1",
		"set_no_option":      "",
	}
}

// vxcanArgs parses option words as vxcan_parse_opt does, for well-formed
// words: peer, then the peer half iplink_parse takes.
func vxcanArgs(t testing.TB, words string, ifindex map[string]uint64, set bool) *VethPeer {
	t.Helper()
	argv := strings.Fields(words)
	if len(argv) == 0 {
		return nil
	}
	if argv[0] != "peer" {
		t.Fatalf("vxcan words %q don't start with peer", words)
	}
	return peerArgs(t, argv[1:], ifindex, set)
}

func TestEncodeVxcanInfoData(t *testing.T) {
	type row struct {
		description string
		peer        *VethPeer
		expected    []byte
		wantErr     bool
	}
	nest := func(flags uint32, attrs ...[]byte) []byte {
		return rtattr(VxcanInfoPeer, slices.Concat(append([][]byte{EncodeIfInfomsg(unix.AF_UNSPEC, 0, flags, flags)},
			attrs...)...))
	}
	tests := []row{
		{
			description: "negative: a peer attribute too long for the buffer",
			peer:        &VethPeer{Attrs: []LinkAttr{{Type: unix.IFLA_IFALIAS, Payload: make([]byte, linkWriteBufCst)}}},
			wantErr:     true,
		},
		{
			description: "boundary: no option words",
			peer:        nil,
			expected:    nil,
		},
		{
			description: "boundary: peer alone",
			peer:        vxcanArgs(t, "peer", nil, false),
			expected:    nest(0),
		},
		{
			description: "corner: a bare word names the peer",
			peer:        vxcanArgs(t, "peer vc1", nil, false),
			expected:    nest(0, rtattr(unix.IFLA_IFNAME, []byte("vc1\x00"))),
		},
		{
			description: "corner: the peer's flags",
			peer:        vxcanArgs(t, "peer up", nil, false),
			expected:    nest(unix.IFF_UP),
		},
	}
	words := vxcanCorpusWords()
	ents, err := os.ReadDir(tdLinkWrite_7_3_0 + "/kinds/vxcan")
	if err != nil {
		t.Fatal(err)
	}
	sent := 0
	for _, e := range ents {
		slug := e.Name()
		data, has, ok := vxcanKind.request(t, slug)
		w, listed := words[slug]
		if ok != listed {
			t.Errorf("%s: sent a request %v, listed %v", slug, ok, listed)
		}
		if !ok || !listed {
			continue
		}
		sent++
		var want []byte
		if has {
			want = append([]byte(nil), data...)
		}
		tests = append(tests, row{
			description: fmt.Sprintf("positive: %s's request", slug),
			peer:        vxcanArgs(t, w, greIfindex(t, vxcanKind, slug), strings.HasPrefix(slug, "set_")),
			expected:    want,
		})
	}
	if sent != 9 {
		t.Errorf("%d vxcan rows sent a request, want 9", sent)
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got, err := EncodeVxcanInfoData(tt.peer)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err: got %v, want an error %v", err, tt.wantErr)
			}
			if (got == nil) != (tt.expected == nil) || !bytes.Equal(got, tt.expected) {
				t.Errorf("got  % x\nwant % x", got, tt.expected)
			}
		})
	}
}
