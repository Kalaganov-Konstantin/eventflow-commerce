package domain

import (
	"testing"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/errors"
	"github.com/google/uuid"
)

func TestStock_Reserve(t *testing.T) {
	tests := []struct {
		name              string
		available         int
		reserved          int
		quantity          int
		wantErr           bool
		wantAvailableLeft int
		wantReservedLeft  int
	}{
		{"reserves within available", 10, 0, 4, false, 6, 4},
		{"reserves exactly what is available", 10, 0, 10, false, 0, 10},
		{"fails when quantity exceeds available", 10, 0, 11, true, 10, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stock := &Stock{ProductID: uuid.New(), QuantityAvailable: tt.available, QuantityReserved: tt.reserved}
			err := stock.Reserve(tt.quantity)

			if tt.wantErr {
				assertInsufficientInventory(t, err)
				if stock.QuantityAvailable != tt.wantAvailableLeft || stock.QuantityReserved != tt.wantReservedLeft {
					t.Errorf("stock mutated on error: %+v", stock)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if stock.QuantityAvailable != tt.wantAvailableLeft {
				t.Errorf("QuantityAvailable = %d, want %d", stock.QuantityAvailable, tt.wantAvailableLeft)
			}
			if stock.QuantityReserved != tt.wantReservedLeft {
				t.Errorf("QuantityReserved = %d, want %d", stock.QuantityReserved, tt.wantReservedLeft)
			}
		})
	}
}

func TestStock_Release(t *testing.T) {
	tests := []struct {
		name              string
		available         int
		reserved          int
		quantity          int
		wantErr           bool
		wantAvailableLeft int
		wantReservedLeft  int
	}{
		{"releases within reserved", 0, 10, 4, false, 4, 6},
		{"releases exactly what is reserved", 0, 10, 10, false, 10, 0},
		{"fails when quantity exceeds reserved", 0, 10, 11, true, 0, 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stock := &Stock{ProductID: uuid.New(), QuantityAvailable: tt.available, QuantityReserved: tt.reserved}
			err := stock.Release(tt.quantity)

			if tt.wantErr {
				assertInsufficientInventory(t, err)
				if stock.QuantityAvailable != tt.wantAvailableLeft || stock.QuantityReserved != tt.wantReservedLeft {
					t.Errorf("stock mutated on error: %+v", stock)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if stock.QuantityAvailable != tt.wantAvailableLeft {
				t.Errorf("QuantityAvailable = %d, want %d", stock.QuantityAvailable, tt.wantAvailableLeft)
			}
			if stock.QuantityReserved != tt.wantReservedLeft {
				t.Errorf("QuantityReserved = %d, want %d", stock.QuantityReserved, tt.wantReservedLeft)
			}
		})
	}
}

func TestNewReservation(t *testing.T) {
	tests := []struct {
		name      string
		orderID   uuid.UUID
		productID uuid.UUID
		quantity  int
		wantErr   bool
	}{
		{"valid reservation", uuid.New(), uuid.New(), 3, false},
		{"missing order id", uuid.Nil, uuid.New(), 3, true},
		{"missing product id", uuid.New(), uuid.Nil, 3, true},
		{"zero quantity", uuid.New(), uuid.New(), 0, true},
		{"negative quantity", uuid.New(), uuid.New(), -1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reservation, err := NewReservation(tt.orderID, tt.productID, tt.quantity)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if reservation.ID == uuid.Nil {
				t.Error("reservation ID was not generated")
			}
			if reservation.Status != ReservationStatusReserved {
				t.Errorf("Status = %v, want %v", reservation.Status, ReservationStatusReserved)
			}
			if reservation.Quantity != tt.quantity {
				t.Errorf("Quantity = %d, want %d", reservation.Quantity, tt.quantity)
			}
		})
	}
}

func TestReservation_Release(t *testing.T) {
	tests := []struct {
		name       string
		from       ReservationStatus
		wantErr    bool
		wantStatus ReservationStatus
	}{
		{"releases a reserved hold", ReservationStatusReserved, false, ReservationStatusReleased},
		{"releasing twice is a no-op", ReservationStatusReleased, false, ReservationStatusReleased},
		{"cannot release a committed reservation", ReservationStatusCommitted, true, ReservationStatusCommitted},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reservation := &Reservation{ID: uuid.New(), Status: tt.from}
			err := reservation.Release()

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got none")
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if reservation.Status != tt.wantStatus {
				t.Errorf("Status = %v, want %v", reservation.Status, tt.wantStatus)
			}
		})
	}
}

func TestIsDuplicateReserve(t *testing.T) {
	orderID := uuid.New()
	productID := uuid.New()

	tests := []struct {
		name     string
		existing *Reservation
		want     bool
	}{
		{"nil reservation", nil, false},
		{"same order and product, reserved", &Reservation{OrderID: orderID, ProductID: productID, Status: ReservationStatusReserved}, true},
		{"same order and product, released", &Reservation{OrderID: orderID, ProductID: productID, Status: ReservationStatusReleased}, false},
		{"different order", &Reservation{OrderID: uuid.New(), ProductID: productID, Status: ReservationStatusReserved}, false},
		{"different product", &Reservation{OrderID: orderID, ProductID: uuid.New(), Status: ReservationStatusReserved}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsDuplicateReserve(tt.existing, orderID, productID); got != tt.want {
				t.Errorf("IsDuplicateReserve() = %v, want %v", got, tt.want)
			}
		})
	}
}

func assertInsufficientInventory(t *testing.T, err error) {
	t.Helper()

	if err == nil {
		t.Fatal("expected error, got none")
	}
	appErr, ok := err.(*errors.AppError)
	if !ok {
		t.Fatalf("error = %T, want *errors.AppError", err)
	}
	if appErr.Code != "INSUFFICIENT_INVENTORY" {
		t.Errorf("Code = %v, want INSUFFICIENT_INVENTORY", appErr.Code)
	}
}
