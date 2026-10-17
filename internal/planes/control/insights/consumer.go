// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package insights

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/domain"
	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
)

// Consumer subscribes to serving.catalog.query_recorded
// (ucp.EventCatalogQueryRecorded) and ucp.negotiation.succeeded/failed
// (domain.EventTypeUCPNegotiationSucceeded/Failed) and folds each into the
// aggregates Store maintains. Runs regardless of billing tier (R16) — the
// entitlement gate is on the read API only, never here.
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

// Start subscribes to every source topic.
func (c *Consumer) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	handlers := map[string]eventbus.EventHandler{
		ucp.EventCatalogQueryRecorded:           c.handleCatalogQuery,
		domain.EventTypeUCPNegotiationSucceeded: c.handleNegotiationSucceeded,
		domain.EventTypeUCPNegotiationFailed:    c.handleNegotiationFailed,
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
			c.log.Error("insights: unsubscribe failed", "topic", sub.Topic(), "error", err)
		}
	}
	c.subs = nil
	return nil
}

// catalogQueryPayload mirrors serving.CatalogQueryRecordedEvent's JSON
// shape exactly (internal/planes/serving/catalog_query_events.go) —
// deliberately no query-string field exists on the wire to unmarshal
// (R12: nothing was ever emitted there to receive).
type catalogQueryPayload struct {
	EventID             string    `json:"event_id"`
	StoreID             string    `json:"store_id"`
	MerchantID          string    `json:"merchant_id"`
	Surface             string    `json:"surface"`
	AgentPlatform       *string   `json:"agent_platform"`
	RequestedProductIDs []string  `json:"requested_product_ids"`
	OccurredAt          time.Time `json:"occurred_at"`
}

func (c *Consumer) handleCatalogQuery(ctx context.Context, ev eventbus.DomainEvent) error {
	var p catalogQueryPayload
	if err := json.Unmarshal(ev.Payload(), &p); err != nil {
		c.log.Error("insights: unmarshal catalog_query_recorded failed", "error", err)
		return nil
	}
	if p.EventID == "" || p.StoreID == "" || p.MerchantID == "" {
		c.log.Error("insights: catalog_query_recorded missing required identifiers", "event_id", p.EventID)
		return nil
	}

	agentPlatform := ""
	if p.AgentPlatform != nil {
		agentPlatform, _ = sanitizeIdentifier(*p.AgentPlatform)
	}

	err := c.store.FoldCatalogQuery(ctx, CatalogQuery{
		EventID:           p.EventID,
		MerchantID:        p.MerchantID,
		StoreID:           p.StoreID,
		ProductIDs:        sanitizeIdentifiers(p.RequestedProductIDs),
		AgentPlatformHost: agentPlatform,
		OccurredAt:        p.OccurredAt,
	})
	if err != nil {
		c.log.Error("insights: fold catalog query failed", "error", err, "event_id", p.EventID)
		return err // NACK — genuine store failure, worth at-least-once retry.
	}
	return nil
}

// negotiationPayload covers the union of NegotiationSucceededEvent and
// NegotiationFailedEvent's JSON shapes (internal/pkg/ucp/negotiation/events.go's
// CapabilitySets.asPayload() plus each event's own extra fields). Reason
// is absent (empty) on a succeeded event. store_id/merchant_id are read
// via the eventbus.DomainEvent interface instead (see the handlers below)
// since neither event's payload carries merchant_id, and they disagree on
// the store field's wire name (instance_id vs store_id).
type negotiationPayload struct {
	PlatformProfileURI       string   `json:"platform_profile_uri"`
	Reason                   string   `json:"reason"`
	RequestedCapabilities    []string `json:"requested_capabilities"`
	DeclaredCapabilities     []string `json:"declared_capabilities"`
	RequestedPaymentHandlers []string `json:"requested_payment_handlers"`
	DeclaredPaymentHandlers  []string `json:"declared_payment_handlers"`
}

// negotiationDedupKey builds the composite idempotency key negotiation
// events must use since they carry no event_id on the wire (see this
// package's doc comment and store.go's NegotiationOutcome.DedupKey).
// occurred_at is preserved identically across a genuine at-least-once
// redelivery of the same message (it is a field of the original event,
// not regenerated), so this key correctly dedupes redelivery while a
// distinct negotiation for the same store/platform essentially never
// collides at nanosecond precision.
func negotiationDedupKey(storeID, platformProfileURI string, occurredAt time.Time) string {
	return storeID + "|" + platformProfileURI + "|" + occurredAt.Format(time.RFC3339Nano)
}

func (c *Consumer) foldNegotiation(ctx context.Context, merchantID, storeID string, succeeded bool, reason string, occurredAt time.Time, p negotiationPayload) error {
	if storeID == "" || merchantID == "" {
		c.log.Error("insights: negotiation event missing required identifiers", "store_id", storeID, "merchant_id", merchantID)
		return nil
	}

	out := NegotiationOutcome{
		DedupKey:                 negotiationDedupKey(storeID, p.PlatformProfileURI, occurredAt),
		MerchantID:               merchantID,
		StoreID:                  storeID,
		PlatformProfileURI:       p.PlatformProfileURI,
		OccurredAt:               occurredAt,
		Succeeded:                succeeded,
		FailureReason:            NegotiationFailureReason(reason),
		RequestedCapabilities:    sanitizeIdentifiers(p.RequestedCapabilities),
		DeclaredCapabilities:     sanitizeIdentifiers(p.DeclaredCapabilities),
		RequestedPaymentHandlers: sanitizeIdentifiers(p.RequestedPaymentHandlers),
		DeclaredPaymentHandlers:  sanitizeIdentifiers(p.DeclaredPaymentHandlers),
	}
	if err := c.store.FoldNegotiationOutcome(ctx, out); err != nil {
		c.log.Error("insights: fold negotiation outcome failed", "error", err, "store_id", storeID)
		return err // NACK — genuine store failure, worth at-least-once retry.
	}
	return nil
}

func (c *Consumer) handleNegotiationSucceeded(ctx context.Context, ev eventbus.DomainEvent) error {
	var p negotiationPayload
	if err := json.Unmarshal(ev.Payload(), &p); err != nil {
		c.log.Error("insights: unmarshal negotiation.succeeded failed", "error", err)
		return nil
	}
	// ev.StoreID()/ev.MerchantID() (eventbus.DomainEvent) rather than a
	// JSON field: neither event adds merchant_id to its payload (see
	// negotiation/events.go's asPayload()), and NegotiationSucceededEvent
	// names its store field "instance_id" on the wire while
	// NegotiationFailedEvent names it "store_id" — the interface methods
	// sidestep both wire-shape differences.
	return c.foldNegotiation(ctx, ev.MerchantID(), ev.StoreID(), true, "", ev.OccurredAt(), p)
}

func (c *Consumer) handleNegotiationFailed(ctx context.Context, ev eventbus.DomainEvent) error {
	var p negotiationPayload
	if err := json.Unmarshal(ev.Payload(), &p); err != nil {
		c.log.Error("insights: unmarshal negotiation.failed failed", "error", err)
		return nil
	}
	return c.foldNegotiation(ctx, ev.MerchantID(), ev.StoreID(), false, p.Reason, ev.OccurredAt(), p)
}
