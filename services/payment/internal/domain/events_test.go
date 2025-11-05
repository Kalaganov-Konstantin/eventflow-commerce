package domain

import (
	"reflect"
	"testing"

	"github.com/google/uuid"
)

func TestEvent_RoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		event Event
	}{
		{
			"PaymentInitiated",
			&PaymentInitiated{
				PaymentID:   uuid.New(),
				OrderID:     uuid.New(),
				CustomerID:  uuid.New(),
				AmountCents: 4999,
				Currency:    "USD",
			},
		},
		{
			"PaymentProcessed",
			&PaymentProcessed{
				PaymentID:     uuid.New(),
				TransactionID: "txn_123",
			},
		},
		{
			"PaymentFailed",
			&PaymentFailed{
				PaymentID: uuid.New(),
				Reason:    "insufficient_funds",
			},
		},
		{
			"PaymentRefunded",
			&PaymentRefunded{
				PaymentID:   uuid.New(),
				AmountCents: 4999,
				Reason:      "customer_request",
			},
		},
		{
			"PaymentCancelled",
			&PaymentCancelled{
				PaymentID: uuid.New(),
				Reason:    "order_cancelled",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := MarshalEvent(tt.event)
			if err != nil {
				t.Fatalf("MarshalEvent() error = %v", err)
			}

			got, err := UnmarshalEvent(tt.event.EventType(), data)
			if err != nil {
				t.Fatalf("UnmarshalEvent() error = %v", err)
			}

			if !reflect.DeepEqual(got, tt.event) {
				t.Errorf("UnmarshalEvent() = %#v, want %#v", got, tt.event)
			}
		})
	}
}

func TestUnmarshalEvent_UnknownType(t *testing.T) {
	_, err := UnmarshalEvent("payment.unknown", []byte(`{}`))
	if err == nil {
		t.Fatal("expected error, got none")
	}
}
