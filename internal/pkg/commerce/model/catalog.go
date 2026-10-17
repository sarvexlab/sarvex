// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package model

// CatalogQuery is a catalog search request in Sarvex's own terms: what every
// served generation's search_request asks for that the serving cache can
// answer. Each generation's codec reads its own request shape into it.
type CatalogQuery struct {
	// Query is the free-text search.
	Query string
	// Cursor is an opaque cursor from a previous page; empty for the first.
	Cursor string
	// Limit is the requested page size; 0 when unset.
	Limit int
	// PriceMin and PriceMax bound the price in minor units; nil when unset.
	PriceMin, PriceMax *int64
}

// CatalogProduct is one product as the serving cache holds it: a single SKU
// with its price and stock. Each generation renders it as its own Product.
type CatalogProduct struct {
	SKU         string
	Title       string
	Description string
	PriceMinor  int64
	Currency    string
	InStock     bool
}

// CatalogPage is one page of a catalog search.
type CatalogPage struct {
	Products []CatalogProduct
	// Cursor continues after this page; empty on the last.
	Cursor  string
	HasMore bool
}
