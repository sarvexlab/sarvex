// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package serving

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"
)

// fakeCatalogCache is a configurable ServingCache double for CatalogService
// tests. It records the limit/cursor CatalogService asked for, and — the
// part that matters — it *implements* the same offset-cursor pagination
// RedisCache does, rather than ignoring the cursor.
//
// It used to ignore it. That is why the pagination bug survived: the
// service handed clients a SKU as a cursor, the real cache could not parse
// it and silently restarted from offset zero, and a double that never
// paginated could not tell. A double that does not model the contract does
// not test it.
type fakeCatalogCache struct {
	items       []*CacheProduct
	searchErr   error
	productErr  error
	product     *CacheProduct
	gotLimit    int
	gotCursor   string
	searchCalls int
}

func (c *fakeCatalogCache) GetProduct(ctx context.Context, storeID, sku string) (*CacheProduct, error) {
	if c.productErr != nil {
		return nil, c.productErr
	}
	return c.product, nil
}

func (c *fakeCatalogCache) Search(ctx context.Context, storeID, query string, limit int, cursor string) (*CacheSearchResult, error) {
	c.searchCalls++
	c.gotLimit = limit
	c.gotCursor = cursor
	if c.searchErr != nil {
		return nil, c.searchErr
	}

	// Offset cursor, same contract as RedisCache.Search — including
	// rejecting one it cannot parse instead of restarting from zero.
	start := 0
	if cursor != "" {
		parsed, err := strconv.Atoi(cursor)
		if err != nil || parsed < 0 {
			return nil, fmt.Errorf("%w: %q", ErrInvalidCursor, cursor)
		}
		start = parsed
	}
	if start >= len(c.items) {
		return &CacheSearchResult{Items: []*CacheProduct{}}, nil
	}
	end := start + limit
	if end > len(c.items) {
		end = len(c.items)
	}
	var next *string
	if end < len(c.items) {
		s := strconv.Itoa(end)
		next = &s
	}
	return &CacheSearchResult{Items: c.items[start:end], Cursor: next, HasMore: end < len(c.items)}, nil
}

func (c *fakeCatalogCache) GetProfile(ctx context.Context, storeID string) (*CacheProfile, error) {
	return nil, ErrNotFound
}

func (c *fakeCatalogCache) Watermark(ctx context.Context, storeID string) (*CacheWatermark, error) {
	return &CacheWatermark{}, nil
}

func skuProducts(n int) []*CacheProduct {
	out := make([]*CacheProduct, n)
	for i := range out {
		out[i] = &CacheProduct{SKU: "SKU-" + string(rune('A'+i))}
	}
	return out
}

func TestCatalogService_Search_DefaultsTheLimit(t *testing.T) {
	cache := &fakeCatalogCache{items: skuProducts(5)}
	svc := NewCatalogService(cache)

	result, err := svc.Search(context.Background(), "store-1", "widget", 0, "")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	// The requested limit is passed through as-is. It used to be limit+1,
	// so the service could peek at one extra item and substitute its SKU
	// as the cursor — the over-fetch and the substitution were one
	// mistake, and they are removed together.
	if cache.gotLimit != DefaultSearchLimit {
		t.Errorf("expected cache to be asked for the limit %d, got %d", DefaultSearchLimit, cache.gotLimit)
	}
	if result.HasMore {
		t.Errorf("expected has_more=false with only 5 items under the default limit, got true")
	}
	if len(result.Items) != 5 {
		t.Errorf("expected all 5 items returned, got %d", len(result.Items))
	}
}

// The cursor belongs to the cache. The service must hand back exactly what
// the cache gave it, because that is the only value the cache can read
// again. Substituting a SKU here is what broke pagination outright.
func TestCatalogService_Search_PropagatesTheCachesCursorVerbatim(t *testing.T) {
	cache := &fakeCatalogCache{items: skuProducts(4)}
	svc := NewCatalogService(cache)

	result, err := svc.Search(context.Background(), "store-1", "", 3, "")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !result.HasMore {
		t.Fatal("expected has_more=true with 4 items and a limit of 3")
	}
	if len(result.Items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(result.Items))
	}
	if result.NextCursor != "3" {
		t.Errorf("expected the cache's own offset cursor %q, got %q — a cursor the cache cannot parse "+
			"makes the next page silently restart from the first", "3", result.NextCursor)
	}
}

