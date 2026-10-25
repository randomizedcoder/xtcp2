package agent

import (
	"context"
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/netip"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_controller"
	flat "github.com/randomizedcoder/xtcp2/gen/go/xtcp_flat_record"
	"github.com/randomizedcoder/xtcp2/internal/goip"
	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/controller"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/proto"
)

type Config struct {
	AgentID           string   `json:"agent_id"`
	Location          string   `json:"location"`
	Controller        string   `json:"controller"`
	ServerName        string   `json:"server_name"`
	Certificate       string   `json:"certificate"`
	PrivateKey        string   `json:"private_key"`
	CA                string   `json:"ca"`
	Directory         string   `json:"directory"`
	EnableApply       bool     `json:"enable_apply"`
	Table             uint32   `json:"table"`
	RulePriority      uint32   `json:"rule_priority"`
	Protocol          uint8    `json:"protocol"`
	ProtectedPrefixes []string `json:"protected_prefixes"`
}

func LoadConfig(path string) (Config, error) {
	var c Config
	f, e := os.Open(path)
	if e != nil {
		return c, e
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 65537))
	d.DisallowUnknownFields()
	if e = d.Decode(&c); e != nil {
		return c, e
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return c, errors.New("trailing config data")
	}
	if !treatment.ValidID(c.AgentID) || !treatment.ValidID(c.Location) || c.Controller == "" {
		return c, errors.New("agent_id, location and controller are required")
	}
	if c.Directory == "" {
		c.Directory = "/var/lib/xtcp2/routes"
	}
	if c.Table == 0 {
		c.Table = 10002
	}
	if c.RulePriority == 0 {
		c.RulePriority = 30000
	}
	if c.Protocol == 0 {
		c.Protocol = 242
	}
	return c, nil
}

type Agent struct {
	Config       Config
	Reconciler   *treatment.Reconciler
	backend      *routeBackend
	client       pb.AgentControlServiceClient
	conn         *grpc.ClientConn
	state        atomic.Pointer[pb.LocalState]
	exposure     atomic.Pointer[exposureSnapshot]
	wake         chan struct{}
	collectionID string
	namespace    uint64
}

type exposureSnapshot struct {
	revision uint64
	held     bool
	routes   map[netip.Addr]*pb.ApplicationRecord
}

// NewLocal creates the offline emergency/watchdog path. It intentionally does
// not read TLS credentials, resolve DNS, or connect to the controller.
func NewLocal(c Config) (*Agent, error) {
	return newLocal(c, localIdentity)
}

// Separate host inspection from construction so configuration and lifecycle
// tests do not depend on permission to inspect PID 1's network namespace.
func localIdentity() (string, uint64, error) {
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", 0, err
	}
	var ns, initNS unix.Stat_t
	if err = unix.Stat("/proc/self/ns/net", &ns); err != nil {
		return "", 0, err
	}
	if err = unix.Stat("/proc/1/ns/net", &initNS); err != nil {
		return "", 0, err
	}
	if ns.Ino != initNS.Ino {
		return "", 0, errors.New("actuator must run in the host network namespace")
	}
	return strings.TrimSpace(string(boot)), ns.Ino, nil
}

func newLocal(c Config, identity func() (string, uint64, error)) (*Agent, error) {
	if !treatment.ValidID(c.AgentID) || !treatment.ValidID(c.Location) {
		return nil, errors.New("invalid local agent identity")
	}
	boot, namespace, e := identity()
	if e != nil {
		return nil, e
	}
	b := &routeBackend{TreatmentBackend: &goip.TreatmentBackend{Table: c.Table, Priority: c.RulePriority, Protocol: c.Protocol, Enabled: c.EnableApply}}
	for _, v := range c.ProtectedPrefixes {
		p, e := netip.ParsePrefix(v)
		if e != nil {
			return nil, e
		}
		b.Protected = append(b.Protected, p)
	}
	if c.EnableApply && len(b.Protected) == 0 {
		return nil, errors.New("protected_prefixes must cover management, DNS and telemetry networks before enabling application")
	}
	var nonce [16]byte
	if _, e = rand.Read(nonce[:]); e != nil {
		return nil, e
	}
	a := &Agent{Config: c, backend: b, wake: make(chan struct{}, 1), namespace: namespace, collectionID: hex.EncodeToString(nonce[:])}
	a.Reconciler = &treatment.Reconciler{Journal: treatment.Journal{Dir: c.Directory}, Backend: b, AgentID: c.AgentID, BootID: boot}
	return a, nil
}

