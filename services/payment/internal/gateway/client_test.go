package gateway

import (
	"context"
	stderrors "errors"
	"testing"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/resilience"
	"github.com/google/uuid"
	"github.com/sony/gobreaker/v2"
)

func TestStubClient_Charge(t *testing.T) {
	tests := []struct {
		name        string
		amountCents int64
		wantApprove bool
		wantDecline string
	}{
		{name: "approves an amount within the limit", amountCents: 999, wantApprove: true},
		{name: "approves an amount at the limit", amountCents: 1000, wantApprove: true},
		{name: "declines a zero amount", amountCents: 0, wantDecline: "insufficient_funds"},
		{name: "declines a negative amount", amountCents: -100, wantDecline: "insufficient_funds"},
		{name: "declines an amount over the limit", amountCents: 1001, wantDecline: "insufficient_funds"},
	}

	client := NewStubClient(Config{MaxAmountCents: 1000})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paymentID := uuid.New()
			result, err := client.Charge(context.Background(), ChargeRequest{
				PaymentID:   paymentID,
				AmountCents: tt.amountCents,
				Currency:    "USD",
			})
			if err != nil {
				t.Fatalf("Charge() error = %v", err)
			}

			if result.Approved != tt.wantApprove {
				t.Errorf("Approved = %v, want %v", result.Approved, tt.wantApprove)
			}
			if tt.wantApprove {
				if result.TransactionID == "" {
					t.Error("expected a transaction id for an approved charge")
				}
				if result.DeclineCode != "" {
					t.Errorf("DeclineCode = %q, want empty", result.DeclineCode)
				}
			} else {
				if result.DeclineCode != tt.wantDecline {
					t.Errorf("DeclineCode = %q, want %q", result.DeclineCode, tt.wantDecline)
				}
				if result.TransactionID != "" {
					t.Errorf("TransactionID = %q, want empty", result.TransactionID)
				}
			}
		})
	}
}

func TestStubClient_Charge_IsDeterministic(t *testing.T) {
	client := NewStubClient(Config{MaxAmountCents: 1000})
	paymentID := uuid.New()
	req := ChargeRequest{PaymentID: paymentID, AmountCents: 500, Currency: "USD"}

	first, err := client.Charge(context.Background(), req)
	if err != nil {
		t.Fatalf("Charge() error = %v", err)
	}
	second, err := client.Charge(context.Background(), req)
	if err != nil {
		t.Fatalf("Charge() error = %v", err)
	}

	if first != second {
		t.Errorf("Charge() is not deterministic: first = %+v, second = %+v", first, second)
	}
}

var errGatewayDown = stderrors.New("gateway down")

func TestStubClient_Charge_DeclinesWithGatewayUnavailableWhenBreakerIsOpen(t *testing.T) {
	client := NewStubClient(Config{MaxAmountCents: 1000})

	// The stub itself never fails, so trip its breaker directly, standing in for a real provider
	// failing repeatedly.
	for i := 0; i < breakerFailureThreshold; i++ {
		if _, err := resilience.Execute(client.breaker, func() (Result, error) {
			return Result{}, errGatewayDown
		}); !stderrors.Is(err, errGatewayDown) {
			t.Fatalf("priming failure %d: error = %v, want errGatewayDown", i, err)
		}
	}

	result, err := client.Charge(context.Background(), ChargeRequest{
		PaymentID:   uuid.New(),
		AmountCents: 500,
		Currency:    "USD",
	})
	if err != nil {
		t.Fatalf("Charge() error = %v, want nil (an open circuit must decline, not error)", err)
	}
	if result.Approved {
		t.Error("Approved = true, want false while the circuit breaker is open")
	}
	if result.DeclineCode != gatewayUnavailableDeclineCode {
		t.Errorf("DeclineCode = %q, want %q", result.DeclineCode, gatewayUnavailableDeclineCode)
	}
	if result.TransactionID != "" {
		t.Errorf("TransactionID = %q, want empty", result.TransactionID)
	}
}

func TestStubClient_Charge_StaysClosedUnderNormalOperation(t *testing.T) {
	client := NewStubClient(Config{MaxAmountCents: 1000})

	for i := 0; i < breakerFailureThreshold+2; i++ {
		if _, err := client.Charge(context.Background(), ChargeRequest{
			PaymentID:   uuid.New(),
			AmountCents: 500,
			Currency:    "USD",
		}); err != nil {
			t.Fatalf("Charge() call %d error = %v, want nil", i, err)
		}
	}

	if client.breaker.State() != gobreaker.StateClosed {
		t.Errorf("breaker state = %v, want closed", client.breaker.State())
	}
}
