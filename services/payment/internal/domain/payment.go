package domain

import (
	"fmt"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/errors"
	"github.com/google/uuid"
)

// Status is the lifecycle state of a payment.
type Status string

const (
	StatusInitiated Status = "initiated"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusRefunded  Status = "refunded"
	StatusCancelled Status = "cancelled"
)

// Payment is the payment aggregate root. It only changes through Apply and is rebuilt from its event
// history. Money fields are integer minor units (cents).
type Payment struct {
	ID          uuid.UUID
	OrderID     uuid.UUID
	CustomerID  uuid.UUID
	AmountCents int64
	Currency    string
	Status      Status
	Version     int

	pendingEvents []Event
}

// Initiate starts a new payment for an order, producing the event that creates the aggregate.
func Initiate(orderID, customerID uuid.UUID, amountCents int64, currency string) (*Payment, error) {
	if orderID == uuid.Nil {
		return nil, errors.NewValidationError("order_id", "must not be empty")
	}
	if customerID == uuid.Nil {
		return nil, errors.NewValidationError("customer_id", "must not be empty")
	}
	if amountCents <= 0 {
		return nil, errors.NewValidationError("amount_cents", "must be greater than zero")
	}
	if currency == "" {
		return nil, errors.NewValidationError("currency", "must not be empty")
	}

	p := &Payment{}
	p.Apply(&PaymentInitiated{
		PaymentID:   uuid.New(),
		OrderID:     orderID,
		CustomerID:  customerID,
		AmountCents: amountCents,
		Currency:    currency,
	})
	return p, nil
}

// Process marks an initiated payment as completed with the gateway's transaction id.
func (p *Payment) Process(transactionID string) error {
	if p.Status != StatusInitiated {
		return p.invalidCommand("process")
	}
	if transactionID == "" {
		return errors.NewValidationError("transaction_id", "must not be empty")
	}
	p.Apply(&PaymentProcessed{PaymentID: p.ID, TransactionID: transactionID})
	return nil
}

// Fail marks an initiated payment as rejected by the gateway.
func (p *Payment) Fail(reason string) error {
	if p.Status != StatusInitiated {
		return p.invalidCommand("fail")
	}
	if reason == "" {
		return errors.NewValidationError("reason", "must not be empty")
	}
	p.Apply(&PaymentFailed{PaymentID: p.ID, Reason: reason})
	return nil
}

// Refund reverses a completed payment.
func (p *Payment) Refund(reason string) error {
	if p.Status != StatusCompleted {
		return p.invalidCommand("refund")
	}
	if reason == "" {
		return errors.NewValidationError("reason", "must not be empty")
	}
	p.Apply(&PaymentRefunded{PaymentID: p.ID, AmountCents: p.AmountCents, Reason: reason})
	return nil
}

// Cancel withdraws a payment before it has been processed.
func (p *Payment) Cancel(reason string) error {
	if p.Status != StatusInitiated {
		return p.invalidCommand("cancel")
	}
	if reason == "" {
		return errors.NewValidationError("reason", "must not be empty")
	}
	p.Apply(&PaymentCancelled{PaymentID: p.ID, Reason: reason})
	return nil
}

// Apply mutates the aggregate to reflect event, advances its version and records the event as pending
// persistence. It is used both by commands and when rebuilding an aggregate from its event history.
func (p *Payment) Apply(event Event) {
	switch e := event.(type) {
	case *PaymentInitiated:
		p.ID = e.PaymentID
		p.OrderID = e.OrderID
		p.CustomerID = e.CustomerID
		p.AmountCents = e.AmountCents
		p.Currency = e.Currency
		p.Status = StatusInitiated
	case *PaymentProcessed:
		p.Status = StatusCompleted
	case *PaymentFailed:
		p.Status = StatusFailed
	case *PaymentRefunded:
		p.Status = StatusRefunded
	case *PaymentCancelled:
		p.Status = StatusCancelled
	}
	p.Version++
	p.pendingEvents = append(p.pendingEvents, event)
}

// PendingEvents returns the events produced by commands since the aggregate was loaded or last saved.
func (p *Payment) PendingEvents() []Event {
	return p.pendingEvents
}

// ClearPendingEvents discards the pending events buffer, e.g. after replaying history or a successful save.
func (p *Payment) ClearPendingEvents() {
	p.pendingEvents = nil
}

func (p *Payment) invalidCommand(command string) error {
	return errors.NewConflict(fmt.Sprintf("payment %s: cannot %s in status %s", p.ID, command, p.Status))
}
