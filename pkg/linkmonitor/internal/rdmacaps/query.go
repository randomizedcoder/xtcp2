// Package rdmacaps implements bounded, read-only local InfiniBand SMP queries.
package rdmacaps

import (
	"context"
	"encoding/binary"
	"errors"
	"strings"
	"syscall"
)

// ErrUnavailable means this build has no UMAD transport.
var ErrUnavailable = errors.New("RDMA capability transport unavailable")

// ErrReply identifies a malformed or mismatched local response.
var ErrReply = errors.New("invalid local RDMA capability reply")

// Capabilities preserves supported, enabled and active masks independently.
// Speed uses the verbs SDR..XDR bit vocabulary; width remains the IB bitmask.
type Capabilities struct {
	Base, Extended, Extended2                 SpeedMasks
	Supported, Enabled, Active                uint32
	WidthSupported, WidthEnabled, WidthActive uint8
	Complete                                  bool
}

// SpeedMasks retains one PortInfo field group's original encodings, including
// enabled-field sentinels/reserved bits that are not supported speed evidence.
type SpeedMasks struct{ Supported, Enabled, Active uint8 }

type exchange func(context.Context, string, uint32, [256]byte) ([256]byte, error)

// Query reads only the selected local port. No remote destination is accepted.
func Query(ctx context.Context, device string, port uint32) (Capabilities, error) {
	return query(ctx, device, port, exchangeLocal)
}

func query(ctx context.Context, device string, port uint32, send exchange) (Capabilities, error) {
	if device == "" || len(device) > 63 || device == "." || device == ".." || strings.ContainsAny(device, "/:\x00") || port == 0 || port > 255 {
		return Capabilities{}, syscall.EINVAL
	}
	if err := ctx.Err(); err != nil {
		return Capabilities{}, err
	}
	req := request(port)
	reply, err := send(ctx, device, port, req)
	if err != nil {
		return Capabilities{}, err
	}
	if err := ctx.Err(); err != nil {
		return Capabilities{}, err
	}
	if err := validateReply(req, reply); err != nil {
		return Capabilities{}, err
	}
	return decode(reply[64:128]), nil
}

func request(port uint32) [256]byte {
	var b [256]byte
	b[0], b[1], b[2], b[3] = 1, 0x81, 1, 1 // directed-route SMP GET
	// A fresh agent/socket per exchange isolates this transaction. The kernel
	// owns the upper TID half; the lower half is echoed and checked here.
	binary.BigEndian.PutUint32(b[12:16], 1)
	binary.BigEndian.PutUint16(b[16:18], 0x15) // PortInfo (includes extended speeds)
	binary.BigEndian.PutUint32(b[20:24], port)
	binary.BigEndian.PutUint16(b[32:34], 0xffff)
	binary.BigEndian.PutUint16(b[34:36], 0xffff)
	// hop count and both path arrays remain zero: the packet cannot traverse a fabric.
	return b
}

func validateReply(req, b [256]byte) error {
	if b[0] != 1 || b[1] != 0x81 || b[2] != 1 || b[3] != 0x81 || b[7] != 0 || b[6] > 1 ||
		binary.BigEndian.Uint32(b[12:16]) != 1 || binary.BigEndian.Uint16(b[16:18]) != 0x15 ||
		binary.BigEndian.Uint32(b[20:24]) != binary.BigEndian.Uint32(req[20:24]) {
		return ErrReply
	}
	status := binary.BigEndian.Uint16(b[4:6])
	if status&0x8000 == 0 {
		return ErrReply
	}
	switch status & 0x7fff {
	case 0:
		return nil
	case 4, 8, 12:
		return syscall.EOPNOTSUPP
	default:
		return syscall.EIO // Includes busy and invalid/protected requests; never retry or redirect.
	}
}

func decode(b []byte) Capabilities {
	c := Capabilities{Supported: uint32(b[32] >> 4), Enabled: uint32(b[35] & 15), Active: uint32(b[35] >> 4),
		WidthSupported: b[30], WidthEnabled: b[29], WidthActive: b[31], Complete: true}
	c.Base = SpeedMasks{Supported: b[32] >> 4, Enabled: b[35] & 15, Active: b[35] >> 4}
	mask := binary.BigEndian.Uint32(b[20:24])
	if c.Active&8 != 0 {
		c.Active = 0
	}
	if mask&(1<<14) != 0 {
		c.Extended = SpeedMasks{Supported: b[62] & 15, Enabled: b[63] & 31, Active: b[62] >> 4}
		c.Supported |= uint32(b[62]&15) << 4
		c.Enabled |= uint32(b[63]&15) << 4
		if active := b[62] >> 4; active != 0 {
			c.Active = uint32(active) << 4
		}
	}
	if mask&(1<<15) != 0 && binary.BigEndian.Uint16(b[60:62])&(1<<11) != 0 {
		// XDR is bit 1 in the second extended field (aggregate extended bit 5).
		supported, enabled, active := (b[56]>>3)&3, b[56]&7, (b[56]>>5)&3
		c.Extended2 = SpeedMasks{Supported: supported, Enabled: enabled, Active: active}
		c.Supported |= uint32(supported&2) << 7
		c.Enabled |= uint32(enabled&2) << 7
		if active != 0 {
			c.Active = uint32(active&2) << 7
		}
		c.Complete = supported&1 == 0 && enabled&5 == 0 && active&1 == 0
	}
	// Base bit 3 and unknown width bits are not known capabilities. Never infer
	// maximum speed from enabled or active fields. FDR10 shares QDR's nominal
	// rate, but PortInfo cannot distinguish their names without vendor data.
	c.Complete = c.Complete && mask&(1<<27) == 0 && c.Supported&8 == 0 && c.Supported != 0 && c.WidthSupported != 0 && c.WidthSupported & ^uint8(31) == 0
	return c
}
