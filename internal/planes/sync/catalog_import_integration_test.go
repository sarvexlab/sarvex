// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp"
)

// TestCatalogImportHandler_Integration_MultiPageWooCommerceFillsCatalogStore
// drives control.catalog.import end to end against a real MongoDB-backed
// CatalogStore and ImportJobRepository, with a fake multi-page WooCommerce
// connector standing in for the live store-API pull client. Skips (rather
// than fails) when MONGO_URI is unset, following the same convention as
// catalog_mongo_test.go.
func TestCatalogImportHandler_Integration_MultiPageWooCommerceFillsCatalogStore(t *testing.T) {
	db := connectTestMongo(t)

	catalogStore, err := NewMongoDBCatalogStore(db)
	if err != nil {
		t.Fatalf("new catalog store: %v", err)
	}
	jobRepo, err := NewMongoImportJobRepository(db)
	if err != nil {
		t.Fatalf("new import job repo: %v", err)
	}

	const totalProducts = 137 // several WooCommerce pages at the default page size
	connector := &fakeStoreConnector{entities: testEntities(totalProducts)}
	eventBus := newFakeEventBus()
	reg, provider := newTestRegistry("store-int-1", connector)
	scorer := newFakeProductRescorer()
	enqueuer := &fakeImportEnqueuer{}
	handler := NewCatalogImportHandler(jobRepo, catalogStore, reg, provider, scorer, eventBus, testImportLogger(), 50)
	handler.SetEnqueuer(enqueuer)

	ctx := context.Background()
	payload, _ := json.Marshal(CatalogImportCommandPayload{
		StoreID:        "store-int-1",
		Reason:         "initial",
		IdempotencyKey: "int-key-1",
	})
	event := newDomainEvent("control.catalog.import", "store-int-1", "merch-int-1", payload)

	if err := handler.HandleCommand(ctx, event); err != nil {
		t.Fatalf("HandleCommand failed: %v", err)
	}
	// The actual import is enqueued by asynq in production; in tests drive it
	// synchronously via RunImport (the same function the asynq handler calls).
	if err := handler.RunImport(ctx, "store-int-1"); err != nil {
		t.Fatalf("RunImport failed: %v", err)
	}

	products, _, err := catalogStore.ListByStore(ctx, "store-int-1", totalProducts+10, "")
	if err != nil {
		t.Fatalf("list products: %v", err)
	}
	if len(products) != totalProducts {
		t.Fatalf("expected %d products in the catalog store, got %d", totalProducts, len(products))
	}
	seen := make(map[string]bool, totalProducts)
	for _, p := range products {
		if p.StoreID != "store-int-1" {
			t.Fatalf("store-int-1 import wrote a product scoped to store %q", p.StoreID)
		}
		seen[p.SKU] = true
	}
	if len(seen) != totalProducts {
		t.Fatalf("expected %d distinct SKUs, got %d (duplicates?)", totalProducts, len(seen))
	}

	job, err := jobRepo.Get(ctx, "store-int-1")
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != ImportStatusCompleted {
		t.Fatalf("expected job completed, got %s", job.Status)
	}
	if job.Counts.Upserted != totalProducts {
		t.Fatalf("expected job counts.upserted=%d, got %d", totalProducts, job.Counts.Upserted)
	}

	var completions int
	for _, e := range eventBus.published {
		if e.EventType() == ucp.EventJourneySignal {
			completions++
			var sig JourneySignalPayload
			if err := json.Unmarshal(e.Payload(), &sig); err != nil {
				t.Fatalf("decode journey signal: %v", err)
			}
			if sig.Signal != "first_sync_complete" {
				t.Errorf("expected first_sync_complete, got %q", sig.Signal)
			}
		}
	}
	if completions != 1 {
		t.Fatalf("expected exactly 1 completion event, got %d", completions)
	}

	// Re-sending the same command (same idempotency key) must be a no-op:
	// HandleCommand sees the existing completed job and does not re-enqueue.
	prevEnqueueCalls := len(enqueuer.calls)
	if err := handler.HandleCommand(ctx, event); err != nil {
		t.Fatalf("duplicate HandleCommand failed: %v", err)
	}
	if len(enqueuer.calls) != prevEnqueueCalls {
		t.Fatalf("expected no additional EnqueueImport calls on resend, got %d total", len(enqueuer.calls))
	}
	// FetchCatalog is only ever called by RunImport, which we drove once.
	if connector.fetchCatalogCalls != 1 {
		t.Fatalf("expected exactly 1 FetchCatalog call across both sends, got %d", connector.fetchCatalogCalls)
	}
	products, _, err = catalogStore.ListByStore(ctx, "store-int-1", totalProducts+10, "")
	if err != nil {
		t.Fatalf("list products after resend: %v", err)
	}
	if len(products) != totalProducts {
		t.Fatalf("expected %d products after resend (no duplicates), got %d", totalProducts, len(products))
	}

	// A store-A import never writes store-B data.
	otherProducts, _, err := catalogStore.ListByStore(ctx, "store-other", 10, "")
	if err != nil {
		t.Fatalf("list store-other: %v", err)
	}
	if len(otherProducts) != 0 {
		t.Fatalf("expected no products leaked into an unrelated store, got %d", len(otherProducts))
	}
}

