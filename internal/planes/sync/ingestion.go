// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
)

// signalReceivedPayload is the wire shape of connector.signal_received,
// published by the control-plane connector gateway (internal/planes/control/connector)
// after HMAC verification. It is a thin signal only: identity, not data.
type signalReceivedPayload struct {
	ConnectorID string `json:"connector_id"`
	StoreID     string `json:"store_id"`
	SKU         string `json:"sku"`
	EntityKind  string `json:"entity_kind"` // "product", "price", "inventory", "order_status"
	Sequence    int64  `json:"sequence"`
	Change      string `json:"change"` // "created", "updated", "deleted"
}

// Event payloads matching the sync.*.changed message schemas in
// sarvex-v1-syncing-plane-async.yaml.
type ProductChangedPayload struct {
	SKU    string `json:"sku"`
	Change string `json:"change"` // created, updated, deleted
}

type PriceChangedPayload struct {
	SKU      string       `json:"sku"`
	NewPrice moneyPayload `json:"new_price"`
}

type InventoryChangedPayload struct {
	SKU      string `json:"sku"`
	InStock  bool   `json:"in_stock"`
	Quantity *int   `json:"quantity"`
}

type moneyPayload struct {
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
}

// mappingPipeline is the sync-internal seam ingestion hands a normalised
// change to for mapping + scoring. Implemented by syncService. This is a
// same-process function call, not a bus topic: ingestion and mapping/scoring
// both run inside the single sync-plane binary, so there is no service
// boundary here for sync.product.changed/sync.price.changed/sync.inventory.changed
// to cross (see sarvex-v1-syncing-plane-async.yaml's own framing of these as
// intra-plane pipeline stages, and the removal of their channel definitions
// from that file alongside this change).
type mappingPipeline interface {
	OnProductChanged(ctx context.Context, storeID, merchantID string, payload ProductChangedPayload) error
	OnPriceChanged(ctx context.Context, storeID, merchantID string, payload PriceChangedPayload) error
	OnInventoryChanged(ctx context.Context, storeID, merchantID string, payload InventoryChangedPayload) error
	// OnProductDeleted cleans up after a product the store no longer has. It
	// takes the product as it was before the delete: its tombstoned record
	// must not be re-read and re-scored (an upsert clears the tombstone), so
	// the issues to retire travel with the call.
	OnProductDeleted(ctx context.Context, storeID, merchantID string, p *CatalogProduct) error
}

// PullError wraps a failure to fetch authoritative data from the merchant's
// store via the Connector. It is always retryable: the caller (the event
// bus) redelivers the signal with backoff rather than dropping it, so a
// transient store outage never loses an ingest signal.
type PullError struct {
	EntityKind string
	Err        error
}

func (e *PullError) Error() string {
	return fmt.Sprintf("ingestion: pull %s failed: %v", e.EntityKind, e.Err)
}

func (e *PullError) Unwrap() error { return e.Err }

// storeGate serialises signal processing per store_id (so a store's signals
// are handled strictly in arrival order, and its in-flight fetch/write never
// races a later signal for the same store) while leaving different stores
// fully concurrent. It also tracks, per store, the message_ids seen (for
// de-dup) and the last admitted sequence (to drop stale/out-of-order
// signals) — both scoped by store_id per architecture §4.5 invariant 4.
// De-dup state is persisted in Redis to survive process restarts per ADR-4.
type storeGate struct {
	mu     sync.Mutex
	locks  map[string]*sync.Mutex
	seen   map[string]map[string]struct{} // store_id -> message_id -> seen (in-process fallback)
	lastSq map[string]int64               // store_id -> last admitted sequence (in-process fallback)
	redis  redis.Cmdable                  // Redis client for persistence (optional)
	log    *slog.Logger
	ttl    time.Duration
}

const (
	deDupKeyPrefix = "dedup:signal"
	deDupTTL       = 24 * time.Hour // 24-hour window for signal de-dup
)

func newStoreGate(client redis.Cmdable, log *slog.Logger) *storeGate {
	return &storeGate{
		locks:  make(map[string]*sync.Mutex),
		seen:   make(map[string]map[string]struct{}),
		lastSq: make(map[string]int64),
		redis:  client,
		log:    log,
		ttl:    deDupTTL,
	}
}

// lockFor returns the mutex for a store, creating it if necessary.
func (g *storeGate) lockFor(storeID string) *sync.Mutex {
	g.mu.Lock()
	defer g.mu.Unlock()
	l, ok := g.locks[storeID]
	if !ok {
		l = &sync.Mutex{}
		g.locks[storeID] = l
	}
	return l
}

