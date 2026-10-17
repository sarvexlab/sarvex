// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
)

func newDeleteTestService(store *fakeCatalogStore, bus *fakeEventBus, readiness *fakeReadinessAggregateStore, rem RemediationStore) *syncService {
	return &syncService{
		bus:            bus,
		log:            handlerTestLogger(),
		catalogStore:   store,
		readinessStore: readiness,
		remediations:   rem,
	}
}

func taskRaisedEvents(bus *fakeEventBus) []TaskRaisedPayload {
	var out []TaskRaisedPayload
	for _, e := range bus.published {
		if e.EventType() != ucp.EventSyncTaskRaised {
			continue
		}
		var p TaskRaisedPayload
		_ = json.Unmarshal(e.Payload(), &p)
		out = append(out, p)
	}
	return out
}

// A deleted product must not keep a store blocked or leave tasks open: its
// blocking-issue tasks resolve, its unapplied remediations go, the store
// aggregate is recomputed without it (so the zero-blockers crossing fires),
// and the serving cache is told to rebuild.
func TestOnProductDeleted_RetiresIssuesAndRecomputesStore(t *testing.T) {
	ctx := context.Background()
	store := newFakeCatalogStore() // the deleted product is already gone from the mirror
	bus := newFakeEventBus()
	readiness := newFakeReadinessAggregateStore()
	readiness.records["store-1"] = &StoreReadinessRecord{StoreID: "store-1", MerchantID: "merchant-1", BlockingSKUCount: 1}
	rem := newMemRemediationStore()
	_ = rem.SyncForSKU(ctx, "merchant-1", "store-1", "SKU-1", []Remediation{
		{ID: "store-1:SKU-1:rule_description_missing", MerchantID: "merchant-1", StoreID: "store-1", SKU: "SKU-1"},
	})
	svc := newDeleteTestService(store, bus, readiness, rem)

	deleted := &CatalogProduct{
		MerchantID: "merchant-1", StoreID: "store-1", SKU: "SKU-1",
		Issues: []ProductIssue{
			{IssueID: "rule_images_missing", Field: "images", Problem: "missing", BlocksCheckout: true},
			{IssueID: "rule_description_missing", Field: "description", Problem: "missing"},
		},
	}
	if err := svc.OnProductDeleted(ctx, "store-1", "merchant-1", deleted); err != nil {
		t.Fatalf("OnProductDeleted: %v", err)
	}

	tasks := taskRaisedEvents(bus)
	if len(tasks) != 1 || !tasks[0].Resolved || tasks[0].SourceRef != "SKU-1" || tasks[0].IssueID != "rule_images_missing" {
		t.Fatalf("expected one resolved task for the blocking issue only, got %+v", tasks)
	}
	if len(rem.records) != 0 {
		t.Errorf("expected the deleted product's remediations retired, got %+v", rem.records)
	}
	if got := journeySignals(t, bus); len(got) != 1 || got[0] != "readiness_threshold_met" {
		t.Errorf("expected readiness_threshold_met once the only blocker is gone, got %v", got)
	}
	if got := rebuildEvents(t, bus); len(got) != 1 || got[0].Reason != "product_deleted" {
		t.Errorf("expected one product_deleted rebuild request, got %+v", got)
	}
}

// The tombstoned record must never be re-scored: an upsert clears the
// tombstone and the deleted product would come back.
func TestOnProductChanged_DeletedDoesNotRescoreOrUpsert(t *testing.T) {
	store := newFakeCatalogStore()
	bus := newFakeEventBus()
	svc := newDeleteTestService(store, bus, nil, nil)
	// svc.db is nil: reaching the collection lookup would panic.

	err := svc.OnProductChanged(context.Background(), "store-1", "merchant-1", ProductChangedPayload{SKU: "SKU-1", Change: "deleted"})
	if err != nil {
		t.Fatalf("OnProductChanged: %v", err)
	}
	if len(store.products) != 0 || len(bus.published) != 0 {
		t.Fatalf("a deleted change must do nothing, got products=%v events=%d", store.products, len(bus.published))
	}
}

// The sweep finds products the plugin's delete signal missed; they need the
// same clean-up.
func TestSweepHandler_RemovedProductIsCleanedUp(t *testing.T) {
	catalogStore := newFakeCatalogStore()
	catalogStore.products["merchant-1:store-1:sku-gone"] = &CatalogProduct{
		StoreID: "store-1", MerchantID: "merchant-1", SKU: "sku-gone", Title: "Gone",
	}
	rescorer := newFakeProductRescorer()
	reg, provider := newTestRegistry("store-1", &fakeStoreConnector{})
	h := NewSweepHandler(newFakeLease(), catalogStore, reg, provider, newFakeEventBus(), rescorer, handlerTestLogger(), 0)

	if err := h.RunSweep(context.Background(), "store-1", "merchant-1"); err != nil {
		t.Fatalf("RunSweep: %v", err)
	}
	if len(rescorer.deleted) != 1 || rescorer.deleted[0] != "sku-gone" {
		t.Fatalf("expected OnProductDeleted for sku-gone, got %v", rescorer.deleted)
	}
}
