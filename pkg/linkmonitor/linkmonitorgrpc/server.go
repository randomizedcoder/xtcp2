// Package linkmonitorgrpc serves linkmonitor.v1.LinkMonitorService from a
// link monitor. It mirrors the prometheus adapter: the core linkmonitor package
// never imports grpc, and the host owns the gRPC server, its listener and its
// access policy (pkg/listenerauth.Policy reads the access class of each RPC
// from the proto).
package linkmonitorgrpc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/bufbuild/protovalidate-go"
	"github.com/prometheus/client_golang/prometheus"
	pb "github.com/randomizedcoder/xtcp2/gen/go/linkmonitor"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Descriptor returns LinkMonitorService's descriptor, for hosts building a
// listenerauth.Policy with listenerauth.ServiceMethods.
func Descriptor() protoreflect.ServiceDescriptor {
	return pb.File_linkmonitor_v1_linkmonitor_proto.Services().ByName("LinkMonitorService")
}

// Monitor is the part of *linkmonitor.Monitor the service uses, so tests can
// substitute a fake.
type Monitor interface {
	Snapshot() linkmonitor.Snapshot
	Submit(linkmonitor.ControlKind) (linkmonitor.ControlTicket, error)
	Await(context.Context, linkmonitor.ControlTicket) error
	ControlProgress(linkmonitor.ControlKind) linkmonitor.ControlProgress
}

// PeerIdentity is implemented by transport AuthInfo that knows more about the
// caller than its address, such as the SO_PEERCRED uid of a Unix socket peer.
type PeerIdentity interface {
	PeerIdentity() string
}

// ErrDisabled is reported, as FailedPrecondition, by a server without a monitor.
var ErrDisabled = errors.New("link monitor disabled")

// Server implements pb.LinkMonitorServiceServer.
type Server struct {
	pb.UnimplementedLinkMonitorServiceServer
	monitor   Monitor
	logger    *slog.Logger
	registry  prometheus.Registerer
	requests  *prometheus.CounterVec
	durations *prometheus.SummaryVec
}

// Option configures a Server.
type Option func(*Server)

// WithLogger sets the audit and error logger. The default discards output.
func WithLogger(logger *slog.Logger) Option {
	return func(s *Server) {
		if logger != nil {
			s.logger = logger
		}
	}
}

// WithRegisterer registers request metrics on reg. Without it the server
// records no metrics.
func WithRegisterer(reg prometheus.Registerer) Option {
	return func(s *Server) { s.registry = reg }
}

// NewServer builds the service. A nil monitor yields a server whose every RPC
// returns FailedPrecondition, so a host can register the service even when the
// monitor is disabled and clients get a clear error.
func NewServer(m Monitor, opts ...Option) (*Server, error) {
	s := &Server{monitor: m, logger: slog.New(slog.DiscardHandler)}
	for _, opt := range opts {
		opt(s)
	}
	if s.registry == nil {
		return s, nil
	}
	s.requests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "go_link_monitor", Subsystem: "grpc", Name: "requests_total",
		Help: "LinkMonitorService requests by method and status code.",
	}, []string{"method", "code"})
	s.durations = prometheus.NewSummaryVec(prometheus.SummaryOpts{
		Namespace: "go_link_monitor", Subsystem: "grpc", Name: "request_duration_seconds",
		Help:       "LinkMonitorService request duration by method.",
		Objectives: map[float64]float64{0.5: 0.05, 0.99: 0.001},
		MaxAge:     10 * time.Minute,
	}, []string{"method"})
	for _, c := range []prometheus.Collector{s.requests, s.durations} {
		if err := s.registry.Register(c); err != nil {
			return nil, fmt.Errorf("register link monitor gRPC metrics: %w", err)
		}
	}
	return s, nil
}

