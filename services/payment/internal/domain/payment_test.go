package domain

import (
	"testing"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/errors"
	"github.com/google/uuid"
)

func TestInitiate(t *testing.T) {
	orderID := uuid.New()
	customerID := uuid.New()

	tests := []struct {
		name        string
		orderID     uuid.UUID
		customerID  uuid.UUID
		amountCents int64
		currency    string
		wantErr     bool
	}{
		{"valid payment", orderID, customerID, 4999, "USD", false},
		{"missing order id", uuid.Nil, customerID, 4999, "USD", true},
		{"missing customer id", orderID, uuid.Nil, 4999, "USD", true},
		{"zero amount", orderID, customerID, 0, "USD", true},
		{"negative amount", orderID, customerID, -1, "USD", true},
		{"missing currency", orderID, customerID, 4999, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payment, err := Initiate(tt.orderID, tt.customerID, tt.amountCents, tt.currency)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if payment.ID == uuid.Nil {
				t.Error("ID was not generated")
			}
			if payment.OrderID != tt.orderID {
				t.Errorf("OrderID = %v, want %v", payment.OrderID, tt.orderID)
			}
			if payment.CustomerID != tt.customerID {
				t.Errorf("CustomerID = %v, want %v", payment.CustomerID, tt.customerID)
			}
			if payment.AmountCents != tt.amountCents {
				t.Errorf("AmountCents = %v, want %v", payment.AmountCents, tt.amountCents)
			}
			if payment.Status != StatusInitiated {
				t.Errorf("Status = %v, want %v", payment.Status, StatusInitiated)
			}
			if payment.Version != 1 {
				t.Errorf("Version = %v, want 1", payment.Version)
			}
			if len(payment.PendingEvents()) != 1 {
				t.Fatalf("len(PendingEvents()) = %d, want 1", len(payment.PendingEvents()))
			}
			if _, ok := payment.PendingEvents()[0].(*PaymentInitiated); !ok {
				t.Errorf("pending event = %T, want *PaymentInitiated", payment.PendingEvents()[0])
			}
		})
	}
}

func TestPayment_Transitions(t *testing.T) {
	allStatuses := []Status{StatusInitiated, StatusCompleted, StatusFailed, StatusRefunded, StatusCancelled}

	tests := []struct {
		name      string
		call      func(*Payment) error
		validFrom Status
		wantTo    Status
	}{
		{"Process", func(p *Payment) error { return p.Process("txn_123") }, StatusInitiated, StatusCompleted},
		{"Fail", func(p *Payment) error { return p.Fail("declined") }, StatusInitiated, StatusFailed},
		{"Refund", func(p *Payment) error { return p.Refund("customer_request") }, StatusCompleted, StatusRefunded},
		{"Cancel", func(p *Payment) error { return p.Cancel("order_cancelled") }, StatusInitiated, StatusCancelled},
	}

	for _, tt := range tests {
		for _, from := range allStatuses {
			t.Run(tt.name+"_from_"+string(from), func(t *testing.T) {
				payment := &Payment{ID: uuid.New(), Status: from, Version: 1}
				err := tt.call(payment)

				if from != tt.validFrom {
					assertConflict(t, err)
					if payment.Status != from {
						t.Errorf("Status changed to %v on invalid command", payment.Status)
					}
					if payment.Version != 1 {
						t.Errorf("Version changed to %v on invalid command", payment.Version)
					}
					return
				}
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if payment.Status != tt.wantTo {
					t.Errorf("Status = %v, want %v", payment.Status, tt.wantTo)
				}
				if payment.Version != 2 {
					t.Errorf("Version = %v, want 2", payment.Version)
				}
				if len(payment.PendingEvents()) != 1 {
					t.Errorf("len(PendingEvents()) = %d, want 1", len(payment.PendingEvents()))
				}
			})
		}
	}
}

func TestPayment_CommandsRejectEmptyReason(t *testing.T) {
	tests := []struct {
		name string
		call func(*Payment) error
	}{
		{"Process", func(p *Payment) error { return p.Process("") }},
		{"Fail", func(p *Payment) error { return p.Fail("") }},
		{"Cancel", func(p *Payment) error { return p.Cancel("") }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payment := &Payment{ID: uuid.New(), Status: StatusInitiated, Version: 1}
			if err := tt.call(payment); err == nil {
				t.Fatal("expected error, got none")
			}
			if payment.Version != 1 {
				t.Errorf("Version changed to %v on rejected command", payment.Version)
			}
		})
	}

	t.Run("Refund", func(t *testing.T) {
		payment := &Payment{ID: uuid.New(), Status: StatusCompleted, Version: 1}
		if err := payment.Refund(""); err == nil {
			t.Fatal("expected error, got none")
		}
		if payment.Version != 1 {
			t.Errorf("Version changed to %v on rejected command", payment.Version)
		}
	})
}

func TestPayment_ClearPendingEvents(t *testing.T) {
	payment, err := Initiate(uuid.New(), uuid.New(), 100, "USD")
	if err != nil {
		t.Fatalf("Initiate() error = %v", err)
	}
	payment.ClearPendingEvents()
	if len(payment.PendingEvents()) != 0 {
		t.Errorf("len(PendingEvents()) = %d, want 0", len(payment.PendingEvents()))
	}
}

func assertConflict(t *testing.T, err error) {
	t.Helper()

	if err == nil {
		t.Fatal("expected error, got none")
	}
	appErr, ok := err.(*errors.AppError)
	if !ok {
		t.Fatalf("error = %T, want *errors.AppError", err)
	}
	if appErr.Code != "CONFLICT" {
		t.Errorf("Code = %v, want CONFLICT", appErr.Code)
	}
}
