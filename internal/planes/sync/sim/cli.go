// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package sim

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/secretmanagement"
	"github.com/sarvexlab/sarvex/internal/planes/sync/connector"
	"github.com/sarvexlab/sarvex/internal/planes/sync/domain"
)

// CLI implements the runtime `sim seed/mutate/fix/status` commands: it
// mutates the persistent Store directly (never touching any app-store data)
// and, for anything a real merchant change would signal, sends the same
// HMAC-signed connector signal the real plugin would — driving the real
// ingestion pipeline (internal/planes/sync/ingestion.go) exactly as a real
// catalog change would. It never calls a control-plane console/API endpoint
// itself: seeding and mutating are store-side actions; importing, resolving
// tasks, and everything else stays a human action in the console.
type CLI struct {
	stores        StoreRepository
	connectorRepo connector.ConnectorRepository
	secretMgmt    secretmanagement.SecretManagementService
	signer        *Signer
	log           *slog.Logger
}

// NewCLI constructs a CLI. connectorRepo and secretMgmt are used only to
// recover the store's connector_id + plaintext pairing code (the HMAC
// signing secret) so mutations can be signed — mirroring
// ConnectorService.DevListConnectors, without needing the full service
// (event bus, profile source) a one-shot CLI process has no use for.
func NewCLI(stores StoreRepository, connectorRepo connector.ConnectorRepository, secretMgmt secretmanagement.SecretManagementService, signer *Signer, log *slog.Logger) *CLI {
	if log == nil {
		log = slog.Default()
	}
	return &CLI{stores: stores, connectorRepo: connectorRepo, secretMgmt: secretMgmt, signer: signer, log: log}
}

// connectorFor resolves storeID's connector_id and plaintext pairing code
// (the HMAC signing secret). Returns a clear error if the store hasn't been
// paired yet — mutate/fix/seed never invent a connector, pairing is a real
// console action (console/sim auto-completes it once begun).
func (c *CLI) connectorFor(ctx context.Context, storeID string) (connectorID, pairingCode string, err error) {
	return resolveConnector(ctx, c.connectorRepo, c.secretMgmt, storeID)
}

// signChange sends an HMAC-signed connector signal for sku, exactly as the
// real plugin's SignalSender would on a product/price/inventory hook firing.
func (c *CLI) signChange(ctx context.Context, storeID, sku string, entityType domain.EntityType, change domain.ChangeKind) error {
	connectorID, pairingCode, err := c.connectorFor(ctx, storeID)
	if err != nil {
		return err
	}

	sequence, err := c.signer.NextSequence(ctx, connectorID)
	if err != nil {
		return fmt.Errorf("allocate signal sequence: %w", err)
	}

	sig := domain.ConnectorSignal{
		EntityType: entityType,
		EntityID:   sku,
		OccurredAt: time.Now().UTC().Format(time.RFC3339),
		Sequence:   sequence,
		Change:     change,
	}

	if err := c.signer.Send(ctx, "POST", "/connector/signals", connectorID, storeID, pairingCode, sequence, sig); err != nil {
		return fmt.Errorf("send signal: %w", err)
	}
	return nil
}

// loadOrCreateStore returns storeID's persistent Store, creating an empty
// one if none exists yet.
func (c *CLI) loadOrCreateStore(ctx context.Context, storeID string) (*Store, error) {
	store, err := c.stores.Get(ctx, storeID)
	if err == ErrStoreNotFound {
		return NewStore(storeID), nil
	}
	if err != nil {
		return nil, err
	}
	return store, nil
}

// Seed creates or extends storeID's persistent catalog with n products
// (messy injects the readiness-issue mix described in Store.Seed), so the
// real connector genuinely has something to pull once import is triggered
// from the console. Seeding does not signal or trigger an import itself —
// per the task's division of roles, "trigger import" is a console action.
func (c *CLI) Seed(ctx context.Context, storeID string, n int, messy bool) (*Store, error) {
	store, err := c.loadOrCreateStore(ctx, storeID)
	if err != nil {
		return nil, err
	}
	store.Seed(n, messy)
	if err := c.stores.Upsert(ctx, store); err != nil {
		return nil, err
	}
	c.log.Info("sim: seeded store", "store_id", storeID, "products", n, "messy", messy)
	return store, nil
}

// SetLocalPickup turns the simulated merchant's Local Pickup shipping method on
// or off. The next pickup probe (sync-plane sweep or first import) then adds or
// removes the pickup extension from the store's profile.
func (c *CLI) SetLocalPickup(ctx context.Context, storeID string, enabled bool) error {
	store, err := c.loadOrCreateStore(ctx, storeID)
	if err != nil {
		return err
	}
	store.LocalPickup = enabled
	if err := c.stores.Upsert(ctx, store); err != nil {
		return err
	}
	c.log.Info("sim: set local pickup", "store_id", storeID, "enabled", enabled)
	return nil
}

