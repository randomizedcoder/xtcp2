package linkmonitor

import (
	"context"
	"errors"
	"io/fs"
	"math"
	"path/filepath"
	"slices"
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
	// kept is a pruned copy of schema once a file proves unavailable, so later
	// polls do not read it again; the shared schema itself is never modified.
	var kept *model.StatisticSchema
	offset := 0
	for index := range job.RDMA.Ports {
		p := job.RDMA.Ports[index]
		err := c.guard(ctx, p, func(path string) error {
			for ; offset < len(schema.Names) && schema.Selected[offset] == index; offset++ {
				if len(samples) == maximumSamples {
					return linuxio.ErrLimit
				}
				name := schema.Names[offset]
				text, err := c.read(ctx, filepath.Join(path, name))
				if rdmaCounterUnavailable(err) {
					if kept == nil {
						kept = &model.StatisticSchema{Names: slices.Clone(schema.Names[:offset]), Selected: slices.Clone(schema.Selected[:offset]), Labels: slices.Clone(schema.Labels[:offset])}
					}
					continue
				}
				if err != nil {
					return err
				}
				sample, err := rdmaCounterSample(job, p, name, text, schema.Labels[offset])
				if err != nil {
					return err
				}
				samples = append(samples, sample)
				if kept != nil {
					kept.Names, kept.Selected, kept.Labels = append(kept.Names, name), append(kept.Selected, index), append(kept.Labels, schema.Labels[offset])
				}
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
	if kept != nil {
		return samples, kept, nil
	}
	return samples, schema, nil
}

// rdmaCounterUnavailable reports a counter file the kernel lists but cannot
// read on this port. mlx5 answers EINVAL for the PortCounters error fields on
// some ConnectX-7 InfiniBand ports (firmware ACCESS_REG 0x805 "bad
// operation"); node_exporter's procfs skips the same errors. Other errors,
// including a vanished file or a permission failure, still fail the set.
func rdmaCounterUnavailable(err error) bool {
	return errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.EOPNOTSUPP)
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
