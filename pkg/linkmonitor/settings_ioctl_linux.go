package linkmonitor

import (
	"context"
	"encoding/binary"
	"errors"
	"runtime"
	"unsafe"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/linuxio"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

const linkSettingsHeader = 48
const maxLinkModeWords = 127 // Positive s8 count in struct ethtool_link_settings.

type ethtoolIoctl struct {
	fd       int
	invoke   func(string, []byte) error
	validate func(context.Context, string, uint32) error
}

func newEthtoolIoctl() (*ethtoolIoctl, error) {
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	c := &ethtoolIoctl{fd: fd}
	c.invoke = c.call
	return c, nil
}

func (c *ethtoolIoctl) Close() error { return unix.Close(c.fd) }

// Keep the data pointer visible to the GC, matching x/sys's private ifreqData.
// ifreq's union occupies 16 bytes on 32-bit and 24 bytes on 64-bit Linux.
type ethtoolIfreq struct {
	name    [unix.IFNAMSIZ]byte
	data    unsafe.Pointer
	padding [8 + unsafe.Sizeof(uintptr(0))]byte
}

func (c *ethtoolIoctl) call(name string, data []byte) error {
	if !validName(name, unix.IFNAMSIZ-1) || len(data) < 4 {
		return linuxio.ErrRequest
	}
	request := ethtoolIfreq{data: unsafe.Pointer(&data[0])}
	copy(request.name[:], name)
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(c.fd), unix.SIOCETHTOOL, uintptr(unsafe.Pointer(&request)))
	runtime.KeepAlive(data)
	if errno != 0 {
		return errno
	}
	return nil
}

func (c *ethtoolIoctl) read(ctx context.Context, name string, index uint32, kind xtcpnl.EthtoolKind) (xtcpnl.EthtoolMessage, error) {
	if err := c.check(ctx, name, index); err != nil {
		return xtcpnl.EthtoolMessage{}, err
	}
	m, err := c.readKind(ctx, name, kind)
	if after := c.check(ctx, name, index); after != nil {
		return xtcpnl.EthtoolMessage{}, after
	}
	return m, err
}

func (c *ethtoolIoctl) check(ctx context.Context, name string, index uint32) error {
	if c.validate != nil {
		return c.validate(ctx, name, index)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validName(name, unix.IFNAMSIZ-1) || index == 0 {
		return linuxio.ErrRequest
	}
	ifr, err := unix.NewIfreq(name)
	if err != nil {
		return err
	}
	if err := unix.IoctlIfreq(c.fd, unix.SIOCGIFINDEX, ifr); err != nil {
		return err
	}
	if ifr.Uint32() != index {
		return unix.ENODEV
	}
	return nil
}

func (c *ethtoolIoctl) readKind(ctx context.Context, name string, kind xtcpnl.EthtoolKind) (xtcpnl.EthtoolMessage, error) {
	m := xtcpnl.EthtoolMessage{Kind: kind}
	if err := ctx.Err(); err != nil {
		return m, err
	}
	if kind == xtcpnl.EthtoolLinkModesKind {
		var err error
		m.LinkModes, err = c.modes(ctx, name)
		return m, err
	}
	var cmd uint32
	switch kind {
	case xtcpnl.EthtoolChannelsKind:
		cmd = unix.ETHTOOL_GCHANNELS
	case xtcpnl.EthtoolRingsKind:
		cmd = unix.ETHTOOL_GRINGPARAM
	default:
		return m, unix.EOPNOTSUPP
	}
	data := make([]byte, 36)
	binary.NativeEndian.PutUint32(data, cmd)
	if err := c.invoke(name, data); err != nil {
		return m, err
	}
	var values [8]uint32
	for i := range values {
		values[i] = binary.NativeEndian.Uint32(data[4+i*4:])
	}
	if kind == xtcpnl.EthtoolChannelsKind {
		m.Channels = &xtcpnl.EthtoolChannels{RXMax: &values[0], TXMax: &values[1], OtherMax: &values[2], CombinedMax: &values[3], RX: &values[4], TX: &values[5], Other: &values[6], Combined: &values[7]}
	} else {
		m.Rings = &xtcpnl.EthtoolRings{RXMax: &values[0], RXMiniMax: &values[1], RXJumboMax: &values[2], TXMax: &values[3], RX: &values[4], RXMini: &values[5], RXJumbo: &values[6], TX: &values[7]}
	}
	return m, nil
}

func (c *ethtoolIoctl) modes(ctx context.Context, name string) (*xtcpnl.EthtoolLinkModes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data := make([]byte, linkSettingsHeader)
	binary.NativeEndian.PutUint32(data, unix.ETHTOOL_GLINKSETTINGS)
	if err := errors.Join(c.invoke(name, data), ctx.Err()); err != nil {
		return nil, err
	}
	n := -int(int8(data[15]))
	if n < 1 || n > maxLinkModeWords || binary.NativeEndian.Uint32(data) != unix.ETHTOOL_GLINKSETTINGS {
		return nil, linuxio.ErrReply
	}
	data = make([]byte, linkSettingsHeader+12*n)
	binary.NativeEndian.PutUint32(data, unix.ETHTOOL_GLINKSETTINGS)
	data[15] = byte(n)
	if err := errors.Join(c.invoke(name, data), ctx.Err()); err != nil {
		return nil, err
	}
	if int(data[15]) != n || binary.NativeEndian.Uint32(data) != unix.ETHTOOL_GLINKSETTINGS {
		return nil, linuxio.ErrReply
	}
	size, speed := uint32(n*32), binary.NativeEndian.Uint32(data[4:])
	ours := &xtcpnl.EthtoolBitset{Size: &size, Compact: true, Mask: make([]uint32, n), Value: make([]uint32, n)}
	for i := range n {
		ours.Mask[i] = binary.NativeEndian.Uint32(data[linkSettingsHeader+4*i:])
		ours.Value[i] = binary.NativeEndian.Uint32(data[linkSettingsHeader+4*(n+i):])
	}
	return &xtcpnl.EthtoolLinkModes{Speed: &speed, Duplex: &data[8], Autoneg: &data[11], Ours: ours}, nil
}

func unsupportedEthtool(err error) bool {
	return errors.Is(err, unix.EOPNOTSUPP) || errors.Is(err, unix.ENOSYS)
}
