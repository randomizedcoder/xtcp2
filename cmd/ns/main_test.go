package main

import (
	"os"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp"
)

// TestMain disables xtcp's hard startup capability check so the tests
// that construct a real XTCP via xtcp.NewNsTestingXTCP (e.g.
// TestRunDaemonDefault_constructs) run to completion on unprivileged CI
// sandboxes that lack CAP_SYS_ADMIN / CAP_NET_ADMIN.
func TestMain(m *testing.M) {
	xtcp.SetCapabilityCheck(func(*xtcp.XTCP) error { return nil })
	os.Exit(m.Run())
}
