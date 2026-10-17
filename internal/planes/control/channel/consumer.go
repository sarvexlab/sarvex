// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package channel

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
)

// Consumer builds Session/DailyAggregate documents from the checkout
// lifecycle event contract (sarvex-v1-syncing-plane-async.yaml,
// event_version "1.0") plus serving.checkout.payment_submitted (the
// pre-existing session-projection contract's one event with no versioned-
// contract equivalent — checkout_service.go emits it and nothing else at
// the payment-submission transition) and serving.request.telemetry_recorded
// (R2's discovery-request fallback). Runs regardless of billing tier
// (R15) — the entitlement gate is on the read API only.
//
// ── R1's correlation-key audit (the finding this whole package is built
// on) ──
//
// Precedence (a), an explicit correlation identifier: every event in the
// versioned checkout lifecycle contract carries checkout_session_id
// (named checkout_id on some events, session.SessionID underneath either
// way — see internal/planes/serving/checkout_lifecycle_events.go). This is
// the ONLY identifier on the wire that reliably correlates more than one
// request to the same caller, and it exists only from checkout-session
// creation onward.
//
// Precedence (b), negotiation context identity: audited via
// internal/pkg/ucp/negotiation/cache.go's CacheKey and
// internal/planes/serving/lifecycle_contract.go's agentPlatformFromHeader.
// Both resolve to the calling *platform's* profile URL host (e.g.
// "gemini.google.com") — exactly the same signal Surface/AgentPlatform
// already capture. It identifies which agent platform is calling, not
// which buyer or which conversation; RFC 9421's keyid
// (internal/pkg/ucp/ucpsig) is the *store's* signing key, not the
// caller's. Nothing in this codebase issues a per-buyer or per-session
// identity to the calling agent. Using the platform host as a session key
// would conflate every concurrent buyer on that platform/store into one
// fabricated "session" — precisely what R1 says not to do ("do not
// fabricate correlation the data cannot support"). Rejected.
//
// Precedence (c), a derived key: the only remaining request attributes
// (store_id, surface, route, timestamp) suffer the identical conflation
// problem as (b) — two different buyers browsing the same store over the
// same surface in the same time window are indistinguishable from one
// buyer's journey. No IP address or cookie exists on this path to break
// the tie (and IP would be forbidden as PII, R4, even if it did). Rejected
// for pre-checkout discovery.
//
// Conclusion: this package sessionizes only checkout-anchored request
// sequences (tier (a)); it does not invent sessions for pre-checkout
// discovery traffic, consistent with R2's explicit fallback — those
// requests still contribute DailyAggregate.DiscoveryRequests, a request-
// level (not session-level) metric, via serving.request.telemetry_recorded.
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

// Start subscribes to every source topic this worker folds.
func (c *Consumer) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	handlers := map[string]eventbus.EventHandler{
		ucp.EventCheckoutInitiated:        c.handleInitiated,
		ucp.EventCheckoutUpdated:          c.handleUpdated,
		ucp.EventCheckoutPaymentSubmitted: c.handlePaymentSubmitted,
		ucp.EventCheckoutEscalated:        c.handleEscalated,
		ucp.EventCheckoutFailed:           c.handleFailed,
		ucp.EventCheckoutAbandoned:        c.handleAbandoned,
		ucp.EventOrderIntent:              c.handleOrderIntent,
		ucp.EventTelemetryRecorded:        c.handleTelemetry,
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
			c.log.Error("channel: unsubscribe failed", "topic", sub.Topic(), "error", err)
		}
	}
	c.subs = nil
	return nil
}

