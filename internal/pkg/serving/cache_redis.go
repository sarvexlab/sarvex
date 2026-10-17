// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package serving

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisCache implements ServingCache backed by Redis.
type RedisCache struct {
	client *redis.Client
	ttl    time.Duration
}

// NewRedisCache creates a ServingCache backed by Redis with explicit timeouts.
// Serving plane reads cache data synchronously; use tight timeouts (3s dial, 2s read/write)
// to prioritize agent request latency.
func NewRedisCache(addr, password string, db int, ttl time.Duration) *RedisCache {
	return &RedisCache{
		client: redis.NewClient(&redis.Options{
			Addr:         addr,
			Password:     password,
			DB:           db,
			DialTimeout:  3 * time.Second,
			ReadTimeout:  2 * time.Second,
			WriteTimeout: 2 * time.Second,
		}),
		ttl: ttl,
	}
}

// GetProduct retrieves a product from the snapshot.
func (rc *RedisCache) GetProduct(ctx context.Context, storeID, sku string) (*CacheProduct, error) {
	key := fmt.Sprintf("serving:%s:product:%s", storeID, sku)
	data, err := rc.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get product: %w", err)
	}
	var prod CacheProduct
	if err := json.Unmarshal(data, &prod); err != nil {
		return nil, fmt.Errorf("unmarshal product: %w", err)
	}
	return &prod, nil
}

// Search performs a paginated search on products.
func (rc *RedisCache) Search(ctx context.Context, storeID, query string, limit int, cursor string) (*CacheSearchResult, error) {
	// Phase 1: fetch all products for store and filter in-memory
	key := fmt.Sprintf("serving:%s:products", storeID)
	products, err := rc.client.LRange(ctx, key, 0, -1).Result()
	if err != nil {
		return nil, fmt.Errorf("search products: %w", err)
	}

	var items []*CacheProduct
	for _, prodJSON := range products {
		var prod CacheProduct
		if err := json.Unmarshal([]byte(prodJSON), &prod); err != nil {
			continue
		}
		// Simple string match on title/sku
		if query == "" || strings.Contains(strings.ToLower(prod.Title), strings.ToLower(query)) || strings.Contains(strings.ToLower(prod.SKU), strings.ToLower(query)) {
			items = append(items, &prod)
		}
	}

	// Paginate. The cursor is an offset into the filtered list, and it is
	// this cache's to define — CatalogService passes it through rather
	// than substituting one of its own (see its doc comment for what went
	// wrong when it did).
	//
	// A cursor that will not parse is rejected rather than ignored. It was
	// ignored once, and the discarded error meant a bad cursor silently
	// restarted from offset 0: the caller got page one back and had no way
	// to tell, so a paginating agent looped over it forever.
	start := 0
	if cursor != "" {
		parsed, err := strconv.Atoi(cursor)
		if err != nil || parsed < 0 {
			return nil, fmt.Errorf("%w: %q", ErrInvalidCursor, cursor)
		}
		start = parsed
	}
	end := start + limit
	if end > len(items) {
		end = len(items)
	}

	var nextCursor *string
	if end < len(items) {
		c := fmt.Sprintf("%d", end)
		nextCursor = &c
	}

	if start >= len(items) {
		return &CacheSearchResult{Items: []*CacheProduct{}, Cursor: nil, HasMore: false}, nil
	}

	return &CacheSearchResult{
		Items:   items[start:end],
		Cursor:  nextCursor,
		HasMore: end < len(items),
	}, nil
}

// GetProfile retrieves the cached profile.
func (rc *RedisCache) GetProfile(ctx context.Context, storeID string) (*CacheProfile, error) {
	keyPrefix := fmt.Sprintf("serving:%s:", storeID)
	pipe := rc.client.Pipeline()
	profileCmd := pipe.Get(ctx, keyPrefix+"profile")
	merchantCmd := pipe.Get(ctx, keyPrefix+"merchant_id")
	platformCmd := pipe.Get(ctx, keyPrefix+"platform")
	storefrontCmd := pipe.Get(ctx, keyPrefix+"storefront_url")
	_, _ = pipe.Exec(ctx) // per-command outcomes are read individually below

	data, err := profileCmd.Bytes()
	if err == redis.Nil {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get profile: %w", err)
	}
	var prof CacheProfile
	if err := json.Unmarshal(data, &prof); err != nil {
		return nil, fmt.Errorf("unmarshal profile: %w", err)
	}
	// StoreID, MerchantID, Platform and StorefrontURL are deliberately
	// excluded from the JSON (json:"-" — they must never appear in the
	// spec-compliant wire response this struct also serializes to directly),
	// so none of them survives inside the "profile" key's bytes. StoreID is
	// recovered from the lookup key itself; the rest from their own companion
	// keys, written alongside the profile by SwapSnapshot — missing only for
	// a snapshot written before those keys existed, which self-heals on the
	// next rebuild.
	//
	// Platform matters beyond bookkeeping: checkout resolves the store's
	// connector by it (connectors/base.Registry), so a profile read back
	// without one fails every checkout with "no factory registered for
	// platform \"\"" — a 503 to the agent — while the profile endpoint,
	// which never reads these fields, looks perfectly healthy.
	prof.StoreID = storeID
	if merchantID, err := merchantCmd.Result(); err == nil {
		prof.MerchantID = merchantID
	}
	if platform, err := platformCmd.Result(); err == nil {
		prof.Platform = platform
	}
	if storefrontURL, err := storefrontCmd.Result(); err == nil {
		prof.StorefrontURL = storefrontURL
	}
	return &prof, nil
}

