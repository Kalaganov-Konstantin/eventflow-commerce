package events

import "testing"

func TestDLQTopic(t *testing.T) {
	tests := []struct {
		topic string
		want  string
	}{
		{OrdersTopic, "orders.events.dlq"},
		{PaymentsTopic, "payments.events.dlq"},
		{InventoryTopic, "inventory.events.dlq"},
	}

	for _, tt := range tests {
		if got := DLQTopic(tt.topic); got != tt.want {
			t.Errorf("DLQTopic(%q) = %q, want %q", tt.topic, got, tt.want)
		}
	}
}

func TestEventTypeConstants(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"EventTypeOrderCreated", EventTypeOrderCreated, "order.created"},
		{"EventTypeOrderReadyForPayment", EventTypeOrderReadyForPayment, "order.ready_for_payment"},
		{"EventTypeOrderConfirmed", EventTypeOrderConfirmed, "order.confirmed"},
		{"EventTypeOrderCancelled", EventTypeOrderCancelled, "order.cancelled"},
		{"EventTypePaymentInitiated", EventTypePaymentInitiated, "payment.initiated"},
		{"EventTypePaymentProcessed", EventTypePaymentProcessed, "payment.processed"},
		{"EventTypePaymentFailed", EventTypePaymentFailed, "payment.failed"},
		{"EventTypePaymentRefunded", EventTypePaymentRefunded, "payment.refunded"},
		{"EventTypeInventoryReserved", EventTypeInventoryReserved, "inventory.reserved"},
		{"EventTypeInventoryReleased", EventTypeInventoryReleased, "inventory.released"},
		{"EventTypeProductUpdated", EventTypeProductUpdated, "product.updated"},
	}

	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
		}
	}
}
