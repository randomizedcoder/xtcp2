package linkmonitor

import (
	"context"
	"errors"
	"fmt"
	"syscall"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/linuxio"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

type trafficReader interface {
	readTraffic(context.Context, trafficRequest) trafficReply
}

type linkRequests interface {
	DumpLinks(context.Context) (linuxio.Result[xtcpnl.LinkInfo], error)
	GetLink(context.Context, int32) (linuxio.Result[xtcpnl.LinkInfo], error)
	Close() error
}

// trafficCollector decorates a worker's other collectors. Each worker owns its
// lazy route client, including while cancellation cannot interrupt a syscall.
type trafficCollector struct {
	workerCollector
	client    linkRequests
	namespace uint64
	sysfs     carrierFilesystem
}

func newTrafficCollector(base workerCollector, namespace uint64, sysfs string) (*trafficCollector, error) {
	client, err := linuxio.NewClient(unix.NETLINK_ROUTE)
	if err != nil {
		return nil, err
	}
	return &trafficCollector{workerCollector: base, client: client, namespace: namespace, sysfs: carrierFilesystem{root: sysfs}}, nil
}

func (c *trafficCollector) Close() error {
	return errors.Join(c.client.Close(), c.workerCollector.Close())
}

func (c *trafficCollector) readTraffic(ctx context.Context, work trafficRequest) trafficReply {
	if work.missing != 0 {
		values, failures := c.sysfs.read(ctx, work.name, work.index, work.missing)
		return trafficReply{fallback: values, failures: failures}
	}
	var response linuxio.Result[xtcpnl.LinkInfo]
	var err error
	if work.index == 0 {
		response, err = c.client.DumpLinks(ctx)
	} else {
		response, err = c.client.GetLink(ctx, int32(work.index))
	}
	if err != nil {
		return trafficReply{err: err}
	}
	if len(response.Values) > maxInventoryDevices {
		return trafficReply{err: linuxio.ErrLimit}
	}
	if work.index != 0 && len(response.Values) != 1 {
		return trafficReply{err: fmt.Errorf("%w: incomplete targeted link result", linuxio.ErrReply)}
	}
	return projectTraffic(c.namespace, response.Values, work.index)
}

func projectTraffic(namespace uint64, links []xtcpnl.LinkInfo, index uint32) trafficReply {
	if len(links) > maxInventoryDevices {
		return trafficReply{err: linuxio.ErrLimit}
	}
	reply := trafficReply{records: make(map[model.DeviceKey]*model.LinkStatistics, len(links))}
	for i := range links {
		if links[i].Stats == nil && links[i].StatsFields != 0 {
			return trafficReply{err: linuxio.ErrReply}
		}
		record := decodeTraffic(namespace, &links[i])
		if err := validTraffic(&record); err != nil {
			return trafficReply{err: errors.Join(linuxio.ErrReply, err)}
		}
		if index != 0 && record.Key.Index != index {
			return trafficReply{err: fmt.Errorf("%w: targeted link identity mismatch", linuxio.ErrReply)}
		}
		if _, exists := reply.records[record.Key]; exists {
			return trafficReply{err: fmt.Errorf("%w: duplicate traffic identity", linuxio.ErrReply)}
		}
		reply.records[record.Key] = &record
	}
	return reply
}

func trafficReason(err error) model.ErrorReason {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return model.ErrorTimeout
	case errors.Is(err, syscall.EACCES), errors.Is(err, syscall.EPERM):
		return model.ErrorPermission
	case errors.Is(err, linuxio.ErrLimit):
		return model.ErrorOversize
	case errors.Is(err, linuxio.ErrReply), errors.Is(err, errCarrierValue):
		return model.ErrorMalformed
	default:
		return model.ErrorIO
	}
}
