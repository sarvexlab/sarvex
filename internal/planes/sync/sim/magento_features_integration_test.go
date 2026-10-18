// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package sim_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/commerce/model"

	"github.com/sarvexlab/sarvex/internal/pkg/simstore"
	ucptype "github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/resilience"
	syncsim "github.com/sarvexlab/sarvex/internal/planes/sync/sim"
	"github.com/sarvexlab/sarvex/internal/platform/magento"
	magentosim "github.com/sarvexlab/sarvex/internal/platform/magento/sim"
)

// The tests below drive the real magento.Connector against the sim store's
// Magento surface for every capability and extension the WooCommerce connector
// is exercised on — checkout, cart, buyer + consent, discounts, fulfillment
// including pickup, order — so a change to one platform cannot silently leave
// the other behind. The sim mirrors the Magento REST API's documented shapes;
// it is not a real Magento, which none of this has been run against.

type magentoHarness struct {
	t       *testing.T
	storeID string
	store   *syncsim.Store
	url     string
	skus    []string
}

func newMagentoHarness(t *testing.T, storeID string, products int) *magentoHarness {
	t.Helper()
	repo := newMemRepo()
	store := syncsim.NewStore(storeID)
	store.Seed(products, false)
	if err := repo.Upsert(context.Background(), store); err != nil {
		t.Fatalf("seed store: %v", err)
	}
	srv := syncsim.NewServer(repo, "http://sim-server.internal", nil, nil, nil, nil)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	h := &magentoHarness{t: t, storeID: storeID, store: store, url: ts.URL}
	for sku := range store.Products {
		h.skus = append(h.skus, sku)
	}
	return h
}

// conn returns a fresh connector (the connector caches its view of the store's
// pickup setting, so a test that flips pickup uses a fresh one per phase).
func (h *magentoHarness) conn() *magento.Connector {
	h.t.Helper()
	conn, err := magento.NewConnector(magento.MerchantConfig{
		MerchantID:        "merchant-1",
		StoreID:           h.storeID,
		BaseURL:           simstore.StoreBaseURL(h.url, h.storeID),
		AdminUser:         magentosim.AdminUser(h.storeID),
		AdminPassword:     magentosim.AdminPassword(h.storeID),
		StoreCode:         magentosim.StoreCode,
		StoreCurrency:     "USD",
		WebhookSecret:     simstore.WebhookSecret(h.storeID),
		PaymentMethodCode: "checkmo",
		RequestTimeout:    5 * time.Second,
		MaxRetries:        1,
	}, nil, resilience.Config{}, nil)
	if err != nil {
		h.t.Fatalf("NewConnector: %v", err)
	}
	return conn
}

func (h *magentoHarness) items(qty int) []ucptype.LineItemCreateRequest {
	return []ucptype.LineItemCreateRequest{{Item: ucptype.ItemRef{ID: h.skus[0]}, Quantity: qty}}
}

var ada = &model.BuyerWithConsent{Buyer: model.Buyer{Email: "ada@example.com", FirstName: "Ada", LastName: "Lovelace", PhoneNumber: "+441234567890"}}

var londonAddress = &model.PostalAddress{
	FirstName: "Ada", LastName: "Lovelace", StreetAddress: "1 Analytical Way",
	AddressLocality: "London", AddressRegion: "LDN", PostalCode: "SW1A 1AA", AddressCountry: "GB",
}

func totalOf(totals model.Totals, typ model.TotalType) (int64, bool) {
	for _, t := range totals {
		if t.Type == typ {
			return int64(t.Amount), true
		}
	}
	return 0, false
}

func fulfillmentMethod(f *model.Fulfillment, typ model.FulfillmentMethodType) *model.FulfillmentMethod {
	if f == nil {
		return nil
	}
	for i := range f.Methods {
		if f.Methods[i].Type == typ {
			return &f.Methods[i]
		}
	}
	return nil
}

