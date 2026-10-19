package linkmonitor

import (
	"context"
	"errors"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/linuxio"
	"golang.org/x/sys/unix"
)

const (
	defaultNetRoot  = "/sys/class/net"
	defaultRDMARoot = "/sys/class/infiniband"
)

func (s *lifecycleSession) rdmaFiles() rdmaFilesystem {
	files := rdmaFilesystem{root: s.rdmaRoot, netRoot: s.sysfsRoot}
	if files.root == "" {
		files.root = defaultRDMARoot
	}
	if files.netRoot == "" {
		files.netRoot = defaultNetRoot
	}
	return files
}

func (s *lifecycleSession) wrapRDMAInventory(ethernet inventoryBackend) (inventoryBackend, error) {
	client, err := linuxio.NewClient(unix.NETLINK_RDMA)
	if err != nil {
		return nil, errors.Join(err, ethernet.Close())
	}
	return &rdmaInventory{ethernet: ethernet, client: client, files: s.rdmaFiles(), clock: s.clock, namespace: s.namespace}, nil
}

func (s *lifecycleSession) openRDMAState(ctx context.Context) (rdmaStateSource, error) {
	if s.rdmaSource != nil {
		return s.rdmaSource(ctx)
	}
	client, err := linuxio.NewClient(unix.NETLINK_RDMA)
	if err != nil {
		return nil, err
	}
	return &rdmaStateReader{client: client, files: s.rdmaFiles()}, nil
}

// rdmaCollector shares the session's counter descriptors with every worker
// and gives each its own netlink client, which allows one request at a time.
// STAT_GET answers for the host's devices, so a fixture root reads sysfs only.
func (s *lifecycleSession) rdmaCollector(base workerCollector, handles *rdmaHandles) (workerCollector, error) {
	files := s.rdmaFiles()
	var stats rdmaStatSource
	if files.root == defaultRDMARoot {
		client, err := linuxio.NewClient(unix.NETLINK_RDMA)
		if err != nil {
			return nil, errors.Join(err, base.Close())
		}
		stats = client
	}
	c := newSharedRDMACollector(base, files, handles.retain(), stats)
	if s.statisticsConfig.onDemand() {
		problems := s.statisticsConfig.problems
		c.split = &problems
	}
	return c, nil
}
