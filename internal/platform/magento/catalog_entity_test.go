// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package magento

import (
	"strings"
	"testing"
)

func TestProductToCatalogEntity_CarriesStoreData(t *testing.T) {
	p := mgProduct{
		SKU: "S1", Name: "Mug", TypeID: "simple", Price: 12.5, Status: 1, Weight: 0.4,
		CustomAttributes: []mgCustomAttribute{
			{AttributeCode: "description", Value: "A mug"},
			{AttributeCode: "short_description", Value: "short"},
			{AttributeCode: "url_key", Value: "mug"},
			{AttributeCode: "ts_dimensions_length", Value: "10"},
			{AttributeCode: "ts_dimensions_width", Value: "8"},
			{AttributeCode: "ts_dimensions_height", Value: "9"},
			{AttributeCode: "color", Value: "Red"},
			{AttributeCode: "meta_title", Value: "ignored"},
			{AttributeCode: "category_ids", Value: []interface{}{"1", "2"}},
			{AttributeCode: "novelty", Value: strings.Repeat("x", maxRawAttributeLen+1)},
		},
		MediaGalleryEntries: []mgMediaGalleryEntry{
			{MediaType: "image", URL: "/m/u/mug.jpg"},
			{MediaType: "external-video", URL: "/v.mp4"},
		},
	}
	e := productToCatalogEntity(p, "USD", "https://shop.test/")

	if e.Description != "A mug" {
		t.Errorf("description = %q", e.Description)
	}
	if len(e.Images) != 1 || e.Images[0] != "https://shop.test/media/catalog/product/m/u/mug.jpg" {
		t.Errorf("images = %v", e.Images)
	}
	want := map[string]string{"type_id": "simple", "weight": "0.4", "dimensions": "10x8x9", "color": "Red"}
	for k, v := range want {
		if e.RawAttributes[k] != v {
			t.Errorf("raw[%q] = %q, want %q", k, e.RawAttributes[k], v)
		}
	}
	for _, k := range []string{"description", "short_description", "url_key", "meta_title", "category_ids", "novelty", "ts_dimensions_length"} {
		if _, ok := e.RawAttributes[k]; ok {
			t.Errorf("raw attributes must not carry %q", k)
		}
	}
}

func TestProductToCatalogEntity_Gaps(t *testing.T) {
	e := productToCatalogEntity(mgProduct{
		SKU: "S2",
		CustomAttributes: []mgCustomAttribute{
			{AttributeCode: "short_description", Value: "short only"},
			{AttributeCode: "ts_dimensions_length", Value: "10"},
		},
	}, "USD", "https://shop.test")

	if e.Description != "short only" {
		t.Errorf("description should fall back to short_description, got %q", e.Description)
	}
	if _, ok := e.RawAttributes["dimensions"]; ok {
		t.Error("partial dimensions must be omitted")
	}
	if _, ok := e.RawAttributes["weight"]; ok {
		t.Error("a zero/unset weight must be omitted")
	}
	if len(e.Images) != 0 {
		t.Errorf("no images expected, got %v", e.Images)
	}
}
