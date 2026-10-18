// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package agent

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// catalogLookupBody is the body GET /catalog/{itemId} actually returns: a
// reconciled ucp.dev Product, as built by the serving plane's
// version codecs (internal/spec/vYYYYMMDD/codec/catalog.go).
//
// Written out as literal JSON rather than built from the serving plane's
// types on purpose — this package is an HTTP client and does not import a
// plane's internals (see Agent's doc comment). The cost of that is exactly
// the drift this test exists to catch: CatalogItem once mirrored
// serving.CacheProduct (flat description/price/stock) instead of the wire
// Product, so every lookup failed to decode with
//
//	json: cannot unmarshal object into Go struct field
//	CatalogItem.description of type string
//
// and the conformance suite reported it as a product defect. If this
// literal and the server ever disagree again, the suite is wrong about the
// product until someone notices.
const catalogLookupBody = `{
  "id": "SIM-abc-0005",
  "title": "Trail Runner",
  "description": {"plain": "A shoe."},
  "price_range": {
    "min": {"amount": 12900, "currency": "USD"},
    "max": {"amount": 12900, "currency": "USD"}
  },
  "variants": [{
    "id": "SIM-abc-0005",
    "sku": "SIM-abc-0005",
    "title": "Trail Runner",
    "description": {"plain": "A shoe."},
    "price": {"amount": 12900, "currency": "USD"},
    "availability": {"available": true, "status": "in_stock"}
  }]
}`

func TestCatalogItemDecodesTheLookupWireShape(t *testing.T) {
	var item CatalogItem
	require.NoError(t, json.Unmarshal([]byte(catalogLookupBody), &item),
		"CatalogItem no longer decodes what GET /catalog/{itemId} returns")

	assert.Equal(t, "SIM-abc-0005", item.SKU())
	assert.Equal(t, "Trail Runner", item.Title)
	assert.Equal(t, "A shoe.", item.Description.Plain)
	assert.True(t, item.InStock())

	price := item.Price()
	assert.Equal(t, int64(12900), price.Amount)
	assert.Equal(t, "USD", price.Currency)
}

// Sarvex renders one variant per cached product today, but the accessors
// must not assume it: an agent reading a variantless product should get a
// usable price from the range and must never be told an item is in stock
// on no evidence.
func TestCatalogItemAccessorsWithoutVariants(t *testing.T) {
	item := CatalogItem{ID: "SKU-1"}
	item.PriceRange.Min = Money{Amount: 500, Currency: "GBP"}

	assert.Equal(t, "SKU-1", item.SKU(), "with no variant the product id is the SKU")
	assert.Equal(t, int64(500), item.Price().Amount, "with no variant the range's floor is the price")
	assert.False(t, item.InStock(), "a product with no variant must not report itself in stock")
}

// An availability block that is absent is not a yes. Treating it as one
// would have an agent add an unpurchasable item to a cart.
func TestCatalogItemWithoutAvailabilityIsNotInStock(t *testing.T) {
	item := CatalogItem{Variants: []Variant{{SKU: "SKU-1"}}}
	assert.False(t, item.InStock())
}

// The featured variant's price wins over the product-level range, so a
// multi-variant product reports what would actually be charged.
func TestCatalogItemPrefersTheVariantPrice(t *testing.T) {
	item := CatalogItem{
		Variants: []Variant{{SKU: "SKU-1", Price: Money{Amount: 999, Currency: "USD"}}},
	}
	item.PriceRange.Min = Money{Amount: 100, Currency: "USD"}

	assert.Equal(t, int64(999), item.Price().Amount)
}