func TestMagento_Checkout_BuyerConsentRecordedAndOrderConfirmed(t *testing.T) {
	h := newMagentoHarness(t, "store-mg-buyer-1", 2)
	conn := h.conn()
	ctx := context.Background()

	yes, no := true, false
	buyer := &model.BuyerWithConsent{Buyer: ada.Buyer, Consent: &model.Consent{Marketing: &yes, Analytics: &no}}

	created, err := conn.CheckoutCreate(ctx, h.storeID, &ucptype.CreateCheckoutRequest{LineItems: h.items(1), Buyer: buyer})
	if err != nil {
		t.Fatalf("CheckoutCreate: %v", err)
	}
	if created.Buyer == nil || created.Buyer.Email != "ada@example.com" || created.Buyer.Consent == nil {
		t.Fatalf("the submitted buyer and consent must be echoed on the response, got %+v", created.Buyer)
	}
	if _, ok := totalOf(created.Totals, model.TotalTypeSubtotal); !ok {
		t.Fatalf("totals must carry a subtotal: %+v", created.Totals)
	}

	if _, err := conn.CheckoutUpdate(ctx, h.storeID, created.ID, &ucptype.UpdateCheckoutRequest{
		Fulfillment: &ucptype.FulfillmentRequest{SelectedMethodID: "flatrate_flatrate", SelectedDestination: londonAddress},
	}); err != nil {
		t.Fatalf("CheckoutUpdate: %v", err)
	}

	completed, err := conn.CheckoutComplete(ctx, h.storeID, created.ID, &ucptype.CompleteCheckoutRequest{Buyer: buyer})
	if err != nil {
		t.Fatalf("CheckoutComplete: %v", err)
	}
	if completed.Status != ucptype.CheckoutStatusCompleted {
		t.Fatalf("status = %q, want completed", completed.Status)
	}
	if completed.ID != created.ID {
		t.Errorf("a completed checkout keeps its id: got %q, want %q", completed.ID, created.ID)
	}
	if completed.Order == nil || completed.Order.ID == "" || completed.Order.PermalinkURL == "" {
		t.Fatalf("a completed checkout must carry the order confirmation the sync plane records the sale from: %+v", completed.Order)
	}
	if completed.Buyer == nil || completed.Buyer.Email != "ada@example.com" {
		t.Errorf("the placed order's buyer = %+v", completed.Buyer)
	}
	if completed.Buyer == nil || completed.Buyer.Consent == nil || completed.Buyer.Consent.Marketing == nil || !*completed.Buyer.Consent.Marketing {
		t.Errorf("the completed checkout must report the consent given: %+v", completed.Buyer)
	}

	// The consent is stored with the order at the merchant.
	var stored string
	for _, o := range h.store.Orders {
		stored = o.BuyerConsent
	}
	if stored == "" {
		t.Error("buyer consent was not recorded with the order")
	}

	// The confirmation's id is the one the order can be fetched by.
	if _, err := conn.OrderGet(ctx, h.storeID, completed.Order.ID); err != nil {
		t.Errorf("OrderGet(%s): %v", completed.Order.ID, err)
	}
}

func TestMagento_Checkout_GuestOrderNeedsBuyerEmail(t *testing.T) {
	h := newMagentoHarness(t, "store-mg-noemail-1", 1)
	conn := h.conn()
	ctx := context.Background()

	created, err := conn.CheckoutCreate(ctx, h.storeID, &ucptype.CreateCheckoutRequest{
		LineItems:   h.items(1),
		Fulfillment: &ucptype.FulfillmentRequest{SelectedMethodID: "flatrate_flatrate", SelectedDestination: londonAddress},
	})
	if err != nil {
		t.Fatalf("CheckoutCreate: %v", err)
	}
	_, err = conn.CheckoutComplete(ctx, h.storeID, created.ID, &ucptype.CompleteCheckoutRequest{})
	var rej *ucptype.CheckoutRejectedError
	if !errors.As(err, &rej) {
		t.Fatalf("a completion the store refuses for a missing email must be a business rejection, got %v", err)
	}
}

