package service

import (
	"context"
	stderrors "errors"
	"testing"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/payment/internal/domain"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/payment/internal/gateway"
	apperrors "github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/errors"
	"github.com/google/uuid"
)

var errTestRepository = stderrors.New("repository failure")

// fakeRepository is an in-memory Repository test double.
type fakeRepository struct {
	payments map[uuid.UUID]*domain.Payment
	byOrder  map[uuid.UUID]uuid.UUID
	saved    []*domain.Payment

	loadErr          error
	saveErr          error
	findByOrderIDErr error
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{payments: make(map[uuid.UUID]*domain.Payment), byOrder: make(map[uuid.UUID]uuid.UUID)}
}

func (f *fakeRepository) Load(_ context.Context, id uuid.UUID) (*domain.Payment, error) {
	if f.loadErr != nil {
		return nil, f.loadErr
	}
	payment, ok := f.payments[id]
	if !ok {
		return nil, apperrors.NewNotFound("payment")
	}
	return payment, nil
}

func (f *fakeRepository) Save(_ context.Context, payment *domain.Payment) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saved = append(f.saved, payment)
	f.payments[payment.ID] = payment
	f.byOrder[payment.OrderID] = payment.ID
	payment.ClearPendingEvents()
	return nil
}

func (f *fakeRepository) FindByOrderID(_ context.Context, orderID uuid.UUID) (*domain.Payment, error) {
	if f.findByOrderIDErr != nil {
		return nil, f.findByOrderIDErr
	}
	id, ok := f.byOrder[orderID]
	if !ok {
		return nil, nil
	}
	return f.payments[id], nil
}

// fakeGateway is a gateway.Client test double.
type fakeGateway struct {
	result gateway.Result
	err    error
	calls  []gateway.ChargeRequest
}

func (g *fakeGateway) Charge(_ context.Context, req gateway.ChargeRequest) (gateway.Result, error) {
	g.calls = append(g.calls, req)
	if g.err != nil {
		return gateway.Result{}, g.err
	}
	return g.result, nil
}

func TestPaymentService_ProcessPayment(t *testing.T) {
	t.Run("initiates and completes an approved payment", func(t *testing.T) {
		repo := newFakeRepository()
		gw := &fakeGateway{result: gateway.Result{Approved: true, TransactionID: "txn_1"}}
		svc := NewPaymentService(repo, gw)

		payment, err := svc.ProcessPayment(context.Background(), uuid.New(), uuid.New(), 4999, "USD")
		if err != nil {
			t.Fatalf("ProcessPayment() error = %v", err)
		}
		if payment.Status != domain.StatusCompleted {
			t.Errorf("Status = %v, want %v", payment.Status, domain.StatusCompleted)
		}
		if len(repo.saved) != 1 {
			t.Fatalf("saved = %d payments, want 1", len(repo.saved))
		}
		if len(gw.calls) != 1 {
			t.Errorf("gateway called %d times, want 1", len(gw.calls))
		}
	})

	t.Run("fails and persists a declined payment", func(t *testing.T) {
		repo := newFakeRepository()
		gw := &fakeGateway{result: gateway.Result{DeclineCode: "insufficient_funds"}}
		svc := NewPaymentService(repo, gw)

		payment, err := svc.ProcessPayment(context.Background(), uuid.New(), uuid.New(), 4999, "USD")
		if payment != nil {
			t.Errorf("payment = %+v, want nil", payment)
		}
		var appErr *apperrors.AppError
		if !stderrors.As(err, &appErr) || appErr.Code != "PAYMENT_FAILED" {
			t.Fatalf("error = %v, want PAYMENT_FAILED", err)
		}
		if len(repo.saved) != 1 {
			t.Fatalf("saved = %d payments, want 1", len(repo.saved))
		}
		if repo.saved[0].Status != domain.StatusFailed {
			t.Errorf("saved payment status = %v, want %v", repo.saved[0].Status, domain.StatusFailed)
		}
	})

	t.Run("returns the existing payment without charging again", func(t *testing.T) {
		repo := newFakeRepository()
		orderID, customerID := uuid.New(), uuid.New()
		existing, err := domain.Initiate(orderID, customerID, 4999, "USD")
		if err != nil {
			t.Fatalf("Initiate: %v", err)
		}
		if err := repo.Save(context.Background(), existing); err != nil {
			t.Fatalf("Save: %v", err)
		}

		gw := &fakeGateway{result: gateway.Result{Approved: true, TransactionID: "txn_1"}}
		svc := NewPaymentService(repo, gw)

		got, err := svc.ProcessPayment(context.Background(), orderID, customerID, 4999, "USD")
		if err != nil {
			t.Fatalf("ProcessPayment() error = %v", err)
		}
		if got.ID != existing.ID {
			t.Errorf("ID = %v, want %v", got.ID, existing.ID)
		}
		if len(gw.calls) != 0 {
			t.Errorf("gateway called %d times, want 0", len(gw.calls))
		}
		if len(repo.saved) != 1 {
			t.Errorf("saved = %d payments, want 1 (no new payment created)", len(repo.saved))
		}
	})

	t.Run("returns a validation error for a non-positive amount without charging", func(t *testing.T) {
		repo := newFakeRepository()
		gw := &fakeGateway{result: gateway.Result{Approved: true, TransactionID: "txn_1"}}
		svc := NewPaymentService(repo, gw)

		_, err := svc.ProcessPayment(context.Background(), uuid.New(), uuid.New(), 0, "USD")
		var appErr *apperrors.AppError
		if !stderrors.As(err, &appErr) || appErr.Code != "VALIDATION_ERROR" {
			t.Fatalf("error = %v, want VALIDATION_ERROR", err)
		}
		if len(gw.calls) != 0 {
			t.Errorf("gateway called %d times, want 0", len(gw.calls))
		}
	})

	t.Run("propagates FindByOrderID errors", func(t *testing.T) {
		repo := newFakeRepository()
		repo.findByOrderIDErr = errTestRepository
		svc := NewPaymentService(repo, &fakeGateway{})

		if _, err := svc.ProcessPayment(context.Background(), uuid.New(), uuid.New(), 4999, "USD"); err == nil {
			t.Fatal("expected error, got none")
		}
	})

	t.Run("propagates gateway errors", func(t *testing.T) {
		repo := newFakeRepository()
		gw := &fakeGateway{err: stderrors.New("gateway unreachable")}
		svc := NewPaymentService(repo, gw)

		if _, err := svc.ProcessPayment(context.Background(), uuid.New(), uuid.New(), 4999, "USD"); err == nil {
			t.Fatal("expected error, got none")
		}
		if len(repo.saved) != 0 {
			t.Errorf("saved = %d payments, want 0", len(repo.saved))
		}
	})

	t.Run("propagates repository save errors", func(t *testing.T) {
		repo := newFakeRepository()
		repo.saveErr = errTestRepository
		gw := &fakeGateway{result: gateway.Result{Approved: true, TransactionID: "txn_1"}}
		svc := NewPaymentService(repo, gw)

		if _, err := svc.ProcessPayment(context.Background(), uuid.New(), uuid.New(), 4999, "USD"); !stderrors.Is(err, errTestRepository) {
			t.Fatalf("error = %v, want %v", err, errTestRepository)
		}
	})
}

