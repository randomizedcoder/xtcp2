package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestShutdownDeadline(t *testing.T) {
	t.Log("negative/boundary: stalled HTTP handler; expected: drain deadline forces connection closed and returns error")
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	server := &http.Server{
		ReadHeaderTimeout: time.Second,
		Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			close(entered)
			<-release
			close(finished)
		}),
	}
	var lc net.ListenConfig
	listener, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	served, requested := make(chan error, 1), make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	go func() { _, err := fetchPath(t.Context(), listener.Addr().String(), "/metrics"); requested <- err }()
	<-entered
	err = shutdownHTTP(t.Context(), server, 20*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Error("deadline not propagated", err)
	}
	if err := <-requested; err == nil {
		t.Error("stalled connection was not closed")
	}
	close(release)
	<-finished
	if err := <-served; !errors.Is(err, http.ErrServerClosed) {
		t.Error(err)
	}
}
