package linkmonitor

import (
	"strconv"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

const interfaceMetricPrefix = "go_link_monitor_interface_"
const unknownSetting = "unknown"
const channelsMetric = "channels"

var interfaceDescriptors = interfaceCatalog()
var ethernetModeLabels = modeLabelCatalog()

func interfaceCatalog() map[string]string {
	names := []string{
		"driver_info", "driver_known", channelsMetric, "channels_max", "ring_entries", "ring_entries_max",
		"ring_rx_buffer_bytes", "ring_cqe_bytes", "ring_tx_push_buffer_bytes", "ring_tx_push_buffer_max_bytes",
		"ring_header_split_threshold_bytes", "ring_header_split_threshold_max_bytes", "ring_tcp_data_split", "ring_tx_push", "ring_rx_push",
		"speed_bits_per_second", "max_speed_bits_per_second", "duplex_info", "autonegotiate",
		"supported_speed_bits_per_second", "advertised_speed_bits_per_second", "supported_port_info",
		"autonegotiate_supported", "autonegotiate_advertised", "pause_supported", "pause_advertised", "asymmetric_pause_supported", "asymmetric_pause_advertised",
	}
	result := make(map[string]string, len(names))
	for _, name := range names {
		result[name] = interfaceMetricPrefix + name
	}
	return result
}

func modeLabelCatalog() [len(ethernetModes)][]model.Label {
	var labels [len(ethernetModes)][]model.Label
	for index := range labels {
		duplex := "full"
		switch index {
		case 0, 2, 4, 90, 100, 101: // All reviewed UAPI half-duplex modes, including FX/T1S.
			duplex = "half"
		}
		labels[index] = []model.Label{{Name: "duplex", Value: duplex}, {Name: "mode", Value: strconv.Itoa(index)}}
	}
	return labels
}

func interfaceGauge(name string, value uint64, labels ...model.Label) model.Sample {
	return model.Sample{Descriptor: interfaceDescriptors[name], Kind: model.SampleGauge, Number: model.Unsigned(value), Labels: labels}
}

func configurationSamples(m *xtcpnl.EthtoolMessage) []model.Sample {
	if m.Channels != nil {
		return channelSamples(m.Channels)
	}
	if m.Rings != nil {
		return ringSamples(m.Rings)
	}
	return nil
}

func channelSamples(c *xtcpnl.EthtoolChannels) []model.Sample {
	values := [...]*uint32{c.RX, c.TX, c.Other, c.Combined, c.RXMax, c.TXMax, c.OtherMax, c.CombinedMax}
	kinds := [...]string{"rx", "tx", "other", "combined"}
	result := make([]model.Sample, 0, len(values))
	for i, v := range values {
		if v == nil {
			continue
		}
		name := channelsMetric
		if i >= len(kinds) {
			name += "_max"
		}
		result = append(result, interfaceGauge(name, uint64(*v), model.Label{Name: "kind", Value: kinds[i%len(kinds)]}))
	}
	return result
}

func ringSamples(r *xtcpnl.EthtoolRings) []model.Sample {
	values := [...]*uint32{r.RX, r.TX, r.RXMini, r.RXJumbo, r.RXMax, r.TXMax, r.RXMiniMax, r.RXJumboMax}
	kinds := [...]string{"rx", "tx", "rx_mini", "rx_jumbo"}
	result := make([]model.Sample, 0, 17)
	for i, v := range values {
		if v == nil {
			continue
		}
		name := "ring_entries"
		if i >= len(kinds) {
			name += "_max"
		}
		result = append(result, interfaceGauge(name, uint64(*v), model.Label{Name: "kind", Value: kinds[i%len(kinds)]}))
	}
	for _, field := range []struct {
		name  string
		value *uint32
	}{
		{"ring_rx_buffer_bytes", r.RXBufLen}, {"ring_cqe_bytes", r.CQESize},
		{"ring_tx_push_buffer_bytes", r.TXPushBufLen}, {"ring_tx_push_buffer_max_bytes", r.TXPushBufLenMax},
		{"ring_header_split_threshold_bytes", r.HDSThresh}, {"ring_header_split_threshold_max_bytes", r.HDSThreshMax},
	} {
		if field.value != nil {
			result = append(result, interfaceGauge(field.name, uint64(*field.value)))
		}
	}
	for _, field := range []struct {
		name  string
		value *uint8
	}{
		{"ring_tcp_data_split", r.TCPDataSplit}, {"ring_tx_push", r.TXPush}, {"ring_rx_push", r.RXPush},
	} {
		if field.value != nil {
			result = append(result, interfaceGauge(field.name, uint64(*field.value)))
		}
	}
	return result
}

func settingsSamples(up model.Optional[bool], m *xtcpnl.EthtoolLinkModes) ([]model.Sample, *model.SettingsChecks) {
	checks := checkEthernet(up, m)
	result := modeSamples(m)
	if checks.maximumBits.Present {
		result = append(result, interfaceGauge("max_speed_bits_per_second", checks.maximumBits.Value))
	}
	if up.Present && up.Value && checks.activeBits.Present {
		result = append(result, interfaceGauge("speed_bits_per_second", checks.activeBits.Value))
	}
	if m != nil && m.Autoneg != nil {
		result = append(result, interfaceGauge("autonegotiate", uint64(*m.Autoneg)))
	}
	duplex := unknownSetting
	if up.Present && up.Value && m != nil && m.Duplex != nil {
		if *m.Duplex == 0 {
			duplex = "half"
		}
		if *m.Duplex == 1 {
			duplex = "full"
		}
	}
	result = append(result, interfaceGauge("duplex_info", 1, model.Label{Name: "duplex", Value: duplex}, model.Label{Name: "source", Value: "ethtool"}))
	return result, &model.SettingsChecks{Speed: checks.speed, Duplex: checks.duplex}
}

func modeSamples(m *xtcpnl.EthtoolLinkModes) []model.Sample {
	if m == nil || m.Ours == nil {
		return nil
	}
	result := make([]model.Sample, 0, 16)
	for index, mbps := range ethernetModes {
		supported, advertised, known := modePresence(m.Ours, uint32(index))
		if !known {
			continue
		}
		if mbps != 0 {
			labels := ethernetModeLabels[index]
			if supported && !m.Ours.NoMask {
				result = append(result, interfaceGauge("supported_speed_bits_per_second", uint64(mbps)*1000000, labels...))
			}
			if advertised {
				result = append(result, interfaceGauge("advertised_speed_bits_per_second", uint64(mbps)*1000000, labels...))
			}
		}
	}
	return appendModeFeatures(result, m.Ours)
}

func modePresence(bits *xtcpnl.EthtoolBitset, index uint32) (supported, advertised, known bool) {
	if bits.Compact {
		if bits.Size == nil || index >= *bits.Size || int(index/32) >= len(bits.Value) {
			return false, false, false
		}
		if bits.NoMask {
			return false, bits.Value[index/32]&(1<<(index%32)) != 0, true
		}
		if len(bits.Mask) != len(bits.Value) {
			return false, false, false
		}
		return bits.Mask[index/32]&(1<<(index%32)) != 0, bits.Value[index/32]&(1<<(index%32)) != 0, true
	}
	for _, bit := range bits.Bits {
		if bit.Index != nil && *bit.Index == index {
			return true, bit.Value, true
		}
	}
	return false, false, bits.Size != nil && index < *bits.Size
}

func appendModeFeatures(result []model.Sample, bits *xtcpnl.EthtoolBitset) []model.Sample {
	for _, feature := range []struct {
		index uint32
		name  string
	}{{6, "autonegotiate"}, {13, "pause"}, {14, "asymmetric_pause"}} {
		supported, advertised, known := modePresence(bits, feature.index)
		if known {
			if !bits.NoMask {
				result = append(result, interfaceGauge(feature.name+"_supported", boolValue(supported)))
			}
			result = append(result, interfaceGauge(feature.name+"_advertised", boolValue(advertised)))
		}
	}
	for _, port := range []struct {
		index uint32
		name  string
	}{{7, "tp"}, {8, "aui"}, {9, "mii"}, {10, "fiber"}, {11, "bnc"}, {16, "backplane"}} {
		supported, _, known := modePresence(bits, port.index)
		if known && supported && !bits.NoMask {
			result = append(result, interfaceGauge("supported_port_info", 1, model.Label{Name: "type", Value: port.name}))
		}
	}
	return result
}

func boolValue(v bool) uint64 {
	if v {
		return 1
	}
	return 0
}
