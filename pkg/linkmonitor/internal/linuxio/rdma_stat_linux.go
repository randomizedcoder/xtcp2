package linuxio

import (
	"context"
	"errors"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// RDMAStat is one port's default hardware counters, the values sysfs shows
// in ports/<n>/hw_counters: stats->value plus any bound counters
// (stat_get_doit_default_counter, nldev.c).
type RDMAStat struct {
	Name        string
	Index, Port uint32
	Counters    []RDMACounter
}

// RDMACounter is one hardware counter by its sysfs file name.
type RDMACounter struct {
	Name  string
	Value uint64
}

var rdmaStatGet = xtcpnl.RdmaNlGetType(xtcpnl.RdmaNlNldev, xtcpnl.RdmaNldevCmdStatGet)

// rdmaStatAttrs are the top-level attributes a stat reply is admitted on.
var rdmaStatAttrs = []uint16{xtcpnl.RdmaNldevAttrDevIndex, xtcpnl.RdmaNldevAttrDevName,
	xtcpnl.RdmaNldevAttrPortIndex, xtcpnl.RdmaNldevAttrStatHwcounters}

// GetRDMAStat reads a port's hardware counters in one doit. Unlike reading
// the sysfs files one by one, it refreshes the driver's counters once: mlx5
// refreshes on nearly every sysfs read in name order. It needs no privilege.
func (c *Client) GetRDMAStat(ctx context.Context, index, port uint32) (RDMAStat, error) {
	if c.protocol != unix.NETLINK_RDMA {
		return RDMAStat{}, ErrRequest
	}
	if port == 0 {
		return RDMAStat{}, ErrRequest
	}
	result, err := execute(ctx, c, request[RDMAStat]{family: rdmaStatGet,
		build: func(sequence uint32, _ uint64) ([]byte, error) {
			return rdmaRequest(rdmaStatGet, index, port, sequence, false)
		}, match: func(e xtcpnl.NetlinkEnvelope) (RDMAStat, bool, error) {
			s, err := decodeRDMAStat(e.Body)
			if err == nil && (s.Index != index || s.Port != port) {
				err = ErrReply
			}
			return s, err == nil, err
		}})
	if err != nil {
		return RDMAStat{}, err
	}
	return result.Values[0], nil
}

// decodeRDMAStat requires the handle and admits each counter only with a
// name and a value. Duplicate names are the caller's to reject: it already
// indexes them by name.
func decodeRDMAStat(body []byte) (RDMAStat, error) {
	var seen [xtcpnl.RdmaNldevAttrStatHwcounters + 1]bool
	if err := admitRDMANested(body, rdmaStatAttrs, xtcpnl.RdmaNldevAttrStatHwcounters, seen[:]); err != nil {
		return RDMAStat{}, err
	}
	s, err := xtcpnl.ParseRdmaStat(body)
	if err != nil {
		return RDMAStat{}, errors.Join(ErrReply, err)
	}
	h := s.RdmaHandle
	if !h.HasDevIndex || !h.HasDevName || !h.HasPortIndex {
		return RDMAStat{}, ErrReply
	}
	if err := admitRDMAIdentity(h, xtcpnl.RdmaNetdev{}); err != nil {
		return RDMAStat{}, err
	}
	// The u16 length of the HWCOUNTERS nest bounds the counters to a few
	// thousand; the kernel sends one NLMSG_DEFAULT_SIZE reply.
	out := RDMAStat{Name: h.DevName, Index: h.DevIndex, Port: h.PortIndex, Counters: make([]RDMACounter, len(s.HwCounters))}
	for i, counter := range s.HwCounters {
		// A counter name is a sysfs file name under hw_counters.
		if !counter.HasValue || !rdmaName(counter.Name, rdmaNameMax) {
			return RDMAStat{}, ErrReply
		}
		out.Counters[i] = RDMACounter{Name: counter.Name, Value: counter.Value}
	}
	return out, nil
}
