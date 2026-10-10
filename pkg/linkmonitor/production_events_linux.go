package linkmonitor

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/linuxio"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/netlink"
	"golang.org/x/sys/unix"
)

type productionEvents struct {
	route     productionEventConn
	optional  func(context.Context, func(model.Event) bool) error
	namespace uint64
	clock     model.Clock
	logger    *slog.Logger
	resync    time.Duration
}

type productionEventConn interface {
	Receive(context.Context, func([]byte) error) (int, error)
	Close() error
}

func openProductionEvents(ctx context.Context, namespace uint64, clock model.Clock, logger *slog.Logger, resync time.Duration) (eventSubscription, error) {
	route, err := netlink.Open(ctx, unix.NETLINK_ROUTE, []uint32{unix.RTNLGRP_LINK})
	if err != nil {
		return eventSubscription{}, err
	}
	return eventSubscription{source: &productionEvents{route: route, namespace: namespace, clock: clock, logger: logger, resync: resync}}, nil
}

func (s *productionEvents) Close() error { return s.route.Close() }

func (s *productionEvents) Run(ctx context.Context, emit func(model.Event) bool) error {
	child, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); s.genericLoop(child, emit) }()
	err := s.routeLoop(child, emit)
	cancel()
	<-done
	return err
}

func (s *productionEvents) routeLoop(ctx context.Context, emit func(model.Event) bool) error {
	for {
		_, err := s.route.Receive(ctx, func(data []byte) error {
			return decodeRouteEvents(data, s.namespace, s.clock.Now(), emit)
		})
		if err != nil {
			return err
		}
	}
}

// A denied/missing optional subscription never tears down working route events.
// Periodic renewal also rediscovers families after unregister/register changes.
func (s *productionEvents) genericLoop(ctx context.Context, emit func(model.Event) bool) {
	backoff := time.Second
	read := s.optional
	if read == nil {
		read = s.genericAttempt
	}
	for ctx.Err() == nil {
		attempt, cancel := context.WithTimeout(ctx, s.resync)
		err := read(attempt, emit)
		expired := errors.Is(attempt.Err(), context.DeadlineExceeded)
		cancel()
		if ctx.Err() != nil {
			return
		}
		if !emit(model.Event{Kind: model.EventResync}) {
			return
		}
		if expired {
			backoff = time.Second
			continue
		}
		s.logger.Warn("ethtool notification coverage unavailable", "error", err)
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		backoff = min(2*backoff, 30*time.Second)
	}
}

func (s *productionEvents) genericAttempt(ctx context.Context, emit func(model.Event) bool) error {
	client, err := linuxio.NewClient(unix.NETLINK_GENERIC)
	if err != nil {
		return err
	}
	family, err := client.DiscoverFamily(ctx, "ethtool")
	if err = errors.Join(err, client.Close()); err != nil {
		return err
	}
	group, ok := family.Group("monitor")
	if !ok {
		return unix.EOPNOTSUPP
	}
	conn, err := netlink.Open(ctx, unix.NETLINK_GENERIC, []uint32{group})
	if err != nil {
		return err
	}
	err = receiveEthtoolEvents(ctx, conn, family.ID(), s.namespace, emit)
	return errors.Join(err, conn.Close())
}

func receiveEthtoolEvents(ctx context.Context, conn *netlink.Conn, family uint16, namespace uint64, emit func(model.Event) bool) error {
	if !emit(model.Event{Kind: model.EventResync}) {
		return unix.ENOBUFS
	}
	for {
		_, err := conn.Receive(ctx, func(data []byte) error {
			return decodeEthtoolEvents(data, family, namespace, emit)
		})
		if err != nil {
			return err
		}
	}
}
