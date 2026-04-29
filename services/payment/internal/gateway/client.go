// Package gateway talks to the external payment processor. There is no real provider to integrate
// with, so StubClient stands in for it with deterministic behavior.
package gateway

import (
	"context"
	"time"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/resilience"
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

// gatewayUnavailableDeclineCode is the decline code Charge reports when the circuit breaker
// guarding the gateway call is open, so a payment fails cleanly with a normal decline instead of
// the caller hanging on a gateway that is being given time to recover.
const gatewayUnavailableDeclineCode = "gateway_unavailable"

// Default circuit breaker settings guarding calls to the external payment gateway.
const (
	breakerFailureThreshold = 5
	breakerWindow           = 60 * time.Second
	breakerOpenTimeout      = 30 * time.Second
)

// StubClient is a deterministic stand-in for the real payment gateway: no provider exists to call, so
// it approves positive amounts within MaxAmountCents and declines everything else.
type StubClient struct {
	maxAmountCents int64
	breaker        *resilience.Breaker
}

// NewStubClient builds a StubClient bounded by cfg.MaxAmountCents, with its gateway call guarded
// by a circuit breaker.
func NewStubClient(cfg Config) *StubClient {
	return &StubClient{
		maxAmountCents: cfg.MaxAmountCents,
		breaker: resilience.NewBreaker(resilience.Config{
			Name:             "payment_gateway",
			FailureThreshold: breakerFailureThreshold,
			Window:           breakerWindow,
			OpenTimeout:      breakerOpenTimeout,
		}),
	}
}

// Charge approves req when its amount is positive and within the configured limit, and declines it
// with code insufficient_funds otherwise. The call is guarded by a circuit breaker; while the
// breaker is open, Charge does not run it at all and instead declines immediately with
// gatewayUnavailableDeclineCode.
func (c *StubClient) Charge(_ context.Context, req ChargeRequest) (Result, error) {
	result, err := resilience.Execute(c.breaker, func() (Result, error) {
		return c.charge(req), nil
	})
	if err != nil {
		// charge never fails on its own, so the only error Execute can return here is the circuit
		// being open.
		return Result{DeclineCode: gatewayUnavailableDeclineCode}, nil
	}
	return result, nil
}

// charge is the gateway call itself, wrapped by Charge in the circuit breaker.
func (c *StubClient) charge(req ChargeRequest) Result {
	if req.AmountCents <= 0 || req.AmountCents > c.maxAmountCents {
		return Result{DeclineCode: "insufficient_funds"}
	}
	return Result{Approved: true, TransactionID: "stub_" + req.PaymentID.String()}
}