func TestPaymentService_RefundPayment(t *testing.T) {
	t.Run("refunds a completed payment", func(t *testing.T) {
		repo := newFakeRepository()
		payment, err := domain.Initiate(uuid.New(), uuid.New(), 4999, "USD")
		if err != nil {
			t.Fatalf("Initiate: %v", err)
		}
		if err := payment.Process("txn_1"); err != nil {
			t.Fatalf("Process: %v", err)
		}
		if err := repo.Save(context.Background(), payment); err != nil {
			t.Fatalf("Save: %v", err)
		}

		svc := NewPaymentService(repo, &fakeGateway{})
		got, err := svc.RefundPayment(context.Background(), payment.ID, "customer_request")
		if err != nil {
			t.Fatalf("RefundPayment() error = %v", err)
		}
		if got.Status != domain.StatusRefunded {
			t.Errorf("Status = %v, want %v", got.Status, domain.StatusRefunded)
		}
	})

	t.Run("returns a conflict when the payment is not completed", func(t *testing.T) {
		repo := newFakeRepository()
		payment, err := domain.Initiate(uuid.New(), uuid.New(), 4999, "USD")
		if err != nil {
			t.Fatalf("Initiate: %v", err)
		}
		if err := repo.Save(context.Background(), payment); err != nil {
			t.Fatalf("Save: %v", err)
		}

		svc := NewPaymentService(repo, &fakeGateway{})
		_, err = svc.RefundPayment(context.Background(), payment.ID, "customer_request")
		var appErr *apperrors.AppError
		if !stderrors.As(err, &appErr) || appErr.Code != "CONFLICT" {
			t.Fatalf("error = %v, want CONFLICT", err)
		}
	})

	t.Run("propagates repository load errors", func(t *testing.T) {
		repo := newFakeRepository()
		svc := NewPaymentService(repo, &fakeGateway{})

		if _, err := svc.RefundPayment(context.Background(), uuid.New(), "customer_request"); err == nil {
			t.Fatal("expected error, got none")
		}
	})

	t.Run("propagates repository save errors", func(t *testing.T) {
		repo := newFakeRepository()
		payment, err := domain.Initiate(uuid.New(), uuid.New(), 4999, "USD")
		if err != nil {
			t.Fatalf("Initiate: %v", err)
		}
		if err := payment.Process("txn_1"); err != nil {
			t.Fatalf("Process: %v", err)
		}
		if err := repo.Save(context.Background(), payment); err != nil {
			t.Fatalf("Save: %v", err)
		}
		repo.saveErr = errTestRepository

		svc := NewPaymentService(repo, &fakeGateway{})
		if _, err := svc.RefundPayment(context.Background(), payment.ID, "customer_request"); !stderrors.Is(err, errTestRepository) {
			t.Fatalf("error = %v, want %v", err, errTestRepository)
		}
	})
}