// Walking every page must visit each product exactly once and terminate.
// This is the assertion whose absence let page two be page one forever.
func TestCatalogService_Search_WalksEveryPageExactlyOnce(t *testing.T) {
	const total, pageSize = 7, 2
	cache := &fakeCatalogCache{items: skuProducts(total)}
	svc := NewCatalogService(cache)

	seen := map[string]int{}
	cursor := ""
	for pages := 0; ; pages++ {
		if pages > total {
			t.Fatalf("pagination did not terminate after %d pages for %d items", pages, total)
		}
		result, err := svc.Search(context.Background(), "store-1", "", pageSize, cursor)
		if err != nil {
			t.Fatalf("Search(cursor=%q): %v", cursor, err)
		}
		for _, item := range result.Items {
			seen[item.SKU]++
		}
		if !result.HasMore {
			break
		}
		if result.NextCursor == "" {
			t.Fatal("has_more=true but no cursor was returned, so the next page is unreachable")
		}
		cursor = result.NextCursor
	}

	if len(seen) != total {
		t.Errorf("walked %d distinct products, want %d", len(seen), total)
	}
	for sku, count := range seen {
		if count != 1 {
			t.Errorf("product %s was returned %d times, want exactly 1", sku, count)
		}
	}
}

// A cursor the cache cannot read is an error the caller sees, not a silent
// restart. The handler maps it to 400.
func TestCatalogService_Search_RejectsAnUnreadableCursor(t *testing.T) {
	cache := &fakeCatalogCache{items: skuProducts(5)}
	svc := NewCatalogService(cache)

	_, err := svc.Search(context.Background(), "store-1", "", 2, "SKU-A")
	if !errors.Is(err, ErrInvalidCursor) {
		t.Errorf("expected ErrInvalidCursor for a non-offset cursor, got %v", err)
	}
}

func TestCatalogService_Search_ClampsOutOfRangeLimit(t *testing.T) {
	cache := &fakeCatalogCache{items: skuProducts(1)}
	svc := NewCatalogService(cache)

	if _, err := svc.Search(context.Background(), "store-1", "", 1000, ""); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if cache.gotLimit != DefaultSearchLimit {
		t.Errorf("expected an out-of-range limit to clamp to the default, got underlying cache limit %d", cache.gotLimit)
	}

	cache.gotLimit = 0
	if _, err := svc.Search(context.Background(), "store-1", "", -5, ""); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if cache.gotLimit != DefaultSearchLimit {
		t.Errorf("expected a negative limit to clamp to the default, got underlying cache limit %d", cache.gotLimit)
	}
}

func TestCatalogService_Search_PropagatesCacheError(t *testing.T) {
	wantErr := ErrNotFound
	cache := &fakeCatalogCache{searchErr: wantErr}
	svc := NewCatalogService(cache)

	if _, err := svc.Search(context.Background(), "store-1", "", 0, ""); err != wantErr {
		t.Errorf("expected the cache's error to propagate unchanged, got %v", err)
	}
}

func TestCatalogService_Lookup_PassesThrough(t *testing.T) {
	want := &CacheProduct{SKU: "SKU-1"}
	cache := &fakeCatalogCache{product: want}
	svc := NewCatalogService(cache)

	got, err := svc.Lookup(context.Background(), "store-1", "SKU-1")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if got != want {
		t.Errorf("expected the cache's product returned unchanged, got %+v", got)
	}
}

func TestCatalogService_Lookup_NotFoundPropagates(t *testing.T) {
	cache := &fakeCatalogCache{productErr: ErrNotFound}
	svc := NewCatalogService(cache)

	if _, err := svc.Lookup(context.Background(), "store-1", "missing"); err != ErrNotFound {
		t.Errorf("expected ErrNotFound to propagate, got %v", err)
	}
}