// fold unmarshals and applies a session-event fold, logging (never
// erroring back to the bus, so a malformed payload doesn't loop
// redelivery forever) on parse failure, matching ledger.Consumer's
// handleFailed/handleAbandoned precedent exactly.
func (c *Consumer) fold(ctx context.Context, kind SessionEventKind, eventID, sessionID, storeID, merchantID, surface, agentPlatform string, occurredAt time.Time, revenueMinor int64, currency string) error {
	if eventID == "" || sessionID == "" {
		c.log.Error("channel: fold missing required identifiers", "kind", kind, "event_id", eventID, "session_id", sessionID)
		return nil
	}
	_, err := c.store.FoldSessionEvent(ctx, SessionEvent{
		EventID: eventID, SessionID: sessionID, MerchantID: merchantID, StoreID: storeID,
		Surface: Surface(surface), AgentPlatform: agentPlatform, OccurredAt: occurredAt,
		Kind: kind, RevenueMinor: revenueMinor, Currency: currency,
	})
	if err != nil {
		c.log.Error("channel: fold session event failed", "error", err, "kind", kind, "event_id", eventID)
		return err // NACK — genuine store failure, worth at-least-once retry.
	}
	return nil
}

type lifecyclePayload struct {
	EventID    string    `json:"event_id"`
	CheckoutID string    `json:"checkout_id"`
	StoreID    string    `json:"store_id"`
	MerchantID string    `json:"merchant_id"`
	Surface    string    `json:"surface"`
	OccurredAt time.Time `json:"occurred_at"`
}

func (c *Consumer) handleInitiated(ctx context.Context, ev eventbus.DomainEvent) error {
	var p lifecyclePayload
	if err := json.Unmarshal(ev.Payload(), &p); err != nil {
		c.log.Error("channel: unmarshal checkout.initiated failed", "error", err)
		return nil
	}
	return c.fold(ctx, SessionEventInitiated, p.EventID, p.CheckoutID, p.StoreID, p.MerchantID, p.Surface, "", p.OccurredAt, 0, "")
}

func (c *Consumer) handleUpdated(ctx context.Context, ev eventbus.DomainEvent) error {
	var p lifecyclePayload
	if err := json.Unmarshal(ev.Payload(), &p); err != nil {
		c.log.Error("channel: unmarshal checkout.updated failed", "error", err)
		return nil
	}
	return c.fold(ctx, SessionEventCartActivity, p.EventID, p.CheckoutID, p.StoreID, p.MerchantID, p.Surface, "", p.OccurredAt, 0, "")
}

func (c *Consumer) handleEscalated(ctx context.Context, ev eventbus.DomainEvent) error {
	var p lifecyclePayload
	if err := json.Unmarshal(ev.Payload(), &p); err != nil {
		c.log.Error("channel: unmarshal checkout.escalated failed", "error", err)
		return nil
	}
	return c.fold(ctx, SessionEventEscalated, p.EventID, p.CheckoutID, p.StoreID, p.MerchantID, p.Surface, "", p.OccurredAt, 0, "")
}

func (c *Consumer) handleFailed(ctx context.Context, ev eventbus.DomainEvent) error {
	var p lifecyclePayload
	if err := json.Unmarshal(ev.Payload(), &p); err != nil {
		c.log.Error("channel: unmarshal checkout.failed failed", "error", err)
		return nil
	}
	return c.fold(ctx, SessionEventFailed, p.EventID, p.CheckoutID, p.StoreID, p.MerchantID, p.Surface, "", p.OccurredAt, 0, "")
}

func (c *Consumer) handleAbandoned(ctx context.Context, ev eventbus.DomainEvent) error {
	var p lifecyclePayload
	if err := json.Unmarshal(ev.Payload(), &p); err != nil {
		c.log.Error("channel: unmarshal checkout.abandoned failed", "error", err)
		return nil
	}
	return c.fold(ctx, SessionEventAbandoned, p.EventID, p.CheckoutID, p.StoreID, p.MerchantID, p.Surface, "", p.OccurredAt, 0, "")
}

