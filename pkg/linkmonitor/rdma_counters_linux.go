package linkmonitor

import (
	"context"
	"errors"
	"io/fs"
	"math"
	"path/filepath"
	"sort"
	"strconv"
	"syscall"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/linuxio"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

func (c *rdmaOptionalCollector) counters(ctx context.Context, job model.Job) ([]model.Sample, *model.StatisticSchema, error) {
	schema := job.StatisticSchema
	if schema == nil {
		var err error
		schema, err = c.counterSchema(ctx, job.RDMA.Ports)
		if err != nil {
			return nil, nil, err
		}
	}
	samples := make([]model.Sample, 0, min(maximumSamples, len(schema.Names)+len(job.RDMA.Ports)+len(job.RDMA.InfoDevices)))
	offset := 0
	for index := range job.RDMA.Ports {
		p := job.RDMA.Ports[index]
		err := c.guard(ctx, p, func(path string) error {
			for offset < len(schema.Names) && schema.Selected[offset] == index {
				if len(samples) == maximumSamples {
					return linuxio.ErrLimit
				}
				name := schema.Names[offset]
				text, err := rdmaScalar(ctx, filepath.Join(path, name))
				if err != nil {
					return err
				}
				sample, err := rdmaCounterSample(job, p, name, text, schema.Labels[offset])
				if err != nil {
					return err
				}
				samples = append(samples, sample)
				offset++
			}
			metadata, err := c.portMetadata(ctx, path, p, job.RDMA.InfoDevices)
			if err != nil {
				return err
			}
			if len(samples)+len(metadata) > maximumSamples {
				return linuxio.ErrLimit
			}
			samples = append(samples, metadata...)
			return nil
		})
		if err != nil {
			return nil, nil, err
		}
	}
	return samples, schema, nil
}

func (c *rdmaOptionalCollector) counterSchema(ctx context.Context, ports []model.RDMAPort) (*model.StatisticSchema, error) {
	schema := &model.StatisticSchema{}
	groups := 0
	for index := range ports {
		p := ports[index]
		err := c.guard(ctx, p, func(path string) error {
			for _, group := range [...]string{"counters", "counters_ext", "hw_counters"} {
				entries, err := rdmaEntries(ctx, filepath.Join(path, group))
				if errors.Is(err, fs.ErrNotExist) {
					continue
				}
				if err != nil {
					return err
				}
				groups++
				sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
				for _, entry := range entries {
					name := group + "/" + entry.Name()
					if _, exists := rdmaCounterCatalog[name]; !exists {
						continue
					}
					if !entry.Type().IsRegular() {
						return linuxio.ErrReply
					}
					if len(schema.Names) == maximumSamples {
						return linuxio.ErrLimit
					}
					schema.Names = append(schema.Names, name)
					schema.Selected = append(schema.Selected, index)
					schema.Labels = append(schema.Labels, rdmaLabels(p))
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	if groups == 0 {
		return nil, syscall.EOPNOTSUPP
	}
	return schema, nil
}

func rdmaCounterSample(job model.Job, p model.RDMAPort, name, text string, labels []model.Label) (model.Sample, error) {
	field := rdmaCounterCatalog[name]
	n, err := strconv.ParseUint(text, 10, 64)
	if err != nil {
		return model.Sample{}, linuxio.ErrReply
	}
	sample := model.Sample{Descriptor: "go_link_monitor_infiniband_" + field.metric, Kind: model.SampleCounter, Labels: labels, NoInterfaceLabel: true}
	if field.scale == 0 {
		// Preserve the reviewed node_exporter integer-millisecond conversion.
		sample.Kind, sample.Number = model.SampleGauge, model.Unsigned(n/1000)
		return sample, nil
	}
	if n > math.MaxUint64/field.scale {
		return model.Sample{}, linuxio.ErrLimit
	}
	sample.Number = model.Unsigned(n * field.scale)
	sample.Counter = model.CounterIdentity{Source: p.Hardware + "/" + p.Device + "/" + strconv.FormatBool(p.Sysfs) + "/" + strconv.FormatUint(uint64(p.Index), 10) + "/" + strconv.FormatUint(uint64(p.Port), 10) + "/" + name, Lifetime: job.Token.Generation}
	return sample, nil
}
