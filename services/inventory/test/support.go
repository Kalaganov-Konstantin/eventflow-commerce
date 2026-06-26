// Package test holds the inventory service's integration tests against a real postgres, started
// by `make test-deps-up` (see docker-compose.test.yml). This file carries no build tag so that
// `go test ./...` still finds a file to build in this package; the tests themselves are gated
// behind the integration tag.
package test

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/inventory/internal/domain"
	"github.com/google/uuid"
	_ "github.com/lib/pq" // registers the postgres driver
)

const (
	defaultTestDatabaseURL = "postgres://inventory_user:inventory_pass@localhost:5433/inventory?sslmode=disable"
	testConnectTimeout     = 5 * time.Second
)

// testDatabaseURL returns the inventory integration test database URL,
// INVENTORY_TEST_DATABASE_URL overriding the default docker-compose.test.yml connection.
func testDatabaseURL() string {
	if v := os.Getenv("INVENTORY_TEST_DATABASE_URL"); v != "" {
		return v
	}
	return defaultTestDatabaseURL
}

// openTestDB opens a connection to the integration test database, verifies it is reachable and
// closes it when t ends. It fails the test immediately when the database cannot be reached, since
// these tests require `make test-deps-up` to have already run.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("postgres", testDatabaseURL())
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	db.SetMaxOpenConns(20)

	ctx, cancel := context.WithTimeout(context.Background(), testConnectTimeout)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("inventory integration test database not reachable at %s, run `make test-deps-up` first: %v", testDatabaseURL(), err)
	}

	t.Cleanup(func() { _ = db.Close() })
	return db
}

// seedProductWithStock inserts a product and its inventory row with the given available
// quantity, returning the product id.
func seedProductWithStock(t *testing.T, db *sql.DB, available int) uuid.UUID {
	t.Helper()

	ctx := context.Background()
	product := &domain.Product{
		ID:         uuid.New(),
		Name:       "Integration Test Widget",
		SKU:        "TEST-" + uuid.New().String(),
		PriceCents: 999,
		Currency:   "USD",
		IsActive:   true,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
		Version:    1,
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO products (id, name, sku, price_cents, currency, is_active, created_at, updated_at, version)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, product.ID, product.Name, product.SKU, product.PriceCents, product.Currency, product.IsActive,
		product.CreatedAt, product.UpdatedAt, product.Version); err != nil {
		t.Fatalf("insert product: %v", err)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO inventory (product_id, quantity_available, quantity_reserved)
		VALUES ($1, $2, 0)
	`, product.ID, available); err != nil {
		t.Fatalf("insert inventory: %v", err)
	}

	return product.ID
}
