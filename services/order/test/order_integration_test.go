//go:build integration

package test

import (
	"context"
	"database/sql"
	"encoding/json"
	stderrors "errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/order/internal/consumer"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/order/internal/domain"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/order/internal/repository"
	apperrors "github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/errors"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/events"
	sharedlogger "github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/logger"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/outbox"
	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"
	"go.uber.org/zap/zaptest"
)

// mustNewOrder builds a valid two-item pending order for customerID, failing the test if the
// domain constructor rejects it.
func mustNewOrder(t *testing.T, customerID uuid.UUID) *domain.Order {
	t.Helper()

	items := []domain.OrderItem{
		{ProductID: uuid.New(), ProductName: "Widget", ProductSKU: "WID-1", Quantity: 2, UnitPriceCents: 1999},
		{ProductID: uuid.New(), ProductName: "Gadget", ProductSKU: "GAD-1", Quantity: 1, UnitPriceCents: 4999},
	}

	order, err := domain.NewOrder(customerID, items, "USD")
	if err != nil {
		t.Fatalf("build order: %v", err)
	}
	return order
}

// TestOrderIntegration_SaveOrderWithItems exercises OrderRepository.Save and GetByID against a
// real postgres: the order, its items and the order.created outbox row must all land atomically.
func TestOrderIntegration_SaveOrderWithItems(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewOrderRepository(db)
	ctx := context.Background()

	order := mustNewOrder(t, uuid.New())
	if err := repo.Save(ctx, order); err != nil {
		t.Fatalf("save order: %v", err)
	}

	got, err := repo.GetByID(ctx, order.ID)
	if err != nil {
		t.Fatalf("get order: %v", err)
	}

	wantTotal := int64(2*1999 + 4999)
	if got.TotalAmountCents != wantTotal {
		t.Errorf("total_amount_cents = %d, want %d", got.TotalAmountCents, wantTotal)
	}
	if len(got.Items) != 2 {
		t.Fatalf("got %d items, want 2", len(got.Items))
	}
	if got.Status != domain.StatusPending {
		t.Errorf("status = %s, want %s", got.Status, domain.StatusPending)
	}

	var outboxCount int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM outbox_messages WHERE aggregate_id = $1 AND event_type = $2
	`, order.ID.String(), events.EventTypeOrderCreated).Scan(&outboxCount); err != nil {
		t.Fatalf("count outbox messages: %v", err)
	}
	if outboxCount != 1 {
		t.Errorf("outbox messages for order.created = %d, want 1", outboxCount)
	}
}

// TestOrderIntegration_ConcurrentUpdateStatusOptimisticLocking fires several concurrent
// UpdateStatus calls carrying the same expected version at a real postgres: exactly one may win,
// the rest must be rejected as a version conflict rather than silently overwriting each other.
func TestOrderIntegration_ConcurrentUpdateStatusOptimisticLocking(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewOrderRepository(db)
	ctx := context.Background()

	order := mustNewOrder(t, uuid.New())
	if err := repo.Save(ctx, order); err != nil {
		t.Fatalf("save order: %v", err)
	}

	const attempts = 5
	results := make(chan error, attempts)
	var wg sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- attemptUpdateStatus(ctx, db, repo, order.ID, order.Version)
		}()
	}
	wg.Wait()
	close(results)

	successes := 0
	for err := range results {
		if err == nil {
			successes++
			continue
		}
		var appErr *apperrors.AppError
		if !stderrors.As(err, &appErr) || appErr.HTTPCode != http.StatusConflict {
			t.Fatalf("expected nil or a 409 conflict, got %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("expected exactly one concurrent update to win, got %d", successes)
	}

	got, err := repo.GetByID(ctx, order.ID)
	if err != nil {
		t.Fatalf("get order: %v", err)
	}
	if got.Version != order.Version+1 {
		t.Errorf("version = %d, want %d", got.Version, order.Version+1)
	}
}

func attemptUpdateStatus(ctx context.Context, db *sql.DB, repo *repository.OrderRepository, orderID uuid.UUID, expectedVersion int) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := repo.UpdateStatus(ctx, tx, orderID, domain.StatusPendingPayment, expectedVersion); err != nil {
		return err
	}
	return tx.Commit()
}

// TestOrderIntegration_OutboxRelayPublishesToKafka saves an order, runs a single outbox relay
// batch against the real database and asserts the order.created event actually lands on the real
// orders.events topic, with the outbox row marked published.
func TestOrderIntegration_OutboxRelayPublishesToKafka(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewOrderRepository(db)
	ctx := context.Background()

	ensureTopic(t, events.OrdersTopic)

	order := mustNewOrder(t, uuid.New())
	if err := repo.Save(ctx, order); err != nil {
		t.Fatalf("save order: %v", err)
	}

	publisher := events.NewPublisher(events.KafkaConfig{Brokers: []string{testKafkaBroker()}})
	t.Cleanup(func() { _ = publisher.Close() })

	relay := outbox.NewRelay(db, publisher, zaptest.NewLogger(t), time.Second, 10)
	if err := relay.RelayBatch(ctx); err != nil {
		t.Fatalf("relay batch: %v", err)
	}

	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:   []string{testKafkaBroker()},
		Topic:     events.OrdersTopic,
		Partition: 0,
		MinBytes:  1,
		MaxBytes:  10e6,
	})
	t.Cleanup(func() { _ = reader.Close() })
	if err := reader.SetOffset(kafka.FirstOffset); err != nil {
		t.Fatalf("set reader offset: %v", err)
	}

	readCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	for {
		msg, err := reader.ReadMessage(readCtx)
		if err != nil {
			t.Fatalf("did not find order.created for order %s on %s: %v", order.ID, events.OrdersTopic, err)
		}

		// The topic may also carry non-event messages, such as ensureTopic's readiness probe;
		// skip anything that is not a domain event instead of failing on it.
		var got events.Event
		if err := json.Unmarshal(msg.Value, &got); err != nil {
			continue
		}
		if got.AggregateID != order.ID.String() || got.Type != events.EventTypeOrderCreated {
			continue
		}

		var published bool
		if err := db.QueryRowContext(ctx, `
			SELECT published_at IS NOT NULL FROM outbox_messages WHERE aggregate_id = $1
		`, order.ID.String()).Scan(&published); err != nil {
			t.Fatalf("check outbox published_at: %v", err)
		}
		if !published {
			t.Error("outbox message was not marked published after the relay ran")
		}
		return
	}
}

// fakeOrderService counts ConfirmPayment/FailPayment calls by order id, standing in for the real
// order service so the idempotency test only exercises the consumer's own dedupe logic.
type fakeOrderService struct {
	mu        sync.Mutex
	confirmed []uuid.UUID
}

func (f *fakeOrderService) ConfirmPayment(_ context.Context, _ *sql.Tx, orderID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.confirmed = append(f.confirmed, orderID)
	return nil
}

func (f *fakeOrderService) FailPayment(_ context.Context, _ *sql.Tx, _ uuid.UUID) error {
	return nil
}

func (f *fakeOrderService) confirmedCount(orderID uuid.UUID) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, id := range f.confirmed {
		if id == orderID {
			count++
		}
	}
	return count
}

// TestOrderIntegration_PaymentsConsumerIdempotency redelivers the same payment.processed event id
// twice through the real payments consumer, backed by a real Kafka topic and the real
// processed_events table, and asserts the order is only confirmed once.
func TestOrderIntegration_PaymentsConsumerIdempotency(t *testing.T) {
	db := openTestDB(t)
	ensureTopic(t, events.PaymentsTopic)
	processed := events.NewProcessedStore(db)
	fake := &fakeOrderService{}

	logger := &sharedlogger.Logger{Logger: zaptest.NewLogger(t)}
	subscriber := events.NewSubscriber(events.KafkaConfig{
		Brokers:  []string{testKafkaBroker()},
		GroupID:  "order-test-idempotency-" + uuid.New().String(),
		DLQTopic: events.DLQTopic(events.PaymentsTopic),
	}, events.PaymentsTopic, logger.Logger)
	t.Cleanup(func() { _ = subscriber.Close() })

	paymentsConsumer := consumer.NewPaymentsConsumer(subscriber, db, processed, fake, logger)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = paymentsConsumer.Start(ctx) }()

	publisher := events.NewPublisher(events.KafkaConfig{Brokers: []string{testKafkaBroker()}})
	t.Cleanup(func() { _ = publisher.Close() })

	orderID := uuid.New()
	event := events.Event{
		ID:   uuid.New().String(),
		Type: events.EventTypePaymentProcessed,
		Data: map[string]interface{}{"order_id": orderID.String()},
	}

	// A brand new consumer group starts reading from the topic's current tail, so a message
	// published before the group finishes joining can be missed entirely. Retry publishing the
	// same event id until it is observed instead of guessing a fixed join delay: the retries are
	// themselves redeliveries the consumer must already handle idempotently, so they do not
	// weaken the assertion below.
	publishUntilConfirmed(ctx, t, publisher, event, fake, orderID)

	// Redeliver the exact same event id once more: the consumer must recognize it via
	// processed_events and not confirm the order again.
	if err := publisher.Publish(ctx, events.PaymentsTopic, event); err != nil {
		t.Fatalf("publish redelivered payment.processed: %v", err)
	}

	time.Sleep(3 * time.Second)
	if got := fake.confirmedCount(orderID); got != 1 {
		t.Fatalf("expected order to be confirmed exactly once after redelivery, got %d", got)
	}
}

// publishUntilConfirmed publishes event to payments.events and waits for it to be reflected in
// fake, republishing on a short interval until it is observed or the overall deadline expires.
func publishUntilConfirmed(ctx context.Context, t *testing.T, publisher *events.Publisher, event events.Event, fake *fakeOrderService, orderID uuid.UUID) {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)
	for {
		if err := publisher.Publish(ctx, events.PaymentsTopic, event); err != nil {
			t.Fatalf("publish payment.processed: %v", err)
		}

		attemptDeadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(attemptDeadline) {
			if fake.confirmedCount(orderID) == 1 {
				return
			}
			time.Sleep(200 * time.Millisecond)
		}

		if time.Now().After(deadline) {
			t.Fatalf("order was not confirmed within the overall timeout")
		}
	}
}
