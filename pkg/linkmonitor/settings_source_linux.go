package linkmonitor

import (
	"context"
	"errors"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/linuxio"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

type settingsRequests interface {
	DiscoverFamily(context.Context, string) (linuxio.Family, error)
	GetEthtool(context.Context, linuxio.Family, xtcpnl.EthtoolKind, uint32) (linuxio.Result[xtcpnl.EthtoolMessage], error)
	Close() error
}

type settingsFallback interface {
	read(context.Context, string, uint32, xtcpnl.EthtoolKind) (xtcpnl.EthtoolMessage, error)
	driver(context.Context, string, uint32) ([]model.Sample, error)
	Close() error
}

// settingsCollector is confined to one worker, including during blocked ioctls.
type settingsCollector struct {
	workerCollector
	client settingsRequests
	ioctl  settingsFallback
	family linuxio.Family
}

func newSettingsCollector(base workerCollector) (*settingsCollector, error) {
	client, err := linuxio.NewClient(unix.NETLINK_GENERIC)
	if err != nil {
		return nil, err
	}
	ioctl, err := newEthtoolIoctl()
	if err != nil {
		return nil, errors.Join(err, client.Close())
	}
	return &settingsCollector{workerCollector: base, client: client, ioctl: ioctl}, nil
}

func (c *settingsCollector) Close() error {
	return errors.Join(c.client.Close(), c.ioctl.Close(), c.workerCollector.Close())
}

func (c *settingsCollector) Collect(ctx context.Context, job model.Job) model.Result {
	kind, handled := settingsKind(job.Key.Collector)
	if !handled {
		return c.workerCollector.Collect(ctx, job)
	}
	result := model.Result{Job: job, Support: model.NotApplicable}
	if job.Device.Key.Kind == model.DeviceEthernet && job.Device.Eligibility == model.EligibilityUnknown {
		result.Support = model.SupportUnknown
	}
	if job.Device.Key.Kind != model.DeviceEthernet || job.Device.Eligibility != model.Eligible {
		return result
	}
	var err error
	if job.Key.Collector == model.CollectorInventory {
		result.Samples, err = c.ioctl.driver(ctx, job.Device.Name, job.Key.Device.Index)
	} else {
		var message xtcpnl.EthtoolMessage
		message, err = c.get(ctx, job, kind)
		if err == nil {
			err = validateSettingsMessage(kind, &message)
		}
		if err == nil {
			if kind == xtcpnl.EthtoolLinkModesKind {
				result.Samples, result.Settings = settingsSamples(job.Device.Up, message.LinkModes)
			} else {
				result.Samples = configurationSamples(&message)
			}
		}
	}
	result.Support = model.Supported
	if err != nil {
		result.Support = model.SupportUnknown
		if unsupportedEthtool(err) {
			result.Support = model.Unsupported
		} else {
			result.Err, result.Reason = err, trafficReason(err)
		}
	}
	return result
}

func validateSettingsMessage(kind xtcpnl.EthtoolKind, m *xtcpnl.EthtoolMessage) error {
	switch kind {
	case xtcpnl.EthtoolLinkModesKind:
		if m.LinkModes == nil || (m.LinkModes.Autoneg != nil && *m.LinkModes.Autoneg > 1) {
			return linuxio.ErrReply
		}
	case xtcpnl.EthtoolChannelsKind:
		if m.Channels == nil {
			return linuxio.ErrReply
		}
	case xtcpnl.EthtoolRingsKind:
		if m.Rings == nil || (m.Rings.TXPush != nil && *m.Rings.TXPush > 1) || (m.Rings.RXPush != nil && *m.Rings.RXPush > 1) {
			return linuxio.ErrReply
		}
	default:
		return linuxio.ErrRequest
	}
	return nil
}

func settingsKind(kind model.CollectorKind) (xtcpnl.EthtoolKind, bool) {
	switch kind {
	case model.CollectorInventory:
		return "", true
	case model.CollectorSettings:
		return xtcpnl.EthtoolLinkModesKind, true
	case model.CollectorChannels:
		return xtcpnl.EthtoolChannelsKind, true
	case model.CollectorRings:
		return xtcpnl.EthtoolRingsKind, true
	default:
		return "", false
	}
}

func (c *settingsCollector) get(ctx context.Context, job model.Job, kind xtcpnl.EthtoolKind) (xtcpnl.EthtoolMessage, error) {
	var err error
	if c.family.ID() == 0 {
		c.family, err = c.client.DiscoverFamily(ctx, "ethtool")
		if errors.Is(err, unix.ENOENT) || unsupportedEthtool(err) {
			return c.ioctl.read(ctx, job.Device.Name, job.Key.Device.Index, kind)
		}
		if err != nil {
			return xtcpnl.EthtoolMessage{}, err
		}
	}
	response, err := c.client.GetEthtool(ctx, c.family, kind, job.Key.Device.Index)
	if errors.Is(err, linuxio.ErrFamily) {
		c.family, err = c.client.DiscoverFamily(ctx, "ethtool")
		if errors.Is(err, unix.ENOENT) || unsupportedEthtool(err) {
			return c.ioctl.read(ctx, job.Device.Name, job.Key.Device.Index, kind)
		}
		if err == nil {
			response, err = c.client.GetEthtool(ctx, c.family, kind, job.Key.Device.Index)
		}
	}
	if unsupportedEthtool(err) {
		return c.ioctl.read(ctx, job.Device.Name, job.Key.Device.Index, kind)
	}
	if err != nil {
		c.family = linuxio.Family{}
		return xtcpnl.EthtoolMessage{}, err
	}
	if len(response.Values) != 1 {
		return xtcpnl.EthtoolMessage{}, linuxio.ErrReply
	}
	return response.Values[0], nil
}

func (c *ethtoolIoctl) driver(ctx context.Context, name string, index uint32) ([]model.Sample, error) {
	if err := c.check(ctx, name, index); err != nil {
		return nil, err
	}
	d, err := unix.IoctlGetEthtoolDrvinfo(c.fd, name)
	if after := c.check(ctx, name, index); after != nil {
		return nil, after
	}
	if err != nil {
		return nil, err
	}
	labels := []model.Label{
		{Name: "driver", Value: unix.ByteSliceToString(d.Driver[:])},
		{Name: "version", Value: unix.ByteSliceToString(d.Version[:])},
		{Name: "firmware_version", Value: unix.ByteSliceToString(d.Fw_version[:])},
		{Name: "bus_info", Value: unix.ByteSliceToString(d.Bus_info[:])},
		{Name: "expansion_rom_version", Value: unix.ByteSliceToString(d.Erom_version[:])},
	}
	return []model.Sample{interfaceGauge("driver_info", 1, labels...), interfaceGauge("driver_known", boolValue(labels[0].Value != ""))}, nil
}
