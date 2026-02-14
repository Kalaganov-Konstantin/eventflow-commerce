package client

import (
	"bytes"
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	apperrors "github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/errors"
	"github.com/google/uuid"
)

// PaymentClient refunds a payment through the payment service's HTTP API.
type PaymentClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewPaymentClient builds a PaymentClient talking to baseURL, bounding every request by timeout.
func NewPaymentClient(baseURL string, timeout time.Duration) *PaymentClient {
	return &PaymentClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: timeout},
	}
}

type refundRequest struct {
	Reason string `json:"reason"`
}

// Refund asks the payment service to refund paymentID. A payment that is not currently completed
// comes back as an *errors.AppError with HTTP status 409.
func (c *PaymentClient) Refund(ctx context.Context, paymentID uuid.UUID, reason string) error {
	body, err := json.Marshal(refundRequest{Reason: reason})
	if err != nil {
		return fmt.Errorf("marshal refund request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/api/v1/payments/"+paymentID.String()+"/refund", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build payment request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return paymentTransportError(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		return nil
	}
	return paymentErrorFromResponse(resp)
}

// paymentTransportError maps a failure to reach the payment service to an *errors.AppError,
// telling a timeout apart from any other connection failure.
func paymentTransportError(err error) error {
	var netErr net.Error
	if stderrors.As(err, &netErr) && netErr.Timeout() {
		return &apperrors.AppError{
			Code:     "PAYMENT_SERVICE_TIMEOUT",
			Message:  "payment service request timed out",
			Details:  err.Error(),
			HTTPCode: http.StatusGatewayTimeout,
		}
	}
	return &apperrors.AppError{
		Code:     "PAYMENT_SERVICE_UNAVAILABLE",
		Message:  "payment service request failed",
		Details:  err.Error(),
		HTTPCode: http.StatusServiceUnavailable,
	}
}

// paymentErrorFromResponse rebuilds the *errors.AppError the payment service reported, falling
// back to a generic one when the body is not the expected shape.
func paymentErrorFromResponse(resp *http.Response) error {
	var body errorBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || body.Code == "" {
		return &apperrors.AppError{
			Code:     "PAYMENT_ERROR",
			Message:  fmt.Sprintf("payment service returned status %d", resp.StatusCode),
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
