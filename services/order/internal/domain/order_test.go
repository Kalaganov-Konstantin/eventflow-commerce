package domain

import (
	"testing"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/errors"
	"github.com/google/uuid"
)

func TestNewOrder(t *testing.T) {
	validItem := OrderItem{ProductID: uuid.New(), ProductName: "Widget", Quantity: 2, UnitPriceCents: 999}

	tests := []struct {
		name       string
		customerID uuid.UUID
		items      []OrderItem
		currency   string
		wantErr    bool
	}{
		{"valid order", uuid.New(), []OrderItem{validItem}, "USD", false},
		{"missing customer id", uuid.Nil, []OrderItem{validItem}, "USD", true},
		{"no items", uuid.New(), nil, "USD", true},
		{"missing currency", uuid.New(), []OrderItem{validItem}, "", true},
		{"missing product id", uuid.New(), []OrderItem{{ProductName: "Widget", Quantity: 1, UnitPriceCents: 100}}, "USD", true},
		{"missing product name", uuid.New(), []OrderItem{{ProductID: uuid.New(), Quantity: 1, UnitPriceCents: 100}}, "USD", true},
		{"zero quantity", uuid.New(), []OrderItem{{ProductID: uuid.New(), ProductName: "Widget", Quantity: 0, UnitPriceCents: 100}}, "USD", true},
		{"negative unit price", uuid.New(), []OrderItem{{ProductID: uuid.New(), ProductName: "Widget", Quantity: 1, UnitPriceCents: -1}}, "USD", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			order, err := NewOrder(tt.customerID, tt.items, tt.currency)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if order.Status != StatusPending {
				t.Errorf("Status = %v, want %v", order.Status, StatusPending)
			}
			if order.Version != 1 {
				t.Errorf("Version = %v, want 1", order.Version)
			}
			if len(order.Items) != len(tt.items) {
				t.Fatalf("len(Items) = %d, want %d", len(order.Items), len(tt.items))
			}
			if order.Items[0].ID == uuid.Nil {
				t.Error("item ID was not generated")
			}
			wantTotal := tt.items[0].UnitPriceCents * int64(tt.items[0].Quantity)
			if order.TotalAmountCents != wantTotal {
				t.Errorf("TotalAmountCents = %v, want %v", order.TotalAmountCents, wantTotal)
			}
			if order.Items[0].TotalPriceCents != wantTotal {
				t.Errorf("item TotalPriceCents = %v, want %v", order.Items[0].TotalPriceCents, wantTotal)
			}
		})
	}
}

func TestOrder_Transitions(t *testing.T) {
	allStatuses := []Status{
		StatusPending, StatusPendingPayment, StatusPaymentFailed, StatusConfirmed,
		StatusProcessing, StatusShipped, StatusDelivered, StatusCancelled,
	}

	singleSourceTests := []struct {
		name      string
		method    func(*Order) error
		validFrom Status
		wantTo    Status
	}{
		{"MarkPendingPayment", (*Order).MarkPendingPayment, StatusPending, StatusPendingPayment},
		{"Confirm", (*Order).Confirm, StatusPendingPayment, StatusConfirmed},
		{"Fail", (*Order).Fail, StatusPendingPayment, StatusPaymentFailed},
	}

	for _, tt := range singleSourceTests {
		for _, from := range allStatuses {
			t.Run(tt.name+"_from_"+string(from), func(t *testing.T) {
				order := &Order{ID: uuid.New(), Status: from}
				err := tt.method(order)

				if from != tt.validFrom {
					assertOrderAlreadyProcessed(t, err)
					return
				}
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if order.Status != tt.wantTo {
					t.Errorf("Status = %v, want %v", order.Status, tt.wantTo)
				}
			})
		}
	}

	for _, from := range allStatuses {
		t.Run("Cancel_from_"+string(from), func(t *testing.T) {
			order := &Order{ID: uuid.New(), Status: from}
			err := order.Cancel()

			if from != StatusPending && from != StatusPendingPayment {
				assertOrderAlreadyProcessed(t, err)
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if order.Status != StatusCancelled {
				t.Errorf("Status = %v, want %v", order.Status, StatusCancelled)
			}
		})
	}
}

func assertOrderAlreadyProcessed(t *testing.T, err error) {
	t.Helper()

	if err == nil {
		t.Fatal("expected error, got none")
	}
	appErr, ok := err.(*errors.AppError)
	if !ok {
		t.Fatalf("error = %T, want *errors.AppError", err)
	}
	if appErr.Code != "ORDER_ALREADY_PROCESSED" {
		t.Errorf("Code = %v, want ORDER_ALREADY_PROCESSED", appErr.Code)
	}
}
