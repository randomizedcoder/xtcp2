package rdmaevents

import (
	"context"
	"encoding/binary"
	"errors"
	"sync"

	"golang.org/x/sys/unix"
)

type pollSource struct {
	epoll, wake int
	handles     map[int32]Handle
	names       map[int32]string
	faults      []Event
	mu          sync.Mutex
	closed      bool
	closeErr    error
}

// Covered is queried after acquisition, before Run takes ownership.
func (s *pollSource) Covered() bool { return len(s.faults) == 0 }

// Open acquires a fixed context set. Per-HCA acquisition failures become fatal
// diagnostics while other HCAs remain subscribed. Invalid input unwinds the set.
func Open(ctx context.Context, provider Provider, identities []Identity) (Source, error) {
	if err := validateIdentities(identities); err != nil {
		return nil, err
	}
	s, err := newPollSource()
	if err != nil {
		return nil, err
	}
	for _, id := range identities {
		if err := s.add(ctx, provider, id); err != nil {
			if ctx.Err() != nil {
				return nil, errors.Join(err, s.dispose())
			}
			s.faults = append(s.faults, Event{Device: id.Name, Kind: Fatal, Err: err})
		}
	}
	return s, nil
}

func validateIdentities(identities []Identity) error {
	if len(identities) > Limit {
		return ErrLimit
	}
	seen := make(map[string]bool, len(identities))
	for _, id := range identities {
		if seen[id.Name] || id.Name == "" || len(id.Name) > 63 || len(id.Hardware) > 1024 {
			return ErrIdentity
		}
		seen[id.Name] = true
	}
	return nil
}

func newPollSource() (*pollSource, error) {
	ep, err := unix.EpollCreate1(unix.EPOLL_CLOEXEC)
	if err != nil {
		return nil, err
	}
	wake, err := unix.Eventfd(0, unix.EFD_NONBLOCK|unix.EFD_CLOEXEC)
	if err != nil {
		return nil, errors.Join(err, unix.Close(ep))
	}
	s := &pollSource{epoll: ep, wake: wake, handles: make(map[int32]Handle), names: make(map[int32]string)}
	err = unix.EpollCtl(ep, unix.EPOLL_CTL_ADD, wake, &unix.EpollEvent{Events: unix.EPOLLIN, Fd: 0})
	if err != nil {
		return nil, errors.Join(err, s.dispose())
	}
	return s, nil
}

func (s *pollSource) add(ctx context.Context, provider Provider, id Identity) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	h, err := provider.Open(ctx, id)
	if err != nil {
		return err
	}
	// Tokens, not descriptor numbers, identify readiness. Tokens are never reused
	// within this source even when a fatal context releases its descriptor.
	token := int32(len(s.handles) + 1)
	err = unix.EpollCtl(s.epoll, unix.EPOLL_CTL_ADD, h.FD(), &unix.EpollEvent{Events: unix.EPOLLIN, Fd: token})
	if err != nil {
		return errors.Join(err, h.Close())
	}
	s.handles[token] = h
	s.names[token] = id.Name
	return ctx.Err()
}

// Close requests termination; Run owns final destruction and joins cancellation
// callbacks before closing eventfd. It never races ibv_close_device with Next.
func (s *pollSource) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return s.closeErr
	}
	var data [8]byte
	binary.NativeEndian.PutUint64(data[:], 1)
	for {
		_, err := unix.Write(s.wake, data[:])
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if errors.Is(err, unix.EAGAIN) {
			return nil
		}
		return err
	}
}

func (s *pollSource) Run(ctx context.Context, emit func(Event) bool) (err error) {
	done := make(chan error, 1)
	stop := context.AfterFunc(ctx, func() { done <- s.Close() })
	defer func() {
		if !stop() {
			err = errors.Join(err, <-done)
		}
		err = errors.Join(err, s.dispose())
	}()
	var ready [Batch]unix.EpollEvent
	for _, event := range s.faults {
		if !emit(event) {
			return ErrLost
		}
	}
	s.faults = nil
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := unix.EpollWait(s.epoll, ready[:], -1)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return err
		}
		for i := range n {
			if ready[i].Fd == 0 {
				return ctx.Err()
			}
			if err := s.drain(ready[i], emit); err != nil {
				return err
			}
		}
	}
}

func (s *pollSource) drain(ready unix.EpollEvent, emit func(Event) bool) error {
	h := s.handles[ready.Fd]
	if h == nil {
		return nil
	}
	for range Batch {
		event, err := h.Next()
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if errors.Is(err, unix.EAGAIN) {
			break
		}
		if err != nil {
			return s.retire(ready.Fd, err, emit)
		}
		if !emit(event) {
			return ErrLost
		}
		if event.Kind == Fatal {
			return s.retire(ready.Fd, nil, emit)
		}
	}
	if ready.Events&(unix.EPOLLERR|unix.EPOLLHUP) != 0 {
		return s.retire(ready.Fd, ErrLost, emit)
	}
	return nil
}

func (s *pollSource) retire(token int32, cause error, emit func(Event) bool) error {
	h := s.handles[token]
	delete(s.handles, token)
	name := s.names[token]
	delete(s.names, token)
	closeErr := errors.Join(unix.EpollCtl(s.epoll, unix.EPOLL_CTL_DEL, h.FD(), nil), h.Close())
	// Retain one bounded cleanup diagnostic; healthy descriptors keep running.
	if closeErr != nil {
		s.closeErr = closeErr
	}
	if cause != nil && !emit(Event{Device: name, Kind: Fatal, Err: errors.Join(cause, closeErr)}) {
		return ErrLost
	}
	return nil
}

func (s *pollSource) dispose() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return s.closeErr
	}
	s.closed = true
	for _, h := range s.handles {
		s.closeErr = errors.Join(s.closeErr, unix.EpollCtl(s.epoll, unix.EPOLL_CTL_DEL, h.FD(), nil), h.Close())
	}
	s.closeErr = errors.Join(s.closeErr, unix.Close(s.wake), unix.Close(s.epoll))
	return s.closeErr
}
