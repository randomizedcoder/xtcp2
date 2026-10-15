package io_uring

import (
	"syscall"
	"testing"
	"time"
)

func TestRecvBufCountFor(t *testing.T) {
	tests := []struct {
		description string
		n           int
		expected    int
	}{
		{description: "positive: a power of two is kept", n: 64, expected: 64},
		{description: "positive: rounds up to the next power of two", n: 65, expected: 128},
		{description: "boundary: one buffer", n: 1, expected: 1},
		{description: "boundary: the kernel's limit is kept", n: maxRecvBufs, expected: maxRecvBufs},
		{description: "corner: zero still gets one buffer", n: 0, expected: 1},
		{description: "corner: past the kernel's limit is capped", n: maxRecvBufs + 1, expected: maxRecvBufs},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := recvBufCountFor(tc.n); got != tc.expected {
				t.Errorf("recvBufCountFor(%d) = %d, expected %d", tc.n, got, tc.expected)
			}
		})
	}
}

// newTestRecvRing is newTestRing with a buffer ring of bufs buffers of
// multishotBufSize bytes.
func newTestRecvRing(t *testing.T, bufs int) *Ring {
	t.Helper()
	r := newTestRing(t, bufs)
	if err := r.setupRecvBufs(bufs, multishotBufSize); err != nil {
		skipNoIoUring(t, err)
		return nil
	}
	return r
}

const multishotBufSize = 64

// TestRecvMultishot queues packets on a datagram socket, arms one
// multishot recv and reaps its CQEs: packets come back in order, each in
// a provided buffer, and running out of buffers ends the recv with
// ENOBUFS, after which a re-arm picks up where it stopped.
func TestRecvMultishot(t *testing.T) {
	tests := []struct {
		description      string
		bufs             int
		packets          int
		recycle          bool // recycle each buffer, and re-arm after an end
		trickle          bool // write a packet per wait after arming, not all up front
		expectedReceived int
		expectedEnded    bool // a CQE came without More
	}{
		{description: "positive: fewer packets than buffers all arrive, the recv stays armed", bufs: 4, packets: 3, expectedReceived: 3},
		{description: "positive: packets trickling in on recycled buffers keep the recv armed past the buffer count", bufs: 4, packets: 12, recycle: true, trickle: true, expectedReceived: 12},
		{description: "boundary: 2 buffers, 5 packets, no recycling: 2 arrive, then ENOBUFS ends the recv", bufs: 2, packets: 5, expectedReceived: 2, expectedEnded: true},
		{description: "corner: 1 buffer, recycled and re-armed after each ENOBUFS, keeps the order", bufs: 1, packets: 5, recycle: true, expectedReceived: 5, expectedEnded: true},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			r := newTestRecvRing(t, tc.bufs)
			srv, cli := socketpair(t)
			written := 0
			write := func() {
				if _, err := syscall.Write(srv, []byte{byte(written)}); err != nil {
					t.Fatal(err)
				}
				written++
			}
			for !tc.trickle && written < tc.packets {
				write()
			}
			if _, err := r.EnqueueRecvMultishot(cli); err != nil {
				t.Fatal(err)
			}

			received, ended := 0, false
			for received < tc.packets && (!ended || tc.recycle) {
				if tc.trickle && written < tc.packets {
					write()
				}
				if _, err := r.Submit(); err != nil {
					t.Fatal(err)
				}
				results, err := r.WaitOneTimeout(time.Second)
				if err != nil {
					break // nothing more is coming
				}
				for _, res := range results {
					if res.Op != OpRecvMultishot {
						t.Fatalf("op = %d, expected OpRecvMultishot", res.Op)
					}
					if res.Data != nil {
						if len(res.Data) != 1 || int(res.Data[0]) != received {
							t.Errorf("packet %d = %x, expected [%02x]", received, res.Data, received)
						}
						received++
						if tc.recycle {
							r.RecycleRecvBuf(res.BufID)
						}
					}
					if !res.More {
						ended = true
						if res.Res != -int32(syscall.ENOBUFS) {
							t.Errorf("end res = %d, expected -ENOBUFS", res.Res)
						}
						if tc.recycle {
							if _, err := r.EnqueueRecvMultishot(cli); err != nil {
								t.Fatal(err)
							}
						}
					}
				}
			}
			if received != tc.expectedReceived || ended != tc.expectedEnded {
				t.Errorf("received %d, ended %v; expected %d, %v",
					received, ended, tc.expectedReceived, tc.expectedEnded)
			}
		})
	}
}

func TestEnqueueRecvMultishot_noBufRing(t *testing.T) {
	r := newTestRing(t, 4)
	_, cli := socketpair(t)
	if _, err := r.EnqueueRecvMultishot(cli); err == nil {
		t.Fatal("EnqueueRecvMultishot without a buffer ring: expected an error")
	}
	if got := r.InFlightLen(); got != 0 {
		t.Errorf("InFlightLen = %d, expected 0", got)
	}
}

// TestCloseCancelsMultishot: an armed multishot recv never completes on
// its own, so Close cancels it rather than wait out the drain timeout.
func TestCloseCancelsMultishot(t *testing.T) {
	r := newTestRecvRing(t, 4)
	_, cli := socketpair(t)
	if _, err := r.EnqueueRecvMultishot(cli); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Submit(); err != nil {
		t.Fatal(err)
	}
	const drainTimeout = 5 * time.Second
	start := time.Now()
	r.Close(drainTimeout, nil)
	if took := time.Since(start); took >= time.Second {
		t.Errorf("Close took %v, expected well under the %v drain timeout", took, drainTimeout)
	}
	if got := r.InFlightLen(); got != 0 {
		t.Errorf("InFlightLen after Close = %d, expected 0", got)
	}
}
