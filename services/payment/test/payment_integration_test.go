//go:build integration

package test

import (
	"context"
	stderrors "errors"
	"net/http"
	"testing"

	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/payment/internal/domain"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/payment/internal/eventstore"
	"github.com/Kalaganov-Konstantin/eventflow-commerce/services/payment/internal/repository"
	apperrors "github.com/Kalaganov-Konstantin/eventflow-commerce/shared/libs/go/errors"
	"github.com/google/uuid"
)

// TestPaymentIntegration_RebuildFromSnapshotAndEvents drives a payment through two commands,
// snapshots it mid-history and applies a third, then asserts Repository.Load reconstructs the
// exact same state by combining the snapshot with only the events recorded after it.
func TestPaymentIntegration_RebuildFromSnapshotAndEvents(t *testing.T) {
	db := openTestDB(t)
	repo := eventstore.NewRepository(db)
	snapshots := eventstore.NewSnapshotStore(db)
	ctx := context.Background()

	payment, err := domain.Initiate(uuid.New(), uuid.New(), 5000, "USD")
	if err != nil {
		t.Fatalf("initiate payment: %v", err)
	}
	if err := payment.Process("txn-1"); err != nil {
		t.Fatalf("process payment: %v", err)
	}
	if err := repo.Save(ctx, payment); err != nil {
		t.Fatalf("save payment: %v", err)
	}
	if payment.Version != 2 {
		t.Fatalf("version after initiate+process = %d, want 2", payment.Version)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if err := snapshots.SaveSnapshot(ctx, tx, payment); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit snapshot tx: %v", err)
	}

	if err := payment.Refund("customer request"); err != nil {
		t.Fatalf("refund payment: %v", err)
	}
	if err := repo.Save(ctx, payment); err != nil {
		t.Fatalf("save refund: %v", err)
	}

	reloaded, err := repo.Load(ctx, payment.ID)
	if err != nil {
		t.Fatalf("load payment: %v", err)
	}

	if reloaded.Status != domain.StatusRefunded {
		t.Errorf("status = %s, want %s", reloaded.Status, domain.StatusRefunded)
	}
	if reloaded.Version != 3 {
		t.Errorf("version = %d, want 3", reloaded.Version)
	}
	if reloaded.AmountCents != 5000 {
		t.Errorf("amount_cents = %d, want 5000", reloaded.AmountCents)
	}
	if reloaded.OrderID != payment.OrderID || reloaded.CustomerID != payment.CustomerID {
		t.Error("order_id/customer_id lost across snapshot + event replay")
	}
}

// TestPaymentIntegration_ConcurrentSaveConflictsOnVersion simulates two commands both starting
// from the same loaded aggregate version: the first Save must win, the second must be rejected as
// a version conflict by the event store's unique (aggregate_id, event_version) constraint instead
// of silently overwriting the first.
func TestPaymentIntegration_ConcurrentSaveConflictsOnVersion(t *testing.T) {
	db := openTestDB(t)
	repo := eventstore.NewRepository(db)
	ctx := context.Background()

	payment, err := domain.Initiate(uuid.New(), uuid.New(), 5000, "USD")
	if err != nil {
		t.Fatalf("initiate payment: %v", err)
	}
	if err := repo.Save(ctx, payment); err != nil {
		t.Fatalf("save initiated payment: %v", err)
	}

	copyA := *payment
	copyB := *payment
	if err := copyA.Process("txn-a"); err != nil {
		t.Fatalf("process copyA: %v", err)
	}
	if err := copyB.Fail("declined"); err != nil {
		t.Fatalf("fail copyB: %v", err)
	}

	if err := repo.Save(ctx, &copyA); err != nil {
		t.Fatalf("save copyA: expected the first concurrent writer to win, got %v", err)
	}

	err = repo.Save(ctx, &copyB)
	if err == nil {
		t.Fatal("expected the second concurrent writer to be rejected as a conflict, got nil")
	}
	var appErr *apperrors.AppError
	if !stderrors.As(err, &appErr) || appErr.HTTPCode != http.StatusConflict {
		t.Fatalf("expected a 409 conflict, got %v", err)
	}

	reloaded, err := repo.Load(ctx, payment.ID)
	if err != nil {
		t.Fatalf("load payment: %v", err)
	}
	if reloaded.Status != domain.StatusCompleted {
		t.Errorf("status = %s, want %s (copyA's outcome)", reloaded.Status, domain.StatusCompleted)
	}
}

// TestPaymentIntegration_ProjectionStaysInSyncWithEventStore asserts the payment_status read
// model reflects a payment's new state immediately after Repository.Save returns, since the
// projection is written in the same transaction as the event store append rather than by a
// separate, eventually-consistent consumer.
func TestPaymentIntegration_ProjectionStaysInSyncWithEventStore(t *testing.T) {
	db := openTestDB(t)
	repo := eventstore.NewRepository(db)
	statusRepo := repository.NewPaymentStatusRepository(db)
	ctx := context.Background()

	payment, err := domain.Initiate(uuid.New(), uuid.New(), 12345, "USD")
	if err != nil {
		t.Fatalf("initiate payment: %v", err)
	}
	if err := payment.Process("txn-sync"); err != nil {
		t.Fatalf("process payment: %v", err)
	}
	if err := repo.Save(ctx, payment); err != nil {
		t.Fatalf("save payment: %v", err)
	}

	status, err := statusRepo.GetByID(ctx, payment.ID)
	if err != nil {
		t.Fatalf("get payment status immediately after save: %v", err)
	}

	if status.Status != string(domain.StatusCompleted) {
		t.Errorf("status = %s, want %s", status.Status, domain.StatusCompleted)
	}
	if status.Version != payment.Version {
		t.Errorf("version = %d, want %d", status.Version, payment.Version)
	}
	if status.TransactionID != "txn-sync" {
		t.Errorf("transaction_id = %q, want %q", status.TransactionID, "txn-sync")
	}
	if status.CompletedAt == nil {
		t.Error("completed_at not set for a completed payment")
	}
	if status.AmountCents != 12345 {
		t.Errorf("amount_cents = %d, want 12345", status.AmountCents)
	}
}
