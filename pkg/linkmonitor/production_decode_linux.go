package linkmonitor

import (
	"errors"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/linuxio"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

func eventControl(e xtcpnl.NetlinkEnvelope) error {
	if e.Control == xtcpnl.NetlinkData {
		return nil
	}
	return linuxio.ErrReply
}

func decodeRouteEvents(data []byte, namespace uint64, now model.Stamp, emit func(model.Event) bool) error {
	return xtcpnl.WalkNetlinkEnvelopes(data, func(e xtcpnl.NetlinkEnvelope) error {
		if err := eventControl(e); err != nil {
			return err
		}
		if e.Header.Type != unix.RTM_NEWLINK && e.Header.Type != unix.RTM_DELLINK {
			return nil
		}
		link, err := xtcpnl.ParseMonitorLink(e.Body, xtcpnl.MonitorLinkRequirements{Name: e.Header.Type == unix.RTM_NEWLINK})
		if err != nil {
			return err
		}
		stats := decodeTraffic(namespace, &link)
		if err := validTraffic(&stats); err != nil {
			return err
		}
		event := model.Event{Kind: model.EventLink, Carrier: stats.Carrier,
			Observation: model.Observation{Observed: now, Device: model.Device{Key: stats.Key, Name: link.Name,
				Up: ethernetUp(presentValue(link.Flags), model.Optional[uint8]{Value: link.OperState, Present: link.HasOperState})}}}
		if e.Header.Type == unix.RTM_DELLINK {
			event.Kind = model.EventRemove
		}
		if !emit(event) {
			return unix.ENOBUFS
		}
		return nil
	})
}

func decodeEthtoolEvents(data []byte, family uint16, namespace uint64, emit func(model.Event) bool) error {
	return xtcpnl.WalkNetlinkEnvelopes(data, func(e xtcpnl.NetlinkEnvelope) error {
		if err := eventControl(e); err != nil {
			return err
		}
		if e.Header.Type != family {
			return nil
		}
		message, err := xtcpnl.ParseEthtool(e.Body, e.Header.Flags)
		if errors.Is(err, xtcpnl.ErrUnsupportedEthtool) {
			if !emit(model.Event{Kind: model.EventResync}) {
				return unix.ENOBUFS
			}
			return nil
		}
		if err != nil {
			return err
		}
		if !message.Notification || message.Request {
			return linuxio.ErrReply
		}
		if message.Header.DeviceIndex == nil || *message.Header.DeviceIndex == 0 {
			return linuxio.ErrReply
		}
		event := model.Event{Kind: model.EventRefresh, Observation: model.Observation{Device: model.Device{
			Key: model.DeviceKey{Namespace: namespace, Kind: model.DeviceEthernet, Index: *message.Header.DeviceIndex}}}}
		if !emit(event) {
			return unix.ENOBUFS
		}
		return nil
	})
}
