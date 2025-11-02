package handler

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"net/http"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/inventory/internal/domain"
	apperrors "github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/errors"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// StockRepository is the persistence port the reservations handler depends on.
type StockRepository interface {
	Reserve(ctx context.Context, orderID uuid.UUID, items []domain.ReserveItem) error
	Release(ctx context.Context, orderID uuid.UUID) error
}

// ReservationsHandler serves the stock reservation HTTP endpoints.
type ReservationsHandler struct {
	stock  StockRepository
	logger *zap.Logger
}

// NewReservationsHandler builds a ReservationsHandler backed by stock.
func NewReservationsHandler(stock StockRepository, logger *zap.Logger) *ReservationsHandler {
	return &ReservationsHandler{stock: stock, logger: logger}
}

type reserveItemRequest struct {
	ProductID string `json:"product_id"`
	Quantity  int    `json:"quantity"`
}

type reserveRequest struct {
	OrderID string               `json:"order_id"`
	Items   []reserveItemRequest `json:"items"`
}

type reservedItemResponse struct {
	ProductID string `json:"product_id"`
	Quantity  int    `json:"quantity"`
}

type reserveResponse struct {
	OrderID string                 `json:"order_id"`
	Items   []reservedItemResponse `json:"items"`
}

// Reserve handles POST /api/v1/inventory/reservations.
func (h *ReservationsHandler) Reserve(w http.ResponseWriter, r *http.Request) {
	var req reserveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, apperrors.NewBadRequest("invalid request body"))
		return
	}

	orderID, err := uuid.Parse(req.OrderID)
	if err != nil {
		h.writeError(w, apperrors.NewValidationError("order_id", "must be a valid uuid"))
		return
	}
	if len(req.Items) == 0 {
		h.writeError(w, apperrors.NewValidationError("items", "must contain at least one item"))
		return
	}

	items := make([]domain.ReserveItem, len(req.Items))
	for i, item := range req.Items {
		productID, err := uuid.Parse(item.ProductID)
		if err != nil {
			h.writeError(w, apperrors.NewValidationError("items.product_id", "must be a valid uuid"))
			return
		}
		if item.Quantity <= 0 {
			h.writeError(w, apperrors.NewValidationError("items.quantity", "must be greater than zero"))
			return
		}
		items[i] = domain.ReserveItem{ProductID: productID, Quantity: item.Quantity}
	}

	if err := h.stock.Reserve(r.Context(), orderID, items); err != nil {
		h.writeError(w, err)
		return
	}

	respItems := make([]reservedItemResponse, len(items))
	for i, item := range items {
		respItems[i] = reservedItemResponse{ProductID: item.ProductID.String(), Quantity: item.Quantity}
	}
	h.writeJSON(w, http.StatusCreated, reserveResponse{OrderID: orderID.String(), Items: respItems})
}

// Release handles DELETE /api/v1/inventory/reservations/{order_id}.
func (h *ReservationsHandler) Release(w http.ResponseWriter, r *http.Request) {
	orderID, err := uuid.Parse(r.PathValue("order_id"))
	if err != nil {
		h.writeError(w, apperrors.NewBadRequest("invalid order id"))
		return
	}

	if err := h.stock.Release(r.Context(), orderID); err != nil {
		h.writeError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *ReservationsHandler) writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		h.logger.Error("failed to encode response", zap.Error(err))
	}
}

func (h *ReservationsHandler) writeError(w http.ResponseWriter, err error) {
	var appErr *apperrors.AppError
	if !stderrors.As(err, &appErr) {
		h.logger.Error("unexpected error", zap.Error(err))
		appErr = apperrors.NewInternalServerError("internal server error")
	}
	h.writeJSON(w, appErr.HTTPCode, appErr)
}
