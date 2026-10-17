// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package codec

import (
	"encoding/json"

	"github.com/sarvexlab/sarvex/internal/pkg/commerce/model"
	v20260825 "github.com/sarvexlab/sarvex/internal/spec/v20260825"
)

// DecodeCatalogSearch implements wireadapt.Codec: a 2026-08-25 search_request
// (dev.ucp.shopping.catalog.search). Categories are accepted but carry nothing
// the serving cache can filter on, so they are not part of the query.
func (Codec) DecodeCatalogSearch(body []byte) (model.CatalogQuery, error) {
	var req v20260825.CatalogSearchRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return model.CatalogQuery{}, err
	}
	q := model.CatalogQuery{Query: req.Query}
	if req.Pagination != nil {
		q.Cursor = req.Pagination.Cursor
		if req.Pagination.Limit != nil {
			q.Limit = *req.Pagination.Limit
		}
	}
	if req.Filters != nil && req.Filters.Price != nil {
		q.PriceMin = minorUnits(req.Filters.Price.Min)
		q.PriceMax = minorUnits(req.Filters.Price.Max)
	}
	return q, nil
}

func minorUnits(a *v20260825.Amount) *int64 {
	if a == nil {
		return nil
	}
	v := int64(*a)
	return &v
}

// EncodeCatalogSearch implements wireadapt.Codec: a 2026-08-25
// search_response. Its "ucp" is left zero-value; the negotiation middleware
// sets it from the negotiated result.
func (Codec) EncodeCatalogSearch(page model.CatalogPage) ([]byte, error) {
	products := make([]v20260825.Product, len(page.Products))
	for i, p := range page.Products {
		products[i] = product(p)
	}
	return json.Marshal(&v20260825.CatalogSearchResponse{
		Products: products,
		Pagination: &v20260825.PaginationResponse{
			Cursor:      page.Cursor,
			HasNextPage: page.HasMore,
		},
	})
}

// EncodeCatalogProduct implements wireadapt.Codec.
func (Codec) EncodeCatalogProduct(p model.CatalogProduct) ([]byte, error) {
	return json.Marshal(product(p))
}

// product renders a serving-cache product as a 2026-08-25 Product. The cache
// holds one SKU per product, with no separate product/variant identity, no
// categories, media or options, so ID and the sole Variant's ID/SKU are all
// the SKU, and every other optional field is left absent rather than
// fabricated. The single SKU is the product's one required Variant (spec:
// "first item is the featured variant").
func product(p model.CatalogProduct) v20260825.Product {
	price := v20260825.Price{
		Amount:   v20260825.Amount(p.PriceMinor),
		Currency: p.Currency,
	}
	description := v20260825.Description{Plain: p.Description}

	status := v20260825.AvailabilityStatusOutOfStock
	if p.InStock {
		status = v20260825.AvailabilityStatusInStock
	}

	variant := v20260825.Variant{
		ID:          p.SKU,
		SKU:         p.SKU,
		Title:       p.Title,
		Description: description,
		Price:       price,
		Availability: &v20260825.Availability{
			Available: p.InStock,
			Status:    status,
		},
	}

	return v20260825.Product{
		ID:          p.SKU,
		Title:       p.Title,
		Description: description,
		PriceRange:  v20260825.PriceRange{Min: price, Max: price},
		Variants:    []v20260825.Variant{variant},
	}
}
