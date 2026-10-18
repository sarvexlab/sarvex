// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
	"github.com/sarvexlab/sarvex/internal/planes/sync"
)

// mockPullConnector is the "mock pull" side of this integration test: it
// stands in for the real store-API client (woocommerce.Connector),
// returning a canned authoritative entity for the SKU under test. The
// embedded nil base.Connector satisfies the rest of the interface
// (checkout/catalog/cart/order), unused here.
type mockPullConnector struct {
	base.Connector
	product *base.CatalogEntity
}

func (m *mockPullConnector) FetchProduct(ctx context.Context, storeID, sku string) (*base.CatalogEntity, error) {
	cp := *m.product
	return &cp, nil
}
func (m *mockPullConnector) FetchInventory(ctx context.Context, storeID, sku string) (*base.InventoryEntity, error) {
	return &base.InventoryEntity{SKU: sku, InStock: m.product.InStock, Quantity: m.product.Quantity}, nil
}
func (m *mockPullConnector) FetchPricing(ctx context.Context, storeID, sku string) (*base.PricingEntity, error) {
	return &base.PricingEntity{SKU: sku, PriceMinor: m.product.PriceMinor, Currency: m.product.Currency}, nil
}
func (m *mockPullConnector) FetchCatalog(ctx context.Context, storeID string, pageSize int) ([]base.CatalogEntity, error) {
	return []base.CatalogEntity{*m.product}, nil
}

// mappingCall records one call ingestion handed to a recordingMappingPipeline
// — ingestion and mapping/scoring are both same-process sync-plane
// components now (see sync.NewIngestionPipeline's mappingPipeline parameter
// doc comment), so there is no bus event left for this integration test to
// observe; it asserts on the direct handoff instead.
type mappingCall struct {
	kind       string // "product", "price", "inventory"
	storeID    string
	merchantID string
	sku        string
	change     string // product only
}

type recordingMappingPipeline struct {
	calls []mappingCall
}

func (p *recordingMappingPipeline) OnProductChanged(_ context.Context, storeID, merchantID string, payload sync.ProductChangedPayload) error {
	p.calls = append(p.calls, mappingCall{kind: "product", storeID: storeID, merchantID: merchantID, sku: payload.SKU, change: payload.Change})
	return nil
}

func (p *recordingMappingPipeline) OnPriceChanged(_ context.Context, storeID, merchantID string, payload sync.PriceChangedPayload) error {
	p.calls = append(p.calls, mappingCall{kind: "price", storeID: storeID, merchantID: merchantID, sku: payload.SKU})
	return nil
}

func (p *recordingMappingPipeline) OnInventoryChanged(_ context.Context, storeID, merchantID string, payload sync.InventoryChangedPayload) error {
	p.calls = append(p.calls, mappingCall{kind: "inventory", storeID: storeID, merchantID: merchantID, sku: payload.SKU})
	return nil
}

func (p *recordingMappingPipeline) OnProductDeleted(_ context.Context, storeID, merchantID string, prod *sync.CatalogProduct) error {
	p.calls = append(p.calls, mappingCall{kind: "product_deleted", storeID: storeID, merchantID: merchantID, sku: prod.SKU})
	return nil
}

type gatewaySignalPayload struct {
	ConnectorID string `json:"connector_id"`
	StoreID     string `json:"store_id"`
	SKU         string `json:"sku"`
	EntityKind  string `json:"entity_kind"`
	Sequence    int64  `json:"sequence"`
	Change      string `json:"change"`
}

type gatewaySignalEvent struct {
	storeID    string
	merchantID string
	payload    []byte
}

func (e *gatewaySignalEvent) EventType() string     { return "connector.signal_received" }
func (e *gatewaySignalEvent) StoreID() string       { return e.storeID }
func (e *gatewaySignalEvent) MerchantID() string    { return e.merchantID }
func (e *gatewaySignalEvent) OccurredAt() time.Time { return time.Now().UTC() }
func (e *gatewaySignalEvent) Payload() []byte       { return e.payload }

// TestIngestion_GatewaySignal_MockPull_CatalogStoreUpdated_ChangeEventEmitted
// exercises the real MongoDB-backed catalog store end to end: a gateway
// signal (shaped exactly as internal/planes/control/connector.AcceptSignal
// publishes it) drives the ingestion pipeline, which pulls the authoritative
// entity from a mocked connector, writes it through the real catalog store,
// and emits the normalised change event.
func TestIngestion_GatewaySignal_MockPull_CatalogStoreUpdated_ChangeEventEmitted(t *testing.T) {
	env := setup(t)
	defer env.cleanup(t)

	catalogStore, err := sync.NewMongoDBCatalogStore(env.db)
	if err != nil {
		t.Fatalf("NewMongoDBCatalogStore: %v", err)
	}

	storeID, merchantID := "store-int-1", "merch-int-1"

	connector := &mockPullConnector{
		product: &base.CatalogEntity{
			SKU: "SKU-INT-1", Name: "Integration Product", PriceMinor: 4599, Currency: "AUD", InStock: true,
		},
	}
	mapping := &recordingMappingPipeline{}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	reg, provider := newTestRegistry(storeID, merchantID, connector)
	pipeline := sync.NewIngestionPipeline(catalogStore, reg, provider, mapping, nil, log)

	payload, _ := json.Marshal(gatewaySignalPayload{
		ConnectorID: "conn-int-1",
		StoreID:     storeID,
		SKU:         "SKU-INT-1",
		EntityKind:  "product",
		Sequence:    1,
		Change:      "updated",
	})
	sig := &gatewaySignalEvent{storeID: storeID, merchantID: merchantID, payload: payload}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := pipeline.HandleSignalReceived(ctx, sig); err != nil {
		t.Fatalf("HandleSignalReceived: %v", err)
	}

	product, err := catalogStore.GetProduct(ctx, storeID, "SKU-INT-1")
	if err != nil {
		t.Fatalf("GetProduct: %v", err)
	}
	if product.Title != "Integration Product" || product.PriceMinor != 4599 {
		t.Errorf("expected pulled entity written to catalog store, got %+v", product)
	}

	if len(mapping.calls) != 1 || mapping.calls[0].kind != "product" {
		t.Fatalf("expected 1 product mapping call, got %+v", mapping.calls)
	}
	if mapping.calls[0].storeID != storeID {
		t.Errorf("expected call keyed by store_id=%s, got %s", storeID, mapping.calls[0].storeID)
	}
}
