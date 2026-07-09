package ipsockopt

import (
	"context"
	"net"
	"testing"

	"golang.org/x/sys/unix"
)

func TestControl_nilWhenUnset(t *testing.T) {
	if Control(0, 0) != nil {
		t.Fatal("Control(0,0) should be nil so the kernel default is kept")
	}
	if Control(3, 0) == nil || Control(0, 3) == nil {
		t.Fatal("Control with a non-zero value should return a callback")
	}
}

// TestControl_setsIPv4TTL binds a real IPv4 listener through the Control hook
// and reads the TTL back with getsockopt to prove it was applied.
func TestControl_setsIPv4TTL(t *testing.T) {
	const ttl = 7
	lc := net.ListenConfig{Control: Control(ttl, 0)}
	ln, err := lc.Listen(context.Background(), "tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	got := getsockoptInt(t, ln, unix.IPPROTO_IP, unix.IP_TTL)
	if got != ttl {
		t.Fatalf("IP_TTL = %d, want %d", got, ttl)
	}
}

// TestControl_setsIPv6HopLimit does the same for IPv6 unicast hops.
func TestControl_setsIPv6HopLimit(t *testing.T) {
	const hops = 5
	lc := net.ListenConfig{Control: Control(0, hops)}
	ln, err := lc.Listen(context.Background(), "tcp6", "[::1]:0")
	if err != nil {
		t.Skipf("IPv6 listen unavailable: %v", err)
	}
	defer ln.Close()

	got := getsockoptInt(t, ln, unix.IPPROTO_IPV6, unix.IPV6_UNICAST_HOPS)
	if got != hops {
		t.Fatalf("IPV6_UNICAST_HOPS = %d, want %d", got, hops)
	}
}

func getsockoptInt(t *testing.T, ln net.Listener, level, opt int) int {
	t.Helper()
	rc, err := ln.(*net.TCPListener).SyscallConn()
	if err != nil {
		t.Fatalf("SyscallConn: %v", err)
	}
	var val int
	var gErr error
	if cErr := rc.Control(func(fd uintptr) {
		val, gErr = unix.GetsockoptInt(int(fd), level, opt)
	}); cErr != nil {
		t.Fatalf("control: %v", cErr)
	}
	if gErr != nil {
		t.Fatalf("getsockopt: %v", gErr)
	}
	return val
}