// paymentSubmittedPayload is serving.checkout.payment_submitted's shape —
// the pre-existing session-projection contract's field names
// (checkout_session_id, emitted_at), not the versioned contract's
// (checkout_id, occurred_at). No surface field either: the session
// already carries surface from its initiated fold, and R1's precedence
// (a) key (checkout_session_id) is present here regardless.
type paymentSubmittedPayload struct {
	EventID           string    `json:"event_id"`
	CheckoutSessionID string    `json:"checkout_session_id"`
	StoreID           string    `json:"store_id"`
	MerchantID        string    `json:"merchant_id"`
	EmittedAt         time.Time `json:"emitted_at"`
}

func (c *Consumer) handlePaymentSubmitted(ctx context.Context, ev eventbus.DomainEvent) error {
	var p paymentSubmittedPayload
	if err := json.Unmarshal(ev.Payload(), &p); err != nil {
		c.log.Error("channel: unmarshal checkout.payment_submitted failed", "error", err)
		return nil
	}
	return c.fold(ctx, SessionEventCheckoutReached, p.EventID, p.CheckoutSessionID, p.StoreID, p.MerchantID, "", "", p.EmittedAt, 0, "")
}

// orderIntentPayload mirrors internal/planes/sync.OrderIntentPayload — no
// event_id field on the wire (see fold's eventID derivation below), no
// surface field (the session already carries it).
type orderIntentPayload struct {
	CheckoutSessionID string `json:"checkout_session_id"`
	OrderID           string `json:"order_id"`
	StoreID           string `json:"store_id"`
	MerchantID        string `json:"merchant_id"`
	Total             struct {
		AmountMinor int64  `json:"amount_minor"`
		Currency    string `json:"currency"`
	} `json:"total"`
}

func (c *Consumer) handleOrderIntent(ctx context.Context, ev eventbus.DomainEvent) error {
	var p orderIntentPayload
	if err := json.Unmarshal(ev.Payload(), &p); err != nil {
		c.log.Error("channel: unmarshal order_intent failed", "error", err)
		return nil
	}
	if p.OrderID == "" {
		c.log.Error("channel: order_intent missing order_id", "checkout_session_id", p.CheckoutSessionID)
		return nil
	}
	// order_intent carries no event_id — OrderID is already this event's
	// natural, unique idempotency key (mirrors
	// internal/planes/sync.OrderIntentHandler's own reliance on it).
	eventID := "order_intent:" + p.OrderID
	return c.fold(ctx, SessionEventCompleted, eventID, p.CheckoutSessionID, p.StoreID, p.MerchantID, "", "", ev.OccurredAt(), p.Total.AmountMinor, p.Total.Currency)
}

// discoveryRouteTemplates are the serving-plane routes whose telemetry
// represents pre-checkout discovery traffic (R2's fallback metric) — the
// catalog read paths and the profile edge, never a checkout-sessions
// route (those requests already fold into Session via the lifecycle
// events above; double-counting them into DiscoveryRequests too would
// misrepresent the funnel).
var discoveryRouteTemplates = map[string]bool{
	"/.well-known/ucp": true,
	"/catalog/search":  true,
	"/catalog/:itemId": true,
}

type telemetryPayload struct {
	EventID       string    `json:"event_id"`
	StoreID       string    `json:"store_id"`
	MerchantID    string    `json:"merchant_id"`
	Surface       string    `json:"surface"`
	RouteTemplate string    `json:"route_template"`
	Synthetic     bool      `json:"synthetic"`
	OccurredAt    time.Time `json:"occurred_at"`
}

func (c *Consumer) handleTelemetry(ctx context.Context, ev eventbus.DomainEvent) error {
	var p telemetryPayload
	if err := json.Unmarshal(ev.Payload(), &p); err != nil {
		c.log.Error("channel: unmarshal telemetry_recorded failed", "error", err)
		return nil
	}
	if p.Synthetic || !discoveryRouteTemplates[p.RouteTemplate] {
		return nil
	}
	if err := c.store.RecordDiscoveryRequest(ctx, p.EventID, p.MerchantID, p.StoreID, Surface(p.Surface), p.OccurredAt); err != nil {
		c.log.Error("channel: record discovery request failed", "error", err, "event_id", p.EventID)
		return err
	}
	return nil
}
