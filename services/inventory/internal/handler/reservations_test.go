package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/inventory/internal/domain"
	apperrors "github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/errors"
	"github.com/google/uuid"
	"go.uber.org/zap/zaptest"
)

// fakeStockRepository is an in-memory StockRepository test double.
type fakeStockRepository struct {
	reserveErr    error
	reservedOrder uuid.UUID
	reservedItems []domain.ReserveItem

	releaseErr     error
	releasedOrders []uuid.UUID
}

func (f *fakeStockRepository) Reserve(_ context.Context, orderID uuid.UUID, items []domain.ReserveItem) error {
	if f.reserveErr != nil {
		return f.reserveErr
	}
	f.reservedOrder = orderID
	f.reservedItems = items
	return nil
}

func (f *fakeStockRepository) Release(_ context.Context, orderID uuid.UUID) error {
	if f.releaseErr != nil {
		return f.releaseErr
	}
	f.releasedOrders = append(f.releasedOrders, orderID)
	return nil
}

func newTestReservationsMux(t *testing.T, repo StockRepository) *http.ServeMux {
	t.Helper()
	h := NewReservationsHandler(repo, zaptest.NewLogger(t))
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/inventory/reservations", h.Reserve)
	mux.HandleFunc("DELETE /api/v1/inventory/reservations/{order_id}", h.Release)
	return mux
}

func TestReservationsHandler_Reserve(t *testing.T) {
	productID := uuid.New()
	validBody := `{"order_id":"` + uuid.New().String() + `","items":[{"product_id":"` + productID.String() + `","quantity":2}]}`

	tests := []struct {
		name       string
		body       string
		repo       *fakeStockRepository
		wantStatus int
		wantCode   string
	}{
		{
			name:       "valid reservation",
			body:       validBody,
			repo:       &fakeStockRepository{},
			wantStatus: http.StatusCreated,
		},
		{
			name:       "malformed JSON body",
			body:       `{`,
			repo:       &fakeStockRepository{},
			wantStatus: http.StatusBadRequest,
			wantCode:   "BAD_REQUEST",
		},
		{
			name:       "invalid order id",
			body:       `{"order_id":"not-a-uuid","items":[{"product_id":"` + productID.String() + `","quantity":1}]}`,
			repo:       &fakeStockRepository{},
			wantStatus: http.StatusBadRequest,
			wantCode:   "VALIDATION_ERROR",
		},
		{
			name:       "no items",
			body:       `{"order_id":"` + uuid.New().String() + `","items":[]}`,
			repo:       &fakeStockRepository{},
			wantStatus: http.StatusBadRequest,
			wantCode:   "VALIDATION_ERROR",
		},
		{
			name:       "invalid item product id",
			body:       `{"order_id":"` + uuid.New().String() + `","items":[{"product_id":"not-a-uuid","quantity":1}]}`,
			repo:       &fakeStockRepository{},
			wantStatus: http.StatusBadRequest,
			wantCode:   "VALIDATION_ERROR",
		},
		{
			name:       "zero item quantity",
			body:       `{"order_id":"` + uuid.New().String() + `","items":[{"product_id":"` + productID.String() + `","quantity":0}]}`,
			repo:       &fakeStockRepository{},
			wantStatus: http.StatusBadRequest,
			wantCode:   "VALIDATION_ERROR",
		},
		{
			name:       "insufficient inventory",
			body:       validBody,
			repo:       &fakeStockRepository{reserveErr: apperrors.NewInsufficientInventory(productID.String(), 2, 1)},
			wantStatus: http.StatusConflict,
			wantCode:   "INSUFFICIENT_INVENTORY",
		},
		{
			name:       "repository failure",
			body:       validBody,
			repo:       &fakeStockRepository{reserveErr: errTestRepository},
			wantStatus: http.StatusInternalServerError,
			wantCode:   "INTERNAL_SERVER_ERROR",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := newTestReservationsMux(t, tt.repo)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/inventory/reservations", bytes.NewBufferString(tt.body))
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body=%s)", w.Code, tt.wantStatus, w.Body.String())
			}

			if tt.wantStatus == http.StatusCreated {
				if tt.repo.reservedOrder == uuid.Nil {
					t.Error("expected the order to be reserved")
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

func TestReservationsHandler_Release(t *testing.T) {
	tests := []struct {
		name       string
		orderID    string
		repo       *fakeStockRepository
		wantStatus int
		wantCode   string
	}{
		{
			name:       "releases an existing reservation",
			orderID:    uuid.New().String(),
			repo:       &fakeStockRepository{},
			wantStatus: http.StatusNoContent,
		},
		{
			name:       "invalid order id",
			orderID:    "not-a-uuid",
			repo:       &fakeStockRepository{},
			wantStatus: http.StatusBadRequest,
			wantCode:   "BAD_REQUEST",
		},
		{
			name:       "repository failure",
			orderID:    uuid.New().String(),
			repo:       &fakeStockRepository{releaseErr: errTestRepository},
			wantStatus: http.StatusInternalServerError,
			wantCode:   "INTERNAL_SERVER_ERROR",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := newTestReservationsMux(t, tt.repo)

			req := httptest.NewRequest(http.MethodDelete, "/api/v1/inventory/reservations/"+tt.orderID, nil)
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body=%s)", w.Code, tt.wantStatus, w.Body.String())
			}

			if tt.wantStatus != http.StatusNoContent {
				appErr := decodeAppError(t, w.Body.Bytes())
				if appErr.Code != tt.wantCode {
					t.Errorf("Code = %v, want %v", appErr.Code, tt.wantCode)
				}
			}
		})
	}

	t.Run("repeated release of the same order is idempotent", func(t *testing.T) {
		repo := &fakeStockRepository{}
		mux := newTestReservationsMux(t, repo)
		orderID := uuid.New().String()

		for i := 0; i < 2; i++ {
			req := httptest.NewRequest(http.MethodDelete, "/api/v1/inventory/reservations/"+orderID, nil)
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			if w.Code != http.StatusNoContent {
				t.Fatalf("call %d: status = %d, want %d", i+1, w.Code, http.StatusNoContent)
			}
		}
	})
}
