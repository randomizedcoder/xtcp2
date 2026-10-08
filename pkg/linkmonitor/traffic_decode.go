package linkmonitor

import (
	"fmt"
	"math"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

var trafficNames = [...]string{
	"receive_packets_total", "transmit_packets_total", "receive_bytes_total", "transmit_bytes_total",
	"receive_errors_total", "transmit_errors_total", "receive_dropped_total", "transmit_dropped_total",
	"multicast_total", "collisions_total", "receive_length_errors_total", "receive_over_errors_total",
	"receive_crc_errors_total", "receive_frame_errors_total", "receive_fifo_errors_total", "receive_missed_errors_total",
	"transmit_aborted_errors_total", "transmit_carrier_errors_total", "transmit_fifo_errors_total",
	"transmit_heartbeat_errors_total", "transmit_window_errors_total", "receive_compressed_total",
	"transmit_compressed_total", "receive_nohandler_total", "receive_otherhost_dropped_total",
}

var trafficDescriptors = trafficCatalog()

func trafficCatalog() [25]string {
	var names [25]string
	for i, name := range trafficNames {
		names[i] = "go_link_monitor_interface_" + name
	}
	return names
}

func decodeTraffic(namespace uint64, link *xtcpnl.LinkInfo) model.LinkStatistics {
	r := model.LinkStatistics{Key: model.DeviceKey{Namespace: namespace, Kind: model.DeviceEthernet, Index: uint32(link.Index)},
		Fields: link.StatsFields, Width: 32}
	if link.StatsIs64 {
		r.Width = 64
	}
	if s := link.Stats; s != nil && r.Fields != 0 {
		r.Values = [25]uint64{s.RxPackets, s.TxPackets, s.RxBytes, s.TxBytes, s.RxErrors, s.TxErrors,
			s.RxDropped, s.TxDropped, s.Multicast, s.Collisions, s.RxLengthErrors, s.RxOverErrors,
			s.RxCrcErrors, s.RxFrameErrors, s.RxFifoErrors, s.RxMissedErrors, s.TxAbortedErrors,
			s.TxCarrierErrors, s.TxFifoErrors, s.TxHeartbeatErrors, s.TxWindowErrors, s.RxCompressed,
			s.TxCompressed, s.RxNohandler, s.RxOtherhostDropped}
	}
	r.Carrier[0] = model.Optional[uint64]{Value: uint64(link.Carrier), Present: link.HasCarrier}
	for i, field := range [...]xtcpnl.U32Attr{link.CarrierChanges, link.CarrierUpCount, link.CarrierDownCount} {
		r.Carrier[i+1] = model.Optional[uint64]{Value: uint64(field.Value), Present: field.Present}
	}
	return r
}

func validTraffic(record *model.LinkStatistics) error {
	if record.Key.Kind != model.DeviceEthernet || record.Key.Index == 0 || record.Key.Index > math.MaxInt32 ||
		(record.Width != 32 && record.Width != 64) || record.Fields > 25 || (record.Width == 32 && record.Fields > 24) {
		return fmt.Errorf("invalid traffic identity or counter layout")
	}
	for _, value := range record.Values[:record.Fields] {
		if record.Width == 32 && value > math.MaxUint32 {
			return fmt.Errorf("traffic counter exceeds source width")
		}
	}
	return validCarrier(record.Carrier)
}

func validCarrier(values model.CarrierValues) error {
	for i, field := range values {
		if field.Present && (field.Value > math.MaxUint32 || (i == 0 && field.Value > 1)) {
			return fmt.Errorf("invalid carrier field")
		}
	}
	return nil
}

func trafficSamples(record *model.LinkStatistics, generation uint64) []model.Sample {
	samples := make([]model.Sample, record.Fields)
	for i := range samples {
		samples[i] = model.Sample{Descriptor: trafficDescriptors[i],
			Kind: model.SampleCounter, Number: model.Unsigned(record.Values[i]),
			Counter: model.CounterIdentity{Source: "rtnetlink:traffic", Width: record.Width, Lifetime: generation}}
	}
	return samples
}
