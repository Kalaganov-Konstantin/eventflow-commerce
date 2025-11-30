package outbox

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"go.uber.org/zap"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/events"
)

// fakePublisher is a substitute publisher that records published events or fails.
type fakePublisher struct {
	published []events.Event
	topics    []string
	err       error
}

func (f *fakePublisher) Publish(_ context.Context, topic string, event events.Event) error {
	if f.err != nil {
		return f.err
	}
	f.topics = append(f.topics, topic)
	f.published = append(f.published, event)
	return nil
}

var pendingColumns = []string{"id", "topic", "event_type", "aggregate_id", "payload", "correlation_id"}

func TestRelay_RelayBatch_PublishesPendingMessageAndMarksItPublished(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, topic, event_type, aggregate_id, payload, correlation_id")).
		WithArgs(10).
		WillReturnRows(sqlmock.NewRows(pendingColumns).
			AddRow("msg-1", "orders.events", "order.created", "order-1", []byte(`{"total_cents":1999}`), nil))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE outbox_messages SET published_at")).
		WithArgs("msg-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	pub := &fakePublisher{}
	relay := &Relay{db: db, pub: pub, logger: zap.NewNop(), batchSize: 10}

	if err := relay.RelayBatch(context.Background()); err != nil {
		t.Fatalf("RelayBatch() error = %v", err)
	}

	if len(pub.published) != 1 {
		t.Fatalf("published = %d events, want 1", len(pub.published))
	}
	if pub.topics[0] != "orders.events" {
		t.Errorf("topic = %q, want %q", pub.topics[0], "orders.events")
	}
	if pub.published[0].Type != "order.created" || pub.published[0].AggregateID != "order-1" {
		t.Errorf("event = %+v, want type order.created and aggregate order-1", pub.published[0])
	}
	if got := pub.published[0].Data["total_cents"]; got != float64(1999) {
		t.Errorf("payload total_cents = %v, want 1999", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRelay_RelayBatch_RecordsFailureWithoutAbortingCommit(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, topic, event_type, aggregate_id, payload, correlation_id")).
		WithArgs(10).
		WillReturnRows(sqlmock.NewRows(pendingColumns).
			AddRow("msg-1", "orders.events", "order.created", "order-1", []byte(`{}`), nil))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE outbox_messages SET attempts = attempts + 1")).
		WithArgs("msg-1", "kafka unreachable").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	pub := &fakePublisher{err: errors.New("kafka unreachable")}
	relay := &Relay{db: db, pub: pub, logger: zap.NewNop(), batchSize: 10}

	if err := relay.RelayBatch(context.Background()); err != nil {
		t.Fatalf("RelayBatch() error = %v", err)
	}

	if len(pub.published) != 0 {
		t.Fatalf("published = %d events, want 0", len(pub.published))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRelay_RelayBatch_NoPendingMessagesCommitsEmptyBatch(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, topic, event_type, aggregate_id, payload, correlation_id")).
		WithArgs(5).
		WillReturnRows(sqlmock.NewRows(pendingColumns))
	mock.ExpectCommit()

	pub := &fakePublisher{}
	relay := &Relay{db: db, pub: pub, logger: zap.NewNop(), batchSize: 5}

	if err := relay.RelayBatch(context.Background()); err != nil {
		t.Fatalf("RelayBatch() error = %v", err)
	}
	if len(pub.published) != 0 {
		t.Fatalf("published = %d events, want 0", len(pub.published))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRelay_StartAndStop_RunsAndExitsCleanly(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	mock.MatchExpectationsInOrder(false)
	mock.ExpectBegin().WillReturnError(errors.New("no rows to relay in this test"))

	pub := &fakePublisher{}
	relay := NewRelay(db, nil, zap.NewNop(), time.Millisecond, 10)
	relay.pub = pub

	relay.Start(context.Background())
	relay.Stop()
}
