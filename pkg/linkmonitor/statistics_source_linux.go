package linkmonitor

import (
	"context"
	"errors"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

type statisticsCollector struct {
	workerCollector
	source *statisticsIoctl
}

func newStatisticsCollector(base workerCollector, cfg configuration) (*statisticsCollector, error) {
	return openStatisticsCollector(base, cfg, newEthtoolIoctl)
}

func openStatisticsCollector(base workerCollector, cfg configuration, open func() (*ethtoolIoctl, error)) (*statisticsCollector, error) {
	ioctl, err := open()
	if err != nil {
		return nil, err
	}
	return &statisticsCollector{workerCollector: base, source: &statisticsIoctl{ioctl: ioctl, include: cfg.include, exclude: cfg.exclude}}, nil
}

func (c *statisticsCollector) Close() error {
	return errors.Join(c.source.Close(), c.workerCollector.Close())
}

func (c *statisticsCollector) Collect(ctx context.Context, job model.Job) model.Result {
	if !isStatisticCollector(job.Key.Collector) {
		return c.workerCollector.Collect(ctx, job)
	}
	result := model.Result{Job: job, Support: model.NotApplicable}
	if job.Device.Key.Kind == model.DeviceEthernet && job.Device.Eligibility == model.EligibilityUnknown {
		result.Support = model.SupportUnknown
	}
	if job.Device.Key.Kind != model.DeviceEthernet || job.Device.Eligibility != model.Eligible {
		return result
	}
	samples, schema, err := c.source.read(ctx, job)
	if err == nil {
		result.Support, result.Samples, result.StatisticSchema = model.Supported, samples, schema
		result.Filtered = presentValue(uint64(len(schema.Names) - len(schema.Selected)))
		return result
	}
	result.InvalidateSchema = true
	if unsupportedEthtool(err) {
		result.Support = model.Unsupported
	} else {
		result.Support, result.Err, result.Reason = model.SupportUnknown, err, trafficReason(err)
	}
	return result
}

func isStatisticCollector(kind model.CollectorKind) bool {
	return kind == model.CollectorDriver || kind == model.CollectorPHY
}
