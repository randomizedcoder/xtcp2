package goip

// The EP2 crafted monitor files: the committed spec/.nl pairs under
// testdata/monitor, replayed through goip's `monitor file` path at both
// release-knob sets, each against the ip output the corpus committed from
// measurement (docs/netlink/goip-parity-events.md, "EP2-T02 the blobs").
// The blobs are inputs the kernel cannot produce — the bond partner states
// past eight bits, the P14 reason u32 read as a u8, the CC_ALG payload with
// garbage past its first NUL, a nested-flagged RTA_METRICS — so nothing in
// the corpus can stand in for them.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
)

// blobDir is the committed spec/.nl/.out set.
const blobDir = "testdata/monitor"

// releaseKnobState is a snapshot of the release-knob set, taken by the tests
// that set it, so they restore what was there rather than the compile-time
// defaults — TestMain runs this package's tests as an older iproute2
// (main_test.go), and "the default" inside a test is whatever it set.
type releaseKnobState struct {
	keywords, lladdr, lacp, nhdstport, ovpn, partnerU8 bool
}

func snapshotReleaseKnobs() releaseKnobState {
	return releaseKnobState{
		keywords: Keywords730, lladdr: LLAddrStrict,
		lacp: render.RenderBondLacpStrict, nhdstport: render.RenderNhDstPort,
		ovpn: render.RenderOvpnMode, partnerU8: render.RenderBondPartnerU8,
	}
}

func (s releaseKnobState) restore() {
	Keywords730, LLAddrStrict = s.keywords, s.lladdr
	render.RenderBondLacpStrict, render.RenderNhDstPort = s.lacp, s.nhdstport
	render.RenderOvpnMode, render.RenderBondPartnerU8 = s.ovpn, s.partnerU8
}

// TestMonitorBlobs is every committed blob through `monitor file` at both
// knobs: GOIP_IP730=1 renders as the pinned 7.3.0 ip does, =0 as the 7.2.0
// one, byte for byte, the -d the corpus measured with. The red case the
// table exists for is bond-partner-hionly: 256 at u16, 0 at u8.
//
// go test ./internal/goip/ -run TestMonitorBlobs
func TestMonitorBlobs(t *testing.T) {
	t.Cleanup(snapshotReleaseKnobs().restore)
	nls, err := filepath.Glob(filepath.Join(blobDir, "*.nl"))
	if err != nil || len(nls) == 0 {
		t.Fatalf("glob %s: %v", blobDir, err)
	}
	for _, nl := range nls {
		name := strings.TrimSuffix(filepath.Base(nl), ".nl")
		for _, knob := range []struct {
			env, out string
		}{{"1", ".ip730.out"}, {"0", ".ip720.out"}} {
			t.Run(name+"/ip730="+knob.env, func(t *testing.T) {
				t.Setenv("GOIP_IP730", knob.env)
				if rc := applyReleaseEnv(&bytes.Buffer{}); rc != 0 {
					t.Fatalf("applyReleaseEnv: rc %d", rc)
				}
				want, err := os.ReadFile(filepath.Join(blobDir, name+knob.out))
				if err != nil {
					t.Fatal(err)
				}
				var out, errOut bytes.Buffer
				c := newRunCtx(nil, &out, &errOut)
				c.showDetails = 1 // the -d the corpus measured with
				m := newMonitor(c, &monitorArgs{})
				if err := m.fromFile(nl); err != nil {
					t.Fatal(err)
				}
				if got := out.Bytes(); !bytes.Equal(got, want) {
					t.Errorf("%s at GOIP_IP730=%s:\n got %q\nwant %q",
						name, knob.env, got, want)
				}
				if errOut.Len() != 0 {
					t.Errorf("stderr: %q", errOut.String())
				}
			})
		}
	}
}

// TestReleaseEnv is the GOIP_IP730 seam: the whole knob set set both ways,
// and a value that is neither 1 nor 0 refused loudly rather than ignored —
// a check that means to pin a release and mistypes the value must fail, not
// silently render the default. The rows' baseline is the ambient release
// (TestMain's), which is what an unset variable must leave.
//
// go test ./internal/goip/ -run TestReleaseEnv
func TestReleaseEnv(t *testing.T) {
	ambient := snapshotReleaseKnobs()
	t.Cleanup(ambient.restore)
	knobs := func() string {
		s := snapshotReleaseKnobs()
		return strings.Join([]string{
			boolStr(s.keywords), boolStr(s.lladdr), boolStr(s.lacp),
			boolStr(s.nhdstport), boolStr(s.ovpn), boolStr(s.partnerU8),
		}, " ")
	}
	tests := []struct {
		description, env string
		want             string
		rc               int
	}{
		{"positive: 7.3.0, the parity target", "1", "true true true true true false", 0},
		{"positive: 7.2.0, the corpora's other pin", "0", "false false false false false true", 0},
		{"boundary: unset leaves what was there", "", knobs(), 0},
		{"negative: a value that is neither", "x", knobs(), ExitUsage},
		{"negative: 01 is neither", "01", knobs(), ExitUsage},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			// The knobs are package state; a row must start from the
			// ambient release or it inherits whatever the row before it set.
			ambient.restore()
			t.Setenv("GOIP_IP730", tc.env)
			var stderr bytes.Buffer
			if rc := applyReleaseEnv(&stderr); rc != tc.rc {
				t.Errorf("rc %d, want %d", rc, tc.rc)
			}
			if got := knobs(); got != tc.want {
				t.Errorf("knobs %s, want %s", got, tc.want)
			}
			if tc.rc != 0 && stderr.Len() == 0 {
				t.Error("a refusal printed nothing")
			}
		})
	}
}

// boolStr is one knob as the table above spells it.
func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