// observe records one finished request. It runs after the response is built.
func (s *Server) observe(method string, start time.Time, err error) {
	if s.requests == nil {
		return
	}
	s.requests.WithLabelValues(method, status.Code(err).String()).Inc()
	s.durations.WithLabelValues(method).Observe(time.Since(start).Seconds())
}

func (s *Server) enabled() error {
	if s.monitor == nil {
		return status.Error(codes.FailedPrecondition, ErrDisabled.Error())
	}
	return nil
}

func validate(msg proto.Message) error {
	if err := protovalidate.Validate(msg); err != nil {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	return nil
}

// GetStatus reports health, counts, baseline and control progress.
func (s *Server) GetStatus(_ context.Context, _ *pb.GetStatusRequest) (resp *pb.GetStatusResponse, err error) {
	defer func(start time.Time) { s.observe("GetStatus", start, err) }(time.Now())
	if err = s.enabled(); err != nil {
		return nil, err
	}
	snap := s.monitor.Snapshot()
	h := snap.Health()
	resp = &pb.GetStatusResponse{
		SnapshotVersion:     snap.Version(),
		Health:              &pb.Health{Running: h.Running, Ready: h.Ready, CollectionHealthy: h.CollectionHealthy, BaselineReady: h.BaselineReady},
		Counts:              counts(snap.Counts()),
		BaselineWriteErrors: snap.BaselineWriteErrors(),
		Resync:              progress(s.monitor.ControlProgress(linkmonitor.ControlResync)),
		Rebaseline:          progress(s.monitor.ControlProgress(linkmonitor.ControlRebaseline)),
	}
	if at, ok := snap.BaselineRecordedAt(); ok {
		resp.BaselineRecordedAt = timestamppb.New(at)
	}
	if at, ok := snap.LastSuccessfulResync(); ok {
		resp.LastSuccessfulResync = timestamppb.New(at)
	}
	return resp, nil
}

func counts(c linkmonitor.LinkCounts) *pb.LinkCounts {
	out := &pb.LinkCounts{}
	if v, ok := c.Current(); ok {
		out.Current = &v
	}
	if v, ok := c.Expected(); ok {
		out.Expected = &v
	}
	if negative, magnitude, ok := c.Delta(); ok && magnitude <= math.MaxInt64 {
		d := int64(magnitude)
		if negative {
			d = -d
		}
		out.Delta = &d
	}
	return out
}

func progress(p linkmonitor.ControlProgress) *pb.ControlProgress {
	return &pb.ControlProgress{Requested: p.Requested, Completed: p.Completed}
}

// nameFilter returns nil when names is empty, meaning "everything".
func nameFilter(names []string) map[string]bool {
	if len(names) == 0 {
		return nil
	}
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	return set
}

// ListInterfaces reports each device's identity, state and policy checks.
func (s *Server) ListInterfaces(_ context.Context, in *pb.ListInterfacesRequest) (resp *pb.ListInterfacesResponse, err error) {
	defer func(start time.Time) { s.observe("ListInterfaces", start, err) }(time.Now())
	if err = s.enabled(); err != nil {
		return nil, err
	}
	if err = validate(in); err != nil {
		return nil, err
	}
	snap := s.monitor.Snapshot()
	want := nameFilter(in.GetNames())
	resp = &pb.ListInterfacesResponse{SnapshotVersion: snap.Version()}
	snap.RangeDevices(func(d linkmonitor.DeviceView) bool {
		if want != nil && !want[d.Name()] {
			return true
		}
		if in.GetEligibleOnly() && d.Eligibility() != linkmonitor.EligibilityIncluded {
			return true
		}
		up, down := d.ObservedTransitions()
		iface := &pb.Interface{
			Name: d.Name(), Identity: d.Identity(), Generation: d.Generation(),
			Eligibility:           eligibility(d.Eligibility()),
			MaximumSpeed:          check(d.MaximumSpeedCheck()),
			MaximumWidth:          check(d.MaximumWidthCheck()),
			FullDuplex:            check(d.FullDuplexCheck()),
			RdmaReadiness:         check(d.RDMAReadinessCheck()),
			MaximumSpeedException: snap.MaximumSpeedException(d),
			UpTransitions:         up, DownTransitions: down,
		}
		if isUp, known := d.Up(); known {
			iface.Up = &isUp
		}
		resp.Interfaces = append(resp.Interfaces, iface)
		return true
	})
	return resp, nil
}

func eligibility(e linkmonitor.Eligibility) pb.Eligibility {
	switch e {
	case linkmonitor.EligibilityIncluded:
		return pb.Eligibility_ELIGIBILITY_INCLUDED
	case linkmonitor.EligibilityExcluded:
		return pb.Eligibility_ELIGIBILITY_EXCLUDED
	default:
		return pb.Eligibility_ELIGIBILITY_UNSPECIFIED
	}
}

func check(c linkmonitor.CheckStatus) pb.CheckStatus {
	switch c {
	case linkmonitor.CheckPass:
		return pb.CheckStatus_CHECK_STATUS_PASS
	case linkmonitor.CheckFail:
		return pb.CheckStatus_CHECK_STATUS_FAIL
	case linkmonitor.CheckNotApplicable:
		return pb.CheckStatus_CHECK_STATUS_NOT_APPLICABLE
	default:
		return pb.CheckStatus_CHECK_STATUS_UNSPECIFIED
	}
}

// ListCollectors reports per-device and host collector diagnostics. Sources
// that were never probed are included with support UNSPECIFIED, as on /metrics.
func (s *Server) ListCollectors(_ context.Context, in *pb.ListCollectorsRequest) (resp *pb.ListCollectorsResponse, err error) {
	defer func(start time.Time) { s.observe("ListCollectors", start, err) }(time.Now())
	if err = s.enabled(); err != nil {
		return nil, err
	}
	if err = validate(in); err != nil {
		return nil, err
	}
	snap := s.monitor.Snapshot()
	want := nameFilter(in.GetNames())
	resp = &pb.ListCollectorsResponse{SnapshotVersion: snap.Version()}
	snap.RangeDevices(func(d linkmonitor.DeviceView) bool {
		if want != nil && !want[d.Name()] {
			return true
		}
		d.RangeCollectors(func(v linkmonitor.CollectorView) bool {
			resp.Collectors = append(resp.Collectors, collector(d.Name(), v))
			return true
		})
		return true
	})
	if want == nil {
		resp.Collectors = append(resp.Collectors, collector("", snap.HostCollector()))
	}
	return resp, nil
}

func collector(iface string, v linkmonitor.CollectorView) *pb.Collector {
	out := &pb.Collector{
		Interface: iface, Name: v.Name(), Support: support(v.Support()), Fresh: v.Fresh(),
		ErrorReason: v.ErrorReason(), Discontinuities: v.Discontinuities(),
	}
	if ok, attempted := v.Success(); attempted {
		out.Success = &ok
	}
	if at, ok := v.LastSuccess(); ok {
		out.LastSuccess = timestamppb.New(at)
	}
	if d, ok := v.Duration(); ok {
		out.Duration = durationpb.New(d)
	}
	v.RangeErrors(func(reason string, count uint64) bool {
		if out.Errors == nil {
			out.Errors = make(map[string]uint64)
		}
		out.Errors[reason] = count
		return true
	})
	v.RangeOmissions(func(reason string, count uint64) bool {
		if out.Omissions == nil {
			out.Omissions = make(map[string]uint64)
		}
		out.Omissions[reason] = count
		return true
	})
	return out
}

func support(s string) pb.Support {
	switch s {
	case "supported":
		return pb.Support_SUPPORT_SUPPORTED
	case "unsupported":
		return pb.Support_SUPPORT_UNSUPPORTED
	case "not_applicable":
		return pb.Support_SUPPORT_NOT_APPLICABLE
	default:
		return pb.Support_SUPPORT_UNSPECIFIED
	}
}

// Rebaseline requests a rebaseline and optionally waits for its durable save.
func (s *Server) Rebaseline(ctx context.Context, in *pb.RebaselineRequest) (resp *pb.RebaselineResponse, err error) {
	defer func(start time.Time) { s.observe("Rebaseline", start, err) }(time.Now())
	ticket, completed, err := s.control(ctx, "Rebaseline", linkmonitor.ControlRebaseline, in, in.GetReason(), in.GetWait())
	if err != nil {
		return nil, err
	}
	snap := s.monitor.Snapshot()
	resp = &pb.RebaselineResponse{Generation: ticket.Generation(), Completed: completed, BaselineWriteErrors: snap.BaselineWriteErrors()}
	if v, ok := snap.Counts().Expected(); ok {
		resp.Expected = &v
	}
	return resp, nil
}

// Resync requests a reconciliation and optionally waits for it.
func (s *Server) Resync(ctx context.Context, in *pb.ResyncRequest) (resp *pb.ResyncResponse, err error) {
	defer func(start time.Time) { s.observe("Resync", start, err) }(time.Now())
	ticket, completed, err := s.control(ctx, "Resync", linkmonitor.ControlResync, in, in.GetReason(), in.GetWait())
	if err != nil {
		return nil, err
	}
	resp = &pb.ResyncResponse{Generation: ticket.Generation(), Completed: completed}
	if at, ok := s.monitor.Snapshot().LastSuccessfulResync(); ok {
		resp.LastSuccessfulResync = timestamppb.New(at)
	}
	return resp, nil
}

// control validates, submits, optionally awaits, and writes the audit record.
func (s *Server) control(ctx context.Context, method string, kind linkmonitor.ControlKind, in proto.Message,
	reason string, wait *durationpb.Duration,
) (ticket linkmonitor.ControlTicket, completed bool, err error) {
	if err = s.enabled(); err != nil {
		return ticket, false, err
	}
	if err = validate(in); err != nil {
		return ticket, false, err
	}
	defer func() {
		s.logger.InfoContext(ctx, "link monitor admin request",
			"rpc", method, "reason", reason, "peer", peerOf(ctx), "wait", wait.AsDuration(),
			"generation", ticket.Generation(), "completed", completed, "code", status.Code(err).String())
	}()
	ticket, err = s.monitor.Submit(kind)
	if err != nil {
		return ticket, false, controlError(err)
	}
	if wait.AsDuration() <= 0 {
		return ticket, false, nil
	}
	waitCtx, cancel := context.WithTimeout(ctx, wait.AsDuration())
	defer cancel()
	switch err = s.monitor.Await(waitCtx, ticket); {
	case err == nil:
		return ticket, true, nil
	case ctx.Err() != nil:
		return ticket, false, status.FromContextError(ctx.Err()).Err()
	case errors.Is(err, context.DeadlineExceeded):
		return ticket, false, status.Errorf(codes.DeadlineExceeded,
			"%s generation %d not completed within %s; baseline write errors: %d",
			kind, ticket.Generation(), wait.AsDuration(), s.monitor.Snapshot().BaselineWriteErrors())
	default:
		return ticket, false, controlError(err)
	}
}

func controlError(err error) error {
	if errors.Is(err, linkmonitor.ErrNotRunning) {
		return status.Error(codes.Unavailable, err.Error())
	}
	return status.Error(codes.Internal, err.Error())
}

// peerOf names the caller for the audit log: transport identity when the
// listener provides one, otherwise the remote address.
func peerOf(ctx context.Context) string {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return "unknown"
	}
	if id, ok := p.AuthInfo.(PeerIdentity); ok {
		return id.PeerIdentity()
	}
	if p.Addr == nil {
		return "unknown"
	}
	return p.Addr.String()
}
