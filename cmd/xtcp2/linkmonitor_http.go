package main

import (
	"log"
	"net/http"
	"sync/atomic"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp"
)

// linkMonitorRoutes serves the embedded link monitor's detail paths on the
// prom listener, and the /ip paths on their own listener when ip_listen is
// set. The listeners start before the daemon and outlive it, so the daemon
// attaches its routes once it has built the monitor; until then, and without
// a monitor, every path is 404.
type linkMonitorRoutes struct {
	// promPath is the core metrics path, set before the listener starts.
	promPath string
	handler  atomic.Pointer[http.Handler]
	// ip serves the /ip listener; nil unless ip_listen is set.
	ip atomic.Pointer[http.Handler]
	// daemon is the running daemon, for SIGUSR1.
	daemon atomic.Pointer[xtcp.XTCP]
}

var linkMonitorHTTP linkMonitorRoutes

func (l *linkMonitorRoutes) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	serveAttached(&l.handler, w, r)
}

// ipRoutes is the /ip listener's handler.
func (l *linkMonitorRoutes) ipRoutes() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveAttached(&l.ip, w, r)
	})
}

// serveAttached serves r with the attached handler, or 404 without one.
func serveAttached(h *atomic.Pointer[http.Handler], w http.ResponseWriter, r *http.Request) {
	if p := h.Load(); p != nil {
		(*p).ServeHTTP(w, r)
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
	ip, err := x.LinkMonitorIPHandler()
	if err != nil {
		return nil, err
	}
	if h != nil {
		l.handler.Store(&h)
	}
	if ip != nil {
		l.ip.Store(&ip)
	}
	l.daemon.Store(x)
	return func() {
		l.handler.Store(nil)
		l.ip.Store(nil)
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
