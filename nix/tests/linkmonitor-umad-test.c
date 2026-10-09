/* Software-only resource-ownership checks for the exact production C helper. */
#include <assert.h>
#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <infiniband/umad.h>

enum failure { NONE, OPEN, FLAGS, REGISTER, ALLOC, SEND, RECEIVE, AGENT, LENGTH,
               STATUS, UNREGISTER, CLOSE };
static enum failure fail_at;
static int opened, closed, registered, unregistered, allocated, freed;

static int fake_open(const char *name, int port) {
    assert(strcmp(name, "hca") == 0 && port == 1);
    if (fail_at == OPEN) { errno = EACCES; return -EIO; }
    opened++; return 42;
}
static int fake_fd(int fd) { assert(fd == 42); return fd; }
static int fake_flags(int fd, int op, int flags) {
    assert(fd == 42 && op == F_SETFD && flags == FD_CLOEXEC);
    if (fail_at == FLAGS) { errno = EIO; return -1; }
    return 0;
}
static int fake_register(int fd, int cls, int version, uint8_t rmpp, long *mask) {
    assert(fd == 42 && cls == 0x81 && version == 1 && rmpp == 0 && mask == NULL);
    if (fail_at == REGISTER) { errno = EPERM; return -1; }
    registered++; return 7;
}
static size_t fake_size(void) { return 64; }
static void *fake_alloc(int count, size_t size) {
    assert(count == 1 && size == 320);
    if (fail_at == ALLOC) return NULL;
    allocated++; return calloc(1, size);
}
static void *fake_mad(void *buf) { return (unsigned char *)buf + 64; }
static int fake_addr(void *buf, int lid, int qp, int sl, int key) {
    assert(buf && lid == 0xffff && qp == 0 && sl == 0 && key == 0); return 0;
}
static int fake_send(int fd, int agent, void *buf, int size, int timeout, int retries) {
    assert(fd == 42 && agent == 7 && buf && size == 256 && timeout == 250 && retries == 0);
    if (fail_at == SEND) { errno = EIO; return -1; }
    return 0;
}
static int fake_recv(int fd, void *buf, int *size, int timeout) {
    assert(fd == 42 && buf && *size == 256 && timeout == 250);
    if (fail_at == RECEIVE) { errno = ETIMEDOUT; return -ETIMEDOUT; }
    if (fail_at == LENGTH) *size = 255;
    return fail_at == AGENT ? 8 : 7;
}
static int fake_status(void *buf) { assert(buf); return fail_at == STATUS ? EIO : 0; }
static void fake_free(void *buf) { assert(buf); freed++; free(buf); }
static int fake_unregister(int fd, int agent) {
    assert(fd == 42 && agent == 7); unregistered++;
    if (fail_at == UNREGISTER) { errno = EIO; return -1; }
    return 0;
}
static int fake_close(int fd) {
    assert(fd == 42); closed++;
    if (fail_at == CLOSE) { errno = EIO; return -1; }
    return 0;
}

#define umad_open_smi_port fake_open
#define umad_get_fd fake_fd
#define fcntl fake_flags
#define umad_register fake_register
#define umad_size fake_size
#define umad_alloc fake_alloc
#define umad_get_mad fake_mad
#define umad_set_addr fake_addr
#define umad_send fake_send
#define umad_recv fake_recv
#define umad_status fake_status
#define umad_free fake_free
#define umad_unregister fake_unregister
#define close fake_close
#include "query.h"

int main(void) {
    const struct {
        enum failure at;
        const char *category, *description, *expected;
        int result, cleanup;
    } cases[] = {
        {NONE, "positive", "successful exchange", "copy then free/unregister/close", 0, 0},
        {OPEN, "negative", "device permission denied", "preserve EACCES; no cleanup of unopened fd", EACCES, 0},
        {FLAGS, "negative", "CLOEXEC failed", "close fd", EIO, 0},
        {REGISTER, "negative", "registration denied", "close fd without unregister", EPERM, 0},
        {ALLOC, "boundary", "allocation fails", "unregister and close", ENOMEM, 0},
        {SEND, "negative", "send fails", "release every acquired resource once", EIO, 0},
        {RECEIVE, "boundary", "receive deadline", "bounded timeout and cleanup", ETIMEDOUT, 0},
        {AGENT, "negative", "wrong response agent", "reject and cleanup", EPROTO, 0},
        {LENGTH, "boundary", "255-byte response", "reject short reply", EPROTO, 0},
        {STATUS, "negative", "kernel transport status", "preserve error", EIO, 0},
        {UNREGISTER, "corner", "agent cleanup fails", "still close; retain cleanup error", 0, EIO},
        {CLOSE, "corner", "fd close fails", "retain cleanup error without retry", 0, EIO},
    };
    for (size_t i = 0; i < sizeof(cases)/sizeof(cases[0]); i++) {
        fail_at = cases[i].at;
        opened = closed = registered = unregistered = allocated = freed = 0;
        unsigned char req[256] = {1}, reply[256] = {0};
        int cleanup = 0;
        int result = monitor_query("hca", 1, req, reply, 250, &cleanup);
        printf("%s: %s; expected: %s\n", cases[i].category, cases[i].description, cases[i].expected);
        assert(result == cases[i].result && cleanup == cases[i].cleanup);
        assert(opened == closed && registered == unregistered && allocated == freed);
        if (!result && !cleanup) assert(memcmp(req, reply, 256) == 0);
    }
    return 0;
}
