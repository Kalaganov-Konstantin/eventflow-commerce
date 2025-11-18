// Package handler exposes the payment HTTP API.
package handler

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"io"
	"net/http"
	"time"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/payment/internal/domain"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/payment/internal/repository"
	apperrors "github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/errors"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

const defaultRefundReason = "customer_request"

// PaymentProcessor is the command port the payments handler depends on.
type PaymentProcessor interface {
	ProcessPayment(ctx context.Context, orderID, customerID uuid.UUID, amountCents int64, currency string) (*domain.Payment, error)
	RefundPayment(ctx context.Context, id uuid.UUID, reason string) (*domain.Payment, error)
}

// PaymentStatusReader is the read model port the payments handler depends on for queries.
type PaymentStatusReader interface {
	GetByID(ctx context.Context, id uuid.UUID) (*repository.PaymentStatus, error)
	ListByOrderID(ctx context.Context, orderID uuid.UUID) ([]*repository.PaymentStatus, error)
}

// EventReader is the persistence port for a payment's full recorded event history.
type EventReader interface {
	Load(ctx context.Context, aggregateID uuid.UUID, fromVersion int) ([]domain.Event, error)
}

// PaymentsHandler serves the payment HTTP endpoints.
type PaymentsHandler struct {
	service      PaymentProcessor
	statusReader PaymentStatusReader
	events       EventReader
	logger       *zap.Logger
}

// NewPaymentsHandler builds a PaymentsHandler backed by service, statusReader and events.
func NewPaymentsHandler(service PaymentProcessor, statusReader PaymentStatusReader, events EventReader, logger *zap.Logger) *PaymentsHandler {
	return &PaymentsHandler{service: service, statusReader: statusReader, events: events, logger: logger}
}

type processPaymentRequest struct {
	OrderID     string `json:"order_id"`
	AmountCents int64  `json:"amount_cents"`
	Currency    string `json:"currency"`
}

type paymentResponse struct {
	ID          string `json:"id"`
	OrderID     string `json:"order_id"`
	CustomerID  string `json:"customer_id"`
	AmountCents int64  `json:"amount_cents"`
	Currency    string `json:"currency"`
	Status      string `json:"status"`
	Version     int    `json:"version"`
}

func newPaymentResponse(p *domain.Payment) paymentResponse {
	return paymentResponse{
		ID:          p.ID.String(),
		OrderID:     p.OrderID.String(),
		CustomerID:  p.CustomerID.String(),
		AmountCents: p.AmountCents,
		Currency:    p.Currency,
		Status:      string(p.Status),
		Version:     p.Version,
	}
}

// Process handles POST /api/v1/payments.
func (h *PaymentsHandler) Process(w http.ResponseWriter, r *http.Request) {
	customerID, err := customerIDFromHeader(r)
	if err != nil {
		h.writeError(w, err)
		return
	}

	var req processPaymentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, apperrors.NewBadRequest("invalid request body"))
		return
	}

	orderID, err := uuid.Parse(req.OrderID)
	if err != nil {
		h.writeError(w, apperrors.NewValidationError("order_id", "must be a valid uuid"))
		return
	}
	if req.Currency == "" {
		req.Currency = "USD"
	}

	payment, err := h.service.ProcessPayment(r.Context(), orderID, customerID, req.AmountCents, req.Currency)
	if err != nil {
		h.writeError(w, err)
		return
	}

	h.writeJSON(w, http.StatusCreated, newPaymentResponse(payment))
}

type refundPaymentRequest struct {
	Reason string `json:"reason"`
}

// Refund handles POST /api/v1/payments/{id}/refund.
func (h *PaymentsHandler) Refund(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		h.writeError(w, apperrors.NewBadRequest("invalid payment id"))
		return
	}

	var req refundPaymentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !stderrors.Is(err, io.EOF) {
		h.writeError(w, apperrors.NewBadRequest("invalid request body"))
		return
	}
	if req.Reason == "" {
		req.Reason = defaultRefundReason
	}

	payment, err := h.service.RefundPayment(r.Context(), id, req.Reason)
	if err != nil {
		h.writeError(w, err)
		return
	}

	h.writeJSON(w, http.StatusOK, newPaymentResponse(payment))
}

