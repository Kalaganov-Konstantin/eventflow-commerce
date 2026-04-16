package service

import (
	"context"
	"time"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/inventory/internal/domain"
	"github.com/google/uuid"
)

// productCacheTTL is how long a cached product read stays valid.
const productCacheTTL = 5 * time.Minute

// ProductRepository is the persistence port the product service reads through.
type ProductRepository interface {
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Product, error)
	List(ctx context.Context, category string, activeOnly bool, limit, offset int) ([]*domain.Product, error)
}

// ProductCache is the cache-aside port GetByID reads and fills. A nil ProductCache disables
// caching, which is how the service degrades when Redis is unavailable at startup.
type ProductCache interface {
	GetJSON(ctx context.Context, key string, dest any) (bool, error)
	SetJSON(ctx context.Context, key string, value any, ttl time.Duration) error
}

// ProductService reads products through a cache-aside layer in front of repo.
type ProductService struct {
	repo  ProductRepository
	cache ProductCache
}

// NewProductService builds a ProductService backed by repo. cache may be nil, in which case
// GetByID always reads repo directly.
func NewProductService(repo ProductRepository, cache ProductCache) *ProductService {
	return &ProductService{repo: repo, cache: cache}
}

// GetByID returns the product with the given id. When a cache is configured, it is read first
// and filled on a miss; a cache error falls back to the repository rather than failing the read.
func (s *ProductService) GetByID(ctx context.Context, id uuid.UUID) (*domain.Product, error) {
	if s.cache == nil {
		return s.repo.GetByID(ctx, id)
	}

	key := productCacheKey(id)

	var cached domain.Product
	if hit, err := s.cache.GetJSON(ctx, key, &cached); err == nil && hit {
		return &cached, nil
	}

	product, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	_ = s.cache.SetJSON(ctx, key, product, productCacheTTL)
	return product, nil
}

// List returns a page of products. List reads are not cached, since the pagination and filter
// combinations make the key space unbounded.
func (s *ProductService) List(ctx context.Context, category string, activeOnly bool, limit, offset int) ([]*domain.Product, error) {
	return s.repo.List(ctx, category, activeOnly, limit, offset)
}

func productCacheKey(id uuid.UUID) string {
	return "product:" + id.String()
}
