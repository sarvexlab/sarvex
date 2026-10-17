// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package serving

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/eventbus"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/safego"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp"
)

// OrderIntentEvent is emitted at checkout.complete after freshness guard
// passes. OrderID/StoreOrderRef/OrderStatus are populated from the
// connector's CheckoutComplete response — by the time this event fires, the
// order already exists at the merchant store; the sync-plane consumer
// (OrderIntentHandler) only records it, it never creates the order itself.
// IdempotencyKey is carried through for audit trail linking and replay diagnostics.
type OrderIntentEvent struct {
	CheckoutSessionID string     `json:"checkout_session_id"`
	OrderID           string     `json:"order_id"`
	StoreOrderRef     string     `json:"store_order_ref"`
	OrderStatus       string     `json:"order_status"`
	IdempotencyKey    string     `json:"idempotency_key"` // for audit trail + replay diagnostics
	StoreID_          string     `json:"store_id"`
	MerchantID_       string     `json:"merchant_id"` // denormalized for scope resolution
	LineItems         []LineItem `json:"line_items"`
	Total             Money      `json:"total"`
	EmittedAt_        time.Time  `json:"emitted_at"`
	Payload_          []byte     `json:"-"`
}

// LineItem is a line item in the order.
type LineItem struct {
	SKU      string `json:"sku"`
	Quantity int    `json:"quantity"`
}

// EventType implements eventbus.DomainEvent.
func (o *OrderIntentEvent) EventType() string {
	return ucp.EventOrderIntent
}

// StoreID implements eventbus.DomainEvent.
func (o *OrderIntentEvent) StoreID() string {
	return o.StoreID_
}

// MerchantID implements eventbus.DomainEvent.
func (o *OrderIntentEvent) MerchantID() string {
	return o.MerchantID_
}

// OccurredAt implements eventbus.DomainEvent.
func (o *OrderIntentEvent) OccurredAt() time.Time {
	return o.EmittedAt_
}

// Payload implements eventbus.DomainEvent.
func (o *OrderIntentEvent) Payload() []byte {
	if o.Payload_ == nil {
		body, _ := json.Marshal(map[string]interface{}{
			"checkout_session_id": o.CheckoutSessionID,
			"order_id":            o.OrderID,
			"store_order_ref":     o.StoreOrderRef,
			"order_status":        o.OrderStatus,
			"idempotency_key":     o.IdempotencyKey,
			"store_id":            o.StoreID_,
			"merchant_id":         o.MerchantID_,
			"line_items":          o.LineItems,
			"total":               o.Total,
			"emitted_at":          o.EmittedAt_,
		})
		o.Payload_ = body
	}
	return o.Payload_
}

// EmitOrderIntent publishes the order-intent event to the sync plane.
func EmitOrderIntent(ctx context.Context, pub eventbus.EventBus, ev *OrderIntentEvent, log *slog.Logger) error {
	err := pub.Publish(ctx, ev)
	if err != nil {
		log.Error("failed to emit order-intent", "error", err, "session_id", ev.CheckoutSessionID, "merchant_id", ev.MerchantID_)
		return fmt.Errorf("publish order-intent: %w", err)
	}

	log.Debug("order-intent emitted", "session_id", ev.CheckoutSessionID, "merchant_id", ev.MerchantID_)
	return nil
}

// RequestLogEvent is emitted for every agent request (fire-and-forget).
type RequestLogEvent struct {
	StoreID_    string    `json:"store_id"`
	MerchantID_ string    `json:"merchant_id"` // denormalized for scope resolution
	Handler     string    `json:"handler"`
	Method      string    `json:"method"`
	Path        string    `json:"path"`
	LatencyMs   int64     `json:"latency_ms"`
	StatusCode  int       `json:"status_code"`
	SessionID   string    `json:"session_id,omitempty"`
	RequestID   string    `json:"request_id"`
	OccurredAt_ time.Time `json:"occurred_at"`
	Payload_    []byte    `json:"-"`
}

