package sockopt

import (
	"errors"
	"flag"
	"fmt"
	"math"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// caNameMax is TCP_CA_NAME_MAX (16) less the NUL.
const caNameMax = 15

// Options are the socket options to set; the zero value sets none.
type Options struct {
	Cong        string
	TOS         int
	Priority    int
	RcvBuf      Size
	Freebind    bool
	Transparent bool
	BindNoPort  bool
}

// Register adds the options' flags to fs.
func (o *Options) Register(fs *flag.FlagSet) {
	fs.StringVar(&o.Cong, "cong", "", "TCP_CONGESTION algorithm; empty = the kernel default")
	fs.IntVar(&o.TOS, "tos", 0, "IP_TOS, and IPV6_TCLASS on tcp6 sockets, 0-255 (0x.. accepted); 0 = not set")
	fs.IntVar(&o.Priority, "priority", 0, "SO_PRIORITY (0-6 unprivileged); 0 = not set")
	fs.Var(&o.RcvBuf, "rcvbuf", "SO_RCVBUF before connect/listen, e.g. 65536 or 1MiB (sets the window scale); 0 = not set")
	fs.BoolVar(&o.Freebind, "freebind", false, "IP_FREEBIND")
	fs.BoolVar(&o.Transparent, "transparent", false, "IP_TRANSPARENT (needs CAP_NET_ADMIN or CAP_NET_RAW)")
	fs.BoolVar(&o.BindNoPort, "bindNoPort", false, "IP_BIND_ADDRESS_NO_PORT")
}

// Validate reports every option out of range.
func (o Options) Validate() error {
	var errs []error
	if len(o.Cong) > caNameMax {
		errs = append(errs, fmt.Errorf("cong name longer than %d", caNameMax))
	}
	if o.TOS < 0 || o.TOS > math.MaxUint8 {
		errs = append(errs, fmt.Errorf("tos %d out of range 0-255", o.TOS))
	}
	if o.Priority < 0 {
		errs = append(errs, fmt.Errorf("priority %d is negative", o.Priority))
	}
	if o.RcvBuf > math.MaxInt32 {
		errs = append(errs, fmt.Errorf("rcvbuf %d exceeds %d", o.RcvBuf, math.MaxInt32))
	}
	return errors.Join(errs...)
}

// Control sets the options on a socket before connect or listen; it has
// the net.Dialer and net.ListenConfig Control signature. Accepted sockets
// inherit a listener's options.
func (o Options) Control(network, _ string, c syscall.RawConn) error {
	var err error
	if cerr := c.Control(func(fd uintptr) { err = o.apply(int(fd), network) }); cerr != nil {
		return cerr
	}
	return err
}

// ApplyConn sets the options on an accepted connection. A listener's
// options don't all reach its accepted sockets: sk_clone zeroes
// sk_priority (net/core/sock.c), and with net.ipv4.tcp_reflect_tos the
// child takes the SYN's TOS instead (net/ipv4/tcp_ipv4.c).
func (o Options) ApplyConn(c syscall.Conn) error {
	raw, err := c.SyscallConn()
	if err != nil {
		return err
	}
	var aerr error
	if cerr := raw.Control(func(fd uintptr) {
		network := "tcp4"
		if d, derr := unix.GetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_DOMAIN); derr == nil && d == unix.AF_INET6 {
			network = "tcp6"
		}
		aerr = o.apply(int(fd), network)
	}); cerr != nil {
		return cerr
	}
	return aerr
}

// intOpt is one integer socket option.
type intOpt struct {
	name       string
	level, opt int
	val        int
}

// apply sets the options on fd. SOL_IP options reach AF_INET6 TCP sockets
// too (ipv6_setsockopt hands them to ip_setsockopt), so only the traffic
// class differs by family.
func (o Options) apply(fd int, network string) error {
	if o.Cong != "" {
		if err := unix.SetsockoptString(fd, unix.IPPROTO_TCP, unix.TCP_CONGESTION, o.Cong); err != nil {
			return fmt.Errorf("TCP_CONGESTION %s: %w", o.Cong, err)
		}
	}
	var opts []intOpt
	if o.TOS != 0 {
		opts = append(opts, intOpt{"IP_TOS", unix.IPPROTO_IP, unix.IP_TOS, o.TOS})
		if network == "tcp6" {
			opts = append(opts, intOpt{"IPV6_TCLASS", unix.IPPROTO_IPV6, unix.IPV6_TCLASS, o.TOS})
		}
	}
	if o.Priority != 0 {
		opts = append(opts, intOpt{"SO_PRIORITY", unix.SOL_SOCKET, unix.SO_PRIORITY, o.Priority})
	}
	if o.RcvBuf != 0 {
		opts = append(opts, intOpt{"SO_RCVBUF", unix.SOL_SOCKET, unix.SO_RCVBUF, int(o.RcvBuf)})
	}
	for _, b := range []struct {
		on  bool
		opt intOpt
	}{
		{o.Freebind, intOpt{"IP_FREEBIND", unix.IPPROTO_IP, unix.IP_FREEBIND, 1}},
		{o.Transparent, intOpt{"IP_TRANSPARENT", unix.IPPROTO_IP, unix.IP_TRANSPARENT, 1}},
		{o.BindNoPort, intOpt{"IP_BIND_ADDRESS_NO_PORT", unix.IPPROTO_IP, unix.IP_BIND_ADDRESS_NO_PORT, 1}},
	} {
		if b.on {
			opts = append(opts, b.opt)
		}
	}
	for _, s := range opts {
		if err := unix.SetsockoptInt(fd, s.level, s.opt, s.val); err != nil {
			return fmt.Errorf("%s %d: %w", s.name, s.val, err)
		}
	}
	return nil
}

// Size is a byte count flag that takes a K, KiB, M, MiB, G or GiB suffix
// (all binary).
type Size int64

// String implements flag.Value.
func (s *Size) String() string { return strconv.FormatInt(int64(*s), 10) }

// Set implements flag.Value.
func (s *Size) Set(v string) error {
	n, err := ParseSize(v)
	if err != nil {
		return err
	}
	*s = n
	return nil
}

// suffixes are the multipliers ParseSize accepts, longest first.
var suffixes = []struct {
	suffix string
	shift  uint
}{
	{"KiB", 10}, {"MiB", 20}, {"GiB", 30},
	{"K", 10}, {"M", 20}, {"G", 30},
}

// ParseSize reads a non-negative byte count with an optional binary suffix.
func ParseSize(v string) (Size, error) {
	num, shift := v, uint(0)
	for _, s := range suffixes {
		if n, ok := strings.CutSuffix(v, s.suffix); ok {
			num, shift = n, s.shift
			break
		}
	}
	n, err := strconv.ParseInt(num, 10, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("size %q: want a non-negative count with an optional K, KiB, M, MiB, G or GiB suffix", v)
	}
	if n > math.MaxInt64>>shift {
		return 0, fmt.Errorf("size %q overflows", v)
	}
	return Size(n << shift), nil
}

// Chain runs each non-nil Control in turn, stopping at the first error.
func Chain(controls ...func(network, address string, c syscall.RawConn) error) func(network, address string, c syscall.RawConn) error {
	return func(network, address string, c syscall.RawConn) error {
		for _, f := range controls {
			if f == nil {
				continue
			}
			if err := f(network, address, c); err != nil {
				return err
			}
		}
		return nil
	}
}
