package linuxio

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

func attribute(typ uint16, data []byte) []byte {
	n := 4 + len(data)
	b := make([]byte, (n+3)&^3)
	binary.LittleEndian.PutUint16(b, uint16(n))
	binary.LittleEndian.PutUint16(b[2:], typ)
	copy(b[4:], data)
	return b
}

func familyBody(id uint16, name string, group uint32) []byte {
	idBytes := make([]byte, 2)
	binary.LittleEndian.PutUint16(idBytes, id)
	b := append([]byte{1, 2, 0, 0}, attribute(1, idBytes)...)
	b = append(b, attribute(2, append([]byte(name), 0))...)
	fields := attribute(1, []byte("monitor\x00"))
	fields = append(fields, attribute(2, word(int32(group)))...)
	return append(b, attribute(7|unix.NLA_F_NESTED, attribute(1|unix.NLA_F_NESTED, fields))...)
}

func ethtoolBody(command uint8, index uint32) []byte {
	return append([]byte{command, 1, 0, 0}, attribute(1|unix.NLA_F_NESTED, attribute(1, word(int32(index))))...)
}

func linkBody(index int32, name string) []byte {
	attr := attribute(unix.IFLA_IFNAME, append([]byte(name), 0))
	b := make([]byte, 16, 16+len(attr))
	binary.LittleEndian.PutUint32(b[4:], uint32(index))
	return append(b, attr...)
}

func genericSocket(id uint16, group uint32) *scriptSocket {
	s := &scriptSocket{}
	s.onSend = func(b []byte) {
		seq := binary.LittleEndian.Uint32(b[8:])
		typ := binary.LittleEndian.Uint16(b[4:])
		body := familyBody(id, "ethtool", group)
		if typ != xtcpnl.GenlControllerID {
			body = ethtoolBody(6, 5)
		}
		s.batches = []batch{{data: [][]byte{frame(typ, 0, seq, body)}}}
	}
	return s
}

func TestFamilyDiscoveryAndRecovery(t *testing.T) {
	first, second := genericSocket(30, 65), genericSocket(45, 129)
	c := scriptedClient(unix.NETLINK_GENERIC, first, second)
	defer c.Close()
	f, err := c.DiscoverFamily(context.Background(), "ethtool")
	if err != nil || f.ID() != 30 {
		t.Fatalf("discovery=%+v error=%v", f, err)
	}
	if group, ok := f.Group("monitor"); !ok || group != 65 {
		t.Fatalf("group=%d present=%v", group, ok)
	}
	if _, ok := f.Group("missing"); ok {
		t.Fatal("invented missing group")
	}
	got, err := c.GetEthtool(context.Background(), f, xtcpnl.EthtoolLinkStateKind, 5)
	if err != nil || len(got.Values) != 1 || got.Epoch != 1 {
		t.Fatalf("GET=%+v error=%v", got, err)
	}
	if err = c.Reset(); err != nil {
		t.Fatal(err)
	}
	if _, err = c.GetEthtool(context.Background(), f, xtcpnl.EthtoolLinkStateKind, 5); !errors.Is(err, ErrFamily) {
		t.Fatalf("stale family error=%v", err)
	}
	if len(second.sent) != 0 {
		t.Fatal("stale ID sent to replacement socket")
	}
	fresh, err := c.DiscoverFamily(context.Background(), "ethtool")
	if err != nil || fresh.ID() != 45 {
		t.Fatalf("rediscovery=%+v error=%v", fresh, err)
	}
	if group, ok := fresh.Group("monitor"); !ok || group != 129 {
		t.Fatalf("new group=%d present=%v", group, ok)
	}
	got, err = c.GetEthtool(context.Background(), fresh, xtcpnl.EthtoolLinkStateKind, 5)
	if err != nil || got.Epoch != 2 {
		t.Fatalf("recovered result=%+v error=%v", got, err)
	}
	if len(first.sent) != 2 || first.closes != 1 {
		t.Fatalf("old socket sends=%d closes=%d", len(first.sent), first.closes)
	}
}

