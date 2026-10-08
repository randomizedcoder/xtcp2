package linkmonitor

import (
	"context"
	"errors"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/linuxio"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

type devlinkRequests interface {
	DiscoverFamily(context.Context, string) (linuxio.Family, error)
	DumpDevlinkPorts(context.Context, linuxio.Family) (linuxio.Result[linuxio.DevlinkPort], error)
	Close() error
}

// ethernetInventory owns a dedicated route client and one devlink snapshot per
// full dump. Optional driver ioctls never run on this inventory lane.
type ethernetInventory struct {
	route     linkRequests
	devlink   devlinkRequests
	metadata  func(context.Context, string, uint32) (hardwareMetadata, error)
	clock     model.Clock
	namespace uint64
	ports     map[uint32]linuxio.DevlinkPort
	bindings  map[uint32]string
}

func newEthernetInventory(namespace uint64, root string, clock model.Clock) (*ethernetInventory, error) {
	route, err := linuxio.NewClient(unix.NETLINK_ROUTE)
	if err != nil {
		return nil, err
	}
	devlink, err := linuxio.NewClient(unix.NETLINK_GENERIC)
	if err != nil {
		return nil, errors.Join(err, route.Close())
	}
	return &ethernetInventory{namespace: namespace, clock: clock, route: route, devlink: devlink, metadata: (identityFilesystem{root: root}).read}, nil
}

func (s *ethernetInventory) Close() error { return errors.Join(s.route.Close(), s.devlink.Close()) }

func (s *ethernetInventory) refreshPorts(ctx context.Context) error {
	s.ports = nil
	s.bindings = make(map[uint32]string)
	family, err := s.devlink.DiscoverFamily(ctx, "devlink")
	if errors.Is(err, unix.ENOENT) || unsupportedEthtool(err) {
		return nil
	}
	if err != nil {
		return err
	}
	result, err := s.devlink.DumpDevlinkPorts(ctx, family)
	if unsupportedEthtool(err) {
		return nil
	}
	if err != nil {
		return err
	}
	ports := make(map[uint32]linuxio.DevlinkPort, len(result.Values))
	for _, port := range result.Values {
		if port.Netdev == 0 {
			continue
		}
		if _, exists := ports[port.Netdev]; exists {
			return linuxio.ErrReply
		}
		ports[port.Netdev] = port
	}
	s.ports = ports
	return nil
}

func (s *ethernetInventory) Dump(ctx context.Context) (model.Candidate, error) {
	c := model.Candidate{Started: s.clock.Now()}
	if err := s.refreshPorts(ctx); err != nil {
		return c, err
	}
	result, err := s.route.DumpLinks(ctx)
	observed := s.clock.Now()
	if err != nil {
		return c, err
	}
	if len(result.Values) > maxInventoryDevices {
		return c, linuxio.ErrLimit
	}
	c.Devices = make([]model.Observation, 0, len(result.Values))
	c.Statistics = make([]model.LinkStatistics, 0, len(result.Values))
	for i := range result.Values {
		observation, statistics, err := s.project(ctx, &result.Values[i], observed, true)
		if err != nil {
			return model.Candidate{}, err
		}
		c.Devices = append(c.Devices, observation)
		c.Statistics = append(c.Statistics, *statistics)
	}
	c.Complete, c.Finished = true, s.clock.Now()
	return c, nil
}

func (s *ethernetInventory) Query(ctx context.Context, key model.DeviceKey) (model.Observation, error) {
	o, _, err := s.QueryStatistics(ctx, key)
	return o, err
}

func (s *ethernetInventory) QueryStatistics(ctx context.Context, key model.DeviceKey) (model.Observation, *model.LinkStatistics, error) {
	if key.Namespace != s.namespace || key.Kind != model.DeviceEthernet {
		return model.Observation{}, nil, linuxio.ErrRequest
	}
	result, err := s.route.GetLink(ctx, int32(key.Index))
	if err != nil {
		return model.Observation{}, nil, err
	}
	if len(result.Values) != 1 || uint32(result.Values[0].Index) != key.Index {
		return model.Observation{}, nil, linuxio.ErrReply
	}
	return s.project(ctx, &result.Values[0], s.clock.Now(), false)
}

func (s *ethernetInventory) project(ctx context.Context, link *xtcpnl.LinkInfo, observed model.Stamp, full bool) (model.Observation, *model.LinkStatistics, error) {
	stats := decodeTraffic(s.namespace, link)
	if err := validTraffic(&stats); err != nil {
		return model.Observation{}, nil, err
	}
	d := model.Device{Key: stats.Key, Name: link.Name, Up: ethernetUp(presentValue(link.Flags), model.Optional[uint8]{Value: link.OperState, Present: link.HasOperState})}
	e := ethernetEvidence{linkType: presentValue(link.Type), kind: link.Kind}
	d.Eligibility = ethernetEligibility(e)
	if d.Eligibility != model.Excluded {
		metadata, err := s.metadata(ctx, link.Name, uint32(link.Index))
		if err != nil {
			return model.Observation{}, nil, err
		}
		var port *linuxio.DevlinkPort
		if value, exists := s.ports[uint32(link.Index)]; exists {
			port = &value
		}
		d.Eligibility, d.HardwareID = classifyHardware(link, metadata, port)
		binding := metadata.path + "\x00" + link.Detail.PhysPortName + "\x00" + string(link.Detail.PhysSwitchID)
		if len(binding) > maxHardwareIdentity {
			return model.Observation{}, nil, linuxio.ErrLimit
		}
		if full {
			s.bindings[uint32(link.Index)] = binding
		} else if port != nil && s.bindings[uint32(link.Index)] != binding {
			// An ifindex alone cannot attach an older devlink port to new hardware.
			d.Eligibility = model.EligibilityUnknown
		}
	}
	stats.Observed = presentValue(observed)
	return model.Observation{Device: d, Observed: observed}, &stats, nil
}
