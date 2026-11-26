package goip

import (
	"fmt"
	"io"
	"os"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
)

// The GOIP_IP730 seam: one environment variable that sets the whole
// release-knob set from outside the binary, so a check or a shell can pin
// which iproute2 goip renders as without rebuilding.
//
// GOIP_IP730=1 is 7.3.0, the parity target and the default; GOIP_IP730=0 is
// 7.2.0 and earlier, for the corpora captured with them. Any other
// non-empty value is refused rather than ignored: a check that means to pin
// a release and mistypes the value must fail loudly, not silently render the
// default.
//
// The set is useWsO5Release's list (ws_differential_test.go), which keeps the
// harness and the binary on the same knobs: Keywords730, LLAddrStrict,
// RenderBondLacpStrict, RenderNhDstPort, RenderOvpnMode, and
// RenderBondPartnerU8 — the last with its polarity inverted, because the
// knob names the older behavior it switches on.

// releaseEnvCst is the environment variable's name.
const releaseEnvCst = "GOIP_IP730"

// applyReleaseEnv sets the release knobs from GOIP_IP730, reporting a bad
// value. An unset or empty variable leaves the defaults.
func applyReleaseEnv(stderr io.Writer) int {
	v := os.Getenv(releaseEnvCst)
	if v == "" {
		return 0
	}
	ip730 := v == "1"
	if !ip730 && v != "0" {
		fmt.Fprintf(stderr, "goip: %s: %q is neither 1 nor 0\n", releaseEnvCst, v)
		return ExitUsage
	}
	Keywords730 = ip730
	LLAddrStrict = ip730
	render.RenderBondLacpStrict = ip730
	render.RenderNhDstPort = ip730
	render.RenderOvpnMode = ip730
	render.RenderBondPartnerU8 = !ip730
	return 0
}
