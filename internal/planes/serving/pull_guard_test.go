// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package serving

import (
	"context"
	"strings"
	"testing"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
)

// recordingConnector answers every method successfully and records which was
// called, so the guard's split can be asserted from the outside.
type recordingConnector struct {
	base.Connector
	called string
}

func (c *recordingConnector) CheckoutCreate(_ context.Context, _ string, _ *base.CreateCheckoutRequest) (*base.CheckoutResponse, error) {
	c.called = "CheckoutCreate"
	return &base.CheckoutResponse{}, nil
}

func (c *recordingConnector) FetchCatalog(_ context.Context, _ string, _ int) ([]base.CatalogEntity, error) {
	c.called = "FetchCatalog"
	return nil, nil
}

// The live checkout call architecture §5 permits must reach the connector.
//
// This is the case the previous guard broke: it refused credentials for the
// whole connector, so CheckoutCreate failed as surely as a catalog pull and
// no agent could ever buy.
func TestBlockPullConnector_AllowsCheckout(t *testing.T) {
	inner := &recordingConnector{}
	guarded := blockPullConnector{Connector: inner, storeID: "store-1"}

	if _, err := guarded.CheckoutCreate(context.Background(), "store-1", &base.CreateCheckoutRequest{}); err != nil {
		t.Fatalf("CheckoutCreate must pass through, got %v", err)
	}
	if inner.called != "CheckoutCreate" {
		t.Errorf("expected the call to reach the connector, got %q", inner.called)
	}
}

// The store-API pull methods belong to the sync plane. An accidental call
// must fail loudly and name what it violated, rather than quietly reaching a
// merchant's store from the serving plane.
func TestBlockPullConnector_BlocksPulls(t *testing.T) {
	inner := &recordingConnector{}
	guarded := blockPullConnector{Connector: inner, storeID: "store-1"}

	_, err := guarded.FetchCatalog(context.Background(), "store-1", 50)
	if err == nil {
		t.Fatal("FetchCatalog must be refused from the serving plane")
	}
	if inner.called != "" {
		t.Errorf("the call must not reach the connector, got %q", inner.called)
	}
	for _, want := range []string{"FetchCatalog", "store-1", "serving cache"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should name %q for a reader to act on it: %v", want, err)
		}
	}
}

// Every method under base.Connector's "Store-API pull (sync-plane only)"
// heading is covered, plus the catalog write the sync plane owns. Listed
// explicitly so adding a pull method to the interface without guarding it
// here is a visible omission rather than a silent hole.
func TestBlockPullConnector_CoversEveryPullMethod(t *testing.T) {
	guarded := blockPullConnector{Connector: &recordingConnector{}, storeID: "store-1"}
	ctx := context.Background()

	checks := map[string]error{}
	_, checks["FetchProduct"] = guarded.FetchProduct(ctx, "store-1", "sku")
	_, checks["FetchInventory"] = guarded.FetchInventory(ctx, "store-1", "sku")
	_, checks["FetchPricing"] = guarded.FetchPricing(ctx, "store-1", "sku")
	_, checks["FetchCatalog"] = guarded.FetchCatalog(ctx, "store-1", 10)
	_, checks["Ping"] = guarded.Ping(ctx, "store-1")
	checks["UpdateProduct"] = guarded.UpdateProduct(ctx, "store-1", "sku", nil)

	for method, err := range checks {
		if err == nil {
			t.Errorf("%s must be refused from the serving plane", method)
		}
	}
}
