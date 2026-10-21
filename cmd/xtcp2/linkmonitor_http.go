package main

import (
	"log"
	"net/http"
	"sync/atomic"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp"
)

// linkMonitorRoutes serves the embedded link monitor's detail paths on the
// prom listener. The listener starts before the daemon and outlives it, so
// the daemon attaches its routes once it has built the monitor; until then,
// and without a monitor, every path is 404.
type linkMonitorRoutes struct {
	// promPath is the core metrics path, set before the listener starts.
	promPath string
	handler  atomic.Pointer[http.Handler]
	// daemon is the running daemon, for SIGUSR1.
	daemon atomic.Pointer[xtcp.XTCP]
}

var linkMonitorHTTP linkMonitorRoutes

func (l *linkMonitorRoutes) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h := l.handler.Load(); h != nil {
		(*h).ServeHTTP(w, r)
		return
	}
	http.NotFound(w, r)
}

// attach serves x's link monitor routes and directs SIGUSR1 at x. It returns
// the detach function, which runs when the daemon has stopped.
func (l *linkMonitorRoutes) attach(x *xtcp.XTCP) (func(), error) {
	h, err := x.LinkMonitorHandler(l.promPath)
	if err != nil {
		return nil, err
	}
	if h != nil {
		l.handler.Store(&h)
	}
	l.daemon.Store(x)
	return func() {
		l.handler.Store(nil)
		l.daemon.Store(nil)
	}, nil
}

// rebaseline asks the running daemon's link monitor to relearn the expected
// up-link count, as SIGUSR1 does for go-link-monitor.
func (l *linkMonitorRoutes) rebaseline() {
	x := l.daemon.Load()
	if x == nil {
		log.Printf("SIGUSR1: no daemon running; rebaseline ignored")
		return
	}
	if err := x.RequestLinkMonitorRebaseline(); err != nil {
		log.Printf("SIGUSR1: rebaseline request rejected: %v", err)
		return
	}
	log.Printf("SIGUSR1: rebaseline requested; durable completion is asynchronous")
}
