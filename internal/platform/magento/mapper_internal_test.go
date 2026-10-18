// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package magento

import (
	"testing"

	"github.com/sarvexlab/sarvex/internal/pkg/commerce/model"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
)

func TestPriceToMinorUnits_Rounds(t *testing.T) {
	// 19.99*100 is 1998.999… in floating point; truncating would lose a cent.
	for price, want := range map[float64]int64{19.99: 1999, 0.29: 29, 0: 0, 1234.56: 123456} {
		if got := priceToMinorUnits(price); got != want {
			t.Errorf("priceToMinorUnits(%v) = %d, want %d", price, got, want)
		}
	}
}

func TestOrderTotals_DiscountIsNegativeAndOptional(t *testing.T) {
	find := func(ts model.Totals, typ model.TotalType) (int64, bool) {
		for _, x := range ts {
			if x.Type == typ {
				return int64(x.Amount), true
			}
		}
		return 0, false
	}

	// Magento reports a discount as a negative number; some versions positive.
	for _, discount := range []float64{-10, 10} {
		d, ok := find(orderTotals(100, 0, 5, discount, 95), model.TotalTypeDiscount)
		if !ok || d != -1000 {
			t.Errorf("discount %v → %d (present=%v), want -1000", discount, d, ok)
		}
	}
	if _, ok := find(orderTotals(100, 0, 5, 0, 105), model.TotalTypeDiscount); ok {
		t.Error("a zero discount must not produce a discount entry (the spec types it strictly negative)")
	}
}

func TestCartTotals_NilTotalsFallBackToLineItems(t *testing.T) {
	cart := &mgCart{Items: []mgCartItem{{ItemID: 1, SKU: "a", Qty: 2, Price: 10.5}}}
	totals := mapper{}.cartTotals(cart)
	if len(totals) != 2 || int64(totals[0].Amount) != 2100 || int64(totals[1].Amount) != 2100 {
		t.Fatalf("totals = %+v, want subtotal and total of 2100", totals)
	}
}

func TestPickupFromSources(t *testing.T) {
	src := func(code string, enabled, pickup bool) mgSource {
		return mgSource{SourceCode: code, Name: code, Enabled: enabled, Country: "US", City: "Austin",
			ExtensionAttributes: &mgSourceExtension{IsPickupLocationActive: pickup}}
	}

	if info := pickupFromSources(nil); info.Enabled {
		t.Error("no sources: pickup must be off")
	}
	if info := pickupFromSources([]mgSource{src("a", true, false), src("b", false, true)}); info.Enabled {
		t.Error("only inactive or disabled sources: pickup must be off")
	}
	info := pickupFromSources([]mgSource{src("z", true, true), src("m", true, true), src("a", true, false)})
	if !info.Enabled || info.Location == nil || info.Location.ID != "m" {
		t.Fatalf("want the first active pickup source by code (m), got %+v", info)
	}
}

func TestExtractErrorMessage_SubstitutesParameters(t *testing.T) {
	cases := map[string]string{
		`{"message":"Product %1 is out of stock","parameters":["ABC"]}`:                                         "Product ABC is out of stock",
		`{"message":"No such entity with %fieldName = %value","parameters":{"fieldName":"cartId","value":"7"}}`: "No such entity with cartId = 7",
		`{"message":"plain"}`: "plain",
		`not json`:            "not json",
	}
	for body, want := range cases {
		if got := extractErrorMessage(body); got != want {
			t.Errorf("extractErrorMessage(%s) = %q, want %q", body, got, want)
		}
	}
}

func TestRejectionCode(t *testing.T) {
	for msg, want := range map[string]string{
		"The requested qty is not available":                   "out_of_stock",
		"Product that you are trying to add is not available.": "out_of_stock",
		"Could not find a product with SKU x":                  "not_found",
		"Payment failed":                                       "payment_failed",
		"The shipping method is missing.":                      "field_required",
		"something else":                                       "",
	} {
		if got := rejectionCode(msg); got != want {
			t.Errorf("rejectionCode(%q) = %q, want %q", msg, got, want)
		}
	}
}

func TestOrderStatusToCheckoutStatus(t *testing.T) {
	m := mapper{}
	for status, want := range map[string]base.CheckoutStatus{
		"complete": base.CheckoutStatusCompleted, "processing": base.CheckoutStatusCompleted, "closed": base.CheckoutStatusCompleted,
		"canceled": base.CheckoutStatusCanceled, "cancelled": base.CheckoutStatusCanceled, // Magento spells it "canceled"
		"pending": base.CheckoutStatusIncomplete, "holded": base.CheckoutStatusIncomplete,
	} {
		if got := m.orderStatusToCheckoutStatus(status); got != want {
			t.Errorf("orderStatusToCheckoutStatus(%q) = %q, want %q", status, got, want)
		}
	}
}
