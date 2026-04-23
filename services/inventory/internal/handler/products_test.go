package handler

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/inventory/internal/domain"
	apperrors "github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/errors"
	"github.com/google/uuid"
	"go.uber.org/zap/zaptest"
)

var errTestRepository = stderrors.New("repository failure")

// fakeProductRepository is an in-memory ProductRepository test double.
type fakeProductRepository struct {
	productsByID map[uuid.UUID]*domain.Product
	getErr       error
	getStale     bool

	listResult   []*domain.Product
	listErr      error
	lastCategory string
	lastActive   bool
	lastLimit    int
	lastOffset   int
}

func (f *fakeProductRepository) GetByID(_ context.Context, id uuid.UUID) (*domain.Product, bool, error) {
	if f.getErr != nil {
		return nil, false, f.getErr
	}
	product, ok := f.productsByID[id]
	if !ok {
		return nil, false, apperrors.NewProductNotFound(id.String())
	}
	return product, f.getStale, nil
}

func (f *fakeProductRepository) List(_ context.Context, category string, activeOnly bool, limit, offset int) ([]*domain.Product, error) {
	f.lastCategory = category
	f.lastActive = activeOnly
	f.lastLimit = limit
	f.lastOffset = offset
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listResult, nil
}

// fakeInventoryRepository is an in-memory InventoryRepository test double.
type fakeInventoryRepository struct {
	stockByProduct map[uuid.UUID]*domain.Stock
	getErr         error
}

func (f *fakeInventoryRepository) GetByProductID(_ context.Context, productID uuid.UUID) (*domain.Stock, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	stock, ok := f.stockByProduct[productID]
	if !ok {
		return nil, apperrors.NewProductNotFound(productID.String())
	}
	return stock, nil
}

