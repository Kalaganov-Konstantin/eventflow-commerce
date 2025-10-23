package handler

import (
	"bytes"
	"context"
	"encoding/json"
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/order/internal/domain"
	apperrors "github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/errors"
	"github.com/google/uuid"
	"go.uber.org/zap/zaptest"
)

var errTestRepository = stderrors.New("repository failure")

// fakeOrderRepository is an in-memory OrderRepository test double.
type fakeOrderRepository struct {
	saveErr error
	saved   []*domain.Order

	ordersByID map[uuid.UUID]*domain.Order
	getErr     error

	listResult []*domain.Order
	listErr    error
	lastLimit  int
	lastOffset int
}

func newFakeOrderRepository() *fakeOrderRepository {
	return &fakeOrderRepository{ordersByID: make(map[uuid.UUID]*domain.Order)}
}

func (f *fakeOrderRepository) Save(_ context.Context, order *domain.Order) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saved = append(f.saved, order)
	f.ordersByID[order.ID] = order
	return nil
}

func (f *fakeOrderRepository) GetByID(_ context.Context, id uuid.UUID) (*domain.Order, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	order, ok := f.ordersByID[id]
	if !ok {
		return nil, apperrors.NewOrderNotFound(id.String())
	}
	return order, nil
}

func (f *fakeOrderRepository) ListByCustomer(_ context.Context, _ uuid.UUID, limit, offset int) ([]*domain.Order, error) {
	f.lastLimit = limit
	f.lastOffset = offset
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listResult, nil
}

