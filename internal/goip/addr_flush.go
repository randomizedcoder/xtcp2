package goip

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// `ip address flush` is the show loop with a flush buffer (ipaddr_flush,
// ip/ipaddress.c:1963-2015). Each round dumps the addresses, copies every
// reply print_addrinfo's filter passes into a RTM_DELADDR, sends the copies
// in as many 3584-byte datagrams as they need, and dumps again, until a dump
// matches nothing (addr-write-path.md, "flush").
//
// goip sends the dumps and writes the copies to GOIP_WRITE_OUT, all rounds
// concatenated (decision 3). Its round 2 cannot see a kernel that applied
// round 1, so it skips every address it has already copied, and it ends after
// the rounds `ip` runs when the kernel deletes what it was asked to
// (decision 5).

// ErrFlushIncomplete is a flush that ran out of rounds: `ip -loops N` with
// addresses still matching after N. `ip` exits 255 for it.
var ErrFlushIncomplete = errors.New("flush remains incomplete")

const (
	// addrFlushBufCst is ipaddr_flush's `char flushb[4096-512]`
	// (ip/ipaddress.c:1966).
	addrFlushBufCst = 4096 - 512
	// addrFlushLoopsCst is max_flush_loops' default (ip/ip.c:38).
	addrFlushLoopsCst = 10
)

// maxFlushLoops is `-loops`, or its default.
func (c *runCtx) maxFlushLoops() int64 {
	if c.haveFlushLoops {
		return c.flushLoops
	}
	return addrFlushLoopsCst
}

// flushBuf is filter.flushb: messages are packed at NLMSG_ALIGN offsets, and
// one that would overflow the buffer sends it first (ip/ipaddress.c:1558).
// The datagrams are kept rather than sent.
type flushBuf struct {
	cur       []byte
	datagrams [][]byte
}

func (b *flushBuf) add(msg []byte) {
	if p := nlmsgAlign(len(b.cur)); len(b.cur) > 0 && p+len(msg) > addrFlushBufCst {
		b.end()
	}
	if pad := nlmsgAlign(len(b.cur)) - len(b.cur); pad > 0 {
		b.cur = append(b.cur, make([]byte, pad)...)
	}
	b.cur = append(b.cur, msg...)
}

// end is flush_update: whatever the buffer holds goes out as one datagram.
func (b *flushBuf) end() {
	if len(b.cur) > 0 {
		b.datagrams = append(b.datagrams, b.cur)
		b.cur = nil
	}
}

func nlmsgAlign(n int) int { return (n + 3) &^ 3 }

// flushKey is an address as the kernel finds it to delete: by family,
// device, prefix length and the two addresses. It is the identity round 2
// subtracts by. The bytes are not, because a dynamic address's
// IFA_CACHEINFO lifetimes count down between two dumps.
type flushKey struct {
	family    uint8
	prefixlen uint8
	index     uint32
	local     string
	address   string
}

func flushKeyOf(ai *xtcpnl.AddrInfo) flushKey {
	return flushKey{family: ai.Family, prefixlen: ai.Prefixlen, index: ai.Index,
		local: string(ai.Local), address: string(ai.Address)}
}

// addrFlushResult is what a flush would have sent: its write datagrams, in
// order, and how many dumps it sent.
type addrFlushResult struct {
	datagrams [][]byte
	dumps     int
}

// addrFlush runs `ip address flush` and writes its RTM_DELADDR copies to
// GOIP_WRITE_OUT. Nothing copied, nothing written. A flush that ran out of
// rounds still writes what it copied, as `ip` has sent it, and then fails.
func addrFlush(c *runCtx, args []string) error {
	r, err := runAddrFlush(c, args)
	if err != nil && !errors.Is(err, ErrFlushIncomplete) {
		return err
	}
	if len(r.datagrams) > 0 {
		if werr := writeAddrOut(joinDatagrams(r.datagrams)); werr != nil {
			return werr
		}
	}
	return err
}

func joinDatagrams(d [][]byte) []byte {
	var out []byte
	for _, b := range d {
		out = append(out, b...)
	}
	return out
}

