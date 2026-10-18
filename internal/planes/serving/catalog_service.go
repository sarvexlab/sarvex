// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package serving

import "context"

// DefaultSearchLimit is used when a caller omits or supplies an
// out-of-range limit. MaxSearchLimit bounds how large a page a caller may
// request in one call.
const (
	DefaultSearchLimit = 20
	MaxSearchLimit     = 100
)

// CatalogService owns catalog read operations against the serving cache.
// Architecture §5: the discovery/read path serves only the cache — no live
// store calls, no control-plane calls — so this service's only dependency
// is ServingCache.
type CatalogService struct {
	cache ServingCache
}

// NewCatalogService constructs a CatalogService.
func NewCatalogService(cache ServingCache) *CatalogService {
	return &CatalogService{cache: cache}
}

// CatalogSearchResult is one page of search results plus the pagination
// state needed to fetch the next page.
type CatalogSearchResult struct {
	Items      []*CacheProduct
	NextCursor string
	HasMore    bool
}

// Search runs a catalog search, defaulting the limit and passing the
// cache's pagination through untouched.
//
// The cursor is the cache's to define and this service must not reinterpret
// it. That is not a stylistic preference — it is the bug this shape fixes.
// Search used to ask the cache for limit+1 items, trim back to limit, and
// substitute its own cursor: the trimmed item's SKU. But ServingCache's
// cursor is a numeric offset (cache_redis.go), so the SKU handed to a
// client came back as an unparseable cursor, the parse failure was
// discarded, the offset stayed 0, and page two was page one — forever, for
// every paginating agent, silently.
//
// Two layers disagreeing about what a cursor *is* is the whole defect, so
// only one layer decides. The over-fetch went with it: it existed only to
// find a SKU to substitute, and the cache already reports HasMore itself.
func (s *CatalogService) Search(ctx context.Context, storeID, query string, limit int, cursor string) (*CatalogSearchResult, error) {
	if limit <= 0 || limit > MaxSearchLimit {
		limit = DefaultSearchLimit
	}

	result, err := s.cache.Search(ctx, storeID, query, limit, cursor)
	if err != nil {
		return nil, err
	}

	// A nil Cursor means "no next page"; the empty string is the same
	// thing on the wire, and callers already read HasMore for the answer.
	nextCursor := ""
	if result.Cursor != nil {
		nextCursor = *result.Cursor
	}

	return &CatalogSearchResult{Items: result.Items, NextCursor: nextCursor, HasMore: result.HasMore}, nil
}

// Lookup retrieves a single catalog item by its item id (SKU).
func (s *CatalogService) Lookup(ctx context.Context, storeID, itemID string) (*CacheProduct, error) {
	return s.cache.GetProduct(ctx, storeID, itemID)
}
