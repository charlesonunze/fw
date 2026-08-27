package fw

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestShutdownServersForcesHTTPConnectionsClosedAfterDeadline(t *testing.T) {
	requestStarted := make(chan struct{})
	requestStopped := make(chan struct{})
	server := &http.Server{
		Handler: http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			close(requestStarted)
			<-r.Context().Done()
			close(requestStopped)
		}),
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	t.Cleanup(func() { _ = server.Close() })
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- server.Serve(listener)
	}()

	clientDone := make(chan error, 1)
	client := &http.Client{Timeout: time.Second}
	go func() {
		response, requestErr := client.Get("http://" + listener.Addr().String())
		if response != nil {
			_ = response.Body.Close()
		}
		clientDone <- requestErr
	}()

	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("HTTP request did not reach server")
	}

	app := New(WithLogger(discardLogger{}))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	app.shutdownServersWithContext(ctx, server)

	select {
	case <-requestStopped:
	case <-time.After(time.Second):
		t.Fatal("forced HTTP close did not cancel active request")
	}
	select {
	case serveErr := <-serveDone:
		if !errors.Is(serveErr, http.ErrServerClosed) {
			t.Fatalf("Serve() error = %v, want http.ErrServerClosed", serveErr)
		}
	case <-time.After(time.Second):
		t.Fatal("HTTP server remained blocked after forced close")
	}
	select {
	case <-clientDone:
	case <-time.After(time.Second):
		t.Fatal("HTTP client remained blocked after forced close")
	}
}
