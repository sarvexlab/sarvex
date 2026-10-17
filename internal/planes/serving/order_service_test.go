// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package serving

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/commerce/model"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
)

func seedCompletedOrderSession(t *testing.T, sessions SessionStore, storeID, sessionID, orderID string) {
	t.Helper()
	session := &CheckoutSession{
		SessionID: sessionID, StoreID: storeID, MerchantID: storeID,
		BackendCheckoutID: "backend-1", State: SessionStateCommittedPendingHandoff,
		OrderID:   orderID,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
		LastResponse: &base.CheckoutResponse{
			ID:       sessionID,
			Status:   base.CheckoutStatusCompleted,
			Currency: "USD",
			LineItems: []model.LineItem{
				{ID: "li_1", Item: model.Item{ID: "SKU-1"}, Quantity: 2},
			},
			Order: &model.OrderConfirmation{ID: orderID, PermalinkURL: "https://shop.example/order/" + orderID},
		},
	}
	if err := sessions.Create(context.Background(), session); err != nil {
		t.Fatalf("seed session: %v", err)
	}
}

func TestOrderService_Get_ReturnsOrderFromCompletedSession(t *testing.T) {
	const storeID = "store-1"
	sessions := NewMemorySessionStore(time.Minute)
	defer sessions.Close()
	seedCompletedOrderSession(t, sessions, storeID, "sess-1", "order-1")

	svc := NewOrderService(sessions)
	order, err := svc.Get(context.Background(), storeID, "order-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if order.ID != "order-1" {
		t.Errorf("expected id order-1, got %q", order.ID)
	}
	if order.CheckoutID != "sess-1" {
		t.Errorf("expected checkout_id sess-1, got %q", order.CheckoutID)
	}
	if order.Currency != "USD" {
		t.Errorf("expected currency USD, got %q", order.Currency)
	}
	if len(order.LineItems) != 1 || order.LineItems[0].Item.ID != "SKU-1" {
		t.Errorf("expected one line item for SKU-1, got %+v", order.LineItems)
	}
	if order.LineItems[0].Quantity.Total != 2 {
		t.Errorf("expected total quantity 2, got %d", order.LineItems[0].Quantity.Total)
	}
}

func TestOrderService_Get_NotFound(t *testing.T) {
	sessions := NewMemorySessionStore(time.Minute)
	defer sessions.Close()
	svc := NewOrderService(sessions)

	_, err := svc.Get(context.Background(), "store-1", "no-such-order")
	if !errors.Is(err, ErrOrderNotFound) {
		t.Fatalf("expected ErrOrderNotFound, got %v", err)
	}
}

func TestOrderService_Update_AppendsFulfillmentEventAndAdjustment(t *testing.T) {
	const storeID = "store-1"
	sessions := NewMemorySessionStore(time.Minute)
	defer sessions.Close()
	seedCompletedOrderSession(t, sessions, storeID, "sess-1", "order-1")

	svc := NewOrderService(sessions)
	order, err := svc.Update(context.Background(), storeID, "order-1", OrderUpdateInput{
		FulfillmentEvents: []model.FulfillmentEvent{{
			ID: "evt-1", Type: model.FulfillmentEventTypeShipped,
			TrackingNumber: "1Z999", TrackingURL: "https://track.example/1Z999",
		}},
		Adjustments: []model.Adjustment{{
			ID: "adj-1", Type: model.AdjustmentTypeRefund, Status: model.AdjustmentStatusCompleted,
		}},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(order.Fulfillment.Events) != 1 || order.Fulfillment.Events[0].ID != "evt-1" {
		t.Errorf("expected one fulfillment event evt-1, got %+v", order.Fulfillment.Events)
	}
	if len(order.Adjustments) != 1 || order.Adjustments[0].ID != "adj-1" {
		t.Errorf("expected one adjustment adj-1, got %+v", order.Adjustments)
	}

	// Retrieving the order again reflects the persisted update.
	refetched, err := svc.Get(context.Background(), storeID, "order-1")
	if err != nil {
		t.Fatalf("Get after update: %v", err)
	}
	if len(refetched.Fulfillment.Events) != 1 || len(refetched.Adjustments) != 1 {
		t.Errorf("expected update to persist, got fulfillment=%+v adjustments=%+v", refetched.Fulfillment.Events, refetched.Adjustments)
	}

	// Resending the same IDs is a no-op, not a duplicate.
	order2, err := svc.Update(context.Background(), storeID, "order-1", OrderUpdateInput{
		Adjustments: []model.Adjustment{{ID: "adj-1", Type: model.AdjustmentTypeRefund, Status: model.AdjustmentStatusCompleted}},
	})
	if err != nil {
		t.Fatalf("second Update: %v", err)
	}
	if len(order2.Adjustments) != 1 {
		t.Errorf("expected adjustment resend to be a no-op, got %+v", order2.Adjustments)
	}
}
