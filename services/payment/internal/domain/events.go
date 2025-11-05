// Package domain holds the payment aggregate, its events and its business rules.
package domain

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// Event is a fact recorded against a payment aggregate.
type Event interface {
	EventType() string
}

const (
	EventTypePaymentInitiated = "payment.initiated"
	EventTypePaymentProcessed = "payment.processed"
	EventTypePaymentFailed    = "payment.failed"
	EventTypePaymentRefunded  = "payment.refunded"
	EventTypePaymentCancelled = "payment.cancelled"
)

// PaymentInitiated records that a payment was started for an order. Money fields are integer minor
// units (cents).
type PaymentInitiated struct {
	PaymentID   uuid.UUID `json:"payment_id"`
	OrderID     uuid.UUID `json:"order_id"`
	CustomerID  uuid.UUID `json:"customer_id"`
	AmountCents int64     `json:"amount_cents"`
	Currency    string    `json:"currency"`
}

// EventType returns the stored type name of a PaymentInitiated event.
func (e *PaymentInitiated) EventType() string { return EventTypePaymentInitiated }

// PaymentProcessed records that a payment was captured by the gateway.
type PaymentProcessed struct {
	PaymentID     uuid.UUID `json:"payment_id"`
	TransactionID string    `json:"transaction_id"`
}

// EventType returns the stored type name of a PaymentProcessed event.
func (e *PaymentProcessed) EventType() string { return EventTypePaymentProcessed }

// PaymentFailed records that a payment attempt was rejected by the gateway.
type PaymentFailed struct {
	PaymentID uuid.UUID `json:"payment_id"`
	Reason    string    `json:"reason"`
}

// EventType returns the stored type name of a PaymentFailed event.
func (e *PaymentFailed) EventType() string { return EventTypePaymentFailed }

// PaymentRefunded records that a completed payment was refunded. Money fields are integer minor units
// (cents).
type PaymentRefunded struct {
	PaymentID   uuid.UUID `json:"payment_id"`
	AmountCents int64     `json:"amount_cents"`
	Reason      string    `json:"reason"`
}

// EventType returns the stored type name of a PaymentRefunded event.
func (e *PaymentRefunded) EventType() string { return EventTypePaymentRefunded }

// PaymentCancelled records that a payment was withdrawn before it was processed.
type PaymentCancelled struct {
	PaymentID uuid.UUID `json:"payment_id"`
	Reason    string    `json:"reason"`
}

// EventType returns the stored type name of a PaymentCancelled event.
func (e *PaymentCancelled) EventType() string { return EventTypePaymentCancelled }

var eventFactories = map[string]func() Event{
	EventTypePaymentInitiated: func() Event { return &PaymentInitiated{} },
	EventTypePaymentProcessed: func() Event { return &PaymentProcessed{} },
	EventTypePaymentFailed:    func() Event { return &PaymentFailed{} },
	EventTypePaymentRefunded:  func() Event { return &PaymentRefunded{} },
	EventTypePaymentCancelled: func() Event { return &PaymentCancelled{} },
}

// MarshalEvent serializes an event to its JSON storage representation.
func MarshalEvent(event Event) ([]byte, error) {
	data, err := json.Marshal(event)
	if err != nil {
		return nil, fmt.Errorf("marshal %s event: %w", event.EventType(), err)
	}
	return data, nil
}

// UnmarshalEvent rebuilds a typed event from its stored type name and JSON payload.
func UnmarshalEvent(eventType string, data []byte) (Event, error) {
	factory, ok := eventFactories[eventType]
	if !ok {
		return nil, fmt.Errorf("unknown payment event type %q", eventType)
	}

	event := factory()
	if err := json.Unmarshal(data, event); err != nil {
		return nil, fmt.Errorf("unmarshal %s event: %w", eventType, err)
	}
	return event, nil
}
