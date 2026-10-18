// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package woocommerce_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/sarvexlab/sarvex/internal/pkg/commerce/model"
	ucptype "github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
	"github.com/sarvexlab/sarvex/internal/platform/woocommerce"
)

// pickupStore fakes the WooCommerce REST surface pickup detection reads, and
// records order writes so a test can see what the connector sent.
type pickupStore struct {
	mu         sync.Mutex
	pickupOn   bool
	zonesFail  bool
	orderBody  map[string]any
	orderWrite int
}

func (p *pickupStore) handler(t *testing.T) http.Handler {
	t.Helper()
	writeJSON := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/wp-json/wc/v3/shipping/zones", func(w http.ResponseWriter, r *http.Request) {
		if p.zonesFail {
			http.Error(w, `{"message":"boom"}`, http.StatusInternalServerError)
			return
		}
		writeJSON(w, []map[string]any{{"id": 1}})
	})
	mux.HandleFunc("/wp-json/wc/v3/shipping/zones/", func(w http.ResponseWriter, r *http.Request) {
		methods := []map[string]any{}
		if strings.Contains(r.URL.Path, "/zones/1/") {
			methods = append(methods, map[string]any{"method_id": "flat_rate", "enabled": true})
			p.mu.Lock()
			on := p.pickupOn
			p.mu.Unlock()
			methods = append(methods, map[string]any{"method_id": "local_pickup", "enabled": on})
		}
		writeJSON(w, methods)
	})
	mux.HandleFunc("/wp-json/wc/v3/settings/general", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, []map[string]any{
			{"id": "woocommerce_store_address", "value": "1 Main St"},
			{"id": "woocommerce_store_city", "value": "Springfield"},
			{"id": "woocommerce_default_country", "value": "US:CA"},
			{"id": "woocommerce_store_postcode", "value": "94000"},
		})
	})
	mux.HandleFunc("/wp-json/wc/v3/orders", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		p.mu.Lock()
		p.orderBody, p.orderWrite = body, p.orderWrite+1
		p.mu.Unlock()
		order := map[string]any{
			"id": 11, "status": "pending", "billing": map[string]any{}, "shipping": body["shipping"],
			"line_items": []map[string]any{{"id": 1, "product_id": 7, "quantity": 1, "name": "Mug", "total": "10.00"}},
			"total":      "10.00", "subtotal": "10.00", "shipping_total": "0.00", "tax_total": "0.00", "discount_total": "0.00",
		}
		if lines, ok := body["shipping_lines"]; ok {
			order["shipping_lines"] = lines
		}
		writeJSON(w, order)
	})
	return mux
}

func pickupConnector(t *testing.T, p *pickupStore) (*httptest.Server, *woocommerce.Connector) {
	t.Helper()
	server := httptest.NewServer(p.handler(t))
	creds := newFakeCredentialsProvider()
	creds.set("store-1", server.URL, "test-consumer-key", "test-consumer-secret")
	return server, newTestConnector(t, server, creds, fastGuardConfig())
}

var oneMug = []ucptype.LineItemCreateRequest{{Item: ucptype.ItemRef{ID: "7"}, Quantity: 1}}

func TestCheckout_OffersPickupOnlyWhenTheStoreHasIt(t *testing.T) {
	for _, on := range []bool{true, false} {
		p := &pickupStore{pickupOn: on}
		server, conn := pickupConnector(t, p)

		resp, err := conn.CheckoutCreate(context.Background(), "store-1", &ucptype.CreateCheckoutRequest{LineItems: oneMug})
		server.Close()
		if err != nil {
			t.Fatalf("CheckoutCreate(pickup=%v): %v", on, err)
		}
		var pickupMethod *model.FulfillmentMethod
		for i := range resp.Fulfillment.Methods {
			if resp.Fulfillment.Methods[i].Type == model.FulfillmentMethodTypePickup {
				pickupMethod = &resp.Fulfillment.Methods[i]
			}
		}
		if on != (pickupMethod != nil) {
			t.Fatalf("pickup enabled=%v but pickup method offered=%v", on, pickupMethod != nil)
		}
		if on {
			if len(pickupMethod.Destinations) != 1 || pickupMethod.Destinations[0].Retail == nil || pickupMethod.Destinations[0].Retail.ID != "pickup_store" {
				t.Errorf("pickup destinations = %+v, want the one retail location", pickupMethod.Destinations)
			}
			if pickupMethod.SelectedDestinationID != nil {
				t.Errorf("nothing chosen yet, but a destination is selected")
			}
		}
	}
}