func TestMagento_Checkout_LineItemUpdateReplaces(t *testing.T) {
	h := newMagentoHarness(t, "store-mg-lines-1", 3)
	conn := h.conn()
	ctx := context.Background()

	created, err := conn.CheckoutCreate(ctx, h.storeID, &ucptype.CreateCheckoutRequest{LineItems: []ucptype.LineItemCreateRequest{
		{Item: ucptype.ItemRef{ID: h.skus[0]}, Quantity: 1},
		{Item: ucptype.ItemRef{ID: h.skus[1]}, Quantity: 1},
	}})
	if err != nil {
		t.Fatalf("CheckoutCreate: %v", err)
	}
	if len(created.LineItems) != 2 {
		t.Fatalf("want 2 lines, got %d", len(created.LineItems))
	}

	// PUT: sku0 quantity 3, sku1 dropped, sku2 added.
	updated, err := conn.CheckoutUpdate(ctx, h.storeID, created.ID, &ucptype.UpdateCheckoutRequest{LineItems: []ucptype.LineItemUpdateRequest{
		{Item: ucptype.ItemRef{ID: h.skus[0]}, Quantity: 3},
		{Item: ucptype.ItemRef{ID: h.skus[2]}, Quantity: 1},
	}})
	if err != nil {
		t.Fatalf("CheckoutUpdate: %v", err)
	}
	got := map[string]int{}
	for _, li := range updated.LineItems {
		got[li.Item.ID] = li.Quantity
	}
	if len(got) != 2 || got[h.skus[0]] != 3 || got[h.skus[2]] != 1 {
		t.Fatalf("line items after PUT = %v, want {%s:3 %s:1}", got, h.skus[0], h.skus[2])
	}

	// Quantity 0 removes a line.
	removed, err := conn.CheckoutUpdate(ctx, h.storeID, created.ID, &ucptype.UpdateCheckoutRequest{LineItems: []ucptype.LineItemUpdateRequest{
		{Item: ucptype.ItemRef{ID: h.skus[0]}, Quantity: 3},
		{Item: ucptype.ItemRef{ID: h.skus[2]}, Quantity: 0},
	}})
	if err != nil {
		t.Fatalf("CheckoutUpdate (remove): %v", err)
	}
	if len(removed.LineItems) != 1 || removed.LineItems[0].Item.ID != h.skus[0] {
		t.Fatalf("line items after removal = %+v", removed.LineItems)
	}
}

func TestMagento_Discounts_CheckoutAndCart(t *testing.T) {
	h := newMagentoHarness(t, "store-mg-discount-1", 1)
	conn := h.conn()
	ctx := context.Background()

	// Checkout: a known code applies, an unknown one is left out.
	checkout, err := conn.CheckoutCreate(ctx, h.storeID, &ucptype.CreateCheckoutRequest{
		LineItems: h.items(2),
		Discounts: &model.DiscountsObject{Codes: []string{"NOPE", "spring20"}},
	})
	if err != nil {
		t.Fatalf("CheckoutCreate: %v", err)
	}
	if checkout.Discounts == nil || len(checkout.Discounts.Applied) != 1 || checkout.Discounts.Applied[0].Code != "SPRING20" {
		t.Fatalf("applied discounts = %+v, want SPRING20 only", checkout.Discounts)
	}
	disc, ok := totalOf(checkout.Totals, model.TotalTypeDiscount)
	if !ok || disc >= 0 {
		t.Fatalf("a discount total must be present and negative, got %d (present=%v)", disc, ok)
	}
	if applied := int64(checkout.Discounts.Applied[0].Amount); applied != -disc {
		t.Errorf("applied amount %d does not match the discount total %d", applied, disc)
	}

	// PUT with an empty (non-nil) set clears it.
	cleared, err := conn.CheckoutUpdate(ctx, h.storeID, checkout.ID, &ucptype.UpdateCheckoutRequest{Discounts: &model.DiscountsObject{Codes: []string{}}})
	if err != nil {
		t.Fatalf("CheckoutUpdate (clear): %v", err)
	}
	if cleared.Discounts != nil {
		t.Errorf("cleared discounts still applied: %+v", cleared.Discounts)
	}
	if _, present := totalOf(cleared.Totals, model.TotalTypeDiscount); present {
		t.Errorf("no discount total should be reported when none applies: %+v", cleared.Totals)
	}

	// Cart: the discount extension applies to carts too.
	cart, err := conn.CartCreate(ctx, h.storeID, &ucptype.CreateCartRequest{
		LineItems: h.items(1),
		Discounts: &model.DiscountsObject{Codes: []string{"SPRING20"}},
	})
	if err != nil {
		t.Fatalf("CartCreate: %v", err)
	}
	if cart.Discounts == nil || len(cart.Discounts.Applied) != 1 {
		t.Fatalf("cart discounts = %+v", cart.Discounts)
	}
}

