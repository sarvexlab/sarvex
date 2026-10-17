// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"testing"
	"time"
)

func TestProductTransformer(t *testing.T) {
	transformer := NewDefaultProductTransformer()

	product := &CatalogProduct{
		ID:            "merchant-1:SKU-001",
		MerchantID:    "merchant-1",
		SKU:           "SKU-001",
		Title:         "Test Product",
		PriceMinor:    1999,
		Currency:      "USD",
		InStock:       true,
		Quantity:      nil,
		RawAttributes: map[string]string{"color": "red"},
		MappedFields:  map[string]string{"color": "red", "size": "L"},
		UpdatedAt:     time.Now(),
		Findings:      make([]*Finding, 0),
	}

	cacheProduct, err := transformer.Transform(product)
	if err != nil {
		t.Fatalf("transform failed: %v", err)
	}

	if cacheProduct.SKU != "SKU-001" {
		t.Errorf("expected SKU SKU-001, got %s", cacheProduct.SKU)
	}
	if cacheProduct.Title != "Test Product" {
		t.Errorf("expected title Test Product, got %s", cacheProduct.Title)
	}
	if cacheProduct.Price.AmountMinor != 1999 {
		t.Errorf("expected price 1999, got %d", cacheProduct.Price.AmountMinor)
	}
	if !cacheProduct.Stock.InStock {
		t.Errorf("expected InStock true, got false")
	}
	if cacheProduct.Attributes["color"] != "red" {
		t.Errorf("expected color red, got %s", cacheProduct.Attributes["color"])
	}
}
