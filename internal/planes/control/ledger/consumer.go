// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package ledger

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/oklog/ulid"

	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
)

// Consumer subscribes to serving.checkout.failed and
// serving.checkout.abandoned (ucp.EventCheckoutFailed/EventCheckoutAbandoned
// — the versioned checkout lifecycle contract, event_version "1.0",
// internal/planes/serving/lifecycle_contract.go) and turns each into
// exactly one ledger Entry (R1, R2): a duplicate event_id is a no-op
// (Store's idempotency guard), an event that can't be classified still
// produces an entry with cause=unclassified (R2) — nothing is ever
// dropped. Runs regardless of billing tier (R15) — the entitlement gate is
// on the read API only, never here.
type Consumer struct {
	store  Store
	events eventbus.EventBus
	log    *slog.Logger

	mu   sync.Mutex
	subs []eventbus.Subscription
}

// NewConsumer constructs a Consumer.
func NewConsumer(store Store, events eventbus.EventBus, log *slog.Logger) *Consumer {
	if log == nil {
		log = slog.Default()
	}
	return &Consumer{store: store, events: events, log: log}
}

// Start subscribes to both source topics.
func (c *Consumer) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	handlers := map[string]eventbus.EventHandler{
		ucp.EventCheckoutFailed:    c.handleFailed,
		ucp.EventCheckoutAbandoned: c.handleAbandoned,
	}
	for topic, handler := range handlers {
		sub, err := c.events.Subscribe(ctx, topic, handler)
		if err != nil {
			return fmt.Errorf("subscribe %s: %w", topic, err)
		}
		c.subs = append(c.subs, sub)
	}
	return nil
}

// Stop unsubscribes from every topic.
func (c *Consumer) Stop(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, sub := range c.subs {
		if err := c.events.Unsubscribe(ctx, sub); err != nil {
			c.log.Error("ledger: unsubscribe failed", "topic", sub.Topic(), "error", err)
		}
	}
	c.subs = nil
	return nil
}

// cartSnapshotFields mirrors serving's CartSnapshot
// (internal/planes/serving/lifecycle_contract.go) — total_minor is 0 when
// no live connector pricing was ever resolved for the session (an honest
// "unknown", not a fabricated 0 price; see that type's doc comment), which
// is exactly R8's revenue_unknown signal.
type cartSnapshotFields struct {
	Currency   string `json:"currency"`
	TotalMinor int64  `json:"total_minor"`
}

func (c cartSnapshotFields) revenueUnknown() bool {
	return c.TotalMinor == 0
}

func (c *Consumer) handleFailed(ctx context.Context, ev eventbus.DomainEvent) error {
	var payload struct {
		EventID    string             `json:"event_id"`
		CheckoutID string             `json:"checkout_id"`
		StoreID    string             `json:"store_id"`
		MerchantID string             `json:"merchant_id"`
		Surface    string             `json:"surface"`
		Code       string             `json:"code"`
		Stage      string             `json:"stage"`
		Cart       cartSnapshotFields `json:"cart_snapshot"`
		OccurredAt time.Time          `json:"occurred_at"`
	}
	if err := json.Unmarshal(ev.Payload(), &payload); err != nil {
		// A payload this package can't even parse still must not be
		// silently dropped (R2) — but there's nothing sensible left to key
		// an idempotent insert on, so this is logged and acked (the eventbus
		// treats a nil return as success) rather than endlessly retried.
		c.log.Error("ledger: unmarshal checkout_failed failed", "error", err)
		return nil
	}
	return c.record(ctx, entryInput{
		EventID:     payload.EventID,
		CheckoutID:  payload.CheckoutID,
		StoreID:     payload.StoreID,
		MerchantID:  payload.MerchantID,
		Surface:     payload.Surface,
		FailureCode: payload.Code,
		Stage:       payload.Stage,
		Cart:        payload.Cart,
		OccurredAt:  payload.OccurredAt,
	})
}

func (c *Consumer) handleAbandoned(ctx context.Context, ev eventbus.DomainEvent) error {
	var payload struct {
		EventID    string             `json:"event_id"`
		CheckoutID string             `json:"checkout_id"`
		StoreID    string             `json:"store_id"`
		MerchantID string             `json:"merchant_id"`
		Surface    string             `json:"surface"`
		Stage      string             `json:"stage"`
		Cart       cartSnapshotFields `json:"cart_snapshot"`
		OccurredAt time.Time          `json:"occurred_at"`
	}
	if err := json.Unmarshal(ev.Payload(), &payload); err != nil {
		c.log.Error("ledger: unmarshal checkout_abandoned failed", "error", err)
		return nil
	}
	return c.record(ctx, entryInput{
		EventID:     payload.EventID,
		CheckoutID:  payload.CheckoutID,
		StoreID:     payload.StoreID,
		MerchantID:  payload.MerchantID,
		Surface:     payload.Surface,
		FailureCode: FailureCodeAbandoned,
		Stage:       payload.Stage,
		Cart:        payload.Cart,
		OccurredAt:  payload.OccurredAt,
	})
}

type entryInput struct {
	EventID     string
	CheckoutID  string
	StoreID     string
	MerchantID  string
	Surface     string
	FailureCode string
	Stage       string
	Cart        cartSnapshotFields
	OccurredAt  time.Time
}

// record classifies in and builds+persists the Entry. Never returns an
// error for a classification miss (R2) — Classify itself always returns
// something, defaulting to CauseUnclassified — only a genuine store
// failure (Mongo unavailable, etc.) returns an error here, which NACKs the
// message for redelivery (at-least-once, R1's duplicate-safe contract
// covers the retry).
func (c *Consumer) record(ctx context.Context, in entryInput) error {
	classification := Classify(in.FailureCode, in.Stage)

	revenueUnknown := in.Cart.revenueUnknown()
	lostRevenueMinor := int64(0)
	currency := ""
	if !revenueUnknown {
		lostRevenueMinor = in.Cart.TotalMinor
		currency = in.Cart.Currency
	}

	now := time.Now().UTC()
	entry := Entry{
		EntryID:           "led_" + ulid.MustNew(ulid.Timestamp(now), rand.Reader).String(),
		MerchantID:        in.MerchantID,
		EventID:           in.EventID,
		OccurredAt:        in.OccurredAt,
		CheckoutID:        in.CheckoutID,
		StoreID:           in.StoreID,
		Surface:           in.Surface,
		Stage:             in.Stage,
		FailureCode:       in.FailureCode,
		Cause:             classification.Cause,
		RecommendedAction: classification.RecommendedAction,
		LostRevenueMinor:  lostRevenueMinor,
		Currency:          currency,
		RevenueUnknown:    revenueUnknown,
		Status:            StatusOpen,
		StatusChangedAt:   now,
		CreatedAt:         now,
	}

	if _, err := c.store.InsertEntryAndRollup(ctx, entry); err != nil {
		c.log.Error("ledger: insert entry and rollup failed", "error", err, "event_id", in.EventID)
		return err
	}
	return nil
}
