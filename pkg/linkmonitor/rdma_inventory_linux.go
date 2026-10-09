package linkmonitor

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/linuxio"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"golang.org/x/sys/unix"
)

type rdmaRequests interface {
	DumpRDMADevices(context.Context) (linuxio.Result[linuxio.RDMAPort], error)
	DumpRDMAPorts(context.Context, uint32) (linuxio.Result[linuxio.RDMAPort], error)
	GetRDMAPort(context.Context, uint32, uint32) (linuxio.Result[linuxio.RDMAPort], error)
	Close() error
}

type rdmaInventory struct {
	ethernet  inventoryBackend
	client    rdmaRequests
	files     rdmaFilesystem
	clock     model.Clock
	namespace uint64
}

func (s *rdmaInventory) Close() error { return errors.Join(s.ethernet.Close(), s.client.Close()) }

func (s *rdmaInventory) Dump(ctx context.Context) (model.Candidate, error) {
	c, err := s.ethernet.Dump(ctx)
	if err != nil {
		return model.Candidate{}, err
	}
	ports, err := s.discover(ctx)
	if err != nil {
		return model.Candidate{}, err
	}
	if err := s.associate(ctx, &c, ports); err != nil {
		return model.Candidate{}, err
	}
	c.Finished = s.clock.Now()
	return c, nil
}

func (s *rdmaInventory) discover(ctx context.Context) ([]model.RDMAPort, error) {
	devices, err := s.client.DumpRDMADevices(ctx)
	if unsupportedRDMA(err) && s.files.namespaceVerified {
		return s.sysfsPorts(ctx)
	}
	if err != nil {
		return nil, err
	}
	if len(devices.Values) > maxInventoryDevices {
		return nil, linuxio.ErrLimit
	}
	seen, names := make(map[uint32]bool), make(map[string]bool)
	var ports []model.RDMAPort
	records := 0
	for _, device := range devices.Values {
		if seen[device.Index] || names[device.Name] || !rdmaName(device.Name, 63) {
			return nil, linuxio.ErrReply
		}
		seen[device.Index], names[device.Name] = true, true
		result, err := s.client.DumpRDMAPorts(ctx, device.Index)
		if err != nil {
			return nil, err
		}
		if len(result.Values) > maxInventoryDevices-len(ports) {
			return nil, linuxio.ErrLimit
		}
		portIDs := make(map[uint32]bool)
		for _, value := range result.Values {
			if value.Name != device.Name || value.Index != device.Index || portIDs[value.Port] {
				return nil, linuxio.ErrReply
			}
			portIDs[value.Port] = true
			if value.Port == 0 {
				continue
			} // Switch management ports are not host links.
			p := model.RDMAPort{Device: value.Name, Index: value.Index, Port: value.Port,
				NetdevName: value.NetdevName, Netdev: model.Optional[uint32]{Value: value.Netdev, Present: value.HasNetdev},
				State:    model.Optional[uint8]{Value: value.State, Present: value.HasState},
				Physical: model.Optional[uint8]{Value: value.Physical, Present: value.HasPhysical}}
			if err := s.files.metadata(ctx, &p); err != nil {
				return nil, err
			}
			records += p.Records
			if records > maxInventoryDevices {
				return nil, linuxio.ErrLimit
			}
			ports = append(ports, p)
		}
	}
	return ports, nil
}

func unsupportedRDMA(err error) bool {
	return errors.Is(err, unix.EOPNOTSUPP) || errors.Is(err, unix.EPROTONOSUPPORT)
}

