// Package e2e drives the order flow end to end through a running `make demo` stack. This file
// carries no build tag so that `go test ./...` still finds a file to build in this package; the
// test itself is gated behind the e2e tag.
package e2e

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	_ "github.com/lib/pq" // registers the postgres driver
)

const (
	defaultGatewayURL             = "http://localhost:8080"
	defaultNotificationURL        = "http://localhost:8084"
	defaultInventoryDatabaseURL   = "postgres://inventory_user:inventory_pass@localhost:5432/inventory?sslmode=disable"
	requestTimeout                = 10 * time.Second
	orderConfirmationPollInterval = 500 * time.Millisecond
)

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func gatewayURL() string      { return envOr("E2E_GATEWAY_URL", defaultGatewayURL) }
func notificationURL() string { return envOr("E2E_NOTIFICATION_URL", defaultNotificationURL) }

// openInventoryDB opens a direct connection to the inventory database, used only to seed a
// product's stock: there is no HTTP endpoint to create one, since the running stack expects
// products to already exist in the catalog.
func openInventoryDB(t *testing.T) *sql.DB {
	t.Helper()

	url := envOr("E2E_INVENTORY_DATABASE_URL", defaultInventoryDatabaseURL)
	db, err := sql.Open("postgres", url)
	if err != nil {
		t.Fatalf("open inventory database: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("inventory database not reachable at %s, is `make demo` running? %v", url, err)
	}

	t.Cleanup(func() { _ = db.Close() })
	return db
}

// seedProductWithStock inserts a product and its inventory row with the given available
// quantity, returning the product id.
func seedProductWithStock(t *testing.T, db *sql.DB, available int) uuid.UUID {
	t.Helper()

	ctx := context.Background()
	productID := uuid.New()
	now := time.Now().UTC()

	if _, err := db.ExecContext(ctx, `
		INSERT INTO products (id, name, sku, price_cents, currency, is_active, created_at, updated_at, version)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, productID, "E2E Test Widget", "E2E-"+productID.String(), 2999, "USD", true, now, now, 1); err != nil {
		t.Fatalf("insert product: %v", err)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO inventory (product_id, quantity_available, quantity_reserved)
		VALUES ($1, $2, 0)
	`, productID, available); err != nil {
		t.Fatalf("insert inventory: %v", err)
	}

	return productID
}

// stockOf reads a product's current available and reserved quantities directly from the
// database.
func stockOf(t *testing.T, db *sql.DB, productID uuid.UUID) (available, reserved int) {
	t.Helper()

	ctx := context.Background()
	if err := db.QueryRowContext(ctx, `
		SELECT quantity_available, quantity_reserved FROM inventory WHERE product_id = $1
	`, productID).Scan(&available, &reserved); err != nil {
		t.Fatalf("select inventory for product %s: %v", productID, err)
	}
	return available, reserved
}

// issueJWT signs a token for customerID using the same secret and claim shape the running
// api-gateway validates: HMAC signing plus non-empty user_id, email and role.
func issueJWT(t *testing.T, customerID uuid.UUID) string {
	t.Helper()

	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		t.Fatal("JWT_SECRET must be set to the same secret the running api-gateway uses, see .env")
	}

	claims := jwt.MapClaims{
		"user_id": customerID.String(),
		"email":   customerID.String() + "@customers.eventflow.local",
		"role":    "customer",
		"exp":     time.Now().Add(time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}
	return signed
}

// apiClient is a small JSON HTTP client authenticated with a customer's bearer token.
type apiClient struct {
	baseURL string
	token   string
	headers map[string]string
	http    *http.Client
}

// newAPIClient builds a client that authenticates every request to baseURL with token as a
// bearer token. Pass an empty token for a service called directly rather than through the
// gateway, which is what actually validates and strips the JWT.
func newAPIClient(baseURL, token string) *apiClient {
	return &apiClient{baseURL: baseURL, token: token, http: &http.Client{Timeout: requestTimeout}}
}

// withHeader returns a copy of c that also sets header on every request, used for talking to a
// backend service directly with the header the gateway would otherwise inject after validating
// the caller's JWT.
func (c *apiClient) withHeader(key, value string) *apiClient {
	headers := make(map[string]string, len(c.headers)+1)
	for k, v := range c.headers {
		headers[k] = v
	}
	headers[key] = value
	return &apiClient{baseURL: c.baseURL, token: c.token, headers: headers, http: c.http}
}

// do issues method against path, decoding a JSON body into out (when out is non-nil) and
// returning the HTTP status code.
func (c *apiClient) do(t *testing.T, method, path string, body any, out any) int {
	t.Helper()

	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		reader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, c.baseURL+path, reader)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	for key, value := range c.headers {
		req.Header.Set(key, value)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}

	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			t.Fatalf("unmarshal response body %q: %v", data, err)
		}
	}

	return resp.StatusCode
}

// waitFor polls cond until it returns true or timeout elapses, failing the test on timeout. msg
// describes what was being waited for, for the failure message.
func waitFor(t *testing.T, timeout time.Duration, msg string, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s waiting for %s", timeout, msg)
		}
		time.Sleep(orderConfirmationPollInterval)
	}
}

// orderItem builds a create-order request item without importing the order service's own
// request type.
func orderItem(productID uuid.UUID, quantity int, unitPriceCents int64) map[string]any {
	return map[string]any{
		"product_id":       productID.String(),
		"product_name":     "E2E Test Widget",
		"quantity":         quantity,
		"unit_price_cents": unitPriceCents,
	}
}