func TestFamilyMatcher(t *testing.T) {
	rows := []struct {
		name, description  string
		body               []byte
		wantMatch, wantErr bool
	}{
		{"valid", "dynamic ID and group parsed", familyBody(30, "ethtool", 65), true, false},
		{"wrong_name", "different named family cannot satisfy discovery", familyBody(30, "devlink", 65), false, false},
		{"wrong_command", "controller notification for another command ignored", []byte{2, 2, 0, 0}, false, false},
		{"short", "partial generic header rejected", []byte{1}, false, true},
		{"missing_identity", "required family attributes cannot be absent", []byte{1, 2, 0, 0}, false, true},
		{"invalid_id", "reserved family ID rejected", familyBody(1, "ethtool", 65), false, true},
		{"group_zero", "zero group cannot be joined", familyBody(30, "ethtool", 0), false, true},
		{"group_overflow", "group must fit membership integer", familyBody(30, "ethtool", math.MaxUint32), false, true},
		{"group_boundary", "maximum positive membership ID retained", familyBody(30, "ethtool", math.MaxInt32), true, false},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Log(row.description)
			_, match, err := familyMatcher("ethtool")(xtcpnl.NetlinkEnvelope{Body: row.body})
			if match != row.wantMatch || (err != nil) != row.wantErr {
				t.Fatalf("match=%v err=%v", match, err)
			}
		})
	}
	if err := validateGroups(map[string]uint32{"one": 65, "two": 65}); !errors.Is(err, ErrReply) {
		t.Fatalf("duplicate group ID: %v", err)
	}
}

func TestEthtoolReplyMatching(t *testing.T) {
	rows := []struct {
		name, description  string
		body               []byte
		wantMatch, wantErr bool
	}{
		{"valid", "exact GET reply and device match", ethtoolBody(6, 5), true, false},
		{"wrong_device", "another device never completes request", ethtoolBody(6, 7), false, false},
		{"wrong_command", "another GET reply ignored", ethtoolBody(4, 5), false, false},
		{"notification", "notification does not count as GET reply", ethtoolBody(5, 5), false, false},
		{"short", "short generic header fails", []byte{6}, false, true},
		{"missing_index", "device identity required", []byte{6, 1, 0, 0}, false, true},
		{"malformed_index", "partial index fails", append([]byte{6, 1, 0, 0}, attribute(1|unix.NLA_F_NESTED, attribute(1, []byte{5}))...), false, true},
		{"future_command", "unrelated future command ignored", []byte{250, 1, 0, 0}, false, false},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Log(row.description)
			_, match, err := ethtoolMatcher(6, 5)(xtcpnl.NetlinkEnvelope{Body: row.body})
			if match != row.wantMatch || (err != nil) != row.wantErr {
				t.Fatalf("match=%v err=%v", match, err)
			}
		})
	}
}

func TestReadOnlyEthtoolCommands(t *testing.T) {
	rows := []struct {
		description string
		kind        xtcpnl.EthtoolKind
		get, reply  uint8
	}{
		{"link info", xtcpnl.EthtoolLinkInfoKind, 2, 2},
		{"link modes", xtcpnl.EthtoolLinkModesKind, 4, 4},
		{"link state", xtcpnl.EthtoolLinkStateKind, 6, 6},
		{"rings", xtcpnl.EthtoolRingsKind, 15, 16},
		{"channels", xtcpnl.EthtoolChannelsKind, 17, 18},
		{"pause", xtcpnl.EthtoolPauseKind, 21, 22},
		{"FEC", xtcpnl.EthtoolFECKind, 29, 30},
	}
	for _, row := range rows {
		t.Run(row.description, func(t *testing.T) {
			s := genericSocket(30, 65)
			c := scriptedClient(unix.NETLINK_GENERIC, s)
			defer c.Close()
			f, err := c.DiscoverFamily(context.Background(), "ethtool")
			if err != nil {
				t.Fatal(err)
			}
			s.onSend = func(b []byte) {
				if b[16] != row.get || binary.LittleEndian.Uint16(b[6:]) != unix.NLM_F_REQUEST {
					t.Fatalf("request bytes=%x", b)
				}
				seq := binary.LittleEndian.Uint32(b[8:])
				s.batches = []batch{{data: [][]byte{frame(30, 0, seq, ethtoolBody(row.reply, 5))}}}
			}
			got, err := c.GetEthtool(context.Background(), f, row.kind, 5)
			if err != nil || len(got.Values) != 1 || got.Values[0].Kind != row.kind {
				t.Fatalf("result=%+v error=%v", got, err)
			}
		})
	}
}

func TestFamilyCannotCrossOwnersOrWrap(t *testing.T) {
	s := genericSocket(30, 65)
	c := scriptedClient(unix.NETLINK_GENERIC, s, genericSocket(30, 65))
	defer c.Close()
	f, err := c.DiscoverFamily(context.Background(), "ethtool")
	if err != nil {
		t.Fatal(err)
	}
	other := scriptedClient(unix.NETLINK_GENERIC, genericSocket(30, 65))
	defer other.Close()
	if _, err = other.GetEthtool(context.Background(), f, xtcpnl.EthtoolLinkStateKind, 5); !errors.Is(err, ErrFamily) {
		t.Fatalf("cross owner error=%v", err)
	}
	c.sequence = math.MaxUint32
	if _, err = c.GetEthtool(context.Background(), f, xtcpnl.EthtoolLinkStateKind, 5); !errors.Is(err, ErrFamily) {
		t.Fatalf("wrap stale handle error=%v", err)
	}
}

