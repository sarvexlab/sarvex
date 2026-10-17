// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package domain

import "time"

// OrderStatus is the lifecycle state of a Google-originated order.
type OrderStatus string

const (
	OrderCreated   OrderStatus = "created"
	OrderConfirmed OrderStatus = "confirmed"
	OrderShipped   OrderStatus = "shipped"
	OrderCancelled OrderStatus = "cancelled"
	OrderRefunded  OrderStatus = "refunded"
)

// OrderSyncState tracks the handoff between Sarvex and the merchant store.
type OrderSyncState string

const (
	SyncPendingHandoff OrderSyncState = "pending_handoff"
	SyncCreated        OrderSyncState = "created"
	SyncSynced         OrderSyncState = "synced"
	SyncFailed         OrderSyncState = "failed"
)

// Order is a purchase created via a Google checkout session.
type Order struct {
	OrderID           string         `json:"order_id"`
	CheckoutSessionID string         `json:"checkout_session_id"`
	StoreOrderRef     *string        `json:"store_order_ref"`
	Status            OrderStatus    `json:"status"`
	SyncState         OrderSyncState `json:"sync_state"`
	Value             Money          `json:"value"`
	PlacedAt          time.Time      `json:"placed_at"`
}