// admit reports whether a signal should be processed: false when messageID
// was already seen for this store (duplicate/replay), or when sequence is
// not strictly greater than the last admitted sequence for this store
// (stale or out-of-order delivery). Callers must hold the store's lock (via
// lockFor) around admit so the check-then-record is race-free per store.
// De-dup state is backed by Redis; falls back to in-process storage if
// Redis is unavailable.
func (g *storeGate) admit(ctx context.Context, storeID, messageID string, sequence int64) (admitted bool, reason string) {
	// Try Redis-backed check first (for persistence across restarts)
	if g.redis != nil {
		admitted, reason := g.admitWithRedis(ctx, storeID, messageID, sequence)
		if reason != "redis_unavailable" {
			return admitted, reason
		}
		// Fall through to in-process on Redis unavailability
		g.log.Warn("falling back to in-process de-dup due to Redis unavailability", "store_id", storeID)
	}

	// In-process fallback
	g.mu.Lock()
	defer g.mu.Unlock()

	storeSeen, ok := g.seen[storeID]
	if !ok {
		storeSeen = make(map[string]struct{})
		g.seen[storeID] = storeSeen
	}
	if _, dup := storeSeen[messageID]; dup {
		return false, "duplicate message_id"
	}
	if last, ok := g.lastSq[storeID]; ok && sequence <= last {
		return false, "stale or out-of-order sequence"
	}

	storeSeen[messageID] = struct{}{}
	g.lastSq[storeID] = sequence
	return true, ""
}

// admitWithRedis checks de-dup state using Redis (for persistence).
func (g *storeGate) admitWithRedis(ctx context.Context, storeID, messageID string, sequence int64) (admitted bool, reason string) {
	// Check if messageID was already seen (duplicate)
	messageKey := fmt.Sprintf("%s:%s:%s", deDupKeyPrefix, storeID, messageID)
	exists, err := g.redis.Exists(ctx, messageKey).Result()
	if err != nil {
		return false, "redis_unavailable" // Fall back to in-process
	}
	if exists > 0 {
		return false, "duplicate message_id"
	}

	// Check sequence ordering
	seqKey := fmt.Sprintf("%s:%s:last_seq", deDupKeyPrefix, storeID)
	lastSeqStr, err := g.redis.Get(ctx, seqKey).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return false, "redis_unavailable" // Fall back to in-process
	}

	if !errors.Is(err, redis.Nil) {
		var lastSeq int64
		if _, err := fmt.Sscanf(lastSeqStr, "%d", &lastSeq); err == nil && sequence <= lastSeq {
			return false, "stale or out-of-order sequence"
		}
	}

	// Record this signal as seen and update last sequence
	pipe := g.redis.Pipeline()
	pipe.Set(ctx, messageKey, "1", g.ttl)
	pipe.Set(ctx, seqKey, fmt.Sprintf("%d", sequence), g.ttl)
	_, err = pipe.Exec(ctx)
	if err != nil {
		return false, "redis_unavailable" // Fall back to in-process
	}

	return true, ""
}

// IngestionPipeline turns verified plugin signals into catalog writes and
// normalised change events: de-dup/order per store_id -> fat pull -> upsert
// -> emit. It is the sync plane's single writer to the catalog store for
// steady-state (post-import) changes (architecture §4.5 invariant 3).
type IngestionPipeline struct {
	catalogStore CatalogStore
	registry     *base.Registry
	provider     ucp.StoreProviderByStoreIDFunc
	pipeline     mappingPipeline
	log          *slog.Logger
	gate         *storeGate
}

// NewIngestionPipeline creates the steady-state ingestion pipeline.
// registry/provider resolve the per-store base.Connector
// (registry.Get(provider(...))) used to pull authoritative data; its pull
// methods are already scoped per store_id and carry their own per-store
// retry/circuit breaker (see internal/pkg/ucp/resilience), so the pipeline
// itself does not need a retry loop — a pull failure surfaces as a
// *PullError, which connector.signal_received's own bus redelivery-with-backoff
// picks up (HandleSignalReceived is still the connector.signal_received
// subscription entrypoint; only the normalised-change handoff to mapping is
// now a direct call). Redis client backs de-dup state for persistence across
// restarts; nil is allowed (falls back to in-process de-dup).
func NewIngestionPipeline(catalogStore CatalogStore, registry *base.Registry, provider ucp.StoreProviderByStoreIDFunc, pipeline mappingPipeline, redisClient redis.Cmdable, log *slog.Logger) *IngestionPipeline {
	return &IngestionPipeline{
		catalogStore: catalogStore,
		registry:     registry,
		provider:     provider,
		pipeline:     pipeline,
		log:          log,
		gate:         newStoreGate(redisClient, log),
	}
}