func TestRouteQueriesAndOwnership(t *testing.T) {
	rows := []struct {
		name, description string
		index             int32
		dump              bool
	}{
		{"dump", "all-device dump requires DONE", 0, true},
		{"target", "target GET skips unrelated device", 5, false},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Log(row.description)
			s := &scriptSocket{}
			c := scriptedClient(unix.NETLINK_ROUTE, s)
			defer c.Close()
			body := linkBody(5, "eth0")
			var frames [][]byte
			s.onSend = func(b []byte) {
				seq := binary.LittleEndian.Uint32(b[8:])
				frames = [][]byte{frame(unix.RTM_NEWLINK, 0, seq, linkBody(7, "other0")), frame(unix.RTM_NEWLINK, 0, seq, body)}
				if row.dump {
					frames = append(frames, frame(unix.NLMSG_DONE, 0, seq, nil))
				}
				s.batches = []batch{{data: frames}}
			}
			var got Result[xtcpnl.LinkInfo]
			var err error
			if row.dump {
				got, err = c.DumpLinks(context.Background())
			} else {
				got, err = c.GetLink(context.Background(), row.index)
			}
			want := 1
			if row.dump {
				want = 2
			}
			if err != nil || len(got.Values) != want || got.Values[want-1].Name != "eth0" {
				t.Fatalf("result=%+v error=%v", got, err)
			}
			for _, b := range frames {
				for i := range b {
					b[i] = 0
				}
			}
			if got.Values[want-1].Name != "eth0" {
				t.Fatal("result aliases borrowed storage")
			}
		})
	}
}

func TestFamilyTimeoutInvalidatesDiscovery(t *testing.T) {
	first, second := genericSocket(30, 65), genericSocket(45, 129)
	c := scriptedClient(unix.NETLINK_GENERIC, first, second)
	defer c.Close()
	f, err := c.DiscoverFamily(context.Background(), "ethtool")
	if err != nil {
		t.Fatal(err)
	}
	first.onSend = nil
	if _, err = c.GetEthtool(context.Background(), f, xtcpnl.EthtoolLinkStateKind, 5); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("GET timeout=%v", err)
	}
	if _, err = c.GetEthtool(context.Background(), f, xtcpnl.EthtoolLinkStateKind, 5); !errors.Is(err, ErrFamily) {
		t.Fatalf("old discovery survived timeout: %v", err)
	}
	if len(second.sent) != 0 || first.closes != 1 {
		t.Fatal("stale handle sent or old socket retained")
	}
	fresh, err := c.DiscoverFamily(context.Background(), "ethtool")
	if err != nil || fresh.ID() != 45 {
		t.Fatalf("rediscovery=%+v %v", fresh, err)
	}
}

func TestRequestAPIBoundaries(t *testing.T) {
	rows := []struct {
		name, description string
		protocol          int
		call              func(*Client) error
		wantErr           error
	}{
		{"zero_index", "route GET requires positive index", unix.NETLINK_ROUTE, func(c *Client) error { _, err := c.GetLink(context.Background(), 0); return err }, ErrRequest},
		{"negative_index", "negative route index rejected", unix.NETLINK_ROUTE, func(c *Client) error { _, err := c.GetLink(context.Background(), -1); return err }, ErrRequest},
		{"wrong_protocol", "generic socket cannot issue route dump", unix.NETLINK_GENERIC, func(c *Client) error { _, err := c.DumpLinks(context.Background()); return err }, ErrRequest},
		{"rdma_separate", "RDMA is not a generic family", unix.NETLINK_RDMA, func(c *Client) error { _, err := c.DiscoverFamily(context.Background(), "ethtool"); return err }, ErrRequest},
		{"empty_handle", "ethool GET requires discovery", unix.NETLINK_GENERIC, func(c *Client) error {
			_, err := c.GetEthtool(context.Background(), Family{}, xtcpnl.EthtoolLinkStateKind, 5)
			return err
		}, ErrFamily},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Log(row.description)
			c, err := NewClient(row.protocol)
			if err != nil {
				t.Fatal(err)
			}
			c.open = func(context.Context, int) (transport, error) {
				t.Fatal("invalid request opened socket")
				return nil, ErrRequest
			}
			if err := row.call(c); !errors.Is(err, row.wantErr) {
				t.Fatalf("error=%v want=%v", err, row.wantErr)
			}
		})
	}
	if _, err := NewClient(-1); !errors.Is(err, ErrRequest) {
		t.Fatalf("invalid protocol=%v", err)
	}
}
