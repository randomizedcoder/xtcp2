// Package tnlsys answers the questions `ip tunnel` and `ip tuntap` ask the
// kernel outside netlink, live or from a recorded snapshot.
//
// iproute2 7.2.0's `ip tunnel show` dumps the links over rtnetlink, then
// asks each tunnel for its parameters with a SIOCGETTUNNEL ioctl
// (ip/tunnel.c tnl_get_ioctl), an isatap tunnel for its routers with
// SIOCGETPRL and a sit tunnel for its 6rd prefix with SIOCGET6RD.
// `ip tuntap show` reads /sys/class/net/DEV/{tun_flags,owner,group}
// (lib/utils.c read_prop) and, under -d, walks /proc for the processes
// holding /dev/net/tun (ip/iptuntap.c show_processes). nlmon sees none of
// that, and strace does not dump the struct behind ifr_data, so a netlink
// capture alone cannot replay these objects.
//
// Sys is the seam: Live asks the kernel, Snapshot answers from a JSON
// sidecar, and Trace (tnlsnap) records one by running the real `ip` under
// ptrace, so a snapshot holds exactly the ioctls `ip` made, each keyed by
// the bytes it passed in. That matters because the kernel's answer for a
// fallback device (gre0, ip6tnl0, ...) depends on those bytes: it looks up
// the tunnel the passed parameters describe (net/ipv4/ip_tunnel.c
// ip_tunnel_ctl, net/ipv6/ip6_tunnel.c ip6_tnl_siocdevprivate).
//
// The package has no goip imports: goip and tnlsnap both use it.
package tnlsys

import (
	"errors"
	"fmt"
	"strings"
)

// The ioctls `ip` reads tunnels with (include/uapi/linux/if_tunnel.h:
// SIOCDEVPRIVATE plus 0, 4 and 8).
const (
	SIOCGETTUNNEL uint32 = 0x89F0
	SIOCGETPRL    uint32 = 0x89F4
	SIOCGET6RD    uint32 = 0x89F8
)

// The C struct sizes those ioctls carry.
const (
	// IPTunnelParmSize is sizeof(struct ip_tunnel_parm): name[16], link,
	// i_flags, o_flags, i_key, o_key and a 20-byte struct iphdr.
	IPTunnelParmSize = 52
	// IP6TnlParm2Size is sizeof(struct ip6_tnl_parm2).
	IP6TnlParm2Size = 76
	// TunnelPRLSize is sizeof(struct ip_tunnel_prl).
	TunnelPRLSize = 16
	// Tunnel6RDSize is sizeof(struct ip_tunnel_6rd).
	Tunnel6RDSize = 24
)

// SpillWindow is how many bytes past an ioctl's argument Live and Trace
// watch. A kernel that answers with a larger struct than `ip` passed
// writes past the buffer: ip6gre copies a 76-byte ip6_tnl_parm2 whatever
// buffer it was given, so `ip tunnel show` naming an ip6gre device has it
// write 24 bytes past a 52-byte ip_tunnel_parm.
const SpillWindow = 64

// maxArg bounds an ioctl argument: a SIOCGETPRL buffer is 16 bytes per
// router, and `ip` asks for 16.
const maxArg = 64 << 10

// ifNameSize is IFNAMSIZ.
const ifNameSize = 16

// ErrNotRecorded is a snapshot asked a question its recording never saw.
// It is a replay error, not a kernel answer: a recorded failure comes back
// as its errno.
var ErrNotRecorded = errors.New("tnlsys: not in the snapshot")

// ErrRequest is an ioctl Live refuses to make: not one of the three gets,
// or an argument of no or absurd size.
var ErrRequest = errors.New("tnlsys: ioctl request refused")

// SocketError is socket(2) failing before the ioctl, which `ip` reports as
// "create socket failed: %s".
type SocketError struct{ Err error }

func (e *SocketError) Error() string { return "tnlsys: socket: " + e.Err.Error() }

func (e *SocketError) Unwrap() error { return e.Err }

// TunFD is one /dev/net/tun descriptor show_processes finds: the process
// and descriptor numbers, the descriptor's fdinfo text (whose "iff:" line
// names the device), and the process's comm file, NoComm when it could not
// be read (get_task_name failing, which `ip` prints as "<NULL>").
type TunFD struct {
	Pid    int    `json:"pid"`
	FD     int    `json:"fd"`
	FdInfo string `json:"fdinfo"`
	Comm   string `json:"comm,omitempty"`
	NoComm bool   `json:"nocomm,omitempty"`
}

// Sys is what `ip tunnel` and `ip tuntap` ask outside netlink.
type Sys interface {
	// Ioctl makes req on a SOCK_DGRAM socket of family for the device
	// name (cut as strlcpy into ifr_name cuts it), with arg as ifr_data.
	// arg is updated in place. spill is what the kernel wrote past arg,
	// nil for nothing. A kernel refusal is its unix.Errno.
	Ioctl(family int, req uint32, name string, arg []byte) (spill []byte, err error)
	// ReadProp is the content of /sys/class/net/DEV/PROP, or its errno.
	ReadProp(dev, prop string) (string, error)
	// TunFDs is every /dev/net/tun descriptor in /proc, in directory
	// order, the caller's own excluded.
	TunFDs() ([]TunFD, error)
}

// ReqName names a request for a snapshot.
func ReqName(req uint32) string {
	switch req {
	case SIOCGETTUNNEL:
		return "SIOCGETTUNNEL"
	case SIOCGETPRL:
		return "SIOCGETPRL"
	case SIOCGET6RD:
		return "SIOCGET6RD"
	}
	return fmt.Sprintf("%#x", req)
}

// reqByName is ReqName's inverse for the three known requests.
func reqByName(s string) (uint32, bool) {
	for _, r := range []uint32{SIOCGETTUNNEL, SIOCGETPRL, SIOCGET6RD} {
		if ReqName(r) == s {
			return r, true
		}
	}
	return 0, false
}

// knownReq is whether req is one of the three gets.
func knownReq(req uint32) bool {
	_, ok := reqByName(ReqName(req))
	return ok
}

// IfName is name as strlcpy(ifr.ifr_name, name, IFNAMSIZ) leaves it: cut
// at a NUL and at IFNAMSIZ-1 bytes.
func IfName(name string) string {
	if i := strings.IndexByte(name, 0); i >= 0 {
		name = name[:i]
	}
	if len(name) > ifNameSize-1 {
		name = name[:ifNameSize-1]
	}
	return name
}

// ArgSize is the argument size `ip` passes for req: the parm struct of
// family for SIOCGETTUNNEL, 16 routers for SIOCGETPRL, one ip_tunnel_6rd
// for SIOCGET6RD.
func ArgSize(family int, req uint32) int {
	switch req {
	case SIOCGETTUNNEL:
		if family == afInet6 {
			return IP6TnlParm2Size
		}
		return IPTunnelParmSize
	case SIOCGETPRL:
		return 16 * TunnelPRLSize
	case SIOCGET6RD:
		return Tunnel6RDSize
	}
	return 0
}

// afInet6 is AF_INET6, spelled here so the package builds off Linux.
const afInet6 = 10
