package linuxio

import (
	"context"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// DumpLinks returns an authoritative candidate only after successful DONE.
// Physical-device eligibility and reconciliation are applied by the caller.
func (c *Client) DumpLinks(ctx context.Context) (Result[xtcpnl.LinkInfo], error) {
	if c.protocol != unix.NETLINK_ROUTE {
		return Result[xtcpnl.LinkInfo]{}, ErrRequest
	}
	return execute(ctx, c, request[xtcpnl.LinkInfo]{
		build: func(seq uint32, _ uint64) ([]byte, error) {
			return xtcpnl.BuildDumpLinkRequestExt(unix.AF_UNSPEC, 0, seq)
		},
		match: linkMatcher(0), family: unix.RTM_NEWLINK, dump: true,
	})
}

// GetLink refreshes one positive index without filtering out traffic counters.
func (c *Client) GetLink(ctx context.Context, index int32) (Result[xtcpnl.LinkInfo], error) {
	if c.protocol != unix.NETLINK_ROUTE || index <= 0 {
		return Result[xtcpnl.LinkInfo]{}, ErrRequest
	}
	return execute(ctx, c, request[xtcpnl.LinkInfo]{
		build: func(seq uint32, _ uint64) ([]byte, error) {
			return xtcpnl.BuildGetLinkByIndexRequest(unix.AF_UNSPEC, index, 0, seq)
		},
		match: linkMatcher(uint32(index)), family: unix.RTM_NEWLINK,
	})
}

func linkMatcher(index uint32) func(xtcpnl.NetlinkEnvelope) (xtcpnl.LinkInfo, bool, error) {
	return func(e xtcpnl.NetlinkEnvelope) (xtcpnl.LinkInfo, bool, error) {
		got, err := replyIndex(e.Body)
		if err != nil || (index != 0 && got != index) {
			return xtcpnl.LinkInfo{}, false, err
		}
		link, err := xtcpnl.ParseMonitorLink(e.Body, xtcpnl.MonitorLinkRequirements{Name: true})
		return link, err == nil, err
	}
}
