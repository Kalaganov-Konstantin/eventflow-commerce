// Package handler exposes the payment HTTP API.
package handler

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"io"
	"net/http"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/payment/internal/domain"
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

// PaymentsHandler serves the payment HTTP endpoints.
type PaymentsHandler struct {
	service PaymentProcessor
	logger  *zap.Logger
}

// NewPaymentsHandler builds a PaymentsHandler backed by service.
func NewPaymentsHandler(service PaymentProcessor, logger *zap.Logger) *PaymentsHandler {
	return &PaymentsHandler{service: service, logger: logger}
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
