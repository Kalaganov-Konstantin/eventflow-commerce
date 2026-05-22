// Package client talks synchronously to the other services the order saga depends on.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	apperrors "github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/errors"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/resilience"
	"github.com/google/uuid"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Default circuit breaker and retry settings shared by the order service's remote clients.
const (
	breakerFailureThreshold = 5
	breakerWindow           = 60 * time.Second
	breakerOpenTimeout      = 30 * time.Second

	retryMaxAttempts = 3
	retryBaseDelay   = 50 * time.Millisecond
	retryMaxDelay    = 500 * time.Millisecond
)

// newBreaker builds a circuit breaker configured with the order client's default thresholds,
// named for the specific remote operation it guards.
func newBreaker(name string) *resilience.Breaker {
	return resilience.NewBreaker(resilience.Config{
		Name:             name,
		FailureThreshold: breakerFailureThreshold,
		Window:           breakerWindow,
		OpenTimeout:      breakerOpenTimeout,
	})
}

// wrapBreakerOpen turns a resilience.ErrOpen into an *apperrors.AppError with code and message,
// so callers only ever see the client's usual AppError shape. Any other error, including nil, is
// returned unchanged.
func wrapBreakerOpen(err error, code, message string) error {
	if err == nil || !stderrors.Is(err, resilience.ErrOpen) {
		return err
	}
	return &apperrors.AppError{
		Code:     code,
		Message:  message,
		Details:  err.Error(),
		HTTPCode: http.StatusServiceUnavailable,
	}
}

// ReserveItem is a single product/quantity pair to reserve or that was reserved.
type ReserveItem struct {
	ProductID uuid.UUID
	Quantity  int
}

// InventoryClient reserves and releases stock through the inventory service's HTTP API.
type InventoryClient struct {
	baseURL        string
	httpClient     *http.Client
	reserveBreaker *resilience.Breaker
	releaseBreaker *resilience.Breaker
}

// NewInventoryClient builds an InventoryClient talking to baseURL, bounding every request by
// timeout. Every request opens a client span and carries the current trace context to the
// inventory service.
func NewInventoryClient(baseURL string, timeout time.Duration) *InventoryClient {
	return &InventoryClient{
		baseURL:        strings.TrimRight(baseURL, "/"),
		httpClient:     &http.Client{Timeout: timeout, Transport: otelhttp.NewTransport(http.DefaultTransport)},
		reserveBreaker: newBreaker("inventory_reserve"),
		releaseBreaker: newBreaker("inventory_release"),
	}
}

type reserveItemRequest struct {
	ProductID string `json:"product_id"`
	Quantity  int    `json:"quantity"`
}

type reserveRequest struct {
	OrderID string               `json:"order_id"`
	Items   []reserveItemRequest `json:"items"`
}

// Reserve asks the inventory service to reserve items for orderID. A stock shortage comes back as
// an *errors.AppError with code INSUFFICIENT_INVENTORY and HTTP status 409.
//
// Reserve is guarded by a circuit breaker but not retried: reserving is not idempotent without an
// idempotency key, so a retried call after an ambiguous failure could double-reserve stock.
func (c *InventoryClient) Reserve(ctx context.Context, orderID uuid.UUID, items []ReserveItem) error {
	reqItems := make([]reserveItemRequest, len(items))
	for i, item := range items {
		reqItems[i] = reserveItemRequest{ProductID: item.ProductID.String(), Quantity: item.Quantity}
	}

	body, err := json.Marshal(reserveRequest{OrderID: orderID.String(), Items: reqItems})
	if err != nil {
		return fmt.Errorf("marshal reserve request: %w", err)
	}

	_, execErr := resilience.Execute(c.reserveBreaker, func() (struct{}, error) {
		return struct{}{}, c.do(ctx, http.MethodPost, "/api/v1/inventory/reservations", body)
	})
	return wrapBreakerOpen(execErr, "INVENTORY_SERVICE_UNAVAILABLE", "inventory service circuit breaker is open")
}

// Release asks the inventory service to release every reservation held for orderID. Releasing an
// order with no active reservation is not an error, so Release is both guarded by a circuit
// breaker and retried on transient failures.
func (c *InventoryClient) Release(ctx context.Context, orderID uuid.UUID) error {
	retryCfg := resilience.RetryConfig{
		MaxAttempts: retryMaxAttempts,
		BaseDelay:   retryBaseDelay,
		MaxDelay:    retryMaxDelay,
		Retryable:   isRetryableInventoryError,
	}

	err := resilience.Retry(ctx, retryCfg, func() error {
		_, execErr := resilience.Execute(c.releaseBreaker, func() (struct{}, error) {
			return struct{}{}, c.do(ctx, http.MethodDelete, "/api/v1/inventory/reservations/"+orderID.String(), nil)
		})
		return execErr
	})
	return wrapBreakerOpen(err, "INVENTORY_SERVICE_UNAVAILABLE", "inventory service circuit breaker is open")
}

// isRetryableInventoryError reports whether a failed inventory call is worth retrying: a
// transport failure or a server-side error might succeed next time, a rejected request or an
// open breaker will not.
func isRetryableInventoryError(err error) bool {
	if stderrors.Is(err, resilience.ErrOpen) {
		return false
	}
	var appErr *apperrors.AppError
	if stderrors.As(err, &appErr) {
		return appErr.HTTPCode >= http.StatusInternalServerError
	}
	return false
}

// do issues an HTTP request against the inventory service and turns a non-2xx response, or a
// transport failure, into an *errors.AppError.
func (c *InventoryClient) do(ctx context.Context, method, path string, body []byte) error {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("build inventory request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return transportError(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		return nil
	}
	return errorFromResponse(resp)
}

// transportError maps a failure to reach the inventory service to an *errors.AppError, telling a
// timeout apart from any other connection failure.
func transportError(err error) error {
	var netErr net.Error
	if stderrors.As(err, &netErr) && netErr.Timeout() {
		return &apperrors.AppError{
			Code:     "INVENTORY_SERVICE_TIMEOUT",
			Message:  "inventory service request timed out",
			Details:  err.Error(),
			HTTPCode: http.StatusGatewayTimeout,
		}
	}
	return &apperrors.AppError{
		Code:     "INVENTORY_SERVICE_UNAVAILABLE",
		Message:  "inventory service request failed",
		Details:  err.Error(),
		HTTPCode: http.StatusServiceUnavailable,
	}
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details string `json:"details"`
}

// errorFromResponse rebuilds the *errors.AppError the inventory service reported, falling back to
// a generic one when the body is not the expected shape.
func errorFromResponse(resp *http.Response) error {
	var body errorBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || body.Code == "" {
		return &apperrors.AppError{
			Code:     "INVENTORY_ERROR",
			Message:  fmt.Sprintf("inventory service returned status %d", resp.StatusCode),
			HTTPCode: resp.StatusCode,
		}
	}
	return &apperrors.AppError{
		Code:     body.Code,
		Message:  body.Message,
		Details:  body.Details,
		HTTPCode: resp.StatusCode,
	}
}
