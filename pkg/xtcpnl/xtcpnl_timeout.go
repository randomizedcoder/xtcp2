package xtcpnl

import (
	"fmt"
	"log"
	"syscall"
)

func SetSocketTimeoutViaSyscall(timeout int64, socketFileDescriptor int) (err error) {

	// Set socket timeout based on constants
	// doing this so that netlinkers can close on their own (or in the very unlikely event the kernel doesn't respond)
	if timeout != 0 {
		// https://godoc.org/golang.org/x/sys/unix#SetsockoptTimeval
		var tv syscall.Timeval
		if timeout >= 1000 {
			// seconds
			tv.Sec = timeout / 1000
		} else {
			// milliseconds
			tv.Usec = timeout * 1000 // microsecond or 1 millionth of a second.  1 milliseconds = 1000 micro
		}
		if debugLevel > 100 {
			fmt.Println("OpenNetlinkSocketWithTimeout\ttv:", tv)
		}

		err = syscall.SetsockoptTimeval(socketFileDescriptor, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &tv)
		if err != nil {
			log.Fatalf("OpenNetlinkSocketWithTimeout SetsockopttimeSpec %s", err)
		}
	}

	return err
}
