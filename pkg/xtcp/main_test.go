package xtcp

import (
	"os"
	"testing"
)

// TestMain disables the hard startup capability check for this package's
// tests so NewXTCP / NewNsTestingXTCP (→ Init) run to completion on
// unprivileged CI sandboxes that lack CAP_SYS_ADMIN / CAP_NET_ADMIN.
// The capability logic itself is exercised directly, with the real
// method, in init_capabilities_test.go — the seam only short-circuits
// the Init() startup gate that would otherwise os.Exit the test binary.
func TestMain(m *testing.M) {
	SetCapabilityCheck(func(*XTCP) error { return nil })
	os.Exit(m.Run())
}