func newTestMux(t *testing.T, products ProductRepository, inventory InventoryRepository) *http.ServeMux {
	t.Helper()
	h := NewProductsHandler(products, inventory, zaptest.NewLogger(t))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/products", h.List)
	mux.HandleFunc("GET /api/v1/products/{id}", h.Get)
	mux.HandleFunc("GET /api/v1/inventory/{product_id}", h.Inventory)
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

func TestProductsHandler_List(t *testing.T) {
	sampleProduct := &domain.Product{ID: uuid.New(), Name: "Widget", SKU: "WID-1", PriceCents: 999, Currency: "USD"}

	tests := []struct {
		name         string
		query        string
		repo         *fakeProductRepository
		wantStatus   int
		wantCode     string
		wantLimit    int
		wantOffset   int
		wantCategory string
		wantActive   bool
	}{
		{
			name:       "default pagination",
			query:      "",
			repo:       &fakeProductRepository{listResult: []*domain.Product{sampleProduct}},
			wantStatus: http.StatusOK,
			wantLimit:  20,
			wantOffset: 0,
		},
		{
			name:         "custom filters and pagination",
			query:        "?category=gadgets&active_only=true&limit=5&offset=10",
			repo:         &fakeProductRepository{listResult: []*domain.Product{sampleProduct}},
			wantStatus:   http.StatusOK,
			wantLimit:    5,
			wantOffset:   10,
			wantCategory: "gadgets",
			wantActive:   true,
		},
		{
			name:       "limit capped at 100",
			query:      "?limit=500",
			repo:       &fakeProductRepository{listResult: []*domain.Product{sampleProduct}},
			wantStatus: http.StatusOK,
			wantLimit:  100,
			wantOffset: 0,
		},
		{
			name:       "invalid limit",
			query:      "?limit=abc",
			repo:       &fakeProductRepository{},
			wantStatus: http.StatusBadRequest,
			wantCode:   "VALIDATION_ERROR",
		},
		{
			name:       "negative offset",
			query:      "?offset=-1",
			repo:       &fakeProductRepository{},
			wantStatus: http.StatusBadRequest,
			wantCode:   "VALIDATION_ERROR",
		},
		{
			name:       "repository failure",
			query:      "",
			repo:       &fakeProductRepository{listErr: errTestRepository},
			wantStatus: http.StatusInternalServerError,
			wantCode:   "INTERNAL_SERVER_ERROR",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := newTestMux(t, tt.repo, &fakeInventoryRepository{})

			req := httptest.NewRequest(http.MethodGet, "/api/v1/products"+tt.query, nil)
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body=%s)", w.Code, tt.wantStatus, w.Body.String())
			}

			if tt.wantStatus == http.StatusOK {
				var got listProductsResponse
				if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}
				if got.Limit != tt.wantLimit || got.Offset != tt.wantOffset {
					t.Errorf("Limit/Offset = %d/%d, want %d/%d", got.Limit, got.Offset, tt.wantLimit, tt.wantOffset)
				}
				if len(got.Products) != 1 {
					t.Errorf("Products = %+v, want 1 entry", got.Products)
				}
				if tt.repo.lastCategory != tt.wantCategory || tt.repo.lastActive != tt.wantActive {
					t.Errorf("repository received category=%q active_only=%v, want category=%q active_only=%v",
						tt.repo.lastCategory, tt.repo.lastActive, tt.wantCategory, tt.wantActive)
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

func TestProductsHandler_Get(t *testing.T) {
	product := &domain.Product{ID: uuid.New(), Name: "Widget", SKU: "WID-1", PriceCents: 999, Currency: "USD"}

	tests := []struct {
		name            string
		productID       string
		repo            *fakeProductRepository
		wantStatus      int
		wantCode        string
		wantStaleHeader bool
	}{
		{
			name:       "existing product",
			productID:  product.ID.String(),
			repo:       &fakeProductRepository{productsByID: map[uuid.UUID]*domain.Product{product.ID: product}},
			wantStatus: http.StatusOK,
		},
		{
			name:      "stale product served from cache fallback",
			productID: product.ID.String(),
			repo: &fakeProductRepository{
				productsByID: map[uuid.UUID]*domain.Product{product.ID: product},
				getStale:     true,
			},
			wantStatus:      http.StatusOK,
			wantStaleHeader: true,
		},
		{
			name:       "unknown product id",
			productID:  uuid.New().String(),
			repo:       &fakeProductRepository{},
			wantStatus: http.StatusNotFound,
			wantCode:   "PRODUCT_NOT_FOUND",
		},
		{
			name:       "invalid product id",
			productID:  "not-a-uuid",
			repo:       &fakeProductRepository{},
			wantStatus: http.StatusBadRequest,
			wantCode:   "BAD_REQUEST",
		},
		{
			name:       "repository failure",
			productID:  product.ID.String(),
			repo:       &fakeProductRepository{getErr: errTestRepository},
			wantStatus: http.StatusInternalServerError,
			wantCode:   "INTERNAL_SERVER_ERROR",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := newTestMux(t, tt.repo, &fakeInventoryRepository{})

			req := httptest.NewRequest(http.MethodGet, "/api/v1/products/"+tt.productID, nil)
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body=%s)", w.Code, tt.wantStatus, w.Body.String())
			}

			if tt.wantStatus == http.StatusOK {
				var got productResponse
				if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}
				if got.ID != product.ID.String() {
					t.Errorf("ID = %v, want %v", got.ID, product.ID.String())
				}

				gotStaleHeader := w.Header().Get("X-Cache") == "stale"
				if gotStaleHeader != tt.wantStaleHeader {
					t.Errorf("X-Cache header present = %v, want %v", gotStaleHeader, tt.wantStaleHeader)
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

func TestProductsHandler_Inventory(t *testing.T) {
	productID := uuid.New()
	stock := &domain.Stock{ProductID: productID, QuantityAvailable: 7, QuantityReserved: 3}

	tests := []struct {
		name       string
		productID  string
		repo       *fakeInventoryRepository
		wantStatus int
		wantCode   string
	}{
		{
			name:       "existing stock",
			productID:  productID.String(),
			repo:       &fakeInventoryRepository{stockByProduct: map[uuid.UUID]*domain.Stock{productID: stock}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "unknown product id",
			productID:  uuid.New().String(),
			repo:       &fakeInventoryRepository{},
			wantStatus: http.StatusNotFound,
			wantCode:   "PRODUCT_NOT_FOUND",
		},
		{
			name:       "invalid product id",
			productID:  "not-a-uuid",
			repo:       &fakeInventoryRepository{},
			wantStatus: http.StatusBadRequest,
			wantCode:   "BAD_REQUEST",
		},
		{
			name:       "repository failure",
			productID:  productID.String(),
			repo:       &fakeInventoryRepository{getErr: errTestRepository},
			wantStatus: http.StatusInternalServerError,
			wantCode:   "INTERNAL_SERVER_ERROR",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := newTestMux(t, &fakeProductRepository{}, tt.repo)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/inventory/"+tt.productID, nil)
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body=%s)", w.Code, tt.wantStatus, w.Body.String())
			}

			if tt.wantStatus == http.StatusOK {
				var got inventoryResponse
				if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}
				if got.QuantityAvailable != stock.QuantityAvailable || got.QuantityReserved != stock.QuantityReserved {
					t.Errorf("got = %+v, want available=%d reserved=%d", got, stock.QuantityAvailable, stock.QuantityReserved)
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
