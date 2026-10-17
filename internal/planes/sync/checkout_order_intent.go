// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
)

// OrderIntentPayload is the body of serving.checkout.order_intent
// (sarvex-v1-syncing-plane-async.yaml order_intent message), extended with
// order_id/store_order_ref/order_status: by the time the serving plane's
// CheckoutComplete (internal/planes/serving/api/handler.go) emits this
// event, it already has these from its synchronous connector call — the
// order already exists at the merchant. OrderIntentHandler's job is to
// record that locally, never to create the order itself.
// IdempotencyKey is carried through for audit trail linking.
type OrderIntentPayload struct {
	CheckoutSessionID string       `json:"checkout_session_id"`
	OrderID           string       `json:"order_id"`
	StoreOrderRef     string       `json:"store_order_ref"`
	OrderStatus       string       `json:"order_status"`
	IdempotencyKey    string       `json:"idempotency_key"`
	LineItems         []lineItem   `json:"line_items"`
	Total             moneyPayload `json:"total"`
}

type lineItem struct {
	SKU      string `json:"sku"`
	Quantity int    `json:"quantity"`
}

// OrderCreatedPayload is the body of sync.order.created.
type OrderCreatedPayload struct {
	OrderID           string `json:"order_id"`
	CheckoutSessionID string `json:"checkout_session_id"`
	StoreOrderRef     string `json:"store_order_ref"`
}

// OrderIntentHandler is the one writer of Order records (architecture
// §4.5): it consumes serving.checkout.order_intent and persists a local
// copy of an order the serving plane's CheckoutComplete already created
// synchronously at the merchant store. It never calls the connector —
// doing so on receipt of this event would create a duplicate order.
type OrderIntentHandler struct {
	orders OrderRepository
	bus    eventbus.EventBus
	log    *slog.Logger
}

// NewOrderIntentHandler constructs a serving.checkout.order_intent handler.
func NewOrderIntentHandler(orders OrderRepository, bus eventbus.EventBus, log *slog.Logger) *OrderIntentHandler {
	if log == nil {
		log = slog.Default()
	}
	return &OrderIntentHandler{orders: orders, bus: bus, log: log}
}

// HandleOrderIntent is the serving.checkout.order_intent subscription
// entrypoint. Returning an error NACKs the event for at-least-once
// redelivery (a transient Mongo write failure is worth retrying this way);
// a structurally incomplete payload is logged and dropped instead — no
// amount of redelivery fixes missing data — with a critical task raised so
// a human investigates, since checkout already created a real order at the
// merchant that Sarvex would otherwise silently lose track of.
func (h *OrderIntentHandler) HandleOrderIntent(ctx context.Context, event eventbus.DomainEvent) error {
	var payload OrderIntentPayload
	if err := json.Unmarshal(event.Payload(), &payload); err != nil {
		h.log.Error("order intent: malformed payload", "error", err, "store_id", event.StoreID())
		return nil
	}

	if payload.CheckoutSessionID == "" || payload.OrderID == "" || payload.StoreOrderRef == "" {
		h.log.Error("order intent: missing order identifiers", "store_id", event.StoreID(), "checkout_session_id", payload.CheckoutSessionID)
		h.raiseHandoffTask(ctx, event, payload.CheckoutSessionID, "order intent event is missing order_id/store_order_ref")
		return nil
	}

	existing, err := h.orders.GetByCheckoutSession(ctx, payload.CheckoutSessionID)
	if err != nil && !errors.Is(err, ErrOrderNotFound) {
		return fmt.Errorf("check existing order: %w", err)
	}
	if err == nil {
		h.log.Debug("order intent: order already recorded, no-op", "checkout_session_id", payload.CheckoutSessionID, "order_id", existing.OrderID)
		return nil
	}

	status := OrderStatus(payload.OrderStatus)
	if status == "" {
		status = OrderStatusCreated
	}

	order := &Order{
		OrderID:           payload.OrderID,
		StoreID:           event.StoreID(),
		MerchantID:        event.MerchantID(),
		CheckoutSessionID: payload.CheckoutSessionID,
		StoreOrderRef:     payload.StoreOrderRef,
		Status:            status,
		SyncState:         OrderSyncSynced, // already confirmed created at the merchant
		ValueMinor:        payload.Total.AmountMinor,
		Currency:          payload.Total.Currency,
		PlacedAt:          event.OccurredAt(),
		IdempotencyKey:    payload.IdempotencyKey,
		MutationAttempts:  []MutationAttempt{}, // empty, will be populated by audit log
	}

	if err := h.orders.Create(ctx, order); err != nil {
		if errors.Is(err, ErrOrderExists) {
			h.log.Debug("order intent: order created concurrently, no-op", "checkout_session_id", payload.CheckoutSessionID)
			return nil
		}
		return fmt.Errorf("create order: %w", err)
	}

	createdBytes, err := json.Marshal(OrderCreatedPayload{
		OrderID:           order.OrderID,
		CheckoutSessionID: order.CheckoutSessionID,
		StoreOrderRef:     order.StoreOrderRef,
	})
	if err != nil {
		h.log.Error("order intent: failed to marshal order.created", "error", err, "order_id", order.OrderID)
		return nil
	}
	if err := h.bus.Publish(ctx, newDomainEvent(ucp.EventOrderCreated, order.StoreID, order.MerchantID, createdBytes)); err != nil {
		h.log.Error("order intent: failed to publish order.created", "error", err, "order_id", order.OrderID)
	}

	h.log.Info("order recorded from checkout", "order_id", order.OrderID, "store_id", order.StoreID, "checkout_session_id", order.CheckoutSessionID)
	return nil
}

// raiseHandoffTask publishes a critical, sarvex-owned task when an
// order_intent event can't be persisted as-is. Reuses sync.task.raised
// (task.Subscriber, a working consumer) rather than the AsyncAPI's
// order.handoff_failed channel, which nothing in the codebase subscribes
// to today — publishing to an unconsumed channel would just recreate the
// "published into the void" problem this handler exists to fix.
func (h *OrderIntentHandler) raiseHandoffTask(ctx context.Context, event eventbus.DomainEvent, checkoutSessionID, reason string) {
	taskBytes, err := json.Marshal(TaskRaisedPayload{
		Source:         "order_handoff",
		SourceRef:      checkoutSessionID,
		Owner:          "sarvex",
		Title:          fmt.Sprintf("Order handoff needs attention for checkout %s: %s", checkoutSessionID, reason),
		BlocksCheckout: false,
	})
	if err != nil {
		h.log.Error("order intent: failed to marshal task.raised", "error", err)
		return
	}
	if err := h.bus.Publish(ctx, newDomainEvent(ucp.EventSyncTaskRaised, event.StoreID(), event.MerchantID(), taskBytes)); err != nil {
		h.log.Error("order intent: failed to publish task.raised", "error", err)
	}
}
