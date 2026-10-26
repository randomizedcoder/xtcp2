package agent

import (
	"context"
	"log"

	"golang.org/x/sys/unix"
)

func (a *Agent) watchRouting(ctx context.Context) {
	fd, e := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, unix.NETLINK_ROUTE)
	if e != nil {
		log.Printf("treatment route watch unavailable: %v; periodic reconciliation remains enabled", e)
		return
	}
	defer func() {
		if cerr := unix.Close(fd); cerr != nil {
			log.Printf("treatment route watch close: %v", cerr)
		}
	}()
	groups := uint32(unix.RTMGRP_LINK | unix.RTMGRP_IPV4_IFADDR | unix.RTMGRP_IPV6_IFADDR | unix.RTMGRP_IPV4_ROUTE | unix.RTMGRP_IPV6_ROUTE | unix.RTMGRP_IPV4_RULE)
	// RTNLGRP_IPV6_RULE is a group number; sockaddr_nl expects its bitmask.
	groups |= 1 << (unix.RTNLGRP_IPV6_RULE - 1)
	if e = unix.Bind(fd, &unix.SockaddrNetlink{Family: unix.AF_NETLINK, Groups: groups}); e != nil {
		log.Printf("treatment route watch bind: %v", e)
		return
	}
	buf := make([]byte, 64<<10)
	for ctx.Err() == nil {
		p := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		_, e := unix.Poll(p, 1000)
		if e == unix.EINTR {
			continue
		}
		if e != nil {
			log.Printf("treatment route watch: %v", e)
			return
		}
		if p[0].Revents == 0 {
			continue
		}
		_, from, e := unix.Recvfrom(fd, buf, 0)
		if e == unix.EAGAIN {
			continue
		}
		if e != nil {
			a.Wake()
			continue
		} // ENOBUFS requires a complete resnapshot.
		if peer, ok := from.(*unix.SockaddrNetlink); ok && peer.Pid == 0 {
			a.Wake()
		}
	}
}
