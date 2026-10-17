// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package sim_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	specall "github.com/sarvexlab/sarvex/internal/spec/all"

	"github.com/sarvexlab/sarvex/internal/pkg/commerce/model"

	"github.com/sarvexlab/sarvex/internal/pkg/simstore"
	ucptype "github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/types"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wireadapt"
	syncsim "github.com/sarvexlab/sarvex/internal/planes/sync/sim"
	"github.com/sarvexlab/sarvex/internal/platform"
)

// TestCapabilityMatrix runs one scenario — checkout, cart, buyer + consent,
// discounts, shipping rates, pickup, order — against every backend platform
// and renders each response in every servable spec generation. Adding a
// platform (a row in platforms) or a generation (specall.Default().Servable()) extends the
// matrix automatically, so a capability or extension cannot be added for one
// platform or version and quietly skipped for another.
//
// The platforms are the simulator's WooCommerce and Magento REST surfaces, not
// real stores.

// matrixPlatform is one row of the matrix: a registered platform, driven
// through its real connector against its simulated store.
type matrixPlatform struct {
	name string
	sim  simstore.Surface
}

// conn builds a fresh connector (connectors cache their view of the store's
// pickup setting, so each phase that flips pickup uses a new one).
func (p matrixPlatform) conn(t *testing.T, baseURL, storeID string) ucptype.Connector {
	t.Helper()
	conn, err := p.sim.Connector(baseURL, "merchant-1", storeID)
	if err != nil {
		t.Fatalf("%s connector: %v", p.name, err)
	}
	return conn
}

// catalogID is the id catalog lookups use for a product (WooCommerce's catalog
// is keyed by numeric product id, Magento's by SKU). Checkout line items
// accept the SKU on every platform.
func (p matrixPlatform) catalogID(prod *syncsim.Product) string { return p.sim.CatalogID(prod) }

// pickupLocationID is the id of the pickup destination the platform offers.
func (p matrixPlatform) pickupLocationID() string { return p.sim.PickupLocationID() }

// matrixPlatforms is every registered platform — the registry, not a list
// kept here, so a newly registered platform runs the whole scenario.
func matrixPlatforms() []matrixPlatform {
	var out []matrixPlatform
	for _, p := range platform.All() {
		out = append(out, matrixPlatform{name: string(p.Name), sim: p.Sim})
	}
	return out
}

// matrixEnv is what a matrix run renders and serves with: the spec
// generations' codecs, and any simulated store surfaces registered beside the
// real platforms' (the canary run adds one).
type matrixEnv struct {
	codecs        *wireadapt.Registry
	extraSurfaces []simstore.Surface
}

func defaultMatrixEnv() matrixEnv { return matrixEnv{codecs: specall.Default().Codecs()} }