// New validates identity before constructing any mutation-capable path. DNS
// resolution is only needed to add management destinations to the exclusions;
// deployments should list stable management prefixes for offline reboot use.
func New(ctx context.Context, c Config) (*Agent, error) {
	tlsConfig, e := controller.TLSConfig(c.Certificate, c.PrivateKey, c.CA, c.ServerName, false)
	if e != nil {
		return nil, e
	}
	cert, e := x509.ParseCertificate(tlsConfig.Certificates[0].Certificate[0])
	if e != nil {
		return nil, e
	}
	id, e := controller.CertificateIdentity(cert)
	if e != nil {
		return nil, e
	}
	if id.Role != "agent" || id.ID != c.AgentID || id.Location != c.Location {
		return nil, errors.New("agent config does not match certificate identity")
	}
	a, e := NewLocal(c)
	if e != nil {
		return nil, e
	}
	b := a.backend
	host, _, e := net.SplitHostPort(c.Controller)
	if e != nil {
		return nil, e
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if ip, e := netip.ParseAddr(host); e == nil {
		b.Protected = append(b.Protected, netip.PrefixFrom(ip, ip.BitLen()))
	} else if ips, e := net.DefaultResolver.LookupNetIP(lookupCtx, "ip", host); e == nil {
		for _, ip := range ips {
			ip = ip.Unmap()
			b.Protected = append(b.Protected, netip.PrefixFrom(ip, ip.BitLen()))
		}
	}
	conn, e := grpc.NewClient(c.Controller, grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)), grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(treatment.MaxStateBytes), grpc.MaxCallSendMsgSize(treatment.MaxStateBytes)))
	if e != nil {
		return nil, e
	}
	a.conn = conn
	a.client = pb.NewAgentControlServiceClient(conn)
	return a, nil
}
func (a *Agent) Close() error {
	if a.conn == nil {
		return nil
	}
	return a.conn.Close()
}
func (a *Agent) Wake() {
	select {
	case a.wake <- struct{}{}:
	default:
	}
}
func (a *Agent) State() *pb.LocalState {
	s := a.state.Load()
	if s == nil {
		return nil
	}
	return proto.Clone(s).(*pb.LocalState)
}

func (a *Agent) reconcile(ctx context.Context, d *pb.DesiredState) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	s, e := a.Reconciler.Run(ctx, d, false)
	if s != nil {
		a.publish(s)
	}
	if e != nil {
		log.Printf("treatment reconcile: %v", e)
	}
}

func (a *Agent) publish(s *pb.LocalState) {
	index := &exposureSnapshot{revision: s.GetDesired().GetRevision(), held: s.LocallySuspended || s.GetDesired().GetSuspended(), routes: map[netip.Addr]*pb.ApplicationRecord{}}
	for _, v := range s.Records {
		if v.Status == pb.ApplyStatus_APPLY_STATUS_VERIFIED && v.Assignment != nil && v.AppliedAt != nil {
			p, e := netip.ParsePrefix(v.Assignment.Treatment.Destination)
			if e == nil {
				index.routes[p.Addr()] = v
			}
		}
	}
	a.state.Store(s)
	a.exposure.Store(index)
}

