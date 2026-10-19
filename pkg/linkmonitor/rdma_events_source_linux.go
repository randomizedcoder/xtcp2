package linkmonitor

import (
	"context"
	"errors"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/linuxio"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/rdmaevents"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/rdmanative"
)

type rdmaEventFactory func(context.Context, rdmaEventSet) (rdmaevents.Source, error)

// rdmaEventSource joins the netlink port event source with the NLDEV
// monitor. An UNREGISTER of a device the port map holds is Fatal for it;
// every other NLDEV notification is a Topology hint.
type rdmaEventSource struct {
	links         rdmaevents.Source
	notifications *linuxio.RDMANotifications
	ports         *rdmanative.PortMap
}

func (s *lifecycleSession) openRDMAEvents(ctx context.Context, set rdmaEventSet) (rdmaevents.Source, error) {
	if s.rdmaEventSource != nil {
		return s.rdmaEventSource(ctx, set)
	}
	n, err := linuxio.OpenRDMANotifications(ctx)
	if err != nil {
		return nil, err
	}
	links, err := rdmanative.Open(ctx, set.ports)
	if err != nil {
		return nil, errors.Join(err, n.Close())
	}
	return &rdmaEventSource{links: links, notifications: n, ports: set.ports}, nil
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
	err := s.links.Run(child, emit)
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

// errRDMAUnregistered is the cause an UNREGISTER Fatal carries.
var errRDMAUnregistered = errors.New("linkmonitor: RDMA device unregistered")

func (s *rdmaEventSource) Close() error { return errors.Join(s.links.Close(), s.notifications.Close()) }
