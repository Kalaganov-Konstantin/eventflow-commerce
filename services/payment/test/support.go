// Package test holds the payment service's integration tests against a real postgres, started by
// `make test-deps-up` (see docker-compose.test.yml). This file carries no build tag so that
// `go test ./...` still finds a file to build in this package; the tests themselves are gated
// behind the integration tag.
package test

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq" // registers the postgres driver
)

const (
	defaultTestDatabaseURL = "postgres://payments_user:payments_pass@localhost:5433/payments?sslmode=disable"
	testConnectTimeout     = 5 * time.Second
)

// testDatabaseURL returns the payment integration test database URL, PAYMENT_TEST_DATABASE_URL
// overriding the default docker-compose.test.yml connection.
func testDatabaseURL() string {
	if v := os.Getenv("PAYMENT_TEST_DATABASE_URL"); v != "" {
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
	db.SetMaxOpenConns(10)

	ctx, cancel := context.WithTimeout(context.Background(), testConnectTimeout)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("payment integration test database not reachable at %s, run `make test-deps-up` first: %v", testDatabaseURL(), err)
	}

	t.Cleanup(func() { _ = db.Close() })
	return db
}
