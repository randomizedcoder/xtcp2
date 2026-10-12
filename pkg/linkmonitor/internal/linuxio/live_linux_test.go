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

// TestLiveRDMAPortGet checks the single-port GET against the kernel, whose
// reply type differs from the request's. It skips without RDMA devices.
func TestLiveRDMAPortGet(t *testing.T) {
	c := liveClient(t, unix.NETLINK_RDMA)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	devices, err := c.DumpRDMADevices(ctx)
	if errors.Is(err, unix.EPROTONOSUPPORT) || errors.Is(err, unix.EAFNOSUPPORT) || errors.Is(err, unix.EOPNOTSUPP) || (err == nil && len(devices.Values) == 0) {
		t.Skipf("no RDMA devices: %v", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range devices.Values {
		start := time.Now()
		got, err := c.GetRDMAPort(ctx, d.Index, 1)
		if err != nil || len(got.Values) != 1 || got.Values[0].Index != d.Index || got.Values[0].Port != 1 {
			t.Fatalf("%s/1 GET=%+v error=%v after %v", d.Name, got, err, time.Since(start))
		}
		t.Logf("%s/1 GET in %v: state=%d physical=%d", d.Name, time.Since(start), got.Values[0].State, got.Values[0].Physical)
	}
}

// liveShapes is the state shared by TestLiveRequestShapes rows. One client per
// protocol keeps each protocol's requests on one socket, so an nlrecord run
// (LINKMONITOR_NLRECORD=<dir> go test -tags nlrecord -run
// '^TestLiveRequestShapes$') records each as one replayable pcap.
type liveShapes struct {
	route, generic, rdma *Client
	links                []xtcpnl.LinkInfo
	ether                uint32
	families             map[string]Family
	rdmaDevices          []RDMAPort
}

// liveEtherIndex picks LINKMONITOR_LIVE_INTERFACE, else the first Ethernet link.
func liveEtherIndex(links []xtcpnl.LinkInfo) uint32 {
	name := os.Getenv("LINKMONITOR_LIVE_INTERFACE")
	for i := range links {
		l := &links[i]
		if (name != "" && l.Name == name) || (name == "" && l.Type == unix.ARPHRD_ETHER) {
			return uint32(l.Index)
		}
	}
	return 0
}

// family rediscovers after an error reply, which retires the socket and with
// it every family handle of that epoch.
func (s *liveShapes) family(ctx context.Context, name string) (Family, error) {
	if f, ok := s.families[name]; ok {
		return f, nil
	}
	f, err := s.generic.DiscoverFamily(ctx, name)
	if err != nil {
		return Family{}, errors.Join(errLivePrerequisite, err)
	}
	s.families[name] = f
	return f, nil
}

var errLivePrerequisite = errors.New("live prerequisite missing")

// TestLiveRequestShapes issues every request shape linuxio builds once against
// the running kernel. A row skips only on its listed unsupported errnos or a
// missing prerequisite (no Ethernet link, no RDMA device), and says which.
func TestLiveRequestShapes(t *testing.T) {
	s := &liveShapes{
		route:    liveClient(t, unix.NETLINK_ROUTE),
		generic:  liveClient(t, unix.NETLINK_GENERIC),
		rdma:     liveClient(t, unix.NETLINK_RDMA),
		families: map[string]Family{},
	}
	noRDMA := []error{unix.EPROTONOSUPPORT, unix.EAFNOSUPPORT, unix.EOPNOTSUPP}
	discover := func(name string) func(context.Context, *liveShapes) (int, error) {
		return func(ctx context.Context, s *liveShapes) (int, error) {
			f, err := s.generic.DiscoverFamily(ctx, name)
			if err == nil {
				s.families[name] = f
			}
			return 1, err
		}
	}
	ethtool := func(kind xtcpnl.EthtoolKind) func(context.Context, *liveShapes) (int, error) {
		return func(ctx context.Context, s *liveShapes) (int, error) {
			f, err := s.family(ctx, ethtoolFamilyName)
			if err != nil || s.ether == 0 {
				return 0, errLivePrerequisite
			}
			got, err := s.generic.GetEthtool(ctx, f, kind, s.ether)
			return len(got.Values), err
		}
	}
	rows := []struct {
		category, description, expected string
		unsupported                     []error
		wantValues                      int // -1: at least one; 0: any count
		run                             func(context.Context, *liveShapes) (int, error)
	}{
		{"positive", "route link dump", "at least one link", nil, -1, func(ctx context.Context, s *liveShapes) (int, error) {
			got, err := s.route.DumpLinks(ctx)
			s.links = got.Values
			s.ether = liveEtherIndex(got.Values)
			return len(got.Values), err
		}},
		{"positive", "route link GET", "exactly the requested link", nil, 1, func(ctx context.Context, s *liveShapes) (int, error) {
			if len(s.links) == 0 {
				return 0, errLivePrerequisite
			}
			got, err := s.route.GetLink(ctx, s.links[0].Index)
			return len(got.Values), err
		}},
		{"positive", "genl GETFAMILY nlctrl", "controller resolves", nil, 1, discover("nlctrl")},
		{"positive", "genl GETFAMILY ethtool", "ethtool family resolves", []error{unix.ENOENT}, 1, discover(ethtoolFamilyName)},
		{"positive", "genl GETFAMILY devlink", "devlink family resolves", []error{unix.ENOENT}, 1, discover("devlink")},
		{"positive", "ethtool LINKINFO_GET", "one reply for the link", []error{unix.EOPNOTSUPP}, 1, ethtool(xtcpnl.EthtoolLinkInfoKind)},
		{"positive", "ethtool LINKMODES_GET", "one reply for the link", []error{unix.EOPNOTSUPP}, 1, ethtool(xtcpnl.EthtoolLinkModesKind)},
		{"positive", "ethtool LINKSTATE_GET", "one reply for the link", []error{unix.EOPNOTSUPP}, 1, ethtool(xtcpnl.EthtoolLinkStateKind)},
		{"positive", "ethtool RINGS_GET", "one reply for the link", []error{unix.EOPNOTSUPP}, 1, ethtool(xtcpnl.EthtoolRingsKind)},
		{"positive", "ethtool CHANNELS_GET", "one reply for the link", []error{unix.EOPNOTSUPP}, 1, ethtool(xtcpnl.EthtoolChannelsKind)},
		{"positive", "ethtool PAUSE_GET", "one reply for the link", []error{unix.EOPNOTSUPP}, 1, ethtool(xtcpnl.EthtoolPauseKind)},
		{"positive", "ethtool FEC_GET", "one reply for the link", []error{unix.EOPNOTSUPP}, 1, ethtool(xtcpnl.EthtoolFECKind)},
		{"boundary", "devlink port dump", "zero or more ports, never partial", nil, 0, func(ctx context.Context, s *liveShapes) (int, error) {
			f, err := s.family(ctx, "devlink")
			if err != nil {
				return 0, err
			}
			got, err := s.generic.DumpDevlinkPorts(ctx, f)
			return len(got.Values), err
		}},
		{"positive", "RDMA device dump", "at least one device", noRDMA, -1, func(ctx context.Context, s *liveShapes) (int, error) {
			got, err := s.rdma.DumpRDMADevices(ctx)
			s.rdmaDevices = got.Values
			if err == nil && len(got.Values) == 0 {
				return 0, errLivePrerequisite
			}
			return len(got.Values), err
		}},
		{"positive", "RDMA port dump", "at least one port of the first device", nil, -1, func(ctx context.Context, s *liveShapes) (int, error) {
			if len(s.rdmaDevices) == 0 {
				return 0, errLivePrerequisite
			}
			got, err := s.rdma.DumpRDMAPorts(ctx, s.rdmaDevices[0].Index)
			return len(got.Values), err
		}},
		{"corner", "RDMA single-port GET", "one port; the reply is typed RDMA_NLDEV_CMD_GET", nil, 1, func(ctx context.Context, s *liveShapes) (int, error) {
			if len(s.rdmaDevices) == 0 {
				return 0, errLivePrerequisite
			}
			got, err := s.rdma.GetRDMAPort(ctx, s.rdmaDevices[0].Index, 1)
			return len(got.Values), err
		}},
		{"positive", "RDMA SYS_GET monitor mode", "monitor mode on", noRDMA, 1, func(ctx context.Context, s *liveShapes) (int, error) {
			return 1, s.rdma.monitorMode(ctx)
		}},
		{"negative", "route GET of an unused index", "ENODEV; the socket is retired, so this row is last on route", []error{}, 0, func(ctx context.Context, s *liveShapes) (int, error) {
			used := make(map[int32]bool, len(s.links))
			for _, l := range s.links {
				used[l.Index] = true
			}
			missing := int32(1)
			for used[missing] {
				missing++
			}
			got, err := s.route.GetLink(ctx, missing)
			if errors.Is(err, unix.ENODEV) && got.Values == nil {
				return 0, nil
			}
			return len(got.Values), errors.Join(errors.New("want ENODEV"), err)
		}},
	}
	for _, row := range rows {
		t.Run(row.category+"/"+row.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", row.category, row.description, row.expected)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			n, err := row.run(ctx, s)
			if err != nil {
				clear(s.families)
			}
			if errors.Is(err, errLivePrerequisite) {
				t.Skipf("skipped: %v", err)
			}
			for _, skip := range row.unsupported {
				if errors.Is(err, skip) {
					t.Skipf("skipped: unsupported here: %v", err)
				}
			}
			if err != nil || (row.wantValues < 0 && n == 0) || (row.wantValues > 0 && n != row.wantValues) {
				t.Fatalf("values=%d want %d error=%v", n, row.wantValues, err)
			}
			t.Logf("values=%d", n)
		})
	}
	if err := recordError(); err != nil {
		t.Fatalf("recording: %v", err)
	}
}