// runAddrFlush is ipaddr_list_flush_or_save's flush arm and ipaddr_flush.
//
// The two refusals come before the keyword loop (ip/ipaddress.c:2155-2164),
// no arguments first. The loop is the show loop, with the device resolved
// after it. The hook is checked once the names resolve, as addrWrite checks
// it, so the dumps go out only when the copies can be written.
func runAddrFlush(c *runCtx, args []string) (addrFlushResult, error) {
	var r addrFlushResult
	// iproute2 ends both lines with a period; Go error strings do not.
	if len(args) == 0 {
		return r, fmt.Errorf("%w: Flush requires arguments", ErrInvalidArgument)
	}
	if c.family == unix.AF_PACKET {
		return r, fmt.Errorf("%w: Cannot flush link addresses", ErrInvalidArgument)
	}
	svc := service.New(c.src, c.nextSeq)
	f, err := parseAddrShowArgs(args, c.family, addrShowLookups(c, svc))
	if err != nil {
		return r, err
	}
	if path, ok := os.LookupEnv(writeOutEnvCst); !ok || path == "" {
		return r, ErrWriteNotApplied
	}
	var ifindex uint32
	if f.ifindex > 0 {
		ifindex = uint32(f.ifindex)
	}
	// print_addrinfo's label test names an address with no IFA_LABEL by
	// ll_index_to_name, which gets an uncached index from the kernel.
	devName := func(idx int32) string {
		resolveIndexName(c, svc, idx)
		return c.lltab.IndexToName(idx)
	}

	copied := map[flushKey]bool{}
	var buf flushBuf
	loops := c.maxFlushLoops()
	for round := int64(0); loops == 0 || round < loops; {
		bodies, derr := svc.AddressBodies(f.family, ifindex)
		if derr != nil {
			return r, derr
		}
		r.dumps++
		flushed, cerr := flushRound(c, &f, bodies, copied, &buf, devName)
		if cerr != nil {
			return r, cerr
		}
		if flushed == 0 {
			return r, flushDone(c.out, c.stats(), round)
		}
		round++
		buf.end()
		r.datagrams = buf.datagrams
		if c.stats() {
			if _, werr := fmt.Fprintf(c.out, "\n*** Round %d, deleting %d addresses ***\n", round, flushed); werr != nil {
				return r, werr
			}
		}
		// A primary flush is one round, or it would go on to the
		// secondaries the kernel promoted (ip/ipaddress.c:2004-2010).
		if f.flags&unix.IFA_F_SECONDARY == 0 && f.flagmask&unix.IFA_F_SECONDARY != 0 {
			return r, flushDone(c.out, c.stats(), round)
		}
	}
	return r, fmt.Errorf("%w: *** Flush remains incomplete after %d rounds. ***", ErrFlushIncomplete, loops)
}

// flushRound copies one dump's matching replies into buf and returns how
// many it copied. An address copied in an earlier round is skipped: `ip`'s
// kernel has deleted it, and goip's has not.
func flushRound(c *runCtx, f *addrFilter, bodies [][]byte, copied map[flushKey]bool,
	buf *flushBuf, devName func(int32) string) (int, error) {
	seen := map[flushKey]bool{}
	n := 0
	for _, body := range bodies {
		ai, err := xtcpnl.ParseNewAddr(body)
		if err != nil {
			return 0, fmt.Errorf("goip: decode RTM_NEWADDR: %w", err)
		}
		k := flushKeyOf(&ai)
		if copied[k] || !f.matchFlush(&ai, devName) {
			continue
		}
		// The copy rule (ip/ipaddress.c:1562-1567): the reply whole, as
		// RTM_DELADDR, flagged NLM_F_REQUEST alone, at the next seq.
		msg, err := xtcpnl.BuildWriteRequest(unix.RTM_DELADDR, 0, c.nextSeq(),
			body[:xtcpnl.FamilyHdrLen(unix.RTM_DELADDR)], body[xtcpnl.FamilyHdrLen(unix.RTM_DELADDR):])
		if err != nil {
			return 0, fmt.Errorf("goip: build address flush: %w", err)
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

// flushDone is ipaddr_flush's flush_done label: under -s, the closing line.
func flushDone(w io.Writer, stats bool, round int64) error {
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