func TestMagento_Cart_CreateUpdateGet(t *testing.T) {
	h := newMagentoHarness(t, "store-mg-cart-1", 2)
	conn := h.conn()
	ctx := context.Background()

	cart, err := conn.CartCreate(ctx, h.storeID, &ucptype.CreateCartRequest{LineItems: h.items(1), Buyer: &ada.Buyer})
	if err != nil {
		t.Fatalf("CartCreate: %v", err)
	}
	if cart.ID == "" || cart.Currency != "USD" || len(cart.LineItems) != 1 {
		t.Fatalf("cart = %+v", cart)
	}
	if cart.Buyer == nil || cart.Buyer.Email != "ada@example.com" {
		t.Errorf("cart buyer = %+v", cart.Buyer)
	}

	updated, err := conn.CartUpdate(ctx, h.storeID, cart.ID, &ucptype.UpdateCartRequest{LineItems: []ucptype.LineItemUpdateRequest{
		{Item: ucptype.ItemRef{ID: h.skus[0]}, Quantity: 4},
	}})
	if err != nil {
		t.Fatalf("CartUpdate: %v", err)
	}
	if updated.LineItems[0].Quantity != 4 {
		t.Errorf("quantity after update = %d, want 4", updated.LineItems[0].Quantity)
	}

	got, err := conn.CartGet(ctx, h.storeID, cart.ID)
	if err != nil || got.LineItems[0].Quantity != 4 {
		t.Fatalf("CartGet = %+v, %v", got, err)
	}
	if _, err := conn.CartGet(ctx, h.storeID, "no-such-cart"); !errors.Is(err, magento.ErrNotFound) {
		t.Errorf("CartGet(unknown) = %v, want ErrNotFound", err)
	}
}

