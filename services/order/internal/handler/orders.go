// Package handler exposes the order HTTP API.
package handler

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"net/http"
	"strconv"
	"time"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/order/internal/client"
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

// InventoryReserver is the port the orders handler uses to reserve stock before an order is
// accepted.
type InventoryReserver interface {
	Reserve(ctx context.Context, orderID uuid.UUID, items []client.ReserveItem) error
	Release(ctx context.Context, orderID uuid.UUID) error
}

// OrderTransitioner is the port the orders handler uses to move a freshly created order into
// pending_payment once its stock is reserved.
type OrderTransitioner interface {
	MarkPendingPaymentAfterCreate(ctx context.Context, orderID uuid.UUID) error
}

// OrdersHandler serves the order HTTP endpoints.
type OrdersHandler struct {
	repo      OrderRepository
	inventory InventoryReserver
	orders    OrderTransitioner
	logger    *zap.Logger
}

// NewOrdersHandler builds an OrdersHandler backed by repo, inventory and orders.
func NewOrdersHandler(repo OrderRepository, inventory InventoryReserver, orders OrderTransitioner, logger *zap.Logger) *OrdersHandler {
	return &OrdersHandler{repo: repo, inventory: inventory, orders: orders, logger: logger}
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

type createOrderResponse struct {
	OrderID string `json:"order_id"`
	Status  string `json:"status"`
}

// Create handles POST /api/v1/orders. It reserves stock for the order synchronously before
// accepting it: a shortage answers 409 and creates nothing. Once stock is reserved, the order is
// saved and moved to pending_payment, which enqueues order.ready_for_payment for the payment saga
// to pick up, and the call returns 202 with the order id.
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

	reserveItems := make([]client.ReserveItem, len(order.Items))
	for i, item := range order.Items {
		reserveItems[i] = client.ReserveItem{ProductID: item.ProductID, Quantity: item.Quantity}
	}
	if err := h.inventory.Reserve(r.Context(), order.ID, reserveItems); err != nil {
		h.writeError(w, err)
		return
	}

	if err := h.repo.Save(r.Context(), order); err != nil {
		if releaseErr := h.inventory.Release(r.Context(), order.ID); releaseErr != nil {
			h.logger.Error("failed to release reservation for an order that was not saved",
				zap.String("order_id", order.ID.String()), zap.Error(releaseErr))
		}
		h.writeError(w, err)
		return
	}

	if err := h.orders.MarkPendingPaymentAfterCreate(r.Context(), order.ID); err != nil {
		h.writeError(w, err)
		return
	}

	h.writeJSON(w, http.StatusAccepted, createOrderResponse{OrderID: order.ID.String(), Status: string(domain.StatusPendingPayment)})
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