func (a *Agent) Run(ctx context.Context) {
	var workers sync.WaitGroup
	workers.Add(1)
	go func() { defer workers.Done(); a.watchRouting(ctx) }()
	defer workers.Wait()
	a.reconcile(ctx, nil)
	watchStarted := false
	for {
		a.reconcile(ctx, nil)
		callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		r := &pb.CheckInRequest{AgentId: a.Config.AgentID, Location: a.Config.Location, Capabilities: a.capabilities(callCtx)}
		if s := a.state.Load(); s != nil {
			r.DesiredRevision = s.GetDesired().GetRevision()
			r.LocallySuspended = s.LocallySuspended
			for _, record := range s.Records {
				v := proto.Clone(record).(*pb.ApplicationRecord)
				v.InstalledRoute = nil
				v.InstalledBootId = ""
				r.Records = append(r.Records, v)
			}
		}
		response, e := a.client.CheckIn(callCtx, r)
		cancel()
		if e != nil {
			log.Printf("treatment check-in: %v", e)
		} else {
			if response.Desired != nil {
				a.reconcile(ctx, response.Desired)
			}
			if !watchStarted {
				watchStarted = true
				workers.Add(1)
				go func() { defer workers.Done(); a.watch(ctx) }()
			}
		}
		jitter, _ := rand.Int(rand.Reader, big.NewInt(6000))
		delay := 27 * time.Second
		if jitter != nil {
			delay += time.Duration(jitter.Int64()) * time.Millisecond
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-a.wake:
			timer.Stop()
		case <-timer.C:
		}
	}
}

func (a *Agent) watch(ctx context.Context) {
	for ctx.Err() == nil {
		revision := uint64(0)
		if s := a.state.Load(); s != nil {
			revision = s.GetDesired().GetRevision()
		}
		stream, e := a.client.WatchDesiredState(ctx, &pb.WatchDesiredStateRequest{AgentId: a.Config.AgentID, AfterRevision: revision})
		if e == nil {
			for {
				_, e = stream.Recv()
				if e != nil {
					break
				}
				a.Wake()
			}
		}
		t := time.NewTimer(5 * time.Second)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
	}
}

func (a *Agent) capabilities(ctx context.Context) *pb.Capabilities {
	c := &pb.Capabilities{BootId: a.Reconciler.BootID, ActuatorEnabled: a.Config.EnableApply}
	var u unix.Utsname
	if unix.Uname(&u) == nil {
		c.KernelRelease = unix.ByteSliceToString(u.Release[:])
	}
	if data, e := os.ReadFile("/proc/sys/net/ipv4/tcp_available_congestion_control"); e == nil {
		c.CongestionAlgorithms = strings.Fields(string(data))
	}
	c.SimpleHostRouting = a.backend.Check(ctx) == nil
	return c
}

func (a *Agent) Stamp(r *flat.XtcpFlatRecord) {
	r.AgentId = a.Config.AgentID
	// Experiment location is certificate-bound, so it must not depend on a
	// second independently configured telemetry label.
	r.Location = a.Config.Location
	r.HostBootId = a.Reconciler.BootID
	r.CollectionId = a.collectionID
	r.TreatmentStateRevision = 0
	r.TreatmentAssignmentId = ""
	r.TreatmentGeneration = 0
	r.TreatmentAppliedTimestampNs = 0
	r.TreatmentAlgorithmExposed = false
	s := a.exposure.Load()
	if s == nil {
		return
	}
	r.TreatmentStateRevision = s.revision
	if s.held || r.NetnsInode != a.namespace {
		return
	}
	raw := r.InetDiagMsgSocketDestination
	if r.InetDiagMsgFamily == unix.AF_INET && len(raw) >= 4 {
		raw = raw[:4]
	}
	ip, ok := netip.AddrFromSlice(raw)
	if !ok {
		return
	}
	if v := s.routes[ip]; v != nil {
		a := v.Assignment
		if a.Mode == pb.AssignmentMode_ASSIGNMENT_MODE_TRIAL && (a.LeaseExpiresAt == nil || !a.LeaseExpiresAt.AsTime().After(time.Now())) {
			return
		}
		r.TreatmentAssignmentId = a.Id
		r.TreatmentGeneration = a.Generation
		r.TreatmentAppliedTimestampNs = v.AppliedAt.AsTime().UnixNano()
		r.TreatmentAlgorithmExposed = a.Treatment.Settings.GetCongestionControl() != "" && r.InetDiagCong == a.Treatment.Settings.GetCongestionControl()
		return
	}
}

func (c Config) String() string {
	return fmt.Sprintf("agent=%s location=%s controller=%s apply=%t", c.AgentID, c.Location, c.Controller, c.EnableApply)
}
