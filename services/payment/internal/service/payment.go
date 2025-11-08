// Package service implements payment use cases on top of the aggregate repository and the gateway.
package service

import (
	"context"
	"fmt"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/payment/internal/domain"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/payment/internal/gateway"
	apperrors "github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/errors"
	"github.com/google/uuid"
)

// Repository is the persistence port the payment service depends on.
type Repository interface {
	Load(ctx context.Context, id uuid.UUID) (*domain.Payment, error)
	Save(ctx context.Context, payment *domain.Payment) error
	FindByOrderID(ctx context.Context, orderID uuid.UUID) (*domain.Payment, error)
}

// PaymentService implements the payment command use cases: processing a charge and refunding it.
type PaymentService struct {
	repo    Repository
	gateway gateway.Client
}

// NewPaymentService builds a PaymentService backed by repo and gw.
func NewPaymentService(repo Repository, gw gateway.Client) *PaymentService {
	return &PaymentService{repo: repo, gateway: gw}
}

// ProcessPayment initiates a payment for orderID and charges it through the gateway, applying Process
// on approval or Fail on decline. A repeated call for an order that already has a payment returns the
// existing payment instead of creating another one.
func (s *PaymentService) ProcessPayment(ctx context.Context, orderID, customerID uuid.UUID, amountCents int64, currency string) (*domain.Payment, error) {
	existing, err := s.repo.FindByOrderID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}

	payment, err := domain.Initiate(orderID, customerID, amountCents, currency)
	if err != nil {
		return nil, err
	}

	result, err := s.gateway.Charge(ctx, gateway.ChargeRequest{
		PaymentID:   payment.ID,
		AmountCents: amountCents,
		Currency:    currency,
	})
	if err != nil {
		return nil, fmt.Errorf("charge payment: %w", err)
	}

	if result.Approved {
		if err := payment.Process(result.TransactionID); err != nil {
			return nil, err
		}
	} else {
		if err := payment.Fail(result.DeclineCode); err != nil {
			return nil, err
		}
	}

	if err := s.repo.Save(ctx, payment); err != nil {
		return nil, err
	}

	if !result.Approved {
		return nil, apperrors.NewPaymentFailed(result.DeclineCode)
	}
	return payment, nil
}

// RefundPayment reverses a completed payment. It fails with a conflict when the payment is not
// currently completed.
func (s *PaymentService) RefundPayment(ctx context.Context, id uuid.UUID, reason string) (*domain.Payment, error) {
	payment, err := s.repo.Load(ctx, id)
	if err != nil {
		return nil, err
	}

	if err := payment.Refund(reason); err != nil {
		return nil, err
	}

	if err := s.repo.Save(ctx, payment); err != nil {
		return nil, err
	}
	return payment, nil
}
