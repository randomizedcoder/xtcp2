// Package agent is the controller-facing half of a treatment host: the mTLS
// check-in loop, the desired-state watch and telemetry stamping, over the
// offline Host from package local. It is a library with two front ends:
// cmd/xtcp2-treatment-agent runs it standalone and cmd/xtcp2 embeds it when
// built with the treatment tag.
package agent

import (
	"context"
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/netip"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	flat "github.com/randomizedcoder/xtcp2/gen/go/xtcp_flat_record"
	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/attribution"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/identity"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/local"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/reconcile"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/wire"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// Config is the shared host configuration file; see local.Config.
type Config = local.Config

// LoadConfig reads the shared host configuration file.
func LoadConfig(path string) (Config, error) { return local.LoadConfig(path) }

// Agent is one host's connection to the controller.
type Agent struct {
	Config     Config
	Host       *local.Host
	Reconciler *reconcile.Reconciler
	client     pb.AgentControlServiceClient
	conn       *grpc.ClientConn
	state      atomic.Pointer[reconcile.State]
	stamper    attribution.Stamper
	wake       chan struct{}
	// collectionID is a per-process nonce; see attribution.Identity.
	collectionID string
}

// newLocal builds the offline part. A nil identity inspects the host.
func newLocal(c Config, id *local.Identity) (*Agent, error) {
	h, err := local.New(c, id)
	if err != nil {
		return nil, err
	}
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	a := &Agent{Config: h.Config, Host: h, Reconciler: h.Reconciler, wake: make(chan struct{}, 1), collectionID: hex.EncodeToString(nonce[:])}
	// The agent owns the nonce: it goes into every journal header it writes,
	// where a telemetry-only daemon reads it, and into every check-in.
	h.Reconciler.CollectionID = a.collectionID
	a.publish(nil)
	return a, nil
}

// New validates identity before constructing any mutation-capable path. DNS
// resolution is only needed to add management destinations to the exclusions;
// deployments should list stable management prefixes for offline reboot use.
func New(ctx context.Context, c Config) (*Agent, error) {
	tlsConfig, e := identity.TLSConfig(c.Certificate, c.PrivateKey, c.CA, c.ServerName, false)
	if e != nil {
		return nil, e
	}
	cert, e := x509.ParseCertificate(tlsConfig.Certificates[0].Certificate[0])
	if e != nil {
		return nil, e
	}
	id, e := identity.FromCertificate(cert)
	if e != nil {
		return nil, e
	}
	if id.Role != identity.RoleAgent || id.ID != c.AgentID || id.Location != c.Location {
		return nil, fmt.Errorf("agent config names %s/%s but the certificate's URI SAN is %s/%s/%s; location and agent_id are certificate-bound", c.Location, c.AgentID, id.Role, id.Location, id.ID)
	}
	a, e := newLocal(c, nil)
	if e != nil {
		return nil, e
	}
	host, _, e := net.SplitHostPort(c.Controller)
	if e != nil {
		return nil, e
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if ip, e := netip.ParseAddr(host); e == nil {
		a.Host.Protect(netip.PrefixFrom(ip, ip.BitLen()))
	} else if ips, e := net.DefaultResolver.LookupNetIP(lookupCtx, "ip", host); e == nil {
		for _, ip := range ips {
			ip = ip.Unmap()
			a.Host.Protect(netip.PrefixFrom(ip, ip.BitLen()))
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

// Close releases the controller connection.
func (a *Agent) Close() error {
	if a.conn == nil {
		return nil
	}
	return a.conn.Close()
}

// Wake asks the run loop to check in now; concurrent wakes coalesce.
func (a *Agent) Wake() {
	select {
	case a.wake <- struct{}{}:
	default:
	}
}

// State returns a copy of the last journal snapshot, nil before the first pass.
func (a *Agent) State() *reconcile.State { return a.state.Load().Clone() }

func (a *Agent) identity() attribution.Identity {
	return attribution.Identity{AgentID: a.Config.AgentID, Location: a.Config.Location, BootID: a.Host.Identity.BootID, CollectionID: a.collectionID, NetnsInode: a.Host.Identity.NetnsInode}
}

func (a *Agent) reconcile(ctx context.Context, d *treatment.DesiredState) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	s, e := a.Reconciler.Run(ctx, d, reconcile.ModeFull)
	if s != nil {
		a.publish(s)
	}
	if e != nil {
		log.Printf("treatment reconcile: %v", e)
	}
}

func (a *Agent) publish(s *reconcile.State) {
	if s != nil {
		a.state.Store(s)
	}
	a.stamper.Publish(attribution.Build(s, a.identity(), true))
}

// Run checks in, reconciles and watches until ctx ends.
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
		report := &treatment.AgentReport{AgentID: a.Config.AgentID, Location: a.Config.Location, Capabilities: a.capabilities(callCtx)}
		if s := a.state.Load(); s != nil {
			report.DesiredRevision = s.Revision()
			report.LocallySuspended = s.LocallySuspended
			report.Records = s.Reports()
		}
		response, e := a.client.CheckIn(callCtx, wire.Report(report))
		cancel()
		if e != nil {
			log.Printf("treatment check-in: %v", e)
		} else {
			if response.Desired != nil {
				if d, e := wire.ParseDesired(response.Desired); e != nil {
					log.Printf("treatment check-in: invalid desired state: %v", e)
				} else {
					a.reconcile(ctx, d)
				}
			}
			if !watchStarted {
				watchStarted = true
				workers.Add(1)
				go func() { defer workers.Done(); a.watch(ctx) }()
			}
		}
		// Without entropy the check-in simply runs unjittered.
		delay := 27 * time.Second
		if jitter, jerr := rand.Int(rand.Reader, big.NewInt(6000)); jerr == nil {
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
		stream, e := a.client.WatchDesiredState(ctx, &pb.WatchDesiredStateRequest{AgentId: a.Config.AgentID, AfterRevision: a.state.Load().Revision()})
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

func (a *Agent) capabilities(ctx context.Context) treatment.Capabilities {
	c := treatment.Capabilities{BootID: a.Host.Identity.BootID, CollectionID: a.collectionID, ActuatorEnabled: a.Config.EnableApply}
	var u unix.Utsname
	if unix.Uname(&u) == nil {
		c.KernelRelease = unix.ByteSliceToString(u.Release[:])
	}
	if data, e := os.ReadFile("/proc/sys/net/ipv4/tcp_available_congestion_control"); e == nil {
		c.CongestionAlgorithms = strings.Fields(string(data))
	}
	c.SimpleHostRouting = a.Host.SimpleHostRouting(ctx)
	return c
}

// Stamp is the hot-path telemetry hook; see attribution.Index.Stamp.
func (a *Agent) Stamp(r *flat.XtcpFlatRecord) { a.stamper.Stamp(r) }