func newTestMux(t *testing.T, repo OrderRepository) *http.ServeMux {
	t.Helper()
	h := NewOrdersHandler(repo, zaptest.NewLogger(t))
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/orders", h.Create)
	mux.HandleFunc("GET /api/v1/orders/{id}", h.Get)
	mux.HandleFunc("GET /api/v1/orders", h.List)
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

func TestOrdersHandler_Create(t *testing.T) {
	validBody := `{"items":[{"product_id":"` + uuid.New().String() + `","product_name":"Widget","quantity":2,"unit_price":9.99}],"currency":"USD"}`

	tests := []struct {
		name       string
		userID     string
		body       string
		repo       *fakeOrderRepository
		wantStatus int
		wantCode   string
	}{
		{
			name:       "valid order",
			userID:     uuid.New().String(),
			body:       validBody,
			repo:       newFakeOrderRepository(),
			wantStatus: http.StatusCreated,
		},
		{
			name:       "missing X-User-ID header",
			userID:     "",
			body:       validBody,
			repo:       newFakeOrderRepository(),
			wantStatus: http.StatusUnauthorized,
			wantCode:   "UNAUTHORIZED",
		},
		{
			name:       "invalid X-User-ID header",
			userID:     "not-a-uuid",
			body:       validBody,
			repo:       newFakeOrderRepository(),
			wantStatus: http.StatusUnauthorized,
			wantCode:   "UNAUTHORIZED",
		},
		{
			name:       "malformed JSON body",
			userID:     uuid.New().String(),
			body:       `{`,
			repo:       newFakeOrderRepository(),
			wantStatus: http.StatusBadRequest,
			wantCode:   "BAD_REQUEST",
		},
		{
			name:       "no items",
			userID:     uuid.New().String(),
			body:       `{"items":[],"currency":"USD"}`,
			repo:       newFakeOrderRepository(),
			wantStatus: http.StatusBadRequest,
			wantCode:   "VALIDATION_ERROR",
		},
		{
			name:       "invalid item product id",
			userID:     uuid.New().String(),
			body:       `{"items":[{"product_id":"not-a-uuid","product_name":"Widget","quantity":1,"unit_price":1}],"currency":"USD"}`,
			repo:       newFakeOrderRepository(),
			wantStatus: http.StatusBadRequest,
			wantCode:   "VALIDATION_ERROR",
		},
		{
			name:       "repository failure",
			userID:     uuid.New().String(),
			body:       validBody,
			repo:       &fakeOrderRepository{saveErr: errTestRepository},
			wantStatus: http.StatusInternalServerError,
			wantCode:   "INTERNAL_SERVER_ERROR",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := newTestMux(t, tt.repo)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", bytes.NewBufferString(tt.body))
			if tt.userID != "" {
				req.Header.Set("X-User-ID", tt.userID)
			}
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body=%s)", w.Code, tt.wantStatus, w.Body.String())
			}

			if tt.wantStatus == http.StatusCreated {
				var got orderResponse
				if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}
				if got.CustomerID != tt.userID {
					t.Errorf("CustomerID = %v, want %v", got.CustomerID, tt.userID)
				}
				if got.Status != string(domain.StatusPending) {
					t.Errorf("Status = %v, want %v", got.Status, domain.StatusPending)
				}
				if got.TotalAmount != 19.98 {
					t.Errorf("TotalAmount = %v, want 19.98", got.TotalAmount)
				}
				if len(tt.repo.saved) != 1 {
					t.Errorf("expected order to be saved, saved = %d", len(tt.repo.saved))
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

func TestOrdersHandler_Get(t *testing.T) {
	owner := uuid.New()
	other := uuid.New()
	order := &domain.Order{
		ID:          uuid.New(),
		CustomerID:  owner,
		Status:      domain.StatusPending,
		TotalAmount: 19.98,
		Currency:    "USD",
	}

	tests := []struct {
		name       string
		userID     string
		orderID    string
		repo       *fakeOrderRepository
		wantStatus int
		wantCode   string
	}{
		{
			name:       "order belongs to the caller",
			userID:     owner.String(),
			orderID:    order.ID.String(),
			repo:       &fakeOrderRepository{ordersByID: map[uuid.UUID]*domain.Order{order.ID: order}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "unknown order id",
			userID:     owner.String(),
			orderID:    uuid.New().String(),
			repo:       newFakeOrderRepository(),
			wantStatus: http.StatusNotFound,
			wantCode:   "ORDER_NOT_FOUND",
		},
		{
			name:       "invalid order id",
			userID:     owner.String(),
			orderID:    "not-a-uuid",
			repo:       newFakeOrderRepository(),
			wantStatus: http.StatusBadRequest,
			wantCode:   "BAD_REQUEST",
		},
		{
			name:       "order belongs to another customer",
			userID:     other.String(),
			orderID:    order.ID.String(),
			repo:       &fakeOrderRepository{ordersByID: map[uuid.UUID]*domain.Order{order.ID: order}},
			wantStatus: http.StatusForbidden,
			wantCode:   "FORBIDDEN",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := newTestMux(t, tt.repo)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/orders/"+tt.orderID, nil)
			req.Header.Set("X-User-ID", tt.userID)
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body=%s)", w.Code, tt.wantStatus, w.Body.String())
			}

			if tt.wantStatus == http.StatusOK {
				var got orderResponse
				if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}
				if got.ID != order.ID.String() {
					t.Errorf("ID = %v, want %v", got.ID, order.ID.String())
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

func TestOrdersHandler_List(t *testing.T) {
	sampleOrder := &domain.Order{ID: uuid.New(), CustomerID: uuid.New(), Status: domain.StatusPending}

	tests := []struct {
		name       string
		query      string
		wantStatus int
		wantCode   string
		wantLimit  int
		wantOffset int
	}{
		{
			name:       "default pagination",
			query:      "",
			wantStatus: http.StatusOK,
			wantLimit:  20,
			wantOffset: 0,
		},
		{
			name:       "custom limit and offset",
			query:      "?limit=5&offset=10",
			wantStatus: http.StatusOK,
			wantLimit:  5,
			wantOffset: 10,
		},
		{
			name:       "limit capped at 100",
			query:      "?limit=500",
			wantStatus: http.StatusOK,
			wantLimit:  100,
			wantOffset: 0,
		},
		{
			name:       "invalid limit",
			query:      "?limit=abc",
			wantStatus: http.StatusBadRequest,
			wantCode:   "VALIDATION_ERROR",
		},
		{
			name:       "negative offset",
			query:      "?offset=-1",
			wantStatus: http.StatusBadRequest,
			wantCode:   "VALIDATION_ERROR",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeOrderRepository{listResult: []*domain.Order{sampleOrder}}
			mux := newTestMux(t, repo)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/orders"+tt.query, nil)
			req.Header.Set("X-User-ID", uuid.New().String())
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body=%s)", w.Code, tt.wantStatus, w.Body.String())
			}

			if tt.wantStatus == http.StatusOK {
				var got listOrdersResponse
				if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}
				if got.Limit != tt.wantLimit {
					t.Errorf("Limit = %v, want %v", got.Limit, tt.wantLimit)
				}
				if got.Offset != tt.wantOffset {
					t.Errorf("Offset = %v, want %v", got.Offset, tt.wantOffset)
				}
				if len(got.Orders) != 1 {
					t.Errorf("Orders = %v, want 1 entry", got.Orders)
				}
				if repo.lastLimit != tt.wantLimit || repo.lastOffset != tt.wantOffset {
					t.Errorf("repository received limit=%d offset=%d, want limit=%d offset=%d",
						repo.lastLimit, repo.lastOffset, tt.wantLimit, tt.wantOffset)
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
