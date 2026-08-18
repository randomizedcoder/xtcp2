//go:build linux

package nicinfo

import "golang.org/x/sys/unix"

// driverInfo queries the ethtool driver information for ifname via the
// SIOCETHTOOL ioctl with ETHTOOL_GDRVINFO (mirrors `ethtool -i`), without
// forking. It opens an AF_INET/SOCK_DGRAM socket purely to carry the ioctl.
//
// This is production-only: Collect calls it only when reading the live "/sys",
// and ignores any error (falling back to the sysfs-derived values). It is kept
// in its own file so unit tests never need to invoke it.
func driverInfo(ifname string) (driver, version, fwVersion, busInfo string, err error) {
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM, 0)
	if err != nil {
		return "", "", "", "", err
	}
	defer unix.Close(fd) //nolint:errcheck // best-effort ioctl socket; nothing to recover on close

	di, err := unix.IoctlGetEthtoolDrvinfo(fd, ifname)
	if err != nil {
		return "", "", "", "", err
	}

	return cstr(di.Driver[:]), cstr(di.Version[:]), cstr(di.Fw_version[:]), cstr(di.Bus_info[:]), nil
}

// cstr converts a NUL-padded C char buffer to a Go string.
func cstr(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}
