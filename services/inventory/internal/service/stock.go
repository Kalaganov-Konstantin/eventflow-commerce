// Package service applies inventory reactions to order lifecycle events.
package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/inventory/internal/domain"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/events"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/outbox"
	"github.com/google/uuid"
)

// Repository is the persistence port the stock service depends on.
type Repository interface {
	ReleaseInTx(ctx context.Context, tx *sql.Tx, orderID uuid.UUID) ([]domain.ReserveItem, error)
}

// StockService reacts to order lifecycle events by releasing or committing stock reservations,
// within a transaction supplied by the caller so it can be combined with other writes.
type StockService struct {
	repo   Repository
	outbox *outbox.Store
}

// NewStockService builds a StockService backed by repo.
func NewStockService(repo Repository) *StockService {
	return &StockService{repo: repo, outbox: outbox.NewStore()}
}

// HandleOrderEvent reacts to an order lifecycle event of eventType for orderID, within tx. Event
// types this service does not act on are ignored.
func (s *StockService) HandleOrderEvent(ctx context.Context, tx *sql.Tx, eventType string, orderID uuid.UUID) error {
	switch eventType {
	case events.EventTypeOrderCancelled:
		return s.releaseOrder(ctx, tx, orderID)
	default:
		return nil
	}
}

// releaseOrder releases every reserved item of orderID and enqueues inventory.released. An order
// with no active reservations is left untouched, since a redelivered or late cancellation is not
// an error.
func (s *StockService) releaseOrder(ctx context.Context, tx *sql.Tx, orderID uuid.UUID) error {
	items, err := s.repo.ReleaseInTx(ctx, tx, orderID)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return nil
	}

	payload, err := json.Marshal(newInventoryEventPayload(orderID, items))
	if err != nil {
		return fmt.Errorf("marshal inventory.released payload: %w", err)
	}

	return s.outbox.Enqueue(ctx, tx, outbox.Message{
		Topic:       events.InventoryTopic,
		EventType:   events.EventTypeInventoryReleased,
		AggregateID: orderID.String(),
		Payload:     payload,
	})
}

// inventoryEventPayload is the outbox payload for an inventory event driven by an order lifecycle
// change. Quantities are counts, not money.
type inventoryEventPayload struct {
	OrderID string                      `json:"order_id"`
	Items   []inventoryEventItemPayload `json:"items"`
}

type inventoryEventItemPayload struct {
	ProductID string `json:"product_id"`
	Quantity  int    `json:"quantity"`
}

func newInventoryEventPayload(orderID uuid.UUID, items []domain.ReserveItem) inventoryEventPayload {
	itemPayloads := make([]inventoryEventItemPayload, len(items))
	for i, item := range items {
		itemPayloads[i] = inventoryEventItemPayload{ProductID: item.ProductID.String(), Quantity: item.Quantity}
	}
	return inventoryEventPayload{OrderID: orderID.String(), Items: itemPayloads}
}