// resolveConnector resolves the base.Connector for (merchantID, storeID) via
// the registry, the same pattern service.go's writeProductToStore uses for
// UpdateProduct.
func (p *IngestionPipeline) resolveConnector(merchantID, storeID string) (base.Connector, error) {
	instance, err := p.provider(merchantID, storeID)
	if err != nil {
		return nil, fmt.Errorf("get store provider: %w", err)
	}
	connector, err := p.registry.Get(instance)
	if err != nil {
		return nil, fmt.Errorf("get connector: %w", err)
	}
	return connector, nil
}

// HandleSignalReceived is the event-bus handler for connector.signal_received.
// It never trusts the signal's own data as authoritative — only entity
// identity and ordering — and always pulls the current entity from the
// connector before writing the catalog store.
func (p *IngestionPipeline) HandleSignalReceived(ctx context.Context, event eventbus.DomainEvent) error {
	var sig signalReceivedPayload
	if err := json.Unmarshal(event.Payload(), &sig); err != nil {
		p.log.Error("ingestion: failed to unmarshal signal payload", "error", err)
		return nil // malformed payload: drop, do not retry
	}

	storeID := sig.StoreID
	if storeID == "" {
		storeID = event.StoreID()
	}
	merchantID := event.MerchantID()

	// message_id is derived deterministically from the signal's own
	// identity (connector + entity + sequence): a redelivery of the exact
	// same signal always carries the exact same triple, so this is a valid
	// de-dup key without requiring a change to the gateway's wire contract.
	messageID := fmt.Sprintf("%s:%s:%d", sig.ConnectorID, sig.SKU, sig.Sequence)

	lock := p.gate.lockFor(storeID)
	lock.Lock()
	defer lock.Unlock()

	admitted, reason := p.gate.admit(ctx, storeID, messageID, sig.Sequence)
	if !admitted {
		p.log.Info("ingestion: signal dropped",
			"store_id", storeID, "sku", sig.SKU, "sequence", sig.Sequence, "reason", reason)
		return nil
	}

	if sig.EntityKind == "order_status" {
		// Order handoff / status-sync is a separate chunk; the ingestion
		// pipeline only de-dups and orders the signal here, it does not
		// have a Fetch* for orders to pull authoritative status from.
		p.log.Info("ingestion: order_status signal received, deferring to order handoff", "store_id", storeID, "sku", sig.SKU)
		return nil
	}

	if sig.Change == "deleted" {
		return p.handleDelete(ctx, storeID, merchantID, sig)
	}

	switch sig.EntityKind {
	case "price":
		return p.handlePriceChanged(ctx, storeID, merchantID, sig)
	case "inventory":
		return p.handleInventoryChanged(ctx, storeID, merchantID, sig)
	default: // "product"
		return p.handleProductChanged(ctx, storeID, merchantID, sig)
	}
}

// existingOrBlank loads the current catalog record for (storeID, sku) so a
// targeted pull (price/inventory only) merges onto it rather than wiping
// fields the pull didn't touch. Returns a fresh record seeded with identity
// fields when none exists yet.
func (p *IngestionPipeline) existingOrBlank(ctx context.Context, storeID, merchantID, sku string) (*CatalogProduct, error) {
	existing, err := p.catalogStore.GetProduct(ctx, storeID, sku)
	if err == nil {
		return existing, nil
	}
	if err != ErrProductNotFound {
		return nil, fmt.Errorf("ingestion: load existing product: %w", err)
	}
	return &CatalogProduct{
		StoreID:       storeID,
		MerchantID:    merchantID,
		SKU:           sku,
		RawAttributes: make(map[string]string),
		MappedFields:  make(map[string]string),
		Findings:      make([]*Finding, 0),
	}, nil
}

func (p *IngestionPipeline) handleProductChanged(ctx context.Context, storeID, merchantID string, sig signalReceivedPayload) error {
	connector, err := p.resolveConnector(merchantID, storeID)
	if err != nil {
		return &PullError{EntityKind: "product", Err: err}
	}
	entity, err := connector.FetchProduct(ctx, storeID, sig.SKU)
	if err != nil {
		return &PullError{EntityKind: "product", Err: err}
	}

	product, err := p.existingOrBlank(ctx, storeID, merchantID, sig.SKU)
	if err != nil {
		return err
	}
	product.applyEntity(*entity)
	product.UpdatedAt = time.Now().UTC()

	if err := p.catalogStore.UpsertProduct(ctx, product); err != nil {
		return fmt.Errorf("ingestion: upsert product: %w", err)
	}

	change := sig.Change
	if change == "" {
		change = "updated"
	}
	return p.pipeline.OnProductChanged(ctx, storeID, merchantID, ProductChangedPayload{SKU: sig.SKU, Change: change})
}