// FreshnessWindow is how long a signalled change may go un-rebuilt before the
// snapshot counts as stale. Also the checkout freshness guard's window.
const FreshnessWindow = 5 * time.Minute

// pendingKey holds the time the oldest un-rebuilt change was signalled for a
// store. Set on serving.rebuild.requested, cleared when the snapshot swaps.
func (rc *RedisCache) pendingKey(storeID string) string {
	return fmt.Sprintf("serving:%s:pending_since", storeID)
}

// MarkPendingChange records that the store's catalog changed and the snapshot
// has not caught up. SetNX, so the marker keeps the *oldest* outstanding
// change: a burst of signals measures lag from the first, not the last.
//
// No TTL — a marker that expired on its own would report a store as level
// with its source precisely because the rebuild never happened.
func (rc *RedisCache) MarkPendingChange(ctx context.Context, storeID string) error {
	if err := rc.client.SetNX(ctx, rc.pendingKey(storeID), time.Now().UTC(), 0).Err(); err != nil {
		return fmt.Errorf("mark pending change: %w", err)
	}
	return nil
}

// Watermark returns freshness info for the snapshot.
func (rc *RedisCache) Watermark(ctx context.Context, storeID string) (*CacheWatermark, error) {
	key := fmt.Sprintf("serving:%s:watermark", storeID)
	data, err := rc.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get watermark: %w", err)
	}
	var wm CacheWatermark
	if err := json.Unmarshal(data, &wm); err != nil {
		return nil, fmt.Errorf("unmarshal watermark: %w", err)
	}
	// Staleness is "a signalled change has not been rebuilt yet", read from
	// the pending marker rather than derived from SourceSync — see
	// CacheWatermark's doc comment for why the old rule inverted.
	if pending, err := rc.client.Get(ctx, rc.pendingKey(storeID)).Time(); err == nil {
		wm.PendingSince = pending
		wm.Age = time.Since(pending)
		wm.IsStale = wm.Age > FreshnessWindow
	}
	return &wm, nil
}

// Client returns the underlying Redis client for use by other components.
func (rc *RedisCache) Client() *redis.Client {
	return rc.client
}

// SwapSnapshot atomically replaces the snapshot for a store.
// Called by sync plane (cache rebuilder).
func (rc *RedisCache) SwapSnapshot(ctx context.Context, snapshot *CacheSnapshot) error {
	keyPrefix := fmt.Sprintf("serving:%s:", snapshot.StoreID)
	pipe := rc.client.Pipeline()

	// Delete old product keys
	keys, err := rc.client.Keys(ctx, keyPrefix+"product:*").Result()
	if err == nil {
		for _, k := range keys {
			pipe.Del(ctx, k)
		}
	}

	// Write new data
	profileJSON, _ := json.Marshal(snapshot.Profile)
	pipe.Set(ctx, keyPrefix+"profile", profileJSON, rc.ttl)
	// MerchantID, Platform and StorefrontURL ride alongside the profile in
	// their own keys since all three are json:"-" on CacheProfile and never
	// survive the profile key's own JSON — see GetProfile's restoration
	// comment.
	pipe.Set(ctx, keyPrefix+"merchant_id", snapshot.MerchantID, rc.ttl)
	pipe.Set(ctx, keyPrefix+"platform", snapshot.Profile.Platform, rc.ttl)
	pipe.Set(ctx, keyPrefix+"storefront_url", snapshot.Profile.StorefrontURL, rc.ttl)

	// Write products as individual keys
	for sku, prod := range snapshot.Products {
		prodJSON, _ := json.Marshal(prod)
		pipe.Set(ctx, keyPrefix+"product:"+sku, prodJSON, rc.ttl)
	}

	// Write product list for search (simple list)
	productList := make([]interface{}, 0, len(snapshot.Products))
	for _, prod := range snapshot.Products {
		prodJSON, _ := json.Marshal(prod)
		productList = append(productList, prodJSON)
	}
	if len(productList) > 0 {
		pipe.Del(ctx, keyPrefix+"products")
		pipe.RPush(ctx, keyPrefix+"products", productList...)
	}

	// Write watermark
	watermark := &CacheWatermark{BuiltAt: snapshot.BuiltAt, SourceSync: snapshot.SourceSync}
	wmJSON, _ := json.Marshal(watermark)
	pipe.Set(ctx, keyPrefix+"watermark", wmJSON, rc.ttl)
	// The snapshot just caught up, so nothing is outstanding. A change
	// signalled while this build was in flight is cleared here too and will be
	// re-marked by its own rebuild request — the window is one build, and the
	// alternative (comparing against the build's start) buys little for a
	// signal that repeats.
	pipe.Del(ctx, rc.pendingKey(snapshot.StoreID))

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("swap snapshot: %w", err)
	}
	return nil
}
