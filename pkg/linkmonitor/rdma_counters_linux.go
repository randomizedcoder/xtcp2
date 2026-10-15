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
	"strings"
	"sync/atomic"
	"syscall"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/linuxio"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/nlconn"
)

func (c *rdmaOptionalCollector) counters(ctx context.Context, job model.Job) ([]model.Sample, *model.StatisticSchema, error) {
	schema := job.StatisticSchema
	if schema == nil {
		var err error
		schema, err = c.counterSchema(ctx, job.RDMA.Ports)
		if err != nil {
			return nil, nil, err
		}
		if c.split != nil {
			schema = rdmaPartition(schema, job, c.split)
		}
	}
	samples := make([]model.Sample, 0, min(maximumSamples, len(schema.Names)+len(job.RDMA.Ports)+len(job.RDMA.InfoDevices)))
	// kept is a pruned copy of schema once a file proves unavailable, so later
	// polls do not read it again; the shared schema itself is never modified.
	var kept *model.StatisticSchema
	offset := 0
	for index := range job.RDMA.Ports {
		lo := offset
		for offset < len(schema.Names) && schema.Selected[offset] == index {
			offset++
		}
		var err error
		samples, err = c.port(ctx, job, index, schema, lo, offset, samples, &kept)
		if err != nil {
			return nil, nil, err
		}
	}
	if kept != nil {
		return samples, kept, nil
	}
	return samples, schema, nil
}

// port reads one port's schema entries lo to hi through its held
// descriptors. Any failure closes them, so the next poll reopens under the
// identity guard.
func (c *rdmaOptionalCollector) port(ctx context.Context, job model.Job, index int, schema *model.StatisticSchema, lo, hi int, samples []model.Sample, kept **model.StatisticSchema) ([]model.Sample, error) {
	if err := ctx.Err(); err != nil {
		return samples, err
	}
	e, err := c.handles.acquire(job.RDMA.Ports[index], job.Key.Collector)
	if err == nil {
		samples, err = c.portSamples(ctx, job, index, e, schema, lo, hi, samples, kept)
	}
	if err != nil {
		err = errors.Join(err, e.closeAll())
	}
	return samples, errors.Join(err, e.release())
}

