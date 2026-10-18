// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/sarvexlab/sarvex/internal/pkg/commerce/model"
	"github.com/sarvexlab/sarvex/internal/planes/serving"
)

// catalogPage is a cache search result as the canonical page every
// generation's codec renders, with the query's price filter applied.
//
// The filter is applied here, after the cache query, and only its price bounds
// are honored: CacheProduct (the serving cache's projection) carries no
// category data, so a categories filter has nothing to check against.
// Silently claiming to filter by a criterion the cache can't represent would
// be worse than not filtering — it would look like "zero results in this
// category" when really no filtering happened. Categories are accepted on the
// wire (each codec reads the request against its schema) and simply not
// applied; closing that gap for real needs a serving-cache schema change, not
// an HTTP-layer one. Because filtering happens after the cache's own
// limit/cursor pagination, a filtered page may legitimately hold fewer than
// the requested limit — the cursor and has-more still reflect the underlying
// (pre-filter) page boundary, which is correct: the next page still starts
// where the cache left off.
func catalogPage(result *serving.CatalogSearchResult, q model.CatalogQuery) model.CatalogPage {
	items := filterByPriceRange(result.Items, q.PriceMin, q.PriceMax)
	products := make([]model.CatalogProduct, len(items))
	for i, p := range items {
		products[i] = catalogProduct(p)
	}
	return model.CatalogPage{Products: products, Cursor: result.NextCursor, HasMore: result.HasMore}
}

// catalogProduct is a serving-cache product in canonical form.
func catalogProduct(p *serving.CacheProduct) model.CatalogProduct {
	return model.CatalogProduct{
		SKU:         p.SKU,
		Title:       p.Title,
		Description: p.Description,
		PriceMinor:  int64(p.Price.AmountMinor),
		Currency:    p.Price.Currency,
		InStock:     p.Stock.InStock,
	}
}

// filterByPriceRange drops items outside [minAmount, maxAmount] (either bound
// optional). Amounts are minor units, matching CacheProduct.Price.AmountMinor
// directly — no currency conversion or cross-currency comparison is attempted
// (Sarvex is single-currency per store today).
func filterByPriceRange(items []*serving.CacheProduct, minAmount, maxAmount *int64) []*serving.CacheProduct {
	if minAmount == nil && maxAmount == nil {
		return items
	}
	out := make([]*serving.CacheProduct, 0, len(items))
	for _, p := range items {
		amount := int64(p.Price.AmountMinor)
		if minAmount != nil && amount < *minAmount {
			continue
		}
		if maxAmount != nil && amount > *maxAmount {
			continue
		}
		out = append(out, p)
	}
	return out
}

// mapCatalogError translates a serving.CatalogService error into an HTTP status.
func (h *servingHandler) mapCatalogError(err error) error {
	switch {
	case errors.Is(err, serving.ErrNotFound):
		return echo.NewHTTPError(http.StatusNotFound, "product not found")
	case errors.Is(err, serving.ErrInvalidCursor):
		// A cursor the cache cannot read is the caller's mistake, and
		// saying so is the point: this used to be swallowed, restarting
		// silently from page one, which an agent had no way to detect.
		return echo.NewHTTPError(http.StatusBadRequest, "invalid pagination cursor")
	default:
		h.logger.Error("catalog request failed", "error", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "catalog request failed")
	}
}
