package goip

import (
	"fmt"
	"os"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// tcpmDelete is the ACKed DEL, written to GOIP_WRITE_OUT: an exact address
// and source, or none at all for flush everything.
func tcpmDelete(c *runCtx, id uint16, f *tcpmFilter) error {
	attrs, err := tcpmAttrs(f)
	if err != nil {
		return err
	}
	req, err := xtcpnl.BuildGenlWriteRequest(xtcpnl.TCPMetricsGenlName, id, xtcpnl.TCPMetricsCmdDel,
		xtcpnl.TCPMetricsGenlVersion, unix.NLM_F_ACK, c.nextSeq(), attrs)
	if err != nil {
		return err
	}
	return writeAddrOut(req)
}

// tcpmFlush is the flush rounds. Round 1 copies each kept entry into a DEL;
// round 2 skips every entry already copied, so it keeps none and ends the
// flush. Nothing kept in round 1 is "Nothing to flush." on stderr, -s or
// not, and nothing written.
func tcpmFlush(c *runCtx, id uint16, f *tcpmFilter) error {
	copied := map[string]bool{}
	var buf flushBuf
	for round := 0; ; {
		bodies, err := tcpmDump(c, id)
		if err != nil {
			return fmt.Errorf("%w: Flush terminated: %w", ErrTCPMetricsDumpTerminated, err)
		}
		flushed := 0
		for _, body := range bodies {
			m, keep, err := tcpmKeep(body, f)
			if err != nil {
				return fmt.Errorf("%w: Flush terminated: %w", ErrTCPMetricsDumpTerminated, err)
			}
			key := m.Addr.String() + "/" + m.Saddr.String()
			if !keep || copied[key] {
				continue
			}
			if round == 0 && flushed == 0 {
				if path, ok := os.LookupEnv(writeOutEnvCst); !ok || path == "" {
					return ErrWriteNotApplied
				}
			}
			del, err := tcpmFlushDel(c, id, &m)
			if err != nil {
				return err
			}
			buf.add(del)
			copied[key] = true
			flushed++
		}
		if flushed == 0 {
			return tcpmFlushDone(c, round, buf.datagrams)
		}
		round++
		buf.end()
		if c.stats() {
			if _, err := fmt.Fprintf(c.out, "\n*** Round %d, deleting %d entries ***\n", round, flushed); err != nil {
				return err
			}
		}
	}
}

// tcpmFlushDel is process_msg's copy: a DEL of the entry's address and
// source, the attribute types the reply carried them in.
func tcpmFlushDel(c *runCtx, id uint16, m *xtcpnl.TCPMetrics) ([]byte, error) {
	var raw [tcpmBufCst]byte
	b := xtcpnl.NewAttrBuilder(raw[:])
	atype, stype := xtcpnl.TCPMetricsAttrAddrIPv6, xtcpnl.TCPMetricsAttrSaddrIPv6
	if m.Addr.Is4() {
		atype = xtcpnl.TCPMetricsAttrAddrIPv4
	}
	if m.Saddr.Is4() {
		stype = xtcpnl.TCPMetricsAttrSaddrIPv4
	}
	if err := b.PutBytes(atype, m.Addr.AsSlice()); err != nil {
		return nil, err
	}
	if m.Saddr.IsValid() {
		if err := b.PutBytes(stype, m.Saddr.AsSlice()); err != nil {
			return nil, err
		}
	}
	return xtcpnl.BuildGenlWriteRequest(xtcpnl.TCPMetricsGenlName, id, xtcpnl.TCPMetricsCmdDel,
		xtcpnl.TCPMetricsGenlVersion, 0, c.nextSeq(), b.Bytes())
}

// tcpmFlushDone ends the flush: nothing to flush on stderr in round 0, or
// the copies written and, under -s, the summary.
func tcpmFlushDone(c *runCtx, round int, datagrams [][]byte) error {
	if round == 0 {
		_, err := fmt.Fprint(c.errOut, "Nothing to flush.\n")
		return err
	}
	if err := writeAddrOut(joinDatagrams(datagrams)); err != nil {
		return err
	}
	if !c.stats() {
		return nil
	}
	return routeFlushDone(c.out, true, round)
}
