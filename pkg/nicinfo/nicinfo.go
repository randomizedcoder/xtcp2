//go:build linux

// Package nicinfo collects Ethernet NIC identity (driver, model, PCI ids,
// firmware, link speed, operstate) from Linux sysfs and the ethtool ioctl
// WITHOUT forking any process. It is pure Go and builds with CGO_ENABLED=0.
//
// The philosophy is non-fatal and best-effort: an interface that cannot be
// read is skipped rather than causing an error, and no function ever calls
// log.Fatal/os.Exit. Callers pass a sysfs root ("/sys" in production, a fake
// tree in tests) so the sysfs paths are fully injectable. The ethtool ioctl
// (firmware + bus-info) is only attempted against the real "/sys" so unit
// tests run without hardware.
package nicinfo

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// NIC is the identity of one physical interface.
type NIC struct {
	Ifname    string
	Driver    string
	Model     string // resolved from pci.ids, may be empty
	BusInfo   string // PCI bus address, e.g. 0000:01:00.0
	FwVersion string
	PCIVendor uint32 // e.g. 0x15b3
	PCIDevice uint32
	SpeedMbps uint32 // 0 if unknown/down
	Operstate string // "up"/"down"/... best-effort
}

// Collect returns NIC info for the named interfaces (best-effort: an interface
// that can't be read is skipped, not fatal). sysfsRoot lets tests inject a fake
// tree; production passes "/sys". If an interface has no PCI device symlink it is
// skipped (virtual: lo/veth*/docker0/bridges/cni*).
func Collect(sysfsRoot string, ifnames []string) []NIC {
	// The ethtool ioctl talks to real hardware, so only attempt it against the
	// live "/sys". Tests inject a fake tree and rely purely on sysfs files.
	useIoctl := sysfsRoot == "/sys"

	var out []NIC
	for _, ifname := range ifnames {
		ifDir := filepath.Join(sysfsRoot, "class", "net", ifname)
		devDir := filepath.Join(ifDir, "device")

		// Skip virtual interfaces: no device entry means lo/veth*/docker0/etc.
		// The entry is a symlink on real sysfs and a directory in testdata; both
		// are visible to Lstat.
		if _, err := os.Lstat(devDir); err != nil {
			continue
		}

		nic := NIC{
			Ifname:    ifname,
			Driver:    readDriver(devDir),
			PCIVendor: readHex(filepath.Join(devDir, "vendor")),
			PCIDevice: readHex(filepath.Join(devDir, "device")),
			BusInfo:   readBusInfo(ifDir, devDir),
			SpeedMbps: readSpeed(filepath.Join(ifDir, "speed")),
			Operstate: readTrimmed(filepath.Join(ifDir, "operstate")),
		}
		nic.Model = modelFor(nic.PCIVendor, nic.PCIDevice)

		// Production only: firmware + bus-info come from ethtool (ethtool -i).
		// Any error is ignored and the sysfs-derived values stand.
		if useIoctl {
			if drv, _, fw, bus, err := driverInfo(ifname); err == nil {
				if drv != "" {
					nic.Driver = drv
				}
				nic.FwVersion = fw
				if bus != "" {
					nic.BusInfo = bus
				}
			}
		}

		out = append(out, nic)
	}
	return out
}

// readDriver resolves the driver name for a device directory. On real sysfs
// <device>/driver is a symlink to .../drivers/<name>; in testdata it is a plain
// file containing the driver name. Both are handled.
func readDriver(devDir string) string {
	p := filepath.Join(devDir, "driver")
	if target, err := os.Readlink(p); err == nil {
		return filepath.Base(target)
	}
	if b, err := os.ReadFile(p); err == nil {
		return strings.TrimSpace(string(b))
	}
	return ""
}

// readBusInfo returns the PCI bus address as a sysfs fallback (used when the
// ethtool ioctl is not consulted). On real sysfs <if>/device is a symlink whose
// basename is the bus address (e.g. 0000:01:00.0). In testdata device is a
// directory, so we fall back to the PCI_SLOT_NAME line in device/uevent, which
// real sysfs also exposes.
func readBusInfo(ifDir, devDir string) string {
	if target, err := os.Readlink(filepath.Join(ifDir, "device")); err == nil {
		return filepath.Base(target)
	}
	if b, err := os.ReadFile(filepath.Join(devDir, "uevent")); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if v, ok := strings.CutPrefix(line, "PCI_SLOT_NAME="); ok {
				return strings.TrimSpace(v)
			}
		}
	}
	return ""
}

// readHex reads a sysfs file like "0x15b3\n" and returns its value. Any error
// (missing file, bad format) yields 0.
func readHex(path string) uint32 {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	s := strings.TrimSpace(string(b))
	s = strings.TrimPrefix(s, "0x")
	s = strings.TrimPrefix(s, "0X")
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return 0
	}
	return uint32(v)
}

// readSpeed reads <if>/speed. The file is absent or contains "-1" when the link
// is down; both map to 0.
func readSpeed(path string) uint32 {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	v, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	if err != nil || v < 0 {
		return 0
	}
	return uint32(v)
}

// readTrimmed returns the trimmed contents of a sysfs file, or "" on error.
func readTrimmed(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
