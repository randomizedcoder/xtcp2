package goip

import (
	"os"
	"testing"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/internal/goip/req"
)

// TestMain runs this package's tests as iproute2 7.1.0. They compare goip's
// requests and output with the corpus under pkg/xtcpnl/testdata/7_1_4, which
// was captured with 7.1.0, as its ip_version sidecars record. Setting the
// knobs here, before any test starts, keeps the parallel tests race-free; a
// row about a later default sets them itself, in a sequential test. The
// 7.3.0 knobs are off for every corpus up to 7.2.0, and the ws-ip730 tests
// turn them on (useIP730).
func TestMain(m *testing.M) {
	req.LLMapNameOnly = false
	render.RenderQlenZero = false
	render.RenderBondLacpStrict = false
	render.RenderNhDstPort = false
	LLAddrStrict = false
	os.Exit(m.Run())
}