func (c *rdmaOptionalCollector) portSamples(ctx context.Context, job model.Job, index int, e *rdmaPortFiles, schema *model.StatisticSchema, lo, hi int, samples []model.Sample, kept **model.StatisticSchema) ([]model.Sample, error) {
	p := job.RDMA.Ports[index]
	owner := slices.Contains(job.RDMA.InfoDevices, rdmaSelector(p))
	// The error counters' job leaves rate and the info series to the rest.
	if err := e.remap(schema, lo, hi, owner, job.Key.Collector != model.CollectorRDMAErrors); err != nil {
		return samples, err
	}
	reads := c.slotReads(len(e.names))
	// The counters STAT_GET may replace are left closed until it fails.
	// A port found through sysfs has no netlink index; device index 0 is a
	// real one, the first device registered.
	netlink := c.stats != nil && !p.Sysfs && !e.netlinkOff && e.hw
	c.skipStatCounters(e, reads, netlink)
	if err := c.openSlots(ctx, p, e, reads, nil); err != nil {
		return samples, err
	}
	// STAT_GET runs while the pool reads the PortCounters files.
	defer c.pool.Wait()
	c.startPool(ctx, e, reads)
	if netlink {
		var err error
		if netlink, err = c.statCounters(ctx, p, e); err != nil {
			return samples, err
		}
		if !netlink {
			c.skipStatCounters(e, reads, false)
			if err := c.openSlots(ctx, p, e, reads, rdmaStatCounter); err != nil {
				return samples, err
			}
		}
	}
	c.readRest(ctx, e, reads)
	c.pool.Wait()
	if err := ctx.Err(); err != nil {
		return samples, err
	}
	for offset := lo; offset < hi; offset++ {
		if len(samples) == maximumSamples {
			return samples, linuxio.ErrLimit
		}
		name, r := schema.Names[offset], reads[offset-lo]
		if r.skip {
			value, ok := c.values[strings.TrimPrefix(name, rdmaHWGroup)]
			r = rdmaSlotRead{value: value, parsed: true}
			if !ok {
				// sysfs lists only counters STAT_GET reports; a name missing
				// from the reply is pruned like an unreadable file.
				r.err = syscall.EOPNOTSUPP
			}
		}
		if rdmaCounterUnavailable(r.err) {
			if *kept == nil {
				*kept = &model.StatisticSchema{Names: slices.Clone(schema.Names[:offset]), Selected: slices.Clone(schema.Selected[:offset]), Labels: slices.Clone(schema.Labels[:offset])}
			}
			continue
		}
		if r.err != nil {
			return samples, r.err
		}
		sample, ok, err := rdmaCounterValue(job, p, name, r.value, r.parsed, schema.Labels[offset])
		if err != nil {
			return samples, err
		}
		if ok {
			samples = append(samples, sample)
		}
		if k := *kept; k != nil {
			k.Names, k.Selected, k.Labels = append(k.Names, name), append(k.Selected, index), append(k.Labels, schema.Labels[offset])
		}
	}
	if !e.metadata {
		return samples, nil
	}
	metadata, err := rdmaPortMetadata(p, e.owner, reads[e.counters], reads[e.counters+1:])
	if err != nil {
		return samples, err
	}
	if len(samples)+len(metadata) > maximumSamples {
		return samples, linuxio.ErrLimit
	}
	return append(samples, metadata...), nil
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
					labels := rdmaLabels(p)
					if _, exists := rdmaCounterCatalog[name]; !exists {
						// Only hw_counters is vendor-defined; an unknown name in
						// the standard PortCounters groups stays dropped, and so
						// does an unknown entry that is not a file.
						if group != "hw_counters" || !entry.Type().IsRegular() {
							continue
						}
						labels = append(labels, model.Label{Name: "counter", Value: entry.Name()})
					}
					if !entry.Type().IsRegular() {
						return linuxio.ErrReply
					}
					if len(schema.Names) == maximumSamples {
						return linuxio.ErrLimit
					}
					schema.Names = append(schema.Names, name)
					schema.Selected = append(schema.Selected, index)
					schema.Labels = append(schema.Labels, labels)
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

// rdmaHWCounterDescriptor carries every hw_counters file outside the reviewed
// catalog, one series per file named by its counter label. Vendors mix
// counters and gauges there (bnxt's active_qps beside its error counts), so
// the value is exported untyped, as ethtool statistics are.
const rdmaHWCounterDescriptor = "go_link_monitor_rdma_hw_counter"

// rdmaCounterSample converts one counter file. ok is false for an uncataloged
// file whose text is not a decimal integer: an unknown vendor file must not
// fail the port's reviewed counters, so it is skipped instead.
func rdmaCounterSample(job model.Job, p model.RDMAPort, name, text string, labels []model.Label) (model.Sample, bool, error) {
	n, err := strconv.ParseUint(text, 10, 64)
	return rdmaCounterValue(job, p, name, n, err == nil, labels)
}

// rdmaCounterValue converts one counter value; parsed is false when the file
// was not a decimal integer.
func rdmaCounterValue(job model.Job, p model.RDMAPort, name string, n uint64, parsed bool, labels []model.Label) (model.Sample, bool, error) {
	field, cataloged := rdmaCounterCatalog[name]
	if !cataloged {
		if !parsed {
			return model.Sample{}, false, nil
		}
		return model.Sample{Descriptor: rdmaHWCounterDescriptor, Kind: model.SampleUntyped, Labels: labels, NoInterfaceLabel: true, Number: model.Unsigned(n)}, true, nil
	}
	if !parsed {
		return model.Sample{}, false, linuxio.ErrReply
	}
	sample := model.Sample{Descriptor: "go_link_monitor_infiniband_" + field.metric, Kind: model.SampleCounter, Labels: labels, NoInterfaceLabel: true}
	if field.scale == 0 {
		// Preserve the reviewed node_exporter integer-millisecond conversion.
		sample.Kind, sample.Number = model.SampleGauge, model.Unsigned(n/1000)
		return sample, true, nil
	}
	if n > math.MaxUint64/field.scale {
		return model.Sample{}, false, linuxio.ErrLimit
	}
	sample.Number = model.Unsigned(n * field.scale)
	sample.Counter = model.CounterIdentity{Source: p.Hardware + "/" + p.Device + "/" + strconv.FormatBool(p.Sysfs) + "/" + strconv.FormatUint(uint64(p.Index), 10) + "/" + strconv.FormatUint(uint64(p.Port), 10) + "/" + name, Lifetime: job.Token.Generation}
	return sample, true, nil
}

// rdmaReadParallelism bounds the PortCounters reads in flight per port. With
// four workers that is at most 16 firmware commands, inside the 31 slots of
// the mlx5 command queue.
const rdmaReadParallelism = 4

func (c *rdmaOptionalCollector) slotReads(n int) []rdmaSlotRead {
	c.reads = slices.Grow(c.reads[:0], n)[:n]
	clear(c.reads)
	return c.reads
}

func (c *rdmaOptionalCollector) buffer(i int) []byte {
	if len(c.bufs) <= i {
		c.bufs = append(c.bufs, make([][]byte, i+1-len(c.bufs))...)
	}
	if c.bufs[i] == nil {
		c.bufs[i] = make([]byte, rdmaScalarLimit+1)
	}
	return c.bufs[i]
}

func (c *rdmaOptionalCollector) skipStatCounters(e *rdmaPortFiles, reads []rdmaSlotRead, skip bool) {
	for i, name := range e.names[:e.counters] {
		if rdmaStatCounter(name) {
			reads[i].skip = skip
		}
	}
}

// openSlots opens the closed slots the poll needs among those only selects.
// With the pool running, only must exclude the PortCounters slots it owns.
func (c *rdmaOptionalCollector) openSlots(ctx context.Context, p model.RDMAPort, e *rdmaPortFiles, reads []rdmaSlotRead, only func(string) bool) error {
	if !e.needsOpen(reads, only) {
		return nil
	}
	// Identity is checked only when a descriptor is opened. A held one stays
	// bound to the kernfs node it opened: a device removed, replaced or
	// switched mode re-registers its sysfs tree, and the old node then fails
	// reads with ENODEV instead of showing another device's values. The RDMA
	// state collector resyncs on that error (rdma_schedule.go).
	return c.guard(ctx, p, func(path string) error {
		e.open(path, reads, only)
		return nil
	})
}

// startPool starts reading the PortCounters files on c.parallel readers;
// each is an uncached firmware command on mlx5. Fewer files than readers
// are left for readRest. Readers never share a buffer, and each result lands
// in its slot's own entry.
func (c *rdmaOptionalCollector) startPool(ctx context.Context, e *rdmaPortFiles, reads []rdmaSlotRead) {
	c.par, c.pooled = c.par[:0], false
	for i, name := range e.names[:e.counters] {
		if !reads[i].skip && reads[i].err == nil && rdmaPMACounter(name) {
			c.par = append(c.par, i)
		}
	}
	if c.parallel <= 1 || len(c.par) < c.parallel {
		return
	}
	c.pooled = true
	var next atomic.Int64
	for w := range c.parallel {
		buf := c.buffer(w + 1)
		c.pool.Go(func() {
			for k := int(next.Add(1)) - 1; k < len(c.par) && ctx.Err() == nil; k = int(next.Add(1)) - 1 {
				e.read(c.par[k], buf, &reads[c.par[k]])
			}
		})
	}
}

// readRest reads the other needed slots in order, hw_counters index 0
// first, then any PortCounters files the pool did not take.
func (c *rdmaOptionalCollector) readRest(ctx context.Context, e *rdmaPortFiles, reads []rdmaSlotRead) {
	c.seq = c.seq[:0]
	for i, name := range e.names {
		switch {
		// PortCounters slots belong to the pool; test them first.
		case i < e.counters && rdmaPMACounter(name), reads[i].skip, reads[i].err != nil:
		case i < e.counters && name == rdmaIndexZero:
			c.seq = slices.Insert(c.seq, 0, i)
		default:
			c.seq = append(c.seq, i)
		}
	}
	if !c.pooled {
		c.seq = append(c.seq, c.par...)
	}
	buf := c.buffer(0)
	for _, i := range c.seq {
		if ctx.Err() != nil {
			return
		}
		e.read(i, buf, &reads[i])
	}
}

// statCounters fills c.values from one STAT_GET for the port. It reports
// false when the port reads hw_counters from sysfs instead: after a failure
// that will not heal for this schema, or for this poll only after a
// transient one. A reply for another device, or a canceled poll, fails the
// set.
func (c *rdmaOptionalCollector) statCounters(ctx context.Context, p model.RDMAPort, e *rdmaPortFiles) (bool, error) {
	stat, err := c.stats.GetRDMAStat(ctx, p.Index, p.Port)
	if err == nil && (stat.Name != p.Device || stat.Index != p.Index || stat.Port != p.Port) {
		err = linuxio.ErrEpoch
	}
	if err != nil {
		if ctx.Err() != nil || errors.Is(err, linuxio.ErrEpoch) || errors.Is(err, syscall.ENODEV) {
			return false, err
		}
		if !rdmaStatTransient(err) {
			e.netlinkOff = true
		}
		return false, nil
	}
	clear(c.values)
	for _, counter := range stat.Counters {
		if _, duplicate := c.values[counter.Name]; duplicate {
			return false, linuxio.ErrReply
		}
		c.values[counter.Name] = counter.Value
	}
	return true, nil
}

// rdmaStatTransient reports a STAT_GET failure the next poll may not see.
func rdmaStatTransient(err error) bool {
	for _, transient := range [...]error{syscall.ETIMEDOUT, syscall.ENOBUFS, syscall.ENOMEM, syscall.EAGAIN, syscall.EINTR, syscall.EBUSY, context.DeadlineExceeded, nlconn.ErrConcurrent, linuxio.ErrInterrupted} {
		if errors.Is(err, transient) {
			return true
		}
	}
	return false
}
