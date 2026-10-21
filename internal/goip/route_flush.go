package goip

import (
	"fmt"
	"io"
	"os"

	"github.com/randomizedcoder/xtcp2/internal/goip/model"
	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// `ip route flush` is the list's argument loop and dump with a flush buffer
// (iproute_flush, ip/iproute.c:1741-1805). Each round dumps the routes,
// copies every reply filter_nlmsg keeps into an RTM_DELROUTE flagged
// NLM_F_REQUEST alone, sends the copies in as many 3584-byte datagrams as they
// need, and dumps again, until a dump matches nothing. Unlike `ip address
// flush` it has no round limit; it gives up after 30 seconds, which a dry run
// never reaches.
//
// goip sends the dumps and writes the copies to GOIP_WRITE_OUT, all rounds
// concatenated, as address flush does. Its round 2 cannot see a kernel that
// applied round 1, so it skips every route it has already copied, and it ends
// after the rounds `ip` runs when the kernel deletes what it was asked to.
// GOIP_WRITE_OUT is checked at the first copy, as restore checks it at the
// first record: a flush with nothing to delete sends what `ip` sends, the
// dump alone, and needs no hook.

const routeFlushVerbCst = "flush"

// routeFlushKey is a route as the kernel finds it to delete: by table,
// prefix, source, tos, metric, type and first hop. It is the identity round 2
// subtracts by. The bytes are not, because an IPv6 route's RTA_CACHEINFO
// moves between two dumps.
type routeFlushKey struct {
	family, dstLen, srcLen, tos, typ uint8
	table, priority, oif             uint32
	dst, src, gateway                string
}

func routeFlushKeyOf(r *model.Route) routeFlushKey {
	return routeFlushKey{family: r.Family, dstLen: r.DstLen, srcLen: r.SrcLen, tos: r.Tos, typ: r.Type,
		table: r.Table, priority: r.Priority, oif: r.Oif,
		dst: string(r.Dst), src: string(r.Src), gateway: string(r.Gateway)}
}

// routeFlushResult is what a flush would have sent: its write datagrams, in
// order, and how many dumps it sent.
type routeFlushResult struct {
	datagrams [][]byte
	dumps     int
}

// routeFlush runs `ip route flush` and writes its RTM_DELROUTE copies to
// GOIP_WRITE_OUT. Nothing copied, nothing written.
func routeFlush(c *runCtx, args []string) error {
	r, err := runRouteFlush(c, args)
	if err != nil {
		return err
	}
	if len(r.datagrams) > 0 {
		return writeAddrOut(joinDatagrams(r.datagrams))
	}
	return nil
}

// runRouteFlush is iproute_list_flush_or_save's flush arm and iproute_flush.
// The refusal of no arguments comes before the keyword loop
// (ip/iproute.c:1838-1841); the loop and the device lookup are the list's.
// -j changes nothing: no JSON object is opened.
func runRouteFlush(c *runCtx, args []string) (routeFlushResult, error) {
	var r routeFlushResult
	// iproute2 ends the line with a period; Go error strings do not.
	if len(args) == 0 {
		return r, fmt.Errorf("%w: \"ip route flush\" requires arguments", ErrInvalidArgument)
	}
	// print_route prints each route it copies under -s -s, without the
	// `table` token filter.tb hides; no capture shows it.
	if c.showStats > 1 {
		return r, fmt.Errorf("-s -s route flush: %w", ErrNotImplemented)
	}
	sel, err := parseRouteShowArgs(args)
	if err != nil {
		return r, err
	}
	svc := service.New(c.src, c.nextSeq)
	dumpFamily, oif, err := routeDumpScope(c, svc, sel)
	if err != nil {
		return r, err
	}

	copied := map[routeFlushKey]bool{}
	var buf flushBuf
	for round := 0; ; {
		bodies, derr := svc.RouteBodies(dumpFamily, sel.Table, oif)
		if derr != nil {
			return r, fmt.Errorf("goip: Flush terminated: %w", derr)
		}
		r.dumps++
		flushed, cerr := routeFlushRound(c, bodies, sel.Table, oif, copied, &buf)
		if cerr != nil {
			return r, cerr
		}
		if flushed == 0 {
			return r, routeFlushDone(c.out, c.stats(), round)
		}
		if round == 0 {
			if path, ok := os.LookupEnv(writeOutEnvCst); !ok || path == "" {
				return routeFlushResult{}, ErrWriteNotApplied
			}
		}
		round++
		buf.end()
		r.datagrams = buf.datagrams
		if c.stats() {
			if _, werr := fmt.Fprintf(c.out, "\n*** Round %d, deleting %d entries ***\n", round, flushed); werr != nil {
				return r, werr
			}
		}
	}
}

// routeFlushRound copies one dump's matching replies into buf and returns how
// many it copied. A route copied in an earlier round is skipped: `ip`'s kernel
// has deleted it, and goip's has not.
func routeFlushRound(c *runCtx, bodies [][]byte, table, oif uint32, copied map[routeFlushKey]bool,
	buf *flushBuf) (int, error) {
	routes := make([]model.Route, len(bodies))
	for i, body := range bodies {
		ri, err := xtcpnl.ParseNewRoute(body)
		if err != nil {
			return 0, fmt.Errorf("goip: decode RTM_NEWROUTE: %w", err)
		}
		routes[i] = model.Route(ri)
	}
	keep := routeKeep(routes, c.family, table, oif)
	seen := map[routeFlushKey]bool{}
	n := 0
	for i, body := range bodies {
		k := routeFlushKeyOf(&routes[i])
		if !keep[i] || copied[k] {
			continue
		}
		// The copy rule (ip/iproute.c:803-818): the reply whole, as
		// RTM_DELROUTE, flagged NLM_F_REQUEST alone, at the next seq.
		msg, err := xtcpnl.BuildWriteRequest(unix.RTM_DELROUTE, 0, c.nextSeq(),
			body[:rtmsgLenCst], body[rtmsgLenCst:])
		if err != nil {
			return 0, fmt.Errorf("goip: build route flush: %w", err)
		}
		buf.add(msg)
		seen[k] = true
		n++
	}
	for k := range seen {
		copied[k] = true
	}
	return n, nil
}

// routeFlushDone is iproute_flush's end: under -s, the closing line. The
// routing cache, filter.cloned's, is not one goip flushes, so a first-round
// end is always "Nothing to flush.".
func routeFlushDone(w io.Writer, stats bool, round int) error {
	if !stats {
		return nil
	}
	var err error
	switch round {
	case 0:
		_, err = fmt.Fprint(w, "Nothing to flush.\n")
	case 1:
		_, err = fmt.Fprint(w, "*** Flush is complete after 1 round ***\n")
	default:
		_, err = fmt.Fprintf(w, "*** Flush is complete after %d rounds ***\n", round)
	}
	return err
}
