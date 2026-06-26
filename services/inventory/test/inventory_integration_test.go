//go:build integration

package test

import (
	"context"
	stderrors "errors"
	"sync"
	"testing"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/inventory/internal/domain"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/inventory/internal/repository"
	apperrors "github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/errors"
	"github.com/google/uuid"
)

// TestInventoryIntegration_ConcurrentReservesNeverGoNegative fires more concurrent reservation
// requests for the same product than there is stock to satisfy, against a real postgres, and
// asserts the available quantity never drops below zero: exactly as many requests succeed as
// there was stock, the rest are rejected, and the ledger ends up consistent.
func TestInventoryIntegration_ConcurrentReservesNeverGoNegative(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewStockRepository(db)
	ctx := context.Background()

	const available = 5
	const attempts = 15
	productID := seedProductWithStock(t, db, available)

	results := make(chan error, attempts)
	var wg sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			orderID := uuid.New()
			items := []domain.ReserveItem{{ProductID: productID, Quantity: 1}}
			results <- repo.Reserve(ctx, orderID, items)
		}()
	}
	wg.Wait()
	close(results)

	successes, failures := 0, 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		default:
			var appErr *apperrors.AppError
			if !stderrors.As(err, &appErr) || appErr.Code != "INSUFFICIENT_INVENTORY" {
				t.Fatalf("expected nil or an insufficient inventory error, got %v", err)
			}
			failures++
		}
	}

	if successes != available {
		t.Errorf("successful reservations = %d, want %d", successes, available)
	}
	if failures != attempts-available {
		t.Errorf("failed reservations = %d, want %d", failures, attempts-available)
	}

	stock, err := repo.GetByProductID(ctx, productID)
	if err != nil {
		t.Fatalf("get stock: %v", err)
	}
	if stock.QuantityAvailable != 0 {
		t.Errorf("quantity_available = %d, want 0", stock.QuantityAvailable)
	}
	if stock.QuantityReserved != available {
		t.Errorf("quantity_reserved = %d, want %d", stock.QuantityReserved, available)
	}
}
