package linuxio

import (
	"bytes"
	"context"
	"encoding/binary"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// DevlinkPort retains the bounded association evidence used by inventory.
// Constants are Linux 7.0 UAPI devlink.h, not dynamically assigned family IDs.
type DevlinkPort struct {
	Bus, Device   string
	Index, Netdev uint32
	Flavor        uint16
	HasFlavor     bool
}

// DumpDevlinkPorts returns no partial evidence when a dump fails.
func (c *Client) DumpDevlinkPorts(ctx context.Context, f Family) (Result[DevlinkPort], error) {
	if c.protocol != unix.NETLINK_GENERIC || f.owner != c || f.name != "devlink" {
		return Result[DevlinkPort]{}, ErrFamily
	}
	return execute(ctx, c, request[DevlinkPort]{family: f.id, dump: true,
		build: func(seq uint32, epoch uint64) ([]byte, error) {
			if f.epoch != epoch {
				return nil, ErrFamily
			}
			data := make([]byte, 20)
			binary.LittleEndian.PutUint32(data, uint32(len(data)))
			binary.LittleEndian.PutUint16(data[4:], f.id)
			binary.LittleEndian.PutUint16(data[6:], unix.NLM_F_REQUEST|unix.NLM_F_DUMP)
			binary.LittleEndian.PutUint32(data[8:], seq)
			data[16], data[17] = 5, 1 // DEVLINK_CMD_PORT_GET, DEVLINK_GENL_VERSION
			return data, nil
		}, match: matchDevlinkPort})
}

func matchDevlinkPort(e xtcpnl.NetlinkEnvelope) (DevlinkPort, bool, error) {
	match, err := genericCommand(e.Body, 7) // DEVLINK_CMD_PORT_NEW
	if err != nil || !match {
		return DevlinkPort{}, false, err
	}
	m, err := xtcpnl.ParseGenericNetlink(e.Body)
	if err != nil {
		return DevlinkPort{}, false, err
	}
	if m.Version != 1 {
		return DevlinkPort{}, false, ErrReply
	}
	p, err := decodeDevlinkPort(m.Attributes)
	return p, err == nil, err
}

func decodeDevlinkPort(attrs []xtcpnl.NetlinkAttribute) (DevlinkPort, error) {
	var p DevlinkPort
	var seen [78]bool
	for _, a := range attrs {
		id := a.ID()
		if id != 1 && id != 2 && id != 3 && id != 6 && id != 77 {
			continue
		}
		if seen[id] || a.Type != id {
			return p, ErrReply
		}
		seen[id] = true
		switch id {
		case 1, 2:
			if len(a.Data) < 2 || len(a.Data) > 256 || a.Data[len(a.Data)-1] != 0 || bytes.IndexByte(a.Data[:len(a.Data)-1], 0) >= 0 {
				return p, ErrReply
			}
			value := string(a.Data[:len(a.Data)-1])
			if id == 1 {
				p.Bus = value
			} else {
				p.Device = value
			}
		case 3, 6:
			if len(a.Data) != 4 {
				return p, ErrReply
			}
			value := binary.LittleEndian.Uint32(a.Data)
			if id == 3 {
				p.Index = value
			} else {
				p.Netdev = value
			}
		case 77:
			if len(a.Data) != 2 {
				return p, ErrReply
			}
			p.Flavor, p.HasFlavor = binary.LittleEndian.Uint16(a.Data), true
		}
	}
	if !seen[1] || !seen[2] || !seen[3] || p.Netdev > 0x7fffffff {
		return p, ErrReply
	}
	return p, nil
}
