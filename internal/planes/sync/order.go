// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"errors"
	"time"
)

// ErrOrderNotFound is returned when no order exists for the given lookup.
var ErrOrderNotFound = errors.New("sync: order not found")

// OrderStatus mirrors the control-plane domain.OrderStatus wire values
// (sarvex-v1-control-plane.yaml order_status schema).
type OrderStatus string

const (
	OrderStatusCreated   OrderStatus = "created"
	OrderStatusConfirmed OrderStatus = "confirmed"
	OrderStatusShipped   OrderStatus = "shipped"
	OrderStatusCancelled OrderStatus = "cancelled"
	OrderStatusRefunded  OrderStatus = "refunded"
)

// OrderSyncState mirrors the control-plane domain.OrderSyncState wire values.
type OrderSyncState string

const (
	OrderSyncCreated OrderSyncState = "created"
	OrderSyncSynced  OrderSyncState = "synced"
	OrderSyncFailed  OrderSyncState = "failed"
)

// Order is Sarvex's local record of a purchase created via a Google
// checkout session. Written once, by OrderIntentHandler
// (checkout_order_intent.go), from data the serving plane's CheckoutComplete
// already obtained synchronously from the merchant store — this package
// never calls the connector to create an order itself; the order already
// exists at the merchant by the time this record is written.
//
// Idempotency: OrderID is deterministically derived from merchant_id +
// idempotency_key + counter, ensuring the same key always produces the
// same order ID. MutationAttempts tracks every attempt (handler response,
// outcome, timestamp) for audit and replay diagnostics.
type Order struct {
	OrderID           string         `bson:"_id" json:"order_id"`
	StoreID           string         `bson:"store_id" json:"store_id"`
	MerchantID        string         `bson:"merchant_id" json:"merchant_id"`
	CheckoutSessionID string         `bson:"checkout_session_id" json:"checkout_session_id"`
	StoreOrderRef     string         `bson:"store_order_ref" json:"store_order_ref"`
	Status            OrderStatus    `bson:"status" json:"status"`
	SyncState         OrderSyncState `bson:"sync_state" json:"sync_state"`
	ValueMinor        int64          `bson:"value_minor" json:"value_minor"`
	Currency          string         `bson:"currency" json:"currency"`
	PlacedAt          time.Time      `bson:"placed_at" json:"placed_at"`
	// IdempotencyKey is the original request's idempotency key,
	// scoped to merchant_id. Enables audit log queries by key.
	IdempotencyKey string `bson:"idempotency_key" json:"idempotency_key"`
	// MutationAttempts logs every attempt to create/capture/refund/void this order,
	// including handler responses, local status, and timestamps.
	// Enables "replay by key" diagnostics and double-charge detection.
	MutationAttempts []MutationAttempt `bson:"mutation_attempts" json:"mutation_attempts"`
}

// MutationAttempt records a single attempt to mutate an order (create, capture, refund, void).
type MutationAttempt struct {
	AttemptedAt     time.Time              `bson:"attempted_at" json:"attempted_at"`
	CompletedAt     *time.Time             `bson:"completed_at,omitempty" json:"completed_at,omitempty"`
	IdempotencyKey  string                 `bson:"idempotency_key" json:"idempotency_key"`
	MutationType    string                 `bson:"mutation_type" json:"mutation_type"` // "create", "capture", "refund", "void"
	HandlerResponse map[string]interface{} `bson:"handler_response" json:"handler_response"`
	LocalStatus     string                 `bson:"local_status" json:"local_status"` // "paid", "capturing", "refunded", etc.
	ErrorMessage    string                 `bson:"error_message,omitempty" json:"error_message,omitempty"`
	RetryCount      int                    `bson:"retry_count" json:"retry_count"`
}

// OrderRepository persists Order records — OrderIntentHandler is the one
// writer per store (architecture §4.5); the control plane reads the same
// collection read-only (operator.OrderReader) for the console.
type OrderRepository interface {
	// Create inserts a new order. Returns ErrOrderExists if one already
	// exists for this checkout session (redelivered order_intent event).
	Create(ctx context.Context, order *Order) error
	// GetByCheckoutSession looks up an order by its originating checkout
	// session, used to detect a redelivered order_intent event before
	// attempting Create. Returns ErrOrderNotFound if none exists.
	GetByCheckoutSession(ctx context.Context, checkoutSessionID string) (*Order, error)
}

// ErrOrderExists is returned by Create when an order already exists for the
// given checkout session.
var ErrOrderExists = errors.New("sync: order already exists for checkout session")
