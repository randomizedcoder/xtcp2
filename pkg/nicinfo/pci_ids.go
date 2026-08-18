//go:build linux

package nicinfo

import (
	_ "embed"
	"strconv"
	"strings"
)

// pciIDsData is a small embedded subset of the pci.ids database covering the
// fleet's known NICs. Embedding avoids reading from the read-only production
// rootfs at runtime. See pci_ids.txt for the format.
//
//go:embed pci_ids.txt
var pciIDsData []byte

// pciKey identifies a device by [vendor, device].
type pciKey [2]uint32

// pciModels maps vendor:device to a human model string, parsed once at init.
var pciModels = parsePCIIDs(pciIDsData)

// parsePCIIDs parses the embedded pci.ids subset. Each non-comment, non-blank
// line is "<vendorhex>:<devicehex> <model...>". Malformed lines are skipped.
func parsePCIIDs(data []byte) map[pciKey]string {
	m := make(map[pciKey]string)
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		id, model, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		vs, ds, ok := strings.Cut(id, ":")
		if !ok {
			continue
		}
		vendor, err := strconv.ParseUint(strings.TrimSpace(vs), 16, 32)
		if err != nil {
			continue
		}
		device, err := strconv.ParseUint(strings.TrimSpace(ds), 16, 32)
		if err != nil {
			continue
		}
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		m[pciKey{uint32(vendor), uint32(device)}] = model
	}
	return m
}

// modelFor returns the model string for a vendor:device pair, or "" if unknown.
func modelFor(vendor, device uint32) string {
	return pciModels[pciKey{vendor, device}]
}
