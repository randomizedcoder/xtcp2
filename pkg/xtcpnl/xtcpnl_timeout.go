package xtcpnl

import (
	"fmt"
	"syscall"
)

// millisToTimeval decomposes a millisecond count into a syscall.Timeval
// (whole seconds + leftover microseconds). Extracted so the
// decomposition can be unit-tested without touching a socket — bug 66
// caught a previous version that dropped the sub-second remainder
// (>=1000 ms → tv.Sec=ms/1000, tv.Usec=0).
func millisToTimeval(timeoutMs int64) syscall.Timeval {
	return syscall.Timeval{
		Sec:  timeoutMs / 1000,
		Usec: (timeoutMs % 1000) * 1000,
	}
}

func SetSocketTimeoutViaSyscall(timeout int64, socketFileDescriptor int) (err error) {

	// Set socket timeout based on constants
	// doing this so that netlinkers can close on their own (or in the very unlikely event the kernel doesn't respond)
	if timeout != 0 {
		// https://godoc.org/golang.org/x/sys/unix#SetsockoptTimeval
		tv := millisToTimeval(timeout)
		if debugLevel > 100 {
			fmt.Println("OpenNetlinkSocketWithTimeout\ttv:", tv)
		}

		err = syscall.SetsockoptTimeval(socketFileDescriptor, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &tv)
		if err != nil {
			fatalf("OpenNetlinkSocketWithTimeout SetsockopttimeSpec %s", err)
			return err
		}
	}

	return err
}
