package xtcp

import (
	"context"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/randomizedcoder/xtcp2/pkg/xtcp_config"
)

// netlinker_loop_test.go drives the full netlinkerSyscall loop using a
// real socketpair fixture. The peer writes a few bytes (which the
// loop's Recvfrom returns), x.Deserialize fails to parse them (it's
// not a valid netlink message) and bumps the err counter, then ctx
// cancellation breaks the loop on the next iteration.
//
// Goal: exercise the loop scaffolding (header logs + metrics + capture
// branch + Deserialize-err branch + maybeForceGC + pool put on exit)
// without standing up a real netlink socket.

// newNetlinkerLoopFixture wires the minimal XTCP fields netlinkerSyscall
// needs: pools (via InitSyncPools), prom metrics, fdToNsMap.
func newNetlinkerLoopFixture(t *testing.T) *XTCP {
	t.Helper()
	x := newTestXTCP(t, "null:")
	x.config = &xtcp_config.XtcpConfig{
		// Same defaults the production daemon picks if InitSyncPools sees
		// these as zero.
		PacketSize:           64 * 1024,
		WriteFiles:           0,
		Modulus:              1, // 0 would divide-by-zero in Deserialize
		EnabledDeserializers: &xtcp_config.EnabledDeserializers{Enabled: map[string]bool{}},
	}
	// Mirror the prom seam newTestXTCP set up; newTestXTCP zeroed config above.
	tx := newTestXTCP(t, "null:")
	x.pC = tx.pC
	x.pH = tx.pH

	x.fdToNsMap = &sync.Map{}

	var wg sync.WaitGroup
	wg.Add(1)
	x.InitSyncPools(&wg)
	wg.Wait()
	// InitSyncPools relies on RTATypeDeserializer for the rta pool — drive
	// InitDeserializers too so subsequent Deserialize calls don't panic
	// when consulting RTATypeDeserializer.
	wg.Add(1)
	x.InitDeserializers(&wg)
	wg.Wait()
	return x
}

// TestNetlinkerSyscall_loopDrivesViaSocketpair pumps a few bytes
// through a socketpair to drive the netlinkerSyscall loop. Deserialize
// rejects the garbage payload, hits the err counter, then we cancel
// ctx so the loop exits.
// setShortRecvTimeout matches what setSocketTimeoutViaSyscall does in
// production but with a 50ms timeout so the loop returns to its ctx
// check every 50ms (otherwise Recvfrom blocks indefinitely on a
// socketpair with no more data).
func setShortRecvTimeout(t *testing.T, fd int) {
	t.Helper()
	tv := syscall.Timeval{Sec: 0, Usec: 50 * 1000} // 50ms
	if err := syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &tv); err != nil {
		t.Fatalf("SetsockoptTimeval: %v", err)
	}
}

func TestNetlinkerSyscall_loopDrivesViaSocketpair(t *testing.T) {
	x := newNetlinkerLoopFixture(t)
	readFD, writeFD, _ := makeSocketPair(t)
	setShortRecvTimeout(t, readFD)

	// Map the readFD to a namespace name so the debug-log helper has
	// something to print at debug>100 (kept low here, but the lookup
	// path still runs).
	x.fdToNsMap.Store(readFD, "test-ns")

	// Pre-write a few bytes so the first Recvfrom returns them.
	if _, err := syscall.Write(writeFD, []byte("garbage-netlink-bytes")); err != nil {
		t.Fatalf("write: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	nsName := "test-ns"
	go x.netlinkerSyscall(ctx, &wg, &nsName, readFD, 7)

	// Give the loop a couple of iterations to chew through the payload
	// + hit the Deserialize-err path, then cancel.
	time.Sleep(50 * time.Millisecond)
	cancel()
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("netlinkerSyscall did not exit after ctx cancel")
	}

	// At least one packet was received + the Deserialize-err branch
	// fired (garbage bytes aren't a valid netlink message).
	if got := testutil.ToFloat64(x.pC.WithLabelValues("Netlinker", "packets", "count")); got < 1 {
		t.Errorf("packets counter = %v, want ≥1", got)
	}
	if got := testutil.ToFloat64(x.pC.WithLabelValues("Netlinker", "complete", "count")); got != 1 {
		t.Errorf("complete counter = %v, want 1", got)
	}
}

// TestNetlinkerSyscall_immediateCancelExitsCleanly verifies the
// goroutine returns within one tick when ctx is already canceled
// before entry.
func TestNetlinkerSyscall_immediateCancelExitsCleanly(t *testing.T) {
	x := newNetlinkerLoopFixture(t)
	readFD, _, _ := makeSocketPair(t)
	setShortRecvTimeout(t, readFD)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel
	var wg sync.WaitGroup
	wg.Add(1)
	nsName := "ns"
	done := make(chan struct{})
	go func() {
		x.netlinkerSyscall(ctx, &wg, &nsName, readFD, 0)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("netlinkerSyscall did not exit on pre-canceled ctx")
	}
}

// TestNetlinkerSyscall_captureBranchFires drives the WriteFiles > 0
// branch of captureToFileIfEnabled by setting WriteFiles=2 + a valid
// CapturePath, then writing one packet to the socketpair.
func TestNetlinkerSyscall_captureBranchFires(t *testing.T) {
	x := newNetlinkerLoopFixture(t)
	x.config.WriteFiles = 2
	x.config.CapturePath = t.TempDir() + "/cap_"
	readFD, writeFD, _ := makeSocketPair(t)
	setShortRecvTimeout(t, readFD)
	if _, err := syscall.Write(writeFD, []byte("xy")); err != nil {
		t.Fatalf("write: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	nsName := "ns"
	go x.netlinkerSyscall(ctx, &wg, &nsName, readFD, 0)
	time.Sleep(50 * time.Millisecond)
	cancel()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("netlinkerSyscall did not exit on ctx cancel")
	}
}
