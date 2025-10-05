// Package domain holds the order aggregate and its business rules.
package domain

import (
	"math"
	"time"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/errors"
	"github.com/google/uuid"
)

// Status is the lifecycle state of an order.
type Status string

const (
	StatusPending        Status = "pending"
	StatusPendingPayment Status = "pending_payment"
	StatusPaymentFailed  Status = "payment_failed"
	StatusConfirmed      Status = "confirmed"
	StatusProcessing     Status = "processing"
	StatusShipped        Status = "shipped"
	StatusDelivered      Status = "delivered"
	StatusCancelled      Status = "cancelled"
)

// OrderItem is a single line item of an order.
type OrderItem struct {
	ID          uuid.UUID
	ProductID   uuid.UUID
	ProductName string
	ProductSKU  string
	Quantity    int
	UnitPrice   float64
	TotalPrice  float64
}

// Order is the order aggregate root.
type Order struct {
	ID          uuid.UUID
	CustomerID  uuid.UUID
	Status      Status
	TotalAmount float64
	Currency    string
	Items       []OrderItem
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Version     int
}

// NewOrder builds a pending order from its line items, computing per-item and order totals.
func NewOrder(customerID uuid.UUID, items []OrderItem, currency string) (*Order, error) {
	if customerID == uuid.Nil {
		return nil, errors.NewValidationError("customer_id", "must not be empty")
	}
	if len(items) == 0 {
		return nil, errors.NewValidationError("items", "must contain at least one item")
	}
	if currency == "" {
		return nil, errors.NewValidationError("currency", "must not be empty")
	}

	now := time.Now().UTC()
	orderItems := make([]OrderItem, len(items))
	var total float64

	for i, item := range items {
		if item.ProductID == uuid.Nil {
			return nil, errors.NewValidationError("product_id", "must not be empty")
		}
		if item.ProductName == "" {
			return nil, errors.NewValidationError("product_name", "must not be empty")
		}
		if item.Quantity <= 0 {
			return nil, errors.NewValidationError("quantity", "must be greater than zero")
		}
		if item.UnitPrice < 0 {
			return nil, errors.NewValidationError("unit_price", "must not be negative")
		}

		item.ID = uuid.New()
		item.TotalPrice = roundToCents(item.UnitPrice * float64(item.Quantity))
		orderItems[i] = item
		total += item.TotalPrice
	}

	return &Order{
		ID:          uuid.New(),
		CustomerID:  customerID,
		Status:      StatusPending,
		TotalAmount: roundToCents(total),
		Currency:    currency,
		Items:       orderItems,
		CreatedAt:   now,
		UpdatedAt:   now,
		Version:     1,
	}, nil
}

// MarkPendingPayment moves a pending order into pending_payment.
func (o *Order) MarkPendingPayment() error {
	return o.transition(StatusPending, StatusPendingPayment)
}

// Confirm moves an order awaiting payment into confirmed.
func (o *Order) Confirm() error {
	return o.transition(StatusPendingPayment, StatusConfirmed)
}

// Fail moves an order awaiting payment into payment_failed.
func (o *Order) Fail() error {
	return o.transition(StatusPendingPayment, StatusPaymentFailed)
}

// Cancel moves a pending or pending_payment order into cancelled.
func (o *Order) Cancel() error {
	if o.Status != StatusPending && o.Status != StatusPendingPayment {
		return errors.NewOrderAlreadyProcessed(o.ID.String())
	}
	o.Status = StatusCancelled
	return nil
}

func (o *Order) transition(from, to Status) error {
	if o.Status != from {
		return errors.NewOrderAlreadyProcessed(o.ID.String())
	}
	o.Status = to
	return nil
}

func roundToCents(v float64) float64 {
	return math.Round(v*100) / 100
}
