#include <errno.h>
#include <fcntl.h>
#include <unistd.h>
#include <stdlib.h>
#include <string.h>
#include <infiniband/umad.h>

/* All resources belong to this synchronous worker call. No SET, remote path,
 * retry, redirect or global library setting is exposed. */
static int monitor_query(char *name, int port, unsigned char *request,
                         unsigned char *reply, int timeout, int *cleanup)
{
    errno = 0;
    int fd = umad_open_smi_port(name, port);
    if (fd < 0) return (fd == -EIO && errno) ? errno : -fd;
    int error = 0;
    if (fcntl(umad_get_fd(fd), F_SETFD, FD_CLOEXEC) < 0) {
        error = errno ? errno : EIO; goto close;
    }
    int agent = umad_register(fd, 0x81, 1, 0, NULL);
    void *buffer = NULL;
    if (agent < 0) { error = errno ? errno : EIO; goto close; }
    buffer = umad_alloc(1, umad_size() + 256);
    if (!buffer) { error = ENOMEM; goto unregister; }
    memcpy(umad_get_mad(buffer), request, 256);
    umad_set_addr(buffer, 0xffff, 0, 0, 0);
    if (umad_send(fd, agent, buffer, 256, timeout, 0) < 0) {
        error = errno ? errno : EIO; goto release;
    }
    int length = 256;
    int received = umad_recv(fd, buffer, &length, timeout);
    if (received < 0) { error = errno ? errno : EIO; goto release; }
    if (received != agent || length != 256) { error = EPROTO; goto release; }
    error = umad_status(buffer);
    if (!error) memcpy(reply, umad_get_mad(buffer), 256);
release:
    umad_free(buffer);
unregister:
    if (umad_unregister(fd, agent) < 0) *cleanup = errno ? errno : EIO;
close:
    /* umad_close_port discards close errors in rdma-core 63.0. Its port ID is
     * the fd; close directly so cleanup failures remain visible. */
    if (close(umad_get_fd(fd)) < 0 && !*cleanup) *cleanup = errno ? errno : EIO;
    return error;
}
