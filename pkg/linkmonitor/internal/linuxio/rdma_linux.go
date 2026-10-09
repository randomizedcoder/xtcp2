package linuxio

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// RDMAPort holds owned NLDEV evidence. Device GET uses Port as the advertised
// port count; PORT_GET uses it as the actual port number. Index zero is valid.
// Constants follow flake-pinned Linux 7.0 include/rdma/rdma_netlink.h.
// Header SHA256: 7e44de6e61bac156edad0a4947c4b267acacb0e9a82b48191cf7ade833218173.
type RDMAPort struct {
	NetdevName            string
	Netdev                uint32
	HasNetdev             bool
	Name                  string
	Index, Port           uint32
	State, Physical       uint8
	HasState, HasPhysical bool
}

const (
	rdmaDeviceGet uint16 = 5<<10 | 1
	rdmaPortGet   uint16 = 5<<10 | 5
)

// DumpRDMADevices enumerates local RDMA devices on a dedicated RDMA socket.
func (c *Client) DumpRDMADevices(ctx context.Context) (Result[RDMAPort], error) {
	return c.rdma(ctx, rdmaDeviceGet, 0, 0, true)
}

// DumpRDMAPorts enumerates a device's ports without trusting its port count.
func (c *Client) DumpRDMAPorts(ctx context.Context, index uint32) (Result[RDMAPort], error) {
	return c.rdma(ctx, rdmaPortGet, index, 0, true)
}

// GetRDMAPort queries a single local host port, never a resource or fabric dump.
func (c *Client) GetRDMAPort(ctx context.Context, index, port uint32) (Result[RDMAPort], error) {
	if port == 0 {
		return Result[RDMAPort]{}, ErrRequest
	}
	return c.rdma(ctx, rdmaPortGet, index, port, false)
}

func (c *Client) rdma(ctx context.Context, command uint16, index, port uint32, dump bool) (Result[RDMAPort], error) {
	if c.protocol != unix.NETLINK_RDMA {
		return Result[RDMAPort]{}, ErrRequest
	}
	return execute(ctx, c, request[RDMAPort]{family: command, dump: dump,
		build: func(sequence uint32, _ uint64) ([]byte, error) {
			return rdmaRequest(command, index, port, sequence, dump), nil
		}, match: func(e xtcpnl.NetlinkEnvelope) (RDMAPort, bool, error) {
			p, err := decodeRDMAPort(e.Body)
			if err == nil && command == rdmaPortGet && (p.Index != index || (!dump && p.Port != port)) {
				err = ErrReply
			}
			return p, err == nil, err
		}})
}

func rdmaRequest(command uint16, index, port, sequence uint32, dump bool) []byte {
	length := 16
	if command == rdmaPortGet {
		length += 8
	}
	if port != 0 {
		length += 8
	}
	b := make([]byte, length)
	binary.LittleEndian.PutUint32(b, uint32(length))
	binary.LittleEndian.PutUint16(b[4:], command)
	flags := uint16(unix.NLM_F_REQUEST)
	if dump {
		flags |= unix.NLM_F_DUMP
	}
	binary.LittleEndian.PutUint16(b[6:], flags)
	binary.LittleEndian.PutUint32(b[8:], sequence)
	if command == rdmaPortGet {
		putRDMAAttribute(b[16:], 1, index)
	}
	if port != 0 {
		putRDMAAttribute(b[24:], 3, port)
	}
	return b
}

func putRDMAAttribute(b []byte, kind uint16, value uint32) {
	binary.LittleEndian.PutUint16(b, 8)
	binary.LittleEndian.PutUint16(b[2:], kind)
	binary.LittleEndian.PutUint32(b[4:], value)
}

func decodeRDMAPort(body []byte) (RDMAPort, error) {
	var p RDMAPort
	attrs, err := xtcpnl.ParseNetlinkAttributes(body)
	if err != nil {
		return p, errors.Join(ErrReply, err)
	}
	var seen [52]bool
	for _, a := range attrs {
		id := a.ID()
		if id != 1 && id != 2 && id != 3 && id != 12 && id != 13 && id != 50 && id != 51 {
			continue
		}
		if seen[id] || a.Type != id {
			return p, ErrReply
		}
		seen[id] = true
		if err := rdmaAttribute(&p, id, a.Data); err != nil {
			return p, err
		}
	}
	if !seen[1] || !seen[2] || !seen[3] || seen[50] != seen[51] {
		return p, ErrReply
	}
	return p, nil
}

func rdmaAttribute(p *RDMAPort, id uint16, data []byte) error {
	switch id {
	case 1, 3, 50:
		if len(data) != 4 {
			return ErrReply
		}
		v := binary.LittleEndian.Uint32(data)
		switch id {
		case 50:
			if v == 0 || v > 0x7fffffff {
				return ErrReply
			}
			p.Netdev, p.HasNetdev = v, true
		case 1:
			p.Index = v
		default:
			p.Port = v
		}
	case 2, 51:
		if len(data) < 2 || len(data) > 64 || data[len(data)-1] != 0 || bytes.ContainsAny(data[:len(data)-1], "\x00/:") {
			return ErrReply
		}
		if id == 51 {
			if len(data) > 16 {
				return ErrReply
			}
			p.NetdevName = string(data[:len(data)-1])
		} else {
			p.Name = string(data[:len(data)-1])
		}
	case 12, 13:
		if len(data) != 1 {
			return ErrReply
		}
		if id == 12 {
			p.State, p.HasState = data[0], true
		} else {
			p.Physical, p.HasPhysical = data[0], true
		}
	}
	return nil
}