func TestMagento_Pickup_DetectedOfferedAndOrdered(t *testing.T) {
	h := newMagentoHarness(t, "store-mg-pickup-1", 1)
	ctx := context.Background()

	// Off by default: the store's source is not a pickup location.
	off, err := h.conn().CheckoutCreate(ctx, h.storeID, &ucptype.CreateCheckoutRequest{LineItems: h.items(1)})
	if err != nil {
		t.Fatalf("CheckoutCreate (pickup off): %v", err)
	}
	if fulfillmentMethod(off.Fulfillment, model.FulfillmentMethodTypePickup) != nil {
		t.Fatalf("pickup offered although the store has not enabled it: %+v", off.Fulfillment)
	}
	if fulfillmentMethod(off.Fulfillment, model.FulfillmentMethodTypeShipping) == nil {
		t.Fatalf("shipping must always be offered: %+v", off.Fulfillment)
	}
	// Selecting pickup at a store without it is a rejection, not a silent shipping order.
	_, err = h.conn().CheckoutCreate(ctx, h.storeID, &ucptype.CreateCheckoutRequest{
		LineItems:   h.items(1),
		Fulfillment: &ucptype.FulfillmentRequest{MethodType: "pickup"},
	})
	var rej *ucptype.CheckoutRejectedError
	if !errors.As(err, &rej) || rej.Code != "pickup_unavailable" {
		t.Fatalf("pickup at a store without it = %v, want pickup_unavailable rejection", err)
	}

	h.store.LocalPickup = true
	conn := h.conn()
	on, err := conn.CheckoutCreate(ctx, h.storeID, &ucptype.CreateCheckoutRequest{LineItems: h.items(1)})
	if err != nil {
		t.Fatalf("CheckoutCreate (pickup on): %v", err)
	}
	pm := fulfillmentMethod(on.Fulfillment, model.FulfillmentMethodTypePickup)
	if pm == nil {
		t.Fatalf("pickup enabled at the store but not offered: %+v", on.Fulfillment)
	}
	loc := pm.Destinations[0].Retail
	if loc == nil || loc.ID != "sim_store" || loc.Address == nil || loc.Address.StreetAddress != "1 Sim Street" {
		t.Fatalf("pickup location = %+v, want the store's pickup source", loc)
	}

	checkout, err := conn.CheckoutCreate(ctx, h.storeID, &ucptype.CreateCheckoutRequest{
		LineItems: h.items(1),
		Buyer:     ada,
		Fulfillment: &ucptype.FulfillmentRequest{
			MethodType:            "pickup",
			SelectedDestinationID: "sim_store",
			SelectedMethodID:      "instore_pickup",
		},
	})
	if err != nil {
		t.Fatalf("CheckoutCreate (pickup selected): %v", err)
	}
	if shipping, _ := totalOf(checkout.Totals, model.TotalTypeFulfillment); shipping != 0 {
		t.Errorf("collecting in store must not be charged shipping, got %d", shipping)
	}
	pm = fulfillmentMethod(checkout.Fulfillment, model.FulfillmentMethodTypePickup)
	if pm == nil || pm.SelectedDestinationID == nil || *pm.SelectedDestinationID != "sim_store" || pm.Groups[0].SelectedOptionID == nil {
		t.Fatalf("pickup selection not reflected: %+v", checkout.Fulfillment)
	}
	if sm := fulfillmentMethod(checkout.Fulfillment, model.FulfillmentMethodTypeShipping); sm == nil || len(sm.Destinations) != 0 || sm.Groups[0].SelectedOptionID != nil {
		t.Errorf("a pickup order must not present the pickup address as a shipping destination or rate: %+v", sm)
	}

	// An unknown location is a rejection.
	_, err = conn.CheckoutUpdate(ctx, h.storeID, checkout.ID, &ucptype.UpdateCheckoutRequest{
		Fulfillment: &ucptype.FulfillmentRequest{MethodType: "pickup", SelectedDestinationID: "elsewhere"},
	})
	if !errors.As(err, &rej) || rej.Code != "invalid_destination" {
		t.Errorf("unknown pickup location = %v, want invalid_destination", err)
	}

	completed, err := conn.CheckoutComplete(ctx, h.storeID, checkout.ID, &ucptype.CompleteCheckoutRequest{Buyer: ada})
	if err != nil {
		t.Fatalf("CheckoutComplete (pickup order): %v", err)
	}
	if completed.Status != ucptype.CheckoutStatusCompleted {
		t.Errorf("pickup order status = %q, want completed", completed.Status)
	}
	if pm := fulfillmentMethod(completed.Fulfillment, model.FulfillmentMethodTypePickup); pm == nil || pm.SelectedDestinationID == nil {
		t.Errorf("the completed order should still show pickup selected: %+v", completed.Fulfillment)
	}
}

func TestMagento_Shipping_RatesOfferedForDestination(t *testing.T) {
	h := newMagentoHarness(t, "store-mg-rates-1", 1)
	conn := h.conn()
	ctx := context.Background()

	// A destination without a chosen rate still yields options to choose from.
	created, err := conn.CheckoutCreate(ctx, h.storeID, &ucptype.CreateCheckoutRequest{
		LineItems:   h.items(1),
		Fulfillment: &ucptype.FulfillmentRequest{SelectedDestination: londonAddress},
	})
	if err != nil {
		t.Fatalf("CheckoutCreate: %v", err)
	}
	sm := fulfillmentMethod(created.Fulfillment, model.FulfillmentMethodTypeShipping)
	if sm == nil || len(sm.Destinations) != 1 || len(sm.Groups[0].Options) != 1 || sm.Groups[0].SelectedOptionID != nil {
		t.Fatalf("want one destination and one unselected rate, got %+v", sm)
	}

	// Choosing the rate is reflected and priced.
	chosen, err := conn.CheckoutUpdate(ctx, h.storeID, created.ID, &ucptype.UpdateCheckoutRequest{
		Fulfillment: &ucptype.FulfillmentRequest{SelectedMethodID: sm.Groups[0].Options[0].ID, SelectedDestination: londonAddress},
	})
	if err != nil {
		t.Fatalf("CheckoutUpdate: %v", err)
	}
	sm = fulfillmentMethod(chosen.Fulfillment, model.FulfillmentMethodTypeShipping)
	if sm.Groups[0].SelectedOptionID == nil || *sm.Groups[0].SelectedOptionID != "flatrate_flatrate" {
		t.Fatalf("selected option = %+v", sm.Groups[0].SelectedOptionID)
	}
	if shipping, _ := totalOf(chosen.Totals, model.TotalTypeFulfillment); shipping != 500 {
		t.Errorf("shipping total = %d, want 500", shipping)
	}
}

