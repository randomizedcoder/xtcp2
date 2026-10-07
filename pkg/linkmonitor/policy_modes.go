package linkmonitor

import (
	"math/bits"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// The LINKMODES_OURS mask is supported capability; its value is advertisement.
// NOMASK therefore cannot establish a hardware maximum, even if values exist.
func maximumEthernetBits(ours *xtcpnl.EthtoolBitset) model.Optional[uint64] {
	if ours == nil || ours.NoMask {
		return model.Optional[uint64]{}
	}
	var mbps uint32
	var known bool
	if ours.Compact {
		mbps, known = compactMaximum(ours)
	} else {
		mbps, known = verboseMaximum(ours)
	}
	return model.Optional[uint64]{Value: uint64(mbps) * 1000000, Present: known && mbps != 0}
}

func compactMaximum(ours *xtcpnl.EthtoolBitset) (uint32, bool) {
	if ours.Size == nil {
		return 0, false
	}
	words := (uint64(*ours.Size) + 31) / 32
	if uint64(len(ours.Mask)) != words || len(ours.Value) != len(ours.Mask) {
		return 0, false
	}
	var maximum uint32
	for wordIndex, word := range ours.Mask {
		for word != 0 {
			index := uint64(wordIndex)*32 + uint64(bits.TrailingZeros32(word))
			if index >= uint64(*ours.Size) || index >= uint64(len(ethernetModes)) {
				return 0, false
			}
			maximum = max(maximum, ethernetModes[index])
			word &= word - 1
		}
	}
	return maximum, true
}

func verboseMaximum(ours *xtcpnl.EthtoolBitset) (uint32, bool) {
	var maximum uint32
	var seen [len(ethernetModes)]bool
	for i := range ours.Bits {
		bit := &ours.Bits[i]
		index, known := modeIndex(bit)
		if !known || seen[index] || (ours.Size != nil && index >= *ours.Size) {
			return 0, false
		}
		seen[index] = true
		maximum = max(maximum, ethernetModes[index])
	}
	return maximum, true
}

// Read-only kernel replies normally include indices. A name-only record is
// retained by xtcpnl but cannot establish a capability here; a driver string is
// not a reviewed UAPI index. Do not infer a speed from its text.
func modeIndex(bit *xtcpnl.EthtoolBit) (uint32, bool) {
	if bit.Index == nil || uint64(*bit.Index) >= uint64(len(ethernetModes)) {
		return 0, false
	}
	return *bit.Index, true
}
