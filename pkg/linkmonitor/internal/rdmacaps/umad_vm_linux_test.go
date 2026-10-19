//go:build linux && rdma_vm

package rdmacaps

import (
	"context"
	"errors"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// TestUMADLive runs Query against a real kernel's umad device: the
// linkmonitor-rdma guest's rxe port, LINKMONITOR_UMAD_DEVICE port 1. The
// guest runs it twice: LINKMONITOR_UMAD_PHASE=unloaded after removing
// ib_umad, then loaded after loading it. rxe is RoCE, so the kernel refuses
// SMI registration; GSI registration shows the descriptor and agent path
// works.
func TestUMADLive(t *testing.T) {
	device, phase := os.Getenv("LINKMONITOR_UMAD_DEVICE"), os.Getenv("LINKMONITOR_UMAD_PHASE")
	if device == "" || phase == "" {
		t.Skip("LINKMONITOR_UMAD_DEVICE and LINKMONITOR_UMAD_PHASE name the guest's device and ib_umad state")
	}
	type row struct {
		name, category, description, expected string
		run                                   func(context.Context) error
		want                                  error
	}
	query := func(device string, port uint32) func(context.Context) error {
		return func(ctx context.Context) error {
			_, err := Query(ctx, device, port)
			return err
		}
	}
	var rows []row
	switch phase {
	case "unloaded":
		rows = []row{
			{"native_unloaded", "negative", "native query with ib_umad not loaded", "EOPNOTSUPP, and ib_umad still not loaded",
				query(device, 1), syscall.EOPNOTSUPP},
		}
	case "loaded":
		rows = []row{
			{"smi_native", "negative", "native PortInfo query on a RoCE port", "EPROTONOSUPPORT from SMI registration",
				query(device, 1), syscall.EPROTONOSUPPORT},
			{"smi_register", "negative", "register the SMI class 0x81 directly", "EPROTONOSUPPORT",
				register(device, 1, 0x81), syscall.EPROTONOSUPPORT},
			{"gsi_register", "positive", "register the GSI PerfMgt class 0x04", "registered, unregistered and closed",
				register(device, 1, 0x04), nil},
			{"absent_device", "negative", "native query of a device that does not exist", "ENODEV",
				query("lm_absent", 1), syscall.ENODEV},
			{"absent_port", "boundary", "native query of port 2 on a one-port device", "EINVAL from GET_CHARDEV",
				query(device, 2), syscall.EINVAL},
		}
	default:
		t.Fatalf("LINKMONITOR_UMAD_PHASE %q: want unloaded or loaded", phase)
	}
	before := openDescriptors(t)
	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			err := tc.run(ctx)
			t.Logf("result: %v", err)
			if (tc.want == nil) != (err == nil) || (tc.want != nil && !errors.Is(err, tc.want)) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
	if phase == "unloaded" {
		if _, err := os.Stat("/sys/module/ib_umad"); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("ib_umad loaded by a query: %v", err)
		}
	}
	if phase == "loaded" {
		t.Run("parallel_native", func(t *testing.T) {
			t.Log("positive: eight native queries of one port at once; expected every one EPROTONOSUPPORT, none waiting on another")
			var wg sync.WaitGroup
			errs := make([]error, 8)
			for i := range errs {
				wg.Go(func() {
					ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
					defer cancel()
					_, errs[i] = Query(ctx, device, 1)
				})
			}
			wg.Wait()
			for i, err := range errs {
				if !errors.Is(err, syscall.EPROTONOSUPPORT) {
					t.Fatalf("query %d: %v", i, err)
				}
			}
		})
	}
	if after := openDescriptors(t); after != before {
		t.Fatalf("descriptors %d before the rows, %d after", before, after)
	}
}

// register opens device port's umad node as exchangeUMAD does and registers
// one agent of class, then unregisters and closes, joining every error.
func register(device string, port uint32, class uint8) func(context.Context) error {
	return func(ctx context.Context) (err error) {
		ops := umadSys{}
		ch, err := ops.resolve(ctx, device, port)
		if err != nil {
			return err
		}
		fd, err := ops.open(&ch)
		if err != nil {
			return err
		}
		defer func() { err = errors.Join(err, ops.close(fd)) }()
		if err = ops.ioctl(fd, umadEnablePKey, nil); err != nil {
			return err
		}
		var reg [umadRegReq2Len]byte
		encodeRegisterAgent2(&reg, class, 1)
		if err = ops.ioctl(fd, umadRegisterAgent2, unsafe.Pointer(&reg)); err != nil {
			return err
		}
		agent := registeredAgent(&reg)
		return ops.ioctl(fd, umadUnregisterAgent, unsafe.Pointer(&agent))
	}
}

func openDescriptors(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}
