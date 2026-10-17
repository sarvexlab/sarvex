// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package simstore

import (
	"errors"
	"testing"
)

func TestSeed_Clean(t *testing.T) {
	s := NewStore("store-1")
	s.Seed(20, false)

	if len(s.Products) != 20 {
		t.Fatalf("want 20 products, got %d", len(s.Products))
	}
	for sku, p := range s.Products {
		if p.PriceMinor <= 0 {
			t.Errorf("clean seed: %s has non-positive price", sku)
		}
		if p.Name == "" {
			t.Errorf("clean seed: %s has empty name", sku)
		}
		if p.InStock && p.StockQuantity != nil && *p.StockQuantity == 0 {
			t.Errorf("clean seed: %s marked in stock with zero quantity", sku)
		}
		if !p.InStock && p.StockQuantity != nil && *p.StockQuantity > 0 {
			t.Errorf("clean seed: %s has an out-of-stock/quantity inconsistency", sku)
		}
	}
}

func TestSeed_Messy_ProducesRealIssues(t *testing.T) {
	s := NewStore("store-1")
	s.Seed(20, true)

	var missingPrice, missingTitle, stockInconsistent int
	for _, p := range s.Products {
		if p.PriceMinor <= 0 {
			missingPrice++
		}
		if p.Name == "" {
			missingTitle++
		}
		if !p.InStock && p.StockQuantity != nil && *p.StockQuantity > 0 {
			stockInconsistent++
		}
	}
	if missingPrice == 0 {
		t.Error("messy seed: expected at least one product with missing price (rule_price_missing)")
	}
	if missingTitle == 0 {
		t.Error("messy seed: expected at least one product with missing title (rule_title_missing)")
	}
	if stockInconsistent == 0 {
		t.Error("messy seed: expected at least one stock inconsistency")
	}
}

func TestSeed_IsIdempotentlyAdditive(t *testing.T) {
	s := NewStore("store-1")
	s.Seed(5, false)
	firstIDs := map[string]int{}
	for sku, p := range s.Products {
		firstIDs[sku] = p.ID
	}

	// A second seed call (e.g. re-running `sim seed`) must not clobber ids
	// of already-seeded SKUs — same SKU pattern means it overwrites in
	// place, which is the desired "extend" behaviour, not duplication.
	s.Seed(5, false)
	if len(s.Products) != 5 {
		t.Fatalf("re-seeding the same count should not duplicate products, got %d", len(s.Products))
	}
}

func TestSetPrice(t *testing.T) {
	s := NewStore("store-1")
	s.Seed(1, false)
	var sku string
	for k := range s.Products {
		sku = k
	}

	p, err := s.SetPrice(sku, 4999)
	if err != nil {
		t.Fatal(err)
	}
	if p.PriceMinor != 4999 {
		t.Errorf("want price 4999, got %d", p.PriceMinor)
	}

	if _, err := s.SetPrice("nope", 100); err == nil {
		t.Error("expected error for unknown sku")
	}
}

func TestSetStock_DerivesInStock(t *testing.T) {
	s := NewStore("store-1")
	s.Seed(1, false)
	var sku string
	for k := range s.Products {
		sku = k
	}

	p, err := s.SetStock(sku, 0)
	if err != nil {
		t.Fatal(err)
	}
	if p.InStock {
		t.Error("quantity 0 should set InStock=false")
	}

	p, err = s.SetStock(sku, 5)
	if err != nil {
		t.Fatal(err)
	}
	if !p.InStock {
		t.Error("quantity 5 should set InStock=true")
	}
}

// Regression test for the conformance suite's checkout.out_of_stock_is_refused
// check: CreateOrder used to place an order for a line item regardless of
// stock, so a checkout for a zero-stock SKU always succeeded with no
// indication anything was wrong.
func TestCreateOrder_RejectsOutOfStockLineItem(t *testing.T) {
	s := NewStore("store-1")
	s.Seed(1, false)
	var p *Product
	for _, prod := range s.Products {
		p = prod
	}

	if _, err := s.SetStock(p.SKU, 0); err != nil {
		t.Fatalf("SetStock: %v", err)
	}

	if _, err := s.CreateOrder(map[int]int{p.ID: 1}, OrderAddress{}, OrderAddress{}, "", nil); !errors.Is(err, ErrOutOfStock) {
		t.Errorf("expected ErrOutOfStock, got %v", err)
	}

	if _, err := s.SetStock(p.SKU, 5); err != nil {
		t.Fatalf("SetStock: %v", err)
	}
	if _, err := s.CreateOrder(map[int]int{p.ID: 1}, OrderAddress{}, OrderAddress{}, "", nil); err != nil {
		t.Errorf("expected the order to succeed once stock is available, got %v", err)
	}
}

// UpdateOrder must apply the same stock check when line items are replaced
// — a checkout that becomes out-of-stock mid-flow (buyer info added after
// stock ran out) must not be silently accepted either.
func TestUpdateOrder_RejectsOutOfStockLineItem(t *testing.T) {
	s := NewStore("store-1")
	s.Seed(1, false)
	var p *Product
	for _, prod := range s.Products {
		p = prod
	}

	order, err := s.CreateOrder(map[int]int{p.ID: 1}, OrderAddress{}, OrderAddress{}, "", nil)
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}

	if _, err := s.SetStock(p.SKU, 0); err != nil {
		t.Fatalf("SetStock: %v", err)
	}

	if _, err := s.UpdateOrder(order.ID, "", map[int]int{p.ID: 1}, nil, nil, nil, nil); !errors.Is(err, ErrOutOfStock) {
		t.Errorf("expected ErrOutOfStock, got %v", err)
	}
}

func TestBreakAttrAndFix(t *testing.T) {
	s := NewStore("store-1")
	s.Seed(1, false)
	var sku string
	for k := range s.Products {
		sku = k
	}

	if _, err := s.BreakAttr(sku, "title"); err != nil {
		t.Fatal(err)
	}
	if s.Products[sku].Name != "" {
		t.Error("BreakAttr(title) should empty the name")
	}

	if _, err := s.Fix(sku, "title"); err != nil {
		t.Fatal(err)
	}
	if s.Products[sku].Name == "" {
		t.Error("Fix(title) should restore a non-empty name")
	}

	if _, err := s.BreakAttr(sku, "not_a_real_field"); err == nil {
		t.Error("expected error for a field the rubric can't evaluate")
	}
}

func TestBreakAttrAndFix_Currency(t *testing.T) {
	s := NewStore("store-1")
	s.Seed(1, false)
	var sku string
	for k := range s.Products {
		sku = k
	}

	if _, err := s.BreakAttr(sku, "currency"); err != nil {
		t.Fatal(err)
	}
	if s.Products[sku].Currency != "" {
		t.Error("BreakAttr(currency) should empty the currency")
	}

	if _, err := s.Fix(sku, "currency"); err != nil {
		t.Fatal(err)
	}
	if s.Products[sku].Currency == "" {
		t.Error("Fix(currency) should restore a non-empty currency")
	}
}

func TestFix_Price(t *testing.T) {
	s := NewStore("store-1")
	s.Seed(1, false)
	var sku string
	for k := range s.Products {
		sku = k
	}
	if _, err := s.SetPrice(sku, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Fix(sku, "price"); err != nil {
		t.Fatal(err)
	}
	if s.Products[sku].PriceMinor <= 0 {
		t.Error("Fix(price) should restore a positive price")
	}
}