func (p *IngestionPipeline) handlePriceChanged(ctx context.Context, storeID, merchantID string, sig signalReceivedPayload) error {
	connector, err := p.resolveConnector(merchantID, storeID)
	if err != nil {
		return &PullError{EntityKind: "price", Err: err}
	}
	pricing, err := connector.FetchPricing(ctx, storeID, sig.SKU)
	if err != nil {
		return &PullError{EntityKind: "price", Err: err}
	}

	product, err := p.existingOrBlank(ctx, storeID, merchantID, sig.SKU)
	if err != nil {
		return err
	}
	product.PriceMinor = pricing.PriceMinor
	product.Currency = pricing.Currency
	product.UpdatedAt = time.Now().UTC()

	if err := p.catalogStore.UpsertProduct(ctx, product); err != nil {
		return fmt.Errorf("ingestion: upsert price: %w", err)
	}

	// Priority: price is compliance-critical, but the priority-lane guarantee
	// (never queued behind bulk catalog work) lives one level up — bulk
	// import runs via control.catalog.import/CatalogImportHandler, a
	// separate command from connector.signal_received/IngestionPipeline —
	// not in this now-direct handoff to mapping.
	return p.pipeline.OnPriceChanged(ctx, storeID, merchantID, PriceChangedPayload{
		SKU:      sig.SKU,
		NewPrice: moneyPayload{AmountMinor: pricing.PriceMinor, Currency: pricing.Currency},
	})
}

func (p *IngestionPipeline) handleInventoryChanged(ctx context.Context, storeID, merchantID string, sig signalReceivedPayload) error {
	connector, err := p.resolveConnector(merchantID, storeID)
	if err != nil {
		return &PullError{EntityKind: "inventory", Err: err}
	}
	inventory, err := connector.FetchInventory(ctx, storeID, sig.SKU)
	if err != nil {
		return &PullError{EntityKind: "inventory", Err: err}
	}

	product, err := p.existingOrBlank(ctx, storeID, merchantID, sig.SKU)
	if err != nil {
		return err
	}
	product.InStock = inventory.InStock
	product.Quantity = inventory.Quantity
	product.UpdatedAt = time.Now().UTC()

	if err := p.catalogStore.UpsertProduct(ctx, product); err != nil {
		return fmt.Errorf("ingestion: upsert inventory: %w", err)
	}

	// Priority: see handlePriceChanged.
	return p.pipeline.OnInventoryChanged(ctx, storeID, merchantID, InventoryChangedPayload{
		SKU:      sig.SKU,
		InStock:  inventory.InStock,
		Quantity: inventory.Quantity,
	})
}

func (p *IngestionPipeline) handleDelete(ctx context.Context, storeID, merchantID string, sig signalReceivedPayload) error {
	existing, err := p.catalogStore.GetProduct(ctx, storeID, sig.SKU)
	if errors.Is(err, ErrProductNotFound) {
		return nil // never mirrored, or already deleted: nothing to clean up
	}
	if err != nil {
		return fmt.Errorf("ingestion: load product to delete: %w", err)
	}

	if err := p.catalogStore.DeleteProduct(ctx, storeID, sig.SKU); err != nil && !errors.Is(err, ErrProductNotFound) {
		return fmt.Errorf("ingestion: delete product: %w", err)
	}

	return p.pipeline.OnProductDeleted(ctx, storeID, merchantID, existing)
}

// simpleDomainEvent is the package-wide eventbus.DomainEvent implementation
// used by every sync-plane component that publishes (ingestion, mapping,
// scoring, catalog import, rebuilder).
type simpleDomainEvent struct {
	topic      string
	storeID    string
	merchantID string
	occurredAt time.Time
	payload    []byte
}

func (e *simpleDomainEvent) EventType() string     { return e.topic }
func (e *simpleDomainEvent) StoreID() string       { return e.storeID }
func (e *simpleDomainEvent) MerchantID() string    { return e.merchantID }
func (e *simpleDomainEvent) OccurredAt() time.Time { return e.occurredAt }
func (e *simpleDomainEvent) Payload() []byte       { return e.payload }

func newDomainEvent(topic, storeID, merchantID string, payload []byte) eventbus.DomainEvent {
	return &simpleDomainEvent{
		topic:      topic,
		storeID:    storeID,
		merchantID: merchantID,
		occurredAt: time.Now().UTC(),
		payload:    payload,
	}
}
