//go:build linux && rdma && cgo

package rdmaevents

/*
#cgo pkg-config: libibverbs
#include <infiniband/verbs.h>
#include <errno.h>
#include <fcntl.h>
#include <stdlib.h>
#include <string.h>
#include "event_copy.h"

static struct ibv_context *monitor_open(const char *name, int *error) {
 int count = 0;
 struct ibv_device **devices = ibv_get_device_list(&count);
 if (!devices) { *error = errno ? errno : ENODEV; return NULL; }
 struct ibv_context *ctx = NULL;
 *error = ENODEV;
 if (count > 65536) { *error = EOVERFLOW; goto out; }
 for (int i = 0; i < count; i++) {
  if (strcmp(ibv_get_device_name(devices[i]), name)) continue;
  ctx = ibv_open_device(devices[i]);
  *error = ctx ? 0 : (errno ? errno : ENODEV);
  break;
 }
 if (ctx) {
  int flags = fcntl(ctx->async_fd, F_GETFL);
  if (flags == -1 || fcntl(ctx->async_fd, F_SETFL, flags | O_NONBLOCK) == -1 ||
      fcntl(ctx->async_fd, F_SETFD, FD_CLOEXEC) == -1) {
   *error = errno;
   ibv_close_device(ctx);
   ctx = NULL;
  }
 }
out:
 ibv_free_device_list(devices);
 return ctx;
}

// Only port events have a valid port_num member. Unknown object events must
// still be acknowledged, but their union member must not escape as a port.
static int monitor_next(struct ibv_context *ctx, int *kind, unsigned int *port) {
 struct ibv_async_event event;
 if (ibv_get_async_event(ctx, &event)) return errno ? errno : EIO;
 monitor_copy_ack(&event, kind, port, ibv_ack_async_event);
 return 0;
}
*/
import "C"

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

type verbsProvider struct{ root string }
type verbsHandle struct {
	context *C.struct_ibv_context
	name    string
}

// NewProvider uses the supplied sysfs class root to verify discovery identity.
func NewProvider(root string) Provider {
	if root == "" {
		root = "/sys/class/infiniband"
	}
	return verbsProvider{root: root}
}

func (p verbsProvider) verify(id Identity) error {
	if id.Name == "" || len(id.Name) > 63 || strings.ContainsAny(id.Name, "/:\x00") || id.Name == "." || id.Name == ".." || id.Hardware == "" {
		return ErrIdentity
	}
	hardware, err := filepath.EvalSymlinks(filepath.Join(p.root, id.Name, "device"))
	if err != nil {
		return err
	}
	if hardware != id.Hardware {
		return ErrIdentity
	}
	return nil
}

func (p verbsProvider) Open(ctx context.Context, id Identity) (Handle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := p.verify(id); err != nil {
		return nil, err
	}
	name := C.CString(id.Name)
	defer C.free(unsafe.Pointer(name))
	var code C.int
	device := C.monitor_open(name, &code)
	if device == nil {
		return nil, syscall.Errno(code)
	}
	h := &verbsHandle{context: device, name: id.Name}
	if err := errors.Join(ctx.Err(), p.verify(id)); err != nil {
		return nil, errors.Join(err, h.Close())
	}
	return h, nil
}

func (h *verbsHandle) FD() int { return int(h.context.async_fd) }

func (h *verbsHandle) Next() (Event, error) {
	var kind C.int
	var port C.uint
	if code := C.monitor_next(h.context, &kind, &port); code != 0 {
		return Event{}, syscall.Errno(code)
	}
	return Event{Device: h.name, Port: uint32(port), Kind: Kind(kind)}, nil
}

func (h *verbsHandle) Close() error {
	if h.context == nil {
		return nil
	}
	code := C.ibv_close_device(h.context)
	h.context = nil
	if code != 0 {
		return syscall.Errno(code)
	}
	return nil
}