// render renders a canonical response in generation v and returns the decoded
// JSON, failing the test if the generation cannot represent it.
func (e matrixEnv) render(t *testing.T, v types.Version, r wireadapt.Resource, canonical any) map[string]any {
	t.Helper()
	b, err := e.codecs.EncodeResponse(v, r, canonical, nil)
	if err != nil {
		t.Fatalf("render %s in %s: %v", r, v, err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("decode rendered %s: %v", r, err)
	}
	return m
}

// TestCapabilityMatrixCoversEverySupportedPlatform fails when a registered
// platform has no simulated store: the matrix iterates the platform registry,
// so a platform without one could not run the shared capability scenario.
func TestCapabilityMatrixCoversEverySupportedPlatform(t *testing.T) {
	for _, p := range platform.All() {
		if p.Sim == nil {
			t.Errorf("platform %q is supported but has no simulated store (platform.Platform.Sim), so TestCapabilityMatrix cannot run it", p.Name)
		}
	}
}

func TestCapabilityMatrix(t *testing.T) {
	for _, p := range matrixPlatforms() {
		for _, v := range specall.Default().Servable() {
			t.Run(p.name+"/"+string(v), func(t *testing.T) {
				runMatrixScenario(t, defaultMatrixEnv(), p, v)
			})
		}
	}
}

func runMatrixScenario(t *testing.T, env matrixEnv, p matrixPlatform, v types.Version) {
	storeID := "store-matrix-" + p.name + "-" + strings.ReplaceAll(string(v), "-", "")
	repo := newMemRepo()
	store := syncsim.NewStore(storeID)
	store.Seed(1, false)
	if err := repo.Upsert(context.Background(), store); err != nil {
		t.Fatalf("seed: %v", err)
	}
	var sku string
	for s := range store.Products {
		sku = s
	}
	srv := syncsim.NewServer(repo, "http://sim-server.internal", nil, nil, nil, nil)
	for _, extra := range env.extraSurfaces {
		srv.AddSurface(extra)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx := context.Background()
	items := []ucptype.LineItemCreateRequest{{Item: ucptype.ItemRef{ID: sku}, Quantity: 2}}
	yes := true
	buyer := &model.BuyerWithConsent{
		Buyer:   model.Buyer{Email: "ada@example.com", FirstName: "Ada", LastName: "Lovelace"},
		Consent: &model.Consent{Marketing: &yes},
	}
	address := &model.PostalAddress{
		FirstName: "Ada", LastName: "Lovelace", StreetAddress: "1 Analytical Way",
		AddressLocality: "London", AddressRegion: "LDN", PostalCode: "SW1A 1AA", AddressCountry: "GB",
	}

	conn := p.conn(t, ts.URL, storeID)
	g := goldenRun{platform: p.name, serverURL: ts.URL, storeID: storeID}

	// Catalog.
	lookup, err := conn.CatalogLookup(ctx, storeID, p.catalogID(store.Products[sku]))
	if err != nil {
		t.Fatalf("catalog lookup: %v", err)
	}
	assertGolden(t, g, canonicalGolden, "catalog_lookup", lookup)

	// Catalog pull (what the sync plane scores): every platform carries the
	// product's description and images typed, and its weight and dimensions as
	// raw attributes, so no platform scores permanently "missing" on data the
	// store actually has.
	pulled, err := conn.FetchProduct(ctx, storeID, sku)
	if err != nil {
		t.Fatalf("fetch product: %v", err)
	}
	if pulled.Description == "" {
		t.Errorf("%s: pulled product has no description", p.name)
	}
	if len(pulled.Images) == 0 || !strings.HasPrefix(pulled.Images[0], "http") {
		t.Errorf("%s: pulled product images must be absolute URLs, got %v", p.name, pulled.Images)
	}
	if got := pulled.RawAttributes["weight"]; got != "0.5" {
		t.Errorf("%s: raw weight = %q, want 0.5", p.name, got)
	}
	if got := pulled.RawAttributes["dimensions"]; got != "10x10x10" {
		t.Errorf("%s: raw dimensions = %q, want 10x10x10", p.name, got)
	}

	// Checkout + buyer_consent + discount + fulfillment (shipping rates).
	checkout, err := conn.CheckoutCreate(ctx, storeID, &ucptype.CreateCheckoutRequest{
		LineItems:   items,
		Buyer:       buyer,
		Discounts:   &model.DiscountsObject{Codes: []string{"SPRING20"}},
		Fulfillment: &ucptype.FulfillmentRequest{SelectedDestination: address},
	})
	if err != nil {
		t.Fatalf("checkout create: %v", err)
	}
	if checkout.Discounts == nil || len(checkout.Discounts.Applied) == 0 {
		t.Errorf("discount extension: nothing applied: %+v", checkout.Discounts)
	}
	if d, ok := totalOf(checkout.Totals, model.TotalTypeDiscount); !ok || d >= 0 {
		t.Errorf("discount total must be present and negative, got %d (present=%v)", d, ok)
	}
	if checkout.Buyer == nil || checkout.Buyer.Email != "ada@example.com" || checkout.Buyer.Consent == nil {
		t.Errorf("buyer_consent extension: buyer/consent not reported: %+v", checkout.Buyer)
	}
	sm := fulfillmentMethod(checkout.Fulfillment, model.FulfillmentMethodTypeShipping)
	if sm == nil || len(sm.Destinations) == 0 || len(sm.Groups) == 0 || len(sm.Groups[0].Options) == 0 {
		t.Fatalf("fulfillment extension: want a shipping method with a destination and rates, got %+v", checkout.Fulfillment)
	}
	rendered := env.render(t, v, wireadapt.Checkout, checkout)
	assertGolden(t, g, canonicalGolden, "checkout_create", checkout)
	assertGolden(t, g, string(v), "checkout_create", rendered)
	if v == types.Version("2026-08-25") {
		// 2026-08-25 keys consent by reverse-DNS purpose and tags destinations.
		consent := digObj(t, rendered, "buyer", "consent")
		if _, ok := consent["dev.ucp.consent.marketing"]; !ok {
			t.Errorf("2026-08-25 consent not keyed by purpose: %v", consent)
		}
	}

	// Choose the offered rate, then place the order.
	rate := sm.Groups[0].Options[0].ID
	if _, err := conn.CheckoutUpdate(ctx, storeID, checkout.ID, &ucptype.UpdateCheckoutRequest{
		Buyer:       buyer,
		Fulfillment: &ucptype.FulfillmentRequest{SelectedMethodID: rate, SelectedDestination: address},
		Discounts:   &model.DiscountsObject{Codes: []string{"SPRING20"}},
	}); err != nil {
		t.Fatalf("checkout update: %v", err)
	}
	done, err := conn.CheckoutComplete(ctx, storeID, checkout.ID, &ucptype.CompleteCheckoutRequest{Buyer: buyer})
	if err != nil {
		t.Fatalf("checkout complete: %v", err)
	}
	if done.Status != ucptype.CheckoutStatusCompleted || done.Order == nil || done.Order.ID == "" {
		t.Errorf("order: want a completed checkout with an order confirmation, got status=%q order=%+v", done.Status, done.Order)
	} else if order, err := conn.OrderGet(ctx, storeID, done.Order.ID); err != nil {
		t.Errorf("order get %s: %v", done.Order.ID, err)
	} else {
		assertGolden(t, g, canonicalGolden, "order_get", order)
	}
	assertGolden(t, g, canonicalGolden, "checkout_complete", done)
	assertGolden(t, g, string(v), "checkout_complete", env.render(t, v, wireadapt.Checkout, done))

	// Cart + discount.
	cart, err := conn.CartCreate(ctx, storeID, &ucptype.CreateCartRequest{
		LineItems: items,
		Discounts: &model.DiscountsObject{Codes: []string{"SPRING20"}},
	})
	if err != nil {
		t.Fatalf("cart create: %v", err)
	}
	if cart.Discounts == nil || len(cart.Discounts.Applied) == 0 {
		t.Errorf("cart discount extension: nothing applied: %+v", cart.Discounts)
	}
	assertGolden(t, g, canonicalGolden, "cart_create", cart)
	assertGolden(t, g, string(v), "cart_create", env.render(t, v, wireadapt.Cart, cart))

	// Pickup: offered only once the merchant enables it.
	off, err := p.conn(t, ts.URL, storeID).CheckoutCreate(ctx, storeID, &ucptype.CreateCheckoutRequest{LineItems: items})
	if err != nil {
		t.Fatalf("checkout create (pickup off): %v", err)
	}
	if fulfillmentMethod(off.Fulfillment, model.FulfillmentMethodTypePickup) != nil {
		t.Errorf("pickup offered before the merchant enabled it")
	}
	store.LocalPickup = true
	on, err := p.conn(t, ts.URL, storeID).CheckoutCreate(ctx, storeID, &ucptype.CreateCheckoutRequest{LineItems: items})
	if err != nil {
		t.Fatalf("checkout create (pickup on): %v", err)
	}
	pm := fulfillmentMethod(on.Fulfillment, model.FulfillmentMethodTypePickup)
	if pm == nil || len(pm.Destinations) == 0 || pm.Destinations[0].Retail == nil || pm.Destinations[0].Retail.ID != p.pickupLocationID() {
		t.Fatalf("pickup enabled but not offered as a business-location destination: %+v", on.Fulfillment)
	}
	picked, err := p.conn(t, ts.URL, storeID).CheckoutCreate(ctx, storeID, &ucptype.CreateCheckoutRequest{
		LineItems: items,
		Buyer:     buyer,
		Fulfillment: &ucptype.FulfillmentRequest{
			MethodType:            string(model.FulfillmentMethodTypePickup),
			SelectedDestinationID: p.pickupLocationID(),
		},
	})
	if err != nil {
		t.Fatalf("checkout create (pickup selected): %v", err)
	}
	if pm := fulfillmentMethod(picked.Fulfillment, model.FulfillmentMethodTypePickup); pm == nil || pm.SelectedDestinationID == nil {
		t.Errorf("pickup selection not reflected: %+v", picked.Fulfillment)
	}
	out := env.render(t, v, wireadapt.Checkout, picked)
	assertGolden(t, g, canonicalGolden, "checkout_pickup", picked)
	assertGolden(t, g, string(v), "checkout_pickup", out)
	if v == types.Version("2026-08-25") {
		if !strings.Contains(mustJSON(t, out["fulfillment"]), `"business_location"`) {
			t.Errorf("2026-08-25 pickup destination not typed business_location: %s", mustJSON(t, out["fulfillment"]))
		}
	}
	if _, err := p.conn(t, ts.URL, storeID).CheckoutComplete(ctx, storeID, picked.ID, &ucptype.CompleteCheckoutRequest{Buyer: buyer}); err != nil {
		t.Errorf("complete pickup order: %v", err)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

// dig walks a decoded JSON value by object keys (string) and array indices
// (int), failing the test if the shape is not as expected.
func dig(t *testing.T, v any, path ...any) any {
	t.Helper()
	for _, p := range path {
		switch k := p.(type) {
		case string:
			m, ok := v.(map[string]any)
			if !ok {
				t.Fatalf("want an object at %q, got %T", k, v)
			}
			v = m[k]
		case int:
			a, ok := v.([]any)
			if !ok || k >= len(a) {
				t.Fatalf("want an array with index %d, got %T", k, v)
			}
			v = a[k]
		default:
			t.Fatalf("bad path element %v", p)
		}
	}
	return v
}

func digObj(t *testing.T, v any, path ...any) map[string]any {
	t.Helper()
	m, ok := dig(t, v, path...).(map[string]any)
	if !ok {
		t.Fatalf("want an object at %v", path)
	}
	return m
}
