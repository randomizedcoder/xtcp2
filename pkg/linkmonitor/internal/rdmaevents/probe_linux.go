//go:build linux && rdma && cgo

package rdmaevents

/*
#cgo LDFLAGS: -ldl
#include <infiniband/verbs.h>
#include <dlfcn.h>
#include <stdlib.h>
#include <errno.h>
#include "event_copy.h"

static _Thread_local int monitor_test_acks;
static void monitor_test_ack(struct ibv_async_event *event) {
 monitor_test_acks++;
 event->element.port_num = -1;
}
static int monitor_test_copy(int type, int value, int *kind, unsigned int *port) {
 struct ibv_async_event event = {0};
 event.event_type = type;
 event.element.port_num = value;
 monitor_test_acks = 0;
 monitor_copy_ack(&event, kind, port, monitor_test_ack);
 return monitor_test_acks;
}

static int monitor_device_count(void) {
 int count = 0;
 struct ibv_device **list = ibv_get_device_list(&count);
 if (!list) return -(errno ? errno : EIO);
 ibv_free_device_list(list);
 return count;
}
static int monitor_load(const char *path) {
 void *h = dlopen(path, RTLD_NOW | RTLD_LOCAL);
 if (!h) return -1;
 return dlclose(h);
}
*/
import "C"

import (
	"fmt"
	"syscall"
	"unsafe"
)

// runtimeProbe only enumerates devices; it never opens hardware contexts.
func runtimeProbe() (int, error) {
	n := int(C.monitor_device_count())
	if n < 0 {
		return 0, syscall.Errno(-n)
	}
	return n, nil
}

func loadProvider(path string) error {
	name := C.CString(path)
	defer C.free(unsafe.Pointer(name))
	if C.monitor_load(name) != 0 {
		return fmt.Errorf("cannot load RDMA provider %s", path)
	}
	return nil
}

func copyProbe(eventType, value int) (Kind, uint32, int) {
	var kind C.int
	var port C.uint
	acks := C.monitor_test_copy(C.int(eventType), C.int(value), &kind, &port)
	return Kind(kind), uint32(port), int(acks)
}
