package listener

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
)

// ErrNotLoopback is returned for a loopback listener address whose host is
// not a loopback IP literal, or whose port is not 1-65535.
var ErrNotLoopback = errors.New("not a loopback host:port")

// ParseLoopback checks that address is host:port with a loopback IP literal
// host (127.0.0.0/8 or ::1) and a numeric port in 1-65535. Hostnames,
// localhost included, are refused: the name service could answer with an
// address that is not loopback.
func ParseLoopback(address string) (netip.AddrPort, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return netip.AddrPort{}, fmt.Errorf("%w: %q: %w", ErrNotLoopback, address, err)
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || !ip.Unmap().IsLoopback() {
		return netip.AddrPort{}, fmt.Errorf("%w: %q: the host must be a loopback IP literal", ErrNotLoopback, address)
	}
	p, err := strconv.ParseUint(port, 10, 16)
	if err != nil || p == 0 {
		return netip.AddrPort{}, fmt.Errorf("%w: %q: the port must be 1-65535", ErrNotLoopback, address)
	}
	return netip.AddrPortFrom(ip, uint16(p)), nil
}

// LoopbackOnly wraps ln so Accept closes, rather than returns, a connection
// whose peer is not a loopback address, and calls rejected, if set, with that
// peer and the error from closing it. A loopback bind alone is not enough: on
// an interface with route_localnet set, as Docker sets on its bridges with the
// userland proxy off, a host on that link can reach 127.0.0.0/8 from its own
// address. One spoofing a loopback source never sees the replies, so cannot
// complete the TCP handshake.
func LoopbackOnly(ln net.Listener, rejected func(peer net.Addr, closeErr error)) net.Listener {
	return loopbackOnly{Listener: ln, rejected: rejected}
}

type loopbackOnly struct {
	net.Listener
	rejected func(peer net.Addr, closeErr error)
}

func (l loopbackOnly) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		peer := c.RemoteAddr()
		if PeerIsLoopback(peer) {
			return c, nil
		}
		closeErr := c.Close()
		if l.rejected != nil {
			l.rejected(peer, closeErr)
		}
	}
}

// PeerIsLoopback reports whether a is a TCP address in 127.0.0.0/8 or ::1,
// IPv4-mapped forms included.
func PeerIsLoopback(a net.Addr) bool {
	tcp, ok := a.(*net.TCPAddr)
	if !ok || tcp == nil {
		return false
	}
	ip, ok := netip.AddrFromSlice(tcp.IP)
	return ok && ip.Unmap().IsLoopback()
}
