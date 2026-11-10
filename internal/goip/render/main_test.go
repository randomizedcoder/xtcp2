package render

import (
	"os"
	"testing"
)

// TestMain runs this package's tests as iproute2 7.1.0, the generation that
// produced the testdata/7_1_4 sidecars they compare against. TestRenderQlenZero
// covers both settings. The 7.3.0 knobs are off too, and their tests set
// each side.
func TestMain(m *testing.M) {
	RenderQlenZero = false
	RenderBondLacpStrict = false
	RenderNhDstPort = false
	os.Exit(m.Run())
}
