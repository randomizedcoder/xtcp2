package linkmonitor

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/linuxio"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

const (
	rdmaEthernetLayer = "Ethernet"
	rdmaNativeLayer   = "InfiniBand"
	duplexLabel       = "duplex"
	fullDuplexLabel   = "full"
)

type rdmaStateSource interface {
	Read(context.Context, model.RDMAPort) (model.RDMAPort, error)
	Close() error
}

type rdmaStateReader struct {
	client rdmaRequests
	files  rdmaFilesystem
}

func (s *rdmaStateReader) Close() error { return s.client.Close() }

func (s *rdmaStateReader) Read(ctx context.Context, p model.RDMAPort) (value model.RDMAPort, err error) {
	if err := ctx.Err(); err != nil {
		return p, err
	}
	if _, err := s.files.portPath(p.Device, p.Port); err != nil {
		return p, err
	}
	hardware, err := filepath.EvalSymlinks(filepath.Join(s.files.root, p.Device, "device"))
	if err != nil {
		return p, err
	}
	if hardware != p.Hardware {
		return p, linuxio.ErrEpoch
	}
	defer func() {
		after, readErr := filepath.EvalSymlinks(filepath.Join(s.files.root, p.Device, "device"))
		if readErr != nil {
			err = errors.Join(err, readErr)
		} else if hardware != after {
			err = errors.Join(err, linuxio.ErrEpoch)
		}
	}()
	if p.Sysfs {
		if !s.files.namespaceVerified {
			return p, linuxio.ErrEpoch
		}
		p.State, p.Physical, err = s.files.state(ctx, p)
		return p, err
	}
	result, err := s.client.GetRDMAPort(ctx, p.Index, p.Port)
	if unsupportedRDMA(err) && s.files.namespaceVerified {
		p.State, p.Physical, err = s.files.state(ctx, p)
		return p, err
	}
	if err != nil {
		return p, err
	}
	if len(result.Values) != 1 {
		return p, linuxio.ErrReply
	}
	v := result.Values[0]
	if v.Name != p.Device || v.Index != p.Index || v.Port != p.Port {
		return p, linuxio.ErrReply
	}
	if v.HasNetdev != p.Netdev.Present || (v.HasNetdev && (v.Netdev != p.Netdev.Value || v.NetdevName != p.NetdevName)) {
		return p, linuxio.ErrEpoch
	}
	p.State = model.Optional[uint8]{Value: v.State, Present: v.HasState}
	p.Physical = model.Optional[uint8]{Value: v.Physical, Present: v.HasPhysical}
	if !p.State.Present || !p.Physical.Present {
		p.State, p.Physical, err = s.files.state(ctx, p)
		if err != nil {
			return p, err
		}
	}
	return p, nil
}

type rdmaWork struct {
	ctx      context.Context
	job      model.Job
	ports    []model.RDMAPort
	revision uint64
}

type rdmaCompletion struct {
	work     rdmaWork
	ports    []model.RDMAPort
	finished model.Stamp
	err      error
}

type rdmaExecutor struct {
	ctx      context.Context
	cancel   context.CancelFunc
	inbox    chan rdmaWork
	results  chan rdmaCompletion
	done     chan struct{}
	closeErr error
}

func newRDMAExecutor(ctx context.Context, clock model.Clock, source rdmaStateSource) *rdmaExecutor {
	child, cancel := context.WithCancel(ctx)
	e := &rdmaExecutor{ctx: child, cancel: cancel, inbox: make(chan rdmaWork, 1), results: make(chan rdmaCompletion, 1), done: make(chan struct{})}
	go func() {
		defer func() { e.closeErr = source.Close(); close(e.done) }()
		for {
			select {
			case <-child.Done():
				return
			case work := <-e.inbox:
				result := rdmaCompletion{work: work}
				result.ports, result.err = readRDMAPorts(work, source)
				result.finished = clock.Now()
				e.results <- result
			}
		}
	}()
	return e
}

func readRDMAPorts(work rdmaWork, source rdmaStateSource) ([]model.RDMAPort, error) {
	ports := make([]model.RDMAPort, 0, len(work.ports))
	for i := range work.ports {
		if err := work.ctx.Err(); err != nil {
			return nil, err
		}
		value, err := source.Read(work.ctx, work.ports[i])
		if err != nil {
			return nil, err
		}
		ports = append(ports, value)
	}
	return ports, work.ctx.Err()
}

func rdmaReady(p model.RDMAPort, ethernetUp model.Optional[bool]) model.Check {
	if p.Layer == rdmaEthernetLayer {
		if !ethernetUp.Present {
			return model.CheckUnknown
		}
		if !ethernetUp.Value {
			return model.CheckNotApplicable
		}
		return roceReadiness(p.State)
	}
	if !p.Physical.Present || p.Physical.Value < 1 || p.Physical.Value > 7 {
		return model.CheckUnknown
	}
	if p.Physical.Value != 5 {
		return model.CheckNotApplicable
	}
	return roceReadiness(p.State)
}

func rdmaSamples(ports []model.RDMAPort, up model.Optional[bool]) ([]model.Sample, model.Check) {
	samples := make([]model.Sample, 0, len(ports)*10)
	check := model.CheckNotApplicable
	for index := range ports {
		p := &ports[index]
		ready := rdmaReady(*p, up)
		check = combineRDMAReady(check, ready)
		labels := []model.Label{{Name: "device", Value: p.Device}, {Name: "port", Value: strconv.FormatUint(uint64(p.Port), 10)}}
		add := func(name string, value uint64, extra ...model.Label) {
			owned := make([]model.Label, 0, len(labels)+len(extra))
			owned = append(owned, labels...)
			owned = append(owned, extra...)
			samples = append(samples, model.Sample{Descriptor: name, Kind: model.SampleGauge, Number: model.Unsigned(value), Labels: owned})
		}
		add("rdma_port_info", 1, model.Label{Name: "link_layer", Value: p.Layer})
		for _, alias := range p.Aliases {
			add("rdma_netdev_info", 1, model.Label{Name: "netdev", Value: alias})
		}
		if p.Layer == rdmaEthernetLayer {
			for _, version := range p.Versions {
				add("rdma_roce_version_info", 1, model.Label{Name: "version", Value: version})
			}
		}
		if value := nativeRDMAUp(p.State, p.Physical); value.Present {
			var n uint64
			if value.Value {
				n = 1
			}
			add("rdma_port_up", n)
		}
		for i, status := range [...]string{unknownSetting, "pass", "fail", "not_applicable"} {
			var n uint64
			if model.Check(i) == ready {
				n = 1
			}
			add("rdma_port_check", n, model.Label{Name: "check", Value: "ready"}, model.Label{Name: "status", Value: status})
		}
		if p.Layer == rdmaNativeLayer {
			samples = append(samples, model.Sample{Descriptor: "interface_duplex_info", Kind: model.SampleGauge, Number: model.Unsigned(1),
				Labels: []model.Label{{Name: duplexLabel, Value: fullDuplexLabel}, {Name: "source", Value: "transport"}}})
		}
	}
	return samples, check
}

func combineRDMAReady(a, b model.Check) model.Check {
	for _, value := range [...]model.Check{model.CheckFail, model.CheckUnknown, model.CheckPass} {
		if a == value || b == value {
			return value
		}
	}
	return model.CheckNotApplicable
}

func rdmaError(err error) model.ErrorReason {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return model.ErrorTimeout
	case errors.Is(err, linuxio.ErrLimit):
		return model.ErrorOversize
	case errors.Is(err, linuxio.ErrReply):
		return model.ErrorMalformed
	default:
		return hostReason(err)
	}
}
