package client

import (
	"context"
	stderrors "errors"
	"net/http"
	"net/http/httptest"
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
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
	})
}
