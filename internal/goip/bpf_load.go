//go:build goip_write

package goip

import (
	"debug/elf"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/cilium/ebpf"
	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"golang.org/x/sys/unix"
)

// The loader's three outside steps, which tests replace: opening the object
// file, loading a program into the kernel, and BPF_OBJ_GET on a pin.
var (
	bpfOpenFile   = func(name string) (bpfObjectFile, error) { return os.Open(name) }
	bpfKernelLoad = bpfKernelLoadProg
	bpfObjGet     = bpfObjGetPinned
)

// bpfObjectFile is the object file as the loader reads it.
type bpfObjectFile interface {
	io.ReaderAt
	io.Closer
}

// bpfLoadProg is bpf_do_load (lib/bpf_legacy.c:946) with libbpf, as
// nixpkgs builds iproute2, for an object, and bpf_obj_pinned for a pin.
func bpfLoadProg(cfg bpfLoadCfg) (uint32, error) {
	if cfg.pinned {
		if len(cfg.object) > 2 && strings.HasPrefix(cfg.object, "m:") {
			// bpf_obj_get's work directory, which mounts bpffs.
			return 0, fmt.Errorf("bpf pinned m:%s: %w", cfg.object[2:], ErrNotImplemented)
		}
		fd, err := bpfObjGet(cfg.object)
		if err != nil {
			// bpf_obj_pinned returns bpf()'s -1, so strerror(1).
			return 0, bpfFailed(fmt.Sprintf("Couldn't retrieve pinned program '%s': %s\n",
				cfg.object, cStrerror(errnoOf(err, unix.ENOENT))), unix.EPERM)
		}
		return fd, nil
	}
	cs, name, err := bpfObjectSpec(cfg)
	if err != nil {
		return 0, err
	}
	fd, err := bpfKernelLoad(cs, name)
	if err != nil {
		return 0, bpfFailed("", errnoOf(err, unix.EINVAL))
	}
	return fd, nil
}

// bpfObjectSpec is iproute2_load_libbpf's checks before the kernel load:
// bpf_elf_ctx_init's open and ELF header checks, libbpf's open, and
// find_prog_to_attach, by `program` name or else by section. It returns a
// spec holding that program alone, as the lwt type, and its name.
func bpfObjectSpec(cfg bpfLoadCfg) (*ebpf.CollectionSpec, string, error) {
	f, err := bpfOpenFile(cfg.object)
	if err != nil {
		// bpf_elf_ctx_init returns open's -1, so strerror(1).
		return nil, "", bpfFailed(fmt.Sprintf("Error opening object %s: %s\nCannot initialize ELF context!\n",
			cfg.object, cStrerror(errnoOf(err, unix.ENOENT))), unix.EPERM)
	}
	spec, name, err := bpfReadSpec(f, cfg)
	if cerr := f.Close(); cerr != nil && err == nil {
		return nil, "", cerr
	}
	return spec, name, err
}

// bpfReadSpec is bpfObjectSpec's work on the open object.
func bpfReadSpec(f io.ReaderAt, cfg bpfLoadCfg) (*ebpf.CollectionSpec, string, error) {
	if err := bpfCheckELF(f); err != nil {
		return nil, "", err
	}
	spec, err := ebpf.LoadCollectionSpecFromReader(f)
	if err != nil {
		return nil, "", bpfFailed("ERROR: opening BPF object file failed\n", unix.ENOENT)
	}
	name, err := bpfFindProg(spec, cfg)
	if err != nil {
		return nil, "", err
	}
	for _, m := range spec.Maps {
		if m.Pinning != ebpf.PinNone {
			// handle_legacy_maps and libbpf's pin_root_path.
			return nil, "", fmt.Errorf("bpf object %s: a pinned map: %w", cfg.object, ErrNotImplemented)
		}
	}
	out := spec.Copy()
	ps := out.Programs[name]
	ps.Type = cfg.progType
	out.Programs = map[string]*ebpf.ProgramSpec{name: ps}
	return out, name, nil
}

