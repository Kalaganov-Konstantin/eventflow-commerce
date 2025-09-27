package httpserver

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
)

func waitForAddr(t *testing.T, srv *Server) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		addr := srv.Addr()
		if addr != "" && addr != "127.0.0.1:0" {
			return addr
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("server did not start listening in time")
	return ""
}

func TestServer_StartAndStop(t *testing.T) {
	srv := New(Options{
		Addr: "127.0.0.1:0",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
		Logger: zap.NewNop(),
	})

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Start() }()

	addr := waitForAddr(t, srv)

	resp, err := http.Get("http://" + addr + "/")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, resp.StatusCode)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := srv.Stop(ctx); err != nil {
		t.Fatalf("stop failed: %v", err)
	}

	if err := <-errCh; err != nil && err != http.ErrServerClosed {
		t.Fatalf("start returned unexpected error: %v", err)
	}
}

func TestServer_StopWaitsForInFlightRequest(t *testing.T) {
	handlerEntered := make(chan struct{})
	release := make(chan struct{})
	var handlerFinished atomic.Bool

	srv := New(Options{
		Addr: "127.0.0.1:0",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			close(handlerEntered)
			<-release
			handlerFinished.Store(true)
			w.WriteHeader(http.StatusOK)
		}),
		Logger: zap.NewNop(),
	})

	go func() { _ = srv.Start() }()
	addr := waitForAddr(t, srv)

	reqDone := make(chan error, 1)
	go func() {
		resp, err := http.Get("http://" + addr + "/")
		if err == nil {
			_ = resp.Body.Close()
		}
		reqDone <- err
	}()

	<-handlerEntered

	stopDone := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		stopDone <- srv.Stop(ctx)
	}()

	select {
	case <-stopDone:
		t.Fatal("Stop returned before the in-flight request completed")
	case <-time.After(100 * time.Millisecond):
	}

	close(release)

	if err := <-stopDone; err != nil {
		t.Fatalf("stop failed: %v", err)
	}
	if !handlerFinished.Load() {
		t.Fatal("handler did not finish before Stop returned")
	}
	if err := <-reqDone; err != nil {
		t.Fatalf("request failed: %v", err)
	}
}

func TestServer_DefaultTimeouts(t *testing.T) {
	srv := New(Options{Addr: "127.0.0.1:0", Handler: http.NotFoundHandler()})

	if srv.httpServer.ReadTimeout != defaultReadTimeout {
		t.Errorf("expected read timeout %v, got %v", defaultReadTimeout, srv.httpServer.ReadTimeout)
	}
	if srv.httpServer.WriteTimeout != defaultWriteTimeout {
		t.Errorf("expected write timeout %v, got %v", defaultWriteTimeout, srv.httpServer.WriteTimeout)
	}
	if srv.httpServer.IdleTimeout != defaultIdleTimeout {
		t.Errorf("expected idle timeout %v, got %v", defaultIdleTimeout, srv.httpServer.IdleTimeout)
	}
}

func TestServer_CustomTimeouts(t *testing.T) {
	srv := New(Options{
		Addr:         "127.0.0.1:0",
		Handler:      http.NotFoundHandler(),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 6 * time.Second,
		IdleTimeout:  7 * time.Second,
	})

	if srv.httpServer.ReadTimeout != 5*time.Second {
		t.Errorf("expected custom read timeout, got %v", srv.httpServer.ReadTimeout)
	}
	if srv.httpServer.WriteTimeout != 6*time.Second {
		t.Errorf("expected custom write timeout, got %v", srv.httpServer.WriteTimeout)
	}
	if srv.httpServer.IdleTimeout != 7*time.Second {
		t.Errorf("expected custom idle timeout, got %v", srv.httpServer.IdleTimeout)
	}
}

func TestServer_ListenError(t *testing.T) {
	srv := New(Options{Addr: "invalid-address", Handler: http.NotFoundHandler(), Logger: zap.NewNop()})

	if err := srv.Start(); err == nil {
		t.Fatal("expected error from Start with an invalid address")
	}
}
