package handler

import (
	"bytes"
	"context"
	"encoding/json"
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/payment/internal/domain"
	apperrors "github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/errors"
	"github.com/google/uuid"
	"go.uber.org/zap/zaptest"
)

var errTestService = stderrors.New("service failure")

// fakePaymentProcessor is a PaymentProcessor test double.
type fakePaymentProcessor struct {
	processResult *domain.Payment
	processErr    error
	lastOrderID   uuid.UUID
	lastCustomer  uuid.UUID
	lastAmount    int64
	lastCurrency  string
}

func (f *fakePaymentProcessor) ProcessPayment(_ context.Context, orderID, customerID uuid.UUID, amountCents int64, currency string) (*domain.Payment, error) {
	f.lastOrderID = orderID
	f.lastCustomer = customerID
	f.lastAmount = amountCents
	f.lastCurrency = currency
	if f.processErr != nil {
		return nil, f.processErr
	}
	return f.processResult, nil
}

func newTestMux(t *testing.T, processor PaymentProcessor) *http.ServeMux {
	t.Helper()
	h := NewPaymentsHandler(processor, zaptest.NewLogger(t))
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/payments", h.Process)
	return mux
}

func decodeAppError(t *testing.T, body []byte) apperrors.AppError {
	t.Helper()
	var appErr apperrors.AppError
	if err := json.Unmarshal(body, &appErr); err != nil {
		t.Fatalf("failed to decode error body: %v", err)
	}
	return appErr
}

func TestPaymentsHandler_Process(t *testing.T) {
	orderID := uuid.New()
	customerID := uuid.New()
	validBody := `{"order_id":"` + orderID.String() + `","amount_cents":4999,"currency":"USD"}`

	completed, err := domain.Initiate(orderID, customerID, 4999, "USD")
	if err != nil {
		t.Fatalf("Initiate: %v", err)
	}
	if err := completed.Process("txn_1"); err != nil {
		t.Fatalf("Process: %v", err)
	}

	tests := []struct {
		name       string
		userID     string
		body       string
		processor  *fakePaymentProcessor
		wantStatus int
		wantCode   string
	}{
		{
			name:       "approved payment",
			userID:     customerID.String(),
			body:       validBody,
			processor:  &fakePaymentProcessor{processResult: completed},
			wantStatus: http.StatusCreated,
		},
		{
			name:       "missing X-User-ID header",
			userID:     "",
			body:       validBody,
			processor:  &fakePaymentProcessor{},
			wantStatus: http.StatusUnauthorized,
			wantCode:   "UNAUTHORIZED",
		},
		{
			name:       "invalid X-User-ID header",
			userID:     "not-a-uuid",
			body:       validBody,
			processor:  &fakePaymentProcessor{},
			wantStatus: http.StatusUnauthorized,
			wantCode:   "UNAUTHORIZED",
		},
		{
			name:       "malformed JSON body",
			userID:     customerID.String(),
			body:       `{`,
			processor:  &fakePaymentProcessor{},
			wantStatus: http.StatusBadRequest,
			wantCode:   "BAD_REQUEST",
		},
		{
			name:       "invalid order id",
			userID:     customerID.String(),
			body:       `{"order_id":"not-a-uuid","amount_cents":4999,"currency":"USD"}`,
			processor:  &fakePaymentProcessor{},
			wantStatus: http.StatusBadRequest,
			wantCode:   "VALIDATION_ERROR",
		},
		{
			name:       "declined by the gateway",
			userID:     customerID.String(),
			body:       validBody,
			processor:  &fakePaymentProcessor{processErr: apperrors.NewPaymentFailed("insufficient_funds")},
			wantStatus: http.StatusPaymentRequired,
			wantCode:   "PAYMENT_FAILED",
		},
		{
			name:       "service failure",
			userID:     customerID.String(),
			body:       validBody,
			processor:  &fakePaymentProcessor{processErr: errTestService},
			wantStatus: http.StatusInternalServerError,
			wantCode:   "INTERNAL_SERVER_ERROR",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := newTestMux(t, tt.processor)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/payments", bytes.NewBufferString(tt.body))
			if tt.userID != "" {
				req.Header.Set("X-User-ID", tt.userID)
			}
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body=%s)", w.Code, tt.wantStatus, w.Body.String())
			}

			if tt.wantStatus == http.StatusCreated {
				var got paymentResponse
				if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}
				if got.OrderID != orderID.String() {
					t.Errorf("OrderID = %v, want %v", got.OrderID, orderID.String())
				}
				if got.Status != string(domain.StatusCompleted) {
					t.Errorf("Status = %v, want %v", got.Status, domain.StatusCompleted)
				}
				if tt.processor.lastAmount != 4999 {
					t.Errorf("service received amount %d, want 4999", tt.processor.lastAmount)
				}
				return
			}

			appErr := decodeAppError(t, w.Body.Bytes())
			if appErr.Code != tt.wantCode {
				t.Errorf("Code = %v, want %v", appErr.Code, tt.wantCode)
			}
		})
	}
}
