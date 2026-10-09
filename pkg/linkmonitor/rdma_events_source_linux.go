package linkmonitor

import (
	"context"
	"errors"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/linuxio"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/rdmaevents"
)

type rdmaEventFactory func(context.Context, []rdmaevents.Identity) (rdmaevents.Source, error)

type rdmaEventSource struct {
	verbs         rdmaevents.Source
	notifications *linuxio.RDMANotifications
}

func (s *rdmaEventSource) Covered() bool {
	coverage, ok := s.verbs.(interface{ Covered() bool })
	return !ok || coverage.Covered()
}

func (s *lifecycleSession) openRDMAEvents(ctx context.Context, ids []rdmaevents.Identity) (rdmaevents.Source, error) {
	if s.rdmaEventSource != nil {
		return s.rdmaEventSource(ctx, ids)
	}
	n, err := linuxio.OpenRDMANotifications(ctx)
	if err != nil {
		return nil, err
	}
	v, err := rdmaevents.Open(ctx, rdmaevents.NewProvider(s.rdmaFiles().root), ids)
	if err != nil {
		return nil, errors.Join(err, n.Close())
	}
	return &rdmaEventSource{verbs: v, notifications: n}, nil
}

func (s *rdmaEventSource) Run(ctx context.Context, emit func(rdmaevents.Event) bool) error {
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		err := s.notifications.Run(child, func() bool { return emit(rdmaevents.Event{Kind: rdmaevents.Topology}) })
		cancel()
		done <- err
	}()
	err := s.verbs.Run(child, emit)
	cancel()
	return errors.Join(err, <-done)
}

func (s *rdmaEventSource) Close() error { return errors.Join(s.verbs.Close(), s.notifications.Close()) }
