// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"fmt"

	servingpkg "github.com/sarvexlab/sarvex/internal/pkg/serving"
)

// ProductTransformer converts CatalogProduct (sync plane) to CacheProduct (serving plane).
// The transformation is lossy by design: only serving-relevant fields are included.
type ProductTransformer interface {
	// Transform converts a scored product to its serving cache projection.
	// Idempotent: same input yields same output.
	Transform(product *CatalogProduct) (*servingpkg.CacheProduct, error)
}

// DefaultProductTransformer implements ProductTransformer.
type DefaultProductTransformer struct{}

// NewDefaultProductTransformer creates a transformer.
func NewDefaultProductTransformer() *DefaultProductTransformer {
	return &DefaultProductTransformer{}
}

// Transform maps CatalogProduct → CacheProduct.
// Lossy: omits Findings, BlocksCheckout, ReadinessScore (sync concerns).
// Preserves: SKU, Title, Price, Stock, Attributes.
func (dpt *DefaultProductTransformer) Transform(product *CatalogProduct) (*servingpkg.CacheProduct, error) {
	if product == nil {
		return nil, fmt.Errorf("product is nil")
	}

	// Extract attributes from mapped fields (ontology-mapped values).
	attributes := make(map[string]string)
	if product.MappedFields != nil {
		attributes = product.MappedFields
	}

	// Convert stock status.
	stock := servingpkg.StockLevel{
		InStock:  product.InStock,
		Quantity: product.Quantity,
	}

	// Convert price to serving format (Money with minor units + currency).
	price := servingpkg.Money{
		AmountMinor: int(product.PriceMinor),
		Currency:    product.Currency,
	}

	// Build cache product.
	cacheProduct := &servingpkg.CacheProduct{
		SKU:           product.SKU,
		Title:         product.Title,
		Description:   product.Description,
		Price:         price,
		Stock:         stock,
		Attributes:    attributes,
		LastScoreTime: product.UpdatedAt,
	}

	return cacheProduct, nil
}
