// Package handler exposes the order HTTP API.
package handler

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"net/http"
	"strconv"
	"time"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/order/internal/domain"
	apperrors "github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/errors"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	defaultListLimit = 20
	maxListLimit     = 100
)

// OrderRepository is the persistence port the orders handler depends on.
type OrderRepository interface {
	Save(ctx context.Context, order *domain.Order) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Order, error)
	ListByCustomer(ctx context.Context, customerID uuid.UUID, limit, offset int) ([]*domain.Order, error)
}

// OrdersHandler serves the order HTTP endpoints.
type OrdersHandler struct {
	repo   OrderRepository
	logger *zap.Logger
}

// NewOrdersHandler builds an OrdersHandler backed by repo.
func NewOrdersHandler(repo OrderRepository, logger *zap.Logger) *OrdersHandler {
	return &OrdersHandler{repo: repo, logger: logger}
}

type createOrderItemRequest struct {
	ProductID      string `json:"product_id"`
	ProductName    string `json:"product_name"`
	ProductSKU     string `json:"product_sku"`
	Quantity       int    `json:"quantity"`
	UnitPriceCents int64  `json:"unit_price_cents"`
}

type createOrderRequest struct {
	Items    []createOrderItemRequest `json:"items"`
	Currency string                   `json:"currency"`
}

type orderItemResponse struct {
	ID              string `json:"id"`
	ProductID       string `json:"product_id"`
	ProductName     string `json:"product_name"`
	ProductSKU      string `json:"product_sku,omitempty"`
	Quantity        int    `json:"quantity"`
	UnitPriceCents  int64  `json:"unit_price_cents"`
	TotalPriceCents int64  `json:"total_price_cents"`
}

type orderResponse struct {
	ID               string              `json:"id"`
	CustomerID       string              `json:"customer_id"`
	Status           string              `json:"status"`
	TotalAmountCents int64               `json:"total_amount_cents"`
	Currency         string              `json:"currency"`
	Items            []orderItemResponse `json:"items"`
	CreatedAt        time.Time           `json:"created_at"`
	UpdatedAt        time.Time           `json:"updated_at"`
	Version          int                 `json:"version"`
}

func newOrderResponse(o *domain.Order) orderResponse {
	items := make([]orderItemResponse, len(o.Items))
	for i, item := range o.Items {
		items[i] = orderItemResponse{
			ID:              item.ID.String(),
			ProductID:       item.ProductID.String(),
			ProductName:     item.ProductName,
			ProductSKU:      item.ProductSKU,
			Quantity:        item.Quantity,
			UnitPriceCents:  item.UnitPriceCents,
			TotalPriceCents: item.TotalPriceCents,
		}
	}

	return orderResponse{
		ID:               o.ID.String(),
		CustomerID:       o.CustomerID.String(),
		Status:           string(o.Status),
		TotalAmountCents: o.TotalAmountCents,
		Currency:         o.Currency,
		Items:            items,
		CreatedAt:        o.CreatedAt,
		UpdatedAt:        o.UpdatedAt,
		Version:          o.Version,
	}
}

// Create handles POST /api/v1/orders.
func (h *OrdersHandler) Create(w http.ResponseWriter, r *http.Request) {
	customerID, err := customerIDFromHeader(r)
	if err != nil {
		h.writeError(w, err)
		return
	}

	var req createOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, apperrors.NewBadRequest("invalid request body"))
		return
	}
	if req.Currency == "" {
		req.Currency = "USD"
	}

	items := make([]domain.OrderItem, len(req.Items))
	for i, item := range req.Items {
		productID, err := uuid.Parse(item.ProductID)
		if err != nil {
			h.writeError(w, apperrors.NewValidationError("items.product_id", "must be a valid uuid"))
			return
		}
		items[i] = domain.OrderItem{
			ProductID:      productID,
			ProductName:    item.ProductName,
			ProductSKU:     item.ProductSKU,
			Quantity:       item.Quantity,
			UnitPriceCents: item.UnitPriceCents,
		}
	}

	order, err := domain.NewOrder(customerID, items, req.Currency)
	if err != nil {
		h.writeError(w, err)
		return
	}

	if err := h.repo.Save(r.Context(), order); err != nil {
		h.writeError(w, err)
		return
	}

	h.writeJSON(w, http.StatusCreated, newOrderResponse(order))
}

// Get handles GET /api/v1/orders/{id}.
func (h *OrdersHandler) Get(w http.ResponseWriter, r *http.Request) {
	customerID, err := customerIDFromHeader(r)
	if err != nil {
		h.writeError(w, err)
		return
	}

	orderID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		h.writeError(w, apperrors.NewBadRequest("invalid order id"))
		return
	}

	order, err := h.repo.GetByID(r.Context(), orderID)
	if err != nil {
		h.writeError(w, err)
		return
	}

	if order.CustomerID != customerID {
		h.writeError(w, apperrors.NewForbidden("order does not belong to the current user"))
		return
	}

	h.writeJSON(w, http.StatusOK, newOrderResponse(order))
}

type listOrdersResponse struct {
	Orders []orderResponse `json:"orders"`
	Limit  int             `json:"limit"`
	Offset int             `json:"offset"`
}

// List handles GET /api/v1/orders.
func (h *OrdersHandler) List(w http.ResponseWriter, r *http.Request) {
	customerID, err := customerIDFromHeader(r)
	if err != nil {
		h.writeError(w, err)
		return
	}

	limit, offset, err := parsePagination(r)
	if err != nil {
		h.writeError(w, err)
		return
	}

	orders, err := h.repo.ListByCustomer(r.Context(), customerID, limit, offset)
	if err != nil {
		h.writeError(w, err)
		return
	}

	responses := make([]orderResponse, len(orders))
	for i, order := range orders {
		responses[i] = newOrderResponse(order)
	}

	h.writeJSON(w, http.StatusOK, listOrdersResponse{Orders: responses, Limit: limit, Offset: offset})
}

func parsePagination(r *http.Request) (limit, offset int, err error) {
	limit = defaultListLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, convErr := strconv.Atoi(raw)
		if convErr != nil || parsed < 0 {
			return 0, 0, apperrors.NewValidationError("limit", "must be a non-negative integer")
		}
		limit = parsed
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}

	if raw := r.URL.Query().Get("offset"); raw != "" {
		parsed, convErr := strconv.Atoi(raw)
		if convErr != nil || parsed < 0 {
			return 0, 0, apperrors.NewValidationError("offset", "must be a non-negative integer")
		}
		offset = parsed
	}

	return limit, offset, nil
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

func (h *OrdersHandler) writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		h.logger.Error("failed to encode response", zap.Error(err))
	}
}

func (h *OrdersHandler) writeError(w http.ResponseWriter, err error) {
	var appErr *apperrors.AppError
	if !stderrors.As(err, &appErr) {
		h.logger.Error("unexpected error", zap.Error(err))
		appErr = apperrors.NewInternalServerError("internal server error")
	}
	h.writeJSON(w, appErr.HTTPCode, appErr)
}
