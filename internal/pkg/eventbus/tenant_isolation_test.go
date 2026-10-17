// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package eventbus

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/sarvexlab/sarvex/internal/testtenancy"
)

// TestTenantIsolation_EventConsumption is the "cross-tenant event
// consumption" deliverable. Topics are named by EventType() alone
// (backend.go's topicName), never by store_id/merchant_id — confirmed by
// TestMultipleSubscribers above, every subscriber on a topic receives every
// event published to it, with no bus-level ACL. Tenant isolation for event
// consumption is therefore entirely the subscribing handler's own
// responsibility: it must filter by StoreID()/MerchantID() from the
// payload/accessors itself.
//
// This test publishes one event per store across a configurable N-tenant x
// M-store/merchant matrix (default 5x3, see internal/testtenancy) onto a
// single shared topic, then registers one handler per tenant that filters
// by MerchantID(). It asserts two things: (1) the bus really does fan every
// event out to every handler regardless of tenant (so a handler that
// forgot to filter would see other tenants' events — this is the
// no-isolation-at-the-bus-level baseline the rest of the system must
// defend against), and (2) a handler that filters correctly only ever
// counts its own tenant's events, never a neighbor's, at N-tenant scale —
// not just the 2-subscriber case TestMultipleSubscribers already covers.
func TestTenantIsolation_EventConsumption(t *testing.T) {
	eb := testBus(t)
	ctx := context.Background()

	cfg := testtenancy.LoadConfig()
	tenants := testtenancy.Matrix(cfg)
	const topic = "tenancy.isolation.test"
	totalEvents := cfg.NumTenants * cfg.StoresPerTenant

	var (
		mu              sync.Mutex
		rawSeenByAll    = make(map[int]int) // handler index -> raw event count (no filter)
		ownSeenByOwner  = make(map[int]int) // tenant index -> events matching its own merchant id
		foreignReceived = make(map[int]int) // tenant index -> events NOT matching its own merchant id it received
	)

	var wg sync.WaitGroup
	wg.Add(len(tenants) * totalEvents)

	for i, tn := range tenants {
		_, err := eb.Subscribe(ctx, topic, func(_ context.Context, evt DomainEvent) error {
			mu.Lock()
			rawSeenByAll[i]++
			if evt.MerchantID() == tn.MerchantID {
				ownSeenByOwner[i]++
			} else {
				foreignReceived[i]++
			}
			mu.Unlock()
			wg.Done()
			return nil
		})
		if err != nil {
			t.Fatalf("subscribe tenant %d: %v", i, err)
		}
	}

	// Give subscriptions time to register before publishing (mirrors
	// TestPublishSubscribe's own settle delay for this same gochannel bus).
	time.Sleep(50 * time.Millisecond)

	for i, tn := range tenants {
		for j, storeID := range tn.StoreIDs {
			evt := &testEvent{
				eventType:  topic,
				merchantID: tn.MerchantID,
				storeID:    storeID,
				occurredAt: time.Now().UTC(),
				payload:    []byte(fmt.Sprintf(`{"tenant":%d,"store":%d}`, i, j)),
			}
			if err := eb.Publish(ctx, evt); err != nil {
				t.Fatalf("publish event for tenant %d store %d: %v", i, j, err)
			}
		}
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for all tenant handlers to see all events")
	}

	mu.Lock()
	defer mu.Unlock()

	for i, tn := range tenants {
		if rawSeenByAll[i] != totalEvents {
			t.Errorf("tenant %d's handler saw %d raw events on the shared topic, want all %d (the bus provides no topic-level tenant filtering)", i, rawSeenByAll[i], totalEvents)
		}
		if ownSeenByOwner[i] != len(tn.StoreIDs) {
			t.Errorf("tenant %d's own-event count after filtering by MerchantID() is %d, want %d", i, ownSeenByOwner[i], len(tn.StoreIDs))
		}
		wantForeign := totalEvents - len(tn.StoreIDs)
		if foreignReceived[i] != wantForeign {
			t.Errorf("tenant %d's handler received %d events not matching its own merchant id, want %d (all other tenants' events, still deliverable — filtering is the handler's job, not the bus's)", i, foreignReceived[i], wantForeign)
		}
	}
}
