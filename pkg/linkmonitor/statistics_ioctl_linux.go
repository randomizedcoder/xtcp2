package linkmonitor

import (
	"context"
	"encoding/binary"
	"errors"
	"regexp"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/linuxio"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"golang.org/x/sys/unix"
)

const driverStatisticSet = 1 // ETH_SS_STATS
const phyStatisticSet = 7    // ETH_SS_PHY_STATS

var errStatisticShape = errors.New("ethtool statistic schema changed")

type statisticsIoctl struct {
	ioctl            *ethtoolIoctl
	buffer           statisticBuffer
	include, exclude *regexp.Regexp
}

func (c *statisticsIoctl) Close() error { return errors.Join(c.buffer.Close(), c.ioctl.Close()) }

func (c *statisticsIoctl) invoke(ctx context.Context, name string, data []byte, command uint32) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	binary.NativeEndian.PutUint32(data, command)
	if err := errors.Join(c.ioctl.invoke(name, data), ctx.Err()); err != nil {
		return err
	}
	if binary.NativeEndian.Uint32(data) != command {
		return linuxio.ErrReply
	}
	return nil
}

func (c *statisticsIoctl) count(ctx context.Context, name string, kind model.CollectorKind) (int, error) {
	data := make([]byte, 196) // struct ethtool_drvinfo
	command, offset := uint32(unix.ETHTOOL_GDRVINFO), 180
	if kind == model.CollectorPHY {
		data, command, offset = data[:20], unix.ETHTOOL_GSSET_INFO, 16
		binary.NativeEndian.PutUint64(data[8:], uint64(1)<<phyStatisticSet)
	}
	if err := c.invoke(ctx, name, data, command); err != nil {
		return 0, err
	}
	if kind == model.CollectorPHY {
		mask := binary.NativeEndian.Uint64(data[8:])
		if mask == 0 {
			return 0, unix.EOPNOTSUPP
		}
		if mask != uint64(1)<<phyStatisticSet || binary.NativeEndian.Uint32(data[4:]) != 0 {
			return 0, linuxio.ErrReply
		}
	}
	count := binary.NativeEndian.Uint32(data[offset:])
	if count > maximumSamples {
		return 0, linuxio.ErrReply
	}
	return int(count), nil
}

func (c *statisticsIoctl) names(ctx context.Context, job model.Job, count int) (*model.StatisticSchema, error) {
	if count == 0 {
		return decodeStatisticSchema(nil, 0, c.include, c.exclude)
	}
	data, err := c.buffer.bytes(12 + count*statisticNameBytes)
	if err != nil {
		return nil, err
	}
	set := uint32(driverStatisticSet)
	if job.Key.Collector == model.CollectorPHY {
		set = phyStatisticSet
	}
	binary.NativeEndian.PutUint32(data[4:], set)
	binary.NativeEndian.PutUint32(data[8:], uint32(count))
	if err := c.invoke(ctx, job.Device.Name, data, unix.ETHTOOL_GSTRINGS); err != nil {
		return nil, err
	}
	if binary.NativeEndian.Uint32(data[4:]) != set {
		return nil, linuxio.ErrReply
	}
	if binary.NativeEndian.Uint32(data[8:]) != uint32(count) {
		return nil, errStatisticShape
	}
	return decodeStatisticSchema(data[12:], count, c.include, c.exclude)
}

func (c *statisticsIoctl) values(ctx context.Context, job model.Job, schema *model.StatisticSchema) ([]model.Sample, error) {
	count := len(schema.Names)
	if count == 0 {
		return nil, nil
	}
	data, err := c.buffer.bytes(8 + count*8)
	if err != nil {
		return nil, err
	}
	command, descriptor := uint32(unix.ETHTOOL_GSTATS), "ethtool_statistic"
	if job.Key.Collector == model.CollectorPHY {
		command, descriptor = unix.ETHTOOL_GPHYSTATS, "phy_statistic"
	}
	binary.NativeEndian.PutUint32(data[4:], uint32(count))
	if err := c.invoke(ctx, job.Device.Name, data, command); err != nil {
		return nil, err
	}
	if binary.NativeEndian.Uint32(data[4:]) != uint32(count) {
		return nil, errStatisticShape
	}
	samples := make([]model.Sample, len(schema.Selected))
	for i, index := range schema.Selected {
		samples[i] = model.Sample{Descriptor: descriptor, Kind: model.SampleUntyped, Labels: schema.Labels[i],
			Number: model.Unsigned(binary.NativeEndian.Uint64(data[8+index*8:]))}
	}
	return samples, nil
}

func (c *statisticsIoctl) read(ctx context.Context, job model.Job) ([]model.Sample, *model.StatisticSchema, error) {
	if err := c.ioctl.check(ctx, job.Device.Name, job.Key.Device.Index); err != nil {
		return nil, nil, err
	}
	schema := job.StatisticSchema
	for range 2 {
		samples, next, err := c.attempt(ctx, job, schema)
		if after := c.ioctl.check(ctx, job.Device.Name, job.Key.Device.Index); after != nil {
			return nil, nil, after
		}
		if !errors.Is(err, errStatisticShape) {
			return samples, next, err
		}
		schema = nil
	}
	return nil, nil, errors.Join(linuxio.ErrReply, errStatisticShape)
}

func (c *statisticsIoctl) attempt(ctx context.Context, job model.Job, schema *model.StatisticSchema) ([]model.Sample, *model.StatisticSchema, error) {
	count, err := c.count(ctx, job.Device.Name, job.Key.Collector)
	if err != nil {
		return nil, nil, err
	}
	if schema == nil || len(schema.Names) != count {
		schema, err = c.names(ctx, job, count)
		if err != nil {
			return nil, nil, err
		}
	}
	samples, err := c.values(ctx, job, schema)
	return samples, schema, err
}
