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

func TestPaymentClient_Refund(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		paymentID := uuid.New()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || r.URL.Path != "/api/v1/payments/"+paymentID.String()+"/refund" {
				t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"` + paymentID.String() + `","status":"refunded"}`))
		}))
		defer srv.Close()

		c := NewPaymentClient(srv.URL, time.Second)
		if err := c.Refund(context.Background(), paymentID, "downstream_failure"); err != nil {
			t.Fatalf("Refund() error = %v", err)
		}
	})

	t.Run("payment not completed", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"code":"CONFLICT","message":"payment is not completed"}`))
		}))
		defer srv.Close()

		c := NewPaymentClient(srv.URL, time.Second)
		err := c.Refund(context.Background(), uuid.New(), "downstream_failure")

		var appErr *apperrors.AppError
		if !stderrors.As(err, &appErr) {
			t.Fatalf("Refund() error = %v, want *AppError", err)
		}
		if appErr.Code != "CONFLICT" {
			t.Errorf("Code = %v, want CONFLICT", appErr.Code)
		}
		if appErr.HTTPCode != http.StatusConflict {
			t.Errorf("HTTPCode = %v, want %v", appErr.HTTPCode, http.StatusConflict)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(100 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		c := NewPaymentClient(srv.URL, 20*time.Millisecond)
		err := c.Refund(context.Background(), uuid.New(), "downstream_failure")

		var appErr *apperrors.AppError
		if !stderrors.As(err, &appErr) {
			t.Fatalf("Refund() error = %v, want *AppError", err)
		}
		if appErr.Code != "PAYMENT_SERVICE_TIMEOUT" {
			t.Errorf("Code = %v, want PAYMENT_SERVICE_TIMEOUT", appErr.Code)
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

		c := NewPaymentClient(srv.URL, time.Second)
		err := c.Refund(context.Background(), uuid.New(), "downstream_failure")

		var appErr *apperrors.AppError
		if !stderrors.As(err, &appErr) {
			t.Fatalf("Refund() error = %v, want *AppError", err)
		}
		if appErr.Code != "PAYMENT_ERROR" {
			t.Errorf("Code = %v, want PAYMENT_ERROR", appErr.Code)
		}
		if appErr.HTTPCode != http.StatusInternalServerError {
			t.Errorf("HTTPCode = %v, want %v", appErr.HTTPCode, http.StatusInternalServerError)
		}
	})
}
