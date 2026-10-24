package render

import (
	"os"
	"testing"
)

// TestMain runs this package's tests as iproute2 7.1.0, the generation that
// produced the testdata/7_1_4 sidecars they compare against. TestRenderQlenZero
// covers both settings.
func TestMain(m *testing.M) {
	RenderQlenZero = false
	os.Exit(m.Run())
}
