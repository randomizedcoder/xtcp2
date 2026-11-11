package goip

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// NX6a (goip-parity-next.md): route show's RTA_ENCAP, held to every
// readback of the lwt and seg6 spaces.

// wsEncapDecoded are the encap types render.EncapViewOf prints.
var wsEncapDecoded = []string{"mpls", "seg6"}

// wsEncapTypes are the types after each `encap` in ip's output, less
// seg6's `mode encap`.
func wsEncapTypes(out []byte) []string {
	var types []string
	f := strings.Fields(string(out))
	for i := range len(f) - 1 {
		if f[i] == "encap" && (i == 0 || f[i-1] != "mode") && !slices.Contains(types, f[i+1]) {
			types = append(types, f[i+1])
		}
	}
	return types
}

// TestWsEncapReadbacks replays each space's readbacks. One where ip
// printed only decoded encap types must match ip; one with another type
// must still differ, so a new decoder shows up here as rows to move.
//
// go test ./internal/goip/ -run TestWsEncapReadbacks
func TestWsEncapReadbacks(t *testing.T) {
	useLLMap720(t)
	t.Cleanup(func() { useWsO5Release("") })
	tests := []struct {
		description string
		space       string
		readbacks   int // compared
		decoded     int // with an mpls or seg6 encap, each matching ip
		gap         int // with another type, each differing
	}{
		{description: "positive: lwt IPv4 routes, mpls among ip and ip6", space: "lwtroute4", readbacks: 18, decoded: 6, gap: 7},
		{description: "positive: lwt IPv6 routes, one mpls among ip6", space: "lwtroute6", readbacks: 8, decoded: 1, gap: 7},
		{description: "positive: lwt multipath hops, each hop's encap first", space: "lwthop", readbacks: 7, decoded: 2, gap: 4},
		{description: "positive: seg6 IPv6 routes, hmac and tunsrc among them", space: "seg6route6", readbacks: 16, decoded: 12},
		{description: "positive: seg6 IPv4 routes", space: "seg6route4", readbacks: 3, decoded: 2},
		{description: "corner: lwtx requests, every route refused, so no encap", space: "lwtxreq", readbacks: 6},
		{description: "positive: lwtx hops, seg6 per hop", space: "lwtxhop", readbacks: 4, decoded: 4},
		{description: "corner: MPLS-family routes under -f mpls, which carry no RTA_ENCAP", space: "mpls", readbacks: 230},
		{description: "negative: seg6local routes, not decoded", space: "seg6local", readbacks: 21, gap: 15},
		{description: "negative: ila routes, not decoded", space: "ila", readbacks: 10, gap: 6},
		{description: "negative: xfrm routes, not decoded", space: "xfrm", readbacks: 9, gap: 6},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			useWsO5Release(tc.space)
			ents, err := os.ReadDir(wsO5SpacePath(tc.space))
			if err != nil {
				t.Fatal(err)
			}
			n, decoded, gap := 0, 0, 0
			for _, e := range ents {
				dir := filepath.Join(wsO5SpacePath(tc.space), e.Name())
				b, err := os.ReadFile(filepath.Join(dir, "cmd.json"))
				if err != nil {
					t.Fatal(err)
				}
				var row struct {
					Readback []struct{ Cmd, Name string } `json:"readback"`
				}
				if err := json.Unmarshal(b, &row); err != nil {
					t.Fatal(err)
				}
				for _, r := range row.Readback {
					if !strings.Contains(r.Cmd, " route ") {
						continue // nexthop show prints NHA_ENCAP, not yet rendered
					}
					out, err := os.ReadFile(filepath.Join(dir, r.Name+".out"))
					if err != nil {
						t.Fatal(err)
					}
					n++
					id := e.Name() + "/" + r.Name
					types := wsEncapTypes(out)
					undecoded := slices.ContainsFunc(types, func(s string) bool { return !slices.Contains(wsEncapDecoded, s) })
					d := wsReadbackDiffers(t, dir, r.Name, r.Cmd)
					switch {
					case undecoded:
						gap++
						if d == "" {
							t.Errorf("%s (%s) prints %v and matches ip: move it to decoded", id, r.Cmd, types)
						}
					case d != "":
						t.Errorf("%s (%s): %s", id, r.Cmd, d)
					case len(types) > 0:
						decoded++
					}
				}
			}
			if n != tc.readbacks || decoded != tc.decoded || gap != tc.gap {
				t.Errorf("%d readbacks, %d decoded, %d gap; want %d, %d, %d", n, decoded, gap, tc.readbacks, tc.decoded, tc.gap)
			}
		})
	}
}
