package main

import (
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"time"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/rtnlmon"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// errCountReached ends Run once -count events were printed.
var errCountReached = errors.New("event count reached")

// printer is the probe's rtnlmon.Handler.
type printer struct {
	w      io.Writer
	ready  string
	pause  string
	count  int
	events int
	poll   time.Duration // the pause file's poll interval; zero is 50ms
}

func (p *printer) Snapshot(s rtnlmon.Snapshot, reason rtnlmon.Reason) error {
	if _, err := fmt.Fprintf(p.w, "RTNLMON_SNAPSHOT %s links=%d addrs=%d routes=%d neighs=%d\n",
		reason, len(s.Links), len(s.Addrs), len(s.Routes), len(s.Neighs)); err != nil {
		return err
	}
	for i := range s.Addrs {
		if _, err := fmt.Fprintf(p.w, "RTNLMON_SNAPSHOT_ADDR %s ifindex=%d addr=%s\n",
			reason, s.Addrs[i].Index, addrString(&s.Addrs[i])); err != nil {
			return err
		}
	}
	if reason == rtnlmon.ReasonStart && p.ready != "" {
		if err := os.WriteFile(p.ready, nil, 0o600); err != nil {
			return err
		}
	}
	p.waitWhilePaused()
	return nil
}

func (p *printer) Event(ev any) error {
	p.waitWhilePaused()
	if _, err := fmt.Fprintln(p.w, "RTNLMON_EVENT "+eventString(ev)); err != nil {
		return err
	}
	p.events++
	if p.count > 0 && p.events >= p.count {
		return errCountReached
	}
	return nil
}

// waitWhilePaused blocks while the pause file exists. Run calls the Handler
// on its receive goroutine, so blocking here stops the socket being read.
func (p *printer) waitWhilePaused() {
	if p.pause == "" {
		return
	}
	poll := p.poll
	if poll == 0 {
		poll = 50 * time.Millisecond
	}
	for {
		if _, err := os.Stat(p.pause); err != nil {
			return
		}
		time.Sleep(poll)
	}
}

func eventString(ev any) string {
	switch e := ev.(type) {
	case xtcpnl.LinkEvent:
		state := "down"
		if e.Link.Flags&unix.IFF_UP != 0 {
			state = "up"
		}
		return fmt.Sprintf("link %s ifindex=%d ifname=%s %s", e.Action, e.Link.Index, e.Link.Name, state)
	case xtcpnl.AddrEvent:
		return fmt.Sprintf("addr %s ifindex=%d addr=%s", e.Action, e.Addr.Index, addrString(&e.Addr))
	case xtcpnl.RouteEvent:
		return fmt.Sprintf("route %s table=%d dst=%s/%d oif=%d", e.Action, e.Route.Table, ipString(e.Route.Dst, e.Route.Family), e.Route.DstLen, e.Route.Oif)
	case xtcpnl.NeighEvent:
		return fmt.Sprintf("neigh %s ifindex=%d dst=%s", e.Action, e.Neigh.Ifindex, ipString(e.Neigh.Dst, e.Neigh.Family))
	default:
		return fmt.Sprintf("unknown %T", ev)
	}
}

// addrString is IFA_LOCAL when present, as `ip` prints it, else IFA_ADDRESS.
func addrString(a *xtcpnl.AddrInfo) string {
	raw := a.Local
	if len(raw) == 0 {
		raw = a.Address
	}
	return fmt.Sprintf("%s/%d", ipString(raw, a.Family), a.Prefixlen)
}

// ipString prints raw as an address; an absent destination (a default
// route) is the family's zero address.
func ipString(raw []byte, family uint8) string {
	if len(raw) == 0 {
		if family == unix.AF_INET6 {
			return "::"
		}
		return "0.0.0.0"
	}
	if ip, ok := netip.AddrFromSlice(raw); ok {
		return ip.String()
	}
	return fmt.Sprintf("%x", raw)
}
