//go:build !capture

package xtcp

// CaptureCompiledIn reports whether this binary contains the netlink capture
// writer (`-writeFiles` / `-capturePath`). It is a debugging tool, compiled in
// only with `-tags capture`; see netlinker_capture.go.
const CaptureCompiledIn = false

// announceCapture is a no-op: there is no capture writer to arm.
func (x *XTCP) announceCapture() {}

// captureToFileIfEnabled is a no-op without the capture build tag. Startup
// and ConfigService.Set both refuse -writeFiles > 0 on this build (see
// CheckBuildSupport), so the budget is always 0 here.
func (x *XTCP) captureToFileIfEnabled(_ uint32, _ []byte, _ uint32) uint32 { return 0 }