type paymentStatusResponse struct {
	ID            string     `json:"id"`
	OrderID       string     `json:"order_id"`
	CustomerID    string     `json:"customer_id"`
	AmountCents   int64      `json:"amount_cents"`
	Currency      string     `json:"currency"`
	Status        string     `json:"status"`
	TransactionID string     `json:"transaction_id,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
	Version       int        `json:"version"`
}

func newPaymentStatusResponse(s *repository.PaymentStatus) paymentStatusResponse {
	return paymentStatusResponse{
		ID:            s.ID.String(),
		OrderID:       s.OrderID.String(),
		CustomerID:    s.CustomerID.String(),
		AmountCents:   s.AmountCents,
		Currency:      s.Currency,
		Status:        s.Status,
		TransactionID: s.TransactionID,
		CreatedAt:     s.CreatedAt,
		UpdatedAt:     s.UpdatedAt,
		CompletedAt:   s.CompletedAt,
		Version:       s.Version,
	}
}

// Get handles GET /api/v1/payments/{id}.
func (h *PaymentsHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		h.writeError(w, apperrors.NewBadRequest("invalid payment id"))
		return
	}

	status, err := h.statusReader.GetByID(r.Context(), id)
	if err != nil {
		h.writeError(w, err)
		return
	}

	h.writeJSON(w, http.StatusOK, newPaymentStatusResponse(status))
}

type listPaymentsResponse struct {
	Payments []paymentStatusResponse `json:"payments"`
}

// List handles GET /api/v1/payments.
func (h *PaymentsHandler) List(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("order_id")
	if raw == "" {
		h.writeError(w, apperrors.NewValidationError("order_id", "is required"))
		return
	}

	orderID, err := uuid.Parse(raw)
	if err != nil {
		h.writeError(w, apperrors.NewValidationError("order_id", "must be a valid uuid"))
		return
	}

	statuses, err := h.statusReader.ListByOrderID(r.Context(), orderID)
	if err != nil {
		h.writeError(w, err)
		return
	}

	responses := make([]paymentStatusResponse, len(statuses))
	for i, status := range statuses {
		responses[i] = newPaymentStatusResponse(status)
	}
	h.writeJSON(w, http.StatusOK, listPaymentsResponse{Payments: responses})
}

type paymentEventResponse struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

type listPaymentEventsResponse struct {
	Events []paymentEventResponse `json:"events"`
}

// Events handles GET /api/v1/payments/{id}/events. It returns the full recorded event history of a
// payment for audit purposes.
func (h *PaymentsHandler) Events(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		h.writeError(w, apperrors.NewBadRequest("invalid payment id"))
		return
	}

	events, err := h.events.Load(r.Context(), id, 0)
	if err != nil {
		h.writeError(w, err)
		return
	}
	if len(events) == 0 {
		h.writeError(w, apperrors.NewNotFound("payment"))
		return
	}

	responses := make([]paymentEventResponse, len(events))
	for i, event := range events {
		data, err := domain.MarshalEvent(event)
		if err != nil {
			h.writeError(w, err)
			return
		}
		responses[i] = paymentEventResponse{Type: event.EventType(), Data: data}
	}
	h.writeJSON(w, http.StatusOK, listPaymentEventsResponse{Events: responses})
}

func customerIDFromHeader(r *http.Request) (uuid.UUID, error) {
	raw := r.Header.Get("X-User-ID")
	if raw == "" {
		return uuid.Nil, apperrors.NewUnauthorized("X-User-ID header is required")
	}

	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, apperrors.NewUnauthorized("X-User-ID header must be a valid uuid")
	}

	return id, nil
}

func (h *PaymentsHandler) writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		h.logger.Error("failed to encode response", zap.Error(err))
	}
}

func (h *PaymentsHandler) writeError(w http.ResponseWriter, err error) {
	var appErr *apperrors.AppError
	if !stderrors.As(err, &appErr) {
		h.logger.Error("unexpected error", zap.Error(err))
		appErr = apperrors.NewInternalServerError("internal server error")
	}
	h.writeJSON(w, appErr.HTTPCode, appErr)
}