func (s *rdmaInventory) sysfsPorts(ctx context.Context) ([]model.RDMAPort, error) {
	devices, err := rdmaEntries(ctx, s.files.root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ports []model.RDMAPort
	records := 0
	for _, device := range devices {
		if !rdmaName(device.Name(), 63) {
			return nil, linuxio.ErrReply
		}
		entries, err := rdmaEntries(ctx, filepath.Join(s.files.root, device.Name(), "ports"))
		if err != nil {
			return nil, err
		}
		if len(entries) > maxInventoryDevices-len(ports) {
			return nil, linuxio.ErrLimit
		}
		for _, entry := range entries {
			n, err := strconv.ParseUint(entry.Name(), 10, 32)
			if err != nil || n == 0 {
				return nil, linuxio.ErrReply
			}
			p := model.RDMAPort{Device: device.Name(), Port: uint32(n), Sysfs: true}
			if err := s.files.metadata(ctx, &p); err != nil {
				return nil, err
			}
			records += p.Records
			if records > maxInventoryDevices {
				return nil, linuxio.ErrLimit
			}
			p.State, p.Physical, err = s.files.state(ctx, p)
			if err != nil {
				return nil, err
			}
			ports = append(ports, p)
		}
	}
	return ports, nil
}

func (s *rdmaInventory) Query(ctx context.Context, key model.DeviceKey) (model.Observation, error) {
	o, _, err := s.QueryStatistics(ctx, key)
	return o, err
}

func (s *rdmaInventory) QueryStatistics(ctx context.Context, key model.DeviceKey) (model.Observation, *model.LinkStatistics, error) {
	// Association changes need a complete candidate. A converging query repeats
	// discovery rather than attaching cached RDMA evidence to a reused ifindex.
	c, err := s.Dump(ctx)
	if err != nil {
		return model.Observation{}, nil, err
	}
	for index := range c.Devices {
		o := c.Devices[index]
		if o.Device.Key != key {
			continue
		}
		for i := range c.Statistics {
			if c.Statistics[i].Key == key {
				return o, &c.Statistics[i], nil
			}
		}
		return o, nil, nil
	}
	return model.Observation{}, nil, unix.ENODEV
}

func rdmaSelector(p model.RDMAPort) string {
	return "rdma:" + p.Device + ":" + strconv.FormatUint(uint64(p.Port), 10)
}

func (s *rdmaInventory) associate(ctx context.Context, c *model.Candidate, ports []model.RDMAPort) error {
	devices := make(map[uint32]model.Device)
	positions := make(map[model.DeviceKey]int)
	for i := range c.Devices {
		devices[c.Devices[i].Device.Key.Index] = c.Devices[i].Device
		positions[c.Devices[i].Device.Key] = i
	}
	metadata, err := s.files.netdevices(ctx)
	if err != nil && len(ports) != 0 {
		return err
	}
	graph := newRDMAGraph(metadata, c.NetdevLinks)
	records := 0
	for i := range ports {
		p := &ports[i]
		if err := s.associatePort(p, graph, devices); err != nil {
			return err
		}
		records += len(p.Aliases) + len(p.Versions)
		if records > maxInventoryDevices {
			return linuxio.ErrLimit
		}
		if p.Eligibility == model.EligibilityUnknown {
			c.RDMAUncertain++
		}
		if p.Eligibility != model.Eligible {
			continue
		}
		if p.Canonical.Kind == model.DeviceNativeRDMA {
			if len(c.Devices) == maxInventoryDevices {
				return linuxio.ErrLimit
			}
			d := model.Device{Key: p.Canonical, Name: rdmaSelector(*p), HardwareID: p.Hardware,
				Eligibility: model.Eligible, Up: nativeRDMAUp(p.State, p.Physical)}
			c.Devices = append(c.Devices, model.Observation{Device: d, Observed: s.clock.Now()})
		} else {
			c.Devices[positions[p.Canonical]].Device.RDMA = true
		}
	}
	c.RDMAPorts = ports
	return nil
}

func (s *rdmaInventory) associatePort(p *model.RDMAPort, graph rdmaGraph, devices map[uint32]model.Device) error {
	layer := rdmaLayerUnknown
	if p.Layer == rdmaNativeLayer {
		layer = rdmaNative
	}
	if p.Layer == rdmaEthernetLayer {
		layer = rdmaEthernet
	}
	e := rdmaIdentityEvidence{key: model.DeviceKey{Namespace: s.namespace, RDMADevice: p.Device, Port: p.Port}, layer: layer}
	if p.Hardware != "" {
		e.hardware = presentValue(true)
	}
	if p.Eligibility == model.Excluded {
		e.hardware = presentValue(false)
	}
	if layer == rdmaNative {
		p.Aliases = append([]string(nil), graph.native[rdmaAliasKey{p.Hardware, uint64(p.Port)}]...)
	} else {
		if p.Netdev.Present {
			n, exists := graph.names[p.NetdevName]
			if !exists || n.index != p.Netdev.Value {
				p.Eligibility = model.EligibilityUnknown
				return nil
			}
			p.Aliases = append(p.Aliases, p.NetdevName)
		}
		for _, name := range p.Aliases {
			e.associations = append(e.associations, resolveRDMALower(name, graph, devices))
		}
	}
	identity := resolveRDMAIdentity(e)
	p.Canonical, p.Eligibility = identity.canonical, identity.eligibility
	sort.Strings(p.Aliases)
	p.Aliases = compactRDMANames(p.Aliases)
	return nil
}

func compactRDMANames(names []string) []string {
	n := 0
	for _, name := range names {
		if n == 0 || names[n-1] != name {
			names[n] = name
			n++
		}
	}
	return names[:n]
}
