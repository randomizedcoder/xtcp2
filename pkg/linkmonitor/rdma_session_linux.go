package linkmonitor

import (
	"context"
	"errors"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/linuxio"
	"golang.org/x/sys/unix"
)

const defaultNetRoot = "/sys/class/net"

func (s *lifecycleSession) rdmaFiles() rdmaFilesystem {
	files := rdmaFilesystem{root: s.rdmaRoot, netRoot: s.sysfsRoot}
	if files.root == "" {
		files.root = "/sys/class/infiniband"
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
