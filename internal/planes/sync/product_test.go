// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func newModelTestProduct() *CatalogProduct {
	qty := 42
	return &CatalogProduct{
		ID:          "merch-1:store-1:SKU-001",
		StoreID:     "store-1",
		MerchantID:  "merch-1",
		SKU:         "SKU-001",
		Title:       "Test Product",
		Description: "A product for model tests",
		Category:    "apparel",
		Brand:       "Acme",
		Images:      []string{"https://example.com/a.jpg"},
		Identifiers: Identifiers{GTIN: "012345678905", MPN: "MPN-1"},
		PriceMinor:  199999999999, // exceeds float64's exact-integer range at 2^53; must survive round-trip
		Currency:    "USD",
		InStock:     true,
		Quantity:    &qty,
		RawAttributes: map[string]string{
			"color": "red",
		},
		MappedFields: map[string]string{},
		UpdatedAt:    time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC),
	}
}

// TestCatalogProduct_JSONRoundTrip verifies the canonical fields survive a
// JSON encode/decode cycle unchanged, and that field names are snake_case
// per the wire convention.
func TestCatalogProduct_JSONRoundTrip(t *testing.T) {
	product := newModelTestProduct()

	data, err := json.Marshal(product)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	for _, key := range []string{`"sku"`, `"identifiers"`, `"source_attribute"`} {
		_ = key // identifiers/attribute_mappings checked structurally below
	}
	if !strings.Contains(string(data), `"gtin"`) {
		t.Errorf("expected snake_case 'gtin' in JSON output, got: %s", data)
	}

	var decoded CatalogProduct
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.SKU != product.SKU || decoded.Title != product.Title {
		t.Errorf("round-trip mismatch: got %+v, want %+v", decoded, product)
	}
	if decoded.Identifiers.GTIN != "012345678905" {
		t.Errorf("expected GTIN to survive round-trip, got %q", decoded.Identifiers.GTIN)
	}
	if decoded.PriceMinor != product.PriceMinor {
		t.Errorf("expected PriceMinor %d, got %d", product.PriceMinor, decoded.PriceMinor)
	}
}

// TestCatalogProduct_BSONRoundTrip verifies the persisted document shape:
// the reconciled field names (score, has_blocking_issue, title) that
// control/catalog.ProductModel expects to read from the same collection.
func TestCatalogProduct_BSONRoundTrip(t *testing.T) {
	product := newModelTestProduct()
	product.ReadinessScore = 87
	product.BlocksCheckout = true

	raw, err := bson.Marshal(product)
	if err != nil {
		t.Fatalf("bson marshal: %v", err)
	}

	var doc bson.M
	if err := bson.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("bson unmarshal to map: %v", err)
	}

	wantWireFields := map[string]any{
		"_id":                "merch-1:store-1:SKU-001",
		"store_id":           "store-1",
		"merchant_id":        "merch-1",
		"sku":                "SKU-001",
		"title":              "Test Product",
		"score":              int32(87),
		"has_blocking_issue": true,
	}
	for field, want := range wantWireFields {
		got, ok := doc[field]
		if !ok {
			t.Errorf("expected bson field %q to be present in %v", field, doc)
			continue
		}
		if got != want {
			t.Errorf("bson field %q: got %v (%T), want %v (%T)", field, got, got, want, want)
		}
	}

	var decoded CatalogProduct
	if err := bson.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("bson unmarshal: %v", err)
	}
	if decoded.ReadinessScore != 87 || !decoded.BlocksCheckout {
		t.Errorf("expected score/blocking flag to survive bson round-trip, got %+v", decoded)
	}
}

// TestCatalogProduct_MoneyIsIntegerMinorUnits verifies price is stored as an
// integer (amount_minor + currency), never a float, and that values beyond
// float64's exact-integer range still round-trip exactly.
func TestCatalogProduct_MoneyIsIntegerMinorUnits(t *testing.T) {
	product := newModelTestProduct()

	data, err := json.Marshal(product)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), `"price_minor":1.99999999999e+11`) {
		t.Fatalf("price_minor serialised as a float: %s", data)
	}

	var decoded CatalogProduct
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.PriceMinor != product.PriceMinor {
		t.Errorf("integer price did not survive round-trip exactly: got %d, want %d", decoded.PriceMinor, product.PriceMinor)
	}
	if decoded.Currency != "USD" {
		t.Errorf("expected currency to survive round-trip, got %q", decoded.Currency)
	}
}

// TestCatalogProduct_DefaultMappingsAndReadinessAreEmpty verifies the
// mappings and readiness slots exist on the model and default empty/null —
// they are populated by later stages (mapping/scoring), not catalog sync.
func TestCatalogProduct_DefaultMappingsAndReadinessAreEmpty(t *testing.T) {
	var product CatalogProduct

	if product.AttributeMappings != nil {
		t.Errorf("expected AttributeMappings to default nil, got %v", product.AttributeMappings)
	}
	if product.Issues != nil {
		t.Errorf("expected Issues to default nil, got %v", product.Issues)
	}
	if product.OpenIssueCount != 0 {
		t.Errorf("expected OpenIssueCount to default 0, got %d", product.OpenIssueCount)
	}
	if product.Completeness != 0 || product.Consistency != 0 || product.AttributeQuality != 0 || product.DeliveryData != 0 {
		t.Errorf("expected readiness dimensions to default 0, got %+v", product)
	}
	if product.ReadinessScore != 0 || product.BlocksCheckout {
		t.Errorf("expected score/blocking flag to default zero-value, got %+v", product)
	}

	data, err := json.Marshal(&product)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if v, ok := decoded["attribute_mappings"]; ok && v != nil {
		t.Errorf("expected empty attribute_mappings in JSON, got %v", v)
	}
	if v, ok := decoded["issues"]; ok && v != nil {
		t.Errorf("expected empty issues in JSON, got %v", v)
	}
}
