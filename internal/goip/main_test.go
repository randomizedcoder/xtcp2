package goip

import (
	"os"
	"testing"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/internal/goip/req"
)

// TestMain runs this package's tests as iproute2 7.1.0. They compare goip's
// requests and output with the corpus under pkg/xtcpnl/testdata/7_1_4, which
// was captured with 7.1.0, as its ip_version sidecars record. Setting both
// knobs here, before any test starts, keeps the parallel tests race-free; a
// row about the 7.2.0 default sets them itself, in a sequential test.
func TestMain(m *testing.M) {
	req.LLMapNameOnly = false
	render.RenderQlenZero = false
	os.Exit(m.Run())
}
