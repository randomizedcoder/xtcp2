//go:build linux && rdma && cgo

package rdmacaps

/*
#cgo pkg-config: libibumad
#include "query.h"
*/
import "C"

import (
	"context"
	"errors"
	"syscall"
	"time"
	"unsafe"
)

// rdma-core 63.0 libibumad keeps process-global ABI/buffer-layout state. Serialize
// this adapter's library access across monitor instances; waiting is cancellable.
var umadLane = make(chan struct{}, 1)

func exchangeLocal(ctx context.Context, device string, port uint32, request [256]byte) ([256]byte, error) {
	var reply [256]byte
	select {
	case umadLane <- struct{}{}:
		defer func() { <-umadLane }()
	case <-ctx.Done():
		return reply, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return reply, err
	}
	timeout := 250 * time.Millisecond
	if deadline, ok := ctx.Deadline(); ok {
		timeout = min(timeout, time.Until(deadline))
	}
	if timeout <= 0 {
		return reply, context.DeadlineExceeded
	}
	name := C.CString(device)
	defer C.free(unsafe.Pointer(name))
	var cleanup C.int
	rc := C.monitor_query(name, C.int(port), (*C.uchar)(unsafe.Pointer(&request[0])), (*C.uchar)(unsafe.Pointer(&reply[0])), C.int(max(1, timeout.Milliseconds())), &cleanup)
	var err error
	if rc != 0 {
		err = syscall.Errno(rc)
	}
	if cleanup != 0 {
		err = errors.Join(err, syscall.Errno(cleanup))
	}
	return reply, errors.Join(err, ctx.Err())
}
