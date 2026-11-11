//go:build capture

package xtcp

import (
	"log"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// The netlink capture writer is a debugging tool: it writes raw inet_diag
// replies, which carry every socket's addresses, to disk. It is compiled in
// only with `-tags capture` and no shipped flavor or image sets that tag.

// CaptureCompiledIn reports whether this binary contains the netlink capture
// writer (`-writeFiles` / `-capturePath`).
const CaptureCompiledIn = true

// Raw netlink captures carry host addresses: owner read/write only.
const writeFilesPermissionsCst = 0o600

// debugCaptureEnabled is 1 while this process is armed to write captures.
var debugCaptureEnabled = prometheus.NewGauge(prometheus.GaugeOpts{
	Namespace: "xtcp2",
	Subsystem: "debug",
	Name:      "capture_enabled",
	Help:      "1 when the netlink capture writer is compiled in and armed (-writeFiles > 0)",
})

func init() {
	prometheus.MustRegister(debugCaptureEnabled)
}

// announceCapture warns, once at Init, when the capture writer is armed.
// Compiled in but idle (-writeFiles 0) it says nothing and the gauge is 0.
func (x *XTCP) announceCapture() {
	if x.config.GetWriteFiles() == 0 {
		debugCaptureEnabled.Set(0)
		return
	}
	debugCaptureEnabled.Set(1)
	log.Printf("WARNING: netlink capture is ARMED: up to %d raw netlink packets per netlinker "+
		"will be written under %q. Captures hold the addresses and ports of every socket on this "+
		"host. This is a debugging feature; do not run it in production.",
		x.config.GetWriteFiles(), x.config.GetCapturePath())
}

// captureToFileIfEnabled writes the packet to disk when the WriteFiles
// budget is positive. Returns the updated budget — 0 means "stop trying
// for the rest of this goroutine's life" (a write error disables
// captures permanently rather than killing the daemon).
func (x *XTCP) captureToFileIfEnabled(wf uint32, packet []byte, id uint32) uint32 {
	if wf == 0 {
		return wf
	}
	now := time.Now()
	// Capture only the n bytes Recvfrom actually filled. Writing the
	// raw *packetBuffer here used to dump the full pool-buffer size
	// (e.g. 8 KiB), trailing the real packet with stale bytes from a
	// previous Recvfrom — pcap-like consumers parsed garbage past
	// the real end of the message.
	err := os.WriteFile(
		x.config.CapturePath+"netlink."+now.Format(time.RFC3339Nano),
		packet,
		writeFilesPermissionsCst)
	if err == nil {
		return wf - 1
	}
	// Diagnostic capture-to-file is a side feature; a disk-full /
	// EACCES / etc. here must NOT take down the daemon (and every
	// other netlinker for every other namespace with it). Stop
	// capturing further packets and count the failure.
	x.pC.WithLabelValues("Netlinker", "WriteFile", "error").Inc()
	if x.debugLevel > 10 {
		log.Printf("Netlinker %d WriteFile err (disabling further captures): %v", id, err)
	}
	return 0
}