// TestCatalogImportHandler_Integration_ResumeAcrossProcessRestart verifies
// that a fresh CatalogImportHandler instance (standing in for a restarted
// process) sharing the same MongoDB-backed repositories resumes an
// in-flight import from its persisted cursor.
func TestCatalogImportHandler_Integration_ResumeAcrossProcessRestart(t *testing.T) {
	db := connectTestMongo(t)

	catalogStore, err := NewMongoDBCatalogStore(db)
	if err != nil {
		t.Fatalf("new catalog store: %v", err)
	}
	jobRepo, err := NewMongoImportJobRepository(db)
	if err != nil {
		t.Fatalf("new import job repo: %v", err)
	}

	const totalProducts = 12
	connector := &fakeStoreConnector{entities: testEntities(totalProducts)}
	eventBus := newFakeEventBus()

	// Simulate "first process died mid-run": seed catalog + job state as they
	// would be after the first batch committed. The asynq worker on the next
	// instance will call RunImport with this persisted state and resume.
	ctx := context.Background()
	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		e := testEntities(totalProducts)[i]
		product := &CatalogProduct{StoreID: "store-int-2", MerchantID: "merch-int-2", SKU: e.SKU, Title: e.Name}
		if err := catalogStore.UpsertProduct(ctx, product); err != nil {
			t.Fatalf("seed product: %v", err)
		}
	}
	if err := jobRepo.Upsert(ctx, &ImportJob{
		StoreID: "store-int-2", MerchantID: "merch-int-2", Reason: "initial", IdempotencyKey: "int-key-2",
		Status: ImportStatusRunning, Cursor: "SKU-004", Counts: ImportCounts{Fetched: totalProducts, Upserted: 5},
		StartedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed job: %v", err)
	}

	// "Second process": a new handler instance over the same durable state.
	// In production the asynq worker re-enqueues the task and calls RunImport;
	// here we call it directly.
	reg, provider := newTestRegistry("store-int-2", connector)
	scorer := newFakeProductRescorer()
	resumedHandler := NewCatalogImportHandler(jobRepo, catalogStore, reg, provider, scorer, eventBus, testImportLogger(), 4)
	if err := resumedHandler.RunImport(ctx, "store-int-2"); err != nil {
		t.Fatalf("resume run failed: %v", err)
	}

	products, _, err := catalogStore.ListByStore(ctx, "store-int-2", totalProducts+10, "")
	if err != nil {
		t.Fatalf("list products: %v", err)
	}
	if len(products) != totalProducts {
		t.Fatalf("expected %d products after resume (no gaps), got %d", totalProducts, len(products))
	}

	job, err := jobRepo.Get(ctx, "store-int-2")
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != ImportStatusCompleted {
		t.Fatalf("expected job completed after resume, got %s", job.Status)
	}

	var completions int
	for _, e := range eventBus.published {
		if e.EventType() == ucp.EventJourneySignal {
			completions++
		}
	}
	if completions != 1 {
		t.Fatalf("expected exactly 1 completion event after resume, got %d", completions)
	}
}
