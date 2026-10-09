package linkmonitor

import (
	"fmt"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

func validateRDMACandidate(c *model.Candidate, devices map[model.DeviceKey]*model.Observation) error {
	if len(c.RDMAPorts) > maxInventoryDevices {
		return fmt.Errorf("RDMA inventory exceeds bound")
	}
	seen := make(map[string]bool, len(c.RDMAPorts))
	var uncertain uint64
	records := 0
	for i := range c.RDMAPorts {
		p := &c.RDMAPorts[i]
		key := rdmaSelector(*p)
		if !rdmaName(p.Device, 63) || p.Port == 0 || len(p.Hardware) > maxHardwareIdentity || seen[key] {
			return fmt.Errorf("invalid RDMA port identity")
		}
		seen[key] = true
		records += len(p.Aliases) + len(p.Versions)
		if records > maxInventoryDevices {
			return fmt.Errorf("RDMA associations exceed bound")
		}
		for _, alias := range p.Aliases {
			if !rdmaName(alias, 15) {
				return fmt.Errorf("invalid RDMA alias")
			}
		}
		if p.Eligibility == model.EligibilityUnknown {
			uncertain++
		}
		if p.Eligibility == model.Eligible {
			d := devices[p.Canonical]
			if d == nil || d.Device.Eligibility != model.Eligible || (p.Layer != rdmaNativeLayer && p.Layer != rdmaEthernetLayer) {
				return fmt.Errorf("RDMA canonical target missing")
			}
			if p.Layer == rdmaNativeLayer && (p.Canonical.Kind != model.DeviceNativeRDMA || p.Canonical.RDMADevice != p.Device || p.Canonical.Port != p.Port) {
				return fmt.Errorf("RDMA native target mismatch")
			}
			if p.Layer == rdmaEthernetLayer && (p.Canonical.Kind != model.DeviceEthernet || !d.Device.RDMA) {
				return fmt.Errorf("RDMA Ethernet target mismatch")
			}
		}
	}
	if uncertain != c.RDMAUncertain {
		return fmt.Errorf("RDMA uncertainty mismatch")
	}
	return nil
}
