package linuxio

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

func liveClient(t *testing.T, protocol int) *Client {
	t.Helper()
	c, err := NewClient(protocol)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := c.Close(); err != nil {
			t.Error(err)
		}
	})
	return c
}

func TestLiveRouteTransactions(t *testing.T) {
	c := liveClient(t, unix.NETLINK_ROUTE)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	dump, err := c.DumpLinks(ctx)
	if err != nil || len(dump.Values) == 0 {
		t.Fatalf("dump=%+v error=%v", dump, err)
	}
	index := dump.Values[0].Index
	got, err := c.GetLink(ctx, index)
	if err != nil || len(got.Values) != 1 || got.Values[0].Index != index || got.Sequence <= dump.Sequence || got.Epoch != dump.Epoch {
		t.Fatalf("GET=%+v error=%v", got, err)
	}
	ack, err := execute(ctx, c, request[xtcpnl.LinkInfo]{
		build: func(seq uint32, _ uint64) ([]byte, error) {
			b, buildErr := xtcpnl.BuildGetLinkByIndexRequest(unix.AF_UNSPEC, index, 0, seq)
			if buildErr == nil {
				binary.LittleEndian.PutUint16(b[6:], unix.NLM_F_REQUEST|unix.NLM_F_ACK)
			}
			return b, buildErr
		}, match: linkMatcher(uint32(index)), family: unix.RTM_NEWLINK, ack: true,
	})
	if err != nil || len(ack.Values) != 1 {
		t.Fatalf("ACK contract=%+v error=%v", ack, err)
	}
	used := make(map[int32]bool, len(dump.Values))
	for _, link := range dump.Values {
		used[link.Index] = true
	}
	missing := int32(1)
	for used[missing] {
		missing++
	}
	if failed, err := c.GetLink(ctx, missing); !errors.Is(err, unix.ENODEV) || failed.Values != nil {
		t.Fatalf("missing device result=%+v error=%v", failed, err)
	}
	recovered, err := c.GetLink(ctx, index)
	if err != nil || recovered.Epoch <= dump.Epoch || recovered.Sequence != 1 {
		t.Fatalf("recovery=%+v error=%v", recovered, err)
	}
	t.Logf("read-only route transactions: %d links; GET and explicit ACK pass; ENODEV recovery epoch %d -> %d", len(dump.Values), dump.Epoch, recovered.Epoch)
}

func TestLiveFamilyDiscovery(t *testing.T) {
	c := liveClient(t, unix.NETLINK_GENERIC)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	f, err := c.DiscoverFamily(ctx, "nlctrl")
	if err != nil || f.ID() != xtcpnl.GenlControllerID {
		t.Fatalf("controller=%+v error=%v", f, err)
	}
	id, ok := f.Group("notify")
	if !ok || id == 0 {
		t.Fatalf("controller notify group=%d present=%v", id, ok)
	}
	if err := c.Reset(); err != nil {
		t.Fatal(err)
	}
	next, err := c.DiscoverFamily(ctx, "nlctrl")
	if err != nil || next.epoch <= f.epoch {
		t.Fatalf("rediscovery=%+v error=%v", next, err)
	}
	t.Logf("read-only controller discovery: family=%d notify=%d; reset rediscovered epoch %d -> %d", f.ID(), id, f.epoch, next.epoch)
}

func TestLiveRequestTimeoutRecovery(t *testing.T) {
	c := liveClient(t, unix.NETLINK_ROUTE)
	dump, err := c.DumpLinks(context.Background())
	if err != nil || len(dump.Values) == 0 {
		t.Fatalf("inventory=%+v error=%v", dump, err)
	}
	index := dump.Values[0].Index
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	result, err := execute(ctx, c, request[xtcpnl.LinkInfo]{
		build: func(seq uint32, _ uint64) ([]byte, error) {
			return xtcpnl.BuildGetLinkByIndexRequest(unix.AF_UNSPEC, index, 0, seq)
		},
		match:  func(xtcpnl.NetlinkEnvelope) (xtcpnl.LinkInfo, bool, error) { return xtcpnl.LinkInfo{}, false, nil },
		family: unix.RTM_NEWLINK,
	})
	if (!errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, os.ErrDeadlineExceeded)) || result.Values != nil {
		t.Fatalf("timeout result=%+v error=%v", result, err)
	}
	got, err := c.GetLink(context.Background(), index)
	if err != nil || got.Epoch <= dump.Epoch || got.Sequence != 1 {
		t.Fatalf("timeout recovery=%+v error=%v", got, err)
	}
}
