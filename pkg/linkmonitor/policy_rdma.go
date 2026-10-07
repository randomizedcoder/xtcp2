package linkmonitor

import "github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"

type rdmaLayer uint8

const (
	rdmaLayerUnknown rdmaLayer = iota
	rdmaNative
	rdmaEthernet
)

type rdmaAssociation struct {
	key         model.DeviceKey
	eligibility model.Eligibility
}

type rdmaIdentityEvidence struct {
	key          model.DeviceKey
	layer        rdmaLayer
	hardware     model.Optional[bool]
	associations []rdmaAssociation // Already resolved through kernel lower-link associations.
}

type rdmaIdentity struct {
	canonical       model.DeviceKey
	eligibility     model.Eligibility
	countSeparately bool
}

func resolveRDMAIdentity(e rdmaIdentityEvidence) rdmaIdentity {
	if !e.hardware.Present {
		return rdmaIdentity{}
	}
	if !e.hardware.Value {
		return rdmaIdentity{eligibility: model.Excluded}
	}
	if e.key.RDMADevice == "" || e.key.Port == 0 {
		return rdmaIdentity{}
	}
	switch e.layer {
	case rdmaNative:
		key := e.key
		key.Kind = model.DeviceNativeRDMA
		key.Index = 0
		return rdmaIdentity{canonical: key, eligibility: model.Eligible, countSeparately: true}
	case rdmaEthernet:
		return resolveRoCE(e)
	default:
		return rdmaIdentity{}
	}
}

func resolveRoCE(e rdmaIdentityEvidence) rdmaIdentity {
	var target model.DeviceKey
	for _, association := range e.associations {
		if association.eligibility == model.EligibilityUnknown {
			return rdmaIdentity{}
		}
		if association.eligibility != model.Eligible {
			continue
		}
		key := association.key
		if key.Kind != model.DeviceEthernet || key.Index == 0 || key.Namespace != e.key.Namespace {
			return rdmaIdentity{}
		}
		if target != (model.DeviceKey{}) && target != key {
			return rdmaIdentity{}
		}
		target = key
	}
	if target == (model.DeviceKey{}) {
		return rdmaIdentity{}
	}
	return rdmaIdentity{canonical: target, eligibility: model.Eligible}
}

func nativeRDMAUp(state, physical model.Optional[uint8]) model.Optional[bool] {
	if !state.Present || !physical.Present {
		return model.Optional[bool]{}
	}
	if state.Value < 1 || state.Value > 5 || physical.Value < 1 || physical.Value > 7 {
		return model.Optional[bool]{}
	}
	return model.Optional[bool]{Value: state.Value == 4 && physical.Value == 5, Present: true}
}

// RDMA readiness is separate from Ethernet up and GID protocol availability.
func roceReadiness(state model.Optional[uint8]) model.Check {
	if !state.Present || state.Value < 1 || state.Value > 5 {
		return model.CheckUnknown
	}
	return equalCheck(state.Value, uint8(4))
}

// A mode is one supported speed/width combination, normalized by the UMAD
// adapter. Never construct a cross-product when hardware reports constraints.
type rdmaMode struct {
	speed uint32
	lanes uint8
}

type rdmaCapabilities struct {
	complete  bool
	supported []rdmaMode
}

type rdmaChecks struct {
	speed, width, duplex      model.Check
	activeBits, maximumBits   model.Optional[uint64]
	activeWidth, maximumWidth model.Optional[uint8]
}

func checkNativeRDMA(up model.Optional[bool], active rdmaMode, supported rdmaCapabilities) rdmaChecks {
	result := rdmaChecks{duplex: model.CheckNotApplicable}
	result.activeBits = nativeRate(active)
	if validLanes(active.lanes) {
		result.activeWidth = presentValue(active.lanes)
	}
	result.maximumBits, result.maximumWidth = nativeMaximum(supported)
	if !up.Present {
		return result
	}
	if !up.Value {
		result.speed, result.width = model.CheckNotApplicable, model.CheckNotApplicable
		return result
	}
	if result.activeBits.Present && result.maximumBits.Present {
		result.speed = equalCheck(result.activeBits.Value, result.maximumBits.Value)
	}
	if result.activeWidth.Present && result.maximumWidth.Present {
		result.width = equalCheck(result.activeWidth.Value, result.maximumWidth.Value)
	}
	return result
}

func nativeMaximum(capabilities rdmaCapabilities) (model.Optional[uint64], model.Optional[uint8]) {
	var rate model.Optional[uint64]
	var width model.Optional[uint8]
	if !capabilities.complete {
		return rate, width
	}
	rateKnown, widthKnown := len(capabilities.supported) > 0, len(capabilities.supported) > 0
	for _, mode := range capabilities.supported {
		current := nativeRate(mode)
		rateKnown = rateKnown && current.Present
		widthKnown = widthKnown && validLanes(mode.lanes)
		rate = presentValue(max(rate.Value, current.Value))
		width = presentValue(max(width.Value, mode.lanes))
	}
	rate.Present, width.Present = rateKnown, widthKnown
	return rate, width
}

// Nominal rates follow ib_port_attr_to_speed_info in Linux verbs.c: per-lane
// 2.5/5/10/10/14/25/50/100/200 Gbit/s for SDR through XDR. These are nominal
// link rates, not payload throughput. Unlike the kernel display fallback, an
// unknown future speed stays unknown instead of being treated as SDR.
func nativeRate(mode rdmaMode) model.Optional[uint64] {
	var perLaneMbps uint64
	switch mode.speed {
	case 1:
		perLaneMbps = 2500 // SDR
	case 2:
		perLaneMbps = 5000 // DDR
	case 4, 8:
		perLaneMbps = 10000 // QDR, FDR10
	case 16:
		perLaneMbps = 14000 // FDR
	case 32:
		perLaneMbps = 25000 // EDR
	case 64:
		perLaneMbps = 50000 // HDR
	case 128:
		perLaneMbps = 100000 // NDR
	case 256:
		perLaneMbps = 200000 // XDR
	default:
		return model.Optional[uint64]{}
	}
	if !validLanes(mode.lanes) {
		return model.Optional[uint64]{}
	}
	return presentValue(perLaneMbps * uint64(mode.lanes) * 1000000)
}

func validLanes(lanes uint8) bool {
	return lanes == 1 || lanes == 2 || lanes == 4 || lanes == 8 || lanes == 12
}

func presentValue[T any](value T) model.Optional[T] {
	return model.Optional[T]{Value: value, Present: true}
}
