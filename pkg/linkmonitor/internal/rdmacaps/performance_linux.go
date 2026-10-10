//go:build linux && rdma && cgo && monitor_bench

package rdmacaps

/*
#include <stdlib.h>
#include <string.h>
// Simulate buffer ownership only; no provider, registration or device I/O.
static int bench_query(const char *name, unsigned char *request, unsigned char *reply) {
 if (strcmp(name, "mlx5_0")) return -1;
 void *(*volatile allocate)(size_t) = malloc;
 unsigned char *buffer = allocate(256);
 if (!buffer) return -1;
 memcpy(buffer, request, 256);
 memcpy(reply, buffer, 256);
 free(buffer);
 return 0;
}
*/
import "C"

import "unsafe"

func benchmarkExchange(request [256]byte) ([256]byte, bool) {
	umadLane <- struct{}{}
	defer func() { <-umadLane }()
	var reply [256]byte
	name := C.CString("mlx5_0")
	defer C.free(unsafe.Pointer(name))
	rc := C.bench_query(name, (*C.uchar)(unsafe.Pointer(&request[0])), (*C.uchar)(unsafe.Pointer(&reply[0])))
	return reply, rc == 0
}
