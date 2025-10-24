package domain

import (
	"time"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/errors"
	"github.com/google/uuid"
)

// ReservationStatus is the lifecycle state of a stock reservation.
type ReservationStatus string

const (
	ReservationStatusReserved  ReservationStatus = "reserved"
	ReservationStatusReleased  ReservationStatus = "released"
	ReservationStatusCommitted ReservationStatus = "committed"
)

// Stock is the available and reserved quantity of a product.
type Stock struct {
	ProductID         uuid.UUID
	QuantityAvailable int
	QuantityReserved  int
	Version           int
	UpdatedAt         time.Time
}

// Reserve moves quantity from available to reserved; it fails when less than quantity is available.
func (s *Stock) Reserve(quantity int) error {
	if quantity > s.QuantityAvailable {
		return errors.NewInsufficientInventory(s.ProductID.String(), quantity, s.QuantityAvailable)
	}
	s.QuantityAvailable -= quantity
	s.QuantityReserved += quantity
	return nil
}

// Release moves quantity from reserved back to available; it fails when more than reserved is released.
func (s *Stock) Release(quantity int) error {
	if quantity > s.QuantityReserved {
		return errors.NewInsufficientInventory(s.ProductID.String(), quantity, s.QuantityReserved)
	}
	s.QuantityReserved -= quantity
	s.QuantityAvailable += quantity
	return nil
}

// ReserveItem is a single product/quantity request line of a stock reservation.
type ReserveItem struct {
	ProductID uuid.UUID
	Quantity  int
}

// Reservation holds quantity of a product against an order, until it is released or committed.
type Reservation struct {
	ID        uuid.UUID
	OrderID   uuid.UUID
	ProductID uuid.UUID
	Quantity  int
	Status    ReservationStatus
	ExpiresAt *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewReservation builds a reserved hold of quantity units of a product for an order.
func NewReservation(orderID, productID uuid.UUID, quantity int) (*Reservation, error) {
	if orderID == uuid.Nil {
		return nil, errors.NewValidationError("order_id", "must not be empty")
	}
	if productID == uuid.Nil {
		return nil, errors.NewValidationError("product_id", "must not be empty")
	}
	if quantity <= 0 {
		return nil, errors.NewValidationError("quantity", "must be greater than zero")
	}

	now := time.Now().UTC()
	return &Reservation{
		ID:        uuid.New(),
		OrderID:   orderID,
		ProductID: productID,
		Quantity:  quantity,
		Status:    ReservationStatusReserved,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// Release transitions a reservation to released; releasing an already released reservation is a no-op.
func (r *Reservation) Release() error {
	if r.Status == ReservationStatusReleased {
		return nil
	}
	if r.Status != ReservationStatusReserved {
		return errors.NewValidationError("status", "only a reserved reservation can be released")
	}
	r.Status = ReservationStatusReleased
	r.UpdatedAt = time.Now().UTC()
	return nil
}

// IsDuplicateReserve reports whether an existing reservation already covers a reserve request for the
// same order and product, so the request must be treated as a no-op instead of reserving again.
func IsDuplicateReserve(existing *Reservation, orderID, productID uuid.UUID) bool {
	return existing != nil &&
		existing.OrderID == orderID &&
		existing.ProductID == productID &&
		existing.Status == ReservationStatusReserved
}
