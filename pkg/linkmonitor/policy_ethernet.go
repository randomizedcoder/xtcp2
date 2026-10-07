package linkmonitor

import (
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// Ethernet metadata is evidence about this netdevice, not just its parent or
// interface-name pattern. Adapters must resolve wireless/switchdev ambiguity.
type ethernetEvidence struct {
	linkType    model.Optional[uint16]
	kind        string
	hardware    model.Optional[bool]
	wireless    model.Optional[bool]
	representor model.Optional[bool]
	conflicting bool
}

func ethernetEligibility(e ethernetEvidence) model.Eligibility {
	if e.conflicting || !e.linkType.Present {
		return model.EligibilityUnknown
	}
	if e.linkType.Value != unix.ARPHRD_ETHER {
		return model.Excluded
	}
	if e.wireless.Present && e.wireless.Value {
		return model.Excluded
	}
	if e.representor.Present && e.representor.Value {
		return model.Excluded
	}
	switch e.kind {
	case "veth", "vlan", "bridge", "bond", "team", "tun", "tap", "dummy", "vxlan", "geneve", "gretap", "ip6gretap", "macvlan", "macvtap", "ipvlan", "vrf", "nlmon":
		return model.Excluded
	case "": // Hardware, USB, guest and VF devices use ancestry evidence.
	default:
		return model.EligibilityUnknown
	}
	if !e.hardware.Present || !e.wireless.Present || !e.representor.Present {
		return model.EligibilityUnknown
	}
	if e.hardware.Value {
		return model.Eligible
	}
	return model.Excluded
}

// ethernetUp deliberately does not use LinkInfo.IsUp: DORMANT never counts, and
// operstate UP does not additionally require the compatibility IFF_RUNNING bit.
func ethernetUp(flags model.Optional[uint32], operstate model.Optional[uint8]) model.Optional[bool] {
	if !flags.Present {
		return model.Optional[bool]{}
	}
	if flags.Value&unix.IFF_UP == 0 {
		return model.Optional[bool]{Present: true}
	}
	if !operstate.Present {
		return model.Optional[bool]{}
	}
	switch operstate.Value {
	case xtcpnl.IfOperUp:
		return model.Optional[bool]{Value: true, Present: true}
	case xtcpnl.IfOperUnknown:
		return model.Optional[bool]{Value: flags.Value&unix.IFF_RUNNING != 0, Present: true}
	case xtcpnl.IfOperNotPresent, xtcpnl.IfOperDown, xtcpnl.IfOperLowerLayerDown, xtcpnl.IfOperTesting, xtcpnl.IfOperDormant:
		return model.Optional[bool]{Present: true}
	default:
		return model.Optional[bool]{}
	}
}

type ethernetChecks struct {
	speed, duplex           model.Check
	activeBits, maximumBits model.Optional[uint64]
}

func checkEthernet(up model.Optional[bool], modes *xtcpnl.EthtoolLinkModes) ethernetChecks {
	var result ethernetChecks
	if modes != nil {
		result.maximumBits = maximumEthernetBits(modes.Ours)
		if modes.Speed != nil && *modes.Speed != 0 && *modes.Speed != xtcpnl.EthtoolSpeedUnknown {
			result.activeBits = model.Optional[uint64]{Value: uint64(*modes.Speed) * 1000000, Present: true}
		}
	}
	if !up.Present {
		return result
	}
	if !up.Value {
		result.speed, result.duplex = model.CheckNotApplicable, model.CheckNotApplicable
		return result
	}
	if result.activeBits.Present && result.maximumBits.Present {
		result.speed = equalCheck(result.activeBits.Value, result.maximumBits.Value)
	}
	if modes != nil && modes.Duplex != nil {
		switch *modes.Duplex {
		case 0:
			result.duplex = model.CheckFail
		case 1:
			result.duplex = model.CheckPass
		}
	}
	return result
}

func equalCheck[T comparable](active, maximum T) model.Check {
	if active == maximum {
		return model.CheckPass
	}
	return model.CheckFail
}
