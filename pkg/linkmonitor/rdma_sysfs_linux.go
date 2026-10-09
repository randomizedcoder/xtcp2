package linkmonitor

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/linuxio"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

const rdmaScalarLimit = 4096

type rdmaFilesystem struct {
	root, netRoot string
	// Only an embedding that has established mount/network namespace agreement
	// may authorize discovery fallback without netlink enumeration.
	namespaceVerified bool
}

func rdmaScalar(ctx context.Context, path string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	b, readErr := io.ReadAll(io.LimitReader(f, rdmaScalarLimit+1))
	err = errors.Join(readErr, f.Close(), ctx.Err())
	if err != nil {
		return "", err
	}
	if len(b) > rdmaScalarLimit {
		return "", linuxio.ErrLimit
	}
	return strings.TrimSpace(string(b)), nil
}

func rdmaEntries(ctx context.Context, path string) ([]os.DirEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	entries, readErr := f.ReadDir(maxInventoryDevices + 1)
	if errors.Is(readErr, io.EOF) {
		readErr = nil
	}
	err = errors.Join(readErr, f.Close(), ctx.Err())
	if err != nil {
		return nil, err
	}
	if len(entries) > maxInventoryDevices {
		return nil, linuxio.ErrLimit
	}
	return entries, nil
}

func rdmaName(name string, limit int) bool {
	return validName(name, limit) && name != "." && name != ".." && !strings.ContainsAny(name, "/:\x00")
}

func (s rdmaFilesystem) portPath(device string, port uint32) (string, error) {
	if !rdmaName(device, 63) || port == 0 {
		return "", linuxio.ErrRequest
	}
	path := filepath.Join(s.root, device, "ports", strconv.FormatUint(uint64(port), 10))
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	// Class symlinks normally resolve into /sys/devices. Fixture trees have the
	// same class hierarchy; never follow a class entry outside that sysfs root.
	base := filepath.Dir(filepath.Dir(s.root))
	rel, err := filepath.Rel(base, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", linuxio.ErrReply
	}
	return path, nil
}

func parseRDMAState(text string) (model.Optional[uint8], error) {
	number, _, _ := strings.Cut(text, ":")
	n, err := strconv.ParseUint(strings.TrimSpace(number), 10, 8)
	if err != nil {
		return model.Optional[uint8]{}, linuxio.ErrReply
	}
	return presentValue(uint8(n)), nil
}

func (s rdmaFilesystem) state(ctx context.Context, p model.RDMAPort) (model.Optional[uint8], model.Optional[uint8], error) {
	path, err := s.portPath(p.Device, p.Port)
	if err != nil {
		return model.Optional[uint8]{}, model.Optional[uint8]{}, err
	}
	var values [2]model.Optional[uint8]
	for i, name := range [...]string{"state", "phys_state"} {
		text, readErr := rdmaScalar(ctx, filepath.Join(path, name))
		if readErr != nil {
			return values[0], values[1], readErr
		}
		values[i], err = parseRDMAState(text)
		if err != nil {
			return values[0], values[1], err
		}
	}
	return values[0], values[1], nil
}

func (s rdmaFilesystem) metadata(ctx context.Context, p *model.RDMAPort) error {
	path, err := s.portPath(p.Device, p.Port)
	if err != nil {
		return err
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	if strings.Contains(resolved, "/devices/virtual/") {
		p.Eligibility = model.Excluded
	}
	p.Layer, err = rdmaScalar(ctx, filepath.Join(path, "link_layer"))
	if err != nil {
		return err
	}
	hardware, err := filepath.EvalSymlinks(filepath.Join(s.root, p.Device, "device"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if len(hardware) > maxHardwareIdentity {
		return linuxio.ErrLimit
	}
	if hardware != "" {
		rel, err := filepath.Rel(filepath.Dir(filepath.Dir(s.root)), hardware)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return linuxio.ErrReply
		}
	}
	if strings.Contains(hardware, "/devices/virtual/") {
		hardware = ""
	}
	p.Hardware = hardware
	return s.gids(ctx, path, p)
}

func (s rdmaFilesystem) gids(ctx context.Context, path string, p *model.RDMAPort) error {
	entries, err := rdmaEntries(ctx, filepath.Join(path, "gid_attrs", "types"))
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) {
		p.Versions = []string{unknownSetting}
		return nil
	}
	if err != nil {
		return err
	}
	versions, aliases := make(map[string]bool), make(map[string]bool)
	p.Records = len(entries)
	for _, entry := range entries {
		if _, err := strconv.ParseUint(entry.Name(), 10, 32); err != nil {
			return linuxio.ErrReply
		}
		value, err := rdmaScalar(ctx, filepath.Join(path, "gid_attrs", "types", entry.Name()))
		if err != nil {
			versions[unknownSetting] = true
		} else {
			switch value {
			case "IB/RoCE v1":
				versions["v1"] = true
			case "RoCE v2":
				versions["v2"] = true
			default:
				versions[unknownSetting] = true
			}
		}
		name, err := rdmaScalar(ctx, filepath.Join(path, "gid_attrs", "ndevs", entry.Name()))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if name == "" {
			continue
		}
		if !rdmaName(name, 15) {
			return linuxio.ErrReply
		}
		aliases[name] = true
	}
	for _, version := range [...]string{"v1", "v2", unknownSetting} {
		if versions[version] {
			p.Versions = append(p.Versions, version)
		}
	}
	if len(p.Versions) == 0 {
		p.Versions = []string{unknownSetting}
	}
	for name := range aliases {
		p.Aliases = append(p.Aliases, name)
	}
	return nil
}
