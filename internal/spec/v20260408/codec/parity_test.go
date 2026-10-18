// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package codec_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/sarvexlab/sarvex/internal/pkg/commerce/model"
	v20260408 "github.com/sarvexlab/sarvex/internal/spec/v20260408"
	consent04 "github.com/sarvexlab/sarvex/internal/spec/v20260408/extensions/buyerconsent"
	discount04 "github.com/sarvexlab/sarvex/internal/spec/v20260408/extensions/discount"
)

// This codec renders the canonical model by adding only an envelope, which is
// correct exactly while the model's JSON is 2026-04-08's wire JSON. The model
// is Sarvex's own copy of those shapes, so this test pins that every part of
// a canonical response reads back through 2026-04-08's own typed schema and
// re-encodes byte-for-byte. A model change that 2026-04-08 cannot carry fails
// here, and belongs in this codec as a real translation.
func TestCanonicalModelIsThe20260408WireForm(t *testing.T) {
	yes, no := true, false
	amount := model.Amount(1999)
	parts := []struct {
		name  string
		model any
		wire  any
	}{
		{"line_items", []model.LineItem{{ID: "li_1", Quantity: 2, Item: model.Item{ID: "SKU-1", Title: "Mug", Price: &amount},
			Totals: model.Totals{{Type: model.TotalTypeSubtotal, Amount: 3998}, {Type: model.TotalTypeTotal, Amount: 3998}}}}, &[]v20260408.LineItem{}},
		{"totals", model.Totals{{Type: model.TotalTypeDiscount, Amount: -400}, {Type: model.TotalTypeTax, Amount: 120}, {Type: model.TotalTypeTotal, Amount: 3718}}, &v20260408.Totals{}},
		{"buyer", &model.BuyerWithConsent{Buyer: model.Buyer{Email: "ada@example.com", FirstName: "Ada"},
			Consent: &model.Consent{Marketing: &yes, SaleOfData: &no}}, &consent04.BuyerWithConsent{}},
		{"discounts", &model.DiscountsObject{Codes: []string{"SPRING20"}, Applied: []model.AppliedDiscount{{Code: "SPRING20", Title: "SPRING20", Amount: 400}}}, &discount04.DiscountsObject{}},
		{"fulfillment", &model.Fulfillment{Methods: []model.FulfillmentMethod{{
			ID: "m1", Type: model.FulfillmentMethodTypePickup, LineItemIDs: []string{"li_1"},
			Destinations: []model.FulfillmentDestination{{Retail: &model.RetailLocation{ID: "store", Name: "Shop"}}},
			Groups:       []model.FulfillmentGroup{{ID: "g1", LineItemIDs: []string{"li_1"}, Options: []model.FulfillmentOption{{ID: "pickup", Title: "Pickup", Totals: model.Totals{{Type: model.TotalTypeTotal, Amount: 0}}}}}},
		}}}, &v20260408.Fulfillment{}},
		{"messages", []model.Message{
			model.NewErrorMessage(model.MessageError{Code: "out_of_stock", Content: "Gone", Severity: model.MessageSeverityRecoverable, Path: "$.line_items[0]"}),
			model.NewInfoMessage(model.MessageInfo{Content: "Note"}),
		}, &[]v20260408.Message{}},
		{"order", &model.Order{ID: "ord_1", CheckoutID: "co_1", PermalinkURL: "https://shop.example/o/1", Currency: "USD",
			Totals: model.Totals{{Type: model.TotalTypeTotal, Amount: 3718}}}, &v20260408.Order{}},
	}
	for _, p := range parts {
		t.Run(p.name, func(t *testing.T) {
			canonical, err := json.Marshal(p.model)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(canonical, p.wire); err != nil {
				t.Fatalf("2026-04-08 cannot read the canonical %s: %v\n%s", p.name, err, canonical)
			}
			wire, err := json.Marshal(reflect.ValueOf(p.wire).Elem().Interface())
			if err != nil {
				t.Fatal(err)
			}
			// The envelope is the codec's (wireadapt.WithEnvelope), never the model's.
			wire = bytes.Replace(wire, []byte(`"ucp":{"version":""},`), nil, 1)
			if string(wire) != string(canonical) {
				t.Errorf("canonical %s is not its 2026-04-08 wire form\n model: %s\n  wire: %s", p.name, canonical, wire)
			}
		})
	}
}
