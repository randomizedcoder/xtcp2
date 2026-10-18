//go:build linux

package rdmacaps

import (
	"bytes"
	"context"
	"errors"
	"syscall"
	"time"
	"unsafe"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/linuxio"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// umadTimeoutCap bounds one exchange's send timeout and poll, as the cgo
// exchange does.
const umadTimeoutCap = 250 * time.Millisecond

// umadOps is the device I/O of one exchange. umadSys is the kernel's; tests
// substitute a socketpair so the same code runs through write, poll and read.
type umadOps interface {
	// loaded reports whether ib_umad is loaded. It is checked first because
	// GET_CHARDEV for umad would otherwise load the module, a side effect a
	// read-only query must not have; libibumad fails the same way.
	loaded() bool
	resolve(ctx context.Context, device string, port uint32) (xtcpnl.RdmaChardev, error)
	open(ch *xtcpnl.RdmaChardev) (int, error)
	ioctl(fd int, request uintptr, arg unsafe.Pointer) error
	write(fd int, p []byte) (int, error)
	poll(fd int, timeoutMs int) (bool, error)
	read(fd int, p []byte) (int, error)
	close(fd int) error
}

// umadExchange is everything the kernel reads or writes during one exchange,
// in one allocation: interface calls let their arguments escape.
type umadExchange struct {
	ch     xtcpnl.RdmaChardev
	packet umadPacket
	reg    [umadRegReq2Len]byte
	agent  uint32
}

// exchangeNative is the cgo-free exchange: GET_CHARDEV names the port's
// umad node, and each exchange owns its descriptor and agent, so nothing is
// shared between exchanges and no lane serializes them.
func exchangeNative(ctx context.Context, device string, port uint32, request [256]byte) ([256]byte, error) {
	return exchangeUMAD(ctx, umadSys{}, device, port, request)
}

// exchangeUMAD resolves and opens the node, registers a directed-route SMP
// agent, writes one request and reads one reply within the earlier of ctx's
// deadline and umadTimeoutCap, then unregisters and closes, joining those
// errors with the result. A good reply is returned with a cleanup error, as
// the cgo exchange returns it. The request's TID is sent as query built it:
// the kernel replaces the upper half with the agent's, so it is unique per
// exchange without a counter here.
func exchangeUMAD(ctx context.Context, ops umadOps, device string, port uint32, request [256]byte) (reply [256]byte, err error) {
	if err = ctx.Err(); err != nil {
		return reply, err
	}
	timeout, expired := umadTimeoutCap, error(syscall.ETIMEDOUT)
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < timeout {
		timeout, expired = time.Until(deadline), context.DeadlineExceeded
	}
	if timeout <= 0 {
		return reply, context.DeadlineExceeded
	}
	deadline := time.Now().Add(timeout)
	if !ops.loaded() {
		return reply, syscall.EOPNOTSUPP
	}
	x := new(umadExchange)
	if x.ch, err = ops.resolve(ctx, device, port); err != nil {
		return reply, err
	}
	if !umadNodeName(x.ch.NameBytes()) {
		return reply, ErrReply
	}
	if x.ch.ABI < umadABIVersion {
		return reply, syscall.EOPNOTSUPP
	}
	fd, err := ops.open(&x.ch)
	if errors.Is(err, linuxio.ErrIdentity) {
		return reply, errors.Join(ErrReply, err)
	}
	if err != nil {
		return reply, err
	}
	defer func() { err = joinCleanup(err, ops.close(fd)) }()
	if x.ch.ABI == umadABIVersion {
		if err = ops.ioctl(fd, umadEnablePKey, nil); err != nil {
			return reply, err
		}
	}
	encodeRegisterAgent2(&x.reg, 0x81, 1)
	if err = ops.ioctl(fd, umadRegisterAgent2, unsafe.Pointer(&x.reg)); err != nil {
		return reply, err
	}
	x.agent = registeredAgent(&x.reg)
	defer func() { err = joinCleanup(err, ops.ioctl(fd, umadUnregisterAgent, unsafe.Pointer(&x.agent))) }()
	encodeUMADRequest(&x.packet, x.agent, uint32(max(1, timeout.Milliseconds())), &request)
	n, err := ops.write(fd, x.packet[:])
	if err != nil {
		return reply, err
	}
	if n != len(x.packet) {
		return reply, syscall.EIO
	}
	if err = umadWait(ops, fd, deadline, expired); err != nil {
		return reply, err
	}
	if n, err = ops.read(fd, x.packet[:]); err != nil {
		return reply, err
	}
	if err = decodeUMADReply(&x.packet, n, x.agent, &reply); err != nil {
		return reply, err
	}
	return reply, ctx.Err()
}

// joinCleanup adds a cleanup error to err; with no cleanup error err is
// returned as it is, so a failed exchange allocates nothing for it.
func joinCleanup(err, cleanup error) error {
	if cleanup == nil {
		return err
	}
	return errors.Join(err, cleanup)
}

// umadWait polls fd until it is readable or deadline passes, then returns
// expired: DeadlineExceeded when the deadline is ctx's, ETIMEDOUT when it is
// the cap's, as umad_recv reports it.
func umadWait(ops umadOps, fd int, deadline time.Time, expired error) error {
	for {
		left := time.Until(deadline)
		if left <= 0 {
			return expired
		}
		ready, err := ops.poll(fd, int((left+time.Millisecond-1)/time.Millisecond))
		switch {
		case errors.Is(err, syscall.EINTR):
		case err != nil:
			return err
		case ready:
			return nil
		}
	}
}

// umadNodeName admits only umad<digits>, so a reply can never name another
// kind of node under /dev/infiniband.
func umadNodeName(b []byte) bool {
	digits, ok := bytes.CutPrefix(b, []byte("umad"))
	if !ok || len(digits) == 0 {
		return false
	}
	for _, c := range digits {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// umadSys is the kernel's umad device.
type umadSys struct{}

// umadABIPath exists while ib_umad is loaded; libibumad reads it for the ABI.
// It is NUL-terminated here so the check passes it without allocating.
var umadABIPath = []byte("/sys/class/infiniband_mad/abi_version\x00")

func (umadSys) loaded() bool {
	cwd := unix.AT_FDCWD
	_, _, errno := unix.Syscall(unix.SYS_FACCESSAT, uintptr(cwd), uintptr(unsafe.Pointer(&umadABIPath[0])), unix.F_OK)
	return errno == 0
}

// resolve finds device's NLDEV index, then asks GET_CHARDEV for port's umad
// node, on a NETLINK_RDMA socket of its own.
func (umadSys) resolve(ctx context.Context, device string, port uint32) (ch xtcpnl.RdmaChardev, err error) {
	c, err := linuxio.NewClient(unix.NETLINK_RDMA)
	if err != nil {
		return ch, err
	}
	defer func() { err = errors.Join(err, c.Close()) }()
	devices, err := c.DumpRDMADevices(ctx)
	if err != nil {
		return ch, err
	}
	for _, d := range devices.Values {
		if d.Name == device {
			return c.Chardev(ctx, d.Index, port, xtcpnl.ChardevUmad)
		}
	}
	return ch, syscall.ENODEV
}

func (umadSys) open(ch *xtcpnl.RdmaChardev) (int, error) {
	return linuxio.OpenChardev(ch, unix.O_RDWR|unix.O_NONBLOCK)
}

func (umadSys) ioctl(fd int, request uintptr, arg unsafe.Pointer) error {
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), request, uintptr(arg)); errno != 0 {
		return errno
	}
	return nil
}

func (umadSys) write(fd int, p []byte) (int, error) { return unix.Write(fd, p) }

func (umadSys) poll(fd int, timeoutMs int) (bool, error) {
	fds := [1]unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	n, err := unix.Poll(fds[:], timeoutMs)
	return n > 0, err
}

func (umadSys) read(fd int, p []byte) (int, error) { return unix.Read(fd, p) }

func (umadSys) close(fd int) error { return unix.Close(fd) }
