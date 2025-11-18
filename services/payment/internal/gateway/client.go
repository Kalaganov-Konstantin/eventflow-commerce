// Package gateway talks to the external payment processor. There is no real provider to integrate
// with, so StubClient stands in for it with deterministic behavior.
package gateway

import (
	"context"

	"github.com/google/uuid"
)

// ChargeRequest carries the amount to charge through the gateway. Money fields are integer minor
// units (cents).
type ChargeRequest struct {
	PaymentID   uuid.UUID
	AmountCents int64
	Currency    string
}

// Result is the gateway's response to a charge attempt.
type Result struct {
	Approved      bool
	TransactionID string
	DeclineCode   string
}

// Client charges a payment through the external payment gateway.
type Client interface {
	Charge(ctx context.Context, req ChargeRequest) (Result, error)
}

// Config controls the stub gateway's deterministic behavior.
type Config struct {
	MaxAmountCents int64
}

// StubClient is a deterministic stand-in for the real payment gateway: no provider exists to call, so
// it approves positive amounts within MaxAmountCents and declines everything else.
type StubClient struct {
	maxAmountCents int64
}

// NewStubClient builds a StubClient bounded by cfg.MaxAmountCents.
func NewStubClient(cfg Config) *StubClient {
	return &StubClient{maxAmountCents: cfg.MaxAmountCents}
}

// Charge approves req when its amount is positive and within the configured limit, and declines it
// with code insufficient_funds otherwise.
func (c *StubClient) Charge(_ context.Context, req ChargeRequest) (Result, error) {
	if req.AmountCents <= 0 || req.AmountCents > c.maxAmountCents {
		return Result{DeclineCode: "insufficient_funds"}, nil
	}
	return Result{Approved: true, TransactionID: "stub_" + req.PaymentID.String()}, nil
}
