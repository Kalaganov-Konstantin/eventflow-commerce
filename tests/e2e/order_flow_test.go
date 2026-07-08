//go:build e2e

package e2e

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
)

type createOrderResponse struct {
	OrderID string `json:"order_id"`
	Status  string `json:"status"`
}

type orderResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type inventoryResponse struct {
	ProductID         string `json:"product_id"`
	QuantityAvailable int    `json:"quantity_available"`
	QuantityReserved  int    `json:"quantity_reserved"`
}

type notificationResponse struct {
	ReferenceID string `json:"reference_id"`
}

// TestE2E_OrderFlowConfirmsAndNotifies drives a whole order through the running stack: create
// through the gateway, wait for the saga to confirm it, then check the side effects the saga is
// meant to leave behind, a notification and a committed stock reservation.
func TestE2E_OrderFlowConfirmsAndNotifies(t *testing.T) {
	db := openInventoryDB(t)
	const seededStock = 10
	const orderQuantity = 2
	productID := seedProductWithStock(t, db, seededStock)

	customerID := uuid.New()
	client := newAPIClient(gatewayURL(), issueJWT(t, customerID))

	createReq := map[string]any{
		"currency": "USD",
		"items":    []any{orderItem(productID, orderQuantity, 1999)},
	}

	var created createOrderResponse
	status := client.do(t, http.MethodPost, "/api/v1/orders", createReq, &created)
	if status != http.StatusAccepted {
		t.Fatalf("create order status = %d, want %d", status, http.StatusAccepted)
	}
	if created.OrderID == "" {
		t.Fatal("create order response has no order_id")
	}

	orderID, err := uuid.Parse(created.OrderID)
	if err != nil {
		t.Fatalf("parse order id %q: %v", created.OrderID, err)
	}

	var order orderResponse
	waitFor(t, 30*time.Second, "order to reach confirmed", func() bool {
		s := client.do(t, http.MethodGet, "/api/v1/orders/"+orderID.String(), nil, &order)
		return s == http.StatusOK && order.Status == "confirmed"
	})

	notificationClient := newAPIClient(notificationURL(), "").withHeader("X-User-ID", customerID.String())
	waitFor(t, 30*time.Second, "a notification for the confirmed order", func() bool {
		var notifications []notificationResponse
		s := notificationClient.do(t, http.MethodGet, "/api/v1/notifications", nil, &notifications)
		if s != http.StatusOK {
			return false
		}
		for _, n := range notifications {
			if n.ReferenceID == orderID.String() {
				return true
			}
		}
		return false
	})

	waitFor(t, 15*time.Second, "the stock reservation to be committed", func() bool {
		var inv inventoryResponse
		s := client.do(t, http.MethodGet, "/api/v1/inventory/"+productID.String(), nil, &inv)
		return s == http.StatusOK && inv.QuantityReserved == 0
	})

	available, reserved := stockOf(t, db, productID)
	if reserved != 0 {
		t.Errorf("quantity_reserved = %d, want 0 once the order is confirmed and committed", reserved)
	}
	wantAvailable := seededStock - orderQuantity
	if available != wantAvailable {
		t.Errorf("quantity_available = %d, want %d", available, wantAvailable)
	}
}

// TestE2E_OrderForOutOfStockProductReturns409 asks for more of a product than is available and
// expects the gateway to answer 409 without creating anything: the order handler reserves stock
// synchronously before it accepts the order.
func TestE2E_OrderForOutOfStockProductReturns409(t *testing.T) {
	db := openInventoryDB(t)
	productID := seedProductWithStock(t, db, 0)

	customerID := uuid.New()
	client := newAPIClient(gatewayURL(), issueJWT(t, customerID))

	createReq := map[string]any{
		"currency": "USD",
		"items":    []any{orderItem(productID, 1, 999)},
	}

	status := client.do(t, http.MethodPost, "/api/v1/orders", createReq, nil)
	if status != http.StatusConflict {
		t.Fatalf("create order for an out-of-stock product status = %d, want %d", status, http.StatusConflict)
	}

	_, reserved := stockOf(t, db, productID)
	if reserved != 0 {
		t.Errorf("quantity_reserved = %d, want 0: a rejected order must not reserve anything", reserved)
	}
}