// bpfCheckELF is bpf_elf_ctx_init's libelf checks: an ELF file (EINVAL
// otherwise), then bpf_elf_check_ehdr's relocatable eBPF object in this
// host's byte order.
func bpfCheckELF(f io.ReaderAt) error {
	const ctxFail = "Cannot initialize ELF context!\n"
	ef, err := elf.NewFile(f)
	if err != nil {
		return bpfFailed(ctxFail, unix.EINVAL)
	}
	if ef.Type != elf.ET_REL || (ef.Machine != elf.EM_NONE && ef.Machine != elf.EM_BPF) ||
		ef.Version != elf.EV_CURRENT {
		return bpfFailed("ELF format error, ELF file not for eBPF?\n"+ctxFail, unix.EINVAL)
	}
	little := binary.NativeEndian.Uint16([]byte{1, 0}) == 1
	switch {
	case ef.Data == elf.ELFDATA2LSB && !little:
		return bpfFailed("We are big endian, eBPF object is little endian!\n"+ctxFail, unix.EIO)
	case ef.Data == elf.ELFDATA2MSB && little:
		return bpfFailed("We are little endian, eBPF object is big endian!\n"+ctxFail, unix.EIO)
	case ef.Data != elf.ELFDATA2LSB && ef.Data != elf.ELFDATA2MSB:
		return bpfFailed("ELF format error, wrong endianness info?\n"+ctxFail, unix.EINVAL)
	}
	return nil
}

// bpfFindProg is find_prog_to_attach: the program named `program`, else
// the one in the section. libbpf takes the first in the object; goip has
// no order for two in one section, and refuses them.
func bpfFindProg(spec *ebpf.CollectionSpec, cfg bpfLoadCfg) (string, error) {
	if cfg.progName != "" {
		if _, ok := spec.Programs[cfg.progName]; !ok {
			return "", bpfFailed(fmt.Sprintf("object file doesn't contain prog %s\n", cfg.progName), unix.ENOENT)
		}
		return cfg.progName, nil
	}
	var found []string
	for name, ps := range spec.Programs {
		if ps.SectionName == cfg.section {
			found = append(found, name)
		}
	}
	switch len(found) {
	case 0:
		return "", bpfFailed(fmt.Sprintf("object file doesn't contain sec %s\n", cfg.section), unix.ENOENT)
	case 1:
		return found[0], nil
	}
	return "", fmt.Errorf("bpf section %s: %d programs: %w", cfg.section, len(found), ErrNotImplemented)
}

// errnoOf is err's errno, or def when it carries none.
func errnoOf(err error, def unix.Errno) unix.Errno {
	var e unix.Errno
	if errors.As(err, &e) {
		return e
	}
	return def
}

// bpfKernelLoadProg loads cs and keeps a duplicate of name's descriptor,
// as load_bpf_object's F_DUPFD_CLOEXEC does before it closes the object.
func bpfKernelLoadProg(cs *ebpf.CollectionSpec, name string) (uint32, error) {
	coll, err := ebpf.NewCollection(cs)
	if err != nil {
		return 0, err
	}
	defer coll.Close()
	return bpfDupFD(coll.Programs[name].FD())
}

// bpfObjGetPinned is bpf_obj_get's BPF_OBJ_GET on a pinned program.
func bpfObjGetPinned(pin string) (uint32, error) {
	p, err := ebpf.LoadPinnedProgram(pin, nil)
	if err != nil {
		return 0, err
	}
	fd, err := bpfDupFD(p.FD())
	if cerr := p.Close(); cerr != nil && err == nil {
		return 0, cerr
	}
	return fd, err
}

func bpfDupFD(fd int) (uint32, error) {
	d, err := unix.FcntlInt(uintptr(fd), unix.F_DUPFD_CLOEXEC, 1)
	if err != nil {
		return 0, err
	}
	u, ok := cint.U32(d)
	if !ok {
		return 0, unix.EBADF
	}
	return u, nil
}
