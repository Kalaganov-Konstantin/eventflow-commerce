package handler

import (
	"bytes"
	"context"
	"encoding/json"
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/payment/internal/domain"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/payment/internal/repository"
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

	refundResult *domain.Payment
	refundErr    error
	lastRefundID uuid.UUID
	lastReason   string
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

func (f *fakePaymentProcessor) RefundPayment(_ context.Context, id uuid.UUID, reason string) (*domain.Payment, error) {
	f.lastRefundID = id
	f.lastReason = reason
	if f.refundErr != nil {
		return nil, f.refundErr
	}
	return f.refundResult, nil
}

// fakePaymentStatusReader is a PaymentStatusReader test double.
type fakePaymentStatusReader struct {
	getResult  *repository.PaymentStatus
	getErr     error
	listResult []*repository.PaymentStatus
	listErr    error
}

func (f *fakePaymentStatusReader) GetByID(_ context.Context, _ uuid.UUID) (*repository.PaymentStatus, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.getResult, nil
}

func (f *fakePaymentStatusReader) ListByOrderID(_ context.Context, _ uuid.UUID) ([]*repository.PaymentStatus, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listResult, nil
}

// fakeEventReader is an EventReader test double.
type fakeEventReader struct {
	events []domain.Event
	err    error
}

func (f *fakeEventReader) Load(_ context.Context, _ uuid.UUID, _ int) ([]domain.Event, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.events, nil
}

func newTestMux(t *testing.T, processor PaymentProcessor) *http.ServeMux {
	t.Helper()
	return newTestMuxWithReaders(t, processor, &fakePaymentStatusReader{}, &fakeEventReader{})
}

func newTestMuxWithReaders(t *testing.T, processor PaymentProcessor, statusReader PaymentStatusReader, events EventReader) *http.ServeMux {
	t.Helper()
	h := NewPaymentsHandler(processor, statusReader, events, zaptest.NewLogger(t))
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/payments", h.Process)
	mux.HandleFunc("POST /api/v1/payments/{id}/refund", h.Refund)
	mux.HandleFunc("GET /api/v1/payments/{id}", h.Get)
	mux.HandleFunc("GET /api/v1/payments", h.List)
	mux.HandleFunc("GET /api/v1/payments/{id}/events", h.Events)
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

func TestPaymentsHandler_Refund(t *testing.T) {
	paymentID := uuid.New()
	orderID := uuid.New()
	customerID := uuid.New()

	refunded, err := domain.Initiate(orderID, customerID, 4999, "USD")
	if err != nil {
		t.Fatalf("Initiate: %v", err)
	}
	if err := refunded.Process("txn_1"); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if err := refunded.Refund("customer_request"); err != nil {
		t.Fatalf("Refund: %v", err)
	}

	tests := []struct {
		name       string
		paymentID  string
		body       string
		processor  *fakePaymentProcessor
		wantStatus int
		wantCode   string
		wantReason string
	}{
		{
			name:       "refunds a completed payment",
			paymentID:  paymentID.String(),
			body:       `{"reason":"customer_request"}`,
			processor:  &fakePaymentProcessor{refundResult: refunded},
			wantStatus: http.StatusOK,
			wantReason: "customer_request",
		},
		{
			name:       "defaults the reason for an empty body",
			paymentID:  paymentID.String(),
			body:       "",
			processor:  &fakePaymentProcessor{refundResult: refunded},
			wantStatus: http.StatusOK,
			wantReason: defaultRefundReason,
		},
		{
			name:       "invalid payment id",
			paymentID:  "not-a-uuid",
			body:       "",
			processor:  &fakePaymentProcessor{},
			wantStatus: http.StatusBadRequest,
			wantCode:   "BAD_REQUEST",
		},
		{
			name:       "malformed JSON body",
			paymentID:  paymentID.String(),
			body:       `{`,
			processor:  &fakePaymentProcessor{},
			wantStatus: http.StatusBadRequest,
			wantCode:   "BAD_REQUEST",
		},
		{
			name:       "payment not completed",
			paymentID:  paymentID.String(),
			body:       "",
			processor:  &fakePaymentProcessor{refundErr: apperrors.NewConflict("payment is not completed")},
			wantStatus: http.StatusConflict,
			wantCode:   "CONFLICT",
		},
		{
			name:       "unknown payment id",
			paymentID:  paymentID.String(),
			body:       "",
			processor:  &fakePaymentProcessor{refundErr: apperrors.NewNotFound("payment")},
			wantStatus: http.StatusNotFound,
			wantCode:   "NOT_FOUND",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := newTestMux(t, tt.processor)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/payments/"+tt.paymentID+"/refund", bytes.NewBufferString(tt.body))
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body=%s)", w.Code, tt.wantStatus, w.Body.String())
			}

			if tt.wantStatus == http.StatusOK {
				var got paymentResponse
				if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}
				if got.Status != string(domain.StatusRefunded) {
					t.Errorf("Status = %v, want %v", got.Status, domain.StatusRefunded)
				}
				if tt.processor.lastReason != tt.wantReason {
					t.Errorf("reason = %q, want %q", tt.processor.lastReason, tt.wantReason)
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

func TestPaymentsHandler_Get(t *testing.T) {
	now := time.Now().UTC()
	status := &repository.PaymentStatus{
		ID: uuid.New(), OrderID: uuid.New(), CustomerID: uuid.New(),
		AmountCents: 4999, Currency: "USD", Status: "completed",
		TransactionID: "txn_1", CreatedAt: now, UpdatedAt: now, Version: 2,
	}

	tests := []struct {
		name         string
		paymentID    string
		statusReader *fakePaymentStatusReader
		wantStatus   int
		wantCode     string
	}{
		{
			name:         "existing payment",
			paymentID:    status.ID.String(),
			statusReader: &fakePaymentStatusReader{getResult: status},
			wantStatus:   http.StatusOK,
		},
		{
			name:         "invalid payment id",
			paymentID:    "not-a-uuid",
			statusReader: &fakePaymentStatusReader{},
			wantStatus:   http.StatusBadRequest,
			wantCode:     "BAD_REQUEST",
		},
		{
			name:         "unknown payment id",
			paymentID:    uuid.New().String(),
			statusReader: &fakePaymentStatusReader{getErr: apperrors.NewNotFound("payment")},
			wantStatus:   http.StatusNotFound,
			wantCode:     "NOT_FOUND",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := newTestMuxWithReaders(t, &fakePaymentProcessor{}, tt.statusReader, &fakeEventReader{})

			req := httptest.NewRequest(http.MethodGet, "/api/v1/payments/"+tt.paymentID, nil)
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body=%s)", w.Code, tt.wantStatus, w.Body.String())
			}

			if tt.wantStatus == http.StatusOK {
				var got paymentStatusResponse
				if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}
				if got.ID != status.ID.String() || got.TransactionID != status.TransactionID {
					t.Errorf("got = %+v, want id=%v transaction_id=%v", got, status.ID, status.TransactionID)
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

func TestPaymentsHandler_List(t *testing.T) {
	orderID := uuid.New()
	status := &repository.PaymentStatus{ID: uuid.New(), OrderID: orderID, CustomerID: uuid.New(), Status: "completed"}

	tests := []struct {
		name         string
		query        string
		statusReader *fakePaymentStatusReader
		wantStatus   int
		wantCode     string
		wantCount    int
	}{
		{
			name:         "payments for the order",
			query:        "?order_id=" + orderID.String(),
			statusReader: &fakePaymentStatusReader{listResult: []*repository.PaymentStatus{status}},
			wantStatus:   http.StatusOK,
			wantCount:    1,
		},
		{
			name:         "missing order_id",
			query:        "",
			statusReader: &fakePaymentStatusReader{},
			wantStatus:   http.StatusBadRequest,
			wantCode:     "VALIDATION_ERROR",
		},
		{
			name:         "invalid order_id",
			query:        "?order_id=not-a-uuid",
			statusReader: &fakePaymentStatusReader{},
			wantStatus:   http.StatusBadRequest,
			wantCode:     "VALIDATION_ERROR",
		},
		{
			name:         "repository failure",
			query:        "?order_id=" + orderID.String(),
			statusReader: &fakePaymentStatusReader{listErr: errTestService},
			wantStatus:   http.StatusInternalServerError,
			wantCode:     "INTERNAL_SERVER_ERROR",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := newTestMuxWithReaders(t, &fakePaymentProcessor{}, tt.statusReader, &fakeEventReader{})

			req := httptest.NewRequest(http.MethodGet, "/api/v1/payments"+tt.query, nil)
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body=%s)", w.Code, tt.wantStatus, w.Body.String())
			}

			if tt.wantStatus == http.StatusOK {
				var got listPaymentsResponse
				if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}
				if len(got.Payments) != tt.wantCount {
					t.Errorf("Payments = %+v, want %d entries", got.Payments, tt.wantCount)
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

func TestPaymentsHandler_Events(t *testing.T) {
	paymentID := uuid.New()
	events := []domain.Event{
		&domain.PaymentInitiated{PaymentID: paymentID, AmountCents: 4999, Currency: "USD"},
		&domain.PaymentProcessed{PaymentID: paymentID, TransactionID: "txn_1"},
	}

	tests := []struct {
		name        string
		paymentID   string
		eventReader *fakeEventReader
		wantStatus  int
		wantCode    string
		wantCount   int
	}{
		{
			name:        "returns the recorded events",
			paymentID:   paymentID.String(),
			eventReader: &fakeEventReader{events: events},
			wantStatus:  http.StatusOK,
			wantCount:   2,
		},
		{
			name:        "invalid payment id",
			paymentID:   "not-a-uuid",
			eventReader: &fakeEventReader{},
			wantStatus:  http.StatusBadRequest,
			wantCode:    "BAD_REQUEST",
		},
		{
			name:        "unknown payment id",
			paymentID:   uuid.New().String(),
			eventReader: &fakeEventReader{},
			wantStatus:  http.StatusNotFound,
			wantCode:    "NOT_FOUND",
		},
		{
			name:        "repository failure",
			paymentID:   paymentID.String(),
			eventReader: &fakeEventReader{err: errTestService},
			wantStatus:  http.StatusInternalServerError,
			wantCode:    "INTERNAL_SERVER_ERROR",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := newTestMuxWithReaders(t, &fakePaymentProcessor{}, &fakePaymentStatusReader{}, tt.eventReader)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/payments/"+tt.paymentID+"/events", nil)
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body=%s)", w.Code, tt.wantStatus, w.Body.String())
			}

			if tt.wantStatus == http.StatusOK {
				var got listPaymentEventsResponse
				if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}
				if len(got.Events) != tt.wantCount {
					t.Errorf("Events = %+v, want %d entries", got.Events, tt.wantCount)
				}
				if got.Events[0].Type != domain.EventTypePaymentInitiated {
					t.Errorf("Events[0].Type = %v, want %v", got.Events[0].Type, domain.EventTypePaymentInitiated)
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
