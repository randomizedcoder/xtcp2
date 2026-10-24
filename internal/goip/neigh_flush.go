package goip

import (
	"errors"
	"fmt"
	"os"

	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// `ip neigh flush` is do_show_or_flush's flush arm (ip/ipneigh.c:506-650):
// the listing's argument loop and link dump, then rounds of a neighbor dump
// whose kept replies print_neigh copies into RTM_DELNEIGH, flagged
// NLM_F_REQUEST alone, each at the next seq, sent in as many 3584-byte
// datagrams as they need, until a dump matches nothing. A dump that fails is
// "Flush terminated", exit 1.
//
// goip sends the link dump and the neighbor dumps and writes the copies to
// GOIP_WRITE_OUT, as route flush does: its round 2 skips every entry it has
// already copied, and GOIP_WRITE_OUT is checked at the first copy, so a flush
// with nothing to delete sends what `ip` sends and needs no hook.

// ErrNeighFlushTerminated is a neighbor dump failing inside `ip neigh flush`:
// `ip` prints "Flush terminated" and exits 1 (ip/ipneigh.c:621-623).
var ErrNeighFlushTerminated = errors.New("flush terminated")

// neighFlushDropStatesCst is the states flush leaves alone: filter.state is
// the int ~(NUD_PERMANENT|NUD_NOARP) (ip/ipneigh.c:521), every other state
// bit and 0x100 too, the bit print_neigh reads as keeping NUD_NONE (:335-338).
const neighFlushDropStatesCst = uint16(unix.NUD_PERMANENT | unix.NUD_NOARP)

// neighFlushKey is a neighbor entry as the kernel finds it to delete: by
// family, device, proxy table and destination. The bytes are not, because
// NDA_CACHEINFO's ages move between two dumps.
type neighFlushKey struct {
	family  uint8
	proxy   bool
	ifindex int32
	dst     string
}

// neighFlushResult is what a flush would have sent: its write datagrams, in
// order, and how many neighbor dumps it sent.
type neighFlushResult struct {
	datagrams [][]byte
	dumps     int
}

// neighFlush runs `ip neigh flush` and writes its RTM_DELNEIGH copies to
// GOIP_WRITE_OUT. Nothing copied, nothing written.
func neighFlush(c *runCtx, args []string) error {
	r, err := runNeighFlush(c, args)
	if err != nil {
		return err
	}
	if len(r.datagrams) > 0 {
		return writeAddrOut(joinDatagrams(r.datagrams))
	}
	return nil
}

// runNeighFlush is the flush arm. The refusal of no arguments comes before
// the loop (:516-519); the loop is the listing's, as goip implements it:
// `dev` and `proxy`. ll_init_map runs after it, and `dev` is resolved out of
// its cache, as neighShow resolves it. -j changes nothing.
func runNeighFlush(c *runCtx, args []string) (neighFlushResult, error) {
	var r neighFlushResult
	// iproute2 ends the line with a period; Go error strings do not.
	if len(args) == 0 {
		return r, fmt.Errorf("%w: Flush requires arguments", ErrInvalidArgument)
	}
	// print_neigh prints each entry it copies under -s -s; no capture shows
	// it.
	if c.showStats > 1 {
		return r, fmt.Errorf("-s -s neigh flush: %w", ErrNotImplemented)
	}
	sel, err := parseNeighShowArgs(routeFlushVerbCst, args)
	if err != nil {
		return r, err
	}
	// flush's filter.state starts at ~(NUD_PERMANENT|NUD_NOARP) (:516-519),
	// not show's mask, and neighFlushKeeps is that default alone; no capture
	// shows a flush narrowed by nud.
	if sel.StateGiven {
		return r, fmt.Errorf("neigh flush %s: %w", nudKeywordCst, ErrNotImplemented)
	}

	svc := service.New(c.src, c.nextSeq)
	links, err := svc.NeighborLinks()
	if err != nil {
		return r, fmt.Errorf("%w: Dump terminated: %w", ErrDumpTerminated, err)
	}
	decoded := make([]xtcpnl.LinkInfo, len(links))
	for i := range links {
		decoded[i] = xtcpnl.LinkInfo(links[i])
	}
	c.lltab.Fill(decoded)
	var index int32
	if sel.DevSet {
		if index = c.lltab.NameToIndex(sel.Dev); index == 0 {
			return r, noSuchDevice(nodevErr(sel.Dev))
		}
	}

	copied := map[neighFlushKey]bool{}
	var buf flushBuf
	for round := 0; ; {
		bodies, derr := svc.NeighborBodies(c.family, sel.NdmFlags, uint32(index))
		if derr != nil {
			return r, fmt.Errorf("%w: %w", ErrNeighFlushTerminated, derr)
		}
		r.dumps++
		flushed, cerr := neighFlushRound(c, bodies, index, copied, &buf)
		if cerr != nil {
			return r, cerr
		}
		if flushed == 0 {
			return r, routeFlushDone(c.out, c.stats(), round)
		}
		if round == 0 {
			if path, ok := os.LookupEnv(writeOutEnvCst); !ok || path == "" {
				return neighFlushResult{}, ErrWriteNotApplied
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

// neighFlushKeeps is print_neigh's state test under flush's filter.state: a
// state outside the two it drops, NUD_NONE by the 0x100 bit, or a proxy or
// externally learned entry whatever its state.
func neighFlushKeeps(ni xtcpnl.NeighInfo) bool {
	return ni.State&^neighFlushDropStatesCst != 0 || ni.State == unix.NUD_NONE ||
		ni.Flags&(unix.NTF_PROXY|unix.NTF_EXT_LEARNED) != 0
}

// neighFlushRound copies one dump's kept replies into buf and returns how many
// it copied. print_neigh's tests, in its order (ip/ipneigh.c:311-367): the
// family, the device, then the state, which a proxy or externally learned
// entry escapes. The state mask loses NUD_FAILED after round 1 (:641), which
// changes nothing here: an entry kept in round 1 is skipped as copied.
func neighFlushRound(c *runCtx, bodies [][]byte, index int32, copied map[neighFlushKey]bool,
	buf *flushBuf) (int, error) {
	seen := map[neighFlushKey]bool{}
	n := 0
	for _, body := range bodies {
		ni, err := xtcpnl.ParseNeigh(body)
		if err != nil {
			return 0, fmt.Errorf("goip: decode RTM_NEWNEIGH: %w", err)
		}
		if c.family != unix.AF_UNSPEC && c.family != ni.Family || index != 0 && index != ni.Ifindex {
			continue
		}
		if !neighFlushKeeps(ni) {
			continue
		}
		k := neighFlushKey{family: ni.Family, proxy: ni.Flags&unix.NTF_PROXY != 0, ifindex: ni.Ifindex, dst: string(ni.Dst)}
		if copied[k] {
			continue
		}
		// The copy rule (:369-382): the reply whole, as RTM_DELNEIGH,
		// flagged NLM_F_REQUEST alone, at the next seq.
		msg, err := xtcpnl.BuildWriteRequest(unix.RTM_DELNEIGH, 0, c.nextSeq(),
			body[:xtcpnl.NdMsgSizeCst], body[xtcpnl.NdMsgSizeCst:])
		if err != nil {
			return 0, fmt.Errorf("goip: build neigh flush: %w", err)
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
