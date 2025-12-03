package outbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/events"
)

// publisher is the subset of *events.Publisher used by Relay, extracted so tests can
// substitute a fake publisher.
type publisher interface {
	Publish(ctx context.Context, topic string, event events.Event) error
}

// Relay polls outbox_messages for unpublished rows and publishes them through a Publisher.
type Relay struct {
	db        *sql.DB
	pub       publisher
	logger    *zap.Logger
	interval  time.Duration
	batchSize int

	stop chan struct{}
	done chan struct{}
}

// NewRelay creates a Relay that polls db every interval for up to batchSize pending messages.
func NewRelay(db *sql.DB, pub *events.Publisher, logger *zap.Logger, interval time.Duration, batchSize int) *Relay {
	return &Relay{
		db:        db,
		pub:       pub,
		logger:    logger,
		interval:  interval,
		batchSize: batchSize,
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
	}
}

// Start runs the relay loop in the background until ctx is cancelled or Stop is called.
func (r *Relay) Start(ctx context.Context) {
	go func() {
		defer close(r.done)

		ticker := time.NewTicker(r.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-r.stop:
				return
			case <-ticker.C:
				if err := r.RelayBatch(ctx); err != nil {
					r.logger.Error("outbox relay batch failed", zap.Error(err))
				}
			}
		}
	}()
}

// Stop signals the relay loop to exit and waits for it to finish.
func (r *Relay) Stop() {
	close(r.stop)
	<-r.done
}

type outboxRow struct {
	id            string
	topic         string
	eventType     string
	aggregateID   string
	payload       []byte
	correlationID sql.NullString
}

// RelayBatch publishes up to batchSize pending outbox messages in a single pass, locking the
// rows it selects with FOR UPDATE SKIP LOCKED so multiple relay instances can run concurrently
// without publishing the same message twice.
func (r *Relay) RelayBatch(ctx context.Context) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin outbox relay transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := r.selectPending(ctx, tx)
	if err != nil {
		return err
	}

	for _, row := range rows {
		r.publishRow(ctx, tx, row)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit outbox relay transaction: %w", err)
	}
	return nil
}

func (r *Relay) selectPending(ctx context.Context, tx *sql.Tx) ([]outboxRow, error) {
	const query = `
		SELECT id, topic, event_type, aggregate_id, payload, correlation_id
		FROM outbox_messages
		WHERE published_at IS NULL
		ORDER BY created_at
		LIMIT $1
		FOR UPDATE SKIP LOCKED
	`

	result, err := tx.QueryContext(ctx, query, r.batchSize)
	if err != nil {
		return nil, fmt.Errorf("failed to select pending outbox messages: %w", err)
	}
	defer result.Close()

	var rows []outboxRow
	for result.Next() {
		var row outboxRow
		if err := result.Scan(&row.id, &row.topic, &row.eventType, &row.aggregateID, &row.payload, &row.correlationID); err != nil {
			return nil, fmt.Errorf("failed to scan outbox message: %w", err)
		}
		rows = append(rows, row)
	}
	if err := result.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate outbox messages: %w", err)
	}

	return rows, nil
}

// publishRow publishes a single row and marks it published or records the failure, without
// aborting the rest of the batch.
func (r *Relay) publishRow(ctx context.Context, tx *sql.Tx, row outboxRow) {
	var data map[string]interface{}
	if err := json.Unmarshal(row.payload, &data); err != nil {
		r.markFailed(ctx, tx, row.id, fmt.Errorf("failed to unmarshal outbox payload: %w", err))
		return
	}

	event := events.Event{
		Type:          row.eventType,
		AggregateID:   row.aggregateID,
		Data:          data,
		CorrelationID: row.correlationID.String,
	}

	if err := r.pub.Publish(ctx, row.topic, event); err != nil {
		r.markFailed(ctx, tx, row.id, err)
		return
	}

	const markPublished = `UPDATE outbox_messages SET published_at = NOW() WHERE id = $1`
	if _, err := tx.ExecContext(ctx, markPublished, row.id); err != nil {
		r.logger.Error("failed to mark outbox message published", zap.String("id", row.id), zap.Error(err))
	}
}

func (r *Relay) markFailed(ctx context.Context, tx *sql.Tx, id string, cause error) {
	r.logger.Error("failed to publish outbox message", zap.String("id", id), zap.Error(cause))

	const query = `UPDATE outbox_messages SET attempts = attempts + 1, last_error = $2 WHERE id = $1`
	if _, err := tx.ExecContext(ctx, query, id, cause.Error()); err != nil {
		r.logger.Error("failed to record outbox failure", zap.String("id", id), zap.Error(err))
	}
}
