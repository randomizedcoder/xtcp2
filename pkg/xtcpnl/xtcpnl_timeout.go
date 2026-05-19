package xtcpnl

import (
	"fmt"
	"syscall"
)

func SetSocketTimeoutViaSyscall(timeout int64, socketFileDescriptor int) (err error) {

	// Set socket timeout based on constants
	// doing this so that netlinkers can close on their own (or in the very unlikely event the kernel doesn't respond)
	if timeout != 0 {
		// https://godoc.org/golang.org/x/sys/unix#SetsockoptTimeval
		// timeout is in milliseconds. Decompose into seconds + leftover
		// microseconds so any value works — not just sub-second values
		// and exact multiples of 1000. The previous branch
		// (>=1000 → Sec=timeout/1000, Usec=0) dropped the sub-second
		// remainder: 1500ms set tv to 1s (losing 500ms), 2500ms set 2s,
		// etc. Now 1500ms → tv.Sec=1, tv.Usec=500000.
		var tv syscall.Timeval
		tv.Sec = timeout / 1000
		tv.Usec = (timeout % 1000) * 1000
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
