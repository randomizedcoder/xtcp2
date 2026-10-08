package linkmonitor

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/linuxio"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

const maxHardwareIdentity = 1024

type hardwareMetadata struct {
	path            string
	wireless, ownVF bool
	port            uint64
}

type identityFilesystem struct{ root string }

func (s identityFilesystem) read(ctx context.Context, name string, index uint32) (hardwareMetadata, error) {
	var m hardwareMetadata
	if !validName(name, 15) || name == "." || name == ".." {
		return m, linuxio.ErrRequest
	}
	path := filepath.Join(s.root, name)
	check := carrierFilesystem{readFile: readCarrierFile}
	if err := check.checkIndex(ctx, path, index); err != nil {
		return m, err
	}
	device, err := filepath.EvalSymlinks(filepath.Join(path, "device"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return m, err
	}
	if len(device) > maxHardwareIdentity-32 {
		return m, linuxio.ErrLimit
	}
	m.path = device
	m.wireless, err = metadataExists(filepath.Join(path, "wireless"))
	if err != nil {
		return m, err
	}
	m.ownVF, err = metadataExists(filepath.Join(path, "device", "physfn"))
	if err != nil {
		return m, err
	}
	m.port, err = readCarrierFile(ctx, filepath.Join(path, "dev_port"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return m, err
	}
	if err := check.checkIndex(ctx, path, index); err != nil {
		return hardwareMetadata{}, err
	}
	return m, nil
}

func metadataExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

func classifyHardware(link *xtcpnl.LinkInfo, m hardwareMetadata, port *linuxio.DevlinkPort) (model.Eligibility, string) {
	e := ethernetEvidence{linkType: presentValue(link.Type), kind: link.Kind,
		hardware: presentValue(m.path != ""), wireless: presentValue(m.wireless)}
	// A switch ID without a correlated port leaves representor status unknown.
	if len(link.Detail.PhysSwitchID) == 0 {
		e.representor = presentValue(false)
	}
	if port != nil {
		if m.path == "" {
			e.conflicting = true
		}
		if !port.HasFlavor {
			e.representor = model.Optional[bool]{}
		} else {
			switch port.Flavor {
			case 0:
				e.representor = presentValue(false) // Physical front-panel port.
			case 3, 4, 5, 6, 7: // PCI PF/VF, virtual, unused, PCI SF.
				e.representor = presentValue(true)
				if port.Flavor == 4 && m.ownVF && port.Bus == "pci" && port.Device == filepath.Base(m.path) {
					e.representor = presentValue(false)
				}
			default:
				e.representor = model.Optional[bool]{}
			}
		}
	}
	identity := ""
	if m.path != "" {
		identity = m.path + "/port=" + strconv.FormatUint(m.port, 10)
	}
	if strings.ContainsRune(identity, 0) || len(identity) > maxHardwareIdentity {
		e.conflicting = true
		identity = ""
	}
	return ethernetEligibility(e), identity
}
