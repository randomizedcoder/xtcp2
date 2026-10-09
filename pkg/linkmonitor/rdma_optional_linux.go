package linkmonitor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/linuxio"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/rdmacaps"
)

type rdmaOptionalCollector struct {
	workerCollector
	files rdmaFilesystem
	query func(context.Context, string, uint32) (rdmacaps.Capabilities, error)
}

func newRDMAOptionalCollector(base workerCollector, files rdmaFilesystem) *rdmaOptionalCollector {
	return &rdmaOptionalCollector{workerCollector: base, files: files, query: rdmacaps.Query}
}

func (c *rdmaOptionalCollector) Collect(ctx context.Context, job model.Job) model.Result {
	if job.Key.Collector != model.CollectorRDMACapabilities && job.Key.Collector != model.CollectorRDMACounters {
		return c.workerCollector.Collect(ctx, job)
	}
	r := model.Result{Job: job, Support: model.NotApplicable}
	if job.RDMA == nil || len(job.RDMA.Ports) == 0 || job.Device.Eligibility != model.Eligible {
		return r
	}
	if job.Key.Collector == model.CollectorRDMACapabilities && job.Device.Key.Kind != model.DeviceNativeRDMA {
		return r
	}
	r.Support = model.Supported
	if job.Key.Collector == model.CollectorRDMACapabilities {
		r.Samples, r.Settings, r.Err = c.capabilities(ctx, job)
	} else {
		r.Samples, r.StatisticSchema, r.Err = c.counters(ctx, job)
	}
	if r.Err != nil {
		r.Samples, r.Settings, r.StatisticSchema = nil, nil, nil
		r.Support, r.Reason, r.InvalidateSchema = model.SupportUnknown, rdmaError(r.Err), true
		if rdmaOptionalUnsupported(r.Err) {
			r.Support, r.Err, r.Reason = model.Unsupported, nil, model.ErrorNone
		}
		if errors.Is(r.Err, rdmacaps.ErrReply) {
			r.Reason = model.ErrorMalformed
		}
	}
	return r
}

func rdmaOptionalUnsupported(err error) bool {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, child := range joined.Unwrap() {
			if !rdmaOptionalUnsupported(child) {
				return false
			}
		}
		return true
	}
	return errors.Is(err, rdmacaps.ErrUnavailable) || unsupportedRDMA(err)
}

// guard pins a read to the discovered hardware and the same port inode across
// the operation; it retains no descriptors across samples or replacements.
func (c *rdmaOptionalCollector) guard(ctx context.Context, p model.RDMAPort, read func(string) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := c.files.portPath(p.Device, p.Port)
	if err != nil {
		return err
	}
	before, err := os.Stat(path)
	if err != nil {
		return err
	}
	if err := c.identity(ctx, p); err != nil {
		return err
	}
	err = read(path)
	after, statErr := os.Stat(path)
	if statErr == nil && !os.SameFile(before, after) {
		statErr = linuxio.ErrEpoch
	}
	return errors.Join(err, statErr, c.identity(ctx, p), ctx.Err())
}

func (c *rdmaOptionalCollector) identity(ctx context.Context, p model.RDMAPort) error {
	hardware, err := filepath.EvalSymlinks(filepath.Join(c.files.root, p.Device, "device"))
	if err != nil {
		return err
	}
	if hardware != p.Hardware || hardware == "" {
		return linuxio.ErrEpoch
	}
	path, err := c.files.portPath(p.Device, p.Port)
	if err != nil {
		return err
	}
	layer, err := rdmaScalar(ctx, filepath.Join(path, "link_layer"))
	if err != nil {
		return err
	}
	if layer != p.Layer {
		return linuxio.ErrEpoch
	}
	return nil
}

func (c *rdmaOptionalCollector) capabilities(ctx context.Context, job model.Job) ([]model.Sample, *model.SettingsChecks, error) {
	if len(job.RDMA.Ports) != 1 || job.RDMA.Ports[0].Layer != rdmaNativeLayer {
		return nil, nil, syscall.EINVAL
	}
	p := job.RDMA.Ports[0]
	var value rdmacaps.Capabilities
	err := c.guard(ctx, p, func(string) error {
		var err error
		value, err = c.query(ctx, p.Device, p.Port)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	samples, checks := rdmaCapabilitySamples(job.Device.Up, p, value)
	return samples, checks, nil
}
