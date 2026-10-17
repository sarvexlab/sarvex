// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package billing

import (
	"context"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// BillingRepository is the persistence interface for merchant billing records.
// The single writer is BillingService.
type BillingRepository interface {
	// Get returns ErrNotFound when no record exists for the merchant.
	Get(ctx context.Context, id domain.MerchantID) (*BillingRecordModel, error)
	// GetByStripeCustomerID returns ErrNotFound when no record has this
	// Stripe customer — used by the webhook handler to map a subscription
	// event back to a merchant.
	GetByStripeCustomerID(ctx context.Context, customerID string) (*BillingRecordModel, error)
	// Upsert creates or replaces the billing document.
	Upsert(ctx context.Context, m *BillingRecordModel) error
}

// WebhookEventRepository dedupes inbound Stripe webhook events by event ID,
// since Stripe redelivers at least once. The single writer is BillingService.
// IsProcessed is checked before processing (skip redeliveries) and
// MarkProcessed is called only after processing succeeds — never the other
// way around, or a mid-processing failure would mark an event as done when
// it was never actually applied, silently dropping the redelivery that
// would otherwise fix it.
type WebhookEventRepository interface {
	// IsProcessed reports whether eventID has already been marked processed.
	IsProcessed(ctx context.Context, eventID string) (bool, error)
	// MarkProcessed records eventID as processed. Idempotent — safe to call
	// more than once for the same eventID.
	MarkProcessed(ctx context.Context, eventID string) error
}

// NotificationPrefsRepository is the persistence interface for merchant notification preferences.
// The single writer is BillingService.
type NotificationPrefsRepository interface {
	// Get returns a document with zero-value defaults when none exists for the merchant.
	Get(ctx context.Context, id domain.MerchantID) (*NotificationPrefsModel, error)
	// Upsert creates or replaces the notification preferences document.
	Upsert(ctx context.Context, m *NotificationPrefsModel) error
}
