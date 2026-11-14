//go:build goip_write

package goip

import (
	"os"
	"testing"

	"github.com/cilium/ebpf"
	"golang.org/x/sys/unix"
)

const (
	// bpfTestObjCst is nix/nlcap-lwt-bpf's lwt_bpf.o, as built (clang
	// -target bpf): pass_in, pass_out and pass_xmit in sections lwt_in,
	// lwt_out and lwt_xmit, and a GPL license.
	bpfTestObjCst = "testdata/bpf/lwt_bpf.o"
	// bpfRowObjCst is where the workbench and the parity guest link it.
	bpfRowObjCst = "/run/nlcap-ws/lwt_bpf.o"
	// bpfFakeFDCst is the descriptor the fake kernel load returns.
	bpfFakeFDCst = 7
)

// bpfFakeLoad is one program the fake kernel took: its name and type.
type bpfFakeLoad struct {
	name string
	typ  ebpf.ProgramType
}

// useFakeBpf opens bpfRowObjCst as the test object and any other path as
// itself, and replaces the kernel's load and BPF_OBJ_GET: a load records
// the one program in the spec and returns bpfFakeFDCst, and a pin in pins
// returns its fd, any other ENOENT. It returns the loads in order.
func useFakeBpf(t *testing.T, pins map[string]uint32) *[]bpfFakeLoad {
	t.Helper()
	prevOpen, prevLoad, prevGet := bpfOpenFile, bpfKernelLoad, bpfObjGet
	t.Cleanup(func() { bpfOpenFile, bpfKernelLoad, bpfObjGet = prevOpen, prevLoad, prevGet })
	var loads []bpfFakeLoad
	bpfOpenFile = func(name string) (bpfObjectFile, error) {
		if name == bpfRowObjCst {
			name = bpfTestObjCst
		}
		return os.Open(name)
	}
	bpfKernelLoad = func(cs *ebpf.CollectionSpec, name string) (uint32, error) {
		if len(cs.Programs) != 1 || cs.Programs[name] == nil {
			t.Errorf("load %q from a spec of %d programs", name, len(cs.Programs))
		}
		loads = append(loads, bpfFakeLoad{name: name, typ: cs.Programs[name].Type})
		return bpfFakeFDCst, nil
	}
	bpfObjGet = func(pin string) (uint32, error) {
		if fd, ok := pins[pin]; ok {
			return fd, nil
		}
		return 0, unix.ENOENT
	}
	return &loads
}