// EventType implements eventbus.DomainEvent.
func (r *RequestLogEvent) EventType() string {
	return ucp.EventLogRequested
}

// StoreID implements eventbus.DomainEvent.
func (r *RequestLogEvent) StoreID() string {
	return r.StoreID_
}

// MerchantID implements eventbus.DomainEvent.
func (r *RequestLogEvent) MerchantID() string {
	return r.MerchantID_
}

// OccurredAt implements eventbus.DomainEvent.
func (r *RequestLogEvent) OccurredAt() time.Time {
	return r.OccurredAt_
}

// Payload implements eventbus.DomainEvent.
func (r *RequestLogEvent) Payload() []byte {
	if r.Payload_ == nil {
		body, _ := json.Marshal(map[string]interface{}{
			"store_id":    r.StoreID_,
			"merchant_id": r.MerchantID_,
			"handler":     r.Handler,
			"method":      r.Method,
			"path":        r.Path,
			"latency_ms":  r.LatencyMs,
			"status_code": r.StatusCode,
			"session_id":  r.SessionID,
			"request_id":  r.RequestID,
			"occurred_at": r.OccurredAt_,
		})
		r.Payload_ = body
	}
	return r.Payload_
}

// EmitRequestLog publishes a request log to the event bus (non-blocking).
func EmitRequestLog(ctx context.Context, pub eventbus.EventBus, ev *RequestLogEvent, log *slog.Logger) {
	// Fire-and-forget in background; never block serving
	safego.Go("emit-request-log", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()

		if err := pub.Publish(ctx, ev); err != nil {
			log.Debug("request-log publish failed", "error", err, "request_id", ev.RequestID)
		}
	})
}

// RebuildRequestedEvent is emitted by mapping/scoring workers when a store's catalog changes.
type RebuildRequestedEvent struct {
	StoreID_    string    `json:"store_id"`
	MerchantID_ string    `json:"merchant_id"` // denormalized for scope resolution
	Reason      string    `json:"reason"`      // "catalog_scored", "price_changed", "inventory_changed"
	TriggerID   string    `json:"trigger_id"`
	OccurredAt_ time.Time `json:"occurred_at"`
	Payload_    []byte    `json:"-"`
}

// SnapshotSwappedEvent is emitted by the cache rebuilder after a successful swap.
type SnapshotSwappedEvent struct {
	StoreID_     string    `json:"store_id"`
	MerchantID_  string    `json:"merchant_id"` // denormalized for scope resolution
	ProductCount int       `json:"product_count"`
	BuiltAt_     time.Time `json:"built_at"`
	SourceSync_  time.Time `json:"source_sync"`
	EmittedAt_   time.Time `json:"emitted_at"`
	Payload_     []byte    `json:"-"`
}

// EventType implements eventbus.DomainEvent.
func (s *SnapshotSwappedEvent) EventType() string {
	return ucp.EventSnapshotSwapped
}

// StoreID implements eventbus.DomainEvent.
func (s *SnapshotSwappedEvent) StoreID() string {
	return s.StoreID_
}

// MerchantID implements eventbus.DomainEvent.
func (s *SnapshotSwappedEvent) MerchantID() string {
	return s.MerchantID_
}

// OccurredAt implements eventbus.DomainEvent.
func (s *SnapshotSwappedEvent) OccurredAt() time.Time {
	return s.EmittedAt_
}

// Payload implements eventbus.DomainEvent.
func (s *SnapshotSwappedEvent) Payload() []byte {
	if s.Payload_ == nil {
		body, _ := json.Marshal(map[string]interface{}{
			"store_id":      s.StoreID_,
			"merchant_id":   s.MerchantID_,
			"product_count": s.ProductCount,
			"built_at":      s.BuiltAt_,
			"source_sync":   s.SourceSync_,
			"emitted_at":    s.EmittedAt_,
		})
		s.Payload_ = body
	}
	return s.Payload_
}
