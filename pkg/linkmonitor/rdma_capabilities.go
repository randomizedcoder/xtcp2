package linkmonitor

import (
	"strconv"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/rdmacaps"
)

var rdmaWidths = [...]uint8{1, 4, 8, 12, 2}

func rdmaWidth(mask uint8) uint8 {
	for bit, lanes := range rdmaWidths {
		if mask == 1<<bit {
			return lanes
		}
	}
	return 0
}

func rdmaSupported(c rdmacaps.Capabilities) rdmaCapabilities {
	result := rdmaCapabilities{complete: c.Complete, supported: make([]rdmaMode, 0, 8)}
	var lanes uint8
	for bit, value := range rdmaWidths {
		if c.WidthSupported&(1<<bit) != 0 {
			lanes = max(lanes, value)
		}
	}
	// Standard PortInfo reports independent supported speed and width masks.
	// Use the largest supported width for each speed, not enabled/active width.
	for _, speed := range [...]uint32{1, 2, 4, 16, 32, 64, 128, 256} {
		if c.Supported&speed != 0 {
			result.supported = append(result.supported, rdmaMode{speed: speed, lanes: lanes})
		}
	}
	return result
}

func rdmaLabels(p model.RDMAPort) []model.Label {
	return []model.Label{{Name: "device", Value: p.Device}, {Name: "port", Value: strconv.FormatUint(uint64(p.Port), 10)}}
}

func rdmaCapabilitySamples(up model.Optional[bool], p model.RDMAPort, c rdmacaps.Capabilities) ([]model.Sample, *model.SettingsChecks) {
	active := rdmaMode{speed: c.Active, lanes: rdmaWidth(c.WidthActive)}
	checks := checkNativeRDMA(up, active, rdmaSupported(c))
	samples := make([]model.Sample, 0, 5)
	if checks.maximumBits.Present {
		samples = append(samples, interfaceGauge("max_speed_bits_per_second", checks.maximumBits.Value))
	}
	if checks.activeBits.Present && up.Present && up.Value {
		samples = append(samples, interfaceGauge("speed_bits_per_second", checks.activeBits.Value))
	}
	add := func(name string, value uint64) {
		samples = append(samples, model.Sample{Descriptor: "go_link_monitor_" + name, Kind: model.SampleGauge, Number: model.Unsigned(value), Labels: rdmaLabels(p)})
	}
	if checks.maximumWidth.Present {
		add("rdma_port_max_width", uint64(checks.maximumWidth.Value))
	}
	if up.Present && up.Value {
		if checks.activeWidth.Present {
			add("rdma_port_active_width", uint64(checks.activeWidth.Value))
		}
		labels := append(rdmaLabels(p), model.Label{Name: "generation", Value: rdmaSpeedName(c.Active)})
		samples = append(samples, model.Sample{Descriptor: "go_link_monitor_rdma_port_speed_info", Kind: model.SampleGauge, Number: model.Unsigned(1), Labels: labels})
	}
	return samples, &model.SettingsChecks{Speed: checks.speed, Width: checks.width, Duplex: checks.duplex}
}

func rdmaSpeedName(speed uint32) string {
	switch speed {
	case 1:
		return "SDR"
	case 2:
		return "DDR"
	case 16:
		return "FDR"
	case 32:
		return "EDR"
	case 64:
		return "HDR"
	case 128:
		return "NDR"
	case 256:
		return "XDR"
	default:
		return unknownSetting // QDR/FDR10 need vendor evidence to distinguish names.
	}
}
