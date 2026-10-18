// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package woocommerce

import "testing"

func TestProductToCatalogEntity_CarriesStoreData(t *testing.T) {
	p := wcProduct{
		SKU: "S1", Name: "Mug", Type: "simple", Slug: "mug", Price: "12.50",
		Description: "<p>A mug</p>", InStock: true, StockStatus: "instock",
		Weight:     "0.4",
		Dimensions: wcDims{Length: "10", Width: "8", Height: "9"},
		Categories: []wcTerm{{ID: 3, Name: "Kitchen"}, {ID: 4, Name: "Gifts"}},
		Images:     []wcImage{{Src: "https://shop.test/a.jpg"}, {Src: ""}, {Src: "https://shop.test/b.jpg"}},
		Attributes: []wcProductAttribute{
			{Name: "Colour", Options: []string{"Red", "Blue"}},
			{Name: "type", Options: []string{"shadowed"}}, // must not override the product type
			{Name: "Empty"},
		},
	}
	e := productToCatalogEntity(p, "USD")

	if e.Description != "<p>A mug</p>" || e.Category != "Kitchen" {
		t.Errorf("description/category = %q/%q", e.Description, e.Category)
	}
	if len(e.Images) != 2 || e.Images[0] != "https://shop.test/a.jpg" {
		t.Errorf("images = %v", e.Images)
	}
	want := map[string]string{
		"type": "simple", "slug": "mug", "weight": "0.4", "dimensions": "10x8x9", "Colour": "Red, Blue",
	}
	for k, v := range want {
		if e.RawAttributes[k] != v {
			t.Errorf("raw[%q] = %q, want %q", k, e.RawAttributes[k], v)
		}
	}
	if _, ok := e.RawAttributes["Empty"]; ok {
		t.Error("an attribute with no options must not be passed through")
	}
}

func TestProductToCatalogEntity_Gaps(t *testing.T) {
	e := productToCatalogEntity(wcProduct{
		SKU: "S2", ShortDescription: "short only",
		Dimensions: wcDims{Length: "10", Width: "8"}, // height unset: cannot ship against it
	}, "USD")

	if e.Description != "short only" {
		t.Errorf("description should fall back to the short description, got %q", e.Description)
	}
	if _, ok := e.RawAttributes["dimensions"]; ok {
		t.Error("partial dimensions must be omitted, not half-rendered")
	}
	if _, ok := e.RawAttributes["weight"]; ok {
		t.Error("an unset weight must be omitted")
	}
	if len(e.Images) != 0 {
		t.Errorf("no images expected, got %v", e.Images)
	}
}