// MutatePrice sets sku's price and signals a price change, matching the
// real plugin's price-change hook (priority-lane per architecture §4.4).
func (c *CLI) MutatePrice(ctx context.Context, storeID, sku string, priceMinor int64) (*Product, error) {
	store, err := c.stores.Get(ctx, storeID)
	if err != nil {
		return nil, err
	}
	p, err := store.SetPrice(sku, priceMinor)
	if err != nil {
		return nil, err
	}
	if err := c.stores.Upsert(ctx, store); err != nil {
		return nil, err
	}
	if err := c.signChange(ctx, storeID, sku, domain.EntityPrice, domain.ChangeUpdated); err != nil {
		return nil, fmt.Errorf("mutated store but failed to signal: %w", err)
	}
	return p, nil
}

// MutateStock sets sku's stock quantity and signals an inventory change.
func (c *CLI) MutateStock(ctx context.Context, storeID, sku string, quantity int) (*Product, error) {
	store, err := c.stores.Get(ctx, storeID)
	if err != nil {
		return nil, err
	}
	p, err := store.SetStock(sku, quantity)
	if err != nil {
		return nil, err
	}
	if err := c.stores.Upsert(ctx, store); err != nil {
		return nil, err
	}
	if err := c.signChange(ctx, storeID, sku, domain.EntityInventory, domain.ChangeUpdated); err != nil {
		return nil, fmt.Errorf("mutated store but failed to signal: %w", err)
	}
	return p, nil
}

// MutateAttr breaks sku's field to a rubric-visible bad value and signals a
// product change.
func (c *CLI) MutateAttr(ctx context.Context, storeID, sku, field string) (*Product, error) {
	store, err := c.stores.Get(ctx, storeID)
	if err != nil {
		return nil, err
	}
	p, err := store.BreakAttr(sku, field)
	if err != nil {
		return nil, err
	}
	if err := c.stores.Upsert(ctx, store); err != nil {
		return nil, err
	}
	if err := c.signChange(ctx, storeID, sku, domain.EntityProduct, domain.ChangeUpdated); err != nil {
		return nil, fmt.Errorf("mutated store but failed to signal: %w", err)
	}
	return p, nil
}

// Fix sets sku's field back to a good value and signals a product change,
// so the corresponding task clears through the real scoring path.
func (c *CLI) Fix(ctx context.Context, storeID, sku, field string) (*Product, error) {
	store, err := c.stores.Get(ctx, storeID)
	if err != nil {
		return nil, err
	}
	p, err := store.Fix(sku, field)
	if err != nil {
		return nil, err
	}
	if err := c.stores.Upsert(ctx, store); err != nil {
		return nil, err
	}

	entityType := domain.EntityProduct
	switch field {
	case "price":
		entityType = domain.EntityPrice
	case "stock":
		entityType = domain.EntityInventory
	}
	if err := c.signChange(ctx, storeID, sku, entityType, domain.ChangeUpdated); err != nil {
		return nil, fmt.Errorf("mutated store but failed to signal: %w", err)
	}
	return p, nil
}

// StatusReport is what `sim status` prints: the store's current holdings
// plus its pairing/signal state.
type StatusReport struct {
	StoreID       string     `json:"store_id"`
	ProductCount  int        `json:"product_count"`
	Products      []*Product `json:"products"`
	Paired        bool       `json:"paired"`
	ConnectorID   string     `json:"connector_id,omitempty"`
	LastMutatedAt time.Time  `json:"last_mutated_at"`
}

// Status reports what the fake store currently holds and its pairing state.
func (c *CLI) Status(ctx context.Context, storeID string) (*StatusReport, error) {
	store, err := c.stores.Get(ctx, storeID)
	if err != nil {
		return nil, err
	}

	report := &StatusReport{
		StoreID:       storeID,
		ProductCount:  len(store.Products),
		LastMutatedAt: store.UpdatedAt,
	}
	for _, p := range store.Products {
		report.Products = append(report.Products, p)
	}

	connectorID, _, err := c.connectorFor(ctx, storeID)
	if err == nil {
		report.Paired = true
		report.ConnectorID = connectorID
	}

	return report, nil
}

// StoreSummary is one row of `sim status` with no --store given: every
// simulated store, briefly.
type StoreSummary struct {
	StoreID      string    `json:"store_id"`
	ProductCount int       `json:"product_count"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// StatusAll reports every simulated store's brief status — used when `sim
// status` is run without --store, since multiple stores may run concurrently.
func (c *CLI) StatusAll(ctx context.Context) ([]StoreSummary, error) {
	stores, err := c.stores.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]StoreSummary, 0, len(stores))
	for _, s := range stores {
		out = append(out, StoreSummary{StoreID: s.StoreID, ProductCount: len(s.Products), UpdatedAt: s.UpdatedAt})
	}
	return out, nil
}
