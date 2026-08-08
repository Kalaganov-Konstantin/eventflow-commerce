package client

import (
	"context"
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	apperrors "github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/errors"
	"github.com/google/uuid"
)

func testItems() []ReserveItem {
	return []ReserveItem{{ProductID: uuid.New(), Quantity: 2}}
}

func TestInventoryClient_Reserve(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || r.URL.Path != "/api/v1/inventory/reservations" {
				t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"order_id":"` + uuid.New().String() + `","items":[]}`))
		}))
		defer srv.Close()

		c := NewInventoryClient(srv.URL, time.Second)
		if err := c.Reserve(context.Background(), uuid.New(), testItems()); err != nil {
			t.Fatalf("Reserve() error = %v", err)
		}
	})

	t.Run("insufficient inventory", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"code":"INSUFFICIENT_INVENTORY","message":"Insufficient inventory","details":"Product x: requested 2, available 1"}`))
		}))
		defer srv.Close()

		c := NewInventoryClient(srv.URL, time.Second)
		err := c.Reserve(context.Background(), uuid.New(), testItems())

		var appErr *apperrors.AppError
		if !stderrors.As(err, &appErr) {
			t.Fatalf("Reserve() error = %v, want *AppError", err)
		}
		if appErr.Code != "INSUFFICIENT_INVENTORY" {
			t.Errorf("Code = %v, want INSUFFICIENT_INVENTORY", appErr.Code)
		}
		if appErr.HTTPCode != http.StatusConflict {
			t.Errorf("HTTPCode = %v, want %v", appErr.HTTPCode, http.StatusConflict)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(100 * time.Millisecond)
			w.WriteHeader(http.StatusCreated)
		}))
		defer srv.Close()

		c := NewInventoryClient(srv.URL, 20*time.Millisecond)
		err := c.Reserve(context.Background(), uuid.New(), testItems())

		var appErr *apperrors.AppError
		if !stderrors.As(err, &appErr) {
			t.Fatalf("Reserve() error = %v, want *AppError", err)
		}
		if appErr.Code != "INVENTORY_SERVICE_TIMEOUT" {
			t.Errorf("Code = %v, want INVENTORY_SERVICE_TIMEOUT", appErr.Code)
		}
		if appErr.HTTPCode != http.StatusGatewayTimeout {
			t.Errorf("HTTPCode = %v, want %v", appErr.HTTPCode, http.StatusGatewayTimeout)
		}
	})

	t.Run("internal server error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("boom"))
		}))
		defer srv.Close()

		c := NewInventoryClient(srv.URL, time.Second)
		err := c.Reserve(context.Background(), uuid.New(), testItems())

		var appErr *apperrors.AppError
		if !stderrors.As(err, &appErr) {
			t.Fatalf("Reserve() error = %v, want *AppError", err)
		}
		if appErr.Code != "INVENTORY_ERROR" {
			t.Errorf("Code = %v, want INVENTORY_ERROR", appErr.Code)
		}
		if appErr.HTTPCode != http.StatusInternalServerError {
			t.Errorf("HTTPCode = %v, want %v", appErr.HTTPCode, http.StatusInternalServerError)
		}
	})

	t.Run("connection refused", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		srv.Close() // nothing listens on srv.URL after this

		c := NewInventoryClient(srv.URL, time.Second)
		err := c.Reserve(context.Background(), uuid.New(), testItems())

		var appErr *apperrors.AppError
		if !stderrors.As(err, &appErr) {
			t.Fatalf("Reserve() error = %v, want *AppError", err)
		}
		if appErr.Code != "INVENTORY_SERVICE_UNAVAILABLE" {
			t.Errorf("Code = %v, want INVENTORY_SERVICE_UNAVAILABLE", appErr.Code)
		}
		if appErr.HTTPCode != http.StatusServiceUnavailable {
			t.Errorf("HTTPCode = %v, want %v", appErr.HTTPCode, http.StatusServiceUnavailable)
		}
	})
}

