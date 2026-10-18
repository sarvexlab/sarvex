// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package serving

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
)

// Service defines the lifecycle contract for the Serving Plane background workers.
type Service interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

type servingService struct {
	cacheRebuilder *CacheRebuilder
	cacheWriter    CacheWriter
	catalogSource  CatalogSource
	bus            eventbus.EventBus
	log            *slog.Logger

	mu           sync.Mutex
	subs         []eventbus.Subscription
	workerCtx    context.Context
	workerCancel context.CancelFunc
	workerWG     sync.WaitGroup
}

// NewServingService creates a new instance of the Serving Plane background service.
func NewServingService(
	cacheRebuilder *CacheRebuilder,
	cacheWriter CacheWriter,
	catalogSource CatalogSource,
	bus eventbus.EventBus,
	log *slog.Logger,
) Service {
	return &servingService{
		cacheRebuilder: cacheRebuilder,
		cacheWriter:    cacheWriter,
		catalogSource:  catalogSource,
		bus:            bus,
		log:            log,
	}
}

// Start registers all event subscriptions and launches background goroutines.
func (s *servingService) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.log.Info("starting serving plane service workers")

	s.workerCtx, s.workerCancel = context.WithCancel(context.Background())

	// Register subscriptions
	if err := s.registerSubscriptions(); err != nil {
		s.workerCancel()
		return fmt.Errorf("register serving subscriptions: %w", err)
	}

	s.log.Info("serving plane service workers started successfully")
	return nil
}

// Stop stops the background workers and unsubscribes from all events.
func (s *servingService) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.log.Info("stopping serving plane service workers")

	// 1. Cancel background workers context
	if s.workerCancel != nil {
		s.workerCancel()
	}

	// 2. Unsubscribe all event streams
	for _, sub := range s.subs {
		s.log.Debug("unsubscribing topic", "topic", sub.Topic())
		if err := s.bus.Unsubscribe(ctx, sub); err != nil {
			s.log.Error("failed to unsubscribe from topic", "topic", sub.Topic(), "error", err)
		}
	}
	s.subs = nil

	// 3. Wait for background goroutines to drain
	done := make(chan struct{})
	go func() {
		s.workerWG.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.log.Info("serving plane service workers stopped cleanly")
		return nil
	case <-ctx.Done():
		s.log.Warn("serving plane service shutdown timed out")
		return ctx.Err()
	}
}

func (s *servingService) registerSubscriptions() error {
	subscriptions := []struct {
		topic   string
		handler eventbus.EventHandler
	}{
		// Sync plane signals product readiness
		{topic: ucp.EventCatalogProductsReady, handler: s.handleProductsReady},
		// Every catalog change asks for a rebuild before one happens. That is
		// the only signal telling the serving plane its snapshot is behind —
		// it cannot read the catalog to work this out (the read path serves
		// only the cache, sarvex-v1-architecture.md A10.2), and the sync plane's own
		// subscription to this topic is a different consumer group, so both
		// receive it.
		{topic: ucp.EventRebuildRequested, handler: s.handleRebuildRequested},
	}

	ctx := context.Background()
	for _, subDef := range subscriptions {
		sub, err := s.bus.Subscribe(ctx, subDef.topic, subDef.handler)
		if err != nil {
			return fmt.Errorf("subscribe %s: %w", subDef.topic, err)
		}
		s.subs = append(s.subs, sub)
	}

	return nil
}

// handleProductsReady processes catalog.products_ready signals from sync plane.
// handleRebuildRequested marks the store's snapshot as behind its source.
//
// The marker is what makes freshness mean "a change was signalled and we have
// not rebuilt since" rather than "the newest product is old" — see
// CacheWatermark. The matching rebuild normally lands moments later on
// catalog.products_ready and clears it; a marker that stays put past the
// freshness window is precisely the case worth reporting.
func (s *servingService) handleRebuildRequested(ctx context.Context, event eventbus.DomainEvent) error {
	storeID := event.StoreID()
	if storeID == "" {
		return nil
	}
	if err := s.cacheWriter.MarkPendingChange(ctx, storeID); err != nil {
		// Best-effort: losing the marker understates staleness, which is not
		// worth failing a rebuild request over. The next change re-marks it.
		s.log.Warn("failed to mark pending catalog change", "error", err, "store_id", storeID)
	}
	return nil
}

// handleStoreConfigChanged rebuilds the store's snapshot after a control-plane
// configuration change. The error is returned, not swallowed: unlike a catalog
// rebuild there is no later sync cycle guaranteed to retry it.
func (s *servingService) handleStoreConfigChanged(ctx context.Context, event eventbus.DomainEvent) error {
	storeID := event.StoreID()
	if storeID == "" {
		return nil
	}
	if err := s.cacheRebuilder.RebuildSnapshot(ctx, storeID); err != nil {
		s.log.Error("failed to rebuild snapshot after configuration change", "error", err, "store_id", storeID, "event", event.EventType())
		return err
	}
	s.log.Info("cache rebuilt after configuration change", "store_id", storeID, "event", event.EventType())
	return nil
}

// Orchestrates full cache rebuild: get store profile, render UCP profile, assemble snapshot.
func (s *servingService) handleProductsReady(ctx context.Context, event eventbus.DomainEvent) error {
	merchantID := event.MerchantID()
	s.log.Info("products ready for cache rebuild", "merchant_id", merchantID)

	// Parse event payload
	var payload map[string]any
	if err := json.Unmarshal(event.Payload(), &payload); err != nil {
		s.log.Error("failed to unmarshal products_ready", "error", err, "merchant_id", merchantID)
		return fmt.Errorf("unmarshal payload: %w", err)
	}

	productCount, _ := payload["product_count"].(float64)
	storeID, _ := payload["store_id"].(string) // sync plane includes store_id in event

	s.log.Info("sync plane built product snapshot",
		"merchant_id", merchantID,
		"store_id", storeID,
		"product_count", int(productCount))

	// If storeID is available, trigger cache rebuild via CacheRebuilder.
	// CacheRebuilder orchestrates:
	// 1. Get store profile from CatalogSource
	// 2. Call ProfileRenderer to build UCP profile
	// 3. Assemble final CacheSnapshot (products + profile + shipping)
	// 4. Swap into cache
	if storeID != "" {
		if err := s.cacheRebuilder.RebuildSnapshot(ctx, storeID); err != nil {
			s.log.Error("failed to rebuild snapshot", "error", err, "store_id", storeID, "merchant_id", merchantID)
			// Non-fatal: log and continue; next sync cycle will retry
			return nil
		}
		s.log.Info("cache rebuild completed", "store_id", storeID, "merchant_id", merchantID)
	} else {
		// TODO: Handle case where storeID is not in event
		// Option 1: Iterate through all stores for merchant and rebuild each
		// Option 2: Wait for event to include storeID
		// Option 3: Use a default store_id for single-store merchants (V1 assumption)
		s.log.Warn("store_id not in event, cache rebuild skipped",
			"merchant_id", merchantID,
			"product_count", int(productCount))
	}

	return nil
}
