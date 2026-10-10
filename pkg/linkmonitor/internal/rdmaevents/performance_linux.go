//go:build linux && rdma && cgo && monitor_bench

package rdmaevents

/*
#include "event_copy.h"
static _Thread_local unsigned int bench_acks;
static void bench_ack(struct ibv_async_event *event) {
 bench_acks++;
 event->element.port_num = -1;
}
static unsigned int bench_batch(unsigned int count, unsigned int *ports) {
 bench_acks = 0;
 for (unsigned int i = 0; i < count; i++) {
  struct ibv_async_event event = {0};
  event.event_type = IBV_EVENT_PORT_ACTIVE;
  event.element.port_num = i + 1;
  int kind;
  monitor_copy_ack(&event, &kind, &ports[i], bench_ack);
 }
 return bench_acks;
}
*/
import "C"

import "unsafe"

// benchmarkBatch exercises real cgo and copy/ack with a simulated provider.
func benchmarkBatch(ports []uint32) int {
	if len(ports) == 0 || len(ports) > 64 {
		return 0
	}
	return int(C.bench_batch(C.uint(len(ports)), (*C.uint)(unsafe.Pointer(&ports[0]))))
}