func TestCheckout_PickupSelection_RoundTrips(t *testing.T) {
	p := &pickupStore{pickupOn: true}
	server, conn := pickupConnector(t, p)
	defer server.Close()

	resp, err := conn.CheckoutCreate(context.Background(), "store-1", &ucptype.CreateCheckoutRequest{
		LineItems: oneMug,
		Fulfillment: &ucptype.FulfillmentRequest{
			MethodType:            "pickup",
			SelectedDestinationID: "pickup_store",
			SelectedMethodID:      "local_pickup",
		},
	})
	if err != nil {
		t.Fatalf("CheckoutCreate: %v", err)
	}

	lines, _ := p.orderBody["shipping_lines"].([]any)
	var first map[string]any
	if len(lines) > 0 {
		first, _ = lines[0].(map[string]any)
	}
	if len(lines) != 1 || first["method_id"] != "local_pickup" {
		t.Fatalf("store was sent shipping_lines %v, want a local_pickup line", p.orderBody["shipping_lines"])
	}
	ship, _ := p.orderBody["shipping"].(map[string]any)
	if ship["address_1"] != "1 Main St" || ship["city"] != "Springfield" {
		t.Errorf("order shipping address = %v, want the pickup location's", ship)
	}

	var pm, sm *model.FulfillmentMethod
	for i := range resp.Fulfillment.Methods {
		switch resp.Fulfillment.Methods[i].Type {
		case model.FulfillmentMethodTypePickup:
			pm = &resp.Fulfillment.Methods[i]
		case model.FulfillmentMethodTypeShipping:
			sm = &resp.Fulfillment.Methods[i]
		}
	}
	if pm == nil || pm.SelectedDestinationID == nil || *pm.SelectedDestinationID != "pickup_store" ||
		pm.Groups[0].SelectedOptionID == nil || *pm.Groups[0].SelectedOptionID != "local_pickup" {
		t.Fatalf("pickup selection not reflected in the response: %+v", pm)
	}
	if sm != nil && (sm.SelectedDestinationID != nil || sm.Groups[0].SelectedOptionID != nil || len(sm.Destinations) != 0) {
		t.Errorf("a pickup order must not also read as a chosen shipping destination/rate: %+v", sm)
	}
}

func TestCheckout_PickupRejectedWhenStoreDoesNotOfferIt(t *testing.T) {
	p := &pickupStore{pickupOn: false}
	server, conn := pickupConnector(t, p)
	defer server.Close()

	_, err := conn.CheckoutCreate(context.Background(), "store-1", &ucptype.CreateCheckoutRequest{
		LineItems:   oneMug,
		Fulfillment: &ucptype.FulfillmentRequest{MethodType: "pickup", SelectedDestinationID: "pickup_store"},
	})
	var rej *ucptype.CheckoutRejectedError
	if !errors.As(err, &rej) || rej.Code != "pickup_unavailable" {
		t.Fatalf("err = %v, want a pickup_unavailable rejection", err)
	}
	if p.orderWrite != 0 {
		t.Errorf("an order was written despite the rejection")
	}

	// And an unknown location is rejected too, when pickup is on.
	on := &pickupStore{pickupOn: true}
	server2, conn2 := pickupConnector(t, on)
	defer server2.Close()
	_, err = conn2.CheckoutCreate(context.Background(), "store-1", &ucptype.CreateCheckoutRequest{
		LineItems:   oneMug,
		Fulfillment: &ucptype.FulfillmentRequest{MethodType: "pickup", SelectedDestinationID: "somewhere_else"},
	})
	if !errors.As(err, &rej) || rej.Code != "invalid_destination" {
		t.Fatalf("err = %v, want an invalid_destination rejection", err)
	}
}
