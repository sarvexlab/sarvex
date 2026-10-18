// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"io"
	"log/slog"
	"testing"
)

func TestConsistencyEngine_ConsistentProductHasNoIssues(t *testing.T) {
	engine := NewConsistencyEngine(testLogger())
	product := &CatalogProduct{SKU: "sku1", StoreID: "store1", PriceMinor: 1000, Currency: "USD", InStock: true, Quantity: ptrInt(10)}

	issues, err := engine.CheckConsistency(context.Background(), product)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 0 {
		t.Errorf("expected no issues, got %+v", issues)
	}
}

// A price the merchant changed is not an inconsistency, whatever the size of
// the change. The engine judges the product alone: it has no earlier copy to
// compare with, so nothing about the price (or a quantity swing) can be flagged
// — a sale must not mark a product blocking.
func TestConsistencyEngine_PriceAndQuantityValuesAreNotJudged(t *testing.T) {
	engine := NewConsistencyEngine(testLogger())
	for _, price := range []int64{0, 1, 100, 1_000_000} {
		product := &CatalogProduct{SKU: "sku1", StoreID: "store1", PriceMinor: price, Currency: "USD", InStock: true, Quantity: ptrInt(1)}
		issues, _ := engine.CheckConsistency(context.Background(), product)
		for _, issue := range issues {
			if issue.Field == "price" {
				t.Errorf("price %d: unexpected price issue %+v", price, issue)
			}
		}
	}
}

// Stock: only a tracked quantity can contradict the in-stock flag. A nil
// quantity is a store that does not track stock — its normal state.
func TestConsistencyEngine_InventoryContradictions(t *testing.T) {
	engine := NewConsistencyEngine(testLogger())
	cases := []struct {
		name    string
		p       *CatalogProduct
		anomaly bool
	}{
		{"in stock, untracked quantity", &CatalogProduct{InStock: true, Quantity: nil}, false},
		{"in stock, tracked and positive", &CatalogProduct{InStock: true, Quantity: ptrInt(4)}, false},
		{"in stock, tracked quantity zero", &CatalogProduct{InStock: true, Quantity: ptrInt(0)}, true},
		{"in stock, tracked negative", &CatalogProduct{InStock: true, Quantity: ptrInt(-2)}, true},
		{"out of stock, tracked positive", &CatalogProduct{InStock: false, Quantity: ptrInt(4)}, true},
		{"out of stock, untracked", &CatalogProduct{InStock: false, Quantity: nil}, false},
		{"out of stock, tracked zero", &CatalogProduct{InStock: false, Quantity: ptrInt(0)}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			issues, err := engine.CheckConsistency(context.Background(), tc.p)
			if err != nil {
				t.Fatal(err)
			}
			if (len(issues) > 0) != tc.anomaly {
				t.Fatalf("anomaly = %v, want %v (%+v)", len(issues) > 0, tc.anomaly, issues)
			}
			for _, i := range issues {
				if i.Field != "inventory" || i.Type != "anomaly" {
					t.Errorf("unexpected issue %+v", i)
				}
			}
		})
	}
}

func TestConsistencyEngine_MarkIssuesOnProduct(t *testing.T) {
	engine := NewConsistencyEngine(testLogger())

	issues := []ConsistencyIssue{
		{Field: "price", Type: "divergence", Description: "Price spike", Severity: "warning"},
		{Field: "inventory", Type: "anomaly", Description: "Out of sync", Severity: "critical"},
	}
	product := &CatalogProduct{ID: "store1:sku1", Findings: make([]*Finding, 0)}

	engine.MarkConsistencyIssuesOnProduct(product, issues)

	if len(product.Findings) != 2 {
		t.Errorf("expected 2 findings, got %d", len(product.Findings))
	}
	for _, finding := range product.Findings {
		if finding.RequiredFor != "consistency_check" {
			t.Errorf("expected RequiredFor='consistency_check', got %s", finding.RequiredFor)
		}
	}
}

// Helper functions
func ptrInt(i int) *int {
	return &i
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
