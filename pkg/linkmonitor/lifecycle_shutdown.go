package linkmonitor

import (
	"context"
	"errors"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

const shutdownGrace = 5 * time.Second

type cleanupCompletion struct {
	err   error
	saved *model.SaveResult
}

// shutdown starts exactly one cleanup task. It never transfers publication
// ownership: after timeout that task can only join, close and log failures.
func (s *lifecycleSession) shutdown(m *Monitor, resources *lifecycleResources, ready <-chan struct{}, life *lifecycle) error {
	timer := s.clock.NewTimer(shutdownGrace)
	defer timer.Stop()
	done := make(chan cleanupCompletion, 1)
	cleaned := make(chan struct{})
	s.cleaned = cleaned
	go func() {
		<-ready
		result := cleanupCompletion{err: resources.join()}
		if resources.storage != nil {
			select {
			case saved := <-resources.storage.results:
				result.saved = &saved
				if saved.Err != nil || saved.Outcome != model.SaveDurable {
					m.logger.Error("baseline save completed during cleanup", "outcome", saved.Outcome, "error", saved.Err)
				}
			default:
			}
		}
		result.err = errors.Join(result.err, s.store.Close())
		if result.err != nil {
			m.logger.Error("monitor cleanup failed", "error", result.err)
		}
		close(cleaned)
		done <- result
	}()
	select {
	case result := <-done:
		return errors.Join(result.err, life.finish(result.saved))
	case <-timer.C():
		return ErrShutdownIncomplete
	}
}

func (r *lifecycleResources) join() error {
	if r.pool != nil {
		r.pool.stop()
		<-r.pool.done
	}
	if r.inventory != nil {
		r.inventory.stop()
		<-r.inventory.done
	}
	if r.events != nil {
		r.events.stop()
		<-r.events.done
	}
	if r.storage != nil {
		r.storage.cancel()
		<-r.storage.done
	}
	err := r.err
	if err == context.Canceled || err == context.DeadlineExceeded {
		err = nil
	}
	if r.pool != nil {
		err = errors.Join(err, r.pool.closeError())
	}
	if r.inventory != nil {
		err = errors.Join(err, r.inventory.closeErr)
	}
	if r.events != nil {
		err = errors.Join(err, r.events.closeErr)
	}
	return err
}

func (l *lifecycle) finish(result *model.SaveResult) error {
	if l == nil {
		return nil
	}
	if result != nil {
		if err := l.saved(*result); err != nil {
			return err
		}
	}
	return l.publish(l.coordinator.scheduler.clock.Now())
}
