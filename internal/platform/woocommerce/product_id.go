// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package woocommerce

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"sync"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
)

// productIDCache memoises SKU → WooCommerce product id per connector instance.
// The mapping is immutable for the life of a product, and a checkout resolves
// the same SKUs repeatedly across create/update/complete, so this keeps the
// lookup to one call per SKU rather than one per line item per request.
type productIDCache struct {
	mu sync.RWMutex
	m  map[string]int
}

func newProductIDCache() *productIDCache { return &productIDCache{m: map[string]int{}} }

func (c *productIDCache) get(sku string) (int, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	id, ok := c.m[sku]
	return id, ok
}

func (c *productIDCache) put(sku string, id int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[sku] = id
}

// resolveProductID turns a UCP item id into the numeric product id WooCommerce
// needs on a line item.
//
// Agents address items by the id Sarvex publishes in its catalog, which is the
// SKU (see internal/spec/vYYYYMMDD/codec/catalog.go). WooCommerce's line_items.product_id
// is the numeric post id, with sku as a separate field — so passing the UCP id
// straight through produced a body WooCommerce could not decode at all
// ("rest_invalid_param: Invalid JSON body"), failing every checkout before it
// reached the store.
//
// An id that is already numeric is taken as-is: a caller that legitimately has
// the platform-native id (a variant id, or a store whose SKUs are numeric)
// should not pay for a lookup.
func (c *Connector) resolveProductID(ctx context.Context, itemID string) (int, error) {
	if itemID == "" {
		return 0, fmt.Errorf("woocommerce: empty item id")
	}
	if id, err := strconv.Atoi(itemID); err == nil {
		return id, nil
	}
	if id, ok := c.productIDs.get(itemID); ok {
		return id, nil
	}

	// WooCommerce's product list filters by exact SKU. It returns an array, so
	// a miss is an empty one rather than a 404.
	var products []wcProduct
	path := "/products?sku=" + url.QueryEscape(itemID)
	if err := c.client.GET(ctx, path, &products); err != nil {
		return 0, fmt.Errorf("woocommerce: look up product id for sku %q: %w", itemID, err)
	}
	for _, p := range products {
		if p.SKU == itemID && p.ID != 0 {
			c.productIDs.put(itemID, p.ID)
			return p.ID, nil
		}
	}
	return 0, fmt.Errorf("%w: no product for sku %q", ErrNotFound, itemID)
}

// lineItem is the (item id, quantity) pair the two request shapes share.
// base has a separate Create and Update line-item type with identical fields
// here, and Go cannot read a field off a union of structs, so they are
// normalised rather than duplicating the resolution loop.
type lineItem struct {
	itemID   string
	quantity int
}

func createLineItems(reqs []base.LineItemCreateRequest) []lineItem {
	out := make([]lineItem, len(reqs))
	for i, r := range reqs {
		out[i] = lineItem{itemID: r.Item.ID, quantity: r.Quantity}
	}
	return out
}

func updateLineItems(reqs []base.LineItemUpdateRequest) []lineItem {
	out := make([]lineItem, len(reqs))
	for i, r := range reqs {
		out[i] = lineItem{itemID: r.Item.ID, quantity: r.Quantity}
	}
	return out
}

// wcLineItems builds the line_items payload, resolving each UCP item id to the
// numeric product id WooCommerce requires.
func (c *Connector) wcLineItems(ctx context.Context, items []lineItem) ([]map[string]any, error) {
	out := make([]map[string]any, 0, len(items))
	for _, li := range items {
		productID, err := c.resolveProductID(ctx, li.itemID)
		if err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"product_id": productID,
			"quantity":   li.quantity,
		})
	}
	return out, nil
}
