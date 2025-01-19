package xtcp

import (
	"fmt"
	"log"
	"os"

	"golang.org/x/sys/unix"
)

// checkCapabilities checks for CAP_NET_ADMIN and CAP_SYS_CHROOT
// https://www.man7.org/linux/man-pages/man7/capabilities.7.html
// https://pkg.go.dev/golang.org/x/sys/unix#pkg-constants
func (x *XTCP) checkCapabilities() error {

	var hdr unix.CapUserHeader
	hdr.Version = unix.LINUX_CAPABILITY_VERSION_3
	hdr.Pid = int32(os.Getpid())

	var caps unix.CapUserData
	// https://pkg.go.dev/golang.org/x/sys/unix#Capget
	if err := unix.Capget(&hdr, &caps); err != nil {
		return fmt.Errorf("failed to get capabilities: %w", err)
	}

	if x.debugLevel > 10 {
		log.Printf("Permitted Capabilities: 0x%X", caps.Permitted)
		log.Printf("Effective Capabilities: 0x%X", caps.Effective)
		log.Printf("Inheritable Capabilities: 0x%X", caps.Inheritable)
	}

	hasChroot := (caps.Effective & (1 << unix.CAP_SYS_CHROOT)) != 0
	hasNetAdmin := (caps.Effective & (1 << unix.CAP_NET_ADMIN)) != 0

	if x.debugLevel > 10 {
		log.Printf("CAP_SYS_CHROOT: %v\n", hasChroot)
		log.Printf("CAP_NET_ADMIN: %v\n", hasNetAdmin)
	}

	if hasChroot && hasNetAdmin {
		if x.debugLevel > 10 {
			log.Println("The program has both CAP_NET_ADMIN and CAP_SYS_CHROOT.")
		}
		return nil
	}

	return fmt.Errorf("xtcp needs CAP_NET_ADMIN and CAP_SYS_CHROOT")
}
