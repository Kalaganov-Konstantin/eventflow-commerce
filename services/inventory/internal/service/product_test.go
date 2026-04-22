package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/inventory/internal/domain"
	"github.com/google/uuid"
)

var errTestProductRepository = errors.New("repository failure")

// fakeProductRepository is an in-memory ProductRepository test double.
type fakeProductRepository struct {
	product  *domain.Product
	getErr   error
	getCalls int

	listResult   []*domain.Product
	listErr      error
	lastCategory string
	lastActive   bool
	lastLimit    int
	lastOffset   int
}

func (f *fakeProductRepository) GetByID(_ context.Context, _ uuid.UUID) (*domain.Product, error) {
	f.getCalls++
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.product, nil
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

// fakeProductCache is an in-memory ProductCache test double.
type fakeProductCache struct {
	store  map[string][]byte
	getErr error
	setErr error
}

func newFakeProductCache() *fakeProductCache {
	return &fakeProductCache{store: make(map[string][]byte)}
}

func (f *fakeProductCache) GetJSON(_ context.Context, key string, dest any) (bool, error) {
	if f.getErr != nil {
		return false, f.getErr
	}
	raw, ok := f.store[key]
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal(raw, dest)
}

func (f *fakeProductCache) SetJSON(_ context.Context, key string, value any, _ time.Duration) error {
	if f.setErr != nil {
		return f.setErr
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	f.store[key] = data
	return nil
}

func newTestProduct() *domain.Product {
	return &domain.Product{ID: uuid.New(), Name: "Widget", SKU: "WID-1", PriceCents: 999, Currency: "USD"}
}

func TestProductService_GetByID_SecondReadDoesNotHitRepository(t *testing.T) {
	product := newTestProduct()
	repo := &fakeProductRepository{product: product}
	cache := newFakeProductCache()
	svc := NewProductService(repo, cache)

	first, err := svc.GetByID(context.Background(), product.ID)
	if err != nil {
		t.Fatalf("GetByID() first call error = %v", err)
	}
	if first.ID != product.ID {
		t.Errorf("GetByID() first call ID = %v, want %v", first.ID, product.ID)
	}
	if repo.getCalls != 1 {
		t.Fatalf("repository GetByID calls after first read = %d, want 1", repo.getCalls)
	}

	second, err := svc.GetByID(context.Background(), product.ID)
	if err != nil {
		t.Fatalf("GetByID() second call error = %v", err)
	}
	if second.ID != product.ID {
		t.Errorf("GetByID() second call ID = %v, want %v", second.ID, product.ID)
	}
	if repo.getCalls != 1 {
		t.Errorf("repository GetByID calls after second read = %d, want 1 (should be served from cache)", repo.getCalls)
	}
}

func TestProductService_GetByID_NilCacheAlwaysReadsRepository(t *testing.T) {
	product := newTestProduct()
	repo := &fakeProductRepository{product: product}
	svc := NewProductService(repo, nil)

	if _, err := svc.GetByID(context.Background(), product.ID); err != nil {
		t.Fatalf("GetByID() first call error = %v", err)
	}
	if _, err := svc.GetByID(context.Background(), product.ID); err != nil {
		t.Fatalf("GetByID() second call error = %v", err)
	}

	if repo.getCalls != 2 {
		t.Errorf("repository GetByID calls = %d, want 2 (no cache configured)", repo.getCalls)
	}
}

func TestProductService_GetByID_RepositoryError(t *testing.T) {
	repo := &fakeProductRepository{getErr: errTestProductRepository}
	svc := NewProductService(repo, newFakeProductCache())

	if _, err := svc.GetByID(context.Background(), uuid.New()); !errors.Is(err, errTestProductRepository) {
		t.Errorf("GetByID() error = %v, want %v", err, errTestProductRepository)
	}
}

func TestProductService_GetByID_CacheErrorFallsBackToRepository(t *testing.T) {
	product := newTestProduct()
	repo := &fakeProductRepository{product: product}
	cache := newFakeProductCache()
	cache.getErr = errors.New("redis unavailable")
	svc := NewProductService(repo, cache)

	got, err := svc.GetByID(context.Background(), product.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v, want nil", err)
	}
	if got.ID != product.ID {
		t.Errorf("GetByID() ID = %v, want %v", got.ID, product.ID)
	}
	if repo.getCalls != 1 {
		t.Errorf("repository GetByID calls = %d, want 1", repo.getCalls)
	}
}

func TestProductService_List_DelegatesToRepository(t *testing.T) {
	want := []*domain.Product{newTestProduct()}
	repo := &fakeProductRepository{listResult: want}
	svc := NewProductService(repo, newFakeProductCache())

	got, err := svc.List(context.Background(), "gadgets", true, 5, 10)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("List() returned %d products, want %d", len(got), len(want))
	}
	if repo.lastCategory != "gadgets" || !repo.lastActive || repo.lastLimit != 5 || repo.lastOffset != 10 {
		t.Errorf("repository received category=%q active=%v limit=%d offset=%d, want gadgets/true/5/10",
			repo.lastCategory, repo.lastActive, repo.lastLimit, repo.lastOffset)
	}
}
