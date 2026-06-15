package xtcp

import (
	"fmt"
	"log"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

// capgetFunc is unix.Capget by default; tests swap it to inject capability
// bits without needing real CAP_SYS_ADMIN.
var capgetFunc = unix.Capget

// requiredCap describes one Linux capability the daemon needs and the
// failure mode if it's missing. The `fatal` flag distinguishes
// hard-required (start refuses without it) from soft-required (warning
// printed; daemon still starts, related features degrade or fail at
// runtime).
type requiredCap struct {
	bit    uint
	name   string
	fatal  bool
	reason string
}

// requiredCaps is the canonical list. Order is the display order in
// startup logs. Hard-required caps come first so an operator reading the
// failure message sees them before the warnings.
var requiredCaps = []requiredCap{
	{
		bit:    unix.CAP_NET_ADMIN,
		name:   "CAP_NET_ADMIN",
		fatal:  true,
		reason: "netlink inet_diag queries; xtcp2 cannot read any TCP socket data without it",
	},
	{
		bit:    unix.CAP_SYS_ADMIN,
		name:   "CAP_SYS_ADMIN",
		fatal:  true,
		reason: "setns(CLONE_NEWNET) into per-namespace netlink sockets; without it, every setns into a new ns AND every restore back to the original fails with EPERM, the openAndSetNSWithRetries retry loop spins through all 10 attempts holding a locked OS thread, and a heavy ns-churn workload exhausts the SetMaxThreads ceiling within a few hours",
	},
	{
		bit:    unix.CAP_NET_RAW,
		name:   "CAP_NET_RAW",
		fatal:  false,
		reason: "raw-socket destinations (UDP IP_HDRINCL) need this — the daemon starts and runs OK without it, but a `-dest udp:…` flow will fail at first packet",
	},
	{
		bit:    unix.CAP_SYS_RESOURCE,
		name:   "CAP_SYS_RESOURCE",
		fatal:  false,
		reason: "io_uring's per-ring locked memory budget is bounded by RLIMIT_MEMLOCK; this capability lets the daemon raise that cap. Without it the io_uring netlink reader (-ioUring) may fail to allocate large SQE/CQE rings",
	},
}

// capabilityCheckResult is the structured outcome of one capability
// scan. Both the missing list (sorted by fatality, then by name) and the
// rendered error message are returned so unit tests can inspect each
// without parsing the error string.
type capabilityCheckResult struct {
	missingFatal   []requiredCap
	missingWarning []requiredCap
}

// hasCap returns true if `bit` is set in `mask`. Pulled out so the
// bit-test pattern is in one place and easy to read at the call site.
func hasCap(mask uint32, bit uint) bool {
	return mask&(1<<bit) != 0
}

// scanCapabilities reads the process's effective capability set via
// the (test-swappable) capgetFunc seam and returns the structured
// scan result. Does not log; the caller decides on logging vs fatal
// exit based on the daemon's configuration.
func scanCapabilities() (capabilityCheckResult, uint32, error) {
	var hdr unix.CapUserHeader
	hdr.Version = unix.LINUX_CAPABILITY_VERSION_3
	hdr.Pid = int32(os.Getpid())

	var caps unix.CapUserData
	if err := capgetFunc(&hdr, &caps); err != nil {
		return capabilityCheckResult{}, 0, fmt.Errorf("Capget: %w", err)
	}

	var res capabilityCheckResult
	for _, r := range requiredCaps {
		if hasCap(caps.Effective, r.bit) {
			continue
		}
		if r.fatal {
			res.missingFatal = append(res.missingFatal, r)
		} else {
			res.missingWarning = append(res.missingWarning, r)
		}
	}
	return res, caps.Effective, nil
}

// renderCapabilityError produces the human-readable error returned to
// the caller when one or more *fatal* capabilities are missing.
// Includes a ready-to-paste systemd snippet so the operator can
// fix the config in one copy/paste.
func renderCapabilityError(res capabilityCheckResult) error {
	if len(res.missingFatal) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteString("xtcp2 cannot start — required capabilities missing:\n")
	for _, m := range res.missingFatal {
		fmt.Fprintf(&b, "  - %s: %s\n", m.name, m.reason)
	}
	b.WriteString("\nGrant via systemd:\n")
	b.WriteString("  [Service]\n")
	b.WriteString("  AmbientCapabilities = ")
	names := allCapNames()
	b.WriteString(strings.Join(names, " "))
	b.WriteString("\n  CapabilityBoundingSet = ")
	b.WriteString(strings.Join(names, " "))
	b.WriteString("\n\nOr (less restricted): run as root.")
	return fmt.Errorf("%s", b.String())
}

// allCapNames returns the names of every required capability — both
// fatal and warning — so the systemd snippet in renderCapabilityError
// produces a complete config the operator can paste without editing.
func allCapNames() []string {
	names := make([]string, 0, len(requiredCaps))
	for _, r := range requiredCaps {
		names = append(names, r.name)
	}
	return names
}

// checkCapabilities performs the startup capability scan. Logs the
// effective bitmap, prints warnings for missing soft-required caps,
// and returns a detailed error if any hard-required cap is absent.
//
// https://www.man7.org/linux/man-pages/man7/capabilities.7.html
// https://pkg.go.dev/golang.org/x/sys/unix#pkg-constants
func (x *XTCP) checkCapabilities() error {
	res, effective, err := scanCapabilities()
	if err != nil {
		return err
	}

	if x.debugLevel > 10 {
		log.Printf("Effective Capabilities: 0x%X", effective)
		for _, r := range requiredCaps {
			present := hasCap(effective, r.bit)
			log.Printf("  %s: %v", r.name, present)
		}
	}

	for _, m := range res.missingWarning {
		log.Printf("WARN: missing capability %s — %s", m.name, m.reason)
	}

	return renderCapabilityError(res)
}
