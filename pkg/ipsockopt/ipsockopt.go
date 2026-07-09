// Package ipsockopt clamps the outgoing IPv4 TTL / IPv6 unicast hop limit on a
// listening socket, so a listener's replies can't travel far if the host is
// unexpectedly internet-exposed. It is the per-listener analogue of a host-level
// nftables TTL clamp, and mirrors prometheus/exporter-toolkit#396.
//
// The option is set on the listening socket via net.ListenConfig.Control (before
// bind); the kernel then inherits it onto every accepted connection.
package ipsockopt

import (
	"errors"
	"syscall"

	"golang.org/x/sys/unix"
)

// Control returns a net.ListenConfig.Control callback that sets IP_TTL (IPv4)
// and/or IPV6_UNICAST_HOPS (IPv6) on the listening socket. A zero value leaves
// the kernel default for that family; if both are zero it returns nil so the
// caller keeps the default (no Control hook at all).
//
// setsockopt is attempted for whichever values are non-zero; ENOPROTOOPT (the
// option not applying to the socket's address family — e.g. IP_TTL on an
// IPv6-only socket) is ignored, matching exporter-toolkit#396.
func Control(ipv4TTL, ipv6HopLimit uint32) func(network, address string, c syscall.RawConn) error {
	if ipv4TTL == 0 && ipv6HopLimit == 0 {
		return nil
	}
	return func(_, _ string, c syscall.RawConn) error {
		var setErr error
		ctrlErr := c.Control(func(fd uintptr) {
			if ipv4TTL > 0 {
				if err := unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_TTL, int(ipv4TTL)); err != nil && !errors.Is(err, unix.ENOPROTOOPT) {
					setErr = err
					return
				}
			}
			if ipv6HopLimit > 0 {
				if err := unix.SetsockoptInt(int(fd), unix.IPPROTO_IPV6, unix.IPV6_UNICAST_HOPS, int(ipv6HopLimit)); err != nil && !errors.Is(err, unix.ENOPROTOOPT) {
					setErr = err
					return
				}
			}
		})
		if ctrlErr != nil {
			return ctrlErr
		}
		return setErr
	}
}
