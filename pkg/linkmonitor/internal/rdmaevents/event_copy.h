#ifndef LINKMONITOR_EVENT_COPY_H
#define LINKMONITOR_EVENT_COPY_H
#include <infiniband/verbs.h>

static int monitor_kind(int type) {
 switch (type) {
 case IBV_EVENT_PORT_ACTIVE: case IBV_EVENT_PORT_ERR: return 0;
 case IBV_EVENT_LID_CHANGE: case IBV_EVENT_PKEY_CHANGE:
 case IBV_EVENT_SM_CHANGE: case IBV_EVENT_CLIENT_REREGISTER:
 case IBV_EVENT_GID_CHANGE: return 1;
 case IBV_EVENT_DEVICE_FATAL: return 2;
 default: return 3;
 }
}

static void monitor_copy_ack(struct ibv_async_event *event, int *kind,
 unsigned int *port, void (*ack)(struct ibv_async_event *)) {
 *kind = monitor_kind(event->event_type);
 *port = (*kind == 0 || *kind == 1) && event->element.port_num > 0
  ? (unsigned int)event->element.port_num : 0;
 ack(event);
}
#endif
