package linkmonitor

import (
	"context"
	"errors"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/linuxio"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/rdmaevents"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/rdmanative"
)

type rdmaEventFactory func(context.Context, rdmaEventSet) (rdmaevents.Source, error)

// rdmaEventSource joins a port event source, verbs or netlink, with the NLDEV
// monitor. With verbs, every NLDEV notification is a Topology hint, as the
// verbs queue reports a device's own fatal error. With netlink, ports is set
// and an UNREGISTER of a device the source holds is Fatal for it, which the
// verbs queue would have reported as DEVICE_FATAL.
type rdmaEventSource struct {
	verbs         rdmaevents.Source
	notifications *linuxio.RDMANotifications
	ports         *rdmanative.PortMap
}

func (s *rdmaEventSource) Covered() bool {
	coverage, ok := s.verbs.(interface{ Covered() bool })
	return !ok || coverage.Covered()
}

func (s *lifecycleSession) openRDMAEvents(ctx context.Context, set rdmaEventSet) (rdmaevents.Source, error) {
	if s.rdmaEventSource != nil {
		return s.rdmaEventSource(ctx, set)
	}
	n, err := linuxio.OpenRDMANotifications(ctx)
	if err != nil {
		return nil, err
	}
	var ports rdmaevents.Source
	if s.rdmaEventsBackend.resolve() == RDMAEventsNetlink {
		ports, err = rdmanative.Open(ctx, set.ports)
	} else {
		ports, err = rdmaevents.Open(ctx, rdmaevents.NewProvider(s.rdmaFiles().root), set.ids)
	}
	if err != nil {
		return nil, errors.Join(err, n.Close())
	}
	source := &rdmaEventSource{verbs: ports, notifications: n}
	if s.rdmaEventsBackend.resolve() == RDMAEventsNetlink {
		source.ports = set.ports
	}
	return source, nil
}

func (s *rdmaEventSource) Run(ctx context.Context, emit func(rdmaevents.Event) bool) error {
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		err := s.notifications.RunInto(child, func(n linuxio.RDMANotification) bool { return emit(s.notification(n)) })
		cancel()
		done <- err
	}()
	err := s.verbs.Run(child, emit)
	cancel()
	return errors.Join(err, <-done)
}

// notification maps one NLDEV notification to the event the schedule sees.
func (s *rdmaEventSource) notification(n linuxio.RDMANotification) rdmaevents.Event {
	if s.ports == nil {
		return rdmaevents.Event{Kind: rdmaevents.Topology}
	}
	var device string
	held := func(index uint32) bool {
		name, ok := s.ports.Device(index)
		device = name
		return ok
	}
	if n.Fatal(held) {
		return rdmaevents.Event{Device: device, Kind: rdmaevents.Fatal, Err: errRDMAUnregistered}
	}
	return rdmaevents.Event{Kind: rdmaevents.Topology}
}

// errRDMAUnregistered is the cause a netlink-backend Fatal carries.
var errRDMAUnregistered = errors.New("linkmonitor: RDMA device unregistered")

func (s *rdmaEventSource) Close() error { return errors.Join(s.verbs.Close(), s.notifications.Close()) }