func TestMagento_Checkout_PaymentTokenForwardedAndDeclineIsRejection(t *testing.T) {
	h := newMagentoHarness(t, "store-mg-pay-1", 1)
	conn := h.conn()
	ctx := context.Background()

	pay := func(token string) *model.Payment {
		return &model.Payment{Instruments: []model.SelectedPaymentInstrument{{
			PaymentInstrument: model.PaymentInstrument{
				ID: "i1", HandlerID: "h", Type: "tokenized_card",
				Credential: &model.PaymentCredential{Type: "token", Token: &model.TokenCredential{Type: "t", Token: token}},
			},
			Selected: true,
		}}}
	}

	prepare := func() string {
		c, err := conn.CheckoutCreate(ctx, h.storeID, &ucptype.CreateCheckoutRequest{
			LineItems:   h.items(1),
			Buyer:       ada,
			Fulfillment: &ucptype.FulfillmentRequest{SelectedMethodID: "flatrate_flatrate", SelectedDestination: londonAddress},
		})
		if err != nil {
			t.Fatalf("CheckoutCreate: %v", err)
		}
		return c.ID
	}

	_, err := conn.CheckoutComplete(ctx, h.storeID, prepare(), &ucptype.CompleteCheckoutRequest{Buyer: ada, Payment: pay("fail_token")})
	var rej *ucptype.CheckoutRejectedError
	if !errors.As(err, &rej) || rej.Code != "payment_failed" {
		t.Fatalf("a declined payment must be a payment_failed rejection (proving the token reached the store), got %v", err)
	}

	completed, err := conn.CheckoutComplete(ctx, h.storeID, prepare(), &ucptype.CompleteCheckoutRequest{Buyer: ada, Payment: pay("tok_ok")})
	if err != nil || completed.Status != ucptype.CheckoutStatusCompleted {
		t.Fatalf("CheckoutComplete with a good token = %+v, %v", completed, err)
	}
}

func TestMagento_Checkout_OutOfStockAndUnknownSKUAreRejections(t *testing.T) {
	h := newMagentoHarness(t, "store-mg-stock-1", 1)
	conn := h.conn()
	ctx := context.Background()

	var rej *ucptype.CheckoutRejectedError

	_, err := conn.CheckoutCreate(ctx, h.storeID, &ucptype.CreateCheckoutRequest{
		LineItems: []ucptype.LineItemCreateRequest{{Item: ucptype.ItemRef{ID: "no-such-sku"}, Quantity: 1}},
	})
	if !errors.As(err, &rej) || rej.Code != "not_found" {
		t.Errorf("unknown SKU = %v, want not_found rejection", err)
	}

	if _, err := h.store.SetStock(h.skus[0], 0); err != nil {
		t.Fatalf("SetStock: %v", err)
	}
	h.store.Products[h.skus[0]].ManageStock = true
	_, err = conn.CheckoutCreate(ctx, h.storeID, &ucptype.CreateCheckoutRequest{LineItems: h.items(1)})
	if !errors.As(err, &rej) || rej.Code != "out_of_stock" {
		t.Errorf("out of stock = %v, want out_of_stock rejection", err)
	}
}
