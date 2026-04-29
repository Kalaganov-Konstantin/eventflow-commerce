package service

import (
	"context"
	"time"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/inventory/internal/domain"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// productCacheTTL is how long a cached product read stays valid for the fast cache-aside path.
const productCacheTTL = 5 * time.Minute

// productFallbackTTL is how long a product stays available as a stale fallback once the database
// fails, well past productCacheTTL so it survives the normal cache-aside entry expiring.
const productFallbackTTL = 24 * time.Hour

// productCacheFallbackTotal counts product reads served from a stale cache entry after the
// database failed. It is registered once per process and shared by every ProductService.
var productCacheFallbackTotal = promauto.NewCounter(prometheus.CounterOpts{
	Name: "inventory_product_cache_fallback_total",
	Help: "Total number of product reads served from a stale cache entry after a database error.",
})

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

// GetByID returns the product with the given id, and whether it was served stale. When a cache is
// configured, it is read first and filled on a miss; a cache error falls back to the repository
// rather than failing the read. When the repository itself fails, GetByID falls back to the
// longer-lived fallback entry for id, if one was ever written, and reports it as stale instead of
// failing the read; the repository error is returned as-is when there is nothing to fall back to
// or the cache itself is unavailable.
func (s *ProductService) GetByID(ctx context.Context, id uuid.UUID) (*domain.Product, bool, error) {
	if s.cache == nil {
		product, err := s.repo.GetByID(ctx, id)
		return product, false, err
	}

	key := productCacheKey(id)

	var cached domain.Product
	if hit, err := s.cache.GetJSON(ctx, key, &cached); err == nil && hit {
		return &cached, false, nil
	}

	product, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if stale, ok := s.staleFromCache(ctx, id); ok {
			productCacheFallbackTotal.Inc()
			return stale, true, nil
		}
		return nil, false, err
	}

	_ = s.cache.SetJSON(ctx, key, product, productCacheTTL)
	_ = s.cache.SetJSON(ctx, productFallbackCacheKey(id), product, productFallbackTTL)
	return product, false, nil
}

// staleFromCache looks up the long-lived fallback entry for id, used once the database itself has
// failed and the fast cache-aside entry has already expired or was never populated.
func (s *ProductService) staleFromCache(ctx context.Context, id uuid.UUID) (*domain.Product, bool) {
	var cached domain.Product
	hit, err := s.cache.GetJSON(ctx, productFallbackCacheKey(id), &cached)
	if err != nil || !hit {
		return nil, false
	}
	return &cached, true
}

// List returns a page of products. List reads are not cached, since the pagination and filter
// combinations make the key space unbounded.
func (s *ProductService) List(ctx context.Context, category string, activeOnly bool, limit, offset int) ([]*domain.Product, error) {
	return s.repo.List(ctx, category, activeOnly, limit, offset)
}

func productCacheKey(id uuid.UUID) string {
	return "product:" + id.String()
}

func productFallbackCacheKey(id uuid.UUID) string {
	return "product:" + id.String() + ":fallback"
}
