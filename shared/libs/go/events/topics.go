package events

// Kafka topics events are published to.
const (
	OrdersTopic    = "orders.events"
	PaymentsTopic  = "payments.events"
	InventoryTopic = "inventory.events"
)

// Event.Type values, matching the domain event names in docs/architecture/saga-pattern.md.
const (
	EventTypeOrderCreated         = "order.created"
	EventTypeOrderReadyForPayment = "order.ready_for_payment"
	EventTypeOrderConfirmed       = "order.confirmed"
	EventTypeOrderCancelled       = "order.cancelled"

	EventTypePaymentInitiated = "payment.initiated"
	EventTypePaymentProcessed = "payment.processed"
	EventTypePaymentFailed    = "payment.failed"
	EventTypePaymentRefunded  = "payment.refunded"

	EventTypeInventoryReserved = "inventory.reserved"
	EventTypeInventoryReleased = "inventory.released"
	EventTypeProductUpdated    = "product.updated"
)

// DLQTopic returns the dead letter queue topic for topic.
func DLQTopic(topic string) string {
	return topic + ".dlq"
}
