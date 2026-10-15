package xtcp

import (
	"context"
	"encoding/binary"
	"syscall"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestIoUringReceiveOrder pins what the dump accounting relies on: the
// io_uring receiver hands packets to the decoders in the order the kernel
// queued them. dumpTracker numbers packets in that order, so a packet
// handed over after the NLMSG_DONE that followed it in the queue would be
// counted into the next dump and its records dropped after the flush.
//
// Each packet carries its sequence number past a 16-byte header-sized
// prefix of zeros, which packetHasDone reads as an nlmsg_len of 0 and
// stops at. Packets are written in bursts onto an idle socket, the case
// where single-shot recvmsg CQEs were seen out of order (every recv armed,
// then a burst wakes them all). With fewer ring buffers than a burst, the
// multishot recv runs out (ENOBUFS) and is re-armed mid-burst.
func TestIoUringReceiveOrder(t *testing.T) {
	const rounds = 2000
	tests := []struct {
		description    string
		size           int
		burst          int
		recvBufs       uint32 // ring buffers; 0 is the default
		expected       int    // packets received out of order
		expectedNoBufs bool   // the recv ran out of buffers and was re-armed
	}{
		{description: "boundary: one packet per burst", size: 32, burst: 1, expected: 0},
		{description: "positive: small packets, bursts of 2", size: 32, burst: 2, expected: 0},
		{description: "positive: small packets, bursts of 6", size: 32, burst: 6, expected: 0},
		{description: "positive: dump-sized packets, bursts of 2", size: 40000, burst: 2, expected: 0},
		{description: "corner: dump-sized packets, bursts of 6 (more than the socket buffer holds at once)", size: 40000, burst: 6, expected: 0},
		{description: "corner: one ring buffer, bursts of 6: re-armed after every ENOBUFS, still in order", size: 32, burst: 6, recvBufs: 1, expected: 0, expectedNoBufs: true},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			requireIoUring(t)
			x := newTestDeserializeXTCP(t)
			x.packetBufferPool.Init(func() *[]byte {
				b := make([]byte, dumpBufferSize)
				return &b
			})
			x.config.NlTimeoutMilliseconds = 50
			x.config.IoUringRecvBatchSize = tc.recvBufs
			readFD, writeFD, _ := makeSocketPair(t)

			ctx, cancel := context.WithCancel(context.Background())
			packetCh := make(chan dumpPacket, 4)
			nsName := "test-ns"
			go x.netlinkerIoUring(ctx, &nsName, readFD, packetCh)
			t.Cleanup(func() {
				cancel()
				for p := range packetCh {
					x.putPacketBuffer(p.buf)
				}
			})

			pkt := make([]byte, tc.size)
			got, seq := 0, 0
			for r := 0; r < rounds; r++ {
				errCh := make(chan error, 1)
				go func(first int) {
					for i := range tc.burst {
						binary.LittleEndian.PutUint32(pkt[xtcpnlHdrPrefix:], uint32(first+i))
						if _, err := syscall.Write(writeFD, pkt); err != nil {
							errCh <- err
							return
						}
					}
					errCh <- nil
				}(seq)
				for i := range tc.burst {
					var p dumpPacket
					select {
					case p = <-packetCh:
					case <-time.After(dumpDoneTimeout):
						t.Fatalf("round %d: packet %d not received", r, i)
					}
					if v := int(binary.LittleEndian.Uint32((*p.buf)[xtcpnlHdrPrefix:])); v != seq+i {
						got++
					}
					x.putPacketBuffer(p.buf)
				}
				if err := <-errCh; err != nil {
					t.Fatal(err)
				}
				seq += tc.burst
			}
			if got != tc.expected {
				t.Errorf("%d of %d packets out of order, expected %d", got, seq, tc.expected)
			}
			noBufs := testutil.ToFloat64(x.pC.WithLabelValues("NetlinkerIoUring", "NoBufs", "count"))
			if (noBufs > 0) != tc.expectedNoBufs {
				t.Errorf("NoBufs = %v, expected some: %v", noBufs, tc.expectedNoBufs)
			}
		})
	}
}

// xtcpnlHdrPrefix is where TestIoUringReceiveOrder stores the sequence
// number: past a zeroed netlink-header-sized prefix.
const xtcpnlHdrPrefix = 16
