// Command rtnlmon-probe runs pkg/rtnlmon's listener against the running
// kernel and prints what it is handed, one line each, for the microVM
// self-test's RTNL_EVENTS check:
//
//	RTNLMON_SNAPSHOT <start|overrun|truncated|periodic> links=N addrs=N routes=N neighs=N
//	RTNLMON_SNAPSHOT_ADDR <reason> ifindex=N addr=A/P   (one per address)
//	RTNLMON_EVENT link <add|del> ifindex=N ifname=X <up|down>
//	RTNLMON_EVENT addr <add|del> ifindex=N addr=A/P
//	RTNLMON_EVENT route <add|del> table=T dst=A/P oif=N
//	RTNLMON_EVENT neigh <add|del> ifindex=N dst=A
//
// -ready names a file it creates once it is subscribed and has the first
// snapshot. While the -pause file exists it stops reading the socket, so the
// check can overflow its receive buffer (-rcvbuf) and see the resync.
//
// Exit 0 after -count events; 1 on a timeout, a listener error or bad flags.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/rtnlmon"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// groupNames maps -groups words to groups; addr and route are both families.
var groupNames = map[string][]rtnlmon.Group{
	"link":   {rtnlmon.GroupLink},
	"addr":   {rtnlmon.GroupIPv4Addr, rtnlmon.GroupIPv6Addr},
	"addr4":  {rtnlmon.GroupIPv4Addr},
	"addr6":  {rtnlmon.GroupIPv6Addr},
	"route":  {rtnlmon.GroupIPv4Route, rtnlmon.GroupIPv6Route},
	"route4": {rtnlmon.GroupIPv4Route},
	"route6": {rtnlmon.GroupIPv6Route},
	"neigh":  {rtnlmon.GroupNeigh},
}

func parseGroups(s string) ([]rtnlmon.Group, error) {
	var out []rtnlmon.Group
	for _, w := range strings.Split(s, ",") {
		gs, ok := groupNames[strings.TrimSpace(w)]
		if !ok {
			return nil, fmt.Errorf("unknown group %q (want link, addr, addr4, addr6, route, route4, route6, neigh)", w)
		}
		out = append(out, gs...)
	}
	return out, nil
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("rtnlmon-probe", flag.ContinueOnError)
	fs.SetOutput(stderr)
	groups := fs.String("groups", "link,addr", "comma-separated groups: link, addr, addr4, addr6, route, route4, route6, neigh")
	rcvBuf := fs.Int("rcvbuf", 0, "receive buffer size in bytes; 0 keeps the kernel default")
	ready := fs.String("ready", "", "file to create once subscribed and snapshotted")
	pause := fs.String("pause", "", "while this file exists, stop reading the socket")
	count := fs.Int("count", 0, "exit 0 after this many events; 0 runs until -timeout")
	timeout := fs.Duration("timeout", 30*time.Second, "give up (exit 1) after this long")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	gs, err := parseGroups(*groups)
	if err != nil {
		fmt.Fprintf(stderr, "rtnlmon-probe: %v\n", err)
		fs.Usage()
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	p := &printer{w: stdout, ready: *ready, pause: *pause, count: *count}
	err = rtnlmon.Run(ctx, rtnlmon.Options{Groups: gs, RcvBuf: *rcvBuf}, p)
	switch {
	case errors.Is(err, errCountReached):
		return 0
	case errors.Is(err, context.DeadlineExceeded):
		fmt.Fprintf(stderr, "rtnlmon-probe: timed out after %s with %d events\n", *timeout, p.events)
		return 1
	default:
		fmt.Fprintf(stderr, "rtnlmon-probe: %v\n", err)
		return 1
	}
}
