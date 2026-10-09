package linkmonitor

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"strconv"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/linuxio"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"golang.org/x/sys/unix"
)

type rdmaNetdev struct {
	index, lower   uint32
	linkType, port uint64
	hardware       string
}

type rdmaAliasKey struct {
	hardware string
	port     uint64
}
type rdmaGraph struct {
	names   map[string]rdmaNetdev
	indices map[uint32]rdmaNetdev
	lowers  map[uint32]model.NetdevLink
	native  map[rdmaAliasKey][]string
}

func newRDMAGraph(names map[string]rdmaNetdev, links []model.NetdevLink) rdmaGraph {
	g := rdmaGraph{names: names, indices: make(map[uint32]rdmaNetdev), lowers: make(map[uint32]model.NetdevLink), native: make(map[rdmaAliasKey][]string)}
	for _, link := range links {
		g.lowers[link.Index] = link
	}
	for name, n := range names {
		g.indices[n.index] = n
		if n.linkType == unix.ARPHRD_INFINIBAND && n.hardware != "" {
			key := rdmaAliasKey{n.hardware, n.port + 1}
			g.native[key] = append(g.native[key], name)
		}
	}
	return g
}

func (s rdmaFilesystem) netdevices(ctx context.Context) (map[string]rdmaNetdev, error) {
	entries, err := rdmaEntries(ctx, s.netRoot)
	if err != nil {
		return nil, err
	}
	result := make(map[string]rdmaNetdev, len(entries))
	indices := make(map[uint32]bool)
	for _, entry := range entries {
		if !rdmaName(entry.Name(), 15) {
			return nil, linuxio.ErrReply
		}
		path := filepath.Join(s.netRoot, entry.Name())
		var numbers [4]uint64
		for i, field := range [...]string{"ifindex", "iflink", "type", "dev_port"} {
			text, err := rdmaScalar(ctx, filepath.Join(path, field))
			if i == 3 && errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, err
			}
			numbers[i], err = strconv.ParseUint(text, 10, 32)
			if err != nil {
				return nil, linuxio.ErrReply
			}
		}
		if numbers[0] == 0 || numbers[0] > 0x7fffffff || indices[uint32(numbers[0])] {
			return nil, linuxio.ErrReply
		}
		indices[uint32(numbers[0])] = true
		hardware, err := filepath.EvalSymlinks(filepath.Join(path, "device"))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		if len(hardware) > maxHardwareIdentity {
			return nil, linuxio.ErrLimit
		}
		result[entry.Name()] = rdmaNetdev{index: uint32(numbers[0]), lower: uint32(numbers[1]), linkType: numbers[2], port: numbers[3], hardware: hardware}
	}
	return result, nil
}

func resolveRDMALower(name string, graph rdmaGraph, devices map[uint32]model.Device) rdmaAssociation {
	seen := make(map[uint32]bool)
	n, exists := graph.names[name]
	for exists {
		if seen[n.index] {
			return rdmaAssociation{}
		}
		seen[n.index] = true
		d, known := devices[n.index]
		if !known {
			return rdmaAssociation{}
		}
		if d.Eligibility != model.Excluded {
			return rdmaAssociation{key: d.Key, eligibility: d.Eligibility}
		}
		if n.lower == 0 || n.lower == n.index {
			return rdmaAssociation{eligibility: model.Excluded}
		}
		link, verified := graph.lowers[n.index]
		if !verified || link.Foreign || link.Lower != n.lower {
			return rdmaAssociation{}
		}
		n, exists = graph.indices[n.lower]
	}
	return rdmaAssociation{}
}
