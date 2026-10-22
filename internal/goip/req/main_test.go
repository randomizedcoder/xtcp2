package req

import (
	"os"
	"testing"
)

// TestMain runs this package's tests as iproute2 7.1.0, the generation that
// captured the testdata/7_1_4 requests they compare against. See
// TestExtMaskLLGenerations for the 7.2.0 default.
func TestMain(m *testing.M) {
	LLMapNameOnly = false
	os.Exit(m.Run())
}