func TestInventoryClient_Release(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		orderID := uuid.New()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodDelete || r.URL.Path != "/api/v1/inventory/reservations/"+orderID.String() {
				t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			}
			w.WriteHeader(http.StatusNoContent)
		}))
		defer srv.Close()

		c := NewInventoryClient(srv.URL, time.Second)
		if err := c.Release(context.Background(), orderID); err != nil {
			t.Fatalf("Release() error = %v", err)
		}
	})

	t.Run("propagates a service failure", func(t *testing.T) {
		var calls int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			atomic.AddInt32(&calls, 1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"code":"BAD_REQUEST","message":"invalid order id"}`))
		}))
		defer srv.Close()

		c := NewInventoryClient(srv.URL, time.Second)
		err := c.Release(context.Background(), uuid.New())

		var appErr *apperrors.AppError
		if !stderrors.As(err, &appErr) {
			t.Fatalf("Release() error = %v, want *AppError", err)
		}
		if appErr.Code != "BAD_REQUEST" {
			t.Errorf("Code = %v, want BAD_REQUEST", appErr.Code)
		}
		if got := atomic.LoadInt32(&calls); got != 1 {
			t.Errorf("calls = %d, want 1 (a rejected request must not be retried)", got)
		}
	})

	t.Run("retries a server error then succeeds", func(t *testing.T) {
		orderID := uuid.New()
		var calls int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if atomic.AddInt32(&calls, 1) < 2 {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		}))
		defer srv.Close()

		c := NewInventoryClient(srv.URL, time.Second)
		if err := c.Release(context.Background(), orderID); err != nil {
			t.Fatalf("Release() error = %v", err)
		}
		if got := atomic.LoadInt32(&calls); got != 2 {
			t.Errorf("calls = %d, want 2", got)
		}
	})
}

func TestInventoryClient_Release_BuildRequestError(t *testing.T) {
	// A control character in the base URL makes http.NewRequestWithContext fail, which is also
	// the only way to drive isRetryableInventoryError through its default, non-AppError branch.
	c := NewInventoryClient("http://\x7f", time.Second)

	err := c.Release(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected error, got none")
	}
	if !strings.Contains(err.Error(), "build inventory request") {
		t.Errorf("error = %v, want it to mention building the request", err)
	}
}

func TestInventoryClient_Release_CircuitBreakerSkipsBackendWhileOpen(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewInventoryClient(srv.URL, time.Second)

	var lastErr error
	var appErr *apperrors.AppError
	for i := 0; i < 5; i++ {
		lastErr = c.Release(context.Background(), uuid.New())
		if stderrors.As(lastErr, &appErr) && appErr.Code == "INVENTORY_SERVICE_UNAVAILABLE" {
			break
		}
	}
	if !stderrors.As(lastErr, &appErr) || appErr.Code != "INVENTORY_SERVICE_UNAVAILABLE" {
		t.Fatalf("expected the circuit breaker to open within 5 release attempts, last error = %v", lastErr)
	}

	callsAfterOpen := atomic.LoadInt32(&calls)

	if err := c.Release(context.Background(), uuid.New()); err == nil {
		t.Fatal("expected Release() to keep failing while the circuit is open")
	}
	if got := atomic.LoadInt32(&calls); got != callsAfterOpen {
		t.Errorf("calls to backend after the breaker opened = %d, want still %d (backend must be skipped)", got, callsAfterOpen)
	}
}

func TestInventoryClient_Reserve_CircuitBreakerSkipsBackendWhileOpen(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewInventoryClient(srv.URL, time.Second)

	// Trip the reserve breaker with breakerFailureThreshold consecutive failures. Reserve is not
	// retried, so each call to Reserve maps to exactly one request.
	for i := 0; i < breakerFailureThreshold; i++ {
		if err := c.Reserve(context.Background(), uuid.New(), testItems()); err == nil {
			t.Fatalf("call %d: expected an error from the failing backend", i)
		}
	}
	if got := atomic.LoadInt32(&calls); got != breakerFailureThreshold {
		t.Fatalf("calls after tripping the breaker = %d, want %d", got, breakerFailureThreshold)
	}

	err := c.Reserve(context.Background(), uuid.New(), testItems())
	var appErr *apperrors.AppError
	if !stderrors.As(err, &appErr) {
		t.Fatalf("Reserve() error = %v, want *AppError", err)
	}
	if appErr.Code != "INVENTORY_SERVICE_UNAVAILABLE" {
		t.Errorf("Code = %v, want INVENTORY_SERVICE_UNAVAILABLE", appErr.Code)
	}
	if got := atomic.LoadInt32(&calls); got != breakerFailureThreshold {
		t.Errorf("calls after the breaker opened = %d, want still %d (backend must be skipped)", got, breakerFailureThreshold)
	}
}
