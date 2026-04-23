// Package handler exposes the inventory HTTP API.
package handler

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"net/http"
	"strconv"
	"time"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/inventory/internal/domain"
	apperrors "github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/errors"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	defaultListLimit = 20
	maxListLimit     = 100
)

// ProductRepository is the persistence port the products handler depends on for the catalog.
// GetByID also reports whether the product was served from a stale cache entry after a database
// failure, so the handler can surface that to the caller.
type ProductRepository interface {
	GetByID(ctx context.Context, id uuid.UUID) (product *domain.Product, stale bool, err error)
	List(ctx context.Context, category string, activeOnly bool, limit, offset int) ([]*domain.Product, error)
}

// InventoryRepository is the persistence port the products handler depends on for stock counters.
type InventoryRepository interface {
	GetByProductID(ctx context.Context, productID uuid.UUID) (*domain.Stock, error)
}

// ProductsHandler serves the product and inventory read endpoints.
type ProductsHandler struct {
	products  ProductRepository
	inventory InventoryRepository
	logger    *zap.Logger
}

// NewProductsHandler builds a ProductsHandler backed by the given repositories.
func NewProductsHandler(products ProductRepository, inventory InventoryRepository, logger *zap.Logger) *ProductsHandler {
	return &ProductsHandler{products: products, inventory: inventory, logger: logger}
}

type productResponse struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	SKU         string    `json:"sku"`
	Category    string    `json:"category,omitempty"`
	Brand       string    `json:"brand,omitempty"`
	PriceCents  int64     `json:"price_cents"`
	CostCents   int64     `json:"cost_cents,omitempty"`
	Currency    string    `json:"currency"`
	IsActive    bool      `json:"is_active"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Version     int       `json:"version"`
}

func newProductResponse(p *domain.Product) productResponse {
	return productResponse{
		ID:          p.ID.String(),
		Name:        p.Name,
		Description: p.Description,
		SKU:         p.SKU,
		Category:    p.Category,
		Brand:       p.Brand,
		PriceCents:  p.PriceCents,
		CostCents:   p.CostCents,
		Currency:    p.Currency,
		IsActive:    p.IsActive,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
		Version:     p.Version,
	}
}

type listProductsResponse struct {
	Products []productResponse `json:"products"`
	Limit    int                `json:"limit"`
	Offset   int                `json:"offset"`
}

// List handles GET /api/v1/products.
func (h *ProductsHandler) List(w http.ResponseWriter, r *http.Request) {
	limit, offset, err := parsePagination(r)
	if err != nil {
		h.writeError(w, err)
		return
	}

	category := r.URL.Query().Get("category")
	activeOnly := r.URL.Query().Get("active_only") == "true"

	products, err := h.products.List(r.Context(), category, activeOnly, limit, offset)
	if err != nil {
		h.writeError(w, err)
		return
	}

	responses := make([]productResponse, len(products))
	for i, product := range products {
		responses[i] = newProductResponse(product)
	}

	h.writeJSON(w, http.StatusOK, listProductsResponse{Products: responses, Limit: limit, Offset: offset})
}

// Get handles GET /api/v1/products/{id}.
func (h *ProductsHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		h.writeError(w, apperrors.NewBadRequest("invalid product id"))
		return
	}

	product, stale, err := h.products.GetByID(r.Context(), id)
	if err != nil {
		h.writeError(w, err)
		return
	}

	if stale {
		w.Header().Set("X-Cache", "stale")
	}
	h.writeJSON(w, http.StatusOK, newProductResponse(product))
}

type inventoryResponse struct {
	ProductID         string `json:"product_id"`
	QuantityAvailable int    `json:"quantity_available"`
	QuantityReserved  int    `json:"quantity_reserved"`
}

// Inventory handles GET /api/v1/inventory/{product_id}.
func (h *ProductsHandler) Inventory(w http.ResponseWriter, r *http.Request) {
	productID, err := uuid.Parse(r.PathValue("product_id"))
	if err != nil {
		h.writeError(w, apperrors.NewBadRequest("invalid product id"))
		return
	}

	stock, err := h.inventory.GetByProductID(r.Context(), productID)
	if err != nil {
		h.writeError(w, err)
		return
	}

	h.writeJSON(w, http.StatusOK, inventoryResponse{
		ProductID:         productID.String(),
		QuantityAvailable: stock.QuantityAvailable,
		QuantityReserved:  stock.QuantityReserved,
	})
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

func (h *ProductsHandler) writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		h.logger.Error("failed to encode response", zap.Error(err))
	}
}

func (h *ProductsHandler) writeError(w http.ResponseWriter, err error) {
	var appErr *apperrors.AppError
	if !stderrors.As(err, &appErr) {
		h.logger.Error("unexpected error", zap.Error(err))
		appErr = apperrors.NewInternalServerError("internal server error")
	}
	h.writeJSON(w, appErr.HTTPCode, appErr)
}
